package agent

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
	"github.com/ziyan/teanode/internal/storage"
)

// skillShareFixture is two people who share a circle: ada, who owns the
// payments skill's account and has a computer attached, and ben, whose agent
// is offered what she shares. The skill is the fictional one the skills
// package tests with, which runs commands; the computer answers each one from
// a script and keeps what it was asked. The judge of what a command would do is
// a model that says whatever the test sets.
type skillShareFixture struct {
	database db.Database
	worker   *Agent
	relay    *fakeRelay

	ada, ben           *models.User
	agentUsers         *models.Group
	adaAgent, benAgent *models.Agent
	adaKey, benKey     []byte
	circle             *models.Circle
	computer           *fakeComputer
	benConversation    *models.AgentConversation

	mutex    sync.Mutex
	commands []string
	// replies are what the computer prints for a command containing the key.
	replies map[string]string

	judged atomic.Value
}

func newSkillShareFixture(t *testing.T) *skillShareFixture {
	t.Helper()
	content, err := os.ReadFile("../skills/testdata/payments.md")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	fixture := &skillShareFixture{relay: &fakeRelay{}, replies: map[string]string{}}
	fixture.judged.Store(callRiskMoney)

	model := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		answer, _ := json.Marshal(map[string]string{"callRisk": fixture.judged.Load().(string), "riskReason": "a test says so"})
		content, _ := json.Marshal(string(answer))
		if !strings.Contains(string(raw), `"stream":true`) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(writer, `{"id":"j1","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`, content)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(writer, "data: {\"id\":\"j1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}]}\n\n", content)
		_, _ = writer.Write([]byte("data: {\"id\":\"j1\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\ndata: [DONE]\n\n"))
	}))
	t.Cleanup(model.Close)

	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	fixture.database = database
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Server.Secret = "a secret for sealing in this test"
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:judge"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fixture.worker = New(&Settings{
		Database: database, Storage: store, Registry: registry, Instance: "test", Tick: time.Hour,
		Configuration: func() *config.Configuration { return configuration },
	})
	fixture.worker.phones.override = fixture.relay

	sealKey := func() ([]byte, string) {
		key := make([]byte, push.PhoneKeyBytes)
		_, _ = rand.Read(key)
		sealed, err := fixture.worker.SealSecret(base64.StdEncoding.EncodeToString(key))
		if err != nil {
			t.Fatalf("SealSecret: %s", err)
		}
		return key, sealed
	}
	var adaSealed, benSealed string
	fixture.adaKey, adaSealed = sealKey()
	fixture.benKey, benSealed = sealKey()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if fixture.ada, err = tx.CreateUser(&models.User{Username: "ada.l", Name: "Ada"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if fixture.ben, err = tx.CreateUser(&models.User{Username: "ben", Name: "Ben"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		// The owner may use agents, which the shared tools run as.
		role, err := tx.CreateRole(&models.Role{Name: "agent users", Permissions: []models.Permission{models.PermissionAgentUse}})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		if fixture.agentUsers, err = tx.CreateGroup(&models.Group{Name: "agent users", UserIDs: []string{fixture.ada.ID}, RoleIDs: []string{role.ID}}); err != nil {
			t.Fatalf("CreateGroup: %s", err)
		}
		if fixture.adaAgent, err = tx.CreateAgent(&models.Agent{UserID: fixture.ada.ID, Enabled: true, Name: "Tea"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if fixture.benAgent, err = tx.CreateAgent(&models.Agent{UserID: fixture.ben.ID, Enabled: true, Name: "Leaf"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		for name, phone := range map[string]struct{ agentId, sealed string }{
			"ada": {fixture.adaAgent.ID, adaSealed}, "ben": {fixture.benAgent.ID, benSealed},
		} {
			if _, err := tx.PutAgentPhone(&models.AgentPhone{
				AgentID: phone.agentId, ClientID: "phone-" + name, PhoneName: "A test phone",
				PushRegistration: "sealed:" + name, PayloadKey: phone.sealed,
			}); err != nil {
				t.Fatalf("PutAgentPhone: %s", err)
			}
		}
		if fixture.circle, err = tx.CreateCircle(&models.Circle{Name: "Home", CircleKind: models.CircleKindFamily}, fixture.ada.ID); err != nil {
			t.Fatalf("CreateCircle: %s", err)
		}
		if _, err = tx.AddCircleMember(fixture.circle.ID, fixture.ben.ID, models.MemberRoleMember, fixture.ada.ID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
		if _, err = tx.PutAgentSkill(&models.AgentSkill{Name: "payments", Version: "1", Description: "A fictional payment program", Content: string(content), Enabled: true}); err != nil {
			t.Fatalf("PutAgentSkill: %s", err)
		}
		if fixture.benConversation, err = tx.MainAgentConversation(fixture.benAgent.ID, "drawer"); err != nil {
			t.Fatalf("MainAgentConversation: %s", err)
		}
	})
	// Each phone is signed in: its client holds a token.
	for name, user := range map[string]*models.User{"ada": fixture.ada, "ben": fixture.ben} {
		if _, err := database.CreateAuthorizedToken(&models.Token{
			ID: "token-" + name, UserID: user.ID, Name: "A test phone", ClientID: "phone-" + name, ExpiresAt: time.Now().Add(time.Hour),
		}, "hash-"+name, "refresh-"+name); err != nil {
			t.Fatalf("CreateAuthorizedToken: %s", err)
		}
	}
	fixture.attachComputer()
	return fixture
}

// attachComputer attaches ada's computer, which answers shell commands.
func (self *skillShareFixture) attachComputer() {
	self.computer = &fakeComputer{agent: self.worker, agentId: self.adaAgent.ID, answers: func(action string, arguments json.RawMessage) (bool, string) {
		var shell struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(arguments, &shell)
		self.mutex.Lock()
		defer self.mutex.Unlock()
		self.commands = append(self.commands, shell.Command)
		printed := `{"status":"ok"}`
		for part, reply := range self.replies {
			if strings.Contains(shell.Command, part) {
				printed = reply
			}
		}
		encoded, _ := json.Marshal(map[string]any{"stdout": printed, "stderr": "", "exitCode": 0})
		return true, string(encoded)
	}}
	self.worker.AttachComputer(self.adaAgent.ID, self.computer, ComputerIdentity{Name: "laptop", System: "linux", Home: "/home/example"})
}

func (self *skillShareFixture) ranCommands() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]string(nil), self.commands...)
}

// share makes ada's share of the tools with ben.
func (self *skillShareFixture) share(t *testing.T, share *models.SkillShare) *models.SkillShare {
	t.Helper()
	share.SkillName, share.GranteeUserID = "payments", self.ben.ID
	var saved *models.SkillShare
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if saved, err = tx.ShareSkill(self.ada.ID, share); err != nil {
			t.Fatalf("ShareSkill: %s", err)
		}
	})
	return saved
}

func dollars(minor int64) *int64 { return &minor }

// benCalls calls one of ben's shared tools the way a turn of his agent does.
func (self *skillShareFixture) benCalls(t *testing.T, share *models.SkillShare, toolName string, arguments map[string]any) (map[string]any, error) {
	t.Helper()
	found := self.benTool(t, toolName)
	if found == nil {
		t.Fatalf("ben is not offered a tool for %s", toolName)
	}
	arguments["request_reason"] = "the weekly groceries"
	encoded, _ := json.Marshal(arguments)
	run := &directRun{agent: self.worker, owner: self.ben, agentModel: self.benAgent}
	ctx := tools.WithRun(context.Background(), &conversationRun{directRun: run, conversation: self.benConversation})
	result, err := found.Run(ctx, &tools.Call{ID: "c1", Arguments: encoded, Confirmed: true})
	if err != nil {
		return nil, err
	}
	if !result.Untrusted {
		t.Error("what a shared call answers is data from outside, and was not marked so")
	}
	var answered map[string]any
	if err := json.Unmarshal([]byte(result.Content), &answered); err != nil {
		t.Fatalf("the answer is not JSON: %s", result.Content)
	}
	return answered, nil
}

// conversationRun is a direct run that is part of a conversation, which is how a
// call knows where to tell the outcome.
type conversationRun struct {
	*directRun
	conversation *models.AgentConversation
}

func (self *conversationRun) Conversation() *models.AgentConversation { return self.conversation }

func (self *skillShareFixture) benTool(t *testing.T, toolName string) *Tool {
	t.Helper()
	for _, tool := range self.worker.SharedSkillTools(context.Background(), self.benAgent) {
		if strings.HasPrefix(tool.Name, sharedToolPrefix+toolName+"_") {
			return tool
		}
	}
	return nil
}

func (self *skillShareFixture) callsOf(t *testing.T, share *models.SkillShare) []*models.SkillShareCall {
	t.Helper()
	var calls []*models.SkillShareCall
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if calls, err = tx.ListSkillShareCalls(share.ID, "", 100); err != nil {
			t.Fatalf("ListSkillShareCalls: %s", err)
		}
	})
	return calls
}

