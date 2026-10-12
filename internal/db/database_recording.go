package db

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"mime"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrRecordingDeleted prevents an offline retry from recreating a deleted capture.
var ErrRecordingDeleted = errors.New("db: recording was deleted")

// RecordingOperation owns recording manifests and durable part receipts.
type RecordingOperation interface {
	CreateRecording(*models.Recording) (*models.Recording, error)
	GetRecording(userId, recordingId string) (*models.Recording, error)
	LockRecording(userId, recordingId string) (*models.Recording, error)
	ListRecordings(userId, beforeId string, limit int) ([]*models.Recording, error)
	UpdateRecordingCapture(userId, recordingId string, expectedGeneration int64, captureState models.RecordingCaptureState, durationMS int64, gaps []models.RecordingGap) (*models.Recording, error)
	CreateRecordingMedia(userId string, media *models.RecordingMedia, maximumMediaBytes, maximumAccountBytes int64) (*models.RecordingMedia, error)
	GetRecordingMedia(userId, mediaId string) (*models.RecordingMedia, error)
	ListRecordingMedia(userId, recordingId string) ([]*models.RecordingMedia, error)
	ListRecordingMediaParts(userId, mediaId string) ([]*models.RecordingMediaPart, error)
	PutRecordingMediaPart(userId string, part *models.RecordingMediaPart) (*models.RecordingMediaPart, error)
	CompleteRecordingMedia(userId, mediaId, verifiedSha256 string) (*models.RecordingMedia, bool, error)
	DeleteRecording(userId, recordingId string) error
	ListRecordingsForCleanup(limit int) ([]*models.Recording, error)
	ListRecordingMediaForCleanup(recordingId string) ([]*models.RecordingMedia, error)
	AdvanceRecordingMediaCleanup(recordingId, mediaId string, expectedPartIndex int) error
	FinishRecordingCleanup(recordingId string) error
}

type recordingModel struct {
	ID                    string
	UserID                *string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	ClientRecordingID     string
	RecordingTitle        string
	RecordingOrigin       models.RecordingOrigin
	RecordingCaptureState models.RecordingCaptureState
	TimelineDurationMS    int64
	CaptureGaps           []models.RecordingGap `gorm:"serializer:json"`
	SourceGeneration      int64
	DeletedAt             *time.Time
	CleanupCompletedAt    *time.Time
}

func (recordingModel) TableName() string { return "recording" }

type recordingMediaModel models.RecordingMedia

func (recordingMediaModel) TableName() string { return "recording_media" }

type recordingPartModel models.RecordingMediaPart

func (recordingPartModel) TableName() string { return "recording_media_part" }

func recordingFromModel(row *recordingModel) *models.Recording {
	return &models.Recording{ID: row.ID, UserID: optionalString(row.UserID), CreatedAt: row.CreatedAt.UTC(), UpdatedAt: row.UpdatedAt.UTC(), ClientRecordingID: row.ClientRecordingID, RecordingTitle: row.RecordingTitle, RecordingOrigin: row.RecordingOrigin, RecordingCaptureState: row.RecordingCaptureState, TimelineDurationMS: row.TimelineDurationMS, CaptureGaps: row.CaptureGaps, SourceGeneration: row.SourceGeneration, DeletedAt: row.DeletedAt}
}

func recordingMediaFromModel(row *recordingMediaModel) *models.RecordingMedia {
	media := models.RecordingMedia(*row)
	media.CreatedAt = media.CreatedAt.UTC()
	if media.VerifiedAt != nil {
		verifiedAt := media.VerifiedAt.UTC()
		media.VerifiedAt = &verifiedAt
	}
	return &media
}

func recordingPartFromModel(row *recordingPartModel) *models.RecordingMediaPart {
	part := models.RecordingMediaPart(*row)
	part.StoredAt = part.StoredAt.UTC()
	return &part
}

