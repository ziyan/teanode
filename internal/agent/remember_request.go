package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// askWhatWasLearned puts the conversation to the scan model.
func (self *Agent) askWhatWasLearned(ctx context.Context, run *Run, conversation *models.AgentConversation, unread []*models.AgentMessage) (*RememberAnswer, *models.AgentConversation, error) {

	material, err := self.retrieveRememberMaterial(ctx, run.Database(), run.Agent.ID, unread)
	if err != nil {
		return nil, nil, err
	}

	prompt, err := buildRememberPrompt(run.Owner, KnowledgeLanguage(run.Agent, run.Owner), unread, material)
	if err != nil {
		return nil, nil, err
	}

	thinking, err := self.oneShot(ctx, run, fmt.Sprintf("Filing what %s taught", chatName(conversation)), prompt, models.AgentJobRemember, config.AgentWorkScan)
	if err != nil {
		return nil, nil, fmt.Errorf("asking the model: %w", err)
	}
	return parseRememberAnswer(thinking.Text), thinking.Conversation, nil
}

// transcriptFor is the conversation as the run reads it, with each
// message's identifier in front so a fact can cite the message it came
// from.
func transcriptFor(messages []*models.AgentMessage) string {
	var builder strings.Builder
	for _, message := range messages {
		who := "them"
		if message.Role == string(llm.RoleAssistant) {
			who = "you"
		}
		builder.WriteString("[" + message.ID + "] " + who + ": ")
		builder.WriteString(shownText(message))
		builder.WriteString("\n\n")
	}
	return strings.TrimSpace(builder.String())
}

// shownText is one message as the transcript shows it: cut to the bound,
// with anything that could close the block said rather than left to close
// it.
//
// One function because two callers need the same answer. What a fact may
// quote is what the model was shown, and the check held the quote against
// the whole message instead: words from past the cut, which the run never
// put in front of the model, passed as something it had been told.
func shownText(message *models.AgentMessage) string {
	return unclosable(cutRunes(message.Content, rememberMessageCharacters))
}

func buildRememberPrompt(owner *models.User, knowledgeLanguage string, unread []*models.AgentMessage, material *rememberMaterial) (string, error) {
	return render("remember.txt", map[string]any{
		"KnowledgeLanguage": languageName(knowledgeLanguage),
		"PersonName":        personName(owner),
		"Index":             material.IndexLines,
		"Pages":             material.PageBlocks,
		"Unlearned":         material.UnlearnedStatements,
		"Transcript":        transcriptFor(unread),
		"Most":              rememberFacts,
	})
}
