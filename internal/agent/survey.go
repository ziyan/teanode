package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Answering a question about a whole area of what the agent knows.
//
// Recall answers a question some fact or passage is close to. "What are
// the strengths and weaknesses of everything these repositories make
// up" is close to no fact: every page holds a detail, and the handful
// recall picks are a handful of details. What is about the whole is the
// structure the night writes over the graph -- an overview on each page,
// themes grouping the pages, reflections over the themes -- and a survey
// reads it the way that structure was built: each overview in scope is
// asked for its part of the answer, several at once, and one more call
// combines the parts into a report.
//
// The survey makes the calls itself rather than asking a model to fan
// out through subagents, because a fan-out that depends on the model
// remembering to do it sometimes does not happen; here the number of
// pages in scope decides the number of calls.

// The bounds of a survey.
const (
	// surveyConcurrency is how many pages one survey asks at once.
	surveyConcurrency = 6

	// A theme is asked in a survey of a page when at least
	// surveyThemeLeastMembers of its members, and at least
	// surveyThemeShareUnder of them, are under that page.
	surveyThemeLeastMembers = 3
	surveyThemeShareUnder   = 0.3

	// surveyFactCount is how many of a page's facts its run is shown,
	// the most wanted first, and surveyFactLength how much of each. A
	// fact cut short ends with an ellipsis and the get that reads it, and
	// a page with more facts says how many more and how to list them.
	surveyFactCount  = 40
	surveyFactLength = 300

	// surveyHeldCount is how many of the pages a page holds its run is
	// named, and surveyReflectionCount how many reflections are shown;
	// past either, a line says how many more.
	surveyHeldCount       = 30
	surveyReflectionCount = 10

	// surveyPartLength is how much of one page's answer the combining
	// call reads. A part cut there says so, and how much it left out.
	surveyPartLength = 6000

	// surveyTitleLength is how much of the question a run's title
	// carries.
	surveyTitleLength = 60
)

// surveyNothingRelevant is what a page's run answers when nothing on it
// bears on the question. The page counts as covered, and its answer is
// not handed to the combining call.
const surveyNothingRelevant = "NOTHING RELEVANT"

// SurveyReport is what a survey answers with.
type SurveyReport struct {
	// Report is the combined answer in markdown, ending with the pages
	// it covered and any that failed.
	Report string

	// CoveredPaths is the pages whose run answered, and FailedPaths the
	// ones whose run did not: an error, or out of time.
	CoveredPaths []string
	FailedPaths  []string

	// EligiblePageCount is how many pages in scope had an overview to ask,
	// and all of them were asked; WithoutOverviewPageCount how many pages
	// in scope had no overview yet and were not asked.
	EligiblePageCount        int
	WithoutOverviewPageCount int

	// RunIDs is every run the survey made, each a transcript the person
	// can open: one per page, and the one that combined them.
	RunIDs []string
}

// surveyPage is one page a survey asks, with what its run is shown.
type surveyPage struct {
	page        *models.AgentNode
	reflections []string
	facts       []string
	heldPages   []string
}

// surveyScope is what a survey covers: the pages it asks, and the
// reflections over the whole, which the combining call reads.
type surveyScope struct {
	pages       []*surveyPage
	reflections []string

	// eligiblePageCount and withoutOverviewPageCount are the counts the
	// report ends with; see SurveyReport.
	eligiblePageCount        int
	withoutOverviewPageCount int
}

// withOverviews is the pages among these that have an overview to ask,
// counting the live ones that have none.
func (self *surveyScope) withOverviews(pages []*models.AgentNode) []*models.AgentNode {
	written := withOverviews(pages)
	self.withoutOverviewPageCount += len(livePages(pages)) - len(written)
	return written
}

// surveyPart is what one page's run came back with.
type surveyPart struct {
	answer         string
	conversationId string
	isFailed       bool
	isRelevant     bool
}

