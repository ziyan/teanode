# Location

The TeaNode app on a person's iPhone reports where they go. The server keeps
the positions, turns them into visits to places, names the places from
OpenStreetMap, and shows the person where they have been. They can share where
they are with the people in their circles, and their agent reads it all. This
says what is recorded, how, who sees what, and what leaves the server.

The reasons for the two choices that shape it are in
`docs/decisions/20261017-a-location-is-shared-at-a-level.md` and
`docs/decisions/20261017-places-are-named-from-openstreetmap.md`. The words are
fixed: a **position** is what a person reads (in the code and the API a
**point**, `LocationPoint`, one reported position); a **visit** is one time at a
place, from an arrival to a departure; a **place** is somewhere with a name and a
radius; a **label** is home, work, school, family or gym; to **check in** is to
say by hand that you are at a place, and a **check-in** is that visit.

## What is recorded

Three things, all kept in the database (migration `0240_location.sql`), all the
person's own, and none of it on the person's agent's say:

- **Positions** (`location_point`): latitude, longitude, accuracy, and when
  present altitude, speed and course, with when it was measured and by what
  (`phone`, or `browser` when the agent asks the browser a turn is in). Only a
  phone reports them to be kept; the command line has nothing that does. A
  batch is at most 500, and sending the same batch again adds nothing (a
  position is unique by person, instant and source), which is how a phone can
  keep sending until it knows the server has them.
- **Places** (`place`): a name, a radius (75 meters for a new one, 25 to 2000),
  an optional label, the coordinates, and what OpenStreetMap said about the
  place: its object, name, kind (`osm_category`) and address. A person can
  rename, label, move and resize a place; a name they give is theirs
  (`is_named_by_person`) and the provider's never replaces it.
- **Visits** (`place_visit`): a place, an arrival, a departure (none while the
  person is still there), the accuracy, and where it came from (`visit` for
  iOS visit monitoring, `region` for entering or leaving a place the person
  named, `check_in`, `browser`).

## How a visit is made and joined

A phone sends an arrival and, later, its departure as two reports under one
`clientVisitKey`; the departure carries no arrival time. The server finds the
visit by the key and completes it, so the two halves are one visit however late
the second comes. A key it does not know is a new visit, at the place that holds
the position (a place the person has whose radius, widened by the accuracy,
contains it), or at the place a region event watched, or at a new place made for
it. A new place is named after its nearest city from a table built into the
server and is named properly right after the report is stored (see below).

iOS reports a visit it noticed again with its departure, and with a position it
has made more exact; the phone's key is made from the position, so the departure
can come under a key the server has not seen. A visit from iOS visit monitoring
under an unknown key whose arrival is in the same second as a stored one is that
visit, and completes it, instead of becoming a second visit whose arrival ends
the first the moment it began.

### Pass-bys

iOS tells of crossing a watched place's circle the moment it happens, with no
least stay, so driving past a place the person labeled would otherwise be a
visit to it. A **pass-by** is a visit from a circle (`region`) shorter than
three minutes, with a position within two minutes of it at 4 m/s or faster and
none during it at 0.5 m/s or slower. Once the positions and visits of a report
are stored, the pass-bys that ended around its times are deleted, and the
visits around them put in order again. A short stop, such as letting somebody
out at a school, has a slow position in it and stays a visit; so does a short
visit with no fast position near it. The positions that tell are usually sent
with the visit; a slow one that comes in a later report, after the visit was
deleted, does not bring it back. Visits iOS noticed are not judged this way:
iOS reports one only after a stay.

Two visits to one place less than ten minutes apart are one visit: a phone that
loses the signal at the front door and finds it again does not make two. Which
visit the surviving row carries does not matter to the phone, because every key
that was sent for either still finds it.

Visits can arrive in any order (a phone that was offline sends a day at once, a
batch is sent again after the next one). After the visits of a report are
stored, the server puts the person's visits around those times in order (storing
a visit does not end it at a later arrival; this does): a visit
with no departure ends at the arrival of the next visit at any place, a visit whose departure
runs past the arrival of the next visit at another place ends at that arrival,
visits to one place with a visit at another place between them are not joined,
and a visit that was joined before the visit between its parts was heard of is
split again where that visit began. A visit known only by its departure is not
joined to anything until its arrival is reported. The result is the same as if
the events had come in order. The times put in order reach back to the arrival
of every visit the report changed: iOS can report leaving a place's circle
hours late, and that departure must not carry the visit over the stops made
since. Its notice is told by when the visit ended
as stored, which is too long ago to tell.

