package db

import (
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/lib/pq"
	"github.com/ziyan/teanode/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecordingSpeakerOperation keeps confirmed labels separate from generated evidence.
type RecordingSpeakerOperation interface {
	ReadRecordingTranscriptPassageOffset(userId, runId string, passageStartMS, passageEndMS int64) (int, error)
	ProjectRecordingSpeakerCorrections(userId string, segments []*models.RecordingTranscriptSegment) error
	CheckRecordingVoiceReferenceNames(userId string, referenceBlobIds, confirmedNames []string) error
	GetRecordingSpeakerEvidence(userId, recordingId, attemptId string) (*models.RecordingTranscriptRun, *models.RecordingTranscriptionAttempt, error)
	HasRecordingSpeakerOverlap(userId, runId, attemptId, localSpeakerId string, passageStartMS, passageEndMS int64) (bool, error)
	ListRecordingsAwaitingSpeakerCuration(pageSize int) ([]*models.Recording, error)
	MarkRecordingSpeakerCuration(userId, recordingId string, sourceGeneration int64) error
	QueueRecordingSpeakerCuration(userId, recordingId string) error
	CreateRecordingVoiceReference(userId string, reference *models.RecordingVoiceReference, blob *models.RecordingBlob, maximumAccountBytes int64) (*models.RecordingVoiceReference, error)
	ListUsableRecordingVoiceReferences(userId string, referenceLimit int) ([]*models.RecordingVoiceReference, error)
	ListRevokedRecordingVoiceReferencesForCleanup(afterReferenceId string, pageSize int) ([]*RecordingVoiceReferenceCleanupCandidate, error)
	LockRevokedRecordingVoiceReferenceForCleanup(userId, recordingId, referenceId string) (*models.RecordingVoiceReference, error)
	DeleteRevokedRecordingVoiceReference(userId, recordingId, referenceId string) error
	ListRecordingSpeakerIdentities(userId string) ([]*models.RecordingSpeakerIdentity, error)
	ListRecordingSpeakerIdentitiesInUse(userId string) ([]*models.RecordingSpeakerIdentity, error)
	ListRecordingSpeakerCorrections(userId, recordingId string) ([]*models.RecordingSpeakerCorrection, error)
	ListRecordingVoiceReferences(userId, recordingId string) ([]*models.RecordingVoiceReference, error)
	CorrectRecordingSpeaker(userId string, correction RecordingSpeakerCorrectionInput) ([]*models.RecordingSpeakerCorrection, error)
	RenameRecordingSpeaker(userId, recordingId, speakerIdentityId, confirmedName string, expectedSourceGeneration int64) error
	SetRecordingSpeakerContact(userId, recordingId, speakerIdentityId, addressBookId, contactId string, expectedSourceGeneration int64) error
	ListRecordingsSpokenByContact(userId string, addressBookId string, contactId string, limit int) ([]*RecordingSpokenByContact, bool, error)
	RemoveRecordingSpeakerCorrection(userId, recordingId, correctionId string, expectedSourceGeneration int64) error
	UndoRecordingSpeakerNaming(userId, recordingId, correctionId string, expectedSourceGeneration int64) (int, error)
	RevokeRecordingVoiceReference(userId, recordingId, referenceId string, expectedSourceGeneration int64) error
}

// RecordingSpeakerCorrectionInput names a request-local group or one selected interval.
type RecordingSpeakerCorrectionInput struct {
	RecordingID                     string
	RecordingTranscriptionAttemptID string
	ClientCorrectionID              string
	LocalSpeakerID                  string
	ConfirmedName                   string
	// IsAccountOwner marks the confirmed speaker as the person; an empty
	// ConfirmedName then defaults to the person's display name.
	IsAccountOwner bool
	// ContactAddressBookID and ContactID, both or neither, tie the confirmed
	// name to a contact of the person: a name already tied to another contact
	// is tied to this one instead. Neither leaves the name's tie as it is.
	ContactAddressBookID     string
	ContactID                string
	ExpectedSourceGeneration int64
	PassageStartMS           int64
	PassageEndMS             int64
}

type recordingSpeakerIdentityModel models.RecordingSpeakerIdentity

func (recordingSpeakerIdentityModel) TableName() string { return "recording_speaker_identity" }

type recordingSpeakerCorrectionModel models.RecordingSpeakerCorrection

func (recordingSpeakerCorrectionModel) TableName() string { return "recording_speaker_correction" }

type recordingVoiceReferenceModel models.RecordingVoiceReference

func (recordingVoiceReferenceModel) TableName() string { return "recording_voice_reference" }

func (self *transaction) ListRecordingSpeakerIdentities(userId string) ([]*models.RecordingSpeakerIdentity, error) {
	var rows []*recordingSpeakerIdentityModel
	if err := self.tx.Where("user_id = ?", userId).Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	identities := make([]*models.RecordingSpeakerIdentity, 0, len(rows))
	for _, row := range rows {
		identities = append(identities, (*models.RecordingSpeakerIdentity)(row))
	}
	if err := self.fillRecordingSpeakerContactNames(identities); err != nil {
		return nil, err
	}
	return identities, nil
}

// ListRecordingSpeakerIdentitiesInUse is the names a person's recordings still
// carry: an identity that names a passage of a recording not deleted, or holds
// a voice reference not revoked, and the person's own. A name every naming of
// which was undone, or whose recordings were all deleted, is left out, so it
// is no longer offered when naming a speaker.
func (self *transaction) ListRecordingSpeakerIdentitiesInUse(userId string) ([]*models.RecordingSpeakerIdentity, error) {
	var rows []*recordingSpeakerIdentityModel
	if err := self.tx.Where(`user_id = ? AND (is_account_owner
		OR EXISTS (SELECT 1 FROM recording_speaker_correction c JOIN recording r ON r.id = c.recording_id
			WHERE c.speaker_identity_id = recording_speaker_identity.id AND NOT c.is_removed AND r.deleted_at IS NULL)
		OR EXISTS (SELECT 1 FROM recording_voice_reference v JOIN recording r ON r.id = v.recording_id
			WHERE v.speaker_identity_id = recording_speaker_identity.id AND NOT v.is_revoked AND r.deleted_at IS NULL))`, userId).
		Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	identities := make([]*models.RecordingSpeakerIdentity, 0, len(rows))
	for _, row := range rows {
		identities = append(identities, (*models.RecordingSpeakerIdentity)(row))
	}
	if err := self.fillRecordingSpeakerContactNames(identities); err != nil {
		return nil, err
	}
	return identities, nil
}

// fillRecordingSpeakerContactNames reads the name of the contact each tied
// identity is, so a listing never shows a name the contact no longer has.
func (self *transaction) fillRecordingSpeakerContactNames(identities []*models.RecordingSpeakerIdentity) error {
	contactIdsByAddressBook := map[string][]string{}
	for _, identity := range identities {
		if identity.ContactID != "" {
			contactIdsByAddressBook[identity.ContactAddressBookID] = append(contactIdsByAddressBook[identity.ContactAddressBookID], identity.ContactID)
		}
	}
	contactNames := map[[2]string]string{}
	for addressBookId, contactIds := range contactIdsByAddressBook {
		var rows []*contactModel
		if err := self.tx.Select("addressbook_id", "id", "name").Where("addressbook_id = ? AND id IN ?", addressBookId, contactIds).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			contactNames[[2]string{row.AddressBookID, row.ID}] = row.Name
		}
	}
	for _, identity := range identities {
		identity.ContactName = contactNames[[2]string{identity.ContactAddressBookID, identity.ContactID}]
	}
	return nil
}

