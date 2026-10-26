import Observation
import SwiftUI
import TeaNodeKit

// The dashboard's Sharing page: everything the person shares and everything
// shared with them, in one list, across calendar, location, Health, memory
// pages, photo albums, recordings, finance accounts, skills and passwords. A
// share is stopped with the kind's own operation, and one made to the person by
// name is removed with DeclineShare. docs/subsystems/circles.md ("Sharing, in
// one place") says what is listed and what is left out.

/// SharingDirectory is what the Sharing screen reads: the two lists, read
/// again after every change.
@MainActor
@Observable
final class SharingDirectory {
    let connection: Connection
    private(set) var list = SharingList(sharesMade: [], sharesReceived: [])
    /// Whether the lists have been read once.
    private(set) var isLoaded = false
    /// Why the first read did not get through, to be said with a way to try
    /// again, since an empty list would say nothing is shared.
    private(set) var loadProblemText: String?
    private(set) var isLoading = false
    var isBusy = false
    var doneText: String?
    var problemText: String?

    init(connection: Connection) {
        self.connection = connection
    }

    func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            list = try await connection.sharing()
            isLoaded = true
            loadProblemText = nil
        } catch {
            if isLoaded { problemText = error.loadProblemText } else { loadProblemText = error.loadProblemText }
        }
    }

    /// Runs a change, says it was done or why it was refused, and reads
    /// everything again either way.
    @discardableResult
    func act(saying said: String, _ work: () async throws -> Void) async -> Bool {
        isBusy = true
        defer { isBusy = false }
        do {
            try await work()
            doneText = said
            await load()
            return true
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
            await load()
            return false
        }
    }
}

/// SharingView is what the person shares, then what is shared with them. A
/// share they made is stopped by a swipe; one made to them by name is removed
/// the same way. One through a circle says in its row that it ends when they
/// leave the circle.
struct SharingView: View {
    @State private var directory: SharingDirectory
    @State private var stopping: SharingMade?
    @State private var removing: SharingReceived?

    init(connection: Connection) {
        _directory = State(initialValue: SharingDirectory(connection: connection))
    }

    var body: some View {
        List {
            if !directory.list.sharesMade.isEmpty {
                Section {
                    ForEach(directory.list.sharesMade) { share in
                        SharingMadeRow(share: share)
                            .confirmationDialog(
                                SharingWords.stopTitle, isPresented: Binding(get: { stopping?.id == share.id }, set: { if !$0 { stopping = nil } }),
                                titleVisibility: .visible, presenting: stopping
                            ) { share in
                                Button("Stop sharing", role: .destructive) { stop(share) }
                            } message: { share in
                                Text(verbatim: SharingWords.stopMessage(share))
                            }
                            .swipeActions { stopAction(share) }
                            .contextMenu { stopAction(share) }
                    }
                } header: {
                    Text(verbatim: SharingWords.madeTitle)
                }
            }
            if !directory.list.sharesReceived.isEmpty {
                Section {
                    ForEach(directory.list.sharesReceived) { share in
                        SharingReceivedRow(share: share)
                            .confirmationDialog(
                                SharingWords.removeTitle(share), isPresented: Binding(get: { removing?.id == share.id }, set: { if !$0 { removing = nil } }),
                                titleVisibility: .visible, presenting: removing
                            ) { share in
                                Button("Remove", role: .destructive) { remove(share) }
                            } message: { share in
                                Text(verbatim: SharingWords.removeMessage(share))
                            }
                            .swipeActions { removeAction(share) }
                            .contextMenu { removeAction(share) }
                    }
                } header: {
                    Text(verbatim: SharingWords.receivedTitle)
                } footer: {
                    Text("A share made to you by name can be removed here. One through a circle ends when you leave the circle.")
                }
            }
        }
        .navigationTitle("Sharing")
        .overlay {
            if let loadProblemText = directory.loadProblemText, !directory.isLoaded {
                LoadFailedView(problemText: loadProblemText) { await directory.load() }
            } else if !directory.isLoaded && directory.isLoading {
                ProgressView()
            } else if directory.isLoaded && directory.list.isEmpty {
                ContentUnavailableView(
                    "Nothing is shared yet", systemImage: "person.2",
                    description: Text("What you share with the people in your circles, and what they share with you, is listed here."))
            }
        }
        .disabled(directory.isBusy)
        .refreshable { await directory.load() }
        .task { await directory.load() }
        .toast(done: Bindable(directory).doneText, problem: Bindable(directory).problemText)
    }

