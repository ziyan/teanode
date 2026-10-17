package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// Sharing a skill is on the API, the command line and the agent's skill tool
// under the same names. The dashboard and the iPhone app call the same
// operations. Answering a call that waits is on the command line and in the
// apps and is not on the tool, by design.
func TestSkillSharingParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.AgentSkillShareQuery](), reflect.TypeFor[apigraph.AgentSkillShareMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}
	table := map[string]struct {
		subcommands []string
		action      string
	}{
		"ListSkillShares":      {[]string{"shares"}, "shares"},
		"ShareSkill":           {[]string{"share"}, "share"},
		"UnshareSkill":         {[]string{"unshare"}, "unshare"},
		"ListSkillShareCalls":  {[]string{"calls"}, "calls"},
		"AnswerSkillShareCall": {[]string{"approve", "decline"}, ""},
	}
	for operation := range operations {
		if _, listed := table[operation]; !listed {
			test.Errorf("the operation %s is not in the parity table", operation)
		}
	}

	commands := map[string]*cli.Command{}
	for _, subcommand := range newAgentSkillCommand().Commands {
		commands[subcommand.Name] = subcommand
	}
	actions := map[string]bool{}
	for _, tool := range tools.Build().All() {
		if tool.Name != "skill" {
			continue
		}
		properties, _ := tool.Parameters["properties"].(map[string]any)
		action, _ := properties["action"].(map[string]any)
		switch enum := action["enum"].(type) {
		case []string:
			for _, name := range enum {
				actions[name] = true
			}
		case []any:
			for _, name := range enum {
				actions[name.(string)] = true
			}
		}
	}
	if len(actions) == 0 {
		test.Fatal("the agent has no skill tool")
	}
	for operation, names := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		for _, subcommand := range names.subcommands {
			if commands[subcommand] == nil {
				test.Errorf("teanode agent skill has no %s", subcommand)
			}
		}
		if names.action != "" && !actions[names.action] {
			test.Errorf("the skill tool has no action %s", names.action)
		}
	}
	// The one thing the tool must not do.
	for name := range actions {
		for _, answering := range []string{"approve", "decline", "answer", "deny", "allow"} {
			if strings.Contains(name, answering) {
				test.Errorf("the skill tool has an action %s that could answer a waiting call", name)
			}
		}
	}
}

// skillSharingServer answers the documents the skill sharing commands send with
// a small world: ben shares a circle with the caller.
func skillSharingServer(test *testing.T) (*httptest.Server, func(string) map[string]any) {
	test.Helper()
	var lock sync.Mutex
	calls := map[string]map[string]any{}
	const call = `{"id":"call-1","skillShareId":"s1","skillName":"payments","granteeUserId":"u-ben","granteeUsername":"ben","granteeName":"Ben","ownerName":"Ada",
		"toolName":"pay","toolArguments":"{}","requestReason":"the weekly groceries","amountMinor":14000,"currencyCode":"usd","callState":"waiting_for_approval",
		"toolOutput":"","decidedAt":null,"decisionNote":"","expiresAt":null,"createdAt":"2026-10-17T10:00:00Z"}`
	answers := map[string]string{
		"ListCirclePeople": `[{"userId":"u-ben","username":"ben","name":"Ben"}]`,
		"ShareSkill": `{"id":"s1","skillName":"payments","toolNames":["pay"],"granteeUsername":"ben","paymentLimitMinor":10000,"monthlyLimitMinor":40000,"currencyCode":"usd",
			"isApprovalRequired":false,"isOwnedByCaller":true,"waitingCallCount":0,"monthTotalMinor":0,"createdAt":"2026-10-17T10:00:00Z"}`,
		"ListSkillShares": `[{"id":"s1","skillName":"payments","toolNames":["pay"],"granteeUsername":"ben","paymentLimitMinor":10000,"monthlyLimitMinor":40000,"currencyCode":"usd",
			"isApprovalRequired":false,"isOwnedByCaller":true,"waitingCallCount":1,"monthTotalMinor":0,"createdAt":"2026-10-17T10:00:00Z"},
			{"id":"s2","skillName":"music","toolNames":["play"],"ownerUsername":"cora","isApprovalRequired":true,"isOwnedByCaller":false,"waitingCallCount":0,"monthTotalMinor":0,"createdAt":"2026-10-17T10:00:00Z"}]`,
		"UnshareSkill":         `true`,
		"ListSkillShareCalls":  `[` + call + `]`,
		"AnswerSkillShareCall": strings.Replace(call, `"callState":"waiting_for_approval"`, `"callState":"ran"`, 1),
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(400)
			return
		}
		data := map[string]json.RawMessage{}
		for operation, answer := range answers {
			if strings.Contains(document.Query, operation+"(") || strings.Contains(document.Query, operation+" ") || strings.Contains(document.Query, operation+"\n") {
				data[operation] = json.RawMessage(answer)
				lock.Lock()
				calls[operation] = document.Variables
				lock.Unlock()
			}
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{"data": data})
	}))
	test.Cleanup(server.Close)
	return server, func(operation string) map[string]any {
		lock.Lock()
		defer lock.Unlock()
		return calls[operation]
	}
}

