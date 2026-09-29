package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The bounds of the theme phase.
const (
	// dreamNewThemes is how many themes one night names, each call within
	// the night's budget. Each costs a model call; a group left unnamed
	// tonight is found again tomorrow.
	dreamNewThemes = 40

	// themeNamingMembers is how many of a group's pages the naming prompt
	// shows, the most important first, and themeNamingOpeningLength how
	// much of each opening.
	themeNamingMembers       = 25
	themeNamingOpeningLength = 300

	// themeNamingTakenMost is how many names already in use the naming
	// prompt lists, so a new theme is called something else.
	themeNamingTakenMost = 60

	// themeListed is how many pages under themes the phase reads.
	themeListed = 5000

	// themeEvidenceQuote is what a theme's link to a member says it came
	// from. A link with only this evidence is the phase's own, to replace
	// as the group changes; any other link is somebody else's and is left
	// alone.
	themeEvidenceQuote = "grouped with the other members by the night's clustering of the links"
)

// themeRootSummary is the opening of the folder the themes are kept in.
const themeRootSummary = "Groups of pages more linked to one another than to the rest, found by the night from the links, and groups of those groups."

// knownTheme is a theme page as the night finds it.
type knownTheme struct {
	page *models.AgentNode

	// memberIds is the pages its own links point at: for a theme of
	// pages, its members; for a theme of themes, the themes under it.
	memberIds map[string]bool

	isMatched bool
}

// plannedTheme is a theme as tonight's clustering wants it.
type plannedTheme struct {
	// known is the page it keeps, or nil for a theme to be made.
	known *knownTheme

	// name and opening are what the model called a new one.
	name    string
	opening string

	// memberIds is a theme of pages' members, and levelOneIndexes a
	// theme of themes' themes, by their place among the level-one plans.
	memberIds       []string
	levelOneIndexes []int

	// page is where it was written, and parent the theme it is under, if
	// any: for a theme of pages, the theme of themes that holds it; for a
	// part of a large theme, the theme it is a part of.
	page   *models.AgentNode
	parent *plannedTheme
}

// pageGroup is a group of pages tonight's clustering found: a level-one
// group, or a part a large one divides into (depth one or more, under
// parent), with the plan for its theme once it has one.
type pageGroup struct {
	memberIds []string
	depth     int
	parent    *pageGroup
	plan      *plannedTheme
}

