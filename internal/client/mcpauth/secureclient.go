package mcpauth

import (
	"fmt"
	"net"
	"net/http"
)

// secureTransport refuses to round-trip a request whose URL scheme isn't
// https, unless the host is loopback (127.0.0.0/8, ::1, or "localhost").
// This is the transport the SDK's AuthorizationCodeHandlerConfig.Client
// uses for every OAuth HTTP call (metadata discovery, registration, token
// exchange, refresh), so a malicious or misconfigured redirect can never
// make jig send credentials in the clear to a non-loopback host.
type secureTransport struct {
	base http.RoundTripper
}

func (t *secureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" && !isLoopbackHost(req.URL.Hostname()) {
		return nil, fmt.Errorf("mcpauth: refusing non-https request to %s", req.URL.Redacted())
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// isLoopbackHost reports whether host (as found in a URL, so possibly
// "localhost" or a bracketed IPv6 literal already stripped of brackets by
// net/url) refers to the local machine.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// SecureClient returns an *http.Client whose transport refuses any request
// whose URL isn't https or loopback. Pass it as
// auth.AuthorizationCodeHandlerConfig.Client so every OAuth-related HTTP
// call jig makes is protected, even against a server that tries to
// redirect discovery or token requests to an attacker-controlled
// non-loopback http:// URL.
func SecureClient() *http.Client {
	return &http.Client{Transport: &secureTransport{}}
}
