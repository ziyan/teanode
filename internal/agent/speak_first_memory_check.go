package agent

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// When a memory check is worth the person's time: memory has to know
// enough about them for the questions to be about something, and checks
// come every two weeks, weekly while the set is still small.
const (
	memoryCheckFactCount          = 100
	memoryCheckApart              = 14 * 24 * time.Hour
	memoryCheckApartWhileSmall    = 7 * 24 * time.Hour
	memoryCheckSmallQuestionCount = 30
)

// memoryCheckReason is the agent asking whether it may check a few things
// it remembers about the person.
func (self *Agent) memoryCheckReason() speakFirstReason {
	return speakFirstReason{
		name:           SpeakFirstMemoryCheck,
		isDailyLimited: true,
		isDue: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, idle time.Duration, now time.Time) (bool, error) {
			if !agent.IsMemoryCheckEnabled || agent.OnboardedAt == nil {
				return false, nil
			}
			return memoryCheckDue(tx, agent, now)
		},
		checkIn: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, now time.Time) (string, error) {
			return speakFirstMessage(owner, now, "It is time for a memory check: a few questions about things you remember about them, to learn how well your memory answers.") + strings.Join([]string{
				"Call memory_check with draft (load it with tool_search first if it is not among your tools). In one short message: say you would like to check a few things you remember about them, that they can say not now, and ask the first question, written from one of the drafted facts as a question they can answer from their own life, without the answer in it. Record it with memory_check ask, giving the answer you believe, before you send the message.",
				"",
				"Then, in their turns: record each reply with memory_check record (confirmed, corrected with their answer, dropped, or unsure) and ask the next, one at a time. After three or four, ask what has changed in their life lately, and add each change they mention with memory_check add, with what used to be true as the outdated answer. Stop at about five questions, or as soon as they want to. When they correct you, or tell you something new, ask whether they want you to remember it; file it with memory only if they say yes, then call memory_check filed on that question.",
				"",
				"If they say not now, call agent_profile with not_now and stop. If they say to stop asking altogether, call agent_profile with no_more_memory_checks.",
			}, "\n"), nil
		},
	}
}

// memoryCheckDue says whether memory holds enough about the person and
// the last check was long enough ago.
func memoryCheckDue(tx db.Transaction, agent *models.Agent, now time.Time) (bool, error) {
	factCount, err := tx.CountAgentFactsToCheck(agent.ID)
	if err != nil || factCount < memoryCheckFactCount {
		return false, err
	}
	questions, err := tx.ListAgentEvaluationQuestions(agent.ID, nil)
	if err != nil {
		return false, err
	}
	if len(questions) == 0 {
		return true, nil
	}
	answeredCount := 0
	for _, question := range questions {
		if question.QuestionState.IsEvaluated() {
			answeredCount++
		}
	}
	apart := memoryCheckApart
	if answeredCount < memoryCheckSmallQuestionCount {
		apart = memoryCheckApartWhileSmall
	}
	// Newest first: the last check is when its latest question was put.
	return now.Sub(questions[0].CreatedAt) >= apart, nil
}
