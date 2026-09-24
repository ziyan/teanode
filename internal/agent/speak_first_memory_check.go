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
		canAsk:         true,
		isDue: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, idle time.Duration, now time.Time) (bool, error) {
			if !agent.IsMemoryCheckEnabled || agent.OnboardedAt == nil {
				return false, nil
			}
			return memoryCheckDue(tx, agent, now)
		},
		checkIn: func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, now time.Time, prepared map[string]string) (string, error) {
			return speakFirstMessage(owner, now, "It is time for a memory check: a few questions about things you remember about them, to learn how well your memory answers.") + askedLine(prepared) + strings.Join([]string{
				"Call memory_check with draft (load it with tool_search first if it is not among your tools). Say in one short line that you would like to check a few things you remember about them, then put them one at a time with ask_user.",
				"",
				"This is not a quiz. They should only ever have to say whether you are right, never recall anything. Put each one as what you remember and ask whether it is right and still true: \"I have that you moved to Lisbon in 2023. Is that right, and still the case?\" Skip any drafted fact that is about somebody else's work or that they would have to look up; ask about their own life, their family, their home, their things. Record each with memory_check ask before you put it: the question it answers, as a plain question (\"where do you live?\"), and the answer you believe.",
				"",
				"Put each with ask_user, the question being what you remember, and these choices, in their language: \"Yes, that's right\", \"It has changed\", \"Not sure\", \"Skip this one\", \"Stop for now\". They can also type their own answer, or choose to chat about it instead, which ends the card: then stop asking, and answer what they write next. When they say it has changed, record nothing yet: ask what it is now, with ask_user and a few likely answers as choices where you can guess them, and only then record it as corrected with what they said. If ask_user says they did not answer, stop; the check stays open and goes on when they write.",
				"",
				"Then: record each reply with memory_check record (confirmed when they say you are right, corrected with their answer, dropped when they would rather not keep it, unsure when they do not know) and put the next, one at a time. When they do not know either, do not press: record unsure and move on. After three or four, ask what has changed in their life lately, and add each change they mention with memory_check add, with what used to be true as the outdated answer. Stop at about five questions, or as soon as they want to. When they correct you, or tell you something new, ask whether they want you to remember it; file it with memory only if they say yes, then call memory_check filed on that question.",
				"",
				"\"Stop for now\", or stop, ends this check: say you will pick it up another time, and that is all. If they say not now before it starts, call agent_profile with not_now. Only if they say they never want memory checks, call agent_profile with no_more_memory_checks; when it is not clear which they mean, ask.",
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
