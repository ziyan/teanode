// Package idea is the agent's list of ideas for the person, as the agent
// manages it in conversation: what is on offer, one it found for them, one
// they took up, finished or do not want. Every action calls the operation
// the dashboard's Ideas tab and the command line's agent idea call, so the
// three agree.
package idea

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
	"github.com/ziyan/teanode/internal/client"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "idea", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskWrite,
				Description: "Your list of ideas for the person: offers of work you can do for them, and what became of each. " +
					"`list` shows the open ones, or those in the statuses given; an expired one has an expiredReason: past_date, missing_tool (it cannot be reopened until the tool is connected) or already_used (they already do it, and may still reopen it). " +
					"`propose` keeps one you found in their own mail, memory or conversations, with the evidence you looked up; it is refused when it needs a tool you lack, does not say where it asks first, or has no evidence. " +
					"`start` records that this conversation carries one out, when they take it up here. " +
					"`done`, `dismiss` and `reopen` are for when they say it is finished, that they do not want it, or want it back.",
				Parameters: tools.Object(map[string]any{
					"action":            tools.EnumProperty("what to do", "list", "propose", "start", "done", "dismiss", "reopen"),
					"idea_id":           tools.StringProperty("for start, done, dismiss and reopen: which idea, by the id list gives"),
					"statuses":          tools.ArrayProperty("for list: open, started, done, dismissed, expired; open when left out", tools.StringProperty("a status")),
					"idea_category":     tools.EnumProperty("for propose: the area it is about", "money", "paperwork", "mail", "home", "family", "travel", "shopping", "health", "work", "fun", "assistant"),
					"emoji":             tools.StringProperty("for propose: one of the area's emoji, which list gives with the areas; the area's own when left out"),
					"headline":          tools.StringProperty("for propose: the offer in the first person, at most 80 characters: \"I can compare this month's charges with the last three.\""),
					"body":              tools.StringProperty("for propose: at most 300 characters: what they give, what you do, and where you stop to ask"),
					"opening_request":   tools.StringProperty("for propose: what they would say to start it, in their voice"),
					"needed_tool_names": tools.ArrayProperty("for propose: the tools it needs, by name", tools.StringProperty("a tool's name")),
					"evidence": tools.ArrayProperty("for propose: what prompted it, each a message (by item id), a memory page (by path) or a conversation (by id)", tools.Object(map[string]any{
						"evidenceKind":    tools.EnumProperty("what it is", "message", "page", "conversation"),
						"evidenceId":      tools.StringProperty("its id, or the page's path"),
						"evidenceSummary": tools.StringProperty("what it is, in a few words"),
					}, "evidenceKind", "evidenceId", "evidenceSummary")),
					"suggestion_reason": tools.StringProperty("for propose: one line saying why them"),
					"expires_on":        tools.StringProperty("for propose: the day it stops mattering, as 2006-01-02, when there is one"),
				}, "action"),
				Guidance: "idea: when the person asks what you could do for them, list their ideas before inventing any. Propose one only when you noticed something in their own data that you could take off their hands with the tools you have, and only after looking up the evidence. When they take one up in this conversation, start it before you begin; done, dismiss or reopen when they say so.",
				Preview: tools.PreviewOf(func(call arguments) string {
					switch strings.TrimSpace(call.Action) {
					case "propose":
						return "Keep an idea: " + strings.TrimSpace(call.Headline)
					case "start":
						return "Start an idea here"
					case "done":
						return "Mark an idea done"
					case "dismiss":
						return "Dismiss an idea"
					case "reopen":
						return "Open an idea again"
					}
					return "List the ideas"
				}),
				Run: run,
			},
		}
	})
}

type arguments struct {
	Action           string           `json:"action"`
	IdeaID           string           `json:"idea_id"`
	Statuses         []string         `json:"statuses"`
	IdeaCategory     string           `json:"idea_category"`
	Emoji            string           `json:"emoji"`
	Headline         string           `json:"headline"`
	Body             string           `json:"body"`
	OpeningRequest   string           `json:"opening_request"`
	NeededToolNames  []string         `json:"needed_tool_names"`
	Evidence         []map[string]any `json:"evidence"`
	SuggestionReason string           `json:"suggestion_reason"`
	ExpiresOn        string           `json:"expires_on"`
}

func run(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	asked, err := tools.DecodeArguments[arguments](call)
	if err != nil {
		return nil, err
	}
	ideaId := strings.TrimSpace(asked.IdeaID)
	switch action := strings.ToLower(strings.TrimSpace(asked.Action)); action {
	case "", "list":
		statuses := asked.Statuses
		if len(statuses) == 0 {
			statuses = []string{"open"}
		}
		result, err := operator.Execute(ctx, client.DocumentListAgentIdeas, map[string]any{"ideaStatuses": statuses})
		if err != nil {
			return nil, err
		}
		return tools.JSONResult(result["ListAgentIdeas"])
	case "propose":
		variables := map[string]any{
			"ideaCategory": asked.IdeaCategory, "emoji": asked.Emoji, "headline": asked.Headline, "body": asked.Body,
			"openingRequest": asked.OpeningRequest, "neededToolNames": asked.NeededToolNames, "evidence": asked.Evidence,
			"suggestionReason": asked.SuggestionReason, "expiresOn": asked.ExpiresOn,
		}
		if variables["evidence"] == nil {
			variables["evidence"] = []map[string]any{}
		}
		result, err := operator.Execute(ctx, client.DocumentProposeAgentIdea, variables)
		if err != nil {
			return nil, err
		}
		answer, err := tools.JSONResult(result["ProposeAgentIdea"])
		if err != nil {
			return nil, err
		}
		answer.Note = "kept the idea; it is on their Ideas tab"
		return answer, nil
	case "start":
		if ideaId == "" {
			return nil, fmt.Errorf("start needs idea_id")
		}
		current, err := tools.RunFrom(ctx)
		if err != nil {
			return nil, err
		}
		conversationId := ""
		if conversation := current.Conversation(); conversation != nil {
			conversationId = conversation.ID
		}
		result, err := operator.Execute(ctx, client.DocumentStartAgentIdea, map[string]any{"ideaId": ideaId, "conversationId": conversationId})
		if err != nil {
			return nil, err
		}
		started, _ := result["StartAgentIdea"].(map[string]any)
		answer, err := tools.JSONResult(started["idea"])
		if err != nil {
			return nil, err
		}
		answer.Note = "this conversation carries the idea out"
		return answer, nil
	case "done", "dismiss", "reopen":
		if ideaId == "" {
			return nil, fmt.Errorf("%s needs idea_id", action)
		}
		status := map[string]string{"done": "done", "dismiss": "dismissed", "reopen": "open"}[action]
		result, err := operator.Execute(ctx, client.DocumentSetAgentIdeaStatus, map[string]any{"ideaId": ideaId, "ideaStatus": status})
		if err != nil {
			return nil, err
		}
		answer, err := tools.JSONResult(result["SetAgentIdeaStatus"])
		if err != nil {
			return nil, err
		}
		answer.Note = "the idea is " + status
		return answer, nil
	default:
		return nil, fmt.Errorf("%q is not list, propose, start, done, dismiss or reopen", action)
	}
}