// Survey answers a question about a whole area of the graph: scopePath
// names a theme, or any page, and empty means everything.
//
// Every run it makes is a run of kind survey, read-only, in a
// conversation of its own. It goes on until every page has answered or ctx
// ends.
func (self *Agent) Survey(ctx context.Context, agent *models.Agent, owner *models.User, question, scopePath string) (*SurveyReport, error) {
	if self == nil || agent == nil || owner == nil || !self.canThink(self.settings.Configuration()) {
		return nil, ErrUnavailable
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, fmt.Errorf("ask a question to survey")
	}
	var scope *surveyScope
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		scope, err = resolveSurveyScope(tx, agent.ID, scopePath)
		return err
	}); err != nil {
		return nil, err
	}
	if len(scope.pages) == 0 {
		return nil, fmt.Errorf("nothing in that scope has an overview yet; the night writes them, and 'teanode agent dream now' starts one")
	}

	run := self.runFor(agent, owner, nil, "")
	knowledgeLanguage := languageName(KnowledgeLanguage(agent, owner))
	shortQuestion := cutAtWord(question, surveyTitleLength)
	parts := self.surveyPages(ctx, run, scope.pages, question, shortQuestion, knowledgeLanguage)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	report := &SurveyReport{
		EligiblePageCount: scope.eligiblePageCount, WithoutOverviewPageCount: scope.withoutOverviewPageCount,
	}
	var answers []string
	for index, part := range parts {
		path := scope.pages[index].page.Path
		if part.conversationId != "" {
			report.RunIDs = append(report.RunIDs, part.conversationId)
		}
		if part.isFailed {
			report.FailedPaths = append(report.FailedPaths, path)
			continue
		}
		report.CoveredPaths = append(report.CoveredPaths, path)
		if part.isRelevant {
			answers = append(answers, fmt.Sprintf("### %s — %s\n%s", path, scope.pages[index].page.Name, surveyPartShown(part.answer)))
		}
	}
	if len(report.CoveredPaths) == 0 {
		return report, fmt.Errorf("no page in scope could answer; the runs are %s", strings.Join(report.RunIDs, ", "))
	}

	combined := ""
	if len(answers) == 0 {
		combined = "Nothing the pages in scope say bears on the question."
	} else {
		prompt, err := render("survey_report.txt", map[string]any{
			"PersonName":        personName(owner),
			"KnowledgeLanguage": knowledgeLanguage,
			"Question":          question,
			"Reflections":       scope.reflections,
			"Answers":           answers,
		})
		if err != nil {
			return nil, err
		}
		thinking, err := self.oneShot(ctx, run, "Survey: "+shortQuestion, prompt, models.AgentJobSurvey, config.AgentWorkResearch)
		if thinking != nil && thinking.Conversation != nil {
			report.RunIDs = append(report.RunIDs, thinking.Conversation.ID)
		}
		if err == nil && thinking != nil {
			combined = thinking.Text
		}
		if strings.TrimSpace(combined) == "" {
			// The parts are the work; a report that could not be
			// written from them still hands them back.
			if err != nil {
				log.Warningf("cannot combine the survey of %q: %s", question, err)
			}
			combined = "The parts could not be combined into one report, so here they are as each page answered.\n\n" + strings.Join(answers, "\n\n")
		}
	}
	report.Report = strings.TrimSpace(combined) + "\n\n" + surveyCoverage(report)
	return report, nil
}

// surveyPages asks each page for its part, at most surveyConcurrency at
// once. The parts are in the order of the pages.
func (self *Agent) surveyPages(ctx context.Context, run *Run, pages []*surveyPage, question, shortQuestion, knowledgeLanguage string) []*surveyPart {
	pagesContext := ctx
	parts := make([]*surveyPart, len(pages))
	slots := make(chan struct{}, surveyConcurrency)
	var pageRuns sync.WaitGroup
	for index, surveyedPage := range pages {
		pageRuns.Add(1)
		go func() {
			defer pageRuns.Done()
			select {
			case slots <- struct{}{}:
			case <-pagesContext.Done():
				parts[index] = &surveyPart{isFailed: true}
				return
			}
			defer func() { <-slots }()
			parts[index] = self.surveyPage(pagesContext, run, surveyedPage, question, shortQuestion, knowledgeLanguage)
		}()
	}
	pageRuns.Wait()
	return parts
}