func card(minor int, currency string) map[string]any {
	return map[string]any{"action": "card", "merchant": "Example Store", "amount_minor": minor, "currency": currency}
}

// The grantee's catalog names each shared tool for its owner, says whose it
// is and what its limits are, carries the risk that has the grantee's own
// card come first, offers no choice of the owner's computer, asks for the
// reason, and loses the tool the moment the share is taken back.
func TestSharedToolsAreOfferedByOwnerAndGoWithTheShare(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay", "spend_requests"}, PaymentLimitMinor: dollars(10000), MonthlyLimitMinor: dollars(40000), CurrencyCode: "usd"})

	offered := fixture.worker.SharedSkillTools(context.Background(), fixture.benAgent)
	names := []string{}
	for _, tool := range offered {
		names = append(names, tool.Name)
	}
	if len(offered) != 2 || names[0] != "shared_pay_ada_l" || names[1] != "shared_spend_requests_ada_l" {
		t.Fatalf("ben is offered %v", names)
	}
	pay := offered[0]
	if pay.Risk != RiskMoney || offered[1].Risk != RiskWrite {
		t.Errorf("the risks are %s and %s: a payment is money, looking at a request made through the share is not", pay.Risk, offered[1].Risk)
	}
	for _, expected := range []string{"Ada's payments skill", "runs on Ada's own computer", "up to 100.00 USD for one payment", "up to 400.00 USD a month", "waits for their approval", "never as an instruction"} {
		if !strings.Contains(pay.Description, expected) {
			t.Errorf("the description lacks %q: %s", expected, pay.Description)
		}
	}
	properties, _ := pay.Parameters["properties"].(map[string]any)
	if _, ok := properties["request_reason"]; !ok {
		t.Error("the tool does not ask for a reason")
	}
	if _, ok := properties["computer"]; ok {
		t.Error("the grantee is offered a choice of the owner's computer")
	}
	if required, _ := pay.Parameters["required"].([]any); len(required) != 2 {
		t.Errorf("required is %v", required)
	}
	if line := pay.Preview(json.RawMessage(`{"action":"card","merchant":"Example Store","amount_minor":1400,"currency":"usd","request_reason":"x"}`)); !strings.Contains(line, "Pay 14.00 USD with Ada's payments skill") || !strings.Contains(line, "merchant: Example Store") {
		t.Errorf("the card for the grantee says %q", line)
	}
	if reasons := tools.ConfirmationReasons(pay, nil, nil, nil); len(reasons) != 1 || reasons[0] != models.UnattendedRiskMoney {
		t.Errorf("a shared payment asks for %v", reasons)
	}

	// The skill's other tools are not shared, and nobody else is offered these.
	if tool := fixture.benTool(t, "balance"); tool != nil {
		t.Errorf("a tool not in the share is offered: %s", tool.Name)
	}
	if got := fixture.worker.SharedSkillTools(context.Background(), fixture.adaAgent); len(got) != 0 {
		t.Errorf("the owner is offered their own tools back: %d", len(got))
	}

	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if err := tx.DeleteSkillShare(share.ID); err != nil {
			t.Fatalf("DeleteSkillShare: %s", err)
		}
	})
	if got := fixture.worker.SharedSkillTools(context.Background(), fixture.benAgent); len(got) != 0 {
		t.Errorf("ben is still offered %d tools after the share was taken back", len(got))
	}
}

