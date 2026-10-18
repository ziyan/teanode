# A relationship is one fact between two people, and each says how they are described

- Status: proposed
- Date: 2026-10-18
- Deciders: the-owner

## Context

People in a family circle want to say who is who: "Sam is my father", "Alex is
my wife". The agent needs it too. A person asks it to "ask my father whether he
is free on Sunday" or "check where my wife is", and without the relationship
the agent has a list of usernames and no way to know which one is meant.

A relationship has two sides. Saying "Sam is my father" also says that the
person is Sam's son or daughter, and which of those depends on the person, not
on Sam. TeaNode kept nothing about anyone's gender, and a guess from a name is
wrong often enough to hurt.

## Decision

A relationship is **one fact between two people who share a circle**, kept as
one row for the pair (`person_relationship`), written from the side of whoever
said it last. The other side is the opposite kind (a parent's child) and is not
stored. Either of the two may change it or remove it, and that changes it for
both, so the two can never disagree. The other person is told, in their
agent's main conversation and on their phones, with where to change it, at
most once a day from the same person.

It is seen only by the two people and their own agents, and only while they
share a circle: it is deleted when they stop sharing any, as a share between
them is.

Each person may say **how they are described**: he, she or they
(`user.pronoun`). Nobody else sets it. It is profile information, seen by the
people who share a circle with them as their name is, since the word anybody
uses for them follows it. Words for a person follow it. Until it
is said, words for them are neutral (spouse, child) and their agent speaks of
them as they, never from a guess. A word somebody chose for another person
("my wife") keeps its gender for the one who chose it until that person says
how they are described, which then wins.

The kinds are a fixed list with fixed opposites (spouse, partner, parent and
child, sibling, grandparent and grandchild, aunt or uncle and niece or nephew,
cousin, the in-laws, the step-relations, friend, colleague). A person picks a
word ("wife", "father-in-law"); the server keeps the kind and the gender of the
word, and each client words it in its own language.

## Consequences

- The agent's situation lists the people with a relationship by name and
  username, so "my father" reaches the right account with the tools that take
  a person, and says how the person is described.
- A free-text relationship ("my wife's cousin's friend") cannot be said. A
  fixed list is what makes the opposite side possible.
- Relationships between two other people, and with somebody who has no account
  here, are out of scope.
