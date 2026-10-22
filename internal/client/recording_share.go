package client

import (
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// Sharing a recording and reading the ones shared with the caller: the same
// documents for the command line and the agent's recording tool.
const (
	recordingShareSelection  = `{ id recordingId granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName recordingShareLevel createdAt }`
	sharedRecordingListing   = `{ recordingId ownerUserId ownerUsername ownerName recordingTitle createdAt recordedAt timelineDurationMS recordingSummary recordingShareLevel }`
	sharedRecordingSelection = `{ recordingId ownerUserId ownerUsername ownerName recordingTitle createdAt recordedAt timelineDurationMS recordingSummary recordingShareLevel speakerNames passages { segmentStartMS segmentEndMS speakerName transcriptText isFiller } passageCount hasMorePassages }`
)

func init() {
	RecordingDocuments["ShareRecording"] = `mutation ($recordingId: String!, $granteeUserId: String, $granteeCircleId: String, $recordingShareLevel: String!) { ShareRecording(recordingId: $recordingId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, recordingShareLevel: $recordingShareLevel) ` + recordingShareSelection + ` }`
	RecordingDocuments["UnshareRecording"] = `mutation ($recordingShareId: String!) { UnshareRecording(recordingShareId: $recordingShareId) }`
	RecordingDocuments["ListRecordingShares"] = `query ($recordingId: String!) { ListRecordingShares(recordingId: $recordingId) ` + recordingShareSelection + ` }`
	RecordingDocuments["ListSharedRecordings"] = `query ($beforeRecordingId: String, $pageSize: Int!) { ListSharedRecordings(beforeRecordingId: $beforeRecordingId, pageSize: $pageSize) ` + sharedRecordingListing + ` }`
	RecordingDocuments["ReadSharedRecording"] = `query ($recordingId: String!, $passageOffset: Int!, $pageSize: Int!) { ReadSharedRecording(recordingId: $recordingId, passageOffset: $passageOffset, pageSize: $pageSize) ` + sharedRecordingSelection + ` }`
}

// RecordingShare is one share of the caller's recording.
type RecordingShare struct {
	ID                  string  `json:"id"`
	RecordingID         string  `json:"recordingId"`
	GranteeUserID       *string `json:"granteeUserId"`
	GranteeUsername     string  `json:"granteeUsername"`
	GranteeName         string  `json:"granteeName"`
	GranteeCircleID     *string `json:"granteeCircleId"`
	GranteeCircleName   string  `json:"granteeCircleName"`
	RecordingShareLevel string  `json:"recordingShareLevel"`
	CreatedAt           string  `json:"createdAt"`
}

// recordingShareAction is RecordingAction for the five sharing operations,
// which answers false for any other.
func recordingShareAction(arguments RecordingActionArguments) (operationName string, variables map[string]any, isShareOperation bool, err error) {
	variables = map[string]any{}
	recordingId := strings.TrimSpace(arguments.RecordingID)
	switch arguments.Operation {
	case "share":
		if recordingId == "" {
			return "", nil, true, fmt.Errorf("give recording_id explicitly")
		}
		recordingShareLevel, isGrantable := models.ParseRecordingShareLevel(arguments.RecordingShareLevel)
		if !isGrantable {
			return "", nil, true, fmt.Errorf("give recording_share_level: summary (the summary and who spoke) or transcript (also the words)")
		}
		granteeUserId, granteeCircleId := strings.TrimSpace(arguments.GranteeUserID), strings.TrimSpace(arguments.GranteeCircleID)
		if (granteeUserId == "") == (granteeCircleId == "") {
			return "", nil, true, fmt.Errorf("share with one person or one circle")
		}
		variables["recordingId"], variables["recordingShareLevel"] = recordingId, string(recordingShareLevel)
		variables["granteeUserId"], variables["granteeCircleId"] = nil, nil
		if granteeUserId != "" {
			variables["granteeUserId"] = granteeUserId
		} else {
			variables["granteeCircleId"] = granteeCircleId
		}
		return "ShareRecording", variables, true, nil
	case "shares":
		if recordingId == "" {
			return "", nil, true, fmt.Errorf("give recording_id explicitly")
		}
		variables["recordingId"] = recordingId
		return "ListRecordingShares", variables, true, nil
	case "unshare":
		if strings.TrimSpace(arguments.RecordingShareID) == "" {
			return "", nil, true, fmt.Errorf("give recording_share_id, as shares lists it")
		}
		variables["recordingShareId"] = strings.TrimSpace(arguments.RecordingShareID)
		return "UnshareRecording", variables, true, nil
	case "shared":
		pageSize := arguments.PageSize
		if pageSize == 0 {
			pageSize = 25
		}
		if pageSize < 1 || pageSize > 100 {
			return "", nil, true, fmt.Errorf("page_size must be 1 to 100")
		}
		variables["pageSize"] = pageSize
		if before := strings.TrimSpace(arguments.BeforeRecordingID); before != "" {
			variables["beforeRecordingId"] = before
		}
		return "ListSharedRecordings", variables, true, nil
	case "read-shared":
		if recordingId == "" {
			return "", nil, true, fmt.Errorf("give recording_id from shared")
		}
		pageSize := arguments.PageSize
		if pageSize == 0 {
			pageSize = 50
		}
		if arguments.SegmentOffset < 0 || pageSize < 1 || pageSize > 250 {
			return "", nil, true, fmt.Errorf("segment_offset must be nonnegative and page_size must be 1 to 250")
		}
		variables["recordingId"], variables["passageOffset"], variables["pageSize"] = recordingId, arguments.SegmentOffset, pageSize
		return "ReadSharedRecording", variables, true, nil
	}
	return "", nil, false, nil
}
