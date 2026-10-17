# Circles: chats between people, shared calendars and memory, faces, and a guardian's approval

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It is maintained in accordance with the ExecPlan rules the maintainer keeps outside this repository; everything a contributor needs is written here.

## Purpose / Big Picture

Today every account on a TeaNode server is alone. A household, a few friends or a small team on one server can only reach each other by mail, and each person's agent knows only what reached that one person. After this plan:

- People who share a **circle** (a family, friends, a team) write to each other in **chats**: directly, two people, or as a group, on the dashboard, the iPhone app and the command line, with read state, reactions, pictures and phone notifications.
- A person can bring their own agent into any chat by addressing it. The agent answers in the chat; what it did to find the answer (reading mail, looking at the calendar) stays in the owner's own conversation, and nobody else can start it unless the owner allowed that for the chat.
- A person can let the people in a circle see their calendar at a level they choose (just busy, titles, details, or edit), and a family can keep one calendar everybody writes. The agent can answer "when are we all free on Saturday".
- A person can share part of their memory (a page and everything under it, such as the children's school or a grandparent's care) with a circle or a person. The other person's agent reads it live, read only, and it disappears from them the moment it is taken back.
- People have a picture, and every agent has a drawn face that moves with what the agent is doing: thinking, speaking, using a tool, waiting for an answer, dreaming.
- In a family circle a child can ask for something to be bought. A guardian approves or declines it from a card or a phone notification, and if they choose, their own agent buys it with their own payment skill on their own computer.

To see it working at the end: two accounts in one family circle, one an admin and one a child. The child sends "can I get the paperback of the book for school, 12.99?" to their agent in the drawer; the agent proposes a purchase approval, the child approves the card; the guardian's phone shows "Approve?"; the guardian opens the dashboard's Chats page, sees the family chat, sees the child's message about the book and the approval card in their own drawer, and approves with "have my agent buy it". The child's drawer then says the purchase was approved. Along the way the guardian's agent's face is shown thinking, then working, then waiting while Link asks the guardian.

The design reasons are in six decision records, all dated 2026-10-16 under `docs/decisions/`: `people-on-one-server-meet-in-circles`, `a-chat-belongs-to-the-people-in-it`, `shared-knowledge-is-read-where-it-lives`, `a-calendar-is-shared-at-a-level`, `a-guardian-answers-and-pays-from-their-own-agent`, and `the-agents-face-is-drawn-and-moves-with-its-run`. They are proposed, not accepted, until the maintainer agrees with them; the first implementation pull request marks them accepted or records what changed.

## Progress

- [x] (2026-10-16) Mapped accounts, access groups, conversations, streaming, push, cards, memory, calendar, the Link skill and avatars; wrote the six decision records and this plan.
- [ ] Maintainer review of the decisions and of the open questions in the Decision Log.
- [ ] Milestone 1: pictures for people.
- [ ] Milestone 2: circles, invitations and the circle directory.
- [ ] Milestone 3: chats between people.
- [ ] Milestone 4: the agent's face and its activity state.
- [ ] Milestone 5: agents in chats.
- [ ] Milestone 6: shared calendars and the circle calendar.
- [ ] Milestone 7: shared memory.
- [ ] Milestone 8: approvals, purchases and allowances.
- [ ] Milestone 9 (decision pending): the circle agent.

## Surprises & Discoveries

- Observation: there is no relationship between accounts anywhere in the schema. The only "group" is the access group (`group`, `user_group`, `group_role` in migration 0014), which says what an account may manage. "Household" and "family" appear only in comments.
  Evidence: `internal/db/migrations/0014_access_control.sql`, `internal/models/user.go`; a search for household, family, team and organization in `internal/models` and `internal/db`.

- Observation: a card can only be answered by the account that owns the agent. `requireAgentPerson` in `internal/api/v1api/apigraph/agent_ask.go` resolves the caller to their own agent, and the card is looked up under that agent. So a guardian's approval cannot be an ordinary card of the child's agent; it needs its own row and its own mutation.
  Evidence: `agent_ask.go` (`requireAgentPerson`, `ResolveAgentConfirmation`), `agent_interaction.go` (`answerInteraction`).

- Observation: payments run on the person's own computer as the person. The `link` skill (in the separate skills registry, `skills/link/skill.md`) calls `link-cli`, never asks for card numbers, never passes `--approve`, and every spend request waits for the person in the Link app. This is what makes "give the child the parent's Link" wrong and "the guardian's agent pays" right.

- Observation: the run events already reduce to an activity state. `activityStateOf` in `internal/agent/live_activity.go` maps `confirmation`, `question` and `locate` to waiting and `text`, `message`, `tool_call` and `tool_result` to working, for the iPhone's Live Activity. The face's states extend this one function rather than inventing a second mapping.

- Observation: phone notifications target an agent, not a person (`notifyPhones(agentId, ...)` in `internal/agent/phone_push.go`, phones in `agent_phone` keyed by agent). A chat message to somebody whose agent is off would reach no phone. Milestone 3 has to make push target a person's phones whether or not their agent is on.

- Observation: `chat` is already a word here, for chat archives read as a knowledge source (`internal/agent/ingest_document.go`, the `mattermost-mm` source type). A TeaNode chat read by an agent is also a chat being read as a source, so the word stays and means the same idea; the source kind for it is `chat` as well, with the chat's id.

## Decision Log

- Decision: the word for a set of people is **circle**, with `circle_kind` `family`, `friends` or `work` and `member_role` `admin`, `member` or `child`. Admins of a family circle are the child's **guardians**.
  Rationale: `group` is the access group and must not mean two things. `household` does not fit colleagues. One word with a kind covers the three cases the request names. See `20261016-people-on-one-server-meet-in-circles.md`.
  Date/Author: 2026-10-16, design.

- Decision: chats are new tables, not `agent_conversation` rows, and an agent's turn in a chat runs in the agent's own conversation of the new kind `chat`, with only its answer posted to the chat.
  Rationale: a conversation is one agent's and holds its tool results; a chat is several people's. Keeping the working transcript with the owner is what stops one person's mail reaching the others through a tool result. See `20261016-a-chat-belongs-to-the-people-in-it.md`.
  Date/Author: 2026-10-16, design.

- Decision: only a person's message starts an agent's turn in a chat, and by default only the agent's owner can start it.
  Rationale: no agent-to-agent loops, no other member steering somebody's agent into their mail. An owner may let others ask, chat by chat, and those turns are held to what all members can see.
  Date/Author: 2026-10-16, design.

- Decision: memory is shared as a live read-only view of a subtree of pages, never copied.
  Rationale: a copy is stale the next day and survives being taken back. See `20261016-shared-knowledge-is-read-where-it-lives.md`.
  Date/Author: 2026-10-16, design.

- Decision: a calendar is shared at one of four levels (`busy`, `titles`, `details`, `edit`); private events are busy to everybody else; a family circle may own a calendar.
  Rationale: the three things people ask of each other's calendars are different, and free-busy already exists as one piece of code. See `20261016-a-calendar-is-shared-at-a-level.md`.
  Date/Author: 2026-10-16, design.

