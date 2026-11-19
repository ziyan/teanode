// Package feed is the agent's feed: the cards it posts to the person for what
// they did not ask, each a headline, a few lines, sometimes a table of facts
// and buttons they answer with a tap. Every action calls the operation the
// dashboard's Feed page, the phone apps and `teanode agent feed` call, so
// they post, list and close cards the same way.
package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "feed", Family: tools.FamilyGeneral, Risk: tools.RiskWrite,
				Description: "The person's feed: cards you post for what they did not ask, newest first, which they scroll like a social feed. " +
					"`post` puts one in: a title (the headline), a few lines of body in Markdown with links, a picture_symbol (an SF Symbol name such as cloud.sun, lock.shield, newspaper), at most eight fact_rows for a short table, source_links for the pages it comes from, and a feed_urgency: now for what cannot wait and notifies their phone, today (the default), or whenever. " +
					"Give a coalesce_key to a card about a subject that changes (today's weather, a flight): a later post with the same key replaces the card instead of piling up. One card per subject. " +
					"To ask them something, add choices: each a choice_key, a choice_label (what the button says) and a choice_role of action (you carry it out when they press it), dismiss (it closes the card and nothing more) or discuss. Nothing waits on the card: carry on, and the answer reaches you as their message in the main conversation. " +
					"A card is drawn in one of four layouts, always with a plain body as well: text (the default); list for news stories, chat mentions, review requests and issues, or mail worth reading, with layout_content {listRows: [{rowTitle, rowDetail, rowMeta (the source, or the channel and time), rowSymbol, rowUrl (http or https), rowRecordReference}]}, at most 10 rows; weather for weather, with {placeName, currentTemperature, temperatureUnit (C or F), conditionText, conditionSymbol, highTemperature, lowTemperature, hourlyForecast: [{forecastHour (ISO time), forecastTemperature, forecastSymbol, precipitationChancePercent}]}, at most 12 hours; agenda for a day plan in time order, with {agendaEntries: [{startsAt, endsAt (ISO times), agendaTitle, agendaEntryKind (meeting, focus, task or break), agendaDetail, agendaRecordReference}]}. " +
					"A brief is one run that posts several cards that belong together: give them stack_title once (\"Morning brief, Wed Nov 19\") and stack_position (the summary at 0, then 1, 2, in reading order); a schedule's cards of one run stack by themselves. " +
					"`update` changes a card you posted, by feed_item_id, where it stands. `withdraw` takes one back when it no longer holds. `list` shows what is in the feed, to find an id or to see what you already said.",
				Parameters: tools.Object(map[string]any{
					"action":         tools.EnumProperty("what to do", "post", "update", "withdraw", "list"),
					"feed_item_id":   tools.StringProperty("for update and withdraw: the card, by the id list or post gave"),
					"title":          tools.StringProperty("for post: the headline, a line"),
					"body":           tools.StringProperty("for post and update: a few lines, Markdown, links allowed"),
					"picture_symbol": tools.StringProperty("an SF Symbol name for the card's small picture, such as cloud.sun, lock.shield, chart.line.uptrend.xyaxis or newspaper"),
					"feed_item_kind": tools.EnumProperty("what the card is; a decision when it has choices, an update otherwise", "update", "decision", "alert", "report", "idea", "news"),
					"feed_urgency":   tools.EnumProperty("now notifies their phone and is for what cannot wait; today is the default; whenever can sit", "now", "today", "whenever"),
					"stack_position": tools.IntegerProperty("where the card sits in its stack, the lowest first: a brief's summary is 0, then its sections 1, 2, 3 in reading order; leave out for cards ordered by when they were posted"),
					"stack_title":    tools.StringProperty("names the stack, once, on any of its cards: \"Morning brief, Wed Nov 19\""),
					"card_layout":    tools.EnumProperty("how the card is drawn; the body is still filled in", "text", "list", "weather", "agenda"),
					"layout_content": map[string]any{"type": "object", "description": "the layout's content: for list {listRows}, for weather {placeName, currentTemperature, temperatureUnit, conditionText, ...}, for agenda {agendaEntries}; see the tool's description"},
					"stack_key":      tools.StringProperty("cards of one source pile into a stack shown as one entry; a goal's or schedule's cards stack by their source on their own, so name one only to stack cards of your own posting; a card with choices that act never stacks"),
					"coalesce_key":   tools.StringProperty("a later post with the same key replaces this card; use one for a subject that changes, and never for two different things"),
					"fact_rows": tools.ArrayProperty("a short table of at most eight rows", tools.Object(map[string]any{
						"fact_label": tools.StringProperty("the row's name"),
						"fact_value": tools.StringProperty("what it is"),
					}, "fact_label", "fact_value")),
					"source_links": tools.ArrayProperty("the pages the card comes from; a page already in an open card of the last 30 days is refused, so never post the same news twice", tools.Object(map[string]any{
						"source_title": tools.StringProperty("what the page is called"),
						"source_url":   tools.StringProperty("its address"),
					}, "source_url")),
					"record_references": tools.ArrayProperty("records of theirs the card is about: a mail by item_id or thread_id", tools.Object(map[string]any{
						"item_id":   tools.StringProperty("a mail's item id"),
						"thread_id": tools.StringProperty("a mail thread's id"),
						"subject":   tools.StringProperty("its subject, for the chip"),
					})),
					"choices": tools.ArrayProperty("at most four buttons that ask them something", tools.Object(map[string]any{
						"choice_key":   tools.StringProperty("a short word naming the choice, unique on the card"),
						"choice_label": tools.StringProperty("what the button says, in their language"),
						"choice_role":  tools.EnumProperty("action: you do it when they press it; dismiss: it only closes the card; discuss: they want to talk it over", "action", "dismiss", "discuss"),
					}, "choice_key", "choice_label", "choice_role")),
					"include_dismissed": tools.BooleanProperty("for list: the dismissed cards too"),
				}, "action"),
				Guidance: "feed: post to the feed what the person did not ask for (news, a report, an idea, something they should decide) and keep the main conversation for answering them. One card per subject; `list` first when you may have posted it already. Write the card for them to read: plain words, no ids or tool names, links in the body. A choice is something you can carry out when pressed, not a way to end the card.",
				Run:      run,
			},
		}
	})
}

