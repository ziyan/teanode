# Big-picture memory: a map of the checkouts, overviews written bottom-up, and answers drawn from them

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

The agent's memory is a graph of pages, each holding one-sentence facts, filled in by the nightly dream from what it reads. It answers "what did Alice say about the release" well, because some fact or passage is close to the question. It cannot answer "looking at the product I work on and every repository behind it, what are its strengths and weaknesses, and how should it improve", because no page and no passage is about the whole. Recall for such a question returns a handful of scattered details: a note about boot security, a timing test on one board, a package list from years ago. Nothing says what the parts are and how they fit together.

Research on the same problem (question answering over a whole corpus, sometimes called global sensemaking) converges on one pattern. Build a structure over the corpus bottom-up, where each level summarizes the level below: summaries of clusters of related things, and summaries of those summaries. Then answer a question about the whole by asking each summary for its part of the answer in parallel and combining the parts ("map-reduce"). For code in particular, a map of the build-time dependencies between components, read from the build files by a program rather than guessed by a model, is what makes the summaries accurate.

After this plan:

- Every checkout the agent knows has its dependencies on other checkouts as links in the graph, read from its build files, with no model involved.
- Every checkout page carries an overview: what it is for, its parts, its interfaces, what it depends on and what depends on it, and how active it is. The dream writes it from the checkout's profile, its links and a few of its key files, and rewrites it when those change.
- Groups of checkouts that depend on one another become system pages, each with an overview written from its members' overviews.
- A question about the whole is answered by a survey: one model call per relevant overview, in parallel, each free to look at code and documents, then one call that combines them into a report with citations. The agent's `survey` tool, `teanode agent survey` and the `SurveyAgentMemory` operation all run it.

To see it working: on a development server with a knowledge source over a folder of checkouts, run `teanode agent memory get projects/<a checkout>` and see "depends on" links and an overview; run `teanode agent memory index systems` and see the system pages; run `teanode agent survey "what are the weak points of the system these checkouts make up?"` and get a report that cites overviews and files.

## Progress

- [x] (2026-09-28) Surveyed the checkout profile, the graph model, the dream, recall and the subagent tool; wrote this plan.
- [ ] Milestone 1: dependencies read from build files on the device, and `depends_on` links between checkout pages.
- [ ] Milestone 2: activity per checkout (commits, authors, issues and merge requests), kept as facts.
- [ ] Milestone 3: overviews of checkout pages, written and rewritten by the dream; shown in the dashboard, the CLI and the memory tool.
- [ ] Milestone 4: system pages from clusters of dependent checkouts, with overviews written from their members'.
- [ ] Milestone 5: the survey: map-reduce answers over overviews, as a tool, a command and an operation; recall prefers overviews for broad questions.
- [ ] Milestone 6: docs, deploy, and the question this plan began with, asked on the development server.

## Surprises & Discoveries

- Observation: the device already builds a profile of every checkout it finds (`RepositoryProfile` in `internal/computer/scan.go`) and sends it to the server once a scan pass ends. Build files are read only for a name (`manifestName` in `internal/computer/scan_repository.go`: the `module` line of `go.mod`, `name` of `package.json`, the first `name` in `pyproject.toml` or `Cargo.toml`). No dependency is read, and nothing reads CMake or jhbuild files.
- Observation: there is no `depends_on` relation (`AgentEdgeRelations` in `internal/models/graph.go`: part_of, works_on, member_of, knows, owns, uses, located_in, related_to, decided_in, about), and no clustering code anywhere.
- Observation: documents from a GitLab source carry `project`, `kind`, `state`, `number`, `author` and `assignees` in their metadata, so issues and merge requests can be counted per repository without a model.
- Observation: a page's opening (`AgentNode.Summary`) is one paragraph, rewritten by `dreamConsolidate` from the page's facts. It is too short to hold an architecture, and it is about what the page is, not how the thing works.
- Observation: the `subagent` tool (`internal/agent/tools_subagent.go`) runs one sub-run per call, depth 1, 20 rounds, 10 minutes. Nothing runs several at once except a model issuing several calls in one round.

## Decision Log

- Decision: dependencies are read from build files on the device, deterministically, and become stated `depends_on` edges with repository evidence.
  Rationale: the dependency map is the one part of the structure a program can get exactly right, and the research on code agents found it is what makes model-written architecture accurate. It costs no model call and updates with every scan. The device already reads the checkout for its profile, so the reader goes beside `manifestName`.
  Date/Author: 2026-09-28.
- Decision: a new relation, `depends_on` ("depends on" / "is depended on by"), rather than reusing `uses`.
  Rationale: `uses` is already written by the model for people and tools ("uses a standing desk"); clustering and overviews need the build-time relation on its own, and one word for two things is what this codebase avoids.
  Date/Author: 2026-09-28.
