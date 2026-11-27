package apigraph

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// oversightWorld is a server with one person who has an agent and several
// conversations, and operators holding each of the permissions that reach them.
type oversightWorld struct {
	*chatWorld

	// observer holds agent:observe alone. actor holds agent:act. member holds
	// neither. Each also holds agent:use except observer, who is only an
	// observer.
	observer, actor, member *models.User

	// The person's conversations.
	mainID, namedID, runID, chatID string
	personAgentID                  string
}

func newOversightWorld(t *testing.T) *oversightWorld {
	t.Helper()
	world := &oversightWorld{chatWorld: newChatWorld(t)}
	now := time.Now()
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		create := func(username string, permissions ...models.Permission) *models.User {
			user, err := tx.CreateUser(&models.User{Username: username, Name: username})
			if err != nil {
				t.Fatalf("CreateUser: %s", err)
			}
			role, err := tx.CreateRole(&models.Role{Name: username + " role", Permissions: permissions})
			if err != nil {
				t.Fatalf("CreateRole: %s", err)
			}
			if _, err := tx.CreateGroup(&models.Group{Name: username + " group", UserIDs: []string{user.ID}, RoleIDs: []string{role.ID}}); err != nil {
				t.Fatalf("CreateGroup: %s", err)
			}
			return user
		}
		world.observer = create("oversight-observer", models.PermissionAgentObserve)
		world.actor = create("oversight-actor", models.PermissionAgentAct, models.PermissionAgentUse)
		world.member = create("oversight-member", models.PermissionAgentUse)

		personAgent, err := tx.GetAgentByUser(world.frank.ID)
		if err != nil || personAgent == nil {
			t.Fatalf("GetAgentByUser: %v %v", personAgent, err)
		}
		world.personAgentID = personAgent.ID
		makeConversation := func(conversation *models.AgentConversation) string {
			lastAt := conversation.LastAt
			conversation.AgentID = personAgent.ID
			created, err := tx.CreateAgentConversation(conversation)
			if err != nil {
				t.Fatalf("CreateAgentConversation: %s", err)
			}
			if conversation.Kind == models.AgentConversationMain || conversation.Kind == models.AgentConversationRun {
				if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: created.ID, Role: "user", Content: "hello there"}); err != nil {
					t.Fatalf("AppendAgentMessage: %s", err)
				}
			}
			// A conversation is made, and written in, with the time it is
			// made at.
			if _, err := tx.UpdateAgentConversation(created.ID, func(each *models.AgentConversation) error {
				each.LastAt = lastAt
				return nil
			}); err != nil {
				t.Fatalf("UpdateAgentConversation: %s", err)
			}
			return created.ID
		}
		world.mainID = makeConversation(&models.AgentConversation{Kind: models.AgentConversationMain, Title: "Main talk", LastAt: now.Add(-time.Hour)})
		world.namedID = makeConversation(&models.AgentConversation{Kind: models.AgentConversationNamed, Title: "Trip planning", LastAt: now.Add(-48 * time.Hour)})
		world.runID = makeConversation(&models.AgentConversation{Kind: models.AgentConversationRun, JobKind: "triage", Title: "Sorted a message", LastAt: now.Add(-2 * time.Hour)})
		world.chatID = makeConversation(&models.AgentConversation{Kind: models.AgentConversationChat, Title: "Between people", LastAt: now})
	})
	return world
}

func (self *oversightWorld) listedIDs(t *testing.T, as *models.User, arguments string) (map[string]map[string]any, int) {
	t.Helper()
	data := self.call(t, as, fmt.Sprintf(`query { ListAllAgentRuns(%s) { totalCount runs { id kind title ownerUsername ownerName isRunning } } }`, arguments))
	page := data["ListAllAgentRuns"].(map[string]any)
	rows := map[string]map[string]any{}
	for _, each := range page["runs"].([]any) {
		row := each.(map[string]any)
		rows[row["id"].(string)] = row
	}
	return rows, int(page["totalCount"].(float64))
}

func TestObserveListsEveryConversationButChatsBetweenPeople(t *testing.T) {
	world := newOversightWorld(t)
	for _, as := range []*models.User{world.observer, world.actor} {
		rows, totalCount := world.listedIDs(t, as, `agentId: "`+world.personAgentID+`"`)
		if _, isListed := rows[world.chatID]; isListed {
			t.Errorf("%s was shown a chat between people", as.Username)
		}
		for _, conversationID := range []string{world.mainID, world.namedID, world.runID} {
			if _, isListed := rows[conversationID]; !isListed {
				t.Errorf("%s was not shown %s: %v", as.Username, conversationID, rows)
			}
		}
		if totalCount != 3 {
			t.Errorf("%s was told %d in all; want 3", as.Username, totalCount)
		}
		if row := rows[world.mainID]; row["ownerUsername"] != world.frank.Username || row["ownerName"] != world.frank.Name || row["isRunning"] != false {
			t.Errorf("the main conversation's row is %v", row)
		}
	}
}

