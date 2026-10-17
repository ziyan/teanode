# Shared knowledge is read where it lives, and never copied

- Status: proposed
- Date: 2026-10-16
- Deciders: the-owner

## Context

Each agent keeps a graph of pages (`agent_node`), facts and edges, keyed
by its agent. Two people in a household know many of the same things (the
children's school, the house, a grandparent's appointments), and each
agent learns them separately, from different mail, and is wrong in
different ways.

One way to share would copy a page into the other agent's graph. A copy is
cheap to read, and wrong the day after: the owner corrects a fact and the
copy keeps the old one, and taking the share back leaves the copy where it
is. Another way would merge both into one graph that both agents write.
That is a graph nobody owns, where one person's dream rewrites the other
person's pages.

A fact also carries its evidence: the words it came from, quoted from the
owner's mail, chat or documents. A fact about a doctor's appointment is
fine to share; the quoted mail it came from may not be.

## Decision

A person shares a **subtree of their pages**, a path and everything under
it, with a circle or with one person in a circle they share. A share is
read only and live: the reader's agent reads the owner's rows as they are
now, through the memory tool, and the reader sees them on the Knowledge
page under "Shared with you".

A reader sees a shared page's name, summary and facts, and not its overview: a
model writes the overview from the page's links, its children and the quotes, so
it can name pages that are not shared and repeat quotes the owner did not share,
and the view says the owner's longer overview is not shared. Edges are
shown only when the page at the other end is shared with them too, and without
the note the owner's agent wrote on them, which is written from pages and
sources that are not shared. The
quotes behind facts are left out unless the owner chose to include them, and
never for a fact read in a chat, which holds other people's words; the source is
named only as the owner's ("from their mail", "from their chats"). A summary that
quotes somebody (four words or more between quotation marks, single or double)
is left out too, unless the owner shares quotes, and even then when the page has
a fact read in a chat. A search finds a page only by what the reader is shown of
it.

Nothing from a shared page is written into the reader's graph. The
reader's dream does not read shared pages, and a fact whose only evidence
is a shared page is refused where it is written, with a message saying to
cite the shared page instead. Taking a share back, or leaving the circle,
removes it from the reader at once, because there is nothing to remove.

A citation of a shared fact names the owner as well as the page, so the
person reading the answer knows whose memory it came from.

## Consequences

One table, `agent_page_share`, and a reader path in the memory tool and
the GraphQL resolvers that resolves a page through a share before it
resolves it in the reader's own graph. The prompt carries a short index of
what is shared with the person, bounded like the rest of the index.

Shared pages cannot be written by the reader. Knowledge a household
writes together, with no single owner, is not part of this design.

A reader's agent that learns something new about a shared subject learns
it in its own graph, under its own page, and the two pages can disagree.
That is honest: they are two people's knowledge.
