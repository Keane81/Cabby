package config

import (
	"strings"
	"testing"
)

const testDSN = "postgres://location:secret-pass@127.0.0.1:5432/location"

func TestLoadDefaultsGRPCPort(t *testing.T) {
	t.Setenv("CABBY_LOCATION_DB_URL", testDSN)
	t.Setenv("CABBY_LOCATION_GRPC_PORT", "")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.GRPCAddress != ":9095" {
		t.Fatalf("GRPCAddress = %q, want :9095", config.GRPCAddress)
	}
	if config.MetricsAddress != ":9096" {
		t.Fatalf("MetricsAddress = %q, want :9096", config.MetricsAddress)
	}
	if config.DatabaseURL != testDSN {
		t.Fatal("DatabaseURL must carry the DSN")
	}
}

func TestLoadKeepsExplicitGRPCPort(t *testing.T) {
	t.Setenv("CABBY_LOCATION_DB_URL", testDSN)
	t.Setenv("CABBY_LOCATION_GRPC_PORT", "9099")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.GRPCAddress != ":9099" {
		t.Fatalf("GRPCAddress = %q, want :9099", config.GRPCAddress)
	}
}

func TestLoadRejectsInvalidGRPCPort(t *testing.T) {
	for _, port := range []string{"grpc", "0", "70000", "-1"} {
		t.Run(port, func(t *testing.T) {
			t.Setenv("CABBY_LOCATION_DB_URL", testDSN)
			t.Setenv("CABBY_LOCATION_GRPC_PORT", port)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load accepted CABBY_LOCATION_GRPC_PORT=%q", port)
			}
			if strings.Contains(err.Error(), "secret-pass") {
				t.Fatal("the error repeats the DSN")
			}
		})
	}
}

func TestLoadFailsWithoutDatabaseURL(t *testing.T) {
	t.Setenv("CABBY_LOCATION_DB_URL", "")

	config, err := Load()
	if err == nil {
		t.Fatal("Load without CABBY_LOCATION_DB_URL must fail before the service starts")
	}
	if config != (Config{}) {
		t.Fatalf("Load returned %+v with an error, want the zero Config", config)
	}
}
