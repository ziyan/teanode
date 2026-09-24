---
name: rss
description: A news feed - RSS or Atom - read with a web request; each entry is a record.
runs: [server, computer]

settings:
  - name: url
    description: the feed's address
    type: string
    pattern: "^https?://[^ ]+$"

secrets:
  - key: token
    description: a token the feed asks for, sent as a bearer token; leave unset for a public feed
    scope: person
    optional: true

authenticationProfiles:
  feed:
    type: bearer
    token: "{{secret:token}}"

containers:
  - fixed: [{}]
    name: feed.jsonl

records:
  # A feed lists its recent entries only, so what falls off the end of it
  # has not been deleted: keep it.
  - request:
      method: GET
      url: "{{settings.url}}"
      auth: feed             # sends nothing while the optional secret is unset
    parse: {xml: {items: "rss.channel.item | feed.entry"}}
    paging: none
    unseen: keep
    record:
      id: "{{item.guid | or item.id | or item.link}}"
      kind: page
      title: "{{item.title}}"
      url: "{{item.link.href | or item.link}}"
      at: "{{item.pubDate | or item.updated | time}}"
      author: "{{item.author.name | or item.author}}"
      private: false
      text: "{{item.title}}\n\n{{item.description | or item.summary | or item.content | html-text}}"
---

# RSS

Reads a news feed, RSS or Atom, with a web request rather than a command: there is no tool to install. Each entry is a record, its text the entry's summary or content turned from HTML into text.

It runs on this server for a public feed, or through one of the person's computers for a feed that answers only inside their network. A feed that wants a token takes it as a secret, kept encrypted on the server and sent only to the address the source was given.

A feed carries only its most recent entries, so an entry that has dropped off the end is kept, not deleted.

## Document identifiers

The feed is the file `feed.jsonl` and an entry is its `guid` (RSS) or `id` (Atom), or its link where it has neither.
