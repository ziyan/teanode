package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/skills"
)

// Sharing the tools of an installed skill with another person, and the calls
// they make. docs/decisions/20261016-a-payment-skill-is-shared-call-by-call.md
// says why a payment is shared call by call, and docs/subsystems/skills.md
// ("Sharing a skill") how.
//
// A skill is shared by its owner, with one person in a circle they share, and
// the share is the owner's alone to take back. The owner sees every call made
// through it, with its arguments and what it came to; the other person sees
// their own. A call that waits is answered by the owner, as a person: no agent
// answers one, and neither does an operator signed in as the owner.
//
// Lending money and approving a call that spends it are done where a person
// taps, and the server holds that: a request is accepted only from the
// dashboard's session or from the phone app's own sign-in. A command line
// token, an API token and the console are refused, because an agent can drive
// the person's command line with their saved sign-in. Taking money away
// (declining, unsharing, lowering limits, asking every time) is open to all.

// AgentSkillShareQuery reads shares and their calls.
type AgentSkillShareQuery interface {
	// The skill shares the caller made and the ones made to them, by who
	// owns each. Needs circle:use.
	ListSkillShares(ctx context.Context) ([]*SkillShareView, error)

	// The calls made through one share, waiting ones first and then newest
	// first. The share's owner sees all of them, with their arguments; the
	// person it is shared with sees theirs. With isShareEnded and no share,
	// the calls of shares that have ended instead: the record stays when a
	// share is taken back or the two stop sharing a circle, and the owner
	// sees every call of it, the other person their own. Needs circle:use.
	ListSkillShareCalls(ctx context.Context, arguments ListSkillShareCallsArguments) ([]*SkillShareCallView, error)
}

// AgentSkillShareMutation shares, takes back, and answers.
type AgentSkillShareMutation interface {
	// Share named tools of an installed skill with a person the caller
	// shares a circle with, run on the caller's own computer as the caller,
	// within limits. Sharing the same skill with the same person again
	// changes the tools and limits. Needs agent:use and circle:use.
	//
	// Creating a share, raising its limits or tools, and turning off asking
	// every time are accepted only from the dashboard's session or the phone
	// app. Asking every time, and lowering what a share allows, are accepted
	// from anywhere, because they only take away.
	ShareSkill(ctx context.Context, arguments ShareSkillArguments) (*SkillShareView, error)

	// Take a share back, with the record of its calls. The other person
	// loses the tools at once. Needs agent:use.
	UnshareSkill(ctx context.Context, arguments UnshareSkillArguments) (bool, error)

	// Answer a call that is waiting for approval, as its owner: approving
	// runs exactly the call that was shown, once, on the owner's computer, and
	// declining stops it. Answering a call that is not waiting any more is
	// refused. Needs agent:use.
	//
	// Approving is accepted only from the dashboard's session or the phone
	// app's own sign-in; a command line or API token is refused with where to
	// approve it, because an agent can drive the owner's command line with
	// their saved sign-in. Declining is open to any sign-in of the owner. Both
	// are refused to the agent's own operations, which carry the agent as the
	// actor, and to an operator signed in as the owner. A shell call whose
	// words look like the command that declines still asks the owner first
	// (tools.AnswersSkillShareCall), as a courtesy; the server is the guard.
	AnswerSkillShareCall(ctx context.Context, arguments AnswerSkillShareCallArguments) (*SkillShareCallView, error)
}

