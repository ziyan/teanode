package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/mcp"
	"github.com/ziyan/teanode/internal/models"
)

// anotherInstance is a second Agent on the same database, configuration and
// models as the worker, under another instance name: what a second process of
// the server is.
func anotherInstance(worker *Agent, instanceName string) *Agent {
	settings := *worker.settings
	settings.Instance = instanceName
	settings.Phones = nil
	other := New(&settings)
	other.operations = worker.operations
	return other
}

func countWatchJobs(t *testing.T, database db.Database, agentId string) int64 {
	t.Helper()
	var watchJobCount int64
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		watchJobCount, err = tx.CountAgentJobs(&db.AgentJobFilter{AgentID: agentId, Kinds: []models.AgentJobKind{models.AgentJobWatch}})
		if err != nil {
			t.Fatal(err)
		}
	})
	return watchJobCount
}

// Two instances that both have the computer queue one look per watch and
// interval between them, not one each, even when the first look has already
// finished.
func TestWatchesAreQueuedOncePerIntervalByTheInstancesTogether(t *testing.T) {
	server := (&judgingModel{}).serve(t)
	fixture := newAlertFixtureWith(t, server.URL, "UTC")
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.PutAgentSkill(&models.AgentSkill{Name: "papers", Enabled: true, Content: watchingSkillForTest}); err != nil {
			t.Fatalf("PutAgentSkill: %s", err)
		}
	})
	other := anotherInstance(fixture.worker, "other")
	world := &watchedWorld{now: time.Now()}
	for _, worker := range []*Agent{fixture.worker, other} {
		worker.AttachComputer(fixture.agent.ID, &fakeComputer{agent: worker, agentId: fixture.agent.ID, answers: world.answer}, ComputerIdentity{Name: "laptop", System: "linux"})
	}

	now := time.Now()
	fixture.worker.queueWatching(t.Context(), now)
	if watchJobCount := countWatchJobs(t, fixture.database, fixture.agent.ID); watchJobCount != 2 {
		t.Fatalf("the first instance queued %d looks, not one for each of the two watches", watchJobCount)
	}
	// The looks finish, so that nothing queued stands in the way of a second.
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		jobs, err := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: fixture.agent.ID, Kinds: []models.AgentJobKind{models.AgentJobWatch}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, job := range jobs {
			if _, err := tx.CancelAgentJobs(job.AgentID, ""); err != nil {
				t.Fatal(err)
			}
		}
	})
	other.queueWatching(t.Context(), now.Add(2*time.Minute))
	if watchJobCount := countWatchJobs(t, fixture.database, fixture.agent.ID); watchJobCount != 2 {
		t.Errorf("the second instance queued looks of its own: %d in all", watchJobCount)
	}
}

// holdingRow is the instance the database says holds a computer, as an
// instance asking after it would be told.
func holdingRow(t *testing.T, database db.Database, agentId, computerName string, freshAfter time.Time) string {
	t.Helper()
	var instance string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if instance, err = tx.FindAgentComputerAttachment(agentId, computerName, freshAfter); err != nil {
			t.Fatal(err)
		}
	})
	return instance
}

// A look claimed by an instance the computer is not attached to waits for
// the one it is attached to; with no instance holding it the look is made
// as it always was, once the computer is back.
func TestAWatchWaitsForTheInstanceThatHasTheComputer(t *testing.T) {
	server := (&judgingModel{}).serve(t)
	fixture := newAlertFixtureWith(t, server.URL, "UTC")
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.PutAgentSkill(&models.AgentSkill{Name: "papers", Enabled: true, Content: watchingSkillForTest}); err != nil {
			t.Fatalf("PutAgentSkill: %s", err)
		}
	})
	fixture.worker.settings.Instance = "first"
	world := &watchedWorld{now: time.Now()}
	connection := &fakeComputer{agent: fixture.worker, agentId: fixture.agent.ID, answers: world.answer}
	fixture.worker.AttachComputer(fixture.agent.ID, connection, ComputerIdentity{Name: "laptop", System: "linux"})
	other := anotherInstance(fixture.worker, "other")

	run := fixture.run()
	run.settings = other.settings
	run.Job = &models.AgentJob{AgentID: fixture.agent.ID, Kind: models.AgentJobWatch, SubjectID: watchSubject("papers", "new_mail"), CreatedAt: time.Now().Add(-time.Hour)}
	err := other.runWatch(t.Context(), run)
	var deferral *Deferral
	if !errors.As(err, &deferral) || !isQuietDeferral(err) {
		t.Fatalf("a look on an instance without the computer is deferred quietly, not finished: %v", err)
	}
	if world.count("papers") != 0 {
		t.Errorf("the instance without the computer ran %q", world.commands)
	}

	// The first instance lets the computer go: nothing holds it, and the
	// look is the next one's, as it was before there were two instances.
	fixture.worker.DetachComputer(fixture.agent.ID, connection)
	if err := other.runWatch(t.Context(), run); err != nil {
		t.Errorf("a look for a computer attached nowhere is left to the next look: %v", err)
	}

	// An instance that went away without saying so is told from its
	// heartbeat going stale.
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if err := tx.PutAgentComputerAttachment(fixture.agent.ID, "laptop", "first", time.Now().Add(-2*turnStaleAfter)); err != nil {
			t.Fatal(err)
		}
	})
	if err := other.runWatch(t.Context(), run); err != nil {
		t.Errorf("a look for a computer whose instance has not been heard of is left to the next look: %v", err)
	}
}

