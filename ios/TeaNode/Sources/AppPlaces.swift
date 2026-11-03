import SwiftUI
import TeaNodeKit

/// AppPlaceTarget is where a place of the app is: a part, or a page, a list
/// or a view inside one, as the part is asked to open it.
enum AppPlaceTarget: Hashable {
    case section(AppSection)
    /// Pages of Settings, outermost first.
    case settings([SettingsPage])
    case financePart(FinancePart)
    case photoSection(PhotoSection)
    case healthSettings
    case location(LocationRoute)
    case passwords(PasswordsDestination)
    case mail(MailPlace)
    case calendar(CalendarModel.ViewMode)
}

/// AppPlaceName is a place's name in the reader's language and in English,
/// so the search finds it by either.
struct AppPlaceName: Hashable {
    let title: String
    let englishTitle: String

    /// The name the String Catalog holds under the key, and its English. The
    /// keys are the English words but for a few, which say their English.
    init(_ key: String, english: String? = nil) {
        englishTitle = english ?? key
        title = Bundle.main.localizedString(forKey: key, value: englishTitle, table: nil)
    }

    init(title: String, englishTitle: String) {
        self.title = title
        self.englishTitle = englishTitle
    }
}

/// AppPlace is a place of the app the Search tab offers by name, as the
/// dashboard's search box offers its pages and their tabs.
struct AppPlace: Identifiable, Hashable {
    let id: String
    let name: AppPlaceName
    /// The places it is inside, outermost first.
    let parents: [AppPlaceName]
    let systemImage: String
    let target: AppPlaceTarget

    /// What the row says: the places it is inside, then its own name.
    var shownTitle: String {
        (parents.map(\.title) + [name.title]).joined(separator: " › ")
    }

    var placeName: PlaceName {
        PlaceName(placeId: id, names: [name.title, name.englishTitle], parentNames: parents.map { [$0.title, $0.englishTitle] })
    }
}

/// AppPlaces is every place the person can open, in the order the app shows
/// them: the parts as More lists them, then the places inside each, then the
/// pages of Settings in its order. Only what the account has is offered, by
/// the same checks the screens make.
@MainActor
enum AppPlaces {
    static func all(app: AppModel, mail: MailModel?, access: OperatorAccess?) -> [AppPlace] {
        var places: [AppPlace] = []
        let sections = AppSectionGroup.allCases.flatMap(\.sections).filter { app.isSectionAvailable($0) } + [.settings]
        for section in sections {
            places.append(AppPlace(id: "section-\(section.rawValue)", name: sectionName(section), parents: [], systemImage: section.systemImage, target: .section(section)))
        }
        for section in sections {
            places += placesInside(section, app: app, mail: mail)
        }
        places += settingsPlaces(app: app, mail: mail, access: access)
        return places
    }

    /// A part's name: its title, and the English the catalog's key is.
    private static func sectionName(_ section: AppSection) -> AppPlaceName {
        let english: String
        switch section {
        case .agent: english = "Chats"
        case .health: english = "Health"
        default: english = section.rawValue.prefix(1).uppercased() + section.rawValue.dropFirst()
        }
        return AppPlaceName(title: section.title, englishTitle: english)
    }

    private static func placesInside(_ section: AppSection, app: AppModel, mail: MailModel?) -> [AppPlace] {
        let parent = [sectionName(section)]
        func place(_ id: String, _ name: AppPlaceName, _ systemImage: String, _ target: AppPlaceTarget) -> AppPlace {
            AppPlace(id: "\(section.rawValue)-\(id)", name: name, parents: parent, systemImage: systemImage, target: target)
        }
        switch section {
        case .mail:
            guard let mail, mail.current != nil else { return [] }
            var places: [AppPlace] = []
            let folders = mail.folders
            for folder in folders where folder.kind == "inbox" {
                places.append(place("folder-\(folder.id)", folderName(folder), MailNames.symbol(folder), .mail(.folder(folder.id))))
            }
            places.append(place("starred", AppPlaceName("Starred"), "star", .mail(.starred)))
            if mail.mailbox?.agent?.isSorting == true {
                places.append(place("priority", AppPlaceName("Priority"), "exclamationmark.circle", .mail(.priority)))
            }
            for folder in folders where folder.kind != "inbox" {
                places.append(place("folder-\(folder.id)", folderName(folder), MailNames.symbol(folder), .mail(.folder(folder.id))))
            }
            places.append(place("subscriptions", AppPlaceName("Subscriptions"), "newspaper", .mail(.subscriptions)))
            return places
        case .calendar:
            let services = app.services ?? .unavailable
            return CalendarModel.ViewMode.allCases.compactMap { mode -> AppPlace? in
                let isOffered = mode == .reminders ? services.isRemindersEnabled : services.isCalendarEnabled
                guard isOffered else { return nil }
                return place("view-\(mode.rawValue)", calendarViewName(mode), mode.systemImage, .calendar(mode))
            }
        case .photos:
            return PhotoSection.allCases.map { photoSection -> AppPlace in
                let name = photoSection == .people ? AppPlaceName("photos.people", english: "People") : AppPlaceName(photoSection.rawValue.prefix(1).uppercased() + photoSection.rawValue.dropFirst())
                return place(photoSection.rawValue, name, photoSection.symbolName, .photoSection(photoSection))
            }
        case .location:
            return [
                place("settings", AppPlaceName("Location settings"), "gearshape", .location(.settings)),
                place("travel", AppPlaceName("Travel"), "figure.walk", .location(.travel)),
            ]
        case .health:
            return [place("settings", AppPlaceName("Health settings"), "gearshape", .healthSettings)]
        case .passwords:
            return [
                place("settings", AppPlaceName("Settings"), "gearshape", .passwords(.settings)),
                place("deleted", AppPlaceName("Recently deleted"), "trash", .passwords(.deleted)),
            ]
        case .finance:
            return FinancePart.allCases.map { part in
                place(part.rawValue, AppPlaceName(title: part.title, englishTitle: financePartEnglish(part)), part.systemImage, .financePart(part))
            }
        default:
            return []
        }
    }

