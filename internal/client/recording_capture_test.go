package client

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

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

// pieceUploadClient is a client whose server accepts every piece, except that
// the first failedRequestCount requests of any kind fail as a dropped
// connection does.
func pieceUploadClient(test *testing.T, failedRequestCount int, requestCount *int) *Client {
	test.Helper()
	var mutex sync.Mutex
	mediaById := map[string]*models.RecordingMedia{}
	transport := recordingSettingsTransport(func(request *http.Request) (*http.Response, error) {
		mutex.Lock()
		defer mutex.Unlock()
		*requestCount++
		if *requestCount <= failedRequestCount {
			return nil, fmt.Errorf("connection reset")
		}
		var answered any
		switch {
		case request.URL.Path == "/api/v1/graphql":
			var envelope struct {
				Variables map[string]any `json:"variables"`
			}
			if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
				test.Fatal(err)
			}
			sizeBytes := int64(envelope.Variables["originalSizeBytes"].(float64))
			media := &models.RecordingMedia{
				ID: "media-fixture", RecordingID: envelope.Variables["recordingId"].(string), ClientMediaID: envelope.Variables["clientMediaId"].(string),
				OriginalFilename: envelope.Variables["originalFilename"].(string), OriginalContentType: "audio/wav", OriginalSizeBytes: sizeBytes,
				OriginalSHA256: envelope.Variables["originalSha256"].(string), PartCount: int((sizeBytes + models.RecordingUploadPartBytes - 1) / models.RecordingUploadPartBytes),
			}
			mediaById[media.ID] = media
			answered = map[string]any{"data": map[string]any{"CreateRecordingMedia": media}}
		case strings.Contains(request.URL.Path, "/parts/"):
			content, err := io.ReadAll(request.Body)
			if err != nil {
				test.Fatal(err)
			}
			answered = &models.RecordingMediaPart{RecordingMediaID: "media-fixture", PartSizeBytes: int64(len(content)), PartSHA256: request.Header.Get("X-Recording-Part-SHA256"), StoredAt: time.Now()}
		case strings.HasSuffix(request.URL.Path, "/complete"):
			verifiedAt := time.Now()
			media := mediaById["media-fixture"]
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
	return connection
}

func shortenPieceRetries(test *testing.T, window time.Duration) {
	test.Helper()
	previousWindow, previousFirstDelay, previousLongest := recordingPieceRetryWindow, recordingPieceRetryFirstDelay, recordingPieceRetryLongest
	recordingPieceRetryWindow, recordingPieceRetryFirstDelay, recordingPieceRetryLongest = window, time.Millisecond, 4*time.Millisecond
	test.Cleanup(func() {
		recordingPieceRetryWindow, recordingPieceRetryFirstDelay, recordingPieceRetryLongest = previousWindow, previousFirstDelay, previousLongest
	})
}

// A piece whose upload fails once is uploaded on the next try instead of being
// lost from the timeline.
func TestAPieceThatFailsToUploadOnceIsUploadedAgain(test *testing.T) {
	shortenPieceRetries(test, time.Minute)
	requestCount := 0
	connection := pieceUploadClient(test, 1, &requestCount)
	var uploaded []int
	uploads := newRecordingPieceUploads(connection, "fixture-recording", RecordingCaptureOptions{OnPieceUploaded: func(pieceIndex int, _, _ int64) { uploaded = append(uploaded, pieceIndex) }})
	uploads.add(0, 0, speechLikePCM(1000, nil))
	if err := uploads.wait(); err != nil || len(uploaded) != 1 || uploaded[0] != 0 {
		test.Fatal("the piece was not uploaded after the failure", uploaded, err)
	}
}

// A piece that cannot be uploaded in the time allowed is reported once the
// retries end, and the pieces after it are still tried.
func TestAPieceThatKeepsFailingIsGivenUpOnAndTheNextIsTried(test *testing.T) {
	shortenPieceRetries(test, 30*time.Millisecond)
	requestCount := 0
	connection := pieceUploadClient(test, 1<<30, &requestCount)
	var problems []string
	uploads := newRecordingPieceUploads(connection, "fixture-recording", RecordingCaptureOptions{OnProblem: func(problemText string) { problems = append(problems, problemText) }})
	uploads.add(0, 0, speechLikePCM(1000, nil))
	uploads.add(1, 1000, speechLikePCM(1000, nil))
	err := uploads.wait()
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		test.Fatal("a piece that never uploaded was not reported", err)
	}
	if requestCount < 4 {
		test.Fatalf("the piece was not retried: %d requests", requestCount)
	}
	if last := problems[len(problems)-1]; !strings.Contains(last, "piece 2 was not uploaded:") {
		test.Fatal("the second piece was not tried or reported", problems)
	}
}

// More audio than the limit waits for the uploads instead of piling up.
func TestAddingPiecesWaitsWhenTooMuchAudioIsQueued(test *testing.T) {
	uploads := &recordingPieceUploads{heldBytes: recordingPieceQueueLimitBytes - 10}
	uploads.changed = sync.NewCond(&uploads.mutex)
	isAdded := make(chan struct{})
	go func() {
		uploads.add(0, 0, make([]byte, 100))
		close(isAdded)
	}()
	select {
	case <-isAdded:
		test.Fatal("a piece was queued past the limit")
	case <-time.After(50 * time.Millisecond):
	}
	uploads.release(recordingPieceQueueLimitBytes - 10)
	select {
	case <-isAdded:
	case <-time.After(2 * time.Second):
		test.Fatal("the piece was not queued once room was made")
	}
}

// liveServer is a server that ends every session it is opened with, after
// readying it, with the given event, and tells which start each hello asked for.
func liveServer(test *testing.T, endingEvent string) (*httptest.Server, chan int64) {
	test.Helper()
	startsMS := make(chan int64, 8)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = connection.Close() }()
		var hello struct {
			StreamStartMS int64 `json:"streamStartMS"`
		}
		if _, message, err := connection.ReadMessage(); err != nil || json.Unmarshal(message, &hello) != nil {
			return
		}
		startsMS <- hello.StreamStartMS
		_ = connection.WriteJSON(map[string]any{"voiceEvent": "ready"})
		_ = connection.WriteJSON(map[string]any{"voiceEvent": endingEvent, "errorMessage": "the session reached its end; stream again"})
	}))
	test.Cleanup(server.Close)
	return server, startsMS
}

