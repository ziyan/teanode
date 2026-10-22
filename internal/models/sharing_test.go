package models_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// Every kind of share has a sentence for the note that tells a person about
// it, in English, and a noun for the note that tells an owner a joiner can see
// it; a recording or a finance account is worded like the other kinds.
func TestShareNotesAreWordedForRecordingsAndFinanceAccounts(test *testing.T) {
	for _, shareKind := range models.ShareKinds {
		if !shareKind.IsValid() {
			test.Errorf("%s is not valid", shareKind)
		}
		text := models.ShareReceivedText(&models.ShareReceivedNote{SharerName: "Alex", ShareKind: shareKind, ShareSubject: "Subject"})
		if text == "Alex shared something with you" && shareKind != models.ShareKindPasswords {
			test.Errorf("%s has no sentence of its own", shareKind)
		}
	}
	for _, each := range []struct {
		note models.ShareReceivedNote
		want string
	}{
		{models.ShareReceivedNote{SharerName: "Alex", ShareKind: models.ShareKindRecording, ShareSubject: "Standup", ShareLevel: "summary"},
			"Alex shared the recording Standup with you (title and summary)"},
		{models.ShareReceivedNote{SharerName: "Alex", ShareKind: models.ShareKindRecording, ShareLevel: "transcript"},
			"Alex shared a recording with you (summary and transcript)"},
		{models.ShareReceivedNote{SharerName: "Alex", ShareKind: models.ShareKindFinance, ShareLevel: "balance"},
			"Alex shared a finance account with you (balance only)"},
		{models.ShareReceivedNote{SharerName: "Alex", ShareKind: models.ShareKindFinance, ShareLevel: "spending"},
			"Alex shared a finance account with you (balance and spending)"},
		{models.ShareReceivedNote{SharerName: "Alex", ShareKind: models.ShareKindFinance, ShareLevel: "transactions"},
			"Alex shared a finance account with you (balance, spending and transactions)"},
	} {
		if got := models.ShareReceivedText(&each.note); got != each.want {
			test.Errorf("got %q, want %q", got, each.want)
		}
	}
}

// The kinds a joiner can see are listed with commas and "and" before the last,
// without a comma before it.
func TestShareReachedTextListsKindsWithAnd(test *testing.T) {
	for _, each := range []struct {
		shareKinds []models.ShareKind
		want       string
	}{
		{[]models.ShareKind{models.ShareKindCalendar}, "Sam joined Family and can now see your calendar"},
		{[]models.ShareKind{models.ShareKindCalendar, models.ShareKindLocation}, "Sam joined Family and can now see your calendar and location"},
		{[]models.ShareKind{models.ShareKindCalendar, models.ShareKindLocation, models.ShareKindFinance},
			"Sam joined Family and can now see your calendar, location and finance"},
		{[]models.ShareKind{models.ShareKindRecording, models.ShareKindFinance}, "Sam joined Family and can now see your recordings and finance"},
	} {
		if got := models.ShareReachedText(&models.ShareReachedNote{MemberName: "Sam", CircleName: "Family", ShareKinds: each.shareKinds}); got != each.want {
			test.Errorf("got %q, want %q", got, each.want)
		}
	}
}
