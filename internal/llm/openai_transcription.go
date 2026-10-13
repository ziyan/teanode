package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"
)

const maximumTranscriptionAudioBytes = 25000000
const maximumTranscriptionResponseBytes = 32 << 20

// TranscriptionCapabilities advertises only models whose file-transcription contract is documented.
func (self *openAI) TranscriptionCapabilities(model string) TranscriptionCapabilities {
	capabilities := TranscriptionCapabilities{MaximumAudioBytes: maximumTranscriptionAudioBytes}
	switch model {
	case "gpt-4o-transcribe-diarize":
		capabilities.CanTranscribe = true
		capabilities.HasSpeakerDiarization = true
		capabilities.HasSegmentTimestamps = true
		capabilities.HasSpeakerReferences = true
		capabilities.MaximumSpeakerReferenceCount = 4
	case "whisper-1":
		capabilities.CanTranscribe = true
		capabilities.HasSegmentTimestamps = true
	case "gpt-transcribe", "gpt-4o-transcribe", "gpt-4o-mini-transcribe", "gpt-4o-mini-transcribe-2025-03-20", "gpt-4o-mini-transcribe-2025-12-15":
		capabilities.CanTranscribe = true
	}
	return capabilities
}

func (self *openAI) Transcribe(ctx context.Context, request *TranscriptionRequest) (*TranscriptionResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, fmt.Errorf("llm: transcription requires media")
	}
	ctx, cancel := context.WithTimeout(ctx, transcriptionTimeout)
	defer cancel()
	capabilities := self.TranscriptionCapabilities(request.Model)
	if !capabilities.CanTranscribe {
		return nil, fmt.Errorf("llm: transcription capabilities for model %q are unknown", request.Model)
	}
	if request.IsSpeakerDiarizationRequired && !capabilities.HasSpeakerDiarization {
		return nil, fmt.Errorf("llm: model %q cannot diarize", request.Model)
	}
	if len(request.SpeakerReferences) > 0 && (!capabilities.HasSpeakerReferences || len(request.SpeakerReferences) > capabilities.MaximumSpeakerReferenceCount) {
		return nil, fmt.Errorf("llm: speaker references are not supported by this model or exceed its limit")
	}
	if len(request.MediaBytes) == 0 || int64(len(request.MediaBytes)) > capabilities.MaximumAudioBytes || request.MediaDurationMS < 0 || request.MediaDurationMS > math.MaxInt64-1000 {
		return nil, fmt.Errorf("llm: transcription media is empty or exceeds its limit")
	}
	if !isTranscriptionContentType(request.MediaContentType) {
		return nil, fmt.Errorf("llm: unsupported transcription media content type")
	}
	if filepath.Base(request.MediaFilename) == "." || strings.TrimSpace(request.MediaFilename) == "" {
		return nil, fmt.Errorf("llm: transcription media needs a filename")
	}
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	writeField := func(fieldName, fieldValue string) error { return writer.WriteField(fieldName, fieldValue) }
	if err := writeField("model", request.Model); err != nil {
		return nil, err
	}
	responseFormat := "json"
	if capabilities.HasSpeakerDiarization {
		responseFormat = "diarized_json"
	} else if request.Model == "whisper-1" {
		responseFormat = "verbose_json"
	}
	if err := writeField("response_format", responseFormat); err != nil {
		return nil, err
	}
	if capabilities.HasSpeakerDiarization {
		if err := writeField("chunking_strategy", "auto"); err != nil {
			return nil, err
		}
	}
	if request.Language != "" {
		if err := writeField("language", request.Language); err != nil {
			return nil, err
		}
	}
	speakerNames := map[string]bool{}
	for _, reference := range request.SpeakerReferences {
		if reference.DurationMS < 2000 || reference.DurationMS > 10000 || len(reference.MediaBytes) == 0 || len(reference.MediaBytes) > 2<<20 || !isTranscriptionContentType(reference.MediaContentType) || strings.TrimSpace(reference.SpeakerName) == "" || len(reference.SpeakerName) > 64 || strings.ContainsAny(reference.SpeakerName, "\r\n") || speakerNames[reference.SpeakerName] {
			return nil, fmt.Errorf("llm: each named speaker needs a distinct 2 to 10 second audio reference")
		}
		speakerNames[reference.SpeakerName] = true
		if err := writeField("known_speaker_names[]", reference.SpeakerName); err != nil {
			return nil, err
		}
		if err := writeField("known_speaker_references[]", "data:"+reference.MediaContentType+";base64,"+base64.StdEncoding.EncodeToString(reference.MediaBytes)); err != nil {
			return nil, err
		}
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": filepath.Base(request.MediaFilename)}))
	header.Set("Content-Type", request.MediaContentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(request.MediaBytes); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	outgoing, err := http.NewRequestWithContext(ctx, http.MethodPost, self.baseUrl+"/audio/transcriptions", &payload)
	if err != nil {
		return nil, err
	}
	outgoing.Header.Set("Content-Type", writer.FormDataContentType())
	outgoing.Header.Set("Accept", "application/json")
	for headerName, headerValue := range self.headers() {
		outgoing.Header.Set(headerName, headerValue)
	}
	response, err := self.transcriptionClient().Do(outgoing)
	if err != nil {
		return nil, fmt.Errorf("llm: transcription: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	responseBytes, err := io.ReadAll(io.LimitReader(response.Body, maximumTranscriptionResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(responseBytes) > maximumTranscriptionResponseBytes {
		return nil, fmt.Errorf("llm: transcription response exceeds its limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &APIError{Status: response.StatusCode, Message: errorMessage(responseBytes)}
	}
	return decodeTranscription(request, responseBytes, capabilities)
}

func isTranscriptionContentType(contentType string) bool {
	switch contentType {
	case "audio/mpeg", "audio/mp3", "audio/mp4", "audio/m4a", "audio/x-m4a", "audio/wav", "audio/x-wav", "audio/webm", "video/mp4", "video/mpeg", "video/webm":
		return true
	}
	return false
}

func decodeTranscription(request *TranscriptionRequest, responseBytes []byte, capabilities TranscriptionCapabilities) (*TranscriptionResponse, error) {
	var decoded struct {
		Duration *float64
		Text     *string
		Segments []struct {
			ID      json.RawMessage
			Speaker string
			Start   *float64
			End     *float64
			Text    string
		}
		Usage json.RawMessage
	}
	if err := json.Unmarshal(responseBytes, &decoded); err != nil {
		return nil, fmt.Errorf("llm: decoding transcription: %w", err)
	}
	if decoded.Text == nil || (capabilities.HasSegmentTimestamps && decoded.Segments == nil) || (decoded.Text != nil && strings.TrimSpace(*decoded.Text) != "" && capabilities.HasSegmentTimestamps && len(decoded.Segments) == 0) {
		return nil, fmt.Errorf("llm: transcription returned text without required segments")
	}
	normalized := &TranscriptionResponse{Model: request.Model, TranscriptText: *decoded.Text, Segments: []TranscriptSegment{}, ProviderResponseJSON: append(json.RawMessage(nil), responseBytes...)}
	if decoded.Duration != nil {
		if *decoded.Duration < 0 || *decoded.Duration >= float64(math.MaxInt64)/1000 {
			return nil, fmt.Errorf("llm: transcription returned invalid duration")
		}
		normalized.MediaDurationMS = new(int64(math.Round(*decoded.Duration * 1000)))
	}
	for segmentIndex, segment := range decoded.Segments {
		if segment.Start == nil || segment.End == nil {
			return nil, fmt.Errorf("llm: transcription segment has no timestamps")
		}
		segmentStart, segmentEnd := *segment.Start, *segment.End

		if math.IsNaN(segmentStart) || math.IsNaN(segmentEnd) || math.IsInf(segmentStart, 0) || math.IsInf(segmentEnd, 0) || segmentStart < 0 || segmentEnd < segmentStart || segmentEnd >= float64(math.MaxInt64)/1000 || (capabilities.HasSpeakerDiarization && strings.TrimSpace(segment.Speaker) == "") {
			return nil, fmt.Errorf("llm: transcription returned an invalid segment")
		}
		startMS, endMS := int64(math.Round(segmentStart*1000)), int64(math.Round(segmentEnd*1000))
		if request.MediaDurationMS > 0 && endMS > request.MediaDurationMS+1000 {
			return nil, fmt.Errorf("llm: transcription segment exceeds the source timeline")
		}
		// A provider's last segment may end a little past the audio it was
		// sent. Within the second allowed, it is cut to the audio's end: the
		// transcript keeps no time the source does not have, and a paid
		// answer is not thrown away over a rounding at its last word.
		if request.MediaDurationMS > 0 && endMS > request.MediaDurationMS {
			endMS = request.MediaDurationMS
			startMS = min(startMS, endMS)
		}
		normalized.Segments = append(normalized.Segments, TranscriptSegment{SegmentID: fmt.Sprintf("segment-%d", segmentIndex), SpeakerID: segment.Speaker, StartMS: startMS, EndMS: endMS, TranscriptText: segment.Text})
	}
	if len(decoded.Usage) > 0 && string(decoded.Usage) != "null" {
		var reported struct {
			Type              string
			InputTokens       *int `json:"input_tokens"`
			OutputTokens      *int `json:"output_tokens"`
			InputTokenDetails struct {
				AudioTokens int `json:"audio_tokens"`
				TextTokens  int `json:"text_tokens"`
			} `json:"input_token_details"`
			Seconds *float64
		}
		if err := json.Unmarshal(decoded.Usage, &reported); err != nil {
			return nil, fmt.Errorf("llm: decoding transcription usage: %w", err)
		}
		if (reported.InputTokens != nil && *reported.InputTokens < 0) || (reported.OutputTokens != nil && *reported.OutputTokens < 0) || reported.InputTokenDetails.AudioTokens < 0 || reported.InputTokenDetails.TextTokens < 0 || (reported.Seconds != nil && (*reported.Seconds < 0 || *reported.Seconds >= float64(math.MaxInt64)/1000)) {
			return nil, fmt.Errorf("llm: transcription returned invalid usage")
		}
		normalized.TranscriptionUsage.ProviderUsageJSON = append(json.RawMessage(nil), decoded.Usage...)
		switch reported.Type {
		case "tokens":
			if reported.InputTokens == nil || reported.OutputTokens == nil {
				return nil, fmt.Errorf("llm: transcription returned incomplete token usage")
			}
			normalized.TranscriptionUsage.Usage = Usage{PromptTokens: *reported.InputTokens, CompletionTokens: *reported.OutputTokens}
			normalized.TranscriptionUsage.InputAudioTokens = reported.InputTokenDetails.AudioTokens
			normalized.TranscriptionUsage.InputTextTokens = reported.InputTokenDetails.TextTokens
			normalized.TranscriptionUsage.HasProviderUsage = true
		case "duration":
			if reported.Seconds == nil {
				return nil, fmt.Errorf("llm: transcription returned incomplete duration usage")
			}
			normalized.TranscriptionUsage.AudioDurationMS = int64(math.Round(*reported.Seconds * 1000))
			normalized.TranscriptionUsage.HasProviderUsage = true
		}
	}
	return normalized, nil
}

// transcriptionTimeout bounds one transcription request. Uploading ten
// minutes of audio and having it transcribed and told apart by speaker can
// take minutes, past the request timeout chat calls share.
const transcriptionTimeout = 10 * time.Minute

// transcriptionClient is the provider's client without the chat request
// timeout, which would cut a long transcription short; the request's
// context bounds it instead (see Transcribe).
func (self *openAI) transcriptionClient() *http.Client {
	return &http.Client{Transport: self.client.Transport, CheckRedirect: self.client.CheckRedirect, Jar: self.client.Jar}
}
