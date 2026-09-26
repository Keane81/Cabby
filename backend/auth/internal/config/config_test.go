package config

import "testing"

func TestLoadDefaultsGRPCPort(t *testing.T) {
	t.Setenv("CABBY_AUTH_DB_URL", "postgres://auth:secret@127.0.0.1:5432/auth")
	t.Setenv("CABBY_AUTH_GRPC_PORT", "")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.GRPCAddress != ":9093" {
		t.Fatalf("GRPCAddress = %q, want :9093", config.GRPCAddress)
	}
	if config.DatabaseURL == "" {
		t.Fatal("DatabaseURL must carry the DSN")
	}
}

func TestLoadKeepsExplicitGRPCPort(t *testing.T) {
	t.Setenv("CABBY_AUTH_DB_URL", "postgres://auth:secret@127.0.0.1:5432/auth")
	t.Setenv("CABBY_AUTH_GRPC_PORT", "9099")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.GRPCAddress != ":9099" {
		t.Fatalf("GRPCAddress = %q, want :9099", config.GRPCAddress)
	}
}

func TestLoadRejectsNonNumericGRPCPort(t *testing.T) {
	t.Setenv("CABBY_AUTH_DB_URL", "postgres://auth:secret@127.0.0.1:5432/auth")
	t.Setenv("CABBY_AUTH_GRPC_PORT", "grpc")

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a non-numeric CABBY_AUTH_GRPC_PORT")
	}
}

func TestLoadFailsWithoutDatabaseURL(t *testing.T) {
	t.Setenv("CABBY_AUTH_DB_URL", "")

	config, err := Load()
	if err == nil {
		t.Fatal("Load without CABBY_AUTH_DB_URL must fail before the service starts")
	}
	if config != (Config{}) {
		t.Fatalf("failed Load returned a partial config: %+v", config)
	}
}

func TestMetricsAddressIsFixed(t *testing.T) {
	t.Setenv("CABBY_AUTH_DB_URL", "postgres://auth:secret@127.0.0.1:5432/auth")
	t.Setenv("CABBY_AUTH_METRICS_PORT", "9999")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if config.MetricsAddress != ":9094" {
		t.Fatalf("MetricsAddress = %q, want the fixed :9094 (R-11)", config.MetricsAddress)
	}
}
