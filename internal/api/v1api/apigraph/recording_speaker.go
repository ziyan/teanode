package apigraph

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// RecordingSpeakerQuery reads confirmed labels and reference provenance.
type RecordingSpeakerQuery interface {
	ReadRecordingTranscriptPassageOffset(context.Context, ReadRecordingTranscriptPassageOffsetArguments) (int, error)
	ReadRecordingReferenceInvalidations(context.Context, RecordingArguments) ([]*models.RecordingReferenceInvalidation, error)
	ReadRecordingLearning(context.Context, RecordingArguments) (*models.RecordingLearningView, error)
	ReadRecordingSpeakers(context.Context, RecordingArguments) (*RecordingSpeakersView, error)
	SuggestRecordingSpeakerNames(context.Context, SuggestRecordingSpeakerNamesArguments) ([]*RecordingSpeakerNameSuggestion, error)
}

// RecordingSpeakerMutation applies explicit user confirmations without retranscription.
type RecordingSpeakerMutation interface {
	ResumeRecordingLearning(context.Context, ResumeRecordingLearningArguments) (*models.RecordingLearningView, error)
	CorrectRecordingSpeaker(context.Context, CorrectRecordingSpeakerArguments) (*RecordingSpeakersView, error)
	NameRecordingTemporarySpeaker(context.Context, NameRecordingTemporarySpeakerArguments) (bool, error)
	SeparateRecordingTemporarySpeaker(context.Context, SeparateRecordingTemporarySpeakerArguments) (bool, error)
	RenameRecordingSpeaker(context.Context, RenameRecordingSpeakerArguments) (*RecordingSpeakersView, error)
	SetRecordingSpeakerContact(context.Context, SetRecordingSpeakerContactArguments) (*RecordingSpeakersView, error)
	RemoveRecordingSpeakerCorrection(context.Context, RemoveRecordingSpeakerCorrectionArguments) (*RecordingSpeakersView, error)
	UndoRecordingSpeakerNaming(context.Context, RemoveRecordingSpeakerCorrectionArguments) (*RecordingSpeakersView, error)
	DeleteRecordingVoiceReference(context.Context, DeleteRecordingVoiceReferenceArguments) (*RecordingSpeakersView, error)
}

// RecordingSpeakersView carries the generation required for concurrent edits.
type RecordingSpeakersView struct {
	SourceGeneration   int64                                `json:"sourceGeneration"`
	SpeakerIdentities  []*models.RecordingSpeakerIdentity   `json:"speakerIdentities"`
	SpeakerCorrections []*models.RecordingSpeakerCorrection `json:"speakerCorrections"`
	VoiceReferences    []*models.RecordingVoiceReference    `json:"voiceReferences"`
}

// CorrectRecordingSpeakerArguments selects one request-local group or one passage.
// Both passage bounds are -1 for the whole group. Given together, the contact
// address book and contact ID tie the confirmed name to that contact of the
// person's, replacing the contact the name was tied to before; left out, they
// leave the name's tie as it is.
type CorrectRecordingSpeakerArguments struct {
	RecordingID                     string `json:"recordingId"`
	ExpectedSourceGeneration        int64  `json:"expectedSourceGeneration"`
	ClientCorrectionID              string `json:"clientCorrectionId"`
	ConfirmedName                   string `json:"confirmedName"`
	IsAccountOwner                  bool   `json:"isAccountOwner" graphapi:"nullable"`
	RecordingTranscriptionAttemptID string `json:"recordingTranscriptionAttemptId"`
	LocalSpeakerID                  string `json:"localSpeakerId"`
	PassageStartMS                  int64  `json:"passageStartMS"`
	PassageEndMS                    int64  `json:"passageEndMS"`
	ContactAddressBookID            string `json:"contactAddressBookId" graphapi:"nullable"`
	ContactID                       string `json:"contactId" graphapi:"nullable"`
}

// NameRecordingTemporarySpeakerArguments names a speaker of a recording that is
// still being captured by the label the timeline gives it, in every piece.
// Given together, the contact address book and contact ID tie the confirmed
// name to that contact of the person's, replacing the contact the name was
// tied to before; left out, they leave the name's tie as it is.
type NameRecordingTemporarySpeakerArguments struct {
	RecordingID          string `json:"recordingId"`
	SpeakerLabel         string `json:"speakerLabel"`
	ConfirmedName        string `json:"confirmedName"`
	IsAccountOwner       bool   `json:"isAccountOwner" graphapi:"nullable"`
	ContactAddressBookID string `json:"contactAddressBookId" graphapi:"nullable"`
	ContactID            string `json:"contactId" graphapi:"nullable"`
}

