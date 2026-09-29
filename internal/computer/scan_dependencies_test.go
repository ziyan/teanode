package computer

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeCheckoutFiles writes files into a directory standing in for a
// checkout, and returns their paths as git would list them.
func writeCheckoutFiles(test *testing.T, directory string, files map[string]string) []string {
	test.Helper()
	var tracked []string
	for relative, content := range files {
		full := filepath.Join(directory, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			test.Fatalf("MkdirAll: %s", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			test.Fatalf("WriteFile: %s", err)
		}
		tracked = append(tracked, relative)
	}
	return tracked
}

// dependencyNames is the names of the dependencies read from one build
// file, in the order they were read.
func dependencyNames(dependencies []RepositoryDependency, file string) []string {
	var names []string
	for _, dependency := range dependencies {
		if dependency.File == file {
			names = append(names, dependency.Name)
		}
	}
	return names
}

// Every manifest format, each with what it needs and what it only needs
// to be developed, which is not read.
func TestDependenciesAreReadFromEveryBuildFile(test *testing.T) {
	directory := test.TempDir()
	tracked := writeCheckoutFiles(test, directory, map[string]string{
		"go.mod": "module git.example.com/apps/example-app\n\ngo 1.26\n\n" +
			"require git.example.com/core/example-lib v1.2.0\n\n" +
			"require (\n\tgit.example.com/core/example-log v0.3.1\n\tgit.example.com/core/example-transitive v0.1.0 // indirect\n)\n",
		"package.json": `{"name": "example-app", "dependencies": {"example-widgets": "^2.0.0", "@example/ui": "1.x"}, "devDependencies": {"example-test-runner": "^9"}}`,
		"pyproject.toml": "[project]\nname = \"example-app\"\ndependencies = [\n  \"Example_Lib[fast]>=1.2\",\n  \"example-client ; python_version>'3.9'\",\n]\n\n" +
			"[project.optional-dependencies]\ntest = [\"example-test-runner\"]\n\n[tool.poetry.dependencies]\npython = \"^3.11\"\nexample-poetry-lib = \"^1\"\n",
		"setup.py":  "from setuptools import setup\nsetup(\n    name='example-app',\n    install_requires=['example-setup-lib[fast]>=2', \"example-other\",\n        'example-third'],\n    extras_require={'docs': ['example-docs']},\n)\n",
		"setup.cfg": "[metadata]\nname = example-app\n\n[options]\ninstall_requires =\n    example-cfg-lib>=1\n    example-cfg-two\npython_requires = >=3.9\n",
		"Cargo.toml": "[package]\nname = \"example-app\"\n\n[dependencies]\nexample-crate = \"1\"\nexample-path = { path = \"../example-path\" }\n\n" +
			"[dependencies.example-table]\nversion = \"2\"\n\n[dev-dependencies]\nexample-bench = \"1\"\n",
		"CMakeLists.txt": "cmake_minimum_required(VERSION 3.20)\nproject(exampleapp)\nfind_package(ExampleLib REQUIRED)\nFIND_PACKAGE ( examplelog CONFIG )\n# find_package(ExampleCommented)\nfind_package(${EXAMPLE_VARIABLE})\n",
	})
	dependencies, modules := repositoryDependencies(directory, tracked)
	if len(modules) != 0 {
		test.Fatalf("no moduleset, no modules: %+v", modules)
	}
	for file, want := range map[string][]string{
		"go.mod":         {"git.example.com/core/example-lib", "git.example.com/core/example-log"},
		"package.json":   {"@example/ui", "example-widgets"},
		"pyproject.toml": {"example-lib", "example-client", "example-poetry-lib"},
		"setup.py":       {"example-setup-lib", "example-other", "example-third"},
		"setup.cfg":      {"example-cfg-lib", "example-cfg-two"},
		"Cargo.toml":     {"example-crate", "example-path", "example-table"},
		"CMakeLists.txt": {"ExampleLib", "examplelog"},
	} {
		if got := dependencyNames(dependencies, file); !reflect.DeepEqual(got, want) {
			test.Errorf("%s: got %q, want %q", file, got, want)
		}
	}
	for _, dependency := range dependencies {
		if dependency.Ecosystem == "" {
			test.Errorf("%q has no ecosystem", dependency.Name)
		}
	}
}

// What git does not track is not the checkout's word, and a vendored or
// third-party tree is somebody else's.
func TestDependenciesComeOnlyFromTheCheckoutsOwnTrackedFiles(test *testing.T) {
	directory := test.TempDir()
	tracked := writeCheckoutFiles(test, directory, map[string]string{
		"vendor/example.modules":      `<moduleset><cmake id="examplevendored"/></moduleset>`,
		"third_party/example.modules": `<moduleset><cmake id="examplethirdparty"/></moduleset>`,
		"README.md":                   "An example.\n",
	})
	writeCheckoutFiles(test, directory, map[string]string{
		"go.mod": "module example\n\nrequire git.example.com/core/example-untracked v1.0.0\n",
	})
	dependencies, modules := repositoryDependencies(directory, tracked)
	if len(dependencies) != 0 || len(modules) != 0 {
		test.Fatalf("read what is not the checkout's own: %+v %+v", dependencies, modules)
	}
	// A build file too large to be a build file is skipped, and the rest
	// still counts.
	tracked = writeCheckoutFiles(test, directory, map[string]string{
		"go.mod":       "module example\n\nrequire git.example.com/core/example-lib v1.0.0\n",
		"package.json": `{"dependencies": {"example-widgets": "1"}, "padding": "` + strings.Repeat("x", dependencyFileBytes) + `"}`,
	})
	dependencies, _ = repositoryDependencies(directory, tracked)
	if len(dependencies) != 1 || dependencies[0].Name != "git.example.com/core/example-lib" {
		test.Fatalf("the large file is skipped and go.mod still read: %+v", dependencies)
	}
}

// A jhbuild moduleset names the repositories its modules build from and
// what each needs, and may include another moduleset of the same
// checkout.
func TestAModulesetsModulesAndTheirRepositories(test *testing.T) {
	directory := test.TempDir()
	tracked := writeCheckoutFiles(test, directory, map[string]string{
		"modulesets/example.modules": `<?xml version="1.0"?>
<!DOCTYPE moduleset SYSTEM "moduleset.dtd">
<moduleset>
  <repository type="git" name="example" href="https://git.example.com/"/>
  <include href="common.xml"/>
  <include href="https://git.example.com/elsewhere.modules"/>
  <include href="../../outside.modules"/>
  <cmake id="examplelibcpp">
    <branch repo="example" module="core/example-lib.git"/>
  </cmake>
  <cmake id="exampleappcpp">
    <branch repo="example" module="apps/example-app.git"/>
    <dependencies><dep package="examplelibcpp"/><dep package="examplebase"/></dependencies>
    <after><dep package="exampleoptional"/></after>
  </cmake>
  <metamodule id="exampleall">
    <dependencies><dep package="exampleappcpp"/><dep package="examplegroup"/></dependencies>
  </metamodule>
  <metamodule id="examplegroup">
    <dependencies><dep package="exampletool"/><dep package="exampleall"/></dependencies>
  </metamodule>
  <cmake id="exampletool">
    <branch repo="example" module="tools/example-tool-source.git" checkoutdir="example-tool"/>
  </cmake>
  <cmake id="examplerelease">
    <branch repo="example" module="apps/example-release.git"/>
    <dependencies><dep package="exampleall"/></dependencies>
  </cmake>
</moduleset>
`,
		"modulesets/common.xml": `<moduleset><autotools id="examplebase"><branch repo="example" module="core/example-base"/></autotools></moduleset>`,
	})
	dependencies, modules := repositoryDependencies(directory, tracked)
	if len(dependencies) != 0 {
		test.Fatalf("a moduleset is modules, not dependencies: %+v", dependencies)
	}
	want := []RepositoryModule{
		{Name: "examplelibcpp", Repository: "example-lib", File: "modulesets/example.modules"},
		{Name: "exampleappcpp", Repository: "example-app", Dependencies: []string{"examplelibcpp", "examplebase"}, File: "modulesets/example.modules"},
		// A branch checked out into a directory of another name is that
		// directory's; a metamodule is not a module, and a dependency on
		// one is on what it groups, a metamodule inside it included.
		{Name: "exampletool", Repository: "example-tool", File: "modulesets/example.modules"},
		{Name: "examplerelease", Repository: "example-release", Dependencies: []string{"exampleappcpp", "exampletool"}, File: "modulesets/example.modules"},
		{Name: "examplebase", Repository: "example-base", File: "modulesets/common.xml"},
	}
	if !reflect.DeepEqual(modules, want) {
		test.Fatalf("modules:\n got %+v\nwant %+v", modules, want)
	}
}

// A checkout that names more than the bound is cut at the bound.
func TestDependenciesAreBounded(test *testing.T) {
	directory := test.TempDir()
	var goMod strings.Builder
	goMod.WriteString("module example\n\nrequire (\n")
	for index := range dependencyEntries + 20 {
		goMod.WriteString("\tgit.example.com/many/example-" + strings.Repeat("a", index%7+1) + "-" + string(rune('a'+index%26)) + string(rune('a'+index/26%26)) + " v1.0.0\n")
	}
	goMod.WriteString(")\n")
	tracked := writeCheckoutFiles(test, directory, map[string]string{
		"go.mod":          goMod.String(),
		"example.modules": `<moduleset><cmake id="exampleone"/></moduleset>`,
	})
	dependencies, modules := repositoryDependencies(directory, tracked)
	// Each list has its own bound, so a long go.mod does not crowd out
	// the moduleset.
	if len(dependencies) != dependencyEntries || len(modules) != 1 {
		test.Fatalf("read %d dependencies and %d modules, want %d and 1", len(dependencies), len(modules), dependencyEntries)
	}
}

// A monorepo's parts are components, each with what it needs: a CMake
// library another links, a package of a workspace, a Go module and a
// crate, while the top of the checkout and its tests are not parts of it.
func TestAMonoreposPartsAreComponents(test *testing.T) {
	directory := test.TempDir()
	tracked := writeCheckoutFiles(test, directory, map[string]string{
		"CMakeLists.txt":            "project(examplemono)\nadd_subdirectory(libs/core)\nadd_subdirectory(apps/viewer)\nadd_subdirectory(tests)\n",
		"libs/core/CMakeLists.txt":  "add_library(examplecore STATIC core.c)\nadd_library(example::core ALIAS examplecore)\nfind_package(ExampleMath REQUIRED)\n",
		"libs/group/CMakeLists.txt": "add_subdirectory(one)\n",
		"apps/viewer/CMakeLists.txt": "project(exampleviewer)\nadd_executable(exampleviewer main.c)\n" +
			"target_link_libraries(exampleviewer\n  PRIVATE example::core\n  pthread\n)\n",
		"tests/CMakeLists.txt":       "add_executable(exampletests tests.c)\ntarget_link_libraries(exampletests examplecore)\n",
		"web/ui/package.json":        `{"name": "@example/ui", "dependencies": {"@example/theme": "1"}}`,
		"web/package.json":           `{"name": "example-web", "private": true, "workspaces": ["ui"]}`,
		"tools/sync/go.mod":          "module git.example.com/mono/tools/sync\n\nrequire git.example.com/core/example-lib v1.0.0\n",
		"crates/parser/Cargo.toml":   "[package]\nname = \"example-parser\"\n\n[dependencies]\nexample-lexer = \"1\"\n",
		"a/b/c/d/e/CMakeLists.txt":   "add_library(exampletoodeep deep.c)\n",
		"vendor/lib/CMakeLists.txt":  "add_library(examplevendored v.c)\n",
		"modulesets/example.modules": `<moduleset><cmake id="examplemonocpp"><branch module="mono/example-mono.git"/></cmake><distutils id="examplemonopy"><branch module="mono/example-mono.git"/><dependencies><dep package="examplemonocpp"/></dependencies></distutils></moduleset>`,
	})
	_, modules := repositoryDependencies(directory, tracked)
	components := repositoryComponents(directory, tracked, ownRepositoryNames(directory+"/checkout", []string{"git@git.example.com:mono/example-mono.git"}), modules)
	want := []RepositoryComponent{
		{Path: "apps/viewer", Name: "exampleviewer", Ecosystem: EcosystemCMake, File: "apps/viewer/CMakeLists.txt", Dependencies: []string{"examplecore"}},
		{Path: "crates/parser", Name: "example-parser", Ecosystem: EcosystemCargo, File: "crates/parser/Cargo.toml", Dependencies: []string{"example-lexer"}},
		{Path: "libs/core", Name: "examplecore", Ecosystem: EcosystemCMake, File: "libs/core/CMakeLists.txt", Dependencies: []string{"ExampleMath"}},
		{Path: "tools/sync", Name: "git.example.com/mono/tools/sync", Ecosystem: EcosystemGo, File: "tools/sync/go.mod", Dependencies: []string{"git.example.com/core/example-lib"}},
		{Path: "web/ui", Name: "@example/ui", Ecosystem: EcosystemNpm, File: "web/ui/package.json", Dependencies: []string{"@example/theme"}},
		{Name: "examplemonocpp", Ecosystem: EcosystemJhbuild, File: "modulesets/example.modules"},
		{Name: "examplemonopy", Ecosystem: EcosystemJhbuild, File: "modulesets/example.modules", Dependencies: []string{"examplemonocpp"}},
	}
	if !reflect.DeepEqual(components, want) {
		test.Fatalf("components:\n got %+v\nwant %+v", components, want)
	}
	// One module built from the checkout is the checkout, not a part of it.
	components = repositoryComponents(directory, nil, []string{"example-mono"}, modules[:1])
	if len(components) != 0 {
		test.Fatalf("one module is not split off: %+v", components)
	}
}

// A CMake target named by the project's variable is the project's name,
// the words around a target that are not names are skipped, and a target
// named by any other variable is not read at all.
func TestCMakeTargetsNamedByTheProjectAreRead(test *testing.T) {
	directory := test.TempDir()
	tracked := writeCheckoutFiles(test, directory, map[string]string{
		"CMakeLists.txt": "project(examplesuite)\nadd_subdirectory(libs/codec)\nadd_subdirectory(apps/player)\n",
		"libs/codec/CMakeLists.txt": "project(examplecodec)\nadd_library(${PROJECT_NAME} SHARED codec.c)\n" +
			"add_library(example::codec ALIAS ${PROJECT_NAME})\n",
		"libs/imported/CMakeLists.txt": "add_library(exampleimported SHARED IMPORTED GLOBAL)\nadd_library(${EXAMPLE_GENERATED_NAME} STATIC generated.c)\n",
		"libs/nested/CMakeLists.txt":   "add_library(${PROJECT_NAME}_nested STATIC EXCLUDE_FROM_ALL nested.c)\n",
		"apps/player/CMakeLists.txt": "add_executable(${CMAKE_PROJECT_NAME}-player WIN32 main.c)\n" +
			"target_link_libraries(${CMAKE_PROJECT_NAME}-player PUBLIC example::codec PRIVATE ${EXAMPLE_EXTRA_LIBRARIES}\n" +
			"  debug examplenested optimized $<TARGET_NAME:examplecodec> general examplesuite_nested)\n",
	})
	components := repositoryComponents(directory, tracked, nil, nil)
	want := []RepositoryComponent{
		{Path: "apps/player", Name: "examplesuite-player", Ecosystem: EcosystemCMake, File: "apps/player/CMakeLists.txt",
			Dependencies: []string{"examplecodec", "examplesuite_nested"}},
		{Path: "libs/codec", Name: "examplecodec", Ecosystem: EcosystemCMake, File: "libs/codec/CMakeLists.txt"},
		{Path: "libs/nested", Name: "examplesuite_nested", Ecosystem: EcosystemCMake, File: "libs/nested/CMakeLists.txt"},
	}
	if !reflect.DeepEqual(components, want) {
		test.Fatalf("components:\n got %+v\nwant %+v", components, want)
	}
}

// With more components than the bound, the shallowest are kept.
func TestComponentsAreKeptShallowestFirst(test *testing.T) {
	directory := test.TempDir()
	files := map[string]string{"zeta/go.mod": "module example.com/zeta\n"}
	for index := range componentEntries {
		files[fmt.Sprintf("alpha/deep/part%03d/go.mod", index)] = fmt.Sprintf("module example.com/part%03d\n", index)
	}
	components := repositoryComponents(directory, writeCheckoutFiles(test, directory, files), nil, nil)
	if len(components) != componentEntries || components[0].Path != "zeta" {
		test.Fatalf("%d components, the first %+v", len(components), components[0])
	}
}

// A directory that builds a library and the programs that check it is the
// library, whichever its file declares first, and the rest of the
// checkout's links to that library land on it.
func TestAComponentIsNamedAfterItsMainTarget(test *testing.T) {
	directory := test.TempDir()
	tracked := writeCheckoutFiles(test, directory, map[string]string{
		"CMakeLists.txt": "add_subdirectory(core)\nadd_subdirectory(app)\n",
		"core/CMakeLists.txt": "add_executable(check_gardencore_accuracy check.cpp)\n" +
			"add_library(gardencore SHARED plants.cpp)\n" +
			"add_executable(gardencore_fuzz fuzz.cpp)\n" +
			"target_link_libraries(check_gardencore_accuracy gardencore)\n",
		"app/CMakeLists.txt": "add_executable(gardenapp main.cpp)\ntarget_link_libraries(gardenapp PRIVATE gardencore)\n",
	})
	components := repositoryComponents(directory, tracked, nil, nil)
	names := map[string]RepositoryComponent{}
	for _, component := range components {
		names[component.Path] = component
	}
	if got := names["core"].Name; got != "gardencore" {
		test.Errorf("the core directory is named %q", got)
	}
	if got := names["app"].Dependencies; len(got) != 1 || got[0] != "gardencore" {
		test.Errorf("the app needs %v", got)
	}
}
