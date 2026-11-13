# Parity: mail and contacts

The iPhone files of the mail and contacts area, and what the Android app does with each. Status: **done**, **partly**
(what is missing is said) or **not ported** (why). Every screen has demo data (`MailDemo.kt`, `ManageDemo.kt`,
`ContactsDemo.kt`) and a Roborazzi picture in light and dark (`MailScreenshotTest`, `ManageScreenshotTest`,
`ContactsScreenshotTest`; the dense ones also at the largest text).

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
| ManageMail.swift | done | Models and every call of the operator's side. ManageMailTests ported in part (`ManageMailTest`, 17 of 28): the permission, mail, message, teaching, outcome, filters, report, DNS, alias, overview and word tests; the credential, logo, template and DNS-recheck call tests are not ported (the calls are). |
| ContactImport.swift | done | Matcher and card helpers; ContactImportTests ported (`ContactImportTest`). A vCard writer for the phone's contacts is new (`ContactCardWriter`, tested), since Android has no serializer. |
| PersonCard.swift | partly | The card and its sections are read as far as the card draws them. The location, health, finance and photos sections have small models of their own (`PersonCardLocation`, `PersonCardHealthType`, ...) instead of the models of those parts, which other agents own; unknown fields are ignored. PersonCardTests ported (`PersonCardTest`); the document is the iPhone's text, including its fragments. |
| Search.swift | not ported here | The cross-part search belongs to Settings (SearchTab). Mail and contact targets open with the routes above. |
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
| MailThreadView.swift: MailThreadScreen | partly | Everything but "Ask <agent>" (opens the Chats screens with a reference) and the "In memory" card (InMemoryViews), which belong to Chats. Open-first rule, marking read, drafts open the composer, star, mark read, Sort as, junk, archive, move, delete, Reply, Reply all, Forward, polling of a pending summary. |
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
| ManageMailViews.swift: ManageOverviewView | partly | The mail rows (Mail, Queue, Reports, Domains). People, Groups, Roles, Audit log, Services, Server and Agents belong to Settings. |
| ManageMailViews.swift: AllMailView | done | Filters by domain, status and kind, words, pages of 50, opens fetched. |
| ManageMailViews.swift: AuditedMailView, AuthenticationSection, DeliveryRow | partly | One page with the outcome, teaching the spam filter, facts, SPF, DMARC, DKIM, ARC, spam filter, antivirus, pictures fetched, deliveries, text, attachments (names and sizes) and headers. Not ported: the MX lookups, the spam rules list (SpamRulesView), the rendered and source views (RenderedMailView, MailSourceView, MailHeadersView as screens) and opening an attachment; DeliveryDetailView is folded into the row. |
| ManageMailViews.swift: DeliveryQueueView | done | Filters, words, failing first, Retry now. |
| ManageMailViews.swift: ReportsView, ReportDetailView | partly | List, filters, one report with its records inline. FeedbackRecordView and FeedbackRecordRow are folded into the report page (the DKIM and SPF result lists are not drawn). |
| ManageMailViews.swift: DomainsView, DomainView | done | List with DNS summary and New domain; one page with overview, records, settings, aliases, credentials and templates. |
| ManageMailViews.swift: DomainSettingsView, DNSRecordView | partly | Mail servers, link host, DKIM selector and new key; the DNS record is a row, not a page. Logo upload and removal are not ported (needs an SVG picker). |
| ManageMailViews.swift: DomainAliasesView, AliasForm | done | Add, edit, disable, delete in a dialog. |
| ManageMailViews.swift: DomainCredentialsView, NewCredentialForm, EditCredentialForm | partly | Add (shown once), delete; editing a credential is not ported. |
| ManageMailViews.swift: DomainTemplatesView, TemplateContentSections, DomainTemplateView, DomainLayoutView | partly | Templates and layouts are listed with their locales; reading, editing and deleting them is not ported. |

### Contacts

| iOS file | Status | Notes |
| --- | --- | --- |
| ContactsView.swift: ContactsModel, ContactsScreen | done | Letters, search, suggestions with Link and Not now, "You" mark, the circles' address books (switch, make, rename, delete), contacts of a circle's book (edit, remove for everyone). The book picker is a menu in the bar. |
| ContactsView.swift: ContactEditor, ContactAccountPicker, ContactCardDestination, ReachRow, ContactWords, ContactPicture, Monogram | done | The editor sends every field; the account picker hides accounts taken by another contact. `Monogram` already exists in the foundation. |
| ContactsView.swift: CircleContactEditorSheet | done | The editor route reads the contact in the book named. |
| ContactImportView.swift | done | Reads the phone's contacts (needs `READ_CONTACTS`, asked at the first use), marks those the book has, chooses all new, imports four at a time, reports the result; denied, failed and empty states. |
| PersonCardView.swift, PersonCardLinking.swift | done | Header with Message (opens the direct chat; falls back to the Chats list when Chats has no conversation route yet), Call, Mail, Directions; Edit; link offers in both directions; manage rows (change, remove, "This is me", remove picture, delete) with confirmations. |
| PersonCardSections.swift | partly | Location, Health, Finance, Photos, Mail, Recordings, Memory, details, circles and relationship rows. Simplified against the iPhone: health amounts are the day's number and unit (no HealthText formatter), finance balance is formatted in the account's currency and shows no credit usage, "Show all" opens the part's first screen (the iPhone also passes the person to Location, Health, Finance and Photos, which those parts' routes do not take yet). The relationship row is read-only (the Relationship sheet belongs to Circles). |
| PersonCardDemo.swift | done | `ContactsDemoData`: a linked person, one with no contact, a contact with no account. |
| PersonVisitsScreen.swift | partly | Visits and places by day with a way back and forth. Trips, pauses and the Travel screen (Location's models) and the map are not drawn. |
| RelationshipWords.swift | partly | The words for every kind and gender, tested. `PersonPronoun.title` is for the profile and the Relationship sheet (Settings and Circles). |

## Shared changes

- `AndroidManifest.xml`: one added `READ_CONTACTS` permission, for the import.
- Nothing else of the shell, components or theme was changed.

## Not verified

Everything that needs a real server, phone or credential: sending and receiving real mail, the web view with real mail
HTML and pictures, opening attachments in other apps, the phone's contacts permission and provider, the operator pages
against a real server (the documents are the iPhone's, which the server checks against its schema), the one document
that is not (`ListCircles` with `id name circleKind`, for the circles' address books; and the fields of the person
card's location, health, finance and photos fragments are the iPhone's).
