package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// Sharing a recording: with one person or one circle, at a level (summary or
// transcript). docs/subsystems/recordings.md ("Sharing") says what each level
// shows. Who may read a shared recording, and what of it, is decided in the
// database (GetRecordingShareLevel) and made in models.RedactRecording; the
// operations here are the owner's, who shares and takes a share back, and the
// reader's two reads, which the agent's tool and the command line use too.

// RecordingShareQuery is the reads of shared recordings.
type RecordingShareQuery interface {
	ListRecordingShares(ctx context.Context, arguments ListRecordingSharesArguments) ([]*RecordingShareView, error)
	ListSharedRecordings(ctx context.Context, arguments ListSharedRecordingsArguments) ([]*models.SharedRecording, error)
	ReadSharedRecording(ctx context.Context, arguments ReadSharedRecordingArguments) (*models.SharedRecording, error)
}

// RecordingShareMutation is sharing a recording and taking a share back.
type RecordingShareMutation interface {
	ShareRecording(ctx context.Context, arguments ShareRecordingArguments) (*RecordingShareView, error)
	UnshareRecording(ctx context.Context, arguments UnshareRecordingArguments) (bool, error)
}

// RecordingShareView is one share of the caller's recording.
type RecordingShareView struct {
	ID          string `json:"id"`
	RecordingID string `json:"recordingId"`

	// Who it is shared with: a person or a circle, never both.
	GranteeUserID     *string `json:"granteeUserId"`
	GranteeUsername   string  `json:"granteeUsername"`
	GranteeName       string  `json:"granteeName"`
	GranteeCircleID   *string `json:"granteeCircleId"`
	GranteeCircleName string  `json:"granteeCircleName"`

	// summary or transcript
	RecordingShareLevel string `json:"recordingShareLevel"`

	CreatedAt time.Time `json:"createdAt"`
}

func describeRecordingShare(share *models.RecordingShare) *RecordingShareView {
	view := &RecordingShareView{
		ID: share.ID, RecordingID: share.RecordingID, RecordingShareLevel: string(share.RecordingShareLevel), CreatedAt: share.CreatedAt,
		GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName, GranteeCircleName: share.GranteeCircleName,
	}
	if share.GranteeUserID != "" {
		view.GranteeUserID = &share.GranteeUserID
	}
	if share.GranteeCircleID != "" {
		view.GranteeCircleID = &share.GranteeCircleID
	}
	return view
}

// requireNotAgentCommand refuses a command the agent ran on the person's
// computer, which reaches the API with the person's own token and says so in
// api.AgentCommandHeader. The agent's recording tool reads what others shared
// only in a run the person started, and checks that itself before it asks;
// a shell command has no such check, so the reads are refused to it here. The
// tool's own operations carry no request, so they are not affected.
func requireNotAgentCommand(ctx context.Context) error {
	if request := api.ContextRequest(ctx); request != nil && request.Header.Get(api.AgentCommandHeader) != "" {
		return api.ErrNotFound
	}
	return nil
}

type ListRecordingSharesArguments struct {
	// One of the caller's own recordings
	RecordingID string `json:"recordingId"`
}

func (self *graph) ListRecordingShares(ctx context.Context, arguments ListRecordingSharesArguments) ([]*RecordingShareView, error) {
	owner, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	source, err := tx.GetRecording(owner.ID, arguments.RecordingID)
	if err != nil {
		return nil, translateError(err)
	}
	shares, err := tx.ListRecordingShares(source.ID)
	if err != nil {
		return nil, translateError(err)
	}
	views := make([]*RecordingShareView, 0, len(shares))
	for _, share := range shares {
		views = append(views, describeRecordingShare(share))
	}
	return views, nil
}

type ShareRecordingArguments struct {
	// One of the caller's own recordings
	RecordingID string `json:"recordingId"`

	// The person to share with, who must share a circle with the caller;
	// leave out when sharing with a circle
	GranteeUserID string `json:"granteeUserId" graphapi:"nullable"`

	// The circle to share with, which the caller must be in; leave out when
	// sharing with a person
	GranteeCircleID string `json:"granteeCircleId" graphapi:"nullable"`

	// summary or transcript
	RecordingShareLevel string `json:"recordingShareLevel"`
}

