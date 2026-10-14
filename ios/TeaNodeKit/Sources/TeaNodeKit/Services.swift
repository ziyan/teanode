import Foundation

/// ServiceCapabilities is the hosted functionality running on one server.
public struct ServiceCapabilities: Codable, Sendable, Equatable {
    public let isMailRoutingEnabled: Bool
    public let isMailboxEnabled: Bool
    public let isCalendarEnabled: Bool
    public let isRemindersEnabled: Bool
    public let isContactsEnabled: Bool
    public let isAssistantEnabled: Bool
    public let hasPendingServiceRestart: Bool
    public let hasCompletedSetup: Bool

    public static let legacy = ServiceCapabilities(isMailRoutingEnabled: true, isMailboxEnabled: true, isCalendarEnabled: true, isRemindersEnabled: true, isContactsEnabled: true, isAssistantEnabled: true, hasPendingServiceRestart: false, hasCompletedSetup: true)
    public static let unavailable = ServiceCapabilities(isMailRoutingEnabled: false, isMailboxEnabled: false, isCalendarEnabled: false, isRemindersEnabled: false, isContactsEnabled: false, isAssistantEnabled: false, hasPendingServiceRestart: false, hasCompletedSetup: false)
}

extension Documents {
    public static let serviceCapabilities = """
        query { GetServiceCapabilities { isMailRoutingEnabled isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled isAssistantEnabled hasPendingServiceRestart hasCompletedSetup } }
        """
}

extension Connection {
    public func serviceCapabilities() async throws -> ServiceCapabilities {
        struct Answer: Decodable, Sendable { let GetServiceCapabilities: ServiceCapabilities }
        do { return try await execute(Documents.serviceCapabilities, as: Answer.self).GetServiceCapabilities }
        catch TeaNodeError.refused(let errorMessage) where errorMessage.contains("Cannot query field") && errorMessage.contains("GetServiceCapabilities") { return .legacy }
    }
}

public struct ServiceSelection: Codable, Sendable {
    public var isMailRoutingEnabled: Bool
    public var isMailboxEnabled: Bool
    public var isCalendarEnabled: Bool
    public var isRemindersEnabled: Bool
    public var isContactsEnabled: Bool
    public init(capabilities: ServiceCapabilities) {
        isMailRoutingEnabled = capabilities.isMailRoutingEnabled
        isMailboxEnabled = capabilities.isMailboxEnabled
        isCalendarEnabled = capabilities.isCalendarEnabled
        isRemindersEnabled = capabilities.isRemindersEnabled
        isContactsEnabled = capabilities.isContactsEnabled
    }
}

public struct SyncCredential: Codable, Sendable, Identifiable {
    public let id: String
    public let credentialName: String
    public let hasCalendarAccess: Bool
    public let hasRemindersAccess: Bool
    public let hasContactsAccess: Bool
    /// When the password was made. Absent only in an answer that predates it.
    public let createdAt: String?
    /// When the password last signed anything in; null until it first does.
    public let lastUsedAt: String?
}

public struct CreatedSyncCredential: Codable, Sendable {
    public let username: String
    public let password: String
    public let principalPath: String
}

public struct CreatedSyncProfile: Codable, Sendable {
    public let downloadLink: String
    public let expiresAt: Date
}

