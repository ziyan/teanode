package skills

import (
	"os"
	"strings"
	"testing"
	"time"
)

func paymentsSkill(t *testing.T) *Skill {
	t.Helper()
	content, err := os.ReadFile("testdata/payments.md")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	skill, err := Parse(content)
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}
	return skill
}

func whole(minor int64) *int64 { return &minor }

// The amount is read from the arguments the declaration names, only for the
// actions it names, and only when it is a whole number above zero.
func TestTheAmountIsReadPerAction(t *testing.T) {
	pay := paymentsSkill(t).Tool("pay")
	for name, test := range map[string]struct {
		arguments map[string]any
		minor     int64
		currency  string
		isKnown   bool
	}{
		"card":                  {map[string]any{"action": "card", "amount_minor": float64(1400), "currency": "USD"}, 1400, "usd", true},
		"card as integer":       {map[string]any{"action": "card", "amount_minor": 1400, "currency": "usd"}, 1400, "usd", true},
		"card with no currency": {map[string]any{"action": "card", "amount_minor": float64(1400)}, 1400, "", true},
		"currency not a code":   {map[string]any{"action": "card", "amount_minor": float64(5), "currency": "dollars"}, 5, "", true},
		"url has none":          {map[string]any{"action": "url", "address": "https://example.com/pay", "amount_minor": float64(1)}, 0, "", false},
		"report has none":       {map[string]any{"action": "report", "amount_minor": float64(1)}, 0, "", false},
		"no action":             {map[string]any{"amount_minor": float64(1400)}, 0, "", false},
		"no amount":             {map[string]any{"action": "card"}, 0, "", false},
		"zero":                  {map[string]any{"action": "card", "amount_minor": float64(0)}, 0, "", false},
		"negative":              {map[string]any{"action": "card", "amount_minor": float64(-100)}, 0, "", false},
		"fraction":              {map[string]any{"action": "card", "amount_minor": 12.5}, 0, "", false},
		"text":                  {map[string]any{"action": "card", "amount_minor": "1400"}, 0, "", false},
		"too large to be exact": {map[string]any{"action": "card", "amount_minor": float64(1 << 60)}, 0, "", false},
	} {
		amount := pay.ReadAmount(test.arguments)
		if (amount != nil) != test.isKnown {
			t.Errorf("%s: read %+v, want known %v", name, amount, test.isKnown)
			continue
		}
		if amount != nil && (amount.Minor != test.minor || amount.CurrencyCode != test.currency) {
			t.Errorf("%s: read %+v, want %d %q", name, amount, test.minor, test.currency)
		}
	}
	if amount := paymentsSkill(t).Tool("balance").ReadAmount(map[string]any{"amount_minor": float64(5)}); amount != nil {
		t.Errorf("a tool that declares no amount reads one: %+v", amount)
	}
}

// A default in the schema is part of the call, as it is when the steps are
// filled in: an amount or an action left to its default is the one paid.
func TestTheAmountReadIsTheAmountTheRunIsGiven(t *testing.T) {
	skill, err := Parse([]byte(`---
name: defaults
description: Tests that the defaults count.
tools:
  - name: pay
    description: Pay.
    type: shell
    command: ["paycli", "{{amount_minor}}"]
    amountField: amount_minor
    parameters:
      type: object
      properties:
        amount_minor:
          type: integer
          default: 700
---
`))
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}
	amount := skill.Tool("pay").ReadAmount(map[string]any{})
	if amount == nil || amount.Minor != 700 {
		t.Errorf("the default amount is not read: %+v", amount)
	}
	if amount := skill.Tool("pay").ReadAmount(map[string]any{"amount_minor": nil}); amount == nil || amount.Minor != 700 {
		t.Errorf("a null falls back to the default, as the run does: %+v", amount)
	}
}

func usdLimits(payment, monthly *int64) ShareLimits {
	return ShareLimits{PaymentLimitMinor: payment, MonthlyLimitMinor: monthly, CurrencyCode: "usd"}
}

func cardOf(minor float64, currency string) map[string]any {
	return map[string]any{"action": "card", "merchant": "Example Store", "amount_minor": minor, "currency": currency}
}