func (self *graph) ShareRecording(ctx context.Context, arguments ShareRecordingArguments) (*RecordingShareView, error) {
	owner, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	recordingShareLevel, isGrantable := models.ParseRecordingShareLevel(arguments.RecordingShareLevel)
	if !isGrantable {
		return nil, fmt.Errorf("%w: a recording is shared at summary or transcript", api.ErrInvalidArguments)
	}
	granteeUserId, granteeCircleId := strings.TrimSpace(arguments.GranteeUserID), strings.TrimSpace(arguments.GranteeCircleID)
	if (granteeUserId == "") == (granteeCircleId == "") {
		return nil, fmt.Errorf("%w: share with one person or one circle", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	recordingId := strings.TrimSpace(arguments.RecordingID)
	// What the grantee was given before, to tell whether this is more.
	earlier, err := tx.ListRecordingShares(recordingId)
	if err != nil {
		return nil, translateError(err)
	}
	saved, err := tx.ShareRecording(owner.ID, &models.RecordingShare{
		RecordingID: recordingId, GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId,
		RecordingShareLevel: recordingShareLevel,
	})
	if err != nil {
		// Not a circle in common, a person who does not exist, a circle the
		// caller is not in, a recording that is not theirs: all of them answer
		// as nothing found, because which of them it was is not the caller's
		// to learn.
		return nil, translateError(err)
	}
	if isRecordingShareWider(earlier, saved) {
		if err := self.tellAboutShare(ctx, owner, models.ShareReceivedNote{
			SharerName: owner.DisplayName(), ShareKind: models.ShareKindRecording, ShareLevel: string(saved.RecordingShareLevel),
			ShareSubject: saved.RecordingTitle,
		}, granteeUserId, granteeCircleId); err != nil {
			return nil, err
		}
	}
	log.Noticef("%s shared a recording at %s", owner.Username, recordingShareLevel)
	return describeRecordingShare(saved), nil
}

// isRecordingShareWider says whether the share gives its grantee more than the
// recording's shares did before it: there was none for them, or a lower level.
func isRecordingShareWider(earlier []*models.RecordingShare, saved *models.RecordingShare) bool {
	for _, share := range earlier {
		if share.GranteeUserID == saved.GranteeUserID && share.GranteeCircleID == saved.GranteeCircleID {
			return saved.RecordingShareLevel.Rank() > share.RecordingShareLevel.Rank()
		}
	}
	return true
}

type UnshareRecordingArguments struct {
	RecordingShareID string `json:"recordingShareId"`
}

func (self *graph) UnshareRecording(ctx context.Context, arguments UnshareRecordingArguments) (bool, error) {
	owner, err := self.recordingAccount(ctx)
	if err != nil {
		return false, err
	}
	// The share is the recording's owner's to take back; anyone else is told
	// there is no such share.
	if err := self.transaction(ctx).DeleteRecordingShare(owner.ID, strings.TrimSpace(arguments.RecordingShareID)); err != nil {
		return false, translateError(err)
	}
	return true, nil
}

type ListSharedRecordingsArguments struct {
	// Continue before this recording
	BeforeRecordingID string `json:"beforeRecordingId" graphapi:"nullable"`

	// How many recordings, 1 to 100; 50 when 0
	PageSize int `json:"pageSize"`
}

func (self *graph) ListSharedRecordings(ctx context.Context, arguments ListSharedRecordingsArguments) ([]*models.SharedRecording, error) {
	if err := requireNotAgentCommand(ctx); err != nil {
		return nil, err
	}
	reader, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	pageSize := arguments.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	shared, err := self.transaction(ctx).ListSharedRecordings(reader.ID, "", strings.TrimSpace(arguments.BeforeRecordingID), pageSize)
	return shared, translateError(err)
}

type ReadSharedRecordingArguments struct {
	RecordingID string `json:"recordingId"`

	// Where in the transcript to start, in passages; at the transcript level
	// only
	PassageOffset int `json:"passageOffset"`

	// How many passages, 1 to 250; 50 when 0
	PageSize int `json:"pageSize"`
}

func (self *graph) ReadSharedRecording(ctx context.Context, arguments ReadSharedRecordingArguments) (*models.SharedRecording, error) {
	if err := requireNotAgentCommand(ctx); err != nil {
		return nil, err
	}
	reader, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	pageSize := arguments.PageSize
	if pageSize == 0 {
		pageSize = 50
	}
	shared, err := self.transaction(ctx).ReadSharedRecording(reader.ID, strings.TrimSpace(arguments.RecordingID), arguments.PassageOffset, pageSize)
	return shared, translateError(err)
}