extension Documents {
    public static let serviceSetup = """
        query { GetServiceSetup { selectedServices { isMailRoutingEnabled isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled } } }
        """
    public static let updateServices = """
        mutation ($isMailRoutingEnabled: Boolean, $isMailboxEnabled: Boolean, $isCalendarEnabled: Boolean, $isRemindersEnabled: Boolean, $isContactsEnabled: Boolean) {
          UpdateServices(isMailRoutingEnabled: $isMailRoutingEnabled, isMailboxEnabled: $isMailboxEnabled, isCalendarEnabled: $isCalendarEnabled, isRemindersEnabled: $isRemindersEnabled, isContactsEnabled: $isContactsEnabled) { isMailRoutingEnabled isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled isAssistantEnabled hasPendingServiceRestart hasCompletedSetup }
        }
        """
    public static let syncCredentials = """
        query { ListSyncCredentials { id credentialName hasCalendarAccess hasRemindersAccess hasContactsAccess createdAt lastUsedAt } }
        """
    public static let createSyncCredential = """
        mutation ($credentialName: String!, $hasCalendarAccess: Boolean!, $hasRemindersAccess: Boolean!, $hasContactsAccess: Boolean!) {
          CreateSyncCredential(credentialName: $credentialName, hasCalendarAccess: $hasCalendarAccess, hasRemindersAccess: $hasRemindersAccess, hasContactsAccess: $hasContactsAccess) { username password principalPath }
        }
        """
    public static let revokeSyncCredential = """
        mutation ($credentialId: String!) { RevokeSyncCredential(credentialId: $credentialId) }
        """
    public static let createSyncSetupProfile = """
        mutation ($credentialName: String!, $hasCalendarAccess: Boolean!, $hasRemindersAccess: Boolean!, $hasContactsAccess: Boolean!) {
          CreateSyncSetupProfile(credentialName: $credentialName, hasCalendarAccess: $hasCalendarAccess, hasRemindersAccess: $hasRemindersAccess, hasContactsAccess: $hasContactsAccess) { downloadLink expiresAt }
        }
        """
}

extension Connection {
    public func serviceSelection() async throws -> ServiceSelection {
        struct Answer: Decodable, Sendable {
            struct Setup: Decodable, Sendable { let selectedServices: ServiceSelection }
            let GetServiceSetup: Setup
        }
        return try await execute(Documents.serviceSetup, as: Answer.self).GetServiceSetup.selectedServices
    }
    public func updateServices(_ selection: ServiceSelection) async throws {
        struct Answer: Decodable, Sendable { let UpdateServices: ServiceCapabilities }
        _ = try await execute(Documents.updateServices, variables: [
            "isMailRoutingEnabled": .bool(selection.isMailRoutingEnabled), "isMailboxEnabled": .bool(selection.isMailboxEnabled),
            "isCalendarEnabled": .bool(selection.isCalendarEnabled), "isRemindersEnabled": .bool(selection.isRemindersEnabled), "isContactsEnabled": .bool(selection.isContactsEnabled),
        ], as: Answer.self)
    }
    public func syncCredentials() async throws -> [SyncCredential] {
        struct Answer: Decodable, Sendable { let ListSyncCredentials: [SyncCredential] }
        return try await execute(Documents.syncCredentials, as: Answer.self).ListSyncCredentials
    }
    public func createSyncCredential(credentialName: String, hasCalendarAccess: Bool, hasRemindersAccess: Bool, hasContactsAccess: Bool) async throws -> CreatedSyncCredential {
        struct Answer: Decodable, Sendable { let CreateSyncCredential: CreatedSyncCredential }
        return try await execute(Documents.createSyncCredential, variables: [
            "credentialName": .string(credentialName), "hasCalendarAccess": .bool(hasCalendarAccess), "hasRemindersAccess": .bool(hasRemindersAccess), "hasContactsAccess": .bool(hasContactsAccess),
        ], as: Answer.self).CreateSyncCredential
    }
    public func revokeSyncCredential(_ credentialId: String) async throws {
        struct Answer: Decodable, Sendable { let RevokeSyncCredential: Bool }
        _ = try await execute(Documents.revokeSyncCredential, variables: ["credentialId": .string(credentialId)], as: Answer.self)
    }
    public func createSyncSetupProfile(credentialName: String, hasCalendarAccess: Bool, hasRemindersAccess: Bool, hasContactsAccess: Bool) async throws -> CreatedSyncProfile {
        struct Answer: Decodable, Sendable { let CreateSyncSetupProfile: CreatedSyncProfile }
        return try await execute(Documents.createSyncSetupProfile, variables: [
            "credentialName": .string(credentialName), "hasCalendarAccess": .bool(hasCalendarAccess), "hasRemindersAccess": .bool(hasRemindersAccess), "hasContactsAccess": .bool(hasContactsAccess),
        ], as: Answer.self).CreateSyncSetupProfile
    }
}
