package recording

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// Sharing a recording, and reading the ones other people shared with the
// person. What the agent reads here it reads as the person, at the level each
// was shared at, through the same API the dashboard and the command line use.
//
// The recordings of other people are read only in a conversation the person
// started, never in a chat with others, a dream or a background run, and what
// they say is marked (tools.SharedRecordingMarker) so that the run that files
// what a conversation taught does not write it down as the person's own.

// sharedRecordingRefusal is what a read of another person's recording is told
// where it may not happen, worded as the memory tool's is for shared pages.
const sharedRecordingRefusal = "recordings other people shared are read only in a one-to-one conversation the person started, not in a chat with others or a background run; tell the person to ask in their own drawer"

// runSharing does the sharing operations; the second answer is false for any
// other operation, which the caller does itself.
func runSharing(ctx context.Context, arguments *recordingArguments) (*tools.Result, bool, error) {
	switch arguments.Operation {
	case "share", "shares", "unshare", "shared", "read-shared":
	default:
		return nil, false, nil
	}
	run := tools.MustRun(ctx)
	isReadingShared := arguments.Operation == "shared" || arguments.Operation == "read-shared"
	if isReadingShared && (!tools.IsStartedByPerson(run) || tools.IsAnsweringInChat(run)) {
		return nil, true, errors.New(sharedRecordingRefusal)
	}
	var grantee *tools.Grantee
	if arguments.Operation == "share" {
		var err error
		if grantee, err = tools.ResolveGrantee(ctx, run.Operations(), arguments.With); err != nil {
			return nil, true, err
		}
		arguments.GranteeUserID, arguments.GranteeCircleID = grantee.UserID, grantee.CircleID
	}
	operationName, variables, err := client.RecordingAction(arguments.RecordingActionArguments)
	if err != nil {
		return nil, true, err
	}
	var answer json.RawMessage
	if err := client.RunRecording(ctx, run.Operations(), operationName, variables, &answer); err != nil {
		return nil, true, err
	}
	switch arguments.Operation {
	case "shared":
		var shared []*models.SharedRecording
		if err := json.Unmarshal(answer, &shared); err != nil {
			return nil, true, err
		}
		return sharedRecordingResult(run, map[string]any{"recordings": shared})
	case "read-shared":
		var shared models.SharedRecording
		if err := json.Unmarshal(answer, &shared); err != nil {
			return nil, true, err
		}
		content := map[string]any{"recording": shared}
		if shared.HasMorePassages {
			content["next_segment_offset"] = arguments.SegmentOffset + len(shared.Passages)
		}
		return sharedRecordingResult(run, content)
	case "share":
		var share client.RecordingShare
		if err := json.Unmarshal(answer, &share); err != nil {
			return nil, true, err
		}
		result, err := tools.JSONResult(map[string]any{
			"recording_share_id": share.ID, "with": grantee.Description, "recording_share_level": share.RecordingShareLevel,
		})
		if err != nil {
			return nil, true, err
		}
		result.Note = "shared a recording with " + grantee.Description
		return result, true, nil
	case "unshare":
		result, err := tools.JSONResult(map[string]any{"unshared": strings.TrimSpace(arguments.RecordingShareID)})
		return result, true, err
	}
	// shares: the names of circles and people are other people's words.
	result, err := tools.JSONResult(json.RawMessage(answer))
	if err != nil {
		return nil, true, err
	}
	result.Untrusted = true
	return result, true, nil
}

// sharedRecordingResult is what another person's recording said, marked as
// shared and as untrusted evidence, and noted on the run as recording
// evidence, as every other read of recorded speech is.
func sharedRecordingResult(run tools.Run, content map[string]any) (*tools.Result, bool, error) {
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, true, err
	}
	result := tools.TextResult("%s\n%s", tools.SharedRecordingMarker, encoded)
	result.Untrusted = true
	tools.MarkRecordingEvidence(run)
	return result, true, nil
}

// recordingShareCard is the confirmation card of share: which recording, who
// gets it and at what level, and that the others who speak in it did not
// agree. It is empty for any other operation, which has its own line.
func recordingShareCard(ctx context.Context, raw json.RawMessage) string {
	var asked recordingArguments
	if json.Unmarshal(raw, &asked) != nil || asked.Operation != "share" {
		return ""
	}
	title := "a recording"
	if run, err := tools.RunFrom(ctx); err == nil && strings.TrimSpace(asked.RecordingID) != "" {
		var view client.RecordingView
		var answer json.RawMessage
		if client.RunRecording(ctx, run.Operations(), "ReadRecording", map[string]any{"recordingId": strings.TrimSpace(asked.RecordingID)}, &answer) == nil &&
			json.Unmarshal(answer, &view) == nil && view.Recording != nil && strings.TrimSpace(view.Recording.RecordingTitle) != "" {
			title = "the recording " + tools.Named(view.Recording.RecordingTitle, "")
		}
	}
	return "Share " + title + " with " + tools.GranteeWords(ctx, asked.With) + ", at " + tools.Named(asked.RecordingShareLevel, "a level") + ". " + models.RecordingShareConsentWords
}
