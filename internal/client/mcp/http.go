package mcp

import (
	"net/http"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// headerRoundTripper adds a fixed set of headers to every request before
// delegating to base.
type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// httpClientWithHeaders returns an *http.Client that adds s.Headers to
// every request, built from s.HTTPClient (or http.DefaultClient) without
// mutating the caller's client. If s.Headers is empty, the base client is
// returned unchanged.
func httpClientWithHeaders(s Spec) *http.Client {
	base := s.HTTPClient
	if base == nil {
		base = http.DefaultClient
	}
	if len(s.Headers) == 0 {
		return base
	}
	clone := *base
	clone.Transport = &headerRoundTripper{base: base.Transport, headers: s.Headers}
	return &clone
}

// streamableTransport builds the streamable HTTP transport for s,
// including OAuth if s.OAuth is set.
func streamableTransport(s Spec) *sdkmcp.StreamableClientTransport {
	return &sdkmcp.StreamableClientTransport{
		Endpoint:     s.URL,
		HTTPClient:   httpClientWithHeaders(s),
		OAuthHandler: s.OAuth,
	}
}

// sseTransport builds the legacy SSE transport for s. SSE takes only
// static headers; OAuth is not supported on this transport.
func sseTransport(s Spec) *sdkmcp.SSEClientTransport {
	return &sdkmcp.SSEClientTransport{
		Endpoint:   s.URL,
		HTTPClient: httpClientWithHeaders(s),
	}
}
