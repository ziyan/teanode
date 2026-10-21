package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ziyan/teanode/internal/agent/tools"
	devtools "github.com/ziyan/teanode/internal/browser"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/passwords"
	"github.com/ziyan/teanode/internal/util/secretbox"
)

// The secrets of the invented items below. None of them may appear in an
// answer, an error or a snapshot.
const (
	inventedPassword      = "Tr0ub4dor&3-example"
	inventedUserName      = "ada@example.test"
	inventedOneTimeSecret = "JBSWY3DPEHPK3PXP"
	inventedCardNumber    = "4000000000001234"
	serverSecret          = "a-server-secret-for-the-tests"
	shopAddress           = "https://shop.example.test/sign-in"
)

// inventedPasskeyKey is the private key of the passkey of the world most
// recently made. The tests do not run in parallel.
var inventedPasskeyKey []byte

// world is ada, her agent's key and her items.
type world struct {
	database db.Database
	ada      *models.User
	ben      *models.User

	grantedLogin, ungrantedLogin, card, othersLogin, noPasskeyLogin, grantedNote, portedLogin string

	passkeyCredential, passkeyPrivateKey, passkeyUserHandle []byte
}

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
	if found.passkeyCredential, found.passkeyPrivateKey, err = passwords.NewPasskey(); err != nil {
		test.Fatal(err)
	}
	inventedPasskeyKey = found.passkeyPrivateKey
	found.passkeyUserHandle = []byte("user-handle-of-ada")

	adaKeyPair, err := passwords.GenerateKeyPair()
	if err != nil {
		test.Fatal(err)
	}
	benKeyPair, err := passwords.GenerateKeyPair()
	if err != nil {
		test.Fatal(err)
	}
	var markCard string
	seal := func(tx db.Transaction, owner *models.User, ownerKeyPair *passwords.KeyPair, number int, itemKind models.PasswordItemKind, fields any, isUsable bool) string {
		itemId := fmt.Sprintf("00000000-0000-4000-8000-%012d", number)
		itemKey, err := passwords.NewKey()
		if err != nil {
			test.Fatal(err)
		}
		plaintext, _ := json.Marshal(fields)
		if isUsable {
			// The owner's device writes the agent's permission into the sealed
			// fields, and the agent opens an item only if it is there.
			var decoded map[string]any
			if err := json.Unmarshal(plaintext, &decoded); err != nil {
				test.Fatal(err)
			}
			decoded["access"] = map[string]any{"isUsableByAgent": true, "grantees": []any{}}
			if plaintext, err = json.Marshal(decoded); err != nil {
				test.Fatal(err)
			}
		}
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
		for _, person := range []*models.User{found.ada, found.ben} {
			dbtest.GrantPermission(test, tx, person.ID, models.PermissionPasswordsUse)
		}
		for person, keyPair := range map[*models.User]*passwords.KeyPair{found.ada: adaKeyPair, found.ben: benKeyPair} {
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
		found.grantedLogin = seal(tx, found.ada, adaKeyPair, 1, models.PasswordItemKindLogin, passwords.LoginFields{
			Name: "Example shop", UserName: inventedUserName, Password: inventedPassword, Sites: []string{"https://www.shop.example.test"},
			OneTimeCodeURI: "otpauth://totp/Example:ada?secret=" + inventedOneTimeSecret,
			Passkeys: []passwords.LoginPasskey{
				{CredentialID: []byte("older-credential"), RelyingPartyID: "shop.example.test", PrivateKey: []byte("older-key"), CreatedAt: "2026-01-01T00:00:00Z"},
				{CredentialID: found.passkeyCredential, RelyingPartyID: "example.test", UserHandle: found.passkeyUserHandle, PrivateKey: found.passkeyPrivateKey, CreatedAt: "2026-02-01T00:00:00Z"},
				{CredentialID: []byte("another-site"), RelyingPartyID: "other.example.org", PrivateKey: []byte("another-key"), CreatedAt: "2026-03-01T00:00:00Z"},
			},
		}, true)
		found.ungrantedLogin = seal(tx, found.ada, adaKeyPair, 2, models.PasswordItemKindLogin, passwords.LoginFields{
			Name: "Example bank", UserName: "ada.bank", Password: "never-given-to-the-agent", Sites: []string{"https://shop.example.test"},
		}, false)
		found.card = seal(tx, found.ada, adaKeyPair, 4, models.PasswordItemKindCard, passwords.CardFields{Name: "Example card", Number: inventedCardNumber}, true)
		found.othersLogin = seal(tx, found.ben, benKeyPair, 5, models.PasswordItemKindLogin, passwords.LoginFields{
			Name: "Ben's shop", UserName: "ben", Password: "bens-password", Sites: []string{"https://shop.example.test"},
		}, true)
		found.noPasskeyLogin = seal(tx, found.ada, adaKeyPair, 6, models.PasswordItemKindLogin, passwords.LoginFields{
			Name: "Plain login", UserName: "ada", Password: "plain-password", Sites: []string{"https://shop.example.test"},
		}, true)
		found.grantedNote = seal(tx, found.ada, adaKeyPair, 7, models.PasswordItemKindNote, passwords.NoteFields{Name: "Shed", Text: "padlock"}, true)
		found.portedLogin = seal(tx, found.ada, adaKeyPair, 8, models.PasswordItemKindLogin, passwords.LoginFields{
			Name: "Ported shop", UserName: "ada.ported", Password: inventedPassword, Sites: []string{"https://shop.example.test:8443"},
			OneTimeCodeURI: "otpauth://totp/Example:ada?secret=" + inventedOneTimeSecret,
		}, true)
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	rawExec := database.(interface{ RawExec(string) error }).RawExec
	if err := rawExec(`ALTER TABLE password_item DROP CONSTRAINT password_item_check1`); err != nil {
		test.Fatal(err)
	}
	if err := rawExec(markCard); err != nil {
		test.Fatal(err)
	}
	return found
}

// fakeTab is the person's browser, which records what it is asked.
type fakeTab struct {
	address          string
	isTakenOver      bool
	canFillPasswords bool
	failure          string
	answer           string
	// fillAnswer is what the extension says to a fill; the default says only
	// that it filled.
	fillAnswer string
	// tabID is the number of the tab the actions go to, as the server last
	// heard it.
	tabID  string
	filled devtools.FilledValues

	mutex sync.Mutex
	asked []askedOfTab
}

type askedOfTab struct {
	action    string
	arguments string
}

func (self *fakeTab) Ask(_ context.Context, action string, arguments any) (json.RawMessage, error) {
	encoded, _ := json.Marshal(arguments)
	self.mutex.Lock()
	self.asked = append(self.asked, askedOfTab{action: action, arguments: string(encoded)})
	self.mutex.Unlock()
	if self.failure != "" {
		return nil, fmt.Errorf("the person's browser refused: %s", self.failure)
	}
	if self.answer != "" && action != "fill_login" && action != "fill_one_time_code" {
		return json.RawMessage(self.answer), nil
	}
	if self.fillAnswer != "" && (action == "fill_login" || action == "fill_one_time_code") {
		return json.RawMessage(self.fillAnswer), nil
	}
	return json.RawMessage(`{"isFilled":true}`), nil
}
func (self *fakeTab) Title() string                  { return "A shop" }
func (self *fakeTab) URL() string                    { return self.address }
func (self *fakeTab) HasTab() bool                   { return true }
func (self *fakeTab) IsTakenOver() bool              { return self.isTakenOver }
func (self *fakeTab) IsWatchable() bool              { return true }
func (self *fakeTab) Filled() *devtools.FilledValues { return &self.filled }
func (self *fakeTab) CanFillPasswords() bool         { return self.canFillPasswords }
func (self *fakeTab) CurrentTabID() string           { return self.tabID }
func (self *fakeTab) NoteCurrentTab(tab string)      { self.tabID = tab }
func (self *fakeTab) ServerHost() string             { return "" }
func (self *fakeTab) ListedTabs() []tools.TabAddress { return nil }

func (self *fakeTab) actions() []askedOfTab {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]askedOfTab{}, self.asked...)
}

