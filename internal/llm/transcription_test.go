package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

func transcriptionFixture() *TranscriptionRequest {
	return &TranscriptionRequest{Model: "gpt-4o-transcribe-diarize", MediaFilename: "meeting.wav", MediaContentType: "audio/wav", MediaBytes: []byte("original fixture bytes"), MediaDurationMS: 5000, IsSpeakerDiarizationRequired: true}
}

func TestTranscriptionUsesRegistryAndPreservesAudioSegmentsAndUsage(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		if incoming.URL.Path != "/v1/audio/transcriptions" || incoming.Header.Get("Authorization") != "Bearer test-key" {
			test.Error("request bypassed the configured provider")
		}
		if err := incoming.ParseMultipartForm(1 << 20); err != nil {
			test.Error(err)
			return
		}
		defer func() { _ = incoming.MultipartForm.RemoveAll() }()
		if incoming.FormValue("response_format") != "diarized_json" || incoming.FormValue("chunking_strategy") != "auto" {
			test.Error("diarization contract was not requested")
		}
		if incoming.FormValue("known_speaker_names[]") != "confirmed-speaker" || incoming.FormValue("known_speaker_references[]") != "data:audio/wav;base64,Y2xpcA==" {
			test.Error("voice reference was changed")
		}
		media, _, err := incoming.FormFile("file")
		if err != nil {
			test.Error(err)
			return
		}
		defer func() { _ = media.Close() }()
		mediaBytes, err := io.ReadAll(media)
		if err != nil {
			test.Error(err)
			return
		}
		if string(mediaBytes) != "original fixture bytes" {
			test.Error("source audio changed")
		}
		_, _ = io.WriteString(writer, `{"duration":5,"text":"An idea.","segments":[{"id":"provider-segment","speaker":"confirmed-speaker","start":0.125,"end":2.75,"text":"An idea."}],"usage":{"type":"tokens","input_tokens":14,"input_token_details":{"audio_tokens":12,"text_tokens":2},"output_tokens":4,"total_tokens":18}}`)
	}))
	defer server.Close()
	configuration := config.Default().Agent
	configuration.Enabled = true
	configuration.Providers = []config.AgentProvider{{Name: "transcripts", Kind: "openai", BaseURL: server.URL + "/v1", APIKey: "test-key"}}
	registry, err := Open(&configuration)
	if err != nil {
		test.Fatal(err)
	}
	selected, err := registry.TranscribingFor("transcripts:gpt-4o-transcribe-diarize", true, true)
	if err != nil {
		test.Fatal(err)
	}
	if selected.Name != "transcripts:gpt-4o-transcribe-diarize" || selected.Capabilities.MaximumSpeakerReferenceCount != 4 {
		test.Fatal("selection lost the configured model identity")
	}
	request := transcriptionFixture()
	request.SpeakerReferences = []SpeakerReference{{SpeakerName: "confirmed-speaker", MediaContentType: "audio/wav", MediaBytes: []byte("clip"), DurationMS: 3000}}
	answer, err := selected.Transcriber.Transcribe(test.Context(), request)
	if err != nil {
		test.Fatal(err)
	}
	if len(answer.Segments) != 1 || answer.Segments[0].SpeakerID != "confirmed-speaker" || answer.Segments[0].StartMS != 125 || answer.Segments[0].EndMS != 2750 {
		test.Fatalf("segments: %+v", answer.Segments)
	}
	if answer.TranscriptionUsage.Usage.PromptTokens != 2 || answer.TranscriptionUsage.Usage.AudioInputTokens != 12 || answer.TranscriptionUsage.InputAudioTokens != 12 || answer.TranscriptionUsage.InputTextTokens != 2 || !answer.TranscriptionUsage.HasProviderUsage {
		test.Fatal("reported usage was lost")
	}
	if !strings.Contains(string(answer.ProviderResponseJSON), "provider-segment") || answer.MediaDurationMS == nil || *answer.MediaDurationMS != 5000 {
		test.Fatal("provider provenance was lost")
	}
}

