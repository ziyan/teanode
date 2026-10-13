import Foundation

/// DeviceSetupMailbox is an owned mailbox available for an installation profile.
public struct DeviceSetupMailbox: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public let addresses: [Mailbox.Address]
}

/// DeviceSetupCapabilities contains only services the current account can configure.
public struct DeviceSetupCapabilities: Codable, Sendable {
    public let canSetUpMail: Bool
    public let canSetUpCalendar: Bool
    public let canSetUpReminders: Bool
    public let canSetUpContacts: Bool
    public let mailboxes: [DeviceSetupMailbox]
    public var hasAvailableService: Bool { canSetUpMail || canSetUpCalendar || canSetUpReminders || canSetUpContacts }
}

/// DeviceSetupSelection retains the exact services the person selected.
public struct DeviceSetupSelection: Sendable {
    public var deviceName: String
    public var mailboxId: String?
    public var hasMailAccess: Bool
    public var hasCalendarAccess: Bool
    public var hasRemindersAccess: Bool
    public var hasContactsAccess: Bool

    public init(deviceName: String, capabilities: DeviceSetupCapabilities, preselectedMailboxId: String? = nil) {
        self.deviceName = deviceName
        mailboxId = capabilities.mailboxes.first { $0.id == preselectedMailboxId }?.id
        hasMailAccess = capabilities.canSetUpMail && mailboxId != nil
        hasCalendarAccess = capabilities.canSetUpCalendar
        hasRemindersAccess = capabilities.canSetUpReminders
        hasContactsAccess = capabilities.canSetUpContacts
    }

    public func retainingAvailableServices(capabilities: DeviceSetupCapabilities) -> Self {
        var retained = self
        retained.hasCalendarAccess = hasCalendarAccess && capabilities.canSetUpCalendar
        retained.hasRemindersAccess = hasRemindersAccess && capabilities.canSetUpReminders
        retained.hasContactsAccess = hasContactsAccess && capabilities.canSetUpContacts
        if !capabilities.canSetUpMail || !capabilities.mailboxes.contains(where: { $0.id == mailboxId }) {
            retained.mailboxId = nil
            retained.hasMailAccess = false
        }
        return retained
    }

    public func canCreateProfile(capabilities: DeviceSetupCapabilities) -> Bool {
        let trimmedName = deviceName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedName.isEmpty, trimmedName.utf8.count <= 128,
            hasMailAccess || hasCalendarAccess || hasRemindersAccess || hasContactsAccess,
            !hasCalendarAccess || capabilities.canSetUpCalendar,
            !hasRemindersAccess || capabilities.canSetUpReminders,
            !hasContactsAccess || capabilities.canSetUpContacts
        else { return false }
        return !hasMailAccess || (capabilities.canSetUpMail && capabilities.mailboxes.contains { $0.id == mailboxId })
    }

    var variables: [String: JSONValue] {
        [
            "deviceName": .string(deviceName.trimmingCharacters(in: .whitespacesAndNewlines)),
            "mailboxId": hasMailAccess ? mailboxId.map(JSONValue.string) ?? .null : .null,
            "hasMailAccess": .bool(hasMailAccess), "hasCalendarAccess": .bool(hasCalendarAccess),
            "hasRemindersAccess": .bool(hasRemindersAccess), "hasContactsAccess": .bool(hasContactsAccess),
            "isDownloadOnly": .bool(true),
        ]
    }
}

/// CreatedDeviceSetupProfile keeps mail and collection revocation metadata separate.
public struct CreatedDeviceSetupProfile: Codable, Sendable {
    public let downloadPath: String
    public let downloadLink: String
    public let expiresAt: String
    public let mailboxAppPassword: MailAppPassword?
    public let syncCredential: SyncCredential?

    public func hasExpired(at currentDate: Date = Date()) -> Bool {
        guard let expiration = ServerTime.date(expiresAt) else { return true }
        return expiration <= currentDate
    }

