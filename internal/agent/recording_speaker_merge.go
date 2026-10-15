package agent

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// recordingMergeCandidateLimit is how many earlier labels one check offers the
// provider, the most it takes samples for.
const recordingMergeCandidateLimit = 4

// checkRecordingTemporarySpeakerMerges asks the provider, once for each new
// label, whether the label's voice is one the recording already has a label
// for. A piece is sent only four samples, so in a meeting of more people a
// voice whose sample was left out comes back under a fresh letter and would be
// given a label of its own. The question is the label's own sample, a clip of
// one voice already cut for sending with later pieces, with the samples of the
// earlier labels that were not sent with its piece as the voices to pick from.
// A merge needs the whole answer under one of them: a wrong merge hides a
// person behind another's label, which costs more than a label to merge by hand.
// It does nothing while transcription is unavailable or the budget is spent,
// and tries again at the next curation.
func (self *Agent) checkRecordingTemporarySpeakerMerges(ctx context.Context, userId, recordingId string, transcriptRuns []*models.RecordingTranscriptRun, files storage.StreamFiles) error {
	if self.settings.Registry == nil || len(transcriptRuns) == 0 {
		return nil
	}
	var temporarySpeakers []*models.RecordingTemporarySpeaker
	var identities []*models.RecordingSpeakerIdentity
	attemptsByID := map[string]*models.RecordingTranscriptionAttempt{}
	var segments []*models.RecordingTranscriptSegment
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		if temporarySpeakers, err = transaction.ListRecordingTemporarySpeakers(userId, recordingId); err != nil {
			return err
		}
		if identities, err = transaction.ListRecordingSpeakerIdentities(userId); err != nil {
			return err
		}
		for _, transcriptRun := range transcriptRuns {
			attempts, err := transaction.ListRecordingTranscriptionAttempts(userId, transcriptRun.ID)
			if err != nil {
				return err
			}
			for _, attempt := range attempts {
				attemptsByID[attempt.ID] = attempt
			}
			runSegments, err := transaction.ListRecordingTranscriptSegments(userId, transcriptRun.ID)
			if err != nil {
				return err
			}
			segments = append(segments, runSegments...)
		}
		// A label from a piece older than the latest transcripts is one
		// whose check could not be made when its piece was new, or one
		// that predates the check: its piece is read on its own, so it is
		// still asked about rather than left out for good.
		loadedRunIds := map[string]bool{}
		for _, transcriptRun := range transcriptRuns {
			loadedRunIds[transcriptRun.ID] = true
		}
		for _, temporarySpeaker := range temporarySpeakers {
			if !isRecordingMergeCheckDue(temporarySpeaker) || attemptsByID[temporarySpeaker.SourceAttemptID] != nil {
				continue
			}
			attempt, err := transaction.GetRecordingTranscriptionAttempt(userId, temporarySpeaker.SourceAttemptID)
			if errors.Is(err, db.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			attemptsByID[attempt.ID] = attempt
			if loadedRunIds[attempt.RecordingTranscriptRunID] {
				continue
			}
			loadedRunIds[attempt.RecordingTranscriptRunID] = true
			runSegments, err := transaction.ListRecordingTranscriptSegments(userId, attempt.RecordingTranscriptRunID)
			if err != nil {
				return err
			}
			segments = append(segments, runSegments...)
		}
		return transaction.ProjectRecordingSpeakerCorrections(userId, segments)
	}); err != nil {
		return err
	}
	var speakersToCheck []*models.RecordingTemporarySpeaker
	for _, temporarySpeaker := range temporarySpeakers {
		if isRecordingMergeCheckDue(temporarySpeaker) {
			speakersToCheck = append(speakersToCheck, temporarySpeaker)
		}
	}
	if len(speakersToCheck) == 0 {
		return nil
	}
	selection, err := self.settings.Registry.TranscribingFor(transcriptRuns[0].ProviderModelName, true, true)
	if err != nil || selection.Capabilities.MaximumSpeakerReferenceCount <= 0 {
		return nil
	}
	confirmedNames := map[string]string{}
	for _, identity := range identities {
		confirmedNames[identity.ID] = identity.ConfirmedName
	}
	// Who speaks in a piece, by the name the provider is sent for them: the
	// confirmed name, or the label for a speaker nobody has named.
	namesSpeakingByAttemptID := map[string]map[string]bool{}
	for _, segment := range segments {
		name := segment.TemporarySpeakerLabel
		if segment.EffectiveSpeakerIdentityID != "" {
			name = segment.EffectiveSpeakerName
		}
		if name == "" {
			continue
		}
		if namesSpeakingByAttemptID[segment.RecordingTranscriptionAttemptID] == nil {
			namesSpeakingByAttemptID[segment.RecordingTranscriptionAttemptID] = map[string]bool{}
		}
		namesSpeakingByAttemptID[segment.RecordingTranscriptionAttemptID][name] = true
	}
	for _, temporarySpeaker := range speakersToCheck {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		attempt := attemptsByID[temporarySpeaker.SourceAttemptID]
		if attempt == nil {
			continue
		}
		isDone, err := self.checkRecordingTemporarySpeakerMerge(ctx, userId, selection, temporarySpeaker, temporarySpeakers, attempt, namesSpeakingByAttemptID[attempt.ID], confirmedNames, files)
		if err != nil || !isDone {
			return err
		}
	}
	return nil
}

