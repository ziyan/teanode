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
| AgentUpkeep.swift | documents only | none | not ported |
| Circles.swift | `circles/Circles.kt` | `CirclesTest` | done |
| DeviceSetup.swift | `devicesetup/DeviceSetup.kt` | `DeviceSetupTest` | done |
| ManageServer.swift | documents only | none | not ported |
| Services.swift | `services/Services.kt` (capabilities are the foundation's) | none | done |
| Sharing.swift | `sharing/Sharing.kt` (the stop calls carry the mutations of each kind) | `SharingTest` | done |
| SkillSharing.swift | documents only | none | not ported |
| Search.swift, PlaceSearch.swift | `search/Search.kt` | `SearchTest` | done |

GraphQL documents are generated from the Swift text by a script (`$` escaped, interpolated field lists kept), so they
are the iPhone app's documents character for character.
