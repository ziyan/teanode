# A checkout with none of your commits in it is somebody else's

- Status: accepted
- Date: 2026-09-18
- Deciders: Ziyan Zhou

## Context

A `files` source is usually pointed at the directory a person keeps their
work in, and that directory holds other people's work too: everything they
have ever cloned to read, to build, or to fix one line of. Read as a tree of
files it all looks the same, so the agent indexed all of it and spent its
nights filing facts about somebody else's source onto the person's own
pages. On one deployment a single source held 32,535 indexed files and more
than half were from three checkouts nobody there had ever committed to.

The obvious fix is a list — of directory names, of project names, of the
places dependencies are usually kept. This program had exactly such a list
and took it out (migration 0082): a name cannot carry the judgement, the
list never names every case, and reading as a promise it cannot keep is
worse than not making it. A second one would fail the same way.

There is better evidence already on hand. The scan computes a profile for
every checkout it finds, and the profile carries every address in that
checkout's history; the server already knows which addresses are the
person's, from the card they marked as themselves.

## Decision

A checkout whose history holds none of the person's addresses is somebody
else's code. Its **profile** is kept — what it is, what its readme calls it,
where it lives, its newest tag — so the graph still knows the checkout is
there and "what was that thing I cloned" has an answer. Its files are not
read, and nor is its commit history. A source may say
`specification.readEveryCheckout` and get all of it, for somebody who does
want a dependency's source read.

Not knowing keeps the files: no addresses from the server, a checkout git
cannot read, a checkout with no commits at all, all read as before. Silence
must never come out as "none of this is yours".

The rule is applied on the device, and its inputs are sent by the server
with every request. The server decides the policy because who somebody is
lives on their card and changes there — a copy kept on a laptop would go
stale with nobody able to see that it had. The device applies it because a
file that is not going to be filed should not be read, hashed and carried
across a socket first; a file left out of the manifest is never in the
hashes the server holds, so deciding at the far end would mean shipping the
text of every one of those files on every pass, for ever.

What that costs the person to see is on the source, wherever the source is
shown: how many checkouts were kept to their profile and how many files that
was.

## Consequences

An affected source loses the documents it should never have had, on its next
full pass, through the ordinary sweep: on the deployment above, roughly
eighteen thousand of them. Facts already learned from those files are not
swept with them — `agent_fact` has no foreign key to `agent_document` — so a
page that says something about somebody else's source goes on saying it
until somebody strikes it.

A person whose commits are all under an address their card does not list
looks, to this rule, like somebody who has never committed: every checkout
is kept to its profile and nothing of theirs is read. That is the same
silence that already made their work history empty, it is already reported
as the commit addresses the source could not place, and the fix is the same
one minute of marking a card. The two now sit next to each other on the
source's row, which is the point.

An older daemon does not know the rule and goes on offering everything; the
server files what it is offered. The person updates the program on their
machine and the next pass is quiet. Nothing on the server has to change for
that to happen, and nothing breaks while it has not.
