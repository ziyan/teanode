package cmd

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
)

// fakePasswordsServer answers the Passwords documents from memory, the way the
// real server does for the things the command line depends on: proofs are
// checked against stored hashes, an item changed from a stale revision is
// refused, a key is stored only for the recipient's current public key, and the
// tap-only operations can be refused. It never opens a sealed value, so a test
// that passes through it shows the server never needed to.
type fakePasswordsServer struct {
	test   *testing.T
	server *httptest.Server

	mutex sync.Mutex
	// users are the people by sign-in token.
	users map[string]*fakePasswordsUser
	items map[string]*fakePasswordsItem
	// revision is the highest revision handed out.
	revision int64
	// isTapRefused refuses the operations that need a person's tap.
	isTapRefused bool
	// agentPublicKey is the key a device seals for to let the agent use an item.
	agentPublicKey string
	// documents are the operations received, in order.
	documents []string
	// lastSetKeyCount is how many keys the last SetPasswordItemShares carried.
	lastSetKeyCount int
}

type fakePasswordsUser struct {
	id, username, name string
	setup              *client.SetUpPasswordsInput
	lockAfterMinutes   int
}

type fakePasswordsItem struct {
	id, ownerUserID, itemKind, encryptedFields, itemKey string
	revision                                            int64
	deletedAt                                           *time.Time
	isUsableByAgent                                     bool
	itemKeyByAgentKey                                   string
	grantees                                            []*client.PasswordShareGranteeInput
	// keys are the item key sealed for each recipient by user id.
	keys map[string]*client.PasswordShareKeyInput
}

func newFakePasswordsServer(test *testing.T) *fakePasswordsServer {
	test.Helper()
	self := &fakePasswordsServer{test: test, items: map[string]*fakePasswordsItem{}, agentPublicKey: ""}
	self.users = map[string]*fakePasswordsUser{
		"token-ada": {id: "user-ada", username: "ada", name: "Ada", lockAfterMinutes: 15},
		"token-ben": {id: "user-ben", username: "ben", name: "Ben", lockAfterMinutes: 15},
	}
	self.server = httptest.NewServer(http.HandlerFunc(self.handle))
	test.Cleanup(self.server.Close)
	return self
}

// operationNames are the operations the fake knows, each as it appears in a
// document.
var fakePasswordsOperations = []string{
	"ReadPasswordsSetup", "SetUpPasswords", "UnlockPasswords", "ChangePasswordsPassphrase", "ReplacePasswordsRecoveryKey",
	"UpdatePasswordsSettings", "ErasePasswords", "ListPasswordItems", "ListPasswordRecipients", "ListWaitingPasswordShares",
	"ReadPasswordsAgentKey", "SavePasswordItem", "DeletePasswordItem", "RestorePasswordItem", "SetPasswordItemShares",
	"AddPasswordShareKeys", "SetPasswordItemUsableByAgent", "GetCurrentUser", "ListCirclePeople", "ListCircles",
}

func passwordsOperationOf(query string) string {
	for _, name := range fakePasswordsOperations {
		if strings.Contains(query, name+"(") || strings.Contains(query, name+" ") {
			return name
		}
	}
	return ""
}

