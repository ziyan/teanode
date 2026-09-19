package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/indexed"
	"github.com/ziyan/teanode/internal/agent/reading"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
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
				&cli.StringSliceFlag{Name: "alias", Usage: "another name the page is known by, so that words find it; repeatable, and what you give replaces the aliases that are there"},
				&cli.BoolFlag{Name: "pin", Usage: "always in the agent's prompt"},
				&cli.BoolFlag{Name: "unpin", Usage: "not always in the prompt"},
			},
			Action: runAgentGraphPage,
		},
		{
			Name:      "move",
			Usage:     "file a page under another, or one fact (with --number) onto another page",
			ArgsUsage: "<path> <under | to>",
			Flags:     []cli.Flag{&cli.IntFlag{Name: "number", Usage: "move the fact with this number rather than the page"}},
			Action:    runAgentGraphMove,
		},
		{
			Name:      "forget",
			Usage:     "forget one fact (with --number) or a whole page and what is under it; a whole page asks first",
			ArgsUsage: "<path>",
			Flags: []cli.Flag{
				&cli.IntFlag{Name: "number", Usage: "the fact's number on the page"},
				ForceFlag(),
			},
			Action: runAgentGraphForget,
		},
		{
			Name:      "merge",
			Usage:     "fold one page into another: its facts, links and children move over, its name becomes an alias, and it goes",
			ArgsUsage: "<path> <into>",
			Action:    runAgentGraphMerge,
		},
		{
			Name:      "link",
			Usage:     "join two pages: what the first is to the second",
			ArgsUsage: "<path> <to>",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "relation", Usage: "part_of, works_on, member_of, knows, owns, uses, located_in, related_to, decided_in or about", Value: "related_to"},
				&cli.StringFlag{Name: "note", Usage: "a few words on the link, such as 'led the controls work on it in 2024'"},
			},
			Action: runAgentGraphLink,
		},
		{
			Name:      "unlink",
			Usage:     "take a join between two pages away",
			ArgsUsage: "<path> <to>",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "relation", Usage: "which join, when there are several", Value: "related_to"},
			},
			Action: runAgentGraphUnlink,
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
			Name:      "recall",
			Usage:     "what a question would carry into a turn: the pages recall would expand and the facts on each. Nothing is said to a model, and nothing is marked as used",
			ArgsUsage: "<question>",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runAgentGraphRecall,
		},
		{
			Name:      "evaluate",
			Usage:     "replay a set of questions through recall and say which ones got the facts they needed",
			ArgsUsage: "<file>",
			Flags:     []cli.Flag{JSONFlag()},
			Action:    runAgentGraphEvaluate,
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
				Name:      "search",
				Usage:     "find passages in what has been indexed: your code, your chat, your notes",
				ArgsUsage: "<words>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.IntFlag{Name: "first", Usage: "how many passages", Value: indexed.SearchLimit},
					&cli.StringFlag{Name: "source", Usage: "narrow to one source, by name or identifier"},
				},
				Action: runKnowledgeSearch,
			},
			{
				Name:      "read",
				Usage:     "read one of those documents, by the identifier a search prints",
				ArgsUsage: "<document-id>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.IntFlag{Name: "from", Usage: "where in the document to start, in characters"},
					&cli.IntFlag{Name: "first", Usage: "how many characters", Value: indexed.ReadLimit},
				},
				Action: runKnowledgeRead,
			},
			{
				Name:      "add",
				Usage:     "point the agent at a directory on one of your computers",
				ArgsUsage: "<name> <path>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "computer", Usage: "which of your computers it is on"},
					&cli.StringFlag{Name: "kind", Usage: "computer, archive or sent", Value: "computer"},
					&cli.StringFlag{Name: "format", Usage: "files, journal or records", Value: "files"},
					&cli.StringFlag{Name: "under", Usage: "where in the graph what it finds is filed, such as projects"},
					&cli.StringFlag{Name: "cron", Usage: "how often to read it, five fields in your zone"},
					&cli.StringFlag{Name: "mailbox", Usage: "for a sent source: which mailbox"},
				},
				Action: runKnowledgeAdd,
			},
			{
				Name:      "set",
				Usage:     "change a source that already exists, leaving everything you do not give alone",
				ArgsUsage: "<source-id-or-name>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "name", Usage: "what to call it"},
					&cli.StringFlag{Name: "path", Usage: "the directory it reads"},
					&cli.StringFlag{Name: "under", Usage: "where in the graph what it finds is filed, such as projects"},
					&cli.StringFlag{Name: "cron", Usage: "how often to read it, five fields in your zone"},
					&cli.StringFlag{Name: "format", Usage: "files, journal or records"},
					&cli.StringFlag{Name: "mailbox", Usage: "for a sent source: which mailbox"},
					&cli.BoolFlag{Name: "read-every-checkout", Usage: "read the files of checkouts you have never committed to, not only their profile"},
					&cli.IntFlag{Name: "commits-per-pass", Usage: "how many commits one pass over the tree carries, shared among its checkouts; 0 is the program's own pace"},
				},
				Action: runKnowledgeSet,
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
				Name:      "remove",
				Usage:     "stop reading a source, and forget what it found; asks first",
				ArgsUsage: "<source-id-or-name>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runKnowledgeRemove,
			},
		},
	}
}

