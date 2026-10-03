package config

import "testing"

func TestTargetLoopbackAccepted(t *testing.T) {
	for _, target := range []string{
		"http://127.0.0.1:8080",
		"http://127.1.2.3:8080",
		"http://[::1]:8080",
		"http://localhost:8080",
		"http://LOCALHOST",
		"http://gateway.localhost:8080",
	} {
		p := Default()
		p.Target = target
		if err := p.Validate(); err != nil {
			t.Errorf("%s refused: %v", target, err)
		}
	}
}

func TestTargetRemoteNeedsFlag(t *testing.T) {
	for _, target := range []string{
		"https://example.com",
		"http://10.0.0.5:8080",
		"http://localhost.example.com",
		"http://notlocalhost",
	} {
		p := Default()
		p.Target = target
		if err := p.Validate(); err == nil {
			t.Errorf("%s accepted without -allow-remote", target)
		}
		p.AllowRemote = true
		if err := p.Validate(); err != nil {
			t.Errorf("%s refused with -allow-remote: %v", target, err)
		}
	}
}

func TestTargetNeedsHttpScheme(t *testing.T) {
	p := Default()
	p.AllowRemote = true
	for _, target := range []string{"ftp://127.0.0.1", "127.0.0.1:8080", "http://"} {
		p.Target = target
		if err := p.Validate(); err == nil {
			t.Errorf("%q accepted", target)
		}
	}
}
