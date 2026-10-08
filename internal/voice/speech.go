package voice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Speaking an answer: a piece of the agent's answer, a sentence or a few,
// read aloud by the provider's text-to-speech and streamed back as it is
// made, so the first words play before the rest is ready. The drawer asks
// for each piece as the answer is written and plays them in order.

// SpeechSettings are how an answer is spoken.
type SpeechSettings struct {
	// BaseURL is the provider's API address, https://api.openai.com/v1
	// when empty; APIKey its key.
	BaseURL string
	APIKey  string

	// SpeechModel and SpeechVoice are the provider's model and voice.
	SpeechModel string
	SpeechVoice string

	// Client reaches the provider; the default one when nil.
	Client *http.Client
}

// speechInstructions is how the models that take instructions are asked
// to read.
const speechInstructions = "You are a personal assistant talking with the person you work for. Speak warmly and naturally, at an easy conversational pace, the way you would on a phone call."

// MaximumAnswerTextLength bounds one piece of an answer: the drawer cuts
// an answer into sentences well below it.
const MaximumAnswerTextLength = 4096

// Speak reads answerText aloud, handing each piece of audio to emit as it
// arrives, mono 16-bit PCM at SampleRate, and says what it cost.
func Speak(ctx context.Context, settings *SpeechSettings, answerText string, emit func(pcm []byte) error) (*Usage, error) {
	if len(answerText) > MaximumAnswerTextLength {
		return nil, fmt.Errorf("voice: a piece of an answer is at most %d bytes", MaximumAnswerTextLength)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	body := map[string]any{
		"model":           settings.SpeechModel,
		"voice":           settings.SpeechVoice,
		"input":           answerText,
		"response_format": "pcm",
		"stream_format":   "sse",
	}
	// The older models read without instructions and refuse them.
	if strings.HasPrefix(settings.SpeechModel, "gpt-") {
		body["instructions"] = speechInstructions
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/audio/speech", bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+settings.APIKey)
	request.Header.Set("Content-Type", "application/json")
	client := settings.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("voice: the provider's speech was not reached: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("voice: the provider refused to speak (%s): %s", response.Status, providerErrorMessage(message))
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var speechEvent struct {
			Type  string `json:"type"`
			Audio []byte `json:"audio"`
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &speechEvent); err != nil {
			continue
		}
		switch speechEvent.Type {
		case "speech.audio.delta":
			if len(speechEvent.Audio) > 0 {
				if err := emit(speechEvent.Audio); err != nil {
					return nil, err
				}
			}
		case "speech.audio.done":
			usage := &Usage{}
			if speechEvent.Usage != nil {
				usage.InputTokens, usage.OutputTokens = speechEvent.Usage.InputTokens, speechEvent.Usage.OutputTokens
			}
			return usage, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("voice: the provider's speech broke off: %w", err)
	}
	return nil, fmt.Errorf("voice: the provider's speech ended before it was done")
}

// providerErrorMessage is the message of a provider's error body, or the
// body itself.
func providerErrorMessage(body []byte) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return strings.TrimSpace(string(body))
}

// WAV wraps mono 16-bit PCM at SampleRate in a WAV file's header, for a
// browser to play as it is.
func WAV(pcm []byte) []byte {
	header := make([]byte, 44)
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+len(pcm)))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 1)
	binary.LittleEndian.PutUint32(header[24:], SampleRate)
	binary.LittleEndian.PutUint32(header[28:], SampleRate*2)
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(len(pcm)))
	return append(header, pcm...)
}

// EstimateSpeechUsage is what speaking answerText costs, for a piece cut
// short before the provider said: a token for every four characters read,
// and the audio at the rate the provider counts it, about eight tokens for
// every five characters.
func EstimateSpeechUsage(answerText string) *Usage {
	characterCount := len([]rune(answerText))
	return &Usage{InputTokens: characterCount/4 + 1, OutputTokens: characterCount * 8 / 5}
}
