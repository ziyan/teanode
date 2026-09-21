// Package memory is the tool the agent keeps what it knows about the
// person with: a graph of pages, each addressed by a path, with facts on
// them and links between them.
//
// Everything here is addressed by path. "people/alice-chen" is an address
// a model can guess, and a guess that lands on a page is a read that would
// otherwise have been a search; a ULID is an address it copies wrongly and
// never remembers between rounds. A fact is cited as "people/alice-chen#3".
//
// The top of the graph is folded into every prompt by the loop, and what
// the turn's own words touch is put in front of the model before it
// answers. This tool is for the rest: going deeper, and writing.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The bounds.
const (
	// batchItems is how many changes one call may make. Each write costs
	// an embedding call and a read of what is already on the page, so an
	// unbounded batch is an unbounded tool call.
	batchItems = 25

	// indexDepth is how deep the tree goes when nobody says.
	indexDepth = 2

	// factsShown is how many of a page's facts a get answers with, and
	// searchLimit how many rows a search answers with.
	factsShown  = 60
	searchLimit = 20

	// historyShown is how many changes a history answers with, and
	// historyMost the ceiling on asking for more. The same numbers the
	// API bounds it by, so the same question asked here and on the
	// command line is answered with the same page of it.
	historyShown = 50
	historyMost  = 200
)

func init() {
	tools.Register(func() []*tools.Tool {
		kinds := make([]string, 0, len(models.AgentNodeKinds))
		for _, kind := range models.AgentNodeKinds {
			kinds = append(kinds, string(kind))
		}
		factKinds := make([]string, 0, len(models.AgentFactKinds))
		for _, kind := range models.AgentFactKinds {
			factKinds = append(factKinds, string(kind))
		}
		relations := make([]string, 0, len(models.AgentEdgeRelations))
		for _, relation := range models.AgentEdgeRelations {
			relations = append(relations, string(relation))
		}
		audiences := make([]string, 0, len(models.AgentAudiences))
		for _, audience := range models.AgentAudiences {
			audiences = append(audiences, string(audience))
		}
		return []*tools.Tool{
			{
				Name: "memory", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskWrite,
				Description: "What you know about the person, kept between conversations as pages with facts on them. Every page has a path: people/alice-chen, projects/portal, self, time/2026/09. A fact on a page is cited as people/alice-chen#3. Your prompt carries the top of the graph and whatever this turn's words touched; `get` a path before telling them you do not know something about them, and `search` when you cannot guess the path. You need not file what you learn -- a run after this conversation does that -- but `note` anything they ask you to remember, and correct a page that is wrong: `note` with the fact's number rewrites that one sentence where it stands. `history` says what has happened to a page and who did it, which is how a line nobody recognizes is accounted for. `look` shows you the picture a fact was read out of, when the answer is in the screenshot rather than in the sentence about it.",
				Parameters: tools.Object(map[string]any{
					"action": tools.EnumProperty("what to do; move files a page under another, or with number moves one fact onto another page",
						"index", "get", "search", "look", "note", "page", "history", "link", "unlink", "move", "merge", "forget", "batch"),
					"path":       tools.StringProperty("the page: a path like people/alice-chen. For note, the page the fact goes on; it is made if it is missing"),
					"depth":      tools.IntegerProperty("for index: how many levels below the path, 2 by default"),
					"query":      tools.StringProperty("for search: words"),
					"document":   tools.StringProperty("for look: one file on its own, by the document identifier knowledge search and read give; not needed when you give path and number"),
					"text":       tools.StringProperty("for note: the fact, in a sentence or two; with number, the whole sentence as it should now read"),
					"fact_kind":  tools.EnumProperty("for note: what sort of statement it is, 'fact' by default; 'preference' and 'decision' are only for what the person themselves said", factKinds...),
					"happened":   tools.StringProperty("for note: when it was true, if that is not now -- 2023-06, 2023-06-14, or a date the person gave"),
					"kind":       tools.EnumProperty("for note and page: what the page is about, needed when the page is new", kinds...),
					"name":       tools.StringProperty("for note and page: what the page is called, when it is new or being renamed"),
					"summary":    tools.StringProperty("for page: what the page says, rewritten whole"),
					"aliases":    tools.ArrayProperty("for page: what else they call it", tools.StringProperty("an alias")),
					"pinned":     tools.BooleanProperty("for page: always in your prompt"),
					"applies_to": tools.ArrayProperty("for note: which runs besides the conversation read it; any of "+strings.Join(audiences, ", "), tools.StringProperty("an audience")),
					"to":         tools.StringProperty("for link: the other page's path. For move: the path of the page it goes under, or with number the page the fact goes on. For merge: the page that survives"),
					"relation":   tools.EnumProperty("for link: what the first page is to the second", relations...),
					"number":     tools.IntegerProperty("for note: the fact to rewrite where it stands, keeping its number, its evidence and the day it was learned, rather than adding another one. For forget: the fact's number on the page; without it the whole page goes. For move: the fact to move onto the page in to, rather than the page itself. For look: the fact whose picture to show you"),
					"limit":      tools.IntegerProperty("for search, index and history: how many"),
					"items":      tools.ArrayProperty("for batch: up to 25 of the above, each with its own action", map[string]any{"type": "object"}),
				}, "action"),
				Guidance: "memory: the graph is addressed by path (people/alice-chen, projects/portal, self) and a fact by number (people/alice-chen#3). `self` is the person you are talking to: what is known about them lives there, and a page under people about them is a duplicate to `merge` into self, never the other way round. `get` a path before saying you do not know something about the person; `search` when you cannot guess the path. `note` what they ask you to remember and correct what is wrong; a run after the conversation files the rest. A fact that says the wrong thing is corrected with `note` and its number, which rewrites that sentence and keeps its number, its evidence and the day it was learned; forgetting it and writing it again loses all three. A fact on the wrong page is `move`d with its number for the same reason. When they ask where something on a page came from, or who changed it, `history` says. When a fact was read out of a picture -- a screenshot of an error, a dashboard, a whiteboard -- `look` at it with the page and the fact's number before answering from the sentence alone: the sentence is what somebody wrote down about the picture months ago, and the picture is still there. A fact addressed to triage changes how mail is sorted from the next message on; one addressed to reply changes how the agent answers for them. Prefer a rule for anything rule-shaped; a fact is for what a rule cannot say.",
				Preview: tools.PreviewOf(func(call struct {
					Action string `json:"action"`
					Path   string `json:"path"`
					Text   string `json:"text"`
					To     string `json:"to"`
					Number int    `json:"number"`
				}) string {
					page := tools.Named(call.Path, "the graph")
					switch call.Action {
					case "index":
						return "Look over what it knows"
					case "get":
						return "Read " + page
					case "search":
						return "Search what it knows"
					case "look":
						if call.Number > 0 {
							return "Look at the picture behind one thing it knows about " + page
						}
						return "Look at a picture it has kept"
					case "note":
						if call.Number > 0 {
							return "Correct one thing it knows about " + page
						}
						return "Remember something about " + page
					case "page":
						return "Rewrite " + page
					case "history":
						return "Look at what has changed on " + page
					case "link":
						return "Link " + page + " to " + tools.Named(call.To, "another page")
					case "unlink":
						return "Unlink " + page + " from " + tools.Named(call.To, "another page")
					case "move":
						if call.Number > 0 {
							return "Move one thing it knows about " + page + " to " + tools.Named(call.To, "another page")
						}
						return "File " + page + " under " + tools.Named(call.To, "somewhere else")
					case "merge":
						return "Merge " + page + " into " + tools.Named(call.To, "another page")
					case "forget":
						if call.Number > 0 {
							return "Forget one thing about " + page
						}
						return "Forget " + page
					case "batch":
						return "Change several things it knows"
					}
					return "Change what it knows"
				}),
				Run:     runMemory,
				RiskOf:  riskOfMemory,
				Overlay: recalledOverlay,
			},
		}
	})
}

