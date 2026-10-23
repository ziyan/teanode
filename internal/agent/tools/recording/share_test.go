package recording

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

// sharingRun is a run that is, or is not, a conversation the person started
// with the agent, and one that is, or is not, an answer in a chat of several.
type sharingRun struct {
	*recordingTestRun
	isStartedByPerson, isAnsweringInChat bool
}

func (self *sharingRun) IsStartedByPerson() bool { return self.isStartedByPerson }
func (self *sharingRun) IsAnsweringInChat() bool { return self.isAnsweringInChat }

// answeringOperations answers each document by the operation it names.
type answeringOperations struct {
	recordingTestOperations
	answers   map[string]string
	documents []string
	variables map[string]map[string]any
}

func (self *answeringOperations) Execute(ctx context.Context, document string, variables map[string]any, answered any) error {
	self.documents = append(self.documents, document)
	if self.variables == nil {
		self.variables = map[string]map[string]any{}
	}
	for operationName, answer := range self.answers {
		if strings.Contains(document, operationName+"(") || strings.Contains(document, operationName+" ") {
			self.variables[operationName] = variables
			return json.Unmarshal([]byte(`{"`+operationName+`":`+answer+`}`), answered)
		}
	}
	return json.Unmarshal([]byte(`{}`), answered)
}

func newSharingRun(isStartedByPerson, isAnsweringInChat bool, answers map[string]string) (*sharingRun, *answeringOperations) {
	operations := &answeringOperations{answers: answers}
	return &sharingRun{
		recordingTestRun:  &recordingTestRun{operations: operations},
		isStartedByPerson: isStartedByPerson, isAnsweringInChat: isAnsweringInChat,
	}, operations
}

func callRecording(run tools.Run, arguments string) (*tools.Result, error) {
	return runRecording(tools.WithRun(context.Background(), run), &tools.Call{Arguments: json.RawMessage(arguments)})
}

const sharedRecordingAnswer = `{"recordingId":"r1","ownerUserId":"u1","ownerUsername":"alex","ownerName":"Alex","recordingTitle":"Standup",
	"recordingSummary":"Two items.","recordingShareLevel":"transcript","speakerNames":["Alex"],"passageCount":3,"hasMorePassages":true,
	"passages":[{"segmentStartMS":0,"segmentEndMS":1000,"speakerName":"Alex","transcriptText":"We ship on Friday.","isFiller":false},
	{"segmentStartMS":1000,"segmentEndMS":2000,"speakerName":"Alex","transcriptText":"Agreed.","isFiller":false}]}`

// Another person's recording is read only in a conversation the person
// started, and what comes back is marked as shared and as untrusted, so that
// the run that files what the conversation taught does not write it down.
func TestSharedRecordingsAreReadOnlyInAConversationThePersonStarted(test *testing.T) {
	answers := map[string]string{
		"ReadSharedRecording":  sharedRecordingAnswer,
		"ListSharedRecordings": `[{"recordingId":"r1","ownerUsername":"alex","recordingTitle":"Standup","recordingShareLevel":"summary","speakerNames":[],"passages":[]}]`,
	}
	for name, allowed := range map[string]struct {
		isStartedByPerson, isAnsweringInChat, isAllowed bool
	}{
		"a conversation the person started":  {true, false, true},
		"a chat of several":                  {true, true, false},
		"a dream or a background run":        {false, false, false},
		"a background run answering in chat": {false, true, false},
	} {
		for _, operation := range []string{"shared", "read-shared"} {
			test.Run(name+" "+operation, func(test *testing.T) {
				run, operations := newSharingRun(allowed.isStartedByPerson, allowed.isAnsweringInChat, answers)
				result, err := callRecording(run, `{"operation":"`+operation+`","recording_id":"r1"}`)
				if !allowed.isAllowed {
					if err == nil || result != nil {
						test.Fatalf("the read was not refused: %+v", result)
					}
					if len(operations.documents) != 0 {
						test.Fatalf("a refused read still asked the server: %v", operations.documents)
					}
					return
				}
				if err != nil {
					test.Fatal(err)
				}
				if !strings.HasPrefix(result.Content, tools.SharedRecordingMarker+"\n") || !result.Untrusted {
					test.Fatalf("a shared recording is marked, and untrusted: %q", result.Content)
				}
				if run.observedEvidenceCount != 1 {
					test.Errorf("the run is told it saw recorded speech: %d", run.observedEvidenceCount)
				}
				if !strings.Contains(tools.TextFromSharedPages(result.Content), "Standup") {
					test.Errorf("the words follow the marker: %q", result.Content)
				}
			})
		}
	}
}

// A page of a shared transcript says where the next begins.
func TestReadingASharedRecordingPagesAndSendsTheOffset(test *testing.T) {
	run, operations := newSharingRun(true, false, map[string]string{"ReadSharedRecording": sharedRecordingAnswer})
	result, err := callRecording(run, `{"operation":"read-shared","recording_id":"r1","segment_offset":10,"page_size":2}`)
	if err != nil {
		test.Fatal(err)
	}
	variables := operations.variables["ReadSharedRecording"]
	if variables["recordingId"] != "r1" || variables["passageOffset"] != 10 || variables["pageSize"] != 2 {
		test.Fatalf("the page asked for: %v", variables)
	}
	var content struct {
		Recording         models.SharedRecording `json:"recording"`
		NextSegmentOffset int                    `json:"next_segment_offset"`
	}
	if err := json.Unmarshal([]byte(tools.TextFromSharedPages(result.Content)), &content); err != nil {
		test.Fatal(err)
	}
	if content.NextSegmentOffset != 12 || len(content.Recording.Passages) != 2 {
		test.Fatalf("the next page starts after the two passages read: %+v", content)
	}
	if _, err := callRecording(run, `{"operation":"read-shared"}`); err == nil {
		test.Fatal("read-shared without a recording")
	}
	if _, err := callRecording(run, `{"operation":"read-shared","recording_id":"r1","page_size":500}`); err == nil {
		test.Fatal("a page larger than the server gives")
	}
}

