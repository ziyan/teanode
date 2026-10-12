package llm

import (
	"context"
	"encoding/json"
	"fmt"
)

// Transcriber is a provider capability for completed, bounded media inputs.
type Transcriber interface {
	TranscriptionCapabilities(model string) TranscriptionCapabilities
	Transcribe(context.Context, *TranscriptionRequest) (*TranscriptionResponse, error)
}

// TranscriptionCapabilities describes one model, rather than every model of a service.
type TranscriptionCapabilities struct {
	CanTranscribe                bool
	HasSpeakerDiarization        bool
	HasSegmentTimestamps         bool
	HasSpeakerReferences         bool
	MaximumAudioBytes            int64
	MaximumSpeakerReferenceCount int
}

// TranscriptionRequest contains source audio, never a conversation or a tool instruction.
type TranscriptionRequest struct {
	Model                        string
	MediaFilename                string
	MediaContentType             string
	MediaBytes                   []byte
	MediaDurationMS              int64
	Language                     string
	IsSpeakerDiarizationRequired bool
	SpeakerReferences            []SpeakerReference
}

// SpeakerReference is an excerpt whose identity was confirmed by the person.
type SpeakerReference struct {
	SpeakerName      string
	MediaContentType string
	MediaBytes       []byte
	DurationMS       int64
}

// TranscriptSegment uses the source audio timeline. SpeakerID is local to one request.
type TranscriptSegment struct {
	SegmentID      string
	SpeakerID      string
	StartMS        int64
	EndMS          int64
	TranscriptText string
}

// TranscriptionUsage preserves reported metrics without turning unknown billing into zero cost.
type TranscriptionUsage struct {
	Usage             Usage
	InputAudioTokens  int
	InputTextTokens   int
	AudioDurationMS   int64
	HasProviderUsage  bool
	ProviderUsageJSON json.RawMessage
}

// TranscriptionResponse is generated evidence; confirmed identity corrections live separately.
type TranscriptionResponse struct {
	ProviderResponseJSON json.RawMessage
	MediaDurationMS      *int64
	Model                string
	TranscriptText       string
	Segments             []TranscriptSegment
	TranscriptionUsage   TranscriptionUsage
}

// TranscriptionSelection ties capability checks to the admitted registry model.
type TranscriptionSelection struct {
	Transcriber  Transcriber
	Model        string
	Name         string
	Capabilities TranscriptionCapabilities
}

// TranscribingFor selects the optional capability without constructing another provider client.
func (self *Registry) TranscribingFor(name string, isSpeakerDiarizationRequired, hasSpeakerReferences bool) (*TranscriptionSelection, error) {
	entry, model, err := self.resolve(name)
	if err != nil {
		return nil, err
	}
	transcriber, isTranscriber := entry.service.(Transcriber)
	if !isTranscriber {
		return nil, fmt.Errorf("llm: provider %q cannot transcribe", entry.configuration.Name)
	}
	capabilities := transcriber.TranscriptionCapabilities(model)
	if !capabilities.CanTranscribe {
		return nil, fmt.Errorf("llm: transcription capabilities for model %q are unknown", model)
	}
	if isSpeakerDiarizationRequired && (!capabilities.HasSpeakerDiarization || !capabilities.HasSegmentTimestamps) {
		return nil, fmt.Errorf("llm: model %q cannot provide timestamped speaker attribution", model)
	}
	if hasSpeakerReferences && !capabilities.HasSpeakerReferences {
		return nil, fmt.Errorf("llm: model %q cannot use speaker references", model)
	}
	return &TranscriptionSelection{Transcriber: transcriber, Model: model, Name: name, Capabilities: capabilities}, nil
}
