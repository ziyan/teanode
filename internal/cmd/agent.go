package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewAgentCommand builds "teanode agent": a person's own agent — turning it
// on, what it knows about them, which mailboxes it may reach — and, for an
// operator, everybody's agents by their tokens.
//
// Every value here goes through the API, so a change made from a shell is
// the change the Agent page would have made.
func NewAgentCommand() *cli.Command {
	return &cli.Command{
		Name:  "agent",
		Usage: "your agent: settings, sources, usage; and everybody's, for an operator",
		Commands: []*cli.Command{
			newAgentAskCommand(),
			newAgentChatCommand(),
			newAgentConversationCommand(),
			newAgentRunCommand(),
			newAgentToolsCommand(),
			newAgentMemoryCommand(),
			newAgentScheduleCommand(),
			newAgentFeedbackCommand(),
			newAgentMCPCommand(),
			newAgentChannelCommand(),
			newAgentSettingsCommand(),
			newAgentSourceCommand(),
			{
				Name:  "usage",
				Usage: "your tokens, by day, kind, mailbox or model",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "since", Usage: "from when, as a date; the last thirty days by default"},
					&cli.StringFlag{Name: "by", Usage: "day, kind, mailbox or model; the total by default"},
				},
				Action: runAgentUsage,
			},
			{
				Name:  "replies",
				Usage: "the replies your agent wrote for you: held, sent, cancelled, refused and why",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "status", Usage: "held, sent, cancelled, refused or failed; all by default"},
					&cli.StringFlag{Name: "mailbox", Usage: "the mailbox, by id; all by default"},
					&cli.IntFlag{Name: "first", Usage: "how many", Value: 50},
				},
				Action: runAgentReplies,
				Commands: []*cli.Command{
					{
						Name:      "cancel",
						Usage:     "cancel a held reply; the draft goes, nothing is sent",
						ArgsUsage: "<reply-id>",
						Flags:     []cli.Flag{JSONFlag()},
						Action:    runAgentReplyCancel,
					},
				},
			},
			{
				Name:      "draft",
				Usage:     "have your agent write a reply to a message; printed, never sent",
				ArgsUsage: "<item-id>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "say", Usage: "what the reply should do, in a line"},
				},
				Action: runAgentDraft,
			},
			newAgentAdminCommand(),
		},
	}
}

func runAgentReplies(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	page, err := client.ListAgentReplies(ctx, connection, command.String("mailbox"), command.String("status"), int(command.Int("first")), 0)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	rows := make([][]string, 0, len(page.Replies))
	for _, reply := range page.Replies {
		when := ""
		switch {
		case reply.SentAt != nil:
			when = reply.SentAt.Local().Format("2006-01-02 15:04")
		case reply.SendAfter != nil && reply.Status == "held":
			when = "sends " + reply.SendAfter.Local().Format("2006-01-02 15:04")
		}
		rows = append(rows, []string{reply.ID, reply.Status, reply.To, reply.Subject, when, reply.Reason})
	}
	if err := printTable([]string{"id", "status", "to", "subject", "when", "reason"}, rows); err != nil {
		return err
	}
	if page.Total > int64(len(page.Replies)) {
		_, _ = fmt.Fprintf(command.Writer, "%d of %d\n", len(page.Replies), page.Total)
	}
	return nil
}