- Decision: a child's purchase is an **approval** answered by a guardian; the guardian's own agent pays with the guardian's own tools.
  Rationale: see `20261016-a-guardian-answers-and-pays-from-their-own-agent.md`.
  Date/Author: 2026-10-16, design.

- Decision: the agent's face is a drawn, parametric description, not a generated image; a person's avatar is an uploaded picture with an initial as the fallback.
  Rationale: it can move with the run, it is the same on every surface, and it needs no image model. See `20261016-the-agents-face-is-drawn-and-moves-with-its-run.md`.
  Date/Author: 2026-10-16, design.

- Decision: the milestones ship in the order of this plan, each as its own pull request, each on every surface (dashboard, agent tools, command line, iPhone app) before the next starts.
  Rationale: `CONTRIBUTING.md` requires parity per capability. Pictures come first because every later screen shows people; circles come before chats because a chat's members are drawn from circles; faces come before agents in chats because a chat with two agents needs them told apart.
  Date/Author: 2026-10-16, design.

- Open question for the maintainer: may a guardian read a child's chats? Proposed answer: no. A guardian sees the child's approvals, which circles and chats the child is in, and the child's agent's settings, but not the messages. Reading a child's messages is the kind of thing a family decides for itself, and a server that does it silently teaches the child to use another app.

- Open question for the maintainer: should a circle have an agent of its own (Milestone 9)? Proposed answer: not in the first release. A circle agent would own a graph the members write together and answer anyone in the circle chat, and it needs `agent.user_id` to become optional, which every agent code path assumes. The shared views of Milestone 7 cover reading each other's knowledge without it.

- Open question for the maintainer: when an account is deleted, what happens to what it wrote in chats? Proposed answer: the messages stay, shown as from "a former member", because the other people's replies make no sense without them; the author link is cleared and the picture removed.

- Open question for the maintainer: chats with people on other servers. Proposed answer: out of scope. A circle is one server's accounts; reaching anyone else stays mail.

## Outcomes & Retrospective

Nothing yet.

## Context and Orientation

TeaNode is a Go program (`cmd/teanode-server`, the server, and `cmd/teanode`, the command line) with a React dashboard (`web/`) embedded in the server binary and a SwiftUI iPhone app (`ios/`). Start with `AGENTS.md` and `CONTRIBUTING.md`. The parts this plan touches:

**Accounts.** `internal/models/user.go` defines `User` (table `user`, migration 0004 and later): `ID` (a ULID), `Username`, `Name`, `Email` (for notifications, not a mailbox), `Locale`, `Timezone`, `ContactID` (the person's own card in their address book), and `GroupIDs` (access groups). There is no picture. The dashboard draws an initial in a `span.avatar` (`web/src/components/accountMenu.tsx`, `web/src/pages/access/users.tsx`).

**Access.** `internal/access` holds groups, roles and permissions. Permissions are listed in `internal/models/permission.go` (`agent:use`, `calendar:use`, `contacts:use` and so on); a migration seeds a new one into roles, the way `0051_calendar.sql` adds `calendar:use` to Administrator, Operator and Member with `INSERT ... SELECT "id", 'calendar:use' FROM "role" WHERE lower("name") IN (...)`. Every GraphQL resolver must call one of the helpers in `internal/api/v1api/apigraph/authorize.go` (`requireSignedIn`, `requirePermission`, `requireAccount` and others); `TestEveryOperationAuthorizes` fails otherwise. A refusal answers "not found", never "forbidden", so a resolver does not reveal whether something exists.

**The agent.** One `agent` row per account (`internal/models/agent.go`, unique `user_id`). Its conversations are `agent_conversation` rows (`internal/models/insight.go`, `AgentConversationKind`: `main`, `named`, `run`, `goal`), its messages `agent_message` rows with roles `user`, `assistant`, `tool`, `note`, `compaction`. A turn starts with the `AskAgent` mutation (`internal/api/v1api/apigraph/agent_ask.go`) which calls `Agent.Ask` (`internal/agent/ask.go`) with `AskSettings`, including the `Surface` (where the person is talking from; `internal/agent/surface.go` describes each). A run emits events (`asked`, `text`, `message`, `tool_call`, `tool_result`, `confirmation`, `question`, `note`, `reacted`, `error`, `done`), delivered to the dashboard and the app by GraphQL subscriptions over a websocket (`AgentConversationEvents`, `AgentRunEvents`) and across server instances by PostgreSQL `NOTIFY` on the channels in `internal/db/database.go` (`AgentEventChannel`, `AgentCommandChannel`). `docs/subsystems/streaming-and-instances.md` explains it.

A **card** is a tool call waiting for the person's word: an `agent_interaction` row (migration 0113, `models.AgentInteraction`), answered with `ResolveAgentConfirmation` or `AnswerAgentQuestion`. Which kinds of call need a card is the call's **risk**: `outward` (speaking for the person), `money`, `destructive`, `granting` (giving somebody access) and `listed`. `docs/decisions/20261007-the-person-chooses-what-the-agent-does-alone.md` explains when a turn with nobody present may go ahead.

Text from anybody but the person reaches the model wrapped in `<untrusted-data>` with a line saying it is data, never an instruction (see `internal/agent/ask.go` and `internal/agent/alert.go`). Chat messages from other members are wrapped the same way.

**Phones.** `agent_phone` rows (migration 0160) are the person's iPhone installs, keyed by agent. `internal/agent/phone_push.go` sends: `pushOwnTurns` (the agent's own messages), `notifyInteraction` (cards), `notifyAnswer`. Payloads are sealed per phone (`internal/push/payload.go`, `push.Content` with a `NotificationKind`) and sent through the relay to Apple (`internal/push`). The app decrypts them in `ios/TeaNodeNotification/NotificationService.swift`.

**Memory.** The graph (`docs/subsystems/memory.md`): `agent_node` rows are pages (`models.AgentNode`: `AgentID`, `Path` such as `people/a-friend`, `Name`, `Summary`, `Overview`), `AgentFact` rows are numbered sentences on a page with evidence, `AgentEdge` rows join pages. Everything is keyed by `agent_id`. The `memory` tool (`internal/agent/tools/memory`) and the Knowledge page (`web/src/pages/knowledge.tsx`) read it. The **dream** is the background run that reads sources and rewrites pages.

**Calendar.** `docs/subsystems/calendar.md`: `calendar` rows (one `events` and one `reminders` per person, `calendar_kind`, unique per person and kind) and `calendar_object` rows (one iCalendar file each), served over CalDAV by `internal/dav` at `/dav/{userId}/calendars/{calendarId}/`. Free-busy is `calendar.FreeBusy` and `calendar.Free` in `internal/calendar`, used by the terminal, the agent and CalDAV alike.

**Storage.** `internal/storage` keeps file bytes in the spool's `media` directory or an S3 bucket (`Files`: `PutFile`, `GetFile`, `DeleteFile`).

**Surfaces.** The command line's subcommands are in `internal/cmd`, one file per group, and go through the same GraphQL operations as the dashboard. The agent's tools are packages under `internal/agent/tools/`, registered in `internal/agent/tools/all/all.go`. Dashboard pages are in `web/src/pages`, shared components in `web/src/components`, and `docs/coding/frontend-design.md` says which to use. The iPhone app's screens are in `ios/TeaNode/Sources`, its GraphQL in `ios/TeaNodeKit/Sources/TeaNodeKit/Operations.swift`, its design rules in `ios/DESIGN.md`.

