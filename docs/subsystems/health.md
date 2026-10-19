# Health

The TeaNode app on a person's iPhone copies their Apple Health data to their
own server: steps, heart rate, sleep, workouts and their routes, weight and
everything else HealthKit lets an app read, except clinical records. The server
keeps it, the dashboard and the app chart it, `teanode health` prints it, and
the person may share some of it with people in their circles. Their agent reads
it only once they let it. This says what is kept, how it arrives, who sees what,
and what leaves the server.

The plan is `docs/planning/health-execplan.md`; why it is shared by category,
as day summaries, is in
`docs/decisions/20261018-health-is-shared-by-category-as-day-summaries.md`.
The words are fixed: a **health type** is one kind of data, named by
HealthKit's identifier without its prefix (`stepCount`, `heartRate`,
`sleepAnalysis`, `workout`); a **reading** is what a person sees of one sample
(the code and the API say **sample**, `HealthSample`); a **day summary** is one
type on one day; a **Health category** is one of the Health app's groups
(Activity, Heart, Sleep and so on); a **route** is a workout's path. Deleting is
**erase**, as for location history.

## What is kept

All in the database (migration `0245_health.sql`), all the person's own, and
all gone with the account:

- **Samples** (`health_sample`): the type, when it began and ended, a quantity
  with its unit or a category value (a sleep stage) or a workout's activity,
  the app and device that wrote it, and HealthKit's other details as JSON. A
  sample is unique by person and HealthKit's identifier, so sending one again
  adds nothing.
- **Day summaries** (`health_day_summary`): per type and the phone's calendar
  day, the sum, average, lowest and highest, the readings counted, and for
  sleep and workouts the time covered and the time in each stage.
- **Routes** (`health_workout_route`): a workout's path, at most 20,000 points.
- **Characteristics** (`health_characteristic`): date of birth, sex, blood
  type, skin type, wheelchair use, as the person set them in the Health app.
- **Counts** (`health_type_total`): readings kept and last received per type,
  kept by a trigger so no screen counts every sample.
- **Recently deleted** (`health_deleted_sample`): the type and times of samples
  the phone deleted in the last 30 days, so a delete sent again is answered the
  same way and the phone can work those days out again. Older rows are removed
  as reports and erases come in.
- **Shares** (`health_share`) and two columns on the user:
  `can_agent_read_health` (false until the person turns it on) and
  `health_erased_at`.

Percentages are kept as HealthKit keeps them, a fraction (0.97), and every
surface shows them as a percentage. The types and how each adds up are one
table, `internal/health/types.go`, mirrored in TeaNodeKit's `HealthTypes.swift`.
A type the table does not know is kept under the category `other`, so a new
iOS type needs no server change.

## How it arrives

Only the iPhone app sends Health data; nothing else can read it, because Apple
keeps it on the phone. The person turns on Health in the app's Settings, reads
one screen saying what is copied and where, and allows what to copy on Apple's
Health permissions screen. The app is never told which kinds were refused: a refused kind looks like
a kind with no data.

For each type the app asks HealthKit what is new or deleted since its saved
anchor, a page of up to 1000 at a time, and sends it with `ReportHealth`; the
anchor moves only after the server answers, so a failed send is read again
rather than kept. The phone keeps no copy of health data, which also keeps it
out of iCloud backups (Apple forbids health data in iCloud). The first copy
reads the whole history, while the app is open and as a background task while
the phone charges. After that, HealthKit wakes the app when new data is saved,
at most hourly for most types. Health data cannot be read while the phone is
locked, so a wake while locked waits for the next unlock.

The phone works out each day's summary, because only Apple's statistics merge
sources the way the Health app does: an iPhone and a Watch counting the same
walk are counted once. Sleep and other times are the union of their readings,
so two apps recording one night count it once. The day is the phone's calendar
day. Each summary carries when the phone worked it out (`workedOutAt`), and the
server keeps the latest, so a late resend cannot replace a newer day. A time
later than the server's own clock is taken as the server's time. When the
server deletes samples it answers with their types and times, and the phone
works those days out again.

A batch is all or nothing: an entry the server refuses is named in the error
(`samples[2]`), and the phone leaves that entry out and sends the rest.

