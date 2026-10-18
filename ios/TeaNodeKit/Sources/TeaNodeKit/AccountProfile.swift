import Foundation

/// AccountProfile is the person's own account: what to call them, what they
/// sign in with, where the server writes to them, and their time zone.
public struct AccountProfile: Codable, Hashable, Sendable {
    public let id: String
    public let username: String
    public let name: String?
    public let email: String?
    public let timezone: String?
    /// auto or fixed; empty is auto.
    public let timezoneMode: String?
    /// Where the person's picture is, a path to read with the app's token;
    /// nil when they have none.
    public let avatarUrl: String?
    /// Which of the twelve colors their initial is drawn on without a picture.
    public let avatarColorIndex: Int?
    /// How the person says they are described: he, she or they; empty or
    /// absent until they say.
    public let pronoun: String?

    public init(
        id: String, username: String, name: String? = nil, email: String? = nil, timezone: String? = nil, timezoneMode: String? = nil,
        avatarUrl: String? = nil, avatarColorIndex: Int? = nil, pronoun: String? = nil
    ) {
        self.id = id
        self.username = username
        self.name = name
        self.email = email
        self.timezone = timezone
        self.timezoneMode = timezoneMode
        self.avatarUrl = avatarUrl
        self.avatarColorIndex = avatarColorIndex
        self.pronoun = pronoun
    }

    /// The pronoun as the server keeps it, where empty or unknown is not set.
    public var chosenPronoun: PersonPronoun {
        PersonPronoun(rawValue: pronoun ?? "") ?? .notSet
    }

    /// What the person is called on screen: their name, or the username when
    /// they have not given one.
    public var displayName: String {
        guard let name, !name.isEmpty else { return username }
        return name
    }

    /// TimezoneMode is whether the time zone follows the browser the
    /// dashboard is open in, or stays the one chosen.
    public enum TimezoneMode: String, CaseIterable, Hashable, Sendable {
        case auto
        case fixed
    }

    /// The mode as the server keeps it, where empty or unknown is auto.
    public var chosenTimezoneMode: TimezoneMode {
        TimezoneMode(rawValue: timezoneMode ?? "") ?? .auto
    }
}

/// ProfileDraft is the profile as its form holds it, and what saving it
/// sends.
public struct ProfileDraft: Hashable, Sendable {
    public let profile: AccountProfile
    public var name: String
    public var username: String
    public var email: String
    public var timezone: String
    public var timezoneMode: AccountProfile.TimezoneMode
    public var pronoun: PersonPronoun

    public init(_ profile: AccountProfile) {
        self.profile = profile
        name = profile.name ?? ""
        username = profile.username
        email = profile.email ?? ""
        timezone = profile.timezone ?? ""
        timezoneMode = profile.chosenTimezoneMode
        pronoun = profile.chosenPronoun
    }

    /// Whether saving changes what the person signs in with, which is asked
    /// about first.
    public var isRenaming: Bool { trimmedText(username) != profile.username }

    public var hasChanges: Bool {
        isRenaming || trimmedText(name) != (profile.name ?? "") || trimmedText(email) != (profile.email ?? "")
            || trimmedText(timezone) != (profile.timezone ?? "") || timezoneMode != profile.chosenTimezoneMode
            || pronoun != profile.chosenPronoun
    }

    public var canSave: Bool { hasChanges && !trimmedText(username).isEmpty }

    /// Chooses a mode. A fixed zone starts from the one the account has, or
    /// the phone's when it has none, rather than from nothing.
    public mutating func choose(_ mode: AccountProfile.TimezoneMode, deviceTimezone: String) {
        timezoneMode = mode
        if mode == .fixed, timezone.isEmpty {
            timezone = (profile.timezone ?? "").isEmpty ? deviceTimezone : profile.timezone ?? ""
        }
    }

