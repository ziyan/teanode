package passwords

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/passwords"
	"github.com/ziyan/teanode/internal/util/secretbox"
)

// The secrets of the invented items below. None of them may ever appear in
// what the tool answers.
const (
	loginPassword      = "Tr0ub4dor&3-example"
	loginOneTimeSecret = "JBSWY3DPEHPK3PXP"
	cardNumber         = "4000000000001234"
	otherPersonsText   = "Ben's note about the garage"
)

var loginPasskeyKey = []byte("invented-passkey-private-key-bytes")

// permissions are those of a person who may use Passwords.
type permissions struct{}

func (permissions) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionPasswordsUse}})
}

func (permissions) Execute(context.Context, string, map[string]any, any) error {
	panic("the passwords tool does not use the API")
}

// fakeRun is a run of ada's agent with a database.
type fakeRun struct {
	tools.Run
	database   db.Database
	owner      *models.User
	secret     string
	surface    string
	isHeadless bool
	hasNoGrant bool
	// isStartedByOthers is a run that content began (a mail, a webhook, a
	// goal, a schedule the agent wrote) rather than the person.
	isStartedByOthers bool
	// isWokenByAgentsSchedule is a schedule the agent wrote waking the run.
	isWokenByAgentsSchedule bool
	configuration           *config.Configuration
}

func (self *fakeRun) IsWokenByAgentsSchedule() bool { return self.isWokenByAgentsSchedule }

func (self *fakeRun) Owner() *models.User   { return self.owner }
func (self *fakeRun) Database() db.Database { return self.database }
func (self *fakeRun) Surface() string       { return self.surface }
func (self *fakeRun) Headless() bool        { return self.isHeadless }
func (self *fakeRun) CanAsk() bool          { return !self.isHeadless }
func (self *fakeRun) IsStartedByPerson() bool {
	return !self.isStartedByOthers
}
func (self *fakeRun) Operations() tools.Operations {
	if self.hasNoGrant {
		return noPermissions{}
	}
	return permissions{}
}
func (self *fakeRun) Configuration() *config.Configuration {
	if self.configuration == nil {
		self.configuration = config.Default()
		self.configuration.Server.Secret = self.secret
	}
	return self.configuration
}

type noPermissions struct{ permissions }

func (noPermissions) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions(nil)
}

// chattingRun is a run whose words are posted to the people of a chat.
type chattingRun struct{ *fakeRun }

func (self chattingRun) IsAnsweringInChat() bool { return true }

// world is ada, her agent's key and her items.
type world struct {
	database       db.Database
	ada, ben, cora *models.User

	grantedLogin, ungrantedLogin, grantedNote, card, othersNote, unmarkedNote, unmarkedLogin string
}

// people are the key pairs of ada and ben, made when they set up Passwords.
// Their devices seal item keys for the agent key with the private half, and the
// server holds the public half.
type people struct{ ada, ben *passwords.KeyPair }

const serverSecret = "a-server-secret-for-the-tests"

