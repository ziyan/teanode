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
few friends, a small team. They want to write to each other without mail,
see when the others are busy, let one agent know what another knows, and
in a family, let a parent answer for a child. Each of those needs the same
first thing: a list of who belongs together, and in what role.

Three shapes were considered. Pairwise relationships (a parent of, a
spouse of, a colleague of) say exactly what is true and make every
question ("who can see this calendar") a walk over a graph. A server-wide
directory, where everybody sees everybody, is simplest and wrong for a
server that hosts two families, or a family and the parents' employer's
side project. A named group with members and a role each is what people
already have in their heads ("the family", "the flat", "the team") and
answers the questions with one lookup.

The word `group` is taken: it is the access group in `internal/access`.

## Decision

A **circle** is a named set of accounts on one server, of one **circle
kind**: `family`, `friends` or `work`. Each member has a **member role**:
`admin`, `member` or `child`. `child` exists only in a family circle, and
the admins of a family circle are the **guardians** of its children. An
account may be in several circles.

A circle is made by any person who holds the new `circle:use`
permission, which the Members role has by default. Joining is by
**invitation**, naming an exact username, and nothing about a person
reaches a circle until they accept. The answer to an invitation is the
same whether the username exists or not, so inviting is not a way to list
the server's accounts. An operator with `user:manage` may put an account
in a circle directly, because the parent who creates a child's account is
usually the operator of a family server, and a child should not have to
accept their own family.

A person sees other people only through circles they share: name,
username and avatar. Everything this design shares (a chat, a calendar, a
page of memory) is shared with a circle or with one person in a circle
the sharer is in, and stops when either of them leaves it.

A child cannot leave a family circle, cannot accept an invitation to
another circle, and cannot share anything with somebody outside the
family, without a guardian's approval
(`20261016-a-guardian-answers-and-pays-from-their-own-agent.md`).

## Consequences

Three tables: `circle`, `circle_member` and `circle_invitation`, and one
authorization helper, `requireCircleMember`, which answers "not found"
like every other refusal.

"Who can see this" is always a circle or a person, and every share row
names one. Leaving a circle, or being removed from it, ends every share
that reached the person only through it, in the same transaction.

Two families on one server are invisible to each other unless somebody
invites somebody. A person who wants to message an account they share no
circle with makes a circle of two first, which is a deliberate step and
the price of not having a directory.

People on other servers cannot be in a circle. Reaching them stays mail.
