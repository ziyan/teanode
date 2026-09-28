package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// What an idea is held to, whoever wrote it: the catalog, the dream, or the
// agent in conversation. An idea the agent then fails at teaches the person
// that ideas are advertising, so an idea may only promise what the
// person's tools do, and says where it stops to ask.

const (
	ideaHeadlineMostRuneCount = 80
	ideaBodyMostRuneCount     = 300
	ideaRequestMostRuneCount  = 300
	ideaReasonMostRuneCount   = 200
	ideaEvidenceMostRuneCount = 120
)

// ideaAsksFirst are the words that say the agent asks before it acts: one
// of them is in the body of every idea that needs a tool acting toward
// somebody else or one that cannot be undone.
var ideaAsksFirst = []string{
	"until you say", "until you approve", "you approve", "asks you first", "ask you first",
	"check with you", "clear with you", "with your go-ahead", "for your approval", "your approval",
	"nothing is sent", "nothing is ordered", "nothing is deleted", "nothing changes",
	"stays with you", "stay with you",
}

// ideaOverpromises are words no idea may use: the agent does not know it
// will manage something every time.
var ideaOverpromises = []string{"guarantee", "always", "every time", "never miss"}

// checkIdea says what is wrong with an idea, or nothing.
func checkIdea(idea *models.AgentIdea, toolRisks map[string]tools.Risk) string {
	headline := strings.TrimSpace(idea.Headline)
	body := strings.TrimSpace(idea.Body)
	switch {
	case headline == "":
		return "an idea needs a headline"
	case utf8.RuneCountInString(headline) > ideaHeadlineMostRuneCount:
		return fmt.Sprintf("the headline is longer than %d characters", ideaHeadlineMostRuneCount)
	case body == "":
		return "an idea needs a body saying what happens"
	case utf8.RuneCountInString(body) > ideaBodyMostRuneCount:
		return fmt.Sprintf("the body is longer than %d characters", ideaBodyMostRuneCount)
	case strings.TrimSpace(idea.OpeningRequest) == "":
		return "an idea needs the request that starts it"
	case utf8.RuneCountInString(idea.OpeningRequest) > ideaRequestMostRuneCount:
		return fmt.Sprintf("the opening request is longer than %d characters", ideaRequestMostRuneCount)
	case utf8.RuneCountInString(idea.SuggestionReason) > ideaReasonMostRuneCount:
		return fmt.Sprintf("the reason is longer than %d characters", ideaReasonMostRuneCount)
	}
	category, ok := models.IdeaCategoryOf(idea.IdeaCategory)
	if !ok {
		return fmt.Sprintf("%q is not an area an idea can be in", idea.IdeaCategory)
	}
	if !slices.Contains(category.Emojis, idea.Emoji) {
		return fmt.Sprintf("%q is not one of the emoji of %s: %s", idea.Emoji, category.IdeaCategory, strings.Join(category.Emojis, " "))
	}
	// The words that promise too much, and those that say where the agent
	// asks first, are English: they hold the catalog, which is written in
	// English first. A personal idea may be in any language, and the
	// model's judgment (judgeIdea) holds it to the same two things.
	isCatalog := idea.IdeaKind == models.IdeaCatalog
	lowered := strings.ToLower(headline + " " + body)
	for _, word := range ideaOverpromises {
		if isCatalog && strings.Contains(lowered, word) {
			return fmt.Sprintf("it promises too much: %q", word)
		}
	}
	isActing := false
	for _, name := range idea.NeededToolNames {
		matched := matchingTools(name, toolRisks)
		if len(matched) == 0 {
			return fmt.Sprintf("it needs %s, which this person's agent does not have", name)
		}
		for _, tool := range matched {
			switch toolRisks[tool] {
			case tools.RiskOutward, tools.RiskDestructive:
				isActing = true
			}
		}
	}
	if isCatalog && isActing && !containsAny(lowered, ideaAsksFirst) {
		return "it needs a tool that acts toward somebody else or cannot be undone, and does not say it asks first"
	}
	for _, evidence := range idea.Evidence {
		switch evidence.EvidenceKind {
		case "message", "page", "conversation":
		default:
			return fmt.Sprintf("%q is not a kind of evidence: message, page or conversation", evidence.EvidenceKind)
		}
		if strings.TrimSpace(evidence.EvidenceID) == "" {
			return "a piece of evidence names nothing"
		}
		if utf8.RuneCountInString(evidence.EvidenceSummary) > ideaEvidenceMostRuneCount {
			return fmt.Sprintf("a piece of evidence is described in more than %d characters", ideaEvidenceMostRuneCount)
		}
	}
	if idea.IdeaKind == models.IdeaPersonal && len(idea.Evidence) == 0 {
		return "a personal idea needs the evidence that prompted it"
	}
	return ""
}

