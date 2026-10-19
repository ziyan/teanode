import Foundation
import Observation
import TeaNodeKit

/// HealthSyncSnapshot is what the Health settings say about the copying to
/// the server, read from HealthSync in one place (which shows invented
/// numbers under -TeaNodeDemo, so the screenshots need nothing here).
struct HealthSyncSnapshot {
    var isOn: Bool
    var isAvailable: Bool
    var isSyncing: Bool
    var lastSentAt: Date?
    var sentSampleCount: Int
    var pendingDayCount: Int
    var isHistoryDone: Bool
    var problemText: String?
    var passProgress: HealthPassProgress?
    var isStoppedByErase: Bool
    var droppedEntryCount: Int

    @MainActor
    static func current() -> HealthSyncSnapshot {
        let sync = HealthSync.shared
        return HealthSyncSnapshot(
            isOn: sync.isOn, isAvailable: sync.isAvailable, isSyncing: sync.isSyncing, lastSentAt: sync.lastSentAt,
            sentSampleCount: sync.sentSampleCount, pendingDayCount: sync.pendingDayCount, isHistoryDone: sync.isHistoryDone,
            problemText: sync.problemText, passProgress: sync.passProgress, isStoppedByErase: sync.isStoppedByErase,
            droppedEntryCount: sync.droppedEntryCount)
    }
}

/// HealthAboutRow is one fact the Health app holds about the person, as the
/// settings show it.
struct HealthAboutRow: Identifiable {
    let healthType: String
    let labelText: String
    let valueText: String
    var id: String { healthType }
}

/// HealthSettingsModel is the Health settings: whether the agent may read
/// Health data, who it is shared with, and erasing it.
@MainActor
@Observable
final class HealthSettingsModel {
    let connection: Connection

    private(set) var settings: HealthSettingsView?
    private(set) var shares: [HealthShareView] = []
    private(set) var ownUserId: String?
    private(set) var hasLoaded = false
    /// Whether the shares were read, so that "no shares" is only said when
    /// that is known.
    private(set) var hasReadShares = false
    private(set) var loadFailedText: String?
    private(set) var isBusy = false
    var problemText: String?
    var doneText: String?

    init(connection: Connection) {
        self.connection = connection
    }

    /// The person's own shares: the people and circles they share Health with.
    var ownShares: [HealthShareView] {
        // Told apart by who the person is, so none until that is known.
        guard let ownUserId else { return [] }
        return shares.filter { $0.ownerUserId == ownUserId }
    }

    /// Whether the person's shares are known: who they are, and the list.
    var isOwnSharesKnown: Bool { hasReadShares && ownUserId != nil }

    // MARK: About the person

    /// The facts the Health app holds about the person, read only, in the
    /// words of the table the dashboard follows too. Only the ones that were
    /// copied are listed.
    var aboutRows: [HealthAboutRow] {
        let characteristics = settings?.characteristics ?? []
        var rows: [HealthAboutRow] = []
        for healthType in Self.aboutHealthTypes {
            guard let found = characteristics.first(where: { $0.healthType == healthType }) else { continue }
            rows.append(
                HealthAboutRow(
                    healthType: healthType, labelText: HealthWords.typeName(healthType: healthType, serverName: HealthTypes.typeName(for: healthType)),
                    valueText: HealthWords.characteristicText(healthType: healthType, characteristicValue: found.characteristicValue)))
        }
        return rows
    }

    private static let aboutHealthTypes = [
        "dateOfBirth", "biologicalSex", "bloodType", "fitzpatrickSkinType", "wheelchairUse", "activityMoveMode",
    ]

    // MARK: Reading

    func load() async {
        if ownUserId == nil, let session = try? await connection.accessSession() { ownUserId = session.userId }
        do {
            settings = try await connection.healthSettings()
            loadFailedText = nil
        } catch {
            if settings == nil { loadFailedText = error.loadProblemText } else { problemText = error.loadProblemText }
        }
        if let readShares = try? await connection.healthShares() {
            shares = readShares
            hasReadShares = true
        }
        hasLoaded = true
    }

    // MARK: The agent

    /// Lets the agent read Health data, or stops it.
    func setAgentCanRead(_ canAgentReadHealth: Bool) async {
        isBusy = true
        defer { isBusy = false }
        do {
            settings = try await connection.updateHealthSettings(canAgentReadHealth: canAgentReadHealth)
            if canAgentReadHealth {
                doneText = String(localized: "The agent can read your Health data now.")
            } else {
                doneText = String(localized: "The agent can no longer read your Health data.")
            }
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "Could not save the setting.")).asReasonSentence
        }
    }

    // MARK: Sharing

    /// Shares the categories with a person or a circle; an empty list takes
    /// the share back. Nil when it worked, else why it did not.
    func setShare(with target: ShareTarget, name: String, categories: [HealthCategory]) async -> String? {
        isBusy = true
        defer { isBusy = false }
        do {
            try await connection.setHealthShare(
                granteeUserId: target.granteeUserId, granteeCircleId: target.granteeCircleId, healthCategories: categories)
            if categories.isEmpty {
                doneText = String(localized: "Stopped sharing with \(name).")
            } else {
                doneText = String(localized: "Sharing with \(name) saved.")
            }
            if let readShares = try? await connection.healthShares() { shares = readShares }
            return nil
        } catch {
            let refusalText = error.refusalReason(orSay: String(localized: "Could not save the sharing.")).asReasonSentence
            problemText = refusalText
            return refusalText
        }
    }

    // MARK: Erasing

    /// Erases everything Health holds on the server, which also makes the
    /// phone stop sending and forget where it was (HealthSync does both, so
    /// that nothing is sent between the two).
    func eraseEverything() async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await HealthSync.shared.eraseEverything()
            doneText = String(localized: "Your Health data was erased.")
            await load()
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "Could not erase the Health data.")).asReasonSentence
        }
    }
}
