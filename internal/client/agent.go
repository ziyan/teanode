package client

import (
	"context"
	"encoding/json"
	"time"
)

// The person's agent, as the command line reaches it. Kept as raw JSON for
// the parts whose shape the server owns — the policy of a source, the
// agent's own settings — so that a field added on the server shows up
// without a change here; typed where the command line reads a value.

// AgentView is what ReadAgent returns.
type AgentView struct {
	Agent      json.RawMessage `json:"agent"`
	Sources    []*AgentSource  `json:"sources"`
	Allowed    map[string]bool `json:"allowed"`
	Budget     *AgentBudget    `json:"budget"`
	Choices    []string        `json:"choices"`
	Timezone   string          `json:"timezone"`
	Language   string          `json:"language"`
	Categories []string        `json:"categories"`
}

// AgentSource is one mailbox as a source.
type AgentSource struct {
	MailboxID string          `json:"mailboxId"`
	Name      string          `json:"name"`
	Addresses []string        `json:"addresses"`
	Policy    json.RawMessage `json:"policy"`
}

// AgentBudget is today's spend.
type AgentBudget struct {
	Used     int64     `json:"used"`
	Limit    int64     `json:"limit"`
	ResetsAt time.Time `json:"resetsAt"`
}

// AgentUsageRow is totals under one key.
type AgentUsageRow struct {
	Key    string `json:"key"`
	Totals struct {
		PromptTokens     int64 `json:"promptTokens"`
		CompletionTokens int64 `json:"completionTokens"`
		CacheReadTokens  int64 `json:"cacheReadTokens"`
		CacheWriteTokens int64 `json:"cacheWriteTokens"`
		Calls            int64 `json:"calls"`
	} `json:"totals"`
}

// AgentSummary is one person's agent as an operator sees it.
type AgentSummary struct {
	AgentID            string         `json:"agentId"`
	UserID             string         `json:"userId"`
	Username           string         `json:"username"`
	Name               string         `json:"name"`
	Enabled            bool           `json:"enabled"`
	OperatorDisabledAt *time.Time     `json:"operatorDisabledAt"`
	DailyTokens        int64          `json:"dailyTokens"`
	Sources            []*AgentSource `json:"sources"`
	Today              *AgentBudget   `json:"today"`
	LastRunAt          *time.Time     `json:"lastRunAt"`
	Dead               int64          `json:"dead"`
	Queued             int64          `json:"queued"`
}

// AgentJob is one job of the worker.
type AgentJob struct {
	ID         string     `json:"id"`
	CreatedAt  time.Time  `json:"createdAt"`
	AgentID    string     `json:"agentId"`
	MailboxID  string     `json:"mailboxId"`
	Kind       string     `json:"kind"`
	SubjectID  string     `json:"subjectId"`
	Status     string     `json:"status"`
	Attempts   int        `json:"attempts"`
	Error      string     `json:"error"`
	FinishedAt *time.Time `json:"finishedAt"`
}

const agentViewSelection = `{
	agent { id name enabled instructions language askModel dailyTokens operatorDisabledAt confirm
		voice { tone length greeting signoff }
		categories { name description }
		notifications { heldReply highPriority runFailed } }
	sources { mailboxId name addresses policy { granted draftReplies search research
		triage { enabled backfill replyExpectation }
		summaries { enabled minimumMessages style }
		autoReply { enabled guidance scope allow never categories when hours { from until days } holdMinutes dailyLimit quietDays } } }
	allowed { enabled triage summaries draftReplies search research autoReply ask schedules browser connectedServers }
	budget { used limit resetsAt }
	choices timezone language categories
}`

const agentSummarySelection = `{
	agentId userId username name enabled operatorDisabledAt dailyTokens dead queued
	sources { mailboxId name addresses policy { granted draftReplies search research triage { enabled } summaries { enabled } autoReply { enabled } } }
	today { used limit resetsAt } lastRunAt
}`

