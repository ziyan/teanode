---
name: confluence
description: A Confluence site's pages and blog posts, read with the confluence command line tool, each page's text as markdown.
requires: [confluence]

settings:
  - name: spaces
    description: the spaces read, by key; empty reads every space the account can see
    type: array
    items: {type: string, pattern: "^[A-Za-z0-9~][A-Za-z0-9_-]*$"}
    default: []
  - name: profile
    description: the confluence tool's configuration profile, for a person signed in to more than one site; empty for its default
    type: string
    pattern: "^([A-Za-z0-9_-]+)?$"
    default: ""

containers:
  # The tool prints "Available spaces:" and then one "KEY - Name" line each,
  # and never more than 500 of them: a site with that many may have more,
  # and a listing that may be short fails rather than dropping spaces.
  - command: [confluence, --profile, "{{settings.profile}}", spaces]
    parse: {lines: {pattern: "^(?P<key>[^ ]+) - (?P<name>.+)$"}}
    paging: {limit: {size: 500}}
    skip: "!({{settings.spaces | empty}} || {{item.key}} in {{settings.spaces}})"
    # Every space in one file, the name the export this replaces used, so a
    # source switched to this type keeps what it has read.
    name: confluence-pages.jsonl
    fields: {space: "{{item.key}}", spaceName: "{{item.name}}"}

records:
  # The tool cannot page, and the site answers at most 250 however many are
  # asked for. So a search is kept to a window of time small enough to come
  # back short of that, and a window that comes back full fails the pass
  # rather than passing for complete. After the first pass each search is
  # only what changed since.
  - each: [page, blogpost]
    command: [confluence, --profile, "{{settings.profile}}", search, --cql, "space = \"{{container.space}}\" and type = {{each}} and lastmodified >= \"{{pass.windowStart | date}}\" and lastmodified < \"{{pass.windowEnd | date}}\"", --limit, "250"]
    parse: {lines: {pattern: "^\\d+\\. (?P<title>.+) \\(ID: (?P<id>\\d+)\\)$"}}
    paging: {limit: {size: 250}}
    since: {first: "2000-01-01", window: 30d}
    record:
      id: "confluence:{{each}}:{{item.id}}"
      kind: page
      title: "{{item.title}}"
      channel: "{{container.spaceName}}"
      private: true
      # A page is found again only in the window it was changed in, so the
      # window is its version: its text is fetched again after an edit.
      version: "{{pass.windowEnd}}"
    detail:
      command: [confluence, --profile, "{{settings.profile}}", read, "{{item.id}}", --format, markdown]
      parse: text
      text: "{{detail.text}}"
---

# Confluence

Reads the pages and blog posts of the spaces an account can see, through the `confluence` command line tool, which holds the person's credentials on their computer. A page is a record, its text the page as markdown.

The tool answers a search with at most a given number of results and no way to ask for the next ones, which is the one thing the runner cannot make safe by itself: a search that came back full might have been cut. So the first pass reads each space ninety days of edits at a time, oldest first, and a pass whose window still comes back full fails rather than filing part of it. After that, a pass searches only what was edited since the last one, and reads each of those pages whole.

A page that is deleted is not seen by a search for what changed, so it stays until a person removes it or the source is read again from the start.

## Document identifiers

Every space is the one file `confluence-pages.jsonl`, and a page is `confluence:page:<id>` or `confluence:blogpost:<id>` within it: the names the export this replaces used.

## Limits

The tool lists at most 500 spaces and a search answers at most 250 results with no way to ask for more, so a site with more than 500 spaces, or a space where more than 250 pages changed in one month (a bulk import), cannot be read completely with it. Such a site makes the pass fail, saying so, and nothing already read is deleted; read it by naming its spaces in `spaces`, or from an export.

## Checked against the tool

The `spaces` and `search` output lines, `--cql`, `--limit` and `read --format markdown` were read from the tool's 1.x release. Not yet checked: whether the tool reports a page's last edit in any output (which would let a page be read again only when it changed), and whether it has a way to list deleted pages. Comments are not read; the export this replaces had them.
