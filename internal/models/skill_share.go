package models

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// A person shares named tools of an installed skill with one person in a
// circle they share. The other person's agent calls them, each call runs on
// the owner's computer as the owner, and a call above the limits, with no
// amount the server can read, or on a share set to ask every time, waits for
// the owner's approval.
// docs/decisions/20261016-a-payment-skill-is-shared-call-by-call.md says why,
// and docs/subsystems/skills.md ("Sharing a skill") how it works.

// SkillShareCallState says how one call of a shared tool stands.
type SkillShareCallState string

// The states. Ran is written before the call is carried out and changed to
// failed if it then errors, so that the month's total counts a call in flight.
const (
	// SkillShareCallRan is a call carried out on the owner's computer.
	SkillShareCallRan SkillShareCallState = "ran"

	// SkillShareCallFailed is a call that was tried and did not complete, or
	// could not be tried because the owner's computer was not connected.
	SkillShareCallFailed SkillShareCallState = "failed"

	// SkillShareCallRefused is a call the server refused before it ran:
	// a request for a spend request that did not come through the share.
	SkillShareCallRefused SkillShareCallState = "refused"

	// SkillShareCallWaitingForApproval is a call that needs the owner's word.
	SkillShareCallWaitingForApproval SkillShareCallState = "waiting_for_approval"

	// SkillShareCallDeclined is a call the owner said no to.
	SkillShareCallDeclined SkillShareCallState = "declined"

	// SkillShareCallExpired is a call the owner did not answer in time.
	SkillShareCallExpired SkillShareCallState = "expired"
)

// SkillShareCallStates is every state.
var SkillShareCallStates = []SkillShareCallState{
	SkillShareCallRan, SkillShareCallFailed, SkillShareCallRefused,
	SkillShareCallWaitingForApproval, SkillShareCallDeclined, SkillShareCallExpired,
}

// IsValid says the state is one of the six.
func (self SkillShareCallState) IsValid() bool {
	for _, known := range SkillShareCallStates {
		if self == known {
			return true
		}
	}
	return false
}

// SkillShareNoLimitsWords says what a share allows that has no limit and does
// not ask every time, in one wording for the dashboard, the app, the command
// line and the agent. Nothing that can be priced goes through unasked there
// (a tool that pays is shared with a limit or with asking every time, and a
// call the server cannot price waits); what does is a tool that works only on
// something an earlier call through the share made, such as checking or
// cancelling that payment, which pays nothing itself (skills.Tool.Weigh).
const SkillShareNoLimitsWords = "No limits: every call waits for approval, except one that only works on something an earlier call through this share made"

// SkillShareCallStatesAsked reads the states somebody asks to see calls in,
// by the words they say: the operation's own names, and "waiting" for
// waiting_for_approval. Each state once, in the order asked; none when no
// state was asked, which is every call. The command line and the agent's
// tool both read them this way.
func SkillShareCallStatesAsked(words []string) ([]SkillShareCallState, error) {
	var states []SkillShareCallState
	for _, word := range words {
		state := SkillShareCallState(strings.ToLower(strings.TrimSpace(word)))
		if state == "waiting" {
			state = SkillShareCallWaitingForApproval
		}
		if !state.IsValid() {
			return nil, fmt.Errorf("a call state is ran, failed, refused, waiting, declined or expired; %q is none of them", word)
		}
		if !slices.Contains(states, state) {
			states = append(states, state)
		}
	}
	return states, nil
}

// SkillShareCallWaitFor is how long a call waits for the owner before it
// expires.
const SkillShareCallWaitFor = 24 * time.Hour

// SkillShare gives one person the use of named tools of one skill, on the
// owner's computer, within limits.
type SkillShare struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	// OwnerAgentID is the agent of the person whose skill it is.
	OwnerAgentID string `json:"ownerAgentId"`

	// SkillName is the installed skill; ToolNames are the tools of it that
	// are shared, by the name the skill declares.
	SkillName string   `json:"skillName"`
	ToolNames []string `json:"toolNames"`

	// GranteeUserID is the person who may call them.
	GranteeUserID string `json:"granteeUserId"`

	// PaymentLimitMinor is the most one payment may be, and MonthlyLimitMinor
	// the most the ran and failed calls of a calendar month may come to, in the minor
	// unit of CurrencyCode (cents). Nil is no limit of that kind. A call above
	// either waits for the owner.
	PaymentLimitMinor *int64 `json:"paymentLimitMinor,omitempty"`
	MonthlyLimitMinor *int64 `json:"monthlyLimitMinor,omitempty"`

	// CurrencyCode is the three letters, lower case, the limits are in. Empty
	// when there are no limits.
	CurrencyCode string `json:"currencyCode,omitempty"`

	// IsApprovalRequired says every call waits for the owner, whatever its
	// amount.
	IsApprovalRequired bool `json:"isApprovalRequired"`

	// The rest is read along with the row. The owner is the account the
	// agent belongs to.
	OwnerUserID     string `json:"ownerUserId,omitempty"`
	OwnerUsername   string `json:"ownerUsername,omitempty"`
	OwnerName       string `json:"ownerName,omitempty"`
	OwnerUser       *User  `json:"-"`
	GranteeUsername string `json:"granteeUsername,omitempty"`
	GranteeName     string `json:"granteeName,omitempty"`
	GranteeUser     *User  `json:"-"`
}