// A job for a source whose computer another instance holds waits for that
// instance instead of marking the source as waiting for its computer; with no
// instance holding it the source waits for the computer, as it always did.
func TestASourceWaitsForTheInstanceThatHasItsComputer(t *testing.T) {
	fixture := newAlertFixtureWith(t, "", "UTC")
	fixture.worker.settings.Instance = "first"
	other := anotherInstance(fixture.worker, "other")
	var sourceId string
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		specification := models.AgentKnowledgeSpecification{Computer: "laptop", Path: "/notes"}
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{AgentID: fixture.agent.ID, Kind: models.SourceComputer, Name: "laptop", Specification: specification, RootPath: "notes", Enabled: true})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		sourceId = source.ID
	})
	lastErrorOfSource := func() (string, string) {
		t.Helper()
		var lastError, instance string
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			source, err := tx.GetAgentSource(fixture.agent.ID, sourceId)
			if err != nil {
				t.Fatal(err)
			}
			lastError, instance = source.LastError, source.Instance
		})
		return lastError, instance
	}

	world := &watchedWorld{now: time.Now()}
	connection := &fakeComputer{agent: fixture.worker, agentId: fixture.agent.ID, answers: world.answer}
	fixture.worker.AttachComputer(fixture.agent.ID, connection, ComputerIdentity{Name: "laptop", System: "linux"})
	if instance := holdingRow(t, fixture.database, fixture.agent.ID, "laptop", time.Now().Add(-turnStaleAfter)); instance != "first" {
		t.Fatalf("the database says %q holds the laptop, not the instance it attached to", instance)
	}

	// The job is claimed by the other instance, which has no such computer.
	run := fixture.run()
	run.settings = other.settings
	run.Job = &models.AgentJob{AgentID: fixture.agent.ID, Kind: models.AgentJobIngest, SubjectID: sourceId, CreatedAt: time.Now().Add(-time.Hour)}
	err := other.runIngest(t.Context(), run)
	var deferral *Deferral
	if !errors.As(err, &deferral) || !isQuietDeferral(err) {
		t.Fatalf("a source whose computer is on another instance is deferred quietly: %v", err)
	}
	if lastError, instance := lastErrorOfSource(); lastError != "" || instance != "" {
		t.Errorf("the deferred source was marked, as waiting %q or as held by %q", lastError, instance)
	}

	// Nothing holds the computer any more: the source waits for it.
	fixture.worker.DetachComputer(fixture.agent.ID, connection)
	err = other.runIngest(t.Context(), run)
	if errors.As(err, &deferral) {
		t.Fatalf("a source whose computer is attached nowhere is deferred: %v", err)
	}
	if lastError, _ := lastErrorOfSource(); lastError == "" {
		t.Error("a source whose computer is attached nowhere is not marked as waiting for it")
	}
}

