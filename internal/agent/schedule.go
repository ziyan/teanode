package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// A schedule is a prompt the agent runs at times the person chose — "every
// weekday at 8, tell me what needs me today" — in their own zone, with
// nobody present: no confirmation can be given, so nothing that needs one
// runs. The answer goes out by mail or into the conversation.

// OperationsFactory makes the API as a person, for a run nobody started
// from a request. The API package provides it.
type OperationsFactory func(ctx context.Context, owner *models.User) (Operations, error)

// SetOperationsFactory hands the worker the way to act as a person.
func (self *Agent) SetOperationsFactory(factory OperationsFactory) {
	self.operations = factory
}

// NextRun is when a schedule next runs for a person, from now.
func NextRun(schedule *models.AgentSchedule, owner *models.User, now time.Time) (time.Time, error) {
	return nextCron(schedule.Cron, now, Location(owner))
}

// SettleSchedule settles a schedule's time and says when it next runs. A
// distance from now ("@in 20m") becomes the moment it means, so that what
// is stored does not move every time it is read. The agent's own tool has
// always done this; a schedule written from the dashboard or the command
// line was refused instead, being told it wanted five cron fields.
func SettleSchedule(schedule *models.AgentSchedule, owner *models.User, now time.Time) (time.Time, error) {
	location := Location(owner)
	settled, err := resolveRelative(schedule.Cron, now, location)
	if err != nil {
		return time.Time{}, err
	}
	schedule.Cron = settled
	return nextCron(schedule.Cron, now, location)
}

// dueSchedules queues a run for every schedule whose time has come, and
// moves each on to its next time.
func (self *Agent) dueSchedules(ctx context.Context, now time.Time) error {
	configuration := self.settings.Configuration()
	if !FeatureAllowed(configuration, "schedules") {
		return nil
	}
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		due, err := tx.ListDueAgentSchedules(now, 50)
		if err != nil {
			return err
		}
		for _, schedule := range due {
			agent, err := tx.GetAgent(schedule.AgentID)
			if err != nil {
				return err
			}
			var owner *models.User
			if agent != nil {
				owner, err = tx.GetUser(agent.UserID)
				if err != nil {
					return err
				}
			}
			next, nextErr := nextCron(schedule.Cron, now, Location(owner))
			if _, err := tx.UpdateAgentSchedule(schedule.ID, func(schedule *models.AgentSchedule) error {
				schedule.LastRunAt = &now
				if nextErr != nil {
					schedule.Enabled = false
					schedule.NextRunAt = nil
				} else {
					schedule.NextRunAt = &next
				}
				return nil
			}); err != nil {
				return err
			}
			if agent == nil || !agent.Active() || owner == nil {
				continue
			}
			if _, err := self.Enqueue(tx, models.AgentJobSchedule, agent.ID, "", schedule.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

// runSchedule is the handler for a schedule job: a headless turn with the
// schedule's prompt, delivered where the schedule says.
func (self *Agent) runSchedule(ctx context.Context, run *Run) error {
	configuration := run.Configuration()
	if !FeatureAllowed(configuration, "schedules") || !FeatureAllowed(configuration, "ask") {
		return nil
	}
	if self.operations == nil {
		return fmt.Errorf("no way to act as the person")
	}
	var schedule *models.AgentSchedule
	var conversation *models.AgentConversation
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		schedule, err = tx.GetAgentSchedule(run.Job.SubjectID)
		if err != nil || schedule == nil || !schedule.Enabled || schedule.AgentID != run.Agent.ID {
			schedule = nil
			return err
		}
		conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.Agent.ID, Kind: models.AgentConversationRun, Title: "Schedule: " + schedule.Name, JobID: run.Job.ID, JobKind: string(models.AgentJobSchedule), SubjectID: schedule.ID, Surface: "schedule", LastAt: time.Now()})
		return err
	}); err != nil {
		return err
	}
	if schedule == nil {
		return nil
	}
	operations, err := self.operations(ctx, run.Owner)
	if err != nil {
		return err
	}
	surface := "schedule"
	if schedule.Deliver == "mail" {
		surface = "mail"
	}
	turn, err := self.Ask(&AskSettings{Agent: run.Agent, Owner: run.Owner, Operations: operations, Conversation: conversation, Message: schedule.Prompt, Surface: surface, Headless: true, UsageKind: string(models.AgentJobSchedule)})
	if err != nil {
		return err
	}
	events, unsubscribe := turn.Subscribe()
	defer unsubscribe()
	answer := ""
	failure := ""
	for event := range events {
		// The job's own deadline, not the agent's: a turn past its time is
		// stopped here, before another instance is handed the job.
		if ctx.Err() != nil {
			turn.Stop()
			break
		}
		switch event.Kind {
		case EventMessage:
			answer = event.Text
		case EventError:
			failure = event.Error
		}
	}
	if failure != "" {
		return fmt.Errorf("the scheduled turn failed: %s", failure)
	}
	if strings.TrimSpace(answer) == "" {
		return nil
	}
	return self.deliverSchedule(ctx, run, schedule, answer)
}

// deliverSchedule puts the answer where the schedule says: by mail from a
// granted mailbox to the account's notification address, or into the main
// conversation as the agent's word with a note saying where it came from.
func (self *Agent) deliverSchedule(ctx context.Context, run *Run, schedule *models.AgentSchedule, answer string) error {
	if schedule.Deliver == "mail" {
		if run.Owner.Email == "" || self.settings.Mailer == nil {
			return fmt.Errorf("the account has no notification address to mail the answer to")
		}
		var from string
		var mailboxId string
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			mailboxes, err := tx.ListMailboxes(run.Owner.ID)
			if err != nil {
				return err
			}
			for _, mailbox := range mailboxes {
				if mailbox.Agent != nil && mailbox.Agent.Granted && len(mailbox.Addresses) > 0 {
					from, mailboxId = mailbox.Addresses[0].Address, mailbox.ID
					return nil
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if from == "" {
			return fmt.Errorf("no granted mailbox has an address to send from")
		}
		subject := schedule.Name
		body := strings.TrimSpace(answer)
		if first, rest, ok := strings.Cut(body, "\n"); ok && len(first) < 120 {
			subject, body = strings.TrimSpace(first), strings.TrimSpace(rest)
		}
		return self.settings.Mailer.Send(ctx, &mailparse.Envelope{MailboxID: mailboxId}, &mailer.Message{
			From: from, FromName: run.Agent.DisplayName(), To: []string{run.Owner.Email}, Subject: subject, Text: body,
			Headers: []string{mailparse.UnsplitHeader("Auto-Submitted", "auto-generated"), mailparse.UnsplitHeader("X-Auto-Response-Suppress", "All")},
		})
	}
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		main, err := tx.ListAgentConversations(run.Agent.ID, []models.AgentConversationKind{models.AgentConversationMain}, &db.Options{Limit: 1})
		if err != nil {
			return err
		}
		var conversation *models.AgentConversation
		if len(main) > 0 {
			conversation = main[0]
		} else if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.Agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			return err
		}
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: models.AgentMessageNote, Content: "From the schedule " + schedule.Name}); err != nil {
			return err
		}
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "assistant", Content: answer}); err != nil {
			return err
		}
		_, err = tx.UpdateAgentConversation(conversation.ID, func(conversation *models.AgentConversation) error {
			conversation.LastAt = time.Now()
			return nil
		})
		return err
	})
}