// isRecordingMergeCheckDue is a label with a sample that nobody named, not
// merged, and not yet asked about.
func isRecordingMergeCheckDue(temporarySpeaker *models.RecordingTemporarySpeaker) bool {
	return temporarySpeaker.RecordingBlobID != "" && temporarySpeaker.SpeakerIdentityID == "" && temporarySpeaker.MergedIntoTemporarySpeakerID == "" && temporarySpeaker.MergeCheckedAt == nil
}

// checkRecordingTemporarySpeakerMerge is one label's check. It returns false,
// with no error, when the check cannot be made now and the rest should wait for
// the next curation too.
func (self *Agent) checkRecordingTemporarySpeakerMerge(ctx context.Context, userId string, selection *llm.TranscriptionSelection, temporarySpeaker *models.RecordingTemporarySpeaker, temporarySpeakers []*models.RecordingTemporarySpeaker, attempt *models.RecordingTranscriptionAttempt, namesSpeaking map[string]bool, confirmedNames map[string]string, files storage.StreamFiles) (bool, error) {
	var candidates []*models.RecordingTemporarySpeaker
	for _, candidate := range temporarySpeakers {
		if candidate.ID == temporarySpeaker.ID || candidate.RecordingBlobID == "" || candidate.MergedIntoTemporarySpeakerID != "" || !candidate.CreatedAt.Before(temporarySpeaker.CreatedAt) || candidate.PassageEndMS-candidate.PassageStartMS < 2000 || candidate.PassageEndMS-candidate.PassageStartMS > 10000 {
			continue
		}
		if slices.Contains(attempt.ReferenceBlobIDs, candidate.RecordingBlobID) || namesSpeaking[recordingCandidateName(candidate, confirmedNames)] {
			continue
		}
		candidates = append(candidates, candidate)
	}
	// Whoever was heard last is the likeliest to be the voice again.
	sort.SliceStable(candidates, func(left, right int) bool { return candidates[left].LastHeardEndMS > candidates[right].LastHeardEndMS })
	var speakerReferences []llm.SpeakerReference
	candidateByName := map[string]*models.RecordingTemporarySpeaker{}
	for _, candidate := range candidates {
		if len(speakerReferences) == min(recordingMergeCandidateLimit, selection.Capabilities.MaximumSpeakerReferenceCount) {
			break
		}
		content, isUsable, err := self.readRecordingSpeakerSample(ctx, userId, candidate.RecordingBlobID, files)
		if err != nil {
			return false, err
		}
		name := recordingCandidateName(candidate, confirmedNames)
		if !isUsable || name == "" || candidateByName[name] != nil {
			continue
		}
		candidateByName[name] = candidate
		speakerReferences = append(speakerReferences, llm.SpeakerReference{SpeakerName: name, MediaContentType: "audio/wav", MediaBytes: content, DurationMS: candidate.PassageEndMS - candidate.PassageStartMS})
	}
	if len(speakerReferences) == 0 {
		_, err := self.claimRecordingTemporarySpeakerMergeCheck(ctx, userId, temporarySpeaker)
		return true, err
	}
	sample, isUsable, err := self.readRecordingSpeakerSample(ctx, userId, temporarySpeaker.RecordingBlobID, files)
	if err != nil || !isUsable {
		return true, err
	}
	var currentAgent *models.Agent
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		if currentAgent, err = transaction.GetAgentByUser(userId); err != nil || currentAgent == nil {
			return err
		}
		owner, err := transaction.GetUser(userId)
		if err != nil {
			return err
		}
		return self.recordingAuthority(transaction, &Run{Agent: currentAgent, Owner: owner}, selection.Name, true)
	}); err != nil {
		var deferral *Deferral
		if errors.As(err, &deferral) {
			return false, nil
		}
		return false, err
	}
	if currentAgent == nil {
		return false, nil
	}
	// Claimed before asking, so a curation running at the same time does not ask too.
	if isClaimed, err := self.claimRecordingTemporarySpeakerMergeCheck(ctx, userId, temporarySpeaker); err != nil || !isClaimed {
		return true, err
	}
	response, providerError := selection.Transcriber.Transcribe(ctx, &llm.TranscriptionRequest{Model: selection.Model, MediaFilename: "sample.wav", MediaContentType: "audio/wav", MediaBytes: sample, MediaDurationMS: temporarySpeaker.PassageEndMS - temporarySpeaker.PassageStartMS, IsSpeakerDiarizationRequired: true, SpeakerReferences: speakerReferences})
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if providerError != nil || response == nil {
		// An error is no evidence either way, and the label is not asked about again.
		return true, nil
	}
	var mergedInto *models.RecordingTemporarySpeaker
	for _, segment := range response.Segments {
		if strings.TrimSpace(segment.TranscriptText) == "" {
			continue
		}
		answered := candidateByName[segment.SpeakerID]
		if answered == nil || (mergedInto != nil && mergedInto != answered) {
			mergedInto = nil
			break
		}
		mergedInto = answered
	}
	return true, self.mergeRecordingTemporarySpeakerAfterCheck(ctx, userId, temporarySpeaker, mergedInto, currentAgent, selection.Name, response.TranscriptionUsage.Usage)
}

