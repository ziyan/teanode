package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// The graph from a terminal.
//
// Addressed by path, as the model addresses it: `teanode agent memory get
// people/alice-chen` is the same page the agent reads, and a fact is
// cited the same way in both places. That is the point of paths -- one
// address a person, a model and a command line all use.

func newAgentGraphCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name:      "index",
			Usage:     "the pages, as a tree",
			ArgsUsage: "[path]",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.IntFlag{Name: "depth", Usage: "how many levels below the path", Value: 3},
			},
			Action: runAgentGraphIndex,
		},
		{
			Name:      "get",
			Usage:     "one page: what it says, its facts, and what it is linked to",
			ArgsUsage: "<path>",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runAgentGraphGet,
		},
		{
			Name:      "search",
			Usage:     "find pages and facts by words",
			ArgsUsage: "<words>",
			Flags:     []cli.Flag{JSONFlag(), &cli.IntFlag{Name: "first", Usage: "how many", Value: 40}},
			Action:    runAgentGraphSearch,
		},
		{
			Name:      "note",
			Usage:     "put a fact on a page; the page is made if it is missing",
			ArgsUsage: "<path> <fact | ->",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.StringFlag{Name: "kind", Usage: "fact, preference, decision, event or howto", Value: "fact"},
				&cli.StringFlag{Name: "happened", Usage: "when it was true: 2023-06, 2023-06-14"},
				&cli.StringFlag{Name: "applies-to", Usage: "which runs read it, comma-separated: triage, reply, research, summaries"},
				&cli.IntFlag{Name: "number", Usage: "change the fact with this number rather than adding one"},
			},
			Action: runAgentGraphNote,
		},
		{
			Name:      "page",
			Usage:     "write what a page says",
			ArgsUsage: "<path> <summary | ->",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.StringFlag{Name: "kind", Usage: "person, project, organization, place, thing, topic, folder or period"},
				&cli.StringFlag{Name: "name", Usage: "what it is called"},
				&cli.BoolFlag{Name: "pin", Usage: "always in the agent's prompt"},
				&cli.BoolFlag{Name: "unpin", Usage: "not always in the prompt"},
			},
			Action: runAgentGraphPage,
		},
		{
			Name:      "move",
			Usage:     "file a page under another",
			ArgsUsage: "<path> <under>",
			Action:    runAgentGraphMove,
		},
		{
			Name:      "forget",
			Usage:     "forget one fact (with --number) or a whole page and what is under it",
			ArgsUsage: "<path>",
			Flags:     []cli.Flag{&cli.IntFlag{Name: "number", Usage: "the fact's number on the page"}},
			Action:    runAgentGraphForget,
		},
		{
			Name:      "history",
			Usage:     "what has happened to a page: every change, and who made it",
			ArgsUsage: "<path>",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.IntFlag{Name: "first", Usage: "how many", Value: 50},
			},
			Action: runAgentGraphHistory,
		},
		{
			Name:  "learned",
			Usage: "what the agent has filed lately, newest first",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.IntFlag{Name: "days", Usage: "how far back", Value: 1},
				&cli.IntFlag{Name: "first", Usage: "how many", Value: 100},
			},
			Action: runAgentGraphLearned,
		},
	}
}

