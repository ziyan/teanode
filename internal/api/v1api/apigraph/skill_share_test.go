package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/skills"
)

// skillShareWorld is the page-sharing world (ada and ben in one circle, cora in
// none of it, eve who cannot use circles) with ada's agent on, a payment skill
// installed, a worker whose computer for ada answers shell commands, and a way
// to make a call wait without a model.
type skillShareWorld struct {
	*pageShareWorld
	worker   *agent.Agent
	computer *shellComputer
	adaAgent *models.Agent
}

// shellComputer is ada's computer: it answers every command and counts them.
type shellComputer struct {
	worker  *agent.Agent
	agentId string
	ran     atomic.Int32
}

func (self *shellComputer) Send(message []byte) error {
	var decoded struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(message, &decoded)
	self.ran.Add(1)
	go self.worker.ComputerAnswered(self.agentId, self, decoded.ID, true, json.RawMessage(`{"stdout":"{\"id\":\"req_0001abcd\"}","stderr":"","exitCode":0}`), "")
	return nil
}

func newSkillShareWorld(t *testing.T) *skillShareWorld {
	t.Helper()
	content, err := os.ReadFile("../../../skills/testdata/payments.md")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	world := &skillShareWorld{pageShareWorld: newPageShareWorld(t)}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		found, err := tx.GetAgentByUser(world.ada.ID)
		if err != nil || found == nil {
			t.Fatalf("GetAgentByUser: %v %v", found, err)
		}
		if world.adaAgent, err = tx.UpdateAgent(found.ID, func(each *models.Agent) error {
			each.Enabled = true
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgent: %s", err)
		}
		if _, err := tx.PutAgentSkill(&models.AgentSkill{Name: "payments", Version: "1", Description: "A fictional payment program", Content: string(content), Enabled: true}); err != nil {
			t.Fatalf("PutAgentSkill: %s", err)
		}
	})
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Server.Secret = "a secret for sealing in this test"
	world.worker = agent.New(&agent.Settings{
		Database: world.database, Configuration: func() *config.Configuration { return configuration },
		Instance: "skill-share-test", Tick: time.Hour,
	})
	world.graph.settings.Agent = world.worker
	world.computer = &shellComputer{worker: world.worker, agentId: world.adaAgent.ID}
	world.worker.AttachComputer(world.adaAgent.ID, world.computer, agent.ComputerIdentity{Name: "laptop", System: "linux", Home: "/home/example"})
	return world
}

const skillShareFields = `{ id skillName toolNames ownerUsername granteeUsername paymentLimitMinor monthlyLimitMinor currencyCode isApprovalRequired isOwnedByCaller waitingCallCount }`
const skillShareCallFields = `{ id toolName callState amountMinor currencyCode requestReason granteeUsername decisionNote toolArguments }`

func (self *skillShareWorld) shareQuery(toolNames string, granteeId string, extra string) string {
	return fmt.Sprintf(`mutation { ShareSkill(skillName: "payments", toolNames: %s, granteeUserId: %q %s) %s }`, toolNames, granteeId, extra, skillShareFields)
}

// shareWithBen makes a share that asks every time, so that every call waits.
func (self *skillShareWorld) shareWithBen(t *testing.T) string {
	t.Helper()
	data := self.call(t, self.ada, self.shareQuery(`["pay"]`, self.ben.ID, `, isApprovalRequired: true`))
	return data["ShareSkill"].(map[string]any)["id"].(string)
}

// waitingCall makes a call of the share wait, as ben's agent would.
func (self *skillShareWorld) waitingCall(t *testing.T, shareId string) string {
	t.Helper()
	outcome, err := self.worker.RunShared(context.Background(), &agent.SharedCall{
		ShareID: shareId, ToolName: "pay", GranteeUserID: self.ben.ID, RequestReason: "the weekly groceries",
		Arguments: map[string]any{"action": "card", "merchant": "Example Store", "amount_minor": 2500, "currency": "usd"},
	})
	if err != nil || outcome.Call.CallState != models.SkillShareCallWaitingForApproval {
		t.Fatalf("a call that waits: %+v %v", outcome, err)
	}
	return outcome.Call.ID
}

