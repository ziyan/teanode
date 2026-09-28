package computer

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The bounds of reading a checkout's build files. A build file larger
// than a megabyte is generated or is data, and a checkout that names more
// than five hundred things it needs is one whose list says nothing a
// person could read anyway.
const (
	dependencyFileBytes = 1 << 20
	dependencyEntries   = 500
)

// The ecosystems a dependency can come from, which is which kind of build
// file named it.
const (
	EcosystemGo     = "go"
	EcosystemNpm    = "npm"
	EcosystemPython = "python"
	EcosystemCargo  = "cargo"
	EcosystemCMake  = "cmake"
)

// repositoryDependencies is what a checkout's build files say it needs,
// and what a jhbuild moduleset in it says about other repositories.
//
// Only files git tracks are read, so a build tree or a virtual
// environment left in the checkout is not taken for the checkout's own
// word; and never under a vendored or third-party directory, which holds
// somebody else's build files and so somebody else's dependencies. The
// manifests are read at the top of the checkout, where the one that
// describes the checkout is; a moduleset is read wherever it is, since
// a checkout that holds them usually keeps them in a directory of their
// own.
//
// Nothing here fails the profile. A file that cannot be read, is too
// large or does not parse is skipped, and the rest still counts.
func repositoryDependencies(directory string, tracked []string) ([]RepositoryDependency, []RepositoryModule) {
	isTracked := make(map[string]bool, len(tracked))
	var modulesets []string
	for _, file := range tracked {
		file = filepath.ToSlash(file)
		if isOthersBuildFile(file) {
			continue
		}
		isTracked[file] = true
		if strings.HasSuffix(file, ".modules") {
			modulesets = append(modulesets, file)
		}
	}
	sort.Strings(modulesets)

	var dependencies []RepositoryDependency
	seen := map[string]bool{}
	add := func(ecosystem, file string, names []string) {
		for _, dependencyName := range names {
			dependencyName = strings.TrimSpace(dependencyName)
			if dependencyName == "" || len(dependencies) >= dependencyEntries {
				continue
			}
			key := ecosystem + "\x00" + dependencyName
			if seen[key] {
				continue
			}
			seen[key] = true
			dependencies = append(dependencies, RepositoryDependency{Name: dependencyName, Ecosystem: ecosystem, File: file})
		}
	}
	for _, reader := range []struct {
		file      string
		ecosystem string
		read      func([]byte) []string
	}{
		{"go.mod", EcosystemGo, goRequirements},
		{"package.json", EcosystemNpm, packageDependencies},
		{"pyproject.toml", EcosystemPython, pyprojectDependencies},
		{"setup.py", EcosystemPython, setupPyRequirements},
		{"setup.cfg", EcosystemPython, setupCfgRequirements},
		{"Cargo.toml", EcosystemCargo, cargoDependencies},
		{"CMakeLists.txt", EcosystemCMake, cmakePackages},
	} {
		if !isTracked[reader.file] {
			continue
		}
		if content, isRead := readBuildFile(directory, reader.file); isRead {
			add(reader.ecosystem, reader.file, reader.read(content))
		}
	}

	// A moduleset may include another, and the one it includes is read
	// even when its name does not end in `.modules`, as long as git
	// tracks it in this checkout. Each file once.
	var modules []RepositoryModule
	isQueued := map[string]bool{}
	for _, file := range modulesets {
		isQueued[file] = true
	}
	for len(modulesets) > 0 && len(dependencies)+len(modules) < dependencyEntries {
		file := modulesets[0]
		modulesets = modulesets[1:]
		content, isRead := readBuildFile(directory, file)
		if !isRead {
			continue
		}
		found, includes := modulesetModules(content, file)
		for _, module := range found {
			if len(dependencies)+len(modules) >= dependencyEntries {
				break
			}
			modules = append(modules, module)
		}
		for _, include := range includes {
			if isTracked[include] && !isQueued[include] {
				isQueued[include] = true
				modulesets = append(modulesets, include)
			}
		}
	}
	return dependencies, modules
}