    /// As the server takes it: trimmed, and no zone of its own when it
    /// follows the browser.
    var variables: [String: JSONValue] {
        [
            "userId": .string(profile.id), "name": .string(trimmedText(name)), "email": .string(trimmedText(email)),
            "username": .string(trimmedText(username)), "timezone": .string(timezoneMode == .fixed ? trimmedText(timezone) : ""),
            "timezoneMode": .string(timezoneMode.rawValue), "pronoun": .string(pronoun.rawValue),
        ]
    }
}

/// The words a person typed, without the spaces around them.
private func trimmedText(_ text: String) -> String { text.trimmingCharacters(in: .whitespacesAndNewlines) }

/// PasswordChangeDraft is the change-password form: the password in use and
/// the new one, typed twice.
public struct PasswordChangeDraft: Hashable, Sendable {
    public var currentPassword = ""
    public var newPassword = ""
    public var newPasswordAgain = ""

    public init() {}

    /// Whether the second typing differs from the first, once there is one.
    public var doesConfirmationDiffer: Bool { !newPasswordAgain.isEmpty && newPassword != newPasswordAgain }

    public var canChange: Bool { !currentPassword.isEmpty && !newPassword.isEmpty && newPassword == newPasswordAgain }
}

/// PairedPhone is one phone the app is connected on, as the Phones list
/// shows it.
public struct PairedPhone: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let phoneName: String
    public let phoneModel: String
    /// The model as people say it, such as "iPhone 17 Pro Max"; the
    /// identifier itself when the server does not know it. Absent from a
    /// server that predates it.
    public let phoneModelName: String?
    /// The kinds it is told about; the server lists every kind when it chose
    /// none.
    public let notificationKinds: [String]
    public let canBeNotified: Bool
    public let lastSeenAt: String?
    public let lastDeliveredAt: String?
    public let lastDeliveryError: String

    /// ModelDisplayName is what the Phones list shows for the model.
    public var modelDisplayName: String {
        if let phoneModelName, !phoneModelName.isEmpty { return phoneModelName }
        return phoneModel
    }

    /// NotificationState is whether notifications reach the phone.
    public enum NotificationState: Hashable, Sendable {
        /// It has not registered for notifications.
        case notRegistered
        /// The last notification sent to it failed.
        case lastFailed
        /// Notifications reach it.
        case notified
    }

    public var notificationState: NotificationState {
        if !canBeNotified { return .notRegistered }
        if !lastDeliveryError.isEmpty { return .lastFailed }
        return .notified
    }
}

/// PairedPhoneList is the person's phones, and what the server offers them.
public struct PairedPhoneList: Codable, Hashable, Sendable {
    public let phones: [PairedPhone]
    /// Whether this server sends notifications to phones at all.
    public let isPushEnabled: Bool
    /// Every kind a phone may choose, in order.
    public let notificationKinds: [String]
}

/// PairedPhoneDraft is one phone's name and notification kinds as its form
/// holds them.
public struct PairedPhoneDraft: Hashable, Sendable {
    public let phone: PairedPhone
    public var phoneName: String
    public var notificationKinds: Set<String>

    public init(_ phone: PairedPhone) {
        self.phone = phone
        phoneName = phone.phoneName
        notificationKinds = Set(phone.notificationKinds)
    }

    public var hasNameChanged: Bool { trimmedText(phoneName) != phone.phoneName }

    public var hasNotificationKindsChanged: Bool { notificationKinds != Set(phone.notificationKinds) }

    public var canSave: Bool { !trimmedText(phoneName).isEmpty && (hasNameChanged || hasNotificationKindsChanged) }

    /// The chosen kinds in the order the server lists every kind.
    public func orderedNotificationKinds(among allNotificationKinds: [String]) -> [String] {
        allNotificationKinds.filter { notificationKinds.contains($0) }
    }
}

