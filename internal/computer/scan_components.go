package computer

import (
	"encoding/json"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The bounds of finding a checkout's components: how many, how deep a
// directory one may be in, and how many things each may be said to need.
const (
	componentEntries           = 200
	componentDepth             = 4
	componentDependencyEntries = 100
)

// EcosystemJhbuild is a component that is a module of a jhbuild moduleset.
const EcosystemJhbuild = "jhbuild"

// RepositoryComponent is one part of a checkout that builds on its own: a
// subdirectory with a build file of its own, or one of several modules a
// moduleset builds from this same repository.
type RepositoryComponent struct {
	// Path is the component's directory, relative to the checkout; empty
	// for a module a moduleset builds from the checkout as a whole.
	Path string `json:"path,omitempty"`

	// Name is what its build file calls it, or its directory's name when
	// the build file does not say.
	Name string `json:"name"`

	Ecosystem string `json:"ecosystem"`
	File      string `json:"file"`

	// Dependencies are what it needs: other components of the checkout by
	// their names, and packages as its build file names them.
	Dependencies []string `json:"dependencies,omitempty"`
}

// componentBuildFiles are the build files that make a directory a
// component, in the order one is chosen when a directory has several.
var componentBuildFiles = []string{"go.mod", "Cargo.toml", "package.json", "pyproject.toml", "setup.py", "CMakeLists.txt"}

// isNotComponentDirectory says whether a directory holds what is built to
// check or show the code rather than a part of it. A test directory with
// a CMakeLists.txt of its own is not a component anybody would name.
func isNotComponentDirectory(directory string) bool {
	for _, segment := range strings.Split(directory, "/") {
		switch strings.ToLower(segment) {
		case "test", "tests", "testing", "testdata", "example", "examples", "sample", "samples",
			"doc", "docs", "bench", "benchmark", "benchmarks", "fixtures":
			return true
		}
	}
	return false
}

// repositoryComponents is the parts of a checkout that build on their own.
//
// A monorepo of forty libraries is forty things, and a page that says only
// what the checkout as a whole needs says nothing about which of them
// needs which. A directory is a component when it has a build file of its
// own below the top of the checkout, at most componentDepth deep: a Go
// module, a crate, a package, a Python project, or a CMake directory that
// declares a project or a target. The top of the checkout is the checkout
// itself and is never a component of it.
//
// A moduleset in the checkout that builds several modules from this same
// repository -- ownNames is what the repository may be called -- makes
// each of them a component too. One module is the checkout, not a part of
// it.
func repositoryComponents(directory string, tracked []string, ownNames []string, modules []RepositoryModule) []RepositoryComponent {
	isBuildFile := map[string]bool{}
	for _, name := range componentBuildFiles {
		isBuildFile[name] = true
	}
	// Every build file below the top, by directory.
	filesByDirectory := map[string]map[string]bool{}
	var cmakeFiles []string
	for _, file := range tracked {
		file = filepath.ToSlash(file)
		base := path.Base(file)
		if !isBuildFile[base] || isOthersBuildFile(file) {
			continue
		}
		fileDirectory := path.Dir(file)
		if base == "CMakeLists.txt" {
			cmakeFiles = append(cmakeFiles, file)
		}
		if fileDirectory == "." || strings.Count(fileDirectory, "/")+1 > componentDepth || isNotComponentDirectory(fileDirectory) {
			continue
		}
		if filesByDirectory[fileDirectory] == nil {
			filesByDirectory[fileDirectory] = map[string]bool{}
		}
		filesByDirectory[fileDirectory][base] = true
	}
	// Every CMake file of the checkout, the top and the tests included,
	// since a target is declared in one and linked in another.
	sort.Strings(cmakeFiles)
	targets := cmakeTargetsOf(directory, cmakeFiles)

	// Shallowest first, then by name, so a checkout with more components
	// than the bound keeps its top-level parts rather than whatever sorts
	// first, all of one deep subtree.
	directories := make([]string, 0, len(filesByDirectory))
	for fileDirectory := range filesByDirectory {
		directories = append(directories, fileDirectory)
	}
	sort.Slice(directories, func(left, right int) bool {
		leftDepth, rightDepth := strings.Count(directories[left], "/"), strings.Count(directories[right], "/")
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return directories[left] < directories[right]
	})
	var components []RepositoryComponent
	for _, fileDirectory := range directories {
		if len(components) >= componentEntries {
			break
		}
		for _, base := range componentBuildFiles {
			if !filesByDirectory[fileDirectory][base] {
				continue
			}
			component, isComponent := readComponent(directory, fileDirectory, base, targets)
			if isComponent {
				components = append(components, component)
				break
			}
		}
	}

	// A CMake component needs the targets of another that its own link,
	// which are named in the checkout by target, and are named here by
	// the component that declares them.
	componentByDirectory := map[string]string{}
	for _, component := range components {
		componentByDirectory[component.Path] = component.Name
	}
	for index := range components {
		component := &components[index]
		if component.Ecosystem != EcosystemCMake {
			continue
		}
		var needed []string
		for _, target := range targets.declaredIn[component.Path] {
			for _, linked := range targets.linked[target] {
				if owner, found := targets.directoryOf[linked]; found && owner != component.Path {
					if name := componentByDirectory[owner]; name != "" {
						needed = append(needed, name)
					}
				}
			}
		}
		component.Dependencies = boundedNames(append(needed, component.Dependencies...))
	}

	isOwnName := map[string]bool{}
	for _, name := range ownNames {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			isOwnName[name] = true
		}
	}
	var ownModules []RepositoryModule
	for _, module := range modules {
		if isOwnName[strings.ToLower(module.Repository)] {
			ownModules = append(ownModules, module)
		}
	}
	if len(ownModules) > 1 {
		for _, module := range ownModules {
			if len(components) >= componentEntries {
				break
			}
			components = append(components, RepositoryComponent{
				Name: module.Name, Ecosystem: EcosystemJhbuild, File: module.File,
				Dependencies: boundedNames(module.Dependencies),
			})
		}
	}
	return components
}

