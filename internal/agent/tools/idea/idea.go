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
	"github.com/ziyan/teanode/internal/models"
)

// ideasShown is how many ideas a list answers with when nobody says, and
// ideasMost the most it answers with when asked. A list of every idea,
// each with its body, request and evidence, came to more than twenty
// thousand characters, which a client cut short without a word.
const (
	ideasShown = 15
	ideasMost  = 50
)

func init() {
	tools.Register(func() []*tools.Tool {
		// The areas' emoji are said once, here, rather than with every
		// list: they never change, and they were a tenth of a listing.
		areas := make([]string, 0, len(models.IdeaCategories()))
		for _, category := range models.IdeaCategories() {
			areas = append(areas, string(category.IdeaCategory)+" "+strings.Join(category.Emojis, " "))
		}
		return []*tools.Tool{
			{
				Name: "idea", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskWrite,
				Description: "Your list of ideas for the person: offers of work you can do for them, and what became of each. " +
					"`list` shows the open ones, or those in the statuses given, 15 at a time, each by its headline; a list with more ends with the offset that reads on. An expired one has an expiredReason: past_date, missing_tool (it cannot be reopened until the tool is connected) or already_used (they already do it, and may still reopen it). " +
					"`get` with idea_id gives one whole: its body, the request that starts it and the evidence. " +
					"`propose` keeps one you found in their own mail, memory or conversations, with the evidence you looked up; it is refused when it needs a tool you lack, does not say where it asks first, or has no evidence. " +
					"`start` records that this conversation carries one out, when they take it up here. " +
					"`done`, `dismiss` and `reopen` are for when they say it is finished, that they do not want it, or want it back.",
				Parameters: tools.Object(map[string]any{
					"action":            tools.EnumProperty("what to do", "list", "get", "propose", "start", "done", "dismiss", "reopen"),
					"idea_id":           tools.StringProperty("for get, start, done, dismiss and reopen: which idea, by the id list gives"),
					"statuses":          tools.ArrayProperty("for list: open, started, done, dismissed, expired; open when left out", tools.StringProperty("a status")),
					"limit":             tools.IntegerProperty("for list: how many, 15 when left out and at most 50"),
					"offset":            tools.IntegerProperty("for list: how many to pass over, to read on from where a list stopped"),
					"idea_category":     tools.EnumProperty("for propose: the area it is about", "money", "paperwork", "mail", "home", "family", "travel", "shopping", "health", "work", "fun", "assistant"),
					"emoji":             tools.StringProperty("for propose: one of the area's emoji, the area's own (the first) when left out: " + strings.Join(areas, "; ")),
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
					case "get":
						return "Read an idea"
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
	Limit            int              `json:"limit"`
	Offset           int              `json:"offset"`
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
		return listIdeas(ctx, asked)
	case "get":
		if ideaId == "" {
			return nil, fmt.Errorf("get needs idea_id")
		}
		result, err := operator.Execute(ctx, client.DocumentListAgentIdeas, map[string]any{"ideaIds": []string{ideaId}})
		if err != nil {
			return nil, err
		}
		list, _ := result["ListAgentIdeas"].(map[string]any)
		ideas, _ := list["ideas"].([]any)
		if len(ideas) == 0 {
			return nil, fmt.Errorf("there is no idea %q; list with statuses gives the ids of closed ones too", ideaId)
		}
		return tools.JSONResult(withoutEmpty(ideas[0]))
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
		return nil, fmt.Errorf("%q is not list, get, propose, start, done, dismiss or reopen", action)
	}
}

// listIdeas is a page of the ideas in the statuses asked for, each by its
// headline and what became of it, and a hint that says how many more there
// are and the offset that reads them.
func listIdeas(ctx context.Context, asked arguments) (*tools.Result, error) {
	statuses := asked.Statuses
	if len(statuses) == 0 {
		statuses = []string{"open"}
	}
	limit := asked.Limit
	if limit <= 0 {
		limit = ideasShown
	}
	limit = min(limit, ideasMost)
	offset := max(asked.Offset, 0)
	result, err := operator.Execute(ctx, client.DocumentListAgentIdeaHeadlines, map[string]any{"ideaStatuses": statuses, "limit": limit, "offset": offset})
	if err != nil {
		return nil, err
	}
	list, _ := result["ListAgentIdeas"].(map[string]any)
	ideas, _ := list["ideas"].([]any)
	totalCount, _ := list["totalCount"].(float64)
	nextOffset, _ := list["nextOffset"].(float64)
	shown := make([]any, 0, len(ideas))
	for _, idea := range ideas {
		shown = append(shown, withoutEmpty(idea))
	}
	payload := map[string]any{"ideas": shown, "totalCount": int(totalCount)}
	switch {
	case nextOffset > 0:
		payload["nextOffset"] = int(nextOffset)
		call := fmt.Sprintf("list again with offset: %d", int(nextOffset))
		if limit != ideasShown {
			call += fmt.Sprintf(" and limit: %d", limit)
		}
		if len(asked.Statuses) > 0 {
			call += " and the same statuses"
		}
		payload["hint"] = fmt.Sprintf("ideas %d to %d of %d shown; %d more, and %s reads them. get with idea_id gives one whole",
			offset+1, offset+len(ideas), int(totalCount), int(totalCount)-offset-len(ideas), call)
	case len(ideas) == 0 && offset > 0:
		payload["hint"] = fmt.Sprintf("offset %d is past the end, there are %d in all", offset, int(totalCount))
	case offset > 0:
		payload["hint"] = fmt.Sprintf("ideas %d to %d of %d shown, the last of them; get with idea_id gives one whole", offset+1, offset+len(ideas), int(totalCount))
	default:
		payload["hint"] = "every idea in those statuses is shown; get with idea_id gives one whole"
	}
	return tools.JSONResult(payload)
}

// withoutEmpty is an idea without the fields that hold nothing: a time
// that has not come, a reason it has none of, a list with nothing in it.
func withoutEmpty(idea any) any {
	fields, isObject := idea.(map[string]any)
	if !isObject {
		return idea
	}
	kept := make(map[string]any, len(fields))
	for key, field := range fields {
		switch field := field.(type) {
		case nil:
			continue
		case string:
			if field == "" {
				continue
			}
		case []any:
			if len(field) == 0 {
				continue
			}
		}
		kept[key] = field
	}
	return kept
}
