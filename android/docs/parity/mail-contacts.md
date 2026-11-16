# Parity: mail and contacts

The iPhone files of the mail and contacts area, and what the Android app does with each. Status: **done**, **partly**
(what is missing is said) or **not ported** (why). Every screen has demo data (`MailDemo.kt`, `ManageDemo.kt`,
`ContactsDemo.kt`) and a Roborazzi picture in light and dark (`MailScreenshotTest`, `ManageScreenshotTest`,
`ContactsScreenshotTest`; the dense ones also at the largest text).

Count of the rows below: 42 in all, 39 done, 2 partly, 1 not ported (round 2: the shared-model dedupe, the operator pages and the card's sections moved most rows to done).

Android packages: `com.teanode.kit.mail`, `com.teanode.kit.contacts`, `com.teanode.app.mail`,
`com.teanode.app.mail.manage`, `com.teanode.app.contacts`. Strings: `mail_strings.xml`, `mail_manage_strings.xml`,
`contacts_strings.xml` (English, zh-rCN, ja).

## Routes

| Route | Screen |
| --- | --- |
| `mail/home` | the folders (Inbox, Starred, Priority, Pinned, Folders, Subscriptions) |
| `mail/place/{place}` | a folder, Starred, Priority (`folder:<id>`, `starred`, `priority`) or the Subscriptions list |
| `mail/search?words=` | the Inbox searched everywhere, as the Search tab's "Show all in Mail" opens it |
| `mail/thread/{itemId}?folderId=` | one conversation |
| `mail/compose/{kind}?itemId=&to=` | a message being written (`new`, `reply`, `replyAll`, `forward`, `draft`) |
| `mail/subscription/{key}` | one mailing list read |
| `mail/settings`, `/settings/general`, `/settings/auto-reply`, `/settings/folders`, `/settings/programs`, `mail/rules` | mailbox settings |
| `mail/manage`, `/manage/all-mail`, `/manage/mail/{mailId}`, `/manage/queue`, `/manage/reports`, `/manage/report/{id}`, `/manage/domains`, `/manage/domain/{id}` | the operator's pages |
| `contacts/home` | the address book |
| `contacts/contact/{contactId}` | a contact, drawn as the person's card |
| `contacts/editor?contactId=` | new or edit contact |
| `contacts/import` | import from the phone |
| `contacts/person/account/{userId}`, `contacts/person/contact/{addressBookId}/{contactId}` | a person's card; other parts open it with `ContactsRoutes.person(target)` |
| `contacts/visits/{userId}` | the visits and places of somebody who shares where they have been |

## Kit (ios/TeaNodeKit)

| iOS file | Status | Notes |
| --- | --- | --- |
| Mail.swift | done | Models, folder tree, verdicts, reply and forward rules, remote-image rewriting, filter, outgoing message and pending send, every call. Documents are generated from the iPhone's text. MailTests ported (`MailTest`). |
| MailboxSettings.swift | done | Auto-reply form, rules and their wire form, folders, app passwords, setup profile call. MailRuleTests ported (`MailRuleTest`). |
| ManageMail.swift | done | Models and every call of the operator's side. ManageMailTests ported in full (`ManageMailTest` and `ManageMailCallsTest`, 28 of 28; the two alias-editor state tests that need the iPhone's editor object are folded into `knowsWhichKindsAndDestinationsAnAliasEditorOffers`). |
| ContactImport.swift | done | Matcher and card helpers; ContactImportTests ported (`ContactImportTest`). A vCard writer for the phone's contacts is new (`ContactCardWriter`, tested), since Android has no serializer. |
| PersonCard.swift | done | The card is read with the real models of the other parts: `PersonLocation` and `PlaceVisit` (location), `HealthTypeView` (health), `SharedFinanceAccount` (finance), `Photo` and `PhotoAlbum` (photos), `CircleMember` (circles). PersonCardTests ported (`PersonCardTest`); the document is the iPhone's text, including its fragments. |
| Search.swift | done by Settings | The cross-part search and SearchTests are Settings' (`kit/search/SearchTest.kt`, 6 tests of the 11; the rest are the iPhone's area-opening tests the Settings area words differently). Mail and contact targets open with the routes above. |
| Calendar.swift (address books and contacts only) | done | `AddressBook`, `ContactSummary`, `ContactDetail`, the link and its suggestions and every contacts call are in `kit.contacts`. ContactAccountTests ported (`ContactAccountTest`). The calendar parts belong to Calendar. |

## Screens (ios/TeaNode/Sources)

### Mail

