package client

import (
	"strings"
	"testing"
)

func TestRecordingActionRequiresExplicitEditsAndStableKeys(test *testing.T) {
	generation := int64(4)
	groupBound := int64(-1)
	unknownDate := ""
	emptySourceCron := ""
	passageStartMS := int64(2000)
	passageEndMS := int64(4000)
	cases := []struct {
		arguments     RecordingActionArguments
		isAllowed     bool
		operationName string
	}{
		{RecordingActionArguments{Operation: "prepare-playback"}, false, ""},
		{RecordingActionArguments{Operation: "prepare-playback", RecordingMediaID: "fixture-media"}, true, "PrepareRecordingPlayback"},
		{RecordingActionArguments{Operation: "read-playback", RecordingMediaID: "fixture-media"}, true, "ReadRecordingPlayback"},
		{RecordingActionArguments{Operation: "resume-learning", RecordingID: "fixture-recording"}, false, ""},
		{RecordingActionArguments{Operation: "resume-learning", RecordingID: "fixture-recording", ExpectedSourceGeneration: &generation}, true, "ResumeRecordingLearning"},
		{RecordingActionArguments{Operation: "passage-offset", RecordingTranscriptRunID: "fixture-run", PassageStartMS: &passageStartMS, PassageEndMS: &passageEndMS}, true, "ReadRecordingTranscriptPassageOffset"},
		{RecordingActionArguments{Operation: "passage-offset", RecordingTranscriptRunID: "fixture-run", PassageStartMS: &groupBound, PassageEndMS: &passageEndMS}, false, ""},
		{RecordingActionArguments{Operation: "source-schedule", KnowledgeSourceID: "fixture-source"}, false, ""},
		{RecordingActionArguments{Operation: "source-schedule", KnowledgeSourceID: "fixture-source", SourceCron: &emptySourceCron}, true, "SaveRecordingSourceSchedule"},
		{RecordingActionArguments{Operation: "source-schedule", SourceCron: &emptySourceCron}, false, ""},
		{RecordingActionArguments{Operation: "process", RecordingMediaID: "fixture-media"}, true, "RequestRecordingTranscription"},
		{RecordingActionArguments{Operation: "reprocess", RecordingMediaID: "fixture-media"}, false, ""},
		{RecordingActionArguments{Operation: "reprocess", RecordingMediaID: "fixture-media", ClientRunID: "initial"}, false, ""},
		{RecordingActionArguments{Operation: "reprocess", RecordingMediaID: "fixture-media", ClientRunID: "stable-paid-key"}, true, "RequestRecordingTranscription"},
		{RecordingActionArguments{Operation: "metadata", RecordingID: "fixture-recording", RecordingTitle: new("Fixture"), ExpectedSourceGeneration: &generation}, false, ""},
		{RecordingActionArguments{Operation: "metadata", RecordingID: "fixture-recording", RecordingTitle: new("Fixture"), RecordedAt: &unknownDate, ExpectedSourceGeneration: &generation}, true, "UpdateRecordingMetadata"},
		{RecordingActionArguments{Operation: "speaker-confirm", RecordingID: "fixture-recording", RecordingTranscriptionAttemptID: "fixture-attempt", LocalSpeakerID: "local-speaker", ConfirmedName: "Speaker Fixture", ClientCorrectionID: "stable-edit-key", ExpectedSourceGeneration: &generation, PassageStartMS: &groupBound, PassageEndMS: &groupBound}, true, "CorrectRecordingSpeaker"},
		{RecordingActionArguments{Operation: "temporary-speaker-name", RecordingID: "fixture-recording", SpeakerLabel: "Speaker 1"}, false, ""},
		{RecordingActionArguments{Operation: "temporary-speaker-name", RecordingID: "fixture-recording", ConfirmedName: "Speaker Fixture"}, false, ""},
		{RecordingActionArguments{Operation: "temporary-speaker-name", SpeakerLabel: "Speaker 1", ConfirmedName: "Speaker Fixture"}, false, ""},
		{RecordingActionArguments{Operation: "temporary-speaker-name", RecordingID: "fixture-recording", SpeakerLabel: "Speaker 1", ConfirmedName: "Speaker Fixture"}, true, "NameRecordingTemporarySpeaker"},
		{RecordingActionArguments{Operation: "temporary-speaker-name", RecordingID: "fixture-recording", SpeakerLabel: "Speaker 1", IsAccountOwner: true}, true, "NameRecordingTemporarySpeaker"},
		{RecordingActionArguments{Operation: "speaker-separate", RecordingID: "fixture-recording"}, false, ""},
		{RecordingActionArguments{Operation: "speaker-separate", SpeakerLabel: "Speaker 9"}, false, ""},
		{RecordingActionArguments{Operation: "speaker-separate", RecordingID: "fixture-recording", SpeakerLabel: "Speaker 9"}, true, "SeparateRecordingTemporarySpeaker"},
		{RecordingActionArguments{Operation: "speaker-contact", RecordingID: "fixture-recording", SpeakerIdentityID: "fixture-identity", AddressBookID: "fixture-book", ContactID: "fixture-contact", ExpectedSourceGeneration: &generation}, true, "SetRecordingSpeakerContact"},
		{RecordingActionArguments{Operation: "speaker-contact", RecordingID: "fixture-recording", SpeakerIdentityID: "fixture-identity", ShouldUntieContact: true, ExpectedSourceGeneration: &generation}, true, "SetRecordingSpeakerContact"},
		{RecordingActionArguments{Operation: "speaker-contact", RecordingID: "fixture-recording", SpeakerIdentityID: "fixture-identity", ExpectedSourceGeneration: &generation}, false, ""},
		{RecordingActionArguments{Operation: "speaker-contact", RecordingID: "fixture-recording", SpeakerIdentityID: "fixture-identity", AddressBookID: "fixture-book", ContactID: "fixture-contact", ShouldUntieContact: true, ExpectedSourceGeneration: &generation}, false, ""},
		{RecordingActionArguments{Operation: "speaker-contact", RecordingID: "fixture-recording", SpeakerIdentityID: "fixture-identity", AddressBookID: "fixture-book", ExpectedSourceGeneration: &generation}, false, ""},
		{RecordingActionArguments{Operation: "speaker-contact", RecordingID: "fixture-recording", AddressBookID: "fixture-book", ContactID: "fixture-contact", ExpectedSourceGeneration: &generation}, false, ""},
		{RecordingActionArguments{Operation: "speaker-contact", RecordingID: "fixture-recording", SpeakerIdentityID: "fixture-identity", AddressBookID: "fixture-book", ContactID: "fixture-contact"}, false, ""},
		{RecordingActionArguments{Operation: "temporary-speaker-name", RecordingID: "fixture-recording", SpeakerLabel: "Speaker 1", ConfirmedName: "Speaker Fixture", AddressBookID: "fixture-book"}, false, ""},
		{RecordingActionArguments{Operation: "temporary-speaker-name", RecordingID: "fixture-recording", SpeakerLabel: "Speaker 1", ConfirmedName: "Speaker Fixture", AddressBookID: "fixture-book", ContactID: "fixture-contact"}, true, "NameRecordingTemporarySpeaker"},
		{RecordingActionArguments{Operation: "text-edit", RecordingID: "fixture-recording", RecordingTranscriptSegmentID: "fixture-segment", CorrectedTranscriptText: "Exact user edit", ExpectedSourceGeneration: &generation}, false, ""},
		{RecordingActionArguments{Operation: "text-edit", RecordingID: "fixture-recording", RecordingTranscriptSegmentID: "fixture-segment", CorrectedTranscriptText: "Exact user edit", ExpectedSourceGeneration: &generation, ClientCorrectionID: "stable-edit-key"}, true, "CorrectRecordingTranscriptText"},
		{RecordingActionArguments{Operation: "share", RecordingID: "fixture-recording", GranteeUserID: "fixture-user", RecordingShareLevel: "summary"}, true, "ShareRecording"},
		{RecordingActionArguments{Operation: "share", RecordingID: "fixture-recording", GranteeCircleID: "fixture-circle", RecordingShareLevel: " Transcript "}, true, "ShareRecording"},
		{RecordingActionArguments{Operation: "share", RecordingID: "fixture-recording", GranteeUserID: "fixture-user"}, false, ""},
		{RecordingActionArguments{Operation: "share", RecordingID: "fixture-recording", GranteeUserID: "fixture-user", RecordingShareLevel: "audio"}, false, ""},
		{RecordingActionArguments{Operation: "share", RecordingID: "fixture-recording", RecordingShareLevel: "summary"}, false, ""},
		{RecordingActionArguments{Operation: "share", RecordingID: "fixture-recording", GranteeUserID: "fixture-user", GranteeCircleID: "fixture-circle", RecordingShareLevel: "summary"}, false, ""},
		{RecordingActionArguments{Operation: "share", GranteeUserID: "fixture-user", RecordingShareLevel: "summary"}, false, ""},
		{RecordingActionArguments{Operation: "shares", RecordingID: "fixture-recording"}, true, "ListRecordingShares"},
		{RecordingActionArguments{Operation: "shares"}, false, ""},
		{RecordingActionArguments{Operation: "unshare", RecordingShareID: "fixture-share"}, true, "UnshareRecording"},
		{RecordingActionArguments{Operation: "unshare"}, false, ""},
		{RecordingActionArguments{Operation: "shared"}, true, "ListSharedRecordings"},
		{RecordingActionArguments{Operation: "shared", PageSize: 101}, false, ""},
		{RecordingActionArguments{Operation: "read-shared", RecordingID: "fixture-recording", SegmentOffset: 50, PageSize: 250}, true, "ReadSharedRecording"},
		{RecordingActionArguments{Operation: "read-shared", RecordingID: "fixture-recording", PageSize: 251}, false, ""},
		{RecordingActionArguments{Operation: "read-shared", RecordingID: "fixture-recording", SegmentOffset: -1}, false, ""},
		{RecordingActionArguments{Operation: "read-shared"}, false, ""},
		{RecordingActionArguments{Operation: "activate-microphone"}, false, ""},
		{RecordingActionArguments{Operation: "retry", RecordingTranscriptRunID: "fixture-run"}, true, "RetryRecordingTranscription"},
	}
	for _, fixture := range cases {
		operationName, variables, err := RecordingAction(fixture.arguments)
		if (err == nil) != fixture.isAllowed {
			test.Fatalf("%s accepted=%t: %v", fixture.arguments.Operation, err == nil, err)
		}
		if !fixture.isAllowed {
			continue
		}
		if operationName != fixture.operationName || RecordingDocuments[operationName] == "" {
			test.Fatal("operation does not use registered account API")
		}
		_, secondVariables, err := RecordingAction(fixture.arguments)
		if err != nil {
			test.Fatal(err)
		}
		for _, fieldName := range []string{"clientRunId", "clientCorrectionId"} {
			if variables[fieldName] != secondVariables[fieldName] {
				test.Fatal("retry changed the request identity")
			}
		}
		if fixture.arguments.Operation == "process" && (variables["isReprocessingRequested"] != false || variables["clientRunId"] != "initial") {
			test.Fatal("initial processing created a new paid version")
		}
		if fixture.arguments.Operation == "metadata" && variables["recordedAt"] != nil {
			test.Fatal("unknown occurrence became an upload date")
		}
		if fixture.arguments.Operation == "share" {
			hasPerson := fixture.arguments.GranteeUserID != ""
			if (variables["granteeUserId"] != nil) != hasPerson || (variables["granteeCircleId"] != nil) == hasPerson {
				test.Fatalf("a share is for the person or the circle, not both: %v", variables)
			}
			if level := variables["recordingShareLevel"]; level != "summary" && level != "transcript" {
				test.Fatalf("the level is sent as the server reads it: %v", level)
			}
		}
	}
}

