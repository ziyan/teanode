package agent

import (
	"sort"
	"strings"

	"github.com/ziyan/teanode/internal/models"
)

// Themes are found by a program, not by a model: groups of pages more
// linked to one another than to the rest, over every stated link. The
// model is asked only to name a group it did not choose, so a grouping
// that is wrong shows in the links rather than hiding in prose.
//
// Everything in this file is arithmetic over identifiers, with no
// database and no clock, so that the same graph always gives the same
// themes and a test can say exactly what it expects.

// The bounds of the clustering.
const (
	// themeRounds is how many passes over the pages label propagation
	// makes before it stops, whether or not it has settled.
	themeRounds = 20

	// themeLeastMembers is how many pages a group needs to be a theme,
	// and themeLeastThemes how many themes a group of themes needs.
	themeLeastMembers = 3
	themeLeastThemes  = 2

	// themeHubLeastDegree and themeHubRatio say when a page is a hub: a
	// page linked to at least this many others, and to this many times
	// as many as the median page is. A hub is left out of propagation,
	// because a page linked to everything is a bridge every group would
	// flow across, and joins a group afterwards. At most one page in a
	// hundred (and at least one) is taken as a hub, the most linked
	// first.
	themeHubLeastDegree = 8
	themeHubRatio       = 4.0

	// themeGiantShare is the share of the linked pages one group may
	// hold before it is taken as the graph being too connected to say
	// anything, and themeGiantLeastPages how many linked pages the graph
	// needs before that is judged at all.
	themeGiantShare      = 0.4
	themeGiantLeastPages = 20

	// themeSplitMembers is how many pages a theme may hold before it is
	// divided into themes under it, themeSplitDepth how many levels deep
	// those may go below the theme they divide, and themeSplitLeastMembers
	// how many pages a group needs to be a theme of its own there; a
	// smaller one stays with the theme above.
	themeSplitMembers      = 120
	themeSplitDepth        = 3
	themeSplitLeastMembers = 5
)

// themePage is a page as the clustering reads it.
type themePage struct {
	id   string
	path string
	kind models.AgentNodeKind
}

// themeLink is a stated link as the clustering reads it.
type themeLink struct {
	fromId   string
	toId     string
	relation models.AgentEdgeRelation
}

// themeClustering is what the clustering found.
type themeClustering struct {
	// levelOne is the groups of three or more pages, each its members'
	// identifiers in order, the largest group first.
	levelOne [][]string

	// levelTwo is the groups of two or more level-one groups, each the
	// indexes of its groups in levelOne in order, the largest first.
	levelTwo [][]int

	// splits is, beside each level-one group, the groups it divides into
	// when it is too large for one overview; nil for one that is not.
	splits [][]*themeSplit

	// hubIds is the pages left out of propagation for being linked to
	// far more than the rest, which joined a group afterwards.
	hubIds []string

	// isSplitByRoot says that one group held too much of the graph, and
	// was divided again with only the links inside each root of the tree
	// (people, projects, topics and so on) counted.
	isSplitByRoot bool
}

// themeSplit is one group a large theme divides into, and the groups it
// divides into in turn.
type themeSplit struct {
	memberIds []string
	splits    []*themeSplit
}

// themeWeight is what a link counts for. A build dependency and a part
// said in so many words tie two pages together more than a mention does.
func themeWeight(relation models.AgentEdgeRelation) float64 {
	if relation == models.EdgeDependsOn || relation == models.EdgePartOf {
		return 2
	}
	return 1
}

// isThemeCandidate says whether a page takes part in the clustering.
// Not the person's own page, time, a folder, a month or a year, whose
// links reach everything and so say nothing about what belongs with
// what; and not a theme, which is what the clustering makes.
func isThemeCandidate(page themePage) bool {
	switch page.kind {
	case models.NodeSelf, models.NodeFolder, models.NodePeriod:
		return false
	}
	if page.path == models.PathSelf || page.path == models.PathTime || strings.HasPrefix(page.path, models.PathTime+"/") {
		return false
	}
	return page.path != models.PathThemes && !models.IsThemePath(page.path)
}

