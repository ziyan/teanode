package agent

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The bounds of the reflection phase.
const (
	// dreamReflections is how many themes one night reflects on.
	dreamReflections = 3

	// reflectionCount is how many observations one reflection keeps, and
	// reflectionLeastCitations how many pages or facts each has to cite.
	reflectionCount          = 5
	reflectionLeastCitations = 2

	// reflectionCitationCount is how many citations one observation
	// keeps: with the night's own line, within models.EvidenceCount.
	reflectionCitationCount = 8

	// reflectionFactsSince is how far back the facts filed under a theme
	// are shown, and reflectionFactCount how many at most.
	reflectionFactsSince = 90 * 24 * time.Hour
	reflectionFactCount  = 80

	// reflectionMemberCount is how many of a theme's members its prompt
	// shows, the most important first.
	reflectionMemberCount = 30

	// reflectionAcrossThemesEvery is how often the night reflects on the
	// top-level themes together.
	reflectionAcrossThemesEvery = 7 * 24 * time.Hour
)

// reflectionKinds is what an observation may be.
var reflectionKinds = []string{"pattern", "tension", "trend", "risk", "question"}

// reflectionAnswer is what the model answers a reflection with.
type reflectionAnswer struct {
	Reflections []struct {
		Text           string   `json:"text"`
		ReflectionKind string   `json:"reflectionKind"`
		Citations      []string `json:"citations"`
	} `json:"reflections"`
}

// reflectionInputs is what one reflection is written from, and what it
// may cite.
type reflectionInputs struct {
	members []string
	facts   []string

	pageIdByPath      map[string]string
	factIdByReference map[string]string
}

// reflection is one observation that passed its checks.
type reflection struct {
	text           string
	reflectionKind string
	evidence       []models.Evidence
}

// dreamReflect writes the night's observations over its themes: a few
// themes whose overview has been written since the night last reflected
// on them, and about once a week the top-level themes together, on the
// person's own page of reflections.
//
// Each observation is a fact of kind reflection on the page it is about,
// citing at least two pages or facts its prompt showed. The ones it
// replaces are superseded, not deleted, so what the night used to think
// can still be read.
func (self *Agent) dreamReflect(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	if !budget.left() {
		return
	}
	var themes []*models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		themes, err = tx.ListAgentThemesToReflect(run.Agent.ID, dreamReflections)
		return err
	}); err != nil {
		log.Warningf("cannot list the themes to reflect on: %s", err)
		return
	}
	for _, theme := range themes {
		if ctx.Err() != nil || !budget.left() {
			return
		}
		var inputs *reflectionInputs
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
			inputs, err = readThemeReflectionInputs(tx, run.Agent.ID, theme)
			return err
		}); err != nil {
			log.Warningf("cannot read what a reflection on %q is written from: %s", theme.Path, err)
			continue
		}
		// Marked with the overview's time as it was listed, so an overview
		// written again while the model answered leaves the theme due.
		reflectedAt := time.Now()
		if theme.OverviewWrittenAt != nil {
			reflectedAt = *theme.OverviewWrittenAt
		}
		record.ReflectionsWritten += self.reflect(ctx, run, budget, theme, inputs, false, reflectedAt)
	}
	if ctx.Err() == nil && budget.left() {
		record.ReflectionsWritten += self.reflectAcrossThemes(ctx, run, budget, time.Now())
	}
}

// reflectAcrossThemes reflects on the top-level themes together, onto
// the person's own page of reflections, when the last time was a week or
// more ago; and says how many observations it wrote.
func (self *Agent) reflectAcrossThemes(ctx context.Context, run *Run, budget *dreamBudget, now time.Time) int {
	var page *models.AgentNode
	var inputs *reflectionInputs
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		existing, err := tx.GetAgentNode(run.Agent.ID, models.PathReflections)
		if err != nil {
			return err
		}
		if existing != nil {
			reflectedAt, err := tx.AgentNodeReflectedAt(run.Agent.ID, existing.ID)
			if err != nil {
				return err
			}
			if reflectedAt != nil && now.Sub(*reflectedAt) < reflectionAcrossThemesEvery {
				return nil
			}
		}
		root, err := tx.GetAgentNode(run.Agent.ID, models.PathThemes)
		if err != nil || root == nil {
			return err
		}
		children, err := tx.ListAgentNodeChildren(run.Agent.ID, root.ID)
		if err != nil {
			return err
		}
		var themes []*models.AgentNode
		for _, child := range children {
			if !child.Dormant && strings.TrimSpace(child.Overview) != "" {
				themes = append(themes, child)
			}
		}
		if len(themes) < themeLeastThemes {
			return nil
		}
		if inputs, err = readReflectionInputs(tx, run.Agent.ID, themes, time.Time{}); err != nil {
			return err
		}
		// Made without an opening: the night's consolidation writes one
		// from the reflections, and blanks one standing over no facts.
		if existing == nil {
			tx.AsActor(models.ActorDream)
			existing, err = tx.PutAgentNode(&models.AgentNode{
				AgentID: run.Agent.ID, Path: models.PathReflections, Kind: models.NodeTopic, Name: "Reflections",
			})
			if err != nil {
				return err
			}
		}
		page = existing
		return nil
	}); err != nil {
		log.Warningf("cannot read the themes to reflect on together: %s", err)
		return 0
	}
	if page == nil || inputs == nil {
		return 0
	}
	return self.reflect(ctx, run, budget, page, inputs, true, now)
}