func TestObserveNarrowsByKindRangeAndRunning(t *testing.T) {
	world := newOversightWorld(t)
	agentArgument := `agentId: "` + world.personAgentID + `", `

	rows, totalCount := world.listedIDs(t, world.observer, agentArgument+`kinds: ["main"]`)
	if len(rows) != 1 || totalCount != 1 || rows[world.mainID] == nil {
		t.Errorf("kind main listed %v (%d)", rows, totalCount)
	}
	rows, _ = world.listedIDs(t, world.observer, agentArgument+`kinds: ["named", "triage"]`)
	if len(rows) != 2 || rows[world.namedID] == nil || rows[world.runID] == nil {
		t.Errorf("kinds named and triage listed %v", rows)
	}
	rows, _ = world.listedIDs(t, world.observer, agentArgument+`kinds: ["reply"]`)
	if len(rows) != 0 {
		t.Errorf("a run kind nothing has listed %v", rows)
	}

	since := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	rows, totalCount = world.listedIDs(t, world.observer, agentArgument+`since: "`+since+`"`)
	if len(rows) != 2 || totalCount != 2 || rows[world.namedID] != nil {
		t.Errorf("since a day ago listed %v (%d)", rows, totalCount)
	}
	until := time.Now().Add(-90 * time.Minute).UTC().Format(time.RFC3339)
	rows, _ = world.listedIDs(t, world.observer, agentArgument+`since: "`+since+`", until: "`+until+`"`)
	if len(rows) != 1 || rows[world.runID] == nil {
		t.Errorf("between a day and an hour and a half ago listed %v", rows)
	}
	world.refused(t, world.observer, `query { ListAllAgentRuns(since: "yesterday") { totalCount } }`)

	// Only a turn in flight counts: one not cut and heard of lately.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		cutAt := time.Now()
		for _, turn := range []*models.AgentTurn{
			{RunID: "oversight-running", ConversationID: world.mainID, Instance: "first", StartedAt: time.Now(), HeartbeatAt: time.Now()},
			{RunID: "oversight-cut", ConversationID: world.namedID, Instance: "first", StartedAt: time.Now(), HeartbeatAt: time.Now(), CutAt: &cutAt},
			{RunID: "oversight-quiet", ConversationID: world.runID, Instance: "second", StartedAt: time.Now().Add(-time.Hour), HeartbeatAt: time.Now().Add(-10 * time.Minute)},
		} {
			if err := tx.PutAgentTurn(turn); err != nil {
				t.Fatalf("PutAgentTurn: %s", err)
			}
		}
	})
	rows, totalCount = world.listedIDs(t, world.observer, agentArgument+`isRunningOnly: true`)
	if len(rows) != 1 || totalCount != 1 || rows[world.mainID] == nil || rows[world.mainID]["isRunning"] != true {
		t.Errorf("running only listed %v (%d)", rows, totalCount)
	}
	rows, _ = world.listedIDs(t, world.observer, agentArgument)
	if rows[world.namedID]["isRunning"] != false || rows[world.runID]["isRunning"] != false {
		t.Errorf("a cut turn and a quiet one are not running: %v", rows)
	}
}

