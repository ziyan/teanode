# A chat is a conversation with people in it, and an agent in it speaks for its owner

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
asked to help. The place they already talk is the drawer, and on the
iPhone the conversation screen: a list of conversations, a message list,
a reply bar, reactions, pictures, cards. A second set of screens for
talking to people would teach everybody two ways of doing one thing, and
every later improvement would have to be made twice.

The storage cannot simply be one shared conversation. A conversation's
rows include its agent's tool results, which can hold a summary of the
owner's mail or a balance; four people reading one set of rows would read
each other's. And an agent in a chat is the easiest way yet for one
person to reach another person's private things: a message from anybody
in the chat is text the agent reads, and the agent can read its owner's
mail.

## Decision

A **chat** is a conversation with people in it: `direct` between two
people, or `group`. A circle has one circle chat whose members follow the
circle's. It is shown where every conversation is shown, in the drawer's
list beside the main and named conversations, on the conversations page
and in the iPhone app, and it is read, followed, written to and reacted
to with the same operations as any conversation. There is no separate
chat page.

Underneath, each member has their own conversation of the kind `chat`
that points at the shared chat. What the people and the agents say there
is stored once, in the chat, and every member's conversation shows it.
What a member's own agent did to answer (its tool calls and results, its
notes, its cards) is stored in that member's conversation alone, the way
it is in any conversation, and only that member reads it. Reading a chat
conversation interleaves the two by time.

A message in a chat goes to the people. It starts an agent's turn only
when the writer addresses an agent (mentioning it, or replying to it), and
from no other message. One message starts at most four agents (the first
four it addresses), so a message that mentions everybody cannot start a
crowd of turns. Another member's message never starts a turn of somebody
else's agent unless its owner allowed that, and an agent's message never
starts a turn of any agent, so two agents in a room cannot talk to each
other. A card the turn raises is the owner's alone. The other members see
that the agent is thinking or working, and not which tool it is using.

The owner may let the other members of one chat ask their agent too. A
turn one of them starts is limited to the chat and the web. It reads none
of the owner's mail, memory, files, calendar or money, and makes no call
that needs the owner's word. That includes what the owner shared with
particular people: a shared calendar or shared memory page was shared with
the person it was shared with, not with the room, and the answer goes to
the whole room, so a member's turn is offered neither.

Whether an agent remembers a chat is its owner's choice, chat by chat,
the way a mailbox is granted: a chat is a source. Every member can see
which agents remember the chat they are in.

## Consequences

The chat's own rows (`chat`, `chat_member`, `chat_message`,
`chat_reaction`) sit under the existing conversation operations, which
learn the kind `chat`. Every person who can chat has an `agent` row,
switched off if they never turned their agent on, because conversations
and phones hang from it; the drawer opens for chats with the agent off.

A person's own conversation with their agent is what it was. Talking to
people does not change it.

An owner who asks their agent something in front of the room has chosen
to say the answer in front of the room. The conduct tells the agent who
is listening, so it does not volunteer what the question did not need.

Other people's words in a chat reach the model as other people's words,
wrapped the way mail from strangers is, and are never instructions.

Usage of every turn, including one another member started, is counted
against the agent's owner, who is the one who allowed it.
