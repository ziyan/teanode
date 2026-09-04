// Package client talks to a running TeaNode server's GraphQL API.
//
// It exists so that the command line tool and the dashboard change
// configuration the same way: through the server. The alternative, editing
// teanode.yaml underneath a running process, loses whichever change the other
// writer made second, because the server holds the whole configuration in
// memory and rewrites the file from it.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
)

// Client is a connection to one server's API.
type Client struct {
	url    string
	token  string
	client *http.Client
}

// Options configure a Client.
type Options struct {
	// URL of the server, for example https://mail.example.com. The API path
	// is appended.
	URL string

	// Token sent as "Authorization: Bearer". Empty is allowed, and works
	// against a server that has no accounts yet.
	Token string

	// HTTPClient overrides the transport, which is how the local connection
	// pins the server's own certificate.
	HTTPClient *http.Client

	// Insecure skips verifying the server's certificate, for a development
	// server with a self-signed one. Ignored when HTTPClient is given.
	Insecure bool

	// Timeout for a single request. Zero means one minute.
	Timeout time.Duration
}

// NormalizeURL is how a server is named everywhere the client remembers one:
// trimmed, without a trailing slash, and https unless a scheme was given.
func NormalizeURL(url string) string {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	if url == "" {
		return ""
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}
	return url
}

// New builds a Client.
func New(options Options) (*Client, error) {
	url := NormalizeURL(options.URL)
	if url == "" {
		return nil, fmt.Errorf("client: no server URL")
	}

	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
		if options.Insecure {
			httpClient.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		}
	}
	if httpClient.Timeout == 0 {
		httpClient.Timeout = options.Timeout
		if httpClient.Timeout == 0 {
			httpClient.Timeout = time.Minute
		}
	}

	return &Client{url: url, token: options.Token, client: httpClient}, nil
}

// URL returns the server this client talks to.
func (self *Client) URL() string {
	return self.url
}

// Error is a message the server returned for a query.
type Error struct {
	Message   string `json:"message"`
	Path      []any  `json:"path,omitempty"`
	Locations []struct {
		Line   int `json:"line"`
		Column int `json:"column"`
	} `json:"locations,omitempty"`
}

func (self *Error) Error() string {
	return self.Message
}

// Errors is every error one query returned.
type Errors []*Error

func (self Errors) Error() string {
	messages := make([]string, 0, len(self))
	for _, err := range self {
		messages = append(messages, err.Message)
	}
	return strings.Join(messages, "; ")
}

// Execute runs a query or mutation and decodes the data into result, which
// should be a pointer to a struct with the field names the query selects.
func (self *Client) Execute(ctx context.Context, query string, variables map[string]any, result any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return fmt.Errorf("client: cannot encode the query: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, self.url+api.PathGraphQL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if self.token != "" {
		request.Header.Set("Authorization", "Bearer "+self.token)
	}

	response, err := self.client.Do(request)
	if err != nil {
		return fmt.Errorf("client: cannot reach %s: %w", self.url, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("client: %s refused the token; sign in again with 'teanode auth login', or run this on the server itself", self.url)
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors Errors          `json:"errors"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("client: %s answered with something that is not a GraphQL reply (HTTP %d): %w", self.url, response.StatusCode, err)
	}
	if len(envelope.Errors) > 0 {
		return envelope.Errors
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("client: %s answered HTTP %d", self.url, response.StatusCode)
	}
	if result == nil || len(envelope.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, result); err != nil {
		return fmt.Errorf("client: cannot decode the reply: %w", err)
	}
	return nil
}

// Download fetches something that is a file rather than a GraphQL reply — the
// raw source of a stored message — with the same credentials. The caller
// closes the body. A reply that is not a success is returned as an error, so
// that a "not found" page is never saved as though it were the file.
func (self *Client) Download(ctx context.Context, path string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, self.url+path, nil)
	if err != nil {
		return nil, err
	}
	if self.token != "" {
		request.Header.Set("Authorization", "Bearer "+self.token)
	}
	response, err := self.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("client: cannot reach %s: %w", self.url, err)
	}
	if response.StatusCode == http.StatusUnauthorized {
		_ = response.Body.Close()
		return nil, fmt.Errorf("client: %s refused the token; sign in again with 'teanode auth login', or run this on the server itself", self.url)
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, fmt.Errorf("client: %s answered HTTP %d for %s", self.url, response.StatusCode, path)
	}
	return response, nil
}