// signInRun is ada's agent in a turn, with the database, a tab and, when
// there is one, a headless page.
type signInRun struct {
	tools.Run
	database   db.Database
	owner      *models.User
	surface    string
	isHeadless bool
	isChat     bool
	// isStartedByOthers is a run that content began, not the person.
	isStartedByOthers bool
	tab               *fakeTab
	page              *devtools.Context
}

func (self *signInRun) Owner() *models.User     { return self.owner }
func (self *signInRun) Database() db.Database   { return self.database }
func (self *signInRun) Surface() string         { return self.surface }
func (self *signInRun) Headless() bool          { return self.isHeadless }
func (self *signInRun) IsAnsweringInChat() bool { return self.isChat }
func (self *signInRun) IsStartedByPerson() bool { return !self.isStartedByOthers }
func (self *signInRun) Agent() *models.Agent    { return &models.Agent{ID: "agent-1"} }
func (self *signInRun) TabsAllowed() bool       { return true }
func (self *signInRun) AttachedTab() tools.Tab {
	if self.tab == nil {
		return nil
	}
	return self.tab
}
func (self *signInRun) Configuration() *config.Configuration {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Browser.Enabled = true
	configuration.Server.Secret = serverSecret
	return configuration
}
func (self *signInRun) BrowserPage(context.Context) (*devtools.Context, error) {
	if self.page == nil {
		return nil, fmt.Errorf("no headless browser in this test")
	}
	return self.page, nil
}
func (self *signInRun) ReconnectedTab(context.Context, tools.Tab, time.Duration) tools.Tab {
	return nil
}

func (self *world) tabRun(address string) *signInRun {
	return &signInRun{database: self.database, owner: self.ada, surface: "web", tab: &fakeTab{address: address, canFillPasswords: true}}
}

// fakeChrome is a DevTools endpoint that answers what the sign-in sends and
// records it.
type fakeChrome struct {
	address string
	// loader is the id of the document the main frame shows, "loader-1" when
	// empty. A test sets it under the mutex to move the page.
	loader string
	// failFrameTree makes the document of the main frame unanswerable.
	failFrameTree bool
	// evaluated is what a script of the model's evaluates to, when set.
	evaluated any

	mutex      sync.Mutex
	commands   []chromeCommand
	socket     *websocket.Conn
	writeMutex sync.Mutex
}

type chromeCommand struct {
	method     string
	parameters string
}

func (self *fakeChrome) sent(method string) []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	var found []string
	for _, command := range self.commands {
		if command.method == method {
			found = append(found, command.parameters)
		}
	}
	return found
}

// fillCall is one fill the page was sent: the script's arguments.
type fillCall struct {
	Host     string
	Port     int
	Ref      int
	Kind     string
	Text     string
	IsSecret bool
}

// fills are the fills the page was sent so far, in order. A fill is a single
// script that checks the page and sets the value, so the text is an argument
// of that script and is never sent by a command of its own.
func (self *fakeChrome) fills(test *testing.T) []fillCall {
	test.Helper()
	var calls []fillCall
	for _, parameters := range self.sent("Runtime.evaluate") {
		var evaluated struct {
			Expression string `json:"expression"`
		}
		if err := json.Unmarshal([]byte(parameters), &evaluated); err != nil {
			test.Fatal(err)
		}
		if !strings.Contains(evaluated.Expression, "isFilled") {
			continue
		}
		start := strings.LastIndex(evaluated.Expression, "\n})(")
		if start < 0 {
			test.Fatalf("a fill script without arguments: %s", evaluated.Expression)
		}
		var arguments []any
		if err := json.Unmarshal([]byte("["+strings.TrimSuffix(evaluated.Expression[start+len("\n})("):], ")")+"]"), &arguments); err != nil || len(arguments) != 6 {
			test.Fatalf("the arguments of a fill: %v %v", arguments, err)
		}
		calls = append(calls, fillCall{
			Host: arguments[0].(string), Port: int(arguments[1].(float64)), Ref: int(arguments[2].(float64)),
			Kind: arguments[3].(string), Text: arguments[4].(string), IsSecret: arguments[5].(bool),
		})
	}
	return calls
}

