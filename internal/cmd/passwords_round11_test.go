package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// An answer is about the item that was asked for. A server that answers a change
// with the view of another item the person owns gets nothing more done with
// that answer: no key sealed, nothing written down under the other item.

// answerOperationWith makes the fake server do an operation on the item that
// was asked for, then answer it with the view of another item.
func (self *fakePasswordsServer) answerOperationWith(operation, requestedItemID, shownItemID string) {
	inner := self.server.Config.Handler
	self.server.Config.Handler = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		request.Body = io.NopCloser(bytes.NewReader(body))
		recorder := httptest.NewRecorder()
		inner.ServeHTTP(recorder, request)
		answer := recorder.Body.Bytes()
		var document struct {
			Variables struct {
				ID    string `json:"id"`
				Input struct {
					ID     string `json:"id"`
					ItemID string `json:"itemId"`
				} `json:"input"`
			} `json:"variables"`
		}
		_ = json.Unmarshal(body, &document)
		isRequested := document.Variables.ID == requestedItemID || document.Variables.Input.ID == requestedItemID || document.Variables.Input.ItemID == requestedItemID
		if bytes.Contains(body, []byte(operation)) && isRequested {
			self.mutex.Lock()
			view := self.itemView(self.items[shownItemID], self.users[testToken])
			self.mutex.Unlock()
			answer, _ = json.Marshal(map[string]any{"data": map[string]any{operation: view}})
		}
		for key, values := range recorder.Header() {
			response.Header()[key] = values
		}
		response.WriteHeader(recorder.Code)
		_, _ = response.Write(answer)
	})
}

func otherItemIDThan(fake *fakePasswordsServer, known string) string {
	for itemID := range fake.items {
		if itemID != known {
			return itemID
		}
	}
	return ""
}

func rewriteRememberedItem(test *testing.T, itemID string, change func(*passwordsKnownItem)) {
	test.Helper()
	path := knownKeysFile(test)
	file, err := readPasswordsKnownKeysFile(path)
	if err != nil {
		test.Fatal(err)
	}
	found := false
	for _, items := range file.Items {
		if items[itemID] != nil {
			change(items[itemID])
			found = true
		}
	}
	if !found {
		test.Fatalf("no memory of %s", itemID)
	}
	content, err := json.Marshal(file)
	if err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		test.Fatal(err)
	}
}

// staleMarkedItems builds two items shared with Ben and usable by the agent (Y
// and Z). The owner then took Ben and the agent off Z on another device; this
// computer never saw that, and only looked at Z when it was shared. The server
// shows Z as it was, marked as needing a new key.
func staleMarkedItems(test *testing.T) (fake *fakePasswordsServer, yID, zID string) {
	fake, _, _ = agentFake(test)
	fake.mustRun(testToken, "", "add", "yyy.example.com", "--password", "y-secret-example")
	yID = firstItemID(fake)
	fake.mustRun(testToken, "", "share", "yyy.example.com", "ben")
	fake.mustRun(testToken, "", "agent", "on", "yyy.example.com", "--force")
	fake.mustRun(testToken, "", "add", "zzz.example.com", "--password", "z-secret-example")
	zID = otherItemIDThan(fake, yID)
	fake.mustRun(testToken, "", "share", "zzz.example.com", "ben")
	fake.mustRun(testToken, "", "agent", "on", "zzz.example.com", "--force")
	rewriteRememberedItem(test, zID, func(known *passwordsKnownItem) { known.SealedForUserIDs, known.HasMadeAgentCopy = nil, false })
	rewriteRememberedItem(test, yID, func(known *passwordsKnownItem) { known.SealedForUserIDs, known.HasMadeAgentCopy = nil, false })
	known, err := os.ReadFile(knownKeysFile(test))
	if err != nil {
		test.Fatal(err)
	}
	zItem := fake.items[zID]
	staleFields := snapshotOf(zItem)
	staleAgentCopy := zItem.itemKeyByAgentKey
	staleKeys := map[string]any{}
	for userID, key := range zItem.keys {
		staleKeys[userID] = key
	}
	staleGrantees := zItem.grantees
	fake.mustRun(testToken, "", "unshare", "zzz.example.com", "ben")
	fake.mustRun(testToken, "", "agent", "off", "zzz.example.com")
	if len(zItem.keys) != 0 || zItem.itemKeyByAgentKey != "" {
		test.Fatalf("the narrowing did not take: %+v", zItem)
	}
	if err := os.WriteFile(knownKeysFile(test), known, 0o600); err != nil {
		test.Fatal(err)
	}
	staleFields.showAgainOn(zItem)
	zItem.isUsableByAgent, zItem.itemKeyByAgentKey, zItem.needsNewKey = true, staleAgentCopy, true
	zItem.grantees = staleGrantees
	for userID := range staleKeys {
		zItem.keys[userID] = fake.items[yID].keys[userID]
	}
	zItem.revision += 1000
	return fake, yID, zID
}

