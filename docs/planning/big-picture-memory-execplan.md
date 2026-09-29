# Big-picture memory: overviews written bottom-up, themes found by clustering, reflections over them, and answers drawn from them

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds.

## Purpose / Big Picture

The agent's memory is a graph of pages, each holding one-sentence facts, filled in by the nightly dream from what it reads. It answers "what did Alice say about the release" well, because some fact or passage is close to the question. It cannot answer "looking at the product I work on and every repository behind it, what are its strengths and weaknesses, and how should it improve", because no page and no passage is about the whole. Recall for such a question returns a handful of scattered details: a note about boot security, a timing test on one board, a package list from years ago. Nothing says what the parts are and how they fit together.

Research on the same problem (question answering over a whole corpus, sometimes called global sensemaking) converges on one pattern. Build a structure over the corpus bottom-up, where each level summarizes the level below: summaries of clusters of related things, and summaries of those summaries. Then answer a question about the whole by asking each summary for its part of the answer in parallel and combining the parts ("map-reduce"). For code in particular, a map of the build-time dependencies between components, read from the build files by a program rather than guessed by a model, is what makes the summaries accurate.

After this plan:

- Every page can carry an overview: several short sections on what the thing is, its parts, how it relates to what it is linked to, what has been happening, and what stands out. The dream writes overviews bottom-up: a page's children and linked pages first, then the page from their overviews and its own facts, and rewrites one only when what it was written from changes. This applies to every kind of page, a person, a project, an organization, a topic, and not only to code.
- Code gets the structure a program can read exactly. Every checkout, and every component inside a checkout (a subdirectory with its own build file, or a module a build set names), is a page, and their build-time dependencies are `depends_on` links, read from build files with no model involved. A monorepo with forty libraries is forty component pages under one checkout page, not one page.
- The whole graph is clustered into themes: groups of pages more linked to one another than to the rest, found by a program over every stated link, then clustered again one level up. Each theme is a page with an overview written from its members' overviews, so there is a structure from single pages to a handful of top-level themes.
- The dream reflects: from time to time it reads a theme's overview and the recent facts under it and writes a few higher-level observations (a pattern that repeats, a tension between two things, a trend, a risk), each citing the pages and facts it rests on. Reflections are facts of their own kind on the theme's page, found by recall like any fact.
- A question about the whole is answered by a survey: one model call per relevant overview, several at once, each free to look up facts, documents and code, then one call that combines them into a report with citations. The agent's `survey` tool, `teanode agent survey` and the `SurveyAgentMemory` operation all run it. Recall carries overviews and reflections for broad questions.

To see it working: on the development server, `teanode agent memory get projects/<a checkout>` shows its components, "depends on" links and an overview; `teanode agent memory index themes` lists the themes; `teanode agent memory get themes/<one>` shows its overview and reflections; `teanode agent survey "what are the strengths and weaknesses of the product these repositories make up, and how should it improve?"` returns a report that cites overviews, facts and files.

## Progress

- [x] (2026-09-28) Surveyed the checkout profile, the graph model, the dream, recall and the subagent tool; wrote this plan.
- [x] (2026-09-28) Milestone 1: dependencies read from build files on the device (`internal/computer/scan_dependencies.go`: go.mod, package.json, pyproject.toml, setup.py, setup.cfg, Cargo.toml, the top CMakeLists.txt, and jhbuild modulesets with their includes), components inside a checkout (`internal/computer/scan_components.go`), and `depends_on` links between checkout and component pages (`internal/agent/ingest_dependencies.go`, `internal/agent/ingest_components.go`), with the dependencies that are not checkouts counted in one keyed fact.
- [x] (2026-09-28) Milestone 2: activity per checkout as one keyed fact, "Activity: ...", rewritten at every pass (`internal/agent/ingest_activity.go`): commits in the last 90 and 365 days and authors in the last 365 counted on the device, open issues, issues opened and merge requests merged in the last 90 days from code-host posts.
- [x] (2026-09-28) Milestone 3: overviews for every kind of page, written bottom-up by the dream (`internal/agent/dream_overview.go`, `dream_overview_files.go`, `prompts/overview.txt`; migration 0118; `internal/db/database_overview.go`); shown in the Knowledge page, `teanode agent memory get` and `memory overview [--rewrite]`, the memory tool's `get`, and counted in the dream log.
- [x] (2026-09-28) Milestone 4: themes (`internal/agent/theme_cluster.go`, the clustering, pure; `internal/agent/dream_themes.go`, the phase; `prompts/theme_name.txt`; `internal/db/database_theme.go`; migration 0119): the graph clustered in two levels after the overviews, theme pages at `themes/<slug>` and `themes/<theme of themes>/<slug>` linked to their members with `about`, kept by member overlap, named by the model at most twelve a night, dormant when their group goes; theme overviews written from their members' overviews by the overview phase on the following nights.
- [x] (2026-09-28) Milestone 5: reflections (`internal/agent/dream_reflect.go`, `prompts/reflect.txt`): fact kind `reflection`, up to three themes a night whose overview is newer than their last reflection, at most five observations each citing two or more pages or facts the prompt showed, older ones superseded; the top-level themes together onto `self/reflections` once a week. Shown apart from the facts in `memory get`, the memory tool's `get` and the Knowledge page; counted in the dream log (CLI and web).
- [x] (2026-09-28) Milestone 6: the survey (`internal/agent/survey.go`, `prompts/survey_part.txt`, `prompts/survey_report.txt`): the pages in scope resolved from the structure, one read-only run each with the memory and knowledge lookups, at most six at once and forty in all, then one run combining the parts into a report ending with what it covered and what failed; runs of kind `survey`. The agent's `survey` tool (`tools_survey.go`), the `SurveyAgentMemory` query, the client document and `teanode agent survey "<question>" [--scope <path>] [--json]`. Recall carries a page's first overview section and a theme's standing reflections, and the prompt's index opens with the themes directly under `themes`.
- [x] (2026-09-28) Review fixes: overview fingerprint and order, activity ranges, older daemons, shared directory names, CMake variables, pass-wide post counts, the survey tool, dormant and divided themes, prompts; see the Decision Log entries marked review.
- [ ] Milestone 7: docs, deploy, and the question this plan began with, asked on the development server.

