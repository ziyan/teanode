package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// followUpServer answers the follow-up documents, and keeps what it was sent.
func followUpServer(test *testing.T) (*httptest.Server, func() []map[string]any) {
	test.Helper()
	var mutex sync.Mutex
	var sent []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var document struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&document); err != nil {
			response.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		defer mutex.Unlock()
		sent = append(sent, document.Variables)
		response.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(document.Query, "BackfillAgentFollowUps"):
			_, _ = response.Write([]byte(`{"data":{"BackfillAgentFollowUps":{"queuedMessageCount":12}}}`))
		case strings.Contains(document.Query, "StopAgentFollowUp"):
			_, _ = response.Write([]byte(`{"data":{"StopAgentFollowUp":{"id":"follow-1","followUpKind":"bill","followUpTitle":"Water bill","followUpState":"stopped"}}}`))
		case strings.Contains(document.Query, "AddAgentFollowUp"):
			_, _ = response.Write([]byte(`{"data":{"AddAgentFollowUp":{"id":"follow-2","followUpKind":"bill","followUpTitle":"Water bill","followUpState":"open","dueOn":"2026-11-03","amount":"84.20"}}}`))
		case strings.Contains(document.Query, "ListAgentFollowUps"):
			_, _ = response.Write([]byte(`{"data":{"ListAgentFollowUps":[{"id":"follow-2","followUpKind":"bill","followUpTitle":"Water bill","followUpState":"open","dueOn":"2026-11-03","amount":"84.20","currencyCode":"USD","payee":"Example Water"}]}}`))
		default:
			response.WriteHeader(http.StatusBadRequest)
		}
	}))
	test.Cleanup(server.Close)
	return server, func() []map[string]any {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]map[string]any(nil), sent...)
	}
}

// Following a bill sends only what was given, stopping one names it, and
// the list asks for what is still watched unless --all says otherwise.
func TestTheFollowCommandsSendWhatTheDashboardWould(test *testing.T) {
	test.Parallel()
	server, sent := followUpServer(test)

	added := runAgainst(test, server, "follow", "add", "--kind", "bill", "--title", "Water bill", "--payee", "Example Water", "--amount", "84.20", "--currency", "USD", "--due", "2026-11-03")
	if !strings.Contains(added, `follow-2: following bill "Water bill"`) {
		test.Errorf("add printed %q", added)
	}
	if stopped := runAgainst(test, server, "follow", "stop", "follow-1"); !strings.Contains(stopped, `follow-1: stopped following "Water bill"`) {
		test.Errorf("stop printed %q", stopped)
	}
	runAgainst(test, server, "follow", "list", "--json")
	runAgainst(test, server, "follow", "list", "--all", "--json")

	variables := sent()
	if len(variables) != 4 {
		test.Fatalf("sent %+v", variables)
	}
	if variables[0]["followUpKind"] != "bill" || variables[0]["payee"] != "Example Water" || variables[0]["amount"] != "84.20" || variables[0]["dueOn"] != "2026-11-03" || variables[0]["carrier"] != nil || variables[0]["place"] != nil {
		test.Errorf("add sent %+v", variables[0])
	}
	if variables[1]["followUpId"] != "follow-1" {
		test.Errorf("stop sent %+v", variables[1])
	}
	if variables[2]["isOpenOnly"] != true || variables[3]["isOpenOnly"] != false {
		test.Errorf("list sent %+v %+v", variables[2], variables[3])
	}
}

// The look back is asked for by name and says how many messages it queued.
func TestTheBackfillCommandAsksForTheLookBack(test *testing.T) {
	test.Parallel()
	server, sent := followUpServer(test)
	if printed := runAgainst(test, server, "follow", "backfill"); !strings.Contains(printed, "queued 12 messages") {
		test.Errorf("backfill printed %q", printed)
	}
	runAgainst(test, server, "follow", "backfill", "--json")
	if variables := sent(); len(variables) != 2 {
		test.Errorf("sent %+v", variables)
	}
}
