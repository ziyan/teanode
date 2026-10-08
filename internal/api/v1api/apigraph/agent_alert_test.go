package apigraph

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// alertAPIFixture is a person with an agent that is on, and the resolver
// they call.
func alertAPIFixture(t *testing.T) (db.Database, *graph, *api.Principal, *models.Agent) {
	t.Helper()
	database, release := dbtest.AcquireDatabase(t)
	t.Cleanup(release)
	var person *models.User
	var created *models.Agent
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if person, err = tx.CreateUser(&models.User{Username: "listener", Name: "Robin Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if created, err = tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	configuration := config.Default()
	configuration.Agent.Enabled = true
	resolver := &graph{database: database, config: config.NewMemoryStore(configuration), settings: &api.Settings{}}
	principal := &api.Principal{User: person, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	return database, resolver, principal, created
}

func asListener(principal *api.Principal, tx db.Transaction) context.Context {
	return api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx)
}

// A new agent alerts, with the default night and day's most; what the
// person sets is what is read back, and a night nobody can read is
// refused.
func TestAlertSettingsRoundTrip(t *testing.T) {
	database, resolver, principal, created := alertAPIFixture(t)
	if !created.IsAlertsEnabled || created.AlertQuietStart != "" || created.AlertDailyMost != 0 || created.EffectiveAlertDailyMost() != 5 {
		t.Fatalf("a new agent alerts, with the defaults: %+v", created)
	}
	start, end := created.AlertQuietHours()
	if start != "22:00" || end != "07:00" {
		t.Fatalf("the default night is ten to seven: %s to %s", start, end)
	}
	isOff, quietStart, quietEnd, dailyMost := false, "21:30", "06:00", 3
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		view, err := resolver.UpdateAgent(asListener(principal, tx), UpdateAgentArguments{
			IsAlertsEnabled: &isOff, AlertQuietStart: &quietStart, AlertQuietEnd: &quietEnd, AlertDailyMost: &dailyMost,
		})
		if err != nil {
			t.Fatalf("UpdateAgent: %s", err)
		}
		if view.Agent.IsAlertsEnabled || view.Agent.AlertQuietStart != "21:30" || view.Agent.AlertQuietEnd != "06:00" || view.Agent.AlertDailyMost != 3 {
			t.Fatalf("what was set is read back: %+v", view.Agent)
		}
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		stored, err := tx.GetAgent(created.ID)
		if err != nil || stored.IsAlertsEnabled || stored.AlertQuietStart != "21:30" || stored.AlertDailyMost != 3 {
			t.Fatalf("and kept: %+v %v", stored, err)
		}
	})
	for _, refused := range []UpdateAgentArguments{{AlertQuietStart: new("late")}, {AlertDailyMost: new(-1)}} {
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			if _, err := resolver.UpdateAgent(asListener(principal, tx), refused); err == nil {
				t.Fatalf("refused: %+v", refused)
			}
		})
	}
}