func runSkillCommand(test *testing.T, server *httptest.Server, arguments ...string) (string, error) {
	test.Helper()
	command := &cli.Command{
		Name:     "fixture",
		Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
		Commands: []*cli.Command{newAgentSkillCommand()},
	}
	var err error
	output := captureStandardOutput(test, func() {
		err = command.Run(test.Context(), append([]string{"fixture", "skill"}, arguments...))
	})
	return output, err
}

// Sharing names a person by username, reads the limits in the currency's own
// units into the minor unit the server holds, and says what it did.
func TestSharingASkillReadsTheLimitsInTheCurrencysUnits(test *testing.T) {
	server, called := skillSharingServer(test)

	output, err := runSkillCommand(test, server, "share", "payments", "Ben", "--tools", "pay", "--payment-limit", "100.00", "--monthly-limit", "400", "--currency", "USD")
	if err != nil {
		test.Fatal(err)
	}
	variables := called("ShareSkill")
	if variables["skillName"] != "payments" || variables["granteeUserId"] != "u-ben" || variables["paymentLimitMinor"] != float64(10000) ||
		variables["monthlyLimitMinor"] != float64(40000) || variables["currencyCode"] != "usd" || variables["isApprovalRequired"] != false {
		test.Errorf("what was sent: %v", variables)
	}
	if tools, _ := variables["toolNames"].([]any); len(tools) != 1 || tools[0] != "pay" {
		test.Errorf("the tools sent: %v", variables["toolNames"])
	}
	if !strings.Contains(output, "100.00 USD a payment") || !strings.Contains(output, "400.00 USD a month") || !strings.Contains(output, "ben") {
		test.Errorf("it says what it did: %q", output)
	}

	if _, err = runSkillCommand(test, server, "share", "payments", "ben", "--tools", "pay,spend_requests", "--ask-every-time"); err != nil {
		test.Fatal(err)
	}
	if variables = called("ShareSkill"); variables["isApprovalRequired"] != true || variables["paymentLimitMinor"] != nil || variables["currencyCode"] != nil {
		test.Errorf("asking every time: %v", variables)
	}
	if tools, _ := called("ShareSkill")["toolNames"].([]any); len(tools) != 2 {
		test.Errorf("the tools sent: %v", called("ShareSkill")["toolNames"])
	}

	for name, arguments := range map[string][]string{
		"a stranger":            {"share", "payments", "stranger", "--tools", "pay", "--ask-every-time"},
		"no tools":              {"share", "payments", "ben"},
		"a limit, no money":     {"share", "payments", "ben", "--tools", "pay", "--payment-limit", "5"},
		"too many digits":       {"share", "payments", "ben", "--tools", "pay", "--payment-limit", "5.001", "--currency", "usd"},
		"a currency that isn't": {"share", "payments", "ben", "--tools", "pay", "--currency", "dollars"},
		"no person":             {"share", "payments", "--tools", "pay"},
	} {
		if _, err := runSkillCommand(test, server, arguments...); err == nil {
			test.Errorf("%s was accepted", name)
		}
	}
}

func TestSkillSharesAndCallsAreListedAndAnswered(test *testing.T) {
	server, called := skillSharingServer(test)

	output, err := runSkillCommand(test, server, "shares")
	if err != nil || !strings.Contains(output, "payments") || !strings.Contains(output, "100.00 USD a payment") || !strings.Contains(output, "cora") || !strings.Contains(output, "asks you every time") {
		test.Errorf("the shares: %q %v", output, err)
	}

	output, err = runSkillCommand(test, server, "calls", "s1", "--waiting")
	if err != nil || !strings.Contains(output, "140.00 USD") || !strings.Contains(output, `"the weekly groceries"`) || !strings.Contains(output, "waiting_for_approval") {
		test.Errorf("the calls: %q %v", output, err)
	}
	if called("ListSkillShareCalls")["callState"] != "waiting_for_approval" || called("ListSkillShareCalls")["shareId"] != "s1" {
		test.Errorf("what was sent: %v", called("ListSkillShareCalls"))
	}

	output, err = runSkillCommand(test, server, "approve", "call-1", "--note", "go ahead")
	if err != nil || !strings.Contains(output, "approved, and it ran on your computer") {
		test.Errorf("approve: %q %v", output, err)
	}
	if variables := called("AnswerSkillShareCall"); variables["callId"] != "call-1" || variables["isApproved"] != true || variables["decisionNote"] != "go ahead" {
		test.Errorf("what approving sent: %v", variables)
	}
	if _, err = runSkillCommand(test, server, "decline", "call-1"); err != nil {
		test.Fatal(err)
	}
	if variables := called("AnswerSkillShareCall"); variables["isApproved"] != false || variables["decisionNote"] != nil {
		test.Errorf("what declining sent: %v", variables)
	}

	if _, err = runSkillCommand(test, server, "unshare", "s1"); err != nil || called("UnshareSkill")["shareId"] != "s1" {
		test.Errorf("unshare: %v %v", err, called("UnshareSkill"))
	}
	for _, arguments := range [][]string{{"approve"}, {"decline"}, {"calls"}, {"unshare"}} {
		if _, err := runSkillCommand(test, server, arguments...); err == nil {
			test.Errorf("%v without an argument was accepted", arguments)
		}
	}
}
