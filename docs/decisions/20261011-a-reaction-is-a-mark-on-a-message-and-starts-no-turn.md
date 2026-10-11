# A reaction is a mark on a message, and starts no turn

- Status: accepted
- Date: 2026-10-11
- Deciders: the person, agent

## Context

A person talking to their agent in the drawer, the command line or the
phone app wants to answer a message the way a chat app lets them: a thumbs
up for an answer that worked, a heart for good news, without writing a turn
the agent then has to answer. The agent, for its part, has no light way to
acknowledge a thank-you; it answers with a sentence, which costs a round and
reads as noise.

Three choices had to be made: where a reaction is kept, whether it wakes
the agent, and how the model learns of one.

## Decision

A reaction is a row of its own (`agent_message_reaction`, migration 0162),
one per reactor per message: the person's and the agent's. Reacting again
replaces the row and an empty emoji deletes it. The message and its
conversation are foreign keys that cascade, so a reaction never outlives
what it is on. It is not a column of the message, because a message is
written once and never updated, and two reactors writing one column would
race.

A reaction is one or two emoji, at most 32 bytes, each an emoji by the
Unicode emoji properties, checked by the server in one place
(`models.ValidateReactionEmoji`) for the API, the agent's tool and the
command line alike.

A reaction starts no turn. The person reacting is saying how they felt
about a message, not asking for anything, and a turn would cost a model
call and put an answer under every thumbs up. The model sees the reaction
the next time it reads the conversation, as a short note after the message
it is on: "(The person reacted 👍)", or "(You reacted 🙏)" for its own.

The agent reacts through a `react` action of the `conversation` tool, not a
tool of its own, since the catalog is near its limit. It is a write, and
needs no card: nothing leaves the server, and the person can see it and
take it back.

A change is told to every open drawer and app as a conversation event,
`reacted`, carrying the message, the emoji (empty when taken away) and who
reacted. It belongs to no run, like a conversation's new title, so it is
never replayed; a drawer that opens later reads the reaction with the
messages.

## Consequences

- A reaction on an old message changes the text of that message as the
  model reads it, so the provider's cache of the history is lost from that
  message on for the next turn. Reactions are rare enough for that to be
  cheap; a reaction counted in the cacheable prefix would not be.
- A drawer that was disconnected when a reaction changed shows the old one
  until it reads the conversation again, which it does on reconnecting.
- The person may react only to what somebody said (their own messages and
  the agent's answers), not to a tool's answer or a note, which the
  dashboard and the app do not show as messages.
- An operator with `agent:act` reading another person's conversation may
  react in it, as that person, as they may speak in it; the log says so.
