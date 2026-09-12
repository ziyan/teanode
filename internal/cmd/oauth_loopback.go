package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/ziyan/teanode/internal/util/deferutil"
)

// Some services only send an authorization back to a loopback address.
// That is the flow a program on somebody's own machine is meant to use,
// and several of them accept nothing else -- so the command line can be
// the place the code comes back to, rather than the dashboard.

// oauthResult is what the service came back with.
type oauthResult struct {
	Code  string
	State string
	Error string
}

// oauthLoopback is one listener waiting for one redirect.
type oauthLoopback struct {
	listener net.Listener
	server   *http.Server
	results  chan oauthResult
}

func newOAuthLoopback(ctx context.Context) (*oauthLoopback, error) {
	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("cannot listen on the loopback interface: %w", err)
	}
	self := &oauthLoopback{listener: listener, results: make(chan oauthResult, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", self.callback)
	self.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		defer deferutil.Recover()
		_ = self.server.Serve(listener)
	}()
	return self, nil
}

// Redirect is the address to give the service.
func (self *oauthLoopback) Redirect() string {
	return fmt.Sprintf("http://127.0.0.1:%d/callback", self.listener.Addr().(*net.TCPAddr).Port)
}

func (self *oauthLoopback) callback(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	result := oauthResult{Code: query.Get("code"), State: query.Get("state"), Error: query.Get("error")}
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if result.Code == "" && result.Error == "" {
		response.WriteHeader(http.StatusBadRequest)
		_, _ = response.Write([]byte("This address is where an authorization comes back to. There is nothing here.\n"))
		return
	}
	if result.Error != "" {
		_, _ = response.Write([]byte("The service refused: " + result.Error + ". You can close this page.\n"))
	} else {
		_, _ = response.Write([]byte("Authorized. You can close this page and go back to the terminal.\n"))
	}
	select {
	case self.results <- result:
	default:
	}
}

// Wait is what came back, or why it did not.
func (self *oauthLoopback) Wait(ctx context.Context, timeout time.Duration) (*oauthResult, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-self.results:
		if result.Error != "" {
			return nil, fmt.Errorf("the service refused the authorization: %s", result.Error)
		}
		return &result, nil
	case <-timer.C:
		return nil, fmt.Errorf("nothing came back within %s; the page may not have been opened, or the service may not send an authorization to a loopback address", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (self *oauthLoopback) Close() {
	shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = self.server.Shutdown(shutdown)
	_ = self.listener.Close()
}
