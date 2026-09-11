package models

import (
	"fmt"
	"strings"
	"time"
)

// Agent is a person's agent: one per account, made when they turn it on,
// holding what is about them. What it may reach — a mailbox, later a
// calendar or an address book — is a source the person grants, and each
// source carries its own processing policy (see AgentMailbox).
//
// docs/decisions/20260910-agents-belong-to-people.md says why the agent is
// the person's and not the mailbox's.
type Agent struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
	UserID     string    `json:"userId"`

	// Name is what the person calls it. "Agent" until they say otherwise.
	Name string `json:"name"`

	// Enabled is the person's own switch. Off cancels queued work and held
	// replies and keeps what was learned.
	Enabled bool `json:"enabled"`

	// Instructions are the person's standing words: who they are, how they
	// write, what matters to them. Appended to every prompt as their own
	// block, never interpolated into the conduct.
	Instructions string `json:"instructions,omitempty" graphapi:"nullable"`

	// Language is what the agent writes in; empty resolves to the account's
	// locale, chosen or learned.
	Language string `json:"language,omitempty" graphapi:"nullable"`

	// Voice is structured style, for models that follow fields better than
	// prose. Nil means neutral, medium, no greeting or sign-off.
	Voice *AgentVoice `json:"voice,omitempty" graphapi:"nullable"`

	// Categories are the person's own, beside the fixed vocabulary in
	// AgentCategories, across every source.
	Categories []AgentCategory `json:"categories"`

	// Notifications say how the person learns what the agent did.
	Notifications *AgentNotifications `json:"notifications,omitempty" graphapi:"nullable"`

	// Confirm names tools the person wants asked about before they run,
	// beyond the risk classes. Add-only: nobody can subtract from the floor.
	Confirm []string `json:"confirm"`

	// AskModel is the person's choice for their conversations, one of the
	// operator's choices, or empty for the operator's ask model.
	AskModel string `json:"askModel,omitempty" graphapi:"nullable"`

	// DailyTokens is this person's budget, set only by an operator; zero is
	// the server's default.
	DailyTokens int64 `json:"dailyTokens"`

	// DailyCost is the same budget said in money — what the day's calls
	// cost at the provider's prices — for an operator who would rather
	// cap the bill than the tokens. Zero is no limit of its own. Where
	// both are set, whichever runs out first stops the day.
	DailyCost float64 `json:"dailyCost"`

	// OperatorDisabledAt is set by an operator and cannot be cleared by the
	// person; while set the agent does nothing and the page says why.
	OperatorDisabledAt *time.Time `json:"operatorDisabledAt,omitempty" graphapi:"nullable"`
}

// AgentDefaultName is what an agent is called until the person names it.
const AgentDefaultName = "Agent"

// AgentVoice is how the agent sounds, as fields.
type AgentVoice struct {
	Tone     string `json:"tone,omitempty" graphapi:"nullable"`   // formal | neutral | casual
	Length   string `json:"length,omitempty" graphapi:"nullable"` // short | medium | long
	Greeting string `json:"greeting,omitempty" graphapi:"nullable"`
	Signoff  string `json:"signoff,omitempty" graphapi:"nullable"`
}

// AgentCategory is a category the person declared, with the one-line
// description the sorting prompt is given.
type AgentCategory struct {
	Name        string `json:"name"`
	Description string `json:"description" graphapi:"nullable"`
}

// AgentCategories is the fixed vocabulary every insight draws from, beside
// the person's own. Fixed so that chips translate and rules stay portable.
var AgentCategories = []string{"personal", "work", "newsletter", "notification", "receipt", "promotion", "social", "invitation", "other"}

// AgentPriorities are the three levels an insight assigns.
var AgentPriorities = []string{"high", "normal", "low"}

// AgentNotifications say, per event, whether the person is told and how:
// "off", "dashboard", or "mail" to the account's notification address.
type AgentNotifications struct {
	HeldReply    string `json:"heldReply,omitempty" graphapi:"nullable"`
	HighPriority string `json:"highPriority,omitempty" graphapi:"nullable"`
	RunFailed    string `json:"runFailed,omitempty" graphapi:"nullable"`
}

// The three ways a person may be told.
const (
	AgentNotifyOff       = "off"
	AgentNotifyDashboard = "dashboard"
	AgentNotifyMail      = "mail"
)