// clusterThemes finds the themes of a graph in two levels.
func clusterThemes(pages []themePage, links []themeLink) themeClustering {
	rootById := map[string]string{}
	for _, page := range pages {
		if isThemeCandidate(page) {
			rootById[page.id] = strings.SplitN(page.path, "/", 2)[0]
		}
	}
	weights := themeWeights(links, func(id string) bool { _, isCandidate := rootById[id]; return isCandidate })
	clustering := themeClustering{}

	labelById, hubIds := propagateAroundHubs(weights)
	clustering.hubIds = hubIds

	groups := groupsOf(labelById)
	linkedCount := len(labelById)
	if linkedCount >= themeGiantLeastPages && len(groups) > 0 && float64(len(groups[0])) > themeGiantShare*float64(linkedCount) {
		// Too connected to say anything: the largest group is divided
		// again counting only the links between pages under the same
		// root, so what is left is at least the projects that belong
		// together and the people who do, rather than one group of all.
		clustering.isSplitByRoot = true
		isInGiant := map[string]bool{}
		for _, pageId := range groups[0] {
			isInGiant[pageId] = true
		}
		withinRoots := map[string]map[string]float64{}
		for pageId, neighbours := range weights {
			if !isInGiant[pageId] {
				continue
			}
			for neighbourId, weight := range neighbours {
				if isInGiant[neighbourId] && rootById[pageId] == rootById[neighbourId] {
					addThemeWeight(withinRoots, pageId, neighbourId, weight/2)
				}
			}
		}
		divided := propagateLabels(withinRoots)
		for _, pageId := range groups[0] {
			if label, isLabelled := divided[pageId]; isLabelled {
				labelById[pageId] = "split:" + label
			} else {
				delete(labelById, pageId)
			}
		}
		groups = groupsOf(labelById)
	}
	for _, group := range groups {
		if len(group) >= themeLeastMembers {
			clustering.levelOne = append(clustering.levelOne, group)
			clustering.splits = append(clustering.splits, splitThemeGroup(group, weights, 1))
		}
	}

	// The second level: the groups clustered again the same way, over the
	// links between their members.
	groupIdByPage := map[string]string{}
	groupIndexById := map[string]int{}
	for index, group := range clustering.levelOne {
		groupIndexById[group[0]] = index
		for _, pageId := range group {
			groupIdByPage[pageId] = group[0]
		}
	}
	between := map[string]map[string]float64{}
	for pageId, neighbours := range weights {
		for neighbourId, weight := range neighbours {
			fromGroup, isFromGrouped := groupIdByPage[pageId]
			toGroup, isToGrouped := groupIdByPage[neighbourId]
			if isFromGrouped && isToGrouped && fromGroup != toGroup {
				addThemeWeight(between, fromGroup, toGroup, weight/2)
			}
		}
	}
	for _, group := range groupsOf(propagateLabels(between)) {
		if len(group) < themeLeastThemes {
			continue
		}
		indexes := make([]int, 0, len(group))
		for _, groupId := range group {
			indexes = append(indexes, groupIndexById[groupId])
		}
		sort.Ints(indexes)
		clustering.levelTwo = append(clustering.levelTwo, indexes)
	}
	return clustering
}

// propagateAroundHubs is label propagation with the hubs found, left out,
// and put back where their links go; a page whose only links were to
// hubs goes where its hub went. It answers with the labels and the hubs.
func propagateAroundHubs(weights map[string]map[string]float64) (map[string]string, []string) {
	hubIds := themeHubs(weights)
	isHub := map[string]bool{}
	for _, hubId := range hubIds {
		isHub[hubId] = true
	}
	withoutHubs := map[string]map[string]float64{}
	for pageId, neighbours := range weights {
		if isHub[pageId] {
			continue
		}
		for neighbourId, weight := range neighbours {
			if !isHub[neighbourId] {
				addThemeWeight(withoutHubs, pageId, neighbourId, weight/2)
			}
		}
	}
	labelById := propagateLabels(withoutHubs)
	assignLeftOut(labelById, weights, hubIds)
	var leftOut []string
	for _, pageId := range sortedKeys(weights) {
		if _, isLabelled := labelById[pageId]; !isLabelled {
			leftOut = append(leftOut, pageId)
		}
	}
	assignLeftOut(labelById, weights, leftOut)
	return labelById, hubIds
}

// splitThemeGroup divides a group too large for one overview into the
// groups it holds, clustered the same way over the links among its own
// members only, and those again while they are too large, at most
// themeSplitDepth levels down; depth is the level being made, from 1.
//
// Over the whole graph, modularity cannot see groups smaller than a
// size set by the whole graph's links (its resolution limit), so a
// graph of thousands of pages settles on a dozen groups of hundreds.
// Over one group's links alone that size is the group's, and the parts
// inside it show. A group smaller than themeSplitLeastMembers stays with
// the theme above, and a group that does not divide into at least two
// such parts is left whole.
func splitThemeGroup(memberIds []string, weights map[string]map[string]float64, depth int) []*themeSplit {
	if len(memberIds) <= themeSplitMembers || depth > themeSplitDepth {
		return nil
	}
	isMember := make(map[string]bool, len(memberIds))
	for _, pageId := range memberIds {
		isMember[pageId] = true
	}
	within := map[string]map[string]float64{}
	for _, pageId := range memberIds {
		for neighbourId, weight := range weights[pageId] {
			if isMember[neighbourId] {
				addThemeWeight(within, pageId, neighbourId, weight/2)
			}
		}
	}
	labelById, _ := propagateAroundHubs(within)
	var parts [][]string
	for _, group := range groupsOf(labelById) {
		if len(group) >= themeSplitLeastMembers {
			parts = append(parts, group)
		}
	}
	if len(parts) < 2 {
		return nil
	}
	splits := make([]*themeSplit, 0, len(parts))
	for _, part := range parts {
		splits = append(splits, &themeSplit{memberIds: part, splits: splitThemeGroup(part, weights, depth+1)})
	}
	return splits
}