func runAgentReplyCancel(ctx context.Context, command *cli.Command) error {
	replyId := command.Args().First()
	if replyId == "" {
		return fmt.Errorf("which reply? give its id, as `teanode agent replies` lists it")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	reply, err := client.CancelAgentReply(ctx, connection, replyId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(reply)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s\n", reply.Subject, reply.Status)
	return nil
}

func runAgentDraft(ctx context.Context, command *cli.Command) error {
	itemId := command.Args().First()
	if itemId == "" {
		return fmt.Errorf("which message? give the item id, as `teanode mailbox` lists it")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	draft, err := client.DraftReply(ctx, connection, itemId, command.String("say"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(draft)
	}
	_, _ = fmt.Fprintln(command.Writer, draft.Text)
	return nil
}

func newAgentSettingsCommand() *cli.Command {
	return &cli.Command{
		Name:  "settings",
		Usage: "what your agent is called and what it knows about you",
		Commands: []*cli.Command{
			{
				Name:   "show",
				Usage:  "your agent as it is set",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentSettingsShow,
			},
			{
				Name:      "set",
				Usage:     "change your agent; turns it on the first time",
				ArgsUsage: "key=value [key=value ...]",
				Description: "Keys: enabled, name, instructions, language, ask-model, confirm (comma list),\n" +
					"voice.tone (formal|neutral|casual), voice.length (short|medium|long), voice.greeting,\n" +
					"voice.signoff, notify.held-reply, notify.high-priority, notify.run-failed (off|dashboard|mail).\n" +
					"A value of \"-\" reads standard input.\n\n" +
					"  teanode agent settings set enabled=true name=Bertie\n" +
					"  teanode agent settings set instructions=- < about-me.txt",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentSettingsSet,
			},
			{
				Name:      "categories",
				Usage:     "your own categories beside the fixed ones",
				ArgsUsage: "add NAME DESCRIPTION | remove NAME",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentCategories,
			},
			{
				Name:   "forget",
				Usage:  "delete your agent and everything it learned",
				Flags:  []cli.Flag{JSONFlag(), ForceFlag()},
				Action: runAgentForget,
			},
		},
	}
}

func newAgentSourceCommand() *cli.Command {
	return &cli.Command{
		Name:  "source",
		Usage: "the mailboxes your agent may reach, and what it does in each",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "every mailbox you own, and whether the agent may reach it",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentSourceList,
			},
			{
				Name:   "grant",
				Usage:  "let the agent reach a mailbox, with a sensible policy",
				Flags:  []cli.Flag{JSONFlag(), mailboxFlag()},
				Action: runAgentSourceGrant,
			},
			{
				Name:   "revoke",
				Usage:  "stop the agent reaching a mailbox; queued work is cancelled",
				Flags:  []cli.Flag{JSONFlag(), mailboxFlag()},
				Action: runAgentSourceRevoke,
			},
			{
				Name:      "set",
				Usage:     "change what the agent does in a mailbox",
				ArgsUsage: "key=value [key=value ...]",
				Description: "Keys: triage, triage.backfill (none|recent|all), triage.reply-expectation (direct|any),\n" +
					"summaries, summaries.minimum, summaries.style (brief|detailed), draft-replies, search, research,\n" +
					"auto-reply, auto-reply.guidance, auto-reply.scope (known|everyone|list), auto-reply.allow,\n" +
					"auto-reply.never, auto-reply.categories, auto-reply.when (always|outsideHours|whenAway),\n" +
					"auto-reply.hours (09:00-17:30), auto-reply.days (1,2,3,4,5), auto-reply.hold (10m),\n" +
					"auto-reply.limit, auto-reply.quiet (7d). Lists are comma-separated; \"-\" reads standard input.\n\n" +
					"  teanode agent source set --mailbox work triage=true auto-reply=true auto-reply.scope=known",
				Flags:  []cli.Flag{JSONFlag(), mailboxFlag()},
				Action: runAgentSourceSet,
			},
		},
	}
}

func newAgentAdminCommand() *cli.Command {
	return &cli.Command{
		Name:  "admin",
		Usage: "everybody's agents, for an operator: usage, limits, switching off; needs agent:audit",
		Commands: []*cli.Command{
			{
				Name:  "usage",
				Usage: "tokens across the server, by day, kind, mailbox, model or agent",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "since", Usage: "from when, as a date; the last thirty days by default"},
					&cli.StringFlag{Name: "by", Usage: "day, kind, mailbox, model or agent; the total by default"},
				},
				Action: runAgentAdminUsage,
			},
			{
				Name:   "list",
				Usage:  "every person with an agent, their sources and today's tokens",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentAdminList,
			},
			{
				Name:      "limit",
				Usage:     "set a person's daily token budget; 0 returns them to the default",
				ArgsUsage: "<username> <tokens>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentAdminLimit,
			},
			{
				Name:      "disable",
				Usage:     "switch a person's agent off; they cannot turn it back on",
				ArgsUsage: "<username>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    func(ctx context.Context, command *cli.Command) error { return runAgentAdminSwitch(ctx, command, true) },
			},
			{
				Name:      "enable",
				Usage:     "lift the switch-off",
				ArgsUsage: "<username>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    func(ctx context.Context, command *cli.Command) error { return runAgentAdminSwitch(ctx, command, false) },
			},
			{
				Name:   "dead-letters",
				Usage:  "jobs the worker gave up on, with their errors",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentAdminDeadLetters,
			},
			{
				Name:      "retry",
				Usage:     "put a dead job back in the queue",
				ArgsUsage: "<job-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentAdminRetry,
			},
		},
	}
}

