package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

func recordingCaptureCommands() []*cli.Command {
	return []*cli.Command{
		{Name: "record", Usage: "record a meeting from the microphone, a file or standard input, uploading it in pieces as it goes; Ctrl-C stops", Flags: []cli.Flag{
			JSONFlag(),
			&cli.StringFlag{Name: "input", Usage: "an audio or video file to record from, or - for standard input; default is standard input when it is not a terminal, else the microphone"},
			&cli.StringFlag{Name: "device", Usage: "the microphone as ffmpeg names it (default: pulse default on Linux, :0 on macOS)"},
			&cli.BoolFlag{Name: "no-live", Usage: "do not stream the audio for words as they are spoken"},
		}, Action: runRecordingRecord},
		{Name: "watch", Usage: "print the words of a recording as they are transcribed, live words first, until its transcription is done", ArgsUsage: "<recording-id>", Action: runRecordingWatch},
	}
}

// recordingFfmpegArguments are the arguments that make ffmpeg write mono
// 16-bit PCM at the capture rate to its output, from the chosen source.
func recordingFfmpegArguments(input, device string, isStdinTerminal bool, operatingSystem string) []string {
	arguments := []string{"-hide_banner", "-loglevel", "error"}
	switch {
	case input == "-" || (input == "" && !isStdinTerminal):
		arguments = append(arguments, "-i", "pipe:0")
	case input != "":
		arguments = append(arguments, "-re", "-i", input)
	case operatingSystem == "darwin":
		arguments = append(arguments, "-f", "avfoundation", "-i", firstNonEmpty(device, ":0"))
	default:
		arguments = append(arguments, "-f", "pulse", "-i", firstNonEmpty(device, "default"))
	}
	return append(arguments, "-ac", "1", "-ar", fmt.Sprint(client.RecordingCaptureSampleRate), "-f", "s16le", "pipe:1")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func runRecordingRecord(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return usage("recording needs ffmpeg on the PATH to read the microphone or a file")
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	isStdinTerminal := term.IsTerminal(int(os.Stdin.Fd()))
	ffmpeg := exec.CommandContext(ctx, "ffmpeg", recordingFfmpegArguments(command.String("input"), command.String("device"), isStdinTerminal, runtime.GOOS)...)
	if command.String("input") == "-" || (command.String("input") == "" && !isStdinTerminal) {
		ffmpeg.Stdin = os.Stdin
	}
	ffmpeg.Stderr = os.Stderr
	pcm, err := ffmpeg.StdoutPipe()
	if err != nil {
		return err
	}
	if err := ffmpeg.Start(); err != nil {
		return err
	}
	isJSON := command.Bool("json")
	say := func(format string, values ...any) {
		if !isJSON {
			fmt.Fprintf(os.Stderr, format+"\n", values...)
		}
	}
	say("Recording. Press Ctrl-C to stop.")
	recording, captureError := connection.CaptureRecording(ctx, pcm, client.RecordingCaptureOptions{
		IsLiveTranscriptionWanted: !command.Bool("no-live"),
		OnUtterance: func(transcriptText string, utteranceStartMS, _ int64) {
			say("[%s] %s", recordingClock(utteranceStartMS), forTerminal(strings.TrimSpace(transcriptText)))
		},
		OnPieceUploaded: func(pieceIndex int, timelineStartMS, durationMS int64) {
			say("Uploaded piece %d (%s to %s).", pieceIndex+1, recordingClock(timelineStartMS), recordingClock(timelineStartMS+durationMS))
		},
		OnProblem: func(problemText string) { say("%s", problemText) },
	})
	_, _ = io.Copy(io.Discard, pcm)
	_ = ffmpeg.Wait()
	if recording == nil {
		return describeError(command, captureError)
	}
	if isJSON {
		if err := PrintJSON(recording); err != nil {
			return err
		}
	} else {
		say("Stopped. Recording %s is %s long; it is named once transcribed.", recording.ID, recordingClock(recording.TimelineDurationMS))
	}
	return describeError(command, captureError)
}

// recordingClock is a timeline position as minutes and seconds.
func recordingClock(timelineMS int64) string {
	seconds := max(timelineMS, 0) / 1000
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

// recordingWatchInterval is how often watch reads the transcript again.
const recordingWatchInterval = 3 * time.Second

func runRecordingWatch(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("give the recording ID")
	}
	recordingId := command.Args().First()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	printedSegments := map[string]bool{}
	printedUtterances := map[string]bool{}
	for {
		var timeline struct {
			Segments []struct {
				ID                      string `json:"id"`
				SegmentStartMS          int64  `json:"segmentStartMS"`
				EffectiveSpeakerName    string `json:"effectiveSpeakerName"`
				TemporarySpeakerLabel   string `json:"temporarySpeakerLabel"`
				TranscriptText          string `json:"transcriptText"`
				EffectiveTranscriptText string `json:"effectiveTranscriptText"`
			} `json:"segments"`
			LiveUtterances []struct {
				UtteranceID      string `json:"utteranceId"`
				UtteranceStartMS int64  `json:"utteranceStartMS"`
				TranscriptText   string `json:"transcriptText"`
			} `json:"liveUtterances"`
			HasMoreSegments bool `json:"hasMoreSegments"`
		}
		for segmentOffset := 0; ; segmentOffset += 100 {
			if err := recordingCall(ctx, command, "ReadRecordingTranscriptTimeline", map[string]any{"recordingId": recordingId, "segmentOffset": segmentOffset, "pageSize": 100}, &timeline); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
			for _, segment := range timeline.Segments {
				if printedSegments[segment.ID] {
					continue
				}
				printedSegments[segment.ID] = true
				speaker := firstNonEmpty(segment.EffectiveSpeakerName, segment.TemporarySpeakerLabel)
				fmt.Printf("[%s] %s: %s\n", recordingClock(segment.SegmentStartMS), forTerminal(speaker), forTerminal(strings.TrimSpace(firstNonEmpty(segment.EffectiveTranscriptText, segment.TranscriptText))))
			}
			if segmentOffset == 0 {
				for _, utterance := range timeline.LiveUtterances {
					if printedUtterances[utterance.UtteranceID] {
						continue
					}
					printedUtterances[utterance.UtteranceID] = true
					fmt.Printf("[%s] live: %s\n", recordingClock(utterance.UtteranceStartMS), forTerminal(strings.TrimSpace(utterance.TranscriptText)))
				}
			}
			if !timeline.HasMoreSegments {
				break
			}
		}
		var processing struct {
			Recording      *models.Recording `json:"recording"`
			TranscriptRuns []struct {
				RecordingProcessingState string `json:"recordingProcessingState"`
			} `json:"transcriptRuns"`
		}
		if err := recordingCall(ctx, command, "ReadRecordingProcessing", map[string]any{"recordingId": recordingId}, &processing); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		isDone := processing.Recording != nil && processing.Recording.RecordingCaptureState != models.RecordingCaptureActive &&
			processing.Recording.RecordingCaptureState != models.RecordingCapturePaused && processing.Recording.RecordingCaptureState != models.RecordingCaptureInterrupted
		for _, run := range processing.TranscriptRuns {
			switch run.RecordingProcessingState {
			case "queued", "preparing", "transcribing":
				isDone = false
			}
		}
		if isDone {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(recordingWatchInterval):
		}
	}
}
