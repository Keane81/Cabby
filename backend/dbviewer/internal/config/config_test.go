package config

import "testing"

func TestLoadRequiresBothDSNs(t *testing.T) {
	t.Setenv("CABBY_DBVIEWER_AUTH_DB_URL", "")
	t.Setenv("CABBY_DBVIEWER_LOCATION_DB_URL", "postgres://x")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a missing auth DSN")
	}

	t.Setenv("CABBY_DBVIEWER_AUTH_DB_URL", "postgres://x")
	t.Setenv("CABBY_DBVIEWER_LOCATION_DB_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a missing location DSN")
	}
}

func TestLoadDefaultPort(t *testing.T) {
	t.Setenv("CABBY_DBVIEWER_AUTH_DB_URL", "postgres://a")
	t.Setenv("CABBY_DBVIEWER_LOCATION_DB_URL", "postgres://l")
	t.Setenv("CABBY_DBVIEWER_HTTP_PORT", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddress != ":8090" {
		t.Fatalf("HTTPAddress = %q, want :8090", cfg.HTTPAddress)
	}
}

func TestLoadRejectsBadPort(t *testing.T) {
	t.Setenv("CABBY_DBVIEWER_AUTH_DB_URL", "postgres://a")
	t.Setenv("CABBY_DBVIEWER_LOCATION_DB_URL", "postgres://l")
	for _, port := range []string{"0", "x", "70000"} {
		t.Setenv("CABBY_DBVIEWER_HTTP_PORT", port)
		if _, err := Load(); err == nil {
			t.Fatalf("Load() accepted port %q", port)
		}
	}
}