const agentJobSelection = `{ id createdAt agentId mailboxId kind subjectId status attempts error finishedAt }`

// The documents, exported so a test can compare them with the schema.
const (
	DocumentReadAgent = `query { ReadAgent ` + agentViewSelection + ` }`

	DocumentUpdateAgent = `mutation ($enabled: Boolean, $name: String, $instructions: String, $language: String,
		$voice: AgentVoiceInput, $categories: [AgentCategoryInput!], $notifications: AgentNotificationsInput,
		$confirm: [String!], $askModel: String, $forget: Boolean) {
		UpdateAgent(enabled: $enabled, name: $name, instructions: $instructions, language: $language,
			voice: $voice, categories: $categories, notifications: $notifications, confirm: $confirm,
			askModel: $askModel, forget: $forget) ` + agentViewSelection + `
	}`

	DocumentGrantAgentMailbox = `mutation ($mailboxId: String!, $policy: AgentMailboxInput) {
		GrantAgentMailbox(mailboxId: $mailboxId, policy: $policy) ` + agentViewSelection + `
	}`

	DocumentRevokeAgentMailbox = `mutation ($mailboxId: String!) {
		RevokeAgentMailbox(mailboxId: $mailboxId) ` + agentViewSelection + `
	}`

	DocumentAgentUsage = `query ($since: DateTime, $by: String) {
		AgentUsage(since: $since, by: $by) { key totals { promptTokens completionTokens cacheReadTokens cacheWriteTokens calls } }
	}`

	DocumentListAgents = `query { ListAgents ` + agentSummarySelection + ` }`

	DocumentAgentServerUsage = `query ($since: DateTime, $by: String) {
		AgentServerUsage(since: $since, by: $by) { key totals { promptTokens completionTokens cacheReadTokens cacheWriteTokens calls } }
	}`

	DocumentListAgentDeadLetters = `query { ListAgentDeadLetters ` + agentJobSelection + ` }`

	DocumentSetAgentLimit = `mutation ($agentId: String!, $dailyTokens: Int!) {
		SetAgentLimit(agentId: $agentId, dailyTokens: $dailyTokens) ` + agentSummarySelection + `
	}`

	DocumentSetAgentDisabled = `mutation ($agentId: String!, $disabled: Boolean!) {
		SetAgentDisabled(agentId: $agentId, disabled: $disabled) ` + agentSummarySelection + `
	}`

	DocumentRetryAgentJob = `mutation ($jobId: String!) { RetryAgentJob(jobId: $jobId) ` + agentJobSelection + ` }`
)

// AgentComputersView is which computers the caller has attached.
type AgentComputersView struct {
	Allowed   bool                `json:"allowed"`
	Computers []AgentComputerView `json:"computers"`
}

// AgentComputerView is one attached computer.
type AgentComputerView struct {
	Name   string    `json:"name"`
	System string    `json:"system,omitempty"`
	Since  time.Time `json:"since"`
}

// ReadAgentComputers says which computers the server sees for the caller.
func ReadAgentComputers(ctx context.Context, connection *Client) (*AgentComputersView, error) {
	var result struct {
		ReadAgentComputers *AgentComputersView `json:"ReadAgentComputers"`
	}
	if err := connection.Execute(ctx, `query { ReadAgentComputers { allowed computers { name system since } } }`, nil, &result); err != nil {
		return nil, err
	}
	return result.ReadAgentComputers, nil
}

// AgentChannel is one chat app of the caller's, as the API shows it.
type AgentChannel struct {
	Kind       string     `json:"kind"`
	HasToken   bool       `json:"hasToken"`
	BotName    string     `json:"botName,omitempty"`
	Linked     bool       `json:"linked"`
	LinkedName string     `json:"linkedName,omitempty"`
	LinkCode   string     `json:"linkCode,omitempty"`
	Enabled    bool       `json:"enabled"`
	Running    bool       `json:"running"`
	LastError  string     `json:"lastError,omitempty"`
	LastSeenAt *time.Time `json:"lastSeenAt,omitempty"`
}

