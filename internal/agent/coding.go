package agent

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A coding tool's session (Claude Code, Codex) is shown what memory knows
// about the checkout it runs in when it starts, and what each prompt
// recalls before the tool answers it, by hooks the person installs with
// `teanode hook install`. Everything here only reads: nothing a session
// is shown moves a fact's `used_at`, since the person did not ask the
// agent anything. See docs/planning/coding-session-memory-execplan.md.

const (
	// codingFactTokens is what the session-start block spends on the
	// project pages' own facts.
	codingFactTokens = 900

	// codingFacts is the most it shows of the first project page, and
	// codingOtherFacts of each other page the checkout is filed on.
	codingFacts      = 6
	codingOtherFacts = 3

	// codingRequests is how many of the person's last requests in the
	// previous session it repeats, and codingRequestCharacters how much
	// of each; codingAnswerCharacters is how much of the assistant's
	// last answer.
	codingRequests          = 3
	codingRequestCharacters = 300
	codingAnswerCharacters  = 700

	// codingDocuments is how many of the newest chat units held in the
	// directory are read to find the previous session. A session being
	// resumed may itself hold many, and the one before it is behind them.
	codingDocuments = 200

	// codingSessionUnits is how many units of the previous session are
	// read for its last words.
	codingSessionUnits = 6

	// codingPromptWords is the fewest words a prompt needs before it is
	// worth a recall: "yes", "go on" and "commit it" recall noise.
	codingPromptWords = 3
)

// codingLessonFloor is how alike a lesson must be to a prompt before a
// coding session is shown it, well above what a turn asks: the lessons are
// read from the agent's own errands, and at the turn's floor a prompt about
// a network failure in the code was shown how to resume a photo download.
const codingLessonFloor = 0.45

// CodingMemoryTag wraps what a coding session is shown, so that the
// sources reading its transcript back can leave it out: memory filed
// again from its own recital would confirm itself.
const CodingMemoryTag = "teanode-memory"

// CodingAssistants are the source types whose transcripts are coding
// sessions, which a capture asks to read again.
var CodingAssistants = []string{"claude-code", "codex"}

// CodingRequest is where a coding session is and what it asks.
type CodingRequest struct {
	// Directory is the session's working directory, absolute, on the
	// computer ComputerName; HomeDirectory is that computer's home
	// directory, which a checkout recorded with a leading ~ is under.
	Directory     string
	ComputerName  string
	HomeDirectory string

	// SessionID is the coding tool's own id for the session, so that the
	// session being resumed is not reported as the one before it.
	SessionID string

	// RemoteURLs, Head and CheckoutRoot are what git says about the
	// checkout the session runs in, where it is one: where it is pushed,
	// the commit it is at, and its top directory. A checkout on another
	// computer is the same project only when it is the same repository,
	// whatever its path, and memory read at another commit may not say
	// what is here.
	RemoteURLs   []string
	Head         string
	CheckoutRoot string

	// Prompt is what the person typed, for a prompt's recall.
	Prompt string

	// ShownPaths are pages the session was shown in its last few
	// prompts, which are not shown again.
	ShownPaths []string

	// IsEverywhere recalls from the whole graph instead of the checkout's
	// project and what it links to.
	IsEverywhere bool
}

// CodingContext is what a coding session is shown.
type CodingContext struct {
	// ProjectPath is the page of the checkout the session runs in, and
	// CheckoutDirectory where that checkout is; both empty where the
	// directory is in no checkout memory knows.
	ProjectPath       string
	CheckoutDirectory string

	// ReadFrom says where and at which commit memory read the project,
	// where that is not the session's own checkout as it stands: another
	// computer's copy of the repository, or this one at an older commit.
	// Empty where they are the same, or where the session's commit is not
	// known.
	ReadFrom string

	// Pages are the pages shown and the facts shown from each.
	Pages []*RecalledPage

	// Lessons are the lessons shown, a line each.
	Lessons []string

	// LastSession is the previous session in the directory, at session
	// start only.
	LastSession *CodingSession

	// ShownPaths is every page shown, for the session to skip next time.
	ShownPaths []string

	// Text is all of it as the session reads it, wrapped in
	// CodingMemoryTag; empty where there is nothing to show.
	Text string
}