// riskOfMemory judges the call, not the tool: reading changes nothing.
// Without it the whole tool counts as writing, and a run that may only
// read loses it altogether -- which is how a research run came to be
// offered memory in its allow list and never see it.
func riskOfMemory(arguments json.RawMessage) tools.Risk {
	var call memoryArguments
	if err := json.Unmarshal(arguments, &call); err != nil {
		return tools.RiskWrite
	}
	switch call.Action {
	case "index", "get", "search", "look", "history":
		return tools.RiskRead
	case "forget":
		// Forgetting a whole page takes its facts with it.
		if call.Number <= 0 {
			return tools.RiskDestructive
		}
		return tools.RiskWrite
	case "merge":
		// A page goes, even though what was on it stays.
		return tools.RiskDestructive
	case "batch":
		risk := tools.RiskRead
		for _, item := range call.Items {
			switch item.Action {
			case "index", "get", "search", "look", "history":
			case "forget":
				if item.Number <= 0 {
					return tools.RiskDestructive
				}
				risk = tools.RiskWrite
			default:
				risk = tools.RiskWrite
			}
		}
		return risk
	}
	return tools.RiskWrite
}

type memoryItem struct {
	Action    string   `json:"action"`
	Path      string   `json:"path"`
	Text      string   `json:"text"`
	FactKind  string   `json:"fact_kind"`
	Happened  string   `json:"happened"`
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Summary   string   `json:"summary"`
	Aliases   []string `json:"aliases"`
	Pinned    *bool    `json:"pinned"`
	AppliesTo []string `json:"applies_to"`
	To        string   `json:"to"`
	Relation  string   `json:"relation"`
	Number    int      `json:"number"`
	// What search and index take. They live on the item, not only on the
	// call, so that a batch of searches keeps its words: for a while they
	// were on the call alone, and every search inside a batch was asked
	// "search what?".
	Query string `json:"query"`
	Depth int    `json:"depth"`
	Limit int    `json:"limit"`

	// Document is one file on its own, for look: the identifier the
	// knowledge tool answers a search and a read with.
	Document string `json:"document"`
}

type memoryArguments struct {
	memoryItem
	Items []memoryItem `json:"items"`
}