// A recording is named by the server, so no operation asks the person for a title: changing only the
// date leaves the title alone, an empty title clears it, and an import carries none by default.
func TestRecordingActionsNeverRequireATitle(test *testing.T) {
	generation := int64(3)
	recordedAt := "2031-03-04T05:06:00Z"
	_, variables, err := RecordingAction(RecordingActionArguments{Operation: "metadata", RecordingID: "fixture-recording", RecordedAt: &recordedAt, ExpectedSourceGeneration: &generation})
	if err != nil {
		test.Fatal(err)
	}
	if _, isPresent := variables["recordingTitle"]; isPresent {
		test.Fatal("an absent title was sent, which would clear it")
	}
	_, variables, err = RecordingAction(RecordingActionArguments{Operation: "metadata", RecordingID: "fixture-recording", RecordingTitle: new(""), RecordedAt: &recordedAt, ExpectedSourceGeneration: &generation})
	if err != nil || variables["recordingTitle"] != "" {
		test.Fatal("an empty title was not sent to clear it", err)
	}
	if !strings.Contains(RecordingDocuments["UpdateRecordingMetadata"], "$recordingTitle: String,") {
		test.Fatal("the title variable is required by the metadata document")
	}
	_, variables, err = RecordingAction(RecordingActionArguments{Operation: "import-attachment", AgentAttachmentID: "fixture-attachment"})
	if err != nil || variables["recordingTitle"] != "" {
		test.Fatal("an import asked for a title", err)
	}
}

