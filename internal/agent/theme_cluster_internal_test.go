package agent

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// themeWorld is a graph for the clustering: pages by path, with the
// identifier of each its path, so a test reads as the groups it expects.
type themeWorld struct {
	pages []themePage
	links []themeLink
}

func (self *themeWorld) page(path string, kind models.AgentNodeKind) {
	self.pages = append(self.pages, themePage{id: path, path: path, kind: kind})
}

func (self *themeWorld) link(fromPath, toPath string, relation models.AgentEdgeRelation) {
	self.links = append(self.links, themeLink{fromId: fromPath, toId: toPath, relation: relation})
}

// clique is pages that all link to one another.
func (self *themeWorld) clique(paths ...string) {
	for _, path := range paths {
		self.page(path, models.NodeProject)
	}
	for left := range paths {
		for right := left + 1; right < len(paths); right++ {
			self.link(paths[left], paths[right], models.EdgeRelatedTo)
		}
	}
}

// Two groups joined by one link are two themes, not one.
func TestTwoGroupsJoinedByOneLinkAreTwoThemes(t *testing.T) {
	world := &themeWorld{}
	world.clique("projects/orchard-a", "projects/orchard-b", "projects/orchard-c", "projects/orchard-d")
	world.clique("topics/tide-a", "topics/tide-b", "topics/tide-c", "topics/tide-d")
	world.link("projects/orchard-d", "topics/tide-a", models.EdgeRelatedTo)

	clustering := clusterThemes(world.pages, world.links)
	want := [][]string{
		{"projects/orchard-a", "projects/orchard-b", "projects/orchard-c", "projects/orchard-d"},
		{"topics/tide-a", "topics/tide-b", "topics/tide-c", "topics/tide-d"},
	}
	if !reflect.DeepEqual(clustering.levelOne, want) {
		t.Errorf("level one is %v, want %v", clustering.levelOne, want)
	}
}

// The person's own page, time, folders and themes are not clustered:
// their links reach everything.
func TestThePersonTimeFoldersAndThemesAreLeftOut(t *testing.T) {
	world := &themeWorld{}
	world.page("self", models.NodeSelf)
	world.page("time/2026/05", models.NodePeriod)
	world.page("projects", models.NodeFolder)
	world.page("themes/orchards", models.NodeTopic)
	world.clique("projects/orchard-a", "projects/orchard-b", "projects/orchard-c")
	for _, path := range []string{"projects/orchard-a", "projects/orchard-b", "projects/orchard-c"} {
		world.link("self", path, models.EdgeWorksOn)
		world.link(path, "time/2026/05", models.EdgeRelatedTo)
		world.link("themes/orchards", path, models.EdgeAboutPlace)
	}
	clustering := clusterThemes(world.pages, world.links)
	want := [][]string{{"projects/orchard-a", "projects/orchard-b", "projects/orchard-c"}}
	if !reflect.DeepEqual(clustering.levelOne, want) {
		t.Errorf("level one is %v, want %v", clustering.levelOne, want)
	}
}

// A page linked to everything does not merge everything. The shape that
// merges most readily under propagation: several small chains, where each
// page has two links of its own and one to the hub, so the hub's label
// would otherwise be the one every page sees most.
func TestAHubLinkedToEverythingDoesNotMergeEverything(t *testing.T) {
	world := &themeWorld{}
	world.page("organizations/example-guild", models.NodeOrganization)
	for chain := 0; chain < 6; chain++ {
		var paths []string
		for position := 0; position < 5; position++ {
			path := fmt.Sprintf("projects/chain-%d-%d", chain, position)
			world.page(path, models.NodeProject)
			world.link("organizations/example-guild", path, models.EdgeMemberOf)
			paths = append(paths, path)
		}
		for position := 1; position < len(paths); position++ {
			world.link(paths[position-1], paths[position], models.EdgeDependsOn)
		}
	}
	clustering := clusterThemes(world.pages, world.links)
	if !reflect.DeepEqual(clustering.hubIds, []string{"organizations/example-guild"}) {
		t.Errorf("the hubs are %v", clustering.hubIds)
	}
	if len(clustering.levelOne) < 6 {
		t.Fatalf("%d themes over six chains: %v", len(clustering.levelOne), clustering.levelOne)
	}
	for _, group := range clustering.levelOne {
		chains := map[string]bool{}
		for _, pageId := range group {
			if pageId != "organizations/example-guild" {
				chains[pageId[:len("projects/chain-0")]] = true
			}
		}
		if len(chains) != 1 {
			t.Errorf("a theme spans %d chains: %v", len(chains), group)
		}
	}
}

