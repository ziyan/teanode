package computer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/computer"
)

// HTTPClient makes its requests through one of the person's computers rather
// than through this server.
//
// Anything that fetches with an *http.Client can be pointed at a computer
// this way: a page the agent reads, a skill calling a service that only
// answers inside the network that computer is on. The computer follows any
// redirects itself, so this client does not follow them again.
func HTTPClient(device tools.Computer) *http.Client {
	return &http.Client{
		Transport: &throughComputer{device: device},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// httpWait is how long the server waits for the computer's answer: the
// computer's own bound on the request, and room for the answer to come back.
const httpWait = 5*time.Minute + 30*time.Second

type throughComputer struct {
	device tools.Computer
}

func (self *throughComputer) RoundTrip(request *http.Request) (*http.Response, error) {
	var body []byte
	if request.Body != nil {
		read, err := io.ReadAll(request.Body)
		_ = request.Body.Close()
		if err != nil {
			return nil, err
		}
		body = read
	}
	timeoutSeconds := 0
	if deadline, ok := request.Context().Deadline(); ok {
		timeoutSeconds = max(int(time.Until(deadline)/time.Second), 1)
	}
	answer, err := self.device.Ask(request.Context(), "http", &computer.HTTPArguments{
		Method: request.Method, URL: request.URL.String(), Header: request.Header, Body: body,
		TimeoutSeconds: timeoutSeconds,
	}, httpWait)
	if err != nil {
		// A daemon older than this action says it does not do it. That is
		// the one failure the person can fix by hand, so say how.
		if strings.Contains(err.Error(), "is not something this program does") {
			return nil, fmt.Errorf("the computer %q runs an older teanode that cannot make requests; update it there and run 'teanode computer start' again", self.device.Name())
		}
		return nil, fmt.Errorf("through the computer %q: %w", self.device.Name(), err)
	}
	var result computer.HTTPResult
	if err := json.Unmarshal(answer, &result); err != nil {
		return nil, fmt.Errorf("the computer %q answered with something unreadable: %w", self.device.Name(), err)
	}
	response := &http.Response{
		Status:        fmt.Sprintf("%d %s", result.Status, http.StatusText(result.Status)),
		StatusCode:    result.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header(result.Header),
		Body:          io.NopCloser(bytes.NewReader(result.Body)),
		ContentLength: int64(len(result.Body)),
		Request:       request,
	}
	if response.Header == nil {
		response.Header = http.Header{}
	}
	// Where the computer ended up after redirects, so a caller reading the
	// final address sees the one the answer came from.
	if result.URL != "" && result.URL != request.URL.String() {
		if final, err := request.URL.Parse(result.URL); err == nil {
			followed := request.Clone(context.WithoutCancel(request.Context()))
			followed.URL = final
			response.Request = followed
		}
	}
	return response, nil
}