// Validate reports everything wrong with the agent.
func (self *Agent) Validate() error {
	var errors ValidationErrors
	if self.UserID == "" {
		errors.add("userId", "required")
	}
	if len(self.Name) > 64 {
		errors.add("name", "must be under 64 characters")
	}
	if len(self.Instructions) > 20000 {
		errors.add("instructions", "must be under 20000 characters")
	}
	if self.Voice != nil {
		if !oneOf(self.Voice.Tone, "", "formal", "neutral", "casual") {
			errors.add("voice.tone", "%q is not a tone", self.Voice.Tone)
		}
		if !oneOf(self.Voice.Length, "", "short", "medium", "long") {
			errors.add("voice.length", "%q is not a length", self.Voice.Length)
		}
	}
	seen := map[string]bool{}
	for index, category := range self.Categories {
		name := strings.ToLower(strings.TrimSpace(category.Name))
		if name == "" {
			errors.add("categories", "category %d has no name", index)
			continue
		}
		if oneOf(name, AgentCategories...) || seen[name] {
			errors.add("categories", "category %d: %q is already a category", index, name)
		}
		seen[name] = true
	}
	if self.Notifications != nil {
		for field, value := range map[string]string{"heldReply": self.Notifications.HeldReply, "highPriority": self.Notifications.HighPriority, "runFailed": self.Notifications.RunFailed} {
			if !oneOf(value, "", AgentNotifyOff, AgentNotifyDashboard, AgentNotifyMail) {
				errors.add("notifications."+field, "%q is not off, dashboard or mail", value)
			}
		}
	}
	if self.DailyCost < 0 {
		return fmt.Errorf("a daily cost cannot be negative")
	}
	if self.DailyTokens < 0 {
		errors.add("dailyTokens", "must not be negative")
	}
	return errors.ErrOrNil()
}

// Active says whether the agent does anything at all: on, and not switched
// off by an operator.
func (self *Agent) Active() bool {
	return self != nil && self.Enabled && self.OperatorDisabledAt == nil
}

// DisplayName is what to call the agent.
func (self *Agent) DisplayName() string {
	if self == nil || strings.TrimSpace(self.Name) == "" {
		return AgentDefaultName
	}
	return self.Name
}

// CategoryNames is the fixed vocabulary followed by the person's own.
func (self *Agent) CategoryNames() []string {
	names := append([]string(nil), AgentCategories...)
	if self != nil {
		for _, category := range self.Categories {
			names = append(names, strings.ToLower(strings.TrimSpace(category.Name)))
		}
	}
	return names
}

// AgentMailbox is a mailbox as a source: whether the agent may reach it and
// what it does there. A JSON column on the mailbox; nil means never granted.
type AgentMailbox struct {
	// Granted says the agent may read this mailbox and act in it.
	Granted bool `json:"granted"`

	Triage       *AgentTriage    `json:"triage,omitempty" graphapi:"nullable"`
	Summaries    *AgentSummaries `json:"summaries,omitempty" graphapi:"nullable"`
	DraftReplies bool            `json:"draftReplies"`
	Search       bool            `json:"search"`
	Research     bool            `json:"research"`
	AutoReply    *AgentAutoReply `json:"autoReply,omitempty" graphapi:"nullable"`
}

// AgentTriage is the sorting policy for one source.
type AgentTriage struct {
	Enabled bool `json:"enabled"`

	// Backfill is what to do with the mail already there when the mailbox
	// is granted: none, recent (the newest 200), or all (under the budget,
	// over days).
	Backfill string `json:"backfill,omitempty" graphapi:"nullable"`

	// ReplyExpectation is what counts as needing a reply: direct (the
	// person is addressed) or any (including copies).
	ReplyExpectation string `json:"replyExpectation,omitempty" graphapi:"nullable"`
}

// AgentSummaries is the summarizing policy for one source.
type AgentSummaries struct {
	Enabled bool `json:"enabled"`

	// MinimumMessages is how long a conversation is before it gets a
	// summary without being opened. Zero means the default, three.
	MinimumMessages int `json:"minimumMessages,omitempty" graphapi:"nullable"`

	// Style is brief or detailed.
	Style string `json:"style,omitempty" graphapi:"nullable"`
}

