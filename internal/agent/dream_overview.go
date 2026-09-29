package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The bounds of the overview phase.
const (
	// dreamOverviews is how many overviews one night writes.
	dreamOverviews = 20

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

	// overviewSectionCount, overviewSectionLength and overviewLength
	// bound what is kept of an answer.
	overviewSectionCount  = 6
	overviewSectionLength = 2500
	overviewLength        = 10000

	// overviewEvidenceCount is how many cited pages and files are kept.
	overviewEvidenceCount = 24
)

// dreamOverviews writes the overviews whose inputs have changed, the
// pages under a page before the page itself.
//
// The listing is deepest first, and the pages of one depth are written a
// few at once, as the openings are: none of them is under another, so
// none is written from another's overview. A page is written only after
// every deeper page of the night has been, which is what lets a parent be
// written from what its children say tonight rather than last week.
func (self *Agent) dreamOverviews(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	if !budget.left() {
		return
	}
	var pages []*models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
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
	for start := 0; start < len(pages); {
		depth := strings.Count(pages[start].Path, "/")
		end := start
		for end < len(pages) && strings.Count(pages[end].Path, "/") == depth {
			end++
		}
		slots := make(chan struct{}, concurrency)
		var group sync.WaitGroup
		for _, page := range pages[start:end] {
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
		start = end
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
	links              []string
	files              []overviewFile

	// pageIdByPath is every page the prompt names, which is every page an
	// answer may cite.
	pageIdByPath map[string]string
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
		"Links":             inputs.links,
		"Files":             files,
	})
	if err != nil {
		return false
	}
	said, err := self.dreamThink(ctx, run, budget, "Wrote the overview of "+page.Path, prompt, false)
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

	facts, err := tx.ListAgentFacts(agentId, page.ID, false, 200)
	if err != nil {
		return nil, err
	}
	for _, fact := range facts {
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
	if len(live) > overviewChildCount {
		live = live[:overviewChildCount]
	}
	for _, child := range live {
		inputs.pageIdByPath[child.Path] = child.ID
		said := cutRunes(strings.TrimSpace(child.Overview), overviewChildLength)
		if said == "" {
			said = cutRunes(strings.TrimSpace(child.Summary), overviewOpeningLength)
		}
		if said == "" {
			said = "(nothing written about it yet)"
		}
		inputs.children = append(inputs.children, fmt.Sprintf("### %s — %s (%s)\n%s", child.Path, child.Name, child.Kind, said))
	}

	edges, err := tx.ListAgentEdges(agentId, page.ID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(edges, func(left, right int) bool { return edges[left].Weight > edges[right].Weight })
	if len(edges) > overviewLinkCount {
		edges = edges[:overviewLinkCount]
	}
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
			if opening := cutRunes(strings.TrimSpace(other.Summary), overviewOpeningLength); opening != "" {
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

// renderOverview is an answer's sections as markdown, each under its own
// heading, bounded; empty where no section says anything.
func renderOverview(answer overviewAnswer) string {
	var written strings.Builder
	count := 0
	for _, section := range answer.Sections {
		heading := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(section.Heading), "#"))
		text := strings.TrimSpace(section.Text)
		if text == "" || count >= overviewSectionCount {
			continue
		}
		if heading == "" {
			heading = "…"
		}
		if written.Len() > 0 {
			written.WriteString("\n\n")
		}
		written.WriteString("## " + cutRunes(strings.ReplaceAll(heading, "\n", " "), 120) + "\n\n" + cutRunes(text, overviewSectionLength))
		count++
	}
	return cutRunes(written.String(), overviewLength)
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