func TestPasswordsRekeyAnsweredWithAnotherItemWritesNothingDown(test *testing.T) {
	fake, yID, zID := staleMarkedItems(test)
	fake.answerOperationWith("RekeyPasswordItem", yID, zID)
	output, err := fake.run(testToken, "", "rekey", "yyy.example.com")
	if err == nil || !strings.Contains(err.Error(), "The server answered about another item.") {
		test.Fatalf("rekey of Y answered with Z: %q, %v", output, err)
	}
	if planted := rememberedItem(test, zID); len(planted.SealedForUserIDs) != 0 || planted.HasMadeAgentCopy {
		test.Fatalf("memory was filed under the other item: %+v", planted)
	}
	// The edit of Z still seals for nobody: this computer never sealed for Ben or the agent.
	fake.mustRun(testToken, "", "edit", "zzz.example.com", "--user-name", "ada")
	zItem := fake.items[zID]
	if key := zItem.keys["user-ben"]; key != nil && key.ItemKeyByRecipientKey != "" {
		test.Fatal("Ben has a key for the new item key of Z")
	}
	if zItem.itemKeyByAgentKey != "" {
		test.Fatal("the agent has a copy of Z")
	}
}

func TestPasswordsEveryChangeAnsweredWithAnotherItemIsRefused(test *testing.T) {
	cases := []struct {
		operation string
		arguments []string
		prepare   func(fake *fakePasswordsServer)
	}{
		{"SavePasswordItem", []string{"edit", "yyy.example.com", "--user-name", "ada"}, nil},
		{"SetPasswordItemShares", []string{"unshare", "yyy.example.com", "ben"}, nil},
		{"DeletePasswordItem", []string{"delete", "yyy.example.com"}, nil},
		{"SetPasswordItemUsableByAgent", []string{"agent", "on", "yyy.example.com", "--force"}, func(fake *fakePasswordsServer) {
			fake.mustRun(testToken, "", "agent", "off", "yyy.example.com")
		}},
	}
	for _, each := range cases {
		test.Run(each.operation, func(test *testing.T) {
			fake, yID, zID := staleMarkedItems(test)
			if each.prepare != nil {
				each.prepare(fake)
			}
			fake.answerOperationWith(each.operation, yID, zID)
			output, err := fake.run(testToken, "", each.arguments...)
			if err == nil || !strings.Contains(err.Error(), "The server answered about another item.") {
				test.Fatalf("%s answered with another item: %q, %v", each.operation, output, err)
			}
			if remembered := rememberedItem(test, zID); len(remembered.SealedForUserIDs) != 0 || remembered.HasMadeAgentCopy {
				test.Fatalf("memory was filed under the other item: %+v", remembered)
			}
		})
	}
}

func TestPasswordsRestoreAnsweredWithAnotherItemIsRefused(test *testing.T) {
	fake, yID, zID := staleMarkedItems(test)
	fake.mustRun(testToken, "", "delete", "yyy.example.com")
	fake.answerOperationWith("RestorePasswordItem", yID, zID)
	_, err := fake.run(testToken, "", "restore", "yyy.example.com")
	if err == nil || !strings.Contains(err.Error(), "The server answered about another item.") {
		test.Fatalf("restore answered with another item: %v", err)
	}
}