// IsSharing says whether the tool is one of those shared.
func (self *SkillShare) IsSharing(toolName string) bool {
	for _, shared := range self.ToolNames {
		if shared == toolName {
			return true
		}
	}
	return false
}

// OpensNothingBeyond says whether sharing these terms lets the person do no
// more than the share they replace already did, so that the change only takes
// something away: it asks the owner every time, or it keeps to tools the share
// already had, in the same currency, with limits at or below the old ones.
// With no share to replace (nil), only asking every time qualifies, because
// that is the one share under which nothing runs without the owner's word.
func (self *SkillShare) OpensNothingBeyond(replaced *SkillShare) bool {
	if self.IsApprovalRequired {
		return true
	}
	if replaced == nil || replaced.IsApprovalRequired {
		return false
	}
	for _, toolName := range self.ToolNames {
		if !replaced.IsSharing(toolName) {
			return false
		}
	}
	// Limits in another currency cannot be compared with the old ones. When
	// the old share had none, there is nothing to compare.
	if (replaced.PaymentLimitMinor != nil || replaced.MonthlyLimitMinor != nil) && self.CurrencyCode != replaced.CurrencyCode {
		return false
	}
	return isLimitNoHigher(self.PaymentLimitMinor, replaced.PaymentLimitMinor) &&
		isLimitNoHigher(self.MonthlyLimitMinor, replaced.MonthlyLimitMinor)
}

// isLimitNoHigher says whether a limit is at most the one it replaces; no
// limit is the highest there is.
func isLimitNoHigher(limit, replaced *int64) bool {
	if replaced == nil {
		return true
	}
	return limit != nil && *limit <= *replaced
}

// SkillShareCall is one call of a shared tool, whatever came of it.
type SkillShareCall struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`

	// SkillShareID is the share it was made through, empty once that share
	// has ended: the call stays, with what it was a call of below.
	SkillShareID string `json:"skillShareId"`

	// OwnerAgentID, GranteeUserID and SkillName are what the call was a call
	// of, kept on the call so that it still says so when its share is gone.
	OwnerAgentID  string `json:"ownerAgentId"`
	GranteeUserID string `json:"granteeUserId"`
	SkillName     string `json:"skillName"`

	// GranteeRunID and GranteeConversationID are the turn of the grantee's
	// agent that made the call, and the conversation that turn was in: where
	// the outcome of a call that waited is told.
	GranteeRunID          string `json:"granteeRunId,omitempty"`
	GranteeConversationID string `json:"granteeConversationId,omitempty"`

	ToolName string `json:"toolName"`

	// ToolArguments are exactly what the grantee's agent called the tool
	// with, which is what the owner is shown and what approving runs.
	ToolArguments map[string]any `json:"toolArguments"`

	// RequestReason is the grantee's agent's own words for why. Another
	// person's words: shown as theirs, never read as an instruction.
	RequestReason string `json:"requestReason,omitempty"`

	// AmountMinor and CurrencyCode are what the server read from the
	// arguments by the skill's declaration; nil and empty when it could not.
	AmountMinor  *int64 `json:"amountMinor,omitempty"`
	CurrencyCode string `json:"currencyCode,omitempty"`

	CallState SkillShareCallState `json:"callState"`

	// ToolOutput is what the call answered, or why it did not complete.
	ToolOutput map[string]any `json:"toolOutput,omitempty"`

	AnsweredAt *time.Time `json:"answeredAt,omitempty"`
	AnswerNote string     `json:"answerNote,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	WokenAt    *time.Time `json:"wokenAt,omitempty"`

	// The people, read along with the calls of shares that ended, which have
	// no share to name them. Empty on any other call.
	OwnerUserID     string `json:"ownerUserId,omitempty"`
	OwnerUsername   string `json:"ownerUsername,omitempty"`
	OwnerName       string `json:"ownerName,omitempty"`
	GranteeUsername string `json:"granteeUsername,omitempty"`
	GranteeName     string `json:"granteeName,omitempty"`
	GranteeUser     *User  `json:"-"`
}

