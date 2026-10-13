package cmd

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/urfave/cli/v3"
	"github.com/ziyan/teanode/internal/client"
)

func recordingActionCommands() []*cli.Command {
	definitions := []struct {
		operationName, commandUsage string
		flagNames                   []string
	}{
		{"prepare-playback", "prepare compatible audio in owned storage without a provider charge", []string{"media-id"}},
		{"read-playback", "inspect a ready compatible audio preview without creating one", []string{"media-id"}},
		{"availability", "show recording processing availability without making a provider call", nil},
		{"models", "inspect known transcription model capabilities (requires server-management permission)", nil},
		{"passage-offset", "locate an exact historical citation without loading the whole transcript", []string{"transcript-run-id", "passage-start-ms", "passage-end-ms"}},
		{"timeline", "read the latest completed original versions in absolute chronological order; liveUtterances are words heard during a capture that no completed version covers yet", []string{"recording-id", "segment-offset", "page-size"}},
		{"versions", "inspect immutable transcript versions and processing status", []string{"recording-id"}},
		{"transcript", "read a transcript page, request-local speakers and provider usage evidence", []string{"transcript-run-id", "segment-offset", "page-size"}},
		{"process", "request initial paid transcription using the configured audio model", []string{"media-id", "client-run-id"}},
		{"reprocess", "explicitly request a new paid version; reuse its key after an uncertain response", []string{"media-id", "client-run-id", "force"}},
		{"cancel", "stop further processing without claiming an in-flight call was refunded", []string{"transcript-run-id"}},
		{"retry", "resume this version when the server can safely continue its work", []string{"transcript-run-id"}},
		{"speakers", "inspect confirmed identities, correction intervals and voice references", []string{"recording-id"}},
		{"speaker-confirm", "apply a name supplied by the user to a passage or request-local group; --me says the speaker is you", []string{"recording-id", "expected-source-generation", "client-correction-id", "confirmed-name", "me", "attempt-id", "local-speaker-id", "passage-start-ms", "passage-end-ms"}},
		{"temporary-speaker-name", "name a speaker of a recording still being captured (label such as \"Speaker 1\" from the timeline) using the name supplied by the user; the name applies to every piece; --me says the speaker is you", []string{"recording-id", "speaker-label", "confirmed-name", "me"}},
		{"speaker-rename", "rename an identity linked to this recording using the name supplied by the user", []string{"recording-id", "expected-source-generation", "speaker-identity-id", "confirmed-name"}},
		{"speaker-reset", "remove a manual speaker correction", []string{"recording-id", "expected-source-generation", "speaker-correction-id"}},
		{"reference-delete", "revoke a curated voice reference without deleting the original", []string{"recording-id", "expected-source-generation", "voice-reference-id", "force"}},
		{"text-corrections", "inspect immutable recognized-text correction history", []string{"recording-id"}},
		{"text-edit", "apply exact recognized-text correction supplied by the user without a provider call", []string{"recording-id", "expected-source-generation", "segment-id", "client-correction-id", "corrected-transcript-text"}},
		{"text-reset", "remove a recognized-text correction without rewriting the generated version", []string{"recording-id", "expected-source-generation", "text-correction-id"}},
		{"resume-learning", "resume source reading from current evidence without paid transcription", []string{"recording-id", "expected-source-generation"}},
		{"learning", "inspect automatic knowledge ingestion and processed passage counts", []string{"recording-id"}},
		{"source-schedule", "change periodic source reading; an explicit empty cron disables it", []string{"knowledge-source-id", "source-cron"}},
		{"reference-invalidations", "inspect attribution made uncertain by revoked voice references", []string{"recording-id"}},
		{"metadata", "edit the title and occurrence date (empty date means unknown; leave --recording-title out to keep the title, give it empty to have it generated again)", []string{"recording-id", "expected-source-generation", "recording-title", "recorded-at"}},
		{"import-attachment", "import a delivered audio/video attachment within the attachment delivery limit", []string{"attachment-id", "client-recording-id", "recording-title", "media-id"}},
	}
	commands := make([]*cli.Command, 0, len(definitions))
	for _, definition := range definitions {
		flags := []cli.Flag{JSONFlag()}
		for _, flagName := range definition.flagNames {
			switch flagName {
			case "force":
				flags = append(flags, &cli.BoolFlag{Name: flagName, Usage: "perform this explicit operation without asking first"})
			case "me":
				flags = append(flags, &cli.BoolFlag{Name: flagName, Usage: "this speaker is you; the name defaults to your display name and no other speaker stays marked as you"})
			case "expected-source-generation", "passage-start-ms", "passage-end-ms":
				flags = append(flags, &cli.Int64Flag{Name: flagName, Usage: "explicit generation or passage bound in milliseconds; -1/-1 selects the request-local speaker group"})
			case "segment-offset", "page-size":
				flags = append(flags, &cli.IntFlag{Name: flagName, Usage: "bounded transcript pagination"})
			default:
				flags = append(flags, &cli.StringFlag{Name: flagName})
			}
		}
		commands = append(commands, &cli.Command{Name: definition.operationName, Usage: definition.commandUsage, Flags: flags, Action: runRecordingAction})
	}
	return commands
}
func runRecordingAction(ctx context.Context, command *cli.Command) error {
	inputFields := map[string]any{"operation": command.Name}
	for _, flag := range command.Flags {
		flagName := flag.Names()[0]
		if !command.IsSet(flagName) {
			continue
		}
		fieldName := strings.ReplaceAll(flagName, "-", "_")
		switch flagName {
		case "force", "json":
			continue
		case "expected-source-generation", "passage-start-ms", "passage-end-ms":
			inputFields[fieldName] = command.Int64(flagName)
		case "me":
			inputFields["is_account_owner"] = command.Bool(flagName)
		case "segment-offset", "page-size":
			inputFields[fieldName] = command.Int(flagName)
		default:
			inputFields[fieldName] = command.String(flagName)
		}
	}
	argumentsJSON, err := json.Marshal(inputFields)
	if err != nil {
		return err
	}
	var arguments client.RecordingActionArguments
	if err := json.Unmarshal(argumentsJSON, &arguments); err != nil {
		return err
	}
	operationName, variables, err := client.RecordingAction(arguments)
	if err != nil {
		return usage(err.Error())
	}
	if command.Name == "reprocess" {
		if err := confirm(command, "A new paid transcription version will be requested. Configured prices are estimates; actual billing requires provider evidence."); err != nil {
			return err
		}
	}
	if command.Name == "reference-delete" {
		if err := confirm(command, "The voice reference will be revoked. Dependent automatic speaker attribution may become uncertain."); err != nil {
			return err
		}
	}
	var answer json.RawMessage
	if err := recordingCall(ctx, command, operationName, variables, &answer); err != nil {
		return err
	}
	return PrintJSON(answer)
}
