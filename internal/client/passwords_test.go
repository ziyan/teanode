package client

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/api"
)

// passwordsServer answers every document with the data it is given and keeps
// the variables of the last one.
func passwordsServer(test *testing.T, data string) (*Client, func() map[string]any) {
	test.Helper()
	var variables map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var document struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(body, &document)
		variables = document.Variables
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":` + data + `}`))
	}))
	test.Cleanup(server.Close)
	connection, err := New(Options{URL: server.URL, Token: "fixture-token"})
	if err != nil {
		test.Fatal(err)
	}
	return connection, func() map[string]any { return variables }
}

func TestReadPasswordsSetupAcceptsNulls(test *testing.T) {
	connection, _ := passwordsServer(test, `{"ReadPasswordsSetup":{"isSetUp":false,"passphraseSalt":null,"passphraseIterationCount":null,"publicKey":null,"lockAfterMinutes":15,"itemCount":0}}`)
	setup, err := ReadPasswordsSetup(test.Context(), connection)
	if err != nil || setup.IsSetUp || setup.PassphraseSalt != "" || setup.LockAfterMinutes != 15 {
		test.Fatalf("setup=%+v, %v", setup, err)
	}
}

// Listing asks for a revision only when it was given one.
func TestListPasswordItemsSendsTheRevisionOnlyWhenGiven(test *testing.T) {
	connection, variables := passwordsServer(test, `{"ListPasswordItems":{"itemIds":["a"],"revision":7,"items":[{"id":"a","ownerUserId":"u","ownerName":"Ada","ownerPublicKey":"key-of-ada","itemKind":"login","encryptedFields":"{}","itemKey":"{}","isSharedWithMe":false,"revision":7,"isUsableByAgent":false,"lastUsedByAgentAt":null,"deletedAt":null,"createdAt":"2026-10-20T10:00:00Z","updatedAt":"2026-10-20T10:00:00Z","shares":[]}]}}`)
	list, err := ListPasswordItems(test.Context(), connection, nil)
	if err != nil || len(list.Items) != 1 || list.Revision != 7 || list.Items[0].CreatedAt.IsZero() {
		test.Fatalf("list=%+v, %v", list, err)
	}
	if list.Items[0].OwnerPublicKey != "key-of-ada" {
		test.Errorf("the owner's public key is %q", list.Items[0].OwnerPublicKey)
	}
	// Every document that answers an item asks for the owner's public key,
	// which a recipient and the agent open a sealed key with.
	for name, document := range map[string]string{
		"ListPasswordItems": DocumentListPasswordItems, "SavePasswordItem": DocumentSavePasswordItem, "DeletePasswordItem": DocumentDeletePasswordItem,
		"RestorePasswordItem": DocumentRestorePasswordItem, "SetPasswordItemShares": DocumentSetPasswordItemShares,
		"SetPasswordItemUsableByAgent": DocumentSetPasswordItemUsableByAgent, "RekeyPasswordItem": DocumentRekeyPasswordItem,
	} {
		if !strings.Contains(document, "ownerPublicKey") {
			test.Errorf("%s does not ask for ownerPublicKey", name)
		}
	}
	if _, isSent := variables()["sinceRevision"]; isSent {
		test.Error("a revision was sent that nobody gave")
	}
	since := int64(3)
	if _, err := ListPasswordItems(test.Context(), connection, &since); err != nil {
		test.Fatal(err)
	}
	if variables()["sinceRevision"] != float64(3) {
		test.Errorf("variables=%v", variables())
	}
	// Revisions are 64-bit: the document declares the Int64 scalar, and a
	// revision past 32 bits is read back whole.
	if !strings.Contains(DocumentListPasswordItems, "$sinceRevision: Int64") {
		test.Error("the listing does not declare sinceRevision as Int64")
	}
	big := int64(5_000_000_000)
	bigConnection, bigVariables := passwordsServer(test, `{"ListPasswordItems":{"itemIds":["a"],"revision":5000000001,"items":[{"id":"a","itemKind":"login","revision":5000000001,"needsNewKey":true,"shares":[]}]}}`)
	bigList, err := ListPasswordItems(test.Context(), bigConnection, &big)
	if err != nil || bigList.Revision != 5_000_000_001 || bigList.Items[0].Revision != 5_000_000_001 || !bigList.Items[0].NeedsNewKey {
		test.Fatalf("big list=%+v, %v", bigList, err)
	}
	if bigVariables()["sinceRevision"] != float64(5_000_000_000) {
		test.Errorf("variables=%v", bigVariables())
	}
	if !strings.Contains(passwordItemFields, "needsNewKey") {
		test.Error("the item documents do not ask for needsNewKey")
	}
}

