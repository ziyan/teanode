package apigraph

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// AgentQuery is a person's own agent.
type AgentQuery interface {
	// The replies the agent wrote on the caller's behalf, newest first:
	// held, sent, cancelled, refused with the reason. Needs agent:use.
	ListAgentReplies(ctx context.Context, arguments ListAgentRepliesArguments) (*AgentReplyPage, error)

	// The caller's agent — or null if they have not turned one on — with every
	// mailbox they own as a source, granted or not, what the deployment
	// allows, and today's budget. Needs agent:use.
	ReadAgent(ctx context.Context) (*AgentView, error)

	// The caller's own token use since a time, totalled under one key: day,
	// kind, mailbox or model. Needs agent:use.
	AgentUsage(ctx context.Context, arguments AgentUsageArguments) ([]models.AgentUsageRow, error)
}

// AgentMutation changes a person's own agent.
type AgentMutation interface {
	// Turn the caller's agent on or off, name it, or change what it knows
	// about them. Makes the agent if they have none. Off cancels queued work
	// and keeps what was learned; forget deletes the agent and everything it
	// holds. Needs agent:use.
	UpdateAgent(ctx context.Context, arguments UpdateAgentArguments) (*AgentView, error)

	// Let the agent reach one of the caller's mailboxes, with the policy for
	// it; called again, it changes the policy. Needs agent:use.
	GrantAgentMailbox(ctx context.Context, arguments GrantAgentMailboxArguments) (*AgentView, error)

	// Have the agent write a reply to a message, for the caller to read,
	// change and send; the text comes back, nothing is saved or sent. The
	// mailbox must be granted with draft replies on. Needs agent:use.
	DraftReply(ctx context.Context, arguments DraftReplyArguments) (*AgentDraftView, error)

	// Cancel a reply the agent is holding: the draft goes, nothing is sent.
	// Needs agent:use.
	CancelAgentReply(ctx context.Context, arguments CancelAgentReplyArguments) (*models.AgentReply, error)

	// Stop the agent reaching a mailbox: queued work for it is cancelled,
	// what it learned there is kept. Needs agent:use.
	RevokeAgentMailbox(ctx context.Context, arguments RevokeAgentMailboxArguments) (*AgentView, error)
}

// AgentAdminQuery is what an operator sees of everybody's agents: state,
// sources, tokens — never content.
type AgentAdminQuery interface {
	// Every person with an agent, the sources each has granted, today's
	// tokens against the limit, and whether an operator switched it off.
	// Needs agent:audit.
	ListAgents(ctx context.Context) ([]*AgentSummary, error)

	// Token use across the server since a time, totalled under one key: day,
	// kind, mailbox, model or agent. Needs agent:audit.
	AgentServerUsage(ctx context.Context, arguments AgentUsageArguments) ([]models.AgentUsageRow, error)

	// Jobs the worker gave up on, newest first, with the error each ended
	// with. Needs agent:audit.
	ListAgentDeadLetters(ctx context.Context) ([]*models.AgentJob, error)
}

// AgentAdminMutation is what an operator may do to a person's agent.
type AgentAdminMutation interface {
	// Set one person's daily token budget; zero returns them to the
	// server's default. Needs agent:audit.
	SetAgentLimit(ctx context.Context, arguments SetAgentLimitArguments) (*AgentSummary, error)

	// Switch a person's agent off, or back on. While switched off the person
	// sees why and cannot turn it on themselves. Needs agent:audit.
	SetAgentDisabled(ctx context.Context, arguments SetAgentDisabledArguments) (*AgentSummary, error)

	// Put a dead job back in the queue. Needs agent:audit.
	RetryAgentJob(ctx context.Context, arguments RetryAgentJobArguments) (*models.AgentJob, error)
}

// AgentView is the caller's agent as the page shows it.
type AgentView struct {
	// Agent is null until the caller turns one on.
	Agent *models.Agent `json:"agent"`

	// Sources are every mailbox the caller owns, with its policy where the
	// agent has been granted it.
	Sources []*AgentSource `json:"sources"`

	// Allowed is what the deployment offers, resolved against what is
	// configured: a feature that needs an embedding model or a browser is
	// off here when there is none.
	Allowed *AgentAllowed `json:"allowed"`

	// Budget is today's tokens against the limit.
	Budget *AgentBudget `json:"budget"`

	// Choices are the models the caller may pick for conversations.
	Choices []string `json:"choices"`

	// Timezone and Language are what the agent resolves for the caller.
	Timezone string `json:"timezone"`
	Language string `json:"language"`

	// Categories are the fixed vocabulary followed by the caller's own.
	Categories []string `json:"categories"`
}