func runMemory(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[memoryArguments](call)
	if err != nil {
		return nil, err
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	return runMemoryItem(ctx, run, call, &arguments)
}

func runMemoryItem(ctx context.Context, run tools.Run, call *tools.Call, arguments *memoryArguments) (*tools.Result, error) {
	switch arguments.Action {
	case "index":
		return indexAction(ctx, run, arguments)
	case "get":
		return getAction(ctx, run, arguments)
	case "search":
		return searchAction(ctx, run, arguments)
	case "look":
		return lookAction(ctx, run, arguments)
	case "note":
		return noteAction(ctx, run, arguments)
	case "page":
		return pageAction(ctx, run, arguments)
	case "history":
		return historyAction(ctx, run, arguments)
	case "link":
		return linkAction(ctx, run, arguments)
	case "unlink":
		return unlinkAction(ctx, run, arguments)
	case "move":
		return moveAction(ctx, run, arguments)
	case "merge":
		return mergeAction(ctx, run, arguments)
	case "forget":
		return forgetAction(ctx, run, arguments)
	case "batch":
		return batchAction(ctx, run, call, arguments)
	}
	return nil, fmt.Errorf("%q is not an action of memory", arguments.Action)
}

// ownPath is the path as the graph has it: a page under people that names
// the person themselves is "self", where what is known about them lives,
// so a model that writes people/<their name> reads and files there rather
// than making a second page about them.
func ownPath(run tools.Run, path string) string {
	path = models.NormalizePath(path)
	if !strings.HasPrefix(path, models.PathPeople+"/") {
		return path
	}
	var selfPage *models.AgentNode
	_ = run.Database().Transaction(func(tx db.Transaction) (err error) {
		selfPage, err = tx.GetAgentNode(run.Agent().ID, models.PathSelf)
		return err
	})
	if models.IsThePerson(path, run.Owner(), selfPage) {
		return models.PathSelf
	}
	return path
}

// --- reading ----------------------------------------------------------

// indexAction is the tree, as lines. Not the whole graph: a tree under a
// path, to a depth, so that a model looking for where something lives can
// walk down rather than read everything.
func indexAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	root := models.NormalizePath(arguments.Path)
	depth := arguments.Depth
	if depth <= 0 {
		depth = indexDepth
	}
	limit := arguments.Limit
	if limit <= 0 {
		limit = 200
	}
	var nodes []*models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		nodes, err = tx.ListAgentNodesUnder(run.Agent().ID, root, limit*4)
		return err
	}); err != nil {
		return nil, err
	}
	rootDepth := 0
	if root != "" {
		rootDepth = strings.Count(root, "/") + 1
	}
	lines := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node.Dormant {
			continue
		}
		level := strings.Count(node.Path, "/") + 1
		if level-rootDepth > depth {
			continue
		}
		indent := strings.Repeat("  ", max(level-rootDepth-1, 0))
		lines = append(lines, indent+node.IndexLine(120))
		if len(lines) >= limit {
			break
		}
	}
	if len(lines) == 0 {
		return tools.TextResult("nothing under %s yet", tools.Named(root, "the graph")), nil
	}
	return tools.TextResult("%s", strings.Join(lines, "\n")), nil
}

// getAction is one page: what it says, its facts numbered, what it is
// joined to, and what is under it.
func getAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	path := ownPath(run, arguments.Path)
	if path == "" {
		return nil, fmt.Errorf("which page? give a path, like people/alice-chen")
	}
	var node *models.AgentNode
	var facts []*models.AgentFact
	var edges []*models.AgentEdge
	var children []*models.AgentNode
	agentId := run.Agent().ID
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if node, err = tx.GetAgentNode(agentId, path); err != nil || node == nil {
			return err
		}
		// The liveliest sixty, shown in number order: a page of two
		// hundred is not read whole, and the sixty oldest were the wrong
		// sixty.
		if facts, err = tx.ListAgentFactsLively(agentId, node.ID, factsShown); err != nil {
			return err
		}
		sort.Slice(facts, func(left, right int) bool { return facts[left].Number < facts[right].Number })
		if edges, err = tx.ListAgentEdges(agentId, node.ID); err != nil {
			return err
		}
		if children, err = tx.ListAgentNodeChildren(agentId, node.ID); err != nil {
			return err
		}
		if err := tx.TouchAgentNodes([]string{node.ID}, time.Now()); err != nil {
			return err
		}
		ids := make([]string, 0, len(facts))
		for _, fact := range facts {
			ids = append(ids, fact.ID)
		}
		return tx.TouchAgentFacts(ids, time.Now())
	}); err != nil {
		return nil, err
	}
	if node == nil {
		// Where the page does not exist, say what is near rather than
		// only "no": a model that guessed the path wrongly can then
		// guess again from the answer instead of searching.
		return notThere(ctx, run, path)
	}
	return tools.TextResult("%s", renderPage(node, facts, edges, children)), nil
}

// renderPage is a page as the model reads it.
func renderPage(node *models.AgentNode, facts []*models.AgentFact, edges []*models.AgentEdge, children []*models.AgentNode) string {
	var builder strings.Builder
	builder.WriteString(node.Path)
	if node.Name != "" {
		builder.WriteString(" — " + node.Name)
	}
	builder.WriteString(" (" + string(node.Kind) + ")")
	if len(node.Aliases) > 0 {
		builder.WriteString("\nalso called: " + strings.Join(node.Aliases, ", "))
	}
	if summary := strings.TrimSpace(node.Summary); summary != "" {
		builder.WriteString("\n\n" + summary)
	}
	if len(facts) > 0 {
		builder.WriteString("\n")
		for _, fact := range facts {
			builder.WriteString("\n#" + strconv.Itoa(fact.Number) + " " + fact.Line())
			if fact.Kind != models.FactPlain {
				builder.WriteString(" [" + string(fact.Kind) + "]")
			}
			if source := evidenceLine(fact); source != "" {
				builder.WriteString("  " + source)
			}
		}
	}
	if len(edges) > 0 {
		builder.WriteString("\n")
		for _, edge := range edges {
			if edge.Relation == models.EdgePartOf {
				continue
			}
			// "perhaps" in front and whose guess it was at the end. A link
			// the nightly walk invented is a hypothesis, and read as a
			// bare relation it was repeated to the person as fact.
			relation := string(edge.Relation)
			guess := ""
			if edge.Proposed() {
				relation = "perhaps " + relation
				guess = " (the agent's guess)"
			}
			if edge.FromPath == node.Path {
				builder.WriteString("\n→ " + relation + " " + edge.ToPath + guess)
			} else {
				builder.WriteString("\n← " + edge.FromPath + " " + relation + " this" + guess)
			}
		}
	}
	if len(children) > 0 {
		builder.WriteString("\n\nunder it: ")
		paths := make([]string, 0, len(children))
		for _, child := range children {
			paths = append(paths, models.LastSegment(child.Path))
			if len(paths) >= 24 {
				paths = append(paths, "…")
				break
			}
		}
		builder.WriteString(strings.Join(paths, ", "))
	}
	return builder.String()
}