// AgentAutoReply is the policy under which the agent answers mail in this
// source on the person's behalf. Whether a given message is answered is the
// refusal ladder's decision, in the reply run; this is what the person set.
type AgentAutoReply struct {
	Enabled bool `json:"enabled"`

	// Guidance is when and how to answer, in the person's words.
	Guidance string `json:"guidance,omitempty" graphapi:"nullable"`

	// Scope is known (contacts only), everyone, or list (Allow only).
	Scope string `json:"scope,omitempty" graphapi:"nullable"`

	// Allow and Never are addresses or domains: the list for scope list,
	// and the ones never answered whatever the scope.
	Allow []string `json:"allow,omitempty" graphapi:"nullable"`
	Never []string `json:"never,omitempty" graphapi:"nullable"`

	// Categories restricts answering to these; empty means any.
	Categories []string `json:"categories,omitempty" graphapi:"nullable"`

	// When is always, outsideHours (see Hours), or whenAway (only while the
	// mailbox's out-of-office reply is on).
	When  string      `json:"when,omitempty" graphapi:"nullable"`
	Hours *AgentHours `json:"hours,omitempty" graphapi:"nullable"`

	// HoldMinutes is how long a reply waits in Drafts before it is sent,
	// during which the person can cancel or take it over. Zero means ten.
	HoldMinutes int `json:"holdMinutes,omitempty" graphapi:"nullable"`

	// DailyLimit is the most replies sent from this source in a day. Zero
	// means twenty.
	DailyLimit int `json:"dailyLimit,omitempty" graphapi:"nullable"`

	// QuietDays is how long a sender is left alone after one reply. Zero
	// means seven; never under one.
	QuietDays int `json:"quietDays,omitempty" graphapi:"nullable"`
}

// AgentHours are the person's working hours in their own zone, for a policy
// that answers outside them.
type AgentHours struct {
	From  string `json:"from"`  // "09:00"
	Until string `json:"until"` // "17:30"
	Days  []int  `json:"days"`  // 0 = Sunday … 6 = Saturday
}

// The defaults the zero values stand for.
const (
	AgentDefaultHoldMinutes     = 10
	AgentDefaultDailyReplyLimit = 20
	AgentDefaultQuietDays       = 7
	AgentDefaultMinimumMessages = 3
)

// EffectiveHoldMinutes resolves the zero value.
func (self *AgentAutoReply) EffectiveHoldMinutes() int {
	if self == nil || self.HoldMinutes <= 0 {
		return AgentDefaultHoldMinutes
	}
	return self.HoldMinutes
}

// EffectiveDailyLimit resolves the zero value.
func (self *AgentAutoReply) EffectiveDailyLimit() int {
	if self == nil || self.DailyLimit <= 0 {
		return AgentDefaultDailyReplyLimit
	}
	return self.DailyLimit
}

// EffectiveQuietDays resolves the zero value and the floor.
func (self *AgentAutoReply) EffectiveQuietDays() int {
	if self == nil || self.QuietDays < 1 {
		if self != nil && self.QuietDays == 0 {
			return AgentDefaultQuietDays
		}
		return 1
	}
	return self.QuietDays
}

// Validate reports everything wrong with the source's policy.
func (self *AgentMailbox) Validate() error {
	var errors ValidationErrors
	if self.Triage != nil {
		if !oneOf(self.Triage.Backfill, "", "none", "recent", "all") {
			errors.add("triage.backfill", "%q is not none, recent or all", self.Triage.Backfill)
		}
		if !oneOf(self.Triage.ReplyExpectation, "", "direct", "any") {
			errors.add("triage.replyExpectation", "%q is not direct or any", self.Triage.ReplyExpectation)
		}
	}
	if self.Summaries != nil {
		if self.Summaries.MinimumMessages < 0 || self.Summaries.MinimumMessages > 100 {
			errors.add("summaries.minimumMessages", "must be between 0 and 100")
		}
		if !oneOf(self.Summaries.Style, "", "brief", "detailed") {
			errors.add("summaries.style", "%q is not brief or detailed", self.Summaries.Style)
		}
	}
	if reply := self.AutoReply; reply != nil {
		if !oneOf(reply.Scope, "", "known", "everyone", "list") {
			errors.add("autoReply.scope", "%q is not known, everyone or list", reply.Scope)
		}
		if !oneOf(reply.When, "", "always", "outsideHours", "whenAway") {
			errors.add("autoReply.when", "%q is not always, outsideHours or whenAway", reply.When)
		}
		if reply.When == "outsideHours" && reply.Hours == nil {
			errors.add("autoReply.hours", "required when answering outside hours")
		}
		if reply.Hours != nil {
			for field, value := range map[string]string{"from": reply.Hours.From, "until": reply.Hours.Until} {
				if _, err := time.Parse("15:04", value); err != nil {
					errors.add("autoReply.hours."+field, "%q is not a time of day like 09:00", value)
				}
			}
			for _, day := range reply.Hours.Days {
				if day < 0 || day > 6 {
					errors.add("autoReply.hours.days", "%d is not a weekday (0 = Sunday … 6 = Saturday)", day)
				}
			}
		}
		if reply.HoldMinutes < 0 || reply.HoldMinutes > 24*60 {
			errors.add("autoReply.holdMinutes", "must be between 0 and 1440")
		}
		if reply.DailyLimit < 0 || reply.DailyLimit > 1000 {
			errors.add("autoReply.dailyLimit", "must be between 0 and 1000")
		}
		if reply.QuietDays < 0 || reply.QuietDays > 365 {
			errors.add("autoReply.quietDays", "must be between 0 and 365")
		}
		for index, address := range append(append([]string(nil), reply.Allow...), reply.Never...) {
			if strings.TrimSpace(address) == "" {
				errors.add("autoReply.allow", "entry %d is empty", index)
			}
		}
	}
	return errors.ErrOrNil()
}