    /// A folder's name: a system folder's in the reader's language and in
    /// English, the person's own as they named it.
    private static func folderName(_ folder: MailFolder) -> AppPlaceName {
        let english: String
        switch folder.kind {
        case "inbox": english = "Inbox"
        case "drafts": english = "Drafts"
        case "sent": english = "Sent"
        case "archive": english = "Archive"
        case "junk": english = "Junk"
        case "trash": english = "Trash"
        default: english = folder.name
        }
        return AppPlaceName(title: MailNames.folder(folder), englishTitle: english)
    }

    private static func calendarViewName(_ mode: CalendarModel.ViewMode) -> AppPlaceName {
        let english: String
        switch mode {
        case .month: english = "Month"
        case .week: english = "Week"
        case .workweek: english = "5 days"
        case .day: english = "Day"
        case .agenda: english = "Agenda"
        case .reminders: english = "Reminders"
        }
        return AppPlaceName(title: mode.title, englishTitle: english)
    }

    private static func financePartEnglish(_ part: FinancePart) -> String {
        switch part {
        case .spending: "Spending"
        case .transactions: "Transactions"
        case .accounts: "Accounts"
        case .budgets: "Budgets"
        case .netWorth: "Net worth"
        case .savingsTargets: "Savings targets"
        case .receipts: "Receipts"
        case .taxes: "Taxes"
        case .sharing: "Sharing"
        case .setup: "Banks and currency"
        }
    }

