package agent

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/agent/tools"
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
)

// ideaAsksFirst are the words that say the agent asks before it acts: one
// of them is in the body of every idea that needs a tool acting toward
// somebody else or one that cannot be undone.
var ideaAsksFirst = []string{
	"until you say", "until you approve", "you approve", "asks you first", "ask you first",
	"check with you", "clear with you", "with your go-ahead", "for your approval", "your approval",
	"nothing is sent", "nothing is ordered", "nothing is deleted", "nothing changes",
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
	}
	category, ok := models.IdeaCategoryOf(idea.IdeaCategory)
	if !ok {
		return fmt.Sprintf("%q is not an area an idea can be in", idea.IdeaCategory)
	}
	if !slices.Contains(category.Emojis, idea.Emoji) {
		return fmt.Sprintf("%q is not one of the emoji of %s: %s", idea.Emoji, category.IdeaCategory, strings.Join(category.Emojis, " "))
	}
	lowered := strings.ToLower(headline + " " + body)
	for _, word := range ideaOverpromises {
		if strings.Contains(lowered, word) {
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
	if isActing && !containsAny(lowered, ideaAsksFirst) {
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