// CodingSession is a previous session in a directory: what it was
// called, when it was last active, and how it ended.
type CodingSession struct {
	Title        string
	Assistant    string
	LastActiveAt time.Time
	Requests     []string
	LastAnswer   string
}

// codingCheckout is the checkout a directory is in and the project pages
// it is filed on, the one whose line was written last first; and the
// checkout memory read it from, which is another computer's where the
// session's own was never profiled.
type codingCheckout struct {
	projects  []*models.AgentNode
	directory string

	readDirectory, readComputer, readHead string
	isReadElsewhere                       bool
}

// readFromOf says where memory read a checkout, where that is not the
// session's own as it stands.
func readFromOf(checkout *codingCheckout, request *CodingRequest) string {
	readHead := shortCommit(checkout.readHead)
	switch {
	case checkout.isReadElsewhere:
		where := "Memory of this project was read from another checkout of it"
		if checkout.readDirectory != "" {
			where = "Memory of this project was read from its checkout at " + checkout.readDirectory
			if checkout.readComputer != "" {
				where += " on " + checkout.readComputer
			}
		}
		if readHead != "" {
			where += " (commit " + readHead + ")"
		}
		return where + ", not this one, which may be at another commit or branch: what it says about the code may not match what is here."
	case readHead != "" && request.Head != "" && !strings.HasPrefix(request.Head, checkout.readHead) && !strings.HasPrefix(checkout.readHead, request.Head):
		return "Memory last read this checkout at commit " + readHead + "; it is now at " + shortCommit(request.Head) + ", so what it says about the code may be out of date."
	}
	return ""
}

func shortCommit(commit string) string {
	if len(commit) > 10 {
		return commit[:10]
	}
	return commit
}

