package recording

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

type recordingTestRun struct {
	tools.Run
	operations tools.Operations
}

func (self *recordingTestRun) Operations() tools.Operations { return self.operations }

type recordingTestOperations struct {
	operationName string
	variables     map[string]any
}

func (self *recordingTestOperations) Permissions() *models.EffectivePermissions {
	return &models.EffectivePermissions{}
}
func (self *recordingTestOperations) Execute(ctx context.Context, document string, variables map[string]any, answered any) error {
	self.variables = variables
	response := `{"ListRecordings":[]}`
	if strings.Contains(document, "ReadRecording(") {
		self.operationName = "ReadRecording"
		response = `{"ReadRecording":{"recording":{"id":"fixture-recording"},"mediaFiles":[{"id":"fixture-media","recordingUploadState":"complete"}]}}`
	}
	return json.Unmarshal([]byte(response), answered)
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