// A name that is not safe for a tool is made safe, kept short, and kept apart.
func TestSharedToolNamesAreSafeShortAndDistinct(t *testing.T) {
	taken := map[string]bool{}
	first := sharedToolName("pay", "Ada.L-Name", taken)
	taken[first] = true
	second := sharedToolName("pay", "ada_l name", taken)
	if first != "shared_pay_ada_l_name" || second != "shared_pay_ada_l_name_2" {
		t.Errorf("the names are %q and %q", first, second)
	}
	long := sharedToolName("spend_requests", strings.Repeat("abcdefghij", 10), map[string]bool{})
	if len(long) > sharedToolNameLongest || !strings.HasPrefix(long, "shared_spend_requests_abc") {
		t.Errorf("a long name is %q (%d)", long, len(long))
	}
	if name := sharedToolName("pay", "!!!", map[string]bool{}); name != "shared_pay_someone" {
		t.Errorf("a username with nothing safe in it is %q", name)
	}
}

// Within the limits a call runs on the owner's computer through the owner's
// own runner, is recorded as ran with what it paid, and what it printed comes
// back to the grantee's agent. Nothing is waiting for the owner.
func TestAPaymentWithinTheLimitsRunsOnTheOwnersComputer(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(10000), MonthlyLimitMinor: dollars(40000), CurrencyCode: "usd"})
	fixture.replies["paycli"] = `{"id":"req_0001abcd","state":"pending"}`

	answered, err := fixture.benCalls(t, share, "pay", card(2500, "USD"))
	if err != nil {
		t.Fatalf("the call: %s", err)
	}
	if answered["callState"] != "ran" || !strings.Contains(fmt.Sprint(answered["answer"]), "req_0001abcd") {
		t.Errorf("the grantee's agent was told %v", answered)
	}
	commands := fixture.ranCommands()
	if len(commands) != 1 || !strings.Contains(commands[0], "paycli") || !strings.Contains(commands[0], "Example Store") || !strings.Contains(commands[0], "2500") {
		t.Fatalf("the owner's computer ran %v", commands)
	}
	calls := fixture.callsOf(t, share)
	if len(calls) != 1 || calls[0].CallState != models.SkillShareCallRan || calls[0].AmountMinor == nil || *calls[0].AmountMinor != 2500 ||
		calls[0].CurrencyCode != "usd" || calls[0].RequestReason != "the weekly groceries" || calls[0].GranteeConversationID != fixture.benConversation.ID {
		t.Fatalf("the record is %+v", calls)
	}
	if calls[0].ToolArguments["merchant"] != "Example Store" || calls[0].ToolArguments["request_reason"] != nil {
		t.Errorf("the arguments kept are %v", calls[0].ToolArguments)
	}
	// Nothing was put to the owner.
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		main, _ := tx.MainAgentConversation(fixture.adaAgent.ID, "drawer")
		messages, _ := tx.ListAgentMessages(main.ID, nil)
		if len(messages) != 0 {
			t.Errorf("the owner was written to: %+v", messages)
		}
	})
	fixture.worker.waitGroup.Wait()
	if len(fixture.relay.to("sealed:ada")) != 0 {
		t.Error("the owner's phone was told of a call that needed nothing of her")
	}
}

// The grantee cannot choose the owner's computer: an argument naming one is
// dropped before the runner sees it.
func TestTheGranteeCannotNameTheOwnersComputer(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(10000), CurrencyCode: "usd"})
	arguments := card(2500, "usd")
	arguments["computer"] = "a-computer-that-is-not-hers"
	if _, err := fixture.benCalls(t, share, "pay", arguments); err != nil {
		t.Fatalf("the call: %s", err)
	}
	if len(fixture.ranCommands()) != 1 {
		t.Error("a call that named a computer did not run on the owner's")
	}
	if calls := fixture.callsOf(t, share); calls[0].ToolArguments["computer"] != nil {
		t.Errorf("the computer is kept in the arguments: %v", calls[0].ToolArguments)
	}
}

