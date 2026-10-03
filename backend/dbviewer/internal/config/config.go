// Package config defines the environment contract of the DB viewer: what it needs to start and
// what it refuses to start without.
package config

import (
	"errors"
	"net"
	"os"
	"strconv"
)

const defaultHTTPPort = "8090"

// Config is the validated startup configuration.
type Config struct {
	// AuthDatabaseURL is the PostgreSQL DSN of the `auth` database.
	AuthDatabaseURL string
	// LocationDatabaseURL is the PostgreSQL DSN of the `location` database.
	LocationDatabaseURL string
	// HTTPAddress is the listen address of the page and its API.
	HTTPAddress string
}

// Load reads the environment and fails fast on a missing database DSN. No error text repeats a
// DSN: it carries the password of the database role.
func Load() (Config, error) {
	authURL := os.Getenv("CABBY_DBVIEWER_AUTH_DB_URL")
	if authURL == "" {
		return Config{}, errors.New("CABBY_DBVIEWER_AUTH_DB_URL is required")
	}
	locationURL := os.Getenv("CABBY_DBVIEWER_LOCATION_DB_URL")
	if locationURL == "" {
		return Config{}, errors.New("CABBY_DBVIEWER_LOCATION_DB_URL is required")
	}
	port := os.Getenv("CABBY_DBVIEWER_HTTP_PORT")
	if port == "" {
		port = defaultHTTPPort
	}
	if number, err := strconv.ParseUint(port, 10, 16); err != nil || number == 0 {
		return Config{}, errors.New("CABBY_DBVIEWER_HTTP_PORT must be a number between 1 and 65535")
	}
	return Config{
		AuthDatabaseURL:     authURL,
		LocationDatabaseURL: locationURL,
		HTTPAddress:         net.JoinHostPort("", port),
	}, nil
}
