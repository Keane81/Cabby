// Package authclient is the gRPC transport of the auth service behind the Operations port. It
// owns the shape of one call — its deadline and the translation of a gRPC status into a domain
// result — and no business rule: the service decides those (plan.md §Structure).
package authclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Keane81/Cabby/backend/cabby-gateway/internal/requestid"
	"github.com/Keane81/Cabby/backend/contracts/authpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// CallTimeout bounds one call to the auth service (R-09). A slow answer is reported as an
// unavailable dependency rather than kept waiting for.
const CallTimeout = 2 * time.Second

// Cabber and Session are the domain results of the port, in the vocabulary of the gateway: no
// protobuf type crosses into internal/server.
type Cabber struct {
	ID    string
	Email string
}

type Session struct {
	AccessToken string
	ExpiresAt   time.Time
}

// Invalid reports the field a request was rejected on and why (FR-006). The value the client sent
// is never part of it (FR-004).
type Invalid struct {
	Field  string
	Reason string
}

func (i Invalid) Error() string {
	return fmt.Sprintf("authclient: field %s is %s", i.Field, i.Reason)
}

// Failures of the dependency and of the case, kept apart from a rejection by their identity so the
// server can map them without reading a message.
var (
	ErrEmailTaken   = errors.New("authclient: email is taken")
	ErrUnauthorized = errors.New("authclient: access is not valid")
	ErrUnavailable  = errors.New("authclient: auth service is unavailable")
	ErrInternal     = errors.New("authclient: auth service failed")
)

// Operations is the port the public server drives. The fake of the tests and the gRPC client
// below implement the same four methods.
type Operations interface {
	RegisterCabber(ctx context.Context, name, email, password string) (Cabber, error)
	CreateCabberSession(ctx context.Context, email, password string) (Session, error)
	DeleteCabberSession(ctx context.Context, accessToken string) error
	// VerifyCabberSession confirms an access and names the cabber it belongs to. It is how an
	// operation that needs a subject learns who is asking (spec 004 FR-009): the answer is the only
	// source of the identifier a later call acts on.
	VerifyCabberSession(ctx context.Context, accessToken string) (cabberID string, err error)
}

// Client talks to auth.v1.AuthService over one connection. grpc-go dials lazily and retries
// nothing by default, which is exactly the policy of R-09: a call is made once or not at all.
type Client struct {
	conn    *grpc.ClientConn
	api     authpb.AuthServiceClient
	timeout time.Duration
}

// Dial prepares the client for the address of CABBY_AUTH_ADDR. It does not connect, so a gateway
// that starts without auth reachable still serves /healthz (plan.md §Constraints).
func Dial(address string) (*Client, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("authclient: cannot dial %s: %w", address, err)
	}
	return &Client{conn: conn, api: authpb.NewAuthServiceClient(conn), timeout: CallTimeout}, nil
}

// Close releases the connection during graceful shutdown.
func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) RegisterCabber(ctx context.Context, name, email, password string) (Cabber, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	response, err := c.api.RegisterCabber(withRequestID(callCtx), &authpb.RegisterCabberRequest{
		Name: name, Email: email, Password: password,
	})
	if err != nil {
		return Cabber{}, translate(err)
	}
	return Cabber{ID: response.GetCabberId(), Email: response.GetEmail()}, nil
}

func (c *Client) CreateCabberSession(ctx context.Context, email, password string) (Session, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	response, err := c.api.CreateCabberSession(withRequestID(callCtx), &authpb.CreateCabberSessionRequest{
		Email: email, Password: password,
	})
	if err != nil {
		return Session{}, translate(err)
	}
	return Session{
		AccessToken: response.GetAccessToken(),
		ExpiresAt:   time.Unix(response.GetExpiresAtUnix(), 0).UTC(),
	}, nil
}

func (c *Client) DeleteCabberSession(ctx context.Context, accessToken string) error {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	_, err := c.api.DeleteCabberSession(withRequestID(callCtx), &authpb.DeleteCabberSessionRequest{
		AccessToken: accessToken,
	})
	return translate(err)
}

func (c *Client) VerifyCabberSession(ctx context.Context, accessToken string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	response, err := c.api.VerifyCabberSession(withRequestID(callCtx), &authpb.VerifyCabberSessionRequest{
		AccessToken: accessToken,
	})
	if err != nil {
		return "", translate(err)
	}
	return response.GetCabberId(), nil
}

// withRequestID attaches the identifier the public server minted for this request. A call that came
// by another route carries none, and inventing one here would put a value in the log of auth that no
// line of ours repeats.
func withRequestID(ctx context.Context) context.Context {
	id := requestid.From(ctx)
	if id == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, requestid.MetadataKey, id)
}

// translate maps a gRPC status onto the domain results of the port, following the table of
// data-model §6. Nothing of the status message travels on: it can carry a value from the request.
func translate(err error) error {
	if err == nil {
		return nil
	}
	st := status.Convert(err)
	switch st.Code() {
	case codes.InvalidArgument:
		return fieldOf(st)
	case codes.AlreadyExists:
		return ErrEmailTaken
	case codes.Unauthenticated:
		return ErrUnauthorized
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return ErrUnavailable
	default:
		return ErrInternal
	}
}

// The request fields the contract allows in ErrorField (contracts/auth.proto). They are the only
// values a field of a peer can take on its way into an answer of ours.
const (
	FieldName     = "name"
	FieldEmail    = "email"
	FieldPassword = "password"
)

var fields = map[string]bool{FieldName: true, FieldEmail: true, FieldPassword: true}

// fieldOf reads the first ErrorField detail of a status. A validation failure without the detail
// keeps the field empty, and the client then receives a rejection that names no field.
func fieldOf(st *status.Status) error {
	for _, detail := range st.Details() {
		field, ok := detail.(*authpb.ErrorField)
		if !ok {
			continue
		}
		if fields[field.GetField()] {
			return Invalid{Field: field.GetField(), Reason: field.GetReason()}
		}
	}
	return Invalid{}
}
