package conversation

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
)

// Calls in a chat, read through the same operations the dashboard and the
// phone use: list_calls is a chat's calls, newest first, and read_call one
// call with its transcript when it was transcribed. Starting a call is not
// the agent's: a call is the person's own voice.

const (
	documentListChatCalls = `query ($conversationId: String!, $first: Int, $before: DateTime) { ListChatCalls(conversationId: $conversationId, first: $first, before: $before) {
		id startedByName startedAt answeredAt endedAt endReason isGoing durationMs transcriptState
		members { name ringState } } }`
	documentReadChatCall = `query ($callId: String!) { ReadChatCall(callId: $callId) {
		call { id chatTitle startedByName startedAt answeredAt endedAt endReason isGoing durationMs transcriptState members { name ringState } }
		transcript { speakerName startMs text } recordingId isRecordingOwned } }`
)

// callActions read the calls of a chat.
var callActions = []string{"list_calls", "read_call"}

func isCallAction(action string) bool {
	for _, name := range callActions {
		if name == action {
			return true
		}
	}
	return false
}

// runCallAction does list_calls or read_call. What was said on a call is
// what other people said, so the answer is data, not instructions.
func runCallAction(ctx context.Context, arguments conversationArguments) (*tools.Result, error) {
	var result map[string]any
	var err error
	switch arguments.Action {
	case "list_calls":
		conversationId := strings.TrimSpace(arguments.ConversationID)
		if conversationId == "" {
			return nil, fmt.Errorf("list_calls needs the conversation_id of a chat, which list gives")
		}
		if result, err = listCalls(ctx, conversationId, arguments); err != nil {
			return nil, err
		}
	case "read_call":
		callId := strings.TrimSpace(arguments.CallID)
		if callId == "" {
			return nil, fmt.Errorf("read_call needs the call_id, which list_calls gives")
		}
		if result, err = operator.Execute(ctx, documentReadChatCall, map[string]any{"callId": callId}); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%q is not an action on calls", arguments.Action)
	}
	answer, err := tools.JSONResult(result)
	if err != nil {
		return nil, err
	}
	answer.Untrusted = true
	return answer, nil
}

// listCalls is one page of a chat's calls, newest first, after skipping
// arguments.Offset of them, with whether there are more and where they
// start. The operation pages by time, so skipping is done by reading on from
// the oldest call seen.
func listCalls(ctx context.Context, conversationId string, arguments conversationArguments) (map[string]any, error) {
	limit := arguments.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	offset := max(arguments.Offset, 0)
	// One more than the page, to know there is another.
	want := offset + limit + 1
	var calls []any
	var before any
	for len(calls) < want {
		variables := map[string]any{"conversationId": conversationId, "first": min(want-len(calls), callsPerRead)}
		if before != nil {
			variables["before"] = before
		}
		answer, err := operator.Execute(ctx, documentListChatCalls, variables)
		if err != nil {
			return nil, err
		}
		page, _ := answer["ListChatCalls"].([]any)
		calls = append(calls, page...)
		if len(page) < variables["first"].(int) {
			break
		}
		last, _ := page[len(page)-1].(map[string]any)
		if before, _ = last["startedAt"]; before == nil {
			break
		}
	}
	calls = calls[min(offset, len(calls)):]
	isMore := len(calls) > limit
	if isMore {
		calls = calls[:limit]
	}
	listed := map[string]any{"calls": calls, "more": isMore}
	if isMore {
		listed["next_offset"] = offset + len(calls)
		listed["more_note"] = pageNote(len(calls), offset+len(calls), "calls")
	}
	return listed, nil
}

// callsPerRead is how many calls one read of the operation is asked for; the
// operation gives 200 at most.
const callsPerRead = 200
