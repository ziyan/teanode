package passwords

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// grantToAgent is what a person's device does when they turn on "Let the agent
// use this": seal the fields with a new item key, and the item key for the
// agent key, signed with the owner's key pair.
func grantToAgent(test *testing.T, ownerKeyPair, agentKeyPair *KeyPair, ownerId, itemId string, itemKind ItemKind, fields any) AgentItem {
	test.Helper()
	itemKey, err := NewKey()
	if err != nil {
		test.Fatal(err)
	}
	plaintext, err := json.Marshal(fields)
	if err != nil {
		test.Fatal(err)
	}
	sealedFields, err := Seal(itemKey, plaintext, ItemFieldsData(itemId, itemKind))
	if err != nil {
		test.Fatal(err)
	}
	sealedKey, err := SealForKeyPair(ownerKeyPair, agentKeyPair.PublicKey(), itemKey, ShareData(itemId, ownerId, AgentRecipientID))
	if err != nil {
		test.Fatal(err)
	}
	return AgentItem{
		ID: itemId, Kind: itemKind, OwnerID: ownerId, OwnerPublicKey: ownerKeyPair.PublicKey(),
		EncryptedFields: sealedFields.String(), ItemKeyByAgentKey: sealedKey.String(),
	}
}

func newKeyPairs(test *testing.T) (ownerKeyPair, agentKeyPair *KeyPair, opener *AgentOpener) {
	test.Helper()
	ownerKeyPair, err := GenerateKeyPair()
	if err != nil {
		test.Fatal(err)
	}
	agentKeyPair, err = GenerateKeyPair()
	if err != nil {
		test.Fatal(err)
	}
	opener, err = NewAgentOpener(agentKeyPair.PrivateKey())
	if err != nil {
		test.Fatal(err)
	}
	return ownerKeyPair, agentKeyPair, opener
}

func TestTheAgentOpensAGrantedLoginAndNote(test *testing.T) {
	ownerKeyPair, agentKeyPair, opener := newKeyPairs(test)

	loginItem := grantToAgent(test, ownerKeyPair, agentKeyPair, "user-ada", "login-1", ItemKindLogin, LoginFields{
		Name: "Example shop", UserName: "ada@example.test", Password: "correct-horse-example", Sites: []string{"https://shop.example.test"},
		Access: AccessWithAgentUse(nil, true),
	})
	login, err := opener.OpenLogin(loginItem)
	if err != nil || login.Name != "Example shop" || login.UserName != "ada@example.test" || login.Password != "correct-horse-example" {
		test.Fatalf("login = %+v, %v", login, err)
	}

	noteItem := grantToAgent(test, ownerKeyPair, agentKeyPair, "user-ada", "note-1", ItemKindNote, NoteFields{Name: "Door code", Text: "The shed code is 0000.", Access: AccessWithAgentUse(nil, true)})
	note, err := opener.OpenNote(noteItem)
	if err != nil || note.Text != "The shed code is 0000." {
		test.Fatalf("note = %+v, %v", note, err)
	}
}

func TestTheAgentRefusesACard(test *testing.T) {
	ownerKeyPair, agentKeyPair, opener := newKeyPairs(test)
	// Even a card whose key somebody sealed for the agent does not open.
	card := grantToAgent(test, ownerKeyPair, agentKeyPair, "user-ada", "card-1", ItemKindCard, CardFields{Name: "Example card", Number: "4000000000000000", Access: AccessWithAgentUse(nil, true)})
	if _, err := opener.OpenFields(card); !errors.Is(err, ErrCardNotUsableByAgent) {
		test.Fatalf("a card: %v", err)
	}
	if _, err := opener.OpenLogin(card); !errors.Is(err, ErrCardNotUsableByAgent) {
		test.Fatalf("a card opened as a login: %v", err)
	}
	if _, err := opener.OpenNote(card); !errors.Is(err, ErrCardNotUsableByAgent) {
		test.Fatalf("a card opened as a note: %v", err)
	}
	odd := card
	odd.Kind = ItemKind("recipe")
	if _, err := opener.OpenFields(odd); err == nil {
		test.Fatal("a kind that is not one")
	}
}

