package memory_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/memory"
)

// codingRun is a run that answers the two coding calls without a database:
// what it was asked is kept, and what it answers is set by the test.
type codingRun struct {
	tools.Run
	shownText         string
	isCaptureAsked    bool
	askedDirectory    string
	askedComputer     string
	askedPrompt       string
	capturedFor       string
	capturedAssistant string
}

func (self *codingRun) CodingMemory(_ context.Context, directory, computerName, prompt string) (string, error) {
	self.askedDirectory, self.askedComputer, self.askedPrompt = directory, computerName, prompt
	return self.shownText, nil
}

func (self *codingRun) CaptureCodingSession(_ context.Context, computerName, assistant string) (bool, error) {
	self.capturedFor, self.capturedAssistant = computerName, assistant
	return self.isCaptureAsked, nil
}

// checkout hands the agent the text a coding session in the directory is
// shown, the same text the hooks, the command line and the dashboard get.
func TestCheckoutShowsWhatACodingSessionIsShown(t *testing.T) {
	run := &codingRun{shownText: "<teanode-memory>\nProject: Corner Market\n</teanode-memory>"}
	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "memory")

	result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"checkout","directory":"~/code/corner-market","computer":"laptop","query":"why is the till slow"}`)})
	if err != nil {
		t.Fatalf("checkout: %s", err)
	}
	if !strings.Contains(result.Content, "Project: Corner Market") {
		t.Errorf("the text the session is shown comes back: %s", result.Content)
	}
	if run.askedDirectory != "~/code/corner-market" || run.askedComputer != "laptop" || run.askedPrompt != "why is the till slow" {
		t.Errorf("the call carried the directory, computer and prompt: %q %q %q", run.askedDirectory, run.askedComputer, run.askedPrompt)
	}

	run.shownText = ""
	result, err = tool.Run(ctx, &tools.Call{ID: "c2", Arguments: []byte(`{"action":"checkout","directory":"~/code/corner-market"}`)})
	if err != nil || !strings.Contains(result.Content, "nothing") {
		t.Errorf("an empty answer is said plainly: %v %v", result, err)
	}
	if _, err := tool.Run(ctx, &tools.Call{ID: "c3", Arguments: []byte(`{"action":"checkout"}`)}); err == nil {
		t.Error("a checkout needs a directory")
	}
}

// capture asks for a computer's coding transcripts to be read now, and says
// so when no source reads them.
func TestCaptureAsksForTheTranscriptsToBeReadNow(t *testing.T) {
	run := &codingRun{isCaptureAsked: true}
	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "memory")

	result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(`{"action":"capture","assistant":"codex","computer":"laptop"}`)})
	if err != nil {
		t.Fatalf("capture: %s", err)
	}
	if run.capturedFor != "laptop" || run.capturedAssistant != "codex" {
		t.Errorf("the call carried the computer and assistant: %q %q", run.capturedFor, run.capturedAssistant)
	}
	if !strings.Contains(result.Content, "again") {
		t.Errorf("it says the sessions are being read: %s", result.Content)
	}

	run.isCaptureAsked = false
	result, err = tool.Run(ctx, &tools.Call{ID: "c2", Arguments: []byte(`{"action":"capture","assistant":"claude-code","computer":"laptop"}`)})
	if err != nil || !strings.Contains(result.Content, "nothing") {
		t.Errorf("no source is said plainly: %v %v", result, err)
	}
	if _, err := tool.Run(ctx, &tools.Call{ID: "c3", Arguments: []byte(`{"action":"capture","assistant":"gemini","computer":"laptop"}`)}); err == nil {
		t.Error("an assistant that is not claude-code or codex is refused")
	}
}