func (self *transaction) ListRecordingSpeakerCorrections(userId, recordingId string) ([]*models.RecordingSpeakerCorrection, error) {
	if _, err := self.GetRecording(userId, recordingId); err != nil {
		return nil, err
	}
	var rows []*recordingSpeakerCorrectionModel
	if err := self.tx.Where("recording_id = ?", recordingId).Order("source_generation, passage_start_ms, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	corrections := make([]*models.RecordingSpeakerCorrection, 0, len(rows))
	for _, row := range rows {
		corrections = append(corrections, (*models.RecordingSpeakerCorrection)(row))
	}
	return corrections, nil
}
func (self *transaction) ListRecordingVoiceReferences(userId, recordingId string) ([]*models.RecordingVoiceReference, error) {
	if _, err := self.GetRecording(userId, recordingId); err != nil {
		return nil, err
	}
	var rows []*recordingVoiceReferenceModel
	if err := self.tx.Where("recording_id = ?", recordingId).Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	references := make([]*models.RecordingVoiceReference, 0, len(rows))
	for _, row := range rows {
		references = append(references, (*models.RecordingVoiceReference)(row))
	}
	return references, nil
}
func validConfirmedSpeakerName(confirmedName string) bool {
	return strings.TrimSpace(confirmedName) != "" && len(confirmedName) <= 255 && !strings.ContainsFunc(confirmedName, unicode.IsControl)
}
func (self *transaction) CorrectRecordingSpeaker(userId string, correction RecordingSpeakerCorrectionInput) ([]*models.RecordingSpeakerCorrection, error) {
	// Account locking matches original-media reservations before taking the source lock.
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return nil, err
	}
	if correction.IsAccountOwner && strings.TrimSpace(correction.ConfirmedName) == "" {
		// "This is me" without a name is the person's own identity under the
		// name it already has, so a repeated request reads as the same one.
		var ownIdentity recordingSpeakerIdentityModel
		err := self.tx.Where("user_id = ? AND is_account_owner", userId).Take(&ownIdentity).Error
		switch {
		case err == nil:
			correction.ConfirmedName = ownIdentity.ConfirmedName
		case errors.Is(err, gorm.ErrRecordNotFound):
			correction.ConfirmedName = strings.TrimSpace(owner.Name)
			if correction.ConfirmedName == "" {
				correction.ConfirmedName = owner.Username
			}
		default:
			return nil, err
		}
	}
	if !validConfirmedSpeakerName(correction.ConfirmedName) || correction.ClientCorrectionID == "" || len(correction.ClientCorrectionID) > 128 || correction.LocalSpeakerID == "" {
		return nil, ErrInvalidArguments
	}
	if (correction.ContactAddressBookID == "") != (correction.ContactID == "") {
		return nil, ErrInvalidArguments
	}
	if err := self.lockRecordingReferenceDependents(userId, correction.RecordingID); err != nil {
		return nil, err
	}
	source, err := self.LockRecording(userId, correction.RecordingID)
	if err != nil {
		return nil, err
	}
	corrected, err := self.correctRecordingSpeakerLocked(userId, source, correction)
	if err != nil {
		return nil, err
	}
	// Also on a request repeated after an uncertain answer, so the tie the
	// person asked for is there whichever of the two attempts got through.
	if correction.ContactID != "" && len(corrected) > 0 {
		if err := self.tieRecordingSpeakerToContact(userId, corrected[0].SpeakerIdentityID, correction.ContactAddressBookID, correction.ContactID); err != nil {
			return nil, err
		}
	}
	return corrected, nil
}