// A computer that attaches again before the old connection's detach has been
// written is still recorded as held, and a detach that leaves nothing attached
// removes the row.
func TestADetachRightAfterAReattachKeepsTheComputerRecorded(t *testing.T) {
	fixture := newAlertFixtureWith(t, "", "UTC")
	fixture.worker.settings.Instance = "first"
	agentId := fixture.agent.ID
	freshAfter := time.Now().Add(-turnStaleAfter)
	identity := ComputerIdentity{Name: "laptop", System: "linux"}
	world := &watchedWorld{now: time.Now()}
	oldConnection := &fakeComputer{agent: fixture.worker, agentId: agentId, answers: world.answer}
	newConnection := &fakeComputer{agent: fixture.worker, agentId: agentId, answers: world.answer}

	fixture.worker.AttachComputer(agentId, oldConnection, identity)
	fixture.worker.AttachComputer(agentId, newConnection, identity)
	// The old connection's detach arrives late and finds the new one there.
	fixture.worker.DetachComputer(agentId, oldConnection)
	if instance := holdingRow(t, fixture.database, agentId, "laptop", freshAfter); instance != "first" {
		t.Errorf("the reconnect was undone by the old connection detaching: %q", instance)
	}
	fixture.worker.DetachComputer(agentId, newConnection)
	if instance := holdingRow(t, fixture.database, agentId, "laptop", freshAfter); instance != "" {
		t.Errorf("a computer attached nowhere is still held by %q", instance)
	}
}

// A row another instance has written is not removed by this instance's
// detaching, and the instance starting forgets only its own rows.
func TestAnInstanceLetsGoOfOnlyItsOwnComputers(t *testing.T) {
	fixture := newAlertFixtureWith(t, "", "UTC")
	fixture.worker.settings.Instance = "first"
	agentId := fixture.agent.ID
	freshAfter := time.Now().Add(-turnStaleAfter)
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if err := tx.PutAgentComputerAttachment(agentId, "laptop", "other", time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := tx.PutAgentComputerAttachment(agentId, "desktop", "first", time.Now()); err != nil {
			t.Fatal(err)
		}
	})
	fixture.worker.recordComputerAttachment(agentId, "laptop")
	if instance := holdingRow(t, fixture.database, agentId, "laptop", freshAfter); instance != "other" {
		t.Errorf("an instance without the laptop took the other's row away: %q", instance)
	}
	fixture.worker.startComputerAttachments()
	t.Cleanup(fixture.worker.cancel)
	if instance := holdingRow(t, fixture.database, agentId, "desktop", freshAfter); instance != "" {
		t.Errorf("an instance starting kept what it held before: %q", instance)
	}
	if instance := holdingRow(t, fixture.database, agentId, "laptop", freshAfter); instance != "other" {
		t.Errorf("an instance starting took another's row away: %q", instance)
	}
}

