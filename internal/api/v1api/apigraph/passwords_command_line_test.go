package apigraph

import (
	"encoding/base64"
	"encoding/json"
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
	"github.com/ziyan/teanode/internal/passwords"
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
		"ada-session":  {world.ada, models.CredentialSession},
		"ada-token":    {world.ada, models.CredentialToken},
		"ben-session":  {world.ben, models.CredentialSession},
		"cora-session": {world.cora, models.CredentialSession},
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

	// The command line refuses what needs the person's tap in one sentence
	// that says where it is approved, not with the server's own words.
	isTapRefusal := func(err error) bool {
		return err != nil && strings.Contains(err.Error(), "done on the Passwords page") && !strings.Contains(err.Error(), api.TapRequiredPhrase)
	}

	const adaPassphrase, benPassphrase, coraPassphrase = "ada passphrase example", "ben passphrase example", "cora passphrase example"

	// A token that is not a person's tap cannot set up; a session can.
	if _, err := run("ada-token", adaPassphrase+"\n", "setup", "--passphrase-stdin"); !isTapRefusal(err) {
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
	// Bringing an item back lets its old recipients wait for keys again: only a tap.
	// (The command may word the refusal itself or pass the server's on.)
	if _, err := run("ada-token", "", "restore", "example.com"); err == nil || (!isTapRefusal(err) && !strings.Contains(err.Error(), api.TapRequiredPhrase)) {
		test.Fatalf("a token restoring an item: %v", err)
	}
	mustRun("ada-session", adaPassphrase+"\n", "restore", "example.com", "--passphrase-stdin")
	mustRun("ada-token", "", "add", "Travel card", "--kind", "card", "--number", "4111111111111111")

	// What the database holds is sealed.
	items := world.list(test, world.ada, nil)["items"].([]any)
	for _, each := range items {
		item := each.(map[string]any)
		if text := item["encryptedFields"].(string) + item["itemKey"].(string); strings.Contains(text, "example") || strings.Contains(text, "4111") {
			test.Fatalf("the server holds a field in the clear: %s", text)
		}
	}

	// Sharing with Ben needs a tap, and he opens it. Nobody is asked about his
	// key. The unlock is kept for the server, not for the sign-in, so the
	// session uses what the token's unlock opened.
	//
	// Marking his key as compared is optional: his safety code, which Ben would
	// read off his own screen, is worked out here from his public key. A wrong
	// code records nothing.
	var benSettings struct {
		Setup struct {
			PublicKey string `json:"publicKey"`
		} `json:"setup"`
	}
	if err := json.Unmarshal([]byte(mustRun("ben-session", "", "settings", "--json")), &benSettings); err != nil {
		test.Fatal(err)
	}
	benPublicKey, err := base64.StdEncoding.DecodeString(benSettings.Setup.PublicKey)
	if err != nil || len(benPublicKey) == 0 {
		test.Fatalf("Ben's public key: %v", err)
	}
	benSafetyCode := passwords.SafetyCode(benPublicKey)
	if _, err := run("ada-token", "", "trust", world.ben.Username, "0000 0000 0000 0000 0000 0000 0000 0001"); err == nil || !strings.Contains(err.Error(), "nothing was recorded") {
		test.Fatalf("trust with a wrong code: %v", err)
	}
	// The right code marks his key as compared, even though the server then
	// refuses a token for want of a tap.
	mustRun("ada-token", benSafetyCode+"\n", "trust", world.ben.Username, "--code-stdin")
	if _, err := run("ada-token", "", "share", "example.com", world.ben.Username); !isTapRefusal(err) {
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

	// The person's own safety code is worked out from the key that unlocking
	// opened and from nothing the server reports, and is the one the people who share compare.
	var adaSettings struct {
		Setup struct {
			PublicKey string `json:"publicKey"`
		} `json:"setup"`
	}
	if err := json.Unmarshal([]byte(mustRun("ada-session", "", "settings", "--json")), &adaSettings); err != nil {
		test.Fatal(err)
	}
	adaPublicKey, err := base64.StdEncoding.DecodeString(adaSettings.Setup.PublicKey)
	if err != nil || len(adaPublicKey) == 0 {
		test.Fatalf("Ada's public key: %v", err)
	}
	if output := mustRun("ada-session", "", "code"); !strings.Contains(output, passwords.SafetyCode(adaPublicKey)) || !strings.Contains(output, world.ada.Username) {
		test.Fatalf("code printed:\n%s", output)
	}

	// Cora has not set Passwords up, so she waits. Once she has, share --waiting
	// seals the item key for her at once, naming the item key it opened, which
	// the server compares with the item's own.
	world.join(test, world.ada, world.home, world.cora)
	mustRun("ada-session", "", "share", "example.com", world.cora.Username)
	mustRun("cora-session", coraPassphrase+"\n", "setup", "--passphrase-stdin")
	var coraSettings struct {
		Setup struct {
			PublicKey string `json:"publicKey"`
		} `json:"setup"`
	}
	if err := json.Unmarshal([]byte(mustRun("cora-session", "", "settings", "--json")), &coraSettings); err != nil {
		test.Fatal(err)
	}
	if coraSettings.Setup.PublicKey == "" {
		test.Fatal("Cora's public key is empty")
	}
	mustRun("cora-session", "", "lock")
	mustRun("ada-session", adaPassphrase+"\n", "unlock", "--passphrase-stdin")
	if output := mustRun("ada-session", "", "share", "--waiting"); !strings.Contains(output, "sealed 1 item") {
		test.Fatalf("share --waiting said:\n%s", output)
	}
	mustRun("cora-session", coraPassphrase+"\n", "unlock", "--passphrase-stdin")
	if listed := mustRun("cora-session", "", "list"); !strings.Contains(listed, "example.com") {
		test.Fatalf("Cora's list:\n%s", listed)
	}
	mustRun("cora-session", "", "lock")
	mustRun("ada-session", adaPassphrase+"\n", "unlock", "--passphrase-stdin")
	// Taking her off re-keys the item, which clears the mark the server set.
	mustRun("ada-session", "", "unshare", "example.com", world.cora.Username)
	for _, each := range world.list(test, world.ada, nil)["items"].([]any) {
		item := each.(map[string]any)
		if item["needsNewKey"] == true {
			test.Fatalf("an item still needs a new key after unshare: %v", item["id"])
		}
	}
	// The same command by hand, on an item that does not need it.
	mustRun("ada-session", "", "rekey", "example.com", "--yes")
	if _, err := run("ada-session", "", "agent", "on", "Travel card", "--force"); err == nil || !strings.Contains(err.Error(), "card cannot be used by the agent") {
		test.Fatalf("a card for the agent: %v", err)
	}
	if _, err := run("ada-token", "", "agent", "on", "example.com", "--force"); !isTapRefusal(err) {
		test.Fatalf("the agent from a token: %v", err)
	}

	mustRun("ada-session", "", "agent", "on", "example.com", "--force")
	if listed := mustRun("ada-session", "", "list"); !strings.Contains(listed, "agent") {
		test.Fatalf("list does not mark the agent:\n%s", listed)
	}
	// The access record the server stored is the one this computer signed, so
	// the sharing is checked, and the agent flag shown is both words together.
	var listedItems []struct {
		Name                string `json:"name"`
		IsUsableByAgent     bool   `json:"isUsableByAgent"`
		SharingCheckVerdict string `json:"sharingCheckVerdict"`
	}
	if err := json.Unmarshal([]byte(mustRun("ada-session", "", "list", "--json")), &listedItems); err != nil || len(listedItems) == 0 {
		test.Fatalf("list --json: %v", err)
	}
	for _, listedItem := range listedItems {
		if listedItem.SharingCheckVerdict != "checked" || listedItem.IsUsableByAgent != (listedItem.Name == "example.com") {
			test.Fatalf("list --json: %+v", listedItem)
		}
	}
	mustRun("ada-token", "", "agent", "off", "example.com")
	if listed := mustRun("ada-session", "", "list"); strings.Contains(listed, "agent") {
		test.Fatalf("list still marks the agent:\n%s", listed)
	}

	// Removing a deleted item for good needs a tap: a token is refused at once
	// and a session removes it, but only once it is deleted.
	mustRun("ada-session", "", "add", "Scratch note", "--kind", "note", "--text", "nothing to keep")
	if _, err := run("ada-session", "", "delete", "Scratch note", "--forever", "--force"); err == nil || !strings.Contains(err.Error(), "not deleted") {
		test.Fatalf("delete --forever of a live item: %v", err)
	}
	mustRun("ada-session", "", "delete", "Scratch note")
	if _, err := run("ada-token", "", "delete", "Scratch note", "--forever", "--force"); !isTapRefusal(err) {
		test.Fatalf("delete --forever from a token: %v", err)
	}
	mustRun("ada-session", "", "delete", "Scratch note", "--forever", "--force")
	if listed := mustRun("ada-session", "", "list", "--deleted"); strings.Contains(listed, "Scratch note") {
		test.Fatalf("the item is still there after delete --forever:\n%s", listed)
	}

	// Changing the passphrase, and erasing, need a tap.
	if _, err := run("ada-token", adaPassphrase+"\nnew passphrase example\n", "change-passphrase", "--passphrase-stdin"); !isTapRefusal(err) {
		test.Fatalf("change-passphrase from a token: %v", err)
	}
	mustRun("ada-session", adaPassphrase+"\nnew passphrase example\n", "change-passphrase", "--passphrase-stdin")
	mustRun("ada-session", "", "lock")
	mustRun("ada-session", "new passphrase example\n", "unlock", "--passphrase-stdin")
	if output := strings.TrimSpace(mustRun("ada-session", "", "copy", "example.com")); output != "real-server-secret-example" {
		test.Fatalf("after changing the passphrase: %q", output)
	}
	if _, err := run("ada-token", "", "erase", "--force"); !isTapRefusal(err) {
		test.Fatalf("erase from a token: %v", err)
	}
	mustRun("ada-session", "", "erase", "--force")
	if output := mustRun("ada-session", "", "settings"); !strings.Contains(output, "not set up") {
		test.Fatalf("settings after erase:\n%s", output)
	}
}