    public func downloadURL(server: ServerAddress) throws -> URL {
        let downloadURL = try validatedDownloadURL(server: server)
        guard !hasExpired() else { throw TeaNodeError.unreadableAnswer }
        return downloadURL
    }

    fileprivate func validatedDownloadURL(server: ServerAddress) throws -> URL {
        guard let downloadURL = URL(string: downloadLink), downloadURL.user == nil, downloadURL.password == nil,
            downloadURL.scheme == server.origin.scheme, downloadURL.host == server.origin.host,
            (downloadURL.port ?? (downloadURL.scheme == "https" ? 443 : 80)) == (server.origin.port ?? (server.origin.scheme == "https" ? 443 : 80)),
            downloadURL.path == downloadPath,
            downloadURL.query == nil, downloadURL.fragment == nil,
            downloadPath.hasPrefix("/api/v1/mailbox/profile/"), downloadPath.hasSuffix(".mobileconfig"),
            ServerTime.date(expiresAt) != nil
        else { throw TeaNodeError.unreadableAnswer }
        return downloadURL
    }
}

extension Documents {
    public static let deviceSetupCapabilities = """
        query { GetDeviceSetupCapabilities {
          canSetUpMail canSetUpCalendar canSetUpReminders canSetUpContacts
          mailboxes { id name addresses { address domain domainId } }
        } }
        """
    public static let createDeviceSetupProfile = """
        mutation ($deviceName: String!, $mailboxId: String, $hasMailAccess: Boolean!, $hasCalendarAccess: Boolean!, $hasRemindersAccess: Boolean!, $hasContactsAccess: Boolean!, $isDownloadOnly: Boolean) {
          CreateDeviceSetupProfile(deviceName: $deviceName, mailboxId: $mailboxId, hasMailAccess: $hasMailAccess, hasCalendarAccess: $hasCalendarAccess, hasRemindersAccess: $hasRemindersAccess, hasContactsAccess: $hasContactsAccess, isDownloadOnly: $isDownloadOnly) {
            downloadPath downloadLink expiresAt
            mailboxAppPassword { id mailboxId name createdAt lastUsedAt }
            syncCredential { id credentialName hasCalendarAccess hasRemindersAccess hasContactsAccess }
          }
        }
        """
}

extension Connection {
    public func deviceSetupCapabilities() async throws -> DeviceSetupCapabilities {
        struct Answer: Decodable, Sendable { let GetDeviceSetupCapabilities: DeviceSetupCapabilities }
        return try await execute(Documents.deviceSetupCapabilities, as: Answer.self).GetDeviceSetupCapabilities
    }

    public func createDeviceSetupProfile(_ selection: DeviceSetupSelection, capabilities: DeviceSetupCapabilities) async throws
        -> CreatedDeviceSetupProfile
    {
        guard selection.canCreateProfile(capabilities: capabilities) else { throw TeaNodeError.unreadableAnswer }
        struct Answer: Decodable, Sendable { let CreateDeviceSetupProfile: CreatedDeviceSetupProfile }
        let profile = try await execute(Documents.createDeviceSetupProfile, variables: selection.variables, as: Answer.self).CreateDeviceSetupProfile
        _ = try profile.validatedDownloadURL(server: account.server)
        guard (profile.mailboxAppPassword != nil) == selection.hasMailAccess,
            (profile.syncCredential != nil) == (selection.hasCalendarAccess || selection.hasRemindersAccess || selection.hasContactsAccess)
        else { throw TeaNodeError.unreadableAnswer }
        if selection.hasMailAccess, profile.mailboxAppPassword?.mailboxId != selection.mailboxId { throw TeaNodeError.unreadableAnswer }
        if let sync = profile.syncCredential {
            guard sync.hasCalendarAccess == selection.hasCalendarAccess,
                sync.hasRemindersAccess == selection.hasRemindersAccess,
                sync.hasContactsAccess == selection.hasContactsAccess
            else { throw TeaNodeError.unreadableAnswer }
        }
        return profile
    }
}