// Within both limits a call goes ahead; above the one for a payment, or past
// the month's, it waits, and the reason says which.
func TestAPaymentWaitsAboveItsLimits(t *testing.T) {
	pay := paymentsSkill(t).Tool("pay")
	limits := usdLimits(whole(10000), whole(40000))
	for name, test := range map[string]struct {
		arguments  map[string]any
		monthSoFar int64
		waits      bool
		reasonWord string
	}{
		"within":             {cardOf(2500, "usd"), 0, false, ""},
		"exactly the limit":  {cardOf(10000, "usd"), 0, false, ""},
		"one over":           {cardOf(10001, "usd"), 0, true, "one payment"},
		"to the month's end": {cardOf(10000, "usd"), 30000, false, ""},
		"past the month":     {cardOf(2500, "usd"), 37501, true, "monthly limit"},
		"other currency":     {cardOf(2500, "eur"), 0, true, "EUR"},
		"no currency":        {map[string]any{"action": "card", "amount_minor": float64(2500)}, 0, true, "which currency"},
		"url always waits":   {map[string]any{"action": "url", "address": "https://example.com/pay"}, 0, true, "cannot be read"},
		"no amount waits":    {map[string]any{"action": "card"}, 0, true, "cannot be read"},
	} {
		weighing := pay.Weigh(test.arguments, limits, test.monthSoFar)
		if weighing.IsApprovalNeeded != test.waits {
			t.Errorf("%s: waits %v (%q), want %v", name, weighing.IsApprovalNeeded, weighing.WaitReason, test.waits)
		}
		if test.waits && !strings.Contains(weighing.WaitReason, test.reasonWord) {
			t.Errorf("%s: the reason is %q, want it to mention %q", name, weighing.WaitReason, test.reasonWord)
		}
	}
}

// With only a monthly limit a large payment goes ahead until the month is
// used; with only one for a payment, the month is not counted.
func TestEitherLimitAloneHoldsOnItsOwn(t *testing.T) {
	pay := paymentsSkill(t).Tool("pay")
	if weighing := pay.Weigh(cardOf(900000, "usd"), usdLimits(nil, whole(1000000)), 0); weighing.IsApprovalNeeded {
		t.Errorf("a payment within the month's limit waits: %s", weighing.WaitReason)
	}
	if weighing := pay.Weigh(cardOf(900000, "usd"), usdLimits(nil, whole(1000000)), 200000); !weighing.IsApprovalNeeded {
		t.Error("a payment that takes the month over goes ahead")
	}
	if weighing := pay.Weigh(cardOf(500, "usd"), usdLimits(whole(1000), nil), 99999999); weighing.IsApprovalNeeded {
		t.Errorf("the month is counted without a limit for it: %s", weighing.WaitReason)
	}
}

// A tool that declares an amount, on a share with no limit of either kind and
// not set to ask every time, waits: nothing was set that a call could be
// within. This is how a share made before a skill update gave the tool an
// amount behaves until it is set to ask.
func TestAPaymentOnAShareWithNoLimitWaits(t *testing.T) {
	pay := paymentsSkill(t).Tool("pay")
	for name, limits := range map[string]ShareLimits{
		"no limits":                {},
		"no limits but a currency": {CurrencyCode: "usd"},
	} {
		weighing := pay.Weigh(cardOf(100, "usd"), limits, 0)
		if !weighing.IsApprovalNeeded || !strings.Contains(weighing.WaitReason, "no limit") {
			t.Errorf("%s: a payment with nothing to be within goes ahead: %+v", name, weighing)
		}
	}
	// With a limit of either kind it is weighed against it, as before.
	if weighing := pay.Weigh(cardOf(100, "usd"), usdLimits(whole(1000), nil), 0); weighing.IsApprovalNeeded {
		t.Errorf("a payment within its limit waits: %s", weighing.WaitReason)
	}
}

// A share that asks every time waits for every call, whatever it pays.
func TestAShareThatAsksEveryTimeAlwaysWaits(t *testing.T) {
	skill := paymentsSkill(t)
	limits := usdLimits(whole(10000), nil)
	limits.IsApprovalRequired = true
	for name, tool := range map[string]map[string]any{
		"pay":            cardOf(100, "usd"),
		"spend_requests": {"action": "retrieve", "spend_request_id": "request-1"},
	} {
		toolName := "pay"
		if name == "spend_requests" {
			toolName = name
		}
		weighing := skill.Tool(toolName).Weigh(tool, limits, 0)
		if !weighing.IsApprovalNeeded || !strings.Contains(weighing.WaitReason, "every call") {
			t.Errorf("%s: %+v", name, weighing)
		}
	}
}