// themeWeights is the links as an undirected weighted graph between the
// pages that take part, with several links between two pages added up.
func themeWeights(links []themeLink, isCandidate func(string) bool) map[string]map[string]float64 {
	weights := map[string]map[string]float64{}
	for _, link := range links {
		if link.fromId == link.toId || !isCandidate(link.fromId) || !isCandidate(link.toId) {
			continue
		}
		addThemeWeight(weights, link.fromId, link.toId, themeWeight(link.relation))
	}
	return weights
}

// addThemeWeight adds a weight to a link in both directions.
func addThemeWeight(weights map[string]map[string]float64, fromId, toId string, weight float64) {
	for _, pair := range [][2]string{{fromId, toId}, {toId, fromId}} {
		if weights[pair[0]] == nil {
			weights[pair[0]] = map[string]float64{}
		}
		weights[pair[0]][pair[1]] += weight
	}
}

// themeHubs is the pages linked to far more pages than the rest: at
// least themeHubLeastDegree, at least themeHubRatio times the median,
// and no more than one in a hundred of the linked pages.
func themeHubs(weights map[string]map[string]float64) []string {
	if len(weights) == 0 {
		return nil
	}
	pageIds := sortedKeys(weights)
	degrees := make([]int, 0, len(pageIds))
	for _, pageId := range pageIds {
		degrees = append(degrees, len(weights[pageId]))
	}
	sorted := append([]int(nil), degrees...)
	sort.Ints(sorted)
	median := float64(sorted[len(sorted)/2])
	most := max(1, len(pageIds)/100)
	type candidate struct {
		pageId string
		degree int
	}
	var candidates []candidate
	for index, pageId := range pageIds {
		degree := degrees[index]
		if degree >= themeHubLeastDegree && float64(degree) >= themeHubRatio*median {
			candidates = append(candidates, candidate{pageId, degree})
		}
	}
	sort.SliceStable(candidates, func(left, right int) bool { return candidates[left].degree > candidates[right].degree })
	if len(candidates) > most {
		candidates = candidates[:most]
	}
	hubIds := make([]string, 0, len(candidates))
	for _, each := range candidates {
		hubIds = append(hubIds, each.pageId)
	}
	sort.Strings(hubIds)
	return hubIds
}

// assignLeftOut puts each page propagation left out in the group its
// links weigh most towards, the smallest label on a tie, or on its own
// where none of its links reach a group.
func assignLeftOut(labelById map[string]string, weights map[string]map[string]float64, hubIds []string) {
	for _, hubId := range hubIds {
		weightByLabel := map[string]float64{}
		for neighbourId, weight := range weights[hubId] {
			if label, isLabelled := labelById[neighbourId]; isLabelled {
				weightByLabel[label] += weight
			}
		}
		best, bestWeight := hubId, 0.0
		for _, label := range sortedKeys(weightByLabel) {
			if weightByLabel[label] > bestWeight {
				best, bestWeight = label, weightByLabel[label]
			}
		}
		labelById[hubId] = best
	}
}