// AgentSource is one of the caller's mailboxes as a source.
type AgentSource struct {
	MailboxID string   `json:"mailboxId"`
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`

	// Policy is null until the mailbox has been granted once.
	Policy *models.AgentMailbox `json:"policy"`
}

// AgentAllowed is what the deployment offers.
type AgentAllowed struct {
	Enabled          bool `json:"enabled"`
	Triage           bool `json:"triage"`
	Summaries        bool `json:"summaries"`
	DraftReplies     bool `json:"draftReplies"`
	Search           bool `json:"search"`
	Research         bool `json:"research"`
	AutoReply        bool `json:"autoReply"`
	Ask              bool `json:"ask"`
	Schedules        bool `json:"schedules"`
	Browser          bool `json:"browser"`
	ConnectedServers bool `json:"connectedServers"`
}

// AgentBudget is today's spend.
type AgentBudget struct {
	Used     int64     `json:"used"`
	Limit    int64     `json:"limit"`
	ResetsAt time.Time `json:"resetsAt"`
}

// AgentUsageArguments say what period and what grouping.
type AgentUsageArguments struct {
	// Since when; the last thirty days when left out. Until when; now
	// when left out.
	Since *time.Time `json:"since"`
	Until *time.Time `json:"until"`

	// By is day, kind, mailbox or model; the total when left out.
	By *string `json:"by"`
}

// UpdateAgentArguments are what a person may change about their agent.
type UpdateAgentArguments struct {
	Enabled       *bool                      `json:"enabled"`
	Name          *string                    `json:"name"`
	Instructions  *string                    `json:"instructions"`
	Language      *string                    `json:"language"`
	Voice         *models.AgentVoice         `json:"voice"`
	Categories    *[]models.AgentCategory    `json:"categories"`
	Notifications *models.AgentNotifications `json:"notifications"`
	Confirm       *[]string                  `json:"confirm"`
	AskModel      *string                    `json:"askModel"`

	// Forget deletes the agent and everything it holds; the other fields
	// are ignored when it is set.
	Forget *bool `json:"forget"`
}

// GrantAgentMailboxArguments name the mailbox and its policy.
type GrantAgentMailboxArguments struct {
	MailboxID string `json:"mailboxId"`

	// Policy replaces the stored one. Left out, a sensible default is set
	// the first time and the stored policy kept after that.
	Policy *models.AgentMailbox `json:"policy"`
}

// RevokeAgentMailboxArguments name the mailbox.
type RevokeAgentMailboxArguments struct {
	MailboxID string `json:"mailboxId"`
}

// AgentSummary is one person's agent as an operator sees it.
type AgentSummary struct {
	AgentID            string                  `json:"agentId"`
	UserID             string                  `json:"userId"`
	Username           string                  `json:"username"`
	Name               string                  `json:"name"`
	Enabled            bool                    `json:"enabled"`
	OperatorDisabledAt *time.Time              `json:"operatorDisabledAt,omitempty"`
	DailyTokens        int64                   `json:"dailyTokens"`
	Sources            []*AgentSource          `json:"sources"`
	Today              *AgentBudget            `json:"today"`
	LastRunAt          *time.Time              `json:"lastRunAt,omitempty"`
	Dead               int64                   `json:"dead"`
	Queued             int64                   `json:"queued"`
	Totals             models.AgentUsageTotals `json:"totals"`
}

// SetAgentLimitArguments name the agent and the budget.
type SetAgentLimitArguments struct {
	AgentID     string `json:"agentId"`
	DailyTokens int64  `json:"dailyTokens"`
}

// SetAgentDisabledArguments name the agent and the switch.
type SetAgentDisabledArguments struct {
	AgentID  string `json:"agentId"`
	Disabled bool   `json:"disabled"`
}

// RetryAgentJobArguments name the job.
type RetryAgentJobArguments struct {
	JobID string `json:"jobId"`
}

func (self *graph) allowed() *AgentAllowed {
	configuration := self.config.Current()
	on := func(feature string) bool { return agent.FeatureAllowed(configuration, feature) }
	return &AgentAllowed{
		Enabled:          configuration.Agent.Enabled,
		Triage:           on("triage"),
		Summaries:        on("summaries"),
		DraftReplies:     on("draftReplies"),
		Search:           on("search") && configuration.Agent.Models.Embedding != "",
		Research:         on("research"),
		AutoReply:        on("autoReply"),
		Ask:              on("ask"),
		Schedules:        on("schedules"),
		Browser:          on("browser") && configuration.Agent.Browser.Enabled,
		ConnectedServers: on("connectedServers") && len(configuration.Agent.MCP.Servers) > 0,
	}
}

func (self *graph) agentView(ctx context.Context, tx db.Transaction, user *models.User) (*AgentView, error) {
	found, err := tx.GetAgentByUser(user.ID)
	if err != nil {
		return nil, err
	}
	view := &AgentView{
		Agent:      found,
		Sources:    []*AgentSource{},
		Allowed:    self.allowed(),
		Choices:    append([]string{}, self.config.Current().Agent.Models.Choices...),
		Timezone:   agent.Location(user).String(),
		Language:   agent.Language(found, user),
		Categories: found.CategoryNames(),
	}
	sources, err := self.sourcesOf(tx, user.ID)
	if err != nil {
		return nil, err
	}
	view.Sources = sources
	if found != nil {
		budget, err := agent.CheckBudget(tx, self.config.Current(), found, user, time.Now())
		if err != nil {
			return nil, err
		}
		view.Budget = &AgentBudget{Used: budget.Used, Limit: budget.Limit, ResetsAt: budget.ResetsAt}
	}
	return view, nil
}

func (self *graph) sourcesOf(tx db.Transaction, userId string) ([]*AgentSource, error) {
	mailboxes, err := tx.ListMailboxes(userId)
	if err != nil {
		return nil, err
	}
	sources := make([]*AgentSource, 0, len(mailboxes))
	for _, mailbox := range mailboxes {
		addresses := []string{}
		for _, address := range mailbox.Addresses {
			addresses = append(addresses, address.Address)
		}
		sources = append(sources, &AgentSource{MailboxID: mailbox.ID, Name: mailbox.Name, Addresses: addresses, Policy: mailbox.Agent})
	}
	return sources, nil
}

func (self *graph) ReadAgent(ctx context.Context) (*AgentView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	user := principal.User
	if user == nil {
		return nil, api.ErrNotLoggedIn
	}
	return self.agentView(ctx, self.transaction(ctx), user)
}

func (self *graph) AgentUsage(ctx context.Context, arguments AgentUsageArguments) ([]models.AgentUsageRow, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	user := principal.User
	if user == nil {
		return nil, api.ErrNotLoggedIn
	}
	tx := self.transaction(ctx)
	found, err := tx.GetAgentByUser(user.ID)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return []models.AgentUsageRow{}, nil
	}
	rows, err := tx.QueryAgentUsage(found.ID, usageSince(arguments.Since), usageUntil(arguments.Until), usageBy(arguments.By))
	if err != nil {
		return nil, translateError(err)
	}
	return rows, nil
}

func usageSince(since *time.Time) time.Time {
	if since != nil {
		return *since
	}
	return time.Now().Add(-30 * 24 * time.Hour)
}

func usageUntil(until *time.Time) time.Time {
	if until != nil {
		return *until
	}
	return time.Time{}
}

func usageBy(by *string) string {
	if by == nil {
		return ""
	}
	return strings.TrimSpace(*by)
}

func (self *graph) UpdateAgent(ctx context.Context, arguments UpdateAgentArguments) (*AgentView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	user := principal.User
	if user == nil {
		return nil, api.ErrNotLoggedIn
	}
	if !self.config.Current().Agent.Enabled {
		return nil, fmt.Errorf("the operator has not enabled agents on this server")
	}
	tx := self.transaction(ctx)
	found, err := tx.GetAgentByUser(user.ID)
	if err != nil {
		return nil, err
	}
	if arguments.Forget != nil && *arguments.Forget {
		if found != nil {
			if err := self.forgetAgent(ctx, tx, found, user); err != nil {
				return nil, translateError(err)
			}
		}
		log.Noticef("%s forgot their agent", operatorName(ctx))
		return self.agentView(ctx, tx, user)
	}
	if found == nil {
		found, err = tx.CreateAgent(&models.Agent{UserID: user.ID, Name: models.AgentDefaultName})
		if err != nil {
			return nil, translateError(err)
		}
	}
	choices := self.config.Current().Agent.Models.Choices
	updated, err := tx.UpdateAgent(found.ID, func(agent *models.Agent) error {
		if arguments.Enabled != nil {
			agent.Enabled = *arguments.Enabled
		}
		if arguments.Name != nil {
			agent.Name = strings.TrimSpace(*arguments.Name)
		}
		if arguments.Instructions != nil {
			agent.Instructions = strings.TrimSpace(*arguments.Instructions)
		}
		if arguments.Language != nil {
			agent.Language = strings.TrimSpace(*arguments.Language)
		}
		if arguments.Voice != nil {
			voice := *arguments.Voice
			agent.Voice = &voice
		}
		if arguments.Categories != nil {
			agent.Categories = *arguments.Categories
		}
		if arguments.Notifications != nil {
			notifications := *arguments.Notifications
			agent.Notifications = &notifications
		}
		if arguments.Confirm != nil {
			agent.Confirm = *arguments.Confirm
		}
		if arguments.AskModel != nil {
			choice := strings.TrimSpace(*arguments.AskModel)
			if choice != "" && !contains(choices, choice) {
				return fmt.Errorf("%q is not one of the models the operator offers", choice)
			}
			agent.AskModel = choice
		}
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	if arguments.Enabled != nil && !*arguments.Enabled {
		if _, err := tx.CancelAgentJobs(updated.ID, ""); err != nil {
			return nil, err
		}
	}
	return self.agentView(ctx, tx, user)
}

// forgetAgent deletes the agent and everything it holds, and clears every
// source's policy. Later milestones add their tables here.
func (self *graph) forgetAgent(ctx context.Context, tx db.Transaction, found *models.Agent, user *models.User) error {
	mailboxes, err := tx.ListMailboxes(user.ID)
	if err != nil {
		return err
	}
	for _, mailbox := range mailboxes {
		if mailbox.Agent == nil {
			continue
		}
		if _, err := tx.UpdateMailbox(mailbox.ID, func(mailbox *models.Mailbox) error {
			mailbox.Agent = nil
			return nil
		}); err != nil {
			return err
		}
		if err := forgetMailbox(tx, mailbox.ID); err != nil {
			return err
		}
	}
	if _, err := tx.DeleteAgentMemories(found.ID); err != nil {
		return err
	}
	if _, err := tx.DeleteAgentConnections(found.ID); err != nil {
		return err
	}
	schedules, err := tx.ListAgentSchedules(found.ID)
	if err != nil {
		return err
	}
	for _, schedule := range schedules {
		if err := tx.DeleteAgentSchedule(schedule.ID); err != nil {
			return err
		}
	}
	if err := agent.ForgetAttachments(ctx, tx, self.storage, found.ID); err != nil {
		return err
	}
	return tx.DeleteAgent(found.ID)
}

// forgetMailbox removes what the agent worked out about a mailbox's mail:
// the insights and the summaries. Withdrawing a mailbox from the agent
// withdraws its notes on it too.
func forgetMailbox(tx db.Transaction, mailboxId string) error {
	if _, err := tx.DeleteMailInsights(mailboxId); err != nil {
		return err
	}
	if _, err := tx.DeleteMailEmbeddings(mailboxId); err != nil {
		return err
	}
	_, err := tx.DeleteThreadSummaries(mailboxId)
	return err
}

func (self *graph) GrantAgentMailbox(ctx context.Context, arguments GrantAgentMailboxArguments) (*AgentView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	user := principal.User
	if user == nil {
		return nil, api.ErrNotLoggedIn
	}
	tx := self.transaction(ctx)
	mailbox, err := tx.GetMailbox(arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	if mailbox == nil || mailbox.UserID != user.ID {
		return nil, api.ErrNotFound
	}
	found, err := tx.GetAgentByUser(user.ID)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("turn the agent on before granting it a mailbox")
	}
	allowed := self.allowed()
	if _, err := tx.UpdateMailbox(mailbox.ID, func(mailbox *models.Mailbox) error {
		policy := arguments.Policy
		if policy == nil {
			if mailbox.Agent != nil {
				copied := *mailbox.Agent
				policy = &copied
			} else {
				policy = &models.AgentMailbox{
					Triage:       &models.AgentTriage{Enabled: allowed.Triage, Backfill: "recent"},
					Summaries:    &models.AgentSummaries{Enabled: allowed.Summaries},
					DraftReplies: allowed.DraftReplies,
					Search:       allowed.Search,
				}
			}
		}
		policy.Granted = true
		mailbox.Agent = policy
		return nil
	}); err != nil {
		return nil, translateError(err)
	}
	// What was already there, per the source's choice: a backfill job
	// queues the sorting of it in batches.
	if granted, err := tx.GetMailbox(mailbox.ID); err == nil && granted != nil && granted.Agent != nil && granted.Agent.Triage != nil && granted.Agent.Triage.Enabled && granted.Agent.Triage.Backfill != "none" {
		if _, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: found.ID, MailboxID: mailbox.ID, Kind: models.AgentJobBackfill}); err != nil {
			return nil, err
		}
	}
	log.Noticef("%s granted their agent the mailbox %q", operatorName(ctx), mailbox.Name)
	return self.agentView(ctx, tx, user)
}

func (self *graph) RevokeAgentMailbox(ctx context.Context, arguments RevokeAgentMailboxArguments) (*AgentView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	user := principal.User
	if user == nil {
		return nil, api.ErrNotLoggedIn
	}
	tx := self.transaction(ctx)
	mailbox, err := tx.GetMailbox(arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	if mailbox == nil || mailbox.UserID != user.ID {
		return nil, api.ErrNotFound
	}
	if mailbox.Agent != nil && mailbox.Agent.Granted {
		if _, err := tx.UpdateMailbox(mailbox.ID, func(mailbox *models.Mailbox) error {
			mailbox.Agent.Granted = false
			return nil
		}); err != nil {
			return nil, translateError(err)
		}
		if found, err := tx.GetAgentByUser(user.ID); err != nil {
			return nil, err
		} else if found != nil {
			if _, err := tx.CancelAgentJobs(found.ID, mailbox.ID); err != nil {
				return nil, err
			}
		}
		if err := forgetMailbox(tx, mailbox.ID); err != nil {
			return nil, err
		}
	}
	return self.agentView(ctx, tx, user)
}

func (self *graph) summarize(tx db.Transaction, found *models.Agent) (*AgentSummary, error) {
	owner, err := tx.GetUser(found.UserID)
	if err != nil {
		return nil, err
	}
	summary := &AgentSummary{
		AgentID:            found.ID,
		UserID:             found.UserID,
		Name:               found.DisplayName(),
		Enabled:            found.Enabled,
		OperatorDisabledAt: found.OperatorDisabledAt,
		DailyTokens:        found.DailyTokens,
		Sources:            []*AgentSource{},
	}
	if owner != nil {
		summary.Username = owner.Username
		if summary.Sources, err = self.sourcesOf(tx, owner.ID); err != nil {
			return nil, err
		}
		budget, err := agent.CheckBudget(tx, self.config.Current(), found, owner, time.Now())
		if err != nil {
			return nil, err
		}
		summary.Today = &AgentBudget{Used: budget.Used, Limit: budget.Limit, ResetsAt: budget.ResetsAt}
	}
	if summary.Totals, err = tx.SumAgentUsage(found.ID, time.Now().Add(-30*24*time.Hour)); err != nil {
		return nil, err
	}
	if summary.Dead, err = tx.CountAgentJobs(&db.AgentJobFilter{AgentID: found.ID, Statuses: []models.AgentJobStatus{models.AgentJobDead}}); err != nil {
		return nil, err
	}
	if summary.Queued, err = tx.CountAgentJobs(&db.AgentJobFilter{AgentID: found.ID, Statuses: []models.AgentJobStatus{models.AgentJobQueued, models.AgentJobRunning}}); err != nil {
		return nil, err
	}
	recent, err := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID, Statuses: []models.AgentJobStatus{models.AgentJobDone}}, &db.Options{Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(recent) > 0 {
		summary.LastRunAt = recent[0].FinishedAt
	}
	return summary, nil
}

func (self *graph) ListAgents(ctx context.Context) ([]*AgentSummary, error) {
	if _, err := self.requirePermission(ctx, models.PermissionAgentAudit); err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	agents, err := tx.ListAgents(nil)
	if err != nil {
		return nil, err
	}
	summaries := make([]*AgentSummary, 0, len(agents))
	for _, found := range agents {
		summary, err := self.summarize(tx, found)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

func (self *graph) AgentServerUsage(ctx context.Context, arguments AgentUsageArguments) ([]models.AgentUsageRow, error) {
	if _, err := self.requirePermission(ctx, models.PermissionAgentAudit); err != nil {
		return nil, err
	}
	rows, err := self.transaction(ctx).QueryAgentUsage("", usageSince(arguments.Since), usageUntil(arguments.Until), usageBy(arguments.By))
	if err != nil {
		return nil, translateError(err)
	}
	return rows, nil
}

func (self *graph) ListAgentDeadLetters(ctx context.Context) ([]*models.AgentJob, error) {
	if _, err := self.requirePermission(ctx, models.PermissionAgentAudit); err != nil {
		return nil, err
	}
	jobs, err := self.transaction(ctx).ListAgentJobs(&db.AgentJobFilter{Statuses: []models.AgentJobStatus{models.AgentJobDead}}, &db.Options{Limit: 200})
	if err != nil {
		return nil, err
	}
	if jobs == nil {
		jobs = []*models.AgentJob{}
	}
	return jobs, nil
}

func (self *graph) SetAgentLimit(ctx context.Context, arguments SetAgentLimitArguments) (*AgentSummary, error) {
	if _, err := self.requirePermission(ctx, models.PermissionAgentAudit); err != nil {
		return nil, err
	}
	if arguments.DailyTokens < 0 {
		return nil, api.ErrInvalidArguments
	}
	tx := self.transaction(ctx)
	updated, err := tx.UpdateAgent(arguments.AgentID, func(agent *models.Agent) error {
		agent.DailyTokens = arguments.DailyTokens
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	log.Noticef("%s set the daily budget of agent %s to %d", operatorName(ctx), updated.ID, arguments.DailyTokens)
	return self.summarize(tx, updated)
}

func (self *graph) SetAgentDisabled(ctx context.Context, arguments SetAgentDisabledArguments) (*AgentSummary, error) {
	if _, err := self.requirePermission(ctx, models.PermissionAgentAudit); err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	updated, err := tx.UpdateAgent(arguments.AgentID, func(agent *models.Agent) error {
		if arguments.Disabled {
			if agent.OperatorDisabledAt == nil {
				now := time.Now()
				agent.OperatorDisabledAt = &now
			}
		} else {
			agent.OperatorDisabledAt = nil
		}
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	if arguments.Disabled {
		if _, err := tx.CancelAgentJobs(updated.ID, ""); err != nil {
			return nil, err
		}
	}
	log.Noticef("%s switched agent %s %s", operatorName(ctx), updated.ID, map[bool]string{true: "off", false: "on"}[arguments.Disabled])
	return self.summarize(tx, updated)
}

func (self *graph) RetryAgentJob(ctx context.Context, arguments RetryAgentJobArguments) (*models.AgentJob, error) {
	if _, err := self.requirePermission(ctx, models.PermissionAgentAudit); err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	job, err := tx.GetAgentJob(arguments.JobID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, api.ErrNotFound
	}
	if job.Status != models.AgentJobDead && job.Status != models.AgentJobCancelled {
		return nil, api.ErrInvalidArguments
	}
	if err := tx.FinishAgentJob(job.ID, models.AgentJobQueued, "", nil); err != nil {
		return nil, err
	}
	return tx.GetAgentJob(job.ID)
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// DraftReplyArguments name the message to answer and, in a line, what the
// reply should do.
type DraftReplyArguments struct {
	ItemID       string `json:"itemId"`
	Instructions string `json:"instructions" graphapi:"nullable"`
}

// AgentDraftView is a reply the agent wrote for the caller to read.
type AgentDraftView struct {
	Text  string `json:"text"`
	Model string `json:"model"`
	RunID string `json:"runId"`
}

func (self *graph) DraftReply(ctx context.Context, arguments DraftReplyArguments) (*AgentDraftView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	user := principal.User
	if user == nil {
		return nil, api.ErrNotLoggedIn
	}
	items, mailbox, err := self.requireItems(ctx, models.PermissionMailRead, []string{arguments.ItemID})
	if err != nil {
		return nil, err
	}
	if mailbox == nil || mailbox.UserID != user.ID {
		return nil, api.ErrNotFound
	}
	if self.settings.Agent == nil || !self.config.Current().Agent.Enabled {
		return nil, agent.ErrUnavailable
	}
	tx := self.transaction(ctx)
	found, err := tx.GetAgentByUser(user.ID)
	if err != nil {
		return nil, err
	}
	if found == nil || !found.Active() {
		return nil, agent.ErrUnavailable
	}
	mails, err := tx.GetMails([]string{items[0].MailID}, nil)
	if err != nil {
		return nil, err
	}
	if len(mails) == 0 || mails[0] == nil {
		return nil, api.ErrNotFound
	}
	draft, err := self.settings.Agent.DraftReply(ctx, &models.AgentDraftRequest{
		Agent:        found,
		Owner:        user,
		Mailbox:      mailbox,
		Mail:         mails[0],
		Instructions: arguments.Instructions,
	})
	if err != nil {
		return nil, err
	}
	return &AgentDraftView{Text: draft.Text, Model: draft.Model, RunID: draft.RunID}, nil
}

// ListAgentRepliesArguments narrow the replies to a mailbox or a status.
type ListAgentRepliesArguments struct {
	MailboxID string `json:"mailboxId" graphapi:"nullable"`
	Status    string `json:"status" graphapi:"nullable"`
	First     int    `json:"first" graphapi:"nullable"`
	Offset    int    `json:"offset" graphapi:"nullable"`
}

// AgentReplyPage is a page of replies and how many there are in all.
type AgentReplyPage struct {
	Replies []*models.AgentReply `json:"replies"`
	Total   int64                `json:"total"`
}

func (self *graph) ListAgentReplies(ctx context.Context, arguments ListAgentRepliesArguments) (*AgentReplyPage, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	user := principal.User
	if user == nil {
		return nil, api.ErrNotLoggedIn
	}
	tx := self.transaction(ctx)
	found, err := tx.GetAgentByUser(user.ID)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return &AgentReplyPage{Replies: []*models.AgentReply{}}, nil
	}
	filter := &db.AgentReplyFilter{AgentID: found.ID, MailboxID: strings.TrimSpace(arguments.MailboxID)}
	if status := strings.TrimSpace(arguments.Status); status != "" {
		filter.Statuses = []models.AgentReplyStatus{models.AgentReplyStatus(status)}
	}
	limit := arguments.First
	if limit <= 0 || limit > api.MaximumPageSize {
		limit = 50
	}
	replies, err := tx.ListAgentReplies(filter, &db.Options{Limit: uint64(limit), Offset: uint64(max(arguments.Offset, 0))})
	if err != nil {
		return nil, err
	}
	total, err := tx.CountAgentReplies(filter)
	if err != nil {
		return nil, err
	}
	return &AgentReplyPage{Replies: replies, Total: total}, nil
}

// CancelAgentReplyArguments name the held reply.
type CancelAgentReplyArguments struct {
	ReplyID string `json:"replyId"`
}

func (self *graph) CancelAgentReply(ctx context.Context, arguments CancelAgentReplyArguments) (*models.AgentReply, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	user := principal.User
	if user == nil {
		return nil, api.ErrNotLoggedIn
	}
	tx := self.transaction(ctx)
	found, err := tx.GetAgentByUser(user.ID)
	if err != nil {
		return nil, err
	}
	reply, err := tx.GetAgentReply(arguments.ReplyID)
	if err != nil {
		return nil, err
	}
	if found == nil || reply == nil || reply.AgentID != found.ID {
		return nil, api.ErrNotFound
	}
	if reply.Status != models.AgentReplyHeld {
		return reply, nil // nothing to cancel; say where it stands
	}
	mailbox, err := tx.GetMailbox(reply.MailboxID)
	if err != nil {
		return nil, err
	}
	if mailbox != nil && reply.DraftItemID != "" {
		if err := self.removeDraft(ctx, tx, mailbox, reply.DraftItemID); err != nil && !errors.Is(err, api.ErrNotFound) {
			return nil, err
		}
	}
	updated, err := tx.UpdateAgentReply(reply.ID, func(reply *models.AgentReply) error {
		reply.Status = models.AgentReplyCancelled
		reply.Reason = "cancelled by the person"
		reply.DraftItemID = ""
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	if err := agent.RecordReplyDeclined(tx, reply, "cancelled by the person before it went"); err != nil {
		log.Warningf("cannot record the correction for reply %q: %s", reply.ID, err)
	}
	log.Noticef("%s cancelled the reply their agent held for %q", operatorName(ctx), reply.Subject)
	return updated, nil
}
