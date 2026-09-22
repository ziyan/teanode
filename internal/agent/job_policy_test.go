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

// A job that reached the bound this package gives it did not fail: it did
// the work it had time for, and the rest is still waiting. Counting it as a
// failure put it on the retry ladder, whose last rung is dead, and nothing
// could bring it forward: a night cannot be asked for while one is queued,
// and a retry is only offered for a job already given up on. A night that
// needed longer than its bound sat parked for an hour and forty minutes
// with two hundred and ten documents left to read.
func TestReachingTheBoundDoesNotConsumeFailureAllowance(test *testing.T) {
	outcome := outcomeForJob(&models.AgentJob{FailureCount: 3}, fmt.Errorf("the night: %w", context.DeadlineExceeded), time.Now())
	if outcome.JobStatus != models.AgentJobQueued {
		test.Fatalf("a job that ran out of time goes back in the queue: %+v", outcome)
	}
	if outcome.FailureCount != 3 {
		test.Errorf("it costs no failure allowance: %d, want 3", outcome.FailureCount)
	}
	if outcome.NotBefore != nil {
		test.Errorf("and waits for nothing: %v", outcome.NotBefore)
	}
	// A job that genuinely failed still climbs the ladder.
	failed := outcomeForJob(&models.AgentJob{FailureCount: 3}, fmt.Errorf("the model refused"), time.Now())
	if failed.FailureCount != 4 || failed.NotBefore == nil {
		test.Errorf("a real failure still waits its turn: %+v", failed)
	}
}