// evidenceLine says where a fact came from, briefly.
func evidenceLine(fact *models.AgentFact) string {
	if len(fact.Evidence) == 0 {
		return ""
	}
	kinds := make([]string, 0, 3)
	seen := map[models.EvidenceKind]bool{}
	for _, evidence := range fact.Evidence {
		if seen[evidence.Kind] || len(kinds) >= 3 {
			continue
		}
		seen[evidence.Kind] = true
		kinds = append(kinds, string(evidence.Kind))
	}
	return "(from " + strings.Join(kinds, ", ") + ")"
}

// notThere answers a get for a page that is not there with whatever is
// nearest by name, so a wrong guess costs one call rather than two.
func notThere(ctx context.Context, run tools.Run, path string) (*tools.Result, error) {
	words := strings.ReplaceAll(models.LastSegment(path), "-", " ")
	var nodes []*models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		found, _, err := tx.SearchAgentGraph(run.Agent().ID, words, 8)
		nodes = found
		return err
	}); err != nil {
		return nil, err
	}
	answer := "there is no page at " + path
	if len(nodes) > 0 {
		paths := make([]string, 0, len(nodes))
		for _, node := range nodes {
			paths = append(paths, node.Path)
		}
		answer += ". Nearest by name: " + strings.Join(paths, ", ")
	} else {
		answer += ", and nothing like it. Use search, or note something on it to make it."
	}
	return tools.TextResult("%s", answer), nil
}

// searchAction finds pages and facts by words, and by meaning where the
// deployment can say what a page means.
func searchAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	query := strings.TrimSpace(arguments.Query)
	if query == "" {
		query = strings.TrimSpace(arguments.Text)
	}
	if query == "" {
		return nil, fmt.Errorf("search what? give some words")
	}
	limit := arguments.Limit
	if limit <= 0 {
		limit = searchLimit
	}
	agentId := run.Agent().ID
	var nodes []*models.AgentNode
	var facts []*models.AgentFact
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		nodes, facts, err = tx.SearchAgentGraph(agentId, query, limit)
		return err
	}); err != nil {
		return nil, err
	}
	// And by meaning, in front of the words: a person asking about "the
	// boat" means the page that says "Marigold", and no word of theirs
	// appears in it.
	if searcher, ok := run.(tools.GraphSearching); ok {
		nearNodes, nearFacts := searcher.SearchGraphByMeaning(ctx, query, limit)
		nodes = mergeNodes(nearNodes, nodes, limit)
		facts = mergeFacts(nearFacts, facts, limit)
	}
	if len(nodes) == 0 && len(facts) == 0 {
		return tools.TextResult("nothing about that"), nil
	}

	paths, err := pathsOf(ctx, run, facts)
	if err != nil {
		return nil, err
	}
	var builder strings.Builder
	for _, node := range nodes {
		builder.WriteString(node.IndexLine(160) + "\n")
	}
	for _, fact := range facts {
		builder.WriteString(fact.Reference(paths[fact.NodeID]) + " " + fact.Line() + "\n")
	}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		nodeIds := make([]string, 0, len(nodes))
		for _, node := range nodes {
			nodeIds = append(nodeIds, node.ID)
		}
		factIds := make([]string, 0, len(facts))
		for _, fact := range facts {
			factIds = append(factIds, fact.ID)
		}
		if err := tx.TouchAgentNodes(nodeIds, time.Now()); err != nil {
			return err
		}
		return tx.TouchAgentFacts(factIds, time.Now())
	}); err != nil {
		return nil, err
	}
	return tools.TextResult("%s", strings.TrimRight(builder.String(), "\n")), nil
}

// pathsOf is the path of every page these facts sit on.
func pathsOf(ctx context.Context, run tools.Run, facts []*models.AgentFact) (map[string]string, error) {
	if len(facts) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(facts))
	for _, fact := range facts {
		ids = append(ids, fact.NodeID)
	}
	var nodes []*models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		nodes, err = tx.GetAgentNodes(run.Agent().ID, ids)
		return err
	}); err != nil {
		return nil, err
	}
	paths := make(map[string]string, len(nodes))
	for _, node := range nodes {
		paths[node.ID] = node.Path
	}
	return paths, nil
}

func mergeNodes(first, second []*models.AgentNode, limit int) []*models.AgentNode {
	seen := map[string]bool{}
	merged := make([]*models.AgentNode, 0, limit)
	for _, list := range [][]*models.AgentNode{first, second} {
		for _, node := range list {
			if seen[node.ID] || len(merged) >= limit {
				continue
			}
			seen[node.ID] = true
			merged = append(merged, node)
		}
	}
	return merged
}

func mergeFacts(first, second []*models.AgentFact, limit int) []*models.AgentFact {
	seen := map[string]bool{}
	merged := make([]*models.AgentFact, 0, limit)
	for _, list := range [][]*models.AgentFact{first, second} {
		for _, fact := range list {
			if seen[fact.ID] || len(merged) >= limit {
				continue
			}
			seen[fact.ID] = true
			merged = append(merged, fact)
		}
	}
	return merged
}

