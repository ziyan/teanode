---
name: github-gh
description: GitHub repositories - their READMEs, issues and pull requests - read with the gh command line tool.
requires: [gh]

settings:
  - name: organizations
    description: organizations whose repositories are read as well as the account's own; each one that cannot be listed fails the pass
    type: array
    items: {type: string, pattern: "^[A-Za-z0-9][A-Za-z0-9-]*$"}
    default: []
  - name: repositories
    description: single repositories read as well, as owner/name, for one worth reading in an organization that is not
    type: array
    items: {type: string, pattern: "^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$"}
    default: []
  - name: forks
    description: read repositories that are forks of somebody else's
    type: boolean
    default: false

containers:
  # The account's own repositories. gh pages by itself with --paginate.
  - command: [gh, api, "user/repos?affiliation=owner&per_page=100", --paginate, --jq, ".[]"]
    parse: jsonl
    paging: all
    skip: "{{item.fork}} && !{{settings.forks}}"
    name: "{{item.full_name | replace \"/\" \" \"}}.jsonl"
    fields: {repository: "{{item.full_name}}", private: "{{item.private}}", updated: "{{item.pushed_at}}", url: "{{item.html_url}}", description: "{{item.description}}"}

  # Each organization named, as a listing of its own.
  - each: settings.organizations
    command: [gh, api, "orgs/{{each}}/repos?type=all&per_page=100", --paginate, --jq, ".[]"]
    parse: jsonl
    paging: all
    skip: "{{item.fork}} && !{{settings.forks}}"
    name: "{{item.full_name | replace \"/\" \" \"}}.jsonl"
    fields: {repository: "{{item.full_name}}", private: "{{item.private}}", updated: "{{item.pushed_at}}", url: "{{item.html_url}}", description: "{{item.description}}"}

  # Each single repository named. The answer is one object, not a list.
  - each: settings.repositories
    command: [gh, api, "repos/{{each}}"]
    parse: {json: {items: "."}}
    paging: none
    name: "{{item.full_name | replace \"/\" \" \"}}.jsonl"
    fields: {repository: "{{item.full_name}}", private: "{{item.private}}", updated: "{{item.pushed_at}}", url: "{{item.html_url}}", description: "{{item.description}}"}

records:
  # What the repository says it is. Read again only when it was pushed to.
  - command: [gh, api, "repos/{{container.repository}}/readme", -H, "Accept: application/vnd.github.raw"]
    parse: text
    paging: none
    missing: empty           # a repository with no README answers 404
    record:
      id: "{{container.repository}}:readme"
      kind: page
      title: "{{container.repository}} README"
      url: "{{container.url}}"
      modifiedAt: "{{container.updated}}"
      private: "{{container.private}}"
      text: "{{container.repository}} - {{container.description}}\n\n{{item.text}}"

  # Issues and pull requests together: the issues endpoint returns both,
  # every one of them, rather than the most recent few hundred.
  - command: [gh, api, "repos/{{container.repository}}/issues?state=all&per_page=100", --paginate, --jq, ".[]"]
    parse: jsonl
    paging: all
    record:
      id: "{{container.repository}}#{{item.number}}"
      kind: post
      title: "{{item.title}}"
      url: "{{item.html_url}}"
      at: "{{item.created_at}}"
      modifiedAt: "{{item.updated_at}}"
      author: "{{item.user.login}}"
      channel: "{{container.repository}}"
      private: "{{container.private}}"
      text: "{{container.repository}}#{{item.number}}, {{item.state}}. {{item.labels.*.name | join \", \"}}\n\n{{item.body}}"
---

# GitHub

Reads the repositories of the account `gh` is signed in as, and of any organization named in the settings: each repository's README, and its issues and pull requests, every one of them.

`gh` holds the person's token on their computer; nothing here carries a credential.

## Document identifiers

A repository is the file `<owner> <name>.jsonl`, and an issue or pull request is `<owner>/<name>#<number>` within it, the README `<owner>/<name>:readme`. These are the names the records script this replaces used, so a source switched to this type keeps what it has read.

## Checked against the tool

`gh repo list --json` and the REST fields used here were read from `gh` 2.x. Not yet run end to end: whether `--paginate --jq ".[]"` over a repository with thousands of issues stays within a pass's time.
