package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// The feed is on the API, the command line and the agent's feed tool under
// the same names. The agent posts, changes, withdraws and lists; choosing,
// liking, reading and discussing are the person's: a card is the person's to
// answer, and a like is what the agent learns from, so the agent does not
// press its own buttons.
func TestFeedParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.AgentFeedQuery](), reflect.TypeFor[apigraph.AgentFeedMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}
	// The subcommand of `teanode agent feed` and the action of the feed tool
	// that do each operation; empty is none, on purpose.
	table := map[string]struct {
		subcommand string
		action     string
	}{
		"ListAgentFeed":             {"list", "list"},
		"ListAgentFeedStack":        {"stack", ""},
		"GetAgentFeedItem":          {"show", ""},
		"ChooseAgentFeedItem":       {"choose", ""},
		"LikeAgentFeedItem":         {"like", ""},
		"ShowLessLikeAgentFeedItem": {"less", ""},
		"DismissAgentFeedItem":      {"dismiss", "withdraw"},
		"MarkAgentFeedRead":         {"read", ""},
		"DiscussAgentFeedItem":      {"discuss", ""},
		"PostAgentFeedItem":         {"post", "post"},
	}
	for operation := range operations {
		if _, isListed := table[operation]; !isListed {
			test.Errorf("the table does not list the operation %s", operation)
		}
	}

	commands := map[string]*cli.Command{}
	for _, subcommand := range newAgentFeedCommand().Commands {
		commands[subcommand.Name] = subcommand
	}
	actions := map[string]bool{}
	var properties map[string]any
	for _, tool := range tools.Build().All() {
		if tool.Name != "feed" {
			continue
		}
		properties, _ = tool.Parameters["properties"].(map[string]any)
		action, _ := properties["action"].(map[string]any)
		switch enum := action["enum"].(type) {
		case []string:
			for _, name := range enum {
				actions[name] = true
			}
		case []any:
			for _, name := range enum {
				actions[name.(string)] = true
			}
		}
	}
	if len(actions) == 0 {
		test.Fatal("the agent has no feed tool")
	}
	for operation, names := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		if commands[names.subcommand] == nil {
			test.Errorf("teanode agent feed has no %s, which does %s", names.subcommand, operation)
		}
		if names.action != "" && !actions[names.action] {
			test.Errorf("the feed tool has no action %s, which does %s", names.action, operation)
		}
	}
	// An action of the tool is an operation, or the update of a post by id.
	for action := range actions {
		known := action == "update" // PostAgentFeedItem with feedItemId
		for _, names := range table {
			known = known || names.action == action
		}
		if !known {
			test.Errorf("the feed tool action %s is not an operation of the API", action)
		}
	}
	// "unlike" is LikeAgentFeedItem with isLiked false.
	if commands["unlike"] == nil {
		test.Error("teanode agent feed has no unlike")
	}
	isOperationCommand := map[string]bool{"unlike": true}
	for _, names := range table {
		isOperationCommand[names.subcommand] = true
	}
	for name := range commands {
		if !isOperationCommand[name] {
			test.Errorf("teanode agent feed %s is not an operation of the API", name)
		}
	}

	// Every argument of the post operation is a flag of the post subcommand
	// and a property of the post action, under the name each surface gives
	// it, or is exempt with the reason.
	flagNames := map[string]string{
		"feedItemId": "card", "feedItemKind": "kind", "pictureSymbol": "symbol", "coalesceKey": "coalesce-key", "feedUrgency": "urgency",
		"stackKey": "stack", "stackPosition": "stack-position", "stackTitle": "stack-title", "cardLayout": "layout", "layoutContent": "layout-content",
		"title": "title", "body": "body", "factRows": "fact", "sourceLinks": "link", "choices": "choice", "media": "photo",
	}
	propertyNames := map[string]string{
		"feedItemId": "feed_item_id", "feedItemKind": "feed_item_kind", "pictureSymbol": "picture_symbol", "coalesceKey": "coalesce_key", "feedUrgency": "feed_urgency",
		"stackKey": "stack_key", "stackPosition": "stack_position", "stackTitle": "stack_title", "cardLayout": "card_layout", "layoutContent": "layout_content",
		"title": "title", "body": "body", "factRows": "fact_rows", "sourceLinks": "source_links", "choices": "choices", "recordReferences": "record_references", "media": "media",
	}
	// "<argument>.<surface>" is exempt for the reason given.
	exempt := map[string]string{
		"feedSourceKind.command":    "a card posted from the command line is the agent's own, with no source",
		"feedSourceId.command":      "a card posted from the command line is the agent's own, with no source",
		"runConversationId.command": "the command line posts outside any turn",
		"recordReferences.command":  "a record is pointed at by an agent's turn, which has the ids",
		"feedSourceKind.tool":       "read off the conversation the tool is called in",
		"feedSourceId.tool":         "read off the conversation the tool is called in",
		"runConversationId.tool":    "read off the conversation the tool is called in",
	}
	method, _ := reflect.TypeFor[apigraph.AgentFeedMutation]().MethodByName("PostAgentFeedItem")
	argumentsType := method.Type.In(1)
	flags := map[string]bool{}
	for _, flag := range commands["post"].Flags {
		for _, name := range flag.Names() {
			flags[name] = true
		}
	}
	for index := 0; index < argumentsType.NumField(); index++ {
		argument, _, _ := strings.Cut(argumentsType.Field(index).Tag.Get("json"), ",")
		if _, isExempt := exempt[argument+".command"]; !isExempt && !flags[flagNames[argument]] {
			test.Errorf("teanode agent feed post has no flag for the argument %s of PostAgentFeedItem", argument)
		}
		if _, isExempt := exempt[argument+".tool"]; !isExempt && properties[propertyNames[argument]] == nil {
			test.Errorf("the feed tool has no property for the argument %s of PostAgentFeedItem", argument)
		}
	}
	for key, reason := range exempt {
		if strings.TrimSpace(reason) == "" {
			test.Errorf("the exemption %s needs a reason", key)
		}
	}
}