// A new item is sent without a base revision, a changed one with it.
func TestSavePasswordItemSendsTheBaseRevision(test *testing.T) {
	connection, variables := passwordsServer(test, `{"SavePasswordItem":{"id":"a","itemKind":"login","revision":1,"shares":[]}}`)
	if _, err := SavePasswordItem(test.Context(), connection, &SavePasswordItemInput{ID: "a", ItemKind: "login", EncryptedFields: "{}", ItemKey: "{}"}); err != nil {
		test.Fatal(err)
	}
	input, _ := variables()["input"].(map[string]any)
	if value, isSent := input["baseRevision"]; !isSent || value != nil {
		test.Errorf("a new item sent %v", input)
	}
	base := int64(4)
	if _, err := SavePasswordItem(test.Context(), connection, &SavePasswordItemInput{ID: "a", ItemKind: "login", EncryptedFields: "{}", ItemKey: "{}", BaseRevision: &base}); err != nil {
		test.Fatal(err)
	}
	input, _ = variables()["input"].(map[string]any)
	if input["baseRevision"] != float64(4) {
		test.Errorf("a changed item sent %v", input)
	}
}

// A grantee is a person or a circle, never both, and a key in a share names no
// item while a waiting one does.
func TestPasswordSharesAreSentInTheShapeTheSchemaTakes(test *testing.T) {
	connection, variables := passwordsServer(test, `{"SetPasswordItemShares":{"id":"a","itemKind":"login","revision":2,"shares":[]}}`)
	person, circle := "user-ben", "circle-family"
	_, err := SetPasswordItemShares(test.Context(), connection, &SetPasswordItemSharesInput{
		ItemID:   "a",
		Grantees: []*PasswordShareGranteeInput{{GranteeUserID: &person}, {GranteeCircleID: &circle}},
		Keys:     []*PasswordShareKeyInput{{RecipientUserID: "user-ben", ItemKeyByRecipientKey: "{}", RecipientPublicKey: "key"}},
	})
	if err != nil {
		test.Fatal(err)
	}
	encoded, _ := json.Marshal(variables())
	if want := `{"input":{"grantees":[{"granteeUserId":"user-ben"},{"granteeCircleId":"circle-family"}],"itemId":"a","keys":[{"itemKeyByRecipientKey":"{}","recipientPublicKey":"key","recipientUserId":"user-ben"}]}}`; string(encoded) != want {
		test.Errorf("shares sent %s", encoded)
	}
	// Nobody left to share with is an empty list, not null.
	if _, err := SetPasswordItemShares(test.Context(), connection, &SetPasswordItemSharesInput{ItemID: "a"}); err != nil {
		test.Fatal(err)
	}
	encoded, _ = json.Marshal(variables())
	if !strings.Contains(string(encoded), `"grantees":[]`) || !strings.Contains(string(encoded), `"keys":[]`) {
		test.Errorf("an unshare sent %s", encoded)
	}

	waitingConnection, waitingVariables := passwordsServer(test, `{"AddPasswordShareKeys":1}`)
	stored, err := AddPasswordShareKeys(test.Context(), waitingConnection, []*PasswordItemShareKeyInput{{ItemID: "a", RecipientUserID: "user-ben", ItemKeyByRecipientKey: "{}", RecipientPublicKey: "key", ItemKey: `{"sealed":"item key"}`}})
	if err != nil || stored != 1 {
		test.Fatalf("stored=%d, %v", stored, err)
	}
	encoded, _ = json.Marshal(waitingVariables())
	if !strings.Contains(string(encoded), `"itemId":"a"`) || !strings.Contains(string(encoded), `"itemKey":"{\"sealed\":\"item key\"}"`) {
		test.Errorf("a waiting key sent %s", encoded)
	}
}