// readThemeReflectionInputs is what a reflection on one theme reads:
// what it holds -- its members, or the themes under it -- and the facts
// filed under those lately.
func readThemeReflectionInputs(tx db.Transaction, agentId string, theme *models.AgentNode) (*reflectionInputs, error) {
	edges, err := tx.ListAgentEdges(agentId, theme.ID)
	if err != nil {
		return nil, err
	}
	var memberIds []string
	for _, edge := range edges {
		if edge.FromID == theme.ID && edge.Relation == models.EdgeAboutPlace {
			memberIds = append(memberIds, edge.ToID)
		}
	}
	members, err := tx.GetAgentNodes(agentId, memberIds)
	if err != nil {
		return nil, err
	}
	children, err := tx.ListAgentNodeChildren(agentId, theme.ID)
	if err != nil {
		return nil, err
	}
	var live []*models.AgentNode
	for _, page := range append(members, children...) {
		if !page.Dormant {
			live = append(live, page)
		}
	}
	return readReflectionInputs(tx, agentId, live, time.Now().Add(-reflectionFactsSince))
}

// readReflectionInputs shows the pages a reflection is over, the most
// important first, and the live facts on them filed since a time.
func readReflectionInputs(tx db.Transaction, agentId string, pages []*models.AgentNode, since time.Time) (*reflectionInputs, error) {
	sort.SliceStable(pages, func(left, right int) bool {
		if pages[left].Importance != pages[right].Importance {
			return pages[left].Importance > pages[right].Importance
		}
		return pages[left].Path < pages[right].Path
	})
	if len(pages) > reflectionMemberCount {
		pages = pages[:reflectionMemberCount]
	}
	inputs := &reflectionInputs{pageIdByPath: map[string]string{}, factIdByReference: map[string]string{}}
	pathById := make(map[string]string, len(pages))
	pageIds := make([]string, 0, len(pages))
	for _, page := range pages {
		inputs.pageIdByPath[page.Path] = page.ID
		inputs.members = append(inputs.members, overviewOfPage(page))
		pathById[page.ID] = page.Path
		pageIds = append(pageIds, page.ID)
	}
	facts, err := tx.ListAgentFactsOnNodesSince(agentId, pageIds, since, reflectionFactCount)
	if err != nil {
		return nil, err
	}
	for _, fact := range facts {
		reference := fact.Reference(pathById[fact.NodeID])
		inputs.factIdByReference[reference] = fact.ID
		inputs.facts = append(inputs.facts, reference+" "+fact.Line())
	}
	return inputs, nil
}