## Surprises & Discoveries

- Observation: the device already builds a profile of every checkout it finds (`RepositoryProfile` in `internal/computer/scan.go`) and sends it to the server once a scan pass ends. Build files are read only for a name (`manifestName` in `internal/computer/scan_repository.go`: the `module` line of `go.mod`, `name` of `package.json`, the first `name` in `pyproject.toml` or `Cargo.toml`). No dependency is read, and nothing reads CMake or jhbuild files.
- Observation: there is no `depends_on` relation (`AgentEdgeRelations` in `internal/models/graph.go`: part_of, works_on, member_of, knows, owns, uses, located_in, related_to, decided_in, about), and no clustering code anywhere.
- Observation: documents from a GitLab source carry `project`, `kind`, `state`, `number`, `author` and `assignees` in their metadata, so issues and merge requests can be counted per repository without a model.
- Observation: a page's opening (`AgentNode.Summary`) is one paragraph, rewritten by `dreamConsolidate` from the page's facts. It is too short to hold an architecture, and it is about what the page is, not how the thing works.
- Observation: the `subagent` tool (`internal/agent/tools_subagent.go`) runs one sub-run per call, depth 1, 20 rounds, 10 minutes. Nothing runs several at once except a model issuing several calls in one round.
- Observation: the commit documents a folder source sends are not a checkout's history. A pass offers a budget of commits shared out among the checkouts (`shareOfCommits` in `internal/computer/scan_history.go`), and a checkout that is somebody else's gets none. Commit counts from them would measure how much was read, not how much happened.
- Observation: the GitLab source type in this repository (`internal/sources/testdata/registry/gitlab-glab.md`) files issues and merge requests as documents of kind `post` with no project, kind or state in their metadata. What they are is in the identifier, `<group>/<project>#<iid>` for an issue and `<group>/<project>!<iid>` for a merge request, prefixed by the container file's name (`<group>/<project>.jsonl#`). `at` is the creation time (the document's `happened_at`) and `modifiedAt` the last update; there is no merge time. The GitHub type does carry `kind` ("issue", "pull request") and `state` in metadata.
- Observation: `PutAgentEdge` upserts on (from, to, relation) and overwrites the evidence and the note, so a program writing a link the person already drew would take their link over, and later delete it. A writer that owns links has to read before it writes.
- Observation: deleting a page is the person's alone (`DeleteAgentNode`'s contract); the nightly run marks pages dormant. A component that leaves its checkout is therefore made dormant, and comes back when it returns.
- Observation: the device and the server need no protocol change for any of this. The new profile fields are optional JSON; a server that predates them ignores them, and a program that predates them sends none, which the server reads as nothing known (no links, no dependency fact, no commit counts).

- Observation: `PutAgentNode` saves the whole row from the caller's copy (`tx.Save`), and most callers build that copy from what a source says. An overview column in the row struct would be blanked by every pass over a checkout. The columns are read-only in GORM (`->`) and written only by `SetAgentNodeOverview`.
- Observation: nothing records which source and directory a checkout page came from except its keyed "checkout" line ("The checkout is at <where> on <computer>."), and a component's page only its "component" line ("A part of <checkout>, in <directory>, built by <file>."). The overview phase reads these back to find key files; the writers now go through `checkoutLine` and `componentLine` beside the readers `checkoutLocationOf` and `componentLocationOf`.
- Observation: the knowledge index reads a file by `GetAgentDocumentByExternal(sourceId, <path relative to the source>)` and `indexed.Read`; a checkout's files are at `<checkout directory relative to the source>/<path in the checkout>`, as `describeCheckout` already reads the readme.