func newAgentDreamCommand() *cli.Command {
	return &cli.Command{
		Name:  "dream",
		Usage: "what your agent dreams: the runs that read, file and rehearse, and when",
		Commands: []*cli.Command{
			{
				Name:   "log",
				Usage:  "the dreams, newest first",
				Flags:  []cli.Flag{JSONFlag(), &cli.IntFlag{Name: "first", Usage: "how many", Value: 14}},
				Action: runDreamLog,
			},
			{
				Name:      "runs",
				Usage:     "the runs one dream made, newest first: every call it made to a model, each openable with 'agent run show'",
				ArgsUsage: "<dream id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runDreamRuns,
			},
			{
				Name:   "reread",
				Usage:  "put back into the queue what a dream marked read in the last so many minutes, for a dream that marked what it never read",
				Flags:  []cli.Flag{&cli.IntFlag{Name: "minutes", Usage: "how far back", Value: 60}},
				Action: runDreamReread,
			},
			{
				Name:   "now",
				Usage:  "dream at the next tick, within the agent's hours, instead of waiting for its turn",
				Action: runDreamNow,
			},
			{
				Name:      "bootstrap",
				Usage:     "switch bootstrapping on or off: a dream at every tick with wider limits until nothing waits to be read, for a first ingest -- best with a model of your own doing the reading",
				ArgsUsage: "on|off",
				Action:    runDreamBootstrap,
			},
			{
				Name:   "progress",
				Usage:  "how far the reading has got: what is read, what waits, and how long the rest takes at the pace of the last dreams",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runDreamProgress,
			},
		},
	}
}

func runDreamReread(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	put, err := client.RereadAgentDocuments(ctx, connection, int(command.Int("minutes")))
	if err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%d document(s) are waiting to be read again\n", put)
	return nil
}

