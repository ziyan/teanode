package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"
)

// importServer answers ImportFinanceCredential with an invented finance
// source and records the variables of every request it was sent.
func importServer(test *testing.T) (*httptest.Server, func() []map[string]any) {
	test.Helper()
	var mutex sync.Mutex
	var asked []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil || !strings.Contains(document.Query, "ImportFinanceCredential") {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		asked = append(asked, document.Variables)
		mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":{"ImportFinanceCredential":{"id":"source-invented","name":"Invented Savings Bank",` +
			`"providerKind":"plaid","institutionName":"Invented Savings Bank","isEnabled":true,"isSignInRequired":false,` +
			`"financeAccounts":[],"createdAt":"2026-09-30T10:00:00Z"}}}`))
	}))
	test.Cleanup(server.Close)
	return server, func() []map[string]any {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]map[string]any(nil), asked...)
	}
}

// runFinanceAgainst runs teanode finance with the arguments against the
// server, answering what it wrote and the error.
func runFinanceAgainst(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	var written bytes.Buffer
	command := &cli.Command{
		Name: "fixture", Writer: &written,
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{NewFinanceCommand()},
	}
	err := command.Run(test.Context(), append([]string{"fixture", "finance"}, arguments...))
	return written.String(), err
}

// A credential given on the command line is refused, saying why, and
// nothing is sent; one in a file is sent, and the new finance source is
// named with when it first syncs.
func TestFinanceImportCredentialRefusesAnInlineCredential(test *testing.T) {
	test.Parallel()
	server, asked := importServer(test)
	for _, inline := range []string{
		"access-sandbox-invented-0008",
		"https://person:invented-password@bridge.example.net/simplefin",
	} {
		_, err := runFinanceAgainst(test, server, "import-credential", "--provider", "plaid", inline)
		if err == nil || !strings.Contains(err.Error(), "history") {
			test.Errorf("%q on the command line answered %v", inline, err)
		}
		if err != nil && strings.Contains(err.Error(), inline) {
			test.Errorf("the refusal repeated the credential: %s", err)
		}
	}
	_, err := runFinanceAgainst(test, server, "import-credential", "--provider", "plaid", "no-such-file-invented")
	if err == nil || strings.Contains(err.Error(), "no-such-file-invented") {
		test.Errorf("an argument that is not a file answered %v", err)
	}
	if _, err := runFinanceAgainst(test, server, "import-credential", "--provider", "elsewhere", "-"); err == nil {
		test.Error("an unknown provider was taken")
	}
	if sent := asked(); len(sent) != 0 {
		test.Fatalf("a refused credential was sent: %v", sent)
	}

	credentialFile := filepath.Join(test.TempDir(), "credential")
	if err := os.WriteFile(credentialFile, []byte("access-sandbox-invented-0009\n"), 0o600); err != nil {
		test.Fatal(err)
	}
	printed, err := runFinanceAgainst(test, server, "import-credential", "--provider", "Plaid", "--institution-name", "Invented Savings Bank", credentialFile)
	if err != nil {
		test.Fatalf("import-credential from a file: %s", err)
	}
	sent := asked()
	if len(sent) != 1 || sent[0]["credential"] != "access-sandbox-invented-0009" || sent[0]["providerKind"] != "plaid" || sent[0]["institutionName"] != "Invented Savings Bank" {
		test.Errorf("sent %v", sent)
	}
	if !strings.Contains(printed, "source-invented") || !strings.Contains(printed, "Invented Savings Bank") || !strings.Contains(printed, "first sync starts within the minute") {
		test.Errorf("printed %q", printed)
	}
	if strings.Contains(printed, "access-sandbox-invented-0009") {
		test.Errorf("the credential was printed: %q", printed)
	}
}