- Observation: plain label propagation with deterministic ties joins two groups that share one link. The first page past the bridge sees one link to each neighbour's label, takes the smallest, which is the other group's, and every page after it follows. A modularity penalty on each label (LPAm) breaks those ties towards the lighter label.
- Observation: the penalized propagation alone settles on pairs inside a sparse group (a chain, a ring), since no single page gains by leaving its pair. Taking the groups as pages and propagating again (the Louvain method's second phase) merges them.
- Observation: a bridge heavier than the links inside the groups it joins (a `depends_on` between two groups held together by `related_to`) can still carry the pages at its ends into the other group, and local moves never undo it. Accepted: in a graph of code, the links inside a group are build dependencies too.
- Observation: consolidation lists a page with an opening and no live facts and blanks its opening, which is every theme the night before it has a reflection; and once it had reflections it would rewrite the opening from them. Themes are left out of consolidation.
- Observation: a theme's `about` links would have made every member's overview due, since a page's links (with the opening at the other end) are part of its fingerprint, and would have shown members "is the subject of <theme>" in their prompts. Links from theme pages are left out of both, on the member's side only.
- Observation: the request bodies a test's model stub reads are JSON with `<` escaped as `\u003c`, so a stub looking for a prompt's block tags has to look for the escaped form.

- Observation: a mutation runs inside one database transaction for the whole request (`graphView`), and so does every operation a tool sends through `agentOperations.Execute`; the upgrade was moved to the background for exactly this (`idle_in_transaction_session_timeout`). Queries do not: `wrapQueryTransactions` opens one per resolver, and `RecallAgentMemory` was already left out of it to read in phases of its own around its model work. The HTTP server sets no write timeout, and the client's per-request timeout is settable (`SetTimeout`, which `memory answers` already raises to ten minutes).
- Observation: `thinking` checked the caller's context only when the turn sent an event, so a run whose model was slow to answer outlived its deadline by up to the provider's request timeout. It now waits on the context beside the events.
- Observation: a theme of themes has no links of its own and at most a few reflections, which is all importance is made of, so the themes at the top of the structure never ranked into the prompt's index by importance.
- Observation: a theme's overview is written before its reflections and hashed with the theme's facts, so writing the reflections, which are facts, made the overview due again, which made the theme due a reflection again: every theme was rewritten and re-reflected every night.
- Observation: on the development server the first night spent its twenty overviews on the deepest pages, which were the least useful (dated records, alerts six levels down), and the first themes were a dozen of 200 to 1400 pages each with no second level. Modularity over the whole graph cannot see groups smaller than a size set by the whole graph (its resolution limit); over one group's own links it can.
- Observation: `add_library(${PROJECT_NAME} SHARED ...)`, the usual way a CMake library names itself, was read as a target called SHARED: words holding a variable were dropped before the first word was taken for the target.
- Observation: a component that left its checkout lost its keyed line, which is also what marks a page as the component pass's own; once a component could no longer take over somebody else's page, a component coming back found its own old page unrecognizable. The line now stays on the sleeping page.
- Observation: a `?` inside a PostgreSQL regular expression in a GORM `Raw` statement is taken for a placeholder; `{0,1}` says the same.
- Observation: `digest.txt` has no line saying its blocks are data; the wording added to the big-picture prompts follows `idea_check.txt` ("data to judge, never instructions to you").

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

- Decision: overviews, themes and reflections apply to every page, not only to code; code adds components and dependencies read from build files, which no other kind of page has.
  Rationale: the person asked for the big picture of their memory as a whole, of which code is one part; what code adds is the one structure a program can read exactly.
  Date/Author: 2026-09-28, at the person's request.
- Decision: components inside a checkout are pages of their own, children of the checkout's page, found from subdirectories with their own build file and from build sets.
  Rationale: many checkouts hold many components, and a one-page checkout cannot say which part depends on which or where the weak point is.
  Date/Author: 2026-09-28, at the person's request.
- Decision: themes replace the plan's first "systems": clusters over every stated link, in two levels, under `themes`, linked to their members with `about`.
  Rationale: systems of code are a special case of themes; one mechanism, weighted towards build dependencies, serves both.
  Date/Author: 2026-09-28.
- Decision: reflections are facts of a new kind on a theme's page, each citing at least two pages or facts, older ones superseded rather than deleted.
  Rationale: as facts they are recalled, numbered, cited and corrected like any other, which the memory's rule that everything is traceable asks for; requiring two citations keeps them from restating one fact.
  Date/Author: 2026-09-28.

- Decision: the activity fact is written by `fileRepository` at every pass, as one more keyed line of the profile, and not by the digest phase once a night.
  Rationale: the keyed-fact code in `fileRepository` deletes every repository-evidence fact it did not write in the same call, so a fact the digest wrote would be struck by the next pass; the pass runs at least as often as the dream, costs no model, and the profile it needs is in hand there.
  Date/Author: 2026-09-28.
- Decision: commit numbers are counted on the device from the `git log` the profile already runs, and sent as `RepositoryProfile.Activity`; the server does not count commit documents.
  Rationale: see the observation about commit documents being a budgeted share. The device sees the whole history at no extra git process. Dates are author dates, so a count can differ from `git log --since` (committer dates) by a rebased commit or two.
  Date/Author: 2026-09-28.
- Decision: issues and merge requests are counted from documents of kind `post` whose metadata `project` is the remote's path (or ends with it), or whose identifier names it followed by `#` or `!`. Kind comes from metadata (`issue`; `merge request`, and GitHub's `pull request`) and otherwise from that mark; state only from metadata (`opened`/`open`, `merged`); the merge time from metadata `mergedAt` or `merged_at`, and otherwise the document's modification time.
  Rationale: this reads both the metadata the plan observed on the development server and the identifiers of the type in this repository, with no text parsing.
  Date/Author: 2026-09-28.
