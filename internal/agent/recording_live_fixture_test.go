package agent

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

// fakeTranscriptionSegment is one passage the fake provider answers with, in
// milliseconds from the start of the audio it was sent.
type fakeTranscriptionSegment struct {
	SpeakerLabel string
	StartMS      int64
	EndMS        int64
	Text         string
}

// fakeTranscriptionRequest is what the fake provider received for one piece.
type fakeTranscriptionRequest struct {
	AudioDurationMS    int64
	KnownSpeakerNames  []string
	KnownSpeakerSample map[string][]byte
}

// fakeTranscriptionProvider speaks the file-transcription protocol of the
// speaker-separating model, so the whole job runs against the real client.
type fakeTranscriptionProvider struct {
	server   *httptest.Server
	lock     sync.Mutex
	requests []fakeTranscriptionRequest
	// answer decides the passages for the n-th request (counting from zero).
	answer func(requestIndex int, request fakeTranscriptionRequest) []fakeTranscriptionSegment
}

func newFakeTranscriptionProvider(test *testing.T) *fakeTranscriptionProvider {
	test.Helper()
	provider := &fakeTranscriptionProvider{}
	provider.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseMultipartForm(64 << 20); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		file, _, err := request.FormFile("file")
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		defer func() { _ = file.Close() }()
		waveSizeBytes, _ := file.Seek(0, 2)
		received := fakeTranscriptionRequest{AudioDurationMS: (waveSizeBytes - 44) / 32, KnownSpeakerNames: request.MultipartForm.Value["known_speaker_names[]"], KnownSpeakerSample: map[string][]byte{}}
		provider.lock.Lock()
		requestIndex := len(provider.requests)
		provider.requests = append(provider.requests, received)
		answer := provider.answer
		provider.lock.Unlock()
		var segments []fakeTranscriptionSegment
		if answer != nil {
			segments = answer(requestIndex, received)
		}
		type wireSegment struct {
			ID      string  `json:"id"`
			Speaker string  `json:"speaker"`
			Start   float64 `json:"start"`
			End     float64 `json:"end"`
			Text    string  `json:"text"`
		}
		wireSegments := []wireSegment{}
		text := ""
		for index, segment := range segments {
			wireSegments = append(wireSegments, wireSegment{ID: "segment-" + string(rune('a'+index)), Speaker: segment.SpeakerLabel, Start: float64(segment.StartMS) / 1000, End: float64(segment.EndMS) / 1000, Text: segment.Text})
			text += segment.Text + " "
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"text": text, "segments": wireSegments, "usage": map[string]any{"type": "duration", "seconds": float64(received.AudioDurationMS) / 1000}})
	}))
	test.Cleanup(provider.server.Close)
	return provider
}

func (self *fakeTranscriptionProvider) requestAt(index int) fakeTranscriptionRequest {
	self.lock.Lock()
	defer self.lock.Unlock()
	return self.requests[index]
}

func (self *fakeTranscriptionProvider) requestCount() int {
	self.lock.Lock()
	defer self.lock.Unlock()
	return len(self.requests)
}

// liveRecordingFixture is a capture whose pieces are added and transcribed one
// at a time, the way a phone uploads them while recording.
type liveRecordingFixture struct {
	test          *testing.T
	database      db.Database
	worker        *Agent
	files         storage.StreamFiles
	provider      *fakeTranscriptionProvider
	userId        string
	owner         *models.User
	assistant     *models.Agent
	source        *models.Recording
	nextStartMS   int64
	pieceCount    int
	configuration *config.Configuration
}

func newLiveRecordingFixture(test *testing.T) *liveRecordingFixture {
	test.Helper()
	if _, err := recording.NewAudioDecoder(test.Context()); err != nil {
		if os.Getenv("TEANODE_RECORDING_DECODER_REQUIRED") == "1" {
			test.Fatal(err)
		}
		test.Skip("descriptor-capable local media readers are unavailable")
	}
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	store, err := storage.Open(&storage.Settings{Directory: test.TempDir()})
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = store.Close() })
	provider := newFakeTranscriptionProvider(test)
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Models.Transcription = "fixture:gpt-4o-transcribe-diarize"
	configuration.Agent.Models.Default = "fixture:summary-model"
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fixture", Kind: "openai", BaseURL: provider.server.URL, APIKey: "fixture-key"}}
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		test.Fatal(err)
	}
	fixture := &liveRecordingFixture{test: test, database: database, files: store.(storage.StreamFiles), provider: provider, userId: dbtest.CreateUser(test, database, "live-owner"), configuration: configuration}
	fixture.worker = New(&Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }})
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		role, err := transaction.CreateRole(&models.Role{Name: "Live recording", Permissions: []models.Permission{models.PermissionAgentUse}})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateGroup(&models.Group{Name: "Live recorders", RoleIDs: []string{role.ID}, UserIDs: []string{fixture.userId}}); err != nil {
			test.Fatal(err)
		}
		fixture.owner, err = transaction.GetUser(fixture.userId)
		if err != nil {
			test.Fatal(err)
		}
		fixture.assistant, err = transaction.CreateAgent(&models.Agent{UserID: fixture.userId, Name: "Live assistant", Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
		fixture.source, err = transaction.CreateRecording(&models.Recording{UserID: fixture.userId, ClientRecordingID: "live-capture", RecordingOrigin: models.RecordingOriginCapture})
		if err != nil {
			test.Fatal(err)
		}
	})
	return fixture
}