/// Passkey is an authenticator that can sign in as the person.
public struct Passkey: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public let createdAt: String?
    public let usedAt: String?
    /// The address it was last used from.
    public let ip: String?
    public let backupEligible: Bool?
    /// Whether it is synced to the person's other devices, rather than kept
    /// on one.
    public let backupState: Bool?
    public let transports: [String]?

    public var createdDate: Date? { Self.date(createdAt) }

    /// When it last signed in; nil when it never has.
    public var lastUsedDate: Date? { Self.date(usedAt) }

    public var isSynced: Bool { backupState == true }

    /// The server writes a time never set as the first year, which is no
    /// time at all.
    static func date(_ text: String?) -> Date? {
        guard let date = ServerTime.date(text) else { return nil }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        return calendar.component(.year, from: date) > 1 ? date : nil
    }
}

/// PasskeyPolicy is whether this server offers passkeys, and how many one
/// account may have.
public struct PasskeyPolicy: Codable, Hashable, Sendable {
    public let enabled: Bool
    public let maximumPerUser: Int
}

extension Documents {
    /// What every read and change of the person's own account answers with.
    static let accountProfileFields = """
        id username name email avatarUrl avatarColorIndex timezone timezoneMode pronoun
        """
    public static let accountProfile = """
        query { GetCurrentUser { \(accountProfileFields) } }
        """
    public static let setAccountAvatar = """
        mutation ($content: String!) { SetUserAvatar(content: $content) { \(accountProfileFields) } }
        """
    public static let removeAccountAvatar = """
        mutation { RemoveUserAvatar { \(accountProfileFields) } }
        """
    public static let updateAccountProfile = """
        mutation ($userId: String!, $username: String, $name: String, $email: String, $timezone: String, $timezoneMode: String, $pronoun: String) {
          UpdateUser(
            userId: $userId, username: $username, name: $name, email: $email, timezone: $timezone, timezoneMode: $timezoneMode,
            pronoun: $pronoun
          ) {
            \(accountProfileFields)
          }
        }
        """
    public static let updateAccountPronoun = """
        mutation ($userId: String!, $pronoun: String) {
          UpdateUser(userId: $userId, pronoun: $pronoun) { \(accountProfileFields) }
        }
        """
    public static let changePassword = """
        mutation ($currentPassword: String!, $newPassword: String!) {
          ChangePassword(currentPassword: $currentPassword, newPassword: $newPassword) { authenticated username }
        }
        """
    public static let pairedPhones = """
        query {
          ListAgentPhones {
            phones { id phoneName phoneModel phoneModelName notificationKinds canBeNotified lastSeenAt lastDeliveredAt lastDeliveryError }
            isPushEnabled notificationKinds
          }
        }
        """
    public static let renamePhone = """
        mutation ($phoneId: String!, $phoneName: String) { UpdateAgentPhone(phoneId: $phoneId, phoneName: $phoneName) { id } }
        """
    public static let testPhone = """
        mutation ($phoneId: String!) { TestAgentPhone(phoneId: $phoneId) { id } }
        """
    public static let removePhone = """
        mutation ($phoneId: String!) { RemoveAgentPhone(phoneId: $phoneId) }
        """
    public static let passkeys = """
        query {
          GetPasskeyPolicy { enabled maximumPerUser }
          ListPasskeys { id name createdAt usedAt ip backupEligible backupState transports }
        }
        """
    public static let deletePasskey = """
        mutation ($passkeyId: String!) { DeletePasskey(passkeyId: $passkeyId) }
        """
}

extension Connection {
    /// The account the token signs in as; nil on a server with no accounts.
    public func accountProfile() async throws -> AccountProfile? {
        struct Answer: Decodable, Sendable { let GetCurrentUser: AccountProfile? }
        return try await execute(Documents.accountProfile, as: Answer.self).GetCurrentUser
    }

    /// Saves the profile as the draft has it, and returns it as the server
    /// now has it.
    public func updateAccountProfile(_ draft: ProfileDraft) async throws -> AccountProfile {
        struct Answer: Decodable, Sendable { let UpdateUser: AccountProfile }
        return try await execute(Documents.updateAccountProfile, variables: draft.variables, as: Answer.self).UpdateUser
    }