const agentChannelSelection = `{ kind hasToken botName linked linkedName linkCode enabled running lastError lastSeenAt }`

// ListAgentChannels is the caller's chat apps.
func ListAgentChannels(ctx context.Context, connection *Client) ([]*AgentChannel, error) {
	var result struct {
		ListAgentChannels []*AgentChannel `json:"ListAgentChannels"`
	}
	if err := connection.Execute(ctx, `query { ListAgentChannels `+agentChannelSelection+` }`, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAgentChannels, nil
}

// SetAgentChannel sets a chat app's bot token, or whether it runs.
func SetAgentChannel(ctx context.Context, connection *Client, kind, token string, enabled *bool) (*AgentChannel, error) {
	var result struct {
		SetAgentChannel *AgentChannel `json:"SetAgentChannel"`
	}
	variables := map[string]any{"kind": kind}
	if token != "" {
		variables["token"] = token
	}
	if enabled != nil {
		variables["enabled"] = *enabled
	}
	if err := connection.Execute(ctx, `mutation ($kind: String!, $token: String, $enabled: Boolean) { SetAgentChannel(kind: $kind, token: $token, enabled: $enabled) `+agentChannelSelection+` }`, variables, &result); err != nil {
		return nil, err
	}
	return result.SetAgentChannel, nil
}

// UnlinkAgentChannel drops the linked chat and draws a new code.
func UnlinkAgentChannel(ctx context.Context, connection *Client, kind string) (*AgentChannel, error) {
	var result struct {
		UnlinkAgentChannel *AgentChannel `json:"UnlinkAgentChannel"`
	}
	if err := connection.Execute(ctx, `mutation ($kind: String!) { UnlinkAgentChannel(kind: $kind) `+agentChannelSelection+` }`, map[string]any{"kind": kind}, &result); err != nil {
		return nil, err
	}
	return result.UnlinkAgentChannel, nil
}

// RemoveAgentChannel forgets a chat app's bot.
func RemoveAgentChannel(ctx context.Context, connection *Client, kind string) error {
	var result struct {
		RemoveAgentChannel bool `json:"RemoveAgentChannel"`
	}
	return connection.Execute(ctx, `mutation ($kind: String!) { RemoveAgentChannel(kind: $kind) }`, map[string]any{"kind": kind}, &result)
}

// ReadAgent returns the caller's agent and sources.
func ReadAgent(ctx context.Context, connection *Client) (*AgentView, error) {
	var result struct {
		ReadAgent *AgentView `json:"ReadAgent"`
	}
	if err := connection.Execute(ctx, DocumentReadAgent, nil, &result); err != nil {
		return nil, err
	}
	return result.ReadAgent, nil
}

// UpdateAgent changes the caller's agent; variables are the mutation's
// arguments by name.
func UpdateAgent(ctx context.Context, connection *Client, variables map[string]any) (*AgentView, error) {
	var result struct {
		UpdateAgent *AgentView `json:"UpdateAgent"`
	}
	if err := connection.Execute(ctx, DocumentUpdateAgent, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateAgent, nil
}

// GrantAgentMailbox lets the agent reach a mailbox, with a policy or the
// stored one.
func GrantAgentMailbox(ctx context.Context, connection *Client, mailboxId string, policy any) (*AgentView, error) {
	var result struct {
		GrantAgentMailbox *AgentView `json:"GrantAgentMailbox"`
	}
	variables := map[string]any{"mailboxId": mailboxId}
	if policy != nil {
		variables["policy"] = policy
	}
	if err := connection.Execute(ctx, DocumentGrantAgentMailbox, variables, &result); err != nil {
		return nil, err
	}
	return result.GrantAgentMailbox, nil
}

// RevokeAgentMailbox stops the agent reaching a mailbox.
func RevokeAgentMailbox(ctx context.Context, connection *Client, mailboxId string) (*AgentView, error) {
	var result struct {
		RevokeAgentMailbox *AgentView `json:"RevokeAgentMailbox"`
	}
	if err := connection.Execute(ctx, DocumentRevokeAgentMailbox, map[string]any{"mailboxId": mailboxId}, &result); err != nil {
		return nil, err
	}
	return result.RevokeAgentMailbox, nil
}

// AgentUsage is the caller's own token use.
func AgentUsage(ctx context.Context, connection *Client, since *time.Time, by string) ([]*AgentUsageRow, error) {
	var result struct {
		AgentUsage []*AgentUsageRow `json:"AgentUsage"`
	}
	variables := map[string]any{}
	if since != nil {
		variables["since"] = since.Format(time.RFC3339)
	}
	if by != "" {
		variables["by"] = by
	}
	if err := connection.Execute(ctx, DocumentAgentUsage, variables, &result); err != nil {
		return nil, err
	}
	return result.AgentUsage, nil
}

// ListAgents is every person's agent, for an operator.
func ListAgents(ctx context.Context, connection *Client) ([]*AgentSummary, error) {
	var result struct {
		ListAgents []*AgentSummary `json:"ListAgents"`
	}
	if err := connection.Execute(ctx, DocumentListAgents, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAgents, nil
}

// AgentServerUsage is token use across the server, for an operator.
func AgentServerUsage(ctx context.Context, connection *Client, since *time.Time, by string) ([]*AgentUsageRow, error) {
	var result struct {
		AgentServerUsage []*AgentUsageRow `json:"AgentServerUsage"`
	}
	variables := map[string]any{}
	if since != nil {
		variables["since"] = since.Format(time.RFC3339)
	}
	if by != "" {
		variables["by"] = by
	}
	if err := connection.Execute(ctx, DocumentAgentServerUsage, variables, &result); err != nil {
		return nil, err
	}
	return result.AgentServerUsage, nil
}

// ListAgentDeadLetters is the jobs the worker gave up on.
func ListAgentDeadLetters(ctx context.Context, connection *Client) ([]*AgentJob, error) {
	var result struct {
		ListAgentDeadLetters []*AgentJob `json:"ListAgentDeadLetters"`
	}
	if err := connection.Execute(ctx, DocumentListAgentDeadLetters, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAgentDeadLetters, nil
}

// SetAgentLimit sets one person's daily budget.
func SetAgentLimit(ctx context.Context, connection *Client, agentId string, dailyTokens int64) (*AgentSummary, error) {
	var result struct {
		SetAgentLimit *AgentSummary `json:"SetAgentLimit"`
	}
	if err := connection.Execute(ctx, DocumentSetAgentLimit, map[string]any{"agentId": agentId, "dailyTokens": dailyTokens}, &result); err != nil {
		return nil, err
	}
	return result.SetAgentLimit, nil
}

// SetAgentDisabled switches a person's agent off or on.
func SetAgentDisabled(ctx context.Context, connection *Client, agentId string, disabled bool) (*AgentSummary, error) {
	var result struct {
		SetAgentDisabled *AgentSummary `json:"SetAgentDisabled"`
	}
	if err := connection.Execute(ctx, DocumentSetAgentDisabled, map[string]any{"agentId": agentId, "disabled": disabled}, &result); err != nil {
		return nil, err
	}
	return result.SetAgentDisabled, nil
}

// RetryAgentJob puts a dead job back.
func RetryAgentJob(ctx context.Context, connection *Client, jobId string) (*AgentJob, error) {
	var result struct {
		RetryAgentJob *AgentJob `json:"RetryAgentJob"`
	}
	if err := connection.Execute(ctx, DocumentRetryAgentJob, map[string]any{"jobId": jobId}, &result); err != nil {
		return nil, err
	}
	return result.RetryAgentJob, nil
}

// DocumentDraftReply asks the agent to write a reply to a message.
const DocumentDraftReply = `mutation ($itemId: String!, $instructions: String) {
  DraftReply(itemId: $itemId, instructions: $instructions) { text model runId }
}`

// AgentDraft is a reply the agent wrote, for the person to read and send.
type AgentDraft struct {
	Text  string `json:"text"`
	Model string `json:"model"`
	RunID string `json:"runId"`
}

// DraftReply has the agent write a reply to the message an item holds.
func DraftReply(ctx context.Context, connection *Client, itemId, instructions string) (*AgentDraft, error) {
	var result struct {
		DraftReply *AgentDraft `json:"DraftReply"`
	}
	variables := map[string]any{"itemId": itemId}
	if instructions != "" {
		variables["instructions"] = instructions
	}
	if err := connection.Execute(ctx, DocumentDraftReply, variables, &result); err != nil {
		return nil, err
	}
	return result.DraftReply, nil
}

// DocumentListAgentReplies lists the replies the agent wrote.
const DocumentListAgentReplies = `query ($mailboxId: String, $status: String, $first: Int, $offset: Int) {
  ListAgentReplies(mailboxId: $mailboxId, status: $status, first: $first, offset: $offset) {
    total
    replies { id createdAt modifiedAt agentId mailboxId mailId threadId draftItemId runId status reason subject from to text sendAfter sentMailId sentAt }
  }
}`

// DocumentCancelAgentReply cancels a held reply.
const DocumentCancelAgentReply = `mutation ($replyId: String!) {
  CancelAgentReply(replyId: $replyId) { id status reason subject to sendAfter }
}`

// AgentReply is a reply the agent wrote on the person's behalf.
type AgentReply struct {
	ID          string     `json:"id"`
	CreatedAt   time.Time  `json:"createdAt"`
	ModifiedAt  time.Time  `json:"modifiedAt"`
	AgentID     string     `json:"agentId"`
	MailboxID   string     `json:"mailboxId"`
	MailID      string     `json:"mailId"`
	ThreadID    string     `json:"threadId"`
	DraftItemID string     `json:"draftItemId"`
	RunID       string     `json:"runId"`
	Status      string     `json:"status"`
	Reason      string     `json:"reason"`
	Subject     string     `json:"subject"`
	From        string     `json:"from"`
	To          string     `json:"to"`
	Text        string     `json:"text"`
	SendAfter   *time.Time `json:"sendAfter"`
	SentMailID  string     `json:"sentMailId"`
	SentAt      *time.Time `json:"sentAt"`
}

// AgentReplyPage is a page of replies.
type AgentReplyPage struct {
	Replies []*AgentReply `json:"replies"`
	Total   int64         `json:"total"`
}

// ListAgentReplies is the replies the agent wrote, newest first.
func ListAgentReplies(ctx context.Context, connection *Client, mailboxId, status string, first, offset int) (*AgentReplyPage, error) {
	var result struct {
		ListAgentReplies *AgentReplyPage `json:"ListAgentReplies"`
	}
	variables := map[string]any{"first": first, "offset": offset}
	if mailboxId != "" {
		variables["mailboxId"] = mailboxId
	}
	if status != "" {
		variables["status"] = status
	}
	if err := connection.Execute(ctx, DocumentListAgentReplies, variables, &result); err != nil {
		return nil, err
	}
	return result.ListAgentReplies, nil
}

// CancelAgentReply cancels a reply the agent is holding.
func CancelAgentReply(ctx context.Context, connection *Client, replyId string) (*AgentReply, error) {
	var result struct {
		CancelAgentReply *AgentReply `json:"CancelAgentReply"`
	}
	if err := connection.Execute(ctx, DocumentCancelAgentReply, map[string]any{"replyId": replyId}, &result); err != nil {
		return nil, err
	}
	return result.CancelAgentReply, nil
}