func TestWithoutObserveOrActNothingOfOtherPeopleIsListedOrRead(t *testing.T) {
	world := newOversightWorld(t)
	for _, query := range []string{
		`query { ListAllAgentRuns { totalCount } }`,
		`query { ListFailedAgentTurns { totalCount } }`,
		fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { total } }`, world.mainID),
	} {
		world.refused(t, world.member, query)
		world.refused(t, world.grace, query)
	}
}

func TestObserveReadsAConversationAndNeverAChatBetweenPeople(t *testing.T) {
	world := newOversightWorld(t)
	for _, as := range []*models.User{world.observer, world.actor} {
		data := world.call(t, as, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { total actingAs isObserved messages { content } } }`, world.mainID))
		view := data["ReadAgentConversation"].(map[string]any)
		if view["isObserved"] != true || view["actingAs"] != world.frank.Username || view["total"].(float64) != 1 {
			t.Errorf("%s reads %v", as.Username, view)
		}
		world.call(t, as, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { total } }`, world.runID))
		world.refused(t, as, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { total } }`, world.chatID))
	}

	// The person reads their own and is not observed.
	data := world.call(t, world.frank, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { isObserved actingAs } }`, world.mainID))
	if view := data["ReadAgentConversation"].(map[string]any); view["isObserved"] != false {
		t.Errorf("the owner reads %v", view)
	}
}

func TestObserveWritesOneAuditRowPerReaderConversationAndTenMinutes(t *testing.T) {
	world := newOversightWorld(t)
	read := func(as *models.User, conversationID string) {
		world.call(t, as, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q, first: 1) { total } }`, conversationID))
	}
	rowsOf := func(conversationID string) []*models.AuditEvent {
		var events []*models.AuditEvent
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			var err error
			if events, err = tx.ListAuditEvents(&db.AuditOptions{ResourceType: string(models.AuditResourceAgentConversation), ResourceID: conversationID, Limit: 50}); err != nil {
				t.Fatalf("ListAuditEvents: %s", err)
			}
		})
		return events
	}

	// Paging the same transcript is one row.
	for range 4 {
		read(world.observer, world.mainID)
	}
	events := rowsOf(world.mainID)
	if len(events) != 1 {
		t.Fatalf("four reads wrote %d rows; want 1", len(events))
	}
	if event := events[0]; event.Action != models.AuditActionObserveConversation || event.ActorUserID != world.observer.ID ||
		!strings.Contains(string(event.After), world.frank.ID) {
		t.Errorf("the row is %+v", event)
	}

	// Another reader, and another conversation, are rows of their own.
	read(world.actor, world.mainID)
	read(world.observer, world.runID)
	if count := len(rowsOf(world.mainID)); count != 2 {
		t.Errorf("two readers wrote %d rows; want 2", count)
	}
	if count := len(rowsOf(world.runID)); count != 1 {
		t.Errorf("a second conversation wrote %d rows; want 1", count)
	}

	// Ten minutes later the same reader is written again.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		conversation, err := tx.GetAgentConversation(world.mainID)
		if err != nil {
			t.Fatal(err)
		}
		isRecorded, err := tx.RecordConversationObserved(conversation, world.observer.ID, world.frank.ID, 10*time.Minute, time.Now().Add(11*time.Minute))
		if err != nil || !isRecorded {
			t.Fatalf("a read after the interval was recorded: %v %v", isRecorded, err)
		}
	})

	// Reading one's own writes nothing.
	world.call(t, world.frank, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { total } }`, world.namedID))
	if count := len(rowsOf(world.namedID)); count != 0 {
		t.Errorf("the owner's own read wrote %d rows", count)
	}
}

func TestObserveAloneCannotWriteInAPersonsConversation(t *testing.T) {
	world := newOversightWorld(t)
	want := "Observing is read-only: only the person, or an operator with agent:act, can write here."
	// The observer may also use their own agent, which is what the writing
	// paths ask first.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		role, err := tx.CreateRole(&models.Role{Name: "observer who may use", Permissions: []models.Permission{models.PermissionAgentObserve, models.PermissionAgentUse}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "observer who may use group", UserIDs: []string{world.observer.ID}, RoleIDs: []string{role.ID}}); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.CreateAgent(&models.Agent{UserID: world.observer.ID, Enabled: true}); err != nil {
			t.Fatal(err)
		}
	})
	message := world.refused(t, world.observer, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "as them") { runId } }`, world.mainID))
	if !strings.Contains(message, want) {
		t.Errorf("a send with agent:observe alone was told %q; want %q", message, want)
	}
	var messageID string
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		answer, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: world.mainID, Role: "assistant", Content: "here you go"})
		if err != nil {
			t.Fatal(err)
		}
		messageID = answer.ID
	})
	if message := world.refused(t, world.observer, fmt.Sprintf(`mutation { ReactToAgentMessage(messageId: %q, emoji: "👍") }`, messageID)); !strings.Contains(message, want) {
		t.Errorf("a reaction with agent:observe alone was told %q", message)
	}
}