At the most places a person may have (5000), a visit that would need a new place
is left out, the rest of the batch is stored, and the answer's
`refusedVisitCount` says how many were left out. The allowance of 60 new places
a minute is taken once the report is committed, for the places it made, so a
report that is rolled back takes nothing and a full report of new places passes.

Storing a visit takes the person's lock, so two reports at the same moment (a
phone that resends while the first send is still being stored) take turns and
make one visit at one place. An arrival, or a check-in, ends every other visit
of the person that has no departure, at that arrival: a departure the phone never
delivered does not leave a visit open for ever. A check-in may carry the `accuracyMeters` the device reported (a browser on a
computer can be hundreds of meters off, a phone a few), which is stored on the
visit; without it the visit stores 0.
Checking in at the place the
person is already at changes nothing and tells nobody they left and arrived.
"Where the person is now" is the newest visit with no departure, unless the
latest position since is outside the place.

## Where names come from, and what leaves the server

Two things leave the server because of Location, both to a host the operator
chose in the server settings (`places`, see `docs/configuration.md`).

**To the place provider**, when a place is made: its position, to six decimals,
and the server's User-Agent, which names the server. Not the person's name, not
the time, not the other places. The provider is the OpenStreetMap Foundation's
public Nominatim server unless the operator sets another (Photon, run by
themselves); an operator who sets `places.providerKind` empty sends nothing
anywhere and places keep the name of their city. A new place is named in the
background right after the report that made it is stored, through one limiter
shared by every caller of the provider: one request a second for each instance,
and a caller that would wait more than a few seconds is turned away, so a person
looping on search cannot starve the naming or break the provider's policy. Each
account is also limited to 12 searches or check-in lists a minute and 60 new
places a minute, may have at most 5000 places, and the naming job takes at most
three waiting places of each person in a run and none of a person who may not
use Location. A road, a rail line, a stop, a barrier, a pole or a man-made
structure such as an elevator is not a point of interest, and neither is street
furniture such as a car park or parking space, a bench, a bin, a post box, a
public telephone, drinking water, toilets, a shelter, a vending machine or a
charging station (`amenity=*`), in a reverse answer and in the check-in list
alike, so the address names the place instead (house number and road, then road, then neighborhood, then
city). A
provider that is too busy to be asked gives the place's claim back, so the next
run takes it at once, and only the first three places of a report are named right
away. The
agent's periodic job is the sweeper for places whose first try failed; it takes a
place again once its claim is old. A failed try is stored as a fixed sentence
("could not reach the place provider"), never the provider's address or the
request, and only the owner reads it. An answer farther from the position than
the place's radius plus a small margin is taken as "nothing known" rather than
as the name, except for an area the provider itself chose for the position (a
park or a campus is positioned at its middle). An answer for a place that was
moved while the provider was being asked is dropped.

The check-in list and the agent's search use the same provider, with the
person's words (search) or position (check-in list) as the request. With the
provider off, the check-in list is the person's own places near the position and
searching is refused with the operator's switch named.