// CodingSessionStart is what a coding session starting in a directory is
// shown: the checkout's project pages and their liveliest facts, and where
// the last session there stopped. Lessons wait for a prompt: matched to a
// whole project they were about anything at all.
func (self *Agent) CodingSessionStart(ctx context.Context, found *models.Agent, owner *models.User, request *CodingRequest) (*CodingContext, error) {
	if self == nil || found == nil || owner == nil {
		return nil, ErrUnavailable
	}
	result := &CodingContext{Pages: []*RecalledPage{}, Lessons: []string{}, ShownPaths: []string{}}
	var checkout *codingCheckout
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if checkout, err = checkoutOfDirectory(tx, found.ID, request); err != nil || checkout == nil {
			return err
		}
		result.ProjectPath, result.CheckoutDirectory = checkout.projects[0].Path, checkout.directory
		result.ReadFrom = readFromOf(checkout, request)
		spent := 0
		for index, project := range checkout.projects {
			facts, err := tx.ListAgentFactsLively(found.ID, project.ID, 60)
			if err != nil {
				return err
			}
			most := codingFacts
			if index > 0 {
				most = codingOtherFacts
			}
			page := &RecalledPage{Path: project.Path, Summary: strings.TrimSpace(project.Summary), Facts: []*models.AgentFact{}}
			for _, fact := range facts {
				// What the checkout's profile computed (its remotes, its
				// languages, where it is) is in the files the session
				// can read for itself; the facts worth its room are what
				// was said and decided.
				if _, isKeyed := repositoryKeyOf(fact); isKeyed || !stillStands(fact) {
					continue
				}
				cost := llm.EstimateTokens(fact.Line())
				if len(page.Facts) >= most || spent+cost > codingFactTokens {
					continue
				}
				spent += cost
				page.Facts = append(page.Facts, fact)
			}
			result.Pages = append(result.Pages, page)
			result.ShownPaths = append(result.ShownPaths, page.Path)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		// Held in this directory, else anywhere in the checkout's own:
		// a session opened in a folder of the checkout carries on from
		// the one opened at its top.
		directories := []string{cleanDirectory(request.Directory, request.HomeDirectory)}
		if result.CheckoutDirectory != "" && result.CheckoutDirectory != directories[0] {
			directories = append(directories, result.CheckoutDirectory)
		}
		for _, directory := range directories {
			if result.LastSession, err = lastCodingSession(tx, found.ID, owner, directory, request.ComputerName, request.SessionID); err != nil || result.LastSession != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	result.Text = renderCodingStart(result)
	return result, nil
}

// CodingPromptRecall is what a prompt typed in a coding session recalls:
// the recall a turn would carry, kept to the checkout's project, the pages
// under it, the pages it links to and the lessons, less the pages the
// session was shown in its last few prompts.
func (self *Agent) CodingPromptRecall(ctx context.Context, found *models.Agent, owner *models.User, request *CodingRequest) (*CodingContext, error) {
	if self == nil || found == nil || owner == nil {
		return nil, ErrUnavailable
	}
	result := &CodingContext{Pages: []*RecalledPage{}, Lessons: []string{}, ShownPaths: []string{}}
	prompt := strings.TrimSpace(request.Prompt)
	// A command to the tool, or a word of assent, is not a question, and
	// nor is what the tool itself sends as a prompt (a finished background
	// task arrives wrapped in a tag of its own).
	if strings.HasPrefix(prompt, "/") || strings.HasPrefix(prompt, "<") || len(strings.Fields(prompt)) < codingPromptWords {
		return result, nil
	}
	shown := map[string]bool{}
	for _, shownPath := range request.ShownPaths {
		shown[shownPath] = true
	}
	var isInScope func(string) bool
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		checkout, err := checkoutOfDirectory(tx, found.ID, request)
		if err != nil || checkout == nil {
			return err
		}
		result.ProjectPath, result.CheckoutDirectory = checkout.projects[0].Path, checkout.directory
		if !request.IsEverywhere {
			isInScope, err = codingScope(tx, found.ID, checkout.projects)
		}
		return err
	}); err != nil {
		return nil, err
	}
	if !request.IsEverywhere && isInScope == nil {
		// No checkout memory knows: nothing is scoped to it, and the
		// whole graph is not offered in its place.
		return result, nil
	}
	isWanted := func(pagePath string) bool {
		return !shown[pagePath] && (isInScope == nil || isInScope(pagePath))
	}
	pages, err := self.recallForQuestion(ctx, found, owner, prompt, nil, nil, isWanted)
	if err != nil {
		return nil, err
	}
	for _, page := range pages {
		if len(page.Facts) == 0 && page.Summary == "" && page.Overview == "" {
			continue
		}
		result.Pages = append(result.Pages, page)
		result.ShownPaths = append(result.ShownPaths, page.Path)
	}
	result.Lessons = self.codingLessons(ctx, found, owner, prompt, request.ShownPaths)
	result.ShownPaths = append(result.ShownPaths, pathsOfLessons(result.Lessons)...)
	result.Text = renderCodingRecall(result)
	return result, nil
}

// CaptureCodingSession asks the computer's sources of a coding tool's
// transcripts to read again now, so that what was just said is searchable,
// and the next session can be told where this one stopped, within a
// minute rather than at the source's next scheduled pass. It says whether
// any source was asked. A source the person paused stays paused. One asked
// while a pass runs reads again after it (see markSource), and a burst of
// short answers before a pass begins is that one pass.
func CaptureCodingSession(tx db.Transaction, agentId, computerName, assistant string) (bool, error) {
	if !slices.Contains(CodingAssistants, assistant) {
		return false, fmt.Errorf("a coding tool is one of %s, not %q", strings.Join(CodingAssistants, ", "), assistant)
	}
	sources, err := tx.ListAgentSources(agentId)
	if err != nil {
		return false, err
	}
	isAsked := false
	now := time.Now()
	for _, source := range sources {
		if !source.Enabled || source.Specification.Type != assistant || source.Specification.Computer != computerName {
			continue
		}
		// Even when it is already due: a pass may be on its first page,
		// and have read the transcript before the answer was written.
		if err := tx.RequestAgentSourceRun(source.ID, now); err != nil {
			return isAsked, err
		}
		isAsked = true
	}
	return isAsked, nil
}