- Decision: an overview is a new text field on a page (`agent_node.overview`, with `overview_written_at` and the inputs' fingerprint `overview_inputs`), not a child page and not the opening.
  Rationale: the opening is what a page is, one paragraph, and every prompt's index carries it; an overview is how the thing works, several short sections, and only a survey or a question about it carries it. A child page would put the overview's sentences through fact filing, which splits prose into claims and loses its structure.
  Date/Author: 2026-09-28.
- Decision: an overview is rewritten only when its inputs change, and the inputs are named by a fingerprint: the checkout's HEAD, its links, and its activity bucket.
  Rationale: one model call per checkout per change keeps the cost proportional to what changed, not to the size of the corpus; a checkout nobody touched keeps its overview for free.
  Date/Author: 2026-09-28.
- Decision: systems are found by clustering the `depends_on` graph (label propagation, deterministic by page id order), named and described by the model, and kept as pages under a new root `systems`.
  Rationale: label propagation is a few dozen lines, needs no library, and finds the groups a dependency graph has (the parts of one product depend on one another far more than on another product's). The model only names and summarizes a group it did not choose, so a wrong grouping is visible in the links rather than hidden in prose.
  Date/Author: 2026-09-28.
- Decision: the survey runs its calls itself, several at once, rather than asking the model to issue subagent calls.
  Rationale: a map step that depends on the model remembering to fan out is a map step that sometimes does not happen; the number of overviews decides the number of calls.
  Date/Author: 2026-09-28.
- Decision: examples in code, tests and docs are invented (`example-app`, `example-lib`, `a warehouse controller`); nothing names the person's employer, products or repositories.
  Rationale: the repository is public.
  Date/Author: 2026-09-28.

## Outcomes & Retrospective

Nothing yet.

## Context and Orientation

The words used here, in this repository's own terms:

A knowledge source is a folder, archive or service the agent reads, configured per person (`teanode agent knowledge list`). A folder source is read on the person's own computer by the computer daemon, a program the person runs that connects to the server (`internal/computer`). The daemon walks the folder (`listTree` in `internal/computer/scan_tree.go`), recognizes every directory with a `.git` as a checkout, builds a `RepositoryProfile` for it (`repositoryProfile` in `internal/computer/scan_repository.go`; fields in `internal/computer/scan.go`: head, branches, newest tag, remotes, languages, readme, description, first and last commit, commit count, contributors, top-level directories, module name, and the top authors), and sends the profiles to the server on the last page of the pass, as entries of kind `repository` (`internal/computer/scan_files.go`). A pass's manifest (`passManifest` in `internal/computer/scan_manifest.go`) is the tree as that pass saw it, built once and reused for every page of the pass.

On the server, `fileRepository` (`internal/agent/ingest_repository.go`) turns a profile into a page of kind project at `projects/<directory name>` (or under the source's root path when it has one), writes keyed facts about it, and when the person made enough of the commits, an event on `self/work` and an edge `self works_on <project>`. Facts written from a profile carry evidence of kind repository whose quote is a key, so the next pass updates the same numbered fact rather than adding another.

The graph is in `internal/models/graph.go`: `AgentNode` (a page: path, kind, name, aliases, `Summary` which is its opening paragraph, importance), `AgentFact` (a numbered sentence with evidence), `AgentEdge` (a relation between two pages, stated or proposed, with evidence and a note). Relations and the phrases that say them are `AgentEdgeRelations`, `relationPhrases` and `pastPhrases` in the same file. The database side is `internal/db/database_graph.go` and `internal/db/database_dream.go`.

The dream is a nightly run of phases in `runDream` (`internal/agent/dream.go`): revise, look for ideas, digest (reading), attachments, timeline, consolidate (rewrite openings), organize, split, the quiet half, associate (the walk that proposes links), embed, rehearse. Each phase spends from a shared budget (`dreamBudget` in `internal/agent/dream_budget.go`), and model calls go through `dreamThink` or `dreamThought` (`internal/agent/dream_request.go`), whose title names the run. What a night did is counted on `models.AgentDream`.

Recall (`internal/agent/graph_recall.go`) picks up to five pages for a turn from full-text and vector search, and every prompt also carries an index of the most important pages (`carryIndex` in `internal/agent/ask.go`). The agent's `memory` tool reads and writes the graph; `teanode agent memory` is the command line; the Knowledge page in the dashboard shows it.

## Plan of Work

Milestone 1, the dependency map. In `internal/computer`, add `Dependencies []RepositoryDependency` to `RepositoryProfile`, where a dependency is a name, an ecosystem (go, npm, python, cargo, cmake, jhbuild) and the build file it came from, and add `Modules []RepositoryModule` for build files that describe other repositories: a jhbuild moduleset (an XML file ending `.modules` whose module elements, such as `autotools`, `cmake`, `distutils`, `meson` and `metamodule`, have an `id`, a `branch` naming a repository and module path, and `dependencies` holding `dep package="..."`) lists modules, each with the repository it builds from and the modules it depends on. New file `internal/computer/scan_dependencies.go` reads, from the checkout's tracked files only: `go.mod` require lines; `package.json` dependencies, devDependencies excluded; `pyproject.toml` `[project] dependencies` and `setup.py` / `setup.cfg` `install_requires`, names only; `Cargo.toml` `[dependencies]`; top-level `CMakeLists.txt` `find_package(Name ...)`; and every `.modules` file. Each reader is bounded (file size, entries) and never fails the profile: an unreadable file is skipped. In `internal/models/graph.go` add the relation `depends_on` with its phrases. In `internal/agent/ingest_repository.go`, after the page is filed, resolve each dependency to a checkout page by, in order: the module name another profile declared, a jhbuild module's repository path basename (without `.git`), a remote's last path segment, and the directory name; write a stated `depends_on` edge with evidence of kind repository and quote `dependency:<name>`, and remove the edges this checkout wrote before that no longer resolve. Dependencies that resolve to nothing are counted in one keyed fact ("Depends on 14 packages that are not checkouts here, among them ..."), at most ten named. A jhbuild moduleset also yields edges between the modules' repositories themselves, since it states them. Tests: `internal/computer/scan_dependencies_test.go` with invented fixture files for each format; `internal/agent/ingest_repository` tests that three invented checkouts where `example-app` requires `example-lib` produce the edge and remove it when the requirement goes.

Milestone 2, activity. On the server, compute per checkout page, without a model: commits in the last 90 and 365 days and distinct authors in the last 365 (from the commit documents the source already sends, metadata `repository`), and from GitLab documents whose `project` ends with the checkout's remote path, open issues, issues opened in the last 90 days, and merge requests merged in the last 90 days. Keep them as one keyed fact "Activity: ..." updated in place by the digest phase once a night, and as numbers the overview reads. Test with invented documents.

Milestone 3, overviews. Migration: `agent_node.overview text not null default ''`, `overview_written_at timestamptz`, `overview_inputs varchar(64) not null default ''`. A new dream phase `dreamOverviews`, after consolidate, writes the overview of checkout pages whose fingerprint (a hash of HEAD, the sorted `depends_on` edges in both directions, and the activity bucket) differs from `overview_inputs`, most important first, at most twelve a night, with the budget. Its prompt (`internal/agent/prompts/overview_checkout.txt`) gives the profile, the page's facts, the links in both directions with the other pages' openings, the activity, and the text of up to four key files found in the source's indexed documents (the readme, the main build file, and the largest two files among the top-level directories' entry points); it asks for a JSON object with sections: purpose, parts, interfaces, depends on and depended on by (in words), activity, and concerns (what looks fragile, with the evidence), each at most a few sentences, and the files it relied on. The code writes the sections as markdown into `overview`, and the cited files as the overview's evidence. A checkout kept to its profile (someone else's work) gets an overview from its profile and links only. GraphQL `AgentNode` gains `overview` and `overviewWrittenAt`; `RewriteAgentOverview(path)` clears the fingerprint so the next dream rewrites it. `teanode agent memory get` prints the overview under the opening; `teanode agent memory overview <path> --rewrite` calls the mutation. The memory tool's `get` includes the overview. The Knowledge page shows it as a section under the opening.

Milestone 4, systems. A new dream phase `dreamSystems`, after overviews, clusters project pages by label propagation over `depends_on` edges treated as undirected (each page starts with its own label; in page id order, each takes the label most common among its neighbours, ties to the smallest; repeat until nothing changes or twenty rounds), keeps clusters of three or more, and matches each to an existing system page by overlap of members (more than half), so a system keeps its path as its members change. For a new cluster the model names it from its members' openings (`internal/agent/prompts/system_name.txt`); the page is made at `systems/<slug>` of kind topic, and each member gets a stated `part_of` edge to it with evidence of kind dream. A system's overview is written like a checkout's, from its members' overviews, with the fingerprint made of its members' fingerprints. A member that leaves the cluster loses its `part_of` edge; a system with no members left is marked dormant, not deleted. Tests: clustering on an invented graph of two groups joined by one edge gives two systems; a member moving keeps the system's path.

Milestone 5, the survey. `internal/agent/survey.go`: given a question and a scope (a system page, a checkout page, or the whole `systems` root), gather the overviews in scope (a system's members, or every system), and for each run one headless ask (the same machinery as a subagent, a run titled "Survey: <question>, <page>") with the question, that overview and permission to look up the page's code and documents through the knowledge and memory tools, at most six at once and forty in all; then one call that combines the partial answers into a report with sections (findings, strengths, weaknesses, what to do, in the order the question asks for) citing pages and files. The agent gets a `survey` tool (question, scope), which returns the report and offers to keep it as an artifact; GraphQL `SurveyAgentMemory(question, scopePath)` returns the report and the runs; `teanode agent survey "<question>" [--scope <path>]` prints it. Recall: a page with an overview carries its first section in the prompt when chosen, and system pages are weighted like folders are not, so the index carries them. The survey is priced and counted as runs of a new kind, `survey`.

Milestone 6: `docs/subsystems/memory.md` gains the map, overviews, systems and the survey; `docs/reference/command-line.md` the commands; `docs/evaluation/end-to-end-tasks.md` a task; deploy, restart the computer daemon so it reads build files, let one dream run, and ask the question on the development server.

## Concrete Steps

From the repository root. Database tests need Docker.

    go test -mod=vendor ./internal/computer/ -run 'Dependenc|Profile'
    go test -mod=vendor ./internal/agent/ -run 'Repository|Overview|System|Survey'
    go test -mod=vendor ./internal/db/ ./internal/api/v1api/apigraph/ ./internal/client/ ./internal/cmd/...
    cd web && npx tsc --noEmit -p .
    set -o pipefail; make lint-ci

After a deploy, restart the computer daemon on the machine that reads the folder so the new reader runs, then:

    teanode agent knowledge sync <source>
    teanode agent memory get projects/<checkout>
    teanode agent dream now
    teanode agent memory index systems
    teanode agent survey "what are the weak points of the system these checkouts make up?"

## Validation and Acceptance

Milestone 1 is accepted when, after a scan, `teanode agent memory get projects/<checkout>` lists "depends on" links to the checkouts its build files name, and removing the requirement and scanning again removes the link. Milestone 2 when the page shows an activity fact whose numbers match `git log --since` in the checkout. Milestone 3 when a dream writes overviews for checkout pages, a second dream with nothing changed writes none, and a new commit makes the next dream rewrite that one. Milestone 4 when `teanode agent memory index systems` lists systems whose members are the groups a person would draw, and each system's overview reads as its members' combined. Milestone 5 when the survey of the question this plan began with returns a report that names parts of the system, cites overviews and files, and costs about one call per overview plus one.

## Idempotence and Recovery

The reader and the edges are rebuilt from the build files at every scan and the edges a checkout wrote are replaced, so a wrong edge goes away with its cause. The migration adds columns with defaults; its reverse drops them and the overviews are written again after it is applied again. Clearing `overview_inputs` for every page (`RewriteAgentOverview` on each) makes the dreams rewrite them all, at a cost of one call each. System pages are ordinary pages; forgetting one removes it and the next dream makes it again from the clusters.

## Artifacts and Notes

An invented jhbuild moduleset, as the tests use:

    <moduleset>
      <repository type="git" name="example" href="https://git.example.com/"/>
      <cmake id="examplelibcpp">
        <branch repo="example" module="core/example-lib.git"/>
      </cmake>
      <cmake id="exampleappcpp">
        <branch repo="example" module="apps/example-app.git"/>
        <dependencies><dep package="examplelibcpp"/></dependencies>
      </cmake>
    </moduleset>

It yields modules `examplelibcpp` (repository `example-lib`) and `exampleappcpp` (repository `example-app`, depends on `examplelibcpp`), and so the edge `projects/example-app depends_on projects/example-lib`.

## Interfaces and Dependencies

In `internal/computer/scan.go`:

    type RepositoryDependency struct { Name, Ecosystem, File string }
    type RepositoryModule struct { Name, Repository string; Dependencies []string; File string }
    // RepositoryProfile gains Dependencies []RepositoryDependency and Modules []RepositoryModule

In `internal/models/graph.go`: the relation `depends_on`. On `AgentNode`: `Overview string`, `OverviewWrittenAt *time.Time`, `OverviewInputs string`.

GraphQL: `AgentNode.overview`, `AgentNode.overviewWrittenAt`, `RewriteAgentOverview(path: String!): Boolean`, `SurveyAgentMemory(question: String!, scopePath: String): AgentSurveyView` with `report` and `runIds`.

No new libraries; XML and TOML are read with the standard library and the TOML reader the device already uses for `pyproject.toml` names, or line-based reading where that suffices.
