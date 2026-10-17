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
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// Sharing a skill is on the API, the command line and the agent's skill tool
// under the same names. The dashboard and the iPhone app call the same
// operations. Declining a call that waits is on the command line and in the
// apps and is not on the tool, by design; approving one is in the apps only,
// because the server accepts it from nothing else.
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
		"AnswerSkillShareCall": {[]string{"decline"}, ""},
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
	// Approving is a person's tap, on the dashboard or in the iPhone app, and
	// there is no command that tries it from a command line.
	if commands["approve"] != nil {
		test.Error("teanode agent skill has an approve command, which the server refuses to a command line token")
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
		"toolOutput":"","answeredAt":null,"answerNote":"","expiresAt":null,"createdAt":"2026-10-17T10:00:00Z"}`
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
	if err != nil || !strings.Contains(output, "140.00 USD") || !strings.Contains(output, `"the weekly groceries"`) || !strings.Contains(output, " waiting ") || strings.Contains(output, " waiting_for_approval ") {
		test.Errorf("the calls: %q %v", output, err)
	}
	if called("ListSkillShareCalls")["callState"] != "waiting_for_approval" || called("ListSkillShareCalls")["shareId"] != "s1" {
		test.Errorf("what was sent: %v", called("ListSkillShareCalls"))
	}

	// A call that waits comes with where to approve it.
	if !strings.Contains(output, server.URL+"/settings/agent/connections") {
		test.Errorf("the waiting call does not say where to approve it: %q", output)
	}
	output, err = runSkillCommand(test, server, "calls", "s1")
	if err != nil || strings.Count(output, "/settings/agent/connections") != 1 {
		test.Errorf("one waiting call, one address: %q %v", output, err)
	}

	output, err = runSkillCommand(test, server, "decline", "call-1", "--note", "not today")
	if err != nil || !strings.Contains(output, "declined; Ben was told") {
		test.Errorf("decline: %q %v", output, err)
	}
	if variables := called("AnswerSkillShareCall"); variables["callId"] != "call-1" || variables["isApproved"] != false || variables["answerNote"] != "not today" {
		test.Errorf("what declining sent: %v", variables)
	}

	if _, err = runSkillCommand(test, server, "unshare", "s1"); err != nil || called("UnshareSkill")["shareId"] != "s1" {
		test.Errorf("unshare: %v %v", err, called("UnshareSkill"))
	}
	for _, arguments := range [][]string{{"decline"}, {"calls"}, {"unshare"}} {
		if _, err := runSkillCommand(test, server, arguments...); err == nil {
			test.Errorf("%v without an argument was accepted", arguments)
		}
	}
}

// Lending with limits is the dashboard's and the iPhone app's, and the server
// refuses it to a command line token. The command says so plainly and prints
// where to do it, rather than showing the server's sentence as a failure of
// the arguments.
func TestSharingWithLimitsFromTheCommandLineSaysWhereToDoIt(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(request.Body).Decode(&document)
		response.Header().Set("Content-Type", "application/json")
		if strings.Contains(document.Query, "ListCirclePeople") {
			_, _ = response.Write([]byte(`{"data":{"ListCirclePeople":[{"userId":"u-ben","username":"ben","name":"Ben"}]}}`))
			return
		}
		_, _ = response.Write([]byte(`{"errors":[{"message":"api: permission denied: ` + api.TapRequiredPhrase + `: lending money is done where a person taps"}]}`))
	}))
	test.Cleanup(server.Close)

	_, err := runSkillCommand(test, server, "share", "payments", "ben", "--tools", "pay", "--payment-limit", "100.00", "--currency", "usd")
	if err == nil {
		test.Fatal("the refusal was not reported")
	}
	for _, expected := range []string{"dashboard", "iPhone app", server.URL + "/settings/agent/connections", "--ask-every-time"} {
		if !strings.Contains(err.Error(), expected) {
			test.Errorf("the refusal does not say %q: %s", expected, err)
		}
	}
}

// A share that ended keeps its calls for the owner, listed without naming a
// share.
func TestTheCallsOfSharesThatEndedAreListedWithoutAShare(test *testing.T) {
	server, called := skillSharingServer(test)

	output, err := runSkillCommand(test, server, "calls", "--ended", "--waiting")
	if err != nil || !strings.Contains(output, "payments pay") || !strings.Contains(output, "ben") {
		test.Errorf("the ended calls: %q %v", output, err)
	}
	variables := called("ListSkillShareCalls")
	if variables["callState"] != "waiting_for_approval" || variables["shareId"] != nil {
		test.Errorf("what was sent: %v", variables)
	}
	for name, arguments := range map[string][]string{
		"a share and --ended": {"calls", "s1", "--ended"},
		"neither":             {"calls"},
	} {
		if _, err := runSkillCommand(test, server, arguments...); err == nil {
			test.Errorf("%s was accepted", name)
		}
	}
}

// --state asks for the calls in a state, the operation's callState, and more
// than one is asked for one at a time and put together, newest first; a state
// there is not is refused before anything is sent.
func TestTheCallsOfAShareAreListedByState(test *testing.T) {
	var lock sync.Mutex
	asked := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(request.Body).Decode(&document)
		callState, _ := document.Variables["callState"].(string)
		lock.Lock()
		asked = append(asked, callState)
		lock.Unlock()
		created := map[string]string{"ran": "2026-10-17T09:00:00Z", "declined": "2026-10-17T11:00:00Z"}[callState]
		if created == "" {
			created = "2026-10-17T10:00:00Z"
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":{"ListSkillShareCalls":[{"id":"call-` + callState + `","skillName":"payments","granteeUsername":"ben","toolName":"pay","callState":"` + callState + `","createdAt":"` + created + `"}]}}`))
	}))
	defer server.Close()

	output, err := runSkillCommand(test, server, "calls", "s1", "--state", "ran", "--state", "declined")
	if err != nil {
		test.Fatal(err)
	}
	lock.Lock()
	got := strings.Join(asked, ",")
	lock.Unlock()
	if got != "ran,declined" {
		test.Errorf("the states asked for were %q", got)
	}
	if !strings.Contains(output, "call-declined") || !strings.Contains(output, "call-ran") || strings.Index(output, "call-declined") > strings.Index(output, "call-ran") {
		test.Errorf("both, the newest first:\n%s", output)
	}

	lock.Lock()
	asked = asked[:0]
	lock.Unlock()
	waitingOutput, err := runSkillCommand(test, server, "calls", "s1", "--state", "waiting")
	if err != nil {
		test.Fatal(err)
	}
	lock.Lock()
	got = strings.Join(asked, ",")
	lock.Unlock()
	if got != "waiting_for_approval" {
		test.Errorf("waiting is waiting_for_approval to the operation: %q", got)
	}
	// The state column says waiting, as --state takes it, not waiting_for_approval
	// (which is still in the call's own identifier here).
	if strings.Contains(waitingOutput, " waiting_for_approval ") || !strings.Contains(waitingOutput, " waiting ") {
		test.Errorf("the table names the state as --state takes it:\n%s", waitingOutput)
	}
	lock.Lock()
	asked = asked[:0]
	lock.Unlock()
	if _, err := runSkillCommand(test, server, "calls", "s1", "--state", "stuck"); err == nil || !strings.Contains(err.Error(), "--state") {
		test.Errorf("a state there is not: %v", err)
	}
	lock.Lock()
	defer lock.Unlock()
	if len(asked) != 0 {
		test.Errorf("something was asked for a state there is not: %v", asked)
	}
}
