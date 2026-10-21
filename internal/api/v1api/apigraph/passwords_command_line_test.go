package apigraph

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/cmd"
	"github.com/ziyan/teanode/internal/models"
)

// `teanode passwords` is run against the real GraphQL handler and database,
// so that what it seals and sends is what the server accepts: the proofs, the
// salt, the public keys, the revisions and the shares. The fake server in the
// command line's own tests cannot show that.
func TestPasswordsCommandLineAgainstTheRealServer(test *testing.T) {
	world := newPasswordsWorld(test)
	test.Setenv("XDG_CONFIG_HOME", test.TempDir())
	test.Setenv("XDG_RUNTIME_DIR", test.TempDir())
	// No clipboard program is found, so copy prints the value and a test never
	// writes secrets to the clipboard of whoever runs it.
	test.Setenv("PATH", test.TempDir())

	people := map[string]struct {
		user           *models.User
		credentialKind models.CredentialKind
	}{
		"ada-session": {world.ada, models.CredentialSession},
		"ada-token":   {world.ada, models.CredentialToken},
		"ben-session": {world.ben, models.CredentialSession},
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		person, isKnown := people[strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")]
		if !isKnown {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		request.Header.Set(api.AuthenticatedUsernameHeader, person.user.Username)
		request.Header.Set(api.CredentialKindHeader, string(person.credentialKind))
		world.graph.graphView(response, request)
	}))
	defer server.Close()

	run := func(token, standardInput string, arguments ...string) (string, error) {
		test.Helper()
		root := &cli.Command{
			Name:   "fixture",
			Reader: strings.NewReader(standardInput),
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: token},
			},
			Commands: []*cli.Command{cmd.NewPasswordsCommand()},
		}
		reader, writer, err := os.Pipe()
		if err != nil {
			test.Fatal(err)
		}
		original := os.Stdout
		os.Stdout = writer
		done := make(chan string)
		go func() {
			content, _ := io.ReadAll(reader)
			done <- string(content)
		}()
		runErr := root.Run(test.Context(), append([]string{"fixture", "passwords"}, arguments...))
		os.Stdout = original
		_ = writer.Close()
		return <-done, runErr
	}
	mustRun := func(token, standardInput string, arguments ...string) string {
		test.Helper()
		output, err := run(token, standardInput, arguments...)
		if err != nil {
			test.Fatalf("teanode passwords %s: %s", strings.Join(arguments, " "), err)
		}
		return output
	}

	const adaPassphrase, benPassphrase = "ada passphrase example", "ben passphrase example"

	// A token that is not a person's tap cannot set up; a session can.
	if _, err := run("ada-token", adaPassphrase+"\n", "setup", "--passphrase-stdin"); err == nil || !strings.Contains(err.Error(), api.TapRequiredPhrase) || !strings.Contains(err.Error(), "dashboard") {
		test.Fatalf("setup with a token: %v", err)
	}
	if output := mustRun("ada-session", adaPassphrase+"\n", "setup", "--passphrase-stdin"); !strings.Contains(output, "recovery key") {
		test.Fatalf("setup said:\n%s", output)
	}
	mustRun("ada-session", "", "lock")
	mustRun("ben-session", benPassphrase+"\n", "setup", "--passphrase-stdin")
	mustRun("ben-session", "", "lock")

	// A wrong passphrase is the server's refusal; a right one unlocks, with a
	// token as well as with a session.
	if _, err := run("ada-token", "wrong passphrase\n", "unlock", "--passphrase-stdin"); err == nil || !strings.Contains(err.Error(), "That passphrase is not right.") {
		test.Fatalf("a wrong passphrase: %v", err)
	}
	mustRun("ada-token", adaPassphrase+"\n", "unlock", "--passphrase-stdin")

	// Items: add, list, show, edit from a stale-proof revision, delete, restore.
	mustRun("ada-token", "", "add", "example.com", "--user-name", "ada@example.com", "--site", "https://example.com",
		"--password", "real-server-secret-example", "--one-time-code", "JBSWY3DPEHPK3PXP")
	if listed := mustRun("ada-token", "", "list"); !strings.Contains(listed, "example.com") || !strings.Contains(listed, "ada@example.com") {
		test.Fatalf("list:\n%s", listed)
	}
	if output := strings.TrimSpace(mustRun("ada-token", "", "copy", "example.com")); output != "real-server-secret-example" {
		test.Fatalf("copy: %q", output)
	}
	mustRun("ada-token", "", "edit", "example.com", "--folder", "web")
	mustRun("ada-token", "", "delete", "example.com")
	if listed := mustRun("ada-token", "", "list", "--deleted"); !strings.Contains(listed, "example.com") {
		test.Fatalf("list --deleted:\n%s", listed)
	}
	mustRun("ada-token", "", "restore", "example.com")
	mustRun("ada-token", "", "add", "Travel card", "--kind", "card", "--number", "4111111111111111")

	// What the database holds is sealed.
	items := world.list(test, world.ada, nil)["items"].([]any)
	for _, each := range items {
		item := each.(map[string]any)
		if text := item["encryptedFields"].(string) + item["itemKey"].(string); strings.Contains(text, "example") || strings.Contains(text, "4111") {
			test.Fatalf("the server holds a field in the clear: %s", text)
		}
	}

	// Sharing with Ben shows his safety code and needs a tap, and he opens it.
	// The unlock is kept for the server, not for the sign-in, so the session
	// uses what the token's unlock opened.
	if _, err := run("ada-session", "", "share", "example.com", world.ben.Username); err == nil || !strings.Contains(err.Error(), "safety code") {
		test.Fatalf("sharing without checking the safety code: %v", err)
	}
	// Confirming his safety code writes his key down, even though the server
	// then refuses a token for want of a tap.
	if _, err := run("ada-token", "", "share", "example.com", world.ben.Username, "--force"); err == nil || !strings.Contains(err.Error(), api.TapRequiredPhrase) {
		test.Fatalf("sharing from a token: %v", err)
	}
	mustRun("ada-session", "", "share", "example.com", world.ben.Username)
	if stored := world.list(test, world.ben, nil)["items"].([]any); len(stored) != 1 || stored[0].(map[string]any)["ownerPublicKey"] == "" {
		test.Fatalf("Ben does not reach the item with its owner's key: %v", stored)
	}
	mustRun("ada-session", "", "lock")

	mustRun("ben-session", benPassphrase+"\n", "unlock", "--passphrase-stdin")
	if listed := mustRun("ben-session", "", "list"); !strings.Contains(listed, "example.com") || !strings.Contains(listed, "shared by") || strings.Contains(listed, "Travel card") {
		test.Fatalf("Ben's list:\n%s", listed)
	}
	if output := strings.TrimSpace(mustRun("ben-session", "", "copy", "example.com")); output != "real-server-secret-example" {
		test.Fatalf("Ben copies %q", output)
	}
	mustRun("ben-session", "", "lock")

	// Unsharing needs no tap and ends it.
	mustRun("ada-token", adaPassphrase+"\n", "unlock", "--passphrase-stdin")
	mustRun("ada-token", "", "unshare", "example.com", world.ben.Username)
	mustRun("ada-token", "", "lock")
	mustRun("ben-session", benPassphrase+"\n", "unlock", "--passphrase-stdin")
	if listed := mustRun("ben-session", "", "list"); strings.Contains(listed, "example.com") {
		test.Fatalf("Ben still reaches the item:\n%s", listed)
	}
	mustRun("ben-session", "", "lock")

	// The agent: a card is refused, a token is not a tap and a session may turn it on, and
	// any sign-in may turn it off.
	mustRun("ada-session", adaPassphrase+"\n", "unlock", "--passphrase-stdin")
	if _, err := run("ada-session", "", "agent", "on", "Travel card", "--force"); err == nil || !strings.Contains(err.Error(), "card cannot be used by the agent") {
		test.Fatalf("a card for the agent: %v", err)
	}
	if _, err := run("ada-token", "", "agent", "on", "example.com", "--force"); err == nil || !strings.Contains(err.Error(), api.TapRequiredPhrase) {
		test.Fatalf("the agent from a token: %v", err)
	}

	mustRun("ada-session", "", "agent", "on", "example.com", "--force")
	if listed := mustRun("ada-session", "", "list"); !strings.Contains(listed, "agent") {
		test.Fatalf("list does not mark the agent:\n%s", listed)
	}
	mustRun("ada-token", "", "agent", "off", "example.com")
	if listed := mustRun("ada-session", "", "list"); strings.Contains(listed, "agent") {
		test.Fatalf("list still marks the agent:\n%s", listed)
	}

	// Changing the passphrase, and erasing, need a tap.
	if _, err := run("ada-token", adaPassphrase+"\nnew passphrase example\n", "change-passphrase", "--passphrase-stdin"); err == nil || !strings.Contains(err.Error(), api.TapRequiredPhrase) {
		test.Fatalf("change-passphrase from a token: %v", err)
	}
	mustRun("ada-session", adaPassphrase+"\nnew passphrase example\n", "change-passphrase", "--passphrase-stdin")
	mustRun("ada-session", "", "lock")
	mustRun("ada-session", "new passphrase example\n", "unlock", "--passphrase-stdin")
	if output := strings.TrimSpace(mustRun("ada-session", "", "copy", "example.com")); output != "real-server-secret-example" {
		test.Fatalf("after changing the passphrase: %q", output)
	}
	if _, err := run("ada-token", "", "erase", "--force"); err == nil || !strings.Contains(err.Error(), api.TapRequiredPhrase) {
		test.Fatalf("erase from a token: %v", err)
	}
	mustRun("ada-session", "", "erase", "--force")
	if output := mustRun("ada-session", "", "settings"); !strings.Contains(output, "not set up") {
		test.Fatalf("settings after erase:\n%s", output)
	}
}