// liveSpeechWave is mono 16 kHz speech-like audio: a tone with a short silence
// every second, which passes the screening a voice sample goes through.
func liveSpeechWave(durationMS int64) []byte {
	samples := make([]byte, durationMS*32)
	for sampleIndex := 0; sampleIndex < len(samples)/2; sampleIndex++ {
		if sampleIndex%16000 > 2000 {
			binary.LittleEndian.PutUint16(samples[sampleIndex*2:], uint16(int16(4000*math.Sin(float64(sampleIndex)*0.1))))
		}
	}
	return recordingReferenceWave(samples)
}

// addPiece uploads the next piece of the capture, starting gapMS after the
// previous one ended, and returns its media.
func (self *liveRecordingFixture) addPiece(durationMS, gapMS int64) *models.RecordingMedia {
	self.test.Helper()
	self.nextStartMS += gapMS
	self.pieceCount++
	waveBytes := liveSpeechWave(durationMS)
	digest := sha256.Sum256(waveBytes)
	digestText := hex.EncodeToString(digest[:])
	var media *models.RecordingMedia
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		media, err = transaction.CreateRecordingMedia(self.userId, &models.RecordingMedia{RecordingID: self.source.ID, ClientMediaID: "piece-" + string(rune('a'+self.pieceCount)), OriginalFilename: "piece.wav", OriginalContentType: "audio/wav", OriginalSizeBytes: int64(len(waveBytes)), OriginalSHA256: digestText, TimelineStartMS: self.nextStartMS, MediaDurationMS: durationMS}, 16<<20, 64<<20)
		if err != nil {
			self.test.Fatal(err)
		}
	})
	mediaStore := recording.New(self.database, self.files)
	if _, err := mediaStore.PutPart(self.test.Context(), self.userId, media.ID, 0, digestText, waveBytes); err != nil {
		self.test.Fatal(err)
	}
	if _, _, err := mediaStore.Complete(self.test.Context(), self.userId, media.ID); err != nil {
		self.test.Fatal(err)
	}
	self.nextStartMS += durationMS
	return media
}

// transcribe queues a piece the way the sweep does and runs its job to the end.
func (self *liveRecordingFixture) transcribe(media *models.RecordingMedia) *models.RecordingTranscriptRun {
	self.test.Helper()
	var transcriptRun *models.RecordingTranscriptRun
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		transcriptRun, err = self.worker.QueueRecordingTranscription(transaction, self.userId, media.ID, "initial", false)
		if err != nil {
			self.test.Fatal(err)
		}
	})
	var job *models.AgentJob
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		jobs, err := transaction.ClaimAgentJobs("live-worker", 10, time.Now())
		if err != nil {
			self.test.Fatal(err)
		}
		for _, claimed := range jobs {
			if claimed.Kind == models.AgentJobRecording && claimed.SubjectID == transcriptRun.ID {
				job = claimed
			}
		}
		if job == nil {
			self.test.Fatal("the piece's job was not claimed")
		}
	})
	run := &Run{Job: job, Agent: self.assistant, Owner: self.owner, settings: self.worker.settings}
	if err := self.worker.runRecording(self.test.Context(), run); err != nil {
		self.test.Fatal(err)
	}
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		transcriptRun, err = transaction.GetRecordingTranscriptRun(self.userId, transcriptRun.ID)
		if err != nil || transcriptRun.RecordingProcessingState != models.RecordingProcessingComplete {
			self.test.Fatal("the piece did not complete", err, transcriptRun)
		}
	})
	return transcriptRun
}

// timeline reads what a client would draw: segments in order with corrections
// and speakers projected.
func (self *liveRecordingFixture) timeline() []*models.RecordingTranscriptSegment {
	self.test.Helper()
	var segments []*models.RecordingTranscriptSegment
	dbtest.RunTransactionOn(self.test, self.database, func(transaction db.Transaction) {
		var err error
		segments, _, _, err = transaction.ReadRecordingTranscriptTimelinePage(self.userId, self.source.ID, 0, 250)
		if err != nil {
			self.test.Fatal(err)
		}
		if err := transaction.ProjectRecordingTranscriptTextCorrections(self.userId, segments); err != nil {
			self.test.Fatal(err)
		}
		if err := transaction.ProjectRecordingSpeakerCorrections(self.userId, segments); err != nil {
			self.test.Fatal(err)
		}
	})
	return segments
}
