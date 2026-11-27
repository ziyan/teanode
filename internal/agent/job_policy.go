package agent

import (
	"context"
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

const jobCompletionTimeout = 10 * time.Second

var retryLadder = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 4 * time.Hour}

// jobTimeout bounds the work; claim release also leaves time to record its outcome.
func jobTimeout(jobKind models.AgentJobKind) time.Duration {
	switch jobKind {
	case models.AgentJobDream:
		return dreamLongest
	case models.AgentJobIngest:
		return ingestLongest
	case models.AgentJobRecording:
		return recordingLongest
	case models.AgentJobEvaluate:
		return evaluationLongest
	case models.AgentJobCategorize:
		return categorizeLongest
	case models.AgentJobRecordingSummary:
		return recordingSummaryLongest
	case models.AgentJobJournal:
		return journalLongest
	default:
		return 10 * time.Minute
	}
}

// jobHasNoDeadline says a kind of job is not stopped at a time: background
// work, which goes on until it is done or somebody stops it, and a turn that
// speaks first, which is a conversation held at the person's pace. Such a job
// renews its claim while it runs (see keepJobClaimed) and is taken for lost
// only when it stops doing so.
func jobHasNoDeadline(jobKind models.AgentJobKind) bool {
	return jobKind == models.AgentJobBackground || jobKind == models.AgentJobSpeakFirst
}

const (
	// jobClaimRenewedEvery is how often a job with no deadline says it is
	// still running, and jobClaimLapsesAfter how long after the last time
	// it did one is put back in the queue as lost.
	jobClaimRenewedEvery = time.Minute
	jobClaimLapsesAfter  = 5 * time.Minute
)

func jobClaimLifetime(jobKind models.AgentJobKind) time.Duration {
	if jobHasNoDeadline(jobKind) {
		return jobClaimLapsesAfter
	}
	return jobTimeout(jobKind) + 5*time.Minute
}

func outcomeForJob(job *models.AgentJob, err error, finishedAt time.Time) *db.AgentJobOutcome {
	outcome := &db.AgentJobOutcome{JobStatus: models.AgentJobDone, FailureCount: job.FailureCount, FinishedAt: finishedAt}
	if err == nil {
		return outcome
	}
	outcome.ErrorMessage = err.Error()
	var deferral *Deferral
	if errors.As(err, &deferral) {
		outcome.JobStatus = models.AgentJobQueued
		outcome.ErrorMessage = deferral.Reason
		outcome.NotBefore = &deferral.Until
		return outcome
	}
	// A job that was stopped, rather than one that went wrong, goes back
	// in the queue without a mark against it.
	//
	// Cancelled means the server is going down. Deadline exceeded means
	// the job reached the bound this very package gives it, which is not
	// the job failing: it did the work it had time for and the rest is
	// still waiting. Counting it as a failure put a night that needed
	// longer than its bound on the retry ladder, and the ladder ends in
	// dead. A night whose reading was nearly done spent an hour and forty
	// minutes parked before its next attempt, and nothing could bring it
	// forward: asking for a dream now cannot make a second job while one
	// is queued, and a retry is only offered for a job already given up
	// on.
	//
	// A provider that went quiet or never finished answering is also a
	// deadline to errors.Is, but it is the provider failing: requeued with
	// no mark against it, a job against a provider that hangs would come
	// back for ever. It takes the retry ladder like any other failure.
	if !llm.IsProviderTimeout(err) && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		outcome.JobStatus = models.AgentJobQueued
		return outcome
	}
	outcome.FailureCount++
	if outcome.FailureCount > len(retryLadder) {
		outcome.JobStatus = models.AgentJobDead
	} else {
		outcome.JobStatus = models.AgentJobQueued
		retryAt := finishedAt.Add(retryLadder[outcome.FailureCount-1])
		outcome.NotBefore = &retryAt
	}
	return outcome
}