// isOthersBuildFile says whether a tracked path is under a directory that
// holds code somebody else wrote: vendored modules, installed packages,
// third-party trees.
func isOthersBuildFile(file string) bool {
	if inIgnoredDirectory(file) {
		return true
	}
	for _, segment := range strings.Split(file, "/") {
		switch strings.ToLower(segment) {
		case "third_party", "third-party", "thirdparty", "3rdparty":
			return true
		}
	}
	return false
}

// readBuildFile is one build file of the checkout, when it is an ordinary
// file no larger than the bound. A link is not followed: a tracked link
// can point anywhere on the machine, and what it points at is not the
// checkout's own word.
func readBuildFile(directory, file string) ([]byte, bool) {
	full := filepath.Join(directory, filepath.FromSlash(file))
	information, err := os.Lstat(full)
	if err != nil || !information.Mode().IsRegular() || information.Size() > dependencyFileBytes {
		return nil, false
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return nil, false
	}
	return content, true
}

// goRequirements is the modules a go.mod requires directly. An indirect
// requirement is one of theirs, not the checkout's.
func goRequirements(content []byte) []string {
	var names []string
	inBlock := false
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		isIndirect := strings.Contains(line, "// indirect")
		if comment := strings.Index(line, "//"); comment >= 0 {
			line = strings.TrimSpace(line[:comment])
		}
		switch {
		case inBlock && line == ")":
			inBlock = false
			continue
		case inBlock:
		case line == "require (" || line == "require(":
			inBlock = true
			continue
		default:
			after, isRequire := strings.CutPrefix(line, "require ")
			if !isRequire {
				continue
			}
			line = strings.TrimSpace(after)
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || isIndirect {
			continue
		}
		names = append(names, strings.Trim(fields[0], "\"`"))
	}
	return names
}

// packageDependencies is what a package.json needs to run. What it needs
// only to be developed -- a test runner, a linter -- says nothing about
// what it is built on.
func packageDependencies(content []byte) []string {
	var manifest struct {
		Dependencies map[string]any `json:"dependencies"`
	}
	if json.Unmarshal(content, &manifest) != nil {
		return nil
	}
	names := make([]string, 0, len(manifest.Dependencies))
	for dependencyName := range manifest.Dependencies {
		names = append(names, dependencyName)
	}
	sort.Strings(names)
	return names
}

// quotedString is one string literal, in either quote.
var quotedString = regexp.MustCompile(`"([^"\n]*)"|'([^'\n]*)'`)

// quotedStrings is every string literal in a piece of text.
func quotedStrings(text string) []string {
	var found []string
	for _, match := range quotedString.FindAllStringSubmatch(text, -1) {
		found = append(found, match[1]+match[2])
	}
	return found
}

// pythonRequirementName is the package a requirement names:
// "example-lib[extra]>=1.2; python_version>'3.8'" is example-lib. Written
// the way the package index compares names, lowercase and with dashes,
// so Example_Lib and example-lib are one.
func pythonRequirementName(requirement string) string {
	requirement = strings.TrimSpace(requirement)
	if requirement == "" || strings.HasPrefix(requirement, "-") || strings.HasPrefix(requirement, "#") {
		return ""
	}
	if end := strings.IndexAny(requirement, " <>=!~;[(@,"); end >= 0 {
		requirement = requirement[:end]
	}
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(requirement)), "_", "-")
}

// tomlTable is the table a line of TOML opens, when it opens one.
func tomlTable(line string) (string, bool) {
	if !strings.HasPrefix(line, "[") || strings.HasPrefix(line, "[[") {
		return "", false
	}
	end := strings.Index(line, "]")
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(line[1:end]), true
}

// tomlKey is the key a line of TOML sets, without its quotes.
func tomlKey(line string) string {
	key, _, isSet := strings.Cut(line, "=")
	if !isSet {
		return ""
	}
	return strings.Trim(strings.TrimSpace(key), "\"'")
}

