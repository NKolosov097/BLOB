package config

import (
	"errors"
	"testing"
)

const validBrowserKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" // 32 zero bytes, base64 standard

func TestLoadIdentityAcceptsValidConfig(t *testing.T) {
	t.Setenv("AUTH_PUBLIC_ORIGIN", "https://localhost:8443")
	t.Setenv("AUTH_BROWSER_KEY", validBrowserKey)

	cfg, err := LoadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicOrigin != "https://localhost:8443" {
		t.Fatalf("got %q", cfg.PublicOrigin)
	}
	var zero [32]byte
	if cfg.BrowserKey != zero {
		t.Fatal("browser key was not decoded")
	}
}

func TestLoadIdentityAcceptsCanonicalOriginWithoutExplicitPort(t *testing.T) {
	t.Setenv("AUTH_PUBLIC_ORIGIN", "https://example.com")
	t.Setenv("AUTH_BROWSER_KEY", validBrowserKey)

	cfg, err := LoadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicOrigin != "https://example.com" {
		t.Fatalf("got %q", cfg.PublicOrigin)
	}
}

func TestLoadIdentityRejectsMalformedOrigin(t *testing.T) {
	for _, origin := range []string{
		"",
		"http://localhost:8443",
		"https://localhost:8443/",
		"https://user:pass@localhost:8443",
		"https://localhost:8443/path",
		"https://localhost:8443?query=1",
		"https://localhost:8443#fragment",
		"https://example.com?",
		"https://example.com#",
		"https://EXAMPLE.com",
		"https://example.com:443",
		"https://example.com:",
		"https://example.com:08443",
		"https://:8443",
		"https://example.com:65536",
		"not a url",
	} {
		t.Run(origin, func(t *testing.T) {
			t.Setenv("AUTH_PUBLIC_ORIGIN", origin)
			t.Setenv("AUTH_BROWSER_KEY", validBrowserKey)

			_, err := LoadIdentity()
			if !errors.Is(err, ErrIdentityConfigInvalid) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestLoadIdentityRejectsMalformedKey(t *testing.T) {
	for _, key := range []string{"", "not-base64!!", "QQ==", validBrowserKey + "QQ=="} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("AUTH_PUBLIC_ORIGIN", "https://localhost:8443")
			t.Setenv("AUTH_BROWSER_KEY", key)

			_, err := LoadIdentity()
			if !errors.Is(err, ErrIdentityConfigInvalid) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestLoadIdentityNeverLogsSuppliedValues(t *testing.T) {
	t.Setenv("AUTH_PUBLIC_ORIGIN", "https://credential-canary.invalid")
	t.Setenv("AUTH_BROWSER_KEY", "credential-canary")

	_, err := LoadIdentity()
	if !errors.Is(err, ErrIdentityConfigInvalid) {
		t.Fatalf("got %v", err)
	}
	if err.Error() != "identity_config_invalid" {
		t.Fatalf("error leaked supplied values: %v", err)
	}
}