// A tool that declares no amount is never priced: it waits.
func TestAToolWithNoDeclaredAmountAlwaysWaits(t *testing.T) {
	weighing := paymentsSkill(t).Tool("balance").Weigh(map[string]any{}, usdLimits(whole(10000), whole(40000)), 0)
	if !weighing.IsApprovalNeeded || weighing.Amount != nil || !strings.Contains(weighing.WaitReason, "does not say what this tool costs") {
		t.Errorf("a tool with no amount: %+v", weighing)
	}
}

// A tool that names a scoped identifier is held to the identifier rule and not
// to the limits, and one that does not declare it but takes spend_request_id is
// held the same way.
func TestAScopedToolReportsItsIdentifier(t *testing.T) {
	skill := paymentsSkill(t)
	spend := skill.Tool("spend_requests")
	if spend.ScopedIDName() != "spend_request_id" {
		t.Fatalf("the scoped field is %q", spend.ScopedIDName())
	}
	weighing := spend.Weigh(map[string]any{"action": "retrieve", "spend_request_id": " sr-7 "}, usdLimits(whole(1), nil), 0)
	if weighing.IsApprovalNeeded || weighing.ScopedName != "spend_request_id" || weighing.ScopedID != "sr-7" {
		t.Errorf("a scoped retrieve: %+v", weighing)
	}
	if weighing := spend.Weigh(map[string]any{"action": "active"}, usdLimits(whole(1), nil), 0); weighing.ScopedID != "" || weighing.ScopedName == "" {
		t.Errorf("a listing names no identifier, and so is not allowed: %+v", weighing)
	}
	if weighing := spend.Weigh(map[string]any{"action": "retrieve", "spend_request_id": float64(7)}, usdLimits(whole(1), nil), 0); weighing.ScopedID != "" {
		t.Errorf("an identifier that is not text is read: %+v", weighing)
	}

	older, err := Parse([]byte(`---
name: older
description: A skill written before scopedIdField existed.
tools:
  - name: look
    description: Look at a request.
    type: shell
    command: ["paycli", "look", "{{spend_request_id}}"]
    parameters:
      type: object
      properties:
        spend_request_id:
          type: string
---
`))
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}
	if got := older.Tool("look").ScopedIDName(); got != "spend_request_id" {
		t.Errorf("the fallback is %q", got)
	}
	if got := skill.Tool("pay").ScopedIDName(); got != "" {
		t.Errorf("a payment tool is scoped by %q", got)
	}
}

// What a declaration may name is checked when the skill is read.
func TestTheDeclarationsForSharingAreChecked(t *testing.T) {
	wrap := func(declarations string) string {
		return `---
name: bad
description: A skill whose declarations are wrong.
tools:
  - name: pay
    description: Pay.
    type: workflow
    actionField: action
` + declarations + `
    parameters:
      type: object
      properties:
        action:
          type: string
        amount_minor:
          type: integer
        price:
          type: string
        currency:
          type: string
        count:
          type: number
        reference:
          type: string
    actions:
      card:
        - name: card
          type: shell
          command: ["paycli"]
---
`
	}
	for name, test := range map[string]struct {
		declarations string
		wantError    string
	}{
		"fine":                       {"    amountField: amount_minor\n    currencyField: currency\n    amountActions: [card]", ""},
		"amount of a missing field":  {"    amountField: cents", "amountField"},
		"amount that is text":        {"    amountField: price", "integer"},
		"amount that is a number":    {"    amountField: count", "integer"},
		"currency that is a number":  {"    amountField: amount_minor\n    currencyField: count", "currencyField"},
		"currency without amount":    {"    currencyField: currency", "without an amountField"},
		"actions without amount":     {"    amountActions: [card]", "without an amountField"},
		"action nobody declared":     {"    amountField: amount_minor\n    amountActions: [wire]", "wire"},
		"scoped field missing":       {"    scopedIdField: nothing", "scopedIdField"},
		"scoped field is a number":   {"    scopedIdField: count", "scopedIdField"},
		"amount and scoped together": {"    amountField: amount_minor\n    scopedIdField: reference", "both"},
	} {
		_, err := Parse([]byte(wrap(test.declarations)))
		switch {
		case test.wantError == "" && err != nil:
			t.Errorf("%s: %s", name, err)
		case test.wantError != "" && err == nil:
			t.Errorf("%s: the skill was accepted", name)
		case test.wantError != "" && !strings.Contains(err.Error(), test.wantError):
			t.Errorf("%s: %s, want it to mention %q", name, err, test.wantError)
		}
	}
}