// Above the limit for one payment, with no amount to read, and on a share that
// asks every time, a call waits: nothing runs, the call is stored as it was
// asked, the owner has a card in her main conversation and a notification on
// her phone that only opens the app, and the grantee's agent is told to stop
// and wait.
func TestACallThatNeedsTheOwnerWaitsForHer(t *testing.T) {
	for name, test := range map[string]struct {
		share     *models.SkillShare
		tool      string
		arguments map[string]any
		reason    string
	}{
		"above the payment limit": {&models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(10000), CurrencyCode: "usd"}, "pay", card(14000, "usd"), "one payment"},
		"an address with no price": {&models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(10000), CurrencyCode: "usd"}, "pay",
			map[string]any{"action": "url", "address": "https://example.com/pay"}, "cannot be read"},
		"asking every time": {&models.SkillShare{ToolNames: []string{"pay"}, IsApprovalRequired: true}, "pay", card(100, "usd"), "every call"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newSkillShareFixture(t)
			share := fixture.share(t, test.share)
			answered, err := fixture.benCalls(t, share, test.tool, test.arguments)
			if err != nil {
				t.Fatalf("the call: %s", err)
			}
			if answered["callState"] != "waiting_for_approval" || !strings.Contains(fmt.Sprint(answered["message"]), test.reason) || !strings.Contains(fmt.Sprint(answered["message"]), "Ada") {
				t.Errorf("the grantee's agent was told %v", answered)
			}
			if len(fixture.ranCommands()) != 0 {
				t.Errorf("a call that waits ran %v", fixture.ranCommands())
			}
			calls := fixture.callsOf(t, share)
			if len(calls) != 1 || calls[0].CallState != models.SkillShareCallWaitingForApproval || calls[0].ExpiresAt == nil ||
				calls[0].ExpiresAt.Before(time.Now().Add(23*time.Hour)) || calls[0].ToolArguments["merchant"] != test.arguments["merchant"] {
				t.Fatalf("the stored call is %+v", calls)
			}
			dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
				main, _ := tx.MainAgentConversation(fixture.adaAgent.ID, "drawer")
				messages, _ := tx.ListAgentMessages(main.ID, nil)
				if len(messages) != 1 || messages[0].Role != models.AgentMessageNote || messages[0].Name != string(models.NotePaymentWaiting) {
					t.Fatalf("the owner's conversation holds %+v", messages)
				}
				note := models.ParseSkillShareCallNote(messages[0].Content)
				if note == nil || note.CallID != calls[0].ID || note.SkillShareID != share.ID || note.GranteeName != "Ben" {
					t.Errorf("the note names %+v", note)
				}
			})
			fixture.worker.waitGroup.Wait()
			sent := fixture.relay.to("sealed:ada")
			if len(sent) != 1 {
				t.Fatalf("the owner's phone was sent %d notifications", len(sent))
			}
			told, err := push.DecryptContent(fixture.adaKey, sent[0].Sealed)
			if err != nil {
				t.Fatal(err)
			}
			if told.NotificationKind != models.NotificationPayment || told.CallID != calls[0].ID || len(told.Buttons) != 1 || told.Buttons[0].ButtonAction != "open" {
				t.Errorf("the phone was told %+v: it must only open the app", told)
			}
			if strings.Contains(told.Body, "Example") || strings.Contains(told.Body, "140") {
				t.Errorf("a lock screen says %q", told.Body)
			}
		})
	}
}

// The month is counted from the calls that ran and the calls that failed: two
// payments inside the month's limit run, the third that would pass it waits, a
// call that still waits paid nothing, and one that failed counts, since a
// failure after the command acted cannot be told from one before it.
func TestTheMonthsLimitCountsRanAndFailedCalls(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(20000), MonthlyLimitMinor: dollars(40000), CurrencyCode: "usd"})
	for index, expected := range []string{"ran", "ran", "waiting_for_approval"} {
		answered, err := fixture.benCalls(t, share, "pay", card(15000, "usd"))
		if err != nil {
			t.Fatalf("call %d: %s", index, err)
		}
		if answered["callState"] != expected {
			t.Errorf("call %d came to %v, want %s", index, answered["callState"], expected)
		}
	}
	// A payment that did not complete still counts toward the month.
	fixture.worker.DetachComputer(fixture.adaAgent.ID, fixture.computer)
	fixture.computer = &fakeComputer{agent: fixture.worker, agentId: fixture.adaAgent.ID, answers: func(string, json.RawMessage) (bool, string) {
		return false, "the program refused"
	}}
	fixture.worker.AttachComputer(fixture.adaAgent.ID, fixture.computer, ComputerIdentity{Name: "laptop", System: "linux", Home: "/home/example"})
	answered, err := fixture.benCalls(t, share, "pay", card(5000, "usd"))
	if err != nil || answered["callState"] != "failed" {
		t.Fatalf("a refusal on the computer: %v %v", answered, err)
	}
	fixture.attachComputerAgain()
	answered, err = fixture.benCalls(t, share, "pay", card(10000, "usd"))
	if err != nil || answered["callState"] != "waiting_for_approval" {
		t.Errorf("a failed payment did not count against the month: %v %v", answered, err)
	}
	answered, err = fixture.benCalls(t, share, "pay", card(5000, "usd"))
	if err != nil || answered["callState"] != "ran" {
		t.Errorf("what is left of the month after the failed one was refused: %v %v", answered, err)
	}
	answered, err = fixture.benCalls(t, share, "pay", card(1, "usd"))
	if err != nil || answered["callState"] != "waiting_for_approval" {
		t.Errorf("the month is used up and a cent still went ahead: %v %v", answered, err)
	}
}

func (self *skillShareFixture) attachComputerAgain() {
	self.worker.DetachComputer(self.adaAgent.ID, nil)
	self.attachComputer()
}