func (self *fakePasswordsServer) handle(response http.ResponseWriter, request *http.Request) {
	var document struct {
		Query     string          `json:"query"`
		Variables json.RawMessage `json:"variables"`
	}
	if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	response.Header().Set("Content-Type", "application/json")
	user := self.users[strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")]
	if user == nil {
		response.WriteHeader(http.StatusUnauthorized)
		return
	}
	operation := passwordsOperationOf(document.Query)
	self.documents = append(self.documents, operation)
	result, message := self.answer(user, operation, document.Variables)
	if message != "" {
		_ = json.NewEncoder(response).Encode(map[string]any{"errors": []map[string]any{{"message": message}}})
		return
	}
	_ = json.NewEncoder(response).Encode(map[string]any{"data": map[string]any{operation: result}})
}

// proofHashOf is what the server stores for a proof: base64 of its SHA-256.
func proofHashOf(proof string) string {
	decoded, err := base64.StdEncoding.DecodeString(proof)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(decoded)
	return base64.StdEncoding.EncodeToString(hash[:])
}

func (self *fakePasswordsServer) setupView(user *fakePasswordsUser) map[string]any {
	view := map[string]any{"isSetUp": user.setup != nil, "lockAfterMinutes": user.lockAfterMinutes, "itemCount": 0}
	if user.setup != nil {
		view["passphraseSalt"] = user.setup.PassphraseSalt
		view["passphraseIterationCount"] = user.setup.PassphraseIterationCount
		view["publicKey"] = user.setup.PublicKey
	}
	for _, item := range self.items {
		if item.ownerUserID == user.id && item.deletedAt == nil {
			view["itemCount"] = view["itemCount"].(int) + 1
		}
	}
	return view
}

func (self *fakePasswordsServer) userByID(id string) *fakePasswordsUser {
	for _, user := range self.users {
		if user.id == id {
			return user
		}
	}
	return nil
}

func (self *fakePasswordsServer) recipientView(item *fakePasswordsItem, user *fakePasswordsUser) map[string]any {
	view := map[string]any{"userId": user.id, "name": user.name, "username": user.username, "publicKey": nil, "hasKey": false}
	if user.setup != nil {
		view["publicKey"] = user.setup.PublicKey
		if item != nil {
			if key := item.keys[user.id]; key != nil && key.RecipientPublicKey == user.setup.PublicKey {
				view["hasKey"] = true
			}
		}
	}
	return view
}

func (self *fakePasswordsServer) reaches(item *fakePasswordsItem, user *fakePasswordsUser) bool {
	for _, grantee := range item.grantees {
		if grantee.GranteeUserID != nil && *grantee.GranteeUserID == user.id {
			return true
		}
	}
	return false
}

func (self *fakePasswordsServer) itemView(item *fakePasswordsItem, reader *fakePasswordsUser) map[string]any {
	ownerPublicKey := ""
	if owner := self.userByID(item.ownerUserID); owner.setup != nil {
		ownerPublicKey = owner.setup.PublicKey
	}
	view := map[string]any{
		"id": item.id, "ownerUserId": item.ownerUserID, "ownerName": self.userByID(item.ownerUserID).name, "ownerPublicKey": ownerPublicKey, "itemKind": item.itemKind,
		"encryptedFields": item.encryptedFields, "itemKey": item.itemKey, "isSharedWithMe": false, "revision": item.revision,
		"isUsableByAgent": item.isUsableByAgent, "lastUsedByAgentAt": nil, "deletedAt": item.deletedAt,
		"createdAt": time.Now().UTC(), "updatedAt": time.Now().UTC(), "shares": []any{},
	}
	if item.ownerUserID != reader.id {
		view["isSharedWithMe"] = true
		view["itemKey"] = item.keys[reader.id].ItemKeyByRecipientKey
		return view
	}
	shares := []any{}
	for _, grantee := range item.grantees {
		recipient := self.userByID(*grantee.GranteeUserID)
		shares = append(shares, map[string]any{
			"id": "share-" + recipient.id, "granteeUserId": recipient.id, "granteeCircleId": nil, "granteeName": recipient.name,
			"recipients": []any{self.recipientView(item, recipient)},
		})
	}
	view["shares"] = shares
	return view
}

// answer runs one operation as the user. It gives the result, or the message
// of the error to answer with.
func (self *fakePasswordsServer) answer(user *fakePasswordsUser, operation string, rawVariables json.RawMessage) (any, string) {
	decode := func(target any) {
		if len(rawVariables) > 0 {
			if err := json.Unmarshal(rawVariables, target); err != nil {
				self.test.Fatalf("%s: variables: %s", operation, err)
			}
		}
	}
	isTapOperation := map[string]bool{"SetUpPasswords": true, "ChangePasswordsPassphrase": true, "ReplacePasswordsRecoveryKey": true, "ErasePasswords": true}
	if isTapOperation[operation] && self.isTapRefused {
		return nil, api.TapRequiredPhrase + ": setting up is done on the dashboard"
	}
	switch operation {
	case "GetCurrentUser":
		return map[string]any{"id": user.id, "username": user.username, "name": user.name}, ""
	case "ListCirclePeople":
		people := []any{}
		for _, other := range self.users {
			if other.id != user.id {
				people = append(people, map[string]any{"userId": other.id, "username": other.username, "name": other.name})
			}
		}
		return people, ""
	case "ListCircles":
		return []any{}, ""
	case "ReadPasswordsSetup":
		return self.setupView(user), ""
	case "SetUpPasswords":
		var variables struct {
			Input *client.SetUpPasswordsInput `json:"input"`
		}
		decode(&variables)
		// The real server checks the tap above, then the input, then whether
		// Passwords is already set up.
		if variables.Input == nil || variables.Input.AccountKeyByPassphrase == "" || variables.Input.PublicKey == "" {
			return nil, "invalid arguments: the account key must be the JSON a device sealed"
		}
		if user.setup != nil {
			return nil, "Passwords is already set up."
		}
		user.setup = variables.Input
		return self.setupView(user), ""
	case "UnlockPasswords":
		var variables struct {
			Proof         string `json:"proof"`
			IsRecoveryKey bool   `json:"isRecoveryKey"`
		}
		decode(&variables)
		if user.setup == nil {
			return nil, "Passwords is not set up yet."
		}
		wrapped, wanted := user.setup.AccountKeyByPassphrase, user.setup.PassphraseProofHash
		if variables.IsRecoveryKey {
			wrapped, wanted = user.setup.AccountKeyByRecoveryKey, user.setup.RecoveryKeyProofHash
		}
		if proofHashOf(variables.Proof) != wanted {
			if variables.IsRecoveryKey {
				return nil, "That recovery key is not right."
			}
			return nil, "That passphrase is not right."
		}
		return map[string]any{"accountKeyWrapped": wrapped, "privateKeyByAccountKey": user.setup.PrivateKeyByAccountKey, "publicKey": user.setup.PublicKey}, ""
	case "ChangePasswordsPassphrase":
		var variables struct {
			Input *client.ChangePasswordsPassphraseInput `json:"input"`
		}
		decode(&variables)
		wanted := user.setup.PassphraseProofHash
		if variables.Input.IsRecoveryKey {
			wanted = user.setup.RecoveryKeyProofHash
		}
		if proofHashOf(variables.Input.Proof) != wanted {
			return nil, "That passphrase is not right."
		}
		user.setup.AccountKeyByPassphrase = variables.Input.AccountKeyByPassphrase
		user.setup.PassphraseSalt = variables.Input.PassphraseSalt
		user.setup.PassphraseIterationCount = variables.Input.PassphraseIterationCount
		user.setup.PassphraseProofHash = variables.Input.PassphraseProofHash
		return self.setupView(user), ""
	case "ReplacePasswordsRecoveryKey":
		var variables struct {
			Input *client.ReplacePasswordsRecoveryKeyInput `json:"input"`
		}
		decode(&variables)
		if proofHashOf(variables.Input.Proof) != user.setup.PassphraseProofHash {
			return nil, "That passphrase is not right."
		}
		user.setup.AccountKeyByRecoveryKey = variables.Input.AccountKeyByRecoveryKey
		user.setup.RecoveryKeyProofHash = variables.Input.RecoveryKeyProofHash
		return self.setupView(user), ""
	case "UpdatePasswordsSettings":
		var variables struct {
			LockAfterMinutes int `json:"lockAfterMinutes"`
		}
		decode(&variables)
		user.lockAfterMinutes = variables.LockAfterMinutes
		return self.setupView(user), ""
	case "ErasePasswords":
		user.setup = nil
		for id, item := range self.items {
			if item.ownerUserID == user.id {
				delete(self.items, id)
			}
		}
		return true, ""
	case "ListPasswordItems":
		ids := []string{}
		views := []any{}
		for _, item := range self.items {
			isOwner := item.ownerUserID == user.id
			if !isOwner && (item.deletedAt != nil || !self.reaches(item, user) || item.keys[user.id] == nil) {
				continue
			}
			ids = append(ids, item.id)
			views = append(views, self.itemView(item, user))
		}
		return map[string]any{"items": views, "itemIds": ids, "revision": self.revision}, ""
	case "ListPasswordRecipients":
		var variables struct {
			GranteeUserID string `json:"granteeUserId"`
		}
		decode(&variables)
		other := self.userByID(variables.GranteeUserID)
		if other == nil || other.id == user.id {
			return nil, "not found"
		}
		return []any{self.recipientView(nil, other)}, ""
	case "ListWaitingPasswordShares":
		waiting := []any{}
		for _, item := range self.items {
			if item.ownerUserID != user.id {
				continue
			}
			for _, grantee := range item.grantees {
				recipient := self.userByID(*grantee.GranteeUserID)
				view := self.recipientView(item, recipient)
				if view["publicKey"] != nil && view["hasKey"] == false {
					waiting = append(waiting, map[string]any{"itemId": item.id, "recipient": view})
				}
			}
		}
		return waiting, ""
	case "ReadPasswordsAgentKey":
		return self.agentPublicKey, ""
	case "SavePasswordItem":
		var variables struct {
			Input *client.SavePasswordItemInput `json:"input"`
		}
		decode(&variables)
		input := variables.Input
		item := self.items[input.ID]
		switch {
		case item == nil && input.BaseRevision != nil:
			return nil, "not found"
		case item != nil && (input.BaseRevision == nil || *input.BaseRevision != item.revision):
			return nil, "This item changed on another device. Open it again."
		case item != nil && item.ownerUserID != user.id:
			return nil, "not found"
		}
		if item == nil {
			item = &fakePasswordsItem{id: input.ID, ownerUserID: user.id, itemKind: input.ItemKind, keys: map[string]*client.PasswordShareKeyInput{}}
			self.items[item.id] = item
		}
		self.revision++
		item.revision = self.revision
		item.encryptedFields, item.itemKey = input.EncryptedFields, input.ItemKey
		return self.itemView(item, user), ""
	case "DeletePasswordItem", "RestorePasswordItem":
		var variables struct {
			ID string `json:"id"`
		}
		decode(&variables)
		item := self.items[variables.ID]
		if item == nil || item.ownerUserID != user.id {
			return nil, "not found"
		}
		self.revision++
		item.revision = self.revision
		item.deletedAt = nil
		if operation == "DeletePasswordItem" {
			moment := time.Now().UTC()
			item.deletedAt = &moment
		}
		return self.itemView(item, user), ""
	case "SetPasswordItemShares":
		var variables struct {
			Input *client.SetPasswordItemSharesInput `json:"input"`
		}
		decode(&variables)
		item := self.items[variables.Input.ItemID]
		if item == nil || item.ownerUserID != user.id {
			return nil, "not found"
		}
		keys := map[string]*client.PasswordShareKeyInput{}
		for _, key := range variables.Input.Keys {
			recipient := self.userByID(key.RecipientUserID)
			if recipient == nil || recipient.setup == nil || recipient.setup.PublicKey != key.RecipientPublicKey {
				return nil, "That person's key changed since it was read. Read their key again and seal the item for it."
			}
			keys[key.RecipientUserID] = key
		}
		self.lastSetKeyCount = len(variables.Input.Keys)
		// A key stored before for somebody who is still reached stays, as on
		// the real server.
		for userID, key := range item.keys {
			still := false
			for _, grantee := range variables.Input.Grantees {
				still = still || (grantee.GranteeUserID != nil && *grantee.GranteeUserID == userID)
			}
			if _, isGiven := keys[userID]; still && !isGiven {
				keys[userID] = key
			}
		}
		item.grantees, item.keys = variables.Input.Grantees, keys
		self.revision++
		item.revision = self.revision
		return self.itemView(item, user), ""
	case "AddPasswordShareKeys":
		var variables struct {
			Keys []*client.PasswordItemShareKeyInput `json:"keys"`
		}
		decode(&variables)
		for _, key := range variables.Keys {
			item := self.items[key.ItemID]
			if item == nil || item.ownerUserID != user.id {
				return nil, "not found"
			}
			item.keys[key.RecipientUserID] = key.ForShare()
		}
		return len(variables.Keys), ""
	case "SetPasswordItemUsableByAgent":
		var variables struct {
			Input *client.SetPasswordItemUsableByAgentInput `json:"input"`
		}
		decode(&variables)
		item := self.items[variables.Input.ItemID]
		if item == nil || item.ownerUserID != user.id {
			return nil, "not found"
		}
		if item.itemKind == "card" {
			return nil, "A card cannot be used by the agent."
		}
		if self.isTapRefused && variables.Input.IsUsableByAgent {
			return nil, api.TapRequiredPhrase + ": letting the agent use an item is done on the dashboard"
		}
		item.isUsableByAgent, item.itemKeyByAgentKey = variables.Input.IsUsableByAgent, variables.Input.ItemKeyByAgentKey
		self.revision++
		item.revision = self.revision
		return self.itemView(item, user), ""
	}
	self.test.Fatalf("the fake server does not know the document: %s", operation)
	return nil, ""
}

// run runs `teanode passwords ...` as the user with the token, with the given
// text on standard input, and returns what it printed and the error. The
// configuration directory is the test's own, so the unlock file is too.
func (self *fakePasswordsServer) run(token, standardInput string, arguments ...string) (string, error) {
	self.test.Helper()
	root := &cli.Command{
		Name:   "fixture",
		Reader: strings.NewReader(standardInput),
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Value: self.server.URL}, &cli.StringFlag{Name: "token", Value: token},
		},
		Commands: []*cli.Command{NewPasswordsCommand()},
	}
	var err error
	output := captureStandardOutput(self.test, func() {
		err = root.Run(self.test.Context(), append([]string{"fixture", "passwords"}, arguments...))
	})
	return output, err
}

// operationCount is how many times an operation was asked for.
func (self *fakePasswordsServer) operationCount(operation string) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	count := 0
	for _, each := range self.documents {
		if each == operation {
			count++
		}
	}
	return count
}