// SeparateRecordingTemporarySpeakerArguments undoes the merge of a label of a
// recording that is still being captured into an earlier one.
type SeparateRecordingTemporarySpeakerArguments struct {
	RecordingID  string `json:"recordingId"`
	SpeakerLabel string `json:"speakerLabel"`
}

// RenameRecordingSpeakerArguments renames an existing confirmed identity.
type RenameRecordingSpeakerArguments struct {
	RecordingID              string `json:"recordingId"`
	ExpectedSourceGeneration int64  `json:"expectedSourceGeneration"`
	SpeakerIdentityID        string `json:"speakerIdentityId"`
	ConfirmedName            string `json:"confirmedName"`
}

// SetRecordingSpeakerContactArguments ties a confirmed identity to one of the
// person's contacts, so the contact's card lists the recordings that voice
// speaks in. An empty address book and contact ID untie it. The recording is
// one the identity is named in.
type SetRecordingSpeakerContactArguments struct {
	RecordingID              string `json:"recordingId"`
	ExpectedSourceGeneration int64  `json:"expectedSourceGeneration"`
	SpeakerIdentityID        string `json:"speakerIdentityId"`
	AddressBookID            string `json:"addressBookId" graphapi:"nullable"`
	ContactID                string `json:"contactId" graphapi:"nullable"`
}

// RemoveRecordingSpeakerCorrectionArguments revokes a confirmed interval.
type RemoveRecordingSpeakerCorrectionArguments struct {
	RecordingID                  string `json:"recordingId"`
	ExpectedSourceGeneration     int64  `json:"expectedSourceGeneration"`
	RecordingSpeakerCorrectionID string `json:"recordingSpeakerCorrectionId"`
}

// DeleteRecordingVoiceReferenceArguments revokes an automatically curated sample.
type DeleteRecordingVoiceReferenceArguments struct {
	RecordingID               string `json:"recordingId"`
	ExpectedSourceGeneration  int64  `json:"expectedSourceGeneration"`
	RecordingVoiceReferenceID string `json:"recordingVoiceReferenceId"`
}

func (self *graph) recordingSpeakerOwner(ctx context.Context) (string, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return "", err
	}
	if principal.User == nil {
		return "", api.ErrNotLoggedIn
	}
	return principal.User.ID, nil
}
func (self *graph) ReadRecordingSpeakers(ctx context.Context, arguments RecordingArguments) (*RecordingSpeakersView, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	transaction := self.transaction(ctx)
	source, err := transaction.GetRecording(userId, arguments.RecordingID)
	if err != nil {
		return nil, translateError(err)
	}
	identities, err := transaction.ListRecordingSpeakerIdentities(userId)
	if err != nil {
		return nil, translateError(err)
	}
	corrections, err := transaction.ListRecordingSpeakerCorrections(userId, source.ID)
	if err != nil {
		return nil, translateError(err)
	}
	references, err := transaction.ListRecordingVoiceReferences(userId, source.ID)
	if err != nil {
		return nil, translateError(err)
	}
	grantedAddressBookIds, err := self.agentGrantedAddressBookIds(ctx, userId)
	if err != nil {
		return nil, err
	}
	for _, identity := range identities {
		if grantedAddressBookIds != nil && !grantedAddressBookIds[identity.ContactAddressBookID] {
			identity.ContactAddressBookID, identity.ContactID, identity.ContactName = "", "", ""
		}
	}
	return &RecordingSpeakersView{SourceGeneration: source.SourceGeneration, SpeakerIdentities: identities, SpeakerCorrections: corrections, VoiceReferences: references}, nil
}