**Typing an address.** A person can say where a place is instead of where they
are, for home above all, or correct where one of their places is.
`SearchAddresses` sends the words they typed to the same provider, through the
same shared limiter and the same 12 lookups a minute for each account, and
answers up to 20 matches (8 by default), called by the same rules as a reverse
answer: a match that is a road or a stop is called by its address, and a house
by its number and road. The matches come nearest first to a center: the
position given, else the place given by `placeId` (the dashboard and the iPhone
pass the place whose address is being changed), else the person's latest
position. The provider is asked to lean toward the center without keeping to it
(Nominatim `/search` with `addressdetails=1` and a 50 km `viewbox` with
`bounded=0`, Photon `/api` with `lat` and `lon`), for up to three times as many
matches, which are then sorted by distance. The provider's best match stays in
the list even when nearer ones would fill it, as its last row, so an address
typed exactly is found in another town too. With no center at all the words
alone decide, best match first, and `distanceMeters` is 0; otherwise it is from
the center. A match's `placeId` is set where one of the person's own places
holds the position. The person picks a match, and `SavePlace` takes its
`addressText` with its `latitudeDegrees` and `longitudeDegrees`. The place moves
there, keeps its label and any name the person gave it, and keeps that address:
it is marked as named at once (`named_at`), so the naming job does not ask the
provider what is there and replace it, and the OpenStreetMap name, object and
kind of the old position are dropped, even for a move within the place's radius,
and also when the place does not move: the map's name for the spot is not the
place the person described.
A place the person did not name is called by the first part of the address.
An address without a position is refused only when there is no `placeId`: with
one, the place stays where it is, loses what OpenStreetMap said about the spot
(object, name, kind), and takes the address exactly as typed, for an address
the map does not know. The
address is then marked as typed (`is_address_typed`, migration
`0241_place_address_typed.sql`), so moving the place afterwards (to put its pin
on the house) keeps it and the place stays named, and the naming job never
replaces it. An empty `addressText` with a `placeId` takes the typed address
away (the CLI's and dashboard's "use the map's address"), and the place is named
from OpenStreetMap again from its position. Without a `placeId`, the same call
makes the place, or changes the person's own place whose radius holds the
position (as a check-in does), so typing an address for home never leaves two
places at one address. The words the person typed leave the server for the
provider, the same as a search does.

**To the map host**, when the dashboard shows a map: the dashboard's content
security policy allows nothing but this server, so the server fetches the style
and the tiles from the one host of the configured style and hands them on. The
host sees the server's address and which tiles are asked for, never the person.
Only accounts that may use Location are served, up to 600 files a minute each.

Names and addresses are written by strangers (OpenStreetMap) or by the person,
so everywhere they reach a model they are cleaned (one line, cut, no angle
brackets) and marked as data, never instructions.

## Who sees what

A person's location is theirs. They share it with one person in a circle they are
both in, or with a circle they are in, at one **share level**:

- `current`, in words **Where I am now**: the viewer sees where the person is now
  and the place they are at. That is the latest position and the visit in
  progress with its place, and nothing before it.
- `history`, in words **Where I am and where I have been**: the viewer also sees
  the person's visits and the places they were at, their places, and their paths,
  including those from before the share was made: the share reads what is stored,
  and does not begin when it is made. A share to a circle reaches everyone in the
  circle now and everyone who joins it later, and stops reaching a person when
  they leave.

Sharing again changes the level; unsharing takes it away. The person's own is
level `owner`.

