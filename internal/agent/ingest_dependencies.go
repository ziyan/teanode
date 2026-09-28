package agent

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The evidence a depends_on link carries says which checkout's build files
// stated it, so that the next pass over that checkout replaces exactly
// what it wrote and nothing else. A checkout's own requirement is
// "dependency:<name>" on a link from its page; a moduleset's statement
// about two other repositories is "moduleset:<page of the checkout that
// holds it>:<file>".
const (
	dependencyQuotePrefix = "dependency:"
	modulesetQuotePrefix  = "moduleset:"
)

// dependenciesNamedElsewhere is how many of the dependencies that are not
// checkouts here a page names; the rest are counted.
const dependenciesNamedElsewhere = 10

// repositoryPage is the name and the page of a checkout, which is where
// fileRepository files it: under the source's root, or under projects,
// by the name of the checkout's own directory.
func repositoryPage(source *models.AgentKnowledgeSource, entry computer.ScanEntry) (string, string) {
	name := entry.Title
	if name == "" {
		name = models.LastSegment(entry.ExternalID)
	}
	if source.RootPath == "" {
		return name, models.JoinPath(models.PathProjects, name)
	}
	return name, models.JoinPath(source.RootPath, name)
}

// profiledCheckout is one checkout of a pass: its page, its profile, and
// the pages of its components, which are also found by their names.
type profiledCheckout struct {
	pagePath            string
	profile             *computer.RepositoryProfile
	components          []componentPage
	componentPageByName map[string]string
}

// checkoutIndex is every checkout a pass profiled, by each name a build
// file could call it: what it calls itself, where its remote is, the
// name its remote ends in, and the name of its directory. The profiles
// arrive together on a pass's last page, so a dependency is resolved
// against all of them and not only the ones filed before it.
type checkoutIndex struct {
	source    *models.AgentKnowledgeSource
	checkouts []profiledCheckout

	// Each keyed in lower case, and each tried in this order.
	pageByModule     map[string]string
	pageByRemote     map[string]string
	pageByComponent  map[string]string
	pageByRemoteName map[string]string
	pageByDirectory  map[string]string

	// repositoryByJhbuildModule is what repository each module of every
	// moduleset in the pass builds from, so a build file that names a
	// module finds the checkout it is built from.
	repositoryByJhbuildModule map[string]string

	// componentPageByJhbuildModule is the page of each module that is a
	// component of its checkout, which a moduleset's statement about it
	// links rather than the checkout as a whole.
	componentPageByJhbuildModule map[string]string

	// pageFiledElsewhere is what the graph said when a name was looked
	// for among the pages other sources filed, empty for none: a
	// checkout names hundreds of packages, most of them the world's, and
	// each is looked for twice a pass.
	pageFiledElsewhere map[string]string
}

// newCheckoutIndex indexes the profiles among a page's entries. The first
// checkout to claim a name keeps it; profiles arrive sorted by directory,
// so which one that is does not change from pass to pass.
func newCheckoutIndex(source *models.AgentKnowledgeSource, entries []computer.ScanEntry) *checkoutIndex {
	index := &checkoutIndex{
		source:                    source,
		pageByModule:              map[string]string{},
		pageByRemote:              map[string]string{},
		pageByComponent:           map[string]string{},
		pageByRemoteName:          map[string]string{},
		pageByDirectory:           map[string]string{},
		repositoryByJhbuildModule: map[string]string{},
		pageFiledElsewhere:        map[string]string{},

		componentPageByJhbuildModule: map[string]string{},
	}
	claim := func(pages map[string]string, key, pagePath string) {
		key = strings.ToLower(strings.TrimSpace(key))
		if _, taken := pages[key]; key != "" && !taken {
			pages[key] = pagePath
		}
	}
	for _, entry := range entries {
		if entry.Repository == nil || entry.Refused != "" {
			continue
		}
		name, pagePath := repositoryPage(source, entry)
		profile := entry.Repository
		checkout := profiledCheckout{
			pagePath: pagePath, profile: profile,
			components: componentPages(pagePath, profile), componentPageByName: map[string]string{},
		}
		for _, component := range checkout.components {
			claim(checkout.componentPageByName, component.component.Name, component.pagePath)
			claim(index.pageByComponent, component.component.Name, component.pagePath)
			if component.component.Ecosystem == computer.EcosystemJhbuild {
				claim(index.componentPageByJhbuildModule, component.component.Name, component.pagePath)
			}
		}
		index.checkouts = append(index.checkouts, checkout)
		claim(index.pageByModule, profile.Module, pagePath)
		for _, remote := range profile.Remotes {
			if host, projectPath := remoteLocation(remote); projectPath != "" {
				claim(index.pageByRemote, host+"/"+projectPath, pagePath)
				claim(index.pageByRemoteName, path.Base(projectPath), pagePath)
			}
		}
		claim(index.pageByDirectory, name, pagePath)
		for _, module := range profile.Modules {
			claim(index.repositoryByJhbuildModule, module.Name, module.Repository)
		}
	}
	return index
}

