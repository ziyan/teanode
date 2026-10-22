package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// Recordings shared with other people: who a recording is shared with and at
// what level, and reading what others shared. The agent's recording tool has
// the same operations under the same names.

func recordingShareCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name:      "share",
			Usage:     "share a recording with somebody you share a circle with, or with a whole circle, at a level",
			ArgsUsage: "<recording-id> <username|circle>",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.StringFlag{Name: "level", Usage: "how much to show: summary (title, date, length, the summary and the speakers you named) or transcript (also the words). Never the audio"},
				&cli.BoolFlag{Name: "force", Usage: "share without asking first"},
			},
			Action: runRecordingShare,
		},
		{
			Name:      "shares",
			Usage:     "show who a recording is shared with, and at what level",
			ArgsUsage: "<recording-id>",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runRecordingShares,
		},
		{
			Name:      "unshare",
			Usage:     "stop sharing a recording with somebody",
			ArgsUsage: "<share-id>",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runRecordingUnshare,
		},
		{
			Name:  "shared",
			Usage: "list the recordings others shared with you",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.StringFlag{Name: "before", Usage: "continue before this recording ID"},
				&cli.IntFlag{Name: "limit", Value: 25, Usage: "recordings per page (1 to 100)"},
			},
			Action: runRecordingShared,
		},
		{
			Name:      "read-shared",
			Usage:     "read a recording somebody shared with you, as far as the level they gave you goes",
			ArgsUsage: "<recording-id>",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.IntFlag{Name: "offset", Usage: "passage of the transcript to start at"},
				&cli.IntFlag{Name: "limit", Value: 50, Usage: "passages per page (1 to 250)"},
			},
			Action: runRecordingReadShared,
		},
	}
}

func runRecordingShare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return usage("usage: teanode recording share <recording-id> <username|circle> --level summary|transcript")
	}
	recordingShareLevel, isGrantable := models.ParseRecordingShareLevel(command.String("level"))
	if !isGrantable {
		return usage("say how much to show with --level summary or transcript")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	typed := command.Args().Get(1)
	granteeUserId, granteeCircleId, described, err := shareTarget(ctx, connection, typed)
	if err != nil {
		return describeError(command, err)
	}
	if err := confirm(command, "Sharing this recording with "+forTerminal(described)+" at "+string(recordingShareLevel)+". "+models.RecordingShareConsentWords); err != nil {
		return err
	}
	operationName, variables, err := client.RecordingAction(client.RecordingActionArguments{
		Operation: "share", RecordingID: command.Args().First(), GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId,
		RecordingShareLevel: string(recordingShareLevel),
	})
	if err != nil {
		return usage(err.Error())
	}
	var share client.RecordingShare
	if err := client.RunRecording(ctx, connection, operationName, variables, &share); err != nil {
		return describeNotFound(command, err, "that recording or "+typed)
	}
	if command.Bool("json") {
		return PrintJSON(share)
	}
	fmt.Printf("shared the recording with %s at %s\n", forTerminal(described), share.RecordingShareLevel)
	return nil
}

func runRecordingShares(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode recording shares <recording-id>")
	}
	operationName, variables, err := client.RecordingAction(client.RecordingActionArguments{Operation: "shares", RecordingID: command.Args().First()})
	if err != nil {
		return usage(err.Error())
	}
	var shares []*client.RecordingShare
	if err := recordingCall(ctx, command, operationName, variables, &shares); err != nil {
		return err
	}
	if command.Bool("json") {
		if shares == nil {
			shares = []*client.RecordingShare{}
		}
		return PrintJSON(shares)
	}
	if len(shares) == 0 {
		fmt.Println("the recording is not shared with anybody")
		return nil
	}
	rows := make([][]string, 0, len(shares))
	for _, share := range shares {
		who := share.GranteeUsername
		if share.GranteeCircleID != nil {
			who = models.DescribeCircleByName(share.GranteeCircleName)
		}
		rows = append(rows, []string{who, share.RecordingShareLevel, share.ID})
	}
	return printTable([]string{"with", "level", "id"}, rows)
}