// surveyPage is one page's run: the question, what the page says, and
// the lookups, with nothing that changes anything.
func (self *Agent) surveyPage(ctx context.Context, run *Run, surveyedPage *surveyPage, question, shortQuestion, knowledgeLanguage string) *surveyPart {
	if ctx.Err() != nil {
		return &surveyPart{isFailed: true}
	}
	page := surveyedPage.page
	prompt, err := render("survey_part.txt", map[string]any{
		"PersonName":        personName(run.Owner),
		"KnowledgeLanguage": knowledgeLanguage,
		"Question":          question,
		"Path":              page.Path,
		"Name":              page.Name,
		"Kind":              string(page.Kind),
		"Opening":           strings.TrimSpace(page.Summary),
		"Overview":          strings.TrimSpace(page.Overview),
		"Reflections":       surveyedPage.reflections,
		"Facts":             surveyedPage.facts,
		"Pages":             surveyedPage.heldPages,
	})
	if err != nil {
		log.Warningf("cannot write the survey prompt for %q: %s", page.Path, err)
		return &surveyPart{isFailed: true}
	}
	// Read-only, with the lookups a dream's describing of a checkout
	// gets: the graph and the sources, and nothing that acts. On the
	// synthesize model rather than the scan one: judging what one area
	// says about a question is most of a survey's thinking, a survey is
	// asked for rather than run every night, and the scan model's parts
	// read as summaries of the overview rather than answers to the
	// question. Unset, synthesize is the research model.
	thinking, err := self.think(ctx, run, fmt.Sprintf("Survey: %s, %s", shortQuestion, page.Path), prompt,
		lookupTools, 0, models.AgentJobSurvey, config.AgentWorkSynthesize)
	part := &surveyPart{}
	if thinking != nil && thinking.Conversation != nil {
		part.conversationId = thinking.Conversation.ID
	}
	// A run stopped for time says whatever it had got to, which is not
	// an answer.
	if err != nil || thinking == nil || ctx.Err() != nil {
		if err != nil {
			log.Warningf("the survey run for %q failed: %s", page.Path, err)
		}
		part.isFailed = true
		return part
	}
	part.answer = strings.TrimSpace(thinking.Text)
	if part.answer == "" {
		part.isFailed = true
		return part
	}
	part.isRelevant = !strings.EqualFold(strings.Trim(part.answer, " .`*\n"), surveyNothingRelevant)
	return part
}

// surveyCoverage is the report's last lines: what it covered, what failed,
// and what in scope it never asked, counted by the code rather than left to
// the model, so an answer from a selection does not read as one about
// everything.
func surveyCoverage(report *SurveyReport) string {
	coveredPaths, failedPaths := report.CoveredPaths, report.FailedPaths
	quoted := func(paths []string) string {
		quotedPaths := make([]string, 0, len(paths))
		for _, path := range paths {
			quotedPaths = append(quotedPaths, "`"+path+"`")
		}
		return strings.Join(quotedPaths, ", ")
	}
	coverage := "---\n\nCovered: " + quoted(coveredPaths) + "."
	if len(failedPaths) > 0 {
		coverage += "\n\nNot covered, the run did not finish: " + quoted(failedPaths) + "."
	}
	if report.WithoutOverviewPageCount > 0 {
		coverage += fmt.Sprintf("\n\nAsked all %d pages in scope that have an overview. %d pages in scope have no overview yet and were not asked.",
			report.EligiblePageCount, report.WithoutOverviewPageCount)
	}
	return coverage
}

// errNoSuchScope is a scope that names no page.
var errNoSuchScope = errors.New("there is no page at that path to survey")