// untilListCloses is a line of a list up to the bracket that closes it,
// and whether it did: a bracket inside a string -- the extras of
// "example-lib[fast]" -- does not.
func untilListCloses(line string) (string, bool) {
	quote := byte(0)
	for index := 0; index < len(line); index++ {
		switch character := line[index]; {
		case quote != 0 && character == quote:
			quote = 0
		case quote != 0:
		case character == '"' || character == '\'':
			quote = character
		case character == '#':
			return line[:index], false
		case character == ']':
			return line[:index], true
		}
	}
	return line, false
}

// pyprojectDependencies is the packages a pyproject.toml needs: the
// `dependencies` list of its [project] table, or the keys of Poetry's
// dependency table. Read line by line, which is all this part of TOML
// needs.
func pyprojectDependencies(content []byte) []string {
	var names []string
	table := ""
	inList := false
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if inList {
			before, isClosed := untilListCloses(line)
			for _, requirement := range quotedStrings(before) {
				names = append(names, pythonRequirementName(requirement))
			}
			inList = !isClosed
			continue
		}
		if name, isTable := tomlTable(line); isTable {
			table = name
			continue
		}
		switch table {
		case "project":
			if tomlKey(line) != "dependencies" {
				continue
			}
			_, value, _ := strings.Cut(line, "=")
			value = strings.TrimSpace(value)
			if !strings.HasPrefix(value, "[") {
				continue
			}
			before, isClosed := untilListCloses(value[1:])
			for _, requirement := range quotedStrings(before) {
				names = append(names, pythonRequirementName(requirement))
			}
			inList = !isClosed
		case "tool.poetry.dependencies":
			if key := tomlKey(line); key != "" && key != "python" && !strings.HasPrefix(key, "#") {
				names = append(names, pythonRequirementName(key))
			}
		}
	}
	return names
}

// setupPyRequirements is the list a setup.py passes as install_requires,
// when it is written out as a list of strings there. One built by code
// elsewhere in the file is not read: running the file is the only way to
// know it, and this does not run anything.
func setupPyRequirements(content []byte) []string {
	text := string(content)
	start := strings.Index(text, "install_requires")
	if start < 0 {
		return nil
	}
	text = text[start:]
	open := strings.Index(text, "[")
	if open < 0 {
		return nil
	}
	close := strings.Index(text[open:], "]")
	if close < 0 {
		return nil
	}
	var names []string
	for _, requirement := range quotedStrings(text[open : open+close]) {
		names = append(names, pythonRequirementName(requirement))
	}
	return names
}

// setupCfgRequirements is the install_requires of a setup.cfg's [options]:
// a value on its own line and the indented lines that continue it.
func setupCfgRequirements(content []byte) []string {
	var names []string
	section := ""
	inValue := false
	for _, raw := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(raw)
		if comment := strings.Index(line, "#"); comment >= 0 {
			line = strings.TrimSpace(line[:comment])
		}
		if inValue {
			if raw != "" && (raw[0] == ' ' || raw[0] == '\t') {
				names = append(names, pythonRequirementName(line))
				continue
			}
			if line == "" {
				continue
			}
			inValue = false
		}
		if name, isSection := tomlTable(line); isSection {
			section = name
			continue
		}
		if section != "options" || tomlKey(line) != "install_requires" {
			continue
		}
		_, value, _ := strings.Cut(line, "=")
		for _, requirement := range strings.Split(value, ";") {
			names = append(names, pythonRequirementName(requirement))
		}
		inValue = true
	}
	return names
}