Whether a viewer reaches a person, and at what level, is decided in one place
(`graph.reachLocation`, the database's `GetLocationShareLevel`) at the moment of
each call: a share to a person reaches them only while the two still share a
circle, a share to a circle only while both are in it, the viewer and the owner both need
`location:use`, and both accounts must be enabled. Leaving a circle ends the
reach at once. Anything short of the level asked for, and a person who does not
exist, are the same answer, "not found". The command line and the agent say
"<name> shares only where they are now" for the one case where the person is
known to the viewer and shares less than what was asked.

A viewer never gets the owner's records about a place, only what draws it: the
id, name, label, address, coordinates and kind. Not when the owner first went
there, how naming went, the radius or the OpenStreetMap object. At `current` the
only place a viewer sees is the open visit's.

Live: the dashboard and the app follow the people on the map through the
`LocationEvents` subscription. The server holds one LISTEN for all of them,
fans events out in memory, and decides who may see an event with one
transaction for all open maps; an account may hold 20 open at once. An event is
sent when a report changed something (not for a batch the server already had),
and when a share is made or ended and history is erased, so that a map that was
following somebody who stopped sharing reads again and drops them. Leaving a
circle, being taken out of one and deleting one end shares too, so they send the
same event, in both directions, for each location that stopped reaching
somebody (`watchLocationReach`).

## Notifications

The notification kind is `location`, described as "People who share their
location arriving at and leaving places". When a person arrives at or leaves a
place they named or labeled, the people their location is shared with are told
on their phones ("arrived home"). Not for a place the server only found a name
for. Only for an arrival or departure within the last 15 minutes and after the
share was made, so a phone that was offline for a day does not tell the family
about yesterday; the same arrival is told once to one receiver in ten minutes;
and only to receivers who may use Location and whose account is enabled. A
notification needs no agent: it goes through the same carrier chats use.

An arrival at a place's circle is told three minutes after it, once the visit
can no longer be a pass-by, and only if the visit is still there; a departure
before then is told a second after the arrival, so the two come in order. A
notice that waits lives in the server's memory, so a restart in those three
minutes loses it.

Nobody is told what they saw for themselves. A receiver who was at the place
too, within three minutes of the arrival or departure, is not told of it: a
visit of their own near the place (its radius and 50 meters) that overlaps
that time, or their latest position in that time inside it. People who arrive
or leave together, and somebody already there when another arrives, get no
notice. A receiver whose phone records nothing is told as before. The arrival
waits three minutes anyway, so the receiver's own arrival has usually reached
the server by then.

A person can mute the notices of one person who shares with them: for an hour,
until tomorrow morning, or until they unmute, the same choices as a pause
(table `location_notification_mute`, migration
`0259_location_notification_mute.sql`, one row for each pair of people).
`MuteLocationNotifications` and `UnmuteLocationNotifications` set and end it,
and `notificationMute` on the person (`ListSharedLocations`, `GetLocation`)
says it is in effect and until when. The dashboard has it on a chosen person's
page, the phone in the drawer's header with that person chosen, the command
line as `teanode location mute` and `unmute`, and the agent as the location
tool's `mute` and `unmute`. A mute silences only the arrived and left notices;
the map still shows where the person is.

## Pausing

A person can stop their location being recorded for a while: for an hour, until
tomorrow morning, or until they resume. A pause is theirs across the phone, the
dashboard, the command line and their agent, so it lives on the server (table
`location_pause`, migration `0257_location_pause.sql`, a row for each pause, so
that the timeline can show the gap afterwards) and every surface reads and sets
the same thing. The phone's "Record where I go" stays as well: it is the phone's
own switch, for battery and permissions.

`PauseLocation` pauses from now, until `endedAt` or, with none, until
`ResumeLocation`. The end must be after now and at most 30 days ahead. "Until
tomorrow morning" is 08:00 the next day in the person's zone, worked out by each
client: the server takes only a time. A pause is in effect at an instant when it
began at or before it and has not ended, and one that ends by itself needs no
event at its end: clients read again at that moment. There is at most one in
effect, and pausing while paused changes the end of that one. Resuming ends it
now, and a pause that lasted no time is deleted. Both need `location:use`, and
the agent may pause. In a conversation the agent's `resume` waits for the
person, as `forget_place` does, since it widens what is recorded and shown, and
`ResumeLocation` refuses a command the agent ran on the person's computer (one
carrying the agent's command mark), so the shell is not a way around the card.
That is a guard against mistakes and steered runs, not a boundary against a
program the person runs themselves.

**What is not stored.** While a pause is in effect, and for the time of a pause
that has ended, `ReportLocation` stores no position and no visit measured
inside it, whatever sends them: a phone that was offline sends its day when it
is back, and cannot fill the gap. The pauses that overlap a report are read
with one query. The answer counts what was left out (`pausedPointCount`,
`pausedVisitCount`) and carries `pause`, the one in effect after the report, so
that a phone learns of a pause set somewhere else. A position the browser gave
the agent is dropped the same way. Visits are cut at the edges of a pause:

- A visit that arrived inside a pause is not stored. If it left after the pause
  it is kept as a visit known only by its departure. One that arrived in the
  last five seconds of a pause that has an end counts from that end: at a
  resume the phone gives its places to iOS again, which answers at once with
  the place the person is inside, stamped to the second by the phone's clock.
  The phone does that only once the server has the resume, since the server
  ends the pause when the resume reaches it.
- A visit that arrived before a pause and left inside it, or was not over when
  it began, or left after it, keeps its arrival and ends at the start of the
  pause. A departure reported alone for a visit stored earlier is cut the same
  way. No visit runs across a time nobody knows where the person was, and the
  cut departure is not told to anybody. Visits to one place less than ten
  minutes apart are not joined when a pause began between them.
- A visit measured entirely inside a pause makes no place either.

A check-in is a visit measured now, so it is refused while paused, saying when
the pause ends. Pausing ends the visit in progress at the start of the pause (at
the latest position, when the positions say the person had left earlier), so a
viewer is never left seeing "at home" for the length of it, and tells nobody that
the person left.

**What others see.** `LocationView.pause` is the pause in effect. For anybody but
the person themselves, at `current` and at `history` alike, while a pause is in
effect `latestPoint` and `openVisit` are null in `GetLocation`,
`ListSharedLocations` (the pauses of everybody who shares are read with one
query) and the person card, and the clients read "Location paused", with "until"
and the time when it ends by itself. The person still sees their own position.
At `history` a viewer also reads the pauses among the visits:
`ListLocationPauses(userId, from, until)` answers the pauses that overlap a range,
newest first, by the reach rules of `ListLocationVisits`. Pausing, resuming and
a change of its end send the location event, so that open maps read again; a
report that stores nothing sends none. An arrival or departure is not notified
when it happened inside a pause, or while the person is paused. `EraseLocationHistory`
erases the pauses that began in its range and are over, and leaves the pause in
effect: erasing does not resume.

**The agent.** The journal writes a pause in a day as "paused from 14:00 to
15:00; where they were is not known", in the person's clock and among the visits,
never as a stay: it is no place and no time away from home. Once the person has a
visit at all, a day with only pauses still has a document, with those lines and
no places, since where the person was is not known; a person with no visit yet
has no journal. The tool reads of another person's location say
"<name> has paused sharing where they are (until 15:00)" and give no position,
and the person's own say recording is paused. The `visits` action lists the
pauses of its range, and `teanode location visits` prints them among the visits
(`paused 14:00 to 15:00`); `teanode location pause [--for D | --until T]` and
`resume` set them.

## What erasing deletes

`EraseLocationHistory` (`teanode location erase`, the tool's `erase`, the
screens) erases the person's positions and visits (in the range, or all of
them), the pauses that began in it and are over, and the places left with no visit
that the person did not name or label.
Places they named or labeled stay. In the same transaction it deletes the
location journal documents for those days, with their chunks. `ForgetPlace`
removes one place and its visits and rewrites the journal for those days.