// historyAction is what has happened to a page, newest first.
//
// A page a nightly run wrote and a page the person wrote themselves read
// exactly alike until somebody asks this: it is the only place the graph
// says where a sentence came from, and it is what the agent needs when
// the person says "I never said that" or asks why a page changed.
func historyAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	path := ownPath(run, arguments.Path)
	if path == "" {
		return nil, fmt.Errorf("which page? give a path, like people/alice-chen")
	}
	limit := arguments.Limit
	if limit <= 0 || limit > historyMost {
		limit = historyShown
	}
	agentId := run.Agent().ID
	var node *models.AgentNode
	var revisions []*models.AgentRevision
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if node, err = tx.GetAgentNode(agentId, path); err != nil || node == nil {
			return err
		}
		revisions, err = tx.ListAgentRevisions(agentId, node.ID, limit)
		return err
	}); err != nil {
		return nil, err
	}
	if node == nil {
		return notThere(ctx, run, path)
	}
	if len(revisions) == 0 {
		return tools.TextResult("nothing has happened to %s yet", node.Path), nil
	}
	location := tools.Location(run.Owner())
	var builder strings.Builder
	for _, revision := range revisions {
		builder.WriteString("#" + strconv.Itoa(revision.Revision) + " " +
			revision.CreatedAt.In(location).Format("2 Jan 2006 15:04") + " — " + revision.Describe() + "\n")
		// The words either side, where words moved. A summary rewritten
		// overnight is the change somebody most often wants back, and a
		// history that only says "rewrote what this page says" cannot
		// give it to them.
		if was := revision.TextBefore(); was != "" {
			builder.WriteString("  was: " + tools.FirstWords(was, 25) + "\n")
		}
		if now := revision.TextAfter(); now != "" {
			builder.WriteString("  now: " + tools.FirstWords(now, 25) + "\n")
		}
	}
	return tools.TextResult("%s", strings.TrimRight(builder.String(), "\n")), nil
}

// --- writing ----------------------------------------------------------

// noteAction puts a fact on a page, making the page if it is missing, or
// with a number rewrites the fact that is already there.
func noteAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	path := ownPath(run, arguments.Path)
	text := strings.TrimSpace(arguments.Text)
	if text == "" {
		return nil, fmt.Errorf("note what? give the fact as text")
	}
	if arguments.Number > 0 {
		return editFactAction(ctx, run, arguments, path, text)
	}
	if path == "" {
		// A fact with nowhere to go goes in notes rather than being
		// refused: a refusal here is answered with the same call again,
		// and the nightly run files it properly.
		path = models.JoinPath(models.PathNotes, tools.FirstWords(text, 5))
	}
	kind := models.AgentFactKind(strings.ToLower(strings.TrimSpace(arguments.FactKind)))
	if kind == "" {
		kind = models.FactPlain
	}
	if !models.IsAgentFactKind(kind) {
		return nil, fmt.Errorf("%q is not a kind of fact; use one of %s", arguments.FactKind, joinKinds())
	}
	happened, err := whenItWasTrue(run, arguments.Happened)
	if err != nil {
		return nil, err
	}

	agentId := run.Agent().ID
	var node *models.AgentNode
	var fact *models.AgentFact
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.EnsureAgentRoots(agentId); err != nil {
			return err
		}
		// The page already there under any of its names, rather than a
		// second one beside it: a model files "people/alice" one day and
		// "people/alice-chen" the next, and both mean her.
		existing, err := resolvePage(ctx, run, tx, path,
			models.AgentNodeKind(strings.ToLower(strings.TrimSpace(arguments.Kind))),
			strings.TrimSpace(arguments.Name))
		if err != nil {
			return err
		}
		if existing == nil {
			return fmt.Errorf("there is nowhere to put that")
		}
		node = existing
		fact, err = tx.AddAgentFact(&models.AgentFact{
			AgentID: agentId, NodeID: node.ID, Kind: kind, Text: text,
			HappenedAt: happened, Confidence: 1,
			Evidence:  []models.Evidence{conversationEvidence(run, text)},
			Audiences: factAudiences(arguments.AppliesTo),
		})
		return err
	}); err != nil {
		return nil, err
	}

	return factWritten(ctx, run, node, fact, "remembered: "+tools.FirstWords(text, 8)), nil
}

// editFactAction rewrites one fact where it stands.
//
// The alternative, and what a model did while this was missing, is to
// forget the sentence and write it again. That gives it a new number,
// throws away the words it came from and the day it was learned, and
// leaves whatever cited the old number pointing at nothing. Correcting it
// in place keeps all three, and the page's history carries what it used
// to say, so the correction can be read and undone.
//
// The page is not made on the way and the fact is not invented: a number
// for a fact that is not there is a typo, and answering it by writing a
// new fact would file the correction of one sentence as a second one.
func editFactAction(ctx context.Context, run tools.Run, arguments *memoryArguments, path, text string) (*tools.Result, error) {
	if path == "" {
		return nil, fmt.Errorf("which fact? give the page's path as well as the number")
	}
	// The kind and the date change only where the call gives them. A
	// correction is nearly always to the words alone, and reading a
	// missing kind as "fact" and a missing date as "now" would turn a
	// decision made in June into an undated fact every time a word in
	// it was fixed.
	kind := models.AgentFactKind(strings.ToLower(strings.TrimSpace(arguments.FactKind)))
	if kind != "" && !models.IsAgentFactKind(kind) {
		return nil, fmt.Errorf("%q is not a kind of fact; use one of %s", arguments.FactKind, joinKinds())
	}
	var happened *time.Time
	if strings.TrimSpace(arguments.Happened) != "" {
		var err error
		if happened, err = whenItWasTrue(run, arguments.Happened); err != nil {
			return nil, err
		}
	}

	agentId := run.Agent().ID
	var node *models.AgentNode
	var fact *models.AgentFact
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.GetAgentNode(agentId, path)
		if err != nil {
			return err
		}
		if found == nil {
			return fmt.Errorf("there is no page at %s", path)
		}
		node = found
		existing, err := tx.GetAgentFact(agentId, node.ID, arguments.Number)
		if err != nil {
			return err
		}
		if existing == nil {
			return fmt.Errorf("there is no %s", path+"#"+strconv.Itoa(arguments.Number))
		}
		fact, err = tx.UpdateAgentFact(agentId, existing.ID, func(fact *models.AgentFact) error {
			fact.Text = text
			if kind != "" {
				fact.Kind = kind
			}
			if happened != nil {
				fact.HappenedAt = happened
			}
			// Only where this call said who reads it. Audiences are set
			// once and hardly ever repeated, and a correction that
			// quietly took a fact out of triage would change how mail is
			// sorted from the next message on without anybody asking.
			if len(arguments.AppliesTo) > 0 {
				fact.Audiences = factAudiences(arguments.AppliesTo)
			}
			// Corrected, so it is no longer a guess, and what it was
			// learned from stays: the correction joins the evidence
			// rather than replacing it, so the page still says where the
			// sentence came from originally.
			fact.Inferred = false
			fact.Confidence = 1
			fact.Evidence = append(fact.Evidence, conversationEvidence(run, text))
			return nil
		})
		return err
	}); err != nil {
		return nil, err
	}
	return factWritten(ctx, run, node, fact, "corrected "+fact.Reference(node.Path)), nil
}