// With the owner's computer not connected the call answers that, nothing is
// recorded as ran, and nothing is stored at all.
func TestACallWithTheOwnersComputerAwayAnswersThatAndRecordsNothing(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(10000), CurrencyCode: "usd"})
	fixture.worker.DetachComputer(fixture.adaAgent.ID, nil)

	_, err := fixture.benCalls(t, share, "pay", card(2500, "usd"))
	if err == nil || !strings.Contains(err.Error(), "Ada's computer is not connected") {
		t.Fatalf("the call answered %v", err)
	}
	if calls := fixture.callsOf(t, share); len(calls) != 0 {
		t.Errorf("a call that could not be tried left %+v", calls)
	}
	// A call that waits does not need the computer yet, and approving it does.
	answered, err := fixture.benCalls(t, share, "pay", card(14000, "usd"))
	if err != nil || answered["callState"] != "waiting_for_approval" {
		t.Fatalf("a call above the limit: %v %v", answered, err)
	}
	waiting := fixture.callsOf(t, share)[0]
	if _, err := fixture.worker.AnswerSharedCall(context.Background(), fixture.ada.ID, waiting.ID, true, ""); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("approving with the computer away: %v", err)
	}
	if still := fixture.callsOf(t, share)[0]; still.CallState != models.SkillShareCallWaitingForApproval {
		t.Errorf("the approval was spent on a call that could not go: %s", still.CallState)
	}
	fixture.attachComputer()
	approved, err := fixture.worker.AnswerSharedCall(context.Background(), fixture.ada.ID, waiting.ID, true, "go ahead")
	if err != nil || approved.CallState != models.SkillShareCallRan || approved.DecisionNote != "go ahead" || len(fixture.ranCommands()) != 1 {
		t.Fatalf("approving once she is back: %+v %v (%v)", approved, err, fixture.ranCommands())
	}
}

// Approving runs exactly the call that was shown, with its stored arguments and
// without looking at the limits again; the grantee's conversation is told once
// and their phone is told; nobody but the owner answers.
func TestApprovingRunsTheStoredCallOnceAndTellsTheGrantee(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(10000), CurrencyCode: "usd"})
	if _, err := fixture.benCalls(t, share, "pay", card(14000, "usd")); err != nil {
		t.Fatal(err)
	}
	waiting := fixture.callsOf(t, share)[0]

	if _, err := fixture.worker.AnswerSharedCall(context.Background(), fixture.ben.ID, waiting.ID, true, ""); err == nil {
		t.Fatal("the person who asked approved their own call")
	}
	if len(fixture.ranCommands()) != 0 {
		t.Fatal("a refused answer ran the call")
	}
	approved, err := fixture.worker.AnswerSharedCall(context.Background(), fixture.ada.ID, waiting.ID, true, "ok")
	if err != nil || approved.CallState != models.SkillShareCallRan || approved.DecidedAt == nil {
		t.Fatalf("approving: %+v %v", approved, err)
	}
	commands := fixture.ranCommands()
	if len(commands) != 1 || !strings.Contains(commands[0], "14000") || !strings.Contains(commands[0], "Example Store") {
		t.Fatalf("the owner's computer ran %v", commands)
	}
	if _, err := fixture.worker.AnswerSharedCall(context.Background(), fixture.ada.ID, waiting.ID, true, ""); err != ErrCallNotWaiting {
		t.Errorf("answering twice: %v", err)
	}

	// The grantee's phone, and their conversation, once.
	fixture.worker.waitGroup.Wait()
	sent := fixture.relay.to("sealed:ben")
	if len(sent) != 1 {
		t.Fatalf("the grantee's phone was sent %d notifications", len(sent))
	}
	told, _ := push.DecryptContent(fixture.benKey, sent[0].Sealed)
	if told.NotificationKind != models.NotificationPayment || !strings.Contains(told.Body, "Ada approved your request") {
		t.Errorf("the grantee's phone was told %+v", told)
	}
	fixture.waitUntilTold(t, waiting.ID)
}

// waitUntilTold waits for the wake to run: with no model to ask it marks the
// call told without a turn.
func (self *skillShareFixture) waitUntilTold(t *testing.T, callId string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var call *models.SkillShareCall
		dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
			call, _ = tx.GetSkillShareCall(callId)
		})
		if call != nil && call.WokenAt != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the grantee's conversation was not told")
}

// Two answers at once, from the phone and the drawer, run the call once.
func TestTwoAnswersRunTheCallOnce(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, IsApprovalRequired: true})
	if _, err := fixture.benCalls(t, share, "pay", card(2500, "usd")); err != nil {
		t.Fatal(err)
	}
	waiting := fixture.callsOf(t, share)[0]

	var group sync.WaitGroup
	var succeeded, refused atomic.Int32
	for range 6 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := fixture.worker.AnswerSharedCall(context.Background(), fixture.ada.ID, waiting.ID, true, ""); err == nil {
				succeeded.Add(1)
			} else if err == ErrCallNotWaiting {
				refused.Add(1)
			}
		}()
	}
	group.Wait()
	if succeeded.Load() != 1 || refused.Load() != 5 || len(fixture.ranCommands()) != 1 {
		t.Fatalf("%d answers took, %d were refused, the computer ran %d commands", succeeded.Load(), refused.Load(), len(fixture.ranCommands()))
	}
	fixture.worker.waitGroup.Wait()
}

// Declining stops the call, tells the grantee, and a call declined is not
// approved afterwards.
func TestDecliningStopsTheCall(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, IsApprovalRequired: true})
	if _, err := fixture.benCalls(t, share, "pay", card(2500, "usd")); err != nil {
		t.Fatal(err)
	}
	waiting := fixture.callsOf(t, share)[0]
	declined, err := fixture.worker.AnswerSharedCall(context.Background(), fixture.ada.ID, waiting.ID, false, "not this week")
	if err != nil || declined.CallState != models.SkillShareCallDeclined || declined.DecisionNote != "not this week" {
		t.Fatalf("declining: %+v %v", declined, err)
	}
	if _, err := fixture.worker.AnswerSharedCall(context.Background(), fixture.ada.ID, waiting.ID, true, ""); err != ErrCallNotWaiting {
		t.Errorf("approving a call already declined: %v", err)
	}
	if len(fixture.ranCommands()) != 0 {
		t.Errorf("a declined call ran %v", fixture.ranCommands())
	}
	fixture.worker.waitGroup.Wait()
	fixture.waitUntilTold(t, waiting.ID)
}