    /// Says how the person is described, alone; not set clears it. Answers the
    /// account as the server now has it.
    public func setAccountPronoun(_ userId: String, pronoun: PersonPronoun) async throws -> AccountProfile {
        struct Answer: Decodable, Sendable { let UpdateUser: AccountProfile }
        return try await execute(
            Documents.updateAccountPronoun, variables: ["userId": .string(userId), "pronoun": .string(pronoun.rawValue)], as: Answer.self
        ).UpdateUser
    }

    /// Sets the person's picture. The bytes are a PNG or JPEG the server takes
    /// at most 8 MiB of; the answer is the account with its new picture address.
    public func setAccountAvatar(_ content: Data) async throws -> AccountProfile {
        struct Answer: Decodable, Sendable { let SetUserAvatar: AccountProfile }
        return try await execute(
            Documents.setAccountAvatar, variables: ["content": .string(content.base64EncodedString())], as: Answer.self
        ).SetUserAvatar
    }

    /// Takes the picture away; the initial on the person's color is shown again.
    public func removeAccountAvatar() async throws -> AccountProfile {
        struct Answer: Decodable, Sendable { let RemoveUserAvatar: AccountProfile }
        return try await execute(Documents.removeAccountAvatar, as: Answer.self).RemoveUserAvatar
    }

    /// Replaces the password, after the server checks the current one. The
    /// person's other browser sessions end.
    public func changePassword(currentPassword: String, newPassword: String) async throws {
        struct Answer: Decodable, Sendable { let ChangePassword: JSONValue? }
        _ = try await execute(
            Documents.changePassword, variables: ["currentPassword": .string(currentPassword), "newPassword": .string(newPassword)],
            as: Answer.self)
    }

    public func pairedPhones() async throws -> PairedPhoneList {
        struct Answer: Decodable, Sendable { let ListAgentPhones: PairedPhoneList }
        return try await execute(Documents.pairedPhones, as: Answer.self).ListAgentPhones
    }

    /// Renames a phone; what it is told about stays as it is (see
    /// updatePhone for that).
    public func renamePhone(_ phoneId: String, phoneName: String) async throws {
        struct Answer: Decodable, Sendable { let UpdateAgentPhone: JSONValue? }
        _ = try await execute(Documents.renamePhone, variables: ["phoneId": .string(phoneId), "phoneName": .string(phoneName)], as: Answer.self)
    }

    /// Sends a phone a notification that says it can be reached.
    public func testPhone(_ phoneId: String) async throws {
        struct Answer: Decodable, Sendable { let TestAgentPhone: JSONValue? }
        _ = try await execute(Documents.testPhone, variables: ["phoneId": .string(phoneId)], as: Answer.self)
    }

    /// Removes a phone: it is signed out and told nothing more.
    public func removePhone(_ phoneId: String) async throws {
        struct Answer: Decodable, Sendable { let RemoveAgentPhone: JSONValue? }
        _ = try await execute(Documents.removePhone, variables: ["phoneId": .string(phoneId)], as: Answer.self)
    }

    public func passkeys() async throws -> (passkeys: [Passkey], policy: PasskeyPolicy?) {
        struct Answer: Decodable, Sendable {
            let GetPasskeyPolicy: PasskeyPolicy?
            let ListPasskeys: [Passkey?]
        }
        let answer = try await execute(Documents.passkeys, as: Answer.self)
        return (answer.ListPasskeys.compactMap { $0 }, answer.GetPasskeyPolicy)
    }

    /// Removes a passkey, so that authenticator can no longer sign in.
    public func deletePasskey(_ passkeyId: String) async throws {
        struct Answer: Decodable, Sendable { let DeletePasskey: JSONValue? }
        _ = try await execute(Documents.deletePasskey, variables: ["passkeyId": .string(passkeyId)], as: Answer.self)
    }
}