func (self *skillShareWorld) answer(callId string, isApproved bool) string {
	return fmt.Sprintf(`mutation { AnswerSkillShareCall(callId: %q, isApproved: %v, decisionNote: "ok") %s }`, callId, isApproved, skillShareCallFields)
}

// Who may share: a person who can use their agent and circles, with someone
// they share a circle with, of a skill that is installed, naming tools it
// declares, bounded when a tool pays. Every refusal that is about who answers
// not found.
func TestOnlyTheOwnerSharesASkillWithinACircle(t *testing.T) {
	world := newSkillShareWorld(t)

	for name, query := range map[string]string{
		"somebody in no circle with her": world.shareQuery(`["pay"]`, world.cora.ID, `, isApprovalRequired: true`),
		"nobody":                         world.shareQuery(`["pay"]`, "nobody-has-this-id", `, isApprovalRequired: true`),
		"a skill that is not installed":  strings.Replace(world.shareQuery(`["pay"]`, world.ben.ID, `, isApprovalRequired: true`), `"payments"`, `"nothing"`, 1),
	} {
		if said := world.refused(t, world.ada, query); !strings.Contains(strings.ToLower(said), "not found") {
			t.Errorf("%s: a refusal says not found: %q", name, said)
		}
	}
	// Someone who cannot use circles cannot share at all.
	world.refused(t, world.eve, world.shareQuery(`["pay"]`, world.ben.ID, `, isApprovalRequired: true`))

	// What is wrong with the request is said.
	for name, test := range map[string]struct{ query, expected string }{
		"a tool that pays with no bound": {world.shareQuery(`["pay"]`, world.ben.ID, ``), "limit"},
		"a tool the skill lacks":         {world.shareQuery(`["wire"]`, world.ben.ID, `, isApprovalRequired: true`), "wire"},
		"no tools":                       {world.shareQuery(`[]`, world.ben.ID, `, isApprovalRequired: true`), "which"},
		"a limit with no currency":       {world.shareQuery(`["pay"]`, world.ben.ID, `, paymentLimitMinor: 500`), "currency"},
		"a currency that is not one":     {world.shareQuery(`["pay"]`, world.ben.ID, `, paymentLimitMinor: 500, currencyCode: "dollars"`), "three letters"},
		"a limit of nothing":             {world.shareQuery(`["pay"]`, world.ben.ID, `, paymentLimitMinor: 0, currencyCode: "usd"`), "above zero"},
	} {
		if said := world.refused(t, world.ada, test.query); !strings.Contains(strings.ToLower(said), test.expected) {
			t.Errorf("%s: %q does not mention %q", name, said, test.expected)
		}
	}

	data := world.call(t, world.ada, world.shareQuery(`["pay"]`, world.ben.ID, `, paymentLimitMinor: 10000, monthlyLimitMinor: 40000, currencyCode: "USD"`))
	share := data["ShareSkill"].(map[string]any)
	if share["granteeUsername"] != world.ben.Username || share["paymentLimitMinor"].(float64) != 10000 || share["currencyCode"] != "usd" ||
		share["isOwnedByCaller"] != true || share["isApprovalRequired"] != false {
		t.Errorf("the share: %v", share)
	}

	// Sharing again changes the terms of the same share.
	again := world.call(t, world.ada, world.shareQuery(`["pay", "spend_requests"]`, world.ben.ID, `, isApprovalRequired: true`))["ShareSkill"].(map[string]any)
	if again["id"] != share["id"] || len(again["toolNames"].([]any)) != 2 || again["isApprovalRequired"] != true {
		t.Errorf("sharing again: %v", again)
	}
	// A skill taken off the server cannot be shared.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.PutAgentSkill(&models.AgentSkill{Name: "payments", Version: "1", Description: "x", Content: "---\nname: payments\n", Enabled: false}); err != nil {
			t.Fatal(err)
		}
	})
	world.refused(t, world.ada, world.shareQuery(`["pay"]`, world.ben.ID, `, isApprovalRequired: true`))
}

