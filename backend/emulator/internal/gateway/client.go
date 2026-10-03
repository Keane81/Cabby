// Package gateway is the emulator's client of the public REST contract of the gateway. It does
// what a real cabber client does and nothing more: register, create a session, record a
// location, delete the session (FR-001).
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	// AuthTimeout bounds registration, session creation and deletion.
	AuthTimeout = 10 * time.Second
	// LocationTimeout bounds one location send; the gateway itself gives up after 2 s.
	LocationTimeout = 5 * time.Second

	maxResponseBytes = 4 << 10
)

// Client talks to one gateway through one shared connection pool.
type Client struct {
	base            string
	http            *http.Client
	authTimeout     time.Duration
	locationTimeout time.Duration
}

// New builds a client with the pool the run needs: HTTP/1.1, keep-alive, no compression, at most
// maxConns connections (research.md R-02).
func New(target string, maxConns int) *Client {
	transport := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxConnsPerHost:     maxConns,
		MaxIdleConnsPerHost: maxConns,
		MaxIdleConns:        maxConns,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
	}
	return NewWithHTTPClient(target, &http.Client{Transport: transport})
}

// NewWithHTTPClient is New with a caller-supplied HTTP client, for tests.
func NewWithHTTPClient(target string, httpClient *http.Client) *Client {
	return &Client{
		base:            strings.TrimRight(target, "/"),
		http:            httpClient,
		authTimeout:     AuthTimeout,
		locationTimeout: LocationTimeout,
	}
}

type registrationBody struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type locationBody struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type sessionAnswer struct {
	AccessToken string `json:"access_token"`
}

// Register creates the account (201).
func (c *Client) Register(ctx context.Context, name, email, password string) Kind {
	kind, _ := c.do(ctx, c.authTimeout, http.MethodPost, "/cabbers", "", registrationBody{name, email, password}, http.StatusCreated, nil)
	return kind
}

// Login creates a session and returns its bearer token (201).
func (c *Client) Login(ctx context.Context, email, password string) (string, Kind) {
	var answer sessionAnswer
	kind, _ := c.do(ctx, c.authTimeout, http.MethodPost, "/cabber/session", "", sessionBody{email, password}, http.StatusCreated, &answer)
	if kind == OK && answer.AccessToken == "" {
		return "", Unexpected
	}
	return answer.AccessToken, kind
}

// RecordLocation adds one position of the session's owner (201).
func (c *Client) RecordLocation(ctx context.Context, token string, latitude, longitude float64) Kind {
	kind, _ := c.do(ctx, c.locationTimeout, http.MethodPost, "/cabber/location", token, locationBody{latitude, longitude}, http.StatusCreated, nil)
	return kind
}

// Logout revokes the presented session (204).
func (c *Client) Logout(ctx context.Context, token string) Kind {
	kind, _ := c.do(ctx, c.authTimeout, http.MethodDelete, "/cabber/session", token, nil, http.StatusNoContent, nil)
	return kind
}

// Health asks the liveness endpoint, used once at start to tell an unreachable target early.
func (c *Client) Health(ctx context.Context) Kind {
	kind, _ := c.do(ctx, c.locationTimeout, http.MethodGet, "/healthz", "", nil, http.StatusOK, nil)
	return kind
}

// do performs one call. A body is decoded into answer only on the expected status; the rest of
// every response is drained so the connection returns to the pool.
func (c *Client) do(ctx context.Context, timeout time.Duration, method, path, token string, body any, want int, answer any) (Kind, int) {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return Unexpected, 0
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(callCtx, method, c.base+path, reader)
	if err != nil {
		return Unexpected, 0
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return classifyError(ctx, err), 0
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxResponseBytes)
	kind := classifyStatus(resp.StatusCode, want)
	if kind == OK && answer != nil {
		if err := json.NewDecoder(limited).Decode(answer); err != nil {
			kind = Unexpected
		}
	}
	_, _ = io.Copy(io.Discard, limited)
	return kind, resp.StatusCode
}
