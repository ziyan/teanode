package browser

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A Chrome across a compose network answers only a Host of localhost, and
// names itself localhost in the address it gives; the address is asked with
// that Host and given back at the host it was found at, and the host:port
// the configuration asks for is enough.
func TestTheDebuggerIsFoundByItsNameAcrossANetwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Host != "localhost" {
			_, _ = io.WriteString(writer, "Host header is specified and is not an IP address or localhost.")
			return
		}
		_, _ = io.WriteString(writer, `{"webSocketDebuggerUrl":"ws://localhost/devtools/browser/a-browser"}`)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")

	for _, endpoint := range []string{host, server.URL, server.URL + "/"} {
		address, err := endpointAddress(context.Background(), endpoint)
		if err != nil {
			t.Fatalf("%s: %v", endpoint, err)
		}
		if address != "ws://"+host+"/devtools/browser/a-browser" {
			t.Errorf("%s gave %s", endpoint, address)
		}
	}
	if address, err := endpointAddress(context.Background(), "ws://elsewhere:9222/devtools/browser/x"); err != nil || address != "ws://elsewhere:9222/devtools/browser/x" {
		t.Errorf("a websocket address was changed: %s, %v", address, err)
	}
}
