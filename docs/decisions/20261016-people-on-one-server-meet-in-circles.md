# People on one server meet in circles

- Status: proposed
- Date: 2026-10-16
- Deciders: the-owner

## Context

Every account on a server is alone. Its mailboxes, calendar, address book,
agent and memory belong to it and to nothing else, and the only way one
account reaches another is to send it mail. The one relationship the
server knows is the access group, which says what an account may manage,
not who it lives or works with.

A server is often shared by people who do know each other: a household, a
few friends, colleagues. They want to write to each other without mail,
see when the others are busy, let one agent know what another knows, and
pay with the household's account. Each of those needs the same first
thing: a list of who belongs together.

Three shapes were considered. Pairwise relationships (a parent of, a
spouse of, a colleague of) say exactly what is true and make every
question ("who can see this calendar") a walk over a graph. A server-wide
directory, where everybody sees everybody, is simplest and wrong for a
server that hosts two families, or a family and the parents' colleagues.
A named set of people is what people already have in their heads ("the
family", "the flat", "the team") and answers the questions with one
lookup.

Roles beyond managing the set were considered too (a guardian who answers
for a child, a child who may not leave) and left out. Each one is a rule
every later feature would have to honor, and what they protect is already
covered: a person who shares something decides how much (a payment share
can ask them every time), and the operator of a family server, who is
usually a parent, can already read any account's conversations.

The word `group` is taken: it is the access group in `internal/access`.

## Decision

A **circle** is a named set of accounts on one server, of one **circle
kind**: `family`, `friends` or `colleagues`. The screens say the kind
where the generic word would be awkward ("your family", "your
colleagues"). An account may be in several circles.

Each member has a **member role**, `admin` or `member`. An admin manages
the circle: who is in it, who else is an admin, its name, its calendar. A
member takes part. That is all a role does: a role never decides what
somebody may see of anybody else's, which is always the owner's choice.

A circle is made by any person who holds the new `circle:use`
permission, which the Members role has by default, and its maker is its
first admin. Joining is by **invitation**, naming an exact username, and
nothing about a person reaches a circle until they accept. The answer to
an invitation is the same whether the username exists or not, so inviting
is not a way to list the server's accounts. An operator with `user:manage`
may put an account in a circle directly, for an account they made for
somebody who will not set it up themselves.

A person sees other people only through circles they share: name,
username and avatar. Everything this design shares (a chat, a calendar, a
page of memory, a skill) is shared with a circle or with one person in a
circle the sharer is in, and stops when either of them leaves it. Anyone
may leave a circle.

When an account is deleted, what it wrote in chats is kept, so the
others' replies still make sense, and is shown as from a deleted account
under the name it had. The account's own rows go as they do today.

## Consequences

Three tables: `circle`, `circle_member` and `circle_invitation`, and one
authorization helper, `requireCircleMember`, which answers "not found"
like every other refusal. A circle always keeps at least one admin.

"Who can see this" is always a circle or a person, and every share row
names one. Leaving a circle, or being removed from it, ends every share
that reached the person only through it, in the same transaction.

Nothing here limits what a member does with their own account. A parent
who wants that for a child sets it where it already lives: the operator's
switches and limits on the child's agent, and the payment share's
approvals.

Two families on one server are invisible to each other unless somebody
invites somebody. A person who wants to message an account they share no
circle with makes a circle of two first, which is a deliberate step and
the price of not having a directory.

People on other servers cannot be in a circle. Reaching them stays mail.
