package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The bounds of the overview phase.
const (
	// dreamOverviews is how many overviews of pages one night writes, and
	// dreamThemeOverviews how many of the themes' before those, each
	// within the night's budget like any call.
	dreamOverviews      = 40
	dreamThemeOverviews = 16

	// overviewLeastFactCount is how many facts a page with nothing under
	// it needs before it gets an overview. Below it the opening says all
	// there is.
	overviewLeastFactCount = 3

	// overviewChildCount and overviewLinkCount are how many of the pages
	// under a page, and linked to it, its prompt shows, most important
	// and strongest first.
	overviewChildCount = 30
	overviewLinkCount  = 30

	// overviewChildLength is how much of a child's overview its parent's
	// prompt carries, and overviewOpeningLength how much of an opening.
	overviewChildLength   = 1500
	overviewOpeningLength = 400

	// overviewMemberFactCount is how many facts a theme's prompt shows of
	// a member that has no overview yet, the most wanted first, and
	// overviewMemberFactLength how much of each.
	overviewMemberFactCount  = 5
	overviewMemberFactLength = 200

	// overviewSectionCount is how many sections an overview usually has:
	// its five headings and room for one more. A search over pages'
	// sections asks for this many a page; nothing an overview says is
	// cut to it.
	overviewSectionCount = 6

	// overviewEvidenceCount is how many cited pages and files are kept.
	overviewEvidenceCount = 24
)

// dreamOverviews writes the overviews whose inputs have changed: the
// themes first, the top of the structure and what a survey reads, and
// then the pages, most important first in each.
//
// Only pages that are ready are listed: none of the pages directly under
// one is due as well (see ListAgentNodesForOverview). A page with a child
// due waits a night, and is then written from what the child says now.
// So none of a night's pages is under another, and they are written a
// few at once, as the openings are.
func (self *Agent) dreamOverviews(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	if !budget.left() {
		return
	}
	var themes, pages []*models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if themes, err = tx.ListAgentThemesForOverview(run.Agent.ID, dreamThemeOverviews); err != nil {
			return err
		}
		pages, err = tx.ListAgentNodesForOverview(run.Agent.ID, overviewLeastFactCount, dreamOverviews)
		return err
	}); err != nil {
		log.Warningf("cannot list the pages due an overview: %s", err)
		return
	}
	concurrency := run.Configuration().Agent.Limits.RewriteConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	for _, batch := range [][]*models.AgentNode{themes, pages} {
		slots := make(chan struct{}, concurrency)
		var group sync.WaitGroup
		for _, page := range batch {
			if ctx.Err() != nil || !budget.left() || !budget.overviewingTimeLeft() {
				break
			}
			slots <- struct{}{}
			group.Add(1)
			go func() {
				defer group.Done()
				defer func() { <-slots }()
				if self.writeOverview(ctx, run, page, budget) {
					budget.mutex.Lock()
					record.OverviewsWritten++
					budget.mutex.Unlock()
				}
			}()
		}
		group.Wait()
		if ctx.Err() != nil || !budget.left() || !budget.overviewingTimeLeft() {
			return
		}
	}
}

// overviewAnswer is what the model answers an overview with.
type overviewAnswer struct {
	Sections []struct {
		Heading string `json:"heading"`
		Text    string `json:"text"`
	} `json:"sections"`
	CitedPages []string `json:"citedPages"`
	CitedFiles []string `json:"citedFiles"`
}

// overviewInputs is what one page's overview is written from.
type overviewInputs struct {
	overviewInputsHash string
	facts              []string
	children           []string
	members            []string
	links              []string
	files              []overviewFile

	// pageIdByPath is every page the prompt names, which is every page an
	// answer may cite.
	pageIdByPath map[string]string

	// coverage is how much of what the page could be written from the
	// prompt shows.
	coverage OverviewCoverage
}

// OverviewCoverage is how much of what a page's overview could be written
// from its prompt shows: of the pages under it, the members of a theme and
// the pages linked to it, how many there are and how many were shown, the
// most important and strongest first; and how many of those shown had no
// overview of their own, so were shown by their opening alone.
type OverviewCoverage struct {
	ChildCount                 int `json:"childCount"`
	ChildShownCount            int `json:"childShownCount"`
	ChildWithoutOverviewCount  int `json:"childWithoutOverviewCount"`
	MemberCount                int `json:"memberCount"`
	MemberShownCount           int `json:"memberShownCount"`
	MemberWithoutOverviewCount int `json:"memberWithoutOverviewCount"`
	LinkCount                  int `json:"linkCount"`
	LinkShownCount             int `json:"linkShownCount"`
}

