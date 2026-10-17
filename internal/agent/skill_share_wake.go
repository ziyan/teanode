package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Telling the agent that made a call that waited how it came out.
//
// The call came from a turn of the grantee's agent in some conversation, and
// that turn has long ended: it was told the call was waiting and stopped. The
// owner's answer, or the day running out, wakes that conversation the way
// finished background work does (docs/decisions/
// 20260929-background-work-wakes-the-conversation-that-started-it.md): the same
// waker, the same count of woken turns before the person writes again, the same
// note instead when there are none left. The row is the record, and the wake
// claims it first so that the instance that decided the call and another
// instance's sweep never both wake the conversation.

// skillShareAnswer is a decided call with the names its telling uses.
type skillShareAnswer struct {
	call      *models.SkillShareCall
	ownerName string
	skillName string
}

// skillShareCallInFlight is the key a call is held under in the background
// in-flight set, apart from commands and work.
func skillShareCallInFlight(callId string) string {
	return "share:" + callId
}

// wakeForSkillShareCall has a decided call wake the conversation of the turn
// that made it, with whatever else ends there meanwhile.
func (self *Agent) wakeForSkillShareCall(answer *skillShareAnswer) {
	if answer == nil || answer.call == nil || answer.call.GranteeConversationID == "" {
		return
	}
	call := answer.call
	ctx, cancel := context.WithTimeout(self.ctx, jobCompletionTimeout)
	defer cancel()
	now := time.Now()
	var isClaimed bool
	var agentId string
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		conversation, err := tx.GetAgentConversation(call.GranteeConversationID)
		if err != nil || conversation == nil {
			return err
		}
		agentId = conversation.AgentID
		isClaimed, err = tx.ClaimSkillShareCallWake(call.ID, now, now.Add(-skillShareWakeClaimExpiry))
		return err
	}); err != nil {
		log.Warningf("cannot claim the wake for call %s of a shared skill; the sweep tries again: %s", call.ID, err)
		return
	}
	if !isClaimed || agentId == "" {
		return
	}
	self.backgroundMutex.Lock()
	defer self.backgroundMutex.Unlock()
	self.ensureBackgroundLocked()
	if self.backgroundInFlight[skillShareCallInFlight(call.ID)] {
		return
	}
	self.backgroundInFlight[skillShareCallInFlight(call.ID)] = true
	wake := self.backgroundWakes[call.GranteeConversationID]
	if wake == nil {
		wake = &backgroundWake{agentId: agentId}
		self.backgroundWakes[call.GranteeConversationID] = wake
	}
	wake.shareCalls = append(wake.shareCalls, answer)
	self.queueWakeLocked(call.GranteeConversationID, wake, backgroundWakeGather)
}

// markSkillShareCallsWoken records that the conversation was told.
func (self *Agent) markSkillShareCallsWoken(answers []*skillShareAnswer) {
	if len(answers) == 0 {
		return
	}
	callIds := make([]string, 0, len(answers))
	for _, answer := range answers {
		callIds = append(callIds, answer.call.ID)
	}
	// Not the server's context, which a stop has already ended.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(self.ctx), jobCompletionTimeout)
	defer cancel()
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.MarkSkillShareCallsWoken(callIds, time.Now())
	}); err != nil {
		log.Warningf("cannot mark calls %v of shared skills as told: %s", callIds, err)
	}
}

// releaseSkillShareCallWakes lets go of the claims of a wake that was given
// up, for the sweep to take again at its next pass.
func (self *Agent) releaseSkillShareCallWakes(answers []*skillShareAnswer) {
	if len(answers) == 0 {
		return
	}
	callIds := make([]string, 0, len(answers))
	for _, answer := range answers {
		callIds = append(callIds, answer.call.ID)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(self.ctx), jobCompletionTimeout)
	defer cancel()
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.ReleaseSkillShareCallWakes(callIds)
	}); err != nil {
		log.Warningf("cannot let go of the wake for calls %v of shared skills; it is taken again once it expires: %s", callIds, err)
	}
}

// wakeMessage is what the woken turn is given: the background message, with the
// answered calls after it, or the answered calls alone.
func wakeMessage(wake *backgroundWake) string {
	if len(wake.endings) == 0 && len(wake.works) == 0 && len(wake.settlings) == 0 {
		return skillShareWakeMessage(wake.shareCalls)
	}
	wokenText := backgroundWakeMessage(wake.endings, wake.works, wake.settlings)
	if len(wake.shareCalls) == 0 {
		return wokenText
	}
	return wokenText + "\n\n" + skillShareWakeMessage(wake.shareCalls)
}

// skillShareOutcome says how a call came out, in a word or two.
func skillShareOutcome(call *models.SkillShareCall) string {
	switch call.CallState {
	case models.SkillShareCallRan:
		return "approved, and it ran"
	case models.SkillShareCallFailed:
		return "approved, but it did not complete"
	case models.SkillShareCallDeclined:
		return "declined"
	case models.SkillShareCallExpired:
		return "expired: nobody answered within a day"
	}
	return string(call.CallState)
}

// skillShareAnswersLine is the answered calls in a line, for a note.
func skillShareAnswersLine(answers []*skillShareAnswer) string {
	described := make([]string, 0, len(answers))
	for _, answer := range answers {
		described = append(described, fmt.Sprintf("%s's %s (%s), %s", answer.ownerName, answer.skillName, answer.call.ToolName, skillShareOutcome(answer.call)))
	}
	return "Shared skill calls answered: " + strings.Join(described, "; ")
}

// skillShareWakeMessage is the answered calls as the woken turn reads them.
//
// It begins with the background marker, so the transcript's readers know the
// person did not write it. The owner's note and what the call answered are
// fenced as data: the note is another person's words and the answer is what
// their program printed, and neither is an instruction.
func skillShareWakeMessage(answers []*skillShareAnswer) string {
	var builder strings.Builder
	if len(answers) == 1 {
		builder.WriteString(models.BackgroundWorkMarker + " A call you made through a skill another person shared has been answered. This is not the person speaking; they may not be watching.\n")
	} else {
		fmt.Fprintf(&builder, "%s %d calls you made through skills other people shared have been answered. This is not the person speaking; they may not be watching.\n", models.BackgroundWorkMarker, len(answers))
	}
	for _, answer := range answers {
		call := answer.call
		builder.WriteString("\n")
		fmt.Fprintf(&builder, "%s call id: %s\nskill: %s's %s, tool %s\noutcome: %s\n", models.BackgroundWorkMarker, call.ID, answer.ownerName, answer.skillName, call.ToolName, skillShareOutcome(call))
		if call.AmountMinor != nil {
			fmt.Fprintf(&builder, "amount: %s\n", models.FormatMinor(*call.AmountMinor, call.CurrencyCode))
		}
		if call.DecisionNote != "" {
			builder.WriteString(answer.ownerName + "'s note:\n" + fenced(call.DecisionNote) + "\n")
		}
		if len(call.ToolOutput) > 0 {
			encoded, _ := json.Marshal(call.ToolOutput)
			builder.WriteString("what it answered:\n" + fenced(cutRunes(string(encoded), backgroundWorkResultCharacters)) + "\n")
		}
	}
	builder.WriteString("\nCarry on with what you made the call for: act on how it ended, and tell the person what came of it. A declined or expired call did not run, and nothing was paid.")
	return builder.String()
}