// remoteLocation is a git remote as a host and a path on it, without the
// scheme, the user, the port or `.git`: "git@git.example.com:core/x.git"
// and "https://git.example.com/core/x" are both git.example.com and
// core/x. Empty for a remote that is a directory on this machine.
func remoteLocation(remote string) (string, string) {
	remote = strings.TrimSpace(remote)
	var host, projectPath string
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err != nil || parsed.Hostname() == "" {
			return "", ""
		}
		host, projectPath = parsed.Hostname(), parsed.Path
	} else {
		// The scp form, user@host:path. A path with no host before its
		// colon is a directory.
		before, after, found := strings.Cut(remote, ":")
		if !found || strings.Contains(before, "/") {
			return "", ""
		}
		if _, afterUser, hasUser := strings.Cut(before, "@"); hasUser {
			before = afterUser
		}
		host, projectPath = before, after
	}
	projectPath = strings.TrimSuffix(strings.Trim(projectPath, "/"), ".git")
	if host == "" || projectPath == "" {
		return "", ""
	}
	return strings.ToLower(host), projectPath
}

// resolve is the page of the checkout or the component a dependency
// names, or empty when it names none here. A Go module is named by where
// it lives, so it is matched against remotes whole; a package of another
// ecosystem is named by a word, which is matched against what checkouts
// and their components call themselves, their remotes' last names and
// their directories.
func (self *checkoutIndex) resolve(tx db.Transaction, dependencyName string) string {
	key := strings.ToLower(strings.TrimSpace(dependencyName))
	if key == "" {
		return ""
	}
	if pagePath := self.pageByModule[key]; pagePath != "" {
		return pagePath
	}
	if pagePath := self.componentPageByJhbuildModule[key]; pagePath != "" {
		return pagePath
	}
	if repository := self.repositoryByJhbuildModule[key]; repository != "" {
		if pagePath := self.resolveRepository(tx, repository); pagePath != "" {
			return pagePath
		}
	}
	if pagePath := self.pageByRemote[strings.TrimSuffix(key, ".git")]; pagePath != "" {
		return pagePath
	}
	if pagePath := self.pageByComponent[key]; pagePath != "" {
		return pagePath
	}
	if strings.Contains(key, "/") {
		return ""
	}
	return self.resolveRepository(tx, key)
}

// resolveRepository is the page of the checkout of a repository, by the
// last name of its path: a checkout whose remote ends in it, one in a
// directory of that name, or a project page of that name that another
// source filed.
func (self *checkoutIndex) resolveRepository(tx db.Transaction, repository string) string {
	key := strings.ToLower(strings.TrimSpace(repository))
	if key == "" {
		return ""
	}
	if pagePath := self.pageByRemoteName[key]; pagePath != "" {
		return pagePath
	}
	if pagePath := self.pageByDirectory[key]; pagePath != "" {
		return pagePath
	}
	if pagePath, asked := self.pageFiledElsewhere[key]; asked {
		return pagePath
	}
	_, pagePath := repositoryPage(self.source, computer.ScanEntry{Title: repository})
	node, err := tx.GetAgentNode(self.source.AgentID, pagePath)
	if err != nil || node == nil || node.Kind != models.NodeProject {
		pagePath = ""
	}
	self.pageFiledElsewhere[key] = pagePath
	return pagePath
}

// dependenciesElsewhere is the line saying what a checkout needs that is
// not a checkout here -- the libraries of the world, mostly -- counted,
// with the first few named. Empty when everything it needs is here.
func (self *checkoutIndex) dependenciesElsewhere(tx db.Transaction, profile *computer.RepositoryProfile) string {
	var elsewhere []string
	for _, dependency := range profile.Dependencies {
		if self.resolve(tx, dependency.Name) == "" {
			elsewhere = append(elsewhere, dependency.Name)
		}
	}
	if len(elsewhere) == 0 {
		return ""
	}
	named := elsewhere
	if len(named) > dependenciesNamedElsewhere {
		named = named[:dependenciesNamedElsewhere]
	}
	if len(elsewhere) == 1 {
		return cutRunes("Depends on 1 package that is not a checkout here, which is "+elsewhere[0]+".", 1000)
	}
	among := "among them"
	if len(named) == len(elsewhere) {
		among = "which are"
	}
	return cutRunes(fmt.Sprintf("Depends on %d packages that are not checkouts here, %s %s.",
		len(elsewhere), among, strings.Join(named, ", ")), 1000)
}