// codingMemory is the memory tool's checkout: what a coding session in a
// directory is shown, the session-start block without a prompt and the
// prompt's recall with one.
func (self *Agent) codingMemory(ctx context.Context, found *models.Agent, owner *models.User, directory, computerName, prompt string) (string, error) {
	// The computer's home directory, where it is attached: a checkout's
	// line may say ~/..., and the transcripts name absolute directories.
	request := &CodingRequest{Directory: directory, ComputerName: computerName, Prompt: prompt}
	if attached := self.computerNamed(found.ID, computerName); attached != nil {
		request.HomeDirectory = attached.Home()
		if request.ComputerName == "" {
			request.ComputerName = attached.Name()
		}
	}
	var shown *CodingContext
	var err error
	if prompt == "" {
		shown, err = self.CodingSessionStart(ctx, found, owner, request)
	} else {
		shown, err = self.CodingPromptRecall(ctx, found, owner, request)
	}
	if err != nil {
		return "", err
	}
	return shown.Text, nil
}

// CodingMemory is tools.CodingRemembering for a turn.
func (self *AskRun) CodingMemory(ctx context.Context, directory, computerName, prompt string) (string, error) {
	return self.agent.codingMemory(ctx, self.settings.Agent, self.settings.Owner, directory, computerName, prompt)
}

// checkoutOfDirectory is the checkout a directory is in, and the project
// pages it is filed on; nil where memory knows no checkout holding it.
//
// First by path, from the lines that say where each checkout is, on the
// session's own computer (on any, where none is named): of the checkouts
// that hold the directory the deepest wins. A path on another computer
// proves nothing -- the same path may hold another repository, or this one
// at another commit -- so a checkout this computer never profiled is found
// by its repository instead: the remote git says it is pushed to, matched
// to the line its profile wrote about where it lives.
//
// The same checkout may be filed on more than one project page (the
// profile's own, and an older page the night grew around it), and every
// one of them is kept.
func checkoutOfDirectory(tx db.Transaction, agentId string, request *CodingRequest) (*codingCheckout, error) {
	directory := cleanDirectory(request.Directory, request.HomeDirectory)
	if directory == "" {
		return nil, nil
	}
	checkout, err := checkoutByPath(tx, agentId, request, directory)
	if err != nil || checkout != nil {
		return checkout, err
	}
	return checkoutByRemote(tx, agentId, request, directory)
}

func checkoutByPath(tx db.Transaction, agentId string, request *CodingRequest, directory string) (*codingCheckout, error) {
	facts, err := tx.ListAgentFactsStartingWith(agentId, checkoutLinePrefix, 0)
	if err != nil {
		return nil, err
	}
	var holding []*models.AgentFact
	bestDirectory, readComputer := "", ""
	for _, fact := range facts {
		where, computerName, isCheckout := checkoutLocationOf(fact.Text)
		if !isCheckout || (request.ComputerName != "" && computerName != request.ComputerName) {
			continue
		}
		where = cleanDirectory(where, request.HomeDirectory)
		if where == "" || (directory != where && !strings.HasPrefix(directory, where+"/")) || len(where) < len(bestDirectory) {
			continue
		}
		if len(where) > len(bestDirectory) {
			holding, bestDirectory = nil, where
		}
		holding = append(holding, fact)
		readComputer = computerName
	}
	if len(holding) == 0 {
		return nil, nil
	}
	checkout := &codingCheckout{directory: bestDirectory, readDirectory: bestDirectory, readComputer: readComputer, readHead: profiledCommitOf(holding[0])}
	if err := checkout.addProjects(tx, agentId, holding, path.Base(bestDirectory)); err != nil || len(checkout.projects) == 0 {
		return nil, err
	}
	return checkout, nil
}

