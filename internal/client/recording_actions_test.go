package client

import "testing"

func TestRecordingActionRequiresExplicitEditsAndStableKeys(test *testing.T) {
	generation := int64(4)
	groupBound := int64(-1)
	unknownDate := ""
	cases := []struct {
		arguments     RecordingActionArguments
		isAllowed     bool
		operationName string
	}{
		{RecordingActionArguments{Operation: "process", RecordingMediaID: "fixture-media"}, true, "RequestRecordingTranscription"},
		{RecordingActionArguments{Operation: "reprocess", RecordingMediaID: "fixture-media"}, false, ""},
		{RecordingActionArguments{Operation: "reprocess", RecordingMediaID: "fixture-media", ClientRunID: "initial"}, false, ""},
		{RecordingActionArguments{Operation: "reprocess", RecordingMediaID: "fixture-media", ClientRunID: "stable-paid-key"}, true, "RequestRecordingTranscription"},
		{RecordingActionArguments{Operation: "metadata", RecordingID: "fixture-recording", RecordingTitle: "Fixture", ExpectedSourceGeneration: &generation}, false, ""},
		{RecordingActionArguments{Operation: "metadata", RecordingID: "fixture-recording", RecordingTitle: "Fixture", RecordedAt: &unknownDate, ExpectedSourceGeneration: &generation}, true, "UpdateRecordingMetadata"},
		{RecordingActionArguments{Operation: "speaker-confirm", RecordingID: "fixture-recording", RecordingTranscriptionAttemptID: "fixture-attempt", LocalSpeakerID: "local-speaker", ConfirmedName: "Speaker Fixture", ClientCorrectionID: "stable-edit-key", ExpectedSourceGeneration: &generation, PassageStartMS: &groupBound, PassageEndMS: &groupBound}, true, "CorrectRecordingSpeaker"},
		{RecordingActionArguments{Operation: "text-edit", RecordingID: "fixture-recording", RecordingTranscriptSegmentID: "fixture-segment", CorrectedTranscriptText: "Exact user edit", ExpectedSourceGeneration: &generation}, false, ""},
		{RecordingActionArguments{Operation: "text-edit", RecordingID: "fixture-recording", RecordingTranscriptSegmentID: "fixture-segment", CorrectedTranscriptText: "Exact user edit", ExpectedSourceGeneration: &generation, ClientCorrectionID: "stable-edit-key"}, true, "CorrectRecordingTranscriptText"},
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
	}
}