// correctRecordingSpeakerLocked is CorrectRecordingSpeaker once the account and
// the recording are locked and the name is checked, so that naming a speaker in
// every piece of a recording takes the locks once.
func (self *transaction) correctRecordingSpeakerLocked(userId string, source *models.Recording, correction RecordingSpeakerCorrectionInput) ([]*models.RecordingSpeakerCorrection, error) {
	var attempt recordingAttemptModel
	if err := self.tx.Where("id = ?", correction.RecordingTranscriptionAttemptID).Take(&attempt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	transcriptRun, err := self.GetRecordingTranscriptRun(userId, attempt.RecordingTranscriptRunID)
	if err != nil {
		return nil, err
	}
	if transcriptRun.RecordingID != source.ID || attempt.RecordingAttemptState != models.RecordingAttemptComplete {
		return nil, ErrNotFound
	}
	var segmentRows []*recordingSegmentModel
	if err := self.tx.Where("recording_transcript_run_id = ? AND recording_transcription_attempt_id = ? AND local_speaker_id = ?", transcriptRun.ID, attempt.ID, correction.LocalSpeakerID).Order("segment_start_ms,id").Limit(20001).Find(&segmentRows).Error; err != nil {
		return nil, err
	}
	segments := make([]*models.RecordingTranscriptSegment, 0, len(segmentRows))
	for _, segmentRow := range segmentRows {
		segments = append(segments, (*models.RecordingTranscriptSegment)(segmentRow))
	}
	isGroup := correction.PassageStartMS == -1 && correction.PassageEndMS == -1
	if !isGroup && (correction.PassageStartMS < 0 || correction.PassageEndMS <= correction.PassageStartMS) {
		return nil, ErrInvalidArguments
	}
	intervals := [][2]int64{}
	for _, segment := range segments {
		if segment.RecordingTranscriptionAttemptID != attempt.ID || segment.LocalSpeakerID != correction.LocalSpeakerID {
			continue
		}
		if isGroup {
			if segment.SegmentEndMS > segment.SegmentStartMS {
				intervals = append(intervals, [2]int64{segment.SegmentStartMS, segment.SegmentEndMS})
			}
			continue
		}
		if correction.PassageStartMS >= segment.SegmentStartMS && correction.PassageEndMS <= segment.SegmentEndMS {
			intervals = append(intervals, [2]int64{correction.PassageStartMS, correction.PassageEndMS})
			break
		}
	}
	if len(intervals) == 0 || len(intervals) > 20000 {
		return nil, ErrInvalidArguments
	}
	var previous []*recordingSpeakerCorrectionModel
	if err := self.tx.Where("recording_id = ? AND client_correction_id = ?", source.ID, correction.ClientCorrectionID).Order("passage_start_ms, id").Find(&previous).Error; err != nil {
		return nil, err
	}
	if len(previous) > 0 {
		var identity recordingSpeakerIdentityModel
		if err := self.tx.Where("id = ? AND user_id = ?", previous[0].SpeakerIdentityID, userId).Take(&identity).Error; err != nil {
			return nil, err
		}
		if identity.ConfirmedName != correction.ConfirmedName || len(previous) != len(intervals) {
			return nil, ErrAlreadyExists
		}
		corrected := make([]*models.RecordingSpeakerCorrection, 0, len(previous))
		for index, row := range previous {
			if row.RecordingTranscriptionAttemptID != attempt.ID || row.LocalSpeakerID != correction.LocalSpeakerID || row.PassageStartMS != intervals[index][0] || row.PassageEndMS != intervals[index][1] {
				return nil, ErrAlreadyExists
			}
			corrected = append(corrected, (*models.RecordingSpeakerCorrection)(row))
		}
		return corrected, nil
	}
	if source.SourceGeneration != correction.ExpectedSourceGeneration {
		return nil, ErrAlreadyExists
	}
	var identity recordingSpeakerIdentityModel
	err = self.tx.Where("user_id = ? AND confirmed_name = ?", userId, correction.ConfirmedName).Take(&identity).Error
	isNewIdentity := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !isNewIdentity {
		return nil, err
	}
	// The unique index allows one owner per account, so the previous one is cleared first.
	if correction.IsAccountOwner && !identity.IsAccountOwner {
		if err := self.tx.Model(&recordingSpeakerIdentityModel{}).Where("user_id = ? AND is_account_owner", userId).Update("is_account_owner", false).Error; err != nil {
			return nil, err
		}
	}
	if isNewIdentity {
		identity = recordingSpeakerIdentityModel{ID: newID(), UserID: userId, ConfirmedName: correction.ConfirmedName, IsAccountOwner: correction.IsAccountOwner, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if err := self.tx.Create(&identity).Error; err != nil {
			return nil, err
		}
	} else if correction.IsAccountOwner && !identity.IsAccountOwner {
		if err := self.tx.Model(&identity).Update("is_account_owner", true).Error; err != nil {
			return nil, err
		}
	}
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	rows := make([]*recordingSpeakerCorrectionModel, 0, len(intervals))
	intervalStartsMS := make([]int64, 0, len(intervals))
	intervalEndsMS := make([]int64, 0, len(intervals))
	for _, interval := range intervals {
		rows = append(rows, &recordingSpeakerCorrectionModel{ID: newID(), RecordingID: source.ID, RecordingMediaID: transcriptRun.RecordingMediaID, RecordingTranscriptionAttemptID: attempt.ID, SpeakerIdentityID: identity.ID, ClientCorrectionID: correction.ClientCorrectionID, LocalSpeakerID: correction.LocalSpeakerID, PassageStartMS: interval[0], PassageEndMS: interval[1], SourceGeneration: source.SourceGeneration + 1, CreatedAt: createdAt})
		intervalStartsMS = append(intervalStartsMS, interval[0])
		intervalEndsMS = append(intervalEndsMS, interval[1])
	}
	if err := self.tx.CreateInBatches(rows, 500).Error; err != nil {
		return nil, err
	}
	// The samples that overlap any of the intervals, found and revoked together:
	// a group of thousands of passages is one correction request.
	overlapCondition := "recording_media_id = ? AND EXISTS (SELECT 1 FROM unnest(?::bigint[], ?::bigint[]) AS span(start_ms, end_ms) WHERE recording_voice_reference.passage_start_ms < span.end_ms AND recording_voice_reference.passage_end_ms > span.start_ms)"
	var affectedReferenceRows []*recordingVoiceReferenceModel
	if err := self.tx.Where("NOT is_revoked AND "+overlapCondition, transcriptRun.RecordingMediaID, pq.Array(intervalStartsMS), pq.Array(intervalEndsMS)).Find(&affectedReferenceRows).Error; err != nil {
		return nil, err
	}
	affectedReferences := make([]*models.RecordingVoiceReference, 0, len(affectedReferenceRows))
	for _, referenceRow := range affectedReferenceRows {
		affectedReferences = append(affectedReferences, (*models.RecordingVoiceReference)(referenceRow))
	}
	if err := self.invalidateRecordingVoiceReferences(userId, affectedReferences, "The confirmed speaker for this reference was corrected."); err != nil {
		return nil, err
	}
	if len(affectedReferences) > 0 {
		if err := self.tx.Model(&recordingVoiceReferenceModel{}).Where(overlapCondition, transcriptRun.RecordingMediaID, pq.Array(intervalStartsMS), pq.Array(intervalEndsMS)).Update("is_revoked", true).Error; err != nil {
			return nil, err
		}
	}
	corrected := make([]*models.RecordingSpeakerCorrection, 0, len(rows))
	for _, row := range rows {
		corrected = append(corrected, (*models.RecordingSpeakerCorrection)(row))
	}
	if err := self.advanceRecordingSpeakerGeneration(source.ID); err != nil {
		return nil, err
	}
	return corrected, nil
}
func (self *transaction) advanceRecordingSpeakerGeneration(recordingId string) error {
	return self.tx.Model(&recordingModel{}).Where("id = ?", recordingId).Updates(map[string]any{"source_generation": gorm.Expr("source_generation + 1"), "updated_at": time.Now().UTC().Truncate(time.Microsecond)}).Error
}
func (self *transaction) RenameRecordingSpeaker(userId, recordingId, speakerIdentityId, confirmedName string, expectedSourceGeneration int64) error {
	if !validConfirmedSpeakerName(confirmedName) {
		return ErrInvalidArguments
	}
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return err
	}
	var identity recordingSpeakerIdentityModel
	if err := self.tx.Where("id = ? AND user_id = ?", speakerIdentityId, userId).Take(&identity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	var affectedRecordingIds []string
	if err := self.tx.Model(&recordingSpeakerCorrectionModel{}).Distinct("recording_id").Where("speaker_identity_id = ?", speakerIdentityId).Order("recording_id").Pluck("recording_id", &affectedRecordingIds).Error; err != nil {
		return err
	}
	if err := self.lockRecordingReferenceDependentSources(userId, affectedRecordingIds); err != nil {
		return err
	}
	var lockedSources []*recordingModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ? AND user_id = ? AND deleted_at IS NULL", affectedRecordingIds, userId).Order("id").Find(&lockedSources).Error; err != nil {
		return err
	}
	hasRequestedSource := false
	for _, source := range lockedSources {
		if source.ID == recordingId {
			hasRequestedSource = true
			if source.SourceGeneration != expectedSourceGeneration {
				return ErrAlreadyExists
			}
		}
	}
	if !hasRequestedSource {
		return ErrNotFound
	}
	if identity.ConfirmedName == confirmedName {
		return nil
	}
	var existingName recordingSpeakerIdentityModel
	err := self.tx.Where("user_id = ? AND confirmed_name = ?", userId, confirmedName).Take(&existingName).Error
	if err == nil && existingName.ID != identity.ID {
		return ErrAlreadyExists
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var affectedReferences []*models.RecordingVoiceReference
	if err := self.tx.Model(&recordingVoiceReferenceModel{}).Where("speaker_identity_id = ? AND NOT is_revoked", speakerIdentityId).Find(&affectedReferences).Error; err != nil {
		return err
	}
	if err := self.invalidateRecordingVoiceReferences(userId, affectedReferences, "The confirmed reference identity was renamed."); err != nil {
		return err
	}
	if err := self.tx.Model(&identity).Update("confirmed_name", confirmedName).Error; err != nil {
		return err
	}
	// All recording rows are locked before graph writes, in the same identifier order.
	for _, source := range lockedSources {
		if err := self.advanceRecordingSpeakerGeneration(source.ID); err != nil {
			return err
		}
		// The memory catches up in the background; the passages read the new name at once.
		if err := self.QueueRecordingKnowledgeSync(userId, source.ID); err != nil {
			return err
		}
	}
	return nil
}

// SetRecordingSpeakerContact ties a confirmed name to one of the person's
// contacts, or unties it when addressBookId and contactId are empty. The name
// is the person's own to tie, and the contact has to be in one of their own
// address books, else it is not found. The recording is the one the person is
// looking at: it has to have the name on a passage, and its generation is
// checked so an edit made from an older view is refused. Tying leaves the
// generation alone, as the name and what it labels do not change.
func (self *transaction) SetRecordingSpeakerContact(userId, recordingId, speakerIdentityId, addressBookId, contactId string, expectedSourceGeneration int64) error {
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return err
	}
	source, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return err
	}
	if source.SourceGeneration != expectedSourceGeneration {
		return ErrAlreadyExists
	}
	var identityCount int64
	if err := self.tx.Model(&recordingSpeakerIdentityModel{}).Where("id = ? AND user_id = ?", speakerIdentityId, userId).Count(&identityCount).Error; err != nil {
		return err
	}
	var correctionCount int64
	if err := self.tx.Model(&recordingSpeakerCorrectionModel{}).Where("recording_id = ? AND speaker_identity_id = ?", recordingId, speakerIdentityId).Count(&correctionCount).Error; err != nil {
		return err
	}
	if identityCount == 0 || correctionCount == 0 {
		return ErrNotFound
	}
	return self.tieRecordingSpeakerToContact(userId, speakerIdentityId, addressBookId, contactId)
}