// The owner lists the shares she made and ben the ones made to him; nobody else
// sees either, nor the calls; only the owner takes one back.
func TestSkillSharesAndTheirCallsAreSeenByTheTwoPeopleAndNoOneElse(t *testing.T) {
	world := newSkillShareWorld(t)
	shareId := world.shareWithBen(t)
	callId := world.waitingCall(t, shareId)

	for user, expectOwned := range map[*models.User]bool{world.ada: true, world.ben: false} {
		listed := world.call(t, user, `query { ListSkillShares `+skillShareFields+` }`)["ListSkillShares"].([]any)
		if len(listed) != 1 || listed[0].(map[string]any)["isOwnedByCaller"] != expectOwned || listed[0].(map[string]any)["waitingCallCount"].(float64) != 1 {
			t.Errorf("%s is shown %v", user.Username, listed)
		}
		calls := world.call(t, user, fmt.Sprintf(`query { ListSkillShareCalls(shareId: %q) %s }`, shareId, skillShareCallFields))["ListSkillShareCalls"].([]any)
		if len(calls) != 1 || calls[0].(map[string]any)["id"] != callId || calls[0].(map[string]any)["requestReason"] != "the weekly groceries" ||
			calls[0].(map[string]any)["callState"] != "waiting_for_approval" || calls[0].(map[string]any)["amountMinor"].(float64) != 2500 {
			t.Errorf("%s is shown the calls %v", user.Username, calls)
		}
	}
	for _, stranger := range []*models.User{world.cora, world.eve} {
		listed, _ := world.ask(t, stranger, `query { ListSkillShares `+skillShareFields+` }`)
		if rows, _ := listed["ListSkillShares"].([]any); len(rows) != 0 {
			t.Errorf("%s is shown %v", stranger.Username, rows)
		}
		world.refused(t, stranger, fmt.Sprintf(`query { ListSkillShareCalls(shareId: %q) %s }`, shareId, skillShareCallFields))
	}
	world.refused(t, world.ada, `query { ListSkillShareCalls(shareId: "nothing-like-it") `+skillShareCallFields+` }`)
	world.refused(t, world.ada, fmt.Sprintf(`query { ListSkillShareCalls(shareId: %q, callState: "invented") %s }`, shareId, skillShareCallFields))
	only := world.call(t, world.ada, fmt.Sprintf(`query { ListSkillShareCalls(shareId: %q, callState: "ran") %s }`, shareId, skillShareCallFields))
	if rows := only["ListSkillShareCalls"].([]any); len(rows) != 0 {
		t.Errorf("a state narrows the calls: %v", rows)
	}

	world.refused(t, world.ben, fmt.Sprintf(`mutation { UnshareSkill(shareId: %q) }`, shareId))
	world.refused(t, world.cora, fmt.Sprintf(`mutation { UnshareSkill(shareId: %q) }`, shareId))
	world.call(t, world.ada, fmt.Sprintf(`mutation { UnshareSkill(shareId: %q) }`, shareId))
	if listed := world.call(t, world.ben, `query { ListSkillShares `+skillShareFields+` }`)["ListSkillShares"].([]any); len(listed) != 0 {
		t.Errorf("ben is still shown a share taken back: %v", listed)
	}
	world.refused(t, world.ada, fmt.Sprintf(`mutation { UnshareSkill(shareId: %q) }`, shareId))
}