// IsShareEnded says the share the call was made through is gone.
func (self *SkillShareCall) IsShareEnded() bool {
	return self.SkillShareID == ""
}

// ToolArgumentsJSON is the arguments as stored.
func (self *SkillShareCall) ToolArgumentsJSON() []byte {
	encoded, err := json.Marshal(self.ToolArguments)
	if err != nil || len(encoded) == 0 {
		return []byte("{}")
	}
	return encoded
}

// SkillShareCallNote is the detail of a skill_share_call_waiting note: which call
// waits, in which share, and who asked, so that the card in the drawer and the
// app reads the rest of it from the call itself and shows its present state.
type SkillShareCallNote struct {
	SkillShareID  string `json:"skillShareId"`
	CallID        string `json:"callId"`
	GranteeName   string `json:"granteeName"`
	GranteeUserID string `json:"granteeUserId"`
}

// SkillShareCallDetail is the detail of a skill_share_call_waiting note.
func SkillShareCallDetail(note SkillShareCallNote) string {
	encoded, err := json.Marshal(note)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// ParseSkillShareCallNote reads the detail of a skill_share_call_waiting note, or
// returns nil when it is not one.
func ParseSkillShareCallNote(detail string) *SkillShareCallNote {
	var note SkillShareCallNote
	if err := json.Unmarshal([]byte(detail), &note); err != nil || note.CallID == "" || note.SkillShareID == "" {
		return nil
	}
	return &note
}

// NormalizeCurrencyCode is a currency code as it is stored: three letters,
// lower case. The second answer is false for anything else.
func NormalizeCurrencyCode(code string) (string, bool) {
	code = strings.ToLower(strings.TrimSpace(code))
	if len(code) != 3 {
		return "", false
	}
	for _, letter := range code {
		if letter < 'a' || letter > 'z' {
			return "", false
		}
	}
	return code, true
}

// CurrencyExponent is how many digits a currency's major unit is divided into:
// 2 for most (cents), 0 for those with no minor unit (yen), 3 for those with
// thousandths (dinars). The limits of a share are stored in the minor unit and
// shown and typed in the major one.
func CurrencyExponent(currencyCode string) int {
	switch strings.ToLower(currencyCode) {
	case "bif", "clp", "djf", "gnf", "jpy", "kmf", "krw", "mga", "pyg", "rwf", "ugx", "vnd", "vuv", "xaf", "xof", "xpf":
		return 0
	case "bhd", "iqd", "jod", "kwd", "lyd", "omr", "tnd":
		return 3
	}
	return 2
}

// FormatMinor writes an amount in minor units as people write money: 14.00
// USD. The code is upper case, the number has the currency's own digits.
func FormatMinor(minor int64, currencyCode string) string {
	exponent := CurrencyExponent(currencyCode)
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	divisor := int64(1)
	for range exponent {
		divisor *= 10
	}
	number := strconv.FormatInt(minor/divisor, 10)
	if exponent > 0 {
		number += "." + fmt.Sprintf("%0*d", exponent, minor%divisor)
	}
	if currencyCode == "" {
		return sign + number
	}
	return sign + number + " " + strings.ToUpper(currencyCode)
}

// ParseMinor reads an amount somebody typed in the currency's major unit
// ("100", "100.5", "100.50") into minor units. It refuses more digits than the
// currency has, a sign, a thousands separator, and anything that is not a
// number above zero, because a limit read wrongly is a limit that does not hold.
func ParseMinor(text, currencyCode string) (int64, error) {
	text = strings.TrimSpace(text)
	exponent := CurrencyExponent(currencyCode)
	whole, fraction, hasFraction := strings.Cut(text, ".")
	if whole == "" || (hasFraction && fraction == "" && exponent > 0) {
		return 0, fmt.Errorf("%q is not an amount", text)
	}
	if len(fraction) > exponent {
		return 0, fmt.Errorf("%q has more digits after the point than %s has", text, strings.ToUpper(currencyCode))
	}
	for _, digits := range []string{whole, fraction} {
		for _, digit := range digits {
			if digit < '0' || digit > '9' {
				return 0, fmt.Errorf("%q is not an amount", text)
			}
		}
	}
	if len(whole) > 15 {
		return 0, fmt.Errorf("%q is too large", text)
	}
	padded := whole + fraction + strings.Repeat("0", exponent-len(fraction))
	minor, err := strconv.ParseInt(padded, 10, 64)
	if err != nil || minor <= 0 {
		return 0, fmt.Errorf("%q is not an amount above zero", text)
	}
	return minor, nil
}
