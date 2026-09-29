package agent

import (
	"path"
	"strings"

	"github.com/ziyan/teanode/internal/agent/indexed"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The key files an overview of code is shown.
const (
	// overviewFileCount is how many files one overview is shown, and
	// overviewEntryPointCount how many of those may be entry points.
	overviewFileCount       = 4
	overviewEntryPointCount = 2

	// overviewFileLength is how much of each file, in characters.
	overviewFileLength = 3000

	// overviewCheckoutDepth is how far above a component its checkout's
	// page is looked for.
	overviewCheckoutDepth = 6
)

// overviewFile is one key file of the code a page is about: where it is
// in its source and the start of its text.
type overviewFile struct {
	Path       string
	Text       string
	documentId string
}

// overviewReadmeNames are the names a directory's readme goes by, in the
// order they are tried.
var overviewReadmeNames = []string{"README.md", "README", "readme.md", "README.rst", "README.txt", "Readme.md"}

// overviewBuildFileNames are the build files tried for a checkout's own
// page, in the order the device chooses one for a component.
var overviewBuildFileNames = []string{"go.mod", "Cargo.toml", "package.json", "pyproject.toml", "setup.py", "setup.cfg", "CMakeLists.txt"}

// overviewEntryPointNames are where a program or a library commonly
// starts, in the order they are tried.
var overviewEntryPointNames = []string{
	"main.go", "cmd/main.go", "src/main.rs", "src/lib.rs",
	"src/index.ts", "src/index.js", "index.ts", "index.js",
	"__main__.py", "main.py", "app.py", "src/main.cpp", "main.cpp", "src/main.c", "main.c",
}

// overviewKeyFiles is the readme, the build file and up to two entry
// points of the code a checkout's or a component's page is about, read
// from what its source indexed. Nothing for any other page, and nothing
// where the checkout cannot be placed in a source: this is what the
// overview is shown, never a reason for it not to be written.
//
// Where a page's code is comes from the lines its profile keeps: the
// checkout's own "checkout" line says where on which computer, and a
// component's "component" line which build file in the checkout makes it.
func overviewKeyFiles(tx db.Transaction, agentId string, page *models.AgentNode, facts []*models.AgentFact) ([]overviewFile, error) {
	directory, buildFile := "", ""
	checkoutText := keyedRepositoryText(facts, checkoutFactKey)
	if checkoutText == "" {
		componentText := keyedRepositoryText(facts, componentFactKey)
		if componentText == "" {
			return nil, nil
		}
		var isComponent bool
		directory, buildFile, isComponent = componentLocationOf(componentText)
		if !isComponent {
			return nil, nil
		}
		// The component's checkout is the nearest page above it that
		// says where it is.
		above := page.Path
		for depth := 0; depth < overviewCheckoutDepth && checkoutText == ""; depth++ {
			above = models.ParentPath(above)
			if above == "" {
				break
			}
			checkout, err := tx.GetAgentNode(agentId, above)
			if err != nil {
				return nil, err
			}
			if checkout == nil {
				continue
			}
			checkoutFacts, err := tx.ListAgentFacts(agentId, checkout.ID, false, 100)
			if err != nil {
				return nil, err
			}
			checkoutText = keyedRepositoryText(checkoutFacts, checkoutFactKey)
		}
	}
	where, computerName, isCheckout := checkoutLocationOf(checkoutText)
	if !isCheckout {
		return nil, nil
	}
	sources, err := tx.ListAgentSources(agentId)
	if err != nil {
		return nil, err
	}
	var source *models.AgentKnowledgeSource
	relative := ""
	for _, candidate := range sources {
		root := strings.TrimRight(candidate.Specification.Path, "/")
		if candidate.Kind != models.SourceComputer || candidate.Specification.Computer != computerName || root == "" {
			continue
		}
		if where != root && !strings.HasPrefix(where, root+"/") {
			continue
		}
		// The deepest source that holds it, where one folder is read
		// inside another.
		if source == nil || len(root) > len(strings.TrimRight(source.Specification.Path, "/")) {
			source, relative = candidate, strings.Trim(strings.TrimPrefix(where, root), "/")
		}
	}
	if source == nil {
		return nil, nil
	}

	var files []overviewFile
	isRead := map[string]bool{}
	// tryFile reads one file of the checkout, by its path in the
	// checkout, and says whether it was there.
	tryFile := func(pathInCheckout string) (bool, error) {
		externalId := strings.Trim(path.Join(relative, pathInCheckout), "/")
		if externalId == "" || isRead[externalId] || len(files) >= overviewFileCount {
			return false, nil
		}
		isRead[externalId] = true
		document, err := tx.GetAgentDocumentByExternal(source.ID, externalId)
		if err != nil || document == nil {
			return false, err
		}
		extract, err := indexed.Read(tx, agentId, document.ID, 0, overviewFileLength)
		if err != nil || extract == nil || strings.TrimSpace(extract.Text) == "" {
			return false, err
		}
		files = append(files, overviewFile{Path: externalId, Text: strings.TrimSpace(extract.Text), documentId: document.ID})
		return true, nil
	}
	for _, name := range overviewReadmeNames {
		if found, err := tryFile(path.Join(directory, name)); err != nil {
			return nil, err
		} else if found {
			break
		}
	}
	buildFiles := []string{buildFile}
	if buildFile == "" {
		buildFiles = nil
		for _, name := range overviewBuildFileNames {
			buildFiles = append(buildFiles, path.Join(directory, name))
		}
	}
	for _, candidate := range buildFiles {
		if found, err := tryFile(candidate); err != nil {
			return nil, err
		} else if found {
			break
		}
	}
	entryPoints := 0
	for _, name := range overviewEntryPointNames {
		if entryPoints >= overviewEntryPointCount {
			break
		}
		found, err := tryFile(path.Join(directory, name))
		if err != nil {
			return nil, err
		}
		if found {
			entryPoints++
		}
	}
	return files, nil
}

// keyedRepositoryText is the words of the line a profile keeps under a
// key, or nothing where the page has no such line.
func keyedRepositoryText(facts []*models.AgentFact, key string) string {
	for _, fact := range facts {
		if found, isKeyed := repositoryKeyOf(fact); isKeyed && found == key {
			return fact.Text
		}
	}
	return ""
}
