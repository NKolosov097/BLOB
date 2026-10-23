package config

import (
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

var ErrIdentityConfigInvalid = errors.New("identity_config_invalid")

// Identity holds API-only browser-session configuration. The worker process
// never loads this; fixed session lifetimes live as constants in the
// identity module, not here.
type Identity struct {
	PublicOrigin string
	BrowserKey   [32]byte
}

func LoadIdentity() (Identity, error) {
	origin := strings.TrimSpace(os.Getenv("AUTH_PUBLIC_ORIGIN"))
	if err := validatePublicOrigin(origin); err != nil {
		return Identity{}, ErrIdentityConfigInvalid
	}
	keyValue := strings.TrimSpace(os.Getenv("AUTH_BROWSER_KEY"))
	decoded, err := base64.StdEncoding.DecodeString(keyValue)
	if err != nil || len(decoded) != 32 {
		return Identity{}, ErrIdentityConfigInvalid
	}
	var key [32]byte
	copy(key[:], decoded)
	return Identity{PublicOrigin: origin, BrowserKey: key}, nil
}

// validatePublicOrigin requires a canonical origin a browser could actually
// send as its Origin header: HTTPS, no userinfo/path/query/fragment
// (including an empty "?" or "#" delimiter with nothing after it), a
// lowercase host, and no explicit default HTTPS port. The result is later
// compared byte-for-byte against the request Origin header, so any
// noncanonical spelling here would make every legitimate mutation fail.
func validatePublicOrigin(origin string) error {
	if origin == "" || strings.HasSuffix(origin, "/") {
		return ErrIdentityConfigInvalid
	}
	if strings.ContainsAny(origin, "?#@") {
		return ErrIdentityConfigInvalid
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return ErrIdentityConfigInvalid
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.ForceQuery {
		return ErrIdentityConfigInvalid
	}

	host, port, splitErr := net.SplitHostPort(parsed.Host)
	switch {
	case splitErr != nil:
		// No colon at all (the common case): the whole Host is the
		// hostname, with no port.
		host, port = parsed.Host, ""
	case port == "":
		// A colon with nothing after it ("example.com:") is not something
		// a browser would ever send as its Origin.
		return ErrIdentityConfigInvalid
	}
	if host == "" {
		return ErrIdentityConfigInvalid
	}
	if host != strings.ToLower(host) {
		return ErrIdentityConfigInvalid
	}
	if port != "" {
		// Require canonical decimal spelling (no leading zero, no sign) so
		// a byte-for-byte Origin comparison cannot be defeated by a
		// noncanonical port a browser would never itself send.
		portNum, err := strconv.Atoi(port)
		if err != nil || portNum < 1 || portNum > 65535 || strconv.Itoa(portNum) != port {
			return ErrIdentityConfigInvalid
		}
		if portNum == 443 {
			return ErrIdentityConfigInvalid
		}
	}
	return nil
}
