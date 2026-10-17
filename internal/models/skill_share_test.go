package models

import "testing"

func TestAnAmountIsWrittenAndReadInTheCurrencysOwnDigits(t *testing.T) {
	for _, test := range []struct {
		minor    int64
		currency string
		written  string
	}{
		{1400, "usd", "14.00 USD"},
		{5, "usd", "0.05 USD"},
		{10000, "EUR", "100.00 EUR"},
		{1400, "jpy", "1400 JPY"},
		{1500, "kwd", "1.500 KWD"},
		{250, "", "2.50"},
	} {
		if got := FormatMinor(test.minor, test.currency); got != test.written {
			t.Errorf("FormatMinor(%d, %q) = %q, want %q", test.minor, test.currency, got, test.written)
		}
	}
	for _, test := range []struct {
		typed    string
		currency string
		minor    int64
		isValid  bool
	}{
		{"100", "usd", 10000, true},
		{"100.5", "usd", 10050, true},
		{"100.50", "usd", 10050, true},
		{"0.05", "usd", 5, true},
		{"1400", "jpy", 1400, true},
		{" 7.250 ", "kwd", 7250, true},
		{"100.505", "usd", 0, false},
		{"1400.5", "jpy", 0, false},
		{"100.", "usd", 0, false},
		{"", "usd", 0, false},
		{"-5", "usd", 0, false},
		{"1,000", "usd", 0, false},
		{"0", "usd", 0, false},
		{"0.00", "usd", 0, false},
		{"12abc", "usd", 0, false},
		{"9999999999999999.00", "usd", 0, false},
	} {
		minor, err := ParseMinor(test.typed, test.currency)
		if (err == nil) != test.isValid || (test.isValid && minor != test.minor) {
			t.Errorf("ParseMinor(%q, %q) = %d, %v; want %d, valid %v", test.typed, test.currency, minor, err, test.minor, test.isValid)
		}
	}
}

func TestAPaymentWaitingNoteReadsBack(t *testing.T) {
	detail := SkillShareCallDetail(SkillShareCallNote{SkillShareID: "share-1", CallID: "call-1", GranteeName: "Ben", GranteeUserID: "user-1"})
	note := ParseSkillShareCallNote(detail)
	if note == nil || note.CallID != "call-1" || note.GranteeName != "Ben" {
		t.Fatalf("the note: %+v", note)
	}
	if ParseSkillShareCallNote("not json") != nil || ParseSkillShareCallNote(`{"callId":"x"}`) != nil {
		t.Error("a detail that names no call and share was read")
	}
	if got := NoteText(NotePaymentWaiting, detail); got != "Ben asked to use a skill you shared, and it is waiting for your approval" {
		t.Errorf("the note in English: %q", got)
	}
}

// A share changed from somewhere that is not a person's tap is let through
// only when it hands over nothing more than the share it replaces did: asking
// every time always, and otherwise the same tools or fewer, in the same
// currency, with each limit at or below the old one (no limit being the
// highest there is).
func TestASharedSkillChangeThatOnlyTakesAwayOpensNothingBeyond(t *testing.T) {
	amount := func(minor int64) *int64 { return &minor }
	existing := &SkillShare{
		ToolNames: []string{"pay", "balance"}, PaymentLimitMinor: amount(10000), MonthlyLimitMinor: amount(40000), CurrencyCode: "usd",
	}
	unlimited := &SkillShare{ToolNames: []string{"balance"}}
	asking := &SkillShare{ToolNames: []string{"pay"}, IsApprovalRequired: true, PaymentLimitMinor: amount(90000), CurrencyCode: "usd"}

	for name, test := range map[string]struct {
		terms    *SkillShare
		replaced *SkillShare
		expected bool
	}{
		"asking every time, with no share before":    {&SkillShare{ToolNames: []string{"pay"}, IsApprovalRequired: true}, nil, true},
		"asking every time over a share with limits": {&SkillShare{ToolNames: []string{"pay", "wire"}, IsApprovalRequired: true}, existing, true},
		"a new share with limits":                    {&SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: amount(1), CurrencyCode: "usd"}, nil, false},
		"the same terms":                             {existing, existing, true},
		"lower limits":                               {&SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: amount(500), MonthlyLimitMinor: amount(40000), CurrencyCode: "usd"}, existing, true},
		"a higher payment limit":                     {&SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: amount(10001), MonthlyLimitMinor: amount(40000), CurrencyCode: "usd"}, existing, false},
		"a limit dropped":                            {&SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: amount(500), CurrencyCode: "usd"}, existing, false},
		"another tool":                               {&SkillShare{ToolNames: []string{"pay", "wire"}, PaymentLimitMinor: amount(500), MonthlyLimitMinor: amount(500), CurrencyCode: "usd"}, existing, false},
		"another currency":                           {&SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: amount(500), MonthlyLimitMinor: amount(500), CurrencyCode: "eur"}, existing, false},
		"limits where there were none":               {&SkillShare{ToolNames: []string{"balance"}, PaymentLimitMinor: amount(500), CurrencyCode: "usd"}, unlimited, true},
		"turning asking every time off":              {&SkillShare{ToolNames: []string{"pay"}, PaymentLimitMinor: amount(500), CurrencyCode: "usd"}, asking, false},
		"turning asking every time off, no limits":   {&SkillShare{ToolNames: []string{"balance"}}, &SkillShare{ToolNames: []string{"balance"}, IsApprovalRequired: true}, false},
	} {
		if got := test.terms.OpensNothingBeyond(test.replaced); got != test.expected {
			t.Errorf("%s: %v, expected %v", name, got, test.expected)
		}
	}
}
