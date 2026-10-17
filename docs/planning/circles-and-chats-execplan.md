# Circles: chats with people in the drawer, shared calendars, memory and payments, and pictures of people

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It is maintained in accordance with the ExecPlan rules the maintainer keeps outside this repository; everything a contributor needs is written here.

## Purpose / Big Picture

Today every account on a TeaNode server is alone. A household, a few friends or colleagues on one server can only reach each other by mail, and each person's agent knows only what reached that one person. After this plan:

- People who share a **circle** (their family, friends or colleagues) write to each other in **chats**: two people directly, or a group. A chat is one more conversation in the drawer and in the iPhone app's conversation list, read, written and reacted to exactly like a conversation with the agent, with read state, pictures and phone notifications. There is no separate chat page to learn.
- A person can bring their own agent into any chat by addressing it. The agent answers in the chat; what it did to find the answer (reading mail, looking at the calendar) stays in the owner's own view of the chat, and nobody else can start it unless the owner allowed that for the chat.
- A person can let the people in a circle see their calendar at a level they choose (just busy, titles, details, or edit), and a family can keep one calendar everybody writes. The agent can answer "when are we all free on Saturday".
- A person can share part of their memory (a page and everything under it, such as the children's school or a grandparent's care) with a circle or a person. The other person's agent reads it live, read only, and it disappears from them the moment it is taken back.
- A person who holds the household's payment skill can share its paying tool with somebody in their circle, with limits: a spouse pays within them without asking, and anything above them becomes an approval the owner answers. A share can also ask the owner every time, which is what a parent sets for a child; approving runs exactly the payment that was asked, on the owner's computer.
- People have a picture, shown beside their name wherever another person sees them.

To see it working at the end: three accounts in one family circle, two adults (one holding the Link skill on their computer) and a teenager. The holder shares `link_pay` with the other adult with a limit of 100.00 a payment, and with the teenager set to ask every time. The other adult asks their agent in the drawer to pay for a 40.00 order: it runs on the holder's computer and Link asks in its app. The teenager writes in the family chat, in the drawer, "can I get the paperback of the book for school?", then asks their agent "@<agent> please buy it, 12.99"; their agent shows the purchase on a card, they approve it, and it reaches the holder as a card in their drawer and a phone notification. The holder approves; the payment runs on the holder's computer; the teenager's drawer says it went through. 

The design reasons are in five decision records, all dated 2026-10-16 under `docs/decisions/`: `people-on-one-server-meet-in-circles`, `a-chat-is-a-conversation-with-people-in-it`, `shared-knowledge-is-read-where-it-lives`, `a-calendar-is-shared-at-a-level` and `a-payment-skill-is-shared-call-by-call`. They are proposed, not accepted, until the maintainer agrees with them.

## Progress