// Only the owner answers, as a person: not the person it is shared with, not a
// stranger, and not the owner's own agent, whose operations carry the agent as
// the actor. A refused answer leaves the call waiting and runs nothing.
func TestOnlyTheOwnerAsAPersonAnswersACall(t *testing.T) {
	world := newSkillShareWorld(t)
	shareId := world.shareWithBen(t)
	callId := world.waitingCall(t, shareId)

	for _, user := range []*models.User{world.ben, world.cora, world.eve} {
		if said := world.refused(t, user, world.answer(callId, true)); !strings.Contains(strings.ToLower(said), "not found") {
			t.Errorf("%s: a refusal says not found: %q", user.Username, said)
		}
	}
	world.refused(t, world.ada, world.answer("no-such-call", true))

	// Her own agent, through the operations its tools use.
	operations := &agentOperations{
		graph: world.graph, user: world.ada,
		permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}, {Permission: models.PermissionCircleUse}}),
	}
	for _, isApproved := range []bool{true, false} {
		var answered map[string]any
		err := operations.Execute(context.Background(), world.answer(callId, isApproved), nil, &answered)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not found") {
			t.Errorf("the agent answered (approved %v): %v %v", isApproved, answered, err)
		}
	}
	// It may look, and it may share.
	var listed map[string]any
	if err := operations.Execute(context.Background(), fmt.Sprintf(`query { ListSkillShareCalls(shareId: %q) %s }`, shareId, skillShareCallFields), nil, &listed); err != nil {
		t.Errorf("the agent looks at the calls: %s", err)
	}

	if world.computer.ran.Load() != 0 {
		t.Fatalf("a refused answer ran %d commands", world.computer.ran.Load())
	}
	calls := world.call(t, world.ada, fmt.Sprintf(`query { ListSkillShareCalls(shareId: %q) %s }`, shareId, skillShareCallFields))["ListSkillShareCalls"].([]any)
	if calls[0].(map[string]any)["callState"] != "waiting_for_approval" {
		t.Errorf("the call after the refusals: %v", calls[0])
	}

	// And she does, as herself.
	approved := world.call(t, world.ada, world.answer(callId, true))["AnswerSkillShareCall"].(map[string]any)
	if approved["callState"] != "ran" || approved["decisionNote"] != "ok" || world.computer.ran.Load() != 1 {
		t.Errorf("the owner's approval: %v (%d commands)", approved, world.computer.ran.Load())
	}
	world.worker.Wait()
}

// The same call answered from the phone and the drawer at once runs once; the
// other answer is told it is not waiting any more.
func TestTwoAnswersToACallRunItOnce(t *testing.T) {
	world := newSkillShareWorld(t)
	shareId := world.shareWithBen(t)
	callId := world.waitingCall(t, shareId)

	var group sync.WaitGroup
	var took, refused atomic.Int32
	for range 6 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, failures := world.ask(t, world.ada, world.answer(callId, true))
			switch {
			case len(failures) == 0:
				took.Add(1)
			case strings.Contains(strings.Join(failures, " "), "not waiting"):
				refused.Add(1)
			default:
				t.Errorf("an answer failed another way: %v", failures)
			}
		}()
	}
	group.Wait()
	if took.Load() != 1 || refused.Load() != 5 || world.computer.ran.Load() != 1 {
		t.Fatalf("%d answers took, %d were told it was not waiting, the computer ran %d commands", took.Load(), refused.Load(), world.computer.ran.Load())
	}
	world.worker.Wait()
}

// The owner's computer being away leaves the call waiting and says so, and a
// decline needs no computer.
func TestApprovingNeedsTheOwnersComputer(t *testing.T) {
	world := newSkillShareWorld(t)
	shareId := world.shareWithBen(t)
	callId := world.waitingCall(t, shareId)
	world.worker.DetachComputer(world.adaAgent.ID, nil)

	if said := world.refused(t, world.ada, world.answer(callId, true)); !strings.Contains(said, "not connected") {
		t.Errorf("approving with the computer away: %q", said)
	}
	declined := world.call(t, world.ada, world.answer(callId, false))["AnswerSkillShareCall"].(map[string]any)
	if declined["callState"] != "declined" || world.computer.ran.Load() != 0 {
		t.Errorf("declining: %v (%d commands)", declined, world.computer.ran.Load())
	}
	world.worker.Wait()
}