// factWritten is the answer to a fact written or rewritten: the fact as
// it now reads, and whatever on the same page it turns out to repeat.
func factWritten(ctx context.Context, run tools.Run, node *models.AgentNode, fact *models.AgentFact, note string) *tools.Result {
	answer := fact.Reference(node.Path) + " " + fact.Line()
	// What it means is worked out as it is written, which is how the same
	// thing written twice is caught. The prompt asks the model to read the
	// page first; this is for the times it does not. It is also what gives
	// an edited fact its vector back, since changing the words throws the
	// old one away.
	if twins := noteMeaning(ctx, run, fact); len(twins) > 0 {
		// An imperative naming both, not a hedge: a warning that
		// something "may be" a duplicate was read and ignored, and the
		// same fact was kept twice.
		answer += "\n\nThis says what " + twins[0].Reference(node.Path) + " already says: \"" +
			twins[0].Line() + "\". Put whatever is new into that one and forget " +
			fact.Reference(node.Path) + ". Do it now, in this turn."
	}
	result := tools.TextResult("%s", answer)
	result.Note = note
	return result
}

// whenItWasTrue reads the date the model gave, in the person's zone.
// Empty means now, which the store leaves unset.
func whenItWasTrue(run tools.Run, said string) (*time.Time, error) {
	said = strings.TrimSpace(said)
	if said == "" {
		return nil, nil
	}
	when, err := tools.ParseTime(said, tools.Location(run.Owner()), time.Now())
	if err != nil {
		// A month on its own -- "2023-06", which is how a model says
		// when something was true -- is not a time the general parser
		// takes, and it is the commonest thing written here.
		if month, monthErr := time.ParseInLocation("2006-01", said, tools.Location(run.Owner())); monthErr == nil {
			return &month, nil
		}
		return nil, fmt.Errorf("%q is not a date I can read: %w", said, err)
	}
	return &when, nil
}

// conversationEvidence is where a fact the model wrote came from.
func conversationEvidence(run tools.Run, text string) models.Evidence {
	evidence := models.Evidence{Kind: models.EvidenceConversation, Quote: tools.FirstWords(text, 20)}
	if conversation := run.Conversation(); conversation != nil {
		evidence.ID = conversation.ID
	}
	return evidence
}

// kindFromPath guesses what a page is about from where it was filed,
// which is right often enough to be worth not asking.
func kindFromPath(path string) models.AgentNodeKind {
	switch strings.Split(path, "/")[0] {
	case models.PathPeople:
		return models.NodePerson
	case models.PathProjects:
		return models.NodeProject
	case models.PathPlaces:
		return models.NodePlace
	case models.PathThings:
		return models.NodeThing
	case models.PathTopics:
		return models.NodeTopic
	case models.PathTime:
		return models.NodePeriod
	case models.PathSelf:
		return models.NodeSelf
	}
	return models.NodeTopic
}

// titleFromSlug is a path segment as a name: dashes back to spaces, first
// letter up. A page the model made without naming still reads as
// something rather than as a slug.
func titleFromSlug(slug string) string {
	words := strings.Split(strings.ReplaceAll(slug, "-", " "), " ")
	for index, word := range words {
		if word == "" {
			continue
		}
		runes := []rune(word)
		words[index] = strings.ToUpper(string(runes[0])) + string(runes[1:])
	}
	return strings.Join(words, " ")
}

func joinKinds() string {
	names := make([]string, 0, len(models.AgentFactKinds))
	for _, kind := range models.AgentFactKinds {
		names = append(names, string(kind))
	}
	return strings.Join(names, ", ")
}

// pageAction rewrites what a page says.
func pageAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	path := ownPath(run, arguments.Path)
	if path == "" {
		return nil, fmt.Errorf("which page? give a path")
	}
	agentId := run.Agent().ID
	var node *models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := tx.EnsureAgentRoots(agentId); err != nil {
			return err
		}
		existing, err := tx.GetAgentNode(agentId, path)
		if err != nil {
			return err
		}
		written := &models.AgentNode{AgentID: agentId, Path: path}
		if existing != nil {
			written = existing
		} else {
			written.Kind = kindFromPath(path)
			written.Name = titleFromSlug(models.LastSegment(path))
		}
		if kind := models.AgentNodeKind(strings.ToLower(strings.TrimSpace(arguments.Kind))); kind != "" && models.IsAgentNodeKind(kind) {
			written.Kind = kind
		}
		if name := strings.TrimSpace(arguments.Name); name != "" {
			written.Name = name
		}
		if arguments.Summary != "" {
			written.Summary = strings.TrimSpace(arguments.Summary)
		}
		if arguments.Aliases != nil {
			written.Aliases = arguments.Aliases
		}
		if arguments.Pinned != nil {
			written.Pinned = *arguments.Pinned
		}
		node, err = tx.PutAgentNode(written)
		return err
	}); err != nil {
		return nil, err
	}
	noteNodeMeaning(ctx, run, node)
	result := tools.TextResult("%s", node.Path+" — "+node.Name+"\n"+node.Summary)
	result.Note = "wrote the page " + node.Path
	return result, nil
}