// agentGrantedAddressBookIds is the address books of the person's that the
// caller may read for this: nil for the person themselves, who may read every
// one, and for their agent the books they gave it. A speaker's contact and a
// person's card both keep to it.
func (self *graph) agentGrantedAddressBookIds(ctx context.Context, userId string) (map[string]bool, error) {
	if db.PrincipalFromContext(ctx).ActorKind != models.AuditActorAgent {
		return nil, nil
	}
	addressBooks, err := self.transaction(ctx).ListAddressBooks(userId)
	if err != nil {
		return nil, translateError(err)
	}
	grantedAddressBookIds := map[string]bool{}
	for _, addressBook := range addressBooks {
		if addressBook.AgentGranted {
			grantedAddressBookIds[addressBook.ID] = true
		}
	}
	return grantedAddressBookIds, nil
}

// requireRecordingSpeakerContactBook refuses a contact in an address book the
// caller may not use for this, as one that is not there: the agent may only
// tie a speaker to a contact of a book the person gave it. Whether the book is
// the person's at all is checked where the tie is written.
func (self *graph) requireRecordingSpeakerContactBook(ctx context.Context, userId, addressBookId string) error {
	if addressBookId == "" {
		return nil
	}
	grantedAddressBookIds, err := self.agentGrantedAddressBookIds(ctx, userId)
	if err != nil {
		return err
	}
	if grantedAddressBookIds != nil && !grantedAddressBookIds[addressBookId] {
		return api.ErrNotFound
	}
	return nil
}

// requireRecordingSpeakerTieKept refuses the agent a change to a speaker's
// tie that it cannot see: a name tied to a contact in an address book the
// person did not give it is not the agent's to untie or tie elsewhere. The
// speaker is named by its identity, or by its confirmed name when that is all
// the call has; a name not yet given is free.
func (self *graph) requireRecordingSpeakerTieKept(ctx context.Context, userId, speakerIdentityId, confirmedName string) error {
	grantedAddressBookIds, err := self.agentGrantedAddressBookIds(ctx, userId)
	if err != nil || grantedAddressBookIds == nil {
		return err
	}
	identities, err := self.transaction(ctx).ListRecordingSpeakerIdentities(userId)
	if err != nil {
		return translateError(err)
	}
	confirmedName = strings.TrimSpace(confirmedName)
	for _, identity := range identities {
		isNamed := identity.ID == speakerIdentityId || (speakerIdentityId == "" && confirmedName != "" && identity.ConfirmedName == confirmedName)
		if isNamed && identity.ContactAddressBookID != "" && !grantedAddressBookIds[identity.ContactAddressBookID] {
			return api.ErrNotFound
		}
	}
	return nil
}

func (self *graph) CorrectRecordingSpeaker(ctx context.Context, arguments CorrectRecordingSpeakerArguments) (*RecordingSpeakersView, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	contactAddressBookId, contactId := strings.TrimSpace(arguments.ContactAddressBookID), strings.TrimSpace(arguments.ContactID)
	if err := self.requireRecordingSpeakerContactBook(ctx, userId, contactAddressBookId); err != nil {
		return nil, err
	}
	if contactId != "" {
		if err := self.requireRecordingSpeakerTieKept(ctx, userId, "", arguments.ConfirmedName); err != nil {
			return nil, err
		}
	}
	_, err = self.transaction(ctx).CorrectRecordingSpeaker(userId, db.RecordingSpeakerCorrectionInput{ContactAddressBookID: contactAddressBookId, ContactID: contactId, RecordingID: arguments.RecordingID, ExpectedSourceGeneration: arguments.ExpectedSourceGeneration, ClientCorrectionID: arguments.ClientCorrectionID, ConfirmedName: arguments.ConfirmedName, IsAccountOwner: arguments.IsAccountOwner, RecordingTranscriptionAttemptID: arguments.RecordingTranscriptionAttemptID, LocalSpeakerID: arguments.LocalSpeakerID, PassageStartMS: arguments.PassageStartMS, PassageEndMS: arguments.PassageEndMS})
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.transaction(ctx).QueueRecordingKnowledgeSync(userId, arguments.RecordingID); err != nil {
		return nil, translateError(err)
	}
	if err := self.queueRecordingSummaryAfterEdit(ctx, userId, arguments.RecordingID); err != nil {
		return nil, translateError(err)
	}
	self.transaction(ctx).AfterCommit(func() {
		if worker := self.agentWorker(); worker != nil {
			worker.WakeRecordingSpeakerCuration(userId, arguments.RecordingID)
		}
	})
	return self.ReadRecordingSpeakers(ctx, RecordingArguments{RecordingID: arguments.RecordingID})
}
func (self *graph) NameRecordingTemporarySpeaker(ctx context.Context, arguments NameRecordingTemporarySpeakerArguments) (bool, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return false, err
	}
	contactAddressBookId, contactId := strings.TrimSpace(arguments.ContactAddressBookID), strings.TrimSpace(arguments.ContactID)
	if err := self.requireRecordingSpeakerContactBook(ctx, userId, contactAddressBookId); err != nil {
		return false, err
	}
	if contactId != "" {
		if err := self.requireRecordingSpeakerTieKept(ctx, userId, "", arguments.ConfirmedName); err != nil {
			return false, err
		}
	}
	if err := self.transaction(ctx).NameRecordingTemporarySpeakerAsContact(userId, arguments.RecordingID, arguments.SpeakerLabel, arguments.ConfirmedName, arguments.IsAccountOwner, contactAddressBookId, contactId); err != nil {
		return false, translateError(err)
	}
	if err := self.transaction(ctx).QueueRecordingKnowledgeSync(userId, arguments.RecordingID); err != nil {
		return false, translateError(err)
	}
	if err := self.queueRecordingSummaryAfterEdit(ctx, userId, arguments.RecordingID); err != nil {
		return false, translateError(err)
	}
	self.transaction(ctx).AfterCommit(func() {
		if worker := self.agentWorker(); worker != nil {
			worker.WakeRecordingSpeakerCuration(userId, arguments.RecordingID)
		}
	})
	return true, nil
}