// tieRecordingSpeakerToContact writes the tie of one of the person's names, the
// contact checked to be in an address book of the person's. Both IDs empty
// unties the name.
func (self *transaction) tieRecordingSpeakerToContact(userId, speakerIdentityId, addressBookId, contactId string) error {
	if (addressBookId == "") != (contactId == "") {
		return ErrInvalidArguments
	}
	updates := map[string]any{"contact_addressbook_id": nil, "contact_id": nil}
	if contactId != "" {
		addressBook, err := self.GetAddressBook(addressBookId)
		if err != nil {
			return err
		}
		if addressBook == nil || addressBook.UserID != userId {
			return ErrNotFound
		}
		contact, err := self.GetContact(addressBookId, contactId)
		if err != nil {
			return err
		}
		if contact == nil {
			return ErrNotFound
		}
		updates = map[string]any{"contact_addressbook_id": addressBookId, "contact_id": contactId}
	}
	updated := self.tx.Model(&recordingSpeakerIdentityModel{}).Where("id = ? AND user_id = ?", speakerIdentityId, userId).Updates(updates)
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordingSpokenByContact is a recording with how many of its passages a speaker
// tied to the contact speaks.
type RecordingSpokenByContact struct {
	Recording          *models.Recording
	SpokenPassageCount int
}

// ListRecordingsSpokenByContact is the person's recordings a speaker tied to the
// contact speaks in, newest first by when they were recorded (by when they were
// made, for one with no date), with how many passages that speaker has in each.
// A passage counts while its naming stands: not one the person took the name off,
// and not one a later naming of an overlapping passage replaced. The bool says
// there are more than limit.
func (self *transaction) ListRecordingsSpokenByContact(userId string, addressBookId string, contactId string, limit int) ([]*RecordingSpokenByContact, bool, error) {
	if userId == "" || addressBookId == "" || contactId == "" {
		return nil, false, ErrNotFound
	}
	if limit < 1 || limit > 100 {
		return nil, false, ErrInvalidArguments
	}
	var counts []struct {
		RecordingID        string
		SpokenPassageCount int
	}
	if err := self.tx.Raw(`SELECT r.id AS recording_id, COUNT(*) AS spoken_passage_count
		FROM recording r
		JOIN recording_speaker_correction c ON c.recording_id = r.id
		JOIN recording_speaker_identity i ON i.id = c.speaker_identity_id
		WHERE r.user_id = @userId AND r.deleted_at IS NULL AND i.user_id = @userId
			AND i.contact_addressbook_id = @addressBookId AND i.contact_id = @contactId
			AND NOT c.is_removed
			AND NOT EXISTS (SELECT 1 FROM recording_speaker_correction later
				WHERE later.recording_media_id = c.recording_media_id AND later.source_generation > c.source_generation
					AND later.passage_start_ms < c.passage_end_ms AND later.passage_end_ms > c.passage_start_ms)
		GROUP BY r.id
		ORDER BY COALESCE(r.recorded_at, r.created_at) DESC, r.id DESC
		LIMIT @limit`,
		sql.Named("userId", userId), sql.Named("addressBookId", addressBookId), sql.Named("contactId", contactId), sql.Named("limit", limit+1)).Scan(&counts).Error; err != nil {
		return nil, false, err
	}
	hasMore := len(counts) > limit
	if hasMore {
		counts = counts[:limit]
	}
	recordingIds := make([]string, 0, len(counts))
	for _, count := range counts {
		recordingIds = append(recordingIds, count.RecordingID)
	}
	var rows []*recordingModel
	if len(recordingIds) > 0 {
		if err := self.tx.Where("id IN ?", recordingIds).Find(&rows).Error; err != nil {
			return nil, false, err
		}
	}
	recordingsByID := map[string]*models.Recording{}
	for _, row := range rows {
		recordingsByID[row.ID] = recordingFromModel(row)
	}
	spoken := make([]*RecordingSpokenByContact, 0, len(counts))
	for _, count := range counts {
		if recording := recordingsByID[count.RecordingID]; recording != nil {
			spoken = append(spoken, &RecordingSpokenByContact{Recording: recording, SpokenPassageCount: count.SpokenPassageCount})
		}
	}
	return spoken, hasMore, nil
}

func (self *transaction) RemoveRecordingSpeakerCorrection(userId, recordingId, correctionId string, expectedSourceGeneration int64) error {
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return err
	}
	if err := self.lockRecordingReferenceDependents(userId, recordingId); err != nil {
		return err
	}
	source, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return err
	}
	if source.SourceGeneration != expectedSourceGeneration {
		return ErrAlreadyExists
	}
	references, err := self.ListRecordingVoiceReferences(userId, recordingId)
	if err != nil {
		return err
	}
	affectedReferences := []*models.RecordingVoiceReference{}
	for _, reference := range references {
		if reference.RecordingSpeakerCorrectionID == correctionId && !reference.IsRevoked {
			affectedReferences = append(affectedReferences, reference)
		}
	}
	if err := self.invalidateRecordingVoiceReferences(userId, affectedReferences, "The user removed this speaker confirmation."); err != nil {
		return err
	}
	updated := self.tx.Model(&recordingSpeakerCorrectionModel{}).Where("id = ? AND recording_id = ?", correctionId, recordingId).Update("is_removed", true)
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		return ErrNotFound
	}
	if err := self.tx.Model(&recordingVoiceReferenceModel{}).Where("recording_speaker_correction_id = ?", correctionId).Update("is_revoked", true).Error; err != nil {
		return err
	}
	return self.advanceRecordingSpeakerGeneration(recordingId)
}