func checkoutByRemote(tx db.Transaction, agentId string, request *CodingRequest, directory string) (*codingCheckout, error) {
	wanted := map[string]bool{}
	for _, remote := range request.RemoteURLs {
		if normalized := normalizeRemote(remote); normalized != "" {
			wanted[normalized] = true
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	facts, err := tx.ListAgentFactsStartingWith(agentId, remoteLinePrefix, 0)
	if err != nil {
		return nil, err
	}
	var holding []*models.AgentFact
	repository := ""
	for _, fact := range facts {
		// The line a profile wrote, not a sentence of the person's that
		// happens to start the same way ("Lives at the old house.").
		if key, isKeyed := repositoryKeyOf(fact); !isKeyed || key != "remote" {
			continue
		}
		remote := strings.TrimSuffix(strings.TrimPrefix(fact.Text, remoteLinePrefix), ".")
		if normalized := normalizeRemote(remote); wanted[normalized] {
			holding = append(holding, fact)
			repository = path.Base(normalized)
		}
	}
	if len(holding) == 0 {
		return nil, nil
	}
	root := cleanDirectory(request.CheckoutRoot, request.HomeDirectory)
	if root == "" {
		root = directory
	}
	checkout := &codingCheckout{directory: root, readHead: profiledCommitOf(holding[0]), isReadElsewhere: true}
	lines, err := tx.ListAgentFactsStartingWith(agentId, checkoutLinePrefix, 0)
	if err != nil {
		return nil, err
	}
	// Where the profile read it: the checkout line written in the same
	// pass, which carries the same commit, else one on the same page.
	var samePage *models.AgentFact
	for _, line := range lines {
		if checkout.readHead != "" && profiledCommitOf(line) == checkout.readHead {
			samePage = line
			break
		}
		if samePage == nil && line.NodeID == holding[0].NodeID {
			samePage = line
		}
	}
	if samePage != nil {
		checkout.readDirectory, checkout.readComputer, _ = checkoutLocationOf(samePage.Text)
	}
	if err := checkout.addProjects(tx, agentId, holding, repository); err != nil || len(checkout.projects) == 0 {
		return nil, err
	}
	return checkout, nil
}

// addProjects adds the project page each line is on, once each.
func (self *codingCheckout) addProjects(tx db.Transaction, agentId string, lines []*models.AgentFact, folder string) error {
	isKept := map[string]bool{}
	for _, line := range lines {
		project, err := projectOfCheckoutLine(tx, agentId, line, folder)
		if err != nil {
			return err
		}
		if project != nil && !isKept[project.ID] {
			isKept[project.ID] = true
			self.projects = append(self.projects, project)
		}
	}
	return nil
}

// The beginnings of the lines a checkout's profile writes about where it
// is and where it is pushed (checkoutLine, and the "remote" line).
const (
	checkoutLinePrefix = "The checkout is at "
	remoteLinePrefix   = "Lives at "
)

// profiledCommitOf is the commit a checkout's profile read when it wrote
// a line, which it keeps as the line's evidence.
func profiledCommitOf(fact *models.AgentFact) string {
	for _, evidence := range fact.Evidence {
		if evidence.Kind == models.EvidenceRepository && evidence.ID != "" {
			return evidence.ID
		}
	}
	return ""
}

// normalizeRemote is a git remote written so that the ways of spelling
// one repository compare equal: ssh://git@host:22/group/name.git,
// git@host:group/name and https://host/group/name are all
// "host/group/name".
func normalizeRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	if scheme := strings.Index(remote, "://"); scheme >= 0 {
		remote = remote[scheme+3:]
	} else if colon := strings.Index(remote, ":"); colon > 0 && !strings.Contains(remote[:colon], "/") {
		// The scp form, host:path.
		remote = remote[:colon] + "/" + remote[colon+1:]
	}
	if at := strings.Index(remote, "@"); at >= 0 && at < strings.Index(remote+"/", "/") {
		remote = remote[at+1:]
	}
	host, rest, _ := strings.Cut(remote, "/")
	host, _, _ = strings.Cut(host, ":")
	rest = strings.TrimSuffix(strings.TrimSuffix(rest, "/"), ".git")
	if host == "" || rest == "" {
		return ""
	}
	return strings.ToLower(host) + "/" + rest
}

// projectOfCheckoutLine is the project page a checkout line belongs to:
// the nearest page at or above the line's own named for the checkout's
// folder, else the highest project above it, else the line's page.
func projectOfCheckoutLine(tx db.Transaction, agentId string, fact *models.AgentFact, folder string) (*models.AgentNode, error) {
	node, err := tx.GetAgentNodeByID(agentId, fact.NodeID)
	if err != nil || node == nil {
		return nil, err
	}
	var named, highestProject *models.AgentNode
	for current := node; current != nil; {
		name := path.Base(current.Path)
		if named == nil && (name == folder || strings.HasPrefix(name, folder+"-")) {
			named = current
		}
		if current.Kind == models.NodeProject {
			highestProject = current
		}
		if current.ParentID == "" {
			break
		}
		if current, err = tx.GetAgentNodeByID(agentId, current.ParentID); err != nil {
			return nil, err
		}
	}
	switch {
	case named != nil:
		return named, nil
	case highestProject != nil:
		return highestProject, nil
	}
	return node, nil
}

// cleanDirectory is a directory as an absolute path with no trailing
// slash, a leading ~ read as the home directory given (or kept, where none
// is); empty for one that is neither absolute nor under a home directory.
func cleanDirectory(directory, homeDirectory string) string {
	directory = strings.TrimSpace(directory)
	if directory == "~" || strings.HasPrefix(directory, "~/") {
		// With no home directory to read it as, it is compared as it is
		// written, which still finds a checkout recorded the same way.
		if homeDirectory == "" {
			return path.Clean(directory)
		}
		directory = homeDirectory + directory[1:]
	}
	if !strings.HasPrefix(directory, "/") {
		return ""
	}
	return path.Clean(directory)
}

// isCodingKind is the kinds of page linked to a project that a coding
// session may be shown: other work, not people, months or the person's own
// page. A colleague's page linked to a project holds what they do at work,
// which is not the coding tool's business, and the first live session
// recalled exactly that.
var isCodingKind = map[models.AgentNodeKind]bool{
	models.NodeProject: true, models.NodeTopic: true, models.NodeThing: true, models.NodeFolder: true,
}

// codingScope is the pages a coding session's recall may carry: the
// projects' pages and those under them, and the work pages linked to them
// either way. Not lessons: they come through codingLessons, at a closer
// match than recall's.
func codingScope(tx db.Transaction, agentId string, projects []*models.AgentNode) (func(string) bool, error) {
	var linkedIds []string
	for _, project := range projects {
		edges, err := tx.ListAgentEdges(agentId, project.ID)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			// A link the night guessed is a guess, and a page it reaches
			// is not the project's business until somebody says so.
			if edge.Status == models.EdgeProposed {
				continue
			}
			if edge.FromID == project.ID {
				linkedIds = append(linkedIds, edge.ToID)
			} else {
				linkedIds = append(linkedIds, edge.FromID)
			}
		}
	}
	linked := map[string]bool{}
	if len(linkedIds) > 0 {
		nodes, err := tx.GetAgentNodes(agentId, linkedIds)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			if isCodingKind[node.Kind] {
				linked[node.Path] = true
			}
		}
	}
	return func(pagePath string) bool {
		for _, project := range projects {
			if pagePath == project.Path || strings.HasPrefix(pagePath, project.Path+"/") {
				return true
			}
		}
		return linked[pagePath]
	}, nil
}

