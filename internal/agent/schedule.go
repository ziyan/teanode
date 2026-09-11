package agent

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
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

// cronField is one field of a cron line: which values it admits.
type cronField struct {
	values map[int]bool
	any    bool
}

func (self cronField) admits(value int) bool {
	return self.any || self.values[value]
}

// parseCron reads five fields — minute hour day month weekday — with *,
// lists, ranges and steps; weekday 0 and 7 are both Sunday.
func parseCron(expression string) ([5]cronField, error) {
	var fields [5]cronField
	parts := strings.Fields(strings.TrimSpace(expression))
	if len(parts) != 5 {
		return fields, fmt.Errorf("a schedule is five fields: minute hour day month weekday")
	}
	bounds := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	for index, part := range parts {
		field := cronField{values: map[int]bool{}}
		for _, piece := range strings.Split(part, ",") {
			step := 1
			if slash := strings.Index(piece, "/"); slash >= 0 {
				parsed, err := strconv.Atoi(piece[slash+1:])
				if err != nil || parsed <= 0 {
					return fields, fmt.Errorf("%q is not a step", piece)
				}
				step = parsed
				piece = piece[:slash]
			}
			low, high := bounds[index][0], bounds[index][1]
			switch {
			case piece == "*":
				if step == 1 {
					field.any = true
				}
			case strings.Contains(piece, "-"):
				ends := strings.SplitN(piece, "-", 2)
				var err error
				if low, err = strconv.Atoi(ends[0]); err != nil {
					return fields, fmt.Errorf("%q is not a range", piece)
				}
				if high, err = strconv.Atoi(ends[1]); err != nil {
					return fields, fmt.Errorf("%q is not a range", piece)
				}
			default:
				value, err := strconv.Atoi(piece)
				if err != nil {
					return fields, fmt.Errorf("%q is not a number", piece)
				}
				low, high = value, value
			}
			if low < bounds[index][0] || high > bounds[index][1] || low > high {
				return fields, fmt.Errorf("%q is out of range", piece)
			}
			for value := low; value <= high; value += step {
				field.values[value] = true
			}
		}
		if index == 4 {
			if field.values[7] {
				field.values[0] = true
			}
		}
		fields[index] = field
	}
	return fields, nil
}

// nextCron is the first moment after the given one that a cron line
// admits, in the zone, or zero when none is found in four years.
func nextCron(expression string, after time.Time, location *time.Location) (time.Time, error) {
	// A schedule for one moment: "@at 2026-09-12 09:00", in the person's
	// zone. It comes once, and a schedule with no next time turns itself
	// off after it.
	if moment, ok := strings.CutPrefix(strings.TrimSpace(expression), "@at "); ok {
		at, err := parseMoment(strings.TrimSpace(moment), location)
		if err != nil {
			return time.Time{}, err
		}
		if !at.After(after) {
			return time.Time{}, fmt.Errorf("the moment has passed")
		}
		return at, nil
	}
	fields, err := parseCron(expression)
	if err != nil {
		return time.Time{}, err
	}
	candidate := after.In(location).Truncate(time.Minute).Add(time.Minute)
	limit := candidate.AddDate(4, 0, 0)
	for candidate.Before(limit) {
		if !fields[3].admits(int(candidate.Month())) {
			candidate = time.Date(candidate.Year(), candidate.Month()+1, 1, 0, 0, 0, 0, location)
			continue
		}
		if !fields[2].admits(candidate.Day()) || !fields[4].admits(int(candidate.Weekday())) {
			candidate = time.Date(candidate.Year(), candidate.Month(), candidate.Day()+1, 0, 0, 0, 0, location)
			continue
		}
		if !fields[1].admits(candidate.Hour()) {
			candidate = time.Date(candidate.Year(), candidate.Month(), candidate.Day(), candidate.Hour()+1, 0, 0, 0, location)
			continue
		}
		if !fields[0].admits(candidate.Minute()) {
			candidate = candidate.Add(time.Minute)
			continue
		}
		return candidate, nil
	}
	return time.Time{}, fmt.Errorf("the schedule never comes round")
}