    /// Stopping a share, offered for every kind but passwords, which are
    /// stopped item by item on the Passwords screen.
    @ViewBuilder
    private func stopAction(_ share: SharingMade) -> some View {
        if share.canStop {
            Button("Stop sharing", systemImage: "person.badge.minus", role: .destructive) { stopping = share }
                .tint(.red)
        }
    }

    /// Removing a share that was made to the person by name; none for one
    /// through a circle or for passwords.
    @ViewBuilder
    private func removeAction(_ share: SharingReceived) -> some View {
        if share.canDecline {
            Button("Remove", systemImage: "trash", role: .destructive) { removing = share }
                .tint(.red)
        }
    }

    private func stop(_ share: SharingMade) {
        Task {
            await directory.act(saying: SharingWords.stoppedText(share)) {
                try await directory.connection.stopSharing(share)
            }
        }
    }

    private func remove(_ share: SharingReceived) {
        Task {
            await directory.act(saying: SharingWords.removedText(share)) {
                try await directory.connection.declineShare(share)
            }
        }
    }
}

/// SharingMadeRow is a share the person made: what it is of, then who it is
/// shared with, at what level and since when.
private struct SharingMadeRow: View {
    let share: SharingMade

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: SharingWords.madeWhat(share))
            Text(verbatim: SharingWords.madeDetails(share)).font(.footnote).foregroundStyle(Color(.secondaryLabel))
        }
        .accessibilityElement(children: .combine)
    }
}

/// SharingReceivedRow is a share made to the person: what it is of, then who
/// shared it, how it reaches them, and when. A share they cannot remove says
/// why on a line of its own.
private struct SharingReceivedRow: View {
    let share: SharingReceived

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: SharingWords.what(
                share.knownKind, shareKind: share.shareKind, subjectName: share.subjectName, subjectMask: share.shownSubjectMask,
                passwordItemCount: share.passwordItemCount))
            Text(verbatim: SharingWords.receivedDetails(share)).font(.footnote).foregroundStyle(Color(.secondaryLabel))
            if let note = SharingWords.cannotRemoveNote(share) {
                Text(verbatim: note).font(.footnote).foregroundStyle(Color(.secondaryLabel))
            }
        }
        .accessibilityElement(children: .combine)
    }
}

// MARK: - Words

/// SharingWords writes what the Sharing screen says. The levels and the Health
/// categories are the words the kind's own screen uses.
enum SharingWords {
    static var madeTitle: String { String(localized: "sharing.byYou", defaultValue: "Shared by you") }
    static var receivedTitle: String { String(localized: "sharing.withYou", defaultValue: "Shared with you") }
    static var stopTitle: String { String(localized: "Stop sharing") }

    static func kindTitle(_ kind: SharingKind?, shareKind: String) -> String {
        switch kind {
        case .calendar: String(localized: "Calendar")
        case .location: String(localized: "Location")
        case .health: String(localized: "Health")
        case .memoryPage: String(localized: "sharing.kindMemoryPage", defaultValue: "Memory page")
        case .photoAlbum: String(localized: "sharing.kindPhotoAlbum", defaultValue: "Photo album")
        case .recording: String(localized: "sharing.kindRecording", defaultValue: "Recording")
        case .finance: String(localized: "sharing.kindFinance", defaultValue: "Finance account")
        case .skill: String(localized: "Skill")
        case .passwords: String(localized: "Passwords")
        case .shoppingList: String(localized: "sharing.kindShoppingList", defaultValue: "Shopping list")
        case nil: shareKind
        }
    }

    static func passwordItems(_ count: Int) -> String {
        count == 1 ? String(localized: "1 item") : String(localized: "\(count) items")
    }

    /// What a share is of: the thing's name (a finance account with the last
    /// digits of its number), "3 items" for passwords, and the kind for
    /// location and Health, which are shared whole.
    static func what(_ kind: SharingKind?, shareKind: String, subjectName: String, subjectMask: String, passwordItemCount: Int) -> String {
        if kind == .passwords { return passwordItems(passwordItemCount) }
        if subjectName.isEmpty { return kindTitle(kind, shareKind: shareKind) }
        if kind == .finance && !subjectMask.isEmpty { return "\(subjectName) ··\(subjectMask)" }
        return subjectName
    }