// AgentJobKind is what a job does.
type AgentJobKind string

// The kinds of job the worker runs.
const (
	AgentJobTriage    AgentJobKind = "triage"
	AgentJobResearch  AgentJobKind = "research"
	AgentJobSummarize AgentJobKind = "summarize"
	AgentJobEmbed     AgentJobKind = "embed"
	AgentJobReply     AgentJobKind = "reply"
	AgentJobSend      AgentJobKind = "send"
	AgentJobSchedule  AgentJobKind = "schedule"

	// AgentJobBackfill queues triage for what was already in a mailbox when
	// it was granted.
	AgentJobBackfill AgentJobKind = "backfill"

	// AgentJobNoop does nothing and records that it ran; it proves the
	// queue end to end before any kind that costs tokens exists.
	AgentJobNoop AgentJobKind = "noop"
)

// AgentJobStatus is where a job is.
type AgentJobStatus string

// A job's lifetime: queued, running, and then one of done, failed (to be
// retried), dead (given up), deferred (waiting for a budget) or cancelled.
const (
	AgentJobQueued    AgentJobStatus = "queued"
	AgentJobRunning   AgentJobStatus = "running"
	AgentJobDone      AgentJobStatus = "done"
	AgentJobDead      AgentJobStatus = "dead"
	AgentJobCancelled AgentJobStatus = "cancelled"
)

// AgentJob is one unit of work for the worker.
type AgentJob struct {
	ID         string         `json:"id"`
	CreatedAt  time.Time      `json:"createdAt"`
	AgentID    string         `json:"agentId"`
	MailboxID  string         `json:"mailboxId,omitempty"`
	Kind       AgentJobKind   `json:"kind"`
	SubjectID  string         `json:"subjectId,omitempty"`
	Status     AgentJobStatus `json:"status"`
	Attempts   int            `json:"attempts"`
	NotBefore  *time.Time     `json:"notBefore,omitempty"`
	ClaimedAt  *time.Time     `json:"claimedAt,omitempty"`
	ClaimedBy  string         `json:"claimedBy,omitempty"`
	Error      string         `json:"error,omitempty"`
	FinishedAt *time.Time     `json:"finishedAt,omitempty"`
}

// AgentUsageValues is what each ordinal of an agent_usage row's values
// holds.
const (
	AgentUsagePromptTokens     = 0
	AgentUsageCompletionTokens = 1
	AgentUsageCacheReadTokens  = 2
	AgentUsageCacheWriteTokens = 3
	AgentUsageCalls            = 4
	AgentUsageValueCount       = 5
)

// AgentUsageTotals are token counts summed over some rows.
type AgentUsageTotals struct {
	PromptTokens     int64 `json:"promptTokens"`
	CompletionTokens int64 `json:"completionTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
	Calls            int64 `json:"calls"`
}

// Total is every token.
func (self AgentUsageTotals) Total() int64 {
	return self.PromptTokens + self.CompletionTokens + self.CacheReadTokens + self.CacheWriteTokens
}

// AgentUsageRow is totals under one key: a day, a run kind, a source or a
// model, depending on what was asked.
type AgentUsageRow struct {
	Key    string           `json:"key"`
	Totals AgentUsageTotals `json:"totals"`
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// AgentDraftRequest asks the agent to write a reply the person will read
// before sending: to this message, in this mailbox, doing what the
// instructions say.
type AgentDraftRequest struct {
	Agent        *Agent
	Owner        *User
	Mailbox      *Mailbox
	Mail         *Mail
	Instructions string
}

// AgentDraft is what the agent wrote: the body of a reply, and the run
// that wrote it.
type AgentDraft struct {
	Text  string `json:"text"`
	Model string `json:"model"`
	RunID string `json:"runId"`
}
