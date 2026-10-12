package client

import (
	"fmt"
	"strings"
	"time"
)

// RecordingActionArguments are explicit user edits or account-owned evidence IDs.
// A stable client key must be reused after an uncertain write response.
type RecordingActionArguments struct {
	Operation                           string  `json:"operation"`
	RecordingID                         string  `json:"recording_id"`
	RecordingMediaID                    string  `json:"media_id"`
	RecordingTranscriptRunID            string  `json:"transcript_run_id"`
	RecordingTranscriptionAttemptID     string  `json:"attempt_id"`
	RecordingTranscriptSegmentID        string  `json:"segment_id"`
	SpeakerIdentityID                   string  `json:"speaker_identity_id"`
	RecordingSpeakerCorrectionID        string  `json:"speaker_correction_id"`
	RecordingVoiceReferenceID           string  `json:"voice_reference_id"`
	RecordingTranscriptTextCorrectionID string  `json:"text_correction_id"`
	AgentAttachmentID                   string  `json:"attachment_id"`
	ClientRecordingID                   string  `json:"client_recording_id"`
	ClientRunID                         string  `json:"client_run_id"`
	ClientCorrectionID                  string  `json:"client_correction_id"`
	ConfirmedName                       string  `json:"confirmed_name"`
	LocalSpeakerID                      string  `json:"local_speaker_id"`
	CorrectedTranscriptText             string  `json:"corrected_transcript_text"`
	RecordingTitle                      string  `json:"recording_title"`
	RecordedAt                          *string `json:"recorded_at"`
	ExpectedSourceGeneration            *int64  `json:"expected_source_generation"`
	PassageStartMS                      *int64  `json:"passage_start_ms"`
	PassageEndMS                        *int64  `json:"passage_end_ms"`
	SegmentOffset                       int     `json:"segment_offset"`
	PageSize                            int     `json:"page_size"`
}