func runDreamBootstrap(ctx context.Context, command *cli.Command) error {
	var on bool
	switch command.Args().First() {
	case "on":
		on = true
	case "off":
	default:
		return fmt.Errorf("on or off? teanode agent dream bootstrap on")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DreamAgentNow(ctx, connection, &on); err != nil {
		return describeError(command, err)
	}
	if on {
		_, _ = fmt.Fprintln(command.Writer, "bootstrapping: a dream starts within the minute and runs again until nothing waits to be read; it switches itself off then")
	} else {
		_, _ = fmt.Fprintln(command.Writer, "dreaming is back to its hours")
	}
	return nil
}

func runDreamNow(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DreamAgentNow(ctx, connection, nil); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintln(command.Writer, "the dream starts within the minute, if it is within your agent's hours")
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
		// A link the night guessed from a walk reads as a guess here as
		// well as in the agent's own words, so somebody reading a page
		// knows which lines nobody has confirmed.
		relation, guess := edge.Relation, ""
		if edge.Status == string(models.EdgeProposed) {
			relation, guess = "perhaps "+relation, " (the agent's guess)"
		}
		if edge.FromPath == node.Path {
			_, _ = fmt.Fprintf(command.Writer, "→ %s %s%s\n", relation, edge.ToPath, guess)
		} else {
			_, _ = fmt.Fprintf(command.Writer, "← %s %s this%s\n", edge.FromPath, relation, guess)
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
	// Aliases are sent only when the flag was given: the server replaces
	// them with whatever arrives and leaves them alone when nothing does,
	// so sending an empty list on every write would drop the names a
	// merge folded into the page.
	if command.IsSet("alias") {
		fields["aliases"] = command.StringSlice("alias")
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
	path, to := command.Args().First(), command.Args().Get(1)
	// With a number it is one sentence that is on the wrong page, not
	// the page itself; the fact takes a new number where it lands,
	// because numbers belong to the page.
	if number := command.Int("number"); number > 0 {
		fact, err := client.MoveAgentFact(ctx, connection, path, int(number), to)
		if err != nil {
			return describeError(command, err)
		}
		_, _ = fmt.Fprintf(command.Writer, "%s#%d is now %s#%d\n", path, number, to, fact.Number)
		return nil
	}
	node, err := client.MoveAgentNode(ctx, connection, path, to)
	if err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s is now %s\n", path, node.Path)
	return nil
}

func runAgentGraphMerge(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return fmt.Errorf("merge what into what? teanode agent memory merge people/ziyan self")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	into, err := client.MergeAgentNodes(ctx, connection, command.Args().First(), command.Args().Get(1))
	if err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s is now part of %s\n", command.Args().First(), into.Path)
	return nil
}

func runAgentGraphLink(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return fmt.Errorf("link what to what? teanode agent memory link people/alice-chen projects/portal --relation works_on")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	from, to := command.Args().First(), command.Args().Get(1)
	if err := client.LinkAgentNodes(ctx, connection, from, to, command.String("relation"), command.String("note")); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s %s %s\n", from, command.String("relation"), to)
	return nil
}

func runAgentGraphUnlink(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return fmt.Errorf("unlink what from what? teanode agent memory unlink people/alice-chen projects/portal --relation works_on")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	from, to := command.Args().First(), command.Args().Get(1)
	if err := client.UnlinkAgentNodes(ctx, connection, from, to, command.String("relation")); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s and %s are no longer joined by %s\n", from, to, command.String("relation"))
	return nil
}

func runAgentGraphForget(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("forget what? teanode agent memory forget people/alice-chen --number 2")
	}
	path := command.Args().First()
	number := int(command.Int("number"))
	// Without a number the whole subtree goes, and none of it is
	// recoverable: the page, every fact on it, and whatever was filed
	// underneath -- which for a root is most of what the agent knows.
	// One fact is not asked about, because striking one sentence is what
	// this command is mostly used for and the strike is recorded as
	// feedback either way.
	//
	// Asked before the connection is opened, so that a refusal costs
	// nothing and a script finds out what it needs from the first line
	// rather than after authenticating.
	if number <= 0 {
		if err := confirm(command, fmt.Sprintf("This forgets %s and every page and fact under it, and cannot be undone.", path)); err != nil {
			return err
		}
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if number > 0 {
		if err := client.DeleteAgentFact(ctx, connection, path, number); err != nil {
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
		rows = append(rows, knowledgeSourceRow(source))
	}
	return printTable(knowledgeSourceHeaders, rows)
}

var knowledgeSourceHeaders = []string{"id", "name", "kind", "where", "state", "documents", "passages", ""}

// knowledgeSourceRow is one source as the list prints it, so that a
// source printed on its own after a change reads the same as the line it
// will be in the list.
func knowledgeSourceRow(source *client.AgentKnowledgeSource) []string {
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
	// Then what it read less of than the path it was given: the checkouts
	// under it nobody here has ever committed to, kept to what git says
	// about them. Said with the way to disagree, because a program that
	// quietly reads less than it was pointed at is one nobody can argue
	// with.
	if note == "" && source.CheckoutsKeptToProfile > 0 {
		note = plural(source.CheckoutsKeptToProfile, "checkout", "checkouts") +
			" you have never committed to: kept to their profile, " +
			plural(source.FilesKeptToProfile, "file", "files") +
			" not read — set --read-every-checkout to read them"
	}
	return []string{
		source.ID, source.Name, source.Kind, where, state,
		strconv.Itoa(source.DocumentCount), strconv.Itoa(source.ChunkCount), note,
	}
}

// runKnowledgeSearch searches what the sources indexed.
//
// The same search the agent's knowledge tool runs, printed the way the
// tool reads it: an identifier looked up exactly first, then the
// passages, each under the document it came from. A passage is somebody
// else's text -- a commit message, a chat post -- so it goes through
// forTerminal before it is written out.
func runKnowledgeSearch(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("search for what? teanode agent knowledge search \"the migration that failed\"")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	found, err := client.SearchAgentDocuments(ctx, connection,
		strings.Join(command.Args().Slice(), " "), int(command.Int("first")), command.String("source"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(found)
	}
	if found == nil || (len(found.Passages) == 0 && len(found.Definitions) == 0) {
		_, _ = fmt.Fprintln(command.Writer, "nothing in what has been indexed is about that")
		return nil
	}
	if len(found.Definitions) > 0 {
		_, _ = fmt.Fprintln(command.Writer, "Defined in:")
		for _, definition := range found.Definitions {
			_, _ = fmt.Fprintf(command.Writer, "  %s (%s) — %s:%d  [%s]\n",
				forTerminal(definition.Symbol), forTerminal(definition.Kind),
				forTerminal(definition.ExternalID), definition.Line, definition.DocumentID)
		}
		_, _ = fmt.Fprintln(command.Writer)
	}
	for _, passage := range found.Passages {
		heading := forTerminal(passage.Title)
		if passage.Author != "" {
			heading += " — " + forTerminal(passage.Author)
		}
		if passage.HappenedAt != nil {
			heading += " — " + passage.HappenedAt.Format("2 Jan 2006")
		}
		if passage.Source != "" {
			heading += " — " + forTerminal(passage.Source)
		}
		_, _ = fmt.Fprintf(command.Writer, "%s  [%s#%d]\n", heading, passage.DocumentID, passage.Number)
		_, _ = fmt.Fprintln(command.Writer, indent(forTerminal(excerpt(passage.Text, indexed.PassageShown)), "  "))
		_, _ = fmt.Fprintln(command.Writer)
	}
	if !found.Meaningful {
		_, _ = fmt.Fprintln(command.Writer, "(found by words alone; this deployment cannot search by meaning)")
	}
	return nil
}

// excerpt is as much of a passage as a listing shows, cut between
// characters rather than inside one, and marked where it was cut. The
// whole passage is one `knowledge read` away, and twelve whole passages
// are a screenful nobody reads.
func excerpt(text string, characters int) string {
	runes := []rune(text)
	if len(runes) <= characters {
		return text
	}
	return string(runes[:characters]) + "…"
}

// runKnowledgeRead reads one indexed document, a slice at a time.
func runKnowledgeRead(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("read which? give the identifier a search printed: teanode agent knowledge read <document-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	extract, err := client.ReadAgentDocument(ctx, connection,
		command.Args().First(), int(command.Int("from")), int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(extract)
	}
	if extract == nil {
		return fmt.Errorf("there is no document %q", command.Args().First())
	}
	_, _ = fmt.Fprintln(command.Writer, forTerminal(extract.Title))
	if extract.URL != "" {
		_, _ = fmt.Fprintln(command.Writer, forTerminal(extract.URL))
	}
	if extract.Author != "" {
		_, _ = fmt.Fprintln(command.Writer, "by "+forTerminal(extract.Author))
	}
	if extract.Source != "" {
		_, _ = fmt.Fprintln(command.Writer, "from "+forTerminal(extract.Source))
	}
	_, _ = fmt.Fprintln(command.Writer)
	_, _ = fmt.Fprintln(command.Writer, forTerminal(extract.Text))
	if extract.Next > 0 {
		_, _ = fmt.Fprintf(command.Writer, "\n… %d characters more: teanode agent knowledge read %s --from %d\n",
			extract.Total-extract.Next, extract.DocumentID, extract.Next)
	}
	return nil
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
	// A records folder is empty until something fills it, and a person who
	// adds one and waits for documents that never come has no way of
	// knowing that from the source's page. Say here what has to happen
	// next, and where the shape of a record is written down.
	if source.Specification.Format == models.FormatRecords {
		_, _ = fmt.Fprintln(command.Writer, "write records as JSON lines under that folder; a refresh script there runs before each scan (see docs/subsystems/memory.md)")
	}
	return nil
}

// runKnowledgeSet changes a source that already exists.
//
// Until now the only way to move a source's directory, file what it finds
// somewhere else, or read it at another hour was to remove it and add it
// again -- and removing one forgets every document and chunk it ever
// produced, which for a checkout or a chat archive is hours of reading
// and the embedding bill that went with it. Only the flags given are
// sent, and the API leaves a field it is not given alone, so changing the
// cron cannot quietly reset where what it finds is filed.
func runKnowledgeSet(ctx context.Context, command *cli.Command) error {
	connection, source, err := knowledgeSourceNamed(ctx, command)
	if err != nil {
		return err
	}
	fields := map[string]any{"sourceId": source.ID}
	for flag, name := range map[string]string{
		"name": "name", "path": "path", "under": "rootPath",
		"cron": "cron", "format": "format", "mailbox": "mailboxId",
	} {
		if value := command.String(flag); value != "" {
			fields[name] = value
		}
	}
	// A flag that is a yes or a no cannot be told apart from one nobody
	// gave by its value, so it is sent only when it was actually typed:
	// otherwise `set work --cron ...` would quietly turn this off.
	if command.IsSet("read-every-checkout") {
		fields["readEveryCheckout"] = command.Bool("read-every-checkout")
	}
	// The same, for a number whose zero means something: nobody typing
	// --name should have the pace of the history put back to the
	// program's own.
	if command.IsSet("commits-per-pass") {
		fields["commitsPerPass"] = command.Int("commits-per-pass")
	}
	if len(fields) == 1 {
		return fmt.Errorf("what should change? --name, --path, --under, --cron, --format, --mailbox, --read-every-checkout or --commits-per-pass")
	}
	changed, err := client.SaveAgentKnowledgeSource(ctx, connection, fields)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(changed)
	}
	return printTable(knowledgeSourceHeaders, [][]string{knowledgeSourceRow(changed)})
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

func runKnowledgeRemove(ctx context.Context, command *cli.Command) error {
	connection, source, err := knowledgeSourceNamed(ctx, command)
	if err != nil {
		return err
	}
	// Removing a source throws away every document and chunk it ever
	// found, which for a checkout or a chat archive is hours of reading
	// and the embedding bill that went with it. `pause` is what somebody
	// who only wants it to stop reading is after.
	if err := confirm(command, fmt.Sprintf("This stops reading %s and forgets everything it found; reading it again costs what the first pass cost. Pause keeps it.", source.Name)); err != nil {
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

// runDreamRuns lists the runs of one dream: it finds the dream in the log
// for the job that ran it, and lists the runs tagged with that job.
func runDreamRuns(ctx context.Context, command *cli.Command) error {
	dreamId := command.Args().First()
	if dreamId == "" {
		return fmt.Errorf("which dream? give its id, as `teanode agent dream log --json` shows it")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	dreams, err := client.ListAgentDreams(ctx, connection, 200)
	if err != nil {
		return describeError(command, err)
	}
	jobId := ""
	for _, dream := range dreams {
		if dream.ID == dreamId {
			jobId = dream.JobID
		}
	}
	if jobId == "" {
		return fmt.Errorf("no dream %q in the last two hundred, or one from before dreams kept their job", dreamId)
	}
	runs, total, err := client.ListAgentRuns(ctx, connection, 200, 0, jobId)
	if err != nil {
		return describeError(command, err)
	}
	if int64(len(runs)) < total {
		_, _ = fmt.Fprintf(command.Writer, "the first %d of %d\n", len(runs), total)
	}
	if command.Bool("json") {
		return PrintJSON(runs)
	}
	rows := make([][]string, 0, len(runs))
	for _, run := range runs {
		rows = append(rows, []string{run.ID, run.LastAt.Local().Format("15:04:05"),
			fmt.Sprintf("%d/%d", run.Usage.PromptTokens+run.Usage.CacheReadTokens, run.Usage.CompletionTokens), fmt.Sprintf("%.4f", run.Usage.Cost), run.Title})
	}
	return printTable([]string{"id", "when", "tokens in/out", "cost", "what"}, rows)
}

func runDreamProgress(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	progress, err := client.ReadAgentReadingProgress(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(progress)
	}
	_, _ = fmt.Fprintln(command.Writer, (&reading.Progress{Waiting: progress.Waiting, Read: progress.Read, PerHour: progress.PerHour, HoursLeft: progress.HoursLeft, Bootstrapping: progress.Bootstrapping}).Describe())
	return nil
}

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
		_, _ = fmt.Fprintln(command.Writer, "it has not dreamed yet")
		return nil
	}
	for _, dream := range dreams {
		_, _ = fmt.Fprintf(command.Writer, "%s  %s\n", dream.StartedAt.Format("Mon 2 Jan, 15:04"), dream.ID)
		var parts []string
		for _, pair := range []struct {
			count int
			what  string
		}{
			{dream.Digested, "read"}, {dream.Filed, "filed"}, {dream.Rewritten, "rewritten"},
			{dream.Moved, "filed away"}, {dream.Dormant, "retired"}, {dream.Embedded, "embedded"},
			{dream.Strengthened, "links reweighted"}, {dream.Associated, "connections noticed"},
			{dream.Rehearsed, "questions rehearsed"},
			// Beside the count, because "12 rehearsed" alone reads as
			// twelve questions memory answered.
			{dream.Gaps, "it could not answer"}, {dream.Unknown, "it could not try"},
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
		case len(parts) == 0 && dream.LastError == "":
			parts = []string{"nothing needed doing"}
		case len(parts) == 0:
			// The line under it says why there is nothing to count.
			parts = []string{"cut short"}
		}
		_, _ = fmt.Fprintf(command.Writer, "  %s\n", strings.Join(parts, ", "))
		if dream.Backlog > 0 {
			dreamsNeeded := (dream.Backlog + 1999) / 2000
			_, _ = fmt.Fprintf(command.Writer, "  %d still waiting, about %d dream(s) at this pace\n", dream.Backlog, dreamsNeeded)
		}
		if dream.Coarse {
			_, _ = fmt.Fprintln(command.Writer, "  worked a stretch at a time to keep up; a later dream can go back over it")
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

// --- the evaluation ---------------------------------------------------

// A question set is replayed through recall alone: for each question, the
// pages and facts a turn would have been carried, graded against what the
// question says it needs. No model is asked anything, so the whole set
// runs in seconds, costs nothing, and gives the same answer twice over
// the same graph -- which is what makes it worth running before and after
// a night to see what the night was worth.
//
// What it does not measure is the answer. Whether the model then used the
// facts it was given is a second evaluation, with a grader and a bill;
// this one says whether it had them at all, which is the failure the
// memory work of this plan is about.

// The kinds of question a set holds, spelled out because the totals are
// per kind and a typo would otherwise become a kind of its own.
const (
	questionDirect     = "direct"     // the words of the fact itself
	questionParaphrase = "paraphrase" // the same thing said another way
	questionChanged    = "changed"    // a fact that was corrected; the old one must not come back
	questionMultihop   = "multihop"   // needs two pages
	questionAbstain    = "abstain"    // there is nothing to carry, and nothing should be
)

// questionKinds is the same list in the order the totals are printed.
var questionKinds = []string{questionDirect, questionParaphrase, questionChanged, questionMultihop, questionAbstain}

// evaluationQuestion is one question of the set as the file holds it.
// docs/evaluation/README.md describes the shape.
type evaluationQuestion struct {
	ID       string            `json:"id"`
	Question string            `json:"question"`
	Kind     string            `json:"kind"`
	Expects  []evaluationClaim `json:"expects"`
	Forbids  []evaluationClaim `json:"forbids"`
}

// evaluationClaim is a fact the question needs recall to carry, or must
// not carry: the page it sits on and the words it says. A claim with no
// words is about the page alone -- anything carried from it answers it --
// which is how an abstain question says "nothing from here".
type evaluationClaim struct {
	Path  string   `json:"path"`
	Words []string `json:"words"`
}

// evaluationOutcome is how one question did, and why when it missed.
type evaluationOutcome struct {
	Hit bool `json:"hit"`

	// Failed names the first expectation that was not met, in the words a
	// person would use to look for it themselves.
	Failed string `json:"failed,omitempty"`
}

// evaluationResult is one row of the table and of the JSON.
type evaluationResult struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Question string `json:"question"`
	Hit      bool   `json:"hit"`
	Failed   string `json:"failed,omitempty"`

	// Carried is every fact recall would have put in front of the model,
	// as the page cites it, so a miss can be read without running the
	// question again by hand.
	Carried []string `json:"carried"`
}

// evaluationTotal is how one kind of question did.
type evaluationTotal struct {
	Kind  string `json:"kind"`
	Hits  int    `json:"hits"`
	Asked int    `json:"asked"`
}

// evaluationReport is the whole run, for --json.
type evaluationReport struct {
	Questions []*evaluationResult `json:"questions"`
	Totals    []*evaluationTotal  `json:"totals"`
	Hits      int                 `json:"hits"`
	Asked     int                 `json:"asked"`
}

// gradeRecall says whether what recall carried holds the facts a question
// needs, and names the first expectation that was not met.
//
// Given the carried set rather than a connection, so that the grading --
// which is the part with rules in it -- is tested without a server.
func gradeRecall(question evaluationQuestion, carried []*client.AgentRecalledPage) evaluationOutcome {
	// An abstain question is the one kind whose expectations are only
	// negative: it exists to catch memory that answers anyway. One that
	// lists expects is a mistake in the file rather than a graph that
	// forgot something, and saying so is more use than grading it.
	if question.Kind == questionAbstain && len(question.Expects) > 0 {
		return evaluationOutcome{Failed: "an abstain question expects nothing; drop its expects or change its kind"}
	}
	for _, claim := range question.Expects {
		if !carriesClaim(carried, claim) {
			return evaluationOutcome{Failed: "did not carry " + describeClaim(claim)}
		}
	}
	for _, claim := range question.Forbids {
		if carriesClaim(carried, claim) {
			return evaluationOutcome{Failed: "carried " + describeClaim(claim)}
		}
	}
	return evaluationOutcome{Hit: true}
}

// carriesClaim says whether the carried pages hold a fact on the claim's
// page containing every one of its words, compared without case.
func carriesClaim(carried []*client.AgentRecalledPage, claim evaluationClaim) bool {
	path := strings.TrimSpace(claim.Path)
	for _, page := range carried {
		if page == nil || !claimReaches(path, strings.TrimSpace(page.Path)) {
			continue
		}
		for _, fact := range page.Facts {
			if fact == nil {
				continue
			}
			if factSays(fact.Text, claim.Words) {
				return true
			}
		}
	}
	return false
}

// claimReaches says whether a page answers for a path the question set
// names: the page itself, or one filed under it.
//
// A night divides a page that has grown too long, and what was
// work/portal#12 becomes work/portal/deployments#3. That is the graph
// working, not the graph losing the fact -- but a question set written
// before the division names the parent, and grading on the exact path
// turned every division into a failed question. Six of twenty failed
// that way the first afternoon the pages were big enough to divide,
// which reads as recall regressing and is nothing of the kind.
func claimReaches(wanted, carried string) bool {
	return strings.EqualFold(carried, wanted) ||
		strings.HasPrefix(strings.ToLower(carried), strings.ToLower(wanted)+"/")
}

// factSays is whether one fact contains every word of a claim.
func factSays(text string, words []string) bool {
	lowered := strings.ToLower(text)
	for _, word := range words {
		if !strings.Contains(lowered, strings.ToLower(strings.TrimSpace(word))) {
			return false
		}
	}
	return true
}

// describeClaim is a claim as a person would say it out loud.
func describeClaim(claim evaluationClaim) string {
	if len(claim.Words) == 0 {
		return "anything on " + claim.Path
	}
	return claim.Path + " saying \"" + strings.Join(claim.Words, " ") + "\""
}

// readQuestionSet reads the file and refuses anything the grading could
// only misreport: a question of no kind, one that asks nothing, two with
// the same identifier.
func readQuestionSet(path string) ([]evaluationQuestion, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var questions []evaluationQuestion
	if err := json.Unmarshal(content, &questions); err != nil {
		return nil, fmt.Errorf("%s is not a list of questions: %w", path, err)
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("%s holds no questions", path)
	}
	seen := map[string]bool{}
	for index, question := range questions {
		where := question.ID
		if where == "" {
			where = "question " + strconv.Itoa(index+1)
		}
		if strings.TrimSpace(question.ID) == "" {
			return nil, fmt.Errorf("%s has no id; the table and the totals are read by it", where)
		}
		if strings.TrimSpace(question.Question) == "" {
			return nil, fmt.Errorf("%s asks nothing", where)
		}
		if !knownQuestionKind(question.Kind) {
			return nil, fmt.Errorf("%s is of kind %q; it has to be one of %s", where, question.Kind, strings.Join(questionKinds, ", "))
		}
		if seen[question.ID] {
			return nil, fmt.Errorf("two questions are called %q", question.ID)
		}
		seen[question.ID] = true
	}
	return questions, nil
}

func knownQuestionKind(kind string) bool {
	for _, known := range questionKinds {
		if kind == known {
			return true
		}
	}
	return false
}

// runAgentGraphRecall is one question through the same recall a turn
// uses, printed rather than graded.
//
// The query behind it was reachable only by writing a question set and
// running `memory evaluate` over it, which answers hit or miss and not
// "why did it not know that?". Asking costs nothing -- no model is asked
// anything, and a page recall expands here is not marked as used, so the
// same graph answers the same twice and asking does not itself change
// what tomorrow's night reads.
func runAgentGraphRecall(ctx context.Context, command *cli.Command) error {
	question := strings.TrimSpace(strings.Join(command.Args().Slice(), " "))
	if question == "" {
		return fmt.Errorf("ask something: teanode agent memory recall \"when does the portal ship?\"")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	recalled, err := client.RecallAgentMemory(ctx, connection, question)
	if err != nil {
		return describeError(command, err)
	}
	var carried []*client.AgentRecalledPage
	if recalled != nil {
		carried = recalled.Pages
	}
	if command.Bool("json") {
		return PrintJSON(carried)
	}
	if len(carried) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "that question carries nothing from the graph")
		return nil
	}
	for index, page := range carried {
		if page == nil {
			continue
		}
		if index > 0 {
			_, _ = fmt.Fprintln(command.Writer)
		}
		_, _ = fmt.Fprintln(command.Writer, page.Path)
		for _, fact := range page.Facts {
			if fact == nil {
				continue
			}
			_, _ = fmt.Fprintf(command.Writer, "#%d %s\n", fact.Number, fact.Text)
		}
	}
	return nil
}

func runAgentGraphEvaluate(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which question set? teanode agent memory evaluate docs/evaluation/memory-questions.json")
	}
	questions, err := readQuestionSet(command.Args().First())
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	report := &evaluationReport{Questions: make([]*evaluationResult, 0, len(questions)), Asked: len(questions)}
	for _, question := range questions {
		recalled, err := client.RecallAgentMemory(ctx, connection, question.Question)
		if err != nil {
			return describeError(command, err)
		}
		var carried []*client.AgentRecalledPage
		if recalled != nil {
			carried = recalled.Pages
		}
		outcome := gradeRecall(question, carried)
		if outcome.Hit {
			report.Hits++
		}
		report.Questions = append(report.Questions, &evaluationResult{
			ID:       question.ID,
			Kind:     question.Kind,
			Question: question.Question,
			Hit:      outcome.Hit,
			Failed:   outcome.Failed,
			Carried:  citeCarried(carried),
		})
	}
	report.Totals = totalsByKind(report.Questions)

	if command.Bool("json") {
		if err := PrintJSON(report); err != nil {
			return err
		}
		return missedQuestions(report)
	}
	rows := make([][]string, 0, len(report.Questions))
	for _, result := range report.Questions {
		outcome := "hit"
		if !result.Hit {
			outcome = "miss"
		}
		rows = append(rows, []string{result.Kind, result.ID, outcome, result.Failed})
	}
	if err := printTable([]string{"kind", "id", "", "what it missed"}, rows); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(command.Writer)
	for _, total := range report.Totals {
		_, _ = fmt.Fprintf(command.Writer, "%s: %d of %d\n", total.Kind, total.Hits, total.Asked)
	}
	_, _ = fmt.Fprintf(command.Writer, "all: %d of %d\n", report.Hits, report.Asked)
	return missedQuestions(report)
}

// missedQuestions is the error a set with a miss in it ends on, so that a
// script running the set before and after a night fails on the set and
// not on the reading of it.
func missedQuestions(report *evaluationReport) error {
	if report.Hits == report.Asked {
		return nil
	}
	return fmt.Errorf("%d of %d questions did not get the facts they needed", report.Asked-report.Hits, report.Asked)
}

// citeCarried is everything recall carried, as the page cites it.
func citeCarried(carried []*client.AgentRecalledPage) []string {
	cited := []string{}
	for _, page := range carried {
		if page == nil {
			continue
		}
		for _, fact := range page.Facts {
			if fact == nil {
				continue
			}
			cited = append(cited, page.Path+"#"+strconv.Itoa(fact.Number))
		}
	}
	return cited
}

// totalsByKind counts the hits of each kind that was asked about, in the
// order the kinds are declared.
func totalsByKind(results []*evaluationResult) []*evaluationTotal {
	hits := map[string]int{}
	asked := map[string]int{}
	for _, result := range results {
		asked[result.Kind]++
		if result.Hit {
			hits[result.Kind]++
		}
	}
	totals := make([]*evaluationTotal, 0, len(questionKinds))
	for _, kind := range questionKinds {
		if asked[kind] == 0 {
			continue
		}
		totals = append(totals, &evaluationTotal{Kind: kind, Hits: hits[kind], Asked: asked[kind]})
	}
	return totals
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