// SkillShareView is one skill share.
type SkillShareView struct {
	ID string `json:"id"`

	// The skill, and the tools of it that are shared
	SkillName string   `json:"skillName"`
	ToolNames []string `json:"toolNames"`

	// Whose skill it is
	OwnerUserID           string  `json:"ownerUserId"`
	OwnerUsername         string  `json:"ownerUsername"`
	OwnerName             string  `json:"ownerName"`
	OwnerAvatarURL        *string `json:"ownerAvatarUrl"`
	OwnerAvatarColorIndex int     `json:"ownerAvatarColorIndex"`

	// Who may call the tools
	GranteeUserID           string  `json:"granteeUserId"`
	GranteeUsername         string  `json:"granteeUsername"`
	GranteeName             string  `json:"granteeName"`
	GranteeAvatarURL        *string `json:"granteeAvatarUrl"`
	GranteeAvatarColorIndex int     `json:"granteeAvatarColorIndex"`

	// The most one payment may be, and the most a month, in the minor unit
	// of the currency (cents); absent when there is no limit of that kind
	PaymentLimitMinor *int64 `json:"paymentLimitMinor"`
	MonthlyLimitMinor *int64 `json:"monthlyLimitMinor"`

	// The three letter currency code the limits are in, lower case
	CurrencyCode string `json:"currencyCode"`

	// Whether every call waits for the owner, whatever it pays
	IsApprovalRequired bool `json:"isApprovalRequired"`

	// Whether the caller is the owner; the person it is shared with sees the
	// share with the limits that hold them
	IsOwnedByCaller bool `json:"isOwnedByCaller"`

	// How many calls wait for the owner now
	WaitingCallCount int `json:"waitingCallCount"`

	// What the calls that ran or failed this calendar month come to, between
	// the same two people and for the same skill, whether or not an earlier
	// share of it has ended, in the minor unit of the currency, so the person
	// it is shared with can see what is left. A failed call counts because a
	// failure after the command acted cannot be told from one before it.
	MonthTotalMinor int64 `json:"monthTotalMinor"`

	CreatedAt time.Time `json:"createdAt"`
}