// RecordingAction resolves one reviewed operation and refuses incomplete edits.
func RecordingAction(arguments RecordingActionArguments) (string, map[string]any, error) {
	variables := map[string]any{}
	require := func(fieldName, fieldValue string) error {
		if strings.TrimSpace(fieldValue) == "" {
			return fmt.Errorf("give %s explicitly", fieldName)
		}
		return nil
	}
	operationName := ""
	switch arguments.Operation {
	case "models":
		operationName = "ListAgentModels"
	case "versions":
		operationName = "ReadRecordingProcessing"
	case "speakers":
		operationName = "ReadRecordingSpeakers"
	case "learning":
		operationName = "ReadRecordingLearning"
	case "reference-invalidations":
		operationName = "ReadRecordingReferenceInvalidations"
	case "text-corrections":
		operationName = "ReadRecordingTranscriptTextCorrections"
	case "timeline":
		operationName = "ReadRecordingTranscriptTimeline"
		if err := require("recording_id", arguments.RecordingID); err != nil {
			return "", nil, err
		}
		pageSize := arguments.PageSize
		if pageSize == 0 {
			pageSize = 50
		}
		if arguments.SegmentOffset < 0 || pageSize < 1 || pageSize > 100 {
			return "", nil, fmt.Errorf("segment_offset must be nonnegative and page_size must be 1 to 100")
		}
		variables["recordingId"], variables["segmentOffset"], variables["pageSize"] = arguments.RecordingID, arguments.SegmentOffset, pageSize
	case "transcript":
		operationName = "ReadRecordingTranscript"
		if err := require("transcript_run_id", arguments.RecordingTranscriptRunID); err != nil {
			return "", nil, err
		}
		pageSize := arguments.PageSize
		if pageSize == 0 {
			pageSize = 50
		}
		if arguments.SegmentOffset < 0 || pageSize < 1 || pageSize > 100 {
			return "", nil, fmt.Errorf("segment_offset must be nonnegative and page_size must be 1 to 100")
		}
		variables["recordingTranscriptRunId"] = arguments.RecordingTranscriptRunID
		variables["segmentOffset"], variables["pageSize"] = arguments.SegmentOffset, pageSize
	case "cancel", "retry":
		operationName = "CancelRecordingTranscription"
		if arguments.Operation == "retry" {
			operationName = "RetryRecordingTranscription"
		}
		if err := require("transcript_run_id", arguments.RecordingTranscriptRunID); err != nil {
			return "", nil, err
		}
		variables["recordingTranscriptRunId"] = arguments.RecordingTranscriptRunID
	case "process", "reprocess":
		operationName = "RequestRecordingTranscription"
		if err := require("media_id", arguments.RecordingMediaID); err != nil {
			return "", nil, err
		}
		clientRunID := arguments.ClientRunID
		if arguments.Operation == "process" && clientRunID == "" {
			clientRunID = "initial"
		}
		if err := require("client_run_id (reuse the same key after an uncertain response)", clientRunID); err != nil {
			return "", nil, err
		}
		if arguments.Operation == "reprocess" && clientRunID == "initial" {
			return "", nil, fmt.Errorf("paid reprocessing requires a new client_run_id")
		}
		variables["recordingMediaId"], variables["clientRunId"], variables["isReprocessingRequested"] = arguments.RecordingMediaID, clientRunID, arguments.Operation == "reprocess"
	case "import-attachment":
		operationName = "ImportRecordingAttachment"
		if err := require("attachment_id delivered by the user", arguments.AgentAttachmentID); err != nil {
			return "", nil, err
		}
		if err := require("recording_title", arguments.RecordingTitle); err != nil {
			return "", nil, err
		}
		clientRecordingID := arguments.ClientRecordingID
		if clientRecordingID == "" {
			clientRecordingID = "attachment-" + arguments.AgentAttachmentID
		}
		variables["agentAttachmentId"], variables["clientRecordingId"], variables["recordingTitle"] = arguments.AgentAttachmentID, clientRecordingID, arguments.RecordingTitle
		if arguments.RecordingMediaID != "" {
			variables["recordingMediaId"] = arguments.RecordingMediaID
		}
	case "metadata":
		operationName = "UpdateRecordingMetadata"
		if err := require("recording_title", arguments.RecordingTitle); err != nil {
			return "", nil, err
		}
		variables["recordingTitle"] = arguments.RecordingTitle
		variables["recordedAt"] = nil
		if arguments.RecordedAt == nil {
			return "", nil, fmt.Errorf("give recorded_at explicitly; empty means unknown occurrence date")
		}
		if *arguments.RecordedAt != "" {
			occurrence, err := time.Parse(time.RFC3339, *arguments.RecordedAt)
			if err != nil {
				return "", nil, fmt.Errorf("recorded_at must be RFC3339: %w", err)
			}
			variables["recordedAt"] = occurrence
		}
	case "speaker-confirm":
		operationName = "CorrectRecordingSpeaker"
		if err := require("confirmed_name supplied by the user", arguments.ConfirmedName); err != nil {
			return "", nil, err
		}
		if err := require("attempt_id", arguments.RecordingTranscriptionAttemptID); err != nil {
			return "", nil, err
		}
		if err := require("local_speaker_id", arguments.LocalSpeakerID); err != nil {
			return "", nil, err
		}
		if err := require("client_correction_id (reuse it after an uncertain response)", arguments.ClientCorrectionID); err != nil {
			return "", nil, err
		}
		if arguments.PassageStartMS == nil || arguments.PassageEndMS == nil {
			return "", nil, fmt.Errorf("give both passage bounds; -1/-1 confirms the request-local group")
		}
		if (*arguments.PassageStartMS != -1 || *arguments.PassageEndMS != -1) && (*arguments.PassageStartMS < 0 || *arguments.PassageEndMS <= *arguments.PassageStartMS) {
			return "", nil, fmt.Errorf("passage bounds must be -1/-1 or a positive interval")
		}
		variables["confirmedName"], variables["recordingTranscriptionAttemptId"], variables["localSpeakerId"] = arguments.ConfirmedName, arguments.RecordingTranscriptionAttemptID, arguments.LocalSpeakerID
		variables["clientCorrectionId"], variables["passageStartMS"], variables["passageEndMS"] = arguments.ClientCorrectionID, *arguments.PassageStartMS, *arguments.PassageEndMS
	case "speaker-rename":
		operationName = "RenameRecordingSpeaker"
		if err := require("speaker_identity_id", arguments.SpeakerIdentityID); err != nil {
			return "", nil, err
		}
		if err := require("confirmed_name supplied by the user", arguments.ConfirmedName); err != nil {
			return "", nil, err
		}
		variables["speakerIdentityId"], variables["confirmedName"] = arguments.SpeakerIdentityID, arguments.ConfirmedName
	case "speaker-reset":
		operationName = "RemoveRecordingSpeakerCorrection"
		if err := require("speaker_correction_id", arguments.RecordingSpeakerCorrectionID); err != nil {
			return "", nil, err
		}
		variables["recordingSpeakerCorrectionId"] = arguments.RecordingSpeakerCorrectionID
	case "reference-delete":
		operationName = "DeleteRecordingVoiceReference"
		if err := require("voice_reference_id", arguments.RecordingVoiceReferenceID); err != nil {
			return "", nil, err
		}
		variables["recordingVoiceReferenceId"] = arguments.RecordingVoiceReferenceID
	case "text-edit":
		operationName = "CorrectRecordingTranscriptText"
		if err := require("segment_id", arguments.RecordingTranscriptSegmentID); err != nil {
			return "", nil, err
		}
		if err := require("corrected_transcript_text supplied by the user", arguments.CorrectedTranscriptText); err != nil {
			return "", nil, err
		}
		if err := require("client_correction_id (reuse it after an uncertain response)", arguments.ClientCorrectionID); err != nil {
			return "", nil, err
		}
		variables["recordingTranscriptSegmentId"], variables["correctedTranscriptText"], variables["clientCorrectionId"] = arguments.RecordingTranscriptSegmentID, arguments.CorrectedTranscriptText, arguments.ClientCorrectionID
	case "text-reset":
		operationName = "RemoveRecordingTranscriptTextCorrection"
		if err := require("text_correction_id", arguments.RecordingTranscriptTextCorrectionID); err != nil {
			return "", nil, err
		}
		variables["recordingTranscriptTextCorrectionId"] = arguments.RecordingTranscriptTextCorrectionID
	default:
		return "", nil, fmt.Errorf("unknown recording operation %q", arguments.Operation)
	}
	switch arguments.Operation {
	case "versions", "speakers", "learning", "reference-invalidations", "text-corrections", "metadata", "speaker-confirm", "speaker-rename", "speaker-reset", "reference-delete", "text-edit", "text-reset":
		if err := require("recording_id", arguments.RecordingID); err != nil {
			return "", nil, err
		}
		variables["recordingId"] = arguments.RecordingID
	}
	switch arguments.Operation {
	case "metadata", "speaker-confirm", "speaker-rename", "speaker-reset", "reference-delete", "text-edit", "text-reset":
		if arguments.ExpectedSourceGeneration == nil || *arguments.ExpectedSourceGeneration < 0 {
			return "", nil, fmt.Errorf("give expected_source_generation from the current recording view")
		}
		variables["expectedSourceGeneration"] = *arguments.ExpectedSourceGeneration
	}
	return operationName, variables, nil
}
