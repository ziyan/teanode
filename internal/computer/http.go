package computer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// A request made through this computer, on the agent's behalf.
//
// The server fetches from where it runs, and it refuses private and internal
// addresses, which is right for a server. The person's own computer is on
// networks the server is not: a work network behind a VPN, a home network, a
// host that trusts a certificate authority of its own. Made from here, a
// request goes out the way a browser on this computer would, through its
// proxy settings and trusting what this computer trusts, and reaches what the
// person can reach from their desk.
//
// Nothing here narrows which addresses may be asked for. That is decided on
// the server, where the person is asked before a request goes through a
// computer, the same as any command run on it.

// HTTPArguments are one request to make through this computer.
type HTTPArguments struct {
	Method string              `json:"method"`
	URL    string              `json:"url"`
	Header map[string][]string `json:"header,omitempty"`
	// Body is the request's body; encoded as base64 on the wire.
	Body []byte `json:"body,omitempty"`
	// TimeoutSeconds bounds the whole request, redirects included.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// MaximumBytes bounds how much of the answer is read.
	MaximumBytes int64 `json:"maximumBytes,omitempty"`
}

// HTTPResult is the answer, after any redirects.
type HTTPResult struct {
	Status int                 `json:"status"`
	Header map[string][]string `json:"header"`
	Body   []byte              `json:"body"`
	// Truncated says the body was longer than MaximumBytes and was cut.
	Truncated bool `json:"truncated,omitempty"`
	// URL is where the answer finally came from.
	URL string `json:"url"`
}

// Bounds on a request made from here: a fetch is for reading something, not
// for moving a disk's worth of data through the agent's connection.
const (
	httpTimeoutDefault = 60 * time.Second
	httpTimeoutLongest = 5 * time.Minute
	httpBytesDefault   = 8 << 20
	httpBytesMost      = 32 << 20
)

// RunHTTP makes one request through this computer.
func RunHTTP(ctx context.Context, arguments *HTTPArguments) (*HTTPResult, error) {
	target, err := url.Parse(strings.TrimSpace(arguments.URL))
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, fmt.Errorf("%q is not an http or https address", arguments.URL)
	}
	method := strings.ToUpper(strings.TrimSpace(arguments.Method))
	if method == "" {
		method = http.MethodGet
	}
	timeout := httpTimeoutDefault
	if arguments.TimeoutSeconds > 0 {
		timeout = min(time.Duration(arguments.TimeoutSeconds)*time.Second, httpTimeoutLongest)
	}
	limit := int64(httpBytesDefault)
	if arguments.MaximumBytes > 0 {
		limit = min(arguments.MaximumBytes, httpBytesMost)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body io.Reader
	if len(arguments.Body) > 0 {
		body = bytes.NewReader(arguments.Body)
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	for name, values := range arguments.Header {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	// This computer's proxy settings, as a browser here would use them; the
	// default transport already reads them, and trusts what the computer
	// trusts.
	client := &http.Client{Transport: http.DefaultTransport}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	read, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	result := &HTTPResult{Status: response.StatusCode, Header: response.Header, URL: response.Request.URL.String()}
	if int64(len(read)) > limit {
		read = read[:limit]
		result.Truncated = true
	}
	result.Body = read
	return result, nil
}