// codingLessons is the lessons a question recalls that are close to it,
// less those on pages already shown.
func (self *Agent) codingLessons(ctx context.Context, found *models.Agent, owner *models.User, question string, shownPaths []string) []string {
	run := &AskRun{agent: self, settings: &AskSettings{Agent: found, Owner: owner, Message: question}, promptMemories: map[string]bool{}}
	run.ctx = ctx
	lessons := []string{}
	for _, line := range run.lessonLinesAbove(ctx, question, codingLessonFloor) {
		if !slices.Contains(shownPaths, lessonPathOf(line)) {
			lessons = append(lessons, line)
		}
	}
	return lessons
}

// lessonPathOf is the page a lesson line names: "- lessons/builds#3 ..."
// is on lessons/builds.
func lessonPathOf(line string) string {
	reference, _, _ := strings.Cut(strings.TrimPrefix(line, "- "), " ")
	pagePath, _, _ := strings.Cut(reference, "#")
	return pagePath
}

// pathsOfLessons is the pages the lesson lines are on.
func pathsOfLessons(lines []string) []string {
	var paths []string
	for _, line := range lines {
		if pagePath := lessonPathOf(line); pagePath != "" && !slices.Contains(paths, pagePath) {
			paths = append(paths, pagePath)
		}
	}
	return paths
}