// cargoDependencies is the crates a Cargo.toml needs: the keys of its
// [dependencies] table, and each [dependencies.<name>] table. A
// workspace's shared table counts, since it is what the members need.
func cargoDependencies(content []byte) []string {
	var names []string
	table := ""
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if name, isTable := tomlTable(line); isTable {
			table = name
			for _, prefix := range []string{"dependencies.", "workspace.dependencies."} {
				if crate, isCrate := strings.CutPrefix(name, prefix); isCrate {
					names = append(names, strings.Trim(crate, "\"'"))
				}
			}
			continue
		}
		if table != "dependencies" && table != "workspace.dependencies" {
			continue
		}
		if key := tomlKey(line); key != "" && !strings.HasPrefix(key, "#") {
			names = append(names, key)
		}
	}
	return names
}

// findPackage is a CMake find_package call and the package it names.
var findPackage = regexp.MustCompile(`(?i)\bfind_package\s*\(\s*([A-Za-z0-9_.+-]+)`)

// cmakePackages is the packages a CMakeLists.txt finds. A name held in a
// variable is not read, since nothing here evaluates CMake.
func cmakePackages(content []byte) []string {
	var names []string
	for _, line := range strings.Split(string(content), "\n") {
		if comment := strings.Index(line, "#"); comment >= 0 {
			line = line[:comment]
		}
		for _, match := range findPackage.FindAllStringSubmatch(line, -1) {
			names = append(names, match[1])
		}
	}
	return names
}

// modulesetModules is the modules a jhbuild moduleset declares, and the
// other files of the same checkout it includes, as tracked paths.
//
// A module is any element directly under the moduleset with an `id`:
// autotools, cmake, meson, distutils, metamodule and the rest. Its
// repository is the last segment of its branch's `module`, the path of
// the repository on the server it names; a module without one is built
// from a repository of its own name, which is what jhbuild does too.
func modulesetModules(content []byte, file string) ([]RepositoryModule, []string) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose
	decoder.Entity = xml.HTMLEntity
	var modules []RepositoryModule
	var includes []string
	var current *RepositoryModule
	depth := 0
	inDependencies := false
	for {
		token, err := decoder.Token()
		if err != nil {
			break // the end of the file, or as far as it parses
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
			attributes := map[string]string{}
			for _, attribute := range element.Attr {
				attributes[attribute.Name.Local] = strings.TrimSpace(attribute.Value)
			}
			switch {
			case depth == 2 && element.Name.Local == "include":
				if include := modulesetInclude(file, attributes["href"]); include != "" {
					includes = append(includes, include)
				}
			case depth == 2 && attributes["id"] != "":
				modules = append(modules, RepositoryModule{Name: attributes["id"], Repository: attributes["id"], File: file})
				current = &modules[len(modules)-1]
			case current != nil && element.Name.Local == "branch":
				if repository := repositoryOfModulePath(attributes["module"]); repository != "" {
					current.Repository = repository
				}
			case current != nil && element.Name.Local == "dependencies":
				inDependencies = true
			case current != nil && inDependencies && element.Name.Local == "dep" && attributes["package"] != "":
				current.Dependencies = append(current.Dependencies, attributes["package"])
			}
		case xml.EndElement:
			switch {
			case depth == 2:
				current = nil
				inDependencies = false
			case element.Name.Local == "dependencies":
				inDependencies = false
			}
			depth--
		}
	}
	return modules, includes
}

// modulesetInclude is where an include points, as a path in the same
// checkout; empty for one that points at another server or out of the
// checkout.
func modulesetInclude(file, href string) string {
	if href == "" || strings.Contains(href, "://") || strings.HasPrefix(href, "/") {
		return ""
	}
	included := path.Clean(path.Join(path.Dir(file), href))
	if included == ".." || strings.HasPrefix(included, "../") {
		return ""
	}
	return included
}

// repositoryOfModulePath is the name a repository path ends in:
// "core/example-lib.git" is example-lib.
func repositoryOfModulePath(modulePath string) string {
	modulePath = strings.TrimRight(strings.TrimSpace(modulePath), "/")
	if modulePath == "" {
		return ""
	}
	return strings.TrimSuffix(path.Base(modulePath), ".git")
}
