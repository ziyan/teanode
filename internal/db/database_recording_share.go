package db

import (
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// RecordingShareOperation is who may read which recording, and how much: a
// person's recording shared with another person or with a circle at a level
// (summary or transcript), as a calendar is shared at a level.
//
// "At what level may this person read this recording" has one answer,
// GetRecordingShareLevel, and every door asks it. What a level shows is made in
// one place, models.RedactRecording, which ReadSharedRecording and
// ListSharedRecordings use and nothing else does. A caller that compares a
// recording's UserID with the person itself is wrong for somebody who left the
// circle that made the share possible.
type RecordingShareOperation interface {
	// ShareRecording gives a person or a circle a level of access to one of
	// the sharer's own recordings, or changes the level they have, and answers
	// the share as stored. ErrNotFound unless the recording is the sharer's
	// and not deleted and, for a person, the two share a circle, or, for a
	// circle, the sharer is in it. ErrInvalidArguments for a level that cannot
	// be given, a grantee that is not exactly one, or the sharer themselves.
	ShareRecording(sharerUserId string, share *models.RecordingShare) (*models.RecordingShare, error)

	// ListRecordingShares are a recording's shares with who they are for,
	// oldest first.
	ListRecordingShares(recordingId string) ([]*models.RecordingShare, error)

	// ListRecordingSharesBy are the shares the person has made of their own
	// recordings, oldest first, with who each is for and which recording.
	ListRecordingSharesBy(ownerUserId string) ([]*models.RecordingShare, error)

	// ListRecordingSharesReaching are the shares that reach the person now,
	// oldest first, with whose recording each is. A share to a person reaches
	// them only while the two still share a circle, and a share to a circle
	// only while both are in it. Not the person's own.
	ListRecordingSharesReaching(userId string) ([]*models.RecordingShare, error)

	// DeleteRecordingShare takes one of the owner's shares back. ErrNotFound
	// when there is none, or it is of somebody else's recording.
	DeleteRecordingShare(ownerUserId, shareId string) error

	// GetRecordingShareLevel is the highest level the shares reaching the
	// reader give them for the recording, and the empty level when none does.
	// The owner is not asked about: a person reads their own recording without
	// a share.
	GetRecordingShareLevel(readerUserId, recordingId string) (models.RecordingShareLevel, error)

	// ListSharedRecordings are the recordings shared with the reader, newest
	// first, each as the reader's level shows it and without the speaker names
	// or the transcript, which a read of one gives. An owner narrows them to
	// one person's. beforeRecordingId continues a list.
	ListSharedRecordings(readerUserId, ownerUserId, beforeRecordingId string, limit int) ([]*models.SharedRecording, error)

	// ReadSharedRecording is one recording shared with the reader as their
	// level shows it, with, at the transcript level, a page of the transcript
	// from passageOffset. ErrNotFound when no share reaches the reader.
	ReadSharedRecording(readerUserId, recordingId string, passageOffset, pageSize int) (*models.SharedRecording, error)
}

type recordingShareModel struct {
	ID                  string    `gorm:"column:id;primaryKey"`
	RecordingID         string    `gorm:"column:recording_id"`
	GranteeUserID       *string   `gorm:"column:grantee_user_id"`
	GranteeCircleID     *string   `gorm:"column:grantee_circle_id"`
	RecordingShareLevel string    `gorm:"column:recording_share_level"`
	CreatedAt           time.Time `gorm:"column:created_at"`
	CreatedByUserID     *string   `gorm:"column:created_by_user_id"`
}

func (recordingShareModel) TableName() string { return "recording_share" }

func (self *recordingShareModel) toModel() *models.RecordingShare {
	share := &models.RecordingShare{
		ID:                  self.ID,
		RecordingID:         self.RecordingID,
		RecordingShareLevel: models.RecordingShareLevel(self.RecordingShareLevel),
		CreatedAt:           self.CreatedAt.In(time.Local),
	}
	if self.GranteeUserID != nil {
		share.GranteeUserID = *self.GranteeUserID
	}
	if self.GranteeCircleID != nil {
		share.GranteeCircleID = *self.GranteeCircleID
	}
	if self.CreatedByUserID != nil {
		share.CreatedByUserID = *self.CreatedByUserID
	}
	return share
}

// reachingRecordingSharesQuery selects the shares that reach one person, with
// the recording each is for. The joins on circle_member are what make a share
// stop reaching someone the moment the circle it depended on is gone: a share
// to a person needs the two to share a circle, and a share to a circle needs
// both the reader and the recording's owner to be in it. The join on the
// owner's account is what stops a disabled account's recording reaching
// anybody, and, since recording.user_id is empty once the account is deleted,
// a recording with no owner reaches nobody. The owner and the reader both need
// agent:use, the permission the recording operations require, so that an
// operator who takes it from a role has switched recordings off for those
// people and what they kept is no longer read by others.
var reachingRecordingSharesQuery = `
SELECT share.id AS share_id, share.recording_id AS recording_id, share.recording_share_level AS recording_share_level
FROM "recording_share" share
JOIN "recording" shared ON shared.id = share.recording_id AND shared.deleted_at IS NULL
JOIN "user" owner_user ON owner_user.id = shared.user_id AND owner_user.disabled_at IS NULL
WHERE shared.user_id <> @reader
  AND ` + userHasPermissionSQL("shared.user_id") + `
  AND EXISTS (SELECT 1 FROM "user" reader_user WHERE reader_user.id = @reader AND reader_user.disabled_at IS NULL)
  AND ` + userHasPermissionSQL("@reader") + `
  AND (@recording = '' OR share.recording_id = @recording)
  AND (@owner = '' OR shared.user_id = @owner)
  AND (
    (share.grantee_user_id = @reader AND EXISTS (
        SELECT 1 FROM "circle_member" mine
        JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
        WHERE mine.user_id = @reader AND theirs.user_id = shared.user_id))
    OR
    (share.grantee_circle_id IS NOT NULL
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = @reader)
     AND EXISTS (SELECT 1 FROM "circle_member" WHERE circle_id = share.grantee_circle_id AND user_id = shared.user_id))
  )
ORDER BY shared.id DESC, share.created_at ASC, share.id ASC`

type reachingRecordingShareRow struct {
	ShareID             string `gorm:"column:share_id"`
	RecordingID         string `gorm:"column:recording_id"`
	RecordingShareLevel string `gorm:"column:recording_share_level"`
}

// reachingRecordingShares are the shares that reach the reader, for one
// recording, one owner or, with empty identifiers, all of them, newest
// recording first.
func (self *transaction) reachingRecordingShares(readerUserId, recordingId, ownerUserId string) ([]reachingRecordingShareRow, error) {
	if readerUserId == "" {
		return nil, nil
	}
	var rows []reachingRecordingShareRow
	if err := self.tx.Raw(reachingRecordingSharesQuery,
		sql.Named("reader", readerUserId), sql.Named("recording", recordingId), sql.Named("owner", ownerUserId),
		sql.Named("permission", string(models.PermissionAgentUse))).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (self *transaction) GetRecordingShareLevel(readerUserId, recordingId string) (models.RecordingShareLevel, error) {
	if recordingId == "" {
		return "", nil
	}
	rows, err := self.reachingRecordingShares(readerUserId, recordingId, "")
	if err != nil {
		return "", err
	}
	var shareLevel models.RecordingShareLevel
	for _, row := range rows {
		shareLevel = models.HighestRecordingShareLevel(shareLevel, models.RecordingShareLevel(row.RecordingShareLevel))
	}
	return shareLevel, nil
}

func (self *transaction) ShareRecording(sharerUserId string, share *models.RecordingShare) (*models.RecordingShare, error) {
	if share == nil || sharerUserId == "" || !share.RecordingShareLevel.IsGrantable() ||
		(share.GranteeUserID == "") == (share.GranteeCircleID == "") {
		return nil, ErrInvalidArguments
	}
	if share.GranteeUserID == sharerUserId {
		return nil, ErrInvalidArguments
	}
	// Only a person's own recording is shared, and one they deleted is not.
	found, err := self.LockRecording(sharerUserId, share.RecordingID)
	if errors.Is(err, ErrRecordingDeleted) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if share.GranteeUserID != "" {
		var count int64
		if err := self.tx.Model(&userModel{}).Where("\"id\" = ?", share.GranteeUserID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, ErrNotFound
		}
		isMutual, err := self.holdMutualCircle(sharerUserId, share.GranteeUserID)
		if err != nil {
			return nil, err
		}
		if !isMutual {
			return nil, ErrNotFound
		}
	} else {
		var rows []circleMemberModel
		if err := self.tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("\"circle_id\" = ? AND \"user_id\" = ?", share.GranteeCircleID, sharerUserId).Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, ErrNotFound
		}
	}

	row := &recordingShareModel{
		ID:                  newID(),
		RecordingID:         found.ID,
		GranteeUserID:       nullableText(share.GranteeUserID),
		GranteeCircleID:     nullableText(share.GranteeCircleID),
		RecordingShareLevel: string(share.RecordingShareLevel),
		CreatedAt:           time.Now(),
		CreatedByUserID:     &sharerUserId,
	}
	conflict := clause.OnConflict{DoUpdates: clause.AssignmentColumns([]string{"recording_share_level"})}
	if share.GranteeUserID != "" {
		conflict.Columns = []clause.Column{{Name: "recording_id"}, {Name: "grantee_user_id"}}
	} else {
		conflict.Columns = []clause.Column{{Name: "recording_id"}, {Name: "grantee_circle_id"}}
	}
	if err := self.tx.Clauses(conflict).Create(row).Error; err != nil {
		return nil, err
	}
	// Read back, because an upsert that changed an existing share keeps its
	// identifier and its date.
	query := self.tx.Where("\"recording_id\" = ?", found.ID)
	if share.GranteeUserID != "" {
		query = query.Where("\"grantee_user_id\" = ?", share.GranteeUserID)
	} else {
		query = query.Where("\"grantee_circle_id\" = ?", share.GranteeCircleID)
	}
	var kept recordingShareModel
	if err := query.Take(&kept).Error; err != nil {
		return nil, err
	}
	shares := []*models.RecordingShare{kept.toModel()}
	if err := nameRecordingShares(self.tx, shares); err != nil {
		return nil, err
	}
	return shares[0], nil
}

// nameRecordingShares fills in the names the listings show: the recording's
// title and whose it is, and who each share is for.
func nameRecordingShares(tx *gorm.DB, shares []*models.RecordingShare) error {
	recordingIds, userIds, circleIds := []string{}, []string{}, []string{}
	for _, share := range shares {
		recordingIds = append(recordingIds, share.RecordingID)
		if share.GranteeUserID != "" {
			userIds = append(userIds, share.GranteeUserID)
		}
		if share.GranteeCircleID != "" {
			circleIds = append(circleIds, share.GranteeCircleID)
		}
	}
	recordings := map[string]*recordingModel{}
	if len(recordingIds) > 0 {
		var rows []recordingModel
		if err := tx.Where("\"id\" IN ?", uniqueStrings(recordingIds)).Find(&rows).Error; err != nil {
			return err
		}
		for index := range rows {
			recordings[rows[index].ID] = &rows[index]
			if rows[index].UserID != nil {
				userIds = append(userIds, *rows[index].UserID)
			}
		}
	}
	users, err := loadUsersById(tx, userIds)
	if err != nil {
		return err
	}
	circleNames := map[string]string{}
	if len(circleIds) > 0 {
		var circles []circleModel
		if err := tx.Where("\"id\" IN ?", uniqueStrings(circleIds)).Find(&circles).Error; err != nil {
			return err
		}
		for _, circle := range circles {
			circleNames[circle.ID] = circle.Name
		}
	}
	for _, share := range shares {
		if recording := recordings[share.RecordingID]; recording != nil {
			share.RecordingTitle = recording.RecordingTitle
			if recording.UserID != nil {
				share.OwnerUserID = *recording.UserID
			}
		}
		if user := users[share.OwnerUserID]; user != nil {
			share.OwnerUsername, share.OwnerName = user.Username, user.DisplayName()
		}
		if user := users[share.GranteeUserID]; user != nil {
			share.GranteeUsername, share.GranteeName = user.Username, user.DisplayName()
		}
		share.GranteeCircleName = circleNames[share.GranteeCircleID]
	}
	return nil
}

func (self *transaction) recordingSharesOf(query *gorm.DB) ([]*models.RecordingShare, error) {
	var rows []recordingShareModel
	if err := query.Order("\"created_at\" ASC, \"id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	shares := make([]*models.RecordingShare, 0, len(rows))
	for index := range rows {
		shares = append(shares, rows[index].toModel())
	}
	if err := nameRecordingShares(self.tx, shares); err != nil {
		return nil, err
	}
	return shares, nil
}

func (self *transaction) ListRecordingShares(recordingId string) ([]*models.RecordingShare, error) {
	return self.recordingSharesOf(self.tx.Where("\"recording_id\" = ?", recordingId))
}

func (self *transaction) ListRecordingSharesBy(ownerUserId string) ([]*models.RecordingShare, error) {
	if ownerUserId == "" {
		return []*models.RecordingShare{}, nil
	}
	return self.recordingSharesOf(self.tx.Where(
		"\"recording_id\" IN (SELECT \"id\" FROM \"recording\" WHERE \"user_id\" = ? AND \"deleted_at\" IS NULL)", ownerUserId))
}

func (self *transaction) ListRecordingSharesReaching(userId string) ([]*models.RecordingShare, error) {
	rows, err := self.reachingRecordingShares(userId, "", "")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []*models.RecordingShare{}, nil
	}
	shareIds := make([]string, 0, len(rows))
	for _, row := range rows {
		shareIds = append(shareIds, row.ShareID)
	}
	return self.recordingSharesOf(self.tx.Where("\"id\" IN ?", shareIds))
}

func (self *transaction) DeleteRecordingShare(ownerUserId, shareId string) error {
	if ownerUserId == "" || shareId == "" {
		return ErrNotFound
	}
	result := self.tx.Where("\"id\" = ? AND \"recording_id\" IN (SELECT \"id\" FROM \"recording\" WHERE \"user_id\" = ?)", shareId, ownerUserId).
		Delete(&recordingShareModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// sharedRecordingSpeakerNames are the names the owner confirmed for speakers
// of the recording, each once, in the order they are first heard. A name the
// owner has not given is not among them: a speaker who is only a label stays
// one. The whole timeline is read for it, a page at a time, because who spoke
// is worked out passage by passage from the owner's corrections.
func (self *transaction) sharedRecordingSpeakerNames(ownerUserId, recordingId string) ([]string, error) {
	const readSize = 250
	names := []string{}
	isNamed := map[string]bool{}
	for offset := 0; ; offset += readSize {
		page, _, segmentCount, err := self.ReadRecordingTranscriptTimelinePage(ownerUserId, recordingId, offset, readSize)
		if err != nil {
			return nil, err
		}
		if err := self.ProjectRecordingSpeakerCorrections(ownerUserId, page); err != nil {
			return nil, err
		}
		for _, segment := range page {
			if segment.EffectiveSpeakerIdentityID != "" && segment.EffectiveSpeakerName != "" && !isNamed[segment.EffectiveSpeakerName] {
				isNamed[segment.EffectiveSpeakerName] = true
				names = append(names, segment.EffectiveSpeakerName)
			}
		}
		if len(page) == 0 || int64(offset+len(page)) >= segmentCount {
			return names, nil
		}
	}
}

func (self *transaction) ReadSharedRecording(readerUserId, recordingId string, passageOffset, pageSize int) (*models.SharedRecording, error) {
	if passageOffset < 0 || pageSize < 1 || pageSize > 250 {
		return nil, ErrInvalidArguments
	}
	shareLevel, err := self.GetRecordingShareLevel(readerUserId, recordingId)
	if err != nil {
		return nil, err
	}
	if !shareLevel.IsGrantable() {
		return nil, ErrNotFound
	}
	var row recordingModel
	if err := self.tx.Where("\"id\" = ? AND \"deleted_at\" IS NULL AND \"user_id\" IS NOT NULL", recordingId).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	ownerUserId := *row.UserID
	owner, err := self.GetUser(ownerUserId)
	if err != nil {
		return nil, err
	}
	// Everything is read with the owner's identifier, since it is the owner's
	// projection of the words and of who spoke that a reader is shown.
	// Finding the names reads the whole timeline, so only the first page
	// pays for it; a reader paging on already has them from the first.
	speakerNames := []string{}
	if passageOffset == 0 {
		speakerNames, err = self.sharedRecordingSpeakerNames(ownerUserId, recordingId)
		if err != nil {
			return nil, err
		}
	}
	disclosure := &models.RecordingDisclosure{Recording: recordingFromModel(&row), Owner: owner, SpeakerNames: speakerNames}
	if shareLevel.AtLeast(models.RecordingShareLevelTranscript) {
		passages, _, passageCount, err := self.ReadRecordingTranscriptTimelinePage(ownerUserId, recordingId, passageOffset, pageSize)
		if err != nil {
			return nil, err
		}
		if err := self.ProjectRecordingTranscriptTextCorrections(ownerUserId, passages); err != nil {
			return nil, err
		}
		if err := self.ProjectRecordingSpeakerCorrections(ownerUserId, passages); err != nil {
			return nil, err
		}
		disclosure.Passages, disclosure.PassageOffset, disclosure.PassageCount = passages, int64(passageOffset), passageCount
	}
	return models.RedactRecording(disclosure, shareLevel), nil
}

func (self *transaction) ListSharedRecordings(readerUserId, ownerUserId, beforeRecordingId string, limit int) ([]*models.SharedRecording, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidArguments
	}
	rows, err := self.reachingRecordingShares(readerUserId, "", ownerUserId)
	if err != nil {
		return nil, err
	}
	// The rows come newest recording first; a recording reached twice, by a
	// share to the person and by one to a circle, is listed once at the higher
	// level.
	recordingIds := []string{}
	levels := map[string]models.RecordingShareLevel{}
	for _, row := range rows {
		if beforeRecordingId != "" && row.RecordingID >= beforeRecordingId {
			continue
		}
		if _, isListed := levels[row.RecordingID]; !isListed {
			if len(recordingIds) == limit {
				continue
			}
			recordingIds = append(recordingIds, row.RecordingID)
		}
		levels[row.RecordingID] = models.HighestRecordingShareLevel(levels[row.RecordingID], models.RecordingShareLevel(row.RecordingShareLevel))
	}
	shared := make([]*models.SharedRecording, 0, len(recordingIds))
	if len(recordingIds) == 0 {
		return shared, nil
	}
	var recordingRows []recordingModel
	if err := self.tx.Where("\"id\" IN ?", recordingIds).Find(&recordingRows).Error; err != nil {
		return nil, err
	}
	rowsById := make(map[string]*recordingModel, len(recordingRows))
	ownerIds := make([]string, 0, len(recordingRows))
	for index := range recordingRows {
		rowsById[recordingRows[index].ID] = &recordingRows[index]
		if recordingRows[index].UserID != nil {
			ownerIds = append(ownerIds, *recordingRows[index].UserID)
		}
	}
	owners, err := loadUsersById(self.tx, ownerIds)
	if err != nil {
		return nil, err
	}
	for _, recordingId := range recordingIds {
		row := rowsById[recordingId]
		if row == nil || row.UserID == nil {
			continue
		}
		shared = append(shared, models.RedactRecording(&models.RecordingDisclosure{
			Recording: recordingFromModel(row), Owner: owners[*row.UserID],
		}, levels[recordingId]))
	}
	return shared, nil
}

// endRecordingSharesOutsideCircles deletes the shares that only a circle made
// possible once the person no longer has it, in both directions; the circle
// code calls it (endSharesOutsideCircles) in the transaction that takes a
// person out of a circle. The reasoning is that of
// endCalendarSharesOutsideCircles: a direct share would reach two people
// again, unchosen, the day they met in another circle, and a share to a circle
// the sharer has left is meaningless. A share to a circle that somebody else
// left stops reaching that person by the join in
// reachingRecordingSharesQuery and needs no deleting.
func endRecordingSharesOutsideCircles(tx *gorm.DB, userId string) error {
	return tx.Exec(`DELETE FROM "recording_share" WHERE "id" IN (
		SELECT share.id FROM "recording_share" share
		JOIN "recording" shared ON shared.id = share.recording_id
		WHERE share.grantee_user_id IS NOT NULL
		  AND (shared.user_id = @user OR share.grantee_user_id = @user)
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" owner
		      JOIN "circle_member" grantee ON grantee.circle_id = owner.circle_id
		      WHERE owner.user_id = shared.user_id AND grantee.user_id = share.grantee_user_id)
		UNION
		SELECT share.id FROM "recording_share" share
		JOIN "recording" shared ON shared.id = share.recording_id
		WHERE share.grantee_circle_id IS NOT NULL
		  AND shared.user_id = @user
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" member
		      WHERE member.circle_id = share.grantee_circle_id AND member.user_id = @user))`,
		sql.Named("user", userId)).Error
}