    /// What a share the person made is of: as `what`, and "Page deleted" for a
    /// memory page that is gone, which is listed so that its share can be stopped.
    static func madeWhat(_ share: SharingMade) -> String {
        if share.isPageGone { return String(localized: "sharing.pageDeleted", defaultValue: "Page deleted") }
        return what(
            share.knownKind, shareKind: share.shareKind, subjectName: share.subjectName, subjectMask: share.shownSubjectMask,
            passwordItemCount: share.passwordItemCount)
    }

    /// The level of a calendar, location, album, recording or finance share in
    /// the words of its own screen; a level the app does not know is shown as the server wrote it.
    static func level(_ kind: SharingKind?, shareLevel: String, isReceived: Bool) -> String {
        switch kind {
        case .calendar: CalendarShareLevel(rawValue: shareLevel)?.title ?? shareLevel
        case .location: locationLevel(shareLevel, isReceived: isReceived)
        case .photoAlbum: PhotoShareLevel(rawValue: shareLevel).map { PhotoWords.title(of: $0) } ?? shareLevel
        case .recording: RecordingShareLevel(rawValue: shareLevel)?.title ?? shareLevel
        case .finance: FinanceShareWords.name(ofLevel: shareLevel)
        default: shareLevel
        }
    }

    /// A location level: the person's own location when they made the share
    /// ("Where I am now", as the Location screen words it), and the other
    /// person's when it was made to them.
    private static func locationLevel(_ shareLevel: String, isReceived: Bool) -> String {
        if !isReceived { return LocationShareLevel(rawValue: shareLevel)?.title ?? shareLevel }
        switch shareLevel {
        case "current": return String(localized: "sharing.levelCurrentTheirs", defaultValue: "Where they are now")
        case "history": return String(localized: "sharing.levelHistoryTheirs", defaultValue: "Where they are and where they have been")
        default: return shareLevel
        }
    }

    /// What else a share gives, worded by the kind it belongs to: the Health
    /// categories, the quotes of a page, the places and face names of an album,
    /// and a skill's tools as the skill names them (a tool may be called "quotes").
    static func scopes(_ kind: SharingKind?, shareScopes: [String]) -> [String] {
        shareScopes.map { (scope: String) -> String in
            switch kind {
            case .health:
                if let category = HealthCategory(rawValue: scope) { return HealthWords.categoryName(category) }
                return scope
            case .memoryPage:
                if scope == "quotes" { return String(localized: "sharing.scopeQuotes", defaultValue: "With quotes") }
                return scope
            case .photoAlbum:
                if scope == "location" { return String(localized: "sharing.scopePlaces", defaultValue: "With places") }
                if scope == "faceNames" { return String(localized: "sharing.scopeFaceNames", defaultValue: "With face names") }
                return scope
            default:
                return scope
            }
        }
    }

    /// The level and what else it gives, in one phrase; empty when there is neither.
    private static func levelAndScopes(_ kind: SharingKind?, shareLevel: String, shareScopes: [String], isReceived: Bool) -> String {
        var parts: [String] = []
        if !shareLevel.isEmpty { parts.append(level(kind, shareLevel: shareLevel, isReceived: isReceived)) }
        parts.append(contentsOf: scopes(kind, shareScopes: shareScopes))
        return parts.joined(separator: ", ")
    }

    private static func sinceText(_ since: Date?) -> String? {
        guard let since else { return nil }
        return String(localized: "Since \(since.formatted(.dateTime.year().month().day()))")
    }

    /// The line under a share the person made: its kind when the title is not
    /// it already, who it is shared with, the level, and since when.
    static func madeDetails(_ share: SharingMade) -> String {
        var parts: [String] = []
        if !share.subjectName.isEmpty || share.knownKind == .passwords {
            parts.append(kindTitle(share.knownKind, shareKind: share.shareKind))
        }
        parts.append(
            share.isCircle ? String(localized: "With the circle \(share.shownGrantee)") : String(localized: "With \(share.shownGrantee)"))
        let words = levelAndScopes(share.knownKind, shareLevel: share.shareLevel, shareScopes: share.shareScopes, isReceived: false)
        if !words.isEmpty { parts.append(words) }
        if let since = sinceText(share.since) { parts.append(since) }
        return parts.joined(separator: " · ")
    }