// readComponent is the component one build file makes of its directory,
// and false when it makes none: a CMake directory with no project and no
// target of its own groups others and is not one, and a Cargo.toml that
// is only a workspace is the same.
func readComponent(directory, fileDirectory, base string, targets *cmakeTargets) (RepositoryComponent, bool) {
	file := fileDirectory + "/" + base
	component := RepositoryComponent{Path: fileDirectory, File: file}
	if base == "CMakeLists.txt" {
		name := targets.projectOf[fileDirectory]
		if name == "" && len(targets.declaredIn[fileDirectory]) == 0 {
			return component, false
		}
		if name == "" {
			name = targets.declaredIn[fileDirectory][0]
		}
		component.Name, component.Ecosystem = name, EcosystemCMake
		component.Dependencies = targets.foundIn[fileDirectory]
		return component, true
	}
	content, isRead := readBuildFile(directory, file)
	if !isRead {
		return component, false
	}
	switch base {
	case "go.mod":
		component.Name, component.Ecosystem = goModuleName(content), EcosystemGo
		component.Dependencies = goRequirements(content)
	case "Cargo.toml":
		name := tomlTableName(content, "package")
		if name == "" {
			return component, false
		}
		component.Name, component.Ecosystem = name, EcosystemCargo
		component.Dependencies = cargoDependencies(content)
	case "package.json":
		name, isWorkspaceRoot := packageName(content)
		if isWorkspaceRoot {
			return component, false
		}
		component.Name, component.Ecosystem = name, EcosystemNpm
		component.Dependencies = packageDependencies(content)
	case "pyproject.toml":
		name := tomlTableName(content, "project")
		if name == "" {
			name = tomlTableName(content, "tool.poetry")
		}
		component.Name, component.Ecosystem = name, EcosystemPython
		component.Dependencies = pyprojectDependencies(content)
	case "setup.py":
		if match := setupName.FindSubmatch(content); match != nil {
			component.Name = string(match[1])
		}
		component.Ecosystem = EcosystemPython
		component.Dependencies = setupPyRequirements(content)
	}
	if component.Name == "" {
		component.Name = path.Base(fileDirectory)
	}
	component.Dependencies = boundedNames(component.Dependencies)
	return component, true
}

// boundedNames is a list of names without blanks or repeats, and no longer
// than a component may say it needs.
func boundedNames(names []string) []string {
	var kept []string
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || len(kept) >= componentDependencyEntries {
			continue
		}
		seen[name] = true
		kept = append(kept, name)
	}
	return kept
}