// propagateLabels is label propagation over a weighted undirected graph:
// every page starts with its own identifier as its label, and in the
// order of the identifiers each takes the label its links weigh most
// towards, until a pass changes nothing or themeRounds passes are made.
//
// Each label's weight is less a penalty for how much the label already
// holds: the links to it, less the page's own links times the label's
// links over twice the links of the whole graph. This is the variant
// that maximizes modularity (Barber and Clark, 2009). Without it,
// deterministic ties ran through a bridge: the first page past a single
// link between two groups saw one link to each of its neighbours'
// labels, took the smallest, which was the other group's, and every page
// after it followed, so two groups joined by one link became one.
//
// A page keeps its label on a tie, and otherwise takes the smallest of
// the best, so the same graph always gives the same groups.
//
// One pass of it settles on small groups -- pairs, often -- that no one
// page gains by leaving, even where the group they make together is
// plainly one. So the groups are then taken as pages themselves, with
// the links between them added up and each keeping the weight of the
// links inside it, and propagated again, until a pass merges nothing
// (the second phase of the Louvain method, Blondel and others, 2008).
func propagateLabels(weights map[string]map[string]float64) map[string]string {
	degreeById := make(map[string]float64, len(weights))
	for pageId, neighbours := range weights {
		for _, weight := range neighbours {
			degreeById[pageId] += weight
		}
	}
	labelById := propagateOnce(weights, degreeById)
	for level := 0; level < themeRounds; level++ {
		groups := groupsOf(labelById)
		if len(groups) == len(weights) {
			break
		}
		// Each group as one page, named by its first member.
		groupByPage := make(map[string]string, len(labelById))
		for _, group := range groups {
			for _, pageId := range group {
				groupByPage[pageId] = group[0]
			}
		}
		merged := map[string]map[string]float64{}
		mergedDegree := map[string]float64{}
		for pageId, neighbours := range weights {
			mergedDegree[groupByPage[pageId]] += degreeById[pageId]
			if merged[groupByPage[pageId]] == nil {
				merged[groupByPage[pageId]] = map[string]float64{}
			}
			for neighbourId, weight := range neighbours {
				if groupByPage[pageId] != groupByPage[neighbourId] {
					merged[groupByPage[pageId]][groupByPage[neighbourId]] += weight
				}
			}
		}
		mergedLabels := propagateOnce(merged, mergedDegree)
		isMerged := false
		for groupId, label := range mergedLabels {
			if label != groupId {
				isMerged = true
			}
		}
		if !isMerged {
			break
		}
		for pageId := range labelById {
			labelById[pageId] = mergedLabels[groupByPage[pageId]]
		}
		weights, degreeById = merged, mergedDegree
		// The pages of the next level are the groups; the labels of the
		// pages of the first are carried through groupByPage above.
		labelById = relabel(labelById)
	}
	return labelById
}

// relabel names each group by its first member, so that a group's label
// is one of its own pages whatever level it was merged at.
func relabel(labelById map[string]string) map[string]string {
	relabelled := make(map[string]string, len(labelById))
	for _, group := range groupsOf(labelById) {
		for _, pageId := range group {
			relabelled[pageId] = group[0]
		}
	}
	return relabelled
}

// propagateOnce is one propagation to a standstill over pages whose
// degree is given, which at a higher level includes the links inside a
// group that are no longer links between pages.
func propagateOnce(weights map[string]map[string]float64, degreeById map[string]float64) map[string]string {
	pageIds := sortedKeys(weights)
	labelById := make(map[string]string, len(pageIds))
	degreeByLabel := make(map[string]float64, len(pageIds))
	total := 0.0
	for _, pageId := range pageIds {
		labelById[pageId] = pageId
		degreeByLabel[pageId] = degreeById[pageId]
		total += degreeById[pageId]
	}
	if total == 0 {
		return labelById
	}
	const tolerance = 1e-9
	for round := 0; round < themeRounds; round++ {
		isChanged := false
		for _, pageId := range pageIds {
			degree := degreeById[pageId]
			current := labelById[pageId]
			degreeByLabel[current] -= degree
			weightByLabel := map[string]float64{}
			for neighbourId, weight := range weights[pageId] {
				weightByLabel[labelById[neighbourId]] += weight
			}
			score := func(label string) float64 {
				return weightByLabel[label] - degree*degreeByLabel[label]/total
			}
			best, bestScore := current, score(current)
			for _, label := range sortedKeys(weightByLabel) {
				if candidateScore := score(label); candidateScore > bestScore+tolerance {
					best, bestScore = label, candidateScore
				}
			}
			degreeByLabel[best] += degree
			if best != current {
				labelById[pageId] = best
				isChanged = true
			}
		}
		if !isChanged {
			break
		}
	}
	return labelById
}

// groupsOf is the pages by label, each group in order, the largest first
// and then by its first page.
func groupsOf(labelById map[string]string) [][]string {
	membersByLabel := map[string][]string{}
	for _, pageId := range sortedKeys(labelById) {
		label := labelById[pageId]
		membersByLabel[label] = append(membersByLabel[label], pageId)
	}
	groups := make([][]string, 0, len(membersByLabel))
	for _, members := range membersByLabel {
		groups = append(groups, members)
	}
	sort.Slice(groups, func(left, right int) bool {
		if len(groups[left]) != len(groups[right]) {
			return len(groups[left]) > len(groups[right])
		}
		return groups[left][0] < groups[right][0]
	})
	return groups
}

// sortedKeys is a map's keys in order.
func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// themeOverlap is how many of a theme's members a group holds, and
// whether that is more than half of them: what lets a theme keep its page
// while its members change.
func themeOverlap(group []string, memberIds map[string]bool) (int, bool) {
	if len(memberIds) == 0 {
		return 0, false
	}
	shared := 0
	for _, pageId := range group {
		if memberIds[pageId] {
			shared++
		}
	}
	return shared, 2*shared > len(memberIds)
}