// "This is me" needs no name; any other confirmation still does.
func TestSpeakerConfirmAcceptsTheAccountOwnerWithoutAName(test *testing.T) {
	generation, groupBound := int64(3), int64(-1)
	arguments := RecordingActionArguments{Operation: "speaker-confirm", RecordingID: "fixture-recording", RecordingTranscriptionAttemptID: "fixture-attempt", LocalSpeakerID: "local-speaker", ClientCorrectionID: "stable-edit-key", ExpectedSourceGeneration: &generation, PassageStartMS: &groupBound, PassageEndMS: &groupBound}
	if _, _, err := RecordingAction(arguments); err == nil {
		test.Fatal("a confirmation with neither a name nor the owner flag was accepted")
	}
	arguments.IsAccountOwner = true
	operationName, variables, err := RecordingAction(arguments)
	if err != nil || operationName != "CorrectRecordingSpeaker" || variables["isAccountOwner"] != true || variables["confirmedName"] != "" {
		test.Fatal("the owner flag was not sent with an empty name", err)
	}
	if !strings.Contains(RecordingDocuments["CorrectRecordingSpeaker"], "isAccountOwner: $isAccountOwner") {
		test.Fatal("the mutation document drops the owner flag")
	}
}

func TestSpeakerNamesAsksForNamesContainingTheSearch(test *testing.T) {
	operationName, variables, err := RecordingAction(RecordingActionArguments{Operation: "speaker-names", SearchText: "ali", PageSize: 5})
	if err != nil || operationName != "SuggestRecordingSpeakerNames" || variables["searchText"] != "ali" || variables["pageSize"] != 5 {
		test.Fatal("the search was not sent", operationName, variables, err)
	}
	if !strings.Contains(RecordingDocuments["SuggestRecordingSpeakerNames"], "speakerNameSource") {
		test.Fatal("the document does not ask where each name came from")
	}
}