// Leaving the circle ends the share and with it the calls, for both people, in
// the same step; the shared tools go from the other person's next turn.
func TestLeavingTheCircleEndsASkillShare(t *testing.T) {
	world := newSkillShareWorld(t)
	shareId := world.shareWithBen(t)
	world.waitingCall(t, shareId)
	world.call(t, world.ben, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, world.home))

	for _, user := range []*models.User{world.ada, world.ben} {
		if listed := world.call(t, user, `query { ListSkillShares `+skillShareFields+` }`)["ListSkillShares"].([]any); len(listed) != 0 {
			t.Errorf("%s is still shown %v", user.Username, listed)
		}
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		ben, _ := tx.GetAgentByUser(world.ben.ID)
		if offered := world.worker.SharedSkillTools(context.Background(), ben); len(offered) != 0 {
			t.Errorf("ben is still offered %d tools", len(offered))
		}
	})
}

// An update of an installed skill is checked against the shares made of the
// old version: a tool that now declares an amount, shared with no limit and no
// asking, would let the other person spend without bound, so that share asks
// every time from then on. Shares that still qualify are left as they are.
func TestSavingASkillMakesSharesThatNoLongerQualifyAskEveryTime(t *testing.T) {
	world := newSkillShareWorld(t)
	content, err := os.ReadFile("../../../skills/testdata/payments.md")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	// The balance tool pays nothing in this version, so it is shared with no
	// limit; pay is shared with a limit.
	world.call(t, world.ada, world.shareQuery(`["balance"]`, world.ben.ID, ``))
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		circle, err := tx.CreateCircle(&models.Circle{Name: "Club", CircleKind: models.CircleKindFriends}, world.ada.ID)
		if err != nil {
			t.Fatalf("CreateCircle: %s", err)
		}
		if _, err := tx.AddCircleMember(circle.ID, world.cora.ID, models.MemberRoleMember, world.ada.ID); err != nil {
			t.Fatalf("AddCircleMember: %s", err)
		}
		if _, err := tx.ShareSkill(world.ada.ID, &models.SkillShare{
			SkillName: "payments", ToolNames: []string{"pay"}, GranteeUserID: world.cora.ID,
			PaymentLimitMinor: new(int64(500)), CurrencyCode: "usd",
		}); err != nil {
			t.Fatalf("ShareSkill: %s", err)
		}
	})
	// A new version in which balance takes an amount.
	updated := strings.Replace(string(content), `    description: What is left to spend. Declares no amount.
    type: shell
    command: ["paycli", "balance"]
    parameters:
      type: object
      properties: {}`, `    description: What is left to spend, and a top up.
    type: shell
    amountField: amount_minor
    command: ["paycli", "balance"]
    parameters:
      type: object
      properties:
        amount_minor:
          type: integer
          description: A top up
`, 1)
	if updated == string(content) {
		t.Fatalf("the test skill changed shape; the replacement matched nothing")
	}
	parsed, err := skills.Parse([]byte(updated))
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}
	var changed []*models.SkillShare
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if changed, err = holdSharesToSkill(tx, parsed); err != nil {
			t.Fatalf("holdSharesToSkill: %s", err)
		}
	})
	if len(changed) != 1 || len(changed[0].ToolNames) != 1 || changed[0].ToolNames[0] != "balance" || !changed[0].IsApprovalRequired {
		t.Fatalf("the share of balance now asks every time: %+v", changed)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		shares, err := tx.ListSkillSharesOf("payments")
		if err != nil {
			t.Fatal(err)
		}
		for _, share := range shares {
			switch share.ToolNames[0] {
			case "balance":
				if !share.IsApprovalRequired {
					t.Errorf("the balance share was stored as asking every time")
				}
			default:
				if share.IsApprovalRequired {
					t.Errorf("a share that still qualifies is left alone: %+v", share)
				}
			}
		}
		// Saved again, nothing more changes and nobody is told twice.
		again, err := holdSharesToSkill(tx, parsed)
		if err != nil || len(again) != 0 {
			t.Errorf("a second save changes nothing: %v %v", again, err)
		}
	})
}

