package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Every tool has a family, a risk class and a schema; the catalog a person
// sees is what their permissions admit minus what the operator switched
// off; and the risk class is the floor of what asks first.
func TestCatalogIsWellFormedAndFiltered(t *testing.T) {
	catalog := NewCatalog()
	registerGeneralTools(catalog)
	registerMailboxTools(catalog)
	for _, tool := range catalog.All() {
		if tool.Family == "" || tool.Risk == "" || tool.Description == "" || tool.Run == nil {
			t.Fatalf("tool %q is missing a family, a risk, a description or a run", tool.Name)
		}
		definition := tool.Definition()
		if definition.Parameters["type"] != "object" {
			t.Fatalf("tool %q has no object schema", tool.Name)
		}
	}
	reader := models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})
	offered := catalog.Offered(reader, &config.AgentTools{})
	names := map[string]bool{}
	for _, tool := range offered {
		names[tool.Name] = true
	}
	if !names["mail_search"] || !names["datetime"] || names["mail_act"] || names["rule_add"] {
		t.Fatalf("a reader should see the reading tools and not the writing ones: %v", names)
	}
	writer := models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}, {Permission: models.PermissionMailWrite}, {Permission: models.PermissionMailboxManage}})
	offered = catalog.Offered(writer, &config.AgentTools{Disabled: []string{"web_fetch", "rule_add"}})
	names = map[string]bool{}
	for _, tool := range offered {
		names[tool.Name] = true
	}
	if !names["mail_act"] || names["web_fetch"] || names["rule_add"] || !names["rule_list"] {
		t.Fatalf("the operator's disabled list should hold: %v", names)
	}
	offered = catalog.Offered(writer, &config.AgentTools{Disabled: []string{"mailbox"}})
	for _, tool := range offered {
		if tool.Family == FamilyMailbox {
			t.Fatalf("a disabled family should leave nothing: %q", tool.Name)
		}
	}

	policy := &config.AgentTools{Confirm: []string{"rule_apply"}}
	person := &models.Agent{Confirm: []string{"mail_draft"}}
	for name, want := range map[string]bool{"mail_send": true, "mail_read": false, "mail_act": false, "folder_manage": false, "rule_apply": true, "mail_draft": true} {
		if got := NeedsConfirmation(catalog.Get(name), nil, policy, person); got != want {
			t.Fatalf("NeedsConfirmation(%s) = %v, want %v", name, got, want)
		}
	}
	// delete_forever and deleting a folder are destructive calls of write
	// tools, and ask.
	if !NeedsConfirmation(catalog.Get("mail_act"), []byte(`{"action":"delete_forever","item_ids":["x"]}`), nil, nil) || NeedsConfirmation(catalog.Get("mail_act"), []byte(`{"action":"archive","item_ids":["x"]}`), nil, nil) {
		t.Fatal("mail_act should ask for delete_forever only")
	}
	if !NeedsConfirmation(catalog.Get("folder_manage"), []byte(`{"action":"delete","folder":"Old"}`), nil, nil) || NeedsConfirmation(catalog.Get("folder_manage"), []byte(`{"action":"rename","folder":"Old","name":"New"}`), nil, nil) {
		t.Fatal("folder_manage should ask for delete only")
	}
}

