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
	documentListChatCalls = `query ($conversationId: String!, $first: Int) { ListChatCalls(conversationId: $conversationId, first: $first) {
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
		first := arguments.Limit
		if first <= 0 {
			first = 20
		}
		if result, err = operator.Execute(ctx, documentListChatCalls, map[string]any{"conversationId": conversationId, "first": first}); err != nil {
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