// shareOf is the share the owner made with ben as the owner's dashboard lists
// it.
func (self *skillShareWorld) shareOf(t *testing.T) map[string]any {
	t.Helper()
	listed := self.call(t, self.ada, `query { ListSkillShares `+skillShareFields+` }`)["ListSkillShares"].([]any)
	if len(listed) != 1 {
		t.Fatalf("the owner is shown %d shares: %v", len(listed), listed)
	}
	return listed[0].(map[string]any)
}

// Lending money is a person's tap, on the dashboard or in the phone app: an
// agent can drive the owner's command line with their saved sign-in, so a
// command line token, an API token and the agent's own operations are not
// accepted for anything that lets a call through unasked. What only takes
// away, asking every time and lowering what a share allows, is open to all.
func TestLendingMoneyIsOnlyAcceptedFromATap(t *testing.T) {
	world := newSkillShareWorld(t)
	limits := func(payment, monthly int, extra string) string {
		return world.shareQuery(`["pay"]`, world.ben.ID, fmt.Sprintf(`, paymentLimitMinor: %d, monthlyLimitMinor: %d, currencyCode: "usd"%s`, payment, monthly, extra))
	}
	askEveryTime := world.shareQuery(`["pay"]`, world.ben.ID, `, isApprovalRequired: true`)

	// Creating a share with limits: refused to what is not a tap, and nothing
	// is stored.
	for name, credentialKind := range map[string]models.CredentialKind{"a command line token": models.CredentialToken, "no credential": ""} {
		_, failures := world.askWith(t, world.ada, credentialKind, limits(10000, 40000, ""))
		if said := strings.Join(failures, "; "); !strings.Contains(said, api.TapRequiredPhrase) {
			t.Errorf("%s was not told where to do it: %q", name, said)
		}
	}
	if listed := world.call(t, world.ada, `query { ListSkillShares `+skillShareFields+` }`)["ListSkillShares"].([]any); len(listed) != 0 {
		t.Fatalf("a refused request stored a share: %v", listed)
	}

	// Asking every time takes nothing, from anywhere.
	if data, failures := world.askWith(t, world.ada, models.CredentialToken, askEveryTime); len(failures) > 0 {
		t.Fatalf("a share that asks every time from a command line token: %v %v", data, failures)
	}
	// Turning that off, with limits, is lending.
	world.askWith(t, world.ada, models.CredentialToken, limits(10000, 40000, ""))
	if share := world.shareOf(t); share["isApprovalRequired"] != true {
		t.Errorf("a command line token turned asking every time off: %v", share)
	}
	// The phone app and the dashboard may.
	for _, credentialKind := range []models.CredentialKind{models.CredentialApp, models.CredentialSession} {
		if data, failures := world.askWith(t, world.ada, credentialKind, limits(10000, 40000, "")); len(failures) > 0 {
			t.Fatalf("%s lent: %v %v", credentialKind, data, failures)
		}
	}
	if share := world.shareOf(t); share["isApprovalRequired"] != false || share["paymentLimitMinor"].(float64) != 10000 {
		t.Fatalf("the share a tap made: %v", share)
	}

	// Raising is lending: a higher limit, no limit where there was one, a
	// tool that was not shared.
	for name, query := range map[string]string{
		"a higher payment limit": limits(20000, 40000, ""),
		"a higher monthly limit": limits(10000, 90000, ""),
		"no monthly limit":       world.shareQuery(`["pay"]`, world.ben.ID, `, paymentLimitMinor: 10000, currencyCode: "usd"`),
		"another tool":           world.shareQuery(`["pay", "spend_requests"]`, world.ben.ID, `, paymentLimitMinor: 10000, monthlyLimitMinor: 40000, currencyCode: "usd"`),
	} {
		_, failures := world.askWith(t, world.ada, models.CredentialToken, query)
		if said := strings.Join(failures, "; "); !strings.Contains(said, api.TapRequiredPhrase) {
			t.Errorf("%s from a command line token: %q", name, said)
		}
	}
	if share := world.shareOf(t); share["paymentLimitMinor"].(float64) != 10000 || share["monthlyLimitMinor"].(float64) != 40000 || len(share["toolNames"].([]any)) != 1 {
		t.Errorf("a refused request changed the share: %v", share)
	}

	// Lowering takes away, so it is open.
	if data, failures := world.askWith(t, world.ada, models.CredentialToken, limits(5000, 40000, "")); len(failures) > 0 {
		t.Fatalf("lowering a limit from a command line token: %v %v", data, failures)
	}
	if share := world.shareOf(t); share["paymentLimitMinor"].(float64) != 5000 {
		t.Errorf("the lowered share: %v", share)
	}
	// And so does asking every time, from the agent's own operations too.
	operations := &agentOperations{
		graph: world.graph, user: world.ada,
		permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}, {Permission: models.PermissionCircleUse}}),
	}
	var shared map[string]any
	if err := operations.Execute(context.Background(), limits(90000, 90000, ""), nil, &shared); err == nil || !strings.Contains(err.Error(), api.TapRequiredPhrase) {
		t.Errorf("the agent raised the limits: %v %v", shared, err)
	}
	if err := operations.Execute(context.Background(), askEveryTime, nil, &shared); err != nil {
		t.Errorf("the agent shared so that every call asks: %s", err)
	}
	if share := world.shareOf(t); share["isApprovalRequired"] != true {
		t.Errorf("the share after the agent asked every time: %v", share)
	}
}