// dreamThemes clusters the graph into themes in two levels, and keeps a
// page for each: themes/<slug> for a theme of themes and for a theme of
// pages in no such group, themes/<theme of themes>/<slug> for the rest.
// A theme of more than themeSplitMembers pages is divided into the parts
// inside it, each a theme under it (splitThemeGroup); it keeps its links
// to all its pages, and its overview is written from its parts' and from
// the pages no part holds.
//
// A theme keeps its page as its members change as long as more than half
// of the members it had are still together, so a path somebody has
// bookmarked or a survey cited stays where it was. A new group is named
// by the model; a group that has gone makes its page dormant, never
// deleted, and keeps its links, so the group coming back wakes it where
// it was, with its reflections and whatever cited it.
func (self *Agent) dreamThemes(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	var shapePages []*db.AgentGraphShapePage
	var shapeLinks []*db.AgentGraphShapeLink
	var themePages []*models.AgentNode
	var aboutEdges []*models.AgentEdge
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if shapePages, shapeLinks, err = tx.ListAgentGraphShape(run.Agent.ID); err != nil {
			return err
		}
		if themePages, err = tx.ListAgentNodesUnder(run.Agent.ID, models.PathThemes, themeListed); err != nil {
			return err
		}
		aboutEdges, err = tx.ListAgentEdgesByRelation(run.Agent.ID, models.EdgeAboutPlace)
		return err
	}); err != nil {
		log.Warningf("cannot read the graph to find its themes: %s", err)
		return
	}
	pages := make([]themePage, 0, len(shapePages))
	importanceById := make(map[string]float32, len(shapePages))
	for _, page := range shapePages {
		pages = append(pages, themePage{id: page.ID, path: page.Path, kind: page.Kind})
		importanceById[page.ID] = page.Importance
	}
	links := make([]themeLink, 0, len(shapeLinks))
	for _, link := range shapeLinks {
		links = append(links, themeLink{fromId: link.FromID, toId: link.ToID, relation: link.Relation})
	}
	clustering := clusterThemes(pages, links)
	if clustering.isSplitByRoot {
		log.Infof("agent %s: one group held most of the graph, so it was divided within each root of the tree", run.Agent.ID)
	}

	levelOneKnown, levelTwoKnown := knownThemes(themePages, aboutEdges)

	// The names already given, so a new theme is not called what another
	// is: a part named after the theme it is a part of, or two siblings
	// under one name, says nothing about what sets it apart.
	takenNames := make([]string, 0, len(themePages))
	for _, page := range themePages {
		if name := strings.TrimSpace(page.Name); name != "" {
			takenNames = append(takenNames, name)
		}
	}
	nameTheme := func(members []string, isGroupOfThemes bool, parentName string) (string, string, bool) {
		name, opening, isNamed := self.nameTheme(ctx, run, budget, members, isGroupOfThemes, parentName, takenNames)
		if isNamed {
			takenNames = append(takenNames, name)
		}
		return name, opening, isNamed
	}

	// Every group of pages: the level-one groups, largest first, and the
	// parts the large ones divide into.
	levelOneGroups := make([]*pageGroup, len(clustering.levelOne))
	var allGroups, parts []*pageGroup
	var addParts func(parent *pageGroup, splits []*themeSplit)
	addParts = func(parent *pageGroup, splits []*themeSplit) {
		for _, split := range splits {
			part := &pageGroup{memberIds: split.memberIds, depth: parent.depth + 1, parent: parent}
			allGroups = append(allGroups, part)
			parts = append(parts, part)
			addParts(part, split.splits)
		}
	}
	for index, memberIds := range clustering.levelOne {
		levelOneGroups[index] = &pageGroup{memberIds: memberIds}
		allGroups = append(allGroups, levelOneGroups[index])
		addParts(levelOneGroups[index], clustering.splits[index])
	}

	// Kept where a known theme matches, the deepest groups first: a part
	// holds more than half of the theme it used to be, where the large
	// group around it would hold more than half of that too and take its
	// page.
	matchingOrder := append([]*pageGroup(nil), allGroups...)
	sort.SliceStable(matchingOrder, func(left, right int) bool { return matchingOrder[left].depth > matchingOrder[right].depth })
	for _, group := range matchingOrder {
		if known := matchTheme(group.memberIds, levelOneKnown); known != nil {
			group.plan = &plannedTheme{known: known, memberIds: group.memberIds}
		}
	}

	// Named where none does, the top of the structure first: the
	// level-one groups, then the themes of themes over them, then the
	// parts, largest first, each only once the theme it is a part of has
	// a page to stand under.
	named := 0
	canName := func() bool { return named < dreamNewThemes && ctx.Err() == nil && budget.left() }
	for _, group := range levelOneGroups {
		if group.plan != nil {
			continue
		}
		if !canName() {
			break
		}
		name, opening, isNamed := nameTheme(self.themeMemberLines(ctx, run, group.memberIds, importanceById), false, "")
		named++
		if isNamed {
			group.plan = &plannedTheme{name: name, opening: opening, memberIds: group.memberIds}
		}
	}
	levelOnePlans := make([]*plannedTheme, len(levelOneGroups))
	for index, group := range levelOneGroups {
		levelOnePlans[index] = group.plan
	}

	// The themes of themes, over the themes of pages that have a plan.
	var levelTwoPlans []*plannedTheme
	for _, group := range clustering.levelTwo {
		var indexes []int
		var themeIds []string
		for _, index := range group {
			if plan := levelOnePlans[index]; plan != nil {
				indexes = append(indexes, index)
				if plan.known != nil {
					themeIds = append(themeIds, plan.known.page.ID)
				}
			}
		}
		if len(indexes) < themeLeastThemes {
			continue
		}
		if known := matchTheme(themeIds, levelTwoKnown); known != nil {
			levelTwoPlans = append(levelTwoPlans, &plannedTheme{known: known, levelOneIndexes: indexes})
			continue
		}
		if !canName() {
			continue
		}
		var lines []string
		for _, index := range indexes {
			lines = append(lines, levelOnePlans[index].line())
		}
		name, opening, isNamed := nameTheme(lines, true, "")
		named++
		if isNamed {
			levelTwoPlans = append(levelTwoPlans, &plannedTheme{name: name, opening: opening, levelOneIndexes: indexes})
		}
	}
	for _, plan := range levelTwoPlans {
		for _, index := range plan.levelOneIndexes {
			levelOnePlans[index].parent = plan
		}
	}

	namingOrder := append([]*pageGroup(nil), parts...)
	sort.SliceStable(namingOrder, func(left, right int) bool {
		if namingOrder[left].depth != namingOrder[right].depth {
			return namingOrder[left].depth < namingOrder[right].depth
		}
		return len(namingOrder[left].memberIds) > len(namingOrder[right].memberIds)
	})
	for _, part := range namingOrder {
		if part.plan != nil || part.parent.plan == nil {
			continue
		}
		if !canName() {
			break
		}
		name, opening, isNamed := nameTheme(self.themeMemberLines(ctx, run, part.memberIds, importanceById), false, part.parent.plan.themeName())
		named++
		if isNamed {
			part.plan = &plannedTheme{name: name, opening: opening, memberIds: part.memberIds}
		}
	}
	// Parents first, so each part has its theme's page to stand under.
	var partPlans []*plannedTheme
	for _, part := range namingOrder {
		if part.plan != nil {
			part.plan.parent = part.parent.plan
			partPlans = append(partPlans, part.plan)
		}
	}

	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		tx.AsActor(models.ActorDream)
		return writeThemes(tx, run.Agent.ID, record, levelOnePlans, levelTwoPlans, partPlans, levelOneKnown, levelTwoKnown)
	}); err != nil {
		log.Warningf("cannot keep the night's themes: %s", err)
	}
}