Migrations are numbered SQL files in `internal/db/migrations` with a matching `.reverse.sql` each (`docs/coding/database-migrations.md`). At the time of writing `main` ends at 0190, and an open pull request takes 0191; every milestone renumbers to whatever `main` has when it opens.

Terms this plan introduces, each used with one meaning everywhere (tables, Go, GraphQL, command line, tools, screens):

- **circle**, **circle kind**, **member role**, **guardian**, **invitation**: see the first decision record.
- **chat**: a conversation between people. **direct chat** (two people) and **group chat**; the **circle chat** is the group chat every circle has. A **chat message** is written by a person or by an agent.
- **face**: the agent's drawn picture. **activity state**: what the agent is doing, which the face shows.
- **avatar**: a person's picture.
- **share**: a row that lets a circle or a person read something of somebody else's. A **calendar share** has a **share level**; a **page share** covers a path and everything under it.
- **approval**: a child's request to their guardians, with an **approval kind** and an **approval state**. An **allowance** is a monthly amount a child's approved purchases count against.

## Plan of Work

Each milestone is one pull request. Each lands on all four surfaces, follows the closest existing feature (named in the milestone), and ends with the screens walked at phone and desktop widths in light and dark and the `ux-review` checklist run. The migration numbers are written as `NNNN` because they are taken when the pull request opens.

### Milestone 1: pictures for people

At the end of this milestone every person can upload a picture, and every place that shows a person (the account menu, the users page, and later the chats) shows it, or their initial on a color of their own. The closest existing feature is the contact photo (`internal/contacts/contacts.go` `Photo`, served by `internal/api/v1api/apigraph/contact_photo.go`).

Add migration `NNNN_user_avatar.sql`: `user.avatar_file_id varchar(64) NULL` and `user.avatar_updated_at timestamptz NULL`. The picture is stored through `storage.Files` under an id of the form `avatar-user-<ULID>`.

Add `internal/avatar` with `Normalize(content []byte) ([]byte, error)`: decode PNG or JPEG with the standard library (WebP is not vendored, so the dashboard and the iPhone app convert WebP and HEIC to JPEG in the browser or on the phone before upload), refuse anything over 20 megapixels before decoding the pixels (read the dimensions with `image.DecodeConfig` first, so a small file that decodes to a huge image is refused cheaply), crop to the center square, scale to 512 by 512, and encode as PNG. Re-encoding drops every metadata block, including the location a phone writes into a photo. Also add `InitialColor(userId string) int`, an FNV-1a hash of the id modulo the 12 avatar colors defined in `web/src/style.css` as `--avatar-0` to `--avatar-11`, and the same function in Swift (`ios/TeaNodeKit/Sources/TeaNodeKit/AvatarColor.swift`) with a shared test vector (three ids and their expected index) in both test suites.

GraphQL: `SetAccountAvatar(content: String!)` (base64, at most 8 MiB, like an attachment upload), `RemoveAccountAvatar`, and `avatarUrl` on the session and on every place a person is returned. The picture is served by an HTTP handler at `/api/v1/avatar/user/{userId}?size=64|512` beside the contact photo handler, with an ETag of the update time, readable by the person, by anyone who shares a circle with them (from Milestone 2; until then only the person and operators with `user:manage`), and refused as not found otherwise.

Surfaces: the dashboard's account settings page gets a picture row (choose, crop preview, remove) and `web/src/components/avatar.tsx` replaces the hand-drawn `span.avatar` everywhere; the command line gets `teanode account avatar set <file>` and `teanode account avatar remove`; the agent's `account` tool gets `set_avatar` taking an attachment id from the conversation ("use this photo as my picture"), which is the person's own change and needs no card; the iPhone app's account profile screen (`AccountProfileViews.swift`) gets a picture row using `PhotosPicker`.

Acceptance: upload a photo with location metadata from the dashboard; the account menu shows it; `curl` the avatar URL with the person's token and `exiftool` the result: no GPS fields. Remove it; the initial is back on the same color on the dashboard and the app.

### Milestone 2: circles

At the end of this milestone a person can make a circle, invite people by username, accept or decline invitations, see the members, change roles if they are an admin, and leave. Nothing is shared yet; the circle is the list later milestones share with. The closest existing feature is access groups (`internal/agent/tools/people/people.go` `group_manage`, `web/src/pages/access`).

Migration `NNNN_circle.sql`:

    circle: id varchar(26) primary key, created_at, modified_at, name varchar(100) not null,
            circle_kind varchar(16) not null check in ('family','friends','work'),
            created_by_user_id varchar(26) null references "user" on delete set null
    circle_member: circle_id references circle on delete cascade,
            user_id references "user" on delete cascade, member_role varchar(16) not null
            check in ('admin','member','child'), joined_at, invited_by_user_id null,
            primary key (circle_id, user_id)
    circle_invitation: id, circle_id references circle on delete cascade,
            invited_user_id references "user" on delete cascade, invited_by_user_id,
            member_role, created_at, expires_at, answered_at null,
            invitation_answer varchar(16) null check in ('accepted','declined','withdrawn')

