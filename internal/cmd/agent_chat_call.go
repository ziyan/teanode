package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// chatCallCommands read the calls of a chat. A call is started, answered and
// talked on in the dashboard or the app; the command line reads what came of
// it.
func chatCallCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name:      "calls",
			Usage:     "the calls of a chat, newest first: who started each, how long it went, and who missed it",
			ArgsUsage: "<conversation-id>",
			Flags:     []cli.Flag{JSONFlag(), &cli.IntFlag{Name: "first", Usage: "how many", Value: 20}},
			Action:    runChatCalls,
		},
		{
			Name:      "call",
			Usage:     "one call of a chat, with its transcript when it was transcribed",
			ArgsUsage: "<call-id>",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runChatCall,
		},
	}
}

func runChatCalls(ctx context.Context, command *cli.Command) error {
	conversationId, err := requireConversationArgument(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	calls, err := client.ListChatCalls(ctx, connection, conversationId, int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(calls)
	}
	rows := make([][]string, 0, len(calls))
	for _, chatCall := range calls {
		rows = append(rows, []string{chatCall.ID, chatCall.StartedAt.Local().Format("2006-01-02 15:04"), chatCall.StartedByName, chatCallOutcome(chatCall), chatCallPeople(chatCall)})
	}
	return printTable([]string{"id", "started", "by", "call", "on it"}, rows)
}

func runChatCall(ctx context.Context, command *cli.Command) error {
	callId := strings.TrimSpace(command.Args().First())
	if callId == "" {
		return fmt.Errorf("give the call's id, which 'agent conversation calls' lists")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	detail, err := client.ReadChatCall(ctx, connection, callId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(detail)
	}
	chatCall := detail.Call
	_, _ = fmt.Fprintf(command.Writer, "%s, started by %s at %s: %s\n", chatCall.ChatTitle, chatCall.StartedByName, chatCall.StartedAt.Local().Format("2006-01-02 15:04"), chatCallOutcome(chatCall))
	if people := chatCallPeople(chatCall); people != "" {
		_, _ = fmt.Fprintf(command.Writer, "On it: %s\n", people)
	}
	switch chatCall.TranscriptState {
	case models.ChatCallTranscriptWaiting:
		_, _ = fmt.Fprintln(command.Writer, "The transcript is being written.")
	case models.ChatCallTranscriptFailed:
		_, _ = fmt.Fprintln(command.Writer, "The transcript could not be written.")
	}
	for _, line := range detail.Transcript {
		_, _ = fmt.Fprintf(command.Writer, "[%s] %s: %s\n", models.VoiceMessageClock(line.StartMS), line.SpeakerName, line.Text)
	}
	// The recording plays the call and has the whole transcript, with its
	// corrections.
	if detail.RecordingID != "" {
		if detail.IsRecordingOwned {
			_, _ = fmt.Fprintf(command.Writer, "Recording: %s (teanode recording read %s)\n", detail.RecordingID, detail.RecordingID)
		} else {
			_, _ = fmt.Fprintf(command.Writer, "Recording shared with you: %s (teanode recording read-shared %s)\n", detail.RecordingID, detail.RecordingID)
		}
	}
	return nil
}

// chatCallOutcome is how a call went, in a few words.
func chatCallOutcome(chatCall *client.ChatCall) string {
	switch {
	case chatCall.IsGoing:
		return "going, " + models.VoiceMessageClock(chatCall.DurationMS)
	case chatCall.AnsweredAt == nil && chatCall.EndReason == string(models.ChatCallDeclined):
		return "declined"
	case chatCall.AnsweredAt == nil:
		return "missed"
	}
	return models.VoiceMessageClock(chatCall.DurationMS)
}

// chatCallPeople are the people who were on a call.
func chatCallPeople(chatCall *client.ChatCall) string {
	var names []string
	for _, member := range chatCall.Members {
		if member.RingState == string(models.ChatCallRingAnswered) || member.RingState == string(models.ChatCallRingNone) {
			names = append(names, member.Name)
		}
	}
	return strings.Join(names, ", ")
}
