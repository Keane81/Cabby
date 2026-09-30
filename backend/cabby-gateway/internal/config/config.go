// Package config defines the environment contract of the gateway: what it needs to start and what
// it refuses to start without.
package config

import (
	"errors"
	"net"
	"os"
	"strconv"
)

const (
	defaultPublicPort = "8080"
	// The metrics address is a constant on purpose: it is never published to the host, and a
	// mismatch with the Prometheus target shows up as an empty dashboard rather than a running but
	// unobserved service.
	metricsAddress = ":9091"
)

// Config is the validated startup configuration.
type Config struct {
	// AuthAddress is the address of the auth gRPC service. It is required, but dialing it connects
	// nowhere: grpc dials lazily, so the health-check keeps answering while auth is down.
	AuthAddress string
	// PublicAddress is the listen address of the public HTTP port.
	PublicAddress string
	// MetricsAddress is the listen address of the Prometheus endpoint.
	MetricsAddress string
}

// Load reads the environment and fails fast on a missing auth address or an unusable port.
func Load() (Config, error) {
	authAddress := os.Getenv("CABBY_AUTH_ADDR")
	if authAddress == "" {
		return Config{}, errors.New("CABBY_AUTH_ADDR is not set")
	}
	port := os.Getenv("CABBY_GATEWAY_PORT")
	if port == "" {
		port = defaultPublicPort
	}
	if number, err := strconv.ParseUint(port, 10, 16); err != nil || number == 0 {
		return Config{}, errors.New("CABBY_GATEWAY_PORT must be a number between 1 and 65535")
	}
	return Config{
		AuthAddress:    authAddress,
		PublicAddress:  net.JoinHostPort("", port),
		MetricsAddress: metricsAddress,
	}, nil
}