plus the permission `circle:use` seeded into Administrator, Operator and Member, and a check (in Go, in one transaction holding the circle's row) that a `child` member is only in a `family` circle, that a circle always keeps at least one admin, and that a child has at least one guardian.

`internal/models/circle.go` defines `Circle`, `CircleMember`, `CircleInvitation`, `CircleKind`, `MemberRole` and `InvitationAnswer`. `internal/db/database_circle.go` holds the queries, including `SharesCircle(userId, otherUserId string) (bool, error)` and `CircleUserIDs(userId string) ([]string, error)` (everybody the person shares a circle with), which every later milestone uses to authorize.

GraphQL operations: `ListCircles`, `ReadCircle`, `CreateCircle`, `RenameCircle`, `DeleteCircle` (admins), `InviteToCircle(circleId, username, memberRole)`, `ListCircleInvitations` (to me), `AnswerCircleInvitation(invitationId, accept)`, `WithdrawCircleInvitation`, `SetCircleMemberRole`, `RemoveCircleMember`, `LeaveCircle`, and for operators `AddCircleMember(circleId, userId, memberRole)`. `requireCircleMember(context, circleId) (*models.Circle, *models.CircleMember, error)` and `requireCircleAdmin` join the helpers in `authorize.go`. `InviteToCircle` answers the same whether or not the username exists or is already a member; an invitation to nobody is never stored. A child's attempt to accept an invitation or leave returns a refusal that says a guardian has to approve it (Milestone 8 turns that into an approval).

The invited person learns of an invitation as a note in their main conversation (a new `AgentNoteKind`, `circle_invited`, when their agent is on), as a phone notification of the new kind `circle` (see Milestone 3 for push without an agent; until then only people whose agent is on get it), and on the dashboard as a badge on Circles.

Surfaces: dashboard page `web/src/pages/circles.tsx` at `/circles` and `/circles/{id}` (members with avatars and roles, invite, pending invitations, leave); command line `internal/cmd/circle.go`: `teanode circle list|show|create|rename|delete|invite|invitations|accept|decline|withdraw|role|remove|leave`; agent tool `internal/agent/tools/circle` with actions `list`, `show`, `invite` (risk `granting`), `answer` (risk `granting`), `leave` (`destructive`); iPhone app `CirclesView.swift` under Settings.

Acceptance: as person A, `teanode circle create --kind family "Home"`, `teanode circle invite <circle> personb --role member`; as person B, `teanode circle invitations` lists it, `teanode circle accept <invitation>`; `teanode circle show <circle>` as either lists both with roles. As A, invite a username that does not exist: the answer is the same as before and `teanode circle show` lists no pending invitation for it. Tests in `internal/api/v1api/apigraph/circle_test.go` cover: a non-member reading a circle gets not found; the last admin cannot leave or be demoted; a child cannot be added to a `friends` circle; removing a member removes nothing else yet (later milestones extend this test).

### Milestone 3: chats between people

At the end of this milestone people who share a circle can write to each other in direct and group chats on every surface, see who has read what, react, attach pictures, and get a phone notification. No agent takes part yet. The closest existing feature is the agent's conversation and its feed (`internal/models/insight.go`, `internal/agent/feed.go`, `web/src/components/agentDrawer.tsx`, `ios/TeaNode/Sources/ConversationView.swift`); the chat copies the drawer's message list, reply bar and reactions rather than inventing new ones.

Migration `NNNN_chat.sql`:

    chat: id, created_at, modified_at, chat_kind check in ('direct','group'),
          title varchar(100) null, circle_id null references circle on delete cascade,
          created_by_user_id null, last_message_at null,
          direct_key varchar(53) null unique   -- the two user ids, sorted, joined by ':'
    chat_member: chat_id references chat on delete cascade, user_id references "user"
          on delete cascade, member_role check in ('admin','member'), joined_at,
          read_through_message_id varchar(26) null, muted_until timestamptz null,
          is_agent_reading boolean not null default false,
          is_agent_askable_by_others boolean not null default false,
          primary key (chat_id, user_id)
    chat_message: id varchar(26) primary key (a ULID, so ids sort by time), chat_id
          references chat on delete cascade, created_at,
          author_user_id null references "user" on delete set null,
          author_agent_id null references agent on delete set null,
          agent_run_id varchar(26) null, body text not null, attachments jsonb not null
          default '[]', reply_to_message_id null, edited_at null, deleted_at null
    chat_reaction: message_id references chat_message on delete cascade,
          reactor_user_id null, reactor_agent_id null, emoji varchar(32) not null,
          reactor_key varchar(40) not null,   -- 'user:<id>' or 'agent:<id>'
          created_at, primary key (message_id, reactor_key)

with an index on `chat_message (chat_id, id)`. A direct chat is unique per pair through `direct_key`, so asking for the chat with somebody twice returns the same chat. A circle's circle chat is made with the circle and its membership follows `circle_member` in the same transactions that change the circle. A group chat's members must each share a circle with the person who adds them (`SharesCircle`). The `is_agent_*` columns exist now so Milestone 5 needs no second migration of this table; nothing reads them yet.

`models.ChatMessage` gets a `ValidateChatMessageBody` (at most 16,000 characters, not empty unless there are attachments). Attachments reuse the agent's attachment upload path: a picture is uploaded first (`UploadChatAttachment`, the same size limits as `agent_attachment`), and the message references the stored file ids. A per-person rate limit (60 messages a minute, counted in memory per instance) stops a runaway script.

Live delivery: a new `NOTIFY` channel `ChatEventChannel` in `internal/db/database.go` carrying only `{chatId, messageId, eventKind}` (never a body), and `internal/chat/feed.go`, a per-instance fan-out like the agent's feed. Subscriptions: `ChatEvents(chatId)` (messages, edits, deletions, reactions, read marks, typing) and `ChatListEvents` (any chat of mine changed: new message, unread count), both checked against `chat_member` at subscribe time and on every event, so a person removed from a chat stops receiving it at once.

Read state: `MarkChatRead(chatId, throughMessageId)` moves `read_through_message_id` forward only. `ListChats` returns each chat with its unread count and last message.

Phones: add `push.NotificationKind` `chat`, and make push target a person. `agent_phone` is keyed by agent; rather than move the table, `notifyPersonPhones(userId, content)` in a new `internal/push/person.go` looks up the person's agent and its phones, and a person without an agent row gets one made disabled (an `agent` row with `enabled = false` already holds phones without the agent being on; check `PairPhone` in `internal/api/v1api/apigraph` does not require `enabled`, and make it not require it if it does). A message notifies every member but its author who is not watching the chat (presence, `internal/agent/presence.go`, extended with the chat being viewed) and has not muted it; the notification's thread id is the chat id so iOS groups them.

GraphQL operations: `ListChats`, `ReadChat` (newest page first, `before` for older, like `ReadAgentConversation`), `OpenDirectChat(userId)`, `CreateGroupChat(title, userIds)`, `RenameChat`, `AddChatMember`, `RemoveChatMember`, `LeaveChat`, `SendChatMessage(chatId, body, attachmentIds, replyToMessageId)`, `EditChatMessage`, `DeleteChatMessage` (own messages; admins of a group chat may delete any), `ReactToChatMessage(messageId, emoji)` (empty emoji removes), `MarkChatRead`, `MuteChat(chatId, until)`, `SetChatTyping(chatId)` (an event only, nothing stored). `requireChatMember` joins `authorize.go`.

Surfaces: dashboard page `web/src/pages/chats.tsx` at `/chats` (a list of chats with avatars, last message and unread count) and `/chats/{id}` (the message list, with each message's author's avatar and name above a run of their messages, read marks, reactions, reply, attach, edit and delete in the hover actions as the drawer has them), and a Chats entry in the side menu with the unread total; command line `internal/cmd/chat.go`: `teanode chat list|show|direct <username>|create|rename|add|remove|leave|send|edit|delete|react|read|mute|watch`, where `watch` follows `ChatEvents` until interrupted; agent tool `internal/agent/tools/chat` with `list`, `read` (the person's chats are the person's to read), and `send` (posts as the agent, marked as the agent's, risk `outward`, so it is a card unless the person allowed outward calls unattended: "tell the family chat I'll be late"); iPhone app: a Chats tab in `AppShell.swift`, `ChatsView.swift` and `ChatView.swift` reusing `PersonBubble`, `ReactionBadges` and `ReplyBar` from `ConversationView.swift`, with the author's avatar on others' messages and notification actions to open the chat.

Acceptance: A and B share a circle. A runs `teanode chat direct personb` and `teanode chat send <chat> "dinner at 7?"`; B's dashboard shows the chat at the top of `/chats` with one unread, live, without a reload; B's phone, with the app in the background, shows "A: dinner at 7?"; B opens it, and A's `teanode chat show <chat> --json` shows B's read mark on the message. C, who shares no circle with A, gets not found for `OpenDirectChat` with A's id. Removing B from the circle removes them from the circle chat and, in the same transaction, from any group chat whose other members no longer share a circle with them; their direct chat with A stays readable to both but cannot be written to (the send is refused with "you no longer share a circle"). Tests cover each of these in `internal/api/v1api/apigraph/chat_test.go` and the feed's removal on membership change in `internal/chat/feed_test.go`.

### Milestone 4: the agent's face and its activity state

At the end of this milestone every agent has a drawn face, shown in the drawer's header, beside the agent's messages, on the iPhone's conversation screen and Live Activity, and it moves with what the agent is doing. The closest existing feature is the Live Activity's state (`internal/agent/live_activity.go`, `ios/TeaNodeWidgets/TeaNodeWidgets.swift`).

The face description, `models.AgentFace`, stored as `agent.face jsonb null` (migration `NNNN_agent_face.sql`):

    type AgentFace struct {
        FaceCreature   string `json:"faceCreature"`   // one of AgentFaceCreatures
        BodyColor      string `json:"bodyColor"`      // one of the palette tokens
        AccentColor    string `json:"accentColor"`
        FaceAccessory  string `json:"faceAccessory"`  // none, glasses, hat, headphones, bow, scarf, sprout
        EyeStyle       string `json:"eyeStyle"`       // dot, round, sleepy, bright
        Expression     string `json:"expression"`     // calm, cheerful, curious, serious
        Version        int    `json:"version"`        // of the drawings it was made for
    }

The creatures are a fixed set drawn for this program: teacup, leaf, cat, fox, owl, bear, rabbit, robot, cloud, and plain round. Each drawing is an SVG built on a shared rig: the same named groups (`body`, `eye-left`, `eye-right`, `brow-left`, `brow-right`, `mouth`, `accessory`, `badge`) at the same anchors, so every animation works on every creature. The drawings live in `web/src/agentFace/drawings.json` (path data and anchors, no styling) and are copied into `ios/TeaNode/Resources/AgentFaceDrawings.json`; `make check-face-drawings` (run by `make lint-ci`) fails when the two differ. Drawing the creatures is real design work; the plan expects a first set to be drawn by hand in SVG, reviewed as screenshots, and iterated before the milestone ships.

Generating a face: `internal/agent/face.go`, `Agent.DescribeFace(ctx, words string) (models.AgentFace, error)`, one structured-output call on the compact model (`internal/llm` structured output, as `acknowledge.go` makes its small call) with the schema of `AgentFace` and the enumerations as the only allowed values, given the agent's name, its instructions' first lines and the person's words if any. Anything outside the enumerations is replaced by the default for that field, never stored. `FaceFromID(agentId string) models.AgentFace` picks one deterministically for an agent with no model or before the first call. A face is made when the agent is turned on, and the welcome (`docs/planning/assistant-welcome-execplan.md`) shows it.

The activity state: `models.AgentActivityState` with `idle`, `thinking`, `speaking`, `working`, `waiting`, `failed`, `dreaming`, `away`. Extend `activityStateOf` into `internal/agent/activity.go`, used by the Live Activity and by a new per-agent fan-out: `asked` becomes `thinking`, the first `text` `speaking`, `tool_call` `working` (with the tool's family: mail, calendar, memory, web, computer, money, other, from a map beside the tool registry), `confirmation`, `question` and `locate` `waiting`, `error` `failed`, `done` `idle`; a dream's start and end (`internal/agent/dream.go`) set `dreaming` and `idle`; a disabled agent is `away`. Subscription `AgentActivityEvents(agentId)`: the owner receives the tool family; anybody else allowed to see the agent (from Milestone 5, members of a chat the agent is in) receives the state only. The state is carried on `NOTIFY` across instances as `{agentId, activityState, toolFamily}`.

Rendering: `web/src/components/agentFace.tsx` draws the SVG and animates with CSS keyframes per state (idle: a blink every few seconds and a slow breath; thinking: eyes up and three dots circling; speaking: the mouth opening with each `text` event, throttled; working: the badge shows the tool family's icon from `icons.tsx` and the eyes follow it; waiting: a raised hand or a question mark that pulses; failed: brows down, once; dreaming: eyes closed and a slow "z"; away: grey), with `prefers-reduced-motion` turning motion off and showing the state as a short word beside the face. The iPhone app's `AgentFaceView.swift` draws the same paths with SwiftUI `Path` and `TimelineView`, honoring Reduce Motion. The Live Activity shows a still frame per state.

Changing it: GraphQL `ReadAgentFace`, `DescribeAgentFace(words)` (returns a new face without saving), `SetAgentFace(face)`. Dashboard: the agent page's profile section shows the face, a "Describe it" box, and pickers for each part; command line `teanode agent face show|describe <words>|set --creature owl --accessory glasses ...`; agent tool: `agent_profile` gains `face` (show), `describe_face` and `set_face` (the person's own change, no card); iPhone: the agent settings screen gets the same.

Acceptance: turn an agent on; the drawer shows a face within a second (the deterministic one) and the described one after the model answers. Ask something that reads mail: the face thinks, then works with the mail badge, then speaks, then rests. With reduced motion on, the face is still and a word says "working". `go test ./internal/agent/ -run Face` covers the enumeration clamp and `FaceFromID` stability; `web/src/components/agentFace.test.tsx` covers the state classes; a screenshot test renders every creature in every state in light and dark for review.

### Milestone 5: agents in chats

At the end of this milestone a person can ask their agent in any chat they are in, the agent answers in the chat with its face and name, and the people in the chat see it thinking. The person can let their agent remember the chat, and can let the others in it ask their agent too. The closest existing feature is a turn in a named conversation (`internal/agent/ask.go`) and the way a background wake posts into a conversation (`docs/decisions/20260929-background-work-wakes-the-conversation-that-started-it.md`).

Add `AgentConversationChat` (`"chat"`) to `AgentConversationKind` and `agent_conversation.chat_id varchar(26) null references chat on delete cascade` with a unique index on `(agent_id, chat_id)` (migration `NNNN_agent_chat_conversation.sql`). `ChatConversation(agentId, chatId)` makes it on demand. It is not listed in the drawer's picker; the owner reaches it from the chat ("what my agent did").

Starting a turn: in `SendChatMessage`, after the message is stored and published, `internal/chat/agents.go` decides which agents to start, by these rules and no others. The message's author is a person. For each agent mentioned in the body by its name (an `@` mention resolved on the client into a structured `mentionedAgentIds` list sent with the message, so a name in ordinary prose never starts a turn) or whose message this one replies to: if the agent's owner is the author, start an **owner turn**; if the owner is a member of the chat and set `is_agent_askable_by_others` for it, start a **member turn**; otherwise post nothing and tell the author, as a note visible to them only, that only the owner can ask that agent here. An agent's own message never starts anything.

Both turns call `Agent.Ask` with the new surface `chat` (in `internal/agent/surface.go`: no suggested replies, no pages to show, reactions drawn), the chat conversation, and a message that carries the recent chat: the newest 40 chat messages before and including the asking one, each as `<name>: <text>`, with every message not written by the owner inside `<untrusted-data>`, and a line saying how many earlier messages there are and that `chat read` with `before` reads them, so nothing is silently cut. The conduct's chat section says who is in the room (names and whether each is the owner), that the answer will be read by all of them, that it should not bring in anything private the question did not need, and that it speaks as the owner's agent.

An owner turn has the agent's usual tools and cards; its cards appear only to the owner, inline in the chat on the owner's screens and on the owner's phone. A **member turn** is held: the run gets the read-only tool set of research on mail from strangers, minus mail, memory, files, calendar, finance and every device, plus the chat tool's `read` for this chat, the shared views of Milestones 6 and 7 restricted to what is shared with every member of the chat, and web search and fetch. It can raise no card; a call that would need one is refused. This reuses the existing "held to reading or to a few named tools" run shape, which already never uses the unattended allowance.

Posting: the turn's final answer of each round that would be shown to the person (`message` events of the assistant's text, not tool rows) is written as a `chat_message` with `author_agent_id`, `agent_run_id`, and published on the chat feed; the streaming `text` deltas are published on `ChatEvents` as a provisional message that the stored one replaces, the same replace-not-append rule the drawer follows. The agent's face in the chat shows its activity state from `AgentActivityEvents`, state only for members who are not its owner.

Memory: `is_agent_reading` set by the member for their own agent makes the chat a source of that agent, kind `chat`, read by the dream like the other chat sources (`internal/agent/ingest_document.go`), from the time it was set and only messages the member could see. The chat's info panel lists, for every member, whether their agent remembers the chat. Turning it off stops reading at once; what was already learned stays, as with any source, and the Knowledge page shows the chat as that facts' evidence.

Operations: `SetChatAgentReading(chatId, isAgentReading)`, `SetChatAgentAskableByOthers(chatId, isAgentAskableByOthers)` (both for the caller's own membership), `ReadChatAgentTurn(chatId, messageId)` (the owner reads what their agent did for an answer). Dashboard: the chat's composer completes `@` names from the agents of members who allowed it and the person's own; the info panel has the two switches; the owner's answer has "what it did". Command line: `teanode chat send --ask-agent`, `teanode chat agent reading on|off`, `teanode chat agent others on|off`. The agent's `chat` tool's `send` was added in Milestone 3. iPhone: the same composer completion, switches and disclosure.

Acceptance: in the family chat, A writes "@<A's agent> what time is the dentist on Thursday?"; B and C see A's agent's face thinking, then an answer from it; A's own view has "what it did", listing the calendar read; B cannot open it (not found). B writes "@<A's agent> what is in A's inbox?": no turn starts, and B alone sees the note that only A can ask. A turns on "others may ask my agent here"; B asks the same: the turn starts held, answers that it cannot read A's mail, and A's usage shows the turn. A message from A's agent mentioning B's agent starts nothing. Tests: `internal/chat/agents_test.go` for every branch of the decision above; `internal/agent/ask_chat_test.go` that a member turn's tool list contains no mail, memory, calendar, finance or device tool and that other members' messages are inside `<untrusted-data>`; a test that an owner turn's tool result rows never appear in `chat_message`.

### Milestone 6: shared calendars and the circle calendar

At the end of this milestone a person can share their calendar with a circle or a person at a level, a family circle can have a calendar of its own, everybody sees the calendars shared with them on the dashboard, the phone (CalDAV) and the app, and the agent can find a time several people are free. The closest existing feature is the reminders list (`docs/planning/reminders-execplan.md`), which added a second calendar beside the first.

Migration `NNNN_calendar_share.sql`:

    calendar_share: id, calendar_id references calendar on delete cascade,
          grantee_user_id null references "user" on delete cascade,
          grantee_circle_id null references circle on delete cascade,
          share_level check in ('busy','titles','details','edit'),
          created_at, created_by_user_id,
          check (num_nonnulls(grantee_user_id, grantee_circle_id) = 1),
          unique (calendar_id, grantee_user_id), unique (calendar_id, grantee_circle_id)
    calendar: user_id becomes nullable; add circle_id null references circle on delete
          cascade; check (num_nonnulls(user_id, circle_id) = 1); unique (circle_id,
          calendar_kind) where circle_id is not null

A share to a person requires that the sharer and the person share a circle; a share to a circle requires that the sharer is a member. The effective level for a reader is the highest of their direct share and the shares to circles they are in. When somebody leaves a circle, the circle's shares stop reaching them by construction (the lookup joins `circle_member`), and direct shares between two people who no longer share any circle are deleted in the leave transaction.

`internal/calendar/visibility.go`: `Redact(object models.CalendarObject, shareLevel ShareLevel) models.CalendarObject`, applied in exactly one place, the database function that lists objects for a reader who is not the owner. `busy` returns only start, end and the busy judgement; `titles` adds summary and location; `details` and `edit` return the object; any level returns busy-only for `CLASS:PRIVATE` and `CLASS:CONFIDENTIAL`. The private rule joins the four judgements in `calendar.FreeBusy`'s neighborhood with a test per level and class. A circle calendar is read and written by every member at `edit`, and members with role `child` too.

CalDAV: the person's home set (`internal/dav/caldav.go`, list calendars) adds each calendar shared with them at `titles` or above and each circle calendar, at `/dav/{userId}/calendars/{calendarId}/` with the owner's name in the display name ("Family", "A's calendar"), and `current-user-privilege-set` read-only below `edit`. A write below `edit` is refused with 403 before the library sees it. `busy` shares are not listed as calendars: they are free-busy only, which the phone asks through scheduling, and the dashboard shows as blocks.

Agent: the `calendar` tool's `list` and `find` gain `whose` (a username, or `everyone` for every calendar shared with the person), answering at the shared level; a new action `free_together` takes usernames and a window and answers the times all of them are free, using `calendar.Free` over the union of their busy times. Adding an event to a circle calendar is the person's own change; adding to another person's shared calendar at `edit` is risk `outward`.

Operations: `ListCalendarShares(calendarId)`, `ShareCalendar(calendarId, granteeUserId | granteeCircleId, shareLevel)`, `UnshareCalendar(shareId)`, `CreateCircleCalendar(circleId)`, and `ListCalendars` returning shared ones with `ownerName`, `shareLevel` and `isCircleCalendar`. Dashboard: the calendar page's side list shows the person's calendars, circle calendars and shared ones, each with a color and a check to show or hide, and a Share panel on their own; the circle page offers "Make a family calendar". When a person joins a family or work circle, the dashboard and the app suggest a share once ("share your calendar with Home as: busy / titles / details / not now"). Command line: `teanode calendar share <username|circle> --level titles`, `teanode calendar shares`, `teanode calendar unshare <id>`, `teanode calendar list --whose <username>`, `teanode calendar free --with a,b --from ... --until ...` (the existing `free` command gains `--with`). iPhone: `CalendarView.swift` shows the same list and the share panel.

Acceptance: A shares at `titles` with B; B's dashboard shows A's events by title, and A's event with `CLASS:PRIVATE` as "Busy". B's phone, after adding the CalDAV account, lists "A's calendar" and refuses to save an edit to it. A raises B to `edit`; B's phone can now edit an event without guests and is refused for one with guests. B asks their agent "when are A and I both free Saturday afternoon?" and gets times consistent with `teanode calendar free`. A leaves the circle: B no longer sees A's calendar on any surface or over CalDAV.

### Milestone 7: shared memory

At the end of this milestone a person can share a page and everything under it with a circle or a person, the reader sees it under "Shared with you" on the Knowledge page and the app, and the reader's agent reads it, cites it with the owner's name, and never copies it. The closest existing feature is the memory tool's reading of a page (`internal/agent/tools/memory`) and the Knowledge page (`web/src/pages/knowledge.tsx`).

Migration `NNNN_agent_page_share.sql`:

    agent_page_share: id, agent_id references agent on delete cascade,  -- the owner's
          path varchar(255) not null,        -- the root of the shared subtree
          grantee_user_id null, grantee_circle_id null, check exactly one,
          is_quote_shared boolean not null default false,
          created_at, created_by_user_id

`internal/agent/shared_memory.go`: `SharedPagesFor(readerUserId string) ([]PageShareView, error)` (every share reaching the reader, through a direct share or a circle they are in, with the owner's name), `ReadSharedPage(readerUserId, ownerAgentId, path string)` (refuses unless some share's path is the page's path or a prefix of it at a `/` boundary), and the redaction: facts without their quote unless `is_quote_shared`, evidence named only as "from <owner name>'s <source kind>", edges only where the far page is also shared with the reader, overview and summary as they are. These are the only functions that read another agent's graph, and every reader path goes through them.

Memory tool: `get`, `search` and `index` gain an `owner` argument (a username). Without it, they read the reader's own graph as today; `search` also searches shared pages and marks each result with its owner. A citation of a shared fact is written as `people/a-grandparent#3 (shared by <name>)`; the post-turn citation linker recognizes the form. Writes (`note`, `move`, `link`, `forget`) refuse an `owner` other than the caller. The prompt's index gets a short "Shared with you" section: the shared roots with their openings, bounded to 20 lines with the count of the rest and the call that lists them, so nothing is silently cut.

Never copied: the after-conversation writer and the dream's reading refuse to store a fact whose only evidence is text from a shared page, with the error "this came from a page another person shared; cite it instead of writing it down" counted on the run like an unfound quote. The dream does not read shared pages at all.

Operations: `ListAgentPageShares` (mine), `ShareAgentPage(path, granteeUserId | granteeCircleId, isQuoteShared)` (risk `granting` for the tool), `UnshareAgentPage(shareId)`, `ListSharedAgentPages` and `ReadSharedAgentPage(ownerUserId, path)` (shared with me), and a preview, `PreviewAgentPageShare(path, grantee)`, which returns exactly what the grantee would see, shown before the owner confirms. Dashboard: a Share action on a page (with the preview), the shares listed on the page, and a "Shared with you" section in the Knowledge sidebar grouped by owner, with an owner badge on every shared page. Command line: `teanode agent memory share <path> <username|circle> [--with-quotes]`, `teanode agent memory shares`, `teanode agent memory unshare <id>`, `teanode agent memory get <path> --owner <username>`. iPhone: `KnowledgeViews.swift` gets the same section and the share sheet.

A child's share with anybody outside their family circles needs an approval (Milestone 8); until Milestone 8 lands, it is refused.

Acceptance: A shares `people/a-grandparent` with the family circle; B's Knowledge page lists it under A's name with its facts and no quotes; B asks their agent "when is grandma's next appointment?" and the answer cites `people/a-grandparent#N (shared by A)`; B's own graph gains no fact quoting it (`teanode agent memory get people/a-grandparent` as B finds nothing, or B's own page without that fact). A unshares; B's next question gets "I don't know" and the Knowledge page no longer lists it. Tests: `internal/agent/shared_memory_test.go` for prefix matching at `/` boundaries (`people/a` does not share `people/ab`), quote redaction, edge redaction, circle membership changes; the memory tool's refusal of writes with `owner`; the writer's refusal of shared-only evidence.

### Milestone 8: approvals, purchases and allowances

At the end of this milestone a child can ask their guardians for a purchase, and for the other things a child may not do alone, from every surface or through their agent; a guardian answers from a card in their drawer, a phone notification, the dashboard's Approvals page, the command line or the app; and a guardian can have their own agent buy it. The closest existing feature is the card (`agent_interaction`, `internal/agent/interaction.go`) and the way an answered card resumes a turn (`Agent.ResumeInteraction`).

Migration `NNNN_circle_approval.sql`:

    circle_approval: id, created_at, modified_at, circle_id references circle on delete
          cascade, requester_user_id references "user" on delete cascade,
          approval_kind check in ('purchase','join_circle','leave_circle','share_outside',
          'widen_unattended'),
          approval_state check in ('pending','approved','declined','withdrawn','expired',
          'bought','buy_failed'),
          approval_title varchar(200) not null, approval_reason text not null default '',
          amount_minor bigint null, currency_code varchar(3) null, merchant_name
          varchar(200) null, item_url text null, approval_arguments jsonb not null default
          '{}',      -- for the non-purchase kinds: the invitation id, the share, the risks
          requester_conversation_id null, decided_by_user_id null, decided_at null,
          decision_note text null, buyer_run_id null, expires_at not null
    circle_allowance: circle_id, child_user_id, amount_minor bigint, currency_code,
          primary key (circle_id, child_user_id)

`internal/circle/approval.go`: `RequestApproval`, `AnswerApproval(approvalId, guardianUserId, answer, note)` (a conditional update from `pending`, so two guardians answering at once cannot both win; the loser is told who answered), `WithdrawApproval`, and `ExpireApprovals` (a sweep in the worker; seven days by default). Answering a non-purchase kind performs the thing it asked for in the same transaction (accepts the invitation, makes the share, sets the risks), so an approved request never needs a second step.

A purchase answered `approved` with `buyer` = `agent` calls `Agent.Ask` on the guardian's agent with surface `approval`, in the guardian's main conversation, with a message built from the approval's fields inside `<untrusted-data>` (the child wrote the title and reason) and a line from the server saying the guardian approved this purchase of this amount at this merchant and that paying goes through the guardian's payment skill, which will ask again. The turn's cards are the guardian's own as usual. When it ends, the approval becomes `bought` or `buy_failed` from the run's outcome as the guardian marks it ("did it go through?" on the card, because only the guardian and Link know); a guardian who buys it themselves marks it `bought` directly.

Telling people: a new `AgentNoteKind` `approval_requested` in each guardian's main conversation, rendered as a card with the approval's fields and the three answers (decline, approve and buy it myself, approve and have my agent buy it), pushed with `NotificationKind` `approval` to each guardian's phones through `notifyPersonPhones`, with Approve and Decline actions that open the app to confirm with Face ID before answering (a lock screen tap must not spend money). The child hears the outcome through a background wake of the conversation the request came from (`requester_conversation_id`), or a note in their main conversation, and a phone notification.

The child's agent: the new `approval` tool, `request` (risk `outward`: it is the child asking on their own behalf, so the child sees a card first; never unattended for a child), `list` and `withdraw`. The refusals added in Milestones 2 and 7 for children (accepting an invitation, leaving, sharing outside the family) now say "this needs a guardian's approval" and offer to request one. A child's agent's `set_unattended` becomes an approval of kind `widen_unattended`. A guardian's agent's `approval` tool can `list` the approvals waiting for the guardian and read one, and cannot answer: answering is a person's act, by the same reasoning that keeps the unattended setting on a card the person answered.

Allowance: `SetCircleAllowance(circleId, childUserId, amountMinor, currencyCode)` by a guardian; `ReadCircleAllowance` shows the month's approved total against it to the child and the guardians. It approves nothing in this design.

Operations: `RequestCircleApproval`, `ListCircleApprovals` (mine as requester, or waiting for me as guardian), `ReadCircleApproval`, `AnswerCircleApproval(approvalId, answer: decline|approve_self|approve_agent, note)`, `WithdrawCircleApproval`, `MarkCircleApprovalBought(approvalId, isBought)`, `SetCircleAllowance`, `ReadCircleAllowance`. Dashboard: `/approvals` (a guardian's waiting list and history, a child's own requests), the card in the drawer, the allowance on the circle page. Command line: `teanode approval list|show|request|approve|decline|withdraw|bought` and `teanode circle allowance set|show`. iPhone: `ApprovalsView.swift`, the notification actions, the card.

Acceptance: the end-to-end story in Purpose / Big Picture, plus: a second guardian answering after the first gets "already answered by <name>"; the guardian's agent cannot answer an approval through any tool; a child's `teanode circle leave` returns the approval offer and a guardian's approval removes them; an approval left a week expires and the child is told. Tests in `internal/circle/approval_test.go` for the state machine and the race, `internal/agent/ask_approval_test.go` for the buyer turn's message wrapping, and `internal/api/v1api/apigraph/circle_approval_test.go` for who may read and answer.

### Milestone 9 (decision pending): the circle agent

Not to be started until the maintainer answers the open question in the Decision Log. Sketch, so the earlier milestones do not paint it into a corner: a circle may turn on an agent of its own, with a name and a face, answering any member in the circle chat, holding a graph the members' agents can write to through it, reading the circle calendar and nothing of any member's. Its usage is counted against the admin who turned it on. It needs `agent.user_id` to become nullable with `agent.circle_id` beside it, which touches every place that goes from an agent to its owner (time zone, locale, phones, sources, limits); that audit is the first step of its own plan. Nothing in Milestones 1 to 8 should assume an agent's chat messages always have an owning person beyond what `author_agent_id` already records.

## Concrete Steps

Every milestone, from the repository root, on a branch from `main`:

    git switch -c feat/<milestone-slug> origin/main
    # write the migration and its reverse; pick the next free number on main
    ls internal/db/migrations | tail -3
    make format
    make lint-ci
    make test            # needs Docker; starts PostgreSQL
    make build
    cd web && npm ci && npm run build && npx tsc --noEmit && cd ..

Then run a dev server (`docs/reference/local-development.md`), create two or three accounts with `teanode-server user add`, and walk the milestone's acceptance on the dashboard at desktop and phone widths in light and dark, on the command line with `TEANODE_PROFILE=local`, and in the iPhone simulator. The iPhone app's screenshots come from its CI run on the pull request.

The expected output of the commands is a clean `make lint-ci` ("0 issues"), every test passing, and the new tests listed in each milestone passing; a test that fails before the change and passes after is named in the pull request.

## Validation and Acceptance

Each milestone's acceptance paragraph is the bar for that milestone. Across all of them, the plan is done when the story in Purpose / Big Picture works on a running server with three accounts, end to end, on the dashboard and the iPhone app, and these hold:

- Everything shared reaches only people who share a circle with the sharer, and stops reaching them in the same transaction that ends the circle membership. One test per kind of share (chat, calendar, page) removes a member and checks every operation refuses.
- No other person can ever read a tool result of somebody's agent. A test searches `chat_message` bodies for the tool result text of an owner turn and finds nothing.
- No agent's message starts a turn, and no member's message starts somebody else's agent unless its owner allowed it for that chat.
- An approval is answered only by a guardian person, never by an agent and never twice.
- `TestEveryOperationAuthorizes` passes with every new operation, and the parity tests pass with every new command and tool name.
- The security review (`docs/security/security-review.md`) gains a section for this feature covering: membership checks on every operation and subscription, prompt injection from chat messages and approval text, avatar decoding limits and metadata removal, `NOTIFY` payloads carrying ids only, and the phone payloads sealed as before.

## Idempotence and Recovery

Every migration is additive until Milestone 6 makes `calendar.user_id` nullable; its reverse refuses to run while a circle calendar exists (it deletes nothing on its own), so a rollback after families started using one is a deliberate step: copy the circle calendars out (each event is one file, `teanode calendar show <id> --json` prints it, or a phone's CalDAV account holds a copy), delete them, then roll back. Deploys follow the repository's existing rule for migrations: back up first, and check the build includes every migration already applied on the server.

Sharing operations are idempotent by their unique keys: sharing the same thing with the same grantee again changes its level rather than adding a row. Opening a direct chat twice returns the same chat. Answering an approval twice returns the first answer.

## Artifacts and Notes

The six decision records under `docs/decisions/` dated 2026-10-16 are the design's reasons, one per decision. The face's drawings and their screenshots become an artifact of Milestone 4 and are attached to its pull request.

## Interfaces and Dependencies

No new third-party dependency is needed. Avatars are decoded with the standard library's `image/png` and `image/jpeg`; the clients convert other formats first.

New packages: `internal/avatar` (picture normalization and the initial color), `internal/circle` (membership rules, approvals, allowances; the database functions stay in `internal/db/database_circle.go` and `database_chat.go` like every other table), `internal/chat` (the feed, the agent-turn decision, the posting of answers), and agent tools `internal/agent/tools/circle`, `internal/agent/tools/chat`, `internal/agent/tools/approval`, each imported in `internal/agent/tools/all/all.go`.

Types, in `internal/models`:

    type CircleKind string        // "family", "friends", "work"
    type MemberRole string        // "admin", "member", "child"
    type ChatKind string          // "direct", "group"
    type ShareLevel string        // "busy", "titles", "details", "edit"
    type ApprovalKind string      // "purchase", "join_circle", "leave_circle", "share_outside", "widen_unattended"
    type ApprovalState string     // "pending", "approved", "declined", "withdrawn", "expired", "bought", "buy_failed"
    type AgentActivityState string // "idle", "thinking", "speaking", "working", "waiting", "failed", "dreaming", "away"
    type AgentFace struct { ... }  // Milestone 4

Functions other milestones depend on, with these signatures:

    // internal/db/database_circle.go
    func (self *Transaction) SharesCircle(userId string, otherUserId string) (bool, error)
    func (self *Transaction) CircleUserIDs(userId string) ([]string, error)
    func (self *Transaction) GuardiansOf(childUserId string, circleId string) ([]string, error)

    // internal/push/person.go
    func NotifyPersonPhones(context context.Context, userId string, content Content) error

    // internal/agent/activity.go
    func ActivityStateOf(event Event) (models.AgentActivityState, string /* tool family */, bool)

    // internal/calendar/visibility.go
    func Redact(object models.CalendarObject, shareLevel models.ShareLevel) models.CalendarObject

    // internal/agent/shared_memory.go
    func (self *Agent) SharedPagesFor(readerUserId string) ([]PageShareView, error)
    func (self *Agent) ReadSharedPage(readerUserId string, ownerAgentId string, path string) (*SharedPage, error)

Revision note (2026-10-16): first version, written from a survey of the code before any implementation.
