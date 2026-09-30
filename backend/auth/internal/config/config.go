// Package config defines the environment contract of the auth service: what it needs to
// start and what it refuses to start without (research R-11).
package config

import (
	"errors"
	"net"
	"os"
	"strconv"
)

// Defaults and fixed addresses. The metrics port is a constant on purpose: it is never
// published outside the compose network, and a mismatch with the Prometheus target shows up
// as an empty dashboard rather than a running-but-unobserved service.
const (
	defaultGRPCPort = "9093"
	metricsAddress  = ":9094"
)

// Config is the validated startup configuration.
type Config struct {
	// DatabaseURL is the PostgreSQL DSN of the `auth` database.
	DatabaseURL string
	// GRPCAddress is the listen address of the auth.v1 service.
	GRPCAddress string
	// MetricsAddress is the listen address of the Prometheus endpoint.
	MetricsAddress string
}

// Load reads the environment and fails fast on a missing database DSN.
func Load() (Config, error) {
	databaseURL := os.Getenv("CABBY_AUTH_DB_URL")
	if databaseURL == "" {
		return Config{}, errors.New("CABBY_AUTH_DB_URL is required")
	}
	port := os.Getenv("CABBY_AUTH_GRPC_PORT")
	if port == "" {
		port = defaultGRPCPort
	}
	if number, err := strconv.ParseUint(port, 10, 16); err != nil || number == 0 {
		return Config{}, errors.New("CABBY_AUTH_GRPC_PORT must be a number between 1 and 65535")
	}
	return Config{
		DatabaseURL:    databaseURL,
		GRPCAddress:    net.JoinHostPort("", port),
		MetricsAddress: metricsAddress,
	}, nil
}