func waitForBrokenLiveStream(test *testing.T, live *recordingLiveStream) {
	test.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		live.stateMutex.Lock()
		isBroken := live.isBroken
		live.stateMutex.Unlock()
		if isBroken {
			return
		}
	}
	test.Fatal("the live stream did not end")
}

// When the server ends a session at its longest length, the capture opens a new
// one that starts at the point the timeline has reached.
func TestALiveStreamTheServerEndedIsOpenedAgainAtTheCurrentTimeline(test *testing.T) {
	previous := recordingLiveReopenAfter
	recordingLiveReopenAfter = 0
	test.Cleanup(func() { recordingLiveReopenAfter = previous })
	server, startsMS := liveServer(test, "error")
	connection, err := New(Options{URL: server.URL, Token: "fixture-token"})
	if err != nil {
		test.Fatal(err)
	}
	live, err := connection.openRecordingLiveStream(test.Context(), "fixture-recording", 0, RecordingCaptureOptions{})
	if err != nil {
		test.Fatal(err)
	}
	if start := <-startsMS; start != 0 {
		test.Fatal("the first session did not start at the beginning", start)
	}
	waitForBrokenLiveStream(test, live)
	reopened := connection.refreshRecordingLiveStream(test.Context(), live, "fixture-recording", 1_800_000, RecordingCaptureOptions{})
	if reopened == live {
		test.Fatal("a stream the server ended was not opened again")
	}
	defer reopened.close()
	select {
	case start := <-startsMS:
		if start != 1_800_000 {
			test.Fatal("the new session did not start at the current timeline", start)
		}
	case <-time.After(2 * time.Second):
		test.Fatal("the new session never said hello")
	}
}

// A server that refuses live words is not asked again.
func TestARefusedLiveStreamIsNotOpenedAgain(test *testing.T) {
	previous := recordingLiveReopenAfter
	recordingLiveReopenAfter = 0
	test.Cleanup(func() { recordingLiveReopenAfter = previous })
	server, startsMS := liveServer(test, "refused")
	connection, err := New(Options{URL: server.URL, Token: "fixture-token"})
	if err != nil {
		test.Fatal(err)
	}
	live, err := connection.openRecordingLiveStream(test.Context(), "fixture-recording", 0, RecordingCaptureOptions{})
	if err != nil {
		test.Fatal(err)
	}
	<-startsMS
	waitForBrokenLiveStream(test, live)
	if connection.refreshRecordingLiveStream(test.Context(), live, "fixture-recording", 60_000, RecordingCaptureOptions{}) != live {
		test.Fatal("a refused stream was opened again")
	}
	select {
	case <-startsMS:
		test.Fatal("the server was asked again after it refused")
	case <-time.After(100 * time.Millisecond):
	}
}
