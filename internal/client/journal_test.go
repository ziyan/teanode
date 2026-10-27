package client

import (
	"strings"
	"testing"
)

// Every document names its operation, and the ones that change something are
// mutations, so a read-only client refuses them before they are sent.
func TestJournalDocumentsNameTheirOperations(t *testing.T) {
	for operation, document := range map[string]string{
		"ListJournalDays": DocumentListJournalDays, "ReadJournalDay": DocumentReadJournalDay, "ReadJournalPeriod": DocumentReadJournalPeriod,
		"SearchJournal": DocumentSearchJournal, "ReadJournalSettings": DocumentReadJournalSettings, "SetJournalWords": DocumentSetJournalWords,
		"SetJournalMood": DocumentSetJournalMood, "SetJournalLookingBack": DocumentSetJournalLookingBack, "TouchUpJournalWords": DocumentTouchUpJournalWords,
		"StopJournalTouchUp": DocumentStopJournalTouchUp, "CorrectJournalNarrative": DocumentCorrectJournalNarrative,
		"RemoveJournalCorrection": DocumentRemoveJournalCorrection, "WriteJournalNarrative": DocumentWriteJournalNarrative,
		"DeleteJournalNarrative": DocumentDeleteJournalNarrative, "PinJournalRecord": DocumentPinJournalRecord, "RemoveJournalLink": DocumentRemoveJournalLink,
		"UpdateJournalSettings": DocumentUpdateJournalSettings, "EraseJournal": DocumentEraseJournal,
	} {
		isQuery := strings.HasPrefix(operation, "List") || strings.HasPrefix(operation, "Read") || operation == "SearchJournal"
		if IsMutationDocument(document) == isQuery {
			t.Errorf("%s: mutation is %v", operation, !isQuery)
		}
		if !strings.Contains(document, operation+"(") && !strings.Contains(document, operation+" ") {
			t.Errorf("%s is not in its document", operation)
		}
	}
}