// dependencyLink is one depends_on link a pass wants: the evidence for it
// from each build file that states it, and a note saying which.
type dependencyLink struct {
	fromPath, toPath string
	evidence         []models.Evidence
	note             string
}

// wantedDependencyLinks is every depends_on link the checkouts of a pass
// state, by their two pages.
func (self *checkoutIndex) wantedDependencyLinks(tx db.Transaction) map[[2]string]*dependencyLink {
	wanted := map[[2]string]*dependencyLink{}
	want := func(fromPath, toPath, head, quote, note string) {
		// Not between a checkout and a part of itself: the tree says that
		// already, and a root build file naming its own workspace's
		// packages is how a monorepo is put together, not a dependency.
		if fromPath == "" || toPath == "" || fromPath == toPath ||
			strings.HasPrefix(toPath, fromPath+"/") || strings.HasPrefix(fromPath, toPath+"/") {
			return
		}
		key := [2]string{fromPath, toPath}
		link := wanted[key]
		if link == nil {
			link = &dependencyLink{fromPath: fromPath, toPath: toPath, note: note}
			wanted[key] = link
		}
		for _, evidence := range link.evidence {
			if evidence.Quote == quote {
				return
			}
		}
		link.evidence = append(link.evidence, models.Evidence{Kind: models.EvidenceRepository, ID: head, Quote: quote})
	}
	for _, checkout := range self.checkouts {
		profile := checkout.profile
		for _, dependency := range profile.Dependencies {
			want(checkout.pagePath, self.resolve(tx, dependency.Name), profile.Head,
				dependencyQuotePrefix+dependency.Name, dependency.File+" names "+dependency.Name)
		}
		// A component needs another of the same checkout by the name the
		// device gave it, and anything else by the names a checkout's own
		// build file would use.
		for _, component := range checkout.components {
			for _, needed := range component.component.Dependencies {
				toPath := checkout.componentPageByName[strings.ToLower(needed)]
				if toPath == "" {
					toPath = self.resolve(tx, needed)
				}
				want(component.pagePath, toPath, profile.Head,
					dependencyQuotePrefix+needed, component.component.File+" names "+needed)
			}
		}
		// A moduleset states what other repositories need, not only what
		// the checkout holding it needs: that is the whole of what a
		// moduleset is for.
		repositoryByModule := map[string]string{}
		for _, module := range profile.Modules {
			if _, taken := repositoryByModule[module.Name]; !taken {
				repositoryByModule[module.Name] = module.Repository
			}
		}
		modulePage := func(moduleName, repository string) string {
			if pagePath := self.componentPageByJhbuildModule[strings.ToLower(moduleName)]; pagePath != "" {
				return pagePath
			}
			return self.resolveRepository(tx, repository)
		}
		for _, module := range profile.Modules {
			fromPath := modulePage(module.Name, module.Repository)
			if fromPath == "" {
				continue
			}
			for _, needed := range module.Dependencies {
				repository, found := repositoryByModule[needed]
				if !found {
					continue
				}
				want(fromPath, modulePage(needed, repository), profile.Head,
					modulesetQuotePrefix+checkout.pagePath+":"+module.File,
					module.File+" builds "+module.Name+" after "+needed)
			}
		}
	}
	return wanted
}

// dependencyEvidenceOwner is the page of the checkout whose build files a
// link's evidence came from, and false for evidence no build file wrote.
func dependencyEvidenceOwner(edge *models.AgentEdge, evidence models.Evidence) (string, bool) {
	if evidence.Kind != models.EvidenceRepository {
		return "", false
	}
	if strings.HasPrefix(evidence.Quote, dependencyQuotePrefix) {
		return edge.FromPath, true
	}
	if rest, found := strings.CutPrefix(evidence.Quote, modulesetQuotePrefix); found {
		owner, _, _ := strings.Cut(rest, ":")
		return owner, owner != ""
	}
	return "", false
}

