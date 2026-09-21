package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

func TestJobDeferralsKeepFailureAllowance(test *testing.T) {
	finishedAt := time.Unix(1000, 0)
	job := &models.AgentJob{Attempts: 20, FailureCount: 0}
	for claimCount := 0; claimCount < 5; claimCount++ {
		until := finishedAt.Add(time.Minute)
		outcome := outcomeForJob(job, fmt.Errorf("waiting: %w", &Deferral{Until: until, Reason: "budget"}), finishedAt)
		if outcome.JobStatus != models.AgentJobQueued || outcome.FailureCount != 0 || !outcome.NotBefore.Equal(until) {
			test.Fatalf("deferral consumed the failure allowance: %+v", outcome)
		}
		job.Attempts++
	}
	for failureCount := 1; failureCount <= len(retryLadder)+1; failureCount++ {
		outcome := outcomeForJob(job, errors.New("temporary failure"), finishedAt)
		if outcome.FailureCount != failureCount {
			test.Fatalf("failure count: %+v", outcome)
		}
		if failureCount <= len(retryLadder) {
			if outcome.JobStatus != models.AgentJobQueued || outcome.NotBefore == nil || !outcome.NotBefore.Equal(finishedAt.Add(retryLadder[failureCount-1])) {
				test.Fatalf("retry ladder: %+v", outcome)
			}
		} else if outcome.JobStatus != models.AgentJobDead {
			test.Fatalf("exhausted job: %+v", outcome)
		}
		job.FailureCount = outcome.FailureCount
	}
}

func TestShutdownDoesNotConsumeFailureAllowance(test *testing.T) {
	outcome := outcomeForJob(&models.AgentJob{FailureCount: 2}, fmt.Errorf("stopping: %w", context.Canceled), time.Now())
	if outcome.JobStatus != models.AgentJobQueued || outcome.FailureCount != 2 {
		test.Fatalf("shutdown outcome: %+v", outcome)
	}
}

func TestJobClaimsOutlastWorkAndCompletion(test *testing.T) {
	for _, jobKind := range []models.AgentJobKind{models.AgentJobNoop, models.AgentJobDream, models.AgentJobIngest} {
		if jobClaimLifetime(jobKind) <= jobTimeout(jobKind)+jobCompletionTimeout {
			test.Fatalf("claim may expire during completion: %s", jobKind)
		}
	}
}
