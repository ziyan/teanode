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
	IsAccountOwner                      bool    `json:"is_account_owner"`
	LocalSpeakerID                      string  `json:"local_speaker_id"`
	SpeakerLabel                        string  `json:"speaker_label"`
	CorrectedTranscriptText             string  `json:"corrected_transcript_text"`
	RecordingTitle                      *string `json:"recording_title"`
	RecordedAt                          *string `json:"recorded_at"`
	KnowledgeSourceID                   string  `json:"knowledge_source_id"`
	SourceCron                          *string `json:"source_cron"`
	ExpectedSourceGeneration            *int64  `json:"expected_source_generation"`
	PassageStartMS                      *int64  `json:"passage_start_ms"`
	PassageEndMS                        *int64  `json:"passage_end_ms"`
	SegmentOffset                       int     `json:"segment_offset"`
	PageSize                            int     `json:"page_size"`
	// AddressBookID and ContactID name one of the person's contacts: the one a
	// speaker-contact ties the speaker to, and optionally the one speaker-confirm
	// and temporary-speaker-name tie the name they give to.
	AddressBookID string `json:"address_book_id"`
	ContactID     string `json:"contact_id"`
	// ShouldUntieContact has speaker-contact take the speaker off its contact.
	ShouldUntieContact bool `json:"should_untie_contact"`
	// SearchText narrows a list of recordings, or the names offered for a
	// speaker, to those containing it.
	SearchText string `json:"search_text"`
	// IsUnnamedOnly narrows the timeline to the passages whose speaker
	// nobody has named yet.
	IsUnnamedOnly bool `json:"is_unnamed_only"`
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
	// A contact is named by its address book and its own ID together.
	if (strings.TrimSpace(arguments.AddressBookID) == "") != (strings.TrimSpace(arguments.ContactID) == "") {
		return "", nil, fmt.Errorf("give address_book_id and contact_id together, as contact_book lists them")
	}
	switch arguments.Operation {
	case "read-playback", "prepare-playback":
		operationName = "ReadRecordingPlayback"
		if arguments.Operation == "prepare-playback" {
			operationName = "PrepareRecordingPlayback"
		}
		if err := require("media_id", arguments.RecordingMediaID); err != nil {
			return "", nil, err
		}
		variables["recordingMediaId"] = arguments.RecordingMediaID
	case "passage-offset":
		operationName = "ReadRecordingTranscriptPassageOffset"
		if err := require("transcript_run_id", arguments.RecordingTranscriptRunID); err != nil {
			return "", nil, err
		}
		if arguments.PassageStartMS == nil || arguments.PassageEndMS == nil || *arguments.PassageStartMS < 0 || *arguments.PassageEndMS <= *arguments.PassageStartMS {
			return "", nil, fmt.Errorf("give nonnegative passage_start_ms and a later passage_end_ms explicitly")
		}
		variables["recordingTranscriptRunId"], variables["passageStartMS"], variables["passageEndMS"] = arguments.RecordingTranscriptRunID, *arguments.PassageStartMS, *arguments.PassageEndMS
	case "source-schedule":
		operationName = "SaveRecordingSourceSchedule"
		if err := require("knowledge_source_id from the recording learning view", arguments.KnowledgeSourceID); err != nil {
			return "", nil, err
		}
		if arguments.SourceCron == nil {
			return "", nil, fmt.Errorf("give source_cron explicitly; empty disables periodic reading")
		}
		variables["sourceId"], variables["cron"] = arguments.KnowledgeSourceID, strings.TrimSpace(*arguments.SourceCron)
	case "resume-learning":
		operationName = "ResumeRecordingLearning"
	case "availability":
		operationName = "ReadRecordingTranscriptionAvailability"
	case "models":
		operationName = "ListAgentModels"
	case "versions":
		operationName = "ReadRecordingProcessing"
	case "speakers":
		operationName = "ReadRecordingSpeakers"
	case "speaker-names":
		operationName = "SuggestRecordingSpeakerNames"
		variables["searchText"] = arguments.SearchText
		if arguments.PageSize > 0 {
			variables["pageSize"] = arguments.PageSize
		}
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
		if arguments.IsUnnamedOnly {
			variables["isUnnamedOnly"] = true
		}
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
		clientRecordingID := arguments.ClientRecordingID
		if clientRecordingID == "" {
			clientRecordingID = "attachment-" + arguments.AgentAttachmentID
		}
		// An empty title is the default: the server names the recording from its transcript.
		variables["agentAttachmentId"], variables["clientRecordingId"], variables["recordingTitle"] = arguments.AgentAttachmentID, clientRecordingID, ""
		if arguments.RecordingTitle != nil {
			variables["recordingTitle"] = *arguments.RecordingTitle
		}
		if arguments.RecordingMediaID != "" {
			variables["recordingMediaId"] = arguments.RecordingMediaID
		}
	case "metadata":
		operationName = "UpdateRecordingMetadata"
		// Absent leaves the title and who wrote it alone; an empty title clears it.
		if arguments.RecordingTitle != nil {
			variables["recordingTitle"] = *arguments.RecordingTitle
		}
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
		// The person's own name is the default when they say the speaker is them.
		if !arguments.IsAccountOwner {
			if err := require("confirmed_name supplied by the user", arguments.ConfirmedName); err != nil {
				return "", nil, err
			}
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
		variables["isAccountOwner"] = arguments.IsAccountOwner
		setContact(variables, arguments)
		variables["confirmedName"], variables["recordingTranscriptionAttemptId"], variables["localSpeakerId"] = arguments.ConfirmedName, arguments.RecordingTranscriptionAttemptID, arguments.LocalSpeakerID
		variables["clientCorrectionId"], variables["passageStartMS"], variables["passageEndMS"] = arguments.ClientCorrectionID, *arguments.PassageStartMS, *arguments.PassageEndMS
	case "temporary-speaker-name":
		operationName = "NameRecordingTemporarySpeaker"
		// The person's own name is the default when they say the speaker is them.
		if !arguments.IsAccountOwner {
			if err := require("confirmed_name supplied by the user", arguments.ConfirmedName); err != nil {
				return "", nil, err
			}
		}
		if err := require("speaker_label (\"Speaker 1\" and so on, from the timeline's temporarySpeakers)", arguments.SpeakerLabel); err != nil {
			return "", nil, err
		}
		variables["speakerLabel"], variables["confirmedName"], variables["isAccountOwner"] = arguments.SpeakerLabel, arguments.ConfirmedName, arguments.IsAccountOwner
		setContact(variables, arguments)
	case "speaker-separate":
		operationName = "SeparateRecordingTemporarySpeaker"
		if err := require("speaker_label (the \"Speaker 9\" that was merged into an earlier one, from the passage's mergedTemporarySpeakerLabel)", arguments.SpeakerLabel); err != nil {
			return "", nil, err
		}
		variables["speakerLabel"] = arguments.SpeakerLabel
	case "speaker-rename":
		operationName = "RenameRecordingSpeaker"
		if err := require("speaker_identity_id", arguments.SpeakerIdentityID); err != nil {
			return "", nil, err
		}
		if err := require("confirmed_name supplied by the user", arguments.ConfirmedName); err != nil {
			return "", nil, err
		}
		variables["speakerIdentityId"], variables["confirmedName"] = arguments.SpeakerIdentityID, arguments.ConfirmedName
	case "speaker-contact":
		operationName = "SetRecordingSpeakerContact"
		if err := require("speaker_identity_id", arguments.SpeakerIdentityID); err != nil {
			return "", nil, err
		}
		if arguments.ShouldUntieContact == (strings.TrimSpace(arguments.ContactID) != "") {
			return "", nil, fmt.Errorf("give address_book_id and contact_id to tie the speaker to that contact, or should_untie_contact to take it off its contact")
		}
		variables["speakerIdentityId"] = arguments.SpeakerIdentityID
		setContact(variables, arguments)
	case "speaker-reset":
		operationName = "RemoveRecordingSpeakerCorrection"
		if err := require("speaker_correction_id", arguments.RecordingSpeakerCorrectionID); err != nil {
			return "", nil, err
		}
		variables["recordingSpeakerCorrectionId"] = arguments.RecordingSpeakerCorrectionID
	case "speaker-undo":
		operationName = "UndoRecordingSpeakerNaming"
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
	case "versions", "speakers", "learning", "reference-invalidations", "text-corrections", "metadata", "speaker-confirm", "temporary-speaker-name", "speaker-separate", "speaker-rename", "speaker-contact", "speaker-reset", "speaker-undo", "reference-delete", "text-edit", "text-reset", "resume-learning":
		if err := require("recording_id", arguments.RecordingID); err != nil {
			return "", nil, err
		}
		variables["recordingId"] = arguments.RecordingID
	}
	switch arguments.Operation {
	case "metadata", "speaker-confirm", "speaker-rename", "speaker-contact", "speaker-reset", "speaker-undo", "reference-delete", "text-edit", "text-reset", "resume-learning":
		if arguments.ExpectedSourceGeneration == nil || *arguments.ExpectedSourceGeneration < 0 {
			return "", nil, fmt.Errorf("give expected_source_generation from the current recording view")
		}
		variables["expectedSourceGeneration"] = *arguments.ExpectedSourceGeneration
	}
	return operationName, variables, nil
}

// setContact puts the contact a speaker is tied to in the variables of an
// operation. SetRecordingSpeakerContact calls its fields addressBookId and
// contactId; the operations that name a speaker call them contactAddressBookId
// and contactId, as they also carry the name's own fields.
func setContact(variables map[string]any, arguments RecordingActionArguments) {
	addressBookId, contactId := strings.TrimSpace(arguments.AddressBookID), strings.TrimSpace(arguments.ContactID)
	if arguments.Operation == "speaker-contact" {
		variables["addressBookId"], variables["contactId"] = addressBookId, contactId
		return
	}
	if contactId != "" {
		variables["contactAddressBookId"], variables["contactId"] = addressBookId, contactId
	}
}