// containsAny says whether text contains any of the phrases.
func containsAny(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// checkEvidence says which of an idea's evidence does not resolve to
// something of this person's: a message in a mailbox their agent may read,
// a page of their memory, a conversation with their agent. An idea citing
// what is not there was made up.
func checkEvidence(tx db.Transaction, agent *models.Agent, owner *models.User, evidence []models.AgentIdeaEvidence) (string, error) {
	var granted map[string]bool
	for _, each := range evidence {
		id := strings.TrimSpace(each.EvidenceID)
		switch each.EvidenceKind {
		case "message":
			if granted == nil {
				mailboxes, err := tx.ListMailboxes(owner.ID)
				if err != nil {
					return "", err
				}
				granted = map[string]bool{}
				for _, mailbox := range mailboxes {
					if mailbox.Agent != nil && mailbox.Agent.Granted {
						granted[mailbox.ID] = true
					}
				}
			}
			item, err := tx.GetItem(id)
			if err != nil {
				return "", err
			}
			if item == nil {
				return fmt.Sprintf("there is no message %q", id), nil
			}
			folder, err := tx.GetFolder(item.FolderID)
			if err != nil {
				return "", err
			}
			if folder == nil || !granted[folder.MailboxID] {
				return fmt.Sprintf("message %q is not in a mailbox this agent may read", id), nil
			}
		case "page":
			node, err := tx.GetAgentNode(agent.ID, models.NormalizePath(id))
			if err != nil {
				return "", err
			}
			if node == nil {
				return fmt.Sprintf("there is no memory page %q", id), nil
			}
		case "conversation":
			conversation, err := tx.GetAgentConversation(id)
			if err != nil {
				return "", err
			}
			if conversation == nil || conversation.AgentID != agent.ID {
				return fmt.Sprintf("there is no conversation %q", id), nil
			}
		}
	}
	return "", nil
}

// ideaRepeatOverlap is how much of two headlines' words they must share to
// be one idea said twice.
const ideaRepeatOverlap = 0.6

// repeatedIdea says which idea already kept, open, taken up or dismissed,
// this one says again, if any. An expired one may come back: what was out
// of date then may be due now.
func repeatedIdea(tx db.Transaction, agent *models.Agent, idea *models.AgentIdea) (string, error) {
	kept, err := tx.ListAgentIdeas(agent.ID, []models.AgentIdeaStatus{models.IdeaOpen, models.IdeaStarted, models.IdeaDone, models.IdeaDismissed}, nil)
	if err != nil {
		return "", err
	}
	words := headlineWords(idea.Headline)
	for _, other := range kept {
		if other.IdeaKey == idea.IdeaKey {
			continue
		}
		if overlap(words, headlineWords(other.Headline)) >= ideaRepeatOverlap {
			return fmt.Sprintf("it repeats an idea already kept (%s): %q", other.IdeaStatus, other.Headline), nil
		}
	}
	return "", nil
}

// headlineWords is the words of a headline that carry its meaning.
func headlineWords(headline string) map[string]bool {
	words := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(headline), func(letter rune) bool {
		return !unicode.IsLetter(letter) && !unicode.IsDigit(letter)
	}) {
		if utf8.RuneCountInString(word) >= 4 {
			words[word] = true
		}
	}
	return words
}

// overlap is how much two sets of words share, of all the words in both.
func overlap(first, second map[string]bool) float64 {
	if len(first) == 0 || len(second) == 0 {
		return 0
	}
	shared := 0
	for word := range first {
		if second[word] {
			shared++
		}
	}
	return float64(shared) / float64(len(first)+len(second)-shared)
}

// judgeIdea asks a model whether a personal idea promises more than the
// tools it names can do, or leaves out where it stops to ask. The rules
// above catch what words can catch; "I'll negotiate the price with the
// seller" passes all of them and needs no tool anybody has.
func (self *Agent) judgeIdea(ctx context.Context, agent *models.Agent, owner *models.User, idea *models.AgentIdea, toolRisks map[string]tools.Risk) (string, error) {
	needed := []string{}
	for _, name := range idea.NeededToolNames {
		for _, tool := range matchingTools(name, toolRisks) {
			needed = append(needed, fmt.Sprintf("- %s (%s)", tool, toolRisks[tool]))
		}
	}
	evidence := []string{}
	for _, each := range idea.Evidence {
		evidence = append(evidence, "- "+each.EvidenceSummary)
	}
	prompt, err := render("idea_check.txt", map[string]any{
		"Headline": idea.Headline, "Body": idea.Body, "OpeningRequest": idea.OpeningRequest,
		"SuggestionReason": idea.SuggestionReason, "Evidence": evidence, "Tools": needed,
	})
	if err != nil {
		return "", err
	}
	judged, err := self.oneShot(ctx, self.runFor(agent, owner, nil, ""), "Checked an idea", prompt, models.AgentJobEvaluate, config.AgentWorkScan)
	if err != nil {
		return "", err
	}
	verdict := readModelAnswer[struct {
		IsHonest bool   `json:"isHonest"`
		Problem  string `json:"problem"`
	}](judged.Text, "isHonest")
	if !verdict.IsValid {
		return "the check of what it promises could not be read", nil
	}
	if !verdict.Value.IsHonest {
		return "it promises more than its tools do: " + strings.TrimSpace(verdict.Value.Problem), nil
	}
	return "", nil
}