// UndoRecordingSpeakerNaming removes a naming whole: every passage the request
// that wrote correctionId named goes back to its speaker label, with the voice
// samples taken from them revoked, as if the name had never been given.
func (self *transaction) UndoRecordingSpeakerNaming(userId, recordingId, correctionId string, expectedSourceGeneration int64) (int, error) {
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return 0, err
	}
	if err := self.lockRecordingReferenceDependents(userId, recordingId); err != nil {
		return 0, err
	}
	source, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return 0, err
	}
	if source.SourceGeneration != expectedSourceGeneration {
		return 0, ErrAlreadyExists
	}
	var named recordingSpeakerCorrectionModel
	if err := self.tx.Where("id = ? AND recording_id = ?", correctionId, recordingId).Take(&named).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	var correctionIds []string
	correctionQuery := self.tx.Model(&recordingSpeakerCorrectionModel{}).Where("recording_id = ? AND NOT is_removed", recordingId)
	temporarySpeakerId, nameDigest := TemporarySpeakerNamingOf(named.ClientCorrectionID)
	if temporarySpeakerId != "" {
		// A temporary speaker is named with one correction per piece, each under its own key.
		correctionQuery = correctionQuery.Where("client_correction_id LIKE ? AND client_correction_id LIKE ?", temporarySpeakerKeyPrefix+temporarySpeakerId+"-%", "%-"+nameDigest)
	} else {
		correctionQuery = correctionQuery.Where("client_correction_id = ?", named.ClientCorrectionID)
	}
	if err := correctionQuery.Pluck("id", &correctionIds).Error; err != nil {
		return 0, err
	}
	if len(correctionIds) == 0 {
		return 0, nil
	}
	references, err := self.ListRecordingVoiceReferences(userId, recordingId)
	if err != nil {
		return 0, err
	}
	isUndone := map[string]bool{}
	for _, undoneId := range correctionIds {
		isUndone[undoneId] = true
	}
	affectedReferences := []*models.RecordingVoiceReference{}
	for _, reference := range references {
		if isUndone[reference.RecordingSpeakerCorrectionID] && !reference.IsRevoked {
			affectedReferences = append(affectedReferences, reference)
		}
	}
	if err := self.invalidateRecordingVoiceReferences(userId, affectedReferences, "The user undid this speaker naming."); err != nil {
		return 0, err
	}
	if err := self.tx.Model(&recordingSpeakerCorrectionModel{}).Where("id IN ? AND recording_id = ?", correctionIds, recordingId).Update("is_removed", true).Error; err != nil {
		return 0, err
	}
	if err := self.tx.Model(&recordingVoiceReferenceModel{}).Where("recording_speaker_correction_id IN ?", correctionIds).Update("is_revoked", true).Error; err != nil {
		return 0, err
	}
	if temporarySpeakerId != "" {
		// Without this a later piece sent the speaker's sample would be named again.
		if err := self.tx.Model(&recordingTemporarySpeakerModel{}).Where("id = ? AND recording_id = ?", temporarySpeakerId, recordingId).Update("speaker_identity_id", nil).Error; err != nil {
			return 0, err
		}
	}
	return len(correctionIds), self.advanceRecordingSpeakerGeneration(recordingId)
}

