# Changelog

Notable changes to TeaNode. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[semantic versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Personal agents. An operator who configures a language model — OpenAI,
  Anthropic, Gemini, or anything that speaks the OpenAI API, such as Ollama —
  gives each person an agent of their own, off until they turn it on and
  blind to any mailbox they have not granted it. Granted a mailbox, it sorts
  what arrives: a category, a priority, whether somebody is waiting on an
  answer, a line of summary and the things the message asks for, shown as
  chips on the row and gathered in a Priority view beside Starred. Rules can
  read what it decided — "category is newsletter, move to Reading" — and run
  once the sorting is done rather than at delivery. Nothing is ever sent to a
  model from inside the delivery path; the work queues and a worker does it,
  retrying on a short ladder and stopping for the day when the person's
  token budget is spent. Operators choose providers and models, switch
  features on and off for the deployment, set per-agent and per-server
  limits, and see token use by day, kind, mailbox and model on the Agents
  tab of the server page; `teanode agent` does the same from a terminal.
  Provider keys and the other secrets of the agent section are sealed with
  the server secret before they are stored, as the domain keys are. The
  account learns its time zone and language from the browser or the CLI, so
  the agent knows when "tomorrow" is. (#73)

- The agent summarizes conversations and drafts replies. A conversation
  that reaches three messages — or whatever the person sets — gets a summary,
  folded above the messages in the reader and rewritten as the conversation
  grows, from the previous summary and only the messages since. Replying in
  a mailbox the agent may draft in, the composer offers *Draft with agent*
  and a line to say what the reply should do; the text lands in the editor,
  in the person's own voice and sign-off, and sending stays with them. Both
  are per mailbox and off until switched on. (#73)

- The agent can answer for you. A mailbox can let the agent reply on your
  behalf under a policy you write — who it may answer (contacts, everyone, a
  list), which kinds of message, when (always, outside your hours, while
  your out-of-office reply is on), how long a reply waits, how many a day,
  how long a sender is then left alone. A reply is held in Drafts, in the
  conversation, with a banner in the reader to cancel it or take it over —
  and an edit by you is a takeover — then sent as you, marked automatic so
  nothing answers it back. Before writing and again before sending it
  climbs the same ladder the out-of-office reply climbs: never to a list,
  a bounce, an automatic message, spam, or a message not addressed to you,
  never twice in a week to the same sender, never fifty in an hour; and the
  model itself declines anything asking for money, credentials, documents
  or a commitment. Every reply it wrote, sent, cancelled or refused is
  listed on the agent page with the reason, and `teanode agent replies`
  shows the same. (#73)

- Ask your agent. A drawer on every page of the dashboard, and `teanode
  agent ask` and `chat` from a terminal, hold one continuous conversation
  with the agent — or a named one kept apart — and what you have open is
  what "this" means. The agent works with tools over the same operations
  the dashboard uses, as you, with exactly your permissions: it searches
  your mail by words or by meaning, reads it, files it, drafts and — with
  your word — sends, keeps your folders and rules, looks something up on
  the web, and does time arithmetic in your zone. Anything it cannot undo,
  and anything that leaves the server, stops at a card you approve or
  decline; an operator can make it ask about more, never less. Everything
  it did is in the transcript, a long conversation is folded into a note
  rather than forgotten, and every action is in the audit trail as the
  agent acting for you. (#73)

- The agent can do what an operator can do — for an operator. A person who
  manages domains, users, groups or the server gets the tools for it:
  adding a domain and checking its DNS, adding and changing addresses and
  sending credentials, retrying the queue, searching the mail audit, making
  accounts and groups and roles, reading the audit log, reading and
  changing the server's settings, upgrading. Each tool is offered only to
  somebody holding the permission behind it and runs as them, so the agent
  can never reach further than the person; removing anything asks first,
  and a secret it makes is shown once and never kept. `teanode agent
  tools` lists what your agent has. (#73)

- The agent remembers, learns and keeps time. It keeps facts about you
  between conversations — who the accountant is, how you sign, what never
  to answer automatically — each addressed to the runs that should read
  it, shown on the agent page and editable there or with `teanode agent
  memory`. It learns from your own hands: a message you file somewhere
  other than where it sorted it, a reply you cancel or take over, become
  examples the next run is shown, for a while. It runs on its own at times
  you set — a morning brief at eight, in your own zone — delivering the
  answer by mail or into your conversation, and never doing anything that
  would have needed your word. It asks you a question when the answer
  changes what it does, and keeps a task list through a long piece of
  work. (#73)

- The agent reaches other services, and looks things up on its own. An
  operator can declare servers that speak the Model Context Protocol — a
  parcel tracker, a wiki, a ticketing system, a tool beside the server —
  over HTTP or as a subprocess, shared by everyone or connected by each
  person with their own credential or an authorization; the agent's
  catalog grows by their tools, each asking your word before it does
  anything unless the operator marked it read-only, and treating what
  comes back as data. When sorting decides a message would be easier to
  act on with something looked up — a tracking number, a reference — a
  research run reads the web, the mailbox and the read-only tools and
  leaves notes above the message, which a reply written for you draws on.
  (#73)

- The agent can use a browser. With a Chrome beside the server — the
  compose file has one under the `browser` profile — the agent opens a page
  in a fresh, isolated browser signed in as nobody, reads it as a tree it
  can point into, clicks, types, scrolls and waits, and never reaches a
  private address or downloads a file. For a page only you can sign into,
  a small extension attaches the tab you are looking at to your agent, so
  it acts there with your session while you watch; the extension itself
  refuses to type into a password or card field and to submit a form that
  pays or changes credentials without your word. A run with nobody present
  may only read a page. (#73)

- The conversation holds up to use. A second message sent while the agent
  is still working queues behind it and says so, rather than waiting on
  you to wait; Stop ends the turn in flight and keeps the words that had
  come. You can hand the agent files — a paperclip, a drop or a paste in
  the drawer, `--attach` on the command line: a picture is shown to it, a
  text file read to it, a video or anything else named — and point it at
  a conversation from the reader's toolbar, so "this" keeps meaning it. A
  tool line opens to what was asked and what came back, and each turn can
  show what it cost; both are switches you set once. Conversations can be
  renamed. (#73)

- The conversation, looked after. A named conversation titles itself after
  the first exchange, and every conversation gets a line of summary once it
  has been quiet for a few minutes with something new said; a name you
  give it is yours and stays. What the agent did on its own — a message
  sorted, a reply written — opens as a transcript that can be talked into,
  with the message and the decision as the history. The picker finds old
  conversations by words in the title, the summary or what was said, and
  deletes one after asking. The transcript shows when each message was
  said, the day changing, the agent thinking, and stays at the end while
  you type. The agent can draw a chart from data and make a page, a
  drawing or a document to open beside the conversation, shown under the
  line that made it. Memories and schedules can be edited; a schedule can
  be one moment (`@at 2026-09-12 09:00`) or a distance from now (`@in
  20m`) for a reminder — "check this in twenty minutes" is one. Your
  agent's page is under Settings, beside a Preferences page that holds how
  its work is shown; each subject on it saves on its own, the mailboxes it
  may reach are one block each with Grant or Revoke, and your own
  categories are rows added in a dialog. The operator's Agents tab is the
  same shape: providers and connected servers are rows edited in a
  dialog, with Test on a provider's row, and models are picked from what
  the providers offer; renaming a provider takes its model assignments
  with it. The tool policy lists every tool by family with one word for
  each — allowed, ask first, off — instead of two boxes of names. The
  main conversation heads the picker, marked; any named conversation can
  be made the main one, or a fresh main one started, and the one before
  is kept, named. What the agent did is a table that pages and filters.
  `mail_read` can hand the agent every header and the server's facts
  about a message — authentication, the spam filter's score, a Reply-To
  or Return-Path pointing elsewhere — and the sorter is told those two
  as well. The newest OpenAI models refuse function tools while they
  reason on chat completions; the client repeats the call with reasoning
  off and remembers the model. Each turn shows what it cost beside its
  tokens, from the provider's pricing. The primary conversation heads the
  picker with a star; any named conversation can be made primary. The
  operator's token-use table takes a date range, and names agents and
  mailboxes rather than showing their ids. Escape opens and closes the
  drawer from anywhere; what is typed in its box survives a refresh. The
  tools a person wants asked about first are picked from the same list
  by family, not typed. (#73)

### Changed

- Every dropdown in the dashboard is the dashboard's own — the browser's
  `<select>` and `<datalist>` are gone — with a box to narrow a long list
  by typing, and room for a value the list does not know where one is
  allowed (a model name, a folder, a template). Recipients in the composer
  and a rule's category are completed the same way. What a form gets wrong,
  and what it saved, is said once through the notice at the top of the
  page rather than in red under the field; a warning that stays true stays
  on the page. Actions are buttons, not underlined words. The mailbox's
  star is a star. Starred and Priority show their unread counts in the
  rail and in the tab's title, which used to carry the Inbox's, and leave
  out what sits in Junk or Trash; the Archive's unread count is not shown,
  since what was put aside to read later is not news. On the roles and
  groups pages a press anywhere on a row picks it. (#73)

## [0.18.2] - 2026-09-10

### Fixed

- A list you have left says when, and says it in a finished sentence.
  "Asked to leave, in one request," ended on a comma with nothing after it —
  the phrase was written to be completed by a time that was never rendered —
  and "in one request" was the name of a protocol rather than anything a
  reader wants: it is RFC 8058, where the sender undertakes to honour a single
  request. It reads "Unsubscribed 2 days ago" now, and when the asking went by
  mail or through the sender's own page it says so, because those take longer
  to take effect.

- Subscriptions has a search box, above the switch because it narrows both
  sides of it: the counts on the switch are counts of what was asked for, so
  "Unsubscribed · 0" answers "is the one I am looking for over there?". It
  matches the name a list calls itself, the address its mail comes from, and
  its own key, in the query rather than over the rows already fetched — the
  list is paged, and filtering afterwards would leave a page of fifty showing
  three and a count that disagreed. What is being looked for is in the address
  with the side, so a search can be linked and comes back with the back
  button.

- The subscriptions page knows its own name. It was missing from the list the
  breadcrumb reads, so the trail, the heading and the tab all called it
  Mailbox — the section it is in.

- Subscriptions has two sides, and shows one of them: the lists writing to you
  and the lists you have left, with a switch between that says how many are on
  each — and nothing beside it, because the switch already says the name and
  the number, and "Subscriptions · 3" next to "Subscribed · 2 | Unsubscribed ·
  1" is the same fact twice. The rows carry no badge either: on that side
  every row is one, and each already says how it was left. A list already left said "Asked to leave" and stayed where it was, so
  the ones dealt with crowded the ones still to deal with. Which side is being
  read is in the address, and it travels with a list you open, so coming back
  lands where you were. A link to a list opens it from either side, because
  mail can keep arriving after the asking and seeing that is the point of
  having asked. An attempt that failed is not a list left: nothing was
  accepted, the mail keeps coming, and that row is the one somebody needs in
  order to try again.

- A subscription exists as soon as a list writes to you. The row was made only
  when something was done about a list — muted, pictures allowed, unsubscribe
  asked for — so until then a list had no identity of its own: nothing to link
  to, and nowhere to keep anything about it. Delivery makes it now, and the
  message names it, so what a list has sent is a lookup rather than a grouping
  over a text key. The lists that arrived before this did are given rows and
  their mail is joined to them by the migration.

- A list is linked to by its own identity: `/mailbox/subscriptions/<id>`
  rather than the list's key. The key is the identifier the sender chose for
  itself, usually an address, and an address in the address bar is an address
  on the screen of anybody looking over a shoulder. Links carrying a key still
  arrive where they meant to, and are put right as they land.

- A list kept out of the Inbox arrives unread. It was filed in the Archive and
  marked read on the way, which said it had been dealt with when it had only
  been put somewhere else — and left every subscription showing nothing to
  read. Muting says where a list's mail waits, not that it is finished with,
  and the unread count beside a list is how somebody comes back to it when
  they have time. Muting a list still moves what the Inbox is holding from it;
  it no longer marks that read either.

## [0.18.1] - 2026-09-10

### Fixed

- A target on a phone has two dimensions. Every control was given a height of
  forty pixels there, and nothing gave the ones that are only a picture a
  width, so they stayed as wide as the icon inside: the cross that dismisses a
  toast was 22 pixels across in a 40 pixel row, and the star on a conversation
  28. The icons are drawn the same size; the box around them is not.

- Sending a message opens the message. The compose page used to become a card
  saying "Sent." with a link to go and find it in the Sent folder; it goes
  there now, to the message itself, and says "Sent." in the line at the foot
  of the window with everything else. `SendMailboxMessage` returns the copy
  that landed in Sent, so the dashboard opens the message rather than a folder
  to look through — and falls back to the folder when there is no copy.

- Everything that has just happened is said in the same place. "Saved.",
  "Saved, and in use now", "It came back." — each was a word beside a button
  or a green line above a form, which had to be taken back the moment anything
  was typed. They join the mail actions in the line at the foot of the window.
  What is still true stays where it is: a setting waiting for a restart, a
  server nobody is supervising, a field that is wrong.

- What a list is narrowed to, and which page of it you are on, are in the
  address too. A mailbox search, the filter on the audit log, and every
  table's page and page size were state — so a search could not be sent to
  anybody, came back empty after a reload, was lost the moment a message was
  opened from it, and gave the back button nothing to return to. Opening a
  message found by a search and coming back now finds the search still there.

- What a page is showing is in the path, not beside it. A subscription being
  read, a group being looked at and a role being edited were each held in a
  variable or in a query parameter written with `replace`, so choosing one
  made no history: the back button left the page rather than returning to the
  one read before, and nothing led forward again. They are
  `/mailbox/subscriptions/<list>`, `/access/groups/<id>` and
  `/access/roles/<id>` now — places, so they can be linked to, gone back to,
  and come forward from. Older links carrying `?key=`, `?group=` or `?role=`
  still arrive where they meant to.

- A discarded draft is gone from the screen as well as from the server. The
  composer told the page it had closed, and the page treated that as somebody
  closing the composer — which keeps the draft — so the row stayed in the list
  and in the conversation, and opening it asked the server for a message that
  had been deleted: "api: not found".

- The empty list says so with room around it. The pane that offers the
  envelope trail and the empty list next to it were the same class, and the
  rule for the pane — which gives up its padding so the drawing can reach the
  edges — came later in the stylesheet, so it took the padding from the list
  as well and left the sentence against both borders.

- The number in the tab counts what the tab says. The rest of the title is
  where you are — "Drafts · Mailbox" — while the count in front of it was
  always the Inbox's, so the two halves described two different places. It is
  the folder's own count while a folder is open, and the count across every
  mailbox everywhere else, which is what a tab in the background is for.

## [0.18.0] - 2026-09-09

### Added

- The mailbox says what it just did, and offers the way back. Archiving,
  moving, reporting junk and deleting each raise a line at the foot of the
  window — "Conversation archived" — with Undo beside it, and the clock stops
  while the pointer is on it so reaching for undo does not lose it. Undo works
  from what the destination gained rather than from the identifiers the action
  was given, because moving a message makes a new item and retires the old
  one; asking for the old ones is how an undo can fail while appearing to
  work.

- Failures are said the same way, in one place, instead of a red block above
  a list that stays until something replaces it. What describes the state of a
  page — a query that failed, a field that is wrong, a question in a dialog —
  stays where it is: a message that takes itself away is no use for something
  that has to be dealt with.

- More keys, and every control that has one says so. "Archive (E)" rather than
  "Archive", because a shortcut nobody is told about belongs to whoever wrote
  it. `j` and `k` move down and up the list.

### Changed

- Archiving what you are reading opens the next conversation instead of
  emptying the pane. The reason somebody archives what is in front of them is
  to get to the next one.

- A draft opens inside its conversation rather than on a page of its own —
  the one place in the program where a half-written reply was shown without
  the thing it replies to.

- What is attached is a line above the message rather than a table under it.
  For anything with a quoted thread beneath it, the file the message was sent
  to deliver was a screen of somebody else's words away.

- Every action in the mailbox says what it did, not only the four that move a
  conversation: acting on a subscription's mail, muting a list, trusting its
  pictures, leaving it, and forgetting contacts all said nothing at all.

- The list pages — roles, groups, users, tokens, sessions, passkeys,
  templates — report through the same line instead of each keeping a red block
  above its own list. Dialogs and forms keep theirs: a question on the screen
  is still being asked, and a field that is wrong says so beside itself.

- On a phone, a card is a band at the width of the screen rather than a box
  inside a box inside a box. Measured at 390px, the page, the card and the row
  together spent 104 pixels — a quarter of the screen — on the space between
  three borders that say the same thing; the people list now gives its text 82%
  of the width where it had 72%, and the profile form 94% where it had 84%.

- A reply written inside a conversation starts at the height of a reply and
  grows as one is written, instead of opening as 240 pixels of empty box above
  the thread it answers. What is attached to it is a row of chips rather than a
  list down the page.

- A table on a phone runs to the edges of the screen. It stays a table and
  scrolls sideways, which is what a table does when it is wider than the
  screen; the page's padding was holding one that had no width to spare 24
  pixels away from the glass.

- And the mailbox runs to the edges of a phone. Four frames stood between the
  screen and the message inside it — the page's padding, the mailbox's border,
  the pane's padding, the message's own box — and a message was rendered in 302
  pixels of a 390 pixel screen; it gets 352 now. The row of actions above it
  wraps onto a second line rather than scrolling: nine actions in a 360 pixel
  pane put 111 pixels of them past the edge, the overflow menu among them,
  reachable only by dragging a toolbar that does not look draggable.

### Fixed

- The foot of a table reads on a phone. The control that chooses how many rows
  to show was as wide as one holding words rather than a number — 160 pixels,
  nearly half the screen — and what it crowded out did not move aside but
  broke: "383 messages" over two lines, "1-50 of 383" over three. The control
  is the width of a number now, each of those is one fact on one line, and the
  bar wraps instead of the words.

- A dropdown opens where there is room for it. The list is fixed to the
  window, so one drawn past the bottom edge cannot be scrolled to — it is
  simply gone, and the control that chooses how many rows a table shows sits
  at the foot of the page, which is exactly where there is no room below. It
  opens upward when there is more room above, is no taller than the room it
  has, and stays inside the window sideways as well.

- The name in a mailbox row gives way before the marks beside it. It was a bare
  run of text in a flex line, which cannot be shortened, so a long list of
  recipients pushed the count and the "Draft" badge out of the row instead —
  and the badge saying an answer was begun and left is the one thing in the row
  somebody needs to see. This was not only a phone: the list column is 380
  pixels at its widest.

## [0.17.3] - 2026-09-09

### Security

- The single sign-on state cookie is always marked `Secure`. It took the request's word for whether it arrived over TLS; there is no sign-in through an identity provider that does not. (#70)

## [0.17.2] - 2026-09-09

### Fixed

- `teanode user update`, `teanode user password` and `teanode user delete`
  failed on every invocation. They named an account by its username after the
  API moved to an identifier, so the whole write half of the `user` command
  group was unusable from the command line. They resolve the name now, asking
  after the signed-in account before listing everybody, so changing your own
  name and password still works without the permission to administer others. (#68)
- `teanode report show` failed on every invocation: it asked for the parsed
  report without saying which parts of it to return. (#68)
- Creating a role in the dashboard failed before it reached the server, and
  saving a domain's mail servers from its DNS page was refused for the type it
  declared. (#68)

## [0.17.1] - 2026-09-09

### Security

- A person who could edit their own account (new in v0.17.0) could rename it to the console's reserved name and hold every permission from then on. The name is refused everywhere an account is named. (#66)
- The command line sign-in page put an unchecked query parameter into a command it offered to paste; a crafted link could carry a shell command in it. The name is checked at both ends now. (#66)
- A spoofed `List-Id` borrowed a reader's "always load pictures" answer for a list they trust; the answer now applies only to messages that passed DMARC. (#66)
- Twenty-two standard library vulnerabilities reachable from this code, among them panics in certificate checking reached from passkey sign-in, are cleared by requiring Go 1.26.6. (#66)
- A permission over one domain opened templates, layouts, deliveries, reports and open-tracking of every domain to whoever held it. Each is now scoped to the row's own domain. (#66)
- A credential restricted to one address could send as anyone at its domain by writing a different `From` line; the header is now held to the restriction too. (#66)
- SPF `ptr` passed for any name the sender's reverse zone claimed, which bypassed DMARC for every domain using it. A name now has to resolve back to the connecting address, and match on a label boundary. (#66)
- One message from anyone on port 25 could cost hours of CPU through folded headers, nested multipart bodies, DKIM signatures or a DMARC report; each is bounded, and so are command lines, connections and the time spent handling a message. (#66)
- The dashboard is no longer served in the clear on port 80 when the server serves HTTPS; it redirects, and sends `Strict-Transport-Security`. `X-Forwarded-Proto` is believed only from a listed proxy. (#66)
- A message with two `From` headers, which DMARC checked one of and mail programs show the other of, is refused. (#66)
- `<noscript>` carried markup past the message sanitizer into the frame; it and the other raw-text elements are removed. A quoted message no longer brings its stylesheet into the compose editor. (#66)
- IMAP enforces `mail:write`; a mailbox rule cannot forward a message in a loop; a forwarded message's `Delivered-To` is added on rule forwards too. (#66)
- Forwarding to a mail server with a password verifies its certificate; the send endpoint and passkey sign-in count against the rate limiter; a refused app-password sign-in takes as long as a checked one; a password change ends the other sessions. (#66)
- The command line client escapes terminal control characters in mail it prints, does not follow redirects with its token, creates its profile file private, and keeps the token off the command line. (#66)
- Smaller: forged `Authentication-Results` naming this server are removed on arrival; `rsa-sha1` is refused; a `_dmarc` name with another TXT record beside the policy no longer refuses the domain's mail; an ARC chain that cannot be validated is a failed chain rather than a refusal; signed bounce addresses compare in constant time; `safefetch` refuses the 6to4, Teredo and NAT64 prefixes; list queries are capped at 1000 rows. (#66)

## [0.17.0] - 2026-09-09

### Added

- Loading a message's remote pictures is remembered. Blocking them by default
  is right — loading one tells the sender the message was opened, and from
  roughly where — but asking again every time the same message is reopened
  protects nobody, since the sender was told the first time. A list can also be
  trusted once and for all: a newsletter is pictures with a few words around
  them, and "always show pictures from this list" is one answer instead of one
  per issue. Both are per mailbox, since two people who received the same
  message decide separately, and an administrator reading somebody else's mail
  in the audit pages records nothing and is always asked.

- Keyboard shortcuts for the message you are reading: `e` archive, `r` reply,
  `a` reply to all, `f` forward, `s` flag, `m` read or unread, `!` junk, `#`
  delete, `u` back to the list, and `?` for the list of them. Every one is
  something the toolbar can also do — a shortcut for something with no button
  is a feature only its author knows about. They are ignored while you are
  typing, ignored with Ctrl or Cmd held, and ignored behind a dialog, which is
  the difference between a shortcut and a trap.

- A press leaves a mark. Buttons, menu rows and the sidebar draw a ripple from
  where the pointer went down, which matters most where the thing pressed does
  not visibly change. Drawn in a layer of its own rather than inside each
  control, so no layout anywhere changes to make room for it, and not drawn at
  all for a reader who asked for less movement.

- Contacts can be chosen and forgotten together, the way messages are: a
  checkbox on each row, one at the head that takes the page, and the action
  beside the filter only while something is chosen. Each contact carries the
  mark its domain publishes when there is one — behind the rule the
  subscriptions list uses, so a mark is never drawn beside an address whose
  mail failed its checks.

- Writing a message has a place in the rail, above the Inbox. It is not a
  folder, so it is not drawn as one.

- An empty reading pane holds the figure from the front page: a dashed line
  across it with an envelope following, drawn differently every time and
  already part way along.

### Changed

- "Manage" has moved from the foot of the rail into the account menu, beside
  Settings. It sat among the mailbox's folders looking like one more of them,
  when it is the same kind of thing as Settings: somewhere that is not the
  mailbox, entered on purpose.

- The list of people is a line each: a monogram, what to call them, and what
  they sign in with. Groups have a page that says what they mean, and when an
  account was made is not read down a list.

- Sessions are one list rather than two — "this browser" and "other browsers"
  were two cards holding rows that a badge already told apart. Whether signed
  out sessions are shown is a button beside "Sign out everywhere" rather than a
  checkbox under it, and the tokens page has the same button for revoked ones.
  Revoking a token, and renaming or removing a passkey, are icon buttons like
  every other row in the program.

### Fixed

- Saving your own profile. The page named the account by its username and the
  new name by another argument, and the schema had stopped taking either, so
  the save failed with a GraphQL error and changed nothing. Underneath that,
  changing your own name needed the permission to administer everybody: your
  account is now yours to change, except whether it may sign in and which
  groups it is in, which stay administrative.

- Inserting a link in the rich text editor reloaded the page. The address
  prompt was a form, the composer around it is a form, and a form inside a form
  is dropped by the browser while parsing — so the insert button submitted the
  composer.

- Reading a subscription shows the mail the list counted. Trash and Junk are
  left out of the count on purpose, and the reader left out neither, so a list
  said it had three messages and showed five.

- Trusting a list's pictures shows the ones already on the screen, rather than
  waiting for the list to be opened again.

- The last row of the rail has room under it, instead of touching the edge.

## [0.16.0] - 2026-09-09

### Added

- Subscriptions can be muted: the list keeps arriving and stops being in the
  way, filed in the Archive and already read. The other answer to a newsletter,
  and often the better one — leaving tells the sender that a person reads this
  address and cannot be taken back, some lists offer no way out at all, and a
  reader may want the mail without wanting it first thing. Muting also clears
  what the Inbox is already holding from that list. Mail the filter called spam
  still goes to Junk: muting says where mail you asked for should go, and is
  not a way past the filter.

- Newsletters whose unsubscribe was removed in transit are subscriptions again.
  A sender never writes `List-Unsubscribe-Post` on its own — it exists only to
  promise that the address in `List-Unsubscribe` answers a POST — so finding it
  alone means the address was taken out on the way here, which is what the
  relays that hide a reader's address do. Measured against a real mailbox, it
  recovers six of the eleven messages that arrived through one. They group and
  archive like any other list; what cannot be offered is a way out, and the
  page says why rather than leaving it looking like the sender withheld one.

### Changed

- A mailing list does not become a contact, and neither does an address that
  says it takes no replies — no-reply@, do-not-reply@ and the rest. Nobody
  corresponds with either. They filled completion with addresses that can never
  be written to and, worse, made the "sender is known" rule true for exactly
  the mail that rule exists to tell apart from a stranger's.

- A message that belongs to a list opens that list, instead of asking about
  leaving in a second place with a second dialog. Leaving is one decision made
  in one place, next to muting and to everything that list has ever sent — and
  it is drawn with the same icon wherever it is offered, which it was not.

### Fixed

- The subscriptions page pages. It asked for 200 and printed the true total
  beside them, so a mailbox with more said two different things at once.

## [0.15.1] - 2026-09-09

### Changed

- The built-in spam filter's classifier now waits until it has learned `minimumMessages` of each kind, spam and not spam, before it contributes, rather than that many in total; and a verdict of "not spam" is worth at most a third of `bayes.weight`, where "spam" is worth all of it. A classifier with a small spam corpus was vouching for every message on the server, phishing included. (#64)
- A valid DKIM signature and an aligned DMARC pass each count -0.3 rather than -1.0, and an ARC pass -0.5, since a throwaway domain gets all three for free. (#64)
- Two more things the server already knows are scored: a host announcing itself under a different domain from its reverse DNS name (one point), and a delivery without TLS (half a point). Neither can reject a message on its own. (#64)

## [0.15.0] - 2026-09-08

### Added

- Help publishing your own logo, on a domain's page. TeaNode shows the mark
  other domains publish for their mail; this is the other side of it. The DNS
  tab has a BIMI row that says what to publish and, when something is stopping
  it, what — most often that the domain's DMARC policy is none, which is the
  policy this same page recommends starting with, so every domain begins
  unable to use one and nothing said so. Upload an SVG and this server hosts
  it, so a domain with no web server has somewhere to put the file; the record
  the page offers names that address. The file is checked first against the
  restricted profile a mark has to satisfy — no script, no animation, nothing
  fetched from elsewhere, square — and refused with the element that is wrong,
  because a receiver refuses the same file silently and the sender never
  learns why. The row then verifies the whole chain rather than the record's
  existence: the logo is read, and checked as a receiver would check it.

  It also says, before anybody starts, that Gmail and Yahoo show a mark only
  for senders holding a Verified Mark Certificate — issued against a
  registered trademark, renewed yearly, on the order of a thousand dollars —
  and that other receivers show one without. Nothing here issues one, and
  nothing here says "verified".

- A published logo can be withdrawn, from the dashboard or the command line.
  `DeleteBimiPublication` was written and never called, so a mark could be
  replaced but never taken down: an operator who published the wrong artwork,
  or who stopped using a domain, had no way out but an edit to the database
  while the record went on naming a file this server went on serving.

- `teanode domain logo show|publish|remove` and `teanode mailbox subscription
  list|show|mail|unsubscribe`, so this week's two features are reachable from
  the command line as commands rather than only through `teanode api call`.
  Publishing a logo sends a file, which nothing in the command line did
  before. `teanode domain check` now also prints what would stop a published
  record having any effect, which until now only the dashboard said.

- `teanode token revoke --user`, which `create` and `list` already took. Without
  it the server's own console could issue tokens and list them but never revoke
  one — which is exactly where somebody who has lost their token is standing.

### Fixed

- Publishing a picture or a logo for a domain now asks for `domain:manage`
  over that domain, as every other operation on a domain does. Both uploads
  asked only that the caller was signed in, so anybody with a mailbox on the
  server could have replaced any domain's published mark, or added a picture
  to any domain's store to be served from that domain's name. The routes that
  serve bytes are unchanged: what they serve is public either way. A test now
  reads this package's own source and fails when a route that changes
  something asks for no more than a session — the GraphQL side has had that
  guarantee for a while, and these routes were the blind spot in it.

- Listing domains asked the database once per domain for its logo, and asked
  it for domains the caller is not allowed to see. One query now, for the
  domains that survive the permission filter.

- The address a BIMI record names is the one the domain already publishes its
  pictures under, rather than the name of the node answering. Those are often
  different — a server called mx1.example.com serves mail.example.com — and a
  record in DNS has to keep meaning the same thing after that machine is
  replaced. A logo published at the old address is still read from storage
  rather than fetched, so a record written before this keeps verifying.

- A domain with everything published no longer reads as one record short
  because nobody has uploaded a logo. The domain list and a domain's overview
  counted optional records as missing; the BIMI row made that visible on every
  domain at once, but an AAAA record has always counted the same way.

## [0.14.0] - 2026-09-08

### Added

- Subscriptions: the mailing lists a mailbox receives, on a page of their own,
  with the way out of each. Most of what arrives in a mailbox is not a letter,
  and the only way to stop one was to open it, find the word "unsubscribe" in
  the small print at the bottom, and hope. A newsletter says how to leave in
  its headers — this reads them. Each row says who sends it, how much of it is
  here and unread, and when it last wrote; a button asks the sender to stop,
  in the way that sender said to ask; and the list's mail can be read together,
  newest first, the way a conversation is. A newsletter open in the ordinary
  reader carries the same button in its toolbar.

  The request is made by this server rather than by the browser, so that
  leaving a list does not tell the sender which address opened which message at
  what moment, and it goes through the same guard that stops the image proxy
  fetching an address inside this network. It asks before it acts, every time,
  and says which of the three things will happen — one request, a message from
  your address, or a page for you to open — because an unsubscribe tells the
  sender that a person reads this address and cannot be taken back. Mail in
  Trash and in Junk is not counted: what you threw away is not a subscription
  you have, and what a filter caught is not one you agreed to.

- The logo a sending domain publishes for its mail, beside the subscription it
  sends. BIMI is a DNS record naming an image, and it is shown only for mail
  that passed DMARC — a mark is a claim about who sent something, and one on
  unproven mail helps whoever is pretending to be them. Fetched by this server
  once a day per domain rather than by the browser, for the same reason the
  images in a message are, and served as a picture the page is told to run
  nothing from. A sender that publishes none gets a monogram. The certificate a
  record names is stored and not checked, so nothing here says "verified".

## [0.13.1] - 2026-09-08

### Fixed

- A remote image in a message could be fetched from a different address than
  the message named. The dashboard undoes the escaping the server applied to
  the blocked address, and it undid the ampersand before the entities an
  ampersand can spell, so an address holding `&amp;lt;` was decoded twice. It
  also looked for a quote written the one way this server never writes it, so
  an address holding a quote or a carriage return was not decoded at all. A
  sender chose what any of those produced. Found by CodeQL. (#61)

## [0.13.0] - 2026-09-08

### Added

- What a message's checks said is in the mailbox, not only on the audit page:
  a shield beside the time in the list, colored by whether the sending domain
  authorized the server, whether the signature held, whether the domain's own
  policy was satisfied and what the spam filter scored it, with all of that in
  its tooltip — and the same in words on the message itself.
- A message carries a link to its audit page, in its own menu, for anyone who
  may read one: where it came from, every delivery attempt, and the raw
  source.
- Tooltips are the dashboard's own rather than the browser's. A native title
  waits about a second, is drawn in the operating system's colors, and cannot
  wrap — a timestamp with a zone name in it came out as one long line in a
  font nobody chose. Every relative time and every icon-only row action uses
  the new one.
- Report junk moves a conversation to Junk and teaches the spam filter what it
  is, in one action, from the reader or over what is selected in the list.
  Moving without teaching leaves the next one from the same sender in the
  Inbox; teaching without moving leaves you looking at what you have just
  called junk. In Junk the button reads Not junk and does the opposite.

### Fixed

- About is the Server page's first tab, and where /server lands: what this
  server is and the upgrade waiting for it, which is what the dot in the rail
  is pointing at. It was last, behind eleven tabs of settings.
- The Server page marks the tab that holds the reason for the dot in the
  rail. The rail said "there is something here" and the page it opened said
  nothing about which of its seven tabs meant it.
- The buttons under the release notes have room above them. They sat against
  the last line of the changelog, reading as part of it.
- A tooltip no longer appears after a tap. A tap emulates a hover and focuses
  what it touched, so the box arrived a third of a second after the button had
  done its job — over the menu that had just opened, describing the button
  underneath it. Tooltips are for a pointer and for keyboard focus.
- On a phone an action stays on the right when it drops below what it acts
  on. Stacked to the left, an action that is one icon — the plus that adds a
  member, the cross that takes one out — was a mark alone on a line, reading
  as something the heading had said rather than as a button.
- The mailbox's rows of actions are one line that scrolls sideways on a
  phone. Ten icons need four hundred pixels and a phone has three hundred and
  ninety, so they wrapped, and the second row pushed the message down and left
  one icon sitting alone under nine.
- What you tap on a phone is big enough to tap. The box that selects a
  conversation and the star that flags it were thirteen pixels wide in a row
  forty pixels tall; a table's page arrows were squeezed to seventeen; a
  group's name on a person's row was a chip too short to hit.
- The box that ticks every conversation sits in the column of the boxes that
  tick one. It was two pixels to the right of them: the row of actions above
  the list had a narrower inset than a row of the list, and a checkbox carries
  a margin of the browser's own.
- Nothing on the page changes size when it is chosen, hovered or ticked. The
  row being read in a list of groups or roles carries a pencil and a bin, and
  grew seven pixels taller than the rows around it, so choosing one moved the
  list you were choosing from; the mailbox's row of actions appeared when the
  first message was ticked and pushed the list down fifteen pixels, so the
  second message you meant to tick had moved. Three more said the same thing
  with a heavier weight — the tabs, a chosen menu item, a ticked row — and
  bolder text is wider text.
- Every tooltip left in the dashboard is the dashboard's own. Twenty-five were
  still the browser's `title` — the rail's collapse and refresh, the table's
  sort headers, its page arrows and its clipped cells, the editor's toolbar,
  the picture and clear buttons, a session's browser, a DNS value, a spam
  check's meaning, who a message is from, and every tab that carries an
  explanation. They waited a second, were drawn in the operating system's
  colors and could not wrap.
- The audit log's disclosure points down to open a row and up to close it. It
  was the same arrow in both states, which says the button does the same thing
  twice.
- A tooltip appeared in the top left corner of the window rather than beside
  what it describes. It was measured from its own anchor, which is drawn as
  nothing at all so that it does not become an item of the row it sits in —
  and an element with no box measures as zeros.
- A row of a tick list with two lines in it — a permission and its key, a
  role and what it is for — was squeezed to the height of one and drawn over
  the row below it.
- A session, an API token and a passkey record who used them rather than what
  forwarded the request, the same way the audit log now does. Behind a CDN the
  list of sessions said every one of them was used from one address in another
  country. The limit on how often a password may be tried counts against the
  client too: shared across everybody behind a proxy, one person guessing
  passwords used up the allowance for the rest.
- The audit log records who asked, not who forwarded. Behind a CDN every row
  read as one address in another country, because the address a request
  arrives from is the proxy's. The forwarded-for header says who the client
  is, and is now read — but only when the connection itself came from an
  address listed in the new `server.trustedProxies`, since anybody who can
  reach the server directly can otherwise write their own address into the
  audit trail. Single sign-on was already reading that header without asking,
  and no longer does.
- An audit row says what the thing is called and links to it: the alias
  `sales@example.com` on its domain's page rather than an identifier, the
  group, the role, the person, the mailbox, the app password. Something since
  deleted is named from the row's own snapshot, which is the case that most
  needed it.
- An audit row of a change shows the fields that changed, each with what it
  was and what it is. A mailbox's rules came out as a list of several thousand
  numbers, since the snapshot is JSON and the API had been describing it as an
  array of bytes.
- A person created while there is no Members group joins no group at all,
  which is no permissions at all — they can sign in and read nothing. The
  server says so in the log, and the page no longer promises a group that is
  not there.
- A reply saved as a draft belongs to the conversation it answers. The headers
  that say what a message answers were written when one was sent and not when
  one was saved, so a half-written reply left the thread it was written in the
  moment the page was left, and turned up in Drafts as a conversation of its
  own.

### Changed

- What an audit row changed is a table: a column of fields, what each was, and
  what it is. As cards across the width, five fields came out in five places
  and the eye had to find each one before it could read it.
- The mailbox picker in the rail and the rows-per-page control under a table
  are the dashboard's own dropdown. A native select opens its list in the
  operating system's colors, which on a dark rail is a white rectangle.
- The mailbox's two toolbars are icons with their names in the tooltip. Nine
  verbs across the top of a conversation — back, reply, reply to all, forward,
  mark unread, flag, archive, report junk, move, delete — wrapped onto a
  second line on anything narrower than a laptop, and every one of them is
  something a mail program already has a picture for. Move to is a menu the
  button opens, rather than a dropdown that had to be read before it could be
  used.
- A message's headers are a list of names and values, with a button that
  copies the block exactly as it arrived. Read as it came off the wire, a
  Received: is four folded lines of one header and the reader has to find
  where each one ends before they can find the one they want.
- A role is managed on the page too, beside the list of roles: what it is
  for, and the sixty-odd permissions it may hold, each with its key under its
  name. They were in a dialog, which for a list that long meant scrolling a
  box that covered the page to reach the one permission you came to change.
- A list where one thing is being looked at says so the way the rail does: a
  raised pill on the row you are on, in the same weight as every other row,
  because bolder text is wider text and the list would shift as you moved
  down it.
- Users and groups are their own tabs, and a group is managed on the page
  rather than in a dialog. Everything a group is — who is in it, the roles it
  holds, the domains those roles apply over — was three lists inside a box
  covering the page, so reading who was in a group meant opening it, reading,
  and closing it again. They are three panels beside each other now: the
  groups, the members of the one being read, and what it carries. Ticking a
  role or taking somebody out saves as it is done. A person's groups are
  shown on the users tab as chips that lead to the group.
- Every date and time the dashboard writes out names its time zone. One
  without a zone is ambiguous the moment it is read anywhere but the machine
  that rendered it, and these are pasted into tickets and compared against
  logs from a server in another country.
- People, groups and roles carry their actions as icons rather than as a
  column of underlined words beside every name. A list of names had more text
  in its actions than in the names.
- A message's body is padded on every side. Whatever came first in it — the
  details, or the notice about blocked images — sat against the border, since
  the line that used to be there carried the padding.
- A conversation says when it holds an unsent message, and shows it — folded,
  since a draft is something to go back to rather than something to read.
  Clicking it, or Edit draft, opens what was written where it was written.
  Reply, reply to all and forward answer the newest message of the
  conversation rather than your own half-written one.
- A message says who it was addressed to in one muted line, the way a mail
  program does. From, To, Received and what the checks found were four
  labelled rows above every message in the conversation, saying at length what
  the line above already said; they are behind "Show details" in the message's
  own menu now.
- The quoted message a reply or a forward carries is folded away while the
  answer is being written, with a link to unfold it. It is hidden rather than
  removed, so it is still in what is sent — but what is being written is the
  answer, and in a conversation the thing it answers is on the screen already.
- Lists of things to tick — a group's people, its roles, its domains — are
  panels with a heading and a rule, and their rows are rows. Each one carried
  the bottom margin of a form field and was drawn in the muted color of a
  field's helper text, so a group of three roles was a dialog of paragraphs
  that read as disabled.
- Copy and blind copy are fields on the compose form like any other. They were
  behind a "Cc / Bcc" link, which made two ordinary boxes into something to go
  looking for and put a link where the form's rhythm wanted a label.

## [0.12.3] - 2026-09-08

### Fixed

- A message whose sender's SPF record could not be evaluated — a broken record, or a lookup this server could not complete — was refused with a permanent error even when its DKIM signatures satisfied the sender's DMARC policy. It is now accepted when DMARC passes, judged on DKIM alone when there is no DMARC verdict, and refused temporarily rather than permanently when the lookup failed only for now. (#57)

## [0.12.2] - 2026-09-08

### Fixed

- A message sent from a mail program showed twice in Sent: once filed by the server when it accepted the message, once uploaded by the program afterwards. The program's copy is now recognised as the message the folder already holds. Copies made before this release remain and can be deleted. (#56)
- The size recorded for a message uploaded over IMAP counted the body alone. (#56)

## [0.12.1] - 2026-09-08

### Fixed

- A message delivered into a mailbox was shown as a delivery still being retried, with no error, until the retry schedule ran out and marked it dropped. The message was in the mailbox the whole time; only the record was wrong. Existing rows in that state are corrected at their next scheduled retry after upgrading. (#55)

## [0.12.0] - 2026-09-08

### Added

- Mail reads as conversations. A folder lists one row per conversation, with
  who has written and how many messages it holds; opening any message shows
  the whole conversation, newest first, with the messages you have read
  collapsed to a line you can click open; and the messages of it that live in
  other folders are in it too, so your own answers — which are in Sent — are
  where they belong. Reply, reply to all and forward open the composer at the
  top of the conversation rather than on a page of its own, and sending puts
  the answer at the top of what you were reading without leaving it.
- A written design guideline for the dashboard,
  `docs/coding/frontend-design.md`: which shared component to reach for, where
  a panel's action goes, when a row action is an icon and when it is a word.
  `CONTRIBUTING.md` points at it.

### Changed

- The dashboard's panels are consistent. Mailbox settings, a domain's aliases
  and credentials, the sessions page and the server page all draw a panel the
  same way now: a heading, the sentence that says what it is for, and the one
  action that adds to it, in the arrangement that folds onto a phone instead
  of squeezing beside the heading. A form's fields are capped inside a panel
  the width of the page, rather than the panel being narrow beside a wide one.
- Folders, app passwords, aliases and credentials are made and changed in a
  dialog. A form under a list moved the list while it was being read, and a
  row that turned into a form made the page jump.
- An app password and a domain credential are shown in the dialog every other
  one-time secret uses, with a copy button, rather than in a banner that can
  be scrolled past.
- Removing an alias or a credential asks first. Both used to go on the click.
- Signing out everywhere asks in the dashboard's own dialog rather than the
  browser's, which was neither styled nor translated. Restarting and upgrading
  the server ask the same way.
- Mailbox rules move and are removed with the same icon buttons the folder
  list uses, and the tab has a heading like the tabs beside it.
- "Out of office" is "Auto reply", which is what it is when it is used to say
  a reply comes from somewhere else.
- The rail draws a rule under what is pinned to the top and another above
  Contacts, so the inbox, the folders, and what is about the mailbox rather
  than in it read as three lists instead of one long one. The rule under the
  pinned area is drawn whether or not anything is pinned, so the rail does not
  change shape the first time somebody pins a folder.
- Pinning is done on the Folders tab, beside renaming and removing, and the
  rail has no pin on its rows. A control that appeared only under the pointer,
  on a row whose whole job is to be clicked, was a second thing to aim at on
  every row and a thing a phone could not reach at all.
- The rail names the mailbox only when there is more than one to choose
  between. One mailbox named above its own folders was a heading that said
  nothing. The "Folders" heading over the list goes for the same reason: the
  rule above it already says where the list starts.
- Pinning a folder lifts it out of the tree rather than copying it. It used to
  appear twice, which made the rail longer the more of it you pinned. A pinned
  folder brings its own subfolders up with it and sits at the top level of the
  pinned area, whatever it was nested under.
- The button that unpins a folder is a pin with a line through it, in the rail
  and on the Folders tab. Both states used to be the same picture.
- Mailbox rules are a list of what each one does, and a rule is added or
  changed in a dialog. The tab used to be every rule open as a form at once,
  which is the wrong shape for the thing people come to it for: finding out
  where their mail is going. Turning a rule on and off and moving it up and
  down stay on the row, since those are what is done to a rule most often.
- The automatic reply is written in the editor the compose page uses, like the
  signature.
- A mailbox's signature is written in the editor the compose page uses, with
  the same Rich text and Plain text switch. It used to be two boxes side by
  side, one of them asking for HTML source — and the HTML one is what actually
  goes out, so leaving it empty quietly meant the signature appeared only on
  plain messages.

### Fixed

- A message is delivered into a mailbox once however many of a domain's
  aliases point at that mailbox. A domain with a catch-all into a mailbox and
  a named address into the same mailbox matched both for that address and put
  two copies in the Inbox. A message you address to yourself still arrives:
  being in Sent does not count as being delivered.
- Every message the server stores now belongs to a conversation. Only mail
  that arrived from outside had one; a message you sent, a draft, and a
  message a mail program appended over IMAP were all stored with no
  conversation, so a reply stood apart from what it answered.
- A message is decoded from the character set it says it is written in.
  Bodies were handed to the browser as they arrived, so a Japanese newsletter
  in ISO-2022-JP was a page of escape sequences, a Chinese one in GB2312 was
  nonsense, and a French one in Windows-1252 had a replacement character where
  every accent had been. Subjects were already decoded, because a header
  carries its character set in each encoded word while a body carries it once
  in the Content-Type — which nothing was reading.
- A message's own menu — download, headers, theme — sits at the end of the
  line that names the message rather than on a row of its own above it. In a
  conversation that was one row holding one button for every message in it.
- Archiving or deleting a conversation from Starred, or from a search across
  the mailbox, acted on every message of it wherever it was filed — including
  your own replies in Sent and an unsent draft. From a list that is not a
  folder, the row now stands for the message it shows.
- Opening a conversation from the Inbox no longer marks read the messages of
  it that are sitting in Junk or another folder.
- Marking a whole conversation unread left it looking read, because the two
  halves of the toggle were swapped.
- A conversation is ordered by when each message was written rather than by
  when its item was filed, so archiving the first message of a conversation
  no longer moves it to the top and renames the conversation after a reply.
- The IMAP settings — the host and ports a mail program is told to connect to
  for reading mail — were not among the sections written to the database, so
  they reset to the ports the process happens to bind every time the server
  restarted. A mail program set up from what the dashboard said would then
  stop connecting. The `imap` section is stored now, and a test walks the
  configuration to check that every section is.
- 183 lines of stylesheet had been pasted into the middle of a phone media
  query, which both broke the rule they landed in — long DNS record values
  stopped wrapping on a phone — and overrode the real rules at phone widths.
- A checkbox and its label on the auto reply tab and the single sign-on
  settings were laid out as a form field, one above the other, because they
  asked for a class that does not exist.

## [0.11.0] - 2026-09-08

### Added

- A server reached through something in front of it — a relay, a tunnel, a
  load balancer — can say so, under Server → Identity or as
  `server.externalAddresses`. The DNS advice then reads a mail server name
  pointing at one of those addresses as right, rather than asking for ever
  that it be changed to the address the server sees for itself, which would
  have stopped the mail.
- What a mail program is told to connect to for reading mail is settable, as
  the address for sending already was: an `imap` section with a host and the
  two ports, on the Setup page beside the sending one. A server behind a
  gateway listens on 10993 and is reached on 993, and the page used to hand
  somebody the port the process happens to bind.

### Changed

- The Setup page drops its "Getting mail flowing" checklist. Three of its
  four steps were already answered by the pages they pointed at, and the
  fourth could never be ticked.

## [0.10.0] - 2026-09-07

### Added

- The command line reaches the mailbox and access work by name rather than
  through `teanode api call`: `teanode mailbox` with `folder`, `rule`,
  `contact`, `device`, `autoreply` and `programs`, and `teanode group`,
  `teanode role` and `teanode audit`. A rule is written as
  `--when from:contains:@github.com --move GitHub`, and `mailbox rule apply`
  runs the stored rules over the mail already in a folder, which nothing
  could do before: a rule only ever filed what arrived after it.
- `ApplyMailboxRules` in the API: a mailbox's stored rules, run over a
  folder that is already filed. It moves, marks, flags and deletes as
  arrival does, and does not forward, because old mail is not sent again.
- Who may do what has its own place in the rail, beside Domains and Server,
  rather than four tabs inside the server's own page. The accounts and the
  groups are one page there: choosing a group narrows the people to its
  members, and membership is edited from either side.

### Changed

- A rule's `header`, `value`, `folderId` and `address`, and an out-of-office
  reply's `subject`, `text` and `html`, are optional in the GraphQL schema
  rather than required, so a client may leave out what it has nothing to
  say about instead of sending an empty string.

### Fixed

- A dialog fits the window it is on: a group with three lists in it was
  taller than a phone, with its title above the screen, its buttons below
  it, and nothing to scroll. A list of people is searched rather than
  scrolled past, and what is already chosen sits at the top of it.

## [0.9.1] - 2026-09-07

### Fixed

- The profile form sits like every other settings form: a heading and an
  intro line, the width cap on the form inside its card rather than on the
  card, and a saved notice where the others show one.

## [0.9.0] - 2026-09-07

### Added

- Mailboxes. Every account has one, the web UI opens on it, and an alias of
  kind "mailbox" delivers into it by reference: a message is stored once,
  however many folders hold it, and kept for as long as any does. Folders,
  flags, search over one folder or the whole mailbox with sender, recipient,
  subject, date and attachment filters, rules with a dry run, reply, reply
  all, forward, drafts whose attachments upload once with a progress bar, a
  signature, and an out-of-office reply with the protections that keep it
  from answering machines, lists or another mailbox that is also away.
  Folders nest to any depth, can be renamed and moved, and each kind has its
  own icon; the Inbox and Starred, every flagged message wherever it sits,
  stay at the top of the rail, and any other folder can be pinned up beside
  them. The list shows the sender's name; the reading pane shows the message
  as a mail program would, with download, headers and the undarkened
  original behind a menu; attachment names are searchable; contacts, kept
  from whoever you write to, have a page of their own; and every page fits a
  phone.
- IMAP, on port 993 and with STARTTLS on 143, so a mail program reads the
  same mailbox; and app passwords, one per device, which sign in to IMAP and
  to submission on port 587 with the mailbox's own addresses.
- Roles, groups and permissions. The management pages — every message, the
  queue, reports, domains, the server — are a mode behind "Manage" and show
  only what the signed-in person may do. Administrator, Operator and Member
  come seeded and all of it is editable; a group can be tied to a domain so
  its permissions reach only that far. Every change to a user, group, role,
  domain, alias, credential or mailbox is in the audit log.
- Single sign-on through an OpenID Connect provider, with a group's "IdP
  group" following the directory.
- `teanode-server user rescue`, which makes an account an administrator when
  nobody can.

### Changed

- A bare pattern on an alias that delivers into a mailbox is anchored when
  it is saved, `hello` becoming `^hello$`, because a mailbox's addresses are
  read back from its patterns. Patterns on other aliases are taken as written.
- Domains, aliases, credentials and users are rows managed one at a time
  rather than a configuration document written back whole; the
  `configuration` table holds settings only.
- A DMARC failure is refused only when the sender's policy says `reject`;
  under `none` or `quarantine` the message is accepted, scored, and — in a
  mailbox — filed in Junk when quarantined. Every reserved example domain
  publishes `reject` now, which had made a local server refuse every test
  message.
- `listen.imap` and `listen.imaps` are settings, editable on the server page,
  since the environment only describes a first run.

### Fixed

- The SPF line of a message's authentication results said the domain
  authorized the address whatever the verdict was; it now says what the
  domain's record actually said: allowed, refused, doubted, silent, absent
  or unreadable.

## [0.8.1] - 2026-09-07

### Fixed

- Two ways a legitimate message was refused on DKIM grounds with SPF passing.
  A verification *error* — the signer's key could not be fetched or read —
  was answered with a permanent 550; it is now recorded and left to DMARC and
  the spam filter. And with no DMARC policy, any non-passing signature
  refused the message, which bounced mail through Apple's private relay for
  carrying a second, broken signature beside a valid one; one valid signature,
  or SPF passing, is now enough. Eleven legitimate messages in six days.

## [0.8.0] - 2026-09-06

### Added

- Mail is scored for spam by a filter inside the server, so a deployment needs
  no second program for it. It reads what the server already established about
  a message — the SPF, DKIM, DMARC and ARC results, whether the sending host
  has a forward-confirmed reverse DNS name, the name it gave in HELO —
  consults public block lists over ordinary DNS, and applies a classifier
  trained on the mail you mark in the dashboard. Scores carry a breakdown of
  which check contributed what.
- `antispam.engine` chooses between that filter and an external SpamAssassin
  daemon. Leaving it empty is resolved rather than defaulted, so a deployment
  already talking to a daemon keeps talking to it.
- Marking a message as spam, or as not spam, in the dashboard. That is what
  teaches the classifier, and it says nothing until it has seen enough of both.
- `teanode-server config rules import` and `config rules show`, which load the
  published pattern rules into the database and report how much of a set this
  server can use. Off until `antispam.builtin.rules.enabled` is set, and there
  is no automatic download: fetching rules unattended means verifying the
  publisher's signature.
- `teanode settings set upgrade …`, which the schema accepted and the command
  line could not reach.

### Changed

- A delivery refused with a permanent 5xx reply is dropped rather than retried
  on the backoff schedule. A message Gmail had refused as unsolicited was being
  offered to Gmail again every few hours, each attempt costing reputation.
- A message's deliveries say how each one is handed on and where — forwarded
  to an address by looking up its mail servers, relayed to a configured host,
  or posted to a URL.
- The compose file no longer starts a spam daemon. It is behind a `spamd`
  profile for deployments that want one, so nothing in the default path
  depends on a third-party image continuing to exist.

### Fixed

- A domain with no spam threshold stored carried zero, which means "reject
  anything the filter has any opinion about". Harmless while scoring needed a
  daemon and was off; with scoring on by default it would have refused almost
  everything.
- `teanode settings set` named an input type the schema has never had, so it
  failed for every section, not just the one being set.

## [0.7.0] - 2026-09-06

### Added

- Read-only profiles: `teanode auth login --read-only`, `teanode auth set-read-only`, and `--read-only` or `TEANODE_READ_ONLY=1` for one command or one shell, refuse every change on this machine before it is sent. For handing the tool to a script or an agent that should be able to look but not touch. (#26)
- The client's exit code says what kind of thing went wrong: 2 for a command called wrongly, 3 for a change refused by read-only, 4 for something the server does not have, 5 for a refused token, 6 for a server that could not be reached. With `--json`, a failure is printed as JSON on standard error as well. (#26)

### Changed

- A command that would ask for confirmation refuses at once, with a `--force` hint, when standard input is not a terminal; `TEANODE_FORCE=1` answers for a whole shell. `teanode alias delete`, `credential delete`, `user delete` and `passkey delete` now ask first, like `domain delete` already did — a script doing those passes `--force`. (#26)
- A refused token, a missing message or delivery, and a typo in a `mail list` filter are now reported as what they are, rather than as the server's error value or an empty list. A list that stopped at `--first` says so on standard error. (#26)

### Fixed

- `teanode passkey list` showed the last-used address with a port after it; the port is no longer recorded. Revoking a token that is not the caller's said "session is not valid"; it now says the token was not found. (#26)

## [0.6.0] - 2026-09-06

### Added

- `database.sslRootCertificate`, and `sslrootcert` in `TEANODE_DATABASE_URL`, so a self-signed or private-authority PostgreSQL can be verified rather than merely encrypted. (#25)
- `docs/reference/deployment.md`: the compose deployment end to end, with upgrades, what to back up, and how to recover from a bad first start. (#25)

### Changed

- The connection to PostgreSQL is encrypted and verified. The compose file generates a certificate for it and starts PostgreSQL serving TLS, and the generated `TEANODE_DATABASE_URL` asks for `sslmode=verify-full` rather than `sslmode=disable`. (#25)
- `config env` says which variables to delete once the server has started, and the generated file explains why keeping them is a trap. (#25)

### Fixed

- The compose deployment could not be brought up from nothing. The SpamAssassin image it named had been deleted from Docker Hub, and compose pulls every service before starting any of them, so `docker compose up -d` failed outright. It now names a maintained image, pinned. (#25)
- `config env` wrote a data directory that the compose file does not mount, so a first start either could not create it or wrote keys, certificates and the spool into a container that the next upgrade discarded. The generated file now matches the volume. (#25)
- The compose file creates the data directory and gives it to the uid the server runs as. Docker creates a missing bind-mount source owned by root, which the server, running unprivileged, then could not write to. (#25)

## [0.5.1] - 2026-09-06

### Changed

- The sign-in form shows "Sign in with a passkey" only on a server that has passkeys turned on. (#24)
- The sign-in form no longer shows a command for adding an account; the getting-started guide's new "If you are locked out" section has the right ones. (#24)

## [0.5.0] - 2026-09-06

### Added

- The dashboard suggests adding a passkey, once, to an account that has none on a server that offers them. **Not now** hides the suggestion in that browser. (#23)

## [0.4.3] - 2026-09-05

### Fixed

- The Upgrade button installed the command-line client over the server. It
  refused to start, the running server carried on unaffected, and the
  dashboard reported that the upgrade did not finish — so nothing was lost,
  but self-upgrade has not worked since v0.3.0, when the client and the server
  became two programs. Because the code doing the upgrading is the code that
  was wrong, this version has to be installed by hand once; after that, and
  after removing `/var/lib/teanode/upgrade/pending` on an instance that
  already tried, the button works again. (#22)

## [0.4.2] - 2026-09-05

### Fixed

- The dashboard's mail list could fail to load the "opened" column when one of
  the messages it was showing had just been deleted by the retention sweep.
  Nothing was lost and the server was never at risk — the request failed and
  reloading fixed it — but it recurred for as long as the list was open. (#20)
- Log lines from the outbound SMTP client — everything about delivering a
  message to another server — were labeled `smtpd`, the name of the listener
  that receives mail, because the package declared its logger under the wrong
  name. They are labeled `smtpc` now. If you grep your logs for delivery
  problems, that is the word that changed. (#20)

## [0.4.1] - 2026-09-05

### Fixed

- The version card shows the release notes for the newest release, formatted
  as a changelog rather than as raw text, and shows them whether or not that
  release is newer than the one running. They were only ever displayed when an
  upgrade was available, which meant never on a server that was up to date or
  ahead. (#19)

## [0.4.0] - 2026-09-04

### Added

- Every setting in the configuration except the database connection can now be
  read and changed from the dashboard and the command line: the message size
  and recipient limits, greylisting, the sign-in rate limits and trusted
  senders, the DNS resolver and check interval, the session lifetime, the
  passkey relying party, the listen addresses, the server's own name, mail
  server names and log level, the message directory and spool retention, the
  GeoIP database, and the ACME contact address, challenge, directory and
  certificate files. Secrets are never returned. Settings that only take effect
  on a restart say so where they are edited, and the listen addresses ask for
  confirmation before saving. (#18)

### Changed

- A domain is one page with tabs — Overview, Settings, Aliases, Credentials,
  Templates — each with its own address, so any of them can be linked to,
  reloaded and reached with the back button. Aliases and credentials are one
  click from the domain instead of four screens down its settings page. Old
  links to `/domains/<id>` and `/domains/<id>/settings` redirect. (#18)
- The Server page's tabs are grouped around the question each answers, and the
  tab that was called DNS is Certificates. `/server/dns` redirects. (#18)
- `Remove` at the end of a row is a trash icon; a long DNS value is clamped to
  its line with a button that copies the whole of it. (#18)

### Fixed

- The last row of every list sat flush against the bottom of its own card. (#18)
- Explanatory text ran the full width of the window in some places and stopped
  at a readable measure in others, and a card could be three times as wide as
  the writing in it. (#18)
- Navigating between two domains could show "Failed to fetch" over a page that
  had loaded correctly. A dropped connection is also retried once now, which a
  browser will not do for a POST. (#18)
- Moving between a domain's pages briefly flashed "Domains" as the page
  heading. (#18)

## [0.3.1] - 2026-09-04

### Fixed

- A server running a build made from a checkout — `0.2.0-7-g8519250`, what
  `make build` stamps in when the commit is not exactly on a tag — is offered a
  release that has overtaken it. It was told about none: the version card said
  the newest release was available, which reads as up to date, and there was no
  button to press. The tag such a build was made past is still not offered, and
  a server already on one has to install this release by hand before the
  dashboard can do it for the next. (#17)

## [0.3.0] - 2026-09-04

### Added

- `teanode auth login --url https://mail.example.com` signs the client in from
  a browser: the dashboard opens, the operator presses Authorize, and the
  token comes back to the command over a loopback connection on the
  operator's own machine, tied to a nonce the page has to echo. The result
  is a profile in `~/.config/teanode/profiles.json`, one per server, with
  `auth list`, `auth switch`, `auth status` and `auth logout`, which revokes
  the token. `--profile` and `TEANODE_PROFILE` pick another for one command.
- A command for every part of the API: `domain`, `alias`, `settings`,
  `server`, `upgrade`, `session`, `passkey`, `mail`, `delivery`, `report`,
  `template` and `layout` join `user`, `token`, `credential` and `dkim`, each with
  `list`, `get`, `create`, `update` and `delete` where the API has them and
  the verbs particular to the resource — `alias match`, `domain check`,
  `mail send`, `template render`, `delivery pending`, `server restart`.
  Tables by default, `--json` everywhere. `teanode api` remains for whatever
  is added later.
- The dashboard's `/cli` page, which the client opens to sign in. It is the
  one page allowed to connect to a loopback address.
- A macOS build of the client in each release.

### Changed

- The server is `teanode-server`; `teanode` is the client. `teanode run`,
  `teanode config …` and `teanode tls …` are `teanode-server run`, `config`
  and `tls`, and `teanode user --offline` is `teanode-server user`. The
  image ships both, so `docker compose exec teanode teanode user list` still
  works. A systemd unit or a script that starts the server has to say
  `teanode-server`.
- The client no longer reads `~/.config/teanode/token`; a token kept there
  is saved as a profile with `teanode auth login --url … --token -`.
  `TEANODE_URL` and `TEANODE_TOKEN` still bypass profiles for scripts.
- `teanode user add` and `credential add|remove` keep working as aliases of
  `create` and `delete`; `credential delete` takes the identifier alone.
### Added

- `teanode auth login --url https://mail.example.com` signs the client in from
  a browser: the dashboard opens, the operator presses Authorize, and the
  token comes back to the command over a loopback connection on the
  operator's own machine, tied to a nonce the page has to echo. The result
  is a profile in `~/.config/teanode/profiles.json`, one per server, with
  `auth list`, `auth switch`, `auth status` and `auth logout`, which revokes
  the token. `--profile` and `TEANODE_PROFILE` pick another for one command. (#15)
- A command for every part of the API: `domain`, `alias`, `settings`,
  `server`, `upgrade`, `session`, `passkey`, `mail`, `delivery`, `report`,
  `template` and `layout` join `user`, `token`, `credential` and `dkim`, each
  with `list`, `get`, `create`, `update` and `delete` where the API has them
  and the verbs particular to the resource — `alias match`, `domain check`,
  `mail send`, `template render`, `delivery pending`, `server restart`.
  Tables by default, `--json` everywhere. `teanode api` remains for whatever
  is added later. (#15)
- The dashboard's `/cli` page, which the client opens to sign in. It is the
  one page allowed to connect to a loopback address. (#15)
- A macOS build of the client in each release. (#15)

### Changed

- The server is `teanode-server`; `teanode` is the client. `teanode run`,
  `teanode config …` and `teanode tls …` are `teanode-server run`, `config`
  and `tls`, and `teanode user --offline` is `teanode-server user`. The
  image ships both, so `docker compose exec teanode teanode user list` still
  works. A systemd unit or a script that starts the server has to say
  `teanode-server`. (#15)
- The client no longer reads `~/.config/teanode/token`; a token kept there
  is saved as a profile with `teanode auth login --url … --token -`.
  `TEANODE_URL` and `TEANODE_TOKEN` still bypass profiles for scripts. (#15)
- `teanode user add` and `credential add|remove` keep working as aliases of
  `create` and `delete`; `credential delete` takes the identifier alone. (#15)

## [0.2.0] - 2026-09-04

### Added

- **The server says when a new version is out, and installs it.** The Server
  page shows what is running, what has been released, the notes that came with
  it and a link to the release, and a button that installs it: downloaded,
  checked against the checksums published with it, put in place, and the server
  restarts into it — no supervisor needed, because the process replaces its own
  image once everything has been drained and closed. `upgrade.automatic` does
  the same on a schedule, off until you turn it on, optionally confined to an
  hour of the day by `upgrade.window`. (#3)
- **The dashboard notices when it has gone stale.** A yellow refresh button
  appears at the top of the rail when the server has been upgraded under the
  page you are looking at, and a dot appears on Server when a release is
  waiting. (#3)

### Changed

- **An older binary no longer reverts a newer one's migrations without being
  asked.** This program undoes migrations it does not recognize, which is how a
  deliberate downgrade works — and it cannot tell one from an upgrade that
  crashed on startup, a second instance that never got the upgrade, or somebody
  pulling last week's image to test something. A start that meets a migration
  it does not have now refuses: nothing is migrated and nothing is opened, and
  the message names the migrations, says what reverting would cost, and gives
  `TEANODE_ALLOW_MIGRATION_REVERT=true` for a downgrade you actually want. (#3)
- **Setup, Integrations and Server are one page.** They were three rows in the
  rail for one subject — what this server is, what it talks to, and which
  version it is running — which made you choose between them before knowing
  which one held the thing you wanted. They are tabs of `/server` now, Setup
  first and About last, and the old addresses redirect. (#3)

## [0.1.2] - 2026-09-03

### Changed

- `deploy/docker-compose.yml` pulls `ghcr.io/ziyan/teanode` rather than naming
  a locally built image, so `docker compose up -d` works on a machine that has
  never built this. `docker compose build` still builds the checkout. The tag
  is `latest` and the comment beside it says to pin a version: an upgrade to a
  mail server should be a thing you did, on a day you chose. (#2)

## [0.1.1] - 2026-09-03

### Fixed

- The account menu opens on a phone. It is drawn at a layer below the
  navigation rail, and on a narrow screen the rail is an overlay rather than a
  column — so the menu opened behind the rail that had just been tapped:
  invisible, with nothing on it reachable. (#1)
- The row of tabs on a message no longer runs off the side of a phone. It
  scrolls sideways instead, and the link that downloads the `.eml` is not
  offered there: a phone has little use for the file, and in a scrolling row it
  sat past the end where nobody would find it. (#1)
- Lists in the dashboard stack on a phone rather than putting a button beside a
  paragraph and leaving each of them a column four words wide. (#1)
- Every chevron in the breadcrumb has the same air on both sides. The gap fell
  on one side only, so the trail read as `Domains> example.com> Templates`. (#1)

## [0.1.0] - 2026-09-03

First open-source release. TeaNode began as the private backend of a hosted
service; this release turns it into something anybody can run for their own
domains. Almost everything below is a consequence of that.

### Fixed

- A domain can be changed one setting at a time. The domain name was a
  required field of `DomainParameters`, so the schema refused every update
  that did not carry it — before the resolver, which has always treated it as
  optional, saw the request. The dashboard sends only what it is changing, so
  saving the mail server names and moving the signing selector both failed,
  and the failure reached the operator as a button that did nothing.

- `domains[].mailServers` is written to the database. The reading half of the
  domain row mapping had the field and the writing half did not, so a domain
  configured with names of its own kept them until the configuration was next
  saved. A deployment could look correct while storing nothing, by deriving
  the same names for another reason.

- A per-message picture address is not cacheable. It set `no-store` and then
  the code that writes the bytes set a year of caching over the top of it. A
  CDN in front of the server duly kept a copy, after which the first fetch is
  counted and every one afterwards is answered by somebody else — which an
  operator reads as nobody having looked again.

- `POST /api/v1/send/{domain}/{template}` can be called again. The
  authentication middleware turned it away for having no session before the
  handler could check the credential it was called with, so it answered
  "not logged in" on any server with an account — which is every server.
  It is let through by path, the way the GraphQL endpoint is, and checks
  its credential itself.
- A message the server composes carries `MIME-Version`, wraps its base64
  at 76 columns rather than writing a part as one line longer than SMTP
  allows, and has a text part or an HTML part only when there is one: a
  `multipart/alternative` with an empty half is a message some clients show
  as blank.

- The port shown beside a new credential is the one a mail client can reach,
  not the one the process binds. Those are the same thing until something
  forwards one to the other — a container publishing 10587, a firewall taking
  587 — and then the dashboard was handing somebody a number nothing answers
  on. `smtp.submission` sets what to advertise, host and port, and both are
  editable on the Setup page; leaving them empty keeps the old behavior of
  following the server.
- The Setup page no longer describes settings as living in `teanode.yaml` and
  being reloaded with a HUP signal. Neither has been true since configuration
  moved into the database.

- Links in a message can be clicked. The sanitizer had been putting
  `target="_blank"` on every link it kept since it was written, but the frame
  showing the message was sandboxed with `allow-same-origin` alone — and a
  browser silently drops a `_blank` click without `allow-popups`. Nothing
  reported an error; the link simply did nothing. The frame now also allows
  the opened tab to escape the sandbox, because a link that "works" and lands
  on a page with no scripts and no origin is worse than one that does not
  open. Scripts, top navigation and forms are still refused, so a message
  cannot run code, cannot navigate the dashboard away from under the reader,
  and cannot submit anything.

- Every page under `/settings` names itself in the breadcrumb and the tab
  title. The settings list already described itself as the one place a surface
  is declared, and the breadcrumb was documented as reading it, but nothing
  did: each page had to remember, and five of the six did not.
- A credential created through the dashboard can send mail immediately. The
  lookup tables are built on first read, and the new store did not mark them
  stale after a change, so a create — which reads the configuration to check
  for a duplicate before appending — left the new credential invisible to
  every lookup until the process restarted. Submitted mail was refused as
  "Invalid credentials" while the credential sat plainly in the configuration.
- Mail to a domain served by this same server is refused as a loop. The check
  compared the domain's MX records against `server.name`, and the MX records
  the dashboard asks an operator to publish name `server.mailServers` — a
  separate list. Wherever the two differ, which is the arrangement the panel
  itself recommends, nothing ever matched and the mail went round.
- The address records asked for are the ones the MX names, not `server.name`.
  With `server.mailServers` set those are different, and the page was asking
  for a record on a name nothing pointed at while not checking the names that
  mattered.
- An AAAA record is marked optional rather than missing. A server reachable
  over IPv4 alone is correctly configured, and coloring it the same as a
  missing MX teaches the reader to ignore the color.
- A catch-all alias can be created again. An empty pattern is a catch-all —
  the configuration layer, the documentation and every existing deployment
  read it that way — but the API refused it as a missing value, while allowing
  an alias to be *edited* into one. The form now says an empty box catches
  everything else, rather than leaving it to be guessed.
- STARTTLS is only advertised when there is a certificate to complete it with.
  A server that has not obtained one yet — the first minutes of a new
  deployment — offered it anyway, and a sender that took the offer failed the
  handshake. Some retry immediately without encryption, so the mail arrived in
  plaintext and nothing recorded that it had.
- A published DKIM key is recognized when it omits the version tag. RFC 6376
  makes `v=` recommended rather than required, defaulting to DKIM1, so a
  record of the form `k=rsa; p=…` is valid and every verifier accepts one.
  The dashboard did not, and reported working keys as needing to be changed —
  which is worse than saying nothing, because it asks somebody to edit DNS
  that was already correct.
- A dns-01 certificate order waits for the challenge record to appear before
  asking the certificate authority to look for it. The wait polled a list of
  nameservers that is optional to configure, and when it was left empty the
  wait was skipped entirely — so every order failed. Unset now means "look up
  the zone's own nameservers", which is what the code always claimed to do.
- A failing certificate order gives up instead of retrying forever. Combined
  with the above it produced a new order every second or so, which is the
  fastest possible route to a certificate authority's failed-validation limit.
  Three attempts, backing off, then it waits for the next scheduled run.
- An unclaimed server no longer answers everything. It used to open the whole
  API while no account existed, so anyone reaching a freshly started server
  could read its domains, aliases and signing selectors, and then claim it.
  Creating the first account still works; nothing else does until somebody has.
- The container runs as an ordinary user rather than root, keeping only the
  capability to bind the low ports. **Upgrading:** the mounted configuration
  and data directories were created by root and have to be given to uid 65532
  once, with `chown -R 65532:65532`, or the server cannot read them.
- Every known vulnerability the code could reach is gone: the build now
  requires Go 1.25.14 and five dependencies moved forward. The one that
  mattered was reachable before authentication — a remote sender could spend
  the server's CPU through the address parser on any `RCPT TO`.
- A failed SMTP authentication no longer writes a working credential to the
  log. The error named the password that would have been accepted, and the
  client chooses the half the server does not derive, so one rejected login
  plus read access to a log file yielded a usable credential. Token
  verification had the same shape. Both now compare in constant time and say
  only that the credential was invalid.

### Added

- **Pictures in a template, served from your own domain.** Upload a picture in
  the layout or template editor and it is stored — on disk, and in the object
  store as well when one is configured — with a row in `media` holding what it
  is and which domain it belongs to. The editors insert it; the preview shows
  it; a megabyte is the limit; and only PNG, JPEG, GIF and WebP are accepted,
  decided by reading the bytes rather than believing the name. SVG is refused
  on purpose: it is a document that can carry script, and it would be served
  over HTTPS from your own domain.

- **Whether a message has been looked at.** Every picture in a message sent
  from a template gets an address of its own, under the sending domain, and a
  fetch of it is recorded — first time, last time, how many times, and from
  where. The mail list has a Pictures column and the message's page has a card.
  Both say what the number is worth: a mail program asked for a picture, which
  is neither "somebody read this" nor, when it is absent, "nobody did". Apple
  Mail fetches every picture before the recipient sees anything; most programs
  fetch none until the reader asks. It is a floor with false positives in it,
  and the dashboard says so where the number is.

- **`domains[].linkHost`, the name that serves a domain's pictures.** Where
  mail arrives and where HTTPS answers are different questions. A mail server
  name resolves to a host whose port 443 may belong to something else
  entirely, and then the mail is delivered, signed and aligned while every
  picture in it is broken — a failure that happens in the reader's mail
  program and is invisible from the server. Empty still means the first mail
  host, which is right when this server answers there. The name has to be
  under the domain: an address in somebody else's domain tells every reader
  who runs the server.

- **Writing and sending mail from the dashboard.** New message, on the Mail
  page, sends as any address at one of your domains: from a template with its
  variables filled in, or written there in rich text or plain text, with
  attachments. The message is signed with the domain's key, recorded under
  Mail like anything a credential submits, and the page links to it once it
  has gone. Behind it is a `SendMail` mutation, so the command line client
  can do the same.

- **Templates and layouts in the dashboard.** Each domain has a Templates
  page listing its templates and the layouts they sit in; each opens in an
  editor with a preview rendered by the server, with sample values for the
  variables it reads. The variables are reported by the API too
  (`Template.variables`), and `RenderTemplate` and `RenderLayout` preview
  content that has not been saved.

- **Templates and layouts in more than one language.** A template keeps its
  name and carries a translation of its subject and content per locale;
  a layout does the same for its content. Sending names a locale — the
  dashboard's language select, `"locale"` in the send endpoint's body, or
  the `locale` argument of `SendMail` — and the closest translation is
  used: `zh-CN` finds `zh-CN`, else `zh`, else any Chinese, else the
  default. The message says which in `Content-Language`. Migration
  `0006_translation` adds the two tables and a `locale` column to each of
  `template` and `layout`; a template with no translations behaves as
  before.

- **Signing in with a passkey.** WebAuthn: the server sends a challenge, an
  authenticator signs it, and the server checks the signature against a public
  key stored at registration. The private half never leaves the phone, laptop
  or security key, so there is no shared secret to leak, phish or reuse — and
  a copy of this server's database is not a set of working credentials, which
  is the one thing a password table can never say.

  Discoverable credentials, so no username is typed: the browser offers
  whichever passkeys it holds for the site. Off by default, because WebAuthn
  binds a credential to an origin permanently and a passkey registered against
  a name that is about to change can never be used again. Settings →
  Passkeys registers and removes them, up to `passkey.maximumPerUser`.

  Half-finished ceremonies wait in the process by default and in Redis when
  one is configured, which is what makes this work behind a load balancer:
  WebAuthn is two requests and the browser has no reason to come back to the
  instance it started with. `deploy/docker-compose.yml` has a Redis in the
  cluster profile. Nothing durable is kept there — keys that expire in five
  minutes and are deleted as they are read, so one challenge answers exactly
  one attempt.

- **A content security policy** on the dashboard, and the standard headers
  beside it. `default-src 'self'`, with `script-src` naming the hash of the
  one inline script the page ships — computed from what is embedded rather
  than written down, so editing the script cannot leave a stale hash behind.
  It is the third layer under the mail a stranger sends, after the sanitizer
  and the sandboxed frame, and the one that holds if either has a hole.

- **Remote images in a message are fetched by the server**, once the reader
  asks for them. Letting the browser fetch them hands the sender the reader's
  address, user agent and the exact moment the message was opened, which is
  what a tracking pixel is for; through the server they learn only that the
  mail server looked. It is also what lets the policy above say
  `img-src 'self'` and mean it.

  The address comes out of mail written by a stranger, so the fetch is
  guarded: http and https only, no credentials in the URL, every address
  actually dialled checked against the loopback, private, link-local and
  reserved ranges — including the ones a redirect causes, which is where this
  check usually has a hole — and the reply served as an image or not at all,
  capped and timed out.

- **A Profile page**, where an account's name, the username it signs in with,
  and its notification address can be changed. Renaming takes the sessions,
  API tokens and passkeys with it, so nothing has to be signed in again.

- **DMARC reports open.** The list answered "is anybody forging me, and is it
  working"; a report now opens to show who reported it, the policy they saw,
  and what they did with each batch of mail from each source — everything that
  was already parsed out of the XML and never shown.

- **An outgoing relay**, for the deployment whose connection blocks outbound
  port 25 — which is almost every domestic ISP, and many hosting providers.
  Outgoing mail is handed to one mail server on a submission port instead of
  being delivered by MX lookup: 587 and 2525 with STARTTLS, 465 with TLS from
  the first byte. Settings → Integrations has presets for Gmail, SES, Postmark
  and Resend, which fill in the host, port and encryption.

  The relay connection is checked, unlike delivery to a stranger's MX: the
  certificate is verified against the host, and a relay that will not encrypt
  is refused rather than fallen back from. There is a name to check and a
  password about to be sent, so accepting any certificate would mean handing
  that password to whoever answered. `security: none` is refused outright when
  a password is set.

  This is also how the server sends through a provider. SES, Postmark and
  Resend all offer SMTP endpoints, so there is nothing provider-specific to
  configure and the message arrives carrying the DKIM signature this server
  already applied. Their HTTP APIs are not equivalent: only SES accepts a
  built message as-is, and Postmark and Resend take decomposed fields, so
  routing through those would mean re-signing by them.

- **Sessions are rows**, so one browser can be signed out without touching the
  others. A session used to be a cookie carrying a username, an expiry and a
  signature over both, with the server keeping nothing — which is why the only
  way to end one was to rotate the signing key, ending every session on the
  server, for everybody. Settings → Sessions now lists the browsers signed in
  to an account, marks the one you are reading it from, says where each was
  last used, and ends them one at a time. "Sign out everywhere" still exists
  and now means this account rather than all of them.
- **API tokens are rows too**, and record when each was last used and from
  where. They used to live inside the account in the configuration, which meant
  writing "last used" would have rewritten the whole configuration and had
  every instance reload it. When a token was last used is data, not a setting.
- A revoked session or token is kept for thirty days, marked revoked, so the
  list can say what happened to it rather than the row silently disappearing.
  An hourly sweep removes those and anything long expired — scheduled, not
  merely written.

- **Settings → Server**, and the API behind it. A dashboard that warns a
  restart is needed should be able to do it, rather than sending the operator
  to a terminal. `GetServerStatus` says which instance you are talking to,
  which build it is running, how long it has been up, and which settings have
  changed that it is not using yet; `RestartServer` ends the process so the
  supervisor starts a new one. The page names what it thinks will start it
  again — a container, systemd, or nothing it can see — and warns in the last
  case, because that is the operator for whom restarting means staying down.
- **Settings → Integrations**, which is the first user interface for settings
  the API has been able to change since it was written: the object store, the
  Route53 solver, and the two scanners. A secret is shown as set or not set
  rather than read back, an empty box leaves it alone, and clearing one is a
  separate deliberate act.
- The object store endpoint and path style are settings the API can change.
  They existed in the configuration but not in the API, so a self-hosted store
  could only be pointed somewhere by exporting the configuration, editing it
  and loading it back — which is what had to be done to this deployment.

- A page for DMARC aggregate reports, which were being received and parsed and
  then shown nowhere. It answers the question the reports exist to answer — who
  is sending mail as one of your domains, and did the receiver believe them —
  with the sender's address and reverse DNS, whether anything aligned, and what
  the receiver did about it. Listing them no longer requires naming a domain
  first, because "is anyone forging me" is not a question about one domain.
- `server.mailServers` names the hosts mail arrives at, so a domain can be
  asked for a pair of MX records rather than one. A deployment reached at
  `mx1` and `mx2` no longer has every domain reported as having the wrong MX,
  and a server can be moved later without twenty five zones changing. Leave it
  unset and nothing changes: the MX record names the server, as before.
- Authentication is rate limited, per address, on the submission port and on
  the dashboard. Verifying an SMTP credential is an HMAC and a comparison, so
  without a limit an address could guess as fast as the network allowed; the
  dashboard's bcrypt hash made each guess expensive for the server too. Tune
  with `smtp.authRateLimit` and `smtp.authRateBurst`, or set either to zero to
  turn it off.
- The deployment test speaks GraphQL, and runs to the end. It had been calling
  three REST endpoints that no longer exist, so it failed at onboarding and
  never reached the checks that matter: receiving mail on port 25, the ARC
  seal on a forwarded message, DKIM signing, submission with a credential, and
  that all of it survives a restart.
- `docs/getting-started.md` and `docs/configuration.md`, which the README has
  been promising. The first walks from nothing to mail arriving, including the
  outbound port 25 blocking that decides whether a host is usable at all; the
  second documents every configuration field, with a check that fails when a
  new one arrives undocumented.
- The secret guard checks every hostname in the tree against a list of what is
  allowed, rather than a list of what is forbidden. A deny list only catches
  the names somebody thought to add; this catches an operator's own domain
  wherever it lands, including in a comment or a test fixture.
- One configuration file, `teanode.yaml`, holding everything an operator sets,
  including each domain's DKIM signing key. It is re-read on `SIGHUP` and
  rewritten by the dashboard.
- A signing key is generated for every domain the moment it is created, and
  the dashboard shows the exact DNS records to publish, key value included.
  Nobody has to know DKIM exists before their mail is trusted.
- The server works out its own external IPv4 and IPv6 addresses and puts them
  in the DNS guidance, so the record for the mail host says the address to use
  rather than leaving the operator to find it.
- Certificates without a cloud account: ACME `http-01` by default, with
  `tls-alpn-01` for hosts where port 80 is blocked, and `dns-01` via Route53
  kept for wildcards.
- A dashboard compiled into the binary, which renders a message as a message —
  authentication verdicts, delivery attempts, and the body with scripts
  stripped and remote images blocked until asked for.
- Dashboard authentication: users in the configuration file with bcrypt
  hashes, and a signed session cookie.
- Local storage for received messages, so a delivery can be retried after a
  restart and the dashboard has something to show. S3 becomes an optional
  mirror rather than the only copy.
- `teanode config init`, `config validate`, `config show`, `config import`,
  `dkim`, `tls self-signed`, `password`, `credential`, `user` and `token`
  commands.
- `teanode api`, which reaches every operation the server offers by reading
  the schema from it: `api list`, `api describe`, `api call` with `name=value`
  arguments, and `api graphql` for a query written by hand. Output is JSON, and
  the typed commands take `--json` too.
- API tokens, so the command line tool can administer a server over the
  network with `--url`. A token belongs to an account and acts as it; removing
  the account revokes it. On the server itself no token is needed.
- AWS credentials can live in `teanode.yaml` for Route53 and S3, instead of a
  shared credentials file or the ambient chain. They are never returned by the
  API, and `config show` redacts them along with every other secret.
- `make dev`, which brings up a development server that cannot send mail.
- `make test-deployment`, which brings the whole stack up in Docker — the
  production image, PostgreSQL, a DNS server answering for .test, and a mail
  sink — and proves it end to end: migrations, onboarding, tokens, the command
  line client over the network, a message received on port 25 and forwarded
  with a verifiable ARC seal, submission over STARTTLS delivered by MX lookup,
  survival of a restart, and that no secret is printed. It found three of the
  bugs listed below.
- The dashboard speaks English, Simplified Chinese and Japanese, picked from
  the browser's language and changeable from a control beside the appearance
  one. No i18n library: lookup and substitution is forty lines, and the
  catalogs are typed against the English one so a missing key does not
  compile. `make check-catalogs` catches what types cannot — a translation that
  dropped a placeholder, or one that was never translated.
- An appearance setting in the dashboard: auto, light or dark. Auto follows
  the operating system and is the default; the other two are for when it is
  wrong. The choice is applied before the page paints, so it does not flash
  the other theme first.
- A domain overview at `/domains/<id>`: whether its DNS is published and when
  it was last checked, how much mail it has received and when the last arrived,
  and what is configured — with a link to the mail list already narrowed to
  that domain.
- An aggregation pipeline on the list queries, in the shape used elsewhere in
  the fleet: a list of stages, each exactly one of a match, a sort, or a
  distinct, applied in order. Filtering and ordering happen in the database
  rather than over whatever the browser fetched, and `CountMailsBy` answers
  what a filter menu needs — the values a column takes and how many rows
  carry each.
- Column sorting in the dashboard's tables: ascending, descending, and back
  to the table's own order.
- A domain publishes its mail records under its own names. The MX names
  `mx1.<that domain>` rather than a name in whichever domain the server is
  called after, the bounce and report subdomain gets an MX of its own instead
  of an alias to the server, and the address records for those names are on
  that domain's page because they are that domain's to create. Pointing every
  domain at one name worked, and published in each of them the name of a
  different one: look up the MX of any and you learn the set. Nothing has to
  change on an existing installation — an MX naming the server's own hosts, or
  a bounce name still aliased to it, is still correct, and is still checked as
  correct.
- Every domain has a signing key of its own, and publishes it at its own name.
  A domain that arrives without one — from a configuration file written by
  hand, from an import, from an older database — is given one on the next
  start, and told in the log where to publish it. A key already there is never
  replaced: it matches a record already published.
- One table component behind the mail list and the queue: per-column filters,
  pagination, and timestamps as "12 hours ago" with the exact time, zone
  named, on hover.
- `smtp.requireReverseDns`, on by default. Turn it off where this server does
  not see the real client address — behind a load balancer, or on a private
  network — because there the check refuses everything.

### Changed

- **A row that goes somewhere is the target, not the one link inside it.** The
  mail list, the queue, the reports and the domains are lists of things you
  open; the link in each row stays for the keyboard and the middle button.

- **A message says what happened to it.** The page opened with the subject and
  the sender, which are the two things the reader already knew — they clicked
  the row. It opens with the verdict and a sentence saying what became of it,
  and carries what was fetched and thrown away: the TLS version, the HELO, the
  Message-ID, where the sender is, the DSN the far end returned.

  Authentication was a row of tags reading "SPF pass DKIM pass", enough to
  know nothing went wrong and never enough to work out why something did. Each
  check is a row now: the mechanism, the verdict, and what was examined — the
  key that signed, the address SPF authorized, the policy DMARC found, the
  rules the spam filter matched. The markup behind the rendered view has a tab
  of its own, highlighted.

- **Accounts are keyed by an identifier rather than by their username**, and
  the table is `user` rather than `operator`. A key that changes is not a key:
  sessions and API tokens named an account by a string a rename would have
  invalidated. They point at the identifier now and cascade from it. `setting`
  became `configuration`, which is what it holds.

- **The rail carries the server's settings beside Domains**, and the account's
  own behind your name at its foot — where opening Settings swaps the rail for
  them. There is no bar across the top any more: what was on it was a control
  belonging to the rail and two menus belonging to the reader, and what was
  left was a 56-pixel band with a rule under it.

- **The filter fields open from a control** rather than sitting under every
  header permanently, where they were the widest thing on the page and used on
  a fraction of visits.

- **Nothing draws "loading…" for the first quarter of a second.** A query on a
  local network answers in less than that, and the word was a flicker on every
  navigation.

- **"Replace the key" is gone from a domain that signs with the primary's
  key.** Such a domain publishes a CNAME rather than a key of its own, and
  replacing it would have given it a key nobody had published. It offers to
  split off instead, and says what that changes.

- **The dashboard follows a quieter design language**: color only where it
  means something. The rail is a warm gray against a
  white page, the row you are on is a raised pill rather than a highlight, and
  what you press is near-black — inverting to near-white on dark rather than
  dimming. Group labels are sentence case rather than small caps, table heads
  are a quiet label rather than a shout, the zebra stripe is gone in favour of
  the rules that were already there, and prose stops at eighty characters.

  The rail carries its groups under their own labels, keeps the way into
  settings and who you are at its foot, and swaps itself for the settings
  navigation once you are in there — so the six settings surfaces are
  reachable from each other rather than only from a hub. Each page now names
  itself in a heading at the top of its own content, and the breadcrumb above
  shows only how you got there.

  The accent is no longer the mark's green. It was on every button, link and
  active row, which meant none of them meant anything; the green now belongs to
  the logo alone, and green, red and amber are kept for state — a record
  published, a delivery failed, a certificate nobody can see.

- Configuration lives in PostgreSQL rather than in `teanode.yaml`, so that
  more than one instance can run against it. The file could not be shared: a
  server held the whole of it in memory and rewrote it from memory on every
  change, so a second instance would not see a domain added on the first and
  would overwrite its changes at the next save. Two things stay outside, in
  the environment, because they cannot be kept in the database they describe
  or must differ per process: `TEANODE_DATABASE_URL`, and `TEANODE_INSTANCE_ID`
  — which the usage counters are keyed by, and which two instances sharing
  would lose each other's counts. `teanode config env` writes a starting
  point, `teanode config init` sets an empty database up from it, and
  `teanode config import` loads an existing `teanode.yaml` in, carrying
  identifiers, signing keys, the server secret and the session key across
  unchanged. `teanode config export` writes one back out.
- Session cookies and API tokens are identifier, secret and a signature over
  both, signed with different keys so a cookie cannot be presented as a token.
  Only a hash of the secret is stored, so a copy of the database is not a set
  of working logins. Every existing token stops working and has to be reissued;
  the format changed and there is no way to write a row that would accept an
  old one.
- Last-used is recorded at most once a minute per credential, guarded in the
  `WHERE` clause rather than in memory, so a dashboard left open on its refresh
  timer costs one row update a minute rather than one per poll — and two
  instances cannot move the column backwards.
- Stored settings are YAML, in a text column, not JSON in a `jsonb` one. A
  server secret is 32 bytes from `crypto/rand`, so most are not valid UTF-8
  and roughly one in eight contains a zero byte. `jsonb` refuses a NUL
  outright, and `encoding/json` quietly replaces an invalid byte with the
  replacement character — which would have invalidated every SMTP password on
  the server without saying anything. YAML writes such a string as `!!binary`,
  which is also what an exported file holds, so the two forms cannot drift.
- Concurrent configuration changes are resolved rather than lost. A write
  carries the version it was based on, one row is taken `FOR UPDATE` for the
  length of it, and a change that lost the race is re-applied to the newer
  configuration rather than merged into it. Instances notice a change made
  elsewhere within five seconds.
- Raw messages go to an S3-compatible object store as well as to local disk,
  which is what lets any instance show or retry a message another one
  received. MinIO is what the compose files use; the endpoint and path-style
  settings that a self-hosted store needs are now configurable.
- The server says when a setting that is only read at startup changes
  underneath it — the listeners, TLS, storage, the data directory, the
  optional integrations. That configuration is shared now, so it can change
  from the dashboard or from another instance while this process is running,
  and a setting that appears to save and does nothing is worth an hour of
  somebody's afternoon.
- Retention sweeps the object store as well as the local spool. Sweeping only
  local files would never expire a message another instance handled, so the
  bucket grew without bound — and the bucket is the copy that matters once
  there is more than one instance.
- Generated secrets are decided inside the mutation that stores them. A server
  secret generated beforehand would be written over the one another instance
  had just stored, leaving the two deriving SMTP passwords from different
  keys.
- `server.dataDirectory` has to be an absolute path. It used to resolve
  against the directory holding the configuration file; with no file it would
  land wherever each process was started from.
- The repository root is the Go module; the binary carries the dashboard.
- The HTTP API is versioned and mounted at `/api/v1`, implemented under
  `internal/api/v1api` as `apigraph` (GraphQL) and `apisend` (the template
  send endpoint).
- Logging in, logging out, claiming a new server and changing a password are
  GraphQL operations, not REST endpoints beside it. A browser's credential is
  a cookie, so those resolvers get the response writer; that is a reason to
  pass one argument, not to run a second protocol. Authorization was always in
  the resolvers rather than the routing, and a test now reads the source and
  fails if a new one forgets to check.
- The command line tool changes configuration through the running server
  rather than writing to the database directly, so that a change made from the
  shell is validated the same way and has the same side effects as the same
  change made in the dashboard. Run on the server it reads the same
  environment the server does; `teanode user --offline` writes straight to the
  database, which is safe now, and remains for recovering from a lockout.
- API tokens and the accounts that administer the server moved out of the
  `dashboard` block: `users` is top level, each with its own `tokens`, and
  what is left is `session`.
- The database keeps only what grows without bound: mail, deliveries, DMARC
  reports, usage counters and templates. Migrations restart at `0000`.
- Domain DNS checks advise rather than gate. Mail for a configured domain is
  accepted, and the dashboard says which records are still missing.

### Removed

- The Redis-backed relay between server instances, and with it the Redis
  dependency.
- Multi-tenant user accounts, per-user domain ownership and magic-link login.
- Test fixtures made of real captured mail, replaced by messages generated at
  test time.

### Fixed

- The port shown beside a new credential is the one a mail client can reach,
  not the one the process binds. Those are the same thing until something
  forwards one to the other — a container publishing 10587, a firewall taking
  587 — and then the dashboard was handing somebody a number nothing answers
  on. `smtp.submission` sets what to advertise, host and port, and both are
  editable on the Setup page; leaving them empty keeps the old behavior of
  following the server.
- The Setup page no longer describes settings as living in `teanode.yaml` and
  being reloaded with a HUP signal. Neither has been true since configuration
  moved into the database.

- Links in a message can be clicked. The sanitizer had been putting
  `target="_blank"` on every link it kept since it was written, but the frame
  showing the message was sandboxed with `allow-same-origin` alone — and a
  browser silently drops a `_blank` click without `allow-popups`. Nothing
  reported an error; the link simply did nothing. The frame now also allows
  the opened tab to escape the sandbox, because a link that "works" and lands
  on a page with no scripts and no origin is worse than one that does not
  open. Scripts, top navigation and forms are still refused, so a message
  cannot run code, cannot navigate the dashboard away from under the reader,
  and cannot submit anything.

- `Pagination.Options` assigned the offset to the limit, so asking for an
  offset silently changed how many rows came back and never skipped any.
- `graphapi` panicked on any named type over a builtin — `type Operation
  string`, which is what every enum in an input is. It coerced by kind and
  returned a plain `string`, which `reflect.Set` then refused. It also
  ignored `graphapi:"nullable"` on method arguments, so an optional argument
  was published as required.

- Mail from anything that composes HTML for a living rendered as a column of
  fragments. The sanitizer dropped every `<style>` block and `style`
  attribute while keeping the class names that referred to them, so a message
  arrived with its whole skeleton of nested tables and not one rule that made
  it a layout. CSS is kept and sanitized now; what stops it fetching or
  scripting is the frame's own policy, not the sanitizer deleting it.

- API replies carried no `Cache-Control`, and a 200 without one is
  heuristically cacheable. After the accounts on a development server were
  cleared, a phone went on showing a login form for a server that had none,
  because it never asked again. The same staleness would let a browser be told
  it is still logged in after logging out.
- A subscription that emitted a message with the identifier `test` every
  second, to anybody who asked, is gone. It was a placeholder nothing called,
  and it was the one operation in the schema that checked no permissions.
- The dashboard followed the operating system's light or dark setting with no
  way to override it, and the viewport did not say whether zooming was
  allowed. On a phone it now respects the notch, keeps pinch-to-zoom, and
  sizes form fields at 16px so that iOS stops zooming in when a field takes
  focus and never zooming back out.

- `arc.Validate` started one verification goroutine more than it collected
  results from, and the discarded one was usually the check covering the
  message body — so a message altered after being sealed validated as `pass`.
  It also leaked a goroutine per call.
- `teanode config show` printed the server secret, the session key and every
  domain's signing key, despite hiding passwords. Redaction is now driven by a
  struct tag with a test that fails when a new secret is not tagged.
- The ARC seal on a forwarded message named the mail host, for example
  `d=mail.example.com`, while the signing key is published at the domain. Every
  receiver that checked a forwarded message looked up a name with no key under
  it, so no seal could be verified — the entire purpose of sealing. Nothing
  here could notice, because only the receiver ever verifies one.
- `deploy/docker-compose.yml` mounted `teanode.yaml` as a file. The server
  saves by writing a temporary file and renaming it over the old one, and a
  rename over a bind-mounted file fails with `EBUSY`, so no change made in the
  dashboard could be saved. The configuration directory is mounted instead.
- An alias relaying to a mail server refused any host name without a dot, so an
  internal smarthost named `smtp` or reached by address could not be
  configured.
- `teanode dkim show --json` ignored `--json` when the server was not running,
  and failed outright before the first start, when the server secret a local
  token is signed with does not exist yet.
- A DKIM record with an empty `p=` was reported as published. An empty value
  means the key is revoked, so an operator was told their signing was fine
  while every signature failed.
- With no log directory configured, every received message was written into
  the process working directory.
- `GetCertificate` served an empty placeholder certificate before the first
  issuance, failing the TLS handshake obscurely.

### Removed

- The `X-Forwarding-Service` header. It announced this software and its version
  to every recipient of every message, which told them nothing they could use
  and told anybody else what to look up. It was also on submitted mail, which
  nobody forwarded. The `Received` header still names the host that handled the
  message, which is what tracing a delivery actually needs.

### Fixed

- The `Received` header on a message submitted over the API says where it came
  from. There is no greeting in an HTTP request, so the from clause was empty —
  `from  (unknown [address])` — which is not the form RFC 5321 describes. It
  uses the address literal, which is.
- A submitted message names one host, not two. The `Received` header said the
  name the sender reached while the `Authentication-Results` beside it still
  said the server's own, so one message carried two different answers to where
  it had been.
- `Feedback-ID` is set on mail this server was asked to send, and on nothing
  else. It used to go on every delivery carrying the delivery's identifier,
  which was wrong twice: the header exists so a receiver can group a sender's
  complaints, and its last field has to mean the same sender every time, so a
  fresh value per message grouped nothing. On forwarded mail — most of what
  this sends — a message that already carried the original sender's went out
  with two, ours first, so the receiver attributed complaints to a meaningless
  value instead of to the one the sender set deliberately. Submitted mail now
  carries the sending domain, which is the identity a receiver's tooling is
  registered against; a forwarded message carries nothing, because it belongs
  to whoever wrote it.
- DMARC is evaluated against the organizational domain when the sender's own
  subdomain publishes no record, as RFC 7489 section 6.6.3 requires. Only the
  exact name was asked, so a message from a sender like
  `rs.email.example.com` — which publishes nothing there and is covered by
  `example.com`'s `p=reject` — was recorded as having no DMARC policy at all. Bulk senders almost all send from
  a subdomain, so this was most of the mail that arrives: a large share of it
  was being judged as unprotected, and the authentication panel said so on
  messages every other receiver reports as `dmarc=pass`. Where the policy comes
  from the domain above, the subdomain policy is the one applied, and the
  domain it was found at is recorded beside it.
- SPF is one of the records a domain is asked for, and is checked. It never
  appeared at all, so a domain sending mail no receiver would accept as
  authorized looked exactly like one that was set up correctly. It is asked for
  at the bounce subdomain rather than at the domain itself, because that is the
  envelope sender on everything this server sends and it is the envelope a
  receiver evaluates — a perfect record at the domain does nothing for it. What
  is checked is that a record exists and permits something: whether it permits
  this server is a question for a resolver, and with a proxy or a relay the
  address mail leaves from is not one this server can see.
- The DMARC record the dashboard asks for names a report address the server
  will actually accept. It asked for `rua@mail.<domain>`, and a report sent
  there is refused: the server takes a report only at a signed address, which
  is what it checks the recipient against. Anybody who published exactly what
  the panel showed them received no aggregate reports and was told nothing.

### Security

- A domain's signing key is encrypted in the database rather than stored as
  PEM in a column. The key is derived from the server secret, so a copy of the
  `domain` table on its own — a partial dump, a support query, a replica of
  some tables and not others — no longer carries usable private keys. It is
  not protection from a full compromise: the server secret is in the same
  database. Keys written by an earlier release are encrypted on the first
  start after the upgrade, so there is nothing to migrate and no window where
  the column is documented as encrypted while sitting in plaintext.
  `teanode config export` still writes a plaintext file.