// emit sends the page's event to the tool, as Chrome does.
func (self *fakeChrome) emit(method, parameters string) {
	self.mutex.Lock()
	socket := self.socket
	self.mutex.Unlock()
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	_ = socket.WriteJSON(map[string]any{"method": method, "sessionId": "session-1", "params": json.RawMessage(parameters)})
}

// pageOf starts the fake Chrome and opens a page in it.
func (self *fakeChrome) pageOf(test *testing.T) *devtools.Context {
	test.Helper()
	upgrader := websocket.Upgrader{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/json/version" {
			_ = json.NewEncoder(writer).Encode(map[string]any{"webSocketDebuggerUrl": "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/browser/x"})
			return
		}
		socket, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		self.mutex.Lock()
		self.socket = socket
		self.mutex.Unlock()
		defer func() { _ = socket.Close() }()
		for {
			var incoming struct {
				ID        int64           `json:"id"`
				SessionID string          `json:"sessionId"`
				Method    string          `json:"method"`
				Params    json.RawMessage `json:"params"`
			}
			if err := socket.ReadJSON(&incoming); err != nil {
				return
			}
			self.mutex.Lock()
			self.commands = append(self.commands, chromeCommand{method: incoming.Method, parameters: string(incoming.Params)})
			address := self.address
			loader, failFrameTree := self.loader, self.failFrameTree
			if loader == "" {
				loader = "loader-1"
			}
			self.mutex.Unlock()
			var result any = map[string]any{}
			switch incoming.Method {
			case "Target.createBrowserContext":
				result = map[string]any{"browserContextId": "ctx-1"}
			case "Target.createTarget":
				result = map[string]any{"targetId": "target-1"}
			case "Target.attachToTarget":
				result = map[string]any{"sessionId": "session-1"}
			case "Page.getNavigationHistory":
				result = map[string]any{"currentIndex": 1, "entries": []map[string]any{{"id": 1, "url": "about:blank"}, {"id": 2, "url": address}}}
			case "Page.getFrameTree":
				if failFrameTree {
					self.writeMutex.Lock()
					_ = socket.WriteJSON(map[string]any{"id": incoming.ID, "sessionId": incoming.SessionID, "error": map[string]any{"code": -32000, "message": "Not attached to an active page"}})
					self.writeMutex.Unlock()
					continue
				}
				result = map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "main", "loaderId": loader, "url": address}}}
			case "Runtime.evaluate":
				value := any(map[string]any{"isFilled": true, "problem": ""})
				if self.evaluated != nil && !strings.Contains(string(incoming.Params), "isFilled") {
					value = self.evaluated
				}
				result = map[string]any{"result": map[string]any{"type": "object", "value": value}}
			case "WebAuthn.addVirtualAuthenticator":
				result = map[string]any{"authenticatorId": "authenticator-1"}
			}
			encoded, _ := json.Marshal(result)
			self.writeMutex.Lock()
			_ = socket.WriteJSON(map[string]any{"id": incoming.ID, "sessionId": incoming.SessionID, "result": json.RawMessage(encoded)})
			self.writeMutex.Unlock()
		}
	}))
	test.Cleanup(server.Close)
	browser, err := devtools.Connect(context.Background(), &devtools.Settings{Endpoint: server.URL, ProxyListen: "127.0.0.1:0"})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = browser.Close() })
	page, err := browser.NewContext(context.Background())
	if err != nil {
		test.Fatal(err)
	}
	return page
}

func (self *world) headlessRun(test *testing.T, address string) (*signInRun, *fakeChrome) {
	test.Helper()
	chrome := &fakeChrome{address: address}
	return &signInRun{database: self.database, owner: self.ada, surface: "web", page: chrome.pageOf(test)}, chrome
}

// browse runs one call of the browser tool as the model makes it.
func browse(run tools.Run, arguments string) (*tools.Result, error) {
	return runBrowser(tools.WithRun(context.Background(), run), &tools.Call{ID: "call-1", Arguments: json.RawMessage(arguments)})
}

// assertNoSecret fails when words carry something that must never reach the
// model.
func assertNoSecret(test *testing.T, words string) {
	test.Helper()
	for _, secret := range []string{inventedPassword, inventedOneTimeSecret, inventedCardNumber, "never-given-to-the-agent", "bens-password", "plain-password",
		base64.StdEncoding.EncodeToString(inventedPasskeyKey), string(inventedPasskeyKey)} {
		if secret != "" && strings.Contains(words, secret) {
			test.Errorf("%q reached the model: %s", secret, words)
		}
	}
}

// usedOn is the hosts the item's recorded uses name, from the audit log.
func (self *world) usedOn(test *testing.T, itemId string) []string {
	test.Helper()
	var hosts []string
	if err := self.database.Transaction(func(tx db.Transaction) error {
		events, err := tx.ListAuditEvents(&db.AuditOptions{ResourceType: string(models.AuditResourcePasswordItem), ResourceID: itemId})
		if err != nil {
			return err
		}
		for _, event := range events {
			var after struct {
				Event string `json:"event"`
				Host  string `json:"host"`
			}
			if json.Unmarshal(event.After, &after) == nil && after.Event == "used_by_agent" {
				hosts = append(hosts, after.Host)
			}
		}
		return nil
	}); err != nil {
		test.Fatal(err)
	}
	return hosts
}

func mustRefuse(test *testing.T, run tools.Run, arguments, wanted string) {
	test.Helper()
	result, err := browse(run, arguments)
	if err == nil {
		test.Fatalf("%s was not refused: %+v", arguments, result)
	}
	if !strings.Contains(err.Error(), wanted) {
		test.Fatalf("%s was refused with %q, wanted %q", arguments, err, wanted)
	}
	assertNoSecret(test, err.Error())
}

func fillLogin(itemId string) string {
	return fmt.Sprintf(`{"action":"fill_login","item_id":%q,"user_name_ref":3,"password_ref":4}`, itemId)
}