// linkAction joins two pages.
func linkAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	from := ownPath(run, arguments.Path)
	to := ownPath(run, arguments.To)
	if from == "" || to == "" {
		return nil, fmt.Errorf("link what to what? give path and to")
	}
	relation := models.AgentEdgeRelation(strings.ToLower(strings.TrimSpace(arguments.Relation)))
	if relation == "" {
		relation = models.EdgeRelatedTo
	}
	if !models.IsAgentEdgeRelation(relation) {
		names := make([]string, 0, len(models.AgentEdgeRelations))
		for _, known := range models.AgentEdgeRelations {
			names = append(names, string(known))
		}
		return nil, fmt.Errorf("%q is not a relation; use one of %s", arguments.Relation, strings.Join(names, ", "))
	}
	agentId := run.Agent().ID
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		fromNode, err := tx.GetAgentNode(agentId, from)
		if err != nil {
			return err
		}
		if fromNode == nil {
			return fmt.Errorf("there is no page at %s", from)
		}
		toNode, err := tx.GetAgentNode(agentId, to)
		if err != nil {
			return err
		}
		if toNode == nil {
			return fmt.Errorf("there is no page at %s", to)
		}
		return tx.PutAgentEdge(&models.AgentEdge{
			AgentID: agentId, FromID: fromNode.ID, ToID: toNode.ID, Relation: relation,
			Evidence: []models.Evidence{conversationEvidence(run, from+" "+string(relation)+" "+to)},
		})
	}); err != nil {
		return nil, err
	}
	result := tools.TextResult("%s", from+" "+string(relation)+" "+to)
	result.Note = "linked " + from + " to " + to
	return result, nil
}

// unlinkAction takes a join away: the person said two pages are not
// what the notes say they are to each other.
func unlinkAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	from := models.NormalizePath(arguments.Path)
	to := models.NormalizePath(arguments.To)
	if from == "" || to == "" {
		return nil, fmt.Errorf("unlink what from what? give path and to")
	}
	relation := models.AgentEdgeRelation(strings.ToLower(strings.TrimSpace(arguments.Relation)))
	if relation == "" {
		relation = models.EdgeRelatedTo
	}
	agentId := run.Agent().ID
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		fromNode, err := tx.GetAgentNode(agentId, from)
		if err != nil {
			return err
		}
		if fromNode == nil {
			return fmt.Errorf("there is no page at %s", from)
		}
		toNode, err := tx.GetAgentNode(agentId, to)
		if err != nil {
			return err
		}
		if toNode == nil {
			return fmt.Errorf("there is no page at %s", to)
		}
		return tx.DeleteAgentEdge(agentId, fromNode.ID, toNode.ID, relation)
	}); err != nil {
		return nil, err
	}
	result := tools.TextResult("%s and %s are no longer joined by %s", from, to, relation)
	result.Note = "unlinked " + from + " from " + to
	return result, nil
}

// mergeAction folds one page into another: two pages for one thing is
// the graph's commonest wrong shape, and the person notices it first.
func mergeAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	path := models.NormalizePath(arguments.Path)
	into := models.NormalizePath(arguments.To)
	if path == "" || into == "" {
		return nil, fmt.Errorf("merge what into what? give path and to")
	}
	for _, root := range models.AgentRoots {
		if root.Path == path {
			return nil, fmt.Errorf("%s is one of the places things are filed, and cannot be merged away", path)
		}
	}
	var survivor *models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		survivor, err = tx.MergeAgentNodes(run.Agent().ID, path, into)
		return err
	}); err != nil {
		return nil, err
	}
	result := tools.TextResult("%s is now part of %s", path, survivor.Path)
	result.Note = "merged " + path + " into " + survivor.Path
	return result, nil
}

// moveAction files a page somewhere else, or one fact onto another page.
func moveAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	path := models.NormalizePath(arguments.Path)
	under := models.NormalizePath(arguments.To)
	if path == "" {
		return nil, fmt.Errorf("move what? give a path")
	}
	if arguments.Number > 0 {
		return moveFactAction(ctx, run, path, under, arguments.Number)
	}
	var moved *models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		moved, err = tx.MoveAgentNode(run.Agent().ID, path, under)
		return err
	}); err != nil {
		return nil, err
	}
	result := tools.TextResult("%s", path+" is now "+moved.Path)
	result.Note = "filed " + path + " under " + tools.Named(under, "the top")
	return result, nil
}