func newAgentKnowledgeCommand() *cli.Command {
	return &cli.Command{
		Name:  "knowledge",
		Usage: "the places your agent reads: a checkout, a chat archive, your notes",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "the sources, with how far each has got",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runKnowledgeList,
			},
			{
				Name:      "add",
				Usage:     "point the agent at a directory on one of your computers",
				ArgsUsage: "<name> <path>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "computer", Usage: "which of your computers it is on"},
					&cli.StringFlag{Name: "kind", Usage: "computer, archive or sent", Value: "computer"},
					&cli.StringFlag{Name: "format", Usage: "files, mattermost or journal", Value: "files"},
					&cli.StringFlag{Name: "under", Usage: "where in the graph what it finds is filed, such as projects"},
					&cli.StringFlag{Name: "cron", Usage: "how often to read it, five fields in your zone"},
					&cli.StringFlag{Name: "mailbox", Usage: "for a sent source: which mailbox"},
				},
				Action: runKnowledgeAdd,
			},
			{
				Name:      "pause",
				Usage:     "stop reading a source for now, keeping everything it found; resume puts it back",
				ArgsUsage: "<source-id-or-name>",
				Action:    runKnowledgePause,
			},
			{
				Name:      "resume",
				Usage:     "read a paused source again",
				ArgsUsage: "<source-id-or-name>",
				Action:    runKnowledgeResume,
			},
			{
				Name:      "sync",
				Usage:     "read a source again now",
				ArgsUsage: "<source-id-or-name>",
				Action:    runKnowledgeSync,
			},
			{
				Name:      "allow",
				Usage:     "let in a directory the scan held back because it looked like it was about other people",
				ArgsUsage: "<source-id-or-name> <directory>",
				Action:    runKnowledgeAllow,
			},
			{
				Name:      "remove",
				Usage:     "stop reading a source, and forget what it found",
				ArgsUsage: "<source-id-or-name>",
				Action:    runKnowledgeRemove,
			},
		},
	}
}

func newAgentDreamCommand() *cli.Command {
	return &cli.Command{
		Name:  "dream",
		Usage: "what your agent did overnight",
		Commands: []*cli.Command{
			{
				Name:   "log",
				Usage:  "the nights, newest first",
				Flags:  []cli.Flag{JSONFlag(), &cli.IntFlag{Name: "first", Usage: "how many", Value: 14}},
				Action: runDreamLog,
			},
			{
				Name:   "now",
				Usage:  "run the night at the next tick, within the agent's hours, instead of waiting for its turn",
				Action: runDreamNow,
			},
		},
	}
}

func runDreamNow(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DreamAgentNow(ctx, connection); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintln(command.Writer, "the night starts within the minute, if it is within your agent's hours")
	return nil
}

// --- the graph --------------------------------------------------------

func runAgentGraphIndex(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	under := command.Args().First()
	nodes, err := client.AgentGraphIndex(ctx, connection, under, 1000)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(nodes)
	}
	depth := command.Int("depth")
	rootDepth := 0
	if under != "" {
		rootDepth = strings.Count(under, "/") + 1
	}
	for _, node := range nodes {
		level := strings.Count(node.Path, "/") + 1
		if depth > 0 && level-rootDepth > depth {
			continue
		}
		indent := strings.Repeat("  ", maximum(level-rootDepth-1, 0))
		line := indent + node.Path
		if node.Name != "" && !strings.EqualFold(node.Name, lastSegmentOf(node.Path)) {
			line += " — " + node.Name
		}
		if node.Pinned {
			line += "  (pinned)"
		}
		if node.Dormant {
			line += "  (dormant)"
		}
		_, _ = fmt.Fprintln(command.Writer, line)
	}
	if len(nodes) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "nothing filed yet")
	}
	return nil
}