// writeOverview writes one page's overview, and says whether it did.
func (self *Agent) writeOverview(ctx context.Context, run *Run, page *models.AgentNode, budget *dreamBudget) bool {
	var inputs *overviewInputs
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		inputs, err = readOverviewInputs(tx, run.Agent.ID, page)
		return err
	}); err != nil {
		log.Warningf("cannot read what the overview of %q is written from: %s", page.Path, err)
		return false
	}
	files := make([]overviewFile, 0, len(inputs.files))
	for _, file := range inputs.files {
		files = append(files, overviewFile{Path: unclosable(file.Path), Text: unclosable(file.Text)})
	}
	prompt, err := render("overview.txt", map[string]any{
		"KnowledgeLanguage": languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"PersonName":        personName(run.Owner),
		"Path":              page.Path,
		"Name":              page.Name,
		"Kind":              string(page.Kind),
		"Opening":           page.Summary,
		"Facts":             inputs.facts,
		"Children":          inputs.children,
		"Members":           inputs.members,
		"Links":             inputs.links,
		"Files":             files,
		"Coverage":          inputs.coverage,
	})
	if err != nil {
		return false
	}
	said, err := self.dreamThinkFor(ctx, run, budget, "Wrote the overview of "+page.Path, prompt, false, config.AgentWorkSynthesize)
	if err != nil {
		log.Warningf("cannot write the overview of %q: %s", page.Path, err)
		return false
	}
	// An answer that cannot be read leaves the overview as it was, and
	// the page due: the hash is written only with an overview.
	read := readModelAnswer[overviewAnswer](said, "sections")
	if !read.IsValid {
		log.Warningf("cannot write the overview of %q: %s", page.Path, read.Problem)
		return false
	}
	overview := renderOverview(read.Value)
	if overview == "" {
		log.Warningf("cannot write the overview of %q: the answer has no section with anything in it", page.Path)
		return false
	}
	evidence := overviewEvidence(read.Value, inputs)
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.SetAgentNodeOverview(run.Agent.ID, page.ID, overview, evidence, inputs.overviewInputsHash, time.Now())
	}); err != nil {
		log.Warningf("cannot keep the overview of %q: %s", page.Path, err)
		return false
	}
	return true
}

// OverviewState is how a page's overview stands: how much of what it
// could be written from its prompt shows now, and whether that has changed
// since it was written. Counted by code, from the graph as it is.
type OverviewState struct {
	Coverage OverviewCoverage

	// IsStale says what the overview is written from has changed since it
	// was written, or a rewrite was asked for; the next dream writes it
	// again.
	IsStale bool
}

// OverviewStateOf reads how a page's overview stands.
func OverviewStateOf(tx db.Transaction, agentId string, page *models.AgentNode) (*OverviewState, error) {
	inputs, err := readOverviewInputs(tx, agentId, page)
	if err != nil {
		return nil, err
	}
	isStale, err := tx.IsAgentNodeOverviewStale(agentId, page.ID)
	if err != nil {
		return nil, err
	}
	return &OverviewState{Coverage: inputs.coverage, IsStale: isStale}, nil
}

