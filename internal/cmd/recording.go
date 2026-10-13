package cmd

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// NewRecordingCommand manages account-owned recordings through the shared API.
func NewRecordingCommand() *cli.Command {
	return &cli.Command{Name: "recording", Usage: "import, browse and download your retained meeting originals", Commands: append([]*cli.Command{
		{Name: "list", Usage: "list your recording library", Flags: []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "before", Usage: "continue before this recording ID"}, &cli.IntFlag{Name: "limit", Value: 25, Usage: "recordings per page (1 to 100)"}}, Action: runRecordingList},
		{Name: "read", Usage: "inspect metadata, media and upload receipts", ArgsUsage: "<recording-id>", Flags: []cli.Flag{JSONFlag()}, Action: runRecordingRead},
		{Name: "limits", Usage: "show recording storage and upload bounds", Flags: []cli.Flag{JSONFlag()}, Action: runRecordingLimits},
		{Name: "import", Usage: "upload an unchanged audio/video file; rerun to resume, your file is always retained", ArgsUsage: "<file>", Flags: []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "recording-id", Usage: "resume this existing recording (requires --media-id)"}, &cli.StringFlag{Name: "media-id", Usage: "exact existing original to resume"}, &cli.StringFlag{Name: "title"}, &cli.StringFlag{Name: "content-type", Usage: "audio or video MIME type if the extension is unknown"}, &cli.StringFlag{Name: "client-recording-id", Usage: "stable import identity; default is the whole-file digest"}}, Action: runRecordingImport},
		{Name: "download", Usage: "save and verify a recording original", ArgsUsage: "<recording-id> <media-id> <destination>", Action: runRecordingDownload},
		{Name: "delete", Usage: "delete a recording, its originals and dependent derived media", ArgsUsage: "<recording-id>", Flags: []cli.Flag{&cli.BoolFlag{Name: "force", Usage: "delete without asking first"}}, Action: runRecordingDelete},
	}, recordingActionCommands()...)}
}
func recordingCall(ctx context.Context, command *cli.Command, operationName string, variables map[string]any, answered any) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	return describeError(command, client.RunRecording(ctx, connection, operationName, variables, answered))
}
func runRecordingList(ctx context.Context, command *cli.Command) error {
	var recordings []*models.Recording
	if err := recordingCall(ctx, command, "ListRecordings", map[string]any{"beforeRecordingId": command.String("before"), "pageSize": command.Int("limit")}, &recordings); err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(recordings)
	}
	rows := make([][]string, 0, len(recordings))
	for _, recording := range recordings {
		rows = append(rows, []string{recording.ID, forTerminal(recording.RecordingTitle), string(recording.RecordingOrigin), string(recording.RecordingCaptureState)})
	}
	return printTable([]string{"ID", "TITLE", "ORIGIN", "CAPTURE"}, rows)
}
func runRecordingRead(ctx context.Context, command *cli.Command) error {
	recordingId := command.Args().First()
	if recordingId == "" {
		return usage("usage: teanode recording read <recording-id>")
	}
	var view client.RecordingView
	if err := recordingCall(ctx, command, "ReadRecording", map[string]any{"recordingId": recordingId}, &view); err != nil {
		return err
	}
	return PrintJSON(view)
}
func runRecordingLimits(ctx context.Context, command *cli.Command) error {
	var limits client.RecordingLimits
	if err := recordingCall(ctx, command, "ReadRecordingLimits", nil, &limits); err != nil {
		return err
	}
	return PrintJSON(limits)
}
func runRecordingImport(ctx context.Context, command *cli.Command) error {
	filename := command.Args().First()
	if filename == "" {
		return usage("usage: teanode recording import <file>")
	}
	source, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	contentType := command.String("content-type")
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(filename))
	}
	if parsedType, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = parsedType
	}
	recordingTitle := command.String("title")
	if recordingTitle == "" {
		recordingTitle = filepath.Base(filename)
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	var view *client.RecordingView
	if command.String("recording-id") != "" || command.String("media-id") != "" {
		if command.String("recording-id") == "" || command.String("media-id") == "" {
			return usage("resume requires both --recording-id and --media-id")
		}
		var ownedView client.RecordingView
		if err := client.RunRecording(ctx, connection, "ReadRecording", map[string]any{"recordingId": command.String("recording-id")}, &ownedView); err != nil {
			return describeError(command, err)
		}
		var expected *models.RecordingMedia
		for _, candidate := range ownedView.MediaFiles {
			if candidate.ID == command.String("media-id") {
				expected = candidate
				break
			}
		}
		view, err = connection.ResumeRecordingFile(ctx, source, expected)
	} else {
		view, err = connection.ImportRecordingFile(ctx, source, filename, contentType, recordingTitle, command.String("client-recording-id"))
	}
	if err != nil {
		return describeError(command, err)
	}
	return PrintJSON(view)
}
func runRecordingDownload(ctx context.Context, command *cli.Command) error {
	recordingId, mediaId, destinationPath := command.Args().Get(0), command.Args().Get(1), command.Args().Get(2)
	if recordingId == "" || mediaId == "" || destinationPath == "" {
		return usage("usage: teanode recording download <recording-id> <media-id> <destination>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	var view client.RecordingView
	if err := client.RunRecording(ctx, connection, "ReadRecording", map[string]any{"recordingId": recordingId}, &view); err != nil {
		return describeError(command, err)
	}
	var media *models.RecordingMedia
	for _, candidate := range view.MediaFiles {
		if candidate.ID == mediaId {
			media = candidate
		}
	}
	if media == nil {
		return fmt.Errorf("recording has no such original")
	}
	if err := downloadRecordingToFile(ctx, connection, media, destinationPath); err != nil {
		return describeError(command, err)
	}
	fmt.Println("saved and verified " + destinationPath)
	return nil
}
func downloadRecordingToFile(ctx context.Context, connection *client.Client, media *models.RecordingMedia, destinationPath string) error {
	destination, err := os.CreateTemp(filepath.Dir(destinationPath), ".recording-download-*")
	if err != nil {
		return err
	}
	defer func() { _ = destination.Close(); _ = os.Remove(destination.Name()) }()
	if err := connection.DownloadRecordingMedia(ctx, media, destination); err != nil {
		return err
	}
	if err := destination.Sync(); err != nil {
		return err
	}
	if err := destination.Close(); err != nil {
		return err
	}
	// Link refuses replacement of a destination somebody already owns.
	if err := os.Link(destination.Name(), destinationPath); err != nil {
		return err
	}
	return nil
}

func runRecordingDelete(ctx context.Context, command *cli.Command) error {
	recordingId := command.Args().First()
	if recordingId == "" {
		return usage("usage: teanode recording delete <recording-id>")
	}
	if err := confirm(command, "The recording and its retained media will be deleted."); err != nil {
		return err
	}
	var isDeleted bool
	if err := recordingCall(ctx, command, "DeleteRecording", map[string]any{"recordingId": recordingId}, &isDeleted); err != nil {
		return err
	}
	fmt.Println("deleted " + recordingId)
	return nil
}