| iOS file | Status | Notes |
| --- | --- | --- |
| MailModel.swift | done | `MailModel`: mailboxes, folders, rail rows, categories, undo for move, delete and junk (the snackbar's Undo), one per account. |
| MailViews.swift: MailScreen, MailPlaceScreen, MailSearchScreen, OpenedThread | done | Routes above, with deep links. |
| MailViews.swift: MailboxesList, FolderLink, MailNames | done | Inbox with its children, Starred, Priority (when the agent sorts), Pinned, Folders, Subscriptions, the mailbox picker, New message, loading, error and "No mailbox yet". |
| MailViews.swift: ThreadList, ThreadListModel | done | Periods (Today to months), pages of 50, search, Filter menu (Unread, Starred, Narrow, Show everything), Select, swipe right for read or unread and left for Archive (Delete where there is no archive or in it), the row actions in the menu of chosen rows (read, star, Sort as, archive, junk, Move to, delete or delete forever), Empty Trash with confirmation, five empty states. The long-press context menu is the Select mode; there is no tab bar to hide. |
| MailViews.swift: ThreadRow, MailRowMarks, SenderLogo, VerdictShield, MailTime, ListPeriod, ListTime | done | Unread dot, sender logo or initial, marks (important, needs reply, shield, star), attachment mark, thread count, two-line summary. `MailTimes` is tested. |
| MailViews.swift: InsightChips, Chip | not ported | Defined on the iPhone but drawn nowhere. |
| MailViews.swift: NarrowSheet, OptionalDatePicker, SortSheet | done | Bottom sheets; date fields with the Material date picker. |
| MailThreadView.swift: MailThreadScreen | done | "Ask <agent>" calls `ChatsHooks.askAbout` with `AgentReference.mail`, and the "In memory" card is Knowledge's `InMemoryCard`. Open-first rule, marking read, drafts open the composer, star, mark read, Sort as, junk, archive, move, delete, Reply, Reply all, Forward, polling of a pending summary. |
| MailThreadView.swift: HeldReplyBanner, SummaryStrip, MessageDetails, ProposalCard, InvitationCard | done | Proposals add the event with a request id (as Calendar's change does) or save the contact. Invitation answers keep their request id on the phone until sent. |
| MailThreadView.swift: MessageCard (and saveSender) | done | Folded and opened states, shield, details, headers, save sender to contacts, read as receipt or tax document, download original, original look versus darkened, attachments (opened through the phone's Downloads), "Pictures are held back" with Load. |
| MailThreadView.swift: MailBody, MailWebView | done | An Android `WebView` with scripts off; every picture it asks for is read by the app as the person, from the server only; links open in the browser. The picture is not drawn in the Robolectric photographs (a WebView does not render there), so the demo messages are plain text. |
| ComposeView.swift: ComposeStart, ComposeModel, ComposeView | done | Draft kept saved every 30 seconds and when the app stops, pending send asked about before it is sent again, quote and forward, agent draft, From picker, Cc and Bcc, size limit, close dialog (save, discard). |
| ComposeView.swift: AddressField, AttachButton | done | Contact suggestions after two letters; attach from photo library, camera or files. |
| MailboxSettingsView.swift | partly | Settings page, name and signature, auto-reply, folders (add, rename, pin, delete), mail program passwords (create shown once and copied, revoke, program settings). "Set up on this iPhone" (DeviceSetupView, a configuration profile) is iOS only; the password row is a plain row (DevicePasswordRow belongs to Passwords). |
| MailRulesView.swift | done | List, turn on or off, remove, reorder (Move up and Move down instead of dragging), editor with conditions and actions, "Try on recent mail". |
| MailSubscriptionsView.swift | done | List with Subscribed and Unsubscribed, search, paging, reader with mark read, pictures, mute, junk, move, delete, Archive all, unsubscribe by kind. |

### Manage (operator)

| iOS file | Status | Notes |
| --- | --- | --- |
| ManageMailViews.swift: ManageOverviewView | partly | The mail rows (Mail, Queue, Reports, Domains). People, Groups, Roles, Audit log, Services, Server and Agents belong to Settings, which owns their routes. |
| ManageMailViews.swift: AllMailView | done | Filters by domain, status and kind, words, pages of 50, opens fetched. |
| ManageMailViews.swift: AuditedMailView, AuthenticationSection, DeliveryRow | done | One page with the outcome, teaching the spam filter, facts, SPF (with the iPhone's sentence per result), DMARC, DKIM (with the signing identity), ARC, spam filter, antivirus, attachments refused, the MX lookups of the sender and From domains, pictures fetched, deliveries (each opens DeliveryDetailView), and the message section: Rendered, Text, HTML, Headers, Raw headers, Download .eml and attachments, each opened as the iPhone opens it. SpamRulesView is `SpamRulesPage`. |
| ManageMailViews.swift: DeliveryQueueView | done | Filters, words, failing first, Retry now. |
| ManageMailViews.swift: ReportsView, ReportDetailView | done | List, filters, one report; each source of the report opens FeedbackRecordView (evaluation, override reason, addresses, the DKIM and SPF result lists). |
| ManageMailViews.swift: DomainsView, DomainView | done | List with DNS summary and New domain; one page with overview, records, settings, aliases, credentials and templates. |
| ManageMailViews.swift: DomainSettingsView, DNSRecordView | done | Mail servers, link host, DKIM selector and new key; each DNS record opens its page (name and value with copy, what is published now, why it is blocked); logo upload through the system file picker (SVG), replace and remove. |
| ManageMailViews.swift: DomainAliasesView, AliasForm | done | Add, edit, disable, delete in a dialog. |
| ManageMailViews.swift: DomainCredentialsView, NewCredentialForm, EditCredentialForm | done | Add (shown once), edit (what it is for, the one address it may send as, disabled), delete. |
| ManageMailViews.swift: DomainTemplatesView, TemplateContentSections, DomainTemplateView, DomainLayoutView | done | Templates and layouts with their details; a template or layout opens to read its subject, HTML and text in each language; both can be removed. The iPhone does not edit them either: they are written on the dashboard. |

### Contacts

| iOS file | Status | Notes |
| --- | --- | --- |
| ContactsView.swift: ContactsModel, ContactsScreen | done | Letters, search, suggestions with Link and Not now, "You" mark, the circles' address books (switch, make, rename, delete), contacts of a circle's book (edit, remove for everyone). The book picker is a menu in the bar. |
| ContactsView.swift: ContactEditor, ContactAccountPicker, ContactCardDestination, ReachRow, ContactWords, ContactPicture, Monogram | done | The editor sends every field; the account picker hides accounts taken by another contact. `Monogram` already exists in the foundation. |
| ContactsView.swift: CircleContactEditorSheet | done | The editor route reads the contact in the book named. |
| ContactImportView.swift | done | Reads the phone's contacts (needs `READ_CONTACTS`, asked at the first use), marks those the book has, chooses all new, imports four at a time, reports the result; denied, failed and empty states. |
| PersonCardView.swift, PersonCardLinking.swift | done | Header with Message (opens the direct chat; falls back to the Chats list when Chats has no conversation route yet), Call, Mail, Directions; Edit; link offers in both directions; manage rows (change, remove, "This is me", remove picture, delete) with confirmations. |
| PersonCardSections.swift | done | Location (the shared `LocationVisitRow`, pause rows, place icons), Health (`HealthTypeRow`), Finance (shared account rows with `AmountText`), Photos (`PhotoCell` strip, `PhotoAlbumRow`), Mail, Recordings, Memory, details, circles and relationship rows. "Show all" passes the person: Location selects them on the map (`LocationModel.select`), Health selects whose data is shown (`HealthModel.select`), Finance opens the shared accounts of that sharer, Photos opens filtered to them (`PhotoFilterRequest.openPerson`). The relationship row is read-only (the Relationship sheet belongs to Circles). |
| PersonCardDemo.swift | done | `ContactsDemoData`: a linked person, one with no contact, a contact with no account. |
| PersonVisitsScreen.swift | done | The page uses Location's own `PersonVisitsState` and `LocationPersonVisitsList`: visits, pauses and trips of the day, a way back and forth by day, their places, and a map of the day's path (`TeaNodeMap`). The Travel screen is the person's own, so it is not offered for somebody else. |
| RelationshipWords.swift | done by Settings | The words are Settings' `relationshipWord` and `relationshipWordOf`, over the Circles area's `RelationshipKind` and `RelationshipTermGender`; the duplicate models and strings of this area are removed. |

## Shared changes

- `AndroidManifest.xml`: one added `READ_CONTACTS` permission, for the import.
- Nothing else of the shell, components or theme was changed.

## Not verified

Everything that needs a real server, phone or credential: sending and receiving real mail, the web view with real mail
HTML and pictures, opening attachments in other apps, the phone's contacts permission and provider, the operator pages
against a real server (the documents are the iPhone's, which the server checks against its schema; `ListCircles` is now
the Circles area's copy of the iPhone's text), the logo upload through the system file picker, copying to the clipboard,
the map tiles of the person's visits page (demo mode draws a stand-in), and the "Show all" hand-offs into Location, Health,
Finance and Photos on a real account (they use the other parts' own models and request objects; the screenshots show the
rows, not the destination).
