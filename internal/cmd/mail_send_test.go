package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestMailSendRetainsAnIdentifierForExplicitRetries(test *testing.T) {
	for _, suppliedId := range []string{"", "fixture-request"} {
		test.Run("identifier-"+suppliedId, func(test *testing.T) {
			requests := make(chan map[string]any, 2)
			var sendCount atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				var document struct {
					Query     string         `json:"query"`
					Variables map[string]any `json:"variables"`
				}
				if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
					response.WriteHeader(400)
					return
				}
				response.Header().Set("Content-Type", "application/json")
				if strings.Contains(document.Query, "ListDomains") {
					_, _ = response.Write([]byte(`{"data":{"ListDomains":[{"id":"domain-fixture","domain":"example.com"}]}}`))
					return
				}
				if !strings.Contains(document.Query, "submissionId: $submissionId") {
					response.WriteHeader(400)
					return
				}
				requests <- document.Variables
				if sendCount.Add(1) == 1 {
					response.WriteHeader(500)
					return
				}
				_, _ = response.Write([]byte(`{"data":{"SendMail":{"submissionId":"fixture","mailId":"accepted-mail","mail":{"id":"accepted-mail"}}}}`))
			}))
			defer server.Close()
			bodyPath := filepath.Join(test.TempDir(), "body.txt")
			if err := os.WriteFile(bodyPath, []byte("Fixture content"), 0600); err != nil {
				test.Fatal(err)
			}
			invoke := func(submissionId string) error {
				command := &cli.Command{Name: "fixture", Flags: []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}}, Commands: []*cli.Command{NewMailCommand()}}
				arguments := []string{"fixture", "mail", "send", "example.com", "--from", "sender@example.com", "--to", "recipient@example.net", "--text", bodyPath}
				if submissionId != "" {
					arguments = append(arguments, "--submission-id", submissionId)
				}
				return command.Run(test.Context(), arguments)
			}
			err := invoke(suppliedId)
			if err == nil {
				test.Fatal("expected lost response")
			}
			if sendCount.Load() != 1 {
				test.Fatalf("request did not reach the server: %v", err)
			}
			first := <-requests
			submissionId, _ := first["submissionId"].(string)
			if submissionId == "" || !strings.Contains(err.Error(), submissionId) || (suppliedId != "" && submissionId != suppliedId) {
				test.Fatalf("retry identifier=%q, error=%v", submissionId, err)
			}
			if err := invoke(submissionId); err != nil {
				test.Fatal(err)
			}
			if sendCount.Load() != 2 {
				test.Fatal("retry did not reach the server")
			}
			second := <-requests
			if !reflect.DeepEqual(first, second) || sendCount.Load() != 2 {
				test.Fatalf("retry changed request: %+v, %+v", first, second)
			}
		})
	}
}

func TestMailSubmissionChecksAcceptanceWithoutMessageInputs(test *testing.T) {
	for _, result := range []string{`null`, `{"submissionId":"fixture-request","mailId":"accepted-mail","acceptedAt":"2020-01-01T00:00:00Z"}`} {
		test.Run(result, func(test *testing.T) {
			var lookupCount atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				var document struct {
					Query     string         `json:"query"`
					Variables map[string]any `json:"variables"`
				}
				if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
					test.Error(err)
					response.WriteHeader(400)
					return
				}
				response.Header().Set("Content-Type", "application/json")
				if strings.Contains(document.Query, "ListDomains") {
					_, _ = response.Write([]byte(`{"data":{"ListDomains":[{"id":"domain-fixture","domain":"example.com"}]}}`))
					return
				}
				if !strings.Contains(document.Query, "GetDomainSubmission") || strings.Contains(document.Query, "mutation") || document.Variables["domainId"] != "domain-fixture" || document.Variables["submissionId"] != "fixture-request" {
					test.Errorf("unexpected operation: %+v", document)
					response.WriteHeader(400)
					return
				}
				lookupCount.Add(1)
				_, _ = response.Write([]byte(`{"data":{"GetDomainSubmission":` + result + `}}`))
			}))
			defer server.Close()
			command := &cli.Command{Name: "fixture", Flags: []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}}, Commands: []*cli.Command{NewMailCommand()}}
			if err := command.Run(test.Context(), []string{"fixture", "mail", "submission", "example.com", "fixture-request", "--json"}); err != nil {
				test.Fatal(err)
			}
			if lookupCount.Load() != 1 {
				test.Fatalf("lookup count=%d", lookupCount.Load())
			}
		})
	}
}