What it cannot remove is what the agent already learned from those days: the
facts and month pages the dream filed from the journal stay in its memory until
the person removes them there. The texts a person is shown say so.

## What the agent sees

The agent has one `location` tool, whose actions are the CLI's subcommands plus
`set_up`. It reads the same API as the person, with the same limits (50 visits
or places a page and at most 200; 20 results of a search and at most 50; 8
addresses and at most 20), and
answers carry the untrusted mark.

- **In a chat with other people**, the tool answers only about the owner's own
  location. `people`, and any read with `person`, are refused with a sentence
  that another person's location is not repeated into a chat, because the
  agent's words are posted to the room.
- **The journal**: for every person who has visits, one document for each ended
  day (`location:YYYY-MM-DD`), written from the visits with no model call, which
  the dream reads like any other document; and a monthly digest of how many
  hours were spent at which places. Every run looks at the last 14 days again, so
  a day written again after an erase of part of it, a forgotten place or a new
  name or label says what is left. A day whose visits are all erased has no
  document. A visit with no departure lasts until the next visit began. If no visit
  followed, it ends at the time of the latest position when that position was
  measured after the arrival and is outside the place; otherwise it lasts until
  now, but at most 72 hours after the person's last report of any kind (the
  latest of a position's measured time and a visit's arrival or departure). The
  phone sends no positions while the person stays put, only visits and moves of
  about 500 m, so a quiet phone at home is a person at home, and a person at
  home for three days gets three day documents saying so; a phone silent for
  longer than 72 hours is not taken as a person still there. A day's text cuts
  the visit at the day's end.
- **Only where a turn can ask a browser or phone** does `now` ask for a fresh
  position, once; elsewhere it answers the last one kept, with its age.

`ReadPlace(placeId, userId)` answers one place as a row of `ListPlaces` does, by the
same access rules (the caller's own, or another person's at `history` level;
not found otherwise). The dashboard uses it for the link from a photo to a place
that is not on the loaded page; `teanode location place` and the agent's
`places` action with `place_id` read the same.

## A day's path

A day's path is `ListLocationPoints` (at most two days at a time, at `history`
level for another person). The dashboard and the iPhone draw it with the Map
button on each day heading. With somebody sharing at `history` chosen, the
dashboard draws theirs for the day chosen, or today when none is, and the
iPhone's map draws theirs with the places they went for the day their list
shows, today at first; today's path grows as they move. The maps draw the
line through the day's visits too, at each arrival and departure in time
order, since a phone in the background sends few positions on the way. `teanode location path` prints it on the command
line, and the agent's `location_path` answers the number of positions, the first
and last time, the distance travelled in kilometers and up to 60 positions
evenly spread, with `is_truncated` saying some were left out.