// postLine is the first line of a post in a chat unit: "15:04 author: words".
var postLine = regexp.MustCompile(`^\d{1,2}:\d{2} ([^:\n]{1,80}): (.*)$`)

// codingPost is one post of a coding session as its unit was filed.
type codingPost struct {
	author string
	text   string
}

// lastCodingSession is the newest session held in a directory on a
// computer other than the one asking, with the person's last requests and the
// assistant's last answer read from its newest units; nil where there was
// none.
func lastCodingSession(tx db.Transaction, agentId string, owner *models.User, directory, computerName, sessionId string) (*CodingSession, error) {
	if directory == "" {
		return nil, nil
	}
	// On the session's own computer, where it is named: the same path on
	// another computer is another checkout, and its last session is not
	// this one's. Narrowed in the query, so another computer's newer
	// sessions do not use up the rows read.
	var sourceIds []string
	if computerName != "" {
		sources, err := tx.ListAgentSources(agentId)
		if err != nil {
			return nil, err
		}
		for _, source := range sources {
			if source.Specification.Computer == computerName {
				sourceIds = append(sourceIds, source.ID)
			}
		}
		if len(sourceIds) == 0 {
			return nil, nil
		}
	}
	documents, err := tx.ListAgentCodingDocuments(agentId, directory, sourceIds, codingDocuments)
	if err != nil || len(documents) == 0 {
		return nil, err
	}
	sessionOf := func(document *models.AgentDocument) string {
		file, _, _ := strings.Cut(document.ExternalID, "#")
		return document.SourceID + " " + file
	}
	var units []*models.AgentDocument
	chosen := ""
	for _, document := range documents {
		session := sessionOf(document)
		if sessionId != "" && strings.Contains(session, sessionId) {
			continue
		}
		if chosen == "" {
			chosen = session
		}
		if session == chosen && len(units) < codingSessionUnits {
			units = append(units, document)
		}
	}
	if len(units) == 0 {
		return nil, nil
	}
	newest := units[0]
	session := &CodingSession{Title: strings.TrimSpace(fmt.Sprint(newest.Metadata["channel"])), Assistant: strings.TrimSpace(fmt.Sprint(newest.Metadata["assistant"]))}
	if newest.HappenedAt != nil {
		session.LastActiveAt = *newest.HappenedAt
	}
	var posts []*codingPost
	for index := len(units) - 1; index >= 0; index-- {
		chunks, err := tx.ListAgentChunks(agentId, units[index].ID)
		if err != nil {
			return nil, err
		}
		posts = append(posts, postsOf(chunks)...)
	}
	for index := len(posts) - 1; index >= 0; index-- {
		post := posts[index]
		switch {
		case post.author == owner.Username && len(session.Requests) < codingRequests:
			session.Requests = append([]string{cutWords(withoutTags(post.text), codingRequestCharacters)}, session.Requests...)
		case post.author == session.Assistant && session.LastAnswer == "":
			session.LastAnswer = cutWords(withoutTags(post.text), codingAnswerCharacters)
		}
	}
	return session, nil
}

// markupTag is a tag a tool wraps pasted or injected text in, such as
// <pasted_content id="...">: noise in a request quoted back.
var markupTag = regexp.MustCompile(`</?[a-z][a-z_-]*(\s[^<>]*)?>`)

func withoutTags(text string) string {
	return markupTag.ReplaceAllString(text, "")
}

// postsOf is the posts in a unit's passages. Passages overlap, so a line
// already read in the passage before is not read twice.
func postsOf(chunks []*models.AgentChunk) []*codingPost {
	var posts []*codingPost
	var previous []string
	for _, chunk := range chunks {
		lines := strings.Split(chunk.Text, "\n")
		start := overlapOf(previous, lines)
		for _, line := range lines[start:] {
			if match := postLine.FindStringSubmatch(line); match != nil {
				posts = append(posts, &codingPost{author: match[1], text: match[2]})
			} else if len(posts) > 0 {
				posts[len(posts)-1].text += "\n" + line
			}
		}
		previous = lines
	}
	for _, post := range posts {
		post.text = strings.TrimSpace(post.text)
	}
	return posts
}