// A call nobody answered in a day expires in the sweep, once, and the grantee
// hears of it; a call within its day is left alone.
func TestACallExpiresAfterADayAndTheGranteeHearsOfIt(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, IsApprovalRequired: true})
	for range 2 {
		if _, err := fixture.benCalls(t, share, "pay", card(2500, "usd")); err != nil {
			t.Fatal(err)
		}
	}
	calls := fixture.callsOf(t, share)
	dbtest.Exec(t, fixture.database, fmt.Sprintf(`UPDATE "skill_share_call" SET "expires_at" = now() - interval '1 minute' WHERE "id" = '%s'`, calls[1].ID))

	fixture.worker.sweepSkillShareCalls(context.Background(), time.Now())
	fixture.worker.sweepSkillShareCalls(context.Background(), time.Now())
	after := fixture.callsOf(t, share)
	states := map[string]models.SkillShareCallState{after[0].ID: after[0].CallState, after[1].ID: after[1].CallState}
	if states[calls[1].ID] != models.SkillShareCallExpired || states[calls[0].ID] != models.SkillShareCallWaitingForApproval {
		t.Fatalf("after the sweep: %v", states)
	}
	if _, err := fixture.worker.AnswerSharedCall(context.Background(), fixture.ada.ID, calls[1].ID, true, ""); err != ErrCallNotWaiting {
		t.Errorf("approving a call that expired: %v", err)
	}
	fixture.worker.waitGroup.Wait()
	fixture.waitUntilTold(t, calls[1].ID)
	sent := fixture.relay.to("sealed:ben")
	if len(sent) != 1 {
		t.Fatalf("the grantee's phone was sent %d notifications", len(sent))
	}
	if told, _ := push.DecryptContent(fixture.benKey, sent[0].Sealed); !strings.Contains(told.Body, "expired") {
		t.Errorf("the grantee's phone was told %q", told.Body)
	}
	if len(fixture.ranCommands()) != 0 {
		t.Errorf("an expired call ran %v", fixture.ranCommands())
	}
}

// A tool that works on a request a share made is allowed only for an
// identifier an earlier call of that share printed, and never for a listing.
func TestARequestIsReachedOnlyThroughTheShareThatMadeIt(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay", "spend_requests"}, PaymentLimitMinor: dollars(10000), CurrencyCode: "usd"})
	fixture.replies["retrieve"] = `{"id":"req_0001abcd","state":"approved"}`

	look := func(arguments map[string]any) map[string]any {
		answered, err := fixture.benCalls(t, share, "spend_requests", arguments)
		if err != nil {
			t.Fatalf("the call: %s", err)
		}
		return answered
	}
	for name, arguments := range map[string]map[string]any{
		"a listing":         {"action": "active"},
		"the history":       {"action": "history"},
		"an id of its own":  {"action": "retrieve", "spend_request_id": "req_0001abcd"},
		"a cancel, unknown": {"action": "cancel", "spend_request_id": "req_9999zzzz"},
	} {
		if answered := look(arguments); answered["callState"] != "refused" {
			t.Errorf("%s was not refused: %v", name, answered)
		}
	}
	if len(fixture.ranCommands()) != 0 {
		t.Fatalf("a refused call ran %v", fixture.ranCommands())
	}
	refused := fixture.callsOf(t, share)
	if len(refused) != 4 || refused[0].CallState != models.SkillShareCallRefused || refused[0].ToolOutput["error"] == nil {
		t.Errorf("the refusals are recorded as %+v", refused)
	}

	fixture.replies["paycli' 'card"] = `{"id":"req_0001abcd","state":"pending_approval"}`
	if answered, err := fixture.benCalls(t, share, "pay", card(2500, "usd")); err != nil || answered["callState"] != "ran" {
		t.Fatalf("the payment that makes the request: %v %v", answered, err)
	}
	if answered := look(map[string]any{"action": "retrieve", "spend_request_id": "req_0001abcd"}); answered["callState"] != "ran" {
		t.Errorf("a request the share made is refused: %v", answered)
	}
	if answered := look(map[string]any{"action": "retrieve", "spend_request_id": "req_0001abc"}); answered["callState"] != "refused" {
		t.Errorf("the start of an identifier counts: %v", answered)
	}
	if answered := look(map[string]any{"action": "active"}); answered["callState"] != "refused" {
		t.Errorf("a listing is shared once there is a request: %v", answered)
	}
}

// Two people who no longer share a circle share nothing, even when the share
// row is still there: it is checked again at every call.
func TestNotSharingACircleRefusesAtCallTime(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(10000), CurrencyCode: "usd"})
	tool := fixture.benTool(t, "pay")
	// Taken out of the circle underneath, as a bug elsewhere might.
	dbtest.Exec(t, fixture.database, fmt.Sprintf(`DELETE FROM "circle_member" WHERE "circle_id" = '%s' AND "user_id" = '%s'`, fixture.circle.ID, fixture.ben.ID))
	if kept := fixture.worker.SharedSkillTools(context.Background(), fixture.benAgent); len(kept) != 0 {
		t.Errorf("ben is still offered %d tools", len(kept))
	}

	arguments, _ := json.Marshal(card(2500, "usd"))
	run := &directRun{agent: fixture.worker, owner: fixture.ben, agentModel: fixture.benAgent}
	if _, err := tool.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "c1", Arguments: arguments, Confirmed: true}); err != ErrSharedUnavailable {
		t.Fatalf("a call from a tool offered before the circle ended: %v", err)
	}
	if len(fixture.ranCommands()) != 0 || len(fixture.callsOf(t, share)) != 0 {
		t.Error("a call outside the circle ran or was recorded")
	}
	// Nor can a share be called by someone it is not to.
	if _, err := fixture.worker.RunShared(context.Background(), &SharedCall{ShareID: share.ID, ToolName: "pay", Arguments: card(100, "usd"), GranteeUserID: fixture.ada.ID}); err != ErrSharedUnavailable {
		t.Errorf("the owner called through her own share: %v", err)
	}
}

