package bootstrap

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Control endpoints come from operator arguments or environment, never an HTTP
// request. Allow private destinations, but do not reinterpret them through a
// proxy or a redirect: drain requests carry the platform's control credential.
func controlHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            (&net.Dialer{Timeout: timeout}).DialContext,
			TLSHandshakeTimeout:    timeout,
			ResponseHeaderTimeout:  timeout,
			MaxResponseHeaderBytes: 64 << 10,
			DisableKeepAlives:      true,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func controlHTTPRequest(ctx context.Context, method, endpoint string) (*http.Request, error) {
	const location = "bootstrap.controlHTTPRequest"
	address, err := url.Parse(endpoint)
	if err != nil || !validControlURL(address) {
		// Avoid including a malformed endpoint, which may contain credentials.
		return nil, fmt.Errorf("%s - expected an absolute HTTP(S) endpoint without userinfo or fragment", location)
	}
	// #nosec G704 -- Operator-only endpoint; the client connects directly and refuses redirects.
	request, err := http.NewRequestWithContext(ctx, method, address.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%s - invalid control endpoint", location)
	}
	return request, nil
}

func validControlURL(address *url.URL) bool {
	if address == nil || (address.Scheme != "http" && address.Scheme != "https") || address.Opaque != "" ||
		address.Hostname() == "" || address.User != nil || address.Fragment != "" || strings.HasSuffix(address.Host, ":") {
		return false
	}
	if port := address.Port(); port != "" {
		number, err := strconv.Atoi(port)
		return err == nil && number > 0 && number <= 65535
	}
	return true
}