## Trips and ways of travel

A trip is the travel from leaving one visit to arriving at the next, and a leg
is a part of a trip with one way of travel: walk, run, bike, car, bus, train,
boat or plane (`docs/planning/location-travel-execplan.md` has the reasons).

- **What they come from.** The visits, the positions and the phone's motion
  activity (table `motion_activity`: what iOS's Core Motion said the person
  was doing from a moment on, sent with `ReportLocation` as `activities` when
  the person allowed Motion & Fitness). Package `internal/travel` builds them
  and knows nothing of the database: the path is the first visit, the
  positions between (an accuracy worse than 500 m is dropped when better ones
  exist, and a jump faster than 300 m/s), then the next visit; less than
  150 m is no trip. Motion activity cuts the trip into legs where the way
  changes (walking, running, cycling, automotive), merging a leg under a
  minute or 100 m into its neighbor. With motion activity for less than half
  of the trip, the trip is one leg guessed from its average speed (up to
  2.2 m/s walk, 5 bike, 75 car, faster plane). A leg of 100 km or more at
  55 m/s or more is a plane whatever the phone said. Bus, train and boat are
  never detected; they come from corrections.
- **When they are built.** Trips are derived, stored in `trip` and
  `trip_leg`, and built again by package `internal/travel/rebuild`: after each
  report for the days it touched (a day before to two days after, and from
  the arrival of a visit it completed), after a check-in, and once at start
  for the days before trips existed (`trip_backfill` says who is done).
  Erasing history erases trips, motion activity and corrections in the
  range.
- **Corrections.** The person corrects a leg (`SetTripLegTravelMode`); the
  correction is kept apart, as a span of time with a way
  (`travel_mode_correction`), and a leg takes the correction that covers half
  of it or more. A rebuild keeps it; taking it back returns the detected way.
- **Totals.** `ListTravelTotals` sums the distance and time of legs by the
  period they started in (day, week from Monday, month, year) in the
  traveler's time zone, at most 400 periods at a time.
- **Who sees them.** Trips and totals are seen at the history share level,
  like the day's path; only the person corrects their own.
- **Where.** The dashboard's day list (a row between visits, a menu per leg
  on one's own) and Travel tab; the iPhone's day list and Travel screen; the
  command line `teanode location trips`, `travel` and `travel-mode`; the
  location tool's `trips`, `travel_totals` and `travel_mode`.

## Photos taken during a visit

`ListLocationVisits` adds `photoCount` and `photos` to each of the caller's
own visits: how many of their library photos (not in the trash) were taken from
the arrival up to, not including, the departure (for a visit nobody ended: up to
the person's latest position when they are no longer at the place, by the rule
`GetLocation` uses for the open visit, else up to now), and the first four, earliest first. One database call (`ListVisitPhotos`) covers
the whole page. They are null for a family member reading the visits, for a
caller without Photos (the service off, or no `photos:use`), and for a visit
with no arrival. Location works without Photos. Other operations that return a
visit (the open visit in `GetLocation`, `CheckIn`) leave them null.
`photosTakenBefore` is where that window ended, and Photos opened over the
visit ends there too: the dashboard's `after` and `before` carry it to the
millisecond, the iPhone's `PhotoFilter` to the second (so a photo taken in the
same second as the arrival or the end may be shown or left out). The agent's
`location_visits` rows give `photo_count`, `photos_taken_after` and
`photos_taken_before` (RFC 3339 in the person's zone, with any fraction of a
second) to pass to the photo tool's `taken_after` and `taken_before`, which
then lists exactly the photos counted.

## Where it lives

- `internal/db/database_location*.go`: the queries; `internal/place`: visits and
  distance, the city table, the providers and the limiter.
- `internal/api/v1api/apigraph/location_history.go` (operations),
  `location_share.go` (shares), `location_events.go` (the live map),
  `location_map.go` (the map proxy), `location_naming.go` (naming right after a
  report), `location_pause.go` (pausing, and dropping what a pause hides from a
  report).
- `internal/agent/place_job.go`, `location_journal.go`, `location_notify.go`,
  `digest_places.go`, `nearby.go`: the agent's side.
- `internal/agent/tools/location` and `internal/cmd/location.go`: the tool and the
  command line, held to the API by `TestLocationParity`.
