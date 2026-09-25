---
name: claude-code
description: The person's conversations with Claude Code, cut down to what was said, and the notes it keeps in memory, read from its folder on their computer with jq.
requires: [jq]

settings:
  - name: path
    description: Claude Code's folder on that computer
    type: path
    default: "~/.claude"
  - name: exclude
    description: a regular expression; sessions whose working directory matches it, and memory files whose path does, are left unread
    type: string
    default: ""

containers:
  - files: {in: "{{settings.path}}", match: "CLAUDE.md"}
    name: "{{item.path}}"
    fields: {path: "{{item.path}}", absolute: "{{item.absolute}}", modifiedAt: "{{item.modifiedAt}}"}
  - files: {in: "{{settings.path}}", match: "projects/*/memory/*.md"}
    name: "{{item.path}}"
    fields: {path: "{{item.path}}", absolute: "{{item.absolute}}", modifiedAt: "{{item.modifiedAt}}"}
  # Only the files directly in a project's folder are conversations; a
  # session's own folder holds its subagents, which are left out.
  - files: {in: "{{settings.path}}", match: "projects/*/*.jsonl"}
    name: "{{item.path}}"
    fields: {path: "{{item.path}}", absolute: "{{item.absolute}}", modifiedAt: "{{item.modifiedAt}}"}

records:
  # A file is read again only when it changed since the last pass: a
  # session can be hundreds of megabytes, nearly all of it tool output.
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
        # What a part of a message says: the typed string, or the text
        # parts of a list. Tool calls and results, thinking and images
        # have no text and fall away.
        def said: if type == "string" then . elif type == "array" then [.[] | select(type == "object" and .type == "text") | .text] | join("\n\n") else "" end;
        def trimmed: gsub("^\\s+|\\s+$"; "");
        # What the person typed, without what the tool put into their turn.
        def spoken:
          reduce ("system-reminder", "command-name", "command-message", "command-args", "local-command-stdout", "local-command-stderr", "local-command-caveat", "task-notification", "user-prompt-submit-hook", "bash-input", "bash-stdout", "bash-stderr") as $tag
            (.; gsub("<" + $tag + "(\\s[^>]*)?>[\\s\\S]*?(</" + $tag + ">|\\z)"; ""))
          | trimmed
          | if test("^(Caveat: The messages below were generated|\\[Request interrupted)") then "" else . end;
        def excluded: $exclude != "()" and test($exclude);
        if ($file | endswith(".md")) then
          [inputs] | join("\n") as $text
          | select(($text | test("\\S")) and (($file | excluded) | not))
          | {id: $file, kind: "page", title: ("Claude Code memory: " + $file), text: $text}
        else
          [inputs | fromjson? | select(type == "object")
            | if .type == "custom-title" then {title: .customTitle}
              elif .type == "pr-link" then {pullRequest: .prUrl}
              elif (.type == "user" or .type == "assistant") and (.isSidechain | not) and (.isMeta | not) and (.isCompactSummary | not) then
                {id: (.uuid // "line-\(input_line_number)"), at: .timestamp, directory: .cwd, branch: .gitBranch,
                 author: (if .type == "user" then "@you" else "Claude Code" end),
                 text: (if .type == "user" then .message.content | said | spoken else .message.content | said | trimmed end)}
              else empty end] as $items
          | ([$items[] | .directory // empty] | first // "") as $directory
          | select(($directory | excluded) | not)
          | ([$items[] | .title // empty] | last) as $title
          | ([$items[] | .branch // empty | select(. != "HEAD")] | last // "") as $branch
          | ([$items[] | .pullRequest // empty] | unique | join(" ")) as $pullRequests
          | $items[] | select(.author and (.text | test("\\S")))
          | {id, kind: "chat", at, author, text,
             channel: ($title // ("Claude Code in " + $directory)),
             directory: $directory, branch: $branch, pullRequests: $pullRequests}
        end
      - "{{container.absolute}}"
    parse: jsonl
    since: {first: "2000-01-01T00:00:00Z", unchangedWhen: "{{container.modifiedAt}} <= {{pass.since}}"}
    record:
      id: "{{item.id}}"
      kind: "{{item.kind}}"
      title: "{{item.title}}"
      channel: "{{item.channel}}"
      at: "{{item.at}}"
      modifiedAt: "{{container.modifiedAt}}"
      author: "{{item.author}}"
      text: "{{item.text}}"
    metadata:
      assistant: Claude Code
      directory: "{{item.directory}}"
      branch: "{{item.branch}}"
      pullRequests: "{{item.pullRequests}}"
---

# Claude Code

Reads what the person said to Claude Code and what it answered, from the folder it keeps on their computer, with `jq`. Only the conversation is kept: the person's typed messages and the assistant's visible replies. Tool calls and their output, thinking, images, hook and system messages, what the tool puts into the person's turn, subagents and the tool's own compaction summaries are left out; they are most of a session's size and little of what was said.

A conversation is read the way a chat is: cut at a half hour of silence or at a size, so a session that runs for days sends only its newest part on each pass. It is named by the session's title, and carries its working directory, branch and the pull requests it opened. The person's own turns are written by `@you`, which TeaNode files under the person's own name.

`CLAUDE.md` and each project's `memory/` notes are read as pages.

`exclude` is a regular expression over each session's working directory and each memory file's path (`/scratch/|/private/`).

## Document identifiers

A file is its path under the folder. A memory page is its file; a conversation's part is its file and its first message.

## Checked against the tool

Read against Claude Code 2's session files: one JSON object a line, of type `user`, `assistant`, `custom-title`, `pr-link` and others, with `isSidechain`, `isMeta` and `isCompactSummary` marking what is not the conversation.