type factRow struct {
	FactLabel string `json:"fact_label"`
	FactValue string `json:"fact_value"`
}

type sourceLink struct {
	SourceTitle string `json:"source_title"`
	SourceURL   string `json:"source_url"`
}

type recordReference struct {
	ItemID   string `json:"item_id"`
	ThreadID string `json:"thread_id"`
	Subject  string `json:"subject"`
}

type choice struct {
	ChoiceKey   string `json:"choice_key"`
	ChoiceLabel string `json:"choice_label"`
	ChoiceRole  string `json:"choice_role"`
}

type arguments struct {
	Action           string            `json:"action"`
	FeedItemID       string            `json:"feed_item_id"`
	Title            string            `json:"title"`
	Body             string            `json:"body"`
	PictureSymbol    string            `json:"picture_symbol"`
	FeedItemKind     string            `json:"feed_item_kind"`
	FeedUrgency      string            `json:"feed_urgency"`
	CoalesceKey      string            `json:"coalesce_key"`
	StackKey         string            `json:"stack_key"`
	StackPosition    *int              `json:"stack_position"`
	StackTitle       string            `json:"stack_title"`
	CardLayout       string            `json:"card_layout"`
	LayoutContent    json.RawMessage   `json:"layout_content"`
	FactRows         []factRow         `json:"fact_rows"`
	SourceLinks      []sourceLink      `json:"source_links"`
	RecordReferences []recordReference `json:"record_references"`
	Choices          []choice          `json:"choices"`
	IsDismissedShown bool              `json:"include_dismissed"`
}