// readOverviewInputs reads what a page's overview is written from.
//
// The hash is read first. Anything that changes while the model is
// answering changes the hash after it was read, so the page is due again
// the next night rather than marked as written from what it never saw.
func readOverviewInputs(tx db.Transaction, agentId string, page *models.AgentNode) (*overviewInputs, error) {
	overviewInputsHash, err := tx.AgentNodeOverviewInputs(agentId, page.ID)
	if err != nil {
		return nil, err
	}
	inputs := &overviewInputs{overviewInputsHash: overviewInputsHash, pageIdByPath: map[string]string{page.Path: page.ID}}

	facts, err := newestFactsOf(tx, agentId, page.ID, pageFactsRewritten)
	if err != nil {
		return nil, err
	}
	for _, fact := range facts {
		// A reflection is written from the overview, not the other way
		// round; the hash leaves it out for the same reason.
		if fact.Kind == models.FactReflection {
			continue
		}
		inputs.facts = append(inputs.facts, fmt.Sprintf("#%d %s", fact.Number, fact.Line()))
	}

	children, err := tx.ListAgentNodeChildren(agentId, page.ID)
	if err != nil {
		return nil, err
	}
	live := make([]*models.AgentNode, 0, len(children))
	for _, child := range children {
		if !child.Dormant {
			live = append(live, child)
		}
	}
	sort.SliceStable(live, func(left, right int) bool { return live[left].Importance > live[right].Importance })
	inputs.coverage.ChildCount = len(live)
	if len(live) > overviewChildCount {
		live = live[:overviewChildCount]
	}
	inputs.coverage.ChildShownCount = len(live)
	for _, child := range live {
		inputs.pageIdByPath[child.Path] = child.ID
		inputs.children = append(inputs.children, overviewOfPage(child))
		if strings.TrimSpace(child.Overview) == "" {
			inputs.coverage.ChildWithoutOverviewCount++
		}
	}

	allEdges, err := tx.ListAgentEdges(agentId, page.ID)
	if err != nil {
		return nil, err
	}
	// A theme's members are what it is written from, as a page's children
	// are, and are shown the same way; the links from themes to a page
	// are not what the page is written from at all.
	edges := make([]*models.AgentEdge, 0, len(allEdges))
	var memberIds []string
	for _, edge := range allEdges {
		switch {
		case models.IsThemePath(page.Path) && edge.FromID == page.ID && edge.Relation == models.EdgeAboutPlace:
			memberIds = append(memberIds, edge.ToID)
		case edge.ToID == page.ID && models.IsThemePath(edge.FromPath):
		default:
			edges = append(edges, edge)
		}
	}
	if len(memberIds) > 0 {
		members, err := tx.GetAgentNodes(agentId, memberIds)
		if err != nil {
			return nil, err
		}
		// A member a theme under this one holds is shown by that theme's
		// overview, among the children, and not again here.
		isHeldBelow, err := membersHeldByThemesUnder(tx, agentId, children)
		if err != nil {
			return nil, err
		}
		live := make([]*models.AgentNode, 0, len(members))
		for _, member := range members {
			if !member.Dormant && !isHeldBelow[member.ID] {
				live = append(live, member)
			}
		}
		sort.SliceStable(live, func(left, right int) bool {
			if live[left].Importance != live[right].Importance {
				return live[left].Importance > live[right].Importance
			}
			return live[left].Path < live[right].Path
		})
		inputs.coverage.MemberCount = len(live)
		if len(live) > overviewChildCount {
			live = live[:overviewChildCount]
		}
		inputs.coverage.MemberShownCount = len(live)
		for _, member := range live {
			inputs.pageIdByPath[member.Path] = member.ID
			said := overviewOfPage(member)
			// A member with no overview yet is shown by its opening and
			// its most wanted facts, so the theme need not wait for it.
			if strings.TrimSpace(member.Overview) == "" {
				inputs.coverage.MemberWithoutOverviewCount++
				facts, err := tx.ListAgentFactsLively(agentId, member.ID, overviewMemberFactCount+5)
				if err != nil {
					return nil, err
				}
				shown := 0
				for _, fact := range facts {
					if fact.Kind == models.FactReflection || shown >= overviewMemberFactCount {
						continue
					}
					said += "\n- " + fact.Reference(member.Path) + " " + cutMarked(strings.ReplaceAll(fact.Line(), "\n", " "), overviewMemberFactLength)
					shown++
				}
			}
			inputs.members = append(inputs.members, said)
		}
	}
	sort.SliceStable(edges, func(left, right int) bool { return edges[left].Weight > edges[right].Weight })
	inputs.coverage.LinkCount = len(edges)
	if len(edges) > overviewLinkCount {
		edges = edges[:overviewLinkCount]
	}
	inputs.coverage.LinkShownCount = len(edges)
	otherIds := make([]string, 0, len(edges))
	for _, edge := range edges {
		if edge.FromID == page.ID {
			otherIds = append(otherIds, edge.ToID)
		} else {
			otherIds = append(otherIds, edge.FromID)
		}
	}
	others, err := tx.GetAgentNodes(agentId, otherIds)
	if err != nil {
		return nil, err
	}
	otherById := make(map[string]*models.AgentNode, len(others))
	for _, other := range others {
		otherById[other.ID] = other
	}
	for index, edge := range edges {
		line := edge.Sentence(page.Path, false)
		if other := otherById[otherIds[index]]; other != nil {
			inputs.pageIdByPath[other.Path] = other.ID
			if opening := cutMarked(strings.TrimSpace(other.Summary), overviewOpeningLength); opening != "" {
				line += ": " + strings.ReplaceAll(opening, "\n", " ")
			}
		}
		inputs.links = append(inputs.links, line)
	}

	inputs.files, err = overviewKeyFiles(tx, agentId, page, facts)
	if err != nil {
		return nil, err
	}
	return inputs, nil
}