func runAgentGraphGet(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which page? teanode agent memory get people/alice-chen")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	page, err := client.AgentGraphPageOf(ctx, connection, command.Args().First())
	if err != nil {
		return describeError(command, err)
	}
	if page == nil || page.Node == nil {
		return fmt.Errorf("there is no page at %s", command.Args().First())
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	node := page.Node
	header := node.Path
	if node.Name != "" {
		header += " — " + node.Name
	}
	_, _ = fmt.Fprintf(command.Writer, "%s (%s)\n", header, node.Kind)
	if len(node.Aliases) > 0 {
		_, _ = fmt.Fprintf(command.Writer, "also called: %s\n", strings.Join(node.Aliases, ", "))
	}
	if page.Contact != nil {
		_, _ = fmt.Fprintf(command.Writer, "contact: %s", page.Contact.Name)
		if len(page.Contact.Emails) > 0 {
			_, _ = fmt.Fprintf(command.Writer, " <%s>", page.Contact.Emails[0])
		}
		_, _ = fmt.Fprintln(command.Writer)
	}
	if summary := strings.TrimSpace(node.Summary); summary != "" {
		_, _ = fmt.Fprintf(command.Writer, "\n%s\n", summary)
	}
	if len(page.Facts) > 0 {
		_, _ = fmt.Fprintln(command.Writer)
		for _, fact := range page.Facts {
			line := fmt.Sprintf("#%d %s", fact.Number, fact.Text)
			var notes []string
			if fact.Kind != "fact" {
				notes = append(notes, fact.Kind)
			}
			if fact.Inferred {
				notes = append(notes, "inferred")
			}
			if fact.HappenedAt != nil {
				notes = append(notes, fact.HappenedAt.Format("Jan 2006"))
			}
			if len(fact.Evidence) > 0 {
				notes = append(notes, "from "+fact.Evidence[0].Kind)
			}
			if len(notes) > 0 {
				line += "  (" + strings.Join(notes, ", ") + ")"
			}
			_, _ = fmt.Fprintln(command.Writer, line)
		}
	}
	for _, edge := range page.Edges {
		if edge.Relation == "part_of" {
			continue
		}
		if edge.FromPath == node.Path {
			_, _ = fmt.Fprintf(command.Writer, "→ %s %s\n", edge.Relation, edge.ToPath)
		} else {
			_, _ = fmt.Fprintf(command.Writer, "← %s %s this\n", edge.FromPath, edge.Relation)
		}
	}
	if len(page.Children) > 0 {
		names := make([]string, 0, len(page.Children))
		for _, child := range page.Children {
			names = append(names, lastSegmentOf(child.Path))
		}
		_, _ = fmt.Fprintf(command.Writer, "\nunder it: %s\n", strings.Join(names, ", "))
	}
	return nil
}

func runAgentGraphSearch(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("search for what? teanode agent memory search \"the boat\"")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	found, err := client.SearchAgentGraph(ctx, connection, strings.Join(command.Args().Slice(), " "), int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(found)
	}
	if found == nil || (len(found.Nodes) == 0 && len(found.Facts) == 0) {
		_, _ = fmt.Fprintln(command.Writer, "nothing about that")
		return nil
	}
	for _, node := range found.Nodes {
		line := node.Path
		if node.Name != "" {
			line += " — " + node.Name
		}
		_, _ = fmt.Fprintln(command.Writer, line)
	}
	for _, row := range found.Facts {
		_, _ = fmt.Fprintf(command.Writer, "%s#%d %s\n", row.Path, row.Fact.Number, row.Fact.Text)
	}
	return nil
}

func runAgentGraphNote(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return fmt.Errorf("give a page and the fact: teanode agent memory note people/alice-chen \"Runs the platform team\"")
	}
	text, err := readValue(command, strings.Join(command.Args().Slice()[1:], " "))
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	fields := map[string]any{
		"path": command.Args().First(),
		"text": text,
		"kind": command.String("kind"),
	}
	if happened := command.String("happened"); happened != "" {
		fields["happened"] = happened
	}
	if audiences := splitList(command.String("applies-to")); len(audiences) > 0 {
		fields["audiences"] = audiences
	}
	if number := command.Int("number"); number > 0 {
		fields["number"] = number
	}
	fact, err := client.SaveAgentFact(ctx, connection, fields)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(fact)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s#%d %s\n", command.Args().First(), fact.Number, fact.Text)
	return nil
}