// Tying a speaker to a contact names the address book and the contact the way each mutation calls them,
// and takes nothing when no contact is given.
func TestRecordingActionTiesASpeakerToAContactByTheNamesEachMutationUses(test *testing.T) {
	generation := int64(4)
	groupBound := int64(-1)
	_, variables, err := RecordingAction(RecordingActionArguments{Operation: "speaker-contact", RecordingID: "fixture-recording", SpeakerIdentityID: "fixture-identity", AddressBookID: "fixture-book", ContactID: "fixture-contact", ExpectedSourceGeneration: &generation})
	if err != nil || variables["addressBookId"] != "fixture-book" || variables["contactId"] != "fixture-contact" || variables["speakerIdentityId"] != "fixture-identity" {
		test.Fatalf("tying a speaker: %v %v", variables, err)
	}
	_, variables, err = RecordingAction(RecordingActionArguments{Operation: "speaker-contact", RecordingID: "fixture-recording", SpeakerIdentityID: "fixture-identity", ShouldUntieContact: true, ExpectedSourceGeneration: &generation})
	if err != nil || variables["addressBookId"] != "" || variables["contactId"] != "" {
		test.Fatalf("untying a speaker: %v %v", variables, err)
	}
	_, variables, err = RecordingAction(RecordingActionArguments{Operation: "speaker-confirm", RecordingID: "fixture-recording", RecordingTranscriptionAttemptID: "fixture-attempt", LocalSpeakerID: "local-speaker", ConfirmedName: "Speaker Fixture", ClientCorrectionID: "stable-edit-key", ExpectedSourceGeneration: &generation, PassageStartMS: &groupBound, PassageEndMS: &groupBound, AddressBookID: "fixture-book", ContactID: "fixture-contact"})
	if err != nil || variables["contactAddressBookId"] != "fixture-book" || variables["contactId"] != "fixture-contact" {
		test.Fatalf("naming a speaker with a contact: %v %v", variables, err)
	}
	_, variables, err = RecordingAction(RecordingActionArguments{Operation: "speaker-confirm", RecordingID: "fixture-recording", RecordingTranscriptionAttemptID: "fixture-attempt", LocalSpeakerID: "local-speaker", ConfirmedName: "Speaker Fixture", ClientCorrectionID: "stable-edit-key", ExpectedSourceGeneration: &generation, PassageStartMS: &groupBound, PassageEndMS: &groupBound})
	if _, hasAddressBook := variables["contactAddressBookId"]; err != nil || hasAddressBook {
		test.Fatalf("naming a speaker without a contact sent one: %v %v", variables, err)
	}
}