// relativePattern is "@in 5m", "@in 2 hours", "@in 1 day": a moment said
// from now, which is how a person asks to be reminded.
var relativePattern = regexp.MustCompile(`^@in\s+(\d+)\s*(m|min|mins|minute|minutes|h|hr|hrs|hour|hours|d|day|days)$`)

// resolveRelative turns "@in 5m" into the "@at" moment it means from now,
// so the schedule stores a moment and not a distance that would move with
// every look. Anything else is returned as it came.
func resolveRelative(expression string, now time.Time, location *time.Location) (string, error) {
	trimmed := strings.TrimSpace(expression)
	if !strings.HasPrefix(trimmed, "@in ") {
		return expression, nil
	}
	var distance time.Duration
	if match := relativePattern.FindStringSubmatch(trimmed); match != nil {
		count, _ := strconv.Atoi(match[1])
		switch match[2][0] {
		case 'm':
			distance = time.Duration(count) * time.Minute
		case 'h':
			distance = time.Duration(count) * time.Hour
		default:
			distance = time.Duration(count) * 24 * time.Hour
		}
	} else if parsed, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(trimmed, "@in "))); err == nil {
		distance = parsed
	} else {
		return "", fmt.Errorf("a distance from now is written @in 5m, @in 2h or @in 1 day")
	}
	if distance < time.Minute {
		return "", fmt.Errorf("the soonest is @in 1m")
	}
	return "@at " + now.Add(distance).In(location).Format("2006-01-02 15:04"), nil
}

// parseMoment reads a date and time the way a person writes one, in their
// zone unless the text carries its own offset.
func parseMoment(text string, location *time.Location) (time.Time, error) {
	if at, err := time.Parse(time.RFC3339, text); err == nil {
		return at, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		if at, err := time.ParseInLocation(layout, text, location); err == nil {
			return at, nil
		}
	}
	return time.Time{}, fmt.Errorf("a moment is written 2026-09-12 09:00")
}