func TestTranscriptionNeverDowngradesRequiredCapabilities(test *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { callCount.Add(1) }))
	defer server.Close()
	provider := newOpenAI(server.URL, "", server.Client())
	for _, model := range []string{"gpt-transcribe", "whisper-1", "unknown-model"} {
		request := transcriptionFixture()
		request.Model = model
		if _, err := provider.Transcribe(test.Context(), request); err == nil {
			test.Fatalf("%s accepted speaker attribution", model)
		}
	}
	request := transcriptionFixture()
	request.SpeakerReferences = []SpeakerReference{{SpeakerName: "speaker", MediaContentType: "audio/wav", MediaBytes: []byte("clip"), DurationMS: 1000}}
	if _, err := provider.Transcribe(test.Context(), request); err == nil {
		test.Fatal("invalid reference was sent")
	}
	if callCount.Load() != 0 {
		test.Fatal("a refused request still called the provider")
	}
	configuration := config.Default().Agent
	configuration.Enabled = true
	configuration.Providers = []config.AgentProvider{{Name: "transcripts", Kind: "openai", BaseURL: server.URL, Models: config.AgentProviderModels{Deny: []string{"gpt-4o-transcribe-diarize"}}}}
	registry, err := Open(&configuration)
	if err != nil {
		test.Fatal(err)
	}
	if _, err := registry.TranscribingFor("transcripts:gpt-4o-transcribe-diarize", true, false); err == nil {
		test.Fatal("model admission was bypassed")
	}
	if _, err := registry.TranscribingFor("transcripts:gpt-transcribe", true, false); err == nil {
		test.Fatal("registry silently dropped speaker attribution")
	}
	if _, err := registry.TranscribingFor("transcripts:gpt-transcribe", false, true); err == nil {
		test.Fatal("registry silently dropped references")
	}
}

// Audio and text input are billed at different prices, so the client
// keeps them apart: PromptTokens holds only the text.
func TestTranscriptionSplitsAudioInputFromTextInput(test *testing.T) {
	request := transcriptionFixture()
	capabilities := newOpenAI("", "", nil).TranscriptionCapabilities(request.Model)
	transcript := `{"text":"words","segments":[{"speaker":"A","start":0,"end":1,"text":"words"}],"usage":`
	answer, err := decodeTranscription(request, []byte(transcript+`{"type":"tokens","input_tokens":903,"output_tokens":922,"input_token_details":{"text_tokens":114,"audio_tokens":789}}}`), capabilities)
	if err != nil {
		test.Fatal(err)
	}
	usage := answer.TranscriptionUsage
	if usage.Usage.PromptTokens != 114 || usage.Usage.AudioInputTokens != 789 || usage.Usage.CompletionTokens != 922 || usage.InputAudioTokens != 789 || usage.InputTextTokens != 114 {
		test.Fatalf("audio and text input were not split: %+v", usage)
	}
	// No breakdown: all of the input is text, as before.
	answer, err = decodeTranscription(request, []byte(transcript+`{"type":"tokens","input_tokens":50,"output_tokens":7}}`), capabilities)
	if err != nil {
		test.Fatal(err)
	}
	if usage := answer.TranscriptionUsage.Usage; usage.PromptTokens != 50 || usage.AudioInputTokens != 0 {
		test.Fatalf("input with no breakdown must stay text: %+v", usage)
	}
	// A breakdown larger than the input is capped rather than going negative.
	answer, err = decodeTranscription(request, []byte(transcript+`{"type":"tokens","input_tokens":10,"output_tokens":1,"input_token_details":{"audio_tokens":30}}}`), capabilities)
	if err != nil {
		test.Fatal(err)
	}
	if usage := answer.TranscriptionUsage.Usage; usage.PromptTokens != 0 || usage.AudioInputTokens != 10 {
		test.Fatalf("audio must be capped at the input: %+v", usage)
	}
}

