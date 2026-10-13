package recording

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

type recordingTestRun struct {
	tools.Run
	operations            tools.Operations
	observedEvidenceCount int
}

func (self *recordingTestRun) ObserveRecordingEvidence() { self.observedEvidenceCount++ }

func (self *recordingTestRun) Operations() tools.Operations { return self.operations }

type recordingTestOperations struct {
	operationName string
	variables     map[string]any
	responseJSON  string
	err           error
}

func (self *recordingTestOperations) Permissions() *models.EffectivePermissions {
	return &models.EffectivePermissions{}
}
func (self *recordingTestOperations) Execute(ctx context.Context, document string, variables map[string]any, answered any) error {
	self.variables = variables
	if self.err != nil {
		return self.err
	}
	if self.responseJSON != "" {
		return json.Unmarshal([]byte(self.responseJSON), answered)
	}
	response := `{"ListRecordings":[]}`
	if strings.Contains(document, "ReadRecording(") {
		self.operationName = "ReadRecording"
		response = `{"ReadRecording":{"recording":{"id":"fixture-recording"},"mediaFiles":[{"id":"fixture-media","recordingUploadState":"complete"}]}}`
	}
	return json.Unmarshal([]byte(response), answered)
}

func TestSuccessfulOwnedEvidenceReadsNotifyTheTrustedObserver(test *testing.T) {
	for _, testCase := range []struct{ operation, operationName string }{
		{"read-playback", "ReadRecordingPlayback"}, {"list", "ListRecordings"}, {"read", "ReadRecording"}, {"versions", "ReadRecordingProcessing"},
		{"timeline", "ReadRecordingTranscriptTimeline"}, {"transcript", "ReadRecordingTranscript"},
		{"speakers", "ReadRecordingSpeakers"}, {"text-corrections", "ReadRecordingTranscriptTextCorrections"},
		{"reference-invalidations", "ReadRecordingReferenceInvalidations"}, {"learning", "ReadRecordingLearning"},
		{"limits", "ReadRecordingLimits"}, {"availability", "ReadRecordingTranscriptionAvailability"},
		{"models", "ListAgentModels"}, {"passage-offset", "ReadRecordingTranscriptPassageOffset"},
	} {
		test.Run(testCase.operation, func(test *testing.T) {
			responseJSON, err := json.Marshal(map[string]any{testCase.operationName: map[string]any{}})
			if err != nil {
				test.Fatal(err)
			}
			operations := &recordingTestOperations{responseJSON: string(responseJSON)}
			run := &recordingTestRun{operations: operations}
			arguments, err := json.Marshal(map[string]any{"operation": testCase.operation, "recording_id": "fixture-recording", "media_id": "fixture-media", "transcript_run_id": "fixture-run", "passage_start_ms": 200, "passage_end_ms": 500})
			if err != nil {
				test.Fatal(err)
			}
			result, err := runRecording(tools.WithRun(test.Context(), run), &tools.Call{Arguments: arguments})
			if err != nil {
				test.Fatal(err)
			}
			expectedEvidenceCount := 1
			switch testCase.operation {
			case "limits", "availability", "models", "passage-offset":
				expectedEvidenceCount = 0
			}
			if run.observedEvidenceCount != expectedEvidenceCount || !result.Untrusted {
				test.Fatal("incorrect evidence observer notification")
			}
			operations.err = errors.New("owned read refused")
			if _, err := runRecording(tools.WithRun(test.Context(), run), &tools.Call{Arguments: arguments}); err == nil || run.observedEvidenceCount != expectedEvidenceCount {
				test.Fatal("failed read notified the observer")
			}
		})
	}
}
func TestRecordingToolUsesAccountAPIAndMarksEvidenceUntrusted(test *testing.T) {
	operations := &recordingTestOperations{}
	run := &recordingTestRun{operations: operations}
	result, err := runRecording(tools.WithRun(test.Context(), run), &tools.Call{Arguments: json.RawMessage(`{"operation":"read","recording_id":"fixture-recording"}`)})
	if err != nil {
		test.Fatal(err)
	}
	if !result.Untrusted || operations.operationName != "ReadRecording" || operations.variables["recordingId"] != "fixture-recording" || !strings.Contains(result.Content, "/api/v1/recordings/media/fixture-media") {
		test.Fatal("tool bypassed shared semantics or trusted source evidence")
	}
	for _, arguments := range []string{`{"operation":"activate_microphone"}`, `{"operation":"read"}`, `{"operation":"delete"}`} {
		if _, err := runRecording(tools.WithRun(context.Background(), run), &tools.Call{Arguments: json.RawMessage(arguments)}); err == nil {
			test.Fatal("invalid recording action accepted")
		}
	}
}

func TestPlaybackPreparationIsAWriteAndReadyPlaybackIsARead(test *testing.T) {
	recordingTool := tools.Build().Get("recording")
	for _, fixture := range []struct {
		operation    string
		expectedRisk tools.Risk
	}{{"prepare-playback", tools.RiskWrite}, {"read-playback", tools.RiskRead}} {
		arguments, err := json.Marshal(map[string]any{"operation": fixture.operation, "media_id": "fixture-media"})
		if err != nil {
			test.Fatal(err)
		}
		if recordingTool.RiskOf(arguments) != fixture.expectedRisk {
			test.Fatal("local playback preparation has the wrong risk")
		}
	}
}