// An item the person did not turn on, a card, a note, somebody else's item
// and an id that is nothing are all refused, in the tab and in the headless
// browser, before the page is asked anything.
func TestSignInRefusesItemsTheAgentMayNotUse(test *testing.T) {
	found := newWorld(test)
	tabRun := found.tabRun(shopAddress)
	headlessRun, chrome := found.headlessRun(test, shopAddress)
	// Not turned on, somebody else's, and no such item.
	for _, itemId := range []string{found.ungrantedLogin, found.othersLogin, "00000000-0000-4000-8000-000000000099"} {
		for _, run := range []*signInRun{tabRun, headlessRun} {
			mustRefuse(test, run, fillLogin(itemId), "Let the agent use this")
			mustRefuse(test, run, fmt.Sprintf(`{"action":"fill_one_time_code","item_id":%q,"ref":5}`, itemId), "Let the agent use this")
		}
	}
	for _, run := range []*signInRun{tabRun, headlessRun} {
		mustRefuse(test, run, fillLogin(found.card), "A card cannot be used by the agent")
		mustRefuse(test, run, fillLogin(found.grantedNote), "Only a login can sign in")
		mustRefuse(test, run, `{"action":"fill_login","user_name_ref":3,"password_ref":4}`, "item_id")
	}
	if asked := tabRun.tab.actions(); len(asked) != 0 {
		test.Fatalf("the tab was asked %v", asked)
	}
	if filled := chrome.fills(test); len(filled) != 0 {
		test.Fatalf("the page was filled: %v", filled)
	}
	for _, itemId := range []string{found.ungrantedLogin, found.card, found.grantedNote, found.othersLogin} {
		if hosts := found.usedOn(test, itemId); len(hosts) != 0 {
			test.Fatalf("a use was recorded for a refused item: %v", hosts)
		}
	}
}

// The page's host is the browser's, never the model's. The host of a look-alike
// page, a parent, a child and a page that merely has the site in its path are
// refused; so is a site the model names that is not the page.
func TestSignInRefusesAHostThatIsNotTheItemsSite(test *testing.T) {
	found := newWorld(test)
	for _, address := range []string{
		"https://shop.example.test.evil.test/sign-in",
		"https://evil.test/shop.example.test",
		"https://example.test/",
		"https://login.shop.example.test/",
		"https://shop.example.test@evil.test/",
		"https://notshop.example.test/",
		"about:blank",
		"",
	} {
		// The model names the right site in every argument it can; the page is
		// elsewhere.
		arguments := fmt.Sprintf(`{"action":"fill_login","item_id":%q,"user_name_ref":3,"password_ref":4,"url":%q,"selector":"#shop.example.test","text":"shop.example.test"}`, found.grantedLogin, shopAddress)
		tabRun := found.tabRun(address)
		mustRefuse(test, tabRun, arguments, "")
		headlessRun, chrome := found.headlessRun(test, address)
		mustRefuse(test, headlessRun, arguments, "")
		mustRefuse(test, headlessRun, fmt.Sprintf(`{"action":"fill_one_time_code","item_id":%q,"ref":5}`, found.grantedLogin), "")
		if asked := tabRun.tab.actions(); len(asked) != 0 {
			test.Fatalf("%s: the tab was asked %v", address, asked)
		}
		if filled := chrome.fills(test); len(filled) != 0 {
			test.Fatalf("%s: the page was filled: %v", address, filled)
		}
	}
	if hosts := found.usedOn(test, found.grantedLogin); len(hosts) != 0 {
		test.Fatalf("a use was recorded for a refused host: %v", hosts)
	}
	// The error says what the page is, in words.
	mustRefuse(test, found.tabRun("https://shop.example.test.evil.test/"), fillLogin(found.grantedLogin), "not one of that login's sites")
	// And a page that is not https is not typed into.
	mustRefuse(test, found.tabRun("http://shop.example.test/"), fillLogin(found.grantedLogin), "not an https page")
	// While the model's own arguments change nothing about a page that is right.
	run := found.tabRun("https://shop.example.test/account")
	if _, err := browse(run, fmt.Sprintf(`{"action":"fill_login","item_id":%q,"user_name_ref":3,"password_ref":4,"url":"https://evil.test/"}`, found.grantedLogin)); err != nil {
		test.Fatalf("the page is the site: %s", err)
	}
}

// A tab the person has taken over is not filled, and neither is one in an
// extension that does not fill passwords yet. A passkey is not carried to the
// person's browser at all.
func TestSignInInTheTabRefusesATakenOverTabAnOldExtensionAndPasskeys(test *testing.T) {
	found := newWorld(test)
	run := found.tabRun(shopAddress)
	run.tab.isTakenOver = true
	mustRefuse(test, run, fillLogin(found.grantedLogin), "taken over")
	mustRefuse(test, run, fmt.Sprintf(`{"action":"fill_one_time_code","item_id":%q,"ref":5}`, found.grantedLogin), "taken over")
	mustRefuse(test, run, fmt.Sprintf(`{"action":"passkey_sign_in","item_id":%q}`, found.grantedLogin), "")
	run.tab.isTakenOver = false

	run.tab.canFillPasswords = false
	mustRefuse(test, run, fillLogin(found.grantedLogin), "The TeaNode extension in that browser needs updating to fill passwords")
	mustRefuse(test, run, fmt.Sprintf(`{"action":"fill_one_time_code","item_id":%q,"ref":5}`, found.grantedLogin), "needs updating to fill passwords")
	run.tab.canFillPasswords = true

	for _, arguments := range []string{
		fmt.Sprintf(`{"action":"passkey_sign_in","item_id":%q}`, found.grantedLogin),
		fmt.Sprintf(`{"action":"passkey_sign_in","item_id":%q,"target":"tab"}`, found.grantedLogin),
	} {
		mustRefuse(test, run, arguments, "works only in the agent's own browser")
	}
	if asked := run.tab.actions(); len(asked) != 0 {
		test.Fatalf("the tab was asked %v", asked)
	}
	if hosts := found.usedOn(test, found.grantedLogin); len(hosts) != 0 {
		test.Fatalf("a use was recorded for a refused action: %v", hosts)
	}
}