func (self *transaction) CreateRecording(recording *models.Recording) (*models.Recording, error) {
	if recording == nil || recording.UserID == "" || strings.TrimSpace(recording.ClientRecordingID) == "" || len(recording.ClientRecordingID) > 128 || len(recording.RecordingTitle) > 255 {
		return nil, ErrInvalidArguments
	}
	switch recording.RecordingOrigin {
	case models.RecordingOriginCapture, models.RecordingOriginAudioImport, models.RecordingOriginVideoImport:
	default:
		return nil, ErrInvalidArguments
	}
	var owner userModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", recording.UserID).Take(&owner).Error; err != nil {
		return nil, err
	}
	var existing recordingModel
	err := self.tx.Where("user_id = ? AND client_recording_id = ?", recording.UserID, recording.ClientRecordingID).Take(&existing).Error
	if err == nil {
		if existing.DeletedAt != nil {
			return nil, ErrRecordingDeleted
		}
		if existing.RecordingOrigin != recording.RecordingOrigin {
			return nil, ErrAlreadyExists
		}
		return recordingFromModel(&existing), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var recordingCount int64
	if err := self.tx.Model(&recordingModel{}).Where("user_id = ? AND deleted_at IS NULL", recording.UserID).Count(&recordingCount).Error; err != nil {
		return nil, err
	}
	if recordingCount >= 5000 {
		return nil, ErrTooMuchAsked
	}
	captureState := models.RecordingCaptureImported
	if recording.RecordingOrigin == models.RecordingOriginCapture {
		captureState = models.RecordingCaptureActive
	}
	row := recordingModel{ID: newID(), UserID: new(recording.UserID), CreatedAt: time.Now().UTC().Truncate(time.Microsecond), UpdatedAt: time.Now().UTC().Truncate(time.Microsecond), ClientRecordingID: recording.ClientRecordingID, RecordingTitle: strings.TrimSpace(recording.RecordingTitle), RecordingOrigin: recording.RecordingOrigin, RecordingCaptureState: captureState, CaptureGaps: []models.RecordingGap{}, SourceGeneration: 1}
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	return recordingFromModel(&row), nil
}

func (self *transaction) recordingOwned(userId, recordingId string, isLocked bool) (*models.Recording, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	query := self.tx.Where("id = ? AND user_id = ?", recordingId, userId)
	if isLocked {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row recordingModel
	if err := query.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if row.DeletedAt != nil {
		return nil, ErrRecordingDeleted
	}
	return recordingFromModel(&row), nil
}
func (self *transaction) GetRecording(userId, recordingId string) (*models.Recording, error) {
	return self.recordingOwned(userId, recordingId, false)
}
func (self *transaction) LockRecording(userId, recordingId string) (*models.Recording, error) {
	return self.recordingOwned(userId, recordingId, true)
}

func (self *transaction) ListRecordings(userId, beforeId string, limit int) ([]*models.Recording, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidArguments
	}
	var rows []recordingModel
	query := self.tx.Where("user_id = ? AND deleted_at IS NULL", userId)
	if beforeId != "" {
		query = query.Where("id < ?", beforeId)
	}
	if err := query.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	recordings := make([]*models.Recording, 0, len(rows))
	for index := range rows {
		recordings = append(recordings, recordingFromModel(&rows[index]))
	}
	return recordings, nil
}