// A tool that pays is not shared without a bound; a limit has a currency; a
// tool the skill does not have is not shared.
func TestOnlyTheRightToolsAreShareableWithoutALimit(t *testing.T) {
	skill := paymentsSkill(t)
	if err := skill.CheckShareable([]string{"pay"}, ShareLimits{}); err == nil {
		t.Error("a payment tool was shared with no limit and no approval")
	}
	if err := skill.CheckShareable([]string{"pay"}, ShareLimits{IsApprovalRequired: true}); err != nil {
		t.Errorf("asking every time is a bound: %s", err)
	}
	if err := skill.CheckShareable([]string{"pay"}, usdLimits(whole(500), nil)); err != nil {
		t.Errorf("a limit is a bound: %s", err)
	}
	if err := skill.CheckShareable([]string{"pay"}, ShareLimits{MonthlyLimitMinor: whole(500)}); err == nil {
		t.Error("a limit with no currency was accepted")
	}
	if err := skill.CheckShareable([]string{"spend_requests"}, ShareLimits{}); err != nil {
		t.Errorf("a tool that pays nothing needs no limit: %s", err)
	}
	if err := skill.CheckShareable([]string{"wire"}, ShareLimits{}); err == nil {
		t.Error("a tool the skill does not declare was shared")
	}
	if err := skill.CheckShareable([]string{"pay", "pay"}, usdLimits(whole(500), nil)); err == nil {
		t.Error("a tool named twice was accepted")
	}
	if err := skill.CheckShareable(nil, ShareLimits{}); err == nil {
		t.Error("nothing was shared")
	}
}

// A payment cannot be allowed to be larger than the month it counts in, so
// the share that says so is refused, in the words the dashboard shows too.
func TestAPaymentLimitAboveTheMonthlyLimitIsRefused(t *testing.T) {
	skill := paymentsSkill(t)
	err := skill.CheckShareable([]string{"pay"}, usdLimits(whole(50000), whole(10000)))
	if err == nil || !strings.Contains(err.Error(), PaymentLimitAboveMonthlyWords) {
		t.Errorf("a payment limit above the monthly limit gave %v", err)
	}
	if err := skill.CheckShareable([]string{"pay"}, usdLimits(whole(10000), whole(10000))); err != nil {
		t.Errorf("a payment limit equal to the monthly limit was refused: %s", err)
	}
	if err := skill.CheckShareable([]string{"pay"}, usdLimits(whole(5000), whole(10000))); err != nil {
		t.Errorf("a payment limit below the monthly limit was refused: %s", err)
	}
}

// A share made before that rule is not held to it when the skill is saved
// again: only a person making or changing a share is.
func TestAShareMadeBeforeThePaymentRuleStillHoldsWhenTheSkillIsSaved(t *testing.T) {
	skill := paymentsSkill(t)
	limits := usdLimits(whole(50000), whole(10000))
	if err := skill.CheckShareStillHolds([]string{"pay"}, limits); err != nil {
		t.Errorf("an existing share with a payment limit above the monthly limit was held: %s", err)
	}
	if err := skill.CheckShareStillHolds([]string{"pay"}, ShareLimits{}); err == nil {
		t.Error("an existing share of a tool that pays with no bound still held")
	}
}

func TestTheMonthIsTheCalendarMonthInUTC(t *testing.T) {
	start, end := ShareMonth(time.Date(2026, time.March, 31, 23, 59, 0, 0, time.FixedZone("west", -8*3600)))
	if !start.Equal(time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)) || !end.Equal(time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("the month is %s to %s", start, end)
	}
}

