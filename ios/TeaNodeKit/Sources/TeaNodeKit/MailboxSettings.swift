import Foundation

/// MailAppPassword is a password a mail program uses for the mailbox.
public struct MailAppPassword: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public let createdAt: String
    public let lastUsedAt: String?
}

/// MailProgramSettings are what a mail program needs to reach the server.
public struct MailProgramSettings: Codable, Hashable, Sendable {
    public let imapHost: String
    public let imapPort: Int
    public let imapsPort: Int
    public let submissionHost: String
    public let submissionPort: Int
    public let davHost: String
}

/// MailAutoReplyDraft is the auto-reply as its form holds it.
public struct MailAutoReplyDraft: Hashable, Sendable {
    public var isEnabled = false
    public var from: Date?
    public var until: Date?
    public var subject = ""
    public var text = ""
    public var isSameDomainOnly = false

    public init() {}

    public init(_ reply: Mailbox.AutoReply?) {
        guard let reply else { return }
        isEnabled = reply.enabled
        from = ServerTime.date(reply.from)
        until = ServerTime.date(reply.until)
        subject = reply.subject
        text = reply.text
        isSameDomainOnly = reply.sameDomainOnly
    }

    /// As the server takes it: plain text, no HTML.
    var variables: JSONValue {
        var fields: [String: JSONValue] = [
            "enabled": .bool(isEnabled), "subject": .string(subject), "text": .string(text), "html": .string(""),
            "sameDomainOnly": .bool(isSameDomainOnly),
        ]
        fields["from"] = from.map { .string(CalendarDates.text($0)) } ?? .null
        fields["until"] = until.map { .string(CalendarDates.text($0)) } ?? .null
        return .object(fields)
    }
}

extension Documents {
    public static let updateMailbox = """
        mutation ($mailboxId: String!, $name: String, $signatureText: String, $signatureHtml: String, $autoReply: MailboxAutoReplyInput) {
          UpdateMailbox(mailboxId: $mailboxId, name: $name, signatureText: $signatureText, signatureHtml: $signatureHtml, autoReply: $autoReply) {
            mailbox { id }
          }
        }
        """
    public static let createMailFolder = """
        mutation ($mailboxId: String!, $name: String!, $parentId: String) {
          CreateMailboxFolder(mailboxId: $mailboxId, name: $name, parentId: $parentId) { id }
        }
        """
    public static let updateMailFolder = """
        mutation ($folderId: String!, $name: String, $parentId: String) {
          UpdateMailboxFolder(folderId: $folderId, name: $name, parentId: $parentId) { id }
        }
        """
    public static let pinMailFolder = """
        mutation ($folderId: String!, $pinned: Boolean!) { SetMailboxFolderPinned(folderId: $folderId, pinned: $pinned) { id } }
        """
    public static let deleteMailFolder = """
        mutation ($folderId: String!) { DeleteMailboxFolder(folderId: $folderId) }
        """
    public static let mailAppPasswords = """
        query ($mailboxId: String!) {
          ListMailboxAppPasswords(mailboxId: $mailboxId) { id name createdAt lastUsedAt }
          GetMailProgramSettings { imapHost imapPort imapsPort submissionHost submissionPort davHost }
        }
        """
    public static let createMailAppPassword = """
        mutation ($mailboxId: String!, $name: String!) {
          CreateMailboxAppPassword(mailboxId: $mailboxId, name: $name) { password username appPassword { id name } }
        }
        """
    public static let createSetupProfile = """
        mutation ($mailboxId: String!, $deviceName: String!) {
          CreateMailboxSetupProfile(mailboxId: $mailboxId, deviceName: $deviceName) { downloadLink expiresAt hasCalendarsAndContacts }
        }
        """
    public static let deleteMailAppPassword = """
        mutation ($appPasswordId: String!) { DeleteMailboxAppPassword(appPasswordId: $appPasswordId) }
        """
}