// overviewOfPage is a page under or in the one being written, as its
// prompt shows it: its overview where it has one, its opening where not.
func overviewOfPage(page *models.AgentNode) string {
	said := cutMarked(strings.TrimSpace(page.Overview), overviewChildLength)
	if said == "" {
		said = cutMarked(strings.TrimSpace(page.Summary), overviewOpeningLength)
	}
	if said == "" {
		said = "(nothing written about it yet)"
	}
	return fmt.Sprintf("### %s — %s (%s)\n%s", page.Path, page.Name, page.Kind, said)
}

// renderOverview is an answer's sections as markdown, each under its own
// heading, bounded; empty where no section says anything.
func renderOverview(answer overviewAnswer) string {
	var written strings.Builder
	for _, section := range answer.Sections {
		heading := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(section.Heading), "#"))
		text := strings.TrimSpace(section.Text)
		if text == "" {
			continue
		}
		if heading == "" {
			heading = "…"
		}
		if written.Len() > 0 {
			written.WriteString("\n\n")
		}
		// Stored whole: an overview is what recall carries first, and a
		// section cut where it was written lost its end for good. What a
		// prompt shows of it is bounded, and marked, where it is shown.
		written.WriteString("## " + strings.ReplaceAll(heading, "\n", " ") + "\n\n" + text)
	}
	return written.String()
}

// overviewEvidence is the pages and files an answer cites, keeping only
// the ones its prompt showed: a path the model made up is not evidence.
func overviewEvidence(answer overviewAnswer, inputs *overviewInputs) []models.Evidence {
	evidence := []models.Evidence{}
	isCited := map[string]bool{}
	for _, path := range answer.CitedPages {
		path = models.NormalizePath(path)
		pageId, isShown := inputs.pageIdByPath[path]
		if !isShown || isCited["page:"+path] || len(evidence) >= overviewEvidenceCount {
			continue
		}
		isCited["page:"+path] = true
		evidence = append(evidence, models.Evidence{Kind: models.EvidenceMemory, ID: pageId, Quote: path})
	}
	for _, path := range answer.CitedFiles {
		path = strings.Trim(strings.TrimSpace(path), "/")
		for _, file := range inputs.files {
			if (file.Path != path && !strings.HasSuffix(file.Path, "/"+path)) || isCited["file:"+file.Path] || len(evidence) >= overviewEvidenceCount {
				continue
			}
			isCited["file:"+file.Path] = true
			evidence = append(evidence, models.Evidence{Kind: models.EvidenceDocument, ID: file.documentId, Quote: file.Path})
		}
	}
	return evidence
}

// membersHeldByThemesUnder is the pages the live themes among children
// are about: what a theme that has been divided holds below itself.
func membersHeldByThemesUnder(tx db.Transaction, agentId string, children []*models.AgentNode) (map[string]bool, error) {
	isHeld := map[string]bool{}
	for _, child := range children {
		if child.Dormant || !models.IsThemePath(child.Path) {
			continue
		}
		edges, err := tx.ListAgentEdges(agentId, child.ID)
		if err != nil {
			return nil, err
		}
		for _, edge := range edges {
			if edge.FromID == child.ID && edge.Relation == models.EdgeAboutPlace {
				isHeld[edge.ToID] = true
			}
		}
	}
	return isHeld, nil
}