func runAgentGraphPage(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which page? teanode agent memory page people/alice-chen \"Runs the platform team at Acme.\"")
	}
	summary := ""
	if command.Args().Len() > 1 {
		read, err := readValue(command, strings.Join(command.Args().Slice()[1:], " "))
		if err != nil {
			return err
		}
		summary = read
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	fields := map[string]any{"path": command.Args().First(), "summary": summary}
	if kind := command.String("kind"); kind != "" {
		fields["kind"] = kind
	}
	if name := command.String("name"); name != "" {
		fields["name"] = name
	}
	if command.Bool("pin") {
		fields["pinned"] = true
	}
	if command.Bool("unpin") {
		fields["pinned"] = false
	}
	node, err := client.SaveAgentNode(ctx, connection, fields)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(node)
	}
	_, _ = fmt.Fprintf(command.Writer, "wrote %s\n", node.Path)
	return nil
}

func runAgentGraphMove(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return fmt.Errorf("give a page and where it goes: teanode agent memory move notes/kittiwake things")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	node, err := client.MoveAgentNode(ctx, connection, command.Args().First(), command.Args().Get(1))
	if err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s is now %s\n", command.Args().First(), node.Path)
	return nil
}

func runAgentGraphForget(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("forget what? teanode agent memory forget people/alice-chen --number 2")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	path := command.Args().First()
	if number := command.Int("number"); number > 0 {
		if err := client.DeleteAgentFact(ctx, connection, path, int(number)); err != nil {
			return describeError(command, err)
		}
		_, _ = fmt.Fprintf(command.Writer, "forgot %s#%d\n", path, number)
		return nil
	}
	if err := client.DeleteAgentNode(ctx, connection, path); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "forgot %s and everything under it\n", path)
	return nil
}

// A page says what it says now; this says how it got there. The column
// that matters is who: a sentence the person wrote stands, and one a
// nightly run wrote is a summary that can be rewritten.
func runAgentGraphHistory(ctx context.Context, command *cli.Command) error {
	path := command.Args().First()
	if path == "" {
		return fmt.Errorf("which page?")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	revisions, err := client.ListAgentPageHistory(ctx, connection, path, int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(revisions)
	}
	if len(revisions) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "nothing has happened to that page yet")
		return nil
	}
	rows := make([][]string, 0, len(revisions))
	for _, revision := range revisions {
		// The words as they were, where words moved: a summary rewritten
		// overnight is the change somebody most often wants to undo.
		was := revision.Before
		if len(was) > 60 {
			was = was[:60] + "…"
		}
		rows = append(rows, []string{
			strconv.Itoa(revision.Revision),
			revision.CreatedAt.Format("2 Jan 15:04"),
			revision.Summary,
			was,
		})
	}
	return printTable([]string{"#", "when", "what happened", "was"}, rows)
}

func runAgentGraphLearned(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	facts, err := client.ListAgentLearned(ctx, connection, int(command.Int("days")), int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(facts)
	}
	if len(facts) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "nothing filed in that time")
		return nil
	}
	rows := make([][]string, 0, len(facts))
	for _, row := range facts {
		quote := ""
		if len(row.Fact.Evidence) > 0 {
			quote = row.Fact.Evidence[0].Quote
		}
		rows = append(rows, []string{
			row.Fact.CreatedAt.Format("2 Jan 15:04"),
			row.Path + "#" + strconv.Itoa(row.Fact.Number),
			row.Fact.Text,
			quote,
		})
	}
	return printTable([]string{"filed", "where", "what", "from"}, rows)
}

// --- knowledge --------------------------------------------------------

