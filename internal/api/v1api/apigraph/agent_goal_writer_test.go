package apigraph

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// asCredential runs one step as the person, signed in with the credential
// kind given, in a transaction of its own: the dashboard's session is a tap on
// a screen, a command line token is not.
func (self *financeFixture) asCredential(test *testing.T, credentialKind models.CredentialKind, run func(ctx context.Context, tx db.Transaction)) {
	test.Helper()
	principal := &api.Principal{User: self.owner, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}}), CredentialKind: credentialKind}
	dbtest.RunTransactionOn(test, self.database, func(tx db.Transaction) {
		run(api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx), tx)
	})
}

// goalWriterOf is who the stored goal says wrote it.
func goalWriterOf(test *testing.T, tx db.Transaction, goalId string) string {
	test.Helper()
	goal, err := tx.GetAgentConversation(goalId)
	if err != nil || goal == nil {
		test.Fatalf("GetAgentConversation: %v %v", goal, err)
	}
	return goal.GoalWrittenBy
}

// A goal started or reworded from the command line is the agent's: the
// agent's own shell holds the same token, so a goal from there cannot be told
// from one the agent made, and its turns must not use Passwords. A tap on the
// dashboard or in the phone app is the person's.
func TestAGoalFromTheCommandLineIsTheAgents(t *testing.T) {
	fixture := newFinanceFixture(t, false)
	var theirsId string
	for name, test := range map[string]struct {
		credentialKind models.CredentialKind
		want           string
	}{
		"the dashboard":       {models.CredentialSession, models.WrittenByPerson},
		"the phone app":       {models.CredentialApp, models.WrittenByPerson},
		"the command line":    {models.CredentialToken, models.WrittenByAgent},
		"a request with none": {"", models.WrittenByAgent},
	} {
		fixture.asCredential(t, test.credentialKind, func(ctx context.Context, tx db.Transaction) {
			started, err := fixture.resolver.StartAgentGoal(ctx, StartAgentGoalArguments{GoalTitle: name, GoalDescription: "Tell me what is on today.", GoalTimes: []string{"0 8 * * *"}})
			if err != nil {
				t.Fatal(err)
			}
			if started.GoalWrittenBy != test.want {
				t.Errorf("a goal started from %s is written by %q, want %q", name, started.GoalWrittenBy, test.want)
			}
			if test.credentialKind == models.CredentialSession {
				theirsId = started.ConversationID
			}
		})
	}
	// Pausing the person's goal from the command line takes it from them, as
	// rewording it does.
	fixture.asCredential(t, models.CredentialToken, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.SetAgentGoalState(ctx, SetAgentGoalStateArguments{ConversationID: theirsId, GoalState: "paused"}); err != nil {
			t.Fatal(err)
		}
		if got := goalWriterOf(t, tx, theirsId); got != models.WrittenByAgent {
			t.Errorf("pausing a goal from the command line left the writer %q, want the agent", got)
		}
	})
}

// The person saving a goal's words or times with a tap makes it theirs, the
// agent's goal included; anything a caller that is not a tap changes about
// what it says, when it runs or how it tells them makes it the agent's.
// Renaming it is neither, and the person pausing their own goal leaves it
// theirs.
func TestChangingAGoalKeepsOrTakesItsWriter(t *testing.T) {
	fixture := newFinanceFixture(t, false)
	start := func(credentialKind models.CredentialKind) string {
		var goalId string
		fixture.asCredential(t, credentialKind, func(ctx context.Context, tx db.Transaction) {
			started, err := fixture.resolver.StartAgentGoal(ctx, StartAgentGoalArguments{GoalTitle: "Morning", GoalDescription: "Tell me what is on today.", GoalTimes: []string{"0 8 * * *"}})
			if err != nil {
				t.Fatal(err)
			}
			goalId = started.ConversationID
		})
		return goalId
	}
	words := "Tell me what is on today and tomorrow."
	sameWords := "Tell me what is on today."
	notify := true
	for name, change := range map[string]UpdateAgentGoalArguments{
		"its words":          {GoalDescription: &words},
		"its time":           {GoalTimes: []string{"30 7 * * *"}},
		"the mail it waits":  {GoalMailConditions: []models.MailboxRuleCondition{{Field: "any"}}},
		"how it tells them":  {IsGoalNotifiedWhenPosted: &notify},
		"its words, resent":  {GoalDescription: &sameWords},
		"its time, the same": {GoalTimes: []string{"0 8 * * *"}},
	} {
		agentsId := start(models.CredentialToken)
		fixture.asCredential(t, models.CredentialSession, func(ctx context.Context, tx db.Transaction) {
			change.ConversationID = agentsId
			if _, err := fixture.resolver.UpdateAgentGoal(ctx, change); err != nil {
				t.Fatal(err)
			}
			wanted := models.WrittenByPerson
			if change.IsGoalNotifiedWhenPosted != nil {
				// Only what it says and when it runs is theirs to take.
				wanted = models.WrittenByAgent
			}
			if got := goalWriterOf(t, tx, agentsId); got != wanted {
				t.Errorf("the person changing %s of the agent's goal on a screen: %q, want %q", name, got, wanted)
			}
		})
		theirsId := start(models.CredentialSession)
		fixture.asCredential(t, models.CredentialToken, func(ctx context.Context, tx db.Transaction) {
			change.ConversationID = theirsId
			if _, err := fixture.resolver.UpdateAgentGoal(ctx, change); err != nil {
				t.Fatal(err)
			}
			if got := goalWriterOf(t, tx, theirsId); got != models.WrittenByAgent {
				t.Errorf("changing %s from the command line left the writer %q, want the agent", name, got)
			}
		})
	}
	theirsId := start(models.CredentialSession)
	fixture.asCredential(t, models.CredentialToken, func(ctx context.Context, tx db.Transaction) {
		renamed := "Early"
		if _, err := fixture.resolver.UpdateAgentGoal(ctx, UpdateAgentGoalArguments{ConversationID: theirsId, GoalTitle: &renamed}); err != nil {
			t.Fatal(err)
		}
		if got := goalWriterOf(t, tx, theirsId); got != models.WrittenByPerson {
			t.Errorf("renaming from the command line made the writer %q", got)
		}
	})
	fixture.asCredential(t, models.CredentialSession, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.SetAgentGoalState(ctx, SetAgentGoalStateArguments{ConversationID: theirsId, GoalState: "paused"}); err != nil {
			t.Fatal(err)
		}
		if got := goalWriterOf(t, tx, theirsId); got != models.WrittenByPerson {
			t.Errorf("the person pausing their own goal made the writer %q", got)
		}
	})
}