func runRecordingUnshare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode recording unshare <share-id>; 'teanode recording shares <recording-id>' lists them")
	}
	operationName, variables, err := client.RecordingAction(client.RecordingActionArguments{Operation: "unshare", RecordingShareID: command.Args().First()})
	if err != nil {
		return usage(err.Error())
	}
	var isUnshared bool
	if err := recordingCall(ctx, command, operationName, variables, &isUnshared); err != nil {
		return err
	}
	fmt.Println("no longer shared")
	return nil
}

// recordingDay is the day a recording is of: when it was recorded, else when
// it was kept.
func recordingDay(recordedAt *time.Time, createdAt time.Time) string {
	if recordedAt != nil {
		return recordedAt.Local().Format("2006-01-02")
	}
	return createdAt.Local().Format("2006-01-02")
}

func runRecordingShared(ctx context.Context, command *cli.Command) error {
	operationName, variables, err := client.RecordingAction(client.RecordingActionArguments{
		Operation: "shared", PageSize: command.Int("limit"), BeforeRecordingID: command.String("before"),
	})
	if err != nil {
		return usage(err.Error())
	}
	var shared []*models.SharedRecording
	if err := recordingCall(ctx, command, operationName, variables, &shared); err != nil {
		return err
	}
	if command.Bool("json") {
		if shared == nil {
			shared = []*models.SharedRecording{}
		}
		return PrintJSON(shared)
	}
	if len(shared) == 0 {
		fmt.Println("nobody has shared a recording with you")
		return nil
	}
	rows := make([][]string, 0, len(shared))
	for _, recording := range shared {
		rows = append(rows, []string{recording.RecordingID, forTerminal(recording.RecordingTitle), forTerminal(recording.OwnerUsername),
			string(recording.RecordingShareLevel), recordingDay(recording.RecordedAt, recording.CreatedAt)})
	}
	return printTable([]string{"ID", "TITLE", "FROM", "LEVEL", "DATE"}, rows)
}

// clockText is a moment of a recording as minutes and seconds, or hours too.
func clockText(milliseconds int64) string {
	seconds := milliseconds / 1000
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds%3600/60, seconds%60)
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

func runRecordingReadShared(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode recording read-shared <recording-id>; 'teanode recording shared' lists them")
	}
	operationName, variables, err := client.RecordingAction(client.RecordingActionArguments{
		Operation: "read-shared", RecordingID: command.Args().First(), SegmentOffset: command.Int("offset"), PageSize: command.Int("limit"),
	})
	if err != nil {
		return usage(err.Error())
	}
	var shared models.SharedRecording
	if err := recordingCall(ctx, command, operationName, variables, &shared); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(shared)
	}
	// Everything below is another person's words: the title, the summary, the
	// names and the transcript are shown with control characters made visible.
	fmt.Printf("%s\n", forTerminal(shared.RecordingTitle))
	fmt.Printf("from %s, %s, %s, shared at %s\n", forTerminal(shared.OwnerUsername), recordingDay(shared.RecordedAt, shared.CreatedAt),
		clockText(shared.TimelineDurationMS), shared.RecordingShareLevel)
	if shared.RecordingSummary != "" {
		fmt.Printf("\n%s\n", forTerminal(shared.RecordingSummary))
	}
	if len(shared.SpeakerNames) > 0 {
		names := make([]string, 0, len(shared.SpeakerNames))
		for _, name := range shared.SpeakerNames {
			names = append(names, forTerminal(name))
		}
		fmt.Printf("\nspeakers: %s\n", strings.Join(names, ", "))
	}
	if shared.RecordingShareLevel != models.RecordingShareLevelTranscript {
		return nil
	}
	fmt.Println()
	for _, passage := range shared.Passages {
		fmt.Printf("[%s] %s: %s\n", clockText(passage.SegmentStartMS), forTerminal(passage.SpeakerName), forTerminal(passage.TranscriptText))
	}
	if shared.HasMorePassages {
		fmt.Printf("\nmore: --offset %d\n", command.Int("offset")+len(shared.Passages))
	}
	return nil
}