// A run of the memory check is queued by one caller at a time per agent.
func TestAnEvaluationIsQueuedByOneInstanceAtATime(t *testing.T) {
	fixture := newAlertFixtureWith(t, "", "UTC")
	other := anotherInstance(fixture.worker, "other")
	if err := fixture.database.TransactionContext(t.Context(), func(tx db.Transaction) error {
		isLocked, err := tx.TryAdvisoryLock(evaluationLockKey(fixture.agent))
		if err != nil || !isLocked {
			t.Fatalf("the lock was not free: %v %v", isLocked, err)
		}
		// Another connection, as another instance's.
		err = fixture.database.TransactionContext(t.Context(), func(second db.Transaction) error {
			_, err := other.QueueEvaluation(second, fixture.agent)
			return err
		})
		if err == nil {
			t.Error("a second caller queued a run while the first was deciding")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.TransactionContext(t.Context(), func(tx db.Transaction) error {
		_, err := other.QueueEvaluation(tx, fixture.agent)
		return err
	}); err != nil {
		t.Errorf("with the first done a caller queues a run: %s", err)
	}
}

// The evening question is asked by one instance at a time: the one that finds
// another in the middle of asking leaves the person to it.
func TestTheEveningQuestionIsAskedByOneInstanceAtATime(t *testing.T) {
	fixture := newPhoneFixture(t)
	fixture.askAt(t, "21:00")
	zone := journal.Zone(fixture.owner)
	evening := time.Date(2026, 10, 26, 21, 30, 0, 0, zone)
	if err := fixture.database.TransactionContext(t.Context(), func(tx db.Transaction) error {
		isLocked, err := tx.TryAdvisoryLock(db.LockKey("journal-evening:" + fixture.agent.ID))
		if err != nil || !isLocked {
			t.Fatalf("the lock was not free: %v %v", isLocked, err)
		}
		fixture.ask(t, evening)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if sent := fixture.relay.to("sealed:a-registration"); len(sent) != 0 {
		t.Fatalf("the question was sent while another instance was asking: %d", len(sent))
	}
	fixture.ask(t, evening)
	if sent := fixture.relay.to("sealed:a-registration"); len(sent) != 1 {
		t.Errorf("the question was sent %d times once the other was done", len(sent))
	}
}

// A conversation another instance has taken to describe is left to it.
func TestAConversationIsDescribedByOneInstance(t *testing.T) {
	fixture := newAlertFixtureWith(t, "", "UTC")
	fixture.worker.settings.Registry = &llm.Registry{}
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: fixture.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now().Add(-time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "user", Content: "find the invoice from the plumber"}); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.UpdateAgentConversation(conversation.ID, func(each *models.AgentConversation) error {
			each.LastAt = time.Now().Add(-time.Hour)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		// Taken by another instance a moment ago.
		if isTaken, err := tx.TakeThrottle("describe:"+conversation.ID, describeQuiet, time.Now()); err != nil || !isTaken {
			t.Fatalf("the turn was not free: %v %v", isTaken, err)
		}
	})
	fixture.worker.describeDue(t.Context(), time.Now())
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		found, err := tx.GetAgentConversation(conversation.ID)
		if err != nil || found.DescribedAt != nil {
			t.Errorf("a conversation another instance had taken was described anyway: %+v %v", found, err)
		}
	})
}

// A sweep every instance runs on its own clock is taken once for the whole
// server, and again an interval later.
func TestASweepIsTakenOnceForTheWholeServer(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	take := func(now time.Time) bool {
		t.Helper()
		var isFree bool
		if err := database.Transaction(func(tx db.Transaction) (err error) {
			isFree, err = takeSweep(tx, "example", time.Hour, now)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return isFree
	}
	start := time.Date(2030, 1, 2, 9, 0, 0, 0, time.UTC)
	if !take(start) {
		t.Fatal("the first instance is refused")
	}
	if take(start.Add(30 * time.Minute)) {
		t.Error("a second instance half an hour later takes it too")
	}
	if !take(start.Add(55 * time.Minute)) {
		t.Error("an instance a little before the hour is up is made to wait a whole interval more")
	}
}

// Two turns that find a person's token expired together refresh it once, and
// both end with the new one.
func TestTwoTurnsRefreshAPersonsTokenOnce(t *testing.T) {
	var refreshCount atomic.Int32
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		refreshCount.Add(1)
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(writer).Encode(map[string]any{"access_token": "fresh-access-token", "refresh_token": "next-refresh-token", "expires_in": 3600})
	}))
	defer tokenServer.Close()

	fixture := newAlertFixtureWith(t, "", "UTC")
	fixture.worker.settings.Configuration().Server.Secret = "a secret long enough to seal things with"
	other := anotherInstance(fixture.worker, "other")
	server := &config.AgentMCPServer{Name: "notes", URL: tokenServer.URL, OAuth: config.AgentMCPOAuth{ClientID: "client", AuthorizationURL: tokenServer.URL + "/authorize", TokenURL: tokenServer.URL + "/token"}}
	expired, _ := json.Marshal(mcp.Tokens{AccessToken: "old-access-token", RefreshToken: "old-refresh-token", ExpiresAt: time.Now().Add(-time.Hour)})
	sealed, err := fixture.worker.SealSecret(string(expired))
	if err != nil {
		t.Fatal(err)
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.PutAgentConnection(&models.AgentConnection{AgentID: fixture.agent.ID, ServerName: "notes", Status: models.ConnectionConnected, Tokens: sealed}); err != nil {
			t.Fatal(err)
		}
	})

	var waitGroup sync.WaitGroup
	obtained := make([]string, 4)
	for index, worker := range []*Agent{fixture.worker, other, fixture.worker, other} {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			token, err := worker.personToken(context.Background(), fixture.agent.ID, server)
			if err != nil {
				t.Errorf("personToken: %s", err)
			}
			obtained[index] = token
		}()
	}
	waitGroup.Wait()
	if refreshTotal := refreshCount.Load(); refreshTotal != 1 {
		t.Errorf("the token was refreshed %d times, not once", refreshTotal)
	}
	for index, token := range obtained {
		if token != "fresh-access-token" {
			t.Errorf("turn %d ended with %q", index, token)
		}
	}
}