func TestTranscriptionRejectsIncompleteSpeakerSegmentsAndRetainsUnknownUsage(test *testing.T) {
	request := transcriptionFixture()
	capabilities := newOpenAI("", "", nil).TranscriptionCapabilities(request.Model)
	for _, responseText := range []string{
		`{}`,
		`{"text":"","segments":null}`,
		`{"text":"words"}`,
		`{"text":"words","segments":[{"start":0,"end":1,"text":"words"}]}`,
		`{"text":"words","segments":[{"speaker":"A","end":1,"text":"words"}]}`,
		`{"text":"words","segments":[{"speaker":"A","start":-1,"end":1,"text":"words"}]}`,
		`{"text":"words","segments":[{"speaker":"A","start":1,"end":0,"text":"words"}]}`,
		`{"text":"words","segments":[{"speaker":"A","start":0,"end":100,"text":"words"}]}`,
	} {
		if _, err := decodeTranscription(request, []byte(responseText), capabilities); err == nil {
			test.Fatalf("accepted malformed transcript: %s", responseText)
		}
	}
	answer, err := decodeTranscription(request, []byte(`{"text":"","segments":[]}`), capabilities)
	if err != nil {
		test.Fatal(err)
	}
	if answer.TranscriptionUsage.HasProviderUsage {
		test.Fatal("missing usage was recorded as actual zero cost")
	}
	for _, usageJSON := range []string{`{}`, `{"type":"future-unit","units":7}`} {
		answer, err = decodeTranscription(request, []byte(`{"text":"","segments":[],"usage":`+usageJSON+`}`), capabilities)
		if err != nil {
			test.Fatal(err)
		}
		if answer.TranscriptionUsage.HasProviderUsage || string(answer.TranscriptionUsage.ProviderUsageJSON) != usageJSON {
			test.Fatal("unknown billing was treated as known or its evidence was discarded")
		}
	}
	for _, usageJSON := range []string{`{"type":"tokens","input_tokens":0}`, `{"type":"duration"}`} {
		if _, err := decodeTranscription(request, []byte(`{"text":"","segments":[],"usage":`+usageJSON+`}`), capabilities); err == nil {
			test.Fatal("accepted incomplete billing metrics")
		}
	}
	answer, err = decodeTranscription(request, []byte(`{"text":"","segments":[],"usage":{"type":"duration","seconds":4.25}}`), capabilities)
	if err != nil {
		test.Fatal(err)
	}
	if answer.TranscriptionUsage.AudioDurationMS != 4250 || len(answer.TranscriptionUsage.ProviderUsageJSON) == 0 {
		test.Fatal("duration billing metrics were lost")
	}
}

func TestTranscriptionHonorsCancellationAndProviderErrors(test *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		if incoming.Header.Get("Authorization") == "Bearer refused" {
			writer.WriteHeader(429)
			_, _ = io.WriteString(writer, `{"error":{"message":"limited"}}`)
			return
		}
		close(started)
		select {
		case <-incoming.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	provider := newOpenAI(server.URL, "refused", server.Client())
	_, err := provider.Transcribe(test.Context(), transcriptionFixture())
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Status != 429 {
		test.Fatalf("provider error: %v", err)
	}
	provider = newOpenAI(server.URL, "", server.Client())
	ctx, cancel := context.WithCancel(test.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := provider.Transcribe(ctx, transcriptionFixture()); finished <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		test.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			test.Fatalf("cancellation: %v", err)
		}
	case <-time.After(time.Second):
		test.Fatal("cancelled request kept running")
	}
}