Each report says the last erase time the phone knows of (`healthErasedAt`,
read when Health is turned on and carried back by every answer). After the
person erases everything, the server stores nothing from a phone that does not
know of that erase, and answers with its time; the phone then turns Health off
and forgets its anchors. The phone's clock plays no part, so a phone whose
clock is off cannot slip past it (the server cuts its own erase time to the
second before comparing, so a phone that kept it without the fraction of a
second still counts as knowing it). Erasing one type erases what is kept
of it; new readings of that type still arrive while Health is on.

## Who sees what

- **The person** sees everything they have, on the dashboard's Health page, in
  the app's Health screen, and with `teanode health`. Two things take the
  person's own tap, on the dashboard or in the iPhone app: turning on the
  agent's switch, and a share that adds categories the grantee does not
  already have (`UpdateHealthSettings` with the switch on, `SetHealthShare`
  widening). The server refuses them to a command line token. It cannot tell
  the person's command line from their agent's: with a computer attached, the
  agent can run commands that act with the person's own login, and so can read
  what that login can read on that computer, Health included. That is why the
  two consents are not given to a login. Turning the switch off, narrowing or
  ending a share, and every read and erase, work from anywhere.
- **People they share with.** A person shares chosen Health categories with a
  circle, or with one person in a circle they share. Those people see the day
  summaries of the types in those categories, as charts, and nothing else: no
  readings, routes or characteristics. Anybody else, or a type outside the
  categories, is "not found". Leaving the circle ends the share.
- **The agent** reads the person's own Health data through the read-only
  `health` tool (types, summary, samples, set_up) only after the person turns on
  **Let the agent read Health data**, on the Settings tab of the Health page on
  the dashboard, or in Health in the iPhone app's Settings. Until then every action but set_up says so.
  It runs only in a conversation the person, or somebody acting as them, is in: a dream, a subagent, a
  turn woken by a background command or survey, a speak-first run or any other
  run nobody is watching is refused. So is a turn whose answer others could
  read: over Telegram or Discord, or in a chat room with other members. There
  every action but set_up answers in one sentence, that Health is read only in
  the TeaNode app and on the dashboard. An operator with `agent:act` speaking as
  the person, and a client of the MCP surfaces, reach the tool only through the
  person's own switch: they act as the person, so with the switch on they read
  what the person's agent reads. It never reads what somebody shares with the
  person (the person sees that on the Health page, under Showing): a person
  shares Health with people, not with their agents' model providers, and only
  the sharer can agree to their data going to one. The tool only reads, and
  reads no routes: erasing cannot be undone, sharing and the switch are the
  person's own consent to give, and a route says where somebody runs, which
  the person shows on the map themselves.
- **Memory.** Dreams never read Health data. When a conversation is filed into
  memory, what the agent said and what its tools answered in a turn that read
  Health is left out, and what the person wrote is kept. A message the person
  steers in while that turn runs belongs to the turn, which lasts until the
  agent's final answer. The filter does not reach further: Health figures that
  the person or the agent repeats later in a conversation (a follow-up question
  about numbers already shown, say), and what the agent writes to memory
  because the person asked it to, are kept like anything else said there. The
  tool's guidance tells the agent not to copy Health into notes or memory
  unless the person asks. A conversation's title and summary are not
  refreshed from a turn that read Health: the conversation keeps the ones it
  had. What the agent said about Health stays in its
  conversation, and erasing does not remove it.

## What leaves the server

What the agent reads goes to the model's provider, like every tool result, and
the switch names that provider. The agent reads Health only in the person's own
conversations in the app and on the dashboard, never over Telegram or Discord
and never in a chat with other people. Conversation titles and summaries are
not refreshed from a turn that read Health. Nothing else leaves: Health data
never goes to the push relay or into a notification's text, the server log
does not print the values of a statement, and it is never used for anything but showing it to the
person and the people they share with.

## Erasing

`EraseHealthData` with a type erases that type's samples, day summaries and
routes; without one it erases everything, characteristics included, and sets
`health_erased_at`. The dashboard, the app (which also turns Health off on the
phone) and `teanode health erase` all do it, each after asking.

## Operating it

Health is on from the first start: `health:use` is granted to the
Administrator, Operator and Member roles. Taking it from a role hides Health
for its people. Nothing needs configuring on the server.
