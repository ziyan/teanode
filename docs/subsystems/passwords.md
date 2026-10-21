# Passwords

Passwords keeps a person's logins, passkeys, cards and notes on their own
server, encrypted on their own devices. The server stores sealed text and
cannot read it, with one exception the person chooses: an item they turn on
**Let the agent use this** for. This page says what the agent may do with
Passwords, how the encryption makes that possible and no more, and the rules
the agent's tool keeps. The whole design, with the format of every sealed value,
is `docs/planning/passwords-execplan.md`.

The words are fixed: an **item** is one login, card or note; a **login** has a
user name, a password, sites, a one-time code and passkeys; the **passphrase**
and the **recovery key** open a person's Passwords on a device; **share** gives
an item to a circle or a person; **Let the agent use this** is the switch on one
item. Not "vault", not "entry", not "credential".

## The encryption, in short

- A person's devices make an **account key** and a key pair. The passphrase (run
  through PBKDF2-SHA256, 600,000 rounds) and the recovery key each wrap the
  account key; the server keeps the wrapped copies and a hash of a proof of each,
  never the passphrase.
- Every item has its own **item key**. The item's fields (name, user name,
  password, sites and the rest) are sealed with it using AES-256-GCM, with the
  item's id and kind in the associated data. The item key is sealed with the
  account key. A database dump shows no site, user name or password.
- Sharing seals the item key for the recipient's public key (ECDH on P-256,
  then HKDF and AES-256-GCM), so only that person's devices open it.
- The server has one key pair of its own, the **agent key**. Its private half is
  sealed under the server's secret (`internal/util/secretbox`, label
  `teanode passwords: agent key-v1`). When the person turns on "Let the agent use
  this" for an item, their device seals that one item's key for the agent key.
  The server can therefore open exactly the items that were turned on, and no
  other: every other item's key is sealed for keys only the person's devices
  hold. Turning the switch off removes the sealed key.

The code is `internal/passwords` (the format, `AgentOpener` for the agent key),
and the same format is written in TypeScript for the dashboard and in Swift for
the iPhone, pinned by shared test vectors.

## What the agent may do

The `passwords` tool (`internal/agent/tools/passwords`), permission
`passwords:use`, has three actions, all reads:

- **list** gives the items the person turned on: id, kind, name, and for a login
  its sites, user name, and whether it has a one-time code and passkeys. It
  never gives a password, a one-time code secret, a passkey or a card. With none
  turned on, it says how: open the item in Passwords and turn on "Let the agent
  use this".
- **read_note** gives the text of a note that was turned on, by id. A note is the
  one thing the model reads in full, so it goes to the model's provider; that is
  what the person chose by turning it on. The text is marked as data that never
  instructs the agent.
- **generate** makes up a random password (20 characters of letters, digits and
  symbols unless asked otherwise; rejection sampling over `crypto/rand`) and
  stores it nowhere.

## The rules

- **Only what the person turned on.** The tool lists items with
  `is_usable_by_agent` that the person owns. It never reads an item somebody
  shared with the person: the owner decides what reaches a model's provider, as
  for Health. It never reads a card: the database refuses to mark one, the
  opener refuses one, and the tool answers that a card cannot be used.
- **Secrets stay out of the model.** A login's password, one-time code and
  passkeys never reach it. This tool does not sign in. The plan gives that to
  fill actions of the browser tool (milestone 7), which type a value in or sign
  with a passkey by the item's id, only on a page whose host matches one of the
  item's sites, and which are not part of this tool.
- **Only where the person alone reads the answer.** In a chat with other people
  and over Telegram or Discord every action answers in one sentence that
  Passwords is not used there, because what was read would reach the room. Unlike
  Health, a run nobody is watching is allowed, a scheduled run included: signing
  in on a schedule is why a person turns an item on. A note is not read in a
  subagent, whose answer comes back as a tool result that filing into memory
  cannot tell from any other.
- **Each use is recorded.** Reading a note records the time on the item
  (`last_used_by_agent_at`) and writes a `password_item` "used by the agent"
  event to the audit log, without the text. The use is recorded before the text
  is given out; a note that cannot be recorded is not read.
- **Memory and titles.** When conversations are filed into memory, what the agent
  said and its tools answered in a turn that read a note or made up a password is
  left out, and the conversation's title and summary are not refreshed from it.
  This is the Health filter (`isPrivateToolCall`, `withoutPrivateTurns` in
  `internal/agent`) with a second tool in its list. It does not reach text the
  person or the agent repeats later; the tool's guidance tells the agent not to
  copy Passwords into notes, memory or messages.
- **The person switches, the agent does not.** Turning "Let the agent use this"
  on or off, sharing, erasing and every change to an item need the person's
  device; the server cannot read an item, so the agent has nothing to change.