extension Connection {
    /// Changes a mailbox's name, signature or auto-reply; what is not given
    /// stays as it is.
    public func updateMailbox(_ mailboxId: String, name: String? = nil, signature: String? = nil, autoReply: MailAutoReplyDraft? = nil) async throws {
        struct Answer: Decodable, Sendable { let UpdateMailbox: JSONValue? }
        var variables: [String: JSONValue] = ["mailboxId": .string(mailboxId)]
        if let name { variables["name"] = .string(name) }
        if let signature {
            variables["signatureText"] = .string(signature)
            variables["signatureHtml"] = .string("")
        }
        if let autoReply { variables["autoReply"] = autoReply.variables }
        _ = try await execute(Documents.updateMailbox, variables: variables, as: Answer.self)
    }

    public func createMailFolder(mailboxId: String, name: String, parentId: String?) async throws {
        struct Answer: Decodable, Sendable { let CreateMailboxFolder: JSONValue? }
        var variables: [String: JSONValue] = ["mailboxId": .string(mailboxId), "name": .string(name)]
        if let parentId, !parentId.isEmpty { variables["parentId"] = .string(parentId) }
        _ = try await execute(Documents.createMailFolder, variables: variables, as: Answer.self)
    }

    public func renameMailFolder(_ folderId: String, name: String) async throws {
        struct Answer: Decodable, Sendable { let UpdateMailboxFolder: JSONValue? }
        _ = try await execute(Documents.updateMailFolder, variables: ["folderId": .string(folderId), "name": .string(name)], as: Answer.self)
    }

    public func pinMailFolder(_ folderId: String, isPinned: Bool) async throws {
        struct Answer: Decodable, Sendable { let SetMailboxFolderPinned: JSONValue? }
        _ = try await execute(Documents.pinMailFolder, variables: ["folderId": .string(folderId), "pinned": .bool(isPinned)], as: Answer.self)
    }

    public func deleteMailFolder(_ folderId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteMailboxFolder: JSONValue? }
        _ = try await execute(Documents.deleteMailFolder, variables: ["folderId": .string(folderId)], as: Answer.self)
    }

    public func mailAppPasswords(mailboxId: String) async throws -> (passwords: [MailAppPassword], settings: MailProgramSettings?) {
        struct Answer: Decodable, Sendable {
            let ListMailboxAppPasswords: [MailAppPassword?]
            let GetMailProgramSettings: MailProgramSettings?
        }
        let answer = try await execute(Documents.mailAppPasswords, variables: ["mailboxId": .string(mailboxId)], as: Answer.self)
        return (answer.ListMailboxAppPasswords.compactMap { $0 }, answer.GetMailProgramSettings)
    }

    /// Makes an app password; it is shown this once.
    public func createMailAppPassword(mailboxId: String, name: String) async throws -> (password: String, username: String) {
        struct Answer: Decodable, Sendable {
            struct Created: Decodable, Sendable {
                let password: String
                let username: String
            }
            let CreateMailboxAppPassword: Created
        }
        let created = try await execute(Documents.createMailAppPassword, variables: ["mailboxId": .string(mailboxId), "name": .string(name)], as: Answer.self)
            .CreateMailboxAppPassword
        return (created.password, created.username)
    }

    /// Makes a setup profile for this device, with an app password of its
    /// own, and returns the address it downloads from, once.
    public func createSetupProfile(mailboxId: String, deviceName: String) async throws -> URL {
        struct Answer: Decodable, Sendable {
            struct Created: Decodable, Sendable { let downloadLink: String }
            let CreateMailboxSetupProfile: Created
        }
        let created = try await execute(
            Documents.createSetupProfile, variables: ["mailboxId": .string(mailboxId), "deviceName": .string(deviceName)], as: Answer.self
        ).CreateMailboxSetupProfile
        guard let link = URL(string: created.downloadLink) else { throw TeaNodeError.unreadableAnswer }
        return link
    }

    public func deleteMailAppPassword(_ appPasswordId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteMailboxAppPassword: JSONValue? }
        _ = try await execute(Documents.deleteMailAppPassword, variables: ["appPasswordId": .string(appPasswordId)], as: Answer.self)
    }
}
