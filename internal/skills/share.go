package skills

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Sharing a skill's tools with another person: what the server works out of a
// call before it lets the call run. The tables, the circles that must still be
// shared, and carrying the call out on the owner's computer are the callers';
// this file is only what the skill itself says about a call, and how that
// compares with a share's limits. docs/subsystems/skills.md ("Sharing a
// skill") says how the parts fit.

// FallbackScopedIDField is the parameter a tool is held to the scoped
// identifier rule by when it declares no scopedIdField of its own. The payment
// skill was written before the declaration existed, and its spend requests are
// named by this parameter.
const FallbackScopedIDField = "spend_request_id"

// exactIntegerMost is the largest whole number a JSON number carries without
// losing the low digits. An amount above it is not read: it would be
// compared as something other than what the skill is given.
const exactIntegerMost = 1 << 53

// Amount is what one call pays, as the server read it out of the arguments.
type Amount struct {
	// Minor is the amount in the currency's minor unit (cents), above zero.
	Minor int64

	// CurrencyCode is the three letters, lower case, or empty when the call
	// did not say or the skill declares no currencyField.
	CurrencyCode string
}

// ReadAmount is the amount of a call, or nil when the server cannot tell.
// Nil is the answer for a tool that declares no amountField, an action that
// is not one of the amountActions, an amount that is missing, not a whole
// number, not above zero or not exactly representable, and a string where a
// number belongs: all of them wait for the owner, because a payment the
// server could not price is the one a limit must not let through.
//
// The arguments are completed with the schema's defaults first, as a run does
// before it fills the skill's steps in, so the amount read is the amount the
// steps are given.
func (self *Tool) ReadAmount(arguments map[string]any) *Amount {
	if self.AmountField == "" {
		return nil
	}
	values := completedArguments(self, arguments)
	if self.ActionField != "" && len(self.AmountActions) > 0 {
		chosenAction, _ := values[self.ActionField].(string)
		isListed := false
		for _, listed := range self.AmountActions {
			if listed == chosenAction {
				isListed = true
			}
		}
		if !isListed {
			return nil
		}
	}
	minor, ok := wholeNumber(values[self.AmountField])
	if !ok || minor <= 0 {
		return nil
	}
	amount := &Amount{Minor: minor}
	if self.CurrencyField != "" {
		if code, isCode := currencyCodeOf(values[self.CurrencyField]); isCode {
			amount.CurrencyCode = code
		}
	}
	return amount
}

// completedArguments is the arguments a run would use: the schema's defaults,
// then what the call gave, a null giving nothing.
func completedArguments(tool *Tool, arguments map[string]any) map[string]any {
	values := map[string]any{}
	for name, declared := range defaults(tool.Parameters) {
		values[name] = declared
	}
	for name, given := range arguments {
		if given != nil {
			values[name] = given
		}
	}
	return values
}

// wholeNumber reads a JSON number that is a whole number. A string is not one,
// whatever it spells.
func wholeNumber(argument any) (int64, bool) {
	switch typed := argument.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed != math.Trunc(typed) || math.Abs(typed) > exactIntegerMost {
			return 0, false
		}
		return int64(typed), true
	}
	return 0, false
}

// currencyCodeOf reads three letters, as lower case.
func currencyCodeOf(argument any) (string, bool) {
	text, ok := argument.(string)
	if !ok {
		return "", false
	}
	text = strings.ToLower(strings.TrimSpace(text))
	if len(text) != 3 {
		return "", false
	}
	for _, letter := range text {
		if letter < 'a' || letter > 'z' {
			return "", false
		}
	}
	return text, true
}

// ScopedIDName is the parameter this tool's shared calls are held to the
// identifier rule by, or empty when it has none: the declared scopedIdField,
// else spend_request_id when the tool takes a parameter of that name.
//
// A tool that declares an amount is never scoped by the fallback, whatever
// parameters it takes: a payment tool that also takes a spend_request_id to
// report on an earlier payment is priced, and its report action, which has no
// amount, waits. Held the other way it would be paid for with no limit.
func (self *Tool) ScopedIDName() string {
	if self.ScopedIDField != "" {
		return self.ScopedIDField
	}
	if self.AmountField != "" {
		return ""
	}
	if parameterNames(self.Parameters)[FallbackScopedIDField] {
		return FallbackScopedIDField
	}
	return ""
}

// ScopedIDOf is the identifier a call names, or empty when it names none or
// not as text.
func (self *Tool) ScopedIDOf(arguments map[string]any) string {
	name := self.ScopedIDName()
	if name == "" {
		return ""
	}
	identifier, _ := completedArguments(self, arguments)[name].(string)
	return strings.TrimSpace(identifier)
}

// DoesDeclareAmount says the tool says where its amount is, which is what a
// limit needs.
func (self *Tool) DoesDeclareAmount() bool {
	return self.AmountField != ""
}

// ShareLimits are what a share holds a call to.
type ShareLimits struct {
	// PaymentLimitMinor and MonthlyLimitMinor are the most one payment may be
	// and the most a month may come to, in CurrencyCode's minor unit. Nil is
	// no limit of that kind.
	PaymentLimitMinor *int64
	MonthlyLimitMinor *int64

	// CurrencyCode is the currency the limits are in, lower case.
	CurrencyCode string

	// IsApprovalRequired makes every call wait.
	IsApprovalRequired bool
}