// claimRecordingTemporarySpeakerMergeCheck records that the label is being
// asked about, and says whether this call was the one to claim it: a label
// another curation claimed first is left to it.
func (self *Agent) claimRecordingTemporarySpeakerMergeCheck(ctx context.Context, userId string, temporarySpeaker *models.RecordingTemporarySpeaker) (bool, error) {
	err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		return transaction.MarkRecordingTemporarySpeakerMergeChecked(userId, temporarySpeaker.ID)
	})
	if errors.Is(err, db.ErrAlreadyExists) {
		return false, nil
	}
	return err == nil, err
}

// mergeRecordingTemporarySpeakerAfterCheck charges the check to the agent and
// merges the label when the provider named an earlier one. A person who named
// or merged the label while the provider was answering wins: the merge is then
// refused and left out.
func (self *Agent) mergeRecordingTemporarySpeakerAfterCheck(ctx context.Context, userId string, temporarySpeaker, mergedInto *models.RecordingTemporarySpeaker, currentAgent *models.Agent, modelName string, usage llm.Usage) error {
	if err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		return RecordUsageIn(transaction, currentAgent.ID, "", modelName, string(models.AgentJobRecording), usage)
	}); err != nil || mergedInto == nil {
		return err
	}
	err := self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		if err := transaction.MergeRecordingTemporarySpeaker(userId, temporarySpeaker.RecordingID, temporarySpeaker.SpeakerLabel, mergedInto.SpeakerLabel); err != nil {
			return err
		}
		if err := transaction.QueueRecordingKnowledgeSync(userId, temporarySpeaker.RecordingID); err != nil {
			return err
		}
		return self.QueueRecordingSummaryAfterEdit(transaction, userId, temporarySpeaker.RecordingID)
	})
	if errors.Is(err, db.ErrInvalidArguments) || errors.Is(err, db.ErrNotFound) {
		return nil
	}
	return err
}

// recordingCandidateName is the name the provider is sent a speaker's sample
// under, and answers with: the confirmed name, or the label before it has one.
func recordingCandidateName(temporarySpeaker *models.RecordingTemporarySpeaker, confirmedNames map[string]string) string {
	if temporarySpeaker.SpeakerIdentityID != "" {
		return confirmedNames[temporarySpeaker.SpeakerIdentityID]
	}
	return temporarySpeaker.SpeakerLabel
}