// goModuleName is the module a go.mod declares.
func goModuleName(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		if module, found := strings.CutPrefix(strings.TrimSpace(line), "module "); found {
			return strings.Trim(strings.TrimSpace(module), "\"`")
		}
	}
	return ""
}

// tomlTableName is the `name` a TOML file sets in one table.
func tomlTableName(content []byte, wanted string) string {
	table := ""
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if name, isTable := tomlTable(line); isTable {
			table = name
			continue
		}
		if table == wanted && tomlKey(line) == "name" {
			_, value, _ := strings.Cut(line, "=")
			return strings.Trim(strings.TrimSpace(value), "\"'")
		}
	}
	return ""
}

// packageName is what a package.json calls its package, and whether it is
// the root of a workspace rather than a package of one.
func packageName(content []byte) (string, bool) {
	var manifest struct {
		Name       string `json:"name"`
		Workspaces any    `json:"workspaces"`
	}
	if json.Unmarshal(content, &manifest) != nil {
		return "", false
	}
	return manifest.Name, manifest.Workspaces != nil
}

// setupName is the name a setup.py passes to setup().
var setupName = regexp.MustCompile(`\bname\s*=\s*['"]([^'"\n]+)['"]`)

// cmakeTargets is what the CMake files of a checkout declare and link.
type cmakeTargets struct {
	// declaredIn is the targets each directory declares, in order, and
	// directoryOf the other way round; an alias is the target it names.
	declaredIn  map[string][]string
	directoryOf map[string]string

	// linked is what each target links, by target name.
	linked map[string][]string

	// projectOf is the project each directory's file names, and foundIn
	// the packages it finds.
	projectOf map[string]string
	foundIn   map[string][]string
}

// cmakeCommand is one CMake command and what is between its parentheses.
// A generator expression holds no parentheses, so the first closing one
// ends the command.
var cmakeCommand = regexp.MustCompile(`(?is)\b(add_library|add_executable|target_link_libraries|project|find_package)\s*\(([^)]*)\)`)

// cmakeTargetKeywords are the words add_library and add_executable take
// after the target's name that are not names, and cmakeLinkKeywords the
// ones target_link_libraries takes among what it links.
var (
	cmakeTargetKeywords = map[string]bool{
		"SHARED": true, "STATIC": true, "MODULE": true, "INTERFACE": true, "OBJECT": true,
		"IMPORTED": true, "ALIAS": true, "GLOBAL": true, "EXCLUDE_FROM_ALL": true,
		"WIN32": true, "MACOSX_BUNDLE": true,
	}
	cmakeLinkKeywords = map[string]bool{
		"PUBLIC": true, "PRIVATE": true, "INTERFACE": true, "LINK_PUBLIC": true, "LINK_PRIVATE": true,
		"LINK_INTERFACE_LIBRARIES": true, "debug": true, "optimized": true, "general": true,
	}
)