// linkCheckoutDependencies writes the depends_on links a pass's build
// files state, and takes away the ones they stated last time and do not
// any more.
//
// After every profile of the page is filed, since a link needs both of
// its pages and the one depended on may be filed after the one that
// depends on it.
//
// A link is this code's only when every piece of its evidence came from a
// build file. One the person drew, or the agent made in a conversation,
// is left exactly as it is, even where a build file says the same thing:
// it is theirs to take away, and the evidence of it is what they said.
// Among links that are this code's, a pass replaces only the evidence of
// its own checkouts, so a link two checkouts state lasts until neither
// does.
func (self *Agent) linkCheckoutDependencies(ctx context.Context, source *models.AgentKnowledgeSource, index *checkoutIndex) {
	if len(index.checkouts) == 0 {
		return
	}
	// A page whose evidence is replaced is a checkout of this pass or a
	// component under one, a component that has since gone included.
	inPass := make(map[string]bool, len(index.checkouts))
	for _, checkout := range index.checkouts {
		inPass[checkout.pagePath] = true
	}
	isInPass := func(owner string) bool {
		return inPass[owner] || inPass[models.ParentPath(owner)]
	}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		if err := lockIngestSource(tx, source); err != nil {
			return err
		}
		tx.AsActor(models.ActorIngest)
		wanted := index.wantedDependencyLinks(tx)
		existing, err := tx.ListAgentEdgesByRelation(source.AgentID, models.EdgeDependsOn)
		if err != nil {
			return err
		}
		for _, edge := range existing {
			key := [2]string{edge.FromPath, edge.ToPath}
			link := wanted[key]
			delete(wanted, key)
			isBuildFiles := len(edge.Evidence) > 0
			var kept []models.Evidence
			for _, evidence := range edge.Evidence {
				owner, isOwned := dependencyEvidenceOwner(edge, evidence)
				if !isOwned {
					isBuildFiles = false
					break
				}
				if !isInPass(owner) {
					kept = append(kept, evidence)
				}
			}
			if !isBuildFiles {
				continue
			}
			note := edge.Note
			if link != nil {
				kept = append(kept, link.evidence...)
				note = link.note
			}
			if len(kept) == 0 {
				if err := tx.DeleteAgentEdge(source.AgentID, edge.FromID, edge.ToID, models.EdgeDependsOn); err != nil {
					return err
				}
				continue
			}
			if sameQuotes(edge.Evidence, kept) {
				continue
			}
			if err := tx.PutAgentEdge(&models.AgentEdge{
				AgentID: source.AgentID, FromID: edge.FromID, ToID: edge.ToID,
				Relation: models.EdgeDependsOn, Weight: edge.Weight, Status: models.EdgeStated,
				Evidence: sortedEvidence(kept), Note: note,
			}); err != nil {
				return err
			}
		}
		keys := make([][2]string, 0, len(wanted))
		for key := range wanted {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(left, right int) bool {
			return keys[left][0]+"\x00"+keys[left][1] < keys[right][0]+"\x00"+keys[right][1]
		})
		pages := map[string]*models.AgentNode{}
		pageOf := func(pagePath string) (*models.AgentNode, error) {
			if node, found := pages[pagePath]; found {
				return node, nil
			}
			node, err := tx.GetAgentNode(source.AgentID, pagePath)
			pages[pagePath] = node
			return node, err
		}
		for _, key := range keys {
			link := wanted[key]
			from, err := pageOf(link.fromPath)
			if err != nil {
				return err
			}
			to, err := pageOf(link.toPath)
			if err != nil {
				return err
			}
			if from == nil || to == nil {
				continue
			}
			if err := tx.PutAgentEdge(&models.AgentEdge{
				AgentID: source.AgentID, FromID: from.ID, ToID: to.ID,
				Relation: models.EdgeDependsOn, Status: models.EdgeStated,
				Evidence: sortedEvidence(link.evidence), Note: link.note,
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot link the checkouts of %q by what they depend on: %s", source.Name, err)
	}
}

// sameQuotes says whether two lists of evidence cite the same things. The
// head a piece of evidence names moves with every commit, and a link is
// not rewritten for that alone.
func sameQuotes(before, after []models.Evidence) bool {
	if len(before) != len(after) {
		return false
	}
	quotes := map[string]int{}
	for _, evidence := range before {
		quotes[evidence.Quote]++
	}
	for _, evidence := range after {
		quotes[evidence.Quote]--
	}
	for _, count := range quotes {
		if count != 0 {
			return false
		}
	}
	return true
}

// sortedEvidence is evidence in the order of its quotes, so a link whose
// sources did not change reads the same from one pass to the next.
func sortedEvidence(evidence []models.Evidence) []models.Evidence {
	sorted := append([]models.Evidence(nil), evidence...)
	sort.SliceStable(sorted, func(left, right int) bool { return sorted[left].Quote < sorted[right].Quote })
	return sorted
}
