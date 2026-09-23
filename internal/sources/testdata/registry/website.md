---
name: website
description: A website's pages, followed from a start page within the addresses allowed, read by TeaNode's own web reader.
reader: web
runs: [server, computer]

settings:
  - name: start
    description: the page to start from
    type: string
    pattern: "^https?://[^ ]+$"
  - name: allow
    description: address prefixes the reader may follow links into; empty allows only the start page's own site
    type: array
    items: {type: string, pattern: "^https?://[^ ]+$"}
    default: []
  - name: depth
    description: how many links away from the start page to read
    type: integer
    minimum: 0
    maximum: 10
    default: 2
---

# Website

Reads a website's pages as documents, starting from one page and following links within the addresses allowed, up to a number of links away. Pages are turned from HTML into text by the reader built into TeaNode, which is why this is a reader and not a list of requests: following links and reading HTML is code.

It can run on this server, for a public site, or through one of the person's computers, for one that answers only inside their network.

**Not built yet.** The source model has carried a web crawl's settings since the first version, but no reader has been written; this type is here so that it is designed with the others.

## Document identifiers

A page is its address, without the part after `#`.
