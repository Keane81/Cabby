package config

import "testing"

func TestLoadRequiresTheAuthAddress(t *testing.T) {
	t.Setenv("CABBY_AUTH_ADDR", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a missing CABBY_AUTH_ADDR")
	}
}

func TestLoadDefaultsThePublicPort(t *testing.T) {
	t.Setenv("CABBY_AUTH_ADDR", "auth:9093")
	t.Setenv("CABBY_GATEWAY_PORT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PublicAddress != ":8080" || cfg.AuthAddress != "auth:9093" || cfg.MetricsAddress != ":9091" {
		t.Errorf("Load = %+v", cfg)
	}
}

func TestLoadRejectsAnUnusablePort(t *testing.T) {
	t.Setenv("CABBY_AUTH_ADDR", "auth:9093")
	for _, port := range []string{"0", "65536", "http", "-1"} {
		t.Setenv("CABBY_GATEWAY_PORT", port)
		if _, err := Load(); err == nil {
			t.Errorf("Load accepted CABBY_GATEWAY_PORT=%q", port)
		}
	}
}
