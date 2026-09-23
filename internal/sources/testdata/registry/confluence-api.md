---
name: confluence-api
description: A Confluence Cloud site's pages, blog posts and comments, read with its web API and an API token of the person's own, each one's text as it is stored.
runs: [computer, server]
# The site counts calls; a steady pace stays well under it.
pace: 100ms

settings:
  - name: site
    description: the site's address without https://, such as example.atlassian.net
    type: string
    pattern: "^[a-z0-9][a-z0-9.-]*\\.[a-z]{2,}$"
  - name: email
    description: the email address the API token belongs to
    type: string
    pattern: "^[^ @]+@[^ @]+$"
  - name: spaces
    description: the spaces read, by key; empty reads the whole site
    type: array
    items: {type: string, pattern: "^[A-Za-z0-9~][A-Za-z0-9_-]*$"}
    default: []

secrets:
  - key: apiToken
    description: an API token made at id.atlassian.com for the email above
    scope: person

authenticationProfiles:
  site:
    type: basic
    username: "{{settings.email}}"
    password: "{{secret:apiToken}}"

containers:
  # Each space is a container, so a space's pages are read and kept
  # together and a site of any size is never held at once. The API pages
  # the list of spaces by giving the next page's address.
  - request:
      url: "https://{{settings.site}}/wiki/rest/api/space?limit=250"
      auth: site
    parse: {json: {items: results}}
    paging: {link: {field: _links.next, base: _links.base}}
    skip: "!({{settings.spaces | empty}} || {{item.key}} in {{settings.spaces}})"
    name: "{{item.key}}.jsonl"
    fields: {space: "{{item.key}}", spaceName: "{{item.name}}"}

records:
  # A space's pages, blog posts and comments, listed without their bodies
  # and paged to the end; a body is fetched only when its version changed,
  # and kept.
  - each: [page, blogpost, comment]
    request:
      # The query, escaped by hand around what varies: type = <kind> and
      # space = "<key>".
      url: "https://{{settings.site}}/wiki/rest/api/content/search?limit=100&expand=version,history&cql=type%20%3D%20{{each}}%20and%20space%20%3D%20%22{{container.space | query-escape}}%22"
      auth: site
    parse: {json: {items: results}}
    paging: {link: {field: _links.next, base: _links.base}}
    detail:
      request:
        url: "https://{{settings.site}}/wiki/rest/api/content/{{item.id}}?expand=body.storage"
        auth: site
      parse: json
      text: "{{detail.body.storage.value | html-text}}"
    record:
      id: "confluence:{{each}}:{{item.id}}"
      kind: "{{each}}"
      title: "{{item.title}}"
      url: "https://{{settings.site}}/wiki{{item._links.webui}}"
      at: "{{item.history.createdDate}}"
      modifiedAt: "{{item.version.when}}"
      author: "{{item.version.by.displayName | or item.history.createdBy.displayName}}"
      channel: "{{container.spaceName}}"
      version: "{{item.version.number}}"
      private: true
      text: "{{item.title}}\n\n{{detail.text}}"

---

# Confluence, through its web API

Reads a Confluence Cloud site with its REST API, as the person, with an API token they make at id.atlassian.com and fill in on the source; the token is kept sealed on the server and sent only with the reading. Each space is a container: its pages, blog posts and comments are listed to the end of the API's paging, so a site of any size is read whole, and a body is fetched only when its version changed. The first pass fetches every body and takes a while; a pass that runs out of time goes on from where it stopped.

## Document identifiers

A space is the file `<space key>.jsonl`; a page is `confluence:page:<id>`, a blog post `confluence:blogpost:<id>` and a comment `confluence:comment:<id>`.

## Checked against the service

`/wiki/rest/api/space`, `/wiki/rest/api/content/search` with `cql`, `limit` and `expand`, and `/wiki/rest/api/content/<id>?expand=body.storage`, each paged by the `_links.next` it answers with, on a Confluence Cloud site.