func (self *transaction) RevokeRecordingVoiceReference(userId, recordingId, referenceId string, expectedSourceGeneration int64) error {
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return err
	}
	if err := self.lockRecordingReferenceDependents(userId, recordingId); err != nil {
		return err
	}
	source, err := self.LockRecording(userId, recordingId)
	if err != nil {
		return err
	}
	if source.SourceGeneration != expectedSourceGeneration {
		return ErrAlreadyExists
	}
	references, err := self.ListRecordingVoiceReferences(userId, recordingId)
	if err != nil {
		return err
	}
	affectedReferences := []*models.RecordingVoiceReference{}
	for _, reference := range references {
		if reference.ID == referenceId && !reference.IsRevoked {
			affectedReferences = append(affectedReferences, reference)
		}
	}
	if err := self.invalidateRecordingVoiceReferences(userId, affectedReferences, "The user revoked this voice reference."); err != nil {
		return err
	}
	updated := self.tx.Model(&recordingVoiceReferenceModel{}).Where("id = ? AND recording_id = ?", referenceId, recordingId).Update("is_revoked", true)
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		return ErrNotFound
	}
	return self.advanceRecordingSpeakerGeneration(recordingId)
}

// CreateRecordingVoiceReference reserves cleanup metadata before writing curated bytes.
func (self *transaction) CreateRecordingVoiceReference(userId string, reference *models.RecordingVoiceReference, blob *models.RecordingBlob, maximumAccountBytes int64) (*models.RecordingVoiceReference, error) {
	if reference == nil || blob == nil || reference.PassageStartMS < 0 || reference.PassageEndMS-reference.PassageStartMS < 2000 || reference.PassageEndMS-reference.PassageStartMS > 10000 || blob.BlobSizeBytes <= 0 || blob.BlobSizeBytes > 320044 || !validRecordingSHA256(blob.BlobSHA256) {
		return nil, ErrInvalidArguments
	}
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return nil, err
	}
	source, err := self.LockRecording(userId, reference.RecordingID)
	if err != nil {
		return nil, err
	}
	if source.SourceGeneration != reference.SourceGeneration {
		return nil, ErrAlreadyExists
	}
	transcriptRun, err := self.GetRecordingTranscriptRun(userId, reference.RecordingTranscriptRunID)
	if err != nil {
		return nil, err
	}
	if transcriptRun.RecordingID != source.ID || transcriptRun.RecordingMediaID != reference.RecordingMediaID {
		return nil, ErrNotFound
	}
	var correction recordingSpeakerCorrectionModel
	if err := self.tx.Where("id = ? AND recording_id = ?", reference.RecordingSpeakerCorrectionID, source.ID).Take(&correction).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if correction.IsRemoved || correction.SpeakerIdentityID != reference.SpeakerIdentityID || correction.RecordingMediaID != reference.RecordingMediaID || reference.PassageStartMS < correction.PassageStartMS || reference.PassageEndMS > correction.PassageEndMS {
		return nil, ErrInvalidArguments
	}
	var correctionAttempt recordingAttemptModel
	if err := self.tx.Where("id = ?", correction.RecordingTranscriptionAttemptID).Take(&correctionAttempt).Error; err != nil {
		return nil, err
	}
	if correctionAttempt.RecordingTranscriptRunID != transcriptRun.ID || correctionAttempt.RecordingAttemptState != models.RecordingAttemptComplete || transcriptRun.RecordingProcessingState != models.RecordingProcessingComplete {
		return nil, ErrInvalidArguments
	}
	isAmbiguousPassage, err := self.HasRecordingSpeakerOverlap(userId, transcriptRun.ID, correctionAttempt.ID, correction.LocalSpeakerID, reference.PassageStartMS, reference.PassageEndMS)
	if err != nil {
		return nil, err
	}
	if isAmbiguousPassage {
		return nil, ErrInvalidArguments
	}
	var overridingCount int64
	if err := self.tx.Model(&recordingSpeakerCorrectionModel{}).Where("recording_media_id = ? AND source_generation > ? AND passage_start_ms < ? AND passage_end_ms > ?", correction.RecordingMediaID, correction.SourceGeneration, reference.PassageEndMS, reference.PassageStartMS).Count(&overridingCount).Error; err != nil {
		return nil, err
	}
	if overridingCount > 0 {
		return nil, ErrAlreadyExists
	}
	var existing recordingVoiceReferenceModel
	err = self.tx.Where("recording_speaker_correction_id = ? AND passage_start_ms = ? AND passage_end_ms = ?", correction.ID, reference.PassageStartMS, reference.PassageEndMS).Take(&existing).Error
	if err == nil {
		if existing.RecordingID != reference.RecordingID || existing.RecordingMediaID != reference.RecordingMediaID || existing.RecordingTranscriptRunID != reference.RecordingTranscriptRunID || existing.SpeakerIdentityID != reference.SpeakerIdentityID {
			return nil, ErrAlreadyExists
		}
		if existing.RecordingBlobID != "" {
			previousBlob, err := self.GetRecordingBlob(userId, existing.RecordingBlobID)
			if err != nil {
				return nil, err
			}
			if previousBlob.BlobSHA256 != blob.BlobSHA256 || previousBlob.BlobSizeBytes != blob.BlobSizeBytes {
				return nil, ErrAlreadyExists
			}
		}
		return (*models.RecordingVoiceReference)(&existing), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var activeCount int64
	if err := self.tx.Model(&recordingVoiceReferenceModel{}).Where("speaker_identity_id = ? AND NOT is_revoked", reference.SpeakerIdentityID).Count(&activeCount).Error; err != nil {
		return nil, err
	}
	if activeCount >= 6 {
		return nil, ErrTooMuchAsked
	}
	var reservedBytes int64
	if err := self.tx.Raw(`SELECT COALESCE((SELECT SUM(original_size_bytes) FROM recording_media m JOIN recording r ON r.id=m.recording_id WHERE r.user_id = ?),0) + COALESCE((SELECT SUM(blob_size_bytes) FROM recording_blob b JOIN recording r ON r.id=b.recording_id WHERE r.user_id = ?),0)`, userId, userId).Scan(&reservedBytes).Error; err != nil {
		return nil, err
	}
	if maximumAccountBytes <= 0 || blob.BlobSizeBytes > maximumAccountBytes-reservedBytes {
		return nil, ErrTooMuchAsked
	}
	created := *reference
	created.ID = newID()
	created.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	created.RecordingBlobID = "recording-reference-" + created.ID
	derived := *blob
	derived.ID = created.RecordingBlobID
	derived.RecordingID = source.ID
	derived.RecordingMediaID = created.RecordingMediaID
	derived.RecordingBlobPurpose = models.RecordingBlobSpeakerReference
	derived.CreatedAt = created.CreatedAt
	derived.IsStoredVerified = false
	if err := self.tx.Create((*recordingBlobModel)(&derived)).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Create((*recordingVoiceReferenceModel)(&created)).Error; err != nil {
		return nil, err
	}
	return &created, nil
}