func TestSplitDefersBeyondTheThresholdAndSearchFinds(t *testing.T) {
	var offered []*Tool
	for index := 0; index < deferralThreshold+5; index++ {
		offered = append(offered, &Tool{Name: "tool_" + string(rune('a'+index%26)) + string(rune('a'+index/26)), Family: FamilyDomains, Description: "does a thing", Core: index < 3})
	}
	offered = append(offered, &Tool{Name: "dns_check", Family: FamilyDomains, Description: "Check the DNS records of a domain."})
	sent, deferred := Split(offered, map[string]bool{}, false)
	if len(sent) != 3 || len(deferred) != len(offered)-3 {
		t.Fatalf("sent %d deferred %d", len(sent), len(deferred))
	}
	found := Search(deferred, "check dns records", 5)
	if len(found) == 0 || found[0].Name != "dns_check" {
		t.Fatalf("search should find dns_check first: %v", found)
	}
	sent, _ = Split(offered, map[string]bool{"dns_check": true}, false)
	if len(sent) != 4 {
		t.Fatalf("a loaded tool is sent: %d", len(sent))
	}
	short := offered[:10]
	sent, deferred = Split(short, nil, false)
	if len(sent) != 10 || len(deferred) != 0 {
		t.Fatal("a short catalog is sent whole")
	}
}

func TestRepairHistoryDropsOrphans(t *testing.T) {
	history := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: "hi"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1", Name: "a"}, {ID: "2", Name: "b"}}},
		{Role: llm.RoleTool, ToolCallID: "1", Content: "one"},
		{Role: llm.RoleTool, ToolCallID: "9", Content: "orphan"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "3", Name: "c"}}},
	}
	repaired := repairHistory(history)
	if len(repaired) != 3 || len(repaired[1].ToolCalls) != 1 || repaired[2].ToolCallID != "1" {
		t.Fatalf("repaired %+v", repaired)
	}
}

func TestDatetimeParsing(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, berlin) // a Thursday
	for phrase, want := range map[string]string{
		"tomorrow 3pm":              "2026-09-11T15:00:00+02:00",
		"next monday":               "2026-09-14T00:00:00+02:00",
		"friday at 9:30":            "2026-09-11T09:30:00+02:00",
		"in 2d":                     "2026-09-12T10:00:00+02:00",
		"2026-10-01 08:00":          "2026-10-01T08:00:00+02:00",
		"today 12am":                "2026-09-10T00:00:00+02:00",
		"2026-12-24T18:00:00+02:00": "2026-12-24T18:00:00+02:00",
	} {
		moment, err := parseTime(phrase, berlin, now)
		if err != nil {
			t.Fatalf("%q: %s", phrase, err)
		}
		if got := moment.Format(time.RFC3339); got != want {
			t.Fatalf("%q = %s, want %s", phrase, got, want)
		}
	}
	if duration, err := parseDuration("2h30m"); err != nil || duration != 2*time.Hour+30*time.Minute {
		t.Fatalf("2h30m = %v %v", duration, err)
	}
	if duration, err := parseDuration("-1w"); err != nil || duration != -7*24*time.Hour {
		t.Fatalf("-1w = %v %v", duration, err)
	}
	if _, err := parsePhrase("whenever", berlin, now); err == nil {
		t.Fatal("nonsense should not parse")
	}
}

func TestAddressMatchesAndHours(t *testing.T) {
	if !addressMatches("maria@example.net", "example.net") || !addressMatches("maria@example.net", "@example.net") || !addressMatches("maria@example.net", "Maria@Example.net") || addressMatches("maria@example.net", "other.example") || !addressMatches("a@mail.example.net", "example.net") {
		t.Fatal("addressMatches")
	}
	hours := &models.AgentHours{From: "09:00", Until: "17:30", Days: []int{1, 2, 3, 4, 5}}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	if !withinHours(hours, time.Date(2026, 9, 10, 10, 0, 0, 0, berlin)) || withinHours(hours, time.Date(2026, 9, 10, 18, 0, 0, 0, berlin)) || withinHours(hours, time.Date(2026, 9, 12, 10, 0, 0, 0, berlin)) {
		t.Fatal("withinHours")
	}
	night := &models.AgentHours{From: "22:00", Until: "06:00"}
	if !withinHours(night, time.Date(2026, 9, 10, 23, 0, 0, 0, berlin)) || withinHours(night, time.Date(2026, 9, 10, 12, 0, 0, 0, berlin)) {
		t.Fatal("withinHours over midnight")
	}
}