// None of the three is a reading action, so the take-over refusal covers
// them, and each is a write.
func TestSignInActionsAreWritingActions(test *testing.T) {
	for action := range browserPasswordActions {
		if browserReadingActions[action] {
			test.Errorf("%s may run in a tab the person has taken over", action)
		}
		if !browserWritingActions[action] {
			test.Errorf("%s is not a write", action)
		}
	}
}

// Over Telegram or Discord and in a chat with other people nothing is filled;
// a schedule the person wrote is allowed.
func TestSignInIsRefusedWhereOthersReadAndAllowedOnASchedule(test *testing.T) {
	found := newWorld(test)
	for _, surface := range []string{"telegram", "discord"} {
		run := found.tabRun(shopAddress)
		run.surface = surface
		mustRefuse(test, run, fillLogin(found.grantedLogin), "never in a chat with other people")
		if asked := run.tab.actions(); len(asked) != 0 {
			test.Fatalf("%s: the tab was asked %v", surface, asked)
		}
	}
	chatRun := found.tabRun(shopAddress)
	chatRun.isChat = true
	mustRefuse(test, chatRun, fillLogin(found.grantedLogin), "never in a chat with other people")

	scheduled, chrome := found.headlessRun(test, shopAddress)
	scheduled.surface, scheduled.isHeadless = "schedule", true
	if _, err := browse(scheduled, fillLogin(found.grantedLogin)); err != nil {
		test.Fatalf("a scheduled run signs in: %s", err)
	}
	if filled := chrome.fills(test); len(filled) != 2 {
		test.Fatalf("filled %v", filled)
	}
}

// In the tab, the extension is sent what the contract says: the host, the
// refs and the values, from the server. The answer says what was filled and
// no value; the use is recorded with the host.
func TestSignInInTheTabSendsTheContractArguments(test *testing.T) {
	found := newWorld(test)
	run := found.tabRun("https://www.Shop.Example.test/sign-in?next=/account")
	result, err := browse(run, fillLogin(found.grantedLogin))
	if err != nil {
		test.Fatal(err)
	}
	if result.Content != "Filled the user name and password for shop.example.test from Example shop" {
		test.Fatalf("the answer: %q", result.Content)
	}
	assertNoSecret(test, result.Content)
	if strings.Contains(result.Content, inventedUserName) {
		test.Fatalf("the user name in the answer: %q", result.Content)
	}
	asked := run.tab.actions()
	if len(asked) != 1 || asked[0].action != "fill_login" {
		test.Fatalf("the tab was asked %v", asked)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(asked[0].arguments), &sent); err != nil {
		test.Fatal(err)
	}
	wanted := map[string]any{"host": "shop.example.test", "port": float64(443), "userNameRef": float64(3), "passwordRef": float64(4), "userName": inventedUserName, "password": inventedPassword}
	if fmt.Sprint(sent) != fmt.Sprint(wanted) {
		test.Fatalf("the extension was sent %v, wanted %v", sent, wanted)
	}
	if hosts := found.usedOn(test, found.grantedLogin); len(hosts) != 1 || hosts[0] != "shop.example.test" {
		test.Fatalf("the use was recorded for %v", hosts)
	}

	// Only the password: the user name is neither sent nor asked for.
	result, err = browse(run, fmt.Sprintf(`{"action":"fill_login","item_id":%q,"password_ref":9}`, found.grantedLogin))
	if err != nil || result.Content != "Filled the password for shop.example.test from Example shop" {
		test.Fatalf("a password alone: %v %v", result, err)
	}
	asked = run.tab.actions()
	if got := asked[len(asked)-1].arguments; strings.Contains(got, inventedUserName) || !strings.Contains(got, `"userNameRef":0`) || !strings.Contains(got, `"passwordRef":9`) {
		test.Fatalf("a password alone was sent as %s", got)
	}
	mustRefuse(test, run, fmt.Sprintf(`{"action":"fill_login","item_id":%q}`, found.grantedLogin), "user_name_ref")
}