- Decision: a `depends_on` link belongs to the build files only when every piece of its evidence is theirs: kind repository with quote `dependency:<name>` (owned by the page the link starts at) or `moduleset:<checkout page>:<file>` (owned by the checkout holding the moduleset). A pass replaces only its own checkouts' evidence (a checkout, or a component under it), deletes a link left with none, and never touches a link with any other evidence.
  Rationale: a link the person drew stays theirs even when a build file agrees, and a link two checkouts state lasts until neither does.
  Date/Author: 2026-09-28.
- Decision: links are written after every profile of the page is filed (`linkCheckoutDependencies`, called from `fileComputerPage`), resolved against all the page's profiles at once (`checkoutIndex`). Resolution order: a checkout's own module name; a jhbuild module that is a component; a jhbuild module's repository; a remote, whole (`host/path`); a component's name; a remote's last segment; a directory name; a project page of that name another source filed.
  Rationale: all profiles arrive together on a pass's last page, and a link needs both pages, so the one depended on may be filed after the one that depends on it.
  Date/Author: 2026-09-28.
- Decision: a component is a subdirectory, at most four deep and not under test, example, doc or bench directories, with its own go.mod, Cargo.toml (with a package), package.json (not a workspace root), pyproject.toml, setup.py, or a CMakeLists.txt that declares a project or a target; and, when a moduleset in the checkout builds two or more modules from the checkout itself, each of those modules. A CMake directory that only adds others with `add_subdirectory` groups them and is not a component. A component's CMake dependencies are the other components whose targets its targets link (read across every CMakeLists.txt of the checkout, aliases included) and the packages it finds. At most 200 components and 100 dependencies each.
  Rationale: the person's request, narrowed where a literal reading would make every test directory and every grouping directory a component nobody would name.
  Date/Author: 2026-09-28.
- Decision: a component's page is `<checkout page>/<slug of its directory>` (or of its name, for a module), kind project, with a keyed fact saying where it is and what builds it; no `part_of` edge, since the tree already says it. Links between a checkout and its own components are not written.
  Rationale: `PutAgentNode` sets the parent from the path, and `part_of` would say the same thing twice; a root build file naming its own workspace's packages is how a monorepo is assembled, not a dependency.
  Date/Author: 2026-09-28.
- Decision: the nightly walk (`dreamAssociate`) is neither offered `depends_on` nor allowed to propose it.
  Rationale: a guessed build dependency among exact ones is a wrong link that looks right, and clustering will weight these links.
  Date/Author: 2026-09-28.
- Decision: the fingerprint (`overview_inputs`) is a SHA-256 worked out in SQL (`overviewInputsExpression` in `internal/db/database_overview.go`) over the opening, each live fact's number and modification time, each live child's id and `overview_written_at`, and each link's direction, relation, other end, note and the other end's opening. It is hashed for every eligible page in the one listing query and again for one page right before its prompt is read, and that second hash is what the overview is stored with.
  Rationale: every input is a column, so a page nobody touched costs no read of its facts and no model call. Weights and use times are left out because the quiet half moves them every night. Checkout and component pages need no extra inputs: their HEAD, build-file and activity lines are keyed facts rewritten in place, so their modification times already move with them. Hashing again just before the read means a child written earlier the same night is part of its parent's hash, and anything that changes during the call leaves the page due.
  Date/Author: 2026-09-28.
- Decision: the order is depth (slashes in the path) descending, then importance, then path; one depth is written at a time, with `RewriteConcurrency` pages of it at once. Overviews run after consolidate, organize and split, with at most half the remaining night (`dreamBudget.overviewUntil`) and within the night's token share like every phase.
  Rationale: children before parents without a graph walk; pages of one depth are never each other's children.
  Date/Author: 2026-09-28.
- Decision: evidence is a JSON column `overview_evidence` of the same `Evidence` objects facts carry: kind memory with the page id and path for a cited page, kind document with the document id and path for a cited file. Only pages and files the prompt showed are kept, at most 24.
  Rationale: the simplest store that the API and the dashboard can already read, and filtering to what was shown keeps a made-up path from reading as a citation.
  Date/Author: 2026-09-28.
- Decision: the model answers with `sections` (heading and text, headings in the knowledge language), `citedPages` and `citedFiles`; the code renders `## heading` sections, drops empty ones, and bounds each (2500 characters, six sections, 10000 in all). An unreadable answer, or one with no text, leaves the page as it was and still due.
  Rationale: headings in the person's language without the code knowing five translations; nothing half-written is stored.
  Date/Author: 2026-09-28.
- Decision: an overview write is not a page revision, and `overview_inputs` is not in the API (`json:"-"`).
  Rationale: an overview is derived from inputs that each have their own history; a revision per nightly rewrite would bury the person's own changes. The hash means nothing to a reader.
  Date/Author: 2026-09-28.
