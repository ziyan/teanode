package apigraph

import (
	"context"
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
	RenameRecordingSpeaker(context.Context, RenameRecordingSpeakerArguments) (*RecordingSpeakersView, error)
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
// Both passage bounds are -1 for the whole group.
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
}

// NameRecordingTemporarySpeakerArguments names a speaker of a recording that is
// still being captured by the label the timeline gives it, in every piece.
type NameRecordingTemporarySpeakerArguments struct {
	RecordingID    string `json:"recordingId"`
	SpeakerLabel   string `json:"speakerLabel"`
	ConfirmedName  string `json:"confirmedName"`
	IsAccountOwner bool   `json:"isAccountOwner" graphapi:"nullable"`
}

// RenameRecordingSpeakerArguments renames an existing confirmed identity.
type RenameRecordingSpeakerArguments struct {
	RecordingID              string `json:"recordingId"`
	ExpectedSourceGeneration int64  `json:"expectedSourceGeneration"`
	SpeakerIdentityID        string `json:"speakerIdentityId"`
	ConfirmedName            string `json:"confirmedName"`
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
	return &RecordingSpeakersView{SourceGeneration: source.SourceGeneration, SpeakerIdentities: identities, SpeakerCorrections: corrections, VoiceReferences: references}, nil
}
func (self *graph) CorrectRecordingSpeaker(ctx context.Context, arguments CorrectRecordingSpeakerArguments) (*RecordingSpeakersView, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	_, err = self.transaction(ctx).CorrectRecordingSpeaker(userId, db.RecordingSpeakerCorrectionInput{RecordingID: arguments.RecordingID, ExpectedSourceGeneration: arguments.ExpectedSourceGeneration, ClientCorrectionID: arguments.ClientCorrectionID, ConfirmedName: arguments.ConfirmedName, IsAccountOwner: arguments.IsAccountOwner, RecordingTranscriptionAttemptID: arguments.RecordingTranscriptionAttemptID, LocalSpeakerID: arguments.LocalSpeakerID, PassageStartMS: arguments.PassageStartMS, PassageEndMS: arguments.PassageEndMS})
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.transaction(ctx).SyncRecordingKnowledge(userId, arguments.RecordingID); err != nil {
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
	if err := self.transaction(ctx).NameRecordingTemporarySpeaker(userId, arguments.RecordingID, arguments.SpeakerLabel, arguments.ConfirmedName, arguments.IsAccountOwner); err != nil {
		return false, translateError(err)
	}
	if err := self.transaction(ctx).SyncRecordingKnowledge(userId, arguments.RecordingID); err != nil {
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
	if err := self.transaction(ctx).SyncRecordingKnowledge(userId, arguments.RecordingID); err != nil {
		return nil, translateError(err)
	}
	self.transaction(ctx).AfterCommit(func() {
		if worker := self.agentWorker(); worker != nil {
			worker.WakeRecordingSpeakerCuration(userId, arguments.RecordingID)
		}
	})
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
	if err := self.transaction(ctx).SyncRecordingKnowledge(userId, arguments.RecordingID); err != nil {
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
	if err := self.transaction(ctx).SyncRecordingKnowledge(userId, arguments.RecordingID); err != nil {
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
	if err := self.transaction(ctx).SyncRecordingKnowledge(userId, arguments.RecordingID); err != nil {
		return nil, translateError(err)
	}
	self.transaction(ctx).AfterCommit(func() {
		if worker := self.agentWorker(); worker != nil {
			worker.WakeRecordingSpeakerCuration(userId, arguments.RecordingID)
		}
	})
	return self.ReadRecordingSpeakers(ctx, RecordingArguments{RecordingID: arguments.RecordingID})
}
