# Parity: Settings and Search

The iPhone files of this area and what the Android app does with each. Status is done, partly or not ported. Kit tests
are in `android/kit/src/test/kotlin/com/teanode/kit/{account,circles,search,sharing,devicesetup,access,agent}`; screens are
in `android/app/src/main/java/com/teanode/app/settings` and `.../search`; screenshots are named `settings-*` and
`search-*` (`android/app/build/outputs/roborazzi/`).

## Screens (ios/TeaNode/Sources)

| iOS file | Android | Status |
| --- | --- | --- |
| SettingsPages.swift | `SettingsRoutes.kt`, `SettingsNavigation.kt`: one route per page (`settings/<page>`, each also `teanode://app/...`) and the places Search offers | done for the pages that exist (no knowledge sources, memory, dreams, schedules, alerts, connections, activity, server, agents pages, see AgentUpkeepViews and ManageServerViews) |
| (TeaNodeApp.swift) Settings home | `SettingsSection.kt`: accounts (switch, add), Your account pages, Photos backup row, Your agent, Manage (for an operator), Display, Mailbox row, sign out with confirmation, version | partly: no notifications section (calls-notifications area), no Health, Journal, People in Photos, Sources, Memory, Dreams, Schedules, Alerts, Connections or Activity rows, no update row |
| DisplaySettings.swift | `SettingsSection.kt` Appearance (system, light, dark) stored by the foundation, and Language that opens Android's per-app language settings (Android 13 and later) | partly: the language is chosen in the system's own settings, not in the app, and the row is hidden below Android 13 |
| AccountProfileViews.swift | `AccountProfileScreens.kt`: Profile (picture from the gallery redrawn as a JPEG at most 2048 px, remove picture, name, pronoun, units, username with rename confirmation, notification address, time zone auto or fixed), Password, Devices, Phones (list, edit name and notification kinds, test notification, remove), Passkeys (list, remove) | done (no passkey creation: iOS has none either, passkeys are added on the dashboard); the Devices page leaves out "Set up on this iPhone" and "Sync passwords" is its own Device sync page |
| AccountSecurityViews.swift | `AccountSecurityScreens.kt`: Sessions (list, end one, end all), API tokens (list, new with lifetime, secret shown once with copy and the export lines, edit name and lifetime, revoke), Apps (list, rename, disconnect, this phone marked) | done |
| CirclesView.swift, CirclesDemo.swift | `CirclesScreens.kt`: circles list with member pictures, invitations (accept, decline), new circle, one circle (members with relationship words and admin tag, make admin or member, remove, invite by username, make an account for someone, pending invitations and withdraw, rename, delete, leave), relationship dialog with the pronoun question; demo data in `SettingsDemo.kt` | partly: no address book and no photo albums of a circle, no Photos row on a member (those need the contacts and photos areas), no calendar and location offer sheet after accepting an invitation (kit has `CircleCalendarOffer`, the sheet needs the calendar sharing of the calendar area) |
| SharingView.swift, SharingDemo.swift | `SharingScreen.kt`: shared by you and with you, what, who, level, scopes and date, stop sharing, remove what was shared with you, the explanation when a share cannot be removed | done (level words are the ones a note of a new share uses, not each kind's own titles) |
| SkillSharingViews.swift | kit documents only (`SkillSharingDocuments.kt`) | not ported: no screen (lending a skill, the calls waiting for approval) |
| DeviceSetupView.swift | kit `DeviceSetup.kt` with its tests (capabilities, selection, validated download address) | not possible on Android: a setup profile is an iOS and macOS configuration profile. The manual sync password part is on the Device sync page |
| ServicesView.swift | `ServicesScreens.kt`: server services (switches, review and apply), Device sync (create a sync password, shown once with copy, list, revoke) | done |
| AccessManagementViews.swift | `AccessScreens.kt`: People (search, new, edit, disable, set password, remove), Groups (new, edit, members, roles and domains chosen from lists, remove), Roles (new, edit, tick permissions with their words, remove), Audit (filter by kind, list, detail with changed fields) | partly: no "add to a circle" action on a person, no roles and domains list on a person's page |
| ManageServerViews.swift | `ServicesScreens.kt` covers its Services page; the Manage page lists People, Groups, Roles, Audit, Services and the mail pages by route | not ported: the Server settings tabs, the Agents administration, the mail delivery pages (those belong to the mail area), the About and restart page |
| AgentSettingsViews.swift | `AgentSettingsScreens.kt`: Your agent (turn on, forget everything), About (on, name, budget, zone, language), Instructions, Language, Voice (tone, length, greeting, sign-off), Speaking voice (choose), What it may reach (mailboxes with the policy in a dialog: sorting, summaries, replies and search, answering with scope, when, lists, hold, limit; calendars and address books), Categories (add, edit, remove), Model, Speaking first, Ask me first (families and tools), When you are not there | partly: no listen sample for a speaking voice (needs audio playback of a WAV), the mailbox policy is one dialog, not pages, the answering hours (From and Until) are kept as they are when "outside my hours" is chosen and cannot be edited |
| AgentUpkeepViews.swift (3923 lines) | kit documents only (`AgentUpkeepDocuments.kt`) | not ported: Sources, Memory, Dreams, Schedules, Alerts, Connections, Activity, usage and the price check screens |
| SearchTab.swift | `search/SearchSection.kt`: field with a short wait after the last key, the places of the app the words name (from every part's `Area.places` plus the parts), what the words find in each part (rows, a grid of pictures for photos), Show all, Search Photos, the parts that did not answer, empty and error states | partly: a result opens the item for chats (`agent/conversation/<id>`), mail (`mail/thread/<id>`) and recordings (`recordings/detail/<id>`) and the part's first screen for the rest; the words are not carried into the part's own search |
| ServerSetupGuideView.swift | the foundation's sign-in `ServerGuide` | not ported here: the foundation did it |

## Kit (ios/TeaNodeKit/Sources/TeaNodeKit)

| iOS file | Kotlin (`com.teanode.kit.*`) | Tests | Status |
| --- | --- | --- | --- |
| AccountProfile.swift | `account/AccountProfile.kt` (+ `AccountProfileDocuments.kt`) | `AccountProfileTest` | done |
| AccountSecurity.swift | `account/AccountSecurity.kt` | `AccountSecurityTest` | done |
| AccessManagement.swift | `access/AccessManagement.kt`, `access/OperatorAccess.kt` (from ManageMail.swift, the part the settings need) | `AccessManagementTest` | done (the session is the foundation's `AccessSession`) |
| AgentBilling.swift | `agent/AgentBilling.kt` | none | ported, not tested, no screen |
| AgentSettings.swift | `agent/AgentSettings.kt` | `AgentSettingsTest` | done except the speech audio calls |
| AgentUpkeep.swift | `agent/AgentUpkeep.kt`, `agent/KnowledgeSources.kt`, `agent/ScheduleTiming.kt` | `AgentUpkeepTest`, `KnowledgeSourcesTest`, `ScheduleTimingTest` | done |
| Circles.swift | `circles/Circles.kt` | `CirclesTest` | done |
| DeviceSetup.swift | `devicesetup/DeviceSetup.kt` | `DeviceSetupTest` | done |
| ManageServer.swift | `manage/AgentAdmin.kt`, `manage/ServerAdmin.kt` | `AgentAdminTest`, `ServerAdminTest` | done |
| Services.swift | `services/Services.kt` (capabilities are the foundation's) | none | done |
| Sharing.swift | `sharing/Sharing.kt` (the stop calls carry the mutations of each kind) | `SharingTest` | done |
| SkillSharing.swift | `skillsharing/SkillSharing.kt` | `SkillSharingTest` | done |
| Search.swift, PlaceSearch.swift | `search/Search.kt` | `SearchTest` | done |

GraphQL documents are generated from the Swift text by a script (`$` escaped, interpolated field lists kept), so they
are the iPhone app's documents character for character.


## Round 2 (second pass over what was not ported)

| iOS file | Android | Status |
| --- | --- | --- |
| AgentUpkeepViews.swift: Dreams, Alerts, Schedules, Memory, Connections, Activity, Sources | `UpkeepScreens.kt` (+ kit `agent/AgentUpkeep.kt`, `agent/KnowledgeSources.kt`, tests `AgentUpkeepTest`): dreams (state, dream now, hours, reading progress, each dream with what it did and cost), alerts (switch, quiet hours and daily most, recent alerts with mute, mutes with unmute), schedules (list, switch, run now, delete, new with a cron line), memory (learned lately, corrections, check questions, start a check, grade now), connections (servers, status, disconnect), activity (background work with stop, recent runs), sources (list with counts, switch, read again, forget) | partly: no editor for a schedule's mail conditions or the daily brief, no memory check run history or answers, no source editor or secrets, no source types, no connecting a server, no skill secrets, reaches or chat channels, no per-run detail, no usage page |
| ManageServerViews.swift: Agents | `AgentAdminScreen.kt` (+ kit `manage/AgentAdmin.kt`, `AgentAdminTest`): everybody's agents with today's use, daily token and cost limits, switch off by an operator | partly: no usage table, dead letters or the other tabs |
| ManageServerViews.swift: Server, mail delivery, About | not ported: the Server settings tabs (about, setup, identity, mail, sending, listeners, certificates, storage, resolver, scanning, spam, sessions, SSO) and the restart page are 4,000 lines of forms for an operator at a desk | not ported |
| SkillSharingViews.swift | `SkillSharingScreen.kt` (+ kit `skillsharing/SkillSharing.kt`, `SkillSharingTest`): shares lent by you and to you, lend a skill (tools, person from the circles, limits in the currency's digits, approval), stop lending, the calls of a share with Approve and Deny | done (the answer note is not asked for) |
| Settings home | Health settings (`health/settings`), Journal (`journal/settings`), People in Photos (Photos' People collection in a Settings route), Skill sharing, Sources, Memory, Dreams, Schedules, Alerts, Connections, Activity rows; the Notifications section (a switch for each kind from `PushSystem.registrar`, or the way into Android's notification settings), the update row from `PushSystem.appUpdates` | done |
| CirclesView.swift | the address book of a circle (make, shown), the circle's photo albums (list, new), the question after accepting an invitation (calendar level, and for a family where the person is) | done (the Photos row on a member is not ported) |
| AgentSettingsViews.swift | Listen to a speaking voice (the server's WAV sample played with MediaPlayer), the hours of "outside my hours" answering editable | done (the days of the week are kept as they are) |
| SearchTab.swift | "Show all in Mail" opens `mail/search?words=...` with the typed words | partly: Mail is the only part whose route takes words; the others open their first screen |


## Round 3

| iOS file | Android | Status |
| --- | --- | --- |
| ManageServerViews.swift: Server tabs | `ServerScreens.kt` (+ kit `manage/ServerAdmin.kt`, `ServerAdminTest`): tabs for identity, mail, sending, listeners, certificates, storage, resolver, scanning, sessions and SSO, built from the sections `GetSettings` returns (switch, number, text, list of words, secret, shown value) with a Save for each section. Secrets are never read back: a sealed value is shown as kept, sent only when typed, cleared with an empty value | done in round 4 (the Setup tab and SSO editing are below) |
| ManageServerViews.swift: About, restart | About tab: instance, version and commit, running since, settings waiting for a restart, Restart the server, check for and apply an upgrade | done |
| ManageServerViews.swift: mail delivery | the Manage hub links to the mail area's delivery pages (`mail/manage/*`) | done (those pages belong to the mail part) |
| ManageServerViews.swift: Usage | `AgentUsageScreen` (+ kit `AgentUsageRow`, `AgentDeadJob`, `usageBounds`): group by day, agent or model, range, tokens, calls and cost, jobs given up on | done |
| AgentUpkeepViews.swift: schedules | `ScheduleEditorDialog`: name, a time with weekday chips or mail conditions (field, header, operator, value), deliver by mail or into the conversation, the prompt; the daily brief card (switch, time, weekdays, send one now). Kit: `agent/ScheduleTiming.kt`, `ScheduleTimingTest` | done |
| AgentUpkeepViews.swift: memory | the scores of each source with the change in points and history, the list of gradings, the answers of a grading (verdict, reason, answer), editing, dropping and confirming a check question, show dropped | done |
| AgentUpkeepViews.swift: sources | `KnowledgeSourceEditorDialog`: files on a computer (computer, path, root, cron) and every typed source the server offers (its settings as fields, its secrets as password fields that are never filled in again, kept or new), the list opens it, a button adds one | partly: sources of the journal, records, sent mail and finance shapes cannot be edited |
| CirclesView.swift: member photos | a Photos item in the menu of a member linked to a contact, opening that member's photos | done |
| SkillSharingViews.swift: answer note | a note field on Approve and Deny, shown on the call | done |
| AgentSettingsViews.swift: days | weekday chips for "outside my hours" answering | done |
| Open pictures | every picture of rounds 2 and 3 was opened; the ones that showed an empty circle, a failed memory page, a missing tab and a raw section name were fixed (see the report) | done |

Not verified: everything that needs a real server, phone or credential (saving a server section, a restart, an upgrade, a
source secret reaching a computer). The kit tests and the demo screenshots cover the calls and the screens only.


## Round 4

| iOS file | Android | Status |
| --- | --- | --- |
| ManageServerViews.swift: Setup tab | `ServerSetupScreens.kt` (+ kit `manage/ServerSetup.kt`, `ServerSetupTest`): this server's address (A, AAAA, why none), how outgoing mail identifies itself (sends from, reverse name, greeting name, each with OK or Change and the advice), what to tell a mail client and a mail program, each opening a form that follows the server when empty | done |
| ManageServerViews.swift: identity providers | `SsoProvidersTab`: every provider (ID, button text, issuer URL, client ID, client secret, groups claim, create accounts), add, remove, the redirect URL to register. The secret is never read back: a stored one shows "Kept as stored; type to replace" and is sent only when typed | done |
| ManageServerViews.swift: Server tabs | the tab row is now About, Setup and the ten settings tabs | done |
| AgentUpkeepViews.swift: schedule words | `UpkeepWords.kt` (+ kit `ScheduleTiming.timeText`, `ScheduleWeekday.name`, `shortNamesText`; tests `ScheduleTimingTest`, `UpkeepWordsTest`): "Weekdays at 7:30 AM", "Fridays at 5:00 PM", "Every Mon, Wed, and Fri at 9:00 AM", "When mail arrives: From contains ..."; any other line is shown as written | done |
| AgentUpkeepViews.swift: memory words | question state, question kind and answer verdict in the reader's words | done |
| ManageMailViews.swift (mail delivery pages) | already covered by the mail part: `mail/manage/*` has the overview, all mail, one message with its authentication, spam rules, deliveries, rendered body, source and headers, the queue, reports and one report with its records, domains, one domain with its settings, DNS records, aliases, credentials, templates and layout. Checked page by page against the iOS list; nothing duplicated here, and the Manage hub links to them | done |
| People edit dialog picture | the demo now answers the access session as an operator, so People, Groups and Roles show their buttons | done |