func runKnowledgeList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	sources, err := client.ListAgentKnowledgeSources(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(sources)
	}
	if len(sources) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "nothing is indexed yet. teanode agent knowledge add <name> <path> --computer <name>")
		return nil
	}
	rows := make([][]string, 0, len(sources))
	for _, source := range sources {
		state := "idle"
		switch {
		case !source.Enabled:
			state = "off"
		case source.More:
			state = "reading"
		case source.LastError != "":
			state = "waiting"
		}
		where := source.Specification.Path
		if source.Specification.Computer != "" {
			where += " on " + source.Specification.Computer
		}
		note := source.LastError
		// Whose commits it could not place comes first: it is the one
		// that makes everything downstream quietly empty, and the one a
		// person can fix in a minute by marking their own card.
		if note == "" && len(source.UnknownAuthors) > 0 {
			note = "commits by " + strings.Join(source.UnknownAuthors, ", ") +
				"; none of them is you — teanode contact me <id>"
		}
		if note == "" && len(source.Sensitive) > 0 {
			note = "held back: " + strings.Join(source.Sensitive, ", ")
		}
		rows = append(rows, []string{
			source.ID, source.Name, source.Kind, where, state,
			strconv.Itoa(source.DocumentCount), strconv.Itoa(source.ChunkCount), note,
		})
	}
	return printTable([]string{"id", "name", "kind", "where", "state", "documents", "passages", ""}, rows)
}

func runKnowledgeAdd(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("give it a name and a path: teanode agent knowledge add work ~/projects --computer laptop")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	fields := map[string]any{
		"name":   command.Args().First(),
		"kind":   command.String("kind"),
		"format": command.String("format"),
	}
	if command.Args().Len() > 1 {
		fields["path"] = command.Args().Get(1)
	}
	for flag, name := range map[string]string{
		"computer": "computer", "under": "rootPath", "cron": "cron", "mailbox": "mailboxId",
	} {
		if value := command.String(flag); value != "" {
			fields[name] = value
		}
	}
	source, err := client.SaveAgentKnowledgeSource(ctx, connection, fields)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(source)
	}
	_, _ = fmt.Fprintf(command.Writer, "indexing %s as %q; the first pass starts within the minute\n", command.Args().Get(1), source.Name)
	if source.Kind == "computer" || source.Kind == "archive" {
		_, _ = fmt.Fprintf(command.Writer, "on that computer, allow it first: teanode computer allow %s\n", command.Args().Get(1))
	}
	return nil
}

func runKnowledgeSync(ctx context.Context, command *cli.Command) error {
	connection, source, err := knowledgeSourceNamed(ctx, command)
	if err != nil {
		return err
	}
	if err := client.SyncAgentKnowledgeSource(ctx, connection, source.ID); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "reading %s again; it starts within the minute\n", source.Name)
	return nil
}

// Pausing is what a person wants when a source turns out to be far larger
// than they meant, and the only way to stop it used to be to forget
// everything it had found. Everything stays; it just is not read again
// until resumed.
func runKnowledgePause(ctx context.Context, command *cli.Command) error {
	return setKnowledgeEnabled(ctx, command, false, "paused %s; nothing it found is lost, and resume reads it again\n")
}

func runKnowledgeResume(ctx context.Context, command *cli.Command) error {
	return setKnowledgeEnabled(ctx, command, true, "reading %s again; it starts within the minute\n")
}

func setKnowledgeEnabled(ctx context.Context, command *cli.Command, enabled bool, said string) error {
	connection, source, err := knowledgeSourceNamed(ctx, command)
	if err != nil {
		return err
	}
	if _, err := client.SaveAgentKnowledgeSource(ctx, connection, map[string]any{
		"sourceId": source.ID, "enabled": enabled,
	}); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, said, source.Name)
	return nil
}

func runKnowledgeAllow(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return fmt.Errorf("give the source and the directory: teanode agent knowledge allow work evaluations")
	}
	connection, source, err := knowledgeSourceNamed(ctx, command)
	if err != nil {
		return err
	}
	updated, err := client.AllowAgentKnowledgeDirectory(ctx, connection, source.ID, command.Args().Get(1))
	if err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s will be read from now on. Allowed: %s\n",
		command.Args().Get(1), strings.Join(updated.Allowed, ", "))
	return nil
}

func runKnowledgeRemove(ctx context.Context, command *cli.Command) error {
	connection, source, err := knowledgeSourceNamed(ctx, command)
	if err != nil {
		return err
	}
	if err := client.DeleteAgentKnowledgeSource(ctx, connection, source.ID); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "stopped reading %s, and forgot what it found\n", source.Name)
	return nil
}