// Taking one person off an item, or the agent, is the person's own narrowing:
// everyone the record still reaches gets the new key, whatever this computer
// sealed before, as on the dashboard and the iPhone.
func TestPasswordsUnshareKeepsPeopleAnotherDeviceSharedWith(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.addCora()
	fake.unlockAs(testToken)
	fake.mustRun(testToken, "", "add", "shared.example.com", "--password", "shared-secret-example")
	itemID := firstItemID(fake)
	fake.mustRun(testToken, "", "share", "shared.example.com", "ben")
	fake.mustRun(testToken, "", "share", "shared.example.com", "cora")
	fake.mustRun(testToken, "", "agent", "on", "shared.example.com", "--force")
	// The sharing looks as if it was done from the dashboard: this computer only looked.
	rewriteRememberedItem(test, itemID, func(known *passwordsKnownItem) { known.SealedForUserIDs, known.HasMadeAgentCopy = nil, false })
	item := fake.items[itemID]
	fake.mustRun(testToken, "", "unshare", "shared.example.com", "cora")
	if item.keys["user-cora"] != nil {
		test.Fatal("Cora kept access")
	}
	if item.keys["user-ben"] == nil || item.keys["user-ben"].ItemKeyByRecipientKey == "" {
		test.Fatal("Ben lost access to an item that is still shared with him")
	}
	if !item.isUsableByAgent || item.itemKeyByAgentKey == "" {
		test.Fatal("the agent lost its copy though the record still lets it use the item")
	}
	fake.unlockAs(benToken)
	if output := strings.TrimSpace(fake.mustRun(benToken, "", "copy", "shared.example.com")); output != "shared-secret-example" {
		test.Fatalf("Ben copies %q", output)
	}
}

func TestPasswordsAgentOffKeepsPeopleAnotherDeviceSharedWith(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.mustRun(testToken, "", "add", "shared.example.com", "--password", "shared-secret-example")
	itemID := firstItemID(fake)
	fake.mustRun(testToken, "", "share", "shared.example.com", "ben")
	fake.mustRun(testToken, "", "agent", "on", "shared.example.com", "--force")
	rewriteRememberedItem(test, itemID, func(known *passwordsKnownItem) { known.SealedForUserIDs, known.HasMadeAgentCopy = nil, false })
	item := fake.items[itemID]
	fake.mustRun(testToken, "", "agent", "off", "shared.example.com")
	if item.isUsableByAgent || item.itemKeyByAgentKey != "" {
		test.Fatal("the agent kept a copy after agent off")
	}
	if item.keys["user-ben"] == nil || item.keys["user-ben"].ItemKeyByRecipientKey == "" {
		test.Fatal("Ben lost access when only the agent was taken off")
	}
}

// An item the owner's record lets the agent use, with no copy on the server, is
// listed as needing rekey, and rekey makes the record say what the server has.
func TestPasswordsListSaysWhenTheAgentCopyIsLost(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "secret-example")
	itemID := firstItemID(fake)
	fake.mustRun(testToken, "", "agent", "on", "example.com", "--force")
	item := fake.items[itemID]
	item.isUsableByAgent, item.itemKeyByAgentKey = false, ""
	item.revision++
	output := fake.mustRun(testToken, "", "list")
	if !strings.Contains(output, "agent copy lost") || !strings.Contains(output, "teanode passwords rekey "+itemID) {
		test.Fatalf("list does not say the agent copy is lost:\n%s", output)
	}
	var summaries []passwordsItemSummary
	if err := json.Unmarshal([]byte(fake.mustRun(testToken, "", "list", "--json")), &summaries); err != nil {
		test.Fatal(err)
	}
	if len(summaries) != 1 || !summaries[0].HasLostAgentCopy || summaries[0].IsUsableByAgent {
		test.Fatalf("json: %+v", summaries)
	}
	fake.mustRun(testToken, "", "rekey", "example.com")
	output = fake.mustRun(testToken, "", "list")
	if strings.Contains(output, "agent copy lost") {
		test.Fatalf("rekey left the agent copy lost:\n%s", output)
	}
}

// Earlier versions are listed and restored by the person's tap only, so the
// command line says where it is done, at once and without asking for anything.
func TestPasswordsVersionsIsRefusedWithWhereItIsDone(test *testing.T) {
	fake, _, _ := agentFake(test)
	fake.mustRun(testToken, "", "add", "example.com", "--password", "secret-example")
	_, err := fake.run(testToken, "", "versions", "example.com")
	if err == nil || !strings.Contains(err.Error(), "dashboard") || !strings.Contains(err.Error(), "iPhone") || !strings.Contains(err.Error(), "Earlier versions") {
		test.Fatalf("versions: %v", err)
	}
}
