# Relationships between people in a circle

This ExecPlan is a living document, kept as `~/.claude/PLAN.md` describes.

## Purpose

A person can say what somebody they share a circle with is to them: "Sam is
my father", "Alex is my wife". The other side is filled in at once ("Sam" sees
that you are their son), and they are told. Their agent and yours know it, so
"ask my father whether he is free on Sunday" or "where is my wife?" reaches the
right account without the person spelling out a username.

Each person may also say how they are described (he, she or they). That is
what turns the other side's word from "child" into "son".

## Decisions (asked of the person, 2026-10-18)

- The opposite word comes from how that person is described, set once on
  their account (`pronoun`: he, she, they). Until it is set the neutral word is
  used (spouse, child, sibling). A word somebody chose for another person
  ("wife") keeps its gender until that person says how they are described,
  which then wins.
- The other side is set at once, and the other person is told (a note in the
  main conversation of their agent, and a phone notification of kind
  `circle`). It is one fact: either of the two can change it or remove it, and
  that changes it for both.
- Only the two people and their own agents see it. Other members of a circle
  do not.

## The fact

One row per pair of people (`person_relationship`), written by whichever of
the two said it last:

| Column | What it holds |
| --- | --- |
| `id` | the row |
| `created_at`, `modified_at` | when |
| `user_id` | who said it last |
| `related_user_id` | who it is about |
| `relationship_kind` | what `related_user_id` is to `user_id`: one of the kinds below |
| `related_user_term_gender` | the gender of the word `user_id` used: `masculine`, `feminine` or `neutral` |

A unique index on the unordered pair keeps it one fact. Both users cascade.
Saying it again, from either side, rewrites the row from the new speaker's
side. It is shown only while the two share a circle (it is read through the
list of people the caller shares a circle with), and setting it needs a shared
circle.

`user.pronoun` is `he`, `she`, `they`, or NULL.

### Kinds and their opposites

| Kind | Opposite | Neutral | Masculine | Feminine |
| --- | --- | --- | --- | --- |
| spouse | spouse | spouse | husband | wife |
| partner | partner | partner | boyfriend | girlfriend |
| parent | child | parent | father | mother |
| child | parent | child | son | daughter |
| sibling | sibling | sibling | brother | sister |
| grandparent | grandchild | grandparent | grandfather | grandmother |
| grandchild | grandparent | grandchild | grandson | granddaughter |
| aunt_or_uncle | niece_or_nephew | aunt or uncle | uncle | aunt |
| niece_or_nephew | aunt_or_uncle | niece or nephew | nephew | niece |
| cousin | cousin | cousin | cousin | cousin |
| parent_in_law | child_in_law | parent-in-law | father-in-law | mother-in-law |
| child_in_law | parent_in_law | child-in-law | son-in-law | daughter-in-law |
| sibling_in_law | sibling_in_law | sibling-in-law | brother-in-law | sister-in-law |
| step_parent | step_child | step-parent | stepfather | stepmother |
| step_child | step_parent | stepchild | stepson | stepdaughter |
| friend | friend | friend | friend | friend |
| colleague | colleague | colleague | colleague | colleague |

A **relationship term** is one cell of that table, written as an identifier
(`wife`, `father`, `aunt_or_uncle`, `father_in_law`, `step_child`, ...). That is
what a person picks and what the API takes. Input is lowercased, and spaces and
hyphens become underscores, so "father-in-law" and "Father in law" both work.
The words are English in `models`; the dashboard and the iPhone app word them
in their own languages from the kind and the gender.

### Which word is shown

What A sees B as: the kind as A sees it, in the gender of B's pronoun when B
has set one (he: masculine, she: feminine, they: neutral), otherwise the gender
of the word the speaker used for B when the speaker was A, otherwise neutral.

## The API (GraphQL)

- `CircleMember` gains, filled in wherever the caller is looking at somebody
  else (`ListCirclePeople`, a circle's `members`), empty otherwise:
  - `relationshipKind`: what this person is to the caller, or "" when nothing
    was said
  - `relationshipTermGender`: `masculine`, `feminine` or `neutral`, the gender
    of the word to show
  - `relationshipWord`: that word in English ("wife"), for the command line
    and the agent
- `SetPersonRelationship(userId, relationshipTerm): CircleMember`: what
  `userId` is to the caller. Needs circle:use and a circle in common (not found
  otherwise). Returns the person as the caller now sees them. Tells the other
  person.
- `RemovePersonRelationship(userId): Boolean`: takes the fact away for both.
  Tells the other person. True when there was one.
- `User.pronoun`, and `UpdateUser(pronoun)`, "" clears. A person may set
  their own.

## Surfaces

| Surface | What |
| --- | --- |
| Migration | `0243_person_relationship.sql` and `.reverse.sql` |
| Models | `internal/models/relationship.go`: kinds, terms, words, `Pronoun` |
| Database | `internal/db/database_relationship.go`; `ListCirclePeople` and circle reads stay as they are, the graph reads the caller's relationships once a request |
| GraphQL | `internal/api/v1api/apigraph/relationship.go`, the `CircleMember` fields, `UpdateUser` |
| Note and phone | `models.NoteRelationshipSet` (`relationship_set`) and `NoteRelationshipRemoved` (`relationship_removed`), written in the transaction; phone notification kind `circle` |
| Agent prompt | the situation says how the person is described, and lists the people with a relationship: "Sam Lee (username sam) is their father." |
| Agent tool | `circle` tool: `relate` (username, relationship), `unrelate` (username); `people` shows the word |
| Command line | `teanode circle relate <username> <relationship>`, `teanode circle unrelate <username>`, a Relationship column in `teanode circle people`; `--pronoun` where the account's name is set |
| Dashboard | circles page: the word beside each person and a "Relationship" choice; settings/profile: "Pronoun" |
| iPhone | Circles screen: the same; profile: "Pronoun"; the two note kinds in the conversation; demo data and a screenshot |
| Docs | `docs/subsystems/circles.md`, `docs/reference/command-line.md`, an ADR |

## Out of scope

- People on other servers, and contacts in the address book who have no
  account here.
- Inferring a relationship from mail or from memory. Only the person's word
  sets one.
- Relationships between two other people ("Sam is Alex's brother").

## Progress

- [ ] Migration, models, database, GraphQL, note kinds, tests
- [ ] Agent prompt, circle tool, command line
- [ ] Dashboard
- [ ] iPhone
- [ ] Docs, ADR
- [ ] Review until a round finds nothing; parity check
- [ ] Deploy and use it

## Decision log

- One row per pair, rewritten from the side of whoever says it last, rather
  than one row per direction: there is never a disagreement to resolve.
- Gender of the word stored, not of the person: saying "my wife" says nothing
  on the other person's account.
