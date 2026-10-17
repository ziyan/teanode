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