// ListUsableRecordingVoiceReferences only returns verified, owned, confirmed and unrevoked samples.
func (self *transaction) ListUsableRecordingVoiceReferences(userId string, referenceLimit int) ([]*models.RecordingVoiceReference, error) {
	if referenceLimit < 1 || referenceLimit > 24 {
		return nil, ErrInvalidArguments
	}
	var rows []*recordingVoiceReferenceModel
	if err := self.tx.Table("recording_voice_reference v").Select("v.*").Joins("JOIN recording r ON r.id=v.recording_id").Joins("JOIN recording_speaker_identity i ON i.id=v.speaker_identity_id").Joins("JOIN recording_speaker_correction c ON c.id=v.recording_speaker_correction_id").Joins("JOIN recording_blob b ON b.id=v.recording_blob_id").Where("r.user_id = ? AND i.user_id = ? AND r.deleted_at IS NULL AND NOT v.is_revoked AND NOT c.is_removed AND b.is_stored_verified", userId, userId).Order("v.created_at DESC, v.id").Limit(referenceLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	references := make([]*models.RecordingVoiceReference, 0, len(rows))
	for _, row := range rows {
		references = append(references, (*models.RecordingVoiceReference)(row))
	}
	return references, nil
}

// RecordingVoiceReferenceCleanupCandidate carries the current owner for storage maintenance.
type RecordingVoiceReferenceCleanupCandidate struct {
	UserID    string
	Reference *models.RecordingVoiceReference
}

// ListRevokedRecordingVoiceReferencesForCleanup does not require an enabled model worker.
func (self *transaction) ListRevokedRecordingVoiceReferencesForCleanup(afterReferenceId string, pageSize int) ([]*RecordingVoiceReferenceCleanupCandidate, error) {
	if pageSize < 1 || pageSize > 100 {
		return nil, ErrInvalidArguments
	}
	var candidateRows []struct {
		UserID    string
		Reference recordingVoiceReferenceModel `gorm:"embedded"`
	}
	if err := self.tx.Model(&recordingVoiceReferenceModel{}).
		Joins("JOIN recording ON recording.id = recording_voice_reference.recording_id").
		Select("recording_voice_reference.*,recording.user_id").
		Where("recording_voice_reference.id > ? AND recording_voice_reference.is_revoked AND recording_voice_reference.recording_blob_id IS NOT NULL AND recording.user_id IS NOT NULL AND recording.deleted_at IS NULL", afterReferenceId).
		Order("recording_voice_reference.id").Limit(pageSize).Scan(&candidateRows).Error; err != nil {
		return nil, err
	}
	candidates := make([]*RecordingVoiceReferenceCleanupCandidate, 0, len(candidateRows))
	for candidateIndex := range candidateRows {
		candidateRow := &candidateRows[candidateIndex]
		candidates = append(candidates, &RecordingVoiceReferenceCleanupCandidate{UserID: candidateRow.UserID, Reference: (*models.RecordingVoiceReference)(&candidateRow.Reference)})
	}
	return candidates, nil
}

// LockRevokedRecordingVoiceReferenceForCleanup retains owner/source locks across byte deletion.
func (self *transaction) LockRevokedRecordingVoiceReferenceForCleanup(userId, recordingId, referenceId string) (*models.RecordingVoiceReference, error) {
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return nil, err
	}
	if _, err := self.LockRecording(userId, recordingId); err != nil {
		return nil, err
	}
	var reference recordingVoiceReferenceModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND recording_id = ? AND is_revoked AND recording_blob_id IS NOT NULL", referenceId, recordingId).Take(&reference).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return (*models.RecordingVoiceReference)(&reference), nil
}

// DeleteRevokedRecordingVoiceReference forgets derived metadata only after storage deletion succeeds.
func (self *transaction) DeleteRevokedRecordingVoiceReference(userId, recordingId, referenceId string) error {
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return err
	}
	if _, err := self.LockRecording(userId, recordingId); err != nil {
		return err
	}
	var reference recordingVoiceReferenceModel
	if err := self.tx.Where("id = ? AND recording_id = ? AND is_revoked", referenceId, recordingId).Take(&reference).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	return self.tx.Where("id = ? AND recording_id = ?", reference.RecordingBlobID, recordingId).Delete(&recordingBlobModel{}).Error
}

// ListRecordingsAwaitingSpeakerCuration avoids repeating a completed unsuitable sample search.
func (self *transaction) ListRecordingsAwaitingSpeakerCuration(pageSize int) ([]*models.Recording, error) {
	if pageSize < 1 || pageSize > 100 {
		return nil, ErrInvalidArguments
	}
	var rows []*recordingModel
	if err := self.tx.Where("user_id IS NOT NULL AND deleted_at IS NULL AND speaker_curated_generation < source_generation AND (recording_origin = 'capture' OR EXISTS (SELECT 1 FROM recording_speaker_correction c WHERE c.recording_id=recording.id))").Order("updated_at,id").Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	sources := make([]*models.Recording, 0, len(rows))
	for _, row := range rows {
		sources = append(sources, recordingFromModel(row))
	}
	return sources, nil
}

// QueueRecordingSpeakerCuration makes completed evidence eligible for bounded recovery again.
func (self *transaction) QueueRecordingSpeakerCuration(userId, recordingId string) error {
	if _, err := self.LockRecording(userId, recordingId); err != nil {
		return err
	}
	return self.tx.Model(&recordingModel{}).Where("id = ? AND user_id = ? AND deleted_at IS NULL", recordingId, userId).Update("speaker_curated_generation", 0).Error
}