// Approving a call spends the owner's money on the owner's computer, and is a
// person's tap. A command line token is refused with where to approve it and
// the call stays waiting; the same token may decline, which only says no.
func TestApprovingACallIsOnlyAcceptedFromATap(t *testing.T) {
	world := newSkillShareWorld(t)
	shareId := world.shareWithBen(t)

	callId := world.waitingCall(t, shareId)
	for name, credentialKind := range map[string]models.CredentialKind{"a command line token": models.CredentialToken, "no credential": ""} {
		_, failures := world.askWith(t, world.ada, credentialKind, world.answer(callId, true))
		if said := strings.Join(failures, "; "); !strings.Contains(said, api.TapRequiredPhrase) {
			t.Errorf("%s approving: %q", name, said)
		}
	}
	if world.computer.ran.Load() != 0 {
		t.Fatalf("a refused approval ran %d commands", world.computer.ran.Load())
	}
	if waiting := world.shareOf(t)["waitingCallCount"].(float64); waiting != 1 {
		t.Fatalf("the call is not waiting any more: %v", waiting)
	}
	// The phone app approves.
	data, failures := world.askWith(t, world.ada, models.CredentialApp, world.answer(callId, true))
	if len(failures) > 0 || data["AnswerSkillShareCall"].(map[string]any)["callState"] != "ran" || world.computer.ran.Load() != 1 {
		t.Fatalf("the app's approval: %v %v (%d commands)", data, failures, world.computer.ran.Load())
	}

	// A command line token declines.
	declinedCall := world.waitingCall(t, shareId)
	data, failures = world.askWith(t, world.ada, models.CredentialToken, world.answer(declinedCall, false))
	if len(failures) > 0 || data["AnswerSkillShareCall"].(map[string]any)["callState"] != "declined" {
		t.Fatalf("declining from a command line token: %v %v", data, failures)
	}
	// The dashboard approves.
	sessionCall := world.waitingCall(t, shareId)
	data, failures = world.askWith(t, world.ada, models.CredentialSession, world.answer(sessionCall, true))
	if len(failures) > 0 || data["AnswerSkillShareCall"].(map[string]any)["callState"] != "ran" || world.computer.ran.Load() != 2 {
		t.Fatalf("the dashboard's approval: %v %v (%d commands)", data, failures, world.computer.ran.Load())
	}
	world.worker.Wait()
}

