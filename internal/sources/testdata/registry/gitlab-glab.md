---
name: gitlab-glab
description: A GitLab server's projects - their issues and merge requests - read with the glab command line tool, on whichever computer can reach the server.
requires: [glab]

settings:
  - name: hostname
    description: the GitLab server, as glab knows it; leave empty for glab's default
    type: string
    pattern: "^([A-Za-z0-9.-]+)?$"
    default: ""
  - name: groups
    description: groups whose projects are read; empty reads every project the account is a member of
    type: array
    items: {type: string, pattern: "^[A-Za-z0-9][A-Za-z0-9_./-]*$"}
    default: []

containers:
  # glab's own list commands page by number, which cannot show a list is
  # complete; its api command follows every page itself.
  - when: "{{settings.groups | empty}}"
    command: [glab, api, --hostname, "{{settings.hostname}}", "projects?membership=true&archived=false&per_page=100", --paginate, --output, ndjson]
    parse: jsonl
    paging: all
    name: "{{item.path_with_namespace}}.jsonl"
    skip: "{{item.marked_for_deletion_on | present}} || {{item.marked_for_deletion_at | present}}"
    fields: {project: "{{item.id}}", path: "{{item.path_with_namespace}}", url: "{{item.web_url}}", visibility: "{{item.visibility}}", lastActivity: "{{item.last_activity_at}}"}

  - each: settings.groups
    command: [glab, api, --hostname, "{{settings.hostname}}", "groups/{{each | urlencode}}/projects?include_subgroups=true&archived=false&per_page=100", --paginate, --output, ndjson]
    parse: jsonl
    paging: all
    name: "{{item.path_with_namespace}}.jsonl"
    skip: "{{item.marked_for_deletion_on | present}} || {{item.marked_for_deletion_at | present}}"
    fields: {project: "{{item.id}}", path: "{{item.path_with_namespace}}", url: "{{item.web_url}}", visibility: "{{item.visibility}}", lastActivity: "{{item.last_activity_at}}"}

records:
  # Only what changed since the last complete pass over the project, and a
  # project with no activity since is not asked at all; what was read
  # before is kept.
  - command: [glab, api, --hostname, "{{settings.hostname}}", "projects/{{container.project}}/issues?scope=all&per_page=100&updated_after={{pass.since | time}}", --paginate, --output, ndjson]
    parse: jsonl
    paging: all
    since: {first: "2000-01-01T00:00:00Z", unchangedWhen: "{{container.lastActivity}} <= {{pass.since}}"}
    record:
      id: "{{container.path}}#{{item.iid}}"
      kind: post
      title: "{{item.title}}"
      url: "{{item.web_url}}"
      at: "{{item.created_at}}"
      modifiedAt: "{{item.updated_at}}"
      author: "{{item.author.username}}"
      channel: "{{container.path}}"
      private: "{{container.visibility}} != public"
      text: "{{container.path}}#{{item.iid}}, issue, {{item.state}}{{', labelled ' | if item.labels}}{{item.labels | join ', '}}.\n\n{{item.description | trim}}"

  # Only what changed since the last complete pass over the project, and a
  # project with no activity since is not asked at all; what was read
  # before is kept.
  - command: [glab, api, --hostname, "{{settings.hostname}}", "projects/{{container.project}}/merge_requests?scope=all&per_page=100&updated_after={{pass.since | time}}", --paginate, --output, ndjson]
    parse: jsonl
    paging: all
    since: {first: "2000-01-01T00:00:00Z", unchangedWhen: "{{container.lastActivity}} <= {{pass.since}}"}
    record:
      id: "{{container.path}}!{{item.iid}}"
      kind: post
      title: "{{item.title}}"
      url: "{{item.web_url}}"
      at: "{{item.created_at}}"
      modifiedAt: "{{item.updated_at}}"
      author: "{{item.author.username}}"
      channel: "{{container.path}}"
      private: "{{container.visibility}} != public"
      text: "{{container.path}}!{{item.iid}}, merge request, {{item.state}}{{', labelled ' | if item.labels}}{{item.labels | join ', '}}.\n\n{{item.description | trim}}"
---

# GitLab

Reads every issue and merge request of the projects an account can see on one GitLab server, through `glab`. A server that answers only inside one network is read by choosing, for this source, a computer inside it.

## Document identifiers

A project is the file `<group>/<project>.jsonl`; an issue is `<group>/<project>#<iid>` and a merge request `<group>/<project>!<iid>`.

These are not the identifiers of a source built from an exported archive, which named each document after the file the export wrote. Switching such a source to this type reads every issue and merge request again as a new document, and the old documents leave when the first complete pass ends. Keeping the old source instead is also a choice: an export does not change unless it is exported again.

## Checked against the tool

Run end to end with `glab` 1.89 against a self-hosted GitLab of some four thousand projects.