// line is a planned theme as a naming prompt shows it.
// lastNames is at most the last few names, the newest being the ones most
// likely to be siblings of the group being named.
func lastNames(names []string, most int) []string {
	if len(names) <= most {
		return names
	}
	return names[len(names)-most:]
}

// themeName is what the theme is called: its page's name, or the name the
// model gave a new one.
func (self *plannedTheme) themeName() string {
	if self.known != nil {
		return self.known.page.Name
	}
	return self.name
}

func (self *plannedTheme) line() string {
	name, opening := self.name, self.opening
	if self.known != nil {
		name, opening = self.known.page.Name, self.known.page.Summary
	}
	line := name
	if opening = cutRunes(strings.TrimSpace(strings.ReplaceAll(opening, "\n", " ")), themeNamingOpeningLength); opening != "" {
		line += ": " + opening
	}
	return line
}

// knownThemes is the theme pages as they stand, dormant ones included, so
// a group that comes back wakes its old theme: a theme of pages is one
// with links of its own to members (a part of a large theme is one too);
// a theme of themes is one directly under themes with a theme of pages
// under it and no members.
func knownThemes(themePages []*models.AgentNode, aboutEdges []*models.AgentEdge) ([]*knownTheme, []*knownTheme) {
	byId := map[string]*knownTheme{}
	for _, page := range themePages {
		if models.IsThemePath(page.Path) {
			byId[page.ID] = &knownTheme{page: page, memberIds: map[string]bool{}}
		}
	}
	for _, edge := range aboutEdges {
		if theme := byId[edge.FromID]; theme != nil && isThemeOwnLink(edge) {
			theme.memberIds[edge.ToID] = true
		}
	}
	var levelOne, levelTwo []*knownTheme
	isLevelOne := map[string]bool{}
	for _, page := range themePages {
		if theme := byId[page.ID]; theme != nil && len(theme.memberIds) > 0 {
			levelOne = append(levelOne, theme)
			isLevelOne[page.ID] = true
		}
	}
	for _, page := range themePages {
		theme := byId[page.ID]
		if theme == nil || isLevelOne[page.ID] || models.ParentPath(page.Path) != models.PathThemes {
			continue
		}
		for _, child := range themePages {
			if child.ParentID == page.ID && isLevelOne[child.ID] {
				theme.memberIds[child.ID] = true
			}
		}
		// A page with no theme under it is not one this phase keeps: a
		// person may have filed something of their own here.
		if len(theme.memberIds) > 0 {
			levelTwo = append(levelTwo, theme)
		}
	}
	return levelOne, levelTwo
}