// A re-key sends the agent's key as null to end the agent's use, and no keys as
// an empty list, never null: the schema takes a list.
func TestRekeyPasswordItemSendsTheShapeTheSchemaTakes(test *testing.T) {
	connection, variables := passwordsServer(test, `{"RekeyPasswordItem":{"id":"a","itemKind":"login","revision":9,"shares":[]}}`)
	view, err := RekeyPasswordItem(test.Context(), connection, PasswordItemRekeyInput{ID: "a", BaseRevision: 8, EncryptedFields: "{}", ItemKey: "{}"})
	if err != nil || view == nil || view.Revision != 9 {
		test.Fatalf("view=%+v, %v", view, err)
	}
	encoded, _ := json.Marshal(variables())
	if want := `{"input":{"baseRevision":8,"encryptedFields":"{}","id":"a","itemKey":"{}","itemKeyByAgentKey":null,"shareKeys":[]}}`; string(encoded) != want {
		test.Errorf("a re-key sent %s", encoded)
	}
	agentKey := "{}"
	_, err = RekeyPasswordItem(test.Context(), connection, PasswordItemRekeyInput{
		ID: "a", BaseRevision: 8, EncryptedFields: "{}", ItemKey: "{}", ItemKeyByAgentKey: &agentKey,
		ShareKeys: []*PasswordItemShareKeyInput{{ItemID: "a", RecipientUserID: "user-ben", ItemKeyByRecipientKey: "{}", RecipientPublicKey: "key"}},
	})
	if err != nil {
		test.Fatal(err)
	}
	encoded, _ = json.Marshal(variables())
	if !strings.Contains(string(encoded), `"itemKeyByAgentKey":"{}"`) || !strings.Contains(string(encoded), `"recipientUserId":"user-ben"`) {
		test.Errorf("a re-key with keys sent %s", encoded)
	}
}

// A read-only profile can look and unlock, since unlocking changes nothing a
// read-only connection promises to keep, but cannot save or erase.
func TestReadOnlyClientUnlocksButDoesNotChange(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":{"UnlockPasswords":{"accountKeyWrapped":"{}","privateKeyByAccountKey":"{}","publicKey":"key"}}}`))
	}))
	defer server.Close()
	connection, err := New(Options{URL: server.URL, Token: "fixture-token", ReadOnly: true})
	if err != nil {
		test.Fatal(err)
	}
	unlocked, err := UnlockPasswords(test.Context(), connection, "proof", false)
	if err != nil || unlocked.PublicKey != "key" {
		test.Fatalf("unlock=%+v, %v", unlocked, err)
	}
	var readOnly *ReadOnlyError
	if _, err := SavePasswordItem(test.Context(), connection, &SavePasswordItemInput{ID: "a"}); !errors.As(err, &readOnly) {
		test.Errorf("save from a read-only client: %v", err)
	}
	if _, err := ErasePasswords(test.Context(), connection); !errors.As(err, &readOnly) {
		test.Errorf("erase from a read-only client: %v", err)
	}
}

// Before a passphrase is typed for it, setting up is probed with an empty setup,
// which the server refuses for want of a tap before it looks at the input.
func TestCheckPasswordsSetUpAllowedTellsTheTapFromTheInput(test *testing.T) {
	var sentInput map[string]any
	answer := ""
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var document struct {
			Variables struct {
				Input map[string]any `json:"input"`
			} `json:"variables"`
		}
		_ = json.Unmarshal(body, &document)
		sentInput = document.Variables.Input
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(answer))
	}))
	defer server.Close()
	connection, err := New(Options{URL: server.URL, Token: "fixture-token"})
	if err != nil {
		test.Fatal(err)
	}

	answer = `{"errors":[{"message":"` + api.TapRequiredPhrase + `: setting up is done on the dashboard"}]}`
	if err := CheckPasswordsSetUpAllowed(test.Context(), connection); !IsTapRequired(err) {
		test.Errorf("a refusal for want of a tap: %v", err)
	}
	for _, value := range sentInput {
		if value != "" && value != float64(0) {
			test.Errorf("the probe sent a value: %v", sentInput)
		}
	}
	answer = `{"errors":[{"message":"invalid arguments: the account key must be the JSON a device sealed"}]}`
	if err := CheckPasswordsSetUpAllowed(test.Context(), connection); err != nil {
		test.Errorf("a refusal of the empty input, which a tap gets: %v", err)
	}
	answer = `{"data":{"SetUpPasswords":{"isSetUp":true,"lockAfterMinutes":15,"itemCount":0}}}`
	if err := CheckPasswordsSetUpAllowed(test.Context(), connection); err == nil {
		test.Error("a server that accepted an empty setup was not reported")
	}
}