// knowledgeSourceNamed finds a source by its identifier or its name,
// because a person types the name and a script has the identifier.
func knowledgeSourceNamed(ctx context.Context, command *cli.Command) (*client.Client, *client.AgentKnowledgeSource, error) {
	if command.Args().Len() < 1 {
		return nil, nil, fmt.Errorf("which source? teanode agent knowledge list")
	}
	connection, err := openClient(command)
	if err != nil {
		return nil, nil, err
	}
	sources, err := client.ListAgentKnowledgeSources(ctx, connection)
	if err != nil {
		return nil, nil, describeError(command, err)
	}
	wanted := command.Args().First()
	for _, source := range sources {
		if source.ID == wanted || strings.EqualFold(source.Name, wanted) {
			return connection, source, nil
		}
	}
	return nil, nil, fmt.Errorf("there is no source %q; teanode agent knowledge list", wanted)
}

// --- the nightly run --------------------------------------------------

func runDreamLog(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	dreams, err := client.ListAgentDreams(ctx, connection, int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(dreams)
	}
	if len(dreams) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "it has not had a night yet")
		return nil
	}
	for _, dream := range dreams {
		_, _ = fmt.Fprintf(command.Writer, "%s\n", dream.StartedAt.Format("Mon 2 Jan, 15:04"))
		var parts []string
		for _, pair := range []struct {
			count int
			what  string
		}{
			{dream.Digested, "read"}, {dream.Filed, "filed"}, {dream.Rewritten, "rewritten"},
			{dream.Moved, "filed away"}, {dream.Dormant, "retired"}, {dream.Embedded, "embedded"},
			{dream.Strengthened, "links reweighted"}, {dream.Associated, "connections noticed"},
			{dream.Rehearsed, "questions rehearsed"},
			{dream.Revised, "lines an older version left"},
			{dream.Merged, "said twice, merged"},
		} {
			if pair.count > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", pair.count, pair.what))
			}
		}
		// A run that has not finished has not decided that nothing needed
		// doing; it has not got to the end of its first phase.
		switch {
		case dream.FinishedAt == nil:
			parts = append([]string{"still working"}, parts...)
		case len(parts) == 0:
			parts = []string{"nothing needed doing"}
		}
		_, _ = fmt.Fprintf(command.Writer, "  %s\n", strings.Join(parts, ", "))
		if dream.Backlog > 0 {
			nights := (dream.Backlog + 399) / 400
			_, _ = fmt.Fprintf(command.Writer, "  %d still waiting, about %d night(s) at this pace\n", dream.Backlog, nights)
		}
		if dream.Coarse {
			_, _ = fmt.Fprintln(command.Writer, "  worked a stretch at a time to keep up; a later night can go back over it")
		}
		for _, proposal := range dream.Proposals {
			switch proposal.Kind {
			case "gap":
				// Not a suggestion but a question memory could not answer,
				// which is what a night of rehearsal is for.
				_, _ = fmt.Fprintf(command.Writer, "  could not answer: %s\n", proposal.Reason)
			case "linked":
				_, _ = fmt.Fprintf(command.Writer, "  noticed: %s → %s (%s)\n", proposal.Path, proposal.To, proposal.Reason)
			default:
				_, _ = fmt.Fprintf(command.Writer, "  suggests: %s %s → %s (%s)\n", proposal.Kind, proposal.Path, proposal.To, proposal.Reason)
			}
		}
		if dream.LastError != "" {
			_, _ = fmt.Fprintf(command.Writer, "  %s\n", dream.LastError)
		}
	}
	return nil
}

// --- small helpers ----------------------------------------------------

func lastSegmentOf(path string) string {
	if index := strings.LastIndexByte(path, '/'); index >= 0 {
		return path[index+1:]
	}
	return path
}

func maximum(left, right int) int {
	if left > right {
		return left
	}
	return right
}