// overlapOf is how many lines at the start of next repeat the end of
// previous: a passage carries the end of the one before it.
func overlapOf(previous, next []string) int {
	for count := min(len(previous), len(next)); count > 0; count-- {
		if slices.Equal(previous[len(previous)-count:], next[:count]) {
			return count
		}
	}
	// A passage is cut at a line where it can be, so its first line may
	// be the tail of one the previous passage ended in the middle of.
	if len(previous) > 0 && len(next) > 0 && strings.HasSuffix(previous[len(previous)-1], next[0]) {
		return 1
	}
	return 0
}

// cutWords is text cut to at most limit characters at a word, with an
// ellipsis where it was cut.
func cutWords(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	cut := string(runes[:limit])
	if at := strings.LastIndex(cut, " "); at > limit/2 {
		cut = cut[:at]
	}
	return cut + "…"
}

// renderCodingStart is the session-start block as the session reads it.
func renderCodingStart(result *CodingContext) string {
	var text strings.Builder
	if result.ProjectPath != "" {
		fmt.Fprintf(&text, "TeaNode, the person's agent, remembers this about the checkout at %s (page %s). Facts are cited as page#number; its memory tool reads a page whole.\n", result.CheckoutDirectory, result.ProjectPath)
		if result.ReadFrom != "" {
			text.WriteString(result.ReadFrom + "\n")
		}
		writeCodingPages(&text, result.Pages)
	}
	writeCodingLessons(&text, result.Lessons)
	if session := result.LastSession; session != nil {
		fmt.Fprintf(&text, "\nThe last session in this directory (%s, %q, last active %s):\n", session.Assistant, session.Title, ageOf(session.LastActiveAt))
		if len(session.Requests) > 0 {
			text.WriteString("The person's last requests:\n")
			for _, request := range session.Requests {
				text.WriteString("- " + request + "\n")
			}
		}
		if session.LastAnswer != "" {
			text.WriteString("Its last answer: " + session.LastAnswer + "\n")
		}
	}
	return wrapCodingMemory(text.String())
}

// renderCodingRecall is a prompt's recall as the session reads it.
func renderCodingRecall(result *CodingContext) string {
	if len(result.Pages) == 0 && len(result.Lessons) == 0 {
		return ""
	}
	var text strings.Builder
	text.WriteString("What TeaNode, the person's agent, remembers that bears on this prompt (cited as page#number):\n")
	writeCodingPages(&text, result.Pages)
	writeCodingLessons(&text, result.Lessons)
	return wrapCodingMemory(text.String())
}

func writeCodingPages(text *strings.Builder, pages []*RecalledPage) {
	for _, page := range pages {
		text.WriteString("\n" + page.Path)
		if page.Summary != "" {
			text.WriteString(": " + page.Summary)
		}
		text.WriteString("\n")
		if page.Overview != "" {
			text.WriteString(page.Overview + "\n")
		}
		for _, fact := range page.Facts {
			text.WriteString("- " + fact.Reference(page.Path) + " " + fact.Line() + "\n")
		}
	}
}

func writeCodingLessons(text *strings.Builder, lessons []string) {
	if len(lessons) == 0 {
		return
	}
	text.WriteString("\nLessons from work that was verified:\n")
	for _, lesson := range lessons {
		text.WriteString(lesson + "\n")
	}
}

// wrapCodingMemory wraps a block in CodingMemoryTag, or is empty.
func wrapCodingMemory(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return "<" + CodingMemoryTag + ">\n" + text + "\n</" + CodingMemoryTag + ">"
}

// ageOf says how long ago a moment was, in the largest unit that fits.
func ageOf(moment time.Time) string {
	if moment.IsZero() {
		return "at an unknown time"
	}
	age := time.Since(moment)
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return plural(int(age.Minutes()), "1 minute ago", "%d minutes ago")
	case age < 48*time.Hour:
		return plural(int(age.Hours()), "1 hour ago", "%d hours ago")
	default:
		return plural(int(age.Hours()/24), "1 day ago", "%d days ago")
	}
}
