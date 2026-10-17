# A chat belongs to the people in it, and an agent in it speaks for its owner

- Status: proposed
- Date: 2026-10-16
- Deciders: the-owner

## Context

A conversation (`agent_conversation`) is one agent's: one owner, one
agent, every message either the owner's or the agent's, with the agent's
tool calls and their results in the same rows. Nothing in it can say that
a message came from somebody else, and nobody else may read it.

People in a circle want to write to each other: two of them directly, or
several at once, with nobody's agent involved or with one or more agents
asked to help. Reusing the agent's conversation would mean deciding whose
agent owns a chat between four people, and would put one person's tool
results (a summary of their mail, a balance) in rows the other three can
read.

An agent in a chat is also the easiest way yet for one person to reach
another person's private things: a message from anybody in the chat is
text the agent reads, and the agent can read its owner's mail.

## Decision

A **chat** is its own thing, belonging to its members: `direct` between
two people, or `group`. A circle has one circle chat whose members follow
the circle's. Messages are written by a person or by an agent, and the
chat keeps who.

An agent takes part in a chat only for its owner. A turn starts when the
owner addresses their agent in the chat (mentioning it, or replying to
it), and from no other message. Another member's message never starts a
turn of somebody else's agent, and an agent's message never starts a turn
of any agent, so two agents in a room cannot talk to each other.

The turn runs in the agent's own conversation, a `chat` conversation kept
for that chat, with its tool calls and results there, where only the
owner can read them. Only the answer is posted to the chat. A card the
turn raises goes to the owner alone. The other members see that the agent
is thinking or working, and not which tool it is using.

The owner may let the other members of one chat ask their agent too. A
turn one of them starts runs held to what everybody in the chat may see:
the chat, what has been shared with all of them, the web. It reads none
of the owner's mail, memory, files or money, and makes no call that
needs the owner's word.

Whether an agent remembers a chat is its owner's choice, chat by chat,
the way a mailbox is granted: a chat is a source. Every member can see
which agents remember the chat they are in.

## Consequences

Four tables (`chat`, `chat_member`, `chat_message`, `chat_reaction`),
their own feed and their own read state per member. The agent's
conversation gains the kind `chat` and a `chat_id`.

An owner who asks their agent something in front of the room has chosen
to say the answer in front of the room. The conduct tells the agent who
is listening, so it does not volunteer what the question did not need.

Other people's words in a chat reach the model as other people's words,
wrapped the way mail from strangers is, and are never instructions.

Usage of every turn, including one another member started, is counted
against the agent's owner, who is the one who allowed it.