// SeparateRecordingTemporarySpeaker gives a label that was found to be an
// earlier speaker's voice back to itself: its passages read as its own label
// again, and anything naming the earlier speaker wrote for it is taken off.
func (self *graph) SeparateRecordingTemporarySpeaker(ctx context.Context, arguments SeparateRecordingTemporarySpeakerArguments) (bool, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return false, err
	}
	if err := self.transaction(ctx).SeparateRecordingTemporarySpeaker(userId, arguments.RecordingID, arguments.SpeakerLabel); err != nil {
		return false, translateError(err)
	}
	if err := self.transaction(ctx).QueueRecordingKnowledgeSync(userId, arguments.RecordingID); err != nil {
		return false, translateError(err)
	}
	if err := self.queueRecordingSummaryAfterEdit(ctx, userId, arguments.RecordingID); err != nil {
		return false, translateError(err)
	}
	self.transaction(ctx).AfterCommit(func() {
		if worker := self.agentWorker(); worker != nil {
			worker.WakeRecordingSpeakerCuration(userId, arguments.RecordingID)
		}
	})
	return true, nil
}
func (self *graph) RenameRecordingSpeaker(ctx context.Context, arguments RenameRecordingSpeakerArguments) (*RecordingSpeakersView, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	err = self.transaction(ctx).RenameRecordingSpeaker(userId, arguments.RecordingID, arguments.SpeakerIdentityID, arguments.ConfirmedName, arguments.ExpectedSourceGeneration)
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.queueRecordingSummaryAfterEdit(ctx, userId, arguments.RecordingID); err != nil {
		return nil, translateError(err)
	}
	self.transaction(ctx).AfterCommit(func() {
		if worker := self.agentWorker(); worker != nil {
			worker.WakeRecordingSpeakerCuration(userId, arguments.RecordingID)
		}
	})
	return self.ReadRecordingSpeakers(ctx, RecordingArguments{RecordingID: arguments.RecordingID})
}