// The goal the daily brief's switch starts is recorded as built in, not as
// the person's: it reads the inbox, so a turn it wakes does not count as the
// person asking for Passwords (it is still handed to the turn as their words).
// Moving its time keeps that; the person rewriting it makes it theirs.
func TestTheBriefSwitchStartsABuiltInGoal(t *testing.T) {
	fixture := newFinanceFixture(t, false)
	fixture.asCredential(t, models.CredentialSession, func(ctx context.Context, tx db.Transaction) {
		brief, err := fixture.resolver.SetAgentBrief(ctx, SetAgentBriefArguments{Enabled: true})
		if err != nil || brief == nil {
			t.Fatalf("SetAgentBrief: %v %v", brief, err)
		}
		if brief.GoalWrittenBy != models.WrittenByBuiltIn || len(brief.GoalTimes) != 1 || brief.GoalTimes[0] != "30 7 * * 1-5" || !brief.IsGoalNotifiedWhenPosted {
			t.Errorf("the brief is %+v", brief)
		}
		moved, err := fixture.resolver.SetAgentBrief(ctx, SetAgentBriefArguments{Enabled: true, At: "08:00"})
		if err != nil || moved.ConversationID != brief.ConversationID || moved.GoalTimes[0] != "0 8 * * 1-5" {
			t.Fatalf("moving the time changes the one brief: %+v %v", moved, err)
		}
		if got := goalWriterOf(t, tx, brief.ConversationID); got != models.WrittenByBuiltIn {
			t.Errorf("moving the time changed the writer to %q", got)
		}
		off, err := fixture.resolver.SetAgentBrief(ctx, SetAgentBriefArguments{Enabled: false})
		if err != nil || off.GoalState != "paused" {
			t.Fatalf("off is paused: %+v %v", off, err)
		}
		words := "Read me my bank balance first thing."
		if _, err := fixture.resolver.UpdateAgentGoal(ctx, UpdateAgentGoalArguments{ConversationID: brief.ConversationID, GoalDescription: &words}); err != nil {
			t.Fatal(err)
		}
		if got := goalWriterOf(t, tx, brief.ConversationID); got != models.WrittenByPerson {
			t.Errorf("words the person wrote are theirs: %q", got)
		}
	})
}

// A goal the agent titled like the brief is not the brief: the switch starts
// a brief of its own and leaves the agent's goal, and its words, the agent's.
func TestTheBriefSwitchDoesNotAdoptTheAgentsLookalike(t *testing.T) {
	fixture := newFinanceFixture(t, false)
	var lookalikeId string
	fixture.asCredential(t, models.CredentialToken, func(ctx context.Context, tx db.Transaction) {
		started, err := fixture.resolver.StartAgentGoal(ctx, StartAgentGoalArguments{
			GoalTitle: BriefName, GoalDescription: "Words the agent wrote.", GoalTimes: []string{"0 6 * * *"},
		})
		if err != nil {
			t.Fatal(err)
		}
		lookalikeId = started.ConversationID
	})
	fixture.asCredential(t, models.CredentialSession, func(ctx context.Context, tx db.Transaction) {
		brief, err := fixture.resolver.SetAgentBrief(ctx, SetAgentBriefArguments{Enabled: true})
		if err != nil || brief == nil || brief.ConversationID == lookalikeId || brief.GoalWrittenBy != models.WrittenByBuiltIn {
			t.Fatalf("the switch adopted the agent's goal: %+v %v", brief, err)
		}
		if got := goalWriterOf(t, tx, lookalikeId); got != models.WrittenByAgent {
			t.Errorf("the agent's lookalike is written by %q", got)
		}
	})
}
