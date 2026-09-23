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
  - name: recentCount
    description: how many of each repository's most recent issues, and as many of its pull requests, are read
    type: integer
    minimum: 1
    maximum: 10000
    default: 400

containers:
  # The account's own repositories. gh pages by itself with --paginate.
  - command: [gh, api, "user/repos?affiliation=owner&per_page=100", --paginate, --jq, ".[]"]
    parse: jsonl
    paging: all
    skip: "{{item.fork}} && !{{settings.forks}}"
    name: "{{item.full_name | replace \"/\" \" \"}}.jsonl"
    fields: {repository: "{{item.full_name}}", private: "{{item.private}}", updated: "{{item.updated_at}}", url: "{{item.html_url}}", description: "{{item.description}}"}

  # Each organization named, as a listing of its own.
  - each: settings.organizations
    command: [gh, api, "orgs/{{each}}/repos?type=all&per_page=100", --paginate, --jq, ".[]"]
    parse: jsonl
    paging: all
    skip: "{{item.fork}} && !{{settings.forks}}"
    name: "{{item.full_name | replace \"/\" \" \"}}.jsonl"
    fields: {repository: "{{item.full_name}}", private: "{{item.private}}", updated: "{{item.updated_at}}", url: "{{item.html_url}}", description: "{{item.description}}"}

  # Each single repository named. The answer is one object, not a list.
  - each: settings.repositories
    command: [gh, api, "repos/{{each}}"]
    parse: {json: {items: "."}}
    paging: none
    name: "{{item.full_name | replace \"/\" \" \"}}.jsonl"
    fields: {repository: "{{item.full_name}}", private: "{{item.private}}", updated: "{{item.updated_at}}", url: "{{item.html_url}}", description: "{{item.description}}"}

records:
  # What the repository says it is: a line naming it, and its README as a
  # file beside it. A repository with no README has no record.
  - command: [gh, api, "repos/{{container.repository}}/readme", -H, "Accept: application/vnd.github.raw"]
    parse: text
    paging: none
    missing: empty           # a repository with no README answers 404
    skip: "{{item.text | empty}}"
    record:
      id: "{{container.repository}}:readme"
      kind: page
      title: "{{container.repository}} README"
      url: "{{container.url}}"
      modifiedAt: "{{container.updated}}"
      private: "{{container.private}}"
      text: "{{container.repository}} \u2014 {{container.description | or 'a repository'}}.\n\n"
    metadata:
      repository: "{{container.repository}}"
    attachments:
      content: "{{item.text}}"
      name: "{{container.repository | replace '/' ' '}} README.md"

  # The most recent issues, and the most recent pull requests. gh answers
  # a repository with issues turned off as a missing one.
  - command: [gh, issue, list, --repo, "{{container.repository}}", --state, all, --limit, "{{settings.recentCount}}", --json, "number,title,body,author,createdAt,updatedAt,url,state,labels"]
    parse: json
    paging: none
    missing: empty
    record: &issue
      id: "{{container.repository}}#{{item.number}}"
      kind: post
      title: "{{item.title}}"
      url: "{{item.url}}"
      at: "{{item.createdAt}}"
      modifiedAt: "{{item.updatedAt}}"
      author: "{{item.author.login}}"
      channel: "{{container.repository}}"
      private: "{{container.private}}"
      text: "{{container.repository}}#{{item.number}} \u2014 issue, {{item.state | lower}}{{', labelled ' | if item.labels}}{{item.labels.*.name | join ', '}}.\n\n{{item.body | trim}}"
    metadata: &metadata
      repository: "{{container.repository}}"
      kind: issue
      state: "{{item.state}}"
      labels: "{{item.labels.*.name | join ', '}}"
      number: "{{item.number}}"

  - command: [gh, pr, list, --repo, "{{container.repository}}", --state, all, --limit, "{{settings.recentCount}}", --json, "number,title,body,author,createdAt,updatedAt,url,state,labels"]
    parse: json
    paging: none
    missing: empty
    record:
      <<: *issue
      text: "{{container.repository}}#{{item.number}} \u2014 pull request, {{item.state | lower}}{{', labelled ' | if item.labels}}{{item.labels.*.name | join ', '}}.\n\n{{item.body | trim}}"
    metadata:
      <<: *metadata
      kind: pull request
---

# GitHub

Reads the repositories of the account `gh` is signed in as, and of any organization named in the settings: each repository's README, and its most recent issues and pull requests, as many as `recentCount` says of each.

`gh` holds the person's token on their computer; nothing here carries a credential.

## Document identifiers

A repository is the file `<owner> <name>.jsonl`, and an issue or pull request is `<owner>/<name>#<number>` within it, the README `<owner>/<name>:readme`. These are the names the records script this replaces used, so a source switched to this type keeps what it has read.

## Checked against the tool

`gh repo list --json` and the REST fields used here were read from `gh` 2.x. Not yet run end to end: whether `--paginate --jq ".[]"` over a repository with thousands of issues stays within a pass's time.