// keyValues parses "key=value" arguments; a value of "-" reads standard
// input once.
func keyValues(command *cli.Command) (map[string]string, error) {
	values := map[string]string{}
	stdinUsed := false
	for _, argument := range command.Args().Slice() {
		key, value, ok := strings.Cut(argument, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("%q is not key=value", argument)
		}
		if value == "-" {
			if stdinUsed {
				return nil, fmt.Errorf("only one value can be read from standard input")
			}
			stdinUsed = true
			content, err := io.ReadAll(os.Stdin)
			if err != nil {
				return nil, err
			}
			value = strings.TrimRight(string(content), "\n")
		}
		values[key] = value
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("nothing to set; give key=value arguments")
	}
	return values, nil
}

func parseBool(key, value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	}
	return false, fmt.Errorf("%s: %q is not true or false", key, value)
}

func commaList(value string) []string {
	var items []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	if items == nil {
		items = []string{}
	}
	return items
}

func printAgentView(command *cli.Command, view *client.AgentView) error {
	if command.Bool("json") {
		return PrintJSON(view)
	}
	if view == nil || len(view.Agent) == 0 || string(view.Agent) == "null" {
		fmt.Println("you have no agent yet; \"teanode agent settings set enabled=true\" makes one")
		return nil
	}
	var agent struct {
		Name               string     `json:"name"`
		Enabled            bool       `json:"enabled"`
		Instructions       string     `json:"instructions"`
		Language           string     `json:"language"`
		AskModel           string     `json:"askModel"`
		DailyTokens        int64      `json:"dailyTokens"`
		OperatorDisabledAt *time.Time `json:"operatorDisabledAt"`
		Confirm            []string   `json:"confirm"`
		Categories         []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(view.Agent, &agent); err != nil {
		return err
	}
	fields := [][2]string{
		{"name", agent.Name},
		{"enabled", yesNo(agent.Enabled)},
	}
	if agent.OperatorDisabledAt != nil {
		fields = append(fields, [2]string{"switched off by an operator", formatTime(agent.OperatorDisabledAt)})
	}
	fields = append(fields, [2]string{"language", view.Language}, [2]string{"time zone", view.Timezone})
	if agent.AskModel != "" {
		fields = append(fields, [2]string{"model for conversations", agent.AskModel})
	}
	if view.Budget != nil {
		limit := "unlimited"
		if view.Budget.Limit > 0 {
			limit = strconv.FormatInt(view.Budget.Limit, 10)
		}
		fields = append(fields, [2]string{"tokens today", fmt.Sprintf("%d of %s, resets %s", view.Budget.Used, limit, formatTime(&view.Budget.ResetsAt))})
	}
	for _, category := range agent.Categories {
		fields = append(fields, [2]string{"category " + category.Name, category.Description})
	}
	if len(agent.Confirm) > 0 {
		fields = append(fields, [2]string{"always confirm", strings.Join(agent.Confirm, ", ")})
	}
	if agent.Instructions != "" {
		fields = append(fields, [2]string{"instructions", truncate(agent.Instructions, 200)})
	}
	granted := 0
	for _, source := range view.Sources {
		var policy struct {
			Granted bool `json:"granted"`
		}
		if len(source.Policy) > 0 && json.Unmarshal(source.Policy, &policy) == nil && policy.Granted {
			granted++
		}
	}
	fields = append(fields, [2]string{"mailboxes it may reach", fmt.Sprintf("%d of %d", granted, len(view.Sources))})
	return printFields(fields)
}

func runAgentSettingsShow(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.ReadAgent(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	return printAgentView(command, view)
}

func runAgentSettingsSet(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	values, err := keyValues(command)
	if err != nil {
		return err
	}
	variables := map[string]any{}
	voice := map[string]any{}
	notify := map[string]any{}
	for key, value := range values {
		switch key {
		case "enabled":
			enabled, err := parseBool(key, value)
			if err != nil {
				return err
			}
			variables["enabled"] = enabled
		case "name", "instructions", "language":
			variables[key] = value
		case "ask-model":
			variables["askModel"] = value
		case "confirm":
			variables["confirm"] = commaList(value)
		case "voice.tone", "voice.length", "voice.greeting", "voice.signoff":
			voice[strings.TrimPrefix(key, "voice.")] = value
		case "notify.held-reply":
			notify["heldReply"] = value
		case "notify.high-priority":
			notify["highPriority"] = value
		case "notify.run-failed":
			notify["runFailed"] = value
		default:
			return fmt.Errorf("%q is not a key this command knows", key)
		}
	}
	if len(voice) > 0 {
		variables["voice"] = voice
	}
	if len(notify) > 0 {
		variables["notifications"] = notify
	}
	view, err := client.UpdateAgent(ctx, connection, variables)
	if err != nil {
		return describeError(command, err)
	}
	return printAgentView(command, view)
}

func runAgentCategories(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	arguments := command.Args().Slice()
	if len(arguments) < 2 {
		return fmt.Errorf("usage: categories add NAME DESCRIPTION | remove NAME")
	}
	view, err := client.ReadAgent(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	var agent struct {
		Categories []map[string]any `json:"categories"`
	}
	if len(view.Agent) > 0 {
		_ = json.Unmarshal(view.Agent, &agent)
	}
	name := strings.ToLower(strings.TrimSpace(arguments[1]))
	categories := []map[string]any{}
	for _, category := range agent.Categories {
		if existing, _ := category["name"].(string); existing != name {
			categories = append(categories, category)
		}
	}
	switch arguments[0] {
	case "add":
		categories = append(categories, map[string]any{"name": name, "description": strings.Join(arguments[2:], " ")})
	case "remove":
	default:
		return fmt.Errorf("%q is not add or remove", arguments[0])
	}
	updated, err := client.UpdateAgent(ctx, connection, map[string]any{"categories": categories})
	if err != nil {
		return describeError(command, err)
	}
	return printAgentView(command, updated)
}

func runAgentForget(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := confirm(command, "This deletes your agent and everything it learned: memories, conversations, run records, and every mailbox's insights and summaries."); err != nil {
		return err
	}
	view, err := client.UpdateAgent(ctx, connection, map[string]any{"forget": true})
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	fmt.Println("forgotten")
	return nil
}

// sourceByFlag finds the source the --mailbox flag names, by name or id, or
// the only one.
func sourceByFlag(command *cli.Command, view *client.AgentView) (*client.AgentSource, error) {
	wanted := strings.TrimSpace(command.String("mailbox"))
	if wanted == "" {
		if len(view.Sources) == 1 {
			return view.Sources[0], nil
		}
		return nil, fmt.Errorf("you have %d mailboxes; say which with --mailbox", len(view.Sources))
	}
	for _, source := range view.Sources {
		if source.MailboxID == wanted || strings.EqualFold(source.Name, wanted) {
			return source, nil
		}
	}
	return nil, fmt.Errorf("no mailbox called %q", wanted)
}

func printSources(command *cli.Command, view *client.AgentView) error {
	if command.Bool("json") {
		return PrintJSON(view.Sources)
	}
	rows := [][]string{}
	for _, source := range view.Sources {
		var policy struct {
			Granted   bool                    `json:"granted"`
			Triage    *struct{ Enabled bool } `json:"triage"`
			Summaries *struct{ Enabled bool } `json:"summaries"`
			AutoReply *struct{ Enabled bool } `json:"autoReply"`
		}
		if len(source.Policy) > 0 {
			_ = json.Unmarshal(source.Policy, &policy)
		}
		on := func(flag *struct{ Enabled bool }) string {
			return yesNo(flag != nil && flag.Enabled)
		}
		rows = append(rows, []string{source.Name, source.MailboxID, yesNo(policy.Granted), on(policy.Triage), on(policy.Summaries), on(policy.AutoReply), strings.Join(source.Addresses, ", ")})
	}
	return printTable([]string{"MAILBOX", "ID", "GRANTED", "SORTING", "SUMMARIES", "ANSWERING", "ADDRESSES"}, rows)
}

func runAgentSourceList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.ReadAgent(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	return printSources(command, view)
}

func runAgentSourceGrant(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.ReadAgent(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	source, err := sourceByFlag(command, view)
	if err != nil {
		return err
	}
	updated, err := client.GrantAgentMailbox(ctx, connection, source.MailboxID, nil)
	if err != nil {
		return describeError(command, err)
	}
	return printSources(command, updated)
}

func runAgentSourceRevoke(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.ReadAgent(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	source, err := sourceByFlag(command, view)
	if err != nil {
		return err
	}
	updated, err := client.RevokeAgentMailbox(ctx, connection, source.MailboxID)
	if err != nil {
		return describeError(command, err)
	}
	return printSources(command, updated)
}

// policyMap is a source's policy as a map, so keys can be set without the
// command line knowing the whole shape.
func policyMap(source *client.AgentSource) map[string]any {
	policy := map[string]any{}
	if len(source.Policy) > 0 && string(source.Policy) != "null" {
		_ = json.Unmarshal(source.Policy, &policy)
	}
	return policy
}

func nested(policy map[string]any, key string) map[string]any {
	if existing, ok := policy[key].(map[string]any); ok && existing != nil {
		return existing
	}
	created := map[string]any{}
	policy[key] = created
	return created
}

func runAgentSourceSet(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	values, err := keyValues(command)
	if err != nil {
		return err
	}
	view, err := client.ReadAgent(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	source, err := sourceByFlag(command, view)
	if err != nil {
		return err
	}
	policy := policyMap(source)
	for key, value := range values {
		switch key {
		case "triage", "summaries", "auto-reply":
			enabled, err := parseBool(key, value)
			if err != nil {
				return err
			}
			nested(policy, map[string]string{"triage": "triage", "summaries": "summaries", "auto-reply": "autoReply"}[key])["enabled"] = enabled
		case "draft-replies", "search", "research":
			enabled, err := parseBool(key, value)
			if err != nil {
				return err
			}
			policy[map[string]string{"draft-replies": "draftReplies", "search": "search", "research": "research"}[key]] = enabled
		case "triage.backfill":
			nested(policy, "triage")["backfill"] = value
		case "triage.reply-expectation":
			nested(policy, "triage")["replyExpectation"] = value
		case "summaries.minimum":
			minimum, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s: %q is not a number", key, value)
			}
			nested(policy, "summaries")["minimumMessages"] = minimum
		case "summaries.style":
			nested(policy, "summaries")["style"] = value
		case "auto-reply.guidance":
			nested(policy, "autoReply")["guidance"] = value
		case "auto-reply.scope":
			nested(policy, "autoReply")["scope"] = value
		case "auto-reply.when":
			nested(policy, "autoReply")["when"] = value
		case "auto-reply.allow", "auto-reply.never", "auto-reply.categories":
			nested(policy, "autoReply")[strings.TrimPrefix(key, "auto-reply.")] = commaList(value)
		case "auto-reply.hours":
			from, until, ok := strings.Cut(value, "-")
			if !ok {
				return fmt.Errorf("%s: %q is not like 09:00-17:30", key, value)
			}
			hours := nested(nested(policy, "autoReply"), "hours")
			hours["from"], hours["until"] = strings.TrimSpace(from), strings.TrimSpace(until)
			if _, ok := hours["days"]; !ok {
				hours["days"] = []int{1, 2, 3, 4, 5}
			}
		case "auto-reply.days":
			days := []int{}
			for _, item := range commaList(value) {
				day, err := strconv.Atoi(item)
				if err != nil {
					return fmt.Errorf("%s: %q is not a weekday number", key, item)
				}
				days = append(days, day)
			}
			nested(nested(policy, "autoReply"), "hours")["days"] = days
		case "auto-reply.hold":
			hold, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("%s: %q is not a duration like 10m", key, value)
			}
			nested(policy, "autoReply")["holdMinutes"] = int(hold.Minutes())
		case "auto-reply.limit":
			limit, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s: %q is not a number", key, value)
			}
			nested(policy, "autoReply")["dailyLimit"] = limit
		case "auto-reply.quiet":
			quiet, err := parseDays(value)
			if err != nil {
				return fmt.Errorf("%s: %s", key, err)
			}
			nested(policy, "autoReply")["quietDays"] = quiet
		default:
			return fmt.Errorf("%q is not a key this command knows", key)
		}
	}
	policy["granted"] = true
	updated, err := client.GrantAgentMailbox(ctx, connection, source.MailboxID, policy)
	if err != nil {
		return describeError(command, err)
	}
	return printSources(command, updated)
}

// parseDays reads "7d", "7" or "168h" as a number of days.
func parseDays(value string) (int, error) {
	value = strings.TrimSpace(value)
	if days, err := strconv.Atoi(strings.TrimSuffix(value, "d")); err == nil {
		return days, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number of days", value)
	}
	return int(duration.Hours() / 24), nil
}

func parseSince(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return &parsed, nil
		}
	}
	if duration, err := time.ParseDuration(value); err == nil {
		since := time.Now().Add(-duration)
		return &since, nil
	}
	return nil, fmt.Errorf("%q is not a date like 2026-09-01 or a duration like 72h", value)
}

func printUsage(command *cli.Command, rows []*client.AgentUsageRow, by string) error {
	if command.Bool("json") {
		return PrintJSON(rows)
	}
	table := [][]string{}
	for _, row := range rows {
		key := row.Key
		if key == "" {
			key = "total"
		}
		total := row.Totals.PromptTokens + row.Totals.CompletionTokens + row.Totals.CacheReadTokens + row.Totals.CacheWriteTokens
		table = append(table, []string{key, strconv.FormatInt(row.Totals.PromptTokens, 10), strconv.FormatInt(row.Totals.CompletionTokens, 10), strconv.FormatInt(row.Totals.CacheReadTokens, 10), strconv.FormatInt(total, 10), strconv.FormatInt(row.Totals.Calls, 10)})
	}
	header := strings.ToUpper(by)
	if header == "" {
		header = "PERIOD"
	}
	return printTable([]string{header, "PROMPT", "COMPLETION", "CACHED", "TOTAL", "CALLS"}, table)
}

func runAgentUsage(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	since, err := parseSince(command.String("since"))
	if err != nil {
		return err
	}
	rows, err := client.AgentUsage(ctx, connection, since, command.String("by"))
	if err != nil {
		return describeError(command, err)
	}
	return printUsage(command, rows, command.String("by"))
}

func runAgentAdminUsage(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	since, err := parseSince(command.String("since"))
	if err != nil {
		return err
	}
	rows, err := client.AgentServerUsage(ctx, connection, since, command.String("by"))
	if err != nil {
		return describeError(command, err)
	}
	return printUsage(command, rows, command.String("by"))
}

func printSummaries(command *cli.Command, summaries []*client.AgentSummary) error {
	if command.Bool("json") {
		return PrintJSON(summaries)
	}
	rows := [][]string{}
	for _, summary := range summaries {
		state := yesNo(summary.Enabled)
		if summary.OperatorDisabledAt != nil {
			state = "switched off"
		}
		granted := 0
		for _, source := range summary.Sources {
			var policy struct {
				Granted bool `json:"granted"`
			}
			if len(source.Policy) > 0 && json.Unmarshal(source.Policy, &policy) == nil && policy.Granted {
				granted++
			}
		}
		today, limit := "", "default"
		if summary.Today != nil {
			today = strconv.FormatInt(summary.Today.Used, 10)
			if summary.Today.Limit > 0 {
				limit = strconv.FormatInt(summary.Today.Limit, 10)
			} else {
				limit = "unlimited"
			}
		}
		rows = append(rows, []string{summary.Username, summary.Name, state, strconv.Itoa(granted), today, limit, formatTime(summary.LastRunAt), strconv.FormatInt(summary.Queued, 10), strconv.FormatInt(summary.Dead, 10)})
	}
	return printTable([]string{"USER", "AGENT", "ON", "SOURCES", "TODAY", "LIMIT", "LAST RUN", "QUEUED", "DEAD"}, rows)
}

func runAgentAdminList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	summaries, err := client.ListAgents(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	return printSummaries(command, summaries)
}

func agentByUsername(ctx context.Context, connection *client.Client, username string) (*client.AgentSummary, error) {
	summaries, err := client.ListAgents(ctx, connection)
	if err != nil {
		return nil, err
	}
	for _, summary := range summaries {
		if strings.EqualFold(summary.Username, username) || summary.AgentID == username {
			return summary, nil
		}
	}
	return nil, fmt.Errorf("no agent belongs to %q", username)
}

func runAgentAdminLimit(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if command.Args().Len() != 2 {
		return fmt.Errorf("usage: limit <username> <tokens>")
	}
	tokens, err := strconv.ParseInt(command.Args().Get(1), 10, 64)
	if err != nil {
		return fmt.Errorf("%q is not a number of tokens", command.Args().Get(1))
	}
	summary, err := agentByUsername(ctx, connection, command.Args().Get(0))
	if err != nil {
		return describeError(command, err)
	}
	updated, err := client.SetAgentLimit(ctx, connection, summary.AgentID, tokens)
	if err != nil {
		return describeError(command, err)
	}
	return printSummaries(command, []*client.AgentSummary{updated})
}

func runAgentAdminSwitch(ctx context.Context, command *cli.Command, disabled bool) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if command.Args().Len() != 1 {
		return fmt.Errorf("usage: <username>")
	}
	summary, err := agentByUsername(ctx, connection, command.Args().First())
	if err != nil {
		return describeError(command, err)
	}
	updated, err := client.SetAgentDisabled(ctx, connection, summary.AgentID, disabled)
	if err != nil {
		return describeError(command, err)
	}
	return printSummaries(command, []*client.AgentSummary{updated})
}

func runAgentAdminDeadLetters(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	jobs, err := client.ListAgentDeadLetters(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(jobs)
	}
	rows := [][]string{}
	for _, job := range jobs {
		rows = append(rows, []string{job.ID, job.Kind, job.AgentID, job.MailboxID, strconv.Itoa(job.Attempts), formatTime(job.FinishedAt), truncate(job.Error, 80)})
	}
	return printTable([]string{"JOB", "KIND", "AGENT", "MAILBOX", "ATTEMPTS", "ENDED", "ERROR"}, rows)
}

func runAgentAdminRetry(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if command.Args().Len() != 1 {
		return fmt.Errorf("usage: retry <job-id>")
	}
	job, err := client.RetryAgentJob(ctx, connection, command.Args().First())
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(job)
	}
	fmt.Printf("job %s is queued again\n", job.ID)
	return nil
}