// What the owner's judge says a command would do decides whether it goes
// ahead: reading, changing and spending go ahead inside the share, and speaking
// for the owner, giving access or destroying something wait for her.
func TestTheOwnersJudgeCanHoldACallForHer(t *testing.T) {
	for judged, waits := range map[string]bool{
		callRiskRead: false, callRiskChange: false, callRiskMoney: false,
		callRiskOutward: true, callRiskGranting: true, callRiskDestructive: true,
	} {
		t.Run(judged, func(t *testing.T) {
			fixture := newSkillShareFixture(t)
			fixture.judged.Store(judged)
			share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(10000), CurrencyCode: "usd"})
			answered, err := fixture.benCalls(t, share, "pay", card(2500, "usd"))
			if err != nil {
				t.Fatal(err)
			}
			if isWaiting := answered["callState"] == "waiting_for_approval"; isWaiting != waits {
				t.Errorf("judged %s, the call came to %v", judged, answered["callState"])
			}
			if waits && !strings.Contains(fmt.Sprint(answered["message"]), "check on the command") {
				t.Errorf("the reason is %v", answered["message"])
			}
			fixture.worker.waitGroup.Wait()
		})
	}
}

// With no model to judge a command, the call waits for the owner.
func TestACommandThatCannotBeJudgedWaitsForTheOwner(t *testing.T) {
	fixture := newSkillShareFixture(t)
	fixture.worker.settings.Registry = nil
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(10000), CurrencyCode: "usd"})
	answered, err := fixture.benCalls(t, share, "pay", card(2500, "usd"))
	if err != nil || answered["callState"] != "waiting_for_approval" || !strings.Contains(fmt.Sprint(answered["message"]), "could not be checked") {
		t.Fatalf("the call: %v %v", answered, err)
	}
	fixture.worker.waitGroup.Wait()
}

// An owner whose agent is switched off has no computer attached, and is not
// offered as a source of tools.
func TestSharedToolsStopWithTheOwnersAgent(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(10000), CurrencyCode: "usd"})
	tool := fixture.benTool(t, "pay")
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.UpdateAgent(fixture.adaAgent.ID, func(agent *models.Agent) error {
			agent.Enabled = false
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	if got := fixture.worker.SharedSkillTools(context.Background(), fixture.benAgent); len(got) != 0 {
		t.Errorf("ben is offered %d tools of an owner whose agent is off", len(got))
	}
	arguments, _ := json.Marshal(card(2500, "usd"))
	run := &directRun{agent: fixture.worker, owner: fixture.ben, agentModel: fixture.benAgent}
	if _, err := tool.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "c1", Arguments: arguments, Confirmed: true}); err != ErrSharedOwnerAway {
		t.Errorf("a call to an owner whose agent is off: %v", err)
	}
	if len(fixture.callsOf(t, share)) != 0 {
		t.Error("a call that could not be tried was recorded")
	}
}

// The reason is the other person's words, cut and cleaned, and a skill taken
// off the server takes its shared tools with it.
func TestTheReasonIsCleanedAndAnUninstalledSkillIsNotOffered(t *testing.T) {
	if got := cleanSharedReason("  groceries\n\tfor the week \x00\x07 " + strings.Repeat("x", 600)); strings.ContainsAny(got, "\n\t\x00\x07") || len([]rune(got)) > sharedReasonCharacters+1 || !strings.HasPrefix(got, "groceries for the week ") {
		t.Errorf("the reason is %q", got)
	}
	fixture := newSkillShareFixture(t)
	fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(10000), CurrencyCode: "usd"})
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if err := tx.DeleteAgentSkill("payments"); err != nil {
			t.Fatal(err)
		}
	})
	if got := fixture.worker.SharedSkillTools(context.Background(), fixture.benAgent); len(got) != 0 {
		t.Errorf("a skill that is not installed is offered: %d", len(got))
	}
}

// An agent that asks again and again cannot bury the owner: past a score of
// calls waiting, the next is turned back until she answers some.
func TestTooManyWaitingCallsAreTurnedBack(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, IsApprovalRequired: true})
	for range sharedWaitingMost {
		if answered, err := fixture.benCalls(t, share, "pay", card(100, "usd")); err != nil || answered["callState"] != "waiting_for_approval" {
			t.Fatalf("a call: %v %v", answered, err)
		}
	}
	answered, err := fixture.benCalls(t, share, "pay", card(100, "usd"))
	if err != nil || answered["callState"] != "refused" || !strings.Contains(fmt.Sprint(answered["message"]), "already waiting") {
		t.Fatalf("one more: %v %v", answered, err)
	}
	// She answers one and there is room again.
	waiting := fixture.callsOf(t, share)
	var oldest *models.SkillShareCall
	for _, call := range waiting {
		if call.CallState == models.SkillShareCallWaitingForApproval {
			oldest = call
		}
	}
	if _, err := fixture.worker.AnswerSharedCall(context.Background(), fixture.ada.ID, oldest.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	if answered, err := fixture.benCalls(t, share, "pay", card(100, "usd")); err != nil || answered["callState"] != "waiting_for_approval" {
		t.Fatalf("after she answered one: %v %v", answered, err)
	}
	fixture.worker.waitGroup.Wait()
}

