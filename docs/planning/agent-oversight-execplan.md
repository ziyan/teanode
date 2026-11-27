# Oversight: the operator reads every person's agent conversations, read-only and audited

## Purpose / Big Picture

An Administrator can list every person's runs with `agent:act` and open them,
on the dashboard, the CLI and both phone apps. But `agent:act` also lets them
speak into a run as that person, reading leaves no audit row (only a
server-log line), people's main and named conversations cannot be listed, and
there is no time range, no "running now" and no list of failed turns.

After this change:

- A separate read-only permission, `agent:observe`, lets the operator read
  every person's agent conversations: runs, main and named.
- Every read of another person's conversation writes an audit row.
- Opening one shows a note in place of the composer, so nothing can be
  typed as them by accident.
- The Oversight screen on every surface lists running now, recent (with a
  time range) and failed turns.

Chats between people stay excluded, as they are now.

## Decisions (the owner's, 2026-11-26)

- Read-only and audited. Speaking as a person stays with `agent:act`.
- Scope: runs and people's main and named conversations. Chats between people
  stay excluded.
- Opening another person's conversation in the apps is always read-only:
  `isObserved` is true even for a holder of `agent:act`, so nobody types as
  someone else by accident from a screen. Speaking as the person stays possible
  for `agent:act` through the API and the CLI, which say whose conversation it
  is.
- The lists (`ListAllAgentRuns`, `ListFailedAgentTurns`) are not audited;
  opening a conversation is. Every way of opening one writes the row: reading
  it, following its events, and asking for the previews of its links.

## Interfaces (fixed; every surface builds on these)

### Permission

- `models.PermissionAgentObserve = "agent:observe"`, a server permission: "read
  every person's agent conversations and runs, without acting; each read is
  audited".
- The Administrator role gets it, as it gets everything. The Operator role does
  not, just as it does not get `agent:act`. Add it to the Operator exclusion in
  `role.go` and to the access screen's explanations on every surface.
- `agent:act` implies observing: everywhere `agent:observe` is checked,
  `agent:act` is accepted too.

### GraphQL

- `ListAllAgentRuns` keeps its name and arguments and accepts `agent:observe`.
  It gains:
  - `since: String` and `until: String` (RFC 3339), on the conversation's
    last activity;
  - `isRunningOnly: Boolean`: only conversations with a turn in flight on any
    instance (the turn records, `ListLiveAgentTurns`);
  - `kinds` may now include `main` and `named` as well as the run kinds. When
    left out, everything but chats between people is listed.
  - Rows gain `ownerUsername`, `ownerName` and `isRunning`.
  - The page gains `totalCount`.
- `ListFailedAgentTurns(first: Int, offset: Int, since: String, agentId:
  String): AgentFailedTurnPage!` lists turns that ended in a failed note
  (`models.NoteFailed` stored in the transcript), newest first.
  - A row is `conversationId`, `conversationKind`, `conversationTitle`,
    `ownerUsername`, `ownerName`, `failedAt`, `errorText` and `agentId`.
  - The page has `failedTurns` and `totalCount`.
  - It needs `agent:observe` or `agent:act`.
- `ReadAgentConversation` (and whatever the apps use to read another person's
  transcript) accepts `agent:observe`.
  - The answer's existing `actingAs` names whose it is.
  - A new `isObserved: Boolean!` is true when the reader is not the owner. The
    clients show the read-only note when it is true.
  - Speaking into it still needs `agent:act`. With `agent:observe` alone, a
    send is refused with "Observing is read-only: only the person, or an
    operator with agent:act, can write here."
- Audit: each read of another person's conversation writes an `audit_log` row,
  action `agent.conversation.observe`, with the reader as the actor and the
  owner and conversation as the subject.
  - Reads are folded: at most one row per reader and conversation per 10
    minutes (use `TakeThrottleWait`, which waits for a parallel read holding the
    same turn instead of skipping, so two reads at once never lose the row), so
    paging a transcript does not flood the log.
  - The server-log line stays.

### CLI

- `teanode agent runs --all` gains:
  - `--since` and `--until` (durations such as `24h` or `7d`, or dates);
  - `--running`;
  - `--kind main|named|<run kind>`.
  - It prints the owner.
- `teanode agent failed-turns [--since 7d] [--agent ID]`.
- Opening another person's conversation prints a header line saying it is
  observed, read-only.

## Surfaces

- **Dashboard:** the existing agent admin page (`web/src/pages/agentAdmin.tsx`)
  gets an "Oversight" tab that replaces "every run":
  - three sections, Running now, Recent and Failed turns;
  - a range picker (24 hours, 7 days, 30 days) and a kind filter, with the
    filters in the URL;
  - pages, not load-more.
  - A row opens the conversation with a note in place of the composer: "This
    is <name>'s conversation. You are observing it and cannot write here."
- **iPhone:** in Manage, Agents, the Runs section becomes Oversight, with a
  segmented control (Running, Recent, Failed), a range menu and server
  paging. Opening a row shows the transcript with the same note in place of
  the composer. Demo data in DemoServer, and shoot lines for the list and an
  observed transcript.
- **Android:** the same, in `settings/manage/` (`AgentAdminHub.kt`,
  `AgentRunsScreen.kt`). Demo data, plus Roborazzi shots.
- **Agent tool:** none. An agent never reads other people's conversations.
- Strings in English, Simplified Chinese and Japanese on every surface.

## Validation

- Server:
  - `agent:observe` lists and reads, including main conversations;
  - a send with `agent:observe` alone is refused;
  - a member without either permission is refused;
  - chats between people never listed;
  - one audit row per reader, conversation and 10 minutes;
  - the time range, running-only and failed-turns lists;
  - the Operator role lacks `agent:observe`.
- CLI parity tests. Dashboard vitest. iPhone kit tests and a Mac build. Android
  tests on station.
