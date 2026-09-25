---
name: codex
description: The person's conversations with Codex, cut down to what was said, and the notes it keeps in memory, read from its folder on their computer with jq.
requires: [jq]

settings:
  - name: path
    description: Codex's folder on that computer
    type: path
    default: "~/.codex"
  - name: exclude
    description: a regular expression; sessions whose working directory matches it, and memory files whose path does, are left unread
    type: string
    default: ""

# The names sessions were given, by session: a later line is a rename.
lookups:
  titles: {file: "{{settings.path}}/session_index.jsonl", parse: jsonl, key: "{{item.id}}", value: "{{item.thread_name}}"}

containers:
  - files: {in: "{{settings.path}}", match: "AGENTS.md"}
    name: "{{item.path}}"
    fields: {path: "{{item.path}}", absolute: "{{item.absolute}}", modifiedAt: "{{item.modifiedAt}}"}
  - files: {in: "{{settings.path}}", match: "memories/**/*.md"}
    name: "{{item.path}}"
    fields: {path: "{{item.path}}", absolute: "{{item.absolute}}", modifiedAt: "{{item.modifiedAt}}"}
  - files: {in: "{{settings.path}}", match: "sessions/**/*.jsonl"}
    name: "{{item.path}}"
    fields: {path: "{{item.path}}", absolute: "{{item.absolute}}", modifiedAt: "{{item.modifiedAt}}"}

records:
  # A file is read again only when it changed since the last pass.
  - command:
      - jq
      - --raw-input
      - --null-input
      - --compact-output
      - --arg
      - file
      - "{{container.path}}"
      - --arg
      - exclude
      # In a group, so an empty setting is still a word: () leaves nothing out.
      - "({{settings.exclude}})"
      - |
        def trimmed: gsub("^\\s+|\\s+$"; "");
        # Codex puts its own context into the person's turn as parts of
        # their message: the repository's instructions, the environment,
        # plugins, skills. A part that opens with one of those is not
        # what they said.
        def injected: test("^\\s*(<[a-z_]+[\\s>]|# AGENTS\\.md instructions|## Skills|A skill is a set of local instructions)");
        def said($role): [.[]? | select(type == "object") | .text // empty | select($role != "user" or (injected | not))] | join("\n\n") | trimmed;
        def excluded: $exclude != "()" and test($exclude);
        if ($file | endswith(".md")) then
          [inputs] | join("\n") as $text
          | select(($text | test("\\S")) and (($file | excluded) | not))
          | {id: $file, kind: "page", title: ("Codex memory: " + $file), text: $text}
        else
          [inputs | fromjson? | select(type == "object")
            | if .type == "session_meta" then {meta: .payload}
              elif .type == "response_item" and .payload.type? == "message" and (.payload.role == "user" or .payload.role == "assistant") then
                {id: (.payload.id // "line-\(input_line_number)"), at: .timestamp,
                 author: (if .payload.role == "user" then "@you" else "Codex" end),
                 text: (.payload.role as $role | .payload.content | said($role))}
              else empty end] as $items
          | ([$items[] | .meta // empty] | first // {}) as $meta
          # A session another session started, or one a script ran with
          # codex exec: its prompts are a program's, not the person's.
          | select(($meta.source // "cli" | type) == "string" and $meta.source != "exec" and ($meta.parent_thread_id // "") == "")
          | ($meta.cwd // "") as $directory
          | select(($directory | excluded) | not)
          | [$items[] | select(.author and (.text | test("\\S")))] as $posts
          # A session nobody named is named by the person's first words.
          | ([$posts[] | select(.author == "@you") | .text] | first // "" | gsub("\\s+"; " ")) as $opening
          | (if ($opening | length) > 60 then $opening[0:60] + "…" elif $opening == "" then "Codex in " + $directory else $opening end) as $title
          | $posts[]
          | {id, kind: "chat", at, author, text, channel: $title, session: ($meta.id // ""),
             directory: $directory, branch: ($meta.git.branch // "")}
        end
      - "{{container.absolute}}"
    parse: jsonl
    since: {first: "2000-01-01T00:00:00Z", unchangedWhen: "{{container.modifiedAt}} <= {{pass.since}}"}
    record:
      id: "{{item.id}}"
      kind: "{{item.kind}}"
      title: "{{item.title}}"
      channel: "{{lookup.titles[item.session] | or item.channel}}"
      at: "{{item.at}}"
      modifiedAt: "{{container.modifiedAt}}"
      author: "{{item.author}}"
      text: "{{item.text}}"
    metadata:
      assistant: Codex
      directory: "{{item.directory}}"
      branch: "{{item.branch}}"
---

# Codex

Reads what the person said to Codex and what it answered, from the folder it keeps on their computer, with `jq`. Only the conversation is kept: the person's typed messages and the assistant's visible replies. Reasoning, tool calls and their output, developer instructions and the context Codex puts into the person's turn (the repository's `AGENTS.md`, the environment, skills and plugins) are left out, and so are sessions another session started and sessions a script ran with `codex exec`.

A conversation is read the way a chat is: cut at a half hour of silence or at a size, so a session that runs for days sends only its newest part on each pass. It is named by the session's name in `session_index.jsonl`, or else by the person's first words, and carries its working directory and branch. The person's own turns are written by `@you`, which TeaNode files under the person's own name.

`AGENTS.md` and the Markdown under `memories/` are read as pages. The per-session summaries Codex keeps in `memories_1.sqlite` are not: they are made from the sessions, which are read.

`exclude` is a regular expression over each session's working directory and each memory file's path (`/scratch/|/private/`).

## Document identifiers

A file is its path under the folder. A memory page is its file; a conversation's part is its file and its first message.

## Checked against the tool

Read against Codex 0.156's rollout files, `sessions/YYYY/MM/DD/rollout-*.jsonl`: one object a line with `type` and `payload`, a `session_meta` line first with `cwd`, `git` and `source` (`cli`, `exec`, or an object naming a subagent), and `response_item` messages with `role` `user`, `assistant` or `developer`.
