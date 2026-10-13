package client

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// speechLikePCM is a tone at RecordingCaptureSampleRate for durationMS, silent
// over each given span, so a level pause falls where a sentence would end.
func speechLikePCM(durationMS int64, silentSpansMS [][2]int64) []byte {
	sampleCount := durationMS * RecordingCaptureSampleRate / 1000
	pcm := make([]byte, sampleCount*2)
	for sampleIndex := int64(0); sampleIndex < sampleCount; sampleIndex++ {
		atMS := sampleIndex * 1000 / RecordingCaptureSampleRate
		isSilent := false
		for _, span := range silentSpansMS {
			isSilent = isSilent || (atMS >= span[0] && atMS < span[1])
		}
		if !isSilent {
			// Syllables rise and fall four times a second, as speech does.
			syllable := 0.05 + 0.95*math.Abs(math.Sin(float64(sampleIndex)*math.Pi*4/RecordingCaptureSampleRate))
			sample := int16(6000 * syllable * math.Sin(float64(sampleIndex)*0.07))
			binary.LittleEndian.PutUint16(pcm[sampleIndex*2:], uint16(sample))
		}
	}
	return pcm
}

func TestCutPolicyWaitsThenCutsAtAPauseAnUtteranceEndOrTheLimit(test *testing.T) {
	policy := &RecordingCutPolicy{}
	policy.StartPiece(0)
	if policy.ObserveFrame(10_000, -60, false) || policy.ObserveUtteranceEnd(15_000, 16_000) {
		test.Fatal("a piece was cut before it was long enough")
	}
	for frameEndMS := int64(10_020); frameEndMS <= 24_000; frameEndMS += 20 {
		// Speech: loud and soft syllables, never a quarter second near the floor.
		levelDecibels := -15.0
		if frameEndMS%200 == 0 {
			levelDecibels = -45
		}
		if policy.ObserveFrame(frameEndMS, levelDecibels, false) {
			test.Fatal("speech was cut")
		}
	}
	isCut := false
	for frameEndMS := int64(24_020); frameEndMS <= 24_400 && !isCut; frameEndMS += 20 {
		isCut = policy.ObserveFrame(frameEndMS, -50, false)
	}
	if !isCut {
		test.Fatal("a quarter second of quiet after twenty seconds did not cut")
	}
	policy.StartPiece(30_000)
	if !policy.ObserveUtteranceEnd(51_000, 52_000) || policy.ObserveUtteranceEnd(53_000, 52_000) {
		test.Fatal("an utterance end was not used, or one past the audio held was")
	}
	if policy.ObserveFrame(60_000, -60, true) || !policy.ObserveFrame(90_000, -20, true) {
		test.Fatal("the live stream did not stop level cuts, or the limit did not cut")
	}
}

