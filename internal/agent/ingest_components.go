package agent

import (
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// componentFactKey is the key of the line on a component's page that says
// where in its checkout it is. It is also what marks a page under a
// checkout as one this code made: a page the person filed there has no
// such line, and is never made dormant here.
const componentFactKey = "component"

// componentPage is one component of a checkout and the page it is kept on.
type componentPage struct {
	pagePath  string
	component computer.RepositoryComponent
}

// componentPages is where each component of a checkout is kept: a page
// under the checkout's own, named after the component's directory, or
// after its name when it is a module built from the whole checkout. The
// tree says the component is part of the checkout, so no part_of link is
// written. Two components that would share a page keep the first.
func componentPages(checkoutPath string, profile *computer.RepositoryProfile) []componentPage {
	var pages []componentPage
	isTaken := map[string]bool{}
	for _, component := range profile.Components {
		segment := component.Path
		if segment == "" {
			segment = component.Name
		}
		pagePath := models.JoinPath(checkoutPath, segment)
		if pagePath == checkoutPath || isTaken[pagePath] {
			continue
		}
		isTaken[pagePath] = true
		pages = append(pages, componentPage{pagePath: pagePath, component: component})
	}
	return pages
}

// ecosystemNouns is what a component of each ecosystem is called.
var ecosystemNouns = map[string]string{
	computer.EcosystemGo:      "a Go module",
	computer.EcosystemNpm:     "an npm package",
	computer.EcosystemPython:  "a Python package",
	computer.EcosystemCargo:   "a Rust crate",
	computer.EcosystemCMake:   "a CMake project",
	computer.EcosystemJhbuild: "a jhbuild module",
}

// componentOpening is the first thing a new component's page says, until
// the night writes a better one from what is learned about it.
func componentOpening(checkoutName string, component computer.RepositoryComponent) string {
	noun := ecosystemNouns[component.Ecosystem]
	if noun == "" {
		noun = "a component"
	}
	if component.Path == "" {
		return fmt.Sprintf("%s, %s built from %s.", component.Name, noun, checkoutName)
	}
	return fmt.Sprintf("%s, %s in %s of %s.", component.Name, noun, component.Path, checkoutName)
}

// componentLine is the keyed line on a component's page: where it is and
// what builds it.
func componentLine(checkoutName string, component computer.RepositoryComponent) string {
	if component.Path == "" {
		return fmt.Sprintf("A module of %s, built by %s.", checkoutName, component.File)
	}
	return fmt.Sprintf("A part of %s, in %s, built by %s.", checkoutName, component.Path, component.File)
}

// componentLocationOf reads componentLine back: the component's
// directory in its checkout, empty for a module built from the whole
// checkout, and the build file that makes it; false for any other words.
func componentLocationOf(text string) (string, string, bool) {
	text = strings.TrimSuffix(text, ".")
	builtBy := strings.LastIndex(text, ", built by ")
	if builtBy <= 0 {
		return "", "", false
	}
	buildFile, before := text[builtBy+len(", built by "):], text[:builtBy]
	switch {
	case strings.HasPrefix(before, "A module of "):
		return "", buildFile, buildFile != ""
	case strings.HasPrefix(before, "A part of "):
		at := strings.LastIndex(before, ", in ")
		if at <= 0 {
			return "", "", false
		}
		return before[at+len(", in "):], buildFile, buildFile != ""
	}
	return "", "", false
}

// fileCheckoutComponents keeps a page for each component of a checkout,
// under the checkout's page, and makes dormant the pages of components it
// no longer has.
//
// A page already there is left as it is but for coming back from dormant:
// its opening may be the night's, or the person's, and either is better
// than the one line a build file gives. Dormant rather than deleted when a
// component goes, the way the night treats what it takes off the index:
// the page may carry what somebody learned about it, and a component
// moved back keeps that.
func fileCheckoutComponents(tx db.Transaction, agentId, checkoutName string, checkout *models.AgentNode, profile *computer.RepositoryProfile) error {
	wanted := componentPages(checkout.Path, profile)
	isWanted := make(map[string]bool, len(wanted))
	for _, page := range wanted {
		isWanted[page.pagePath] = true
		node, err := tx.GetAgentNode(agentId, page.pagePath)
		if err != nil {
			return err
		}
		switch {
		case node == nil:
			node, err = tx.PutAgentNode(&models.AgentNode{
				AgentID: agentId, Path: page.pagePath, Kind: models.NodeProject,
				Name: page.component.Name, Summary: componentOpening(checkoutName, page.component),
			})
		case node.Dormant || node.Kind == models.NodeFolder:
			revived := *node
			revived.Dormant, revived.Kind = false, models.NodeProject
			if strings.TrimSpace(revived.Summary) == "" {
				revived.Summary = componentOpening(checkoutName, page.component)
			}
			node, err = tx.PutAgentNode(&revived)
		}
		if err != nil {
			return err
		}
		if err := putKeyedRepositoryFact(tx, agentId, node.ID, componentFactKey,
			componentLine(checkoutName, page.component), profile.Head); err != nil {
			return err
		}
	}
	children, err := tx.ListAgentNodeChildren(agentId, checkout.ID)
	if err != nil {
		return err
	}
	for _, child := range children {
		if isWanted[child.Path] {
			continue
		}
		removed, err := deleteKeyedRepositoryFacts(tx, agentId, child.ID, componentFactKey)
		if err != nil {
			return err
		}
		if removed == 0 || child.Dormant {
			continue // not a page this code made, or already put away
		}
		dormant := *child
		dormant.Dormant = true
		if _, err := tx.PutAgentNode(&dormant); err != nil {
			return err
		}
	}
	return nil
}

// repositoryKeyOf is the key a fact computed from a profile carries as the
// quote of its repository evidence, and false for a fact that has none.
func repositoryKeyOf(fact *models.AgentFact) (string, bool) {
	for _, evidence := range fact.Evidence {
		if evidence.Kind == models.EvidenceRepository && evidence.Quote != "" {
			return evidence.Quote, true
		}
	}
	return "", false
}

// putKeyedRepositoryFact writes one computed line on a page under its key,
// changing the words of the fact that already carries the key rather than
// adding another, as fileRepository does for a checkout's own lines.
func putKeyedRepositoryFact(tx db.Transaction, agentId, nodeId, key, text, head string) error {
	facts, err := tx.ListAgentFacts(agentId, nodeId, true, 100)
	if err != nil {
		return err
	}
	evidence := []models.Evidence{{Kind: models.EvidenceRepository, ID: head, Quote: key}}
	var kept *models.AgentFact
	for _, fact := range facts {
		if factKey, found := repositoryKeyOf(fact); !found || factKey != key {
			continue
		}
		if kept != nil {
			// A second line under one key is one too many.
			if err := tx.DeleteAgentFact(agentId, fact.ID); err != nil {
				return err
			}
			continue
		}
		kept = fact
	}
	if kept == nil {
		_, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: agentId, NodeID: nodeId, Kind: models.FactPlain, Text: text, Evidence: evidence,
		})
		return err
	}
	if kept.Text == text {
		return nil
	}
	_, err = tx.UpdateAgentFact(agentId, kept.ID, func(fact *models.AgentFact) error {
		fact.Text = text
		fact.Evidence = evidence
		return nil
	})
	return err
}

// deleteKeyedRepositoryFacts takes the computed lines under one key off a
// page, and says how many there were.
func deleteKeyedRepositoryFacts(tx db.Transaction, agentId, nodeId, key string) (int, error) {
	facts, err := tx.ListAgentFacts(agentId, nodeId, true, 100)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, fact := range facts {
		if factKey, found := repositoryKeyOf(fact); found && factKey == key {
			if err := tx.DeleteAgentFact(agentId, fact.ID); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}