    /// The pages of Settings, in its order, with the pages inside Manage,
    /// Agent settings and Mailbox under them.
    private static func settingsPlaces(app: AppModel, mail: MailModel?, access: OperatorAccess?) -> [AppPlace] {
        guard app.connection != nil else { return [] }
        let settings = sectionName(.settings)
        var places: [AppPlace] = []
        func add(_ id: String, _ key: String, _ systemImage: String, _ pages: [SettingsPage], inside: [AppPlaceName] = []) {
            places.append(AppPlace(id: "settings-\(id)", name: AppPlaceName(key), parents: [settings] + inside, systemImage: systemImage, target: .settings(pages)))
        }
        add("profile", "Profile", "person.crop.circle", [.profile])
        if app.accessSession?.holds("circle:use") == true {
            add("circles", "Circles", "person.2", [.circles])
            add("sharing", "Sharing", "square.and.arrow.up", [.sharing])
        }
        add("password", "Password", "key", [.password])
        add("passkeys", "Passkeys", "person.badge.key", [.passkeys])
        add("api-tokens", "API tokens", "terminal", [.apiTokens])
        add("devices", "Devices", "iphone", [.devices])
        add("apps", "Apps", "app.badge.checkmark", [.apps])
        add("sessions", "Sessions", "macbook.and.iphone", [.sessions])
        if app.isPhotosOffered {
            add("photo-backup", "Photo backup", "icloud.and.arrow.up", [.photoBackup])
            add("photo-faces", "People in Photos", "person.crop.circle", [.photoFaces])
        }
        if app.canUseJournal {
            places.append(AppPlace(id: "settings-journal", name: AppPlaceName(title: JournalText.title, englishTitle: "Journal"), parents: [settings], systemImage: "book.closed", target: .settings([.journal])))
        }

        let agentSettings = AppPlaceName("Agent settings")
        add("agent-settings", "Agent settings", "slider.horizontal.3", [.agentSettings(nil)])
        let agentParts: [(String, String, String, AgentSettingsPart)] = [
            ("agent-about", "About your agent", "person.crop.circle", .about),
            ("agent-instructions", "Standing instructions", "text.quote", .instructions),
            ("agent-language", "Language", "globe", .language),
            ("agent-voice", "Voice", "text.bubble", .writingVoice),
            ("agent-speaking-voice", "Speaking voice", "speaker.wave.2", .speakingVoice),
            ("agent-reach", "What it may reach", "tray.2", .reach),
            ("agent-categories", "Your own categories", "tag", .categories),
            ("agent-model", "Model for conversations", "cpu", .model),
            ("agent-speaking-first", "Speaking first", "bubble.left.and.text.bubble.right", .speakingFirst),
            ("agent-ask-first", "Ask me first", "hand.raised", .askFirst),
            ("agent-when-away", "When you are not there", "moon", .whenAway),
        ]
        for (id, key, systemImage, part) in agentParts {
            add(id, key, systemImage, [.agentSettings(part)], inside: [agentSettings])
        }
        add("sources", "Sources", "tray.2", [.knowledgeSources])
        add("memory", "Memory", "brain", [.memory])
        add("dreams", "Dreams", "moon.zzz", [.dreams])
        add("schedules", "Schedules", "clock", [.schedules])
        add("alerts", "Alerts", "bell.badge", [.alerts])
        add("connections", "Connections", "point.3.connected.trianglepath.dotted", [.connections])
        add("activity", "Activity", "list.bullet.rectangle", [.activity])

        if let access, access.isOperator || access.canOpenMailDelivery || access.canManageServer || access.canOpenAgentAdmin {
            let manage = AppPlaceName("Manage")
            add("manage", "Manage", "server.rack", [.manage])
            func addManaged(_ id: String, _ key: String, _ systemImage: String, _ page: SettingsPage) {
                add(id, key, systemImage, [.manage, page], inside: [manage])
            }
            if access.canOpenMailDelivery, app.services?.isMailRoutingEnabled != false {
                if access.canAuditMail { addManaged("manage-mail", "Mail", "tray.full", .manageMail) }
                if access.canManageQueue { addManaged("manage-queue", "Queue", "clock.arrow.circlepath", .manageQueue) }
                if access.canReadReports { addManaged("manage-reports", "Reports", "checkmark.shield", .manageReports) }
                if access.canManageDomains { addManaged("manage-domains", "Domains", "globe", .manageDomains) }
            }
            if access.isOperator {
                addManaged("manage-people", "People", "person.2", .managePeople)
                addManaged("manage-groups", "Groups", "person.3", .manageGroups)
                addManaged("manage-roles", "Roles", "checkmark.seal", .manageRoles)
                addManaged("manage-audit-log", "Audit log", "list.bullet.clipboard", .manageAuditLog)
            }
            if access.canManageServer {
                addManaged("manage-services", "Services", "switch.2", .manageServices)
                addManaged("manage-server", "Server", "server.rack", .manageServer)
            }
            if access.canOpenAgentAdmin { addManaged("manage-agents", "Agents", "person.2.wave.2", .manageAgents) }
        }

        if app.services?.isMailboxEnabled != false, let mail, mail.mailbox != nil {
            let mailbox = AppPlaceName("Mailbox")
            add("mailbox", "Mailbox", "tray", [.mailbox])
            add("mailbox-name", "Name and signature", "signature", [.mailbox, .mailboxNameAndSignature], inside: [mailbox])
            add("mailbox-auto-reply", "Auto-reply", "arrowshape.turn.up.left.2", [.mailbox, .mailboxAutoReply], inside: [mailbox])
            add("mailbox-folders", "Folders", "folder", [.mailbox, .mailboxFolders], inside: [mailbox])
            add("mailbox-rules", "Rules", "line.3.horizontal.decrease.circle", [.mailbox, .mailboxRules], inside: [mailbox])
            add("mailbox-programs", "Mail programs", "laptopcomputer.and.iphone", [.mailbox, .mailboxPrograms], inside: [mailbox])
        }
        return places
    }
}

extension AppModel {
    /// Opens a place the Search tab offered, as a link to it would.
    func openPlace(_ target: AppPlaceTarget) {
        switch target {
        case .section(let section):
            openedSection = section
        case .settings(let pages):
            openedSettingsPages = pages
        case .financePart(let part):
            openedFinancePart = part
            openedSection = .finance
        case .photoSection(let photoSection):
            openedPhotoSection = photoSection
        case .healthSettings:
            openedHealthLink = HealthLink(healthType: nil, isSettings: true)
            openedSection = .health
        case .location(let route):
            openedLocationRoute = route
            openedSection = .location
        case .passwords(let destination):
            openedPasswordsDestination = destination
            openedSection = .passwords
        case .mail(let place):
            openedMailPlace = place
            openedSection = .mail
        case .calendar(let mode):
            openedCalendarViewMode = mode
            openedSection = .calendar
        }
    }
}