// isThemeOwnLink says whether a link is one the theme phase wrote: its
// only evidence is the phase's own.
func isThemeOwnLink(edge *models.AgentEdge) bool {
	if len(edge.Evidence) == 0 {
		return false
	}
	for _, evidence := range edge.Evidence {
		if evidence.Kind != models.EvidenceDream || evidence.Quote != themeEvidenceQuote {
			return false
		}
	}
	return true
}

// matchTheme is the known theme a group keeps, if any: the one not yet
// taken of which the group holds more than half the members, the most
// shared first and the shorter path on a tie.
func matchTheme(group []string, known []*knownTheme) *knownTheme {
	var best *knownTheme
	bestShared := 0
	for _, theme := range known {
		if theme.isMatched {
			continue
		}
		shared, isMatch := themeOverlap(group, theme.memberIds)
		if !isMatch {
			continue
		}
		if best == nil || shared > bestShared || (shared == bestShared && theme.page.Path < best.page.Path) {
			best, bestShared = theme, shared
		}
	}
	if best != nil {
		best.isMatched = true
	}
	return best
}

// themeMemberLines is a group's pages as the naming prompt shows them,
// the most important first.
func (self *Agent) themeMemberLines(ctx context.Context, run *Run, group []string, importanceById map[string]float32) []string {
	ordered := append([]string(nil), group...)
	sort.SliceStable(ordered, func(left, right int) bool { return importanceById[ordered[left]] > importanceById[ordered[right]] })
	if len(ordered) > themeNamingMembers {
		ordered = ordered[:themeNamingMembers]
	}
	var members []*models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		members, err = tx.GetAgentNodes(run.Agent.ID, ordered)
		return err
	}); err != nil {
		log.Warningf("cannot read the pages of a theme: %s", err)
		return nil
	}
	byId := make(map[string]*models.AgentNode, len(members))
	for _, member := range members {
		byId[member.ID] = member
	}
	lines := make([]string, 0, len(ordered))
	for _, pageId := range ordered {
		member := byId[pageId]
		if member == nil {
			continue
		}
		line := member.Path
		if name := strings.TrimSpace(member.Name); name != "" {
			line += " — " + name
		}
		if opening := cutRunes(strings.TrimSpace(strings.ReplaceAll(member.Summary, "\n", " ")), themeNamingOpeningLength); opening != "" {
			line += ": " + opening
		}
		lines = append(lines, line)
	}
	return lines
}

// themeNameAnswer is what the model answers a naming with.
type themeNameAnswer struct {
	Name    string `json:"name"`
	Opening string `json:"opening"`
}

// nameTheme asks the model what a group is called, and says whether it
// answered with a name. parentName is the theme a part is a part of, empty
// for a theme at the top; takenNames are the names other themes have.
func (self *Agent) nameTheme(ctx context.Context, run *Run, budget *dreamBudget, members []string, isGroupOfThemes bool, parentName string, takenNames []string) (string, string, bool) {
	if len(members) == 0 {
		return "", "", false
	}
	prompt, err := render("theme_name.txt", map[string]any{
		"KnowledgeLanguage": languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"PersonName":        personName(run.Owner),
		"Members":           members,
		"IsGroupOfThemes":   isGroupOfThemes,
		"ParentName":        parentName,
		"TakenNames":        lastNames(takenNames, themeNamingTakenMost),
	})
	if err != nil {
		return "", "", false
	}
	what := "pages"
	if isGroupOfThemes {
		what = "themes"
	}
	said, err := self.dreamThink(ctx, run, budget, fmt.Sprintf("Named a theme of %d %s", len(members), what), prompt, false)
	if err != nil {
		log.Warningf("cannot name a theme: %s", err)
		return "", "", false
	}
	read := readModelAnswer[themeNameAnswer](said, "name")
	if !read.IsValid {
		log.Warningf("cannot name a theme: %s", read.Problem)
		return "", "", false
	}
	name := cutRunes(strings.TrimSpace(strings.ReplaceAll(read.Value.Name, "\n", " ")), 80)
	if models.Slug(name) == "" {
		log.Warningf("cannot name a theme: the name %q has nothing a path can be made of", read.Value.Name)
		return "", "", false
	}
	return name, cutRunes(strings.TrimSpace(read.Value.Opening), 1000), true
}