func (self *transaction) MarkRecordingSpeakerCuration(userId, recordingId string, sourceGeneration int64) error {
	updated := self.tx.Model(&recordingModel{}).Where("id = ? AND user_id = ? AND deleted_at IS NULL AND source_generation = ?", recordingId, userId, sourceGeneration).Update("speaker_curated_generation", sourceGeneration)
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected == 0 {
		return ErrAlreadyExists
	}
	return nil
}

// GetRecordingSpeakerEvidence reads only attribution provenance, excluding potentially large provider receipts.
func (self *transaction) GetRecordingSpeakerEvidence(userId, recordingId, attemptId string) (*models.RecordingTranscriptRun, *models.RecordingTranscriptionAttempt, error) {
	if _, err := self.GetRecording(userId, recordingId); err != nil {
		return nil, nil, err
	}
	var attempt recordingAttemptModel
	if err := self.tx.Select("id,recording_transcript_run_id,recording_audio_chunk_id,recording_attempt_state").Where("id = ?", attemptId).Take(&attempt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	transcriptRun, err := self.GetRecordingTranscriptRun(userId, attempt.RecordingTranscriptRunID)
	if err != nil {
		return nil, nil, err
	}
	if transcriptRun.RecordingID != recordingId {
		return nil, nil, ErrNotFound
	}
	return transcriptRun, recordingAttemptFromModel(&attempt), nil
}
func (self *transaction) HasRecordingSpeakerOverlap(userId, runId, attemptId, localSpeakerId string, passageStartMS, passageEndMS int64) (bool, error) {
	transcriptRun, err := self.GetRecordingTranscriptRun(userId, runId)
	if err != nil {
		return false, err
	}
	var isAmbiguousPassage bool
	err = self.tx.Raw(`SELECT COUNT(*) > 1 OR COALESCE(BOOL_OR(recording_transcription_attempt_id <> ? OR local_speaker_id <> ?),false)
		FROM recording_transcript_segment WHERE recording_transcript_run_id = ? AND segment_start_ms < ? AND segment_end_ms > ?`, attemptId, localSpeakerId, transcriptRun.ID, passageEndMS, passageStartMS).Scan(&isAmbiguousPassage).Error
	return isAmbiguousPassage, err
}

// CheckRecordingVoiceReferenceNames holds confirmed reference rows through paid-call admission.
func (self *transaction) CheckRecordingVoiceReferenceNames(userId string, referenceBlobIds, confirmedNames []string) error {
	if len(referenceBlobIds) != len(confirmedNames) || len(referenceBlobIds) > 4 {
		return ErrInvalidArguments
	}
	if len(referenceBlobIds) == 0 {
		return nil
	}
	var owner userModel
	if err := self.tx.Raw("SELECT * FROM \"user\" WHERE id = ? FOR UPDATE", userId).Scan(&owner).Error; err != nil {
		return err
	}
	expectedNames := map[string]string{}
	for referenceIndex, blobId := range referenceBlobIds {
		if blobId == "" || confirmedNames[referenceIndex] == "" {
			return ErrInvalidArguments
		}
		if _, hasDuplicate := expectedNames[blobId]; hasDuplicate {
			return ErrInvalidArguments
		}
		expectedNames[blobId] = confirmedNames[referenceIndex]
	}
	var rows []struct {
		RecordingBlobID string
		ConfirmedName   string
	}
	// References and identities serialize correction/revocation without locking another
	// recording after the worker has already locked its own source.
	if err := self.tx.Raw(`SELECT v.recording_blob_id,i.confirmed_name
  FROM recording_voice_reference v
  JOIN recording r ON r.id=v.recording_id
  JOIN recording_speaker_identity i ON i.id=v.speaker_identity_id
  JOIN recording_speaker_correction c ON c.id=v.recording_speaker_correction_id
  JOIN recording_blob b ON b.id=v.recording_blob_id
  WHERE r.user_id = ? AND i.user_id = ? AND r.deleted_at IS NULL
   AND NOT v.is_revoked AND NOT c.is_removed AND b.is_stored_verified
   AND b.recording_blob_purpose = ? AND b.recording_id = v.recording_id
   AND b.recording_media_id = v.recording_media_id AND v.recording_blob_id IN ?
  FOR SHARE OF v,i,c,b`, userId, userId, models.RecordingBlobSpeakerReference, referenceBlobIds).Scan(&rows).Error; err != nil {
		return err
	}
	// A sample of a temporary speaker is sent under its label, or under the
	// confirmed name once the person has named it.
	foundBlobIds := map[string]bool{}
	for _, row := range rows {
		foundBlobIds[row.RecordingBlobID] = true
	}
	var remainingBlobIds []string
	for _, blobId := range referenceBlobIds {
		if !foundBlobIds[blobId] {
			remainingBlobIds = append(remainingBlobIds, blobId)
		}
	}
	if len(remainingBlobIds) > 0 {
		var temporaryRows []struct {
			RecordingBlobID string
			ConfirmedName   string
		}
		if err := self.tx.Raw(`SELECT t.recording_blob_id,COALESCE(i.confirmed_name,t.speaker_label) AS confirmed_name
  FROM recording_temporary_speaker t
  JOIN recording r ON r.id=t.recording_id
  JOIN recording_blob b ON b.id=t.recording_blob_id
  LEFT JOIN recording_speaker_identity i ON i.id=t.speaker_identity_id
  WHERE r.user_id = ? AND r.deleted_at IS NULL AND b.is_stored_verified
   AND b.recording_blob_purpose = ? AND b.recording_id = t.recording_id AND t.recording_blob_id IN ?
  FOR SHARE OF t,b`, userId, models.RecordingBlobSpeakerReference, remainingBlobIds).Scan(&temporaryRows).Error; err != nil {
			return err
		}
		for _, temporaryRow := range temporaryRows {
			rows = append(rows, struct {
				RecordingBlobID string
				ConfirmedName   string
			}{temporaryRow.RecordingBlobID, temporaryRow.ConfirmedName})
		}
	}
	if len(rows) != len(referenceBlobIds) {
		return ErrAlreadyExists
	}
	for _, row := range rows {
		if expectedNames[row.RecordingBlobID] != row.ConfirmedName {
			return ErrAlreadyExists
		}
	}
	return nil
}