// A call that waited counts toward the month it was approved in, not the one it
// was asked in.
func TestAnApprovedCallCountsWhereItRan(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(10000), MonthlyLimitMinor: dollars(15000), CurrencyCode: "usd"})
	if answered, err := fixture.benCalls(t, share, "pay", card(14000, "usd")); err != nil || answered["callState"] != "waiting_for_approval" {
		t.Fatalf("the call: %v %v", answered, err)
	}
	waiting := fixture.callsOf(t, share)[0]
	// Asked last month.
	dbtest.Exec(t, fixture.database, fmt.Sprintf(`UPDATE "skill_share_call" SET "created_at" = now() - interval '40 days', "expires_at" = now() + interval '1 hour' WHERE "id" = '%s'`, waiting.ID))
	if _, err := fixture.worker.AnswerSharedCall(context.Background(), fixture.ada.ID, waiting.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	// It ran now, so it is this month's: only 1000 of the 15000 is left.
	if answered, err := fixture.benCalls(t, share, "pay", card(2000, "usd")); err != nil || answered["callState"] != "waiting_for_approval" {
		t.Errorf("a payment that takes the month past its limit: %v %v", answered, err)
	}
	if answered, err := fixture.benCalls(t, share, "pay", card(1000, "usd")); err != nil || answered["callState"] != "ran" {
		t.Errorf("a payment inside what is left: %v %v", answered, err)
	}
	fixture.worker.waitGroup.Wait()
}

// Arguments too large to show on a card are turned back before anything is
// stored.
func TestArgumentsTooLargeForACardAreTurnedBack(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, IsApprovalRequired: true})
	arguments := card(100, "usd")
	arguments["merchant"] = strings.Repeat("x", sharedArgumentsBytes+1)
	if _, err := fixture.benCalls(t, share, "pay", arguments); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("a call with arguments of a megabyte: %v", err)
	}
	if calls := fixture.callsOf(t, share); len(calls) != 0 {
		t.Errorf("a call turned back for its size left %+v", calls)
	}
}

// A share made while its two people could take part stops working when an
// operator disables either account or takes agent:use from the owner: the
// tools are not offered, a call is turned back, and the owner's answer to a
// call that waits is refused. It works again when they can.
func TestSharedToolsStopWhileEitherAccountIsDisabledOrTheOwnerMayNotUseAgents(t *testing.T) {
	fixture := newSkillShareFixture(t)
	share := fixture.share(t, &models.SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: dollars(10000), CurrencyCode: "usd"})
	waiting, err := fixture.benCalls(t, share, "pay", card(25000, "usd"))
	if err != nil || waiting["callState"] != "waiting_for_approval" {
		t.Fatalf("a call above the limit: %v %v", waiting, err)
	}
	waitingId, _ := waiting["callId"].(string)
	if waitingId == "" {
		t.Fatalf("the waiting call has no id: %v", waiting)
	}
	tool := fixture.benTool(t, "pay")
	if tool == nil {
		t.Fatal("ben is offered no tool to start from")
	}

	disable := func(userId string, isDisabled bool) {
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			if _, err := tx.UpdateUser(userId, func(user *models.User) error {
				user.DisabledAt = nil
				if isDisabled {
					now := time.Now()
					user.DisabledAt = &now
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	setAgentUse := func(isHeld bool) {
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			if _, err := tx.UpdateGroup(fixture.agentUsers.ID, func(group *models.Group) error {
				group.UserIDs = nil
				if isHeld {
					group.UserIDs = []string{fixture.ada.ID}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	run := &directRun{agent: fixture.worker, owner: fixture.ben, agentModel: fixture.benAgent}
	attempt := func(name string) {
		t.Helper()
		if got := fixture.worker.SharedSkillTools(context.Background(), fixture.benAgent); len(got) != 0 {
			t.Errorf("%s: ben is offered %d shared tools", name, len(got))
		}
		arguments, _ := json.Marshal(card(2500, "usd"))
		if _, err := tool.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "c1", Arguments: arguments, Confirmed: true}); !errors.Is(err, ErrSharedUnavailable) {
			t.Errorf("%s: a call was not turned back: %v", name, err)
		}
		for _, isApproved := range []bool{true, false} {
			if _, err := fixture.worker.AnswerSharedCall(context.Background(), fixture.ada.ID, waitingId, isApproved, ""); !errors.Is(err, ErrSharedUnavailable) {
				t.Errorf("%s: the owner's answer (approved %v) was taken: %v", name, isApproved, err)
			}
		}
		if len(fixture.ranCommands()) != 0 {
			t.Errorf("%s: a command ran on the owner's computer: %v", name, fixture.ranCommands())
		}
	}

	disable(fixture.ada.ID, true)
	attempt("the owner is disabled")
	disable(fixture.ada.ID, false)
	setAgentUse(false)
	attempt("the owner may not use agents")
	setAgentUse(true)
	disable(fixture.ben.ID, true)
	attempt("the grantee is disabled")
	disable(fixture.ben.ID, false)

	// Back as it was, it works.
	if got := fixture.worker.SharedSkillTools(context.Background(), fixture.benAgent); len(got) == 0 {
		t.Error("nothing is offered once both can take part again")
	}
	if _, err := fixture.worker.AnswerSharedCall(context.Background(), fixture.ada.ID, waitingId, false, ""); err != nil {
		t.Errorf("declining once both can take part again: %v", err)
	}
}