func TestTheAgentCannotOpenWhatWasNotSealedForIt(test *testing.T) {
	ownerKeyPair, agentKeyPair, opener := newKeyPairs(test)
	otherKeyPair, _ := GenerateKeyPair()

	// Sealed for somebody else's key pair.
	elsewhere := grantToAgent(test, ownerKeyPair, otherKeyPair, "user-ada", "login-1", ItemKindLogin, LoginFields{Name: "Example", Access: AccessWithAgentUse(nil, true)})
	if _, err := opener.OpenLogin(elsewhere); !errors.Is(err, ErrCannotOpen) {
		test.Fatalf("another key pair: %v", err)
	}

	// Sealed for the agent, but moved to another item's id, kind, owner or
	// recipient.
	item := grantToAgent(test, ownerKeyPair, agentKeyPair, "user-ada", "login-1", ItemKindLogin, LoginFields{Name: "Example", Access: AccessWithAgentUse(nil, true)})
	moved := item
	moved.ID = "login-2"
	if _, err := opener.OpenLogin(moved); !errors.Is(err, ErrCannotOpen) {
		test.Fatalf("another item id: %v", err)
	}
	moved = item
	moved.OwnerID = "user-ben"
	if _, err := opener.OpenLogin(moved); !errors.Is(err, ErrCannotOpen) {
		test.Fatalf("another owner id: %v", err)
	}
	if _, err := opener.OpenNote(item); err == nil {
		test.Fatal("a login opened as a note")
	}
	moved = item
	moved.Kind = ItemKindNote
	if _, err := opener.OpenNote(moved); !errors.Is(err, ErrCannotOpen) {
		test.Fatalf("a login relabeled as a note: %v", err)
	}
	// The owner's public key is part of the key: one the owner did not seal
	// with, which is what the server planting an item would have, does not open.
	moved = item
	moved.OwnerPublicKey = otherKeyPair.PublicKey()
	if _, err := opener.OpenLogin(moved); !errors.Is(err, ErrCannotOpen) {
		test.Fatalf("another owner key: %v", err)
	}
	userKey, _ := SealForKeyPair(ownerKeyPair, agentKeyPair.PublicKey(), sequentialBytes(1, KeyLength), ShareData("login-1", "user-ada", "user-ada"))
	moved = item
	moved.ItemKeyByAgentKey = userKey.String()
	if _, err := opener.OpenLogin(moved); !errors.Is(err, ErrCannotOpen) {
		test.Fatalf("a key sealed for a person: %v", err)
	}
	moved = item
	moved.ItemKeyByAgentKey = "not json"
	if _, err := opener.OpenLogin(moved); err == nil || strings.Contains(err.Error(), "Example") {
		test.Fatalf("garbage: %v", err)
	}
	if _, err := NewAgentOpener([]byte("short")); err == nil {
		test.Fatal("a key that is not 32 bytes")
	}
}

func TestTheAgentRefusesAnItemWhoseSealedAccessDoesNotAllowIt(test *testing.T) {
	ownerKeyPair, agentKeyPair, opener := newKeyPairs(test)
	people := []PasswordGrantee{{UserID: "user-ben"}, {CircleID: "circle-1"}}

	cases := []struct {
		name   string
		fields LoginFields
	}{
		{"no access record", LoginFields{Name: "Example"}},
		{"agent use off", LoginFields{Name: "Example", Access: AccessWithAgentUse(nil, false)}},
		{"shared but agent use off", LoginFields{Name: "Example", Access: AccessWithGrantees(nil, people)}},
	}
	for _, testCase := range cases {
		item := grantToAgent(test, ownerKeyPair, agentKeyPair, "user-ada", "login-1", ItemKindLogin, testCase.fields)
		if _, err := opener.OpenFields(item); !errors.Is(err, ErrNotUsableByAgent) {
			test.Errorf("%s: OpenFields = %v", testCase.name, err)
		}
		if _, err := opener.OpenLogin(item); !errors.Is(err, ErrNotUsableByAgent) {
			test.Errorf("%s: OpenLogin = %v", testCase.name, err)
		}
	}
	note := grantToAgent(test, ownerKeyPair, agentKeyPair, "user-ada", "note-1", ItemKindNote, NoteFields{Name: "Example"})
	if _, err := opener.OpenNote(note); !errors.Is(err, ErrNotUsableByAgent) {
		test.Errorf("a note without access: %v", err)
	}

	// The same item with the record on, and grantees besides, opens.
	allowed := LoginFields{Name: "Example", Access: AccessWithAgentUse(AccessWithGrantees(nil, people), true)}
	item := grantToAgent(test, ownerKeyPair, agentKeyPair, "user-ada", "login-1", ItemKindLogin, allowed)
	login, err := opener.OpenLogin(item)
	if err != nil || login.Access == nil || !login.Access.IsUsableByAgent || len(login.Access.Grantees) != 2 {
		test.Fatalf("login = %+v, %v", login, err)
	}
}

func TestTheAgentRefusesAnAccessRecordOfTheWrongShape(test *testing.T) {
	ownerKeyPair, agentKeyPair, opener := newKeyPairs(test)
	for _, record := range []string{
		`"yes"`,
		`{"isUsableByAgent": "true", "grantees": []}`,
		`{"isUsableByAgent": true}`,
		`{"isUsableByAgent": true, "grantees": [{"userId": "user-ben", "circleId": "circle-1"}]}`,
		`{"isUsableByAgent": true, "grantees": [{"userId": ""}]}`,
		`{"isUsableByAgent": true, "grantees": [{}]}`,
		`{"isUsableByAgent": true, "grantees": [{"groupId": "group-1"}]}`,
		`{"isUsableByAgent": true, "grantees": [], "extra": 1}`,
	} {
		fields := json.RawMessage(`{"name": "Example", "access": ` + record + `}`)
		item := grantToAgent(test, ownerKeyPair, agentKeyPair, "user-ada", "login-1", ItemKindLogin, fields)
		_, err := opener.OpenFields(item)
		if err == nil || errors.Is(err, ErrNotUsableByAgent) {
			test.Errorf("%s: OpenFields = %v, want an unreadable record", record, err)
		}
	}
}