func (self *transaction) UpdateRecordingCapture(userId, recordingId string, expectedGeneration int64, captureState models.RecordingCaptureState, durationMS int64, gaps []models.RecordingGap) (*models.Recording, error) {
	recording, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return nil, err
	}
	if recording.SourceGeneration != expectedGeneration {
		return nil, ErrAlreadyExists
	}
	switch captureState {
	case models.RecordingCaptureActive, models.RecordingCapturePaused, models.RecordingCaptureInterrupted, models.RecordingCaptureStopped, models.RecordingCaptureImported:
	default:
		return nil, ErrInvalidArguments
	}
	if durationMS < recording.TimelineDurationMS || len(gaps) > 10000 {
		return nil, ErrInvalidArguments
	}
	if recording.RecordingOrigin != models.RecordingOriginCapture {
		if captureState != models.RecordingCaptureImported || len(gaps) != 0 {
			return nil, ErrInvalidArguments
		}
	} else if captureState == models.RecordingCaptureImported || (recording.RecordingCaptureState == models.RecordingCaptureStopped && (captureState != models.RecordingCaptureStopped || durationMS != recording.TimelineDurationMS)) {
		return nil, ErrInvalidArguments
	}
	if len(gaps) < len(recording.CaptureGaps) {
		return nil, ErrInvalidArguments
	}
	for gapIndex, previousGap := range recording.CaptureGaps {
		nextGap := gaps[gapIndex]
		canExtendGap := gapIndex == len(recording.CaptureGaps)-1 && previousGap.GapEndMS == recording.TimelineDurationMS && (recording.RecordingCaptureState == models.RecordingCapturePaused || recording.RecordingCaptureState == models.RecordingCaptureInterrupted)
		if nextGap.GapID != previousGap.GapID || nextGap.GapStartMS != previousGap.GapStartMS || nextGap.RecordingGapReason != previousGap.RecordingGapReason || nextGap.GapEndMS < previousGap.GapEndMS || (!canExtendGap && nextGap.GapEndMS != previousGap.GapEndMS) {
			return nil, ErrInvalidArguments
		}
	}
	previousEndMS := int64(0)
	gapIds := map[string]bool{}
	for _, gap := range gaps {
		if gap.GapID == "" || len(gap.GapID) > 128 || gapIds[gap.GapID] || gap.GapStartMS < previousEndMS || gap.GapEndMS < gap.GapStartMS || gap.GapEndMS > durationMS || (gap.RecordingGapReason != models.RecordingGapPaused && gap.RecordingGapReason != models.RecordingGapInterrupted) {
			return nil, ErrInvalidArguments
		}
		gapIds[gap.GapID] = true
		previousEndMS = gap.GapEndMS
	}
	if gaps == nil {
		gaps = []models.RecordingGap{}
	}
	row := recordingModel{RecordingCaptureState: captureState, TimelineDurationMS: durationMS, CaptureGaps: gaps, SourceGeneration: recording.SourceGeneration + 1, UpdatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err := self.tx.Model(&recordingModel{}).Where("id = ?", recordingId).Select("recording_capture_state", "timeline_duration_ms", "capture_gaps", "source_generation", "updated_at").Updates(&row).Error; err != nil {
		return nil, err
	}
	return self.GetRecording(userId, recordingId)
}

func validRecordingSHA256(digest string) bool {
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == 32 && digest == strings.ToLower(digest)
}

func (self *transaction) CreateRecordingMedia(userId string, media *models.RecordingMedia, maximumMediaBytes, maximumAccountBytes int64) (*models.RecordingMedia, error) {
	if media == nil || media.OriginalSizeBytes <= 0 || media.OriginalSizeBytes > maximumMediaBytes || maximumAccountBytes <= 0 || maximumMediaBytes <= 0 || media.OriginalSizeBytes > math.MaxInt64-models.RecordingUploadPartBytes || !validRecordingSHA256(media.OriginalSHA256) || media.TimelineStartMS < 0 || media.MediaDurationMS < 0 || media.TimelineStartMS > math.MaxInt64-media.MediaDurationMS || media.ClientMediaID == "" || len(media.ClientMediaID) > 128 || media.OriginalFilename == "" || len(media.OriginalFilename) > 255 || (media.OriginalFilename == "." || media.OriginalFilename == ".." || filepath.Base(media.OriginalFilename) != media.OriginalFilename) || strings.ContainsAny(media.OriginalFilename, "\r\n\\") || len(media.OriginalContentType) > 128 {
		return nil, ErrInvalidArguments
	}
	var owner userModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userId).Take(&owner).Error; err != nil {
		return nil, err
	}
	recording, err := self.LockRecording(userId, media.RecordingID)
	if err != nil {
		return nil, err
	}
	mediaType, parameters, err := mime.ParseMediaType(media.OriginalContentType)
	if err != nil || len(parameters) != 0 || mediaType != media.OriginalContentType || strings.ContainsFunc(media.OriginalFilename, unicode.IsControl) {
		return nil, ErrInvalidArguments
	}
	if (recording.RecordingOrigin == models.RecordingOriginVideoImport && !strings.HasPrefix(mediaType, "video/")) || (recording.RecordingOrigin != models.RecordingOriginVideoImport && !strings.HasPrefix(mediaType, "audio/")) {
		return nil, ErrInvalidArguments
	}
	var previous recordingMediaModel
	err = self.tx.Where("recording_id = ? AND client_media_id = ?", media.RecordingID, media.ClientMediaID).Take(&previous).Error
	if err == nil {
		if previous.OriginalSHA256 != media.OriginalSHA256 || previous.OriginalSizeBytes != media.OriginalSizeBytes || previous.TimelineStartMS != media.TimelineStartMS || previous.MediaDurationMS != media.MediaDurationMS || previous.OriginalContentType != media.OriginalContentType {
			return nil, ErrAlreadyExists
		}
		return recordingMediaFromModel(&previous), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var mediaCount int64
	if err := self.tx.Model(&recordingMediaModel{}).Where("recording_id = ?", media.RecordingID).Count(&mediaCount).Error; err != nil {
		return nil, err
	}
	if mediaCount >= 10000 {
		return nil, ErrTooMuchAsked
	}
	var reservedSizeBytes int64
	if err := self.tx.Table("recording_media").Select("COALESCE(SUM(original_size_bytes),0)").Joins("JOIN recording ON recording.id = recording_media.recording_id").Where("recording.user_id = ?", userId).Scan(&reservedSizeBytes).Error; err != nil {
		return nil, err
	}
	if reservedSizeBytes > maximumAccountBytes-media.OriginalSizeBytes {
		return nil, ErrTooMuchAsked
	}
	created := *media
	created.ID = newID()
	created.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	created.RecordingUploadState = models.RecordingUploadPending
	created.VerifiedAt = nil
	created.CleanupNextPartIndex = 0
	created.PartCount = int((media.OriginalSizeBytes + models.RecordingUploadPartBytes - 1) / models.RecordingUploadPartBytes)
	row := recordingMediaModel(created)
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	return &created, nil
}