    /// The line under a share made to the person: its kind when the title is
    /// not it already, who shared it, how it reaches them, the level, and since
    /// when.
    static func receivedDetails(_ share: SharingReceived) -> String {
        var parts: [String] = []
        if !share.subjectName.isEmpty || share.knownKind == .passwords {
            parts.append(kindTitle(share.knownKind, shareKind: share.shareKind))
        }
        parts.append(String(localized: "From \(share.shownOwner)"))
        parts.append(share.isDirect ? String(localized: "By name") : String(localized: "Through the circle \(share.granteeCircleName)"))
        let words = levelAndScopes(share.knownKind, shareLevel: share.shareLevel, shareScopes: share.shareScopes, isReceived: true)
        if !words.isEmpty { parts.append(words) }
        if let since = sinceText(share.since) { parts.append(since) }
        return parts.joined(separator: " · ")
    }

    /// Why a share made to the person has no Remove: it ends with the circle,
    /// or its owner removes it. Nil for one that can be removed.
    static func cannotRemoveNote(_ share: SharingReceived) -> String? {
        if share.canDecline { return nil }
        return share.isDirect
            ? String(localized: "Removed by the person who shared it") : String(localized: "Ends when you leave the circle")
    }

    static func stopMessage(_ share: SharingMade) -> String {
        let thing = share.isPageGone ? String(localized: "sharing.thisPage", defaultValue: "this page") : madeWhat(share)
        return String(localized: "\(share.shownGrantee) will no longer see \(thing).")
    }

    static func stoppedText(_ share: SharingMade) -> String {
        String(localized: "Stopped sharing with \(share.shownGrantee)")
    }

    static func removeTitle(_ share: SharingReceived) -> String {
        String(localized: "Remove what \(share.shownOwner) shared?")
    }

    static func removeMessage(_ share: SharingReceived) -> String {
        let thing = what(
            share.knownKind, shareKind: share.shareKind, subjectName: share.subjectName, subjectMask: share.shownSubjectMask,
            passwordItemCount: share.passwordItemCount)
        return String(
            localized: "You will no longer receive \(thing) from \(share.shownOwner). \(share.shownOwner) is not told, and has to share it again for you to have it back.")
    }

    static func removedText(_ share: SharingReceived) -> String {
        String(localized: "Removed what \(share.shownOwner) shared")
    }
}

// MARK: - The notes

/// ShareReceivedNote is the detail of a share_received note: who shared what
/// with the person.
private struct ShareReceivedNote: Decodable {
    var sharerName: String = ""
    var shareKind: String = ""
    var shareLevel: String?
    var shareSubject: String?
}

/// ShareReachedNote is the detail of a share_reached note: who joined which
/// circle, and the kinds of what the person shares with it.
private struct ShareReachedNote: Decodable {
    var memberName: String = ""
    var circleName: String = ""
    var shareKinds: [String] = []
}

extension SharingWords {
    /// A level as a note says it, to the person it was shared with: "titles",
    /// "where they are now".
    private static func noteLevel(_ shareLevel: String) -> String {
        switch shareLevel {
        case "busy": String(localized: "sharing.noteLevelBusy", defaultValue: "busy times only")
        case "titles": String(localized: "sharing.noteLevelTitles", defaultValue: "titles")
        case "details": String(localized: "sharing.noteLevelDetails", defaultValue: "details")
        case "edit": String(localized: "sharing.noteLevelEdit", defaultValue: "you can edit it")
        case "current": String(localized: "sharing.noteLevelCurrent", defaultValue: "where they are now")
        case "history": String(localized: "sharing.noteLevelHistory", defaultValue: "where they are and where they have been")
        case "view": String(localized: "sharing.noteLevelView", defaultValue: "you can view it")
        case "contribute": String(localized: "sharing.noteLevelContribute", defaultValue: "you can add photos")
        case "summary": String(localized: "sharing.noteLevelSummary", defaultValue: "title and summary")
        case "transcript": String(localized: "sharing.noteLevelTranscript", defaultValue: "summary and transcript")
        case "balance": String(localized: "sharing.noteLevelBalance", defaultValue: "balance only")
        case "spending": String(localized: "sharing.noteLevelSpending", defaultValue: "balance and spending")
        case "transactions": String(localized: "sharing.noteLevelTransactions", defaultValue: "balance, spending and transactions")
        default: shareLevel
        }
    }

