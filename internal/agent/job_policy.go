package agent

import (
	"context"
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/db"
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
	case models.AgentJobEvaluate:
		return evaluationLongest
	default:
		return 10 * time.Minute
	}
}

func jobClaimLifetime(jobKind models.AgentJobKind) time.Duration {
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
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
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