func TestSignInWithAOneTimeCodeInTheTab(test *testing.T) {
	found := newWorld(test)
	run := found.tabRun(shopAddress)
	before := currentCode(test)
	result, err := browse(run, fmt.Sprintf(`{"action":"fill_one_time_code","item_id":%q,"ref":6}`, found.grantedLogin))
	after := currentCode(test)
	if err != nil {
		test.Fatal(err)
	}
	if result.Content != "Filled the one-time code for shop.example.test from Example shop" {
		test.Fatalf("the answer: %q", result.Content)
	}
	assertNoSecret(test, result.Content)
	asked := run.tab.actions()
	if len(asked) != 1 || asked[0].action != "fill_one_time_code" {
		test.Fatalf("the tab was asked %v", asked)
	}
	var sent struct {
		Host string `json:"host"`
		Port int    `json:"port"`
		Ref  int    `json:"ref"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(asked[0].arguments), &sent); err != nil {
		test.Fatal(err)
	}
	if sent.Host != "shop.example.test" || sent.Port != 443 || sent.Ref != 6 || (sent.Code != before && sent.Code != after) {
		test.Fatalf("the extension was sent %+v, wanted the code %s or %s", sent, before, after)
	}
	if strings.Contains(result.Content, sent.Code) {
		test.Fatalf("the code is in the answer: %q", result.Content)
	}
	if hosts := found.usedOn(test, found.grantedLogin); len(hosts) != 1 || hosts[0] != "shop.example.test" {
		test.Fatalf("the use was recorded for %v", hosts)
	}
	// A login with no code saved has nothing to fill.
	mustRefuse(test, run, fmt.Sprintf(`{"action":"fill_one_time_code","item_id":%q,"ref":6}`, found.noPasskeyLogin), "no one-time code")
}

func currentCode(test *testing.T) string {
	test.Helper()
	oneTimeCode, err := passwords.ParseOneTimeCodeURI("otpauth://totp/Example:ada?secret=" + inventedOneTimeSecret)
	if err != nil {
		test.Fatal(err)
	}
	code, err := oneTimeCode.CodeAt(time.Now())
	if err != nil {
		test.Fatal(err)
	}
	return code
}

// Whatever the extension says, no value comes back to the model.
func TestSignInDoesNotPassOnAValueTheExtensionRepeats(test *testing.T) {
	found := newWorld(test)
	run := found.tabRun(shopAddress)
	run.tab.failure = "could not type " + inventedPassword + " for " + inventedUserName
	_, err := browse(run, fillLogin(found.grantedLogin))
	if err == nil {
		test.Fatal("a refusal was taken for a fill")
	}
	assertNoSecret(test, err.Error())
	if strings.Contains(err.Error(), inventedUserName) || !strings.Contains(err.Error(), "[hidden]") {
		test.Fatalf("the error: %q", err)
	}
}

// In the headless browser each value is put into the field the ref names by one
// script that also checks the page's host and port and the kind of field; the
// answer carries no value.
func TestSignInInTheHeadlessBrowserFillsTheFields(test *testing.T) {
	found := newWorld(test)
	run, chrome := found.headlessRun(test, "https://shop.example.test/sign-in")
	result, err := browse(run, fillLogin(found.grantedLogin))
	if err != nil {
		test.Fatal(err)
	}
	if result.Content != "Filled the user name and password for shop.example.test from Example shop" {
		test.Fatalf("the answer: %q", result.Content)
	}
	assertNoSecret(test, result.Content)
	fills := chrome.fills(test)
	wanted := []fillCall{
		{Host: "shop.example.test", Port: 443, Ref: 3, Kind: "userName", Text: inventedUserName, IsSecret: false},
		{Host: "shop.example.test", Port: 443, Ref: 4, Kind: "password", Text: inventedPassword, IsSecret: true},
	}
	if fmt.Sprint(fills) != fmt.Sprint(wanted) {
		test.Fatalf("filled %v, wanted %v", fills, wanted)
	}
	// A value is never typed by a command of its own.
	if len(chrome.sent("Input.insertText")) != 0 || len(chrome.sent("Input.dispatchKeyEvent")) != 0 {
		test.Fatal("a value was typed")
	}
	if hosts := found.usedOn(test, found.grantedLogin); len(hosts) != 1 || hosts[0] != "shop.example.test" {
		test.Fatalf("the use was recorded for %v", hosts)
	}

	before := currentCode(test)
	result, err = browse(run, fmt.Sprintf(`{"action":"fill_one_time_code","item_id":%q,"ref":6}`, found.grantedLogin))
	after := currentCode(test)
	if err != nil || result.Content != "Filled the one-time code for shop.example.test from Example shop" {
		test.Fatalf("a code: %v %v", result, err)
	}
	fills = chrome.fills(test)
	if code := fills[len(fills)-1]; code.Kind != "oneTimeCode" || !code.IsSecret || code.Ref != 6 || (code.Text != before && code.Text != after) {
		test.Fatalf("filled %+v", code)
	}
	assertNoSecret(test, result.Content)
}

// A passkey is set up for the page's relying party only, and taken away after
// the next navigation.
func TestPasskeySignInSetsUpAVirtualAuthenticatorForTheRelyingParty(test *testing.T) {
	found := newWorld(test)
	run, chrome := found.headlessRun(test, "https://login.example.test/")
	// The login's sites are shop.example.test, but a passkey is bound to its
	// relying party, example.test, which this page is under.
	result, err := browse(run, fmt.Sprintf(`{"action":"passkey_sign_in","item_id":%q,"target":"headless"}`, found.grantedLogin))
	if err != nil {
		test.Fatal(err)
	}
	assertNoSecret(test, result.Content)
	if !strings.Contains(result.Content, "Ready to sign in to login.example.test with the passkey from Example shop") {
		test.Fatalf("the answer: %q", result.Content)
	}
	if enabled := chrome.sent("WebAuthn.enable"); len(enabled) != 1 {
		test.Fatalf("WebAuthn.enable %v", enabled)
	}
	options := chrome.sent("WebAuthn.addVirtualAuthenticator")
	if len(options) != 1 {
		test.Fatalf("authenticators %v", options)
	}
	var addedOptions struct {
		Options map[string]any `json:"options"`
	}
	if err := json.Unmarshal([]byte(options[0]), &addedOptions); err != nil {
		test.Fatal(err)
	}
	wantedOptions := map[string]any{"protocol": "ctap2", "transport": "internal", "hasResidentKey": true, "hasUserVerification": true, "isUserVerified": true, "automaticPresenceSimulation": true}
	if fmt.Sprint(addedOptions.Options) != fmt.Sprint(wantedOptions) {
		test.Fatalf("the authenticator: %v", addedOptions.Options)
	}
	credentials := chrome.sent("WebAuthn.addCredential")
	if len(credentials) != 1 {
		test.Fatalf("credentials %v", credentials)
	}
	var added struct {
		AuthenticatorID string `json:"authenticatorId"`
		Credential      struct {
			CredentialID         string `json:"credentialId"`
			IsResidentCredential bool   `json:"isResidentCredential"`
			RPID                 string `json:"rpId"`
			PrivateKey           string `json:"privateKey"`
			UserHandle           string `json:"userHandle"`
			SignCount            int    `json:"signCount"`
		} `json:"credential"`
	}
	if err := json.Unmarshal([]byte(credentials[0]), &added); err != nil {
		test.Fatal(err)
	}
	encode := base64.StdEncoding.EncodeToString
	if added.AuthenticatorID != "authenticator-1" || added.Credential.RPID != "example.test" || !added.Credential.IsResidentCredential || added.Credential.SignCount != 0 ||
		added.Credential.CredentialID != encode(found.passkeyCredential) || added.Credential.PrivateKey != encode(found.passkeyPrivateKey) || added.Credential.UserHandle != encode(found.passkeyUserHandle) {
		test.Fatalf("the credential: %+v", added)
	}
	if hosts := found.usedOn(test, found.grantedLogin); len(hosts) != 1 || hosts[0] != "login.example.test" {
		test.Fatalf("the use was recorded for %v", hosts)
	}
	if removed := chrome.sent("WebAuthn.removeVirtualAuthenticator"); len(removed) != 0 {
		test.Fatalf("removed before the page navigated: %v", removed)
	}
	if !run.page.HasPasskey() {
		test.Fatal("the page holds no passkey")
	}

	// A navigation of a frame inside the page does not take it away; the
	// page's own does.
	chrome.emit("Page.frameNavigated", `{"frame":{"id":"inner","parentId":"main"}}`)
	chrome.emit("Page.frameNavigated", `{"frame":{"id":"main"}}`)
	deadline := time.Now().Add(5 * time.Second)
	for len(chrome.sent("WebAuthn.removeVirtualAuthenticator")) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	removed := chrome.sent("WebAuthn.removeVirtualAuthenticator")
	if len(removed) != 1 || !strings.Contains(removed[0], "authenticator-1") || run.page.HasPasskey() {
		test.Fatalf("after the navigation: removed %v", removed)
	}
}

// A passkey for another site, a login with no passkey and a page that is not
// under the relying party are refused.
func TestPasskeySignInRefusesAWrongRelyingPartyAndLoginsWithoutPasskeys(test *testing.T) {
	found := newWorld(test)
	run, chrome := found.headlessRun(test, "https://shop.example.org/")
	mustRefuse(test, run, fmt.Sprintf(`{"action":"passkey_sign_in","item_id":%q,"target":"headless"}`, found.grantedLogin), "no passkey for shop.example.org")
	mustRefuse(test, run, fmt.Sprintf(`{"action":"passkey_sign_in","item_id":%q,"target":"headless"}`, found.noPasskeyLogin), "no passkey saved")
	mustRefuse(test, run, fmt.Sprintf(`{"action":"passkey_sign_in","item_id":%q,"target":"headless"}`, found.ungrantedLogin), "Let the agent use this")
	look, lookChrome := found.headlessRun(test, "https://example.test.evil.test/")
	mustRefuse(test, look, fmt.Sprintf(`{"action":"passkey_sign_in","item_id":%q,"target":"headless"}`, found.grantedLogin), "no passkey")
	for _, seen := range []*fakeChrome{chrome, lookChrome} {
		if len(seen.sent("WebAuthn.addVirtualAuthenticator")) != 0 || len(seen.sent("WebAuthn.addCredential")) != 0 {
			test.Fatal("an authenticator was set up")
		}
	}
	if hosts := found.usedOn(test, found.grantedLogin); len(hosts) != 0 {
		test.Fatalf("a use was recorded for a refused passkey: %v", hosts)
	}
}

// The preview and the risk of the three are those of a write.
func TestSignInActionsAreWritesInTheToolDefinition(test *testing.T) {
	browserTool := tools.Build().Get("browser")
	if browserTool == nil {
		test.Fatal("no browser tool")
	}
	for _, action := range []string{"fill_login", "fill_one_time_code", "passkey_sign_in"} {
		arguments := json.RawMessage(fmt.Sprintf(`{"action":%q}`, action))
		if risk := browserTool.RiskOf(arguments); risk != tools.RiskWrite {
			test.Errorf("%s has risk %v", action, risk)
		}
		if browserTool.Preview(arguments) == "Browser: "+action {
			test.Errorf("%s has no preview of its own", action)
		}
	}
	steps := json.RawMessage(`{"action":"steps","steps":[{"action":"fill_login"}]}`)
	if risk := browserTool.RiskOf(steps); risk != tools.RiskWrite {
		test.Errorf("steps with a fill have risk %v", risk)
	}
}

// After a password is filled into the person's tab, scripts, pictures and raw
// DevTools commands are refused on that tab until it commits a new document;
// other actions still work.
func TestAFilledTabRefusesScriptsUntilItCommitsANewDocument(test *testing.T) {
	found := newWorld(test)
	run := found.tabRun(shopAddress)
	run.tab.tabID = "7"
	run.tab.fillAnswer = `{"isFilled":true,"tab":7,"documentId":"doc-1","commitCount":3}`
	run.tab.answer = `{"result":"fine"}`
	evaluate := `{"action":"evaluate","expression":"1+1"}`
	cdp := `{"action":"cdp","method":"Runtime.evaluate","params":{"expression":"1"}}`

	// Before any fill, scripts run; the page they ran in is then not filled
	// (see TestAScriptedPageIsNotFilled), until the tab commits a new document.
	if _, err := browse(run, evaluate); err != nil {
		test.Fatalf("a script before a fill: %s", err)
	}
	mustRefuse(test, run, fillLogin(found.grantedLogin), devtools.ScriptedPageWords)
	run.tab.Filled().Committed("7", devtools.Commit{DocumentID: "doc-0", Address: shopAddress, Count: 2})
	if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	asked := len(run.tab.actions())
	mustRefuse(test, run, evaluate, lockedWords)
	mustRefuse(test, run, cdp, lockedWords)
	// A step reports its refusal in the answer, which stops the steps.
	stepped, err := browse(run, `{"action":"steps","steps":[{"action":"evaluate","expression":"document.title"}]}`)
	if err != nil || !strings.Contains(stepped.Content, lockedWords) {
		test.Fatalf("a script in steps: %v %v", stepped, err)
	}
	if len(run.tab.actions()) != asked {
		test.Fatalf("the tab was asked while locked: %v", run.tab.actions()[asked:])
	}
	// Reading and acting in other ways still work.
	for _, arguments := range []string{`{"action":"snapshot"}`, `{"action":"click","ref":3}`} {
		if _, err := browse(run, arguments); err != nil {
			test.Fatalf("%s on a locked tab: %s", arguments, err)
		}
	}
	// A change of the address that keeps the document (a fragment, pushState)
	// is not a new document.
	run.tab.address = shopAddress + "#password"
	mustRefuse(test, run, evaluate, lockedWords)
	run.tab.address = "https://shop.example.test/pushed"
	mustRefuse(test, run, evaluate, lockedWords)
	// Going to a page with the tool, back, or to another tab is not the tab
	// committing a new document: only the browser saying so is.
	for _, arguments := range []string{`{"action":"navigate","url":"https://shop.example.test/"}`, `{"action":"back"}`} {
		if _, err := browse(run, arguments); err != nil {
			test.Fatal(err)
		}
		mustRefuse(test, run, evaluate, lockedWords)
	}
	// The browser says the tab committed a new document: unlocked.
	run.tab.Filled().Committed("7", devtools.Commit{DocumentID: "doc-2", Address: "https://shop.example.test/account", Count: 4})
	if _, err := browse(run, evaluate); err != nil {
		test.Fatalf("a script after the tab committed a new document: %s", err)
	}

	// A one-time code locks the tab too (on a document no script ran in).
	run.tab.Filled().Committed("7", devtools.Commit{DocumentID: "doc-3", Address: "https://shop.example.test/verify", Count: 5})
	run.tab.fillAnswer = `{"isFilled":true,"tab":7,"documentId":"doc-3","commitCount":5}`
	if _, err := browse(run, fmt.Sprintf(`{"action":"fill_one_time_code","item_id":%q,"ref":6}`, found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	mustRefuse(test, run, evaluate, lockedWords)
	// A fill of the user name alone holds no secret and locks nothing.
	other := found.tabRun(shopAddress)
	other.tab.answer = `{"result":"fine"}`
	if _, err := browse(other, fmt.Sprintf(`{"action":"fill_login","item_id":%q,"user_name_ref":3}`, found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	if _, err := browse(other, evaluate); err != nil {
		test.Fatalf("a script after only a user name: %s", err)
	}
}

// lockedWords is what the lock answers.
const lockedWords = "are not run on a page TeaNode filled a password or code into, until that tab loads a new page"

// For the life of the tab every answer and error has the filled values
// replaced, also after the tab moved on.
func TestAFilledTabHidesTheValuesInEveryAnswer(test *testing.T) {
	found := newWorld(test)
	run := found.tabRun(shopAddress)
	if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	before := currentCode(test)
	if _, err := browse(run, fmt.Sprintf(`{"action":"fill_one_time_code","item_id":%q,"ref":6}`, found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	after := currentCode(test)

	run.tab.answer = fmt.Sprintf(`{"url":"https://shop.example.test/","text":"[ref=4] input(password) \"secret\" value=\"Tr0ub4dor\u00263-example\" and %s %s Tr0ub4dor&3-example"}`, before, after)
	result, err := browse(run, `{"action":"snapshot"}`)
	if err != nil {
		test.Fatal(err)
	}
	assertNoSecret(test, result.Content)
	if strings.Contains(result.Content, before) || strings.Contains(result.Content, after) || strings.Count(result.Content, "[hidden]") < 3 {
		test.Fatalf("the snapshot: %s", result.Content)
	}
	// The note too, and an error from the tab.
	run.tab.failure = "the page said " + inventedPassword
	_, err = browse(run, `{"action":"click","ref":4}`)
	if err == nil || strings.Contains(err.Error(), inventedPassword) || !strings.Contains(err.Error(), "[hidden]") {
		test.Fatalf("the error: %v", err)
	}

	// After the tab moved on and scripts run again, the values are still hidden.
	run.tab.failure = ""
	run.tab.address = "https://shop.example.test/account"
	run.tab.Filled().Committed("", devtools.Commit{DocumentID: "doc-2", Address: "https://shop.example.test/account", Count: 1})
	run.tab.answer = fmt.Sprintf(`{"value":%q}`, inventedPassword)
	result, err = browse(run, `{"action":"evaluate","expression":"document.title"}`)
	if err != nil {
		test.Fatal(err)
	}
	assertNoSecret(test, result.Content)
	if !strings.Contains(result.Content, "[hidden]") {
		test.Fatalf("after moving on: %s", result.Content)
	}
}

// In the headless browser too: scripts and pictures are refused until the main
// frame commits a new document, and a script's result is scrubbed.
func TestAFilledHeadlessPageRefusesScriptsUntilItNavigatesAndHidesValues(test *testing.T) {
	found := newWorld(test)
	run, chrome := found.headlessRun(test, shopAddress)
	evaluate := `{"action":"evaluate","expression":"document.querySelector('input').value"}`
	chrome.evaluated = "the field holds " + inventedPassword
	if _, err := browse(run, evaluate); err != nil {
		test.Fatalf("a script before a fill: %s", err)
	}
	// The site the script ran on is not filled, in any document; a fresh
	// browser is.
	mustRefuse(test, run, fillLogin(found.grantedLogin), devtools.ScriptedSiteWords)
	chrome.mutex.Lock()
	chrome.loader = "loader-1b"
	chrome.mutex.Unlock()
	mustRefuse(test, run, fillLogin(found.grantedLogin), devtools.ScriptedSiteWords)
	if _, err := browse(run, `{"action":"reset"}`); err != nil {
		test.Fatal(err)
	}
	if _, err := browse(run, fillLogin(found.grantedLogin)); err != nil {
		test.Fatal(err)
	}
	asked := len(chrome.sent("Runtime.evaluate"))
	mustRefuse(test, run, evaluate, lockedWords)
	mustRefuse(test, run, `{"action":"screenshot"}`, lockedWords)
	if len(chrome.sent("Runtime.evaluate")) != asked || len(chrome.sent("Page.captureScreenshot")) != 0 {
		test.Fatal("the page ran a script or was photographed while locked")
	}
	// A frame inside the page navigating does not unlock it.
	chrome.emit("Page.frameNavigated", `{"frame":{"id":"inner","parentId":"main","loaderId":"loader-frame"}}`)
	time.Sleep(100 * time.Millisecond)
	mustRefuse(test, run, evaluate, lockedWords)
	chrome.mutex.Lock()
	chrome.loader = "loader-2"
	chrome.mutex.Unlock()
	chrome.emit("Page.frameNavigated", `{"frame":{"id":"main","loaderId":"loader-2","url":"https://shop.example.test/account"}}`)
	deadline := time.Now().Add(5 * time.Second)
	for run.page.Filled().IsLocked("") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	result, err := browse(run, evaluate)
	if err != nil {
		test.Fatalf("a script after the page navigated: %s", err)
	}
	assertNoSecret(test, result.Content)
	if !strings.Contains(result.Content, "[hidden]") {
		test.Fatalf("the script's result: %s", result.Content)
	}
}