// resolveSurveyScope is the pages a survey asks, and the reflections
// over the whole:
//
//   - a theme of pages: its members that have an overview, with the
//     theme's reflections; for a large theme divided into themes under
//     it, those themes and the members none of them holds;
//   - a theme of themes: the themes under it that have an overview;
//   - any other page: the page if it has an overview, and the pages
//     under it that have one;
//   - nothing: every theme directly under themes that has an overview
//     (the themes of themes, and the themes in no group), with the
//     person's own page of reflections; where none has one yet, every
//     theme that has; and where there are no themes, the most important
//     pages that have an overview.
//
// A scope with nothing in it that has an overview is the scope page
// alone, which still has its opening and its facts. At most
// the most important first.
func resolveSurveyScope(tx db.Transaction, agentId, scopePath string) (*surveyScope, error) {
	scope := &surveyScope{}
	var pages []*models.AgentNode
	reflectionsPath := models.PathReflections
	if scopePath = models.NormalizePath(scopePath); scopePath == "" {
		top, err := tx.ListAgentTopThemes(agentId, themeListed)
		if err != nil {
			return nil, err
		}
		pages = scope.withOverviews(top)
		if len(pages) == 0 {
			themes, err := tx.ListAgentNodesUnder(agentId, models.PathThemes, themeListed)
			if err != nil {
				return nil, err
			}
			// Counted again from this list, which holds the top ones.
			scope.withoutOverviewPageCount = 0
			for _, theme := range scope.withOverviews(themes) {
				if models.IsThemePath(theme.Path) {
					pages = append(pages, theme)
				}
			}
		}
		if len(pages) == 0 {
			written, err := tx.ListAgentNodesWithOverviews(agentId, themeListed)
			if err != nil {
				return nil, err
			}
			pages = written
		}
	} else {
		scopePage, err := tx.GetAgentNode(agentId, scopePath)
		if err != nil {
			return nil, err
		}
		if scopePage == nil || scopePage.Dormant {
			return nil, fmt.Errorf("%w: %s", errNoSuchScope, scopePath)
		}
		reflectionsPath = scopePage.Path
		held, err := heldBy(tx, agentId, scopePage)
		if err != nil {
			return nil, err
		}
		if !models.IsThemePath(scopePage.Path) && strings.TrimSpace(scopePage.Overview) != "" {
			pages = append(pages, scopePage)
		}
		pages = append(pages, scope.withOverviews(held)...)
		if !models.IsThemePath(scopePage.Path) {
			// A page's own children are the tree's view of it; the themes
			// found under it are the links' view, and on a large page
			// they are where the overviews of its parts are.
			themes, err := themesUnder(tx, agentId, scopePage.Path)
			if err != nil {
				return nil, err
			}
			pages = append(pages, themes...)
		}
		if len(pages) == 0 {
			pages = []*models.AgentNode{scopePage}
		}
	}

	sort.SliceStable(pages, func(left, right int) bool {
		if pages[left].Importance != pages[right].Importance {
			return pages[left].Importance > pages[right].Importance
		}
		return pages[left].Path < pages[right].Path
	})
	scope.eligiblePageCount = len(pages)
	for _, page := range pages {
		surveyedPage, err := readSurveyPage(tx, agentId, page)
		if err != nil {
			return nil, err
		}
		scope.pages = append(scope.pages, surveyedPage)
	}

	if reflectionsPage, err := tx.GetAgentNode(agentId, reflectionsPath); err != nil {
		return nil, err
	} else if reflectionsPage != nil {
		facts, err := tx.ListAgentFacts(agentId, reflectionsPage.ID, false, everyFactOnPage)
		if err != nil {
			return nil, err
		}
		// The combining call has no tools, so the line about the
		// reflections left out says how many and not how to read them.
		scope.reflections = reflectionLines(reflectionsPage.Path, facts, false)
	}
	return scope, nil
}