// The owner's record of what was asked of their money does not go when the
// share does, whether they took it back or the other person left the circle:
// the calls are listed as those of a share that ended, the owner sees every
// one and the other person their own, nobody else any, and the call that was
// waiting is expired. A share made again between the two starts the month with
// what the earlier one spent.
func TestTheOwnersRecordOfCallsSurvivesTheShareEnding(t *testing.T) {
	world := newSkillShareWorld(t)
	shareId := world.shareWithBen(t)
	ranId := world.waitingCall(t, shareId)
	world.call(t, world.ada, world.answer(ranId, true))
	waitingId := world.waitingCall(t, shareId)
	world.worker.Wait()

	// The other person leaves the circle they shared; the share ends with it.
	world.call(t, world.ben, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, world.home))
	if listed := world.call(t, world.ada, `query { ListSkillShares `+skillShareFields+` }`)["ListSkillShares"].([]any); len(listed) != 0 {
		t.Fatalf("the share is still listed: %v", listed)
	}
	listEnded := func(user *models.User, extra string) []any {
		t.Helper()
		return world.call(t, user, `query { ListSkillShareCalls(isShareEnded: true `+extra+`) { id skillName toolName callState amountMinor isShareEnded skillShareId granteeUsername ownerName } }`)["ListSkillShareCalls"].([]any)
	}
	ended := listEnded(world.ada, "")
	states := map[string]string{}
	for _, each := range ended {
		call := each.(map[string]any)
		states[call["id"].(string)] = call["callState"].(string)
		if call["isShareEnded"] != true || call["skillShareId"] != "" || call["skillName"] != "payments" || call["granteeUsername"] != world.ben.Username || call["ownerName"] == "" {
			t.Errorf("an ended call does not say what it was a call of: %v", call)
		}
	}
	if len(ended) != 2 || states[ranId] != "ran" || states[waitingId] != "expired" {
		t.Fatalf("the owner is shown %v", states)
	}
	if only := listEnded(world.ada, `, callState: "ran"`); len(only) != 1 {
		t.Errorf("a state narrows the ended calls: %v", only)
	}
	if theirs := listEnded(world.ben, ""); len(theirs) != 2 {
		t.Errorf("the other person is shown %v", theirs)
	}
	if rows := listEnded(world.cora, ""); len(rows) != 0 {
		t.Errorf("a stranger is shown %v", rows)
	}
	world.refused(t, world.eve, `query { ListSkillShareCalls(isShareEnded: true) { id } }`)
	// The ended calls are not a share's calls, and say so when asked wrongly.
	world.refused(t, world.ada, fmt.Sprintf(`query { ListSkillShareCalls(shareId: %q) %s }`, shareId, skillShareCallFields))
	world.refused(t, world.ada, fmt.Sprintf(`query { ListSkillShareCalls(shareId: %q, isShareEnded: true) %s }`, shareId, skillShareCallFields))
	world.refused(t, world.ada, `query { ListSkillShareCalls `+skillShareCallFields+` }`)
	// Nothing waits any more, so nothing can be approved.
	world.refused(t, world.ada, world.answer(waitingId, true))

	// The two share a circle again and a new share carries on the month.
	world.call(t, world.ada, fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: %q, memberRole: "member") }`, world.home, world.ben.Username))
	for _, each := range world.call(t, world.ben, `query { ListCircleInvitations { id } }`)["ListCircleInvitations"].([]any) {
		world.call(t, world.ben, fmt.Sprintf(`mutation { AnswerCircleInvitation(invitationId: %q, isAccepted: true) { id } }`, each.(map[string]any)["id"]))
	}
	data := world.call(t, world.ada, world.shareQuery(`["pay"]`, world.ben.ID, `, paymentLimitMinor: 10000, monthlyLimitMinor: 40000, currencyCode: "usd"`))
	if data["ShareSkill"].(map[string]any)["id"] == shareId {
		t.Fatal("the new share has the old identifier")
	}
	listed := world.call(t, world.ada, `query { ListSkillShares { monthTotalMinor } }`)["ListSkillShares"].([]any)
	if len(listed) != 1 || listed[0].(map[string]any)["monthTotalMinor"].(float64) != 2500 {
		t.Errorf("the new share's month so far: %v; want the 2500 the ended share spent", listed)
	}
}