// Three themes linked to one another form a group of themes; a fourth
// linked to none of them stands alone.
func TestThreeLinkedThemesFormASecondLevel(t *testing.T) {
	world := &themeWorld{}
	world.clique("projects/kiln-a", "projects/kiln-b", "projects/kiln-c", "projects/kiln-d")
	world.clique("projects/glaze-a", "projects/glaze-b", "projects/glaze-c", "projects/glaze-d")
	world.clique("projects/clay-a", "projects/clay-b", "projects/clay-c", "projects/clay-d")
	world.clique("topics/sail-a", "topics/sail-b", "topics/sail-c", "topics/sail-d")
	world.link("projects/kiln-a", "projects/glaze-a", models.EdgeRelatedTo)
	world.link("projects/glaze-b", "projects/clay-b", models.EdgeRelatedTo)
	world.link("projects/clay-c", "projects/kiln-c", models.EdgeRelatedTo)

	clustering := clusterThemes(world.pages, world.links)
	if len(clustering.levelOne) != 4 {
		t.Fatalf("level one is %v", clustering.levelOne)
	}
	if len(clustering.levelTwo) != 1 || len(clustering.levelTwo[0]) != 3 {
		t.Fatalf("level two is %v over %v", clustering.levelTwo, clustering.levelOne)
	}
	for _, index := range clustering.levelTwo[0] {
		if clustering.levelOne[index][0] == "topics/sail-a" {
			t.Errorf("the unlinked theme joined the group: %v", clustering.levelTwo)
		}
	}
}

// A graph where the links run everywhere gives one group of nearly
// everything; it is divided again within each root of the tree, and says
// so.
func TestAGiantGroupIsDividedByTheRootsOfTheTree(t *testing.T) {
	world := &themeWorld{}
	var paths []string
	for index := 0; index < 12; index++ {
		paths = append(paths, fmt.Sprintf("projects/mesh-%02d", index), fmt.Sprintf("people/mesh-%02d", index))
	}
	world.clique(paths...)
	clustering := clusterThemes(world.pages, world.links)
	if !clustering.isSplitByRoot {
		t.Fatalf("a graph of one group was not divided: %v", clustering.levelOne)
	}
	if len(clustering.levelOne) != 2 {
		t.Fatalf("level one is %v", clustering.levelOne)
	}
	for _, group := range clustering.levelOne {
		root := group[0][:len("people")]
		for _, pageId := range group {
			if pageId[:len(root)] != root {
				t.Errorf("a divided theme spans roots: %v", group)
			}
		}
	}
}

func TestThemeOverlapIsMoreThanHalfOfTheMembers(t *testing.T) {
	members := map[string]bool{"a": true, "b": true, "c": true, "d": true}
	if _, isMatch := themeOverlap([]string{"a", "b", "x"}, members); isMatch {
		t.Errorf("two of four matched")
	}
	if shared, isMatch := themeOverlap([]string{"a", "b", "c", "x", "y"}, members); !isMatch || shared != 3 {
		t.Errorf("three of four did not match: %d", shared)
	}
}

// A group of three hundred pages made of three dense parts, linked to one
// another here and there, divides into three themes of a hundred, none
// of which divides again.
func TestALargeGroupDividesIntoTheGroupsInsideIt(t *testing.T) {
	world := &themeWorld{}
	var memberIds []string
	seed := uint32(7)
	random := func(bound int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % bound
	}
	for _, part := range []string{"north", "south", "east"} {
		var paths []string
		for index := range 100 {
			path := fmt.Sprintf("topics/%s-%03d", part, index)
			world.page(path, models.NodeTopic)
			paths = append(paths, path)
		}
		for left := range paths {
			for right := left + 1; right < len(paths); right++ {
				if random(100) < 20 {
					world.link(paths[left], paths[right], models.EdgeRelatedTo)
				}
			}
		}
		memberIds = append(memberIds, paths...)
	}
	for range 40 {
		world.link(memberIds[random(300)], memberIds[random(300)], models.EdgeRelatedTo)
	}
	weights := themeWeights(world.links, func(string) bool { return true })

	splits := splitThemeGroup(memberIds, weights, 1)
	if len(splits) != 3 {
		t.Fatalf("%d parts, want 3", len(splits))
	}
	for _, split := range splits {
		if len(split.memberIds) != 100 || split.splits != nil {
			t.Errorf("a part of %d pages, divided into %d", len(split.memberIds), len(split.splits))
		}
		part := split.memberIds[0][len("topics/"):][:5]
		for _, pageId := range split.memberIds {
			if pageId[len("topics/"):][:5] != part {
				t.Errorf("%s is in the %s part", pageId, part)
			}
		}
	}
	// A group no larger than the bound is left whole, and so is one
	// three levels down.
	if splits := splitThemeGroup(memberIds[:themeSplitMembers], weights, 1); splits != nil {
		t.Errorf("a group of %d pages was divided", themeSplitMembers)
	}
	if splits := splitThemeGroup(memberIds, weights, themeSplitDepth+1); splits != nil {
		t.Errorf("a group was divided past the deepest level")
	}
}