func run(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	asked, err := tools.DecodeArguments[arguments](call)
	if err != nil {
		return nil, err
	}
	current, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	action := strings.ToLower(strings.TrimSpace(asked.Action))
	feedItemId := strings.TrimSpace(asked.FeedItemID)
	switch action {
	case "list":
		return operate(ctx, client.DocumentListAgentFeed, map[string]any{"first": 30, "includeDismissed": asked.IsDismissedShown}, "ListAgentFeed", "")
	case "withdraw":
		if feedItemId == "" {
			return nil, fmt.Errorf("which card? give feed_item_id, from list")
		}
		return operate(ctx, client.DocumentDismissAgentFeedItem, map[string]any{"feedItemId": feedItemId}, "DismissAgentFeedItem", "withdrawn")
	case "post", "update":
		variables := postVariables(&asked)
		if action == "update" {
			if feedItemId == "" {
				return nil, fmt.Errorf("which card? give feed_item_id, from list")
			}
			variables["feedItemId"] = feedItemId
		} else if strings.TrimSpace(asked.Title) == "" {
			return nil, fmt.Errorf("a card needs a title, the headline")
		}
		// Where it came from, read off the turn that posts, so that the card
		// can say which goal or schedule it is and the info sheet can link there.
		if here := current.Conversation(); here != nil {
			variables["runConversationId"] = here.ID
			if action == "post" {
				switch {
				case here.IsGoal():
					variables["feedSourceKind"], variables["feedSourceId"] = string(models.FeedSourceGoal), here.ID
				case here.Kind == models.AgentConversationRun && here.JobKind == string(models.AgentJobSchedule) && here.SubjectID != "":
					variables["feedSourceKind"], variables["feedSourceId"] = string(models.FeedSourceSchedule), here.SubjectID
				case here.Kind == models.AgentConversationRun && here.JobKind == string(models.AgentJobSpeakFirst) && here.SubjectID != "":
					variables["feedSourceKind"], variables["feedSourceId"] = string(models.FeedSourceSpeakFirst), here.SubjectID
				}
			}
		}
		note := "posted to the feed"
		if action == "update" {
			note = "the card is changed"
		}
		return operate(ctx, client.DocumentPostAgentFeedItem, variables, "PostAgentFeedItem", note)
	}
	return nil, fmt.Errorf("%q is not post, update, withdraw or list", asked.Action)
}

// postVariables is what the operation takes, from what the model gave.
func postVariables(asked *arguments) map[string]any {
	variables := map[string]any{}
	for name, value := range map[string]string{
		"title": asked.Title, "body": asked.Body, "pictureSymbol": asked.PictureSymbol, "feedItemKind": asked.FeedItemKind,
		"feedUrgency": asked.FeedUrgency, "coalesceKey": asked.CoalesceKey, "stackKey": asked.StackKey, "stackTitle": asked.StackTitle, "cardLayout": asked.CardLayout,
	} {
		if strings.TrimSpace(value) != "" {
			variables[name] = value
		}
	}
	if asked.StackPosition != nil {
		variables["stackPosition"] = *asked.StackPosition
	}
	if content := strings.TrimSpace(string(asked.LayoutContent)); content != "" && content != "null" {
		variables["layoutContent"] = content
	}
	if len(asked.FactRows) > 0 {
		rows := make([]map[string]string, 0, len(asked.FactRows))
		for _, row := range asked.FactRows {
			rows = append(rows, map[string]string{"factLabel": row.FactLabel, "factValue": row.FactValue})
		}
		variables["factRows"] = rows
	}
	if len(asked.SourceLinks) > 0 {
		links := make([]map[string]string, 0, len(asked.SourceLinks))
		for _, link := range asked.SourceLinks {
			links = append(links, map[string]string{"sourceTitle": link.SourceTitle, "sourceUrl": link.SourceURL})
		}
		variables["sourceLinks"] = links
	}
	if len(asked.Choices) > 0 {
		choices := make([]map[string]string, 0, len(asked.Choices))
		for _, each := range asked.Choices {
			choices = append(choices, map[string]string{"choiceKey": each.ChoiceKey, "choiceLabel": each.ChoiceLabel, "choiceRole": each.ChoiceRole})
		}
		variables["choices"] = choices
	}
	return variables
}

// operate runs one of the feed operations as the person and answers with what
// it returned.
func operate(ctx context.Context, document string, variables map[string]any, operation, note string) (*tools.Result, error) {
	result, err := operator.Execute(ctx, document, variables)
	if err != nil {
		return nil, err
	}
	answer, err := tools.JSONResult(result[operation])
	if err != nil {
		return nil, err
	}
	answer.Note = note
	return answer, nil
}