// Muted from an alert, by default and by a scope the person names, or by
// a target alone, listed, and taken back; an alert listed with what it
// covered and what a Mute of it offers.
func TestAlertMutesThroughTheAPI(t *testing.T) {
	database, resolver, principal, created := alertAPIFixture(t)
	var alert *models.AgentAlert
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: principal.User.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		mail, err := tx.CreateMail(&models.Mail{Subject: "Your sign-in code is 123456", From: "no-reply@photos.example.com", Kind: models.MailKindIncoming}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		candidate, err := tx.CreateAgentAlertCandidate(&models.AgentAlertCandidate{AgentID: created.ID, MailboxID: mailbox.ID, MailID: mail.ID, CandidateKind: models.AlertCandidateBurst, BurstKey: "no-reply@photos.example.com|your sign-in code is", BurstCount: 6})
		if err != nil {
			t.Fatalf("CreateAgentAlertCandidate: %s", err)
		}
		if alert, err = tx.CreateAgentAlert(&models.AgentAlert{AgentID: created.ID, SubjectKey: "photo app sign-in codes", AlertText: "Someone is asking for your codes.", CandidateIDs: []string{candidate.ID}}); err != nil {
			t.Fatalf("CreateAgentAlert: %s", err)
		}
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ctx := asListener(principal, tx)
		alerts, err := resolver.ListAgentAlerts(ctx, ListAgentAlertsArguments{})
		if err != nil || len(alerts) != 1 || alerts[0].ID != alert.ID || len(alerts[0].Covered) != 1 || alerts[0].Covered[0].CandidateKind != "burst" {
			t.Fatalf("ListAgentAlerts: %+v %v", alerts, err)
		}
		if choices := alerts[0].MuteChoices; len(choices) == 0 || choices[0].MuteScope != models.AlertMuteSubjectKey || choices[0].MuteTarget != "no-reply@photos.example.com|your sign-in code is" {
			t.Fatalf("a Mute offers the burst first: %+v", choices)
		}
		bySubject, err := resolver.MuteAgentAlert(ctx, MuteAgentAlertArguments{AlertID: alert.ID})
		if err != nil || bySubject.MuteScope != models.AlertMuteSubjectKey || bySubject.MuteTarget != "no-reply@photos.example.com|your sign-in code is" {
			t.Fatalf("muted by its burst, not the model's words: %+v %v", bySubject, err)
		}
		if byKind, err := resolver.MuteAgentAlert(ctx, MuteAgentAlertArguments{AlertID: alert.ID, MuteScope: "kind"}); err != nil || byKind.MuteTarget != "burst" {
			t.Fatalf("muted by its kind: %+v %v", byKind, err)
		}
		if bySender, err := resolver.MuteAgentAlert(ctx, MuteAgentAlertArguments{MuteScope: "sender", MuteTarget: "Offers@Shop.example.com"}); err != nil || bySender.MuteTarget != "offers@shop.example.com" {
			t.Fatalf("muted by a sender named: %+v %v", bySender, err)
		}
		if byTarget, err := resolver.MuteAgentAlert(ctx, MuteAgentAlertArguments{MuteTarget: "lottery.example.net"}); err != nil || byTarget.MuteScope != models.AlertMuteDomain {
			t.Fatalf("a target named alone has its scope read from it: %+v %v", byTarget, err)
		}
		for _, refused := range []MuteAgentAlertArguments{{}, {MuteScope: "sender"}, {MuteScope: "color", MuteTarget: "blue"}, {AlertID: "no-such-alert"}} {
			if _, err := resolver.MuteAgentAlert(ctx, refused); err == nil {
				t.Fatalf("refused: %+v", refused)
			}
		}
		mutes, err := resolver.ListAgentAlertMutes(ctx)
		if err != nil || len(mutes) != 4 {
			t.Fatalf("ListAgentAlertMutes: %+v %v", mutes, err)
		}
		if isUnmuted, err := resolver.UnmuteAgentAlert(ctx, UnmuteAgentAlertArguments{MuteID: bySubject.ID}); err != nil || !isUnmuted {
			t.Fatalf("UnmuteAgentAlert: %v %v", isUnmuted, err)
		}
		if _, err := resolver.UnmuteAgentAlert(ctx, UnmuteAgentAlertArguments{MuteID: bySubject.ID}); err == nil {
			t.Fatal("a mute taken back is gone")
		}
		if mutes, _ := resolver.ListAgentAlertMutes(ctx); len(mutes) != 3 {
			t.Fatalf("three left: %+v", mutes)
		}
	})
}

// A person chooses the voice their answers are read aloud in, one of the
// provider's, and an empty choice goes back to the server's.
func TestTheSpeakingVoiceIsThePersons(t *testing.T) {
	database, resolver, principal, created := alertAPIFixture(t)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		view, err := resolver.UpdateAgent(asListener(principal, tx), UpdateAgentArguments{SpeechVoice: new(" Cedar ")})
		if err != nil || view.Agent.SpeechVoice != "cedar" {
			t.Fatalf("chosen: %+v %v", view, err)
		}
	})
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		stored, err := tx.GetAgent(created.ID)
		if err != nil || stored.SpeechVoice != "cedar" {
			t.Fatalf("kept: %+v %v", stored, err)
		}
		if _, err := resolver.UpdateAgent(asListener(principal, tx), UpdateAgentArguments{SpeechVoice: new("robot")}); err == nil {
			t.Fatalf("a voice the provider does not have is refused")
		}
		view, err := resolver.UpdateAgent(asListener(principal, tx), UpdateAgentArguments{SpeechVoice: new("")})
		if err != nil || view.Agent.SpeechVoice != "" {
			t.Fatalf("back to the server's: %+v %v", view, err)
		}
	})
	if got := speechVoiceOf(resolver.config.Current(), &models.Agent{SpeechVoice: "onyx"}); got != "onyx" {
		t.Fatalf("the person's voice: %s", got)
	}
	if got := speechVoiceOf(resolver.config.Current(), &models.Agent{}); got != config.VoiceSpeechVoiceDefault {
		t.Fatalf("the server's voice: %s", got)
	}
}