// moveFactAction puts one fact on another page.
//
// Without it the only way to correct a sentence filed under the wrong
// name is to forget it and write it again, which loses the words it came
// from and the day it was learned. The page it lands on is not made on
// the way: a mistyped path would otherwise hide the sentence on a page
// nobody reads.
func moveFactAction(ctx context.Context, run tools.Run, path, to string, number int) (*tools.Result, error) {
	if to == "" {
		return nil, fmt.Errorf("move the fact to which page? give to")
	}
	agentId := run.Agent().ID
	var answer string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		node, err := tx.GetAgentNode(agentId, path)
		if err != nil {
			return err
		}
		if node == nil {
			return fmt.Errorf("there is no page at %s", path)
		}
		fact, err := tx.GetAgentFact(agentId, node.ID, number)
		if err != nil {
			return err
		}
		if fact == nil {
			return fmt.Errorf("there is no %s", path+"#"+strconv.Itoa(number))
		}
		destination, err := tx.GetAgentNode(agentId, to)
		if err != nil {
			return err
		}
		if destination == nil {
			return fmt.Errorf("there is no page at %s", to)
		}
		moved, err := tx.MoveAgentFact(agentId, fact.ID, destination.ID)
		if err != nil {
			return err
		}
		answer = path + "#" + strconv.Itoa(number) + " is now " + moved.Reference(to)
		return nil
	}); err != nil {
		return nil, err
	}
	result := tools.TextResult("%s", answer)
	result.Note = answer
	return result, nil
}

// forgetAction removes a fact, or a whole page.
func forgetAction(ctx context.Context, run tools.Run, arguments *memoryArguments) (*tools.Result, error) {
	path := models.NormalizePath(arguments.Path)
	if path == "" {
		return nil, fmt.Errorf("forget what? give a path, and a number for one fact")
	}
	agentId := run.Agent().ID
	var answer string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		node, err := tx.GetAgentNode(agentId, path)
		if err != nil {
			return err
		}
		if node == nil {
			return fmt.Errorf("there is no page at %s", path)
		}
		if arguments.Number > 0 {
			fact, err := tx.GetAgentFact(agentId, node.ID, arguments.Number)
			if err != nil {
				return err
			}
			if fact == nil {
				return fmt.Errorf("there is no %s", path+"#"+strconv.Itoa(arguments.Number))
			}
			if err := tx.DeleteAgentFact(agentId, fact.ID); err != nil {
				return err
			}
			answer = "forgot " + fact.Reference(path)
			return nil
		}
		removed, err := tx.DeleteAgentNode(agentId, path)
		if err != nil {
			return err
		}
		answer = fmt.Sprintf("forgot %s and %d page(s) under it", path, removed-1)
		return nil
	}); err != nil {
		return nil, err
	}
	result := tools.TextResult("%s", answer)
	result.Note = answer
	return result, nil
}

// batchAction runs several of the above. Bounded, and each answers for
// itself, so one bad path does not lose the rest.
func batchAction(ctx context.Context, run tools.Run, call *tools.Call, arguments *memoryArguments) (*tools.Result, error) {
	if len(arguments.Items) > batchItems {
		return nil, fmt.Errorf("a batch takes up to %d items at a time; send them in several calls", batchItems)
	}
	lines := make([]string, 0, len(arguments.Items))
	for _, item := range arguments.Items {
		inner := &memoryArguments{memoryItem: item}
		result, err := runMemoryItem(ctx, run, call, inner)
		if err != nil {
			lines = append(lines, item.Action+" "+item.Path+": "+err.Error())
			continue
		}
		lines = append(lines, result.Content)
	}
	return tools.TextResult("%s", strings.Join(lines, "\n")), nil
}

// --- meaning ----------------------------------------------------------

// resolvePage asks the run which page this belongs on, where the run can
// say; otherwise the path is taken at face value.
func resolvePage(ctx context.Context, run tools.Run, tx db.Transaction, path string, kind models.AgentNodeKind, name string) (*models.AgentNode, error) {
	if remembering, ok := run.(tools.Remembering); ok {
		return remembering.ResolvePage(ctx, tx, path, kind, name)
	}
	existing, err := tx.GetAgentNode(run.Agent().ID, path)
	if err != nil || existing != nil {
		return existing, err
	}
	if !models.IsAgentNodeKind(kind) {
		kind = kindFromPath(path)
	}
	if name == "" {
		name = titleFromSlug(models.LastSegment(path))
	}
	return tx.PutAgentNode(&models.AgentNode{AgentID: run.Agent().ID, Path: path, Kind: kind, Name: name})
}

// noteMeaning gives a fact its vector and names whatever on the same page
// it may be a copy of, where the deployment can say what a fact means.
func noteMeaning(ctx context.Context, run tools.Run, fact *models.AgentFact) []*models.AgentFact {
	remembering, ok := run.(tools.Remembering)
	if !ok || fact == nil {
		return nil
	}
	return remembering.NoteFact(ctx, fact)
}

func noteNodeMeaning(ctx context.Context, run tools.Run, node *models.AgentNode) {
	if remembering, ok := run.(tools.Remembering); ok && node != nil {
		remembering.NoteNode(ctx, node)
	}
}

// recalledOverlay is what this turn's words already brought back, so the
// model does not go looking for what is in front of it.
func recalledOverlay(ctx context.Context) string {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return ""
	}
	lines := run.Recalled()
	if len(lines) == 0 {
		return ""
	}
	return "<recalled>\n" + strings.Join(lines, "\n") + "\n</recalled>"
}

// factAudiences is the runs a fact is addressed to, as the model named
// them: lowercased, unknown names dropped, and always the conversation. A
// person who says "remember this" expects the agent they said it to to
// remember it; a fact kept for sorting alone is one the conversation never
// sees, and the agent then says it knows nothing.
func factAudiences(names []string) []models.AgentAudience {
	audiences := []models.AgentAudience{models.AudienceAsk}
	for _, name := range names {
		audience := models.AgentAudience(strings.ToLower(strings.TrimSpace(name)))
		if audience == models.AudienceAsk {
			continue
		}
		for _, known := range models.AgentAudiences {
			if audience == known {
				audiences = append(audiences, audience)
				break
			}
		}
	}
	return audiences
}