// themesUnder is the themes with an overview whose members are mostly
// pages under path: at least surveyThemeLeastMembers of them, and at least
// surveyThemeShareUnder of its members. Where a theme and a theme it is
// divided into both qualify, both are asked; the survey reads each as one
// part and the combining call sees them together.
func themesUnder(tx db.Transaction, agentId, path string) ([]*models.AgentNode, error) {
	themes, err := tx.ListAgentNodesUnder(agentId, models.PathThemes, themeListed)
	if err != nil {
		return nil, err
	}
	themeById := map[string]*models.AgentNode{}
	for _, theme := range withOverviews(themes) {
		if models.IsThemePath(theme.Path) {
			themeById[theme.ID] = theme
		}
	}
	if len(themeById) == 0 {
		return nil, nil
	}
	edges, err := tx.ListAgentEdgesByRelation(agentId, models.EdgeAboutPlace)
	if err != nil {
		return nil, err
	}
	membersByTheme := map[string][]string{}
	var memberIds []string
	for _, edge := range edges {
		if themeById[edge.FromID] != nil {
			membersByTheme[edge.FromID] = append(membersByTheme[edge.FromID], edge.ToID)
			memberIds = append(memberIds, edge.ToID)
		}
	}
	members, err := tx.GetAgentNodes(agentId, memberIds)
	if err != nil {
		return nil, err
	}
	isUnder := map[string]bool{}
	prefix := path + "/"
	for _, member := range members {
		if member.Path == path || strings.HasPrefix(member.Path, prefix) {
			isUnder[member.ID] = true
		}
	}
	var found []*models.AgentNode
	for themeId, themeMemberIds := range membersByTheme {
		underCount := 0
		for _, memberId := range themeMemberIds {
			if isUnder[memberId] {
				underCount++
			}
		}
		if underCount >= surveyThemeLeastMembers && float64(underCount) >= surveyThemeShareUnder*float64(len(themeMemberIds)) {
			found = append(found, themeById[themeId])
		}
	}
	sort.Slice(found, func(left, right int) bool { return found[left].Path < found[right].Path })
	return found, nil
}

// heldBy is what a page holds, live: a theme of pages its members, a
// theme of themes the themes under it, a large theme the themes it is
// divided into and the members none of them holds, and any other page its
// children.
func heldBy(tx db.Transaction, agentId string, page *models.AgentNode) ([]*models.AgentNode, error) {
	children, err := tx.ListAgentNodeChildren(agentId, page.ID)
	if err != nil {
		return nil, err
	}
	if !models.IsThemePath(page.Path) {
		return livePages(children), nil
	}
	isHeldBelow, err := membersHeldByThemesUnder(tx, agentId, children)
	if err != nil {
		return nil, err
	}
	edges, err := tx.ListAgentEdges(agentId, page.ID)
	if err != nil {
		return nil, err
	}
	var memberIds []string
	for _, edge := range edges {
		if edge.FromID == page.ID && edge.Relation == models.EdgeAboutPlace && !isHeldBelow[edge.ToID] {
			memberIds = append(memberIds, edge.ToID)
		}
	}
	held := livePages(children)
	if len(memberIds) > 0 {
		members, err := tx.GetAgentNodes(agentId, memberIds)
		if err != nil {
			return nil, err
		}
		held = append(held, livePages(members)...)
	}
	return held, nil
}