// newWorld stores ada and ben, the server's agent key, and the items. Ada
// turned on "Let the agent use this" for a login and a note, not for a second
// login, and a card was somehow marked as usable (the database refuses that,
// so the test lifts its check and marks it by hand). Ben turned it on for a note of his own.
func newWorld(test *testing.T) *world {
	test.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	found := &world{database: database}

	agentKeyPair, err := passwords.GenerateKeyPair()
	if err != nil {
		test.Fatal(err)
	}
	box, err := secretbox.New([]byte(serverSecret), models.PasswordAgentKeyLabel)
	if err != nil {
		test.Fatal(err)
	}
	sealedPrivateKey, err := box.Seal(agentKeyPair.PrivateKey())
	if err != nil {
		test.Fatal(err)
	}

	var markCard string
	keyPairs := people{}
	for _, target := range []**passwords.KeyPair{&keyPairs.ada, &keyPairs.ben} {
		if *target, err = passwords.GenerateKeyPair(); err != nil {
			test.Fatal(err)
		}
	}
	seal := func(tx db.Transaction, owner *models.User, ownerKeyPair *passwords.KeyPair, number int, itemKind models.PasswordItemKind, fields any, isUsable bool) string {
		itemId := fmt.Sprintf("00000000-0000-4000-8000-%012d", number)
		itemKey, err := passwords.NewKey()
		if err != nil {
			test.Fatal(err)
		}
		plaintext, _ := json.Marshal(fields)
		sealedFields, err := passwords.Seal(itemKey, plaintext, passwords.ItemFieldsData(itemId, passwords.ItemKind(itemKind)))
		if err != nil {
			test.Fatal(err)
		}
		if _, err := tx.SavePasswordItem(&models.PasswordItem{
			ID: itemId, OwnerUserID: owner.ID, ItemKind: itemKind,
			EncryptedFields: sealedFields.String(), ItemKeyByAccountKey: `{"version":1,"nonce":"AA==","ciphertext":"AA=="}`,
		}, nil); err != nil {
			test.Fatal(err)
		}
		if !isUsable {
			return itemId
		}
		itemKeyByAgentKey, err := passwords.SealForKeyPair(ownerKeyPair, agentKeyPair.PublicKey(), itemKey, passwords.ShareData(itemId, owner.ID, passwords.AgentRecipientID))
		if err != nil {
			test.Fatal(err)
		}
		if itemKind == models.PasswordItemKindCard {
			if _, err := tx.SetPasswordItemUsableByAgent(owner.ID, itemId, true, itemKeyByAgentKey.String()); err == nil {
				test.Fatal("the database should refuse a card")
			}
			// Outside the transaction, which has not committed the row yet.
			markCard = fmt.Sprintf(`UPDATE password_item SET is_usable_by_agent = true, item_key_by_agent_key = '%s' WHERE id = '%s'`, itemKeyByAgentKey.String(), itemId)
			return itemId
		}
		if _, err := tx.SetPasswordItemUsableByAgent(owner.ID, itemId, true, itemKeyByAgentKey.String()); err != nil {
			test.Fatal(err)
		}
		return itemId
	}

	if err := database.Transaction(func(tx db.Transaction) (err error) {
		if found.ada, err = tx.CreateUser(&models.User{Username: "ada", Name: "Ada Example"}); err != nil {
			return err
		}
		if found.ben, err = tx.CreateUser(&models.User{Username: "ben", Name: "Ben Example"}); err != nil {
			return err
		}
		if found.cora, err = tx.CreateUser(&models.User{Username: "cora", Name: "Cora Example"}); err != nil {
			return err
		}
		for _, person := range []*models.User{found.ada, found.ben, found.cora} {
			dbtest.GrantPermission(test, tx, person.ID, models.PermissionPasswordsUse)
		}
		for person, keyPair := range map[*models.User]*passwords.KeyPair{found.ada: keyPairs.ada, found.ben: keyPairs.ben} {
			if _, err := tx.CreatePasswordsSetup(&models.PasswordSetup{
				UserID: person.ID, AccountKeyByPassphrase: "a", PassphraseSalt: "c2FsdA==", PassphraseIterationCount: 600000, PassphraseProofHash: "b",
				AccountKeyByRecoveryKey: "c", RecoveryKeyProofHash: "d", PrivateKeyByAccountKey: "e",
				PublicKey: base64.StdEncoding.EncodeToString(keyPair.PublicKey()),
			}); err != nil {
				return err
			}
		}
		if _, err := tx.CreatePasswordAgentKey(&models.PasswordAgentKey{
			PublicKey: base64.StdEncoding.EncodeToString(agentKeyPair.PublicKey()), PrivateKeySealed: sealedPrivateKey,
		}); err != nil {
			return err
		}
		found.grantedLogin = seal(tx, found.ada, keyPairs.ada, 1, models.PasswordItemKindLogin, passwords.LoginFields{
			Name: "Example shop", UserName: "ada@example.test", Password: loginPassword, Sites: []string{"https://shop.example.test"},
			Access:         passwords.AccessWithAgentUse(nil, true),
			OneTimeCodeURI: "otpauth://totp/Example:ada?secret=" + loginOneTimeSecret,
			Notes:          "the notes of the login are not listed",
			Passkeys:       []passwords.LoginPasskey{{CredentialID: []byte("credential"), RelyingPartyID: "shop.example.test", PrivateKey: loginPasskeyKey}},
		}, true)
		found.ungrantedLogin = seal(tx, found.ada, keyPairs.ada, 2, models.PasswordItemKindLogin, passwords.LoginFields{
			Name: "Example bank", UserName: "ada.bank", Password: "never-given-to-the-agent", Sites: []string{"https://bank.example.test"},
		}, false)
		found.grantedNote = seal(tx, found.ada, keyPairs.ada, 3, models.PasswordItemKindNote, passwords.NoteFields{Name: "Shed", Text: "The shed padlock opens with 0000.", Access: passwords.AccessWithAgentUse(nil, true)}, true)
		found.card = seal(tx, found.ada, keyPairs.ada, 4, models.PasswordItemKindCard, passwords.CardFields{Name: "Example card", Number: cardNumber, SecurityCode: "123"}, true)
		found.othersNote = seal(tx, found.ben, keyPairs.ben, 5, models.PasswordItemKindNote, passwords.NoteFields{Name: "Garage", Text: otherPersonsText, Access: passwords.AccessWithAgentUse(nil, true)}, true)
		// Turned on in the database, but the sealed fields do not say the agent
		// may use them (the record the person's device writes is what counts).
		found.unmarkedNote = seal(tx, found.ada, keyPairs.ada, 6, models.PasswordItemKindNote, passwords.NoteFields{Name: "Loft", Text: "The loft hatch sticks."}, true)
		found.unmarkedLogin = seal(tx, found.ada, keyPairs.ada, 7, models.PasswordItemKindLogin, passwords.LoginFields{Name: "Example club", UserName: "ada.club", Password: "an-invented-club-password", Sites: []string{"https://club.example.test"}}, true)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	// The database refuses a usable card with a check on the table. The
	// tool refuses one too, in case a row ever got past, so the test lifts
	// the check in its own throwaway database to prove it.
	rawExec := database.(interface{ RawExec(string) error }).RawExec
	if err := rawExec(`ALTER TABLE password_item DROP CONSTRAINT password_item_check1`); err != nil {
		test.Fatal(err)
	}
	if err := rawExec(markCard); err != nil {
		test.Fatal(err)
	}
	return found
}

func (self *world) runOf() *fakeRun {
	return &fakeRun{database: self.database, owner: self.ada, secret: serverSecret}
}

// act runs one action of the passwords tool as the model does: through the
// merged tool, naming the action.
func act(test *testing.T, run tools.Run, action, arguments string) (string, *tools.Result, error) {
	test.Helper()
	merged := tools.Build().Get("passwords")
	if merged == nil {
		test.Fatal("no passwords tool")
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(arguments), &fields); err != nil {
		test.Fatalf("%s: %s", err, arguments)
	}
	fields["action"] = action
	encoded, _ := json.Marshal(fields)
	result, err := merged.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "call", Arguments: encoded})
	if err != nil {
		return "", nil, err
	}
	return result.Content, result, nil
}