// writeThemes keeps the night's themes: the pages made, kept, moved,
// woken or put to sleep, and each theme of pages linked to its members.
// partPlans are the parts of large themes, each after the theme it is a
// part of.
func writeThemes(tx db.Transaction, agentId string, record *models.AgentDream, levelOnePlans, levelTwoPlans, partPlans []*plannedTheme, levelOneKnown, levelTwoKnown []*knownTheme) error {
	if len(levelOnePlans) > 0 || len(levelOneKnown) > 0 {
		root, err := tx.GetAgentNode(agentId, models.PathThemes)
		if err != nil {
			return err
		}
		if root == nil {
			if _, err := tx.PutAgentNode(&models.AgentNode{
				AgentID: agentId, Path: models.PathThemes, Kind: models.NodeFolder, Name: "Themes", Summary: themeRootSummary,
			}); err != nil {
				return err
			}
		}
	}
	isUpdated := map[string]bool{}

	for _, plan := range levelTwoPlans {
		if err := keepTheme(tx, agentId, record, plan, models.PathThemes, isUpdated); err != nil {
			return err
		}
	}
	for _, plan := range levelOnePlans {
		if plan == nil {
			continue
		}
		parentPath := models.PathThemes
		if plan.parent != nil && plan.parent.page != nil {
			parentPath = plan.parent.page.Path
		}
		if err := keepTheme(tx, agentId, record, plan, parentPath, isUpdated); err != nil {
			return err
		}
		if err := linkThemeMembers(tx, agentId, plan, isUpdated); err != nil {
			return err
		}
	}
	for _, plan := range partPlans {
		var parentPath string
		switch {
		case plan.parent != nil && plan.parent.page != nil:
			parentPath = plan.parent.page.Path
		case plan.known != nil:
			// The theme it is a part of was not named tonight: the part
			// stays where it is until it is.
			current, err := themePageNow(tx, agentId, plan.known.page)
			if err != nil || current == nil {
				return err
			}
			parentPath = models.ParentPath(current.Path)
		default:
			continue
		}
		if err := keepTheme(tx, agentId, record, plan, parentPath, isUpdated); err != nil {
			return err
		}
		if err := linkThemeMembers(tx, agentId, plan, isUpdated); err != nil {
			return err
		}
	}

	// What no group kept sleeps, with its links, so that the group coming
	// back finds it and wakes it where it was.
	for _, theme := range levelOneKnown {
		if theme.isMatched || theme.page.Dormant {
			continue
		}
		if err := setThemeDormant(tx, agentId, theme.page, true); err != nil {
			return err
		}
		isUpdated[theme.page.ID] = true
	}
	for _, theme := range levelTwoKnown {
		if theme.isMatched || theme.page.Dormant {
			continue
		}
		if err := setThemeDormant(tx, agentId, theme.page, true); err != nil {
			return err
		}
		isUpdated[theme.page.ID] = true
	}
	record.ThemesUpdated += len(isUpdated)
	return nil
}

// themePageNow is a theme's page as it is now: a theme above it may have
// been moved since it was read, and taken it along.
func themePageNow(tx db.Transaction, agentId string, page *models.AgentNode) (*models.AgentNode, error) {
	pages, err := tx.GetAgentNodes(agentId, []string{page.ID})
	if err != nil || len(pages) == 0 {
		return nil, err
	}
	return pages[0], nil
}