// A last segment that ends a little past the audio it was sent is cut to the
// audio's end, rather than refused: refusing it threw away an answer already
// paid for, over a rounding at its last word.
func TestASegmentJustPastTheAudioIsCutToItsEnd(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		_, _ = io.WriteString(writer, `{"duration":5,"text":"Done.","segments":[{"id":"last","speaker":"A","start":4.5,"end":5.4,"text":"Done."}]}`)
	}))
	defer server.Close()
	configuration := config.Default().Agent
	configuration.Enabled = true
	configuration.Providers = []config.AgentProvider{{Name: "transcripts", Kind: "openai", BaseURL: server.URL + "/v1", APIKey: "test-key"}}
	registry, err := Open(&configuration)
	if err != nil {
		test.Fatal(err)
	}
	selected, err := registry.TranscribingFor("transcripts:gpt-4o-transcribe-diarize", true, false)
	if err != nil {
		test.Fatal(err)
	}
	answer, err := selected.Transcriber.Transcribe(test.Context(), transcriptionFixture())
	if err != nil {
		test.Fatalf("a segment 400 ms past the audio was refused: %s", err)
	}
	if len(answer.Segments) != 1 || answer.Segments[0].StartMS != 4500 || answer.Segments[0].EndMS != 5000 {
		test.Fatalf("segments: %+v", answer.Segments)
	}
}

// A vocabulary prompt goes to a plain transcription model as the prompt field
// and is left out for the diarizing model, which does not accept one.
func TestTranscriptionSendsTheVocabularyPromptOnlyToModelsThatAcceptIt(test *testing.T) {
	for _, scenario := range []struct {
		modelName        string
		isDiarizing      bool
		expectedPrompt   string
		expectedChunking string
	}{
		{modelName: "whisper-1", isDiarizing: false, expectedPrompt: "Names and terms: Ferncroft, Plover.", expectedChunking: ""},
		{modelName: "gpt-4o-transcribe", isDiarizing: false, expectedPrompt: "Names and terms: Ferncroft, Plover.", expectedChunking: "auto"},
		{modelName: "gpt-4o-transcribe-diarize", isDiarizing: true, expectedPrompt: "", expectedChunking: "auto"},
	} {
		test.Run(scenario.modelName, func(test *testing.T) {
			var hasPrompt bool
			var promptValue, chunkingValue string
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
				if err := incoming.ParseMultipartForm(1 << 20); err != nil {
					test.Error(err)
					return
				}
				defer func() { _ = incoming.MultipartForm.RemoveAll() }()
				_, hasPrompt = incoming.MultipartForm.Value["prompt"]
				promptValue = incoming.FormValue("prompt")
				chunkingValue = incoming.FormValue("chunking_strategy")
				_, _ = io.WriteString(writer, `{"duration":5,"text":"An idea.","segments":[{"id":"s1","speaker":"speaker-one","start":0,"end":2,"text":"An idea."}]}`)
			}))
			defer server.Close()
			configuration := config.Default().Agent
			configuration.Enabled = true
			configuration.Providers = []config.AgentProvider{{Name: "transcripts", Kind: "openai", BaseURL: server.URL + "/v1", APIKey: "test-key"}}
			registry, err := Open(&configuration)
			if err != nil {
				test.Fatal(err)
			}
			selected, err := registry.TranscribingFor("transcripts:"+scenario.modelName, scenario.isDiarizing, false)
			if err != nil {
				test.Fatal(err)
			}
			request := transcriptionFixture()
			request.Model = scenario.modelName
			request.IsSpeakerDiarizationRequired = scenario.isDiarizing
			request.VocabularyPrompt = "Names and terms: Ferncroft, Plover."
			if _, err := selected.Transcriber.Transcribe(test.Context(), request); err != nil {
				test.Fatal(err)
			}
			if hasPrompt != (scenario.expectedPrompt != "") || promptValue != scenario.expectedPrompt {
				test.Fatalf("prompt field for %s: present=%v value=%q", scenario.modelName, hasPrompt, promptValue)
			}
			// A model that may stop at a pause is asked to cut the audio at its pauses instead.
			if chunkingValue != scenario.expectedChunking {
				test.Fatalf("chunking_strategy for %s: %q", scenario.modelName, chunkingValue)
			}
		})
	}
}