// NextRun is when a schedule next runs for a person, from now.
func NextRun(schedule *models.AgentSchedule, owner *models.User, now time.Time) (time.Time, error) {
	return nextCron(schedule.Cron, now, Location(owner))
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

type scheduleArguments struct {
	Action     string `json:"action"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Cron       string `json:"cron"`
	Prompt     string `json:"prompt"`
	Deliver    string `json:"deliver"`
	Enabled    *bool  `json:"enabled"`
	ScheduleID string `json:"schedule_id"`
}

func registerScheduleTools(catalog *Catalog) {
	catalog.Register(&Tool{
		Name: "schedule", Family: FamilyGeneral, Core: true, Risk: RiskWrite,
		Description: "Things you do on your own at set times, in the person's zone: add one (a name, a cron line or a single moment, what to do, and whether the answer goes by mail or into the conversation), list, change, remove, or run one now. A reminder for later is a schedule at one moment; \"check this in five minutes\" is @in 5m with the item's id in the prompt. A scheduled run can confirm nothing, so it does only what needs no confirmation.",
		Parameters: object(map[string]any{
			"action":  enumProperty("what to do", "add", "list", "update", "remove", "run"),
			"id":      stringProperty("for update, remove and run: the schedule"),
			"name":    stringProperty("what it is called"),
			"cron":    stringProperty("five fields: minute hour day month weekday, such as 0 8 * * 1-5; or once, at a moment in the person's zone: @at 2026-09-12 09:00; or once, from now: @in 5m, @in 2h, @in 1 day"),
			"prompt":  stringProperty("what to do, as the person would say it"),
			"deliver": enumProperty("where the answer goes", "drawer", "mail"),
			"enabled": booleanProperty("on"),
		}, "action"),
		Run: runScheduleTool,
	})
}

func runScheduleTool(ctx context.Context, call *Call) (*Result, error) {
	arguments, err := decodeArguments[scheduleArguments](call)
	if err != nil {
		return nil, err
	}
	run := runOf(ctx)
	if !FeatureAllowed(run.agent.settings.Configuration(), "schedules") {
		return nil, fmt.Errorf("schedules are off on this server")
	}
	database := run.agent.settings.Database
	agentId := run.settings.Agent.ID
	location := Location(run.Owner())
	describe := func(schedule *models.AgentSchedule) map[string]any {
		row := map[string]any{"id": schedule.ID, "name": schedule.Name, "cron": schedule.Cron, "prompt": schedule.Prompt, "deliver": schedule.Deliver, "enabled": schedule.Enabled}
		if schedule.NextRunAt != nil {
			row["next_run"] = schedule.NextRunAt.In(location).Format("2006-01-02 15:04")
		}
		return row
	}
	id := arguments.ID
	if id == "" {
		id = arguments.ScheduleID
	}
	own := func(tx db.Transaction) (*models.AgentSchedule, error) {
		schedule, err := tx.GetAgentSchedule(id)
		if err != nil {
			return nil, err
		}
		if schedule == nil || schedule.AgentID != agentId {
			return nil, fmt.Errorf("there is no schedule %q", id)
		}
		return schedule, nil
	}
	switch arguments.Action {
	case "add":
		cron, err := resolveRelative(arguments.Cron, time.Now(), location)
		if err != nil {
			return nil, err
		}
		arguments.Cron = cron
		next, err := nextCron(arguments.Cron, time.Now(), location)
		if err != nil {
			return nil, err
		}
		var created *models.AgentSchedule
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			created, err = tx.CreateAgentSchedule(&models.AgentSchedule{AgentID: agentId, Name: arguments.Name, Cron: arguments.Cron, Prompt: arguments.Prompt, Deliver: arguments.Deliver, Enabled: arguments.Enabled == nil || *arguments.Enabled, NextRunAt: &next})
			return err
		}); err != nil {
			return nil, err
		}
		result, err := jsonResult(describe(created))
		if err != nil {
			return nil, err
		}
		result.Note = "scheduled " + created.Name
		return result, nil
	case "list":
		var schedules []*models.AgentSchedule
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			schedules, err = tx.ListAgentSchedules(agentId)
			return err
		}); err != nil {
			return nil, err
		}
		rows := make([]map[string]any, 0, len(schedules))
		for _, schedule := range schedules {
			rows = append(rows, describe(schedule))
		}
		return jsonResult(map[string]any{"schedules": rows})
	case "update":
		var updated *models.AgentSchedule
		if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
			if _, err := own(tx); err != nil {
				return err
			}
			var err error
			updated, err = tx.UpdateAgentSchedule(id, func(schedule *models.AgentSchedule) error {
				if arguments.Name != "" {
					schedule.Name = arguments.Name
				}
				if arguments.Prompt != "" {
					schedule.Prompt = arguments.Prompt
				}
				if arguments.Deliver != "" {
					schedule.Deliver = arguments.Deliver
				}
				if arguments.Enabled != nil {
					schedule.Enabled = *arguments.Enabled
				}
				if arguments.Cron != "" {
					cron, err := resolveRelative(arguments.Cron, time.Now(), location)
					if err != nil {
						return err
					}
					schedule.Cron = cron
				}
				if arguments.Cron != "" || (arguments.Enabled != nil && *arguments.Enabled) {
					next, err := nextCron(schedule.Cron, time.Now(), location)
					if err != nil {
						return err
					}
					schedule.NextRunAt = &next
				}
				return nil
			})
			return err
		}); err != nil {
			return nil, err
		}
		return jsonResult(describe(updated))
	case "remove":
		if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
			if _, err := own(tx); err != nil {
				return err
			}
			return tx.DeleteAgentSchedule(id)
		}); err != nil {
			return nil, err
		}
		return textResult("removed the schedule"), nil
	case "run":
		var schedule *models.AgentSchedule
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			if schedule, err = own(tx); err != nil {
				return err
			}
			_, err = run.agent.Enqueue(tx, models.AgentJobSchedule, agentId, "", schedule.ID)
			return err
		}); err != nil {
			return nil, err
		}
		return textResult("queued a run of %q; the answer arrives where the schedule says", schedule.Name), nil
	}
	return nil, fmt.Errorf("%q is not an action of schedule", arguments.Action)
}