// keepTheme makes a planned theme's page, or wakes and moves the one it
// keeps, under the parent it belongs under.
func keepTheme(tx db.Transaction, agentId string, record *models.AgentDream, plan *plannedTheme, parentPath string, isUpdated map[string]bool) error {
	if plan.known == nil {
		path, err := freeThemePath(tx, agentId, parentPath, plan.name)
		if err != nil {
			return err
		}
		page, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: agentId, Path: path, Kind: models.NodeTopic, Name: plan.name, Summary: plan.opening,
		})
		if err != nil {
			return err
		}
		plan.page = page
		record.ThemesMade++
		return nil
	}
	page, err := themePageNow(tx, agentId, plan.known.page)
	if err != nil {
		return err
	}
	if page == nil {
		return fmt.Errorf("the theme %q is gone", plan.known.page.Path)
	}
	if page.Dormant {
		if err := setThemeDormant(tx, agentId, page, false); err != nil {
			return err
		}
		page.Dormant = false
		isUpdated[page.ID] = true
	}
	if models.ParentPath(page.Path) != parentPath {
		target := models.JoinPath(parentPath, models.LastSegment(page.Path))
		existing, err := tx.GetAgentNode(agentId, target)
		if err != nil {
			return err
		}
		if existing == nil {
			moved, err := tx.MoveAgentNode(agentId, page.Path, parentPath)
			if err != nil {
				return err
			}
			page = moved
			isUpdated[page.ID] = true
		} else {
			log.Infof("theme %q stays where it is: %q is already a page", page.Path, target)
		}
	}
	plan.page = page
	return nil
}

// linkThemeMembers links a theme of pages to its members and unlinks the
// pages that have left it, touching only the links the phase wrote.
func linkThemeMembers(tx db.Transaction, agentId string, plan *plannedTheme, isUpdated map[string]bool) error {
	edges, err := tx.ListAgentEdges(agentId, plan.page.ID)
	if err != nil {
		return err
	}
	isWanted := make(map[string]bool, len(plan.memberIds))
	for _, memberId := range plan.memberIds {
		isWanted[memberId] = true
	}
	isLinked := map[string]bool{}
	for _, edge := range edges {
		if edge.FromID != plan.page.ID || edge.Relation != models.EdgeAboutPlace {
			continue
		}
		isLinked[edge.ToID] = true
		if !isWanted[edge.ToID] && isThemeOwnLink(edge) {
			if err := tx.DeleteAgentEdge(agentId, plan.page.ID, edge.ToID, models.EdgeAboutPlace); err != nil {
				return err
			}
			isUpdated[plan.page.ID] = true
		}
	}
	for _, memberId := range plan.memberIds {
		if isLinked[memberId] {
			continue
		}
		if err := tx.PutAgentEdge(&models.AgentEdge{
			AgentID: agentId, FromID: plan.page.ID, ToID: memberId, Relation: models.EdgeAboutPlace,
			Weight: 1, Status: models.EdgeStated,
			Evidence: []models.Evidence{{Kind: models.EvidenceDream, Quote: themeEvidenceQuote}},
		}); err != nil {
			return err
		}
		if plan.known != nil {
			isUpdated[plan.page.ID] = true
		}
	}
	// A new theme is counted as made, not as updated as well.
	if plan.known == nil {
		delete(isUpdated, plan.page.ID)
	}
	return nil
}

// freeThemePath is a path under a parent for a new theme, from its name,
// with a number after it where the name is taken.
func freeThemePath(tx db.Transaction, agentId, parentPath, name string) (string, error) {
	base := models.JoinPath(parentPath, name)
	for attempt := 1; attempt <= 50; attempt++ {
		path := base
		if attempt > 1 {
			path = fmt.Sprintf("%s-%d", base, attempt)
		}
		existing, err := tx.GetAgentNode(agentId, path)
		if err != nil {
			return "", err
		}
		if existing == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no free path for a theme called %q under %s", name, parentPath)
}

// setThemeDormant wakes a theme's page or puts it to sleep, from the page
// as it stands now, so nothing else of it moves.
func setThemeDormant(tx db.Transaction, agentId string, page *models.AgentNode, isDormant bool) error {
	current, err := themePageNow(tx, agentId, page)
	if err != nil || current == nil || current.Dormant == isDormant {
		return err
	}
	current.Dormant = isDormant
	_, err = tx.PutAgentNode(current)
	return err
}