func TestFailedTurnsAreListedNewestFirstWithTheirOwner(t *testing.T) {
	world := newOversightWorld(t)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		for _, conversationID := range []string{world.namedID, world.mainID, world.chatID} {
			if _, err := tx.AppendAgentMessage(models.NewAgentNote(conversationID, models.NoteFailed, "the provider could not be reached")); err != nil {
				t.Fatal(err)
			}
			time.Sleep(5 * time.Millisecond)
		}
		if _, err := tx.AppendAgentMessage(models.NewAgentNote(world.runID, models.NoteStopped, "")); err != nil {
			t.Fatal(err)
		}
	})
	fields := `{ totalCount failedTurns { conversationId conversationKind conversationTitle ownerUsername ownerName failedAt errorText agentId } }`
	for _, as := range []*models.User{world.observer, world.actor} {
		data := world.call(t, as, `query { ListFailedAgentTurns(first: 10) `+fields+` }`)
		page := data["ListFailedAgentTurns"].(map[string]any)
		turns := page["failedTurns"].([]any)
		if len(turns) != 2 || page["totalCount"].(float64) != 2 {
			t.Fatalf("%s sees %v; want the main and named failures and no chat", as.Username, page)
		}
		newest := turns[0].(map[string]any)
		if newest["conversationId"] != world.mainID || newest["conversationKind"] != "main" || newest["conversationTitle"] != "Main talk" ||
			newest["ownerUsername"] != world.frank.Username || newest["errorText"] != "the provider could not be reached" ||
			newest["agentId"] != world.personAgentID || newest["failedAt"] == "" {
			t.Errorf("the newest failure is %v", newest)
		}
		if turns[1].(map[string]any)["conversationId"] != world.namedID {
			t.Errorf("the older failure is %v", turns[1])
		}
	}

	// A range after the failures is empty, and an agent with none is too.
	since := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	data := world.call(t, world.observer, `query { ListFailedAgentTurns(since: "`+since+`") `+fields+` }`)
	if page := data["ListFailedAgentTurns"].(map[string]any); page["totalCount"].(float64) != 0 || len(page["failedTurns"].([]any)) != 0 {
		t.Errorf("a range after them lists %v", page)
	}
	data = world.call(t, world.observer, `query { ListFailedAgentTurns(agentId: "no-such-agent") `+fields+` }`)
	if page := data["ListFailedAgentTurns"].(map[string]any); page["totalCount"].(float64) != 0 {
		t.Errorf("an agent with none lists %v", page)
	}
	data = world.call(t, world.observer, `query { ListFailedAgentTurns(first: 1, offset: 1) `+fields+` }`)
	if page := data["ListFailedAgentTurns"].(map[string]any); len(page["failedTurns"].([]any)) != 1 || page["totalCount"].(float64) != 2 {
		t.Errorf("the second page is %v", page)
	}
}

// observedRowCount is how many audit rows say the operator read the
// conversation.
func (self *oversightWorld) observedRowCount(t *testing.T, conversationID string) int {
	t.Helper()
	var count int
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		events, err := tx.ListAuditEvents(&db.AuditOptions{ResourceType: string(models.AuditResourceAgentConversation), ResourceID: conversationID, Limit: 50})
		if err != nil {
			t.Fatalf("ListAuditEvents: %s", err)
		}
		for _, event := range events {
			if event.Action == models.AuditActionObserveConversation {
				count++
			}
		}
	})
	return count
}

// giveAnAgent gives an operator an agent of their own, which following a
// conversation or asking about its links needs besides the permission to
// observe.
func (self *oversightWorld) giveAnAgent(t *testing.T, user *models.User) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		if _, err := tx.CreateAgent(&models.Agent{UserID: user.ID, Enabled: true, Name: "Operator agent"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
}

func TestObserveSubscribingToConversationEventsWritesAnAuditRow(t *testing.T) {
	world := newOversightWorld(t)
	world.giveAnAgent(t, world.actor)
	subscribeAs := func(user *models.User, conversationID string) error {
		principal := &api.Principal{User: user, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentAct}, {Permission: models.PermissionAgentUse}})}
		ctx, cancel := context.WithCancel(api.ContextWithPrincipal(context.Background(), principal))
		defer cancel()
		_, err := world.graph.AgentConversationEvents(ctx, ReadAgentConversationEventsArguments{ConversationID: conversationID})
		return err
	}
	for range 3 {
		if err := subscribeAs(world.actor, world.mainID); err != nil {
			t.Fatalf("AgentConversationEvents: %s", err)
		}
	}
	if count := world.observedRowCount(t, world.mainID); count != 1 {
		t.Errorf("three subscriptions wrote %d rows; want 1", count)
	}
	// The person following their own conversation writes none.
	if err := subscribeAs(world.frank, world.namedID); err != nil {
		t.Fatalf("AgentConversationEvents: %s", err)
	}
	if count := world.observedRowCount(t, world.namedID); count != 0 {
		t.Errorf("the owner's own subscription wrote %d rows", count)
	}
}

func TestObserveALinkPreviewInAPersonsConversationWritesAnAuditRow(t *testing.T) {
	world := newOversightWorld(t)
	world.giveAnAgent(t, world.actor)
	for range 2 {
		world.call(t, world.actor, fmt.Sprintf(`query { ReadLinkPreviews(conversationId: %q, urls: ["https://shop.example/chair"]) { url } }`, world.mainID))
	}
	if count := world.observedRowCount(t, world.mainID); count != 1 {
		t.Errorf("two link previews wrote %d rows; want 1", count)
	}
	world.call(t, world.frank, fmt.Sprintf(`query { ReadLinkPreviews(conversationId: %q, urls: ["https://shop.example/chair"]) { url } }`, world.namedID))
	if count := world.observedRowCount(t, world.namedID); count != 0 {
		t.Errorf("the owner's own link preview wrote %d rows", count)
	}
}