// cmakeTargetsOf reads the CMake files of a checkout for what they declare
// and link.
//
// Nothing is evaluated, with one exception: ${PROJECT_NAME} is the name
// of the nearest project() at or above the file's directory, and
// ${CMAKE_PROJECT_NAME} that of the top one, which is how most libraries
// name their own target. Any other variable is not a name here: a target
// named by one is skipped, and a linked name held in one is left out,
// rather than taking the word after it for the target.
func cmakeTargetsOf(directory string, files []string) *cmakeTargets {
	targets := &cmakeTargets{
		declaredIn: map[string][]string{}, directoryOf: map[string]string{},
		linked: map[string][]string{}, projectOf: map[string]string{}, foundIn: map[string][]string{},
	}
	commandsByFile := make(map[string][][]string, len(files))
	for _, file := range files {
		content, isRead := readBuildFile(directory, file)
		if !isRead {
			continue
		}
		var uncommented strings.Builder
		for _, line := range strings.Split(string(content), "\n") {
			if comment := strings.Index(line, "#"); comment >= 0 {
				line = line[:comment]
			}
			uncommented.WriteString(line)
			uncommented.WriteByte('\n')
		}
		for _, match := range cmakeCommand.FindAllStringSubmatch(uncommented.String(), -1) {
			command := []string{strings.ToLower(match[1])}
			for _, word := range strings.Fields(match[2]) {
				if word = strings.Trim(word, "\""); word != "" {
					command = append(command, word)
				}
			}
			commandsByFile[file] = append(commandsByFile[file], command)
		}
	}
	// The projects first, every file's, so a file read before the one
	// above it still knows its project's name.
	for _, file := range files {
		for _, command := range commandsByFile[file] {
			fileDirectory := path.Dir(file)
			if command[0] == "project" && len(command) > 1 && !strings.Contains(command[1], "$") && targets.projectOf[fileDirectory] == "" {
				targets.projectOf[fileDirectory] = command[1]
			}
		}
	}
	for _, file := range files {
		fileDirectory := path.Dir(file)
		name := func(word string) (string, bool) {
			return targets.resolveName(fileDirectory, word)
		}
		for _, command := range commandsByFile[file] {
			if len(command) < 2 {
				continue
			}
			switch command[0] {
			case "find_package":
				if found, isName := name(command[1]); isName {
					targets.foundIn[fileDirectory] = append(targets.foundIn[fileDirectory], found)
				}
			case "add_library", "add_executable":
				target, isName := name(command[1])
				if !isName {
					continue
				}
				isImported, isAlias, aliased := false, false, ""
				for index, word := range command[2:] {
					switch {
					case word == "IMPORTED":
						isImported = true
					case word == "ALIAS":
						isAlias = true
						if index+3 < len(command) {
							aliased, _ = name(command[index+3])
						}
					}
				}
				if isImported {
					continue // somebody else's, found rather than built
				}
				if isAlias {
					if owner, found := targets.directoryOf[aliased]; found {
						targets.directoryOf[target] = owner
					}
					continue
				}
				if _, isTaken := targets.directoryOf[target]; !isTaken {
					targets.directoryOf[target] = fileDirectory
					targets.declaredIn[fileDirectory] = append(targets.declaredIn[fileDirectory], target)
				}
			case "target_link_libraries":
				target, isName := name(command[1])
				if !isName {
					continue
				}
				for _, word := range command[2:] {
					if cmakeLinkKeywords[word] || strings.HasPrefix(word, "-") {
						continue
					}
					if linked, isName := name(word); isName {
						targets.linked[target] = append(targets.linked[target], linked)
					}
				}
			}
		}
	}
	return targets
}

// resolveName is a word of a CMake command as a name, with the project
// variables put in; false for a word that still holds a variable or a
// generator expression, which nothing here can know the value of.
func (self *cmakeTargets) resolveName(fileDirectory, word string) (string, bool) {
	if strings.Contains(word, "${PROJECT_NAME}") {
		project := self.nearestProject(fileDirectory)
		if project == "" {
			return "", false
		}
		word = strings.ReplaceAll(word, "${PROJECT_NAME}", project)
	}
	if strings.Contains(word, "${CMAKE_PROJECT_NAME}") {
		project := self.projectOf["."]
		if project == "" {
			project = self.nearestProject(fileDirectory)
		}
		if project == "" {
			return "", false
		}
		word = strings.ReplaceAll(word, "${CMAKE_PROJECT_NAME}", project)
	}
	if word == "" || strings.Contains(word, "$") || cmakeTargetKeywords[word] {
		return "", false
	}
	return word, true
}

// nearestProject is the project a directory's files are in: its own
// project(), or the nearest one above it.
func (self *cmakeTargets) nearestProject(fileDirectory string) string {
	for {
		if project := self.projectOf[fileDirectory]; project != "" {
			return project
		}
		if fileDirectory == "." || fileDirectory == "/" || fileDirectory == "" {
			return ""
		}
		fileDirectory = path.Dir(fileDirectory)
	}
}

// ownRepositoryNames is what a checkout's repository may be called: the
// name of its directory, and the last name of each of its remotes.
func ownRepositoryNames(directory string, remotes []string) []string {
	names := []string{filepath.Base(directory)}
	for _, remote := range remotes {
		remote = strings.TrimRight(strings.TrimSpace(remote), "/")
		if cut := strings.LastIndexAny(remote, "/:"); cut >= 0 {
			remote = remote[cut+1:]
		}
		if remote = strings.TrimSuffix(remote, ".git"); remote != "" {
			names = append(names, remote)
		}
	}
	return names
}