- Decision: only a page read on its own asks for the overview (`AgentGraphPage`, the memory tool's `get`); index and search listings do not.
  Rationale: an index of four hundred pages has no use for four hundred overviews of up to 10000 characters each.
  Date/Author: 2026-09-28.
- Decision: migration number 0118 (main's latest was 0117 when this was written).
  Date/Author: 2026-09-28.
- Decision: themes are found by label propagation that maximizes modularity (Barber and Clark's LPAm) with the groups propagated again as pages until nothing merges (the Louvain method's second phase), in page id order, keeping a page's label on a tie and otherwise taking the smallest. Weights: `depends_on` and `part_of` 2, every other stated relation 1, undirected, several links between two pages added. Left out: the person's page, `time` and everything under it, folders, periods, and `themes` and everything under it. Proposed links are left out.
  Rationale: see the observations about plain propagation joining groups through one link and the penalized one stalling at pairs. Both steps are a few dozen lines, deterministic, and need no library.
  Date/Author: 2026-09-28.
- Decision: a hub is a page linked to at least 8 others and to at least four times as many as the median page, at most one page in a hundred (at least one), the most linked first. Hubs are left out of propagation and then join the group their links weigh most towards; a page whose only links were to hubs follows its hub.
  Rationale: an organization or a person everyone is linked to is a bridge every group flows across; tested on six chains of five pages all linked to one page, which stay six themes.
  Date/Author: 2026-09-28.
- Decision: when one group holds more than 40% of the linked pages and there are at least 20 of them, that group is propagated again counting only the links between pages under the same root of the tree (`projects`, `people`, `topics`, ...), and the log says so.
  Rationale: a graph linked everywhere otherwise gives one theme of everything; dividing by root is the one grouping the tree already states, and it at least separates the projects that belong together from the people who do.
  Date/Author: 2026-09-28.
- Decision: the second level propagates over the links between the members of different themes only, with no weight for the links inside each theme, so any themes linked at all tend to group (two themes joined by one link are a group of two). A theme of themes holds its themes as pages under it in the tree, with no `about` links; a theme of pages in no group stands at `themes/<slug>`.
  Rationale: the plan's "clustered again the same way over the edges between their members"; with the internal weight counted, the second level would repeat the first level's answer and merge nothing. The tree says what a theme of themes holds without a second kind of link.
  Date/Author: 2026-09-28.
- Decision: a theme of pages keeps its page when a group holds more than half of the members its own links point at (the most shared first, then the shorter path); a theme of themes when a group holds more than half of the themes under it. A kept theme is woken if dormant and moved under the theme of themes it now belongs to (or up to `themes`) with `MoveAgentNode`, unless the target path is taken. A theme's link to a member is its own when its only evidence is kind dream with the phase's quote; only those are replaced, so a link the person drew is left alone and does not count as membership. A theme no group keeps loses its own links and is marked dormant; a theme of themes with none of its themes kept is marked dormant.
  Rationale: a path somebody cited stays put while the members drift; deleting a page is the person's alone.
  Date/Author: 2026-09-28.
- Decision: at most twelve themes are named a night, themes of pages first and largest first, every call within the night's budget; a group left unnamed is found again the next night. The answer is JSON `{name, opening}`; a name with nothing a slug can be made of is refused. A new theme is kind topic at a free path (a number after the slug where the name is taken), its opening the model's.
  Rationale: naming is the phase's only model cost; clustering, matching and linking are free and run even with no budget left.
  Date/Author: 2026-09-28.
- Decision: themes are left out of consolidation (their opening is the name's) and of retirement in the quiet half (whether a theme is live is the theme phase's to say).
  Rationale: see the observation about consolidation blanking a theme's opening.
  Date/Author: 2026-09-28.
- Decision: theme overviews come from their own listing, `ListAgentThemesForOverview`, up to eight a night after the other pages, deepest first so a theme of themes comes after its themes. A theme of pages is eligible when it has `about` links; its members (live, most important first, at most 30) are shown as the prompt's members, with their overviews, as a page's children are. The fingerprint gains, for pages under `themes/` only, each member's id and overview time; the part is NULL for any other page, which `concat_ws` skips, so no existing page's hash changes. Links from pages under `themes/` are left out of a member's fingerprint and prompt.
  Rationale: a theme is written from its members as a parent is from its children; its own listing lets a night with more pages due than it writes still write a few themes, after the pages they are written from.
  Date/Author: 2026-09-28.
- Decision: a reflection is a fact of kind `reflection`, inferred, confidence 0.7. Its evidence is one line of kind dream, `reflection: <pattern|tension|trend|risk|question>`, then one of kind memory per citation, the page's or fact's id with `path` or `path#number` as the quote, at most eight. An observation is kept only with two or more citations of pages or facts its prompt showed (not the theme itself), and a kind from the list. The model may not file one: the memory tool leaves the kind out of its schema and refuses it, and filing a conversation turns it into a plain fact (`AgentFactKind.FromTheNight`, `AgentFactKindsFiled`).
  Rationale: as facts they are recalled, numbered and cited like any other, and recall and the index filter no kinds; the kind of observation fits in the evidence the fact already carries without a column.
  Date/Author: 2026-09-28.
- Decision: a theme is due a reflection when its overview was written after `agent_node.reflected_at` (migration 0119), which is set to the overview's time as listed, whether or not any observation passed. The new reflections supersede the page's previous ones by `FoldAgentFact` into the first new one. An answer that cannot be read leaves the theme due.
  Rationale: comparing with the newest reflection's time would ask a theme with nothing worth saying again every night; marking with the listed overview time leaves a theme rewritten during the call due.
  Date/Author: 2026-09-28.
- Decision: once a week (`self/reflections`' `reflected_at` a week old or unset), the night reflects on the live pages directly under `themes` that have overviews (the themes of themes and the themes in no group), at least two, citing them and their reflections, onto `self/reflections`, made on first use with no opening.
  Rationale: the top of the structure is what the person's own page of observations is about; an opening written before any fact would be blanked by consolidation.
  Date/Author: 2026-09-28.
- Decision: migration number 0119 (the branch's latest was 0118).
  Date/Author: 2026-09-28.

- Decision: the scope of a survey. A theme of pages: its `about` members that have an overview, with the theme's reflections handed to the combining call. A theme of themes: the themes under it that have one. Any other page: itself if it has an overview, and its children that have one. Nothing: every live page directly under `themes` that has an overview (the themes of themes and the themes in no group, both the top of the structure), with `self/reflections`; failing that, every theme with an overview; failing that, the most important pages with overviews. A scope with nothing that has an overview is the scope page alone. At most 40, the most important first.
  Rationale: the plan's rule, with "every level-two theme" read as the top of the structure: a theme of pages in no group stands at `themes/<slug>` beside the themes of themes and would otherwise be left out of a survey of everything.
  Date/Author: 2026-09-28.
- Decision: each page's run is `think` with `lookupTools` (read-only, the memory and knowledge tools), 8 rounds, 5 minutes, on the scan model; it is shown the page's opening, overview, reflections, up to 40 of its facts (the most wanted, laid out by number) and up to 30 of the pages it holds. The combining call is one round with no tools on the research model. At most 6 runs at once; the whole survey 15 minutes, of which the last 3 are kept for combining. A run that errs, answers nothing or runs out of time is a failed page; one that answers `NOTHING RELEVANT` is covered but not handed on. The report ends with lines the code writes, the covered and the failed paths; where the combining call fails, the parts are returned as they are.
  Rationale: the read-only lookup set is what describing a checkout already uses, so nothing a survey does can write; the scan model is what wrote the overviews and is the one priced for bulk; coverage said by code cannot be dropped by a model.
  Date/Author: 2026-09-28.
- Decision: `SurveyAgentMemory` is a query, left out of the per-resolver transaction like `RecallAgentMemory`, reading the person in short phases before and after the work; not a mutation that starts a survey and a query that fetches it.
  Rationale: a mutation holds one transaction open for the whole request, which a quarter of an hour of model calls must not do; the server has no write timeout and the command raises the client's timeout to sixteen minutes, so one long request works. A proxy in front with a shorter timeout would cut it; if that turns up, the survey becomes a job with its report on a run.
  Date/Author: 2026-09-28.
- Decision: the `survey` tool is built by the agent (`tools_survey.go`) like `subagent`, offered from the start of every turn somebody is present for and never in a headless run; the registered catalog, and so `TestTheCatalogStaysShort`, is unchanged.
  Rationale: it starts runs, which only the agent can do, and a tool package could only reach the survey through `agentOperations`, whose one transaction per operation would be held open for the whole survey. Loaded from the start because its guidance is what tells the model to reach for it on a broad question; not headless because the night is given every tool and a survey is minutes of calls nobody asked for.
  Date/Author: 2026-09-28.
- Decision: recall carries, for a chosen page with an overview, its first section cut to 600 characters, dropped before the page itself is passed over for want of room; for a theme (or `self/reflections`), up to 3 of its standing reflections, the ones the words hit first, each cut to 400 characters, in place of the facts the words hit. The prompt's index opens with up to 12 live pages directly under `themes` that have an opening or an overview, a line each within the index's token budget, and then the pages by importance.
  Rationale: a direct inclusion rather than a kind weight in `RecomputeAgentImportance`, since a weight large enough to lift a page with no links over the busiest pages would lift every theme, and the top of the structure is a dozen lines at most.
  Date/Author: 2026-09-28.

- Decision (review, 2026-09-28): reflections are left out of a page's overview fingerprint and out of the facts its overview prompt shows.
  Rationale: see the observation; a reflection is written from the overview, not the other way round.
  Date/Author: 2026-09-28.
- Decision (review; supersedes "the order is depth descending"): a page is ready for its overview when no page directly under it is due; for a theme, when no theme directly under it is due. Ready pages are written most important first, themes before pages, 16 themes and 40 pages a night within the budget, a few at once (none of a night's pages is directly under another). A page is eligible only when its importance is at least the median importance of the live pages with three or more facts (reflections not counted); the rest wait until they rise.
  Rationale: deepest-first wrote the least useful pages first and reached the top of a six-level tree a week later. Direct children only, not all descendants, so a page is not held back by a deep chain it is not written from. A theme does not wait for its members: members churn every night, and a theme waiting on them would never be written; a member with no overview is shown by its opening and five most wanted facts, which the fingerprint then covers (for members not held by a theme under it), and a member rewritten later makes the theme due on a later night.
  Date/Author: 2026-09-28, at review, refined after the first night on the development server.
- Decision (review): the activity line says every count as a range, none, 1-5, 6-20, 21-50, 51-100 or more than 100, and never the exact number, which is kept nowhere.
  Rationale: the line is a fact in the page's fingerprint, and exact counts rewrote the overview of every busy checkout every night.
  Date/Author: 2026-09-28.
- Decision (review): `RepositoryProfile.IsBuildRead`, set by the daemon that reads build files, rather than a reader version number. Without it the server leaves the dependency links a checkout stated, its component pages and its `dependencies-elsewhere` and `activity` lines exactly as an earlier pass wrote them.
  Rationale: the question the server asks is only whether the fields mean anything; a version number would invite comparisons nothing needs yet, and can be added beside the flag if a reader change ever has to be told apart.
  Date/Author: 2026-09-28.
- Decision (review): checkouts of one pass sharing a directory name get distinct pages: the one whose keyed checkout line the existing page carries keeps it, or where none does the first by relative path; each other is `<name>-<the directory above it>`, then `-2`, `-3` on a clash. A name two pages claim (a module, a remote, a component, a directory, a jhbuild module's repository) resolves to nothing and is counted among what is not a checkout here. Resolution order: the module (with and without a Go `/vN`), a jhbuild component, a jhbuild module's repository, a remote whole, a component's whole name, then by the last meaningful word (without `/vN` or an npm scope): a remote's last segment, a component's name, a directory, a project page another source filed.
  Rationale: a remote is the repository itself and a component's name is a build file's choice; a guess between two pages is a wrong link that looks right.
  Date/Author: 2026-09-28.
- Decision (review): CMake `${PROJECT_NAME}` is the nearest `project()` at or above the file's directory and `${CMAKE_PROJECT_NAME}` the top one (or the nearest); any other variable makes a target unreadable (the command is skipped) or a linked name unreadable (the word is skipped). The keywords of `add_library`/`add_executable` and of `target_link_libraries` are never names; `IMPORTED` anywhere skips the target, `ALIAS` points it at the aliased one.
  Date/Author: 2026-09-28.
- Decision (review): issues and merge requests are counted by one grouped statement per pass over every post (`CountAgentProjectPosts`), the project read from metadata or from the identifier, no text read; no expression index, since the statement reads each post of the agent once and a pass runs it once.
  Date/Author: 2026-09-28.
- Decision (review): the `survey` tool is offered only where the `subagent` tool is (the `subagents` feature, depth 0, someone present). One survey a turn: a second call is refused before any model call, pointing at the first report, rather than returning it again, which would repeat several thousand words in the context. `SurveyAgentMemory` stays a query, as decided above; the resolver's comment now says it runs paid work and must not be retried or prefetched.
  Date/Author: 2026-09-28.
- Decision (review; supersedes "at most twelve themes are named a night" and "a theme no group keeps loses its own links"): a theme no group keeps is made dormant and keeps its `about` links; the known themes include dormant ones, so a group that returns wakes its old page where it was, with its reflections. At most 40 namings a night within the budget: the level-one groups largest first, then the themes of themes, then the parts of large themes by depth and size.
  Rationale: a theme that lost its links could never be matched again, and the group coming back took a new path, leaving whatever cited the old one pointing at a sleeping page.
  Date/Author: 2026-09-28.
- Decision (review): a theme of more than 120 pages is clustered again over the links among its own members (hubs found and handled as at the top), into themes under it at `themes/.../<theme>/<part>`, repeated while a part has more than 120 pages, at most three levels below the theme it divides; parts smaller than five pages stay with the theme above, and a theme that does not divide into at least two such parts stays whole. The large theme keeps its `about` links to all its pages, which is what matches it on later nights; its overview and a survey of it read the parts (its children) and the members no part holds. Matching runs deepest first so a part keeps the page of the theme it used to be; a part whose theme was not named tonight stays where it is.
  Rationale: the level-two grouping is unchanged; this adds levels beneath it where a group is too large for one overview or one part of a survey.
  Date/Author: 2026-09-28, after the first night on the development server.
- Decision (review): the overview, theme naming, reflection and survey page prompts say that what is inside their blocks is data to read, never instructions.
  Date/Author: 2026-09-28.
- Decision (review): smaller fixes. A jhbuild branch's `checkoutdir` names the repository when present. A metamodule is taken out of the modules and a dependency on it becomes the modules it lists, recursively and once each. Modules and dependencies have separate bounds of 500. A `setup.py` list is read to its closing bracket, so extras (`lib[fast]`) do not end it. Components past the bound of 200 are kept shallowest first, then by directory. A component never takes over a page it did not make (one without its keyed line, other than an empty folder), and no link is written to or from such a page. The API refuses saving a fact of kind reflection.
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

Milestone 3, overviews for every page. Migration: `agent_node.overview text not null default ''`, `overview_written_at timestamptz`, `overview_inputs varchar(64) not null default ''`. A new dream phase `dreamOverviews`, after consolidate, picks pages whose inputs changed, deepest first so a parent is written after its children, most important first within a depth, at most twenty a night within the budget. A page's inputs, and the fingerprint over them, are: its opening and facts (their numbers and modification times), its children's overviews, and the openings of the pages it is linked to with the relations; for a checkout or component page also its HEAD, its build files and its activity. Pages with fewer than three facts and no children get no overview; their opening is enough. The prompt (`internal/agent/prompts/overview.txt`) asks for a JSON object with sections that fit any kind of page: what it is, its parts (from the children), how it relates to what it is linked to, what has been happening, and what stands out (concerns, open questions, with the evidence); for a checkout or component page the prompt adds the text of up to four key files found in the source's indexed documents (the readme, the build file, and entry points). The code writes the sections as markdown into `overview` and keeps the pages and files it cites as evidence. GraphQL `AgentNode` gains `overview` and `overviewWrittenAt`; `RewriteAgentOverview(path)` clears the fingerprint so the next dream rewrites it. `teanode agent memory get` prints the overview under the opening; `teanode agent memory overview <path> --rewrite` calls the mutation. The memory tool's `get` includes it, and the Knowledge page shows it as a section under the opening.

Milestone 4, themes. A new dream phase `dreamThemes`, after overviews, clusters pages by label propagation (each page starts with its own label; in page id order each takes the label with the greatest total edge weight among its neighbours, ties to the smallest; until nothing changes or twenty rounds) over stated edges of every relation, treated as undirected, weighting `depends_on` and `part_of` 2, others 1, leaving out `self` and the `time` and `people` roots' folder pages, whose links reach everything. Clusters of three or more members become level-one themes; the themes are then clustered again the same way over the edges between their members, and groups of two or more become level-two themes. A theme is matched to an existing theme page by member overlap (more than half), so it keeps its path as members change; a new one is named by the model from its members' openings (`internal/agent/prompts/theme_name.txt`) and made at `themes/<slug>` (level two) or `themes/<level two slug>/<slug>` (level one, under its parent), of kind topic. A theme is linked to each member with a stated `about` edge whose evidence is of kind dream. A theme's overview is written by Milestone 3's phase from its members' overviews, with the same fingerprint rule. A member that leaves loses its edge; an empty theme is marked dormant, not deleted. Tests: two invented groups joined by one edge give two themes; a moved member keeps the theme's path; a second level forms over three themes that link to one another.

Milestone 5, reflections. A new fact kind `reflection`. A dream phase `dreamReflect`, after themes, takes up to three themes a night whose overview changed since their last reflection or that have none, and asks (`internal/agent/prompts/reflect.txt`) for at most five higher-level observations from the theme's overview, its members' overviews and the facts written under it in the last ninety days: a pattern that repeats, a tension between two things, a trend over time, a risk, or a question nobody has answered. Each observation must cite at least two pages or facts it rests on; one that does not is dropped. They are written as facts of kind reflection on the theme's page, with evidence of kind memory naming the cited pages and facts, and older reflections on that page that the new ones replace are superseded rather than deleted. The index and recall treat reflections like facts; the Knowledge page shows them under the overview with their citations. The same phase, once a week, reflects on the level-two themes together and writes to `self/reflections`, the person's own page of observations across their life and work.

Milestone 6, the survey. `internal/agent/survey.go`: given a question and a scope (a theme, a page, or everything), gather the overviews in scope (a theme's members, or every level-one theme under a level-two one, or every level-two theme), and for each run one headless ask (a run titled "Survey: <question>, <page>") with the question, that overview, the theme's reflections, and permission to look up facts, documents and code through the memory and knowledge tools, at most six at once and forty in all; then one call that combines the partial answers into a report whose sections follow the question, citing pages, facts and files. The agent gets a `survey` tool (question, scope), which returns the report and offers to keep it as an artifact; GraphQL `SurveyAgentMemory(question, scopePath)` returns the report and the runs; `teanode agent survey "<question>" [--scope <path>]` prints it. Recall: a chosen page with an overview carries its first section, a theme carries its reflections, and the prompt's index carries the level-two themes. The survey is priced and counted as runs of a new kind, `survey`.

Milestone 7: `docs/subsystems/memory.md` gains components, overviews, themes, reflections and the survey; `docs/reference/command-line.md` the commands; `docs/evaluation/end-to-end-tasks.md` a task; deploy, restart the computer daemon so it reads build files, let dreams run, and ask the question this plan began with on the development server.

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

Milestone 1 is accepted when, after a scan, `teanode agent memory get projects/<checkout>` lists its components as child pages and "depends on" links to the checkouts and components its build files name, and removing a requirement and scanning again removes that link. Milestone 2 when the page shows an activity fact whose numbers match `git log --since` in the checkout. Milestone 3 when a dream writes overviews for pages of several kinds, children before parents, a second dream with nothing changed writes none, and a new fact or commit makes the next dream rewrite that page and then its parent. Milestone 4 when `teanode agent memory index themes` lists themes in two levels whose members are the groups a person would draw, and each theme's overview reads as its members' combined. Milestone 5 when a theme's page shows reflections, each citing at least two pages or facts that say what it claims. Milestone 6 when the survey of the question this plan began with returns a report that names parts of the product, cites overviews, reflections, facts and files, and costs about one call per overview plus one.

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
    type RepositoryActivity struct { CommitCountLast90Days, CommitCountLast365Days, AuthorCountLast365Days int }
    // RepositoryProfile gains Dependencies []RepositoryDependency, Modules []RepositoryModule,
    // Components []RepositoryComponent and Activity *RepositoryActivity

In `internal/computer/scan_components.go`:

    type RepositoryComponent struct { Path, Name, Ecosystem, File string; Dependencies []string }

In `internal/db`: `ListAgentEdgesByRelation(agentId, relation)` and `ListAgentProjectPosts(agentId, projectPath, limit)`.

In `internal/models/graph.go`: the relation `depends_on`. On `AgentNode`: `Overview string`, `OverviewWrittenAt *time.Time`, `OverviewInputs string`.

GraphQL: `AgentNode.overview`, `AgentNode.overviewWrittenAt`, `RewriteAgentOverview(path: String!): Boolean`, `SurveyAgentMemory(question: String!, scopePath: String): AgentSurveyView` with `report` and `runIds`.

No new libraries; XML and TOML are read with the standard library and the TOML reader the device already uses for `pyproject.toml` names, or line-based reading where that suffices.