// readSurveyPage is what one page's run is shown beyond the page itself:
// its reflections, its facts and the pages it holds.
func readSurveyPage(tx db.Transaction, agentId string, page *models.AgentNode) (*surveyPage, error) {
	surveyedPage := &surveyPage{page: page}
	every, err := tx.ListAgentFacts(agentId, page.ID, false, everyFactOnPage)
	if err != nil {
		return nil, err
	}
	statedCount := 0
	for _, fact := range every {
		if fact.Kind != models.FactReflection {
			statedCount++
		}
	}
	facts, err := tx.ListAgentFactsLively(agentId, page.ID, surveyFactCount)
	if err != nil {
		return nil, err
	}
	var stated []*models.AgentFact
	for _, fact := range facts {
		if fact.Kind != models.FactReflection {
			stated = append(stated, fact)
		}
	}
	sort.SliceStable(stated, func(left, right int) bool { return stated[left].Number < stated[right].Number })
	for _, fact := range stated {
		readIt := fmt.Sprintf("memory get with path %s and from %d", page.Path, fact.Number)
		surveyedPage.facts = append(surveyedPage.facts, fact.Reference(page.Path)+" "+cutWithMore(fact.Line(), surveyFactLength, readIt))
	}
	// The run is told what it was not shown, and how to read it: a list
	// that stops without a word reads as all the page knows.
	if moreCount := statedCount - len(stated); moreCount > 0 {
		surveyedPage.facts = append(surveyedPage.facts, fmt.Sprintf("(and %d more facts on this page, not shown here: memory get with path %s and from 1 lists every one)", moreCount, page.Path))
	}
	// The reflections apart, not the few the lively order put among the
	// facts: they are the night's reading of the whole page.
	surveyedPage.reflections = reflectionLines(page.Path, every, true)
	held, err := heldBy(tx, agentId, page)
	if err != nil {
		return nil, err
	}
	for index, heldPage := range held {
		if index >= surveyHeldCount {
			surveyedPage.heldPages = append(surveyedPage.heldPages, fmt.Sprintf("… and %d more", len(held)-surveyHeldCount))
			break
		}
		surveyedPage.heldPages = append(surveyedPage.heldPages, heldPage.Path+" — "+heldPage.Name)
	}
	return surveyedPage, nil
}

// reflectionLines is a page's live reflections as a prompt shows them:
// the reference, the observation, its kind and what it cites. The first
// surveyReflectionCount of them, and then a line saying how many more
// there are and, for a reader with the memory tool, how to read them.
func reflectionLines(path string, facts []*models.AgentFact, canRead bool) []string {
	var reflections []*models.AgentFact
	for _, fact := range facts {
		if fact.Kind == models.FactReflection {
			reflections = append(reflections, fact)
		}
	}
	// The newest first: when there are more than are shown, what goes
	// unshown is the oldest.
	sort.SliceStable(reflections, func(left, right int) bool { return reflections[left].Number > reflections[right].Number })
	var lines []string
	moreCount := 0
	for _, fact := range reflections {
		if len(lines) >= surveyReflectionCount {
			moreCount++
			continue
		}
		line := fact.Reference(path) + " " + strings.TrimSpace(fact.Text)
		if reflectionKind := fact.ReflectionKind(); reflectionKind != "" {
			line += " [" + reflectionKind + "]"
		}
		if citations := fact.Citations(); len(citations) > 0 {
			line += " (citing " + strings.Join(citations, ", ") + ")"
		}
		lines = append(lines, line)
	}
	if moreCount > 0 {
		more := fmt.Sprintf("(and %d more reflections on %s, not shown here", moreCount, path)
		if canRead {
			more += ": memory get with path " + path + " and from 1 lists every one"
		}
		lines = append(lines, more+")")
	}
	return lines
}

// surveyPartShown is one page's answer as the combining call reads it: to
// surveyPartLength characters, and where it is cut, a line saying so and
// how much was left out, so that the report does not treat the start of
// a part as the whole of it.
func surveyPartShown(answer string) string {
	moreCount := len([]rune(answer)) - surveyPartLength
	if moreCount <= 0 {
		return answer
	}
	return cutMarked(answer, surveyPartLength) + fmt.Sprintf("\n(this part is cut here; %d more characters of it are left out)", moreCount)
}

// withOverviews is the live pages among these that have an overview.
func withOverviews(pages []*models.AgentNode) []*models.AgentNode {
	var written []*models.AgentNode
	for _, page := range pages {
		if !page.Dormant && strings.TrimSpace(page.Overview) != "" {
			written = append(written, page)
		}
	}
	return written
}

// livePages is the pages among these that are not dormant.
func livePages(pages []*models.AgentNode) []*models.AgentNode {
	var live []*models.AgentNode
	for _, page := range pages {
		if !page.Dormant {
			live = append(live, page)
		}
	}
	return live
}