// Sharing resolves who it is for the way every share does, asks the person
// first with the warning that the others who speak did not agree, and
// unsharing is a write that needs no such card.
func TestSharingARecordingAsksWithTheConsentWarning(test *testing.T) {
	answers := map[string]string{
		"ListCirclePeople": `[{"userId":"u-sam","username":"sam"}]`,
		"ListCircles":      `[{"id":"c1","name":"Family","members":[{"userId":"u-alex"},{"userId":"u-sam"}]}]`,
		"ShareRecording":   `{"id":"s1","recordingId":"r1","granteeUserId":"u-sam","granteeUsername":"sam","recordingShareLevel":"transcript"}`,
		"ReadRecording":    `{"recording":{"id":"r1","recordingTitle":"Standup"},"mediaFiles":[]}`,
		"UnshareRecording": `true`,
	}
	recordingTool := tools.Build().Get("recording")
	shareCall := `{"operation":"share","recording_id":"r1","with":"sam","recording_share_level":"transcript"}`
	if recordingTool.RiskFor(json.RawMessage(shareCall)) != tools.RiskGranting {
		test.Fatal("sharing a recording is granting")
	}
	for _, operation := range []string{"unshare", "shares", "shared", "read-shared"} {
		want := tools.RiskRead
		if operation == "unshare" {
			want = tools.RiskWrite
		}
		if got := recordingTool.RiskFor(json.RawMessage(`{"operation":"` + operation + `"}`)); got != want {
			test.Errorf("%s has risk %q", operation, got)
		}
	}

	run, operations := newSharingRun(true, false, answers)
	card := recordingTool.PreviewLine(tools.WithRun(context.Background(), run), json.RawMessage(shareCall))
	for _, wanted := range []string{`"Standup"`, "sam", `"transcript"`, "have not agreed to it being shared"} {
		if !strings.Contains(card, wanted) {
			test.Errorf("the card lacks %q: %s", wanted, card)
		}
	}
	if strings.Contains(card, "r1") || strings.Contains(card, "u-sam") {
		test.Errorf("the card shows an identifier: %s", card)
	}

	result, err := callRecording(run, shareCall)
	if err != nil {
		test.Fatal(err)
	}
	variables := operations.variables["ShareRecording"]
	if variables["recordingId"] != "r1" || variables["granteeUserId"] != "u-sam" || variables["granteeCircleId"] != nil || variables["recordingShareLevel"] != "transcript" {
		test.Fatalf("who it was shared with: %v", variables)
	}
	if !strings.Contains(result.Content, "sam") || !strings.Contains(result.Content, "s1") {
		test.Errorf("the result says what was done: %s", result.Content)
	}

	// A circle by name, and a level there is not.
	if _, err := callRecording(run, `{"operation":"share","recording_id":"r1","with":"Family","recording_share_level":"summary"}`); err != nil {
		test.Fatal(err)
	}
	if variables := operations.variables["ShareRecording"]; variables["granteeCircleId"] != "c1" || variables["granteeUserId"] != nil {
		test.Errorf("a circle by its name: %v", variables)
	}
	if _, err := callRecording(run, `{"operation":"share","recording_id":"r1","with":"sam","recording_share_level":"audio"}`); err == nil {
		test.Error("a level there is not")
	}
	if _, err := callRecording(run, `{"operation":"share","recording_id":"r1","with":"stranger","recording_share_level":"summary"}`); err == nil {
		test.Error("somebody the person shares no circle with")
	}
	if _, err := callRecording(run, `{"operation":"unshare","recording_share_id":"s1"}`); err != nil {
		test.Fatal(err)
	}
	if operations.variables["UnshareRecording"]["recordingShareId"] != "s1" {
		test.Errorf("the share taken back: %v", operations.variables["UnshareRecording"])
	}
}

// The operations of sharing are in the tool's schema, with the arguments they
// take.
func TestTheRecordingToolOffersTheSharingOperations(test *testing.T) {
	recordingTool := tools.Build().Get("recording")
	properties, _ := recordingTool.Parameters["properties"].(map[string]any)
	operation, _ := properties["operation"].(map[string]any)
	offered := map[string]bool{}
	switch enum := operation["enum"].(type) {
	case []string:
		for _, name := range enum {
			offered[name] = true
		}
	case []any:
		for _, name := range enum {
			offered[name.(string)] = true
		}
	}
	for _, name := range []string{"share", "shares", "unshare", "shared", "read-shared"} {
		if !offered[name] {
			test.Errorf("the recording tool has no %s", name)
		}
	}
	for _, name := range []string{"with", "recording_share_level", "recording_share_id"} {
		if properties[name] == nil {
			test.Errorf("the recording tool has no %s argument", name)
		}
	}
}