func TestCaptureRecordingUploadsPiecesCutAtPausesThenStops(test *testing.T) {
	var mutex sync.Mutex
	recording := &models.Recording{ID: "fixture-recording", RecordingOrigin: models.RecordingOriginCapture, RecordingCaptureState: models.RecordingCaptureActive, SourceGeneration: 1}
	mediaById := map[string]*models.RecordingMedia{}
	var pieces []*models.RecordingMedia
	var stopVariables map[string]any
	transport := recordingSettingsTransport(func(request *http.Request) (*http.Response, error) {
		mutex.Lock()
		defer mutex.Unlock()
		var answered any
		switch {
		case request.URL.Path == "/api/v1/graphql":
			var envelope struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
				test.Fatal(err)
			}
			switch {
			case strings.Contains(envelope.Query, "CreateRecordingMedia("):
				sizeBytes := int64(envelope.Variables["originalSizeBytes"].(float64))
				media := &models.RecordingMedia{
					ID: fmt.Sprintf("media-%d", len(pieces)), RecordingID: recording.ID, ClientMediaID: envelope.Variables["clientMediaId"].(string),
					OriginalFilename: envelope.Variables["originalFilename"].(string), OriginalContentType: envelope.Variables["originalContentType"].(string),
					OriginalSizeBytes: sizeBytes, OriginalSHA256: envelope.Variables["originalSha256"].(string),
					TimelineStartMS: int64(envelope.Variables["timelineStartMS"].(float64)), MediaDurationMS: int64(envelope.Variables["mediaDurationMS"].(float64)),
					PartCount: int((sizeBytes + models.RecordingUploadPartBytes - 1) / models.RecordingUploadPartBytes),
				}
				mediaById[media.ID] = media
				pieces = append(pieces, media)
				answered = map[string]any{"CreateRecordingMedia": media}
			case strings.Contains(envelope.Query, "CreateRecording("):
				if envelope.Variables["recordingTitle"] != "" || envelope.Variables["recordingOrigin"] != string(models.RecordingOriginCapture) {
					test.Fatal("a capture was created with a title or as an import", envelope.Variables)
				}
				recording.ClientRecordingID = envelope.Variables["clientRecordingId"].(string)
				answered = map[string]any{"CreateRecording": recording}
			case strings.Contains(envelope.Query, "UpdateRecordingCapture("):
				stopVariables = envelope.Variables
				stopped := *recording
				stopped.RecordingCaptureState = models.RecordingCaptureStopped
				stopped.TimelineDurationMS = int64(envelope.Variables["timelineDurationMS"].(float64))
				answered = map[string]any{"UpdateRecordingCapture": &stopped}
			case strings.Contains(envelope.Query, "ReadRecording("):
				answered = map[string]any{"ReadRecording": RecordingView{Recording: recording}}
			default:
				test.Fatalf("unexpected query %s", envelope.Query)
			}
			answered = map[string]any{"data": answered}
		case strings.Contains(request.URL.Path, "/parts/"):
			content, err := io.ReadAll(request.Body)
			if err != nil {
				test.Fatal(err)
			}
			pathParts := strings.Split(request.URL.Path, "/")
			partIndex := 0
			_, _ = fmt.Sscan(pathParts[len(pathParts)-1], &partIndex)
			answered = &models.RecordingMediaPart{RecordingMediaID: pathParts[len(pathParts)-3], PartIndex: partIndex, PartSizeBytes: int64(len(content)), PartSHA256: request.Header.Get("X-Recording-Part-SHA256"), StoredAt: time.Now()}
		case strings.HasSuffix(request.URL.Path, "/complete"):
			mediaId := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/api/v1/recordings/media/"), "/complete")
			media := mediaById[mediaId]
			verifiedAt := time.Now()
			media.VerifiedAt, media.RecordingUploadState = &verifiedAt, models.RecordingUploadComplete
			answered = media
		default:
			test.Fatalf("unexpected request %s", request.URL.Path)
		}
		content, err := json.Marshal(answered)
		if err != nil {
			test.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(content))}, nil
	})
	connection, err := New(Options{URL: "https://example.com", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		test.Fatal(err)
	}
	pcm := speechLikePCM(120_000, [][2]int64{{30_000, 30_600}, {80_000, 80_600}})
	var uploaded []int
	stopped, err := connection.CaptureRecording(test.Context(), bytes.NewReader(pcm), RecordingCaptureOptions{
		OnPieceUploaded: func(pieceIndex int, _, _ int64) { uploaded = append(uploaded, pieceIndex) },
	})
	if err != nil {
		test.Fatal(err)
	}
	if len(pieces) != 3 || len(uploaded) != 3 {
		test.Fatalf("expected three pieces, got %d uploaded %v", len(pieces), uploaded)
	}
	var endMS int64
	for index, piece := range pieces {
		if piece.TimelineStartMS != endMS || piece.OriginalContentType != "audio/wav" || piece.VerifiedAt == nil {
			test.Fatalf("piece %d is not placed after the one before it or not verified: %+v", index, piece)
		}
		endMS = piece.TimelineStartMS + piece.MediaDurationMS
	}
	if pieces[1].TimelineStartMS < 30_000 || pieces[1].TimelineStartMS > 30_600 || pieces[2].TimelineStartMS < 80_000 || pieces[2].TimelineStartMS > 80_600 {
		test.Fatalf("pieces were not cut at the pauses: %d %d", pieces[1].TimelineStartMS, pieces[2].TimelineStartMS)
	}
	if endMS != 120_000 || stopped.RecordingCaptureState != models.RecordingCaptureStopped || stopVariables["recordingCaptureState"] != string(models.RecordingCaptureStopped) {
		test.Fatalf("the capture was not stopped at its full length: %d %v", endMS, stopVariables)
	}
}
