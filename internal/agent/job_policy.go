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
	if errors.Is(err, context.Canceled) {
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