- [x] (2026-10-16) Mapped accounts, access groups, conversations, streaming, push, cards, memory, calendar, the Link skill and avatars; wrote the five decision records and this plan.
- [x] (2026-10-16) Maintainer answered the first open questions (see the Decision Log); revised: chats live in the drawer, payment skills are shared with limits, no circle agent, kinds are family, friends and colleagues.
- [x] (2026-10-16) Simplified at the maintainer's word: a circle has admins and members, nothing else; no guardian and no child role.
- [ ] Maintainer review of the revised decisions.
- [x] (2026-10-16) Milestone 1: pictures for people.
- [x] (2026-10-16) Milestone 2: circles and invitations.
- [x] (2026-10-16) Milestone 3: chats in the drawer.
- [x] (2026-10-16) Milestone 4 (the agent's face) dropped at the maintainer's word after seeing the drawings; only people have pictures.
- [x] (2026-10-16) Milestone 5: agents in chats.
- [x] (2026-10-17) Milestone 6: shared calendars and the circle calendar.
- [x] (2026-10-17) Milestone 7: shared memory.
- [x] (2026-10-17) Milestone 8: shared payment skills.
- [x] (2026-10-17) Two security passes, a correctness pass, a words and conventions pass and a review of what the model is shown; the findings are fixed in this branch and their reasons are in the Decision Log and the Revision notes.
- [x] (2026-10-17) A parity pass: a circle named by a name two circles have is refused with the ids and sizes of the circles on every surface, and a username wins over a circle of the same name; an invitation reaches the phones of a person whose agent is off or whose server has no worker; the cards for lending a skill and sharing a page say exactly what is granted (and the memory tool has `preview`); replying to an agent's message addresses it on the server, and the command line has `--address` and `--state`; the notification kinds are named on every surface; shared pages are searched (`SearchSharedAgentPages`); Find a time and the offer to share a calendar on joining are on the dashboard and the iPhone; the conversation tool searches chats and reacts in them (`ReactInChatAsAgent`).
- [x] (2026-10-17) The security review section for this feature (`docs/security/security-review.md`, the ninth pass).
- [ ] Link skill declarations (`amountField`, `currencyField`, `amountActions`) in the skills registry (see Outcomes & Retrospective).

## Surprises & Discoveries

- Observation: there is no relationship between accounts anywhere in the schema. The only "group" is the access group (`group`, `user_group`, `group_role` in migration 0014), which says what an account may manage. "Household" and "family" appear only in comments.
  Evidence: `internal/db/migrations/0014_access_control.sql`, `internal/models/user.go`; a search for household, family, team and organization in `internal/models` and `internal/db`.

- Observation: a card can only be answered by the account that owns the agent. `requireAgentPerson` in `internal/api/v1api/apigraph/agent_ask.go` resolves the caller to their own agent, and the card is looked up under that agent. So an approval answered by somebody else cannot be an ordinary card; it needs its own row and its own mutation.
  Evidence: `agent_ask.go` (`requireAgentPerson`, `ResolveAgentConfirmation`), `agent_interaction.go` (`answerInteraction`).

- Observation: an operator with `agent:act` can already read anybody's conversation: `conversationFor` in `agent_ask.go` falls back to another person's conversation when the caller holds it. On a family server the operator is usually a parent, which is why the maintainer saw no need for a guardian role to read a child's conversations.
  Evidence: `internal/api/v1api/apigraph/agent_ask.go`, `conversationFor`; `internal/models/permission.go`, `PermissionAgentAct`.

- Observation: payments run on the person's own computer as the person. The `link` skill (in the separate skills registry, `skills/link/skill.md`) calls `link-cli`, never asks for card numbers, never passes `--approve`, and every spend request waits in the Link app. `link_pay` has three actions: `card` (a one-time card for a merchant, with `amount_cents` and `currency` in its arguments), `url` (pays an address answering HTTP 402, whose amount is not in the arguments) and `report`. So a limit can be checked before a `card` payment and cannot be before a `url` one.

- Observation: phone notifications target an agent, not a person (`notifyPhones(agentId, ...)` in `internal/agent/phone_push.go`, phones in `agent_phone` keyed by agent). Since every person who chats gets an `agent` row (switched off if they never turned it on), phones keep hanging from it and nothing about phones moves.

- Observation: `chat` is already a word here, for chat archives read as a knowledge source (`internal/agent/ingest_document.go`, the `mattermost-mm` source type), and `teanode agent chat` is the terminal's turn-by-turn talk with the agent. Both mean the same idea as a chat with people (a conversation), so the word stays; `teanode agent chat --conversation <a chat>` simply works.

## Decision Log

- Decision: the word for a set of people is **circle**, with `circle_kind` `family`, `friends` or `colleagues` and `member_role` `admin` or `member`; screens say the kind where "circle" would read oddly ("your colleagues").
  Rationale: `group` is the access group and must not mean two things. The maintainer asked whether "circle" was awkward for coworkers and settled on the kind carrying it. See `20261016-people-on-one-server-meet-in-circles.md`.
  Date/Author: 2026-10-16, maintainer.

- Decision: a circle has admins and members, and nothing else. An admin manages who is in it and its calendar; a role never decides what anybody may see of anybody else's. There is no guardian and no child role.
  Rationale: the maintainer asked to keep it simple. Every role is a rule every later feature must honor. What a child role protected is already covered: the payment share can ask the owner every time, and the operator of a family server (usually a parent) can already read any account's conversations with `agent:act` and set limits on its agent. An earlier revision had a guardian flag and a child role with approvals for joining, leaving and sharing; it was dropped.
  Date/Author: 2026-10-16, maintainer.

- Decision: a chat is shown and used as a conversation, in the drawer and the app's conversation screen, through the existing conversation operations. Storage keeps what the people and agents say once, in `chat_message`, and each member's own agent's working rows in that member's `agent_conversation` of kind `chat`; reading interleaves the two.
  Rationale: the maintainer asked to reuse the drawer and the agent conversations rather than add a new interface and navigation. One shared set of conversation rows was not possible because they would include each member's agent's tool results. See `20261016-a-chat-is-a-conversation-with-people-in-it.md`.
  Date/Author: 2026-10-16, maintainer.

- Decision: only a person's message starts an agent's turn in a chat, and by default only the agent's owner can start it.
  Rationale: no agent-to-agent loops, no other member steering somebody's agent into their mail. An owner may let others ask, chat by chat, and those turns are offered the chat and the web only: no mail, memory, files, calendar or money, and no shared calendar or shared memory either, because the owner's shares were made to particular people, not to the room that reads the answer.
  Date/Author: 2026-10-16, design.

- Decision: payments are shared as named tools of a skill, with limits per payment and per month, run on the owner's computer; above a limit, with no readable amount, or on a share set to ask every time, a call waits for the owner's approval.
  Rationale: the maintainer pointed out that a spouse, an adult member, needs to pay with the household's Link too. One mechanism covers both the spouse (limits, rarely asked) and a child (asked every time), and approving runs exactly the asked call instead of a fresh turn of the owner's agent. See `20261016-a-payment-skill-is-shared-call-by-call.md`.
  Date/Author: 2026-10-16, maintainer.

- Decision: how a shared call is built (Milestone 8). `RunShared` is a method of the agent (`(*Agent).RunShared` in `internal/agent/skill_share.go`, taking a `SharedCall` and answering a `SharedOutcome`), not a function in `internal/skills`, because it needs the owner's computers, secrets and reach; `internal/skills/share.go` holds only what the skill says about a call (the amount, the identifier rule, the limits). The call is carried out by calling the very runner the owner's own agent uses for that tool, with a run made for the owner's agent, so nothing about "on the owner's computer, as the owner" is rebuilt. A call is written as `ran` before it is carried out and changed to `failed` if it errors, so the month's total counts a call in flight and two at once cannot both pass the limit. The command judge of the owner's own agent is asked once per call: reading, changing and spending go ahead inside a share, which is the owner's word on spending; speaking for the owner, giving access, destroying something, or a judgment that could not be had make the call wait. The identifier rule is a declared `scopedIdField` with a fallback on a parameter named `spend_request_id`, and an amount declaration always wins over the fallback. A share has at most twenty calls waiting; two columns, `wake_claimed_at` and `woken_at`, make telling the grantee's conversation happen once.
  Rationale: found while building it; each is the smallest way to meet "exactly as for the owner", "the server behaves safely without the registry's Link skill being updated" and "approving twice runs once". See `docs/subsystems/skills.md` ("Sharing a skill").
  Date/Author: 2026-10-17, implementation.

- Decision: memory is shared as a live read-only view of a subtree of pages, never copied.
  Rationale: a copy is stale the next day and survives being taken back. See `20261016-shared-knowledge-is-read-where-it-lives.md`.
  Date/Author: 2026-10-16, design.

- Decision: a calendar is shared at one of four levels (`busy`, `titles`, `details`, `edit`); private events are busy to everybody else; a family circle may own a calendar.
  Rationale: the three things people ask of each other's calendars are different, and free-busy already exists as one piece of code. See `20261016-a-calendar-is-shared-at-a-level.md`.
  Date/Author: 2026-10-16, design.

- Decision: no face for the agent. A person's avatar is an uploaded picture with an initial as the fallback; an agent is shown by its name, and in a chat as "<name> is thinking" while a turn runs, the way a person is shown typing.
  Rationale: a drawn, animated face was built and the maintainer found the drawings not good enough, and asked for people's pictures only. The decision record for it was withdrawn before it was merged.
  Date/Author: 2026-10-16, maintainer.

- Decision: an agent's chat message says whose agent it is. Most agents keep the name "Agent", so two answers in one group were indistinguishable. `chat_message.author_owner_name` keeps the owner's name as the chat knew them, `AgentMessage` and the `typing` and `text` events carry it as `authorOwnerName`, and every surface words an agent as "<agent name> · <owner>'s agent", or "<owner>'s agent" while it has the default name.
  Rationale: one rule everywhere (drawer, app, command line, phone notification title, the "is thinking" line, and the model's own view of the chat) so a person never has to guess whose agent spoke.
  Date/Author: 2026-10-17, maintainer.

- Decision: no circle agent. A circle has no agent of its own and no graph its members write together.
  Rationale: the maintainer agreed to drop it for now; it would need `agent.user_id` to become optional, which every agent code path assumes.
  Date/Author: 2026-10-16, maintainer.

- Decision: when an account is deleted, its chat messages are kept, shown as from a deleted account under the name it had (`chat_member.removed_at` and `member_name` kept on the membership row; `author_user_id` cleared).
  Rationale: the maintainer agreed to a soft delete: the others' replies make no sense without what they answered.
  Date/Author: 2026-10-16, maintainer.

- Decision: people on other servers are out of scope.
  Rationale: the maintainer's answer. Reaching them stays mail.
  Date/Author: 2026-10-16, maintainer.

- Decision: the milestones ship in the order of this plan, all in one pull request (#528) at the maintainer's word, each as its own commits and on every surface (dashboard, agent tools, command line, iPhone app) before the next starts.
  Rationale: `CONTRIBUTING.md` requires parity per capability. Pictures come first because every later screen shows people; circles come before chats because a chat's members are drawn from circles.
  Date/Author: 2026-10-16, design.

## Outcomes & Retrospective

Built, on the dashboard, the command line, the agent's tools and the iPhone app: pictures for people (`internal/avatar`, `SetUserAvatar`), circles with invitations, chats as conversations in the drawer and the app with read marks, reactions and phone notifications (`internal/chat`), agents taking part in chats as their owner's, calendars shared at a level with a calendar of a circle's own, pages of memory shared as a live read-only view, and skills shared call by call with limits and an owner's approval. Migrations 0192 to 0197 carry them, and 0198 marks the token of an app that is signed in where a person taps. Milestone 4, a face for the agent, was built and dropped.

What changed from the design, and why. A member turn in a chat is offered the chat and the web and nothing else, not even the calendars and pages the owner shared, because those were shared with particular people and the answer goes to the whole room. Approving a call and lending money are accepted only from the dashboard's session or the app's own sign-in, because an agent can drive the owner's command line; `teanode agent skill approve` is gone and `decline` stays. The calls of a share outlive it, as the owner's record, and an invitation send is counted before any lookup so the count cannot be used to find out who has an account. A shared page is read without its overview or the notes on its links, which a model writes from pages that are not shared. An agent's chat message says whose agent it is, since most keep the name "Agent". Reviews of the finished branch renamed what a reader had to look up: the answer to a call is approved or declined with `answered_at` and `answer_note`, taking a chat message away is deleting it, the sharing arguments of the calendar, memory and skill tools are `with`, `share_level` and `share_id`, and a person's picture is an avatar in code and a picture on screens.

What is left. The Link skill in the skills registry must declare `amountField`, `currencyField` and `amountActions` and be signed and released there before a card payment is read as an amount; until then every call of it waits, which is safe and slow. The security review (`docs/security/security-review.md`) has its section for this feature, the ninth pass: membership checks on every operation and subscription, prompt injection from chat messages and from the reasons on waiting calls, avatar decoding limits, and the `NOTIFY` payloads that carry ids only. The decision records are still proposed, until the maintainer accepts them. Circle agents and people on other servers stay out of scope.

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

Migrations are numbered SQL files in `internal/db/migrations` with a matching `.reverse.sql` each (`docs/coding/database-migrations.md`). `main` ended at 0191 when this work started. This branch takes 0192 to 0198: `0192_user_avatar`, `0193_circle`, `0194_chat`, `0195_calendar_share`, `0196_agent_page_share`, `0197_skill_share`, and `0198_token_client_kind` (added by the second security pass). Nothing here has shipped, so the migrations were edited in place as the design changed.

Terms this plan introduces, each used with one meaning everywhere (tables, Go, GraphQL, command line, tools, screens):

- **circle**, **circle kind**, **member role**, **invitation**: see the first decision record.
- **chat**: a conversation with people in it. **direct chat** (two people) and **group chat**; the **circle chat** is the group chat every circle has. A **chat message** is written by a person or by an agent and is seen by every member. A member's **chat conversation** is their own `agent_conversation` of kind `chat` for that chat, which also holds what their own agent did there.
- **avatar**: a person's picture.
- **share**: a row that lets a circle or a person use something of somebody else's. A **calendar share** has a **share level**; a **page share** covers a path and everything under it; a **skill share** names tools of a skill and has limits.
- **approval**: the owner's answer to a call of a skill share that waits for it: above its limits, with no amount the server can read, or on a share set to ask every time. Only the owner, a person, gives it.

## Plan of Work

All milestones land in one pull request, #528, each as its own commits. Each lands on all four surfaces, follows the closest existing feature (named in the milestone), and ends with the screens walked at phone and desktop widths in light and dark and the `ux-review` checklist run. The migrations are numbered 0192 to 0197 in the order of the milestones (see Context and Orientation).

### Milestone 1: pictures for people

At the end of this milestone every person can upload a picture, and every place that shows a person (the account menu, the users page, and later the chats in the drawer) shows it, or their initial on a color of their own. The closest existing feature is the contact photo (`internal/contacts/contacts.go` `Photo`, served by `internal/api/v1api/apigraph/contact_photo.go`).

Add migration `0192_user_avatar.sql`: `user.avatar_file_id varchar(64) NULL` and `user.avatar_updated_at timestamptz NULL`. The picture is stored through `storage.Files` under an id of the form `avatar-user-<ULID>`.

Add `internal/avatar` with `Normalize(ctx context.Context, content []byte) ([]byte, error)`: decode PNG or JPEG with the standard library (WebP is not vendored, so the dashboard and the iPhone app convert WebP and HEIC to JPEG in the browser or on the phone before upload), refuse anything over 20 megapixels before decoding the pixels (read the dimensions with `image.DecodeConfig` first, so a small file that decodes to a huge image is refused cheaply), crop to the center square, scale to 512 by 512, and encode as PNG. Re-encoding drops every metadata block, including the location a phone writes into a photo. Also add `InitialColorIndex(userId string) int`, an FNV-1a hash of the id modulo the 12 avatar colors defined in `web/src/style.css` as `--avatar-0` to `--avatar-11`, and the same function in Swift (`ios/TeaNodeKit/Sources/TeaNodeKit/AvatarColor.swift`) with a shared test vector (three ids and their expected index) in both test suites.

GraphQL: `SetUserAvatar(content: String!)` (base64, at most 8 MiB, like an attachment upload), `RemoveUserAvatar`, and `avatarUrl` on the session and on every place a person is returned. The picture is served by an HTTP handler at `/api/v1/avatar/user/{userId}?size=64|512` beside the contact photo handler, with an ETag of the update time, readable by the person, by anyone who shares a circle with them (from Milestone 2; until then only the person and operators with `user:manage`), and refused as not found otherwise.

Surfaces: the dashboard's account settings page gets a picture row (choose, crop preview, remove) and `web/src/components/avatar.tsx` replaces the hand-drawn `span.avatar` everywhere; the command line gets `teanode user avatar set <file>` and `teanode user avatar remove`; the agent's `account` tool gets `set_avatar` taking an attachment id from the conversation ("use this picture as mine"), which is the person's own change and needs no card; the iPhone app's account profile screen (`AccountProfileViews.swift`) gets a picture row using `PhotosPicker`.

Acceptance: upload a picture with location metadata from the dashboard; the account menu shows it; `curl` the avatar URL with the person's token and `exiftool` the result: no GPS fields. Remove it; the initial is back on the same color on the dashboard and the app.

### Milestone 2: circles and invitations

At the end of this milestone a person can make a circle, invite people by username, accept or decline invitations, see the members, make another member an admin if they are one, and leave. Nothing is shared yet; the circle is the list later milestones share with. The closest existing feature is access groups (`internal/agent/tools/people/people.go` `group_manage`, `web/src/pages/access`).

Migration `0193_circle.sql`:

    circle: id varchar(26) primary key, created_at, modified_at, name varchar(100) not null,
            circle_kind varchar(16) not null check in ('family','friends','colleagues'),
            created_by_user_id varchar(26) null references "user" on delete set null
    circle_member: circle_id references circle on delete cascade,
            user_id references "user" on delete cascade, member_role varchar(16) not null
            check in ('admin','member'), joined_at, invited_by_user_id null,
            primary key (circle_id, user_id)
    circle_invitation: id, circle_id references circle on delete cascade,
            invited_user_id references "user" on delete cascade, invited_by_user_id,
            member_role, created_at, expires_at, answered_at null,
            invitation_answer varchar(16) null check in ('accepted','declined','withdrawn')

plus the permission `circle:use` seeded into Administrator, Operator and Member, and a check (in Go, in one transaction holding the circle's row) that a circle always keeps at least one admin: the last admin cannot leave or be made a member while others remain, and a circle whose last member leaves is deleted.

`internal/models/circle.go` defines `Circle`, `CircleMember`, `CircleInvitation`, `CircleKind`, `MemberRole` and `InvitationAnswer`. `internal/db/database_circle.go` holds the queries, including `SharesCircle(userId, otherUserId string) (bool, error)` and `CircleUserIDs(userId string) ([]string, error)` (everybody the person shares a circle with), which later milestones use to authorize. The rules that keep a circle consistent (at least one admin, the last member deleting it, a person leaving taking their shares with them) live in `internal/db/database_circle.go` too; there is no `internal/circle` package.

GraphQL operations: `ListCircles`, `ReadCircle`, `CreateCircle`, `RenameCircle`, `DeleteCircle` (admins), `InviteToCircle(circleId, username, memberRole)` (admins), `ListCircleInvitations` (to me), `AnswerCircleInvitation(invitationId, accept)`, `WithdrawCircleInvitation`, `SetCircleMemberRole` (admins), `RemoveCircleMember` (admins), `LeaveCircle`, and for operators `AddCircleMember(circleId, userId, memberRole)`. `requireCircleMember(context, circleId) (*models.Circle, *models.CircleMember, error)` and `requireCircleAdmin` join the helpers in `authorize.go`. `InviteToCircle` answers the same whether or not the username exists or is already a member; an invitation to nobody is never stored.

The invited person learns of an invitation as a note in their main conversation (a new `AgentNoteKind`, `circle_invited`), as a phone notification of the new kind `circle` when they have a phone paired, and on the dashboard as a badge on the account menu's Circles entry.

Surfaces: dashboard page `web/src/pages/circles.tsx` at `/circles` and `/circles/{id}` (members with avatars and an admin mark, invite, pending invitations, leave), reached from the account menu, not a new top-level section; command line `internal/cmd/circle.go`: `teanode circle list|show|create|rename|delete|invite|invitations|accept|decline|withdraw|role|remove|leave`; agent tool `internal/agent/tools/circle` with actions `list`, `show`, `invite` (risk `granting`), `answer` (risk `granting`), `leave` (`destructive`); iPhone app `CirclesView.swift` under Settings.

Acceptance: as person A, `teanode circle create --kind family "Home"`, `teanode circle invite <circle> personb`, `teanode circle invite <circle> personc`; as B and C, `teanode circle invitations` lists it and `teanode circle accept <invitation>` joins. `teanode circle show <circle>` lists all three with A as admin. As B, inviting is refused (not an admin); A makes B an admin and B's invitation then works. As A, invite a username that does not exist: the answer is the same and no pending invitation is listed. Tests in `internal/api/v1api/apigraph/circle_test.go` cover: a non-member reading a circle gets not found; a member who is not an admin cannot invite, remove or change roles; the last admin cannot leave or be demoted while others remain; the operator's `AddCircleMember` needs `user:manage`.

### Milestone 3: chats in the drawer

At the end of this milestone people who share a circle can write to each other in direct and group chats, which appear in the drawer's conversation list beside the main and named conversations, on the conversations page, in the iPhone app's conversation list and on the command line's conversation commands. They see who has read what, react, attach pictures and get phone notifications. No agent takes part yet. There is no new page and no new navigation: the closest existing feature is the conversation itself (`internal/models/insight.go`, `internal/agent/feed.go`, `web/src/components/agentDrawer.tsx`, `web/src/pages/agentConversations.tsx`, `ios/TeaNode/Sources/ConversationView.swift`), and this milestone teaches it about people.

Storage. Migration `0194_chat.sql`:

    chat: id, created_at, modified_at, chat_kind check in ('direct','group'),
          title varchar(100) null, circle_id null references circle on delete cascade,
          created_by_user_id null, last_message_at null,
          direct_key varchar(53) null unique   -- the two user ids, sorted, joined by ':'
    chat_member: chat_id references chat on delete cascade,
          user_id null references "user" on delete set null, member_name varchar(100),
          member_role check in ('admin','member'), joined_at, removed_at null,
          read_through_message_id varchar(26) null, muted_until timestamptz null,
          is_agent_reading boolean not null default false,
          is_agent_askable_by_others boolean not null default false,
          unique (chat_id, user_id)
    chat_message: id varchar(26) primary key (a ULID, so ids sort by time), chat_id
          references chat on delete cascade, created_at,
          author_user_id null references "user" on delete set null,
          author_member_name varchar(100) not null,
          author_agent_id null references agent on delete set null,
          agent_run_id varchar(26) null, body text not null, attachments jsonb not null
          default '[]', reply_to_message_id null, edited_at null, deleted_at null
    chat_reaction: message_id references chat_message on delete cascade,
          reactor_key varchar(40) not null,   -- 'user:<id>' or 'agent:<id>'
          emoji varchar(32) not null, created_at, primary key (message_id, reactor_key)
    agent_conversation: add chat_id varchar(26) null references chat on delete cascade,
          unique (agent_id, chat_id); kind gains 'chat'

with an index on `chat_message (chat_id, id)`. A direct chat is unique per pair through `direct_key`, so opening the chat with somebody twice returns the same chat. A circle's circle chat is made with the circle, and its membership follows `circle_member` in the same transactions that change the circle. A group chat's members must each share a circle with the person who adds them (`SharesCircle`). The `is_agent_*` columns exist now so Milestone 5 needs no second migration of this table.

Every member of a chat has a **chat conversation**: their own `agent_conversation` with kind `chat` and the chat's id, made in the transaction that adds them (`EnsureChatConversation(agentId, chatId)`). That needs an `agent` row for everyone, so `EnsureAgent(userId)` makes a switched-off one (`enabled = false`) for a person who never turned theirs on; check that nothing treats a switched-off agent's row as "this person uses the agent" (the usage page, the operator's agent list, the dream scheduler) and filter on `enabled` where it does. The chat conversation is where a member's own agent's working rows go in Milestone 5; in this milestone it holds nothing but its title, archive state and read state.

When an account is deleted, `chat_member.user_id` and `chat_message.author_user_id` become null by their foreign keys and `removed_at` is set; `member_name` and `author_member_name` keep the name the person had, and the screens show it as "(deleted account)".

Reading. `ReadAgentConversation` on a chat conversation returns, newest page first as it does today, the chat's messages interleaved by id with the caller's own rows in their chat conversation. `AgentMessage` in GraphQL gains `authorKind` (`person` or `agent`), `authorUserId`, `authorName`, `authorAvatarUrl`, `authorAgentId` and `isMine`; for today's conversations these say the owner and their agent, so the drawer's code needs one path. `ListAgentConversations` lists chat conversations with the other members (names and avatars), unread counts and the last message, and takes `kind: chat` to filter. `AgentConversationEvents` on a chat conversation adds the chat's events: `said` (a new chat message, carrying the message), `edited`, `deleted`, `reacted` (with the reactor's name), `read` (a member's read mark moved) and `typing`. Those reach every member's subscription through a new `NOTIFY` channel, `ChatEventChannel` in `internal/db/database.go`, which carries only `{chatId, messageId, chatEventKind}` (never a body), and a per-instance fan-out, the hub in `internal/chat/hub.go`, that rechecks `chat_member` for every subscriber on every event, so a person removed from a chat stops receiving it at once.

Writing. `AskAgent(conversationId, message, ...)` on a chat conversation stores a chat message instead of starting a turn, and publishes it; in Milestone 5 it also starts a turn when the message addresses the sender's agent. Attachments use the existing `agent_attachment` upload, claimed by the chat message (the bytes stay where they are; the row records the chat). `ReactToAgentMessage` reacts to chat messages, one reaction per person. New operations only for what a conversation with an agent never needed: `OpenDirectChat(userId)` (returns the caller's chat conversation), `CreateGroupChat(title, userIds)`, `RenameChat`, `AddChatMembers`, `RemoveChatMember`, `LeaveChat`, `EditChatMessage`, `DeleteChatMessage` (own messages; a group chat's admins may delete any), `MarkChatRead(conversationId, throughMessageId)` (forward only), `MuteChat(conversationId, mutedUntil)` and `SetChatTyping(conversationId)` (an event, nothing stored). `requireChatMember` joins `authorize.go`. A per-person rate limit (60 messages a minute, counted in memory per instance) stops a runaway script, and `ValidateChatMessageBody` caps a message at 16,000 characters.

Phones. A chat message notifies every member but its author who has a phone paired, is not watching that conversation (presence, `internal/agent/presence.go`, already reports what a tab or phone is showing) and has not muted it, as a new `push.NotificationKind` `chat` with the conversation id as its thread id so the phone groups them, and the author's name and the first line as its title and body. Tapping it opens the app on that conversation.

The drawer. The drawer opens for a person whose agent is off: it shows their chats, and the main conversation becomes an offer to turn the agent on. The conversation picker lists chats with the other members' avatars (two overlapped for a group), a bold title and an unread count, sorted with the other conversations by last message; the drawer's button shows the total unread. The picker's "New conversation" offers "Message someone" and "New group", choosing from the people the person shares a circle with. A chat's header shows its members' avatars, and its menu holds members, add, leave, mute and rename. Messages from other people are on the left with the author's avatar and name above a run of their messages; the person's own on the right as today; read marks under the last message each member has read. The conversations page (`agentConversations.tsx`) lists chats too, with a filter. The iPhone app's conversation list and `ConversationView.swift` do the same with `PersonBubble` for every person, the author's avatar on others' messages, and the same menu.

Command line: the conversation commands work on chats: `teanode agent conversation list --kind chat`, `teanode agent conversation show <id>` (with authors), `teanode agent ask --conversation <id> "..."` (posts in the chat), `teanode agent chat --conversation <id>` (follows and writes until the person leaves), `teanode agent react`; and new ones under the same group: `teanode agent conversation direct <username>`, `teanode agent conversation group "<title>" --with <username>,...`, `teanode agent conversation members|add|remove|leave|mute|read <id>`, `teanode agent conversation edit|delete-message <message-id>`. Agent tool: the `conversation` tool's `list` and `read` include chats (the person's own to read), and a new action `send` posts into a chat as the agent, marked as the agent's through `SayInChatAsAgent` (risk `outward`, so a card unless the person allowed outward calls unattended: "tell the family I'll be late").

Acceptance: A and B share a circle. A runs `teanode agent conversation direct personb` and `teanode agent ask --conversation <id> "dinner at 7?"`; B's drawer shows the chat at the top of the picker with one unread, live, without a reload, and B's button shows 1; B's phone, with the app in the background, shows "A: dinner at 7?"; B opens it, and A's `teanode agent conversation show <id> --json` shows B's read mark. C, who has never turned on their agent, can open the drawer, sees the circle chat and writes in it. D, who shares no circle with A, gets not found for `OpenDirectChat` with A's id. Removing B from the circle removes them from the circle chat and from any group chat whose other members no longer share a circle with them, in the same transaction; their direct chat with A stays readable to both and refuses new messages with "you no longer share a circle". Deleting B's account leaves B's messages shown as "B (deleted account)". Tests cover each of these in `internal/api/v1api/apigraph/chat_test.go`, the interleaved reading (a member never sees another member's chat conversation rows) in `internal/db/database_chat_test.go`, and the feed's removal on membership change in `internal/chat/hub_test.go`.

### Milestone 4: dropped

The agent's face (a drawn, animated picture of the agent) was built and removed at the maintainer's word. The numbering of the later milestones is kept so references to them stay right.

### Milestone 5: agents in chats

At the end of this milestone a person can ask their agent in any chat they are in, the agent answers in the chat under its name, and the people in the chat see that it is thinking. The person can let their agent remember the chat, and can let the others in it ask their agent too. The closest existing feature is a turn in a named conversation (`internal/agent/ask.go`) and the way a background wake posts into a conversation (`docs/decisions/20260929-background-work-wakes-the-conversation-that-started-it.md`).

Starting a turn. `AskAgent` on a chat conversation takes `addressedAgentIds`: the drawer and the app fill it from an `@` mention chosen in the composer's completion, or from replying to an agent's message, so an agent's name in ordinary prose never starts a turn; the command line has `--ask-agent` for the sender's own. `internal/chat/agents.go` decides, by these rules and no others. The message's author is a person. For each addressed agent: if the agent's owner is the author, start an **owner turn**; if the owner is a member of the chat and set `is_agent_askable_by_others` for it, start a **member turn**; otherwise start nothing and tell the author, in a note only they see, that only the owner can ask that agent here. An agent's own message never starts anything.

Both turns call `Agent.Ask` with the new surface `chat` (in `internal/agent/surface.go`: no suggested replies, no pages to show, reactions drawn) in the owner's chat conversation. The model sees that conversation as `ReadAgentConversation` would show it to the owner: the chat's messages interleaved with the owner's own agent rows, bounded the way any conversation's history is, with compaction writing its note into the owner's chat conversation (never into the chat). Every chat message not written by the owner or the owner's agent is wrapped in `<untrusted-data>` with its author's name. The conduct's chat section says who is in the room (names, and which is the owner), that the answer will be read by all of them, that it should not bring in anything private the question did not need, and that it speaks as the owner's agent.

An owner turn has the agent's usual tools and cards. Its cards are rows in the owner's chat conversation, so only the owner sees them, inline in the chat on the owner's screens and on the owner's phone. A **member turn** is held: the run gets the read-only tool set of research on mail from strangers, minus mail, memory, files, calendar, finance and every device, plus the conversation tool's `read` for this chat, and web search and fetch. It is offered no shared calendar and no shared memory, on purpose: what the owner shared was shared with particular people, not with the room, and the answer goes to the whole room. It can raise no card; a call that would need one is refused. This reuses the existing "limited to reading or to a few named tools" run shape, which already never uses the unattended allowance. Its tool rows are written to the owner's chat conversation, where the owner can see what somebody else's question made their agent do.

Posting. The assistant's answer of each round is written as a `chat_message` with `author_agent_id` and `agent_run_id` instead of an `agent_message`, and published as `said`; tool calls, tool results and notes stay `agent_message` rows in the owner's chat conversation. The streaming `text` deltas go to every member as a provisional message that the stored one replaces, the same replace-not-append rule the drawer already follows. While the turn runs, every member's view shows "<agent name> is thinking", from a `typing` event the turn publishes with the agent's name when it starts and again every few seconds until its answer is posted; nobody but the owner learns which tool it is using.

Memory. `is_agent_reading`, set by a member for their own agent, makes the chat a source of that agent, kind `chat`, read by the dream like the other chat sources (`internal/agent/ingest_document.go`), from the time it was set. The chat's members panel shows, for every member, whether their agent remembers the chat. Turning it off stops reading at once; what was already learned stays, as with any source, and the Knowledge page shows the chat as those facts' evidence.

Operations: `SetChatAgentReading(conversationId, isAgentReading)` and `SetChatAgentAskableByOthers(conversationId, isAgentAskableByOthers)`, each for the caller's own membership. Dashboard and app: the composer's `@` completion offers the person's own agent and the agents of members who allowed it; the members panel has the two switches; in the owner's view, the agent's answer is preceded by its tool rows folded into one "what it did" line, as a turn in the main conversation already shows them. Command line: `teanode agent ask --conversation <id> --ask-agent "..."`, `teanode agent conversation remember <id> on|off`, `teanode agent conversation others-may-ask <id> on|off`.

Acceptance: in the family chat, A writes "@<A's agent> what time is the dentist on Thursday?"; B and C see "<A's agent> is thinking", then an answer from it; A's view has "what it did", listing the calendar read; B's view of the same chat has the answer and no tool rows, and reading A's chat conversation as B is not found. B writes "@<A's agent> what is in A's inbox?": no turn starts, and B alone sees the note that only A can ask. A turns on "others may ask my agent here"; B asks the same: the turn starts held, answers that it cannot read A's mail, and A's usage shows the turn. A message from A's agent naming B's agent starts nothing. Tests: `internal/chat/agents_test.go` for every branch of the decision above; `internal/agent/ask_chat_test.go` that a member turn's tool list contains no mail, memory, calendar, finance or device tool, that other members' messages are inside `<untrusted-data>`, and that no tool result of an owner turn appears in any `chat_message`.

### Milestone 6: shared calendars and the circle calendar

At the end of this milestone a person can share their calendar with a circle or a person at a level, a family circle can have a calendar of its own, everybody sees the calendars shared with them on the dashboard, the phone (CalDAV) and the app, and the agent can find a time several people are free. The closest existing feature is the reminders list (`docs/planning/reminders-execplan.md`), which added a second calendar beside the first.

Migration `0195_calendar_share.sql`:

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

`internal/calendar/visibility.go`: `Redact(object models.CalendarObject, shareLevel ShareLevel) models.CalendarObject`, applied in exactly one place, the database function that lists objects for a reader who is not the owner. `busy` returns only start, end and the busy judgment; `titles` adds summary and location; `details` and `edit` return the object; any level returns busy-only for `CLASS:PRIVATE` and `CLASS:CONFIDENTIAL`. The private rule joins the four judgments in `calendar.FreeBusy`'s neighborhood with a test per level and class. A circle calendar is read and written by every member at `edit`.

CalDAV: the person's home set (`internal/dav/caldav.go`, list calendars) adds each calendar shared with them at `titles` or above and each circle calendar, at `/dav/{userId}/calendars/{calendarId}/` with the owner's name in the display name ("Family", "A's calendar"), and `current-user-privilege-set` read-only below `edit`. A write below `edit` is refused with 403 before the library sees it. `busy` shares are not listed as calendars: they are free-busy only, which the phone asks through scheduling, and the dashboard shows as blocks.

Agent: the `calendar` tool's `agenda` gains `owner` (a username, or `everyone` for every calendar shared with the person), answering at the shared level; new actions are `free_together` (takes `with`, a list of usernames, and a window, and answers the times all of them are free, using `calendar.FreeByDay` over the union of their busy times), `shares`, `share` (`with` is a username or a circle id, and `share_level`) and `unshare` (`share_id`). Adding an event to a circle calendar is the person's own change; adding to another person's shared calendar at `edit` is risk `outward`.

Operations: `ListCalendarShares(calendarId)`, `ShareCalendar(calendarId, granteeUserId | granteeCircleId, shareLevel)`, `UnshareCalendar(shareId)`, `CreateCircleCalendar(circleId)`, and `ListCalendars` returning shared ones with `ownerName`, `shareLevel` and `isCircleCalendar`. Dashboard: the calendar page's side list shows the person's calendars, circle calendars and shared ones, each with a color and a check to show or hide, and a Share panel on their own; the circle page offers "Make a family calendar". When a person joins a family or colleagues circle, the dashboard and the app suggest a share once ("share your calendar with Home as: busy / titles / details / not now"). Command line: `teanode calendar share <username|circle> --level titles`, `teanode calendar shares`, `teanode calendar unshare <id>`, `teanode calendar list --owner <username>`, `teanode calendar free --with a,b --from ... --until ...` (the existing `free` command gains `--with`). iPhone: `CalendarView.swift` shows the same list and the share panel.

Acceptance: A shares at `titles` with B; B's dashboard shows A's events by title, and A's event with `CLASS:PRIVATE` as "Busy". B's phone, after adding the CalDAV account, lists "A's calendar" and refuses to save an edit to it. A raises B to `edit`; B's phone can now edit an event without guests and is refused for one with guests. B asks their agent "when are A and I both free Saturday afternoon?" and gets times consistent with `teanode calendar free`. A leaves the circle: B no longer sees A's calendar on any surface or over CalDAV.

### Milestone 7: shared memory

At the end of this milestone a person can share a page and everything under it with a circle or a person, the reader sees it under "Shared with you" on the Knowledge page and the app, and the reader's agent reads it, cites it with the owner's name, and never copies it. The closest existing feature is the memory tool's reading of a page (`internal/agent/tools/memory`) and the Knowledge page (`web/src/pages/knowledge.tsx`).

Migration `0196_agent_page_share.sql`:

    agent_page_share: id, agent_id references agent on delete cascade,  -- the owner's
          path varchar(255) not null,        -- the root of the shared subtree
          grantee_user_id null, grantee_circle_id null, check exactly one,
          is_quote_shared boolean not null default false,
          created_at, created_by_user_id

`internal/agent/shared_memory.go`: `SharedPagesFor(readerUserId string) ([]PageShareView, error)` (every share reaching the reader, through a direct share or a circle they are in, with the owner's name), `ReadSharedPage(readerUserId, ownerAgentId, path string)` (refuses unless some share's path is the page's path or a prefix of it at a `/` boundary), and the redaction: facts without their quote unless `is_quote_shared`, evidence named only as "from <owner name>'s <source kind>", edges only where the far page is also shared with the reader, no overview, and the summary unless it quotes somebody and the quotes are not shared. These are the only functions that read another agent's graph, and every reader path goes through them.

Memory tool: `get`, `search` and `index` gain an `owner` argument (a username). Without it, they read the reader's own graph as today; `search` also searches shared pages and marks each result with its owner. A citation of a shared fact is written as `people/a-grandparent#3 (shared by <name>)`; the post-turn citation linker recognizes the form. Writes (`note`, `move`, `link`, `forget`) refuse an `owner` other than the caller. The prompt's index gets a short "Shared with you" section: the shared roots with their openings, bounded to 20 lines with the count of the rest and the call that lists them, so nothing is silently cut.

Never copied: the after-conversation writer and the dream's reading refuse to store a fact whose only evidence is text from a shared page, with the error "this came from a page another person shared; cite it instead of writing it down" counted on the run like an unfound quote. The dream does not read shared pages at all.

Operations: `ListAgentPageShares` (mine), `ShareAgentPage(path, granteeUserId | granteeCircleId, isQuoteShared)` (risk `granting` for the tool), `UnshareAgentPage(shareId)`, `ListSharedAgentPages` and `ReadSharedAgentPage(ownerUserId, path)` (shared with me), and a preview, `PreviewAgentPageShare(path, grantee)`, which returns exactly what the grantee would see, shown before the owner confirms. Dashboard: a Share action on a page (with the preview), the shares listed on the page, and a "Shared with you" section in the Knowledge sidebar grouped by owner, with an owner badge on every shared page. Command line: `teanode agent memory share <path> <username|circle> [--with-quotes]`, `teanode agent memory shares`, `teanode agent memory unshare <id>`, `teanode agent memory get <path> --owner <username>`. iPhone: `KnowledgeViews.swift` gets the same section and the share sheet.

Acceptance: A shares `people/a-grandparent` with the family circle; B's Knowledge page lists it under A's name with its facts and no quotes; B asks their agent "when is grandma's next appointment?" and the answer cites `people/a-grandparent#N (shared by A)`; B's own graph gains no fact quoting it (`teanode agent memory get people/a-grandparent` as B finds nothing, or B's own page without that fact). A unshares; B's next question gets "I don't know" and the Knowledge page no longer lists it. Tests: `internal/agent/shared_memory_test.go` for prefix matching at `/` boundaries (`people/a` does not share `people/ab`), quote redaction, edge redaction, circle membership changes; the memory tool's refusal of writes with `owner`; the writer's refusal of shared-only evidence.

### Milestone 8: shared payment skills

At the end of this milestone a person can share named tools of an installed skill (for payments, the Link skill's `link_pay`) with one person in a circle they share, with limits; the other person's agent pays through it, on the owner's computer; and a call above the limits, or on a share that asks every time, waits for the owner to approve it. The closest existing features are the skill tool call (`Skill.Run` in `internal/skills/run.go`, which a skill tool of the owner's own agent already goes through, on the owner's computer) and the card (`agent_interaction`, `internal/agent/interaction.go`).

Knowing the amount. A limit can only be checked on an amount the server can read before the call runs. The skill declaration (`internal/skills/skill.go`, `Tool`) gains two optional fields, `amountField` and `currencyField`, naming the parameters that hold the amount in minor units and the currency code, and, where the tool routes on `actionField`, `amountActions` (the actions they apply to, `[card]` for `link_pay`). The Link skill in the skills registry is updated to declare them; that is a signed change in the registry repository and its own pull request there. A call to a shared tool, or an action of it, without a declared amount always waits for an approval, which is what `link_pay`'s `url` action (an address whose price only the merchant knows) gets.

Storage. Migration `0197_skill_share.sql`:

    skill_share: id, created_at, modified_at, owner_agent_id references agent on delete
          cascade, skill_name varchar(100) not null, tool_names text[] not null,
          grantee_user_id references "user" on delete cascade,
          payment_limit_minor bigint null, monthly_limit_minor bigint null,
          currency_code varchar(3) null, is_approval_required boolean not null default false,
          unique (owner_agent_id, skill_name, grantee_user_id)
    skill_share_call: id, created_at, modified_at, skill_share_id references skill_share
          on delete cascade, grantee_run_id varchar(26), grantee_conversation_id null,
          tool_name, tool_arguments jsonb, amount_minor bigint null,
          call_state check in ('ran','failed','refused','waiting_for_approval',
          'declined','expired'),
          tool_output jsonb null, answered_at null, answer_note text null, expires_at null

Running a shared call. The grantee's agent's catalog gains each shared tool under the name `shared_<tool>_<owner username>` (the owner's username made safe for a tool name), with the skill's description and parameters and a first line saying whose it is and what its limits are. `(*Agent).RunShared` in `internal/agent/skill_share.go` is the only path: it checks the share still exists and both people still share a circle, works out the amount from the declared fields, adds the month's `ran` calls, and either runs the call through `Skill.Run` as a call of the owner's agent on the owner's computer (so the owner's command judge, the skill's own rules and the owner's computer being connected all apply exactly as for the owner), or stores it as `waiting_for_approval` and answers the grantee's agent that it is waiting for the owner. The grantee's own card comes first either way: a shared payment tool carries risk `money` for the grantee's agent, so the grantee sees what will be paid before anything is sent, and an unattended turn of the grantee pays only if the grantee allowed money calls unattended.

The output of a call (the spend request's id and state) goes back to the grantee's agent and is kept on `skill_share_call`. `link_spend_requests` may be shared too, and then its `retrieve` and `cancel` are allowed only for a `spend_request_id` that appears in an earlier call's output of the same share; its `active` and `history` listings are never shared, because they list the owner's other spend requests.

Approving. `AnswerSkillShareCall(callId, isApproved, note)`, for the share's owner only, is a conditional update from `waiting_for_approval`, so answering twice (from the phone and the drawer at once) runs the call once. Approving runs exactly the stored call, with its stored arguments, through `RunShared` with the limits skipped, and records `ran` or `failed`. A waiting call expires after a day. The grantee's agent hears the outcome through a background wake of the conversation the call came from (`grantee_conversation_id`), and the grantee gets a phone notification. An approval is answered by the owner and never by an agent: no tool has an answer action, and `AnswerSkillShareCall` refuses a caller that is an agent's run token.

Telling the owner: a new `AgentNoteKind` `skill_share_call_waiting` in the owner's main conversation, rendered as a card that says what the other person's agent asked to run, with the tool, the amount and the reason their agent gave, and Approve and Decline; pushed with a new `NotificationKind` `skill_share_call` to the owner's phones, whose actions open the app to confirm with Face ID before answering (a lock screen tap must not spend money).

Operations: `ListSkillShares` (mine, and shared with me), `ShareSkill(skillName, toolNames, granteeUserId, paymentLimitMinor, monthlyLimitMinor, currencyCode, isApprovalRequired)`, `UnshareSkill(shareId)`, `ListSkillShareCalls(shareId, callState)`, `AnswerSkillShareCall`. Dashboard: on the agent page's skills tab, a Share action on an installed skill (person, tools, limits, "ask me every time"; the share's calls listed under it, waiting ones first) and, for the grantee, the skills shared with them; the card in the drawer. Command line: `teanode agent skill share <skill> <username> --tools link_pay --payment-limit 100.00 --monthly-limit 400.00 --currency usd [--ask-every-time]`, `teanode agent skill shares`, `teanode agent skill unshare <id>`, `teanode agent skill calls <share-id> [--waiting|--ended]`, `teanode agent skill decline <call-id> [--note ...]` (approving is done on the dashboard or in the iPhone app). Agent tool: sharing a skill is the `skill` tool's `share` action with risk `granting`; the `skill` tool's `calls` lists a share's calls, read only. iPhone: the services screen's skill row gets Share and the calls, the card and the notification actions.

Acceptance: the end-to-end story in Purpose / Big Picture, plus: the other adult's 140.00 payment waits for the holder, and approving it runs it; a `url` payment always waits; with the holder's computer disconnected, a shared call answers that the owner's computer is not connected and nothing is stored as ran; approving the same call from two places runs it once; no tool of any agent can answer a waiting call; `retrieve` of a spend request id that did not come through the share is refused; unsharing removes the shared tools from the grantee's next turn; leaving the circle does too. Tests in `internal/skills/share_test.go` (amount reading, limits, the month's total, the id rule) and `internal/api/v1api/apigraph/skill_share_test.go` (who may share, call, read and answer; the answer race).

## Concrete Steps

Every milestone, from the repository root, on a branch from `main`:

    git switch -c feat/<milestone-slug> origin/main
    # write the migration and its reverse; pick the next free number on main (this branch took 0192 to 0198)
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

- Everything shared reaches only people who share a circle with the sharer, and stops reaching them in the same transaction that ends the circle membership. One test per kind of share (chat, calendar, page, skill) removes a member and checks every operation refuses.
- No other person can ever read a tool result of somebody's agent. A test searches `chat_message` bodies for the tool result text of an owner turn and finds nothing, and reads each member's view of a chat to check it holds no other member's chat conversation rows.
- No agent's message starts a turn, and no member's message starts somebody else's agent unless its owner allowed it for that chat.
- A waiting call is answered only by the share's owner, never by an agent and never twice, and approving runs exactly the call that was shown.
- No shared payment runs above its limits without an approval, and none runs anywhere but on the owner's computer.
- `TestEveryOperationAuthorizes` passes with every new operation, and the parity tests pass with every new command and tool name.
- The security review (`docs/security/security-review.md`) gains a section for this feature covering: membership checks on every operation and subscription, prompt injection from chat messages and from the reasons on waiting calls, shared skill calls and their limits, avatar decoding limits and metadata removal, `NOTIFY` payloads carrying ids only, and the phone payloads sealed as before.

## Idempotence and Recovery

Every migration is additive until Milestone 6 makes `calendar.user_id` nullable; its reverse refuses to run while a circle calendar exists (it deletes nothing on its own), so a rollback after families started using one is a deliberate step: copy the circle calendars out (each event is one file, `teanode calendar show <id> --json` prints it, or a phone's CalDAV account holds a copy), delete them, then roll back. Deploys follow the repository's existing rule for migrations: back up first, and check the build includes every migration already applied on the server.

Sharing operations are idempotent by their unique keys: sharing the same thing with the same grantee again changes its level or limits rather than adding a row. Opening a direct chat twice returns the same chat. A payment approved twice runs once.

## Artifacts and Notes

The five decision records under `docs/decisions/` dated 2026-10-16 are the design's reasons, one per decision.

## Interfaces and Dependencies

No new third-party dependency is needed. Avatars are decoded with the standard library's `image/png` and `image/jpeg`; the clients convert other formats first. Milestone 8 needs a change to the Link skill in the skills registry (the `amountField`, `currencyField` and `amountActions` declarations), signed and released there before the milestone ships.

New packages: `internal/avatar` (picture normalization and the initial color), `internal/chat` (the hub that carries a chat to its members, the decision which agents a message starts, and the phone notifications; the turn an agent takes in a chat is `internal/agent/chat_turn.go`), and the agent tool `internal/agent/tools/circle`, imported in `internal/agent/tools/all/all.go`. There is no `internal/circle` package: the database functions for circles, chats and skill shares stay in `internal/db/database_circle.go`, `database_chat.go` and `database_skill_share.go` like every other table. Chats need no tool of their own: the `conversation` tool gains them.

Types, in `internal/models`:

    type CircleKind string         // "family", "friends", "colleagues"
    type MemberRole string         // "admin", "member"
    type ChatKind string           // "direct", "group"
    // A chat's events are agent.EventKind values: "said", "edited", "deleted", "reacted", "read", "typing", "members"
    type ShareLevel string         // "busy", "titles", "details", "edit"
    type SkillShareCallState string // "ran", "failed", "refused", "waiting_for_approval", "declined", "expired"
    const AgentConversationChat AgentConversationKind = "chat"

Functions other milestones depend on, with these signatures:

    // internal/db/database_circle.go
    func (self *transaction) SharesCircle(userId, otherUserId string) (bool, error)
    func (self *transaction) CircleUserIDs(userId string) ([]string, error)

    // internal/db/database_chat.go
    func (self *transaction) EnsureChatConversation(agentId, chatId string) (*models.AgentConversation, error)
    func (self *transaction) ListChatConversationMessages(conversation *models.AgentConversation, limit, offset int) ([]*models.AgentMessage, int, error)

    // internal/calendar/visibility.go
    func Redact(object models.CalendarObject, shareLevel models.ShareLevel) models.CalendarObject

    // internal/agent/shared_memory.go
    func SharedPagesFor(tx db.Transaction, readerUserId string) ([]*models.SharedRoot, error)
    func ReadSharedPage(tx db.Transaction, readerUserId, ownerUserId, pagePath string) (*models.SharedPage, error)

    // internal/agent/skill_share.go
    func (self *Agent) RunShared(ctx context.Context, request *SharedCall) (*SharedOutcome, error)

Revision note (2026-10-16): first version, written from a survey of the code before any implementation.

Revision note (2026-10-16, later): revised after the maintainer's answers. Chats are conversations in the drawer and the app through the existing conversation operations, with no chat page and no new navigation; guardians are a flag separate from circle admins and may read their children's conversations; payments are shared as a skill's tools with limits, covering an adult member as well as a child, and approving runs the asked call instead of a turn of the approver's agent; the circle agent is dropped; the kinds are family, friends and colleagues; a deleted account's chat messages are kept under the name it had.

Revision note (2026-10-16, last): simplified at the maintainer's word. A circle has admins and members only; the guardian flag, the child role, guardians reading a child's conversations and the approvals for a child's joining, leaving and sharing are gone. Approvals remain only for shared payment calls, answered by the share's owner, and live on `skill_share_call` rather than a table of their own.

Revision note (2026-10-16, later still): the agent's face was built, shown and dropped at the maintainer's word; Milestone 4 is kept as a dropped entry so the numbers of the others do not move, its decision record was removed before merging, and agents in chats are shown thinking by name.

Revision note (2026-10-17): the second security pass changed four things the milestones above describe. Approving a call and lending money are accepted only from the dashboard's session or the phone app's own sign-in (a mark on the token row, `token.client_kind`, migration 0198), so `teanode agent skill approve` is gone, `calls` says where to approve and `share` with limits is refused to a command line token; the skill tool's `share` shares only so that every call asks. The calls of a share stay when it ends (`skill_share_call` carries the owner's agent, the other person, the skill, and loses only its share), and the month's total counts them. The bound on invitations counts every call in `circle_invitation_send`, not the invitations stored. A person keeps at most 1GB of chat files.