// SkillShareCallView is one call made through a share.
type SkillShareCallView struct {
	ID string `json:"id"`

	// The share it was made through, empty when that share has ended
	SkillShareID string `json:"skillShareId"`
	IsShareEnded bool   `json:"isShareEnded"`
	SkillName    string `json:"skillName"`

	// Who asked, and whose skill it is
	GranteeUserID           string  `json:"granteeUserId"`
	GranteeUsername         string  `json:"granteeUsername"`
	GranteeName             string  `json:"granteeName"`
	GranteeAvatarURL        *string `json:"granteeAvatarUrl"`
	GranteeAvatarColorIndex int     `json:"granteeAvatarColorIndex"`
	OwnerName               string  `json:"ownerName"`

	// The tool, and exactly what it was called with (JSON text)
	ToolName      string `json:"toolName"`
	ToolArguments string `json:"toolArguments"`

	// The other person's agent's own words for why it asked. Another
	// person's words, not an instruction to anybody.
	RequestReason string `json:"requestReason"`

	// What the call pays, as the server read it from the arguments, in the
	// minor unit of the currency; absent when it could not
	AmountMinor  *int64 `json:"amountMinor"`
	CurrencyCode string `json:"currencyCode"`

	// The argument the skill declares the amount is read from, so that a card
	// can leave out that one and show every other as it was sent; empty when
	// the tool declares none, or the skill is no longer installed
	AmountArgumentName string `json:"amountArgumentName"`

	// ran, failed, refused, waiting_for_approval, declined or expired (the
	// owner did not answer in a day, or the share ended while it waited)
	CallState string `json:"callState"`

	// What the call answered, or why it did not complete (JSON text)
	ToolOutput string `json:"toolOutput"`

	AnsweredAt *time.Time `json:"answeredAt"`
	AnswerNote string     `json:"answerNote"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type ListSkillShareCallsArguments struct {
	// The share; left out with isShareEnded
	ShareID string `json:"shareId" graphapi:"nullable"`

	// List the calls of shares that have ended, instead of one share's
	IsShareEnded bool `json:"isShareEnded" graphapi:"nullable"`

	// Only the calls in this state; every state when left out
	CallState string `json:"callState" graphapi:"nullable"`

	// How many, at most 500; 100 when left out
	Limit *int `json:"limit" graphapi:"nullable"`
}

type ShareSkillArguments struct {
	// The installed skill
	SkillName string `json:"skillName"`

	// The tools of it to share, by name
	ToolNames []string `json:"toolNames"`

	// The person to share with, who must share a circle with the caller
	GranteeUserID string `json:"granteeUserId"`

	// The most one payment may be, in the minor unit of the currency; no
	// limit of that kind when left out
	PaymentLimitMinor *int64 `json:"paymentLimitMinor" graphapi:"nullable"`

	// The most a calendar month may come to, in the minor unit of the
	// currency; no limit of that kind when left out
	MonthlyLimitMinor *int64 `json:"monthlyLimitMinor" graphapi:"nullable"`

	// The three letter currency code the limits are in; needed with a limit
	CurrencyCode string `json:"currencyCode" graphapi:"nullable"`

	// Make every call wait for the caller's approval, whatever it pays
	IsApprovalRequired bool `json:"isApprovalRequired" graphapi:"nullable"`
}

type UnshareSkillArguments struct {
	ShareID string `json:"shareId"`
}

type AnswerSkillShareCallArguments struct {
	CallID string `json:"callId"`

	// Approve and run the call, or decline it
	IsApproved bool `json:"isApproved"`

	// A word for the other person, shown to them and to their agent as
	// yours
	AnswerNote string `json:"answerNote" graphapi:"nullable"`
}

// approveOnTheDashboard is what a request is told when it is one that only a
// person's tap may make and it did not come as one.
const approveOnTheDashboard = api.TapRequiredPhrase + ": lending money, and approving what spends it, are done where a person taps, not from a command line or an API token"

// shareOnTheDashboard is what a share is told when it is one that only a
// person's tap may make: any share that does not ask every time and does not
// lower an earlier share, whether or not the tools pay.
const shareOnTheDashboard = api.TapRequiredPhrase + ": from a command line or an API token a skill is shared only with every call asking you, or with no more than an earlier share of it allowed; any other share is made where a person taps"

func describeSkillShare(share *models.SkillShare, callerUserId string) *SkillShareView {
	return &SkillShareView{
		ID: share.ID, SkillName: share.SkillName, ToolNames: share.ToolNames,
		OwnerUserID: share.OwnerUserID, OwnerUsername: share.OwnerUsername, OwnerName: share.OwnerName,
		OwnerAvatarURL: avatarUrl(share.OwnerUser), OwnerAvatarColorIndex: avatar.InitialColorIndex(share.OwnerUserID),
		GranteeUserID: share.GranteeUserID, GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName,
		GranteeAvatarURL: avatarUrl(share.GranteeUser), GranteeAvatarColorIndex: avatar.InitialColorIndex(share.GranteeUserID),
		PaymentLimitMinor: share.PaymentLimitMinor, MonthlyLimitMinor: share.MonthlyLimitMinor,
		CurrencyCode: share.CurrencyCode, IsApprovalRequired: share.IsApprovalRequired,
		IsOwnedByCaller: share.OwnerUserID == callerUserId, CreatedAt: share.CreatedAt,
	}
}

func describeSkillShareCall(call *models.SkillShareCall, share *models.SkillShare) *SkillShareCallView {
	arguments, _ := json.Marshal(call.ToolArguments)
	// A call of a share that has ended is named by the call itself, which
	// carries what it was a call of.
	if share == nil {
		share = &models.SkillShare{
			SkillName: call.SkillName, OwnerUserID: call.OwnerUserID, OwnerUsername: call.OwnerUsername, OwnerName: call.OwnerName,
			GranteeUserID: call.GranteeUserID, GranteeUsername: call.GranteeUsername, GranteeName: call.GranteeName, GranteeUser: call.GranteeUser,
		}
	}
	view := &SkillShareCallView{
		ID: call.ID, SkillShareID: call.SkillShareID, IsShareEnded: call.IsShareEnded(), SkillName: share.SkillName,
		GranteeUserID: share.GranteeUserID, GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName,
		GranteeAvatarURL: avatarUrl(share.GranteeUser), GranteeAvatarColorIndex: avatar.InitialColorIndex(share.GranteeUserID),
		OwnerName: share.OwnerName,
		ToolName:  call.ToolName, ToolArguments: string(arguments), RequestReason: call.RequestReason,
		AmountMinor: call.AmountMinor, CurrencyCode: call.CurrencyCode, CallState: string(call.CallState),
		AnsweredAt: call.AnsweredAt, AnswerNote: call.AnswerNote, ExpiresAt: call.ExpiresAt, CreatedAt: call.CreatedAt,
	}
	if len(call.ToolOutput) > 0 {
		output, _ := json.Marshal(call.ToolOutput)
		view.ToolOutput = string(output)
	}
	return view
}

// nameAmountArguments says on each call which of its arguments the skill
// declares the amount is read from, by the skill as it is installed now. A
// call of a skill that has been taken off, or whose tool is gone, names none.
func (self *graph) nameAmountArguments(ctx context.Context, views []*SkillShareCallView) error {
	tx := self.transaction(ctx)
	installed := map[string]*skills.Skill{}
	for _, view := range views {
		skill, isKnown := installed[view.SkillName]
		if !isKnown {
			row, err := tx.GetAgentSkill(view.SkillName)
			if err != nil {
				return err
			}
			if row != nil {
				if parsed, err := skills.Parse([]byte(row.Content)); err == nil {
					skill = parsed
				}
			}
			installed[view.SkillName] = skill
		}
		if skill == nil {
			continue
		}
		if tool := skill.Tool(view.ToolName); tool != nil {
			view.AmountArgumentName = tool.AmountField
		}
	}
	return nil
}

// skillShareOfParty is the share, when the user is its owner or the person it
// is shared with; anyone else, and a share that does not exist, is not found.
func (self *graph) skillShareOfParty(ctx context.Context, user *models.User, shareId string) (*models.SkillShare, error) {
	share, err := self.transaction(ctx).GetSkillShare(strings.TrimSpace(shareId))
	if err != nil {
		return nil, err
	}
	if share == nil || (share.OwnerUserID != user.ID && share.GranteeUserID != user.ID) {
		return nil, api.ErrNotFound
	}
	return share, nil
}

func (self *graph) ListSkillShares(ctx context.Context) ([]*SkillShareView, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	mine, err := tx.ListSkillSharesBy(user.ID)
	if err != nil {
		return nil, err
	}
	withMe, err := tx.ListSkillSharesReaching(user.ID)
	if err != nil {
		return nil, err
	}
	views := make([]*SkillShareView, 0, len(mine)+len(withMe))
	for _, share := range append(mine, withMe...) {
		view := describeSkillShare(share, user.ID)
		waiting, err := tx.ListSkillShareCalls(share.ID, models.SkillShareCallWaitingForApproval, 500)
		if err != nil {
			return nil, err
		}
		view.WaitingCallCount = len(waiting)
		if share.CurrencyCode != "" {
			from, until := skills.ShareMonth(time.Now())
			if view.MonthTotalMinor, err = tx.SkillShareMonthTotal(share.OwnerAgentID, share.GranteeUserID, share.SkillName, share.CurrencyCode, from, until); err != nil {
				return nil, err
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func (self *graph) ListSkillShareCalls(ctx context.Context, arguments ListSkillShareCallsArguments) ([]*SkillShareCallView, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return nil, err
	}
	callState := models.SkillShareCallState(strings.TrimSpace(arguments.CallState))
	if callState != "" && !callState.IsValid() {
		return nil, fmt.Errorf("%w: %q is not a state of a call", api.ErrInvalidArguments, arguments.CallState)
	}
	limit := 0
	if arguments.Limit != nil {
		limit = *arguments.Limit
	}
	if arguments.IsShareEnded {
		if strings.TrimSpace(arguments.ShareID) != "" {
			return nil, fmt.Errorf("%w: a share that has ended has no identifier to name; leave shareId out", api.ErrInvalidArguments)
		}
		calls, err := self.transaction(ctx).ListEndedSkillShareCalls(user.ID, callState, limit)
		if err != nil {
			return nil, err
		}
		views := make([]*SkillShareCallView, 0, len(calls))
		for _, call := range calls {
			views = append(views, describeSkillShareCall(call, nil))
		}
		if err := self.nameAmountArguments(ctx, views); err != nil {
			return nil, err
		}
		return views, nil
	}
	if strings.TrimSpace(arguments.ShareID) == "" {
		return nil, fmt.Errorf("%w: say which share, or ask for the shares that ended with isShareEnded", api.ErrInvalidArguments)
	}
	share, err := self.skillShareOfParty(ctx, user, arguments.ShareID)
	if err != nil {
		return nil, err
	}
	calls, err := self.transaction(ctx).ListSkillShareCalls(share.ID, callState, limit)
	if err != nil {
		return nil, err
	}
	views := make([]*SkillShareCallView, 0, len(calls))
	for _, call := range calls {
		views = append(views, describeSkillShareCall(call, share))
	}
	if err := self.nameAmountArguments(ctx, views); err != nil {
		return nil, err
	}
	return views, nil
}

func (self *graph) ShareSkill(ctx context.Context, arguments ShareSkillArguments) (*SkillShareView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	if _, err := self.requirePermission(ctx, models.PermissionCircleUse); err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	skillName := strings.ToLower(strings.TrimSpace(arguments.SkillName))
	row, err := tx.GetAgentSkill(skillName)
	if err != nil {
		return nil, err
	}
	// A skill that is not installed and switched on has nothing to share.
	if row == nil || !row.Enabled {
		return nil, api.ErrNotFound
	}
	skill, err := skills.Parse([]byte(row.Content))
	if err != nil {
		return nil, fmt.Errorf("%w: the %s skill cannot be read", api.ErrInvalidArguments, skillName)
	}
	owner, err := tx.GetAgentByUser(principal.User.ID)
	if err != nil {
		return nil, err
	}
	// Their computer is where it runs, and it is attached only to an agent
	// that is on.
	if !owner.Active() {
		return nil, fmt.Errorf("%w: your agent is switched off, and a shared skill runs on the computer it is attached to", api.ErrInvalidArguments)
	}
	currencyCode := ""
	if strings.TrimSpace(arguments.CurrencyCode) != "" {
		var isCode bool
		if currencyCode, isCode = models.NormalizeCurrencyCode(arguments.CurrencyCode); !isCode {
			return nil, fmt.Errorf("%w: a currency is three letters", api.ErrInvalidArguments)
		}
	}
	for _, limit := range []*int64{arguments.PaymentLimitMinor, arguments.MonthlyLimitMinor} {
		if limit != nil && *limit <= 0 {
			return nil, fmt.Errorf("%w: a limit is above zero", api.ErrInvalidArguments)
		}
	}
	if err := skill.CheckShareable(arguments.ToolNames, skills.ShareLimits{
		PaymentLimitMinor: arguments.PaymentLimitMinor, MonthlyLimitMinor: arguments.MonthlyLimitMinor,
		CurrencyCode: currencyCode, IsApprovalRequired: arguments.IsApprovalRequired,
	}); err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, strings.TrimPrefix(err.Error(), "skills: "))
	}
	terms := &models.SkillShare{
		SkillName: skill.Name, ToolNames: arguments.ToolNames, GranteeUserID: strings.TrimSpace(arguments.GranteeUserID),
		PaymentLimitMinor: arguments.PaymentLimitMinor, MonthlyLimitMinor: arguments.MonthlyLimitMinor,
		CurrencyCode: currencyCode, IsApprovalRequired: arguments.IsApprovalRequired,
	}
	// Lending money is the person's tap. A share that opens nothing beyond
	// what was already open is not lending more, and is let through from
	// anywhere: asking every time, and lowering what a share allows.
	replaced, err := self.ownSkillShare(tx, principal.User.ID, terms)
	if err != nil {
		return nil, err
	}
	if !principal.IsPersonsTap() && !terms.OpensNothingBeyond(replaced) {
		return nil, fmt.Errorf("%w: %s", api.ErrPermissionDenied, shareOnTheDashboard)
	}
	saved, err := tx.ShareSkill(principal.User.ID, terms)
	if err != nil {
		// A person who shares no circle with the caller, one who does not
		// exist: both answer as nothing found, because which it was is not
		// the caller's to learn.
		return nil, translateError(err)
	}
	if replaced == nil || !terms.OpensNothingBeyond(replaced) {
		if err := self.tellAboutShare(ctx, principal.User, models.ShareReceivedNote{
			SharerName: principal.User.DisplayName(), ShareKind: models.ShareKindSkill, ShareSubject: skill.Name,
		}, saved.GranteeUserID, ""); err != nil {
			return nil, err
		}
	}
	log.Noticef("%s shared the %s skill with someone", principal.User.Username, skill.Name)
	return describeSkillShare(saved, principal.User.ID), nil
}

// ownSkillShare is the share the owner already made of this skill with this
// person, or nil.
func (self *graph) ownSkillShare(tx db.Transaction, ownerUserId string, terms *models.SkillShare) (*models.SkillShare, error) {
	shares, err := tx.ListSkillSharesBy(ownerUserId)
	if err != nil {
		return nil, err
	}
	for _, share := range shares {
		if share.SkillName == terms.SkillName && share.GranteeUserID == terms.GranteeUserID {
			return share, nil
		}
	}
	return nil, nil
}

func (self *graph) UnshareSkill(ctx context.Context, arguments UnshareSkillArguments) (bool, error) {
	principal, err := self.requireShareOwner(ctx)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	share, err := tx.GetSkillShare(strings.TrimSpace(arguments.ShareID))
	if err != nil {
		return false, err
	}
	// The share is the owner's to take back; anyone else is told there is no
	// such share.
	if share == nil || share.OwnerUserID != principal.User.ID {
		return false, api.ErrNotFound
	}
	if err := tx.DeleteSkillShare(share.ID); err != nil {
		return false, translateError(err)
	}
	return true, nil
}

func (self *graph) AnswerSkillShareCall(ctx context.Context, arguments AnswerSkillShareCallArguments) (*SkillShareCallView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	// The agent's own operations carry the agent as the actor, and an
	// operator signed in as the owner is not the owner: neither answers a
	// call that spends the owner's money. A request that carries the owner's
	// own sign-in is not told apart from the owner (see the operation's
	// comment for what holds that case).
	if db.PrincipalFromContext(ctx).ActorKind == models.AuditActorAgent || principal.Impersonator != nil {
		return nil, api.ErrNotFound
	}
	// Approving spends the owner's money on the owner's computer, and is a
	// person's tap: the dashboard's session or the phone app. A command line
	// token is what an agent holds when it drives the person's computer.
	// Declining takes nothing, and is left open to the owner's other sign-ins.
	if arguments.IsApproved && !principal.IsPersonsTap() {
		return nil, fmt.Errorf("%w: %s", api.ErrPermissionDenied, approveOnTheDashboard)
	}
	worker := self.agentWorker()
	if worker == nil {
		return nil, agent.ErrUnavailable
	}
	// The answer runs the call, which takes as long as the call does, and is
	// not part of this request's transaction.
	answered, err := self.answerSkillShareCall(ctx, worker, principal.User.ID, arguments)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	share, err := tx.GetSkillShare(answered.SkillShareID)
	if err != nil || share == nil {
		return nil, api.ErrNotFound
	}
	log.Noticef("%s %s a call of a skill they shared", principal.User.Username, map[bool]string{true: "approved", false: "declined"}[arguments.IsApproved])
	view := describeSkillShareCall(answered, share)
	if err := self.nameAmountArguments(ctx, []*SkillShareCallView{view}); err != nil {
		return nil, err
	}
	return view, nil
}

// answerSkillShareCall gives the worker the owner's word and says what came of
// it. A call answered meanwhile, by the owner somewhere else or by the clock, is
// refused and not run again; the screen reads the call to see how it was
// answered.
func (self *graph) answerSkillShareCall(ctx context.Context, worker *agent.Agent, ownerUserId string, arguments AnswerSkillShareCallArguments) (*models.SkillShareCall, error) {
	answered, err := worker.AnswerSharedCall(ctx, ownerUserId, strings.TrimSpace(arguments.CallID), arguments.IsApproved, arguments.AnswerNote)
	switch {
	case err == nil:
		return answered, nil
	case errors.Is(err, agent.ErrCallNotWaiting):
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	case errors.Is(err, agent.ErrSharedUnavailable):
		return nil, api.ErrNotFound
	case errors.Is(err, agent.ErrSharedOwnerAway):
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	if translated := translateError(err); translated != err {
		return nil, translated
	}
	// The owner's computer is not connected, or the call could not be tried:
	// the call stays waiting, and the owner is told why.
	return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
}