    /// The kind as a note lists it after "your": "calendar", "memory pages".
    private static func noteKind(_ shareKind: String) -> String {
        switch SharingKind(rawValue: shareKind) {
        case .calendar: String(localized: "sharing.noteKindCalendar", defaultValue: "calendar")
        case .location: String(localized: "sharing.noteKindLocation", defaultValue: "location")
        case .health: String(localized: "sharing.noteKindHealth", defaultValue: "Health")
        case .memoryPage: String(localized: "sharing.noteKindMemoryPages", defaultValue: "memory pages")
        case .photoAlbum: String(localized: "sharing.noteKindPhotoAlbums", defaultValue: "photo albums")
        case .recording: String(localized: "sharing.noteKindRecordings", defaultValue: "recordings")
        case .finance: String(localized: "sharing.noteKindFinance", defaultValue: "finance")
        case .skill: String(localized: "sharing.noteKindSkills", defaultValue: "skills")
        case .passwords: String(localized: "sharing.noteKindPasswords", defaultValue: "passwords")
        case .shoppingList: String(localized: "sharing.noteKindShoppingLists", defaultValue: "shopping lists")
        case nil: shareKind
        }
    }

    /// The note that somebody shared something with the person, or nil for a
    /// detail that is not one.
    static func shareReceivedNote(_ detail: String) -> String? {
        guard let data = detail.data(using: .utf8), let note = try? JSONDecoder().decode(ShareReceivedNote.self, from: data),
            !note.sharerName.isEmpty
        else { return nil }
        let level = noteLevel(note.shareLevel ?? "")
        let subject = note.shareSubject ?? ""
        switch SharingKind(rawValue: note.shareKind) {
        case .calendar: return String(localized: "\(note.sharerName) shared their calendar with you (\(level)).")
        case .location: return String(localized: "\(note.sharerName) shared their location with you (\(level)).")
        case .health: return String(localized: "\(note.sharerName) shared their Health with you.")
        case .memoryPage: return String(localized: "\(note.sharerName) shared the memory page \(subject) with you.")
        case .skill: return String(localized: "\(note.sharerName) shared their \(subject) skill with you.")
        case .photoAlbum: return String(localized: "\(note.sharerName) shared the photo album \(subject) with you (\(level)).")
        case .recording:
            if subject.isEmpty { return String(localized: "\(note.sharerName) shared a recording with you (\(level)).") }
            return String(localized: "\(note.sharerName) shared the recording \(subject) with you (\(level)).")
        case .finance: return String(localized: "\(note.sharerName) shared a finance account with you (\(level)).")
        case .shoppingList: return String(localized: "\(note.sharerName) shared the shopping list \(subject) with you.")
        case .passwords, nil: return nil
        }
    }

    /// The note that somebody joined a circle the person shares things with,
    /// or nil for a detail that is not one.
    static func shareReachedNote(_ detail: String) -> String? {
        guard let data = detail.data(using: .utf8), let note = try? JSONDecoder().decode(ShareReachedNote.self, from: data),
            !note.memberName.isEmpty, !note.circleName.isEmpty, !note.shareKinds.isEmpty
        else { return nil }
        let kinds = noteKindList(note.shareKinds.map { noteKind($0) })
        return String(localized: "\(note.memberName) joined \(note.circleName) and can now see your \(kinds).")
    }

    /// The kinds as a sentence lists them, "calendar, location and finance":
    /// the system's list style would put a comma before the "and" in English,
    /// which the dashboard and the server's own note do not.
    private static func noteKindList(_ names: [String]) -> String {
        guard let last = names.last, names.count > 1 else { return names.first ?? "" }
        let front = names.dropLast().joined(separator: String(localized: "sharing.noteListSeparator", defaultValue: ", "))
        return String(localized: "\(front) and \(last)")
    }
}
