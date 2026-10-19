import Foundation
import Observation
import TeaNodeKit

/// HealthSharer is somebody who shares Health data with the person.
struct HealthSharer: Identifiable, Hashable {
    let userId: String
    let displayName: String
    var id: String { userId }
}

/// HealthTypeGroup is the kinds of data of one Health category.
struct HealthTypeGroup: Identifiable {
    let healthCategory: HealthCategory
    let types: [HealthTypeView]
    var id: String { healthCategory.rawValue }
}

/// HealthModel is the Health screen: the kinds of data the person has, or a
/// person sharing with them has, grouped as the Health app groups them.
@MainActor
@Observable
final class HealthModel {
    let connection: Connection

    private(set) var types: [HealthTypeView] = []
    /// Everyone who shares Health with the person, and the person's own
    /// shares, as the server last said.
    private(set) var shares: [HealthShareView] = []
    private(set) var ownUserId: String?
    /// Whether the first read has finished, one way or the other.
    private(set) var hasLoaded = false
    /// How many reads are under way.
    private var activeLoadCount = 0
    /// Why the first read failed, when there is nothing to show instead.
    private(set) var loadFailedText: String?
    /// Whose data is shown: somebody sharing, or nobody for the person's own.
    private(set) var selectedUserId: String?
    var problemText: String?
    var doneText: String?

    init(connection: Connection) {
        self.connection = connection
    }

    var isLoading: Bool { activeLoadCount > 0 }

    /// Whether the person's own data is shown, which is the only data with
    /// readings and routes.
    var isViewingOwn: Bool { selectedUserId == nil }

    /// The people sharing Health with the person, once each however many
    /// shares reach them, by name.
    var sharers: [HealthSharer] {
        // The person's own shares are told apart by who they are, so none are
        // listed until that is known.
        guard let ownUserId else { return [] }
        var found: [String: HealthSharer] = [:]
        for share in shares where share.ownerUserId != ownUserId {
            if found[share.ownerUserId] == nil {
                let name = share.ownerName ?? ""
                found[share.ownerUserId] = HealthSharer(userId: share.ownerUserId, displayName: name.isEmpty ? share.ownerUsername : name)
            }
        }
        return found.values.sorted { $0.displayName.localizedStandardCompare($1.displayName) == .orderedAscending }
    }

    var selectedSharer: HealthSharer? { sharers.first { $0.userId == selectedUserId } }

    /// The kinds of data by Health category, in the Health app's order.
    var groups: [HealthTypeGroup] {
        HealthCategory.allCases.compactMap { healthCategory in
            let inCategory = types.filter { $0.healthCategoryValue == healthCategory }
            return inCategory.isEmpty ? nil : HealthTypeGroup(healthCategory: healthCategory, types: inCategory)
        }
    }

    /// The name a kind of data is called, in the person's language.
    func name(of type: HealthTypeView) -> String {
        HealthWords.typeName(healthType: type.healthType, serverName: type.typeName)
    }

    // MARK: Reading

    /// Reads who shares with the person and the kinds of data shown.
    func load() async {
        activeLoadCount += 1
        defer {
            activeLoadCount -= 1
            hasLoaded = true
        }
        if ownUserId == nil, let session = try? await connection.accessSession() { ownUserId = session.userId }
        if let readShares = try? await connection.healthShares() {
            shares = readShares
            // Somebody who stopped sharing is no longer chosen.
            if selectedUserId != nil, selectedSharer == nil {
                selectedUserId = nil
                problemText = String(localized: "That person does not share Health data with you.")
            }
        }
        let requestedUserId = selectedUserId
        do {
            let readTypes = try await connection.healthTypes(ownerUserId: requestedUserId)
            // The person may have chosen somebody else while this was read.
            guard requestedUserId == selectedUserId else { return }
            types = readTypes
            loadFailedText = nil
        } catch {
            guard requestedUserId == selectedUserId else { return }
            if types.isEmpty { loadFailedText = error.loadProblemText } else { problemText = error.loadProblemText }
        }
    }

    /// Shows somebody else's data, or the person's own for nil.
    func select(userId: String?) async {
        guard userId != selectedUserId else { return }
        selectedUserId = userId
        types = []
        loadFailedText = nil
        hasLoaded = false
        await load()
    }

    // MARK: Erasing

    /// Erases one kind of data from the server; whether it did.
    func erase(healthType: String) async -> Bool {
        do {
            let result = try await connection.eraseHealthData(healthType: healthType)
            doneText = HealthModel.erasedText(result.erasedSampleCount)
            await load()
            return true
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "Could not erase the Health data.")).asReasonSentence
            return false
        }
    }

    /// "12 readings erased."
    static func erasedText(_ erasedSampleCount: Int) -> String {
        String(localized: "\(erasedSampleCount) readings erased.")
    }
}