// Weighing is what the server makes of one call of a shared tool before it
// runs: what it pays, and whether it may go ahead without the owner.
type Weighing struct {
	// Amount is what the call pays, or nil when the server cannot tell or the
	// tool pays nothing.
	Amount *Amount

	// IsApprovalNeeded says the call needs the owner's approval, and WaitReason
	// says why. The other person's agent reads it, so it speaks of the owner in
	// the third person.
	IsApprovalNeeded bool
	WaitReason       string

	// ScopedID is the identifier a call of a scoped tool names. A scoped call
	// goes ahead only if the identifier came out of an earlier call of the
	// same share, which the caller checks against what it stored; ScopedName
	// is the parameter, empty for a tool that is not scoped.
	ScopedName string
	ScopedID   string
}

// Weigh holds one call of this tool to a share's limits. monthTotalMinor is
// what the share's ran and failed calls of this calendar month in the share's
// currency already come to (a call that failed counts, since a failure after
// the command acted cannot be told from one before it).
//
// A tool that names a scoped identifier pays nothing and is not held to the
// limits: it is held to the identifier rule, which the caller decides. Every
// other tool is held to the limits, and one that declares no amount waits.
func (self *Tool) Weigh(arguments map[string]any, limits ShareLimits, monthTotalMinor int64) Weighing {
	weighing := Weighing{ScopedName: self.ScopedIDName()}
	if limits.IsApprovalRequired {
		weighing.IsApprovalNeeded, weighing.WaitReason = true, "this share asks for the owner's approval on every call"
	}
	if weighing.ScopedName != "" {
		weighing.ScopedID = self.ScopedIDOf(arguments)
		return weighing
	}
	weighing.Amount = self.ReadAmount(arguments)
	if weighing.IsApprovalNeeded {
		return weighing
	}
	amount := weighing.Amount
	switch {
	case amount == nil:
		weighing.IsApprovalNeeded = true
		if self.DoesDeclareAmount() {
			weighing.WaitReason = "the amount cannot be read from this call, so only the owner can say whether it is within what they meant to allow"
		} else {
			weighing.WaitReason = "the skill does not say what this tool costs, so only the owner can say whether it is within what they meant to allow"
		}
	case limits.PaymentLimitMinor == nil && limits.MonthlyLimitMinor == nil:
		// Reaching here with an amount that was read and no limit of any kind
		// would let the call go ahead as though a limit had passed it. A share
		// of a tool that pays is made with a limit or with asking every time
		// (CheckShareable), but a skill updated since may declare an amount
		// the tool did not have when it was shared.
		weighing.IsApprovalNeeded, weighing.WaitReason = true, "this share has no limit for a payment, so only the owner can say whether it is within what they meant to allow"
	case limits.CurrencyCode != "" && amount.CurrencyCode != limits.CurrencyCode:
		weighing.IsApprovalNeeded = true
		if amount.CurrencyCode == "" {
			weighing.WaitReason = "the call does not say which currency it pays in, and the owner's limits are in " + strings.ToUpper(limits.CurrencyCode)
		} else {
			weighing.WaitReason = "the call pays in " + strings.ToUpper(amount.CurrencyCode) + ", and the owner's limits are in " + strings.ToUpper(limits.CurrencyCode)
		}
	case limits.PaymentLimitMinor != nil && amount.Minor > *limits.PaymentLimitMinor:
		weighing.IsApprovalNeeded, weighing.WaitReason = true, "it is above the owner's limit for one payment"
	case limits.MonthlyLimitMinor != nil && monthTotalMinor+amount.Minor > *limits.MonthlyLimitMinor:
		weighing.IsApprovalNeeded, weighing.WaitReason = true, "it would take this month past the owner's monthly limit (a use that failed counts toward it)"
	}
	return weighing
}

// ShareMonth is the calendar month a moment falls in, in UTC: the span the
// monthly limit of a share counts over. One clock for every share, so that
// the limit does not move with whoever looks at it.
func ShareMonth(at time.Time) (start, end time.Time) {
	at = at.UTC()
	start = time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

// CheckShareable refuses a set of tools that cannot be shared as the person
// asked: a tool the skill does not declare, and tools that read an amount
// shared with no limit and no approval on every call, which would let the
// other person spend without bound.
func (self *Skill) CheckShareable(toolNames []string, limits ShareLimits) error {
	if len(toolNames) == 0 {
		return fmt.Errorf("skills: say which of %s's tools to share", self.Name)
	}
	seen := map[string]bool{}
	hasAmount := false
	for _, name := range toolNames {
		tool := self.Tool(name)
		if tool == nil {
			return fmt.Errorf("skills: %s has no tool called %q", self.Name, name)
		}
		if seen[name] {
			return fmt.Errorf("skills: %s is named twice", name)
		}
		seen[name] = true
		if tool.DoesDeclareAmount() {
			hasAmount = true
		}
	}
	if hasAmount && !limits.IsApprovalRequired && limits.PaymentLimitMinor == nil && limits.MonthlyLimitMinor == nil {
		return fmt.Errorf("skills: a tool that pays is not shared without a limit for one payment, a monthly limit, or asking you every time")
	}
	if (limits.PaymentLimitMinor != nil || limits.MonthlyLimitMinor != nil) && limits.CurrencyCode == "" {
		return fmt.Errorf("skills: a limit is in a currency; say which")
	}
	return nil
}