func (self *transaction) GetRecordingMedia(userId, mediaId string) (*models.RecordingMedia, error) {
	var row recordingMediaModel
	if err := self.tx.Where("id = ?", mediaId).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if _, err := self.GetRecording(userId, row.RecordingID); err != nil {
		return nil, err
	}
	return recordingMediaFromModel(&row), nil
}
func (self *transaction) ListRecordingMedia(userId, recordingId string) ([]*models.RecordingMedia, error) {
	if _, err := self.GetRecording(userId, recordingId); err != nil {
		return nil, err
	}
	return self.ListRecordingMediaForCleanup(recordingId)
}
func (self *transaction) ListRecordingMediaForCleanup(recordingId string) ([]*models.RecordingMedia, error) {
	var rows []recordingMediaModel
	if err := self.tx.Where("recording_id = ?", recordingId).Order("timeline_start_ms, created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	media := make([]*models.RecordingMedia, 0, len(rows))
	for _, row := range rows {
		media = append(media, recordingMediaFromModel(&row))
	}
	return media, nil
}
func (self *transaction) ListRecordingMediaParts(userId, mediaId string) ([]*models.RecordingMediaPart, error) {
	if _, err := self.GetRecordingMedia(userId, mediaId); err != nil {
		return nil, err
	}
	var rows []recordingPartModel
	if err := self.tx.Where("recording_media_id = ?", mediaId).Order("part_index").Find(&rows).Error; err != nil {
		return nil, err
	}
	parts := make([]*models.RecordingMediaPart, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, recordingPartFromModel(&row))
	}
	return parts, nil
}
func (self *transaction) PutRecordingMediaPart(userId string, part *models.RecordingMediaPart) (*models.RecordingMediaPart, error) {
	if part == nil || !validRecordingSHA256(part.PartSHA256) {
		return nil, ErrInvalidArguments
	}
	media, err := self.GetRecordingMedia(userId, part.RecordingMediaID)
	if err != nil {
		return nil, err
	}
	if _, err := self.LockRecording(userId, media.RecordingID); err != nil {
		return nil, err
	}
	media, err = self.GetRecordingMedia(userId, part.RecordingMediaID)
	if err != nil {
		return nil, err
	}
	if part.PartSizeBytes <= 0 || media.PartSizeBytes(part.PartIndex) != part.PartSizeBytes {
		return nil, ErrInvalidArguments
	}
	var previous recordingPartModel
	err = self.tx.Where("recording_media_id = ? AND part_index = ?", media.ID, part.PartIndex).Take(&previous).Error
	if err == nil {
		if previous.PartSHA256 != part.PartSHA256 || previous.PartSizeBytes != part.PartSizeBytes {
			return nil, ErrAlreadyExists
		}
		return recordingPartFromModel(&previous), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if media.RecordingUploadState != models.RecordingUploadPending {
		return nil, ErrAlreadyExists
	}
	created := *part
	created.StoredAt = time.Now().UTC().Truncate(time.Microsecond)
	row := recordingPartModel(created)
	if err := self.tx.Create(&row).Error; err != nil {
		return nil, err
	}
	return &created, nil
}
func (self *transaction) CompleteRecordingMedia(userId, mediaId, verifiedSha256 string) (*models.RecordingMedia, bool, error) {
	media, err := self.GetRecordingMedia(userId, mediaId)
	if err != nil {
		return nil, false, err
	}
	if _, err := self.LockRecording(userId, media.RecordingID); err != nil {
		return nil, false, err
	}
	media, err = self.GetRecordingMedia(userId, mediaId)
	if err != nil {
		return nil, false, err
	}
	if verifiedSha256 != media.OriginalSHA256 {
		return nil, false, ErrInvalidArguments
	}
	if media.RecordingUploadState == models.RecordingUploadComplete {
		return media, false, nil
	}
	parts, err := self.ListRecordingMediaParts(userId, mediaId)
	if err != nil {
		return nil, false, err
	}
	if len(parts) != media.PartCount {
		return nil, false, ErrInvalidArguments
	}
	for partIndex, part := range parts {
		if part.PartIndex != partIndex || part.PartSizeBytes != media.PartSizeBytes(partIndex) {
			return nil, false, ErrInvalidArguments
		}
	}
	verifiedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := self.tx.Model(&recordingMediaModel{}).Where("id = ?", mediaId).Updates(map[string]any{"recording_upload_state": models.RecordingUploadComplete, "verified_at": verifiedAt}).Error; err != nil {
		return nil, false, err
	}
	media.RecordingUploadState = models.RecordingUploadComplete
	media.VerifiedAt = &verifiedAt
	return media, true, nil
}
func (self *transaction) DeleteRecording(userId, recordingId string) error {
	recording, err := self.LockRecording(userId, recordingId)
	if errors.Is(err, ErrRecordingDeleted) {
		return nil
	}
	if err != nil {
		return err
	}
	return self.tx.Model(&recordingModel{}).Where("id = ?", recordingId).Updates(map[string]any{"deleted_at": time.Now().UTC().Truncate(time.Microsecond), "source_generation": recording.SourceGeneration + 1}).Error
}
func (self *transaction) ListRecordingsForCleanup(limit int) ([]*models.Recording, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidArguments
	}
	var rows []recordingModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("user_id IS NULL OR (deleted_at IS NOT NULL AND cleanup_completed_at IS NULL)").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	recordings := make([]*models.Recording, 0, len(rows))
	for index := range rows {
		recordings = append(recordings, recordingFromModel(&rows[index]))
	}
	return recordings, nil
}
func (self *transaction) AdvanceRecordingMediaCleanup(recordingId, mediaId string, expectedPartIndex int) error {
	var recording recordingModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND (deleted_at IS NOT NULL OR user_id IS NULL)", recordingId).Take(&recording).Error; err != nil {
		return err
	}
	updated := self.tx.Model(&recordingMediaModel{}).Where("id = ? AND recording_id = ? AND cleanup_next_part_index = ? AND cleanup_next_part_index < part_count", mediaId, recordingId, expectedPartIndex).Update("cleanup_next_part_index", expectedPartIndex+1)
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrInvalidArguments
	}
	return nil
}

func (self *transaction) FinishRecordingCleanup(recordingId string) error {
	var row recordingModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND (deleted_at IS NOT NULL OR user_id IS NULL)", recordingId).Take(&row).Error; err != nil {
		return err
	}
	var incompleteCount int64
	if err := self.tx.Model(&recordingMediaModel{}).Where("recording_id = ? AND cleanup_next_part_index < part_count", recordingId).Count(&incompleteCount).Error; err != nil {
		return err
	}
	if incompleteCount != 0 {
		return ErrInvalidArguments
	}
	if err := self.tx.Where("recording_id = ?", recordingId).Delete(&recordingMediaModel{}).Error; err != nil {
		return err
	}
	if row.UserID == nil {
		return self.tx.Where("id = ?", recordingId).Delete(&row).Error
	}
	return self.tx.Model(&row).Updates(map[string]any{"recording_title": "", "capture_gaps": "[]", "cleanup_completed_at": time.Now().UTC().Truncate(time.Microsecond)}).Error
}

// RecordingPartFileID keeps even uncommitted part writes discoverable for cleanup.
func RecordingPartFileID(mediaId string, partIndex int) string {
	return fmt.Sprintf("recording-part-%d-%s", partIndex, mediaId)
}