// SetRecordingSpeakerContact ties a confirmed name to one of the person's
// contacts, or unties it when no contact is given. Only the person's own
// address books count, and for their agent only those they gave it.
func (self *graph) SetRecordingSpeakerContact(ctx context.Context, arguments SetRecordingSpeakerContactArguments) (*RecordingSpeakersView, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	addressBookId, contactId := strings.TrimSpace(arguments.AddressBookID), strings.TrimSpace(arguments.ContactID)
	if err := self.requireRecordingSpeakerContactBook(ctx, userId, addressBookId); err != nil {
		return nil, err
	}
	if err := self.requireRecordingSpeakerTieKept(ctx, userId, arguments.SpeakerIdentityID, ""); err != nil {
		return nil, err
	}
	if err := self.transaction(ctx).SetRecordingSpeakerContact(userId, arguments.RecordingID, arguments.SpeakerIdentityID, addressBookId, contactId, arguments.ExpectedSourceGeneration); err != nil {
		return nil, translateError(err)
	}
	return self.ReadRecordingSpeakers(ctx, RecordingArguments{RecordingID: arguments.RecordingID})
}
func (self *graph) RemoveRecordingSpeakerCorrection(ctx context.Context, arguments RemoveRecordingSpeakerCorrectionArguments) (*RecordingSpeakersView, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	err = self.transaction(ctx).RemoveRecordingSpeakerCorrection(userId, arguments.RecordingID, arguments.RecordingSpeakerCorrectionID, arguments.ExpectedSourceGeneration)
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.transaction(ctx).QueueRecordingKnowledgeSync(userId, arguments.RecordingID); err != nil {
		return nil, translateError(err)
	}
	if err := self.queueRecordingSummaryAfterEdit(ctx, userId, arguments.RecordingID); err != nil {
		return nil, translateError(err)
	}
	self.transaction(ctx).AfterCommit(func() {
		if worker := self.agentWorker(); worker != nil {
			worker.WakeRecordingSpeakerCuration(userId, arguments.RecordingID)
		}
	})
	return self.ReadRecordingSpeakers(ctx, RecordingArguments{RecordingID: arguments.RecordingID})
}

// UndoRecordingSpeakerNaming removes the whole naming one correction belongs
// to: every passage the same request named goes back to its speaker label.
func (self *graph) UndoRecordingSpeakerNaming(ctx context.Context, arguments RemoveRecordingSpeakerCorrectionArguments) (*RecordingSpeakersView, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := self.transaction(ctx).UndoRecordingSpeakerNaming(userId, arguments.RecordingID, arguments.RecordingSpeakerCorrectionID, arguments.ExpectedSourceGeneration); err != nil {
		return nil, translateError(err)
	}
	if err := self.transaction(ctx).QueueRecordingKnowledgeSync(userId, arguments.RecordingID); err != nil {
		return nil, translateError(err)
	}
	if err := self.queueRecordingSummaryAfterEdit(ctx, userId, arguments.RecordingID); err != nil {
		return nil, translateError(err)
	}
	self.transaction(ctx).AfterCommit(func() {
		if worker := self.agentWorker(); worker != nil {
			worker.WakeRecordingSpeakerCuration(userId, arguments.RecordingID)
		}
	})
	return self.ReadRecordingSpeakers(ctx, RecordingArguments{RecordingID: arguments.RecordingID})
}
func (self *graph) DeleteRecordingVoiceReference(ctx context.Context, arguments DeleteRecordingVoiceReferenceArguments) (*RecordingSpeakersView, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	err = self.transaction(ctx).RevokeRecordingVoiceReference(userId, arguments.RecordingID, arguments.RecordingVoiceReferenceID, arguments.ExpectedSourceGeneration)
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.transaction(ctx).QueueRecordingKnowledgeSync(userId, arguments.RecordingID); err != nil {
		return nil, translateError(err)
	}
	if err := self.queueRecordingSummaryAfterEdit(ctx, userId, arguments.RecordingID); err != nil {
		return nil, translateError(err)
	}
	self.transaction(ctx).AfterCommit(func() {
		if worker := self.agentWorker(); worker != nil {
			worker.WakeRecordingSpeakerCuration(userId, arguments.RecordingID)
		}
	})
	return self.ReadRecordingSpeakers(ctx, RecordingArguments{RecordingID: arguments.RecordingID})
}

// queueRecordingSummaryAfterEdit has the recording's summary written again a
// few minutes after the person stops changing who said what or the words, so
// it names the people they named. The edit stands whether or not that could
// be queued: a failure is logged, never answered.
func (self *graph) queueRecordingSummaryAfterEdit(ctx context.Context, userId, recordingId string) error {
	if worker := self.agentWorker(); worker != nil {
		if err := worker.QueueRecordingSummaryAfterEdit(self.transaction(ctx), userId, recordingId); err != nil {
			log.Warningf("cannot queue the summary of recording %q after an edit: %s", recordingId, err)
		}
	}
	return nil
}