// reflect asks for one reflection, keeps what passes, supersedes what it
// replaces and marks the page reflected on; and says how many
// observations it wrote. An answer that cannot be read leaves the page
// due; one that finds nothing worth saying marks it all the same, so the
// same question is not asked again until something changes.
func (self *Agent) reflect(ctx context.Context, run *Run, budget *dreamBudget, page *models.AgentNode, inputs *reflectionInputs, isAcrossThemes bool, reflectedAt time.Time) int {
	if len(inputs.members) == 0 {
		return 0
	}
	prompt, err := render("reflect.txt", map[string]any{
		"KnowledgeLanguage": languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"PersonName":        personName(run.Owner),
		"IsAcrossThemes":    isAcrossThemes,
		"Path":              page.Path,
		"Name":              page.Name,
		"Opening":           page.Summary,
		"Overview":          page.Overview,
		"Members":           inputs.members,
		"Facts":             inputs.facts,
		"Most":              reflectionCount,
	})
	if err != nil {
		return 0
	}
	title := "Reflected on " + page.Path
	if isAcrossThemes {
		title = "Reflected on the themes together"
	}
	said, err := self.dreamThink(ctx, run, budget, title, prompt, false)
	if err != nil {
		log.Warningf("cannot reflect on %q: %s", page.Path, err)
		return 0
	}
	read := readModelAnswer[reflectionAnswer](said, "reflections")
	if !read.IsValid {
		log.Warningf("cannot reflect on %q: %s", page.Path, read.Problem)
		return 0
	}
	kept := keptReflections(read.Value, inputs)
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorDream)
		return writeReflections(tx, run.Agent.ID, page, kept, reflectedAt)
	}); err != nil {
		log.Warningf("cannot keep the reflections on %q: %s", page.Path, err)
		return 0
	}
	return len(kept)
}

// keptReflections is the observations of an answer that pass: a kind
// that is one of reflectionKinds, some text, and at least two citations
// of pages or facts the prompt showed. A citation of anything else is
// dropped before counting, so a made-up path cannot carry one over.
func keptReflections(answer reflectionAnswer, inputs *reflectionInputs) []reflection {
	var kept []reflection
	for _, wanted := range answer.Reflections {
		if len(kept) >= reflectionCount {
			break
		}
		text := cutRunes(strings.TrimSpace(wanted.Text), 900)
		reflectionKind := strings.ToLower(strings.TrimSpace(wanted.ReflectionKind))
		if text == "" || !isReflectionKind(reflectionKind) {
			continue
		}
		evidence := []models.Evidence{{Kind: models.EvidenceDream, Quote: models.ReflectionEvidencePrefix + reflectionKind}}
		isCited := map[string]bool{}
		for _, citation := range wanted.Citations {
			reference, id, isShown := resolveCitation(citation, inputs)
			if !isShown || isCited[reference] || len(evidence)-1 >= reflectionCitationCount {
				continue
			}
			isCited[reference] = true
			evidence = append(evidence, models.Evidence{Kind: models.EvidenceMemory, ID: id, Quote: reference})
		}
		if len(evidence)-1 < reflectionLeastCitations {
			continue
		}
		kept = append(kept, reflection{text: text, reflectionKind: reflectionKind, evidence: evidence})
	}
	return kept
}

// resolveCitation reads one citation as the page or fact it names, and
// says whether the prompt showed it.
func resolveCitation(citation string, inputs *reflectionInputs) (string, string, bool) {
	citation = strings.Trim(strings.TrimSpace(citation), "`[]()")
	if index := strings.LastIndexByte(citation, '#'); index > 0 {
		number, err := strconv.Atoi(strings.TrimSpace(citation[index+1:]))
		if err != nil {
			return "", "", false
		}
		reference := fmt.Sprintf("%s#%d", models.NormalizePath(citation[:index]), number)
		factId, isShown := inputs.factIdByReference[reference]
		return reference, factId, isShown
	}
	path := models.NormalizePath(citation)
	pageId, isShown := inputs.pageIdByPath[path]
	return path, pageId, isShown
}

func isReflectionKind(reflectionKind string) bool {
	for _, known := range reflectionKinds {
		if known == reflectionKind {
			return true
		}
	}
	return false
}

// writeReflections puts the kept observations on the page, supersedes
// the ones they replace, and marks the page reflected on.
func writeReflections(tx db.Transaction, agentId string, page *models.AgentNode, kept []reflection, reflectedAt time.Time) error {
	if len(kept) > 0 {
		previous, err := tx.ListAgentFacts(agentId, page.ID, false, 200)
		if err != nil {
			return err
		}
		var written []*models.AgentFact
		for _, each := range kept {
			fact, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: agentId, NodeID: page.ID, Kind: models.FactReflection, Text: each.text,
				Confidence: 0.7, Inferred: true, Evidence: each.evidence,
			})
			if err != nil {
				return err
			}
			written = append(written, fact)
		}
		for _, fact := range previous {
			if fact.Kind != models.FactReflection {
				continue
			}
			if _, err := tx.FoldAgentFact(agentId, fact.ID, written[0].ID, "a later reflection on the same page replaces it"); err != nil {
				return err
			}
		}
	}
	return tx.MarkAgentNodeReflected(agentId, page.ID, reflectedAt)
}