// A payment tool that also takes a spend request to report on an earlier payment
// is priced like any other: the fallback for older skills must not make it a
// tool with no limit.
func TestAPaymentToolWithASpendRequestParameterIsStillPriced(t *testing.T) {
	skill, err := Parse([]byte(`---
name: reporting
description: Pays, and reports on what it paid.
tools:
  - name: pay
    description: Pay or report.
    type: workflow
    actionField: action
    amountField: amount_minor
    currencyField: currency
    amountActions: [card]
    parameters:
      type: object
      properties:
        action:
          type: string
        amount_minor:
          type: integer
        currency:
          type: string
        spend_request_id:
          type: string
    actions:
      card:
        - name: card
          type: shell
          command: ["paycli", "card"]
      report:
        - name: report
          type: shell
          command: ["paycli", "report", "{{spend_request_id}}"]
---
`))
	if err != nil {
		t.Fatalf("Parse: %s", err)
	}
	pay := skill.Tool("pay")
	if pay.ScopedIDName() != "" {
		t.Fatalf("a tool that declares an amount is scoped by %q", pay.ScopedIDName())
	}
	limits := usdLimits(whole(10000), nil)
	if weighing := pay.Weigh(map[string]any{"action": "card", "amount_minor": float64(14000), "currency": "usd", "spend_request_id": "req_0001abcd"}, limits, 0); !weighing.IsApprovalNeeded {
		t.Errorf("a payment above the limit went ahead because it named a spend request: %+v", weighing)
	}
	if weighing := pay.Weigh(map[string]any{"action": "report", "spend_request_id": "req_0001abcd"}, limits, 0); !weighing.IsApprovalNeeded {
		t.Errorf("a report, which has no amount, did not wait: %+v", weighing)
	}
	if weighing := pay.Weigh(map[string]any{"action": "card", "amount_minor": float64(2500), "currency": "usd"}, limits, 0); weighing.IsApprovalNeeded {
		t.Errorf("a payment within the limit waits: %+v", weighing)
	}
}

// The reason a call waits is read by the other person's agent, so it speaks of
// the owner in the third person: "your limit" would be read as that agent's
// own.
func TestEveryWaitReasonIsWrittenAboutTheOwner(t *testing.T) {
	pay := paymentsSkill(t).Tool("pay")
	asksEveryTime := usdLimits(whole(10000), nil)
	asksEveryTime.IsApprovalRequired = true
	reasons := map[string]string{
		"asks every time":  pay.Weigh(cardOf(100, "usd"), asksEveryTime, 0).WaitReason,
		"no amount":        pay.Weigh(map[string]any{"action": "card"}, usdLimits(whole(10000), nil), 0).WaitReason,
		"no declared cost": paymentsSkill(t).Tool("balance").Weigh(map[string]any{}, usdLimits(whole(10000), nil), 0).WaitReason,
		"no limit":         pay.Weigh(cardOf(100, "usd"), ShareLimits{}, 0).WaitReason,
		"other currency":   pay.Weigh(cardOf(100, "eur"), usdLimits(whole(10000), nil), 0).WaitReason,
		"no currency":      pay.Weigh(map[string]any{"action": "card", "amount_minor": float64(100)}, usdLimits(whole(10000), nil), 0).WaitReason,
		"over one payment": pay.Weigh(cardOf(10001, "usd"), usdLimits(whole(10000), nil), 0).WaitReason,
		"over the month":   pay.Weigh(cardOf(2500, "usd"), usdLimits(nil, whole(40000)), 37501).WaitReason,
	}
	for name, reason := range reasons {
		if reason == "" {
			t.Errorf("%s: no reason", name)
			continue
		}
		for _, word := range strings.Fields(strings.ToLower(strings.NewReplacer(",", " ", ".", " ", "(", " ", ")", " ").Replace(reason))) {
			if word == "you" || word == "your" || word == "yours" {
				t.Errorf("%s: the reason speaks to the owner, not about them: %q", name, reason)
			}
		}
	}
	if !strings.Contains(reasons["over one payment"], "the owner's limit for one payment") {
		t.Errorf("the owner is named in the third person: %q", reasons["over one payment"])
	}
}