func mustAct(test *testing.T, run tools.Run, action, arguments string) (string, *tools.Result) {
	test.Helper()
	content, result, err := act(test, run, action, arguments)
	if err != nil {
		test.Fatalf("%s: %s", action, err)
	}
	return content, result
}

// lastUsedAt is when the agent last used the item, nil if never. The card's
// row is read the same way as any.
func (self *world) lastUsedAt(test *testing.T, itemId string) bool {
	test.Helper()
	var isUsed bool
	if err := self.database.Transaction(func(tx db.Transaction) error {
		for _, owner := range []*models.User{self.ada, self.ben} {
			items, err := tx.ListAgentUsablePasswordItems(owner.ID)
			if err != nil {
				return err
			}
			for _, item := range items {
				if item.ID == itemId {
					isUsed = item.LastUsedByAgentAt != nil
				}
			}
		}
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	return isUsed
}

// assertNoSecret fails when the answer carries something it must never carry.
func assertNoSecret(test *testing.T, content string) {
	test.Helper()
	for _, secret := range []string{
		loginPassword, loginOneTimeSecret, cardNumber, "never-given-to-the-agent",
		base64.StdEncoding.EncodeToString(loginPasskeyKey), string(loginPasskeyKey), "the notes of the login",
	} {
		if strings.Contains(content, secret) {
			test.Errorf("the answer carries %q: %s", secret, content)
		}
	}
}

// The list shows the items the person let the agent use and what is safe to
// say of them, and no password, code secret, passkey, card or item that is
// not turned on, or somebody else's.
func TestListShowsOnlyGrantedItemsAndNoSecrets(test *testing.T) {
	found := newWorld(test)
	content, _ := mustAct(test, found.runOf(), "list", `{}`)
	assertNoSecret(test, content)

	var answer struct {
		ItemCount int          `json:"item_count"`
		ItemList  []listedItem `json:"item_list"`
	}
	if err := json.Unmarshal([]byte(content), &answer); err != nil {
		test.Fatalf("%s: %s", err, content)
	}
	byID := map[string]listedItem{}
	for _, item := range answer.ItemList {
		byID[item.ItemID] = item
	}
	login, ok := byID[found.grantedLogin]
	if !ok || login.ItemKind != "login" || login.ItemName != "Example shop" || login.LoginUserName != "ada@example.test" ||
		len(login.Sites) != 1 || login.Sites[0] != "https://shop.example.test" ||
		login.HasOneTimeCode == nil || !*login.HasOneTimeCode || login.HasPasskeys == nil || !*login.HasPasskeys {
		test.Fatalf("the granted login: %+v in %s", login, content)
	}
	if note, ok := byID[found.grantedNote]; !ok || note.ItemKind != "note" || note.ItemName != "Shed" {
		test.Fatalf("the granted note: %+v in %s", note, content)
	}
	// A note's text is not in the list, only its name.
	if strings.Contains(content, "padlock") {
		test.Fatalf("a note's text in the list: %s", content)
	}
	if _, listed := byID[found.ungrantedLogin]; listed || strings.Contains(content, "Example bank") {
		test.Fatalf("an item the person did not turn on: %s", content)
	}
	if _, listed := byID[found.othersNote]; listed || strings.Contains(content, "Garage") {
		test.Fatalf("somebody else's item: %s", content)
	}
	// The card was somehow marked usable: it is listed as unreadable, by id
	// only, without its name.
	card, listed := byID[found.card]
	if !listed || !card.IsUnreadable || card.ItemName != "" || strings.Contains(content, "Example card") {
		test.Fatalf("the card: %+v in %s", card, content)
	}
	// An item whose sealed record does not let the agent use it is listed the
	// same way, by id only.
	for _, itemId := range []string{found.unmarkedNote, found.unmarkedLogin} {
		unmarked, listed := byID[itemId]
		if !listed || !unmarked.IsUnreadable || unmarked.ItemName != "" || strings.Contains(content, "Loft") || strings.Contains(content, "Example club") {
			test.Fatalf("an item whose sealed record does not allow the agent: %+v in %s", unmarked, content)
		}
	}
	if answer.ItemCount != 5 {
		test.Fatalf("item_count %d in %s", answer.ItemCount, content)
	}
}

// With nothing turned on, the answer says how to turn it on.
func TestListWithNothingSaysHowToTurnItOn(test *testing.T) {
	found := newWorld(test)
	run := &fakeRun{database: found.database, owner: found.cora, secret: serverSecret}
	content, _ := mustAct(test, run, "list", `{}`)
	if !strings.Contains(content, "turn on \"Let the agent use this\"") || !strings.Contains(content, "open the item in Passwords") {
		test.Fatalf("the words: %s", content)
	}
}

// A note is read by id, and the read is recorded on the note.
func TestReadNoteGivesTheTextAndRecordsTheUse(test *testing.T) {
	found := newWorld(test)
	if found.lastUsedAt(test, found.grantedNote) {
		test.Fatal("used before it was read")
	}
	content, result := mustAct(test, found.runOf(), "read_note", fmt.Sprintf(`{"item_id":%q}`, found.grantedNote))
	var answer struct {
		ItemName string `json:"item_name"`
		NoteText string `json:"note_text"`
	}
	if err := json.Unmarshal([]byte(content), &answer); err != nil || answer.NoteText != "The shed padlock opens with 0000." || answer.ItemName != "Shed" {
		test.Fatalf("%v: %s", err, content)
	}
	if !result.Untrusted {
		test.Error("the text of a note is data, not instructions")
	}
	if !found.lastUsedAt(test, found.grantedNote) {
		test.Fatal("the use is not recorded")
	}
	assertNoSecret(test, content)
}

// Everything but a note the person let the agent use is refused, in words, and
// is not recorded as used.
func TestReadNoteRefusesWhatIsNotAGrantedNote(test *testing.T) {
	found := newWorld(test)
	for name, testCase := range map[string]struct {
		itemId string
		want   string
	}{
		"a login that is not turned on": {found.ungrantedLogin, "turn on \"Let the agent use this\""},
		"somebody else's note":          {found.othersNote, "turn on \"Let the agent use this\""},
		"an item that does not exist":   {"00000000-0000-4000-8000-999999999999", "turn on \"Let the agent use this\""},
		"a login that is turned on":     {found.grantedLogin, "Only a note can be read"},
		"a card marked usable":          {found.card, "A card cannot be used by the agent"},
	} {
		content, _ := mustAct(test, found.runOf(), "read_note", fmt.Sprintf(`{"item_id":%q}`, testCase.itemId))
		if !strings.Contains(content, testCase.want) {
			test.Errorf("%s: %s", name, content)
		}
		assertNoSecret(test, content)
		if strings.Contains(content, otherPersonsText) {
			test.Errorf("%s: %s", name, content)
		}
		if found.lastUsedAt(test, testCase.itemId) {
			test.Errorf("%s: recorded as used", name)
		}
	}
	if _, _, err := act(test, found.runOf(), "read_note", `{}`); err == nil {
		test.Error("a note asked for without an id")
	}
}

// Where other people would read the answer, or the text would be filed from a
// subagent's answer, nothing is read and nothing is recorded.
func TestNothingIsReadWhereOthersWouldSeeIt(test *testing.T) {
	found := newWorld(test)
	asNote := fmt.Sprintf(`{"item_id":%q}`, found.grantedNote)

	for name, run := range map[string]tools.Run{
		"a chat with other people": chattingRun{found.runOf()},
		"telegram":                 &fakeRun{database: found.database, owner: found.ada, secret: serverSecret, surface: string(models.AgentChannelTelegram)},
		"discord":                  &fakeRun{database: found.database, owner: found.ada, secret: serverSecret, surface: string(models.AgentChannelDiscord)},
	} {
		for _, action := range []string{"list", "read_note", "generate"} {
			content, _ := mustAct(test, run, action, asNote)
			if !strings.Contains(content, "never in a chat with other people") {
				test.Errorf("%s, %s: %s", name, action, content)
			}
			if strings.Contains(content, "Shed") || strings.Contains(content, "padlock") {
				test.Errorf("%s, %s: %s", name, action, content)
			}
		}
	}

	subagent := &fakeRun{database: found.database, owner: found.ada, secret: serverSecret, surface: subagentSurface}
	content, _ := mustAct(test, subagent, "read_note", asNote)
	if strings.Contains(content, "padlock") || !strings.Contains(content, "not in a subagent") {
		test.Errorf("a subagent: %s", content)
	}
	// The names, sites and user names that list shows would come back in the
	// subagent's answer, so list is refused there too.
	content, _ = mustAct(test, subagent, "list", `{}`)
	if !strings.Contains(content, "not in a subagent") || strings.Contains(content, "item_list") || strings.Contains(content, "Shed") || strings.Contains(content, "padlock") {
		test.Errorf("a subagent, list: %s", content)
	}
	if found.lastUsedAt(test, found.grantedNote) {
		test.Error("a refused read was recorded as a use")
	}
}

// A run nobody is watching may use the items when the person started it: a
// schedule they wrote themselves, since signing in on a schedule is why the
// person turns them on.
func TestAScheduleThePersonWroteMayReadAGrantedNote(test *testing.T) {
	found := newWorld(test)
	run := &fakeRun{database: found.database, owner: found.ada, secret: serverSecret, surface: "schedule", isHeadless: true}
	content, _ := mustAct(test, run, "read_note", fmt.Sprintf(`{"item_id":%q}`, found.grantedNote))
	if !strings.Contains(content, "padlock") {
		test.Fatalf("a scheduled run: %s", content)
	}
	if listed, _ := mustAct(test, run, "list", `{}`); !strings.Contains(listed, found.grantedLogin) {
		test.Fatalf("a scheduled run lists: %s", listed)
	}
	if err := CheckSignInHere(run); err != nil {
		test.Fatalf("a scheduled run signs in: %s", err)
	}
}

// A run that content started (an inbound mail, a webhook, a chat app, a goal, a
// dream, a schedule the agent wrote for itself) reads no note, lists no item
// and signs in to nothing, and records no use.
func TestARunThatContentStartedUsesNothing(test *testing.T) {
	found := newWorld(test)
	for _, surface := range []string{"research", "goal", "mail", "schedule", "background", "dream", "speak-first:overnight", ""} {
		run := &fakeRun{database: found.database, owner: found.ada, secret: serverSecret, surface: surface, isHeadless: true, isStartedByOthers: true}
		for _, action := range []string{"list", "read_note"} {
			content, _ := mustAct(test, run, action, fmt.Sprintf(`{"item_id":%q}`, found.grantedNote))
			if !strings.Contains(content, "Ask in the TeaNode app or on the dashboard") {
				test.Errorf("%q, %s: %s", surface, action, content)
			}
			if strings.Contains(content, "padlock") || strings.Contains(content, "Example shop") {
				test.Errorf("%q, %s: %s", surface, action, content)
			}
		}
		err := CheckSignInHere(run)
		if err == nil || !strings.Contains(err.Error(), "content started") {
			test.Errorf("%q signs in: %v", surface, err)
		}
	}
	if found.lastUsedAt(test, found.grantedNote) {
		test.Error("a refused read was recorded as a use")
	}
	// The same run, started by the person, may.
	run := &fakeRun{database: found.database, owner: found.ada, secret: serverSecret}
	if err := CheckSignInHere(run); err != nil {
		test.Fatalf("the person's own run: %s", err)
	}
	// A run that cannot say who started it is not the person's.
	if err := CheckSignInHere(runWithoutOrigin{found.runOf()}); err == nil {
		test.Fatal("a run that cannot say who started it signed in")
	}
}

// A run woken by a schedule the agent wrote is told so, and how the person
// opens it: the same words from every action and from the browser's sign-in
// check, and no item text. Any other run content started keeps the general
// words.
func TestARunOfAnAgentsScheduleIsToldHowThePersonOpensIt(test *testing.T) {
	found := newWorld(test)
	run := &fakeRun{database: found.database, owner: found.ada, secret: serverSecret, surface: "schedule", isHeadless: true, isStartedByOthers: true, isWokenByAgentsSchedule: true}
	wanted := "The person can open the schedule on the dashboard or in the app (Agent, Schedules) and save it themselves to let its runs use Passwords."
	for _, action := range []string{"list", "read_note"} {
		content, _ := mustAct(test, run, action, fmt.Sprintf(`{"item_id":%q}`, found.grantedNote))
		if !strings.Contains(content, "schedule that the agent wrote") || !strings.Contains(content, wanted) {
			test.Errorf("%s: %s", action, content)
		}
		if strings.Contains(content, "padlock") || strings.Contains(content, "Example shop") {
			test.Errorf("%s: %s", action, content)
		}
	}
	err := CheckSignInHere(run)
	if err == nil || !strings.Contains(err.Error(), wanted) {
		test.Errorf("sign in: %v", err)
	}
	// A run that content started, which no schedule woke, keeps the general words.
	other := &fakeRun{database: found.database, owner: found.ada, secret: serverSecret, surface: "mail", isHeadless: true, isStartedByOthers: true}
	content, _ := mustAct(test, other, "list", `{}`)
	if strings.Contains(content, "Agent, Schedules") || !strings.Contains(content, "content started") {
		test.Errorf("a mail's run: %s", content)
	}
}

// An item the server lists as usable by the agent, but whose sealed fields do
// not carry the person's own record saying so, is refused in the same words as
// any other item the agent may not use, and nothing of it is given out.
func TestAnItemWhoseSealedRecordDoesNotAllowTheAgentIsRefused(test *testing.T) {
	found := newWorld(test)
	run := found.runOf()
	content, _ := mustAct(test, run, "read_note", fmt.Sprintf(`{"item_id":%q}`, found.unmarkedNote))
	if !strings.HasPrefix(content, "This item is not one you may use.") || strings.Contains(content, "hatch") {
		test.Errorf("read_note: %s", content)
	}
	if found.lastUsedAt(test, found.unmarkedNote) {
		test.Error("a refused read was recorded as a use")
	}
	_, err := OpenLoginToSignIn(context.Background(), run, found.unmarkedLogin)
	if err == nil || !strings.HasPrefix(err.Error(), "This item is not one you may use.") || strings.Contains(err.Error(), "club-password") {
		test.Errorf("sign in: %v", err)
	}
}

// runWithoutOrigin hides the run's answer to who started it.
type runWithoutOrigin struct{ tools.Run }

// The person must still hold passwords:use when an item is used: a role that
// lost it, or an account that was disabled, since the item was turned on, or
// since the run began, ends the agent's use of every item.
func TestTheOwnerMustStillHoldThePasswordsPermissionAtTheMomentOfUse(test *testing.T) {
	found := newWorld(test)
	run := found.runOf()
	if content, _ := mustAct(test, run, "list", `{}`); !strings.Contains(content, found.grantedLogin) {
		test.Fatalf("before: %s", content)
	}
	// The run was made while ada held the permission (its Operations say so);
	// then her group is taken away.
	dbtest.Exec(test, found.database, fmt.Sprintf(`DELETE FROM user_group WHERE user_id = '%s'`, found.ada.ID))
	content, _ := mustAct(test, run, "list", `{}`)
	if !strings.Contains(content, "not allowed to use Passwords") || strings.Contains(content, "Example shop") {
		test.Fatalf("list: %s", content)
	}
	content, _ = mustAct(test, run, "read_note", fmt.Sprintf(`{"item_id":%q}`, found.grantedNote))
	if !strings.Contains(content, "not allowed to use Passwords") || strings.Contains(content, "padlock") {
		test.Fatalf("read_note: %s", content)
	}
	if found.lastUsedAt(test, found.grantedNote) {
		test.Fatal("a refused read was recorded")
	}
	_, err := OpenLoginToSignIn(context.Background(), run, found.grantedLogin)
	if err == nil || !strings.Contains(err.Error(), "not allowed to use Passwords") {
		test.Fatalf("OpenLoginToSignIn: %v", err)
	}
}

// A person without Passwords is refused whatever the run.
func TestTheActionsNeedThePasswordsPermission(test *testing.T) {
	found := newWorld(test)
	run := found.runOf()
	run.hasNoGrant = true
	for _, action := range []string{"list", "read_note", "generate"} {
		if _, _, err := act(test, run, action, fmt.Sprintf(`{"item_id":%q}`, found.grantedNote)); err == nil {
			test.Errorf("%s without passwords:use", action)
		}
	}
}

// An agent key sealed under another secret does not open: the tool says so as
// an error rather than listing nothing, and gives out nothing.
func TestAWrongServerSecretOpensNothing(test *testing.T) {
	found := newWorld(test)
	run := found.runOf()
	run.secret = "another-secret-entirely"
	for _, action := range []string{"list", "read_note"} {
		content, _, err := act(test, run, action, fmt.Sprintf(`{"item_id":%q}`, found.grantedNote))
		if err == nil || strings.Contains(content, "padlock") {
			test.Errorf("%s: %q, %v", action, content, err)
		}
	}
}

// A password made up in a subagent would come back to its parent as a tool
// result: refused, with the words that say where it would go.
func TestGenerateIsRefusedInASubagent(test *testing.T) {
	found := newWorld(test)
	run := &fakeRun{database: found.database, owner: found.ada, secret: serverSecret, surface: subagentSurface}
	content, _ := mustAct(test, run, "generate", `{}`)
	if strings.Contains(content, "generated_password") || !strings.Contains(content, "go to the model's provider") || !strings.Contains(content, "save it themselves") {
		test.Fatalf("a subagent: %s", content)
	}
	// In a conversation with the person it still works, and says what it is.
	merged := tools.Build().Get("passwords")
	encoded, _ := json.Marshal(merged.Parameters)
	if text := merged.Description + string(encoded); !strings.Contains(text, "goes to the model's provider") {
		test.Fatalf("the tool does not say where the password goes: %s", text)
	}
}

// generate makes up a password of the length and kinds asked for and keeps no
// record of it.
func TestGenerateMakesAPassword(test *testing.T) {
	found := newWorld(test)
	content, _ := mustAct(test, found.runOf(), "generate", `{}`)
	var answer struct {
		GeneratedPassword string `json:"generated_password"`
		LengthCount       int    `json:"length_count"`
	}
	if err := json.Unmarshal([]byte(content), &answer); err != nil || len(answer.GeneratedPassword) != 20 || answer.LengthCount != 20 {
		test.Fatalf("%v: %s", err, content)
	}

	content, _ = mustAct(test, found.runOf(), "generate", `{"length_count":12,"should_include_letters":false,"should_include_symbols":false}`)
	if err := json.Unmarshal([]byte(content), &answer); err != nil || len(answer.GeneratedPassword) != 12 || strings.Trim(answer.GeneratedPassword, "0123456789") != "" {
		test.Fatalf("digits only: %v: %s", err, content)
	}
	if _, _, err := act(test, found.runOf(), "generate", `{"should_include_letters":false,"should_include_digits":false,"should_include_symbols":false}`); err == nil {
		test.Error("a password of no kind of character")
	}
	if _, _, err := act(test, found.runOf(), "generate", `{"length_count":500}`); err == nil {
		test.Error("a password of 500 characters")
	}
}
