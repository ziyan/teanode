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

/// MailRule is one of the rules that sort mail as it arrives: when all of
/// its conditions match, its actions run, and a rule that stops ends the
/// run.
public struct MailRule: Codable, Hashable, Sendable {
    /// Condition is one test of a message.
    public struct Condition: Codable, Hashable, Sendable {
        /// from, to, subject, header, score, sender-known, category,
        /// priority, needs-reply or any.
        public var field: String
        /// The header's name, when the field is header.
        public var header: String?
        /// contains, equals, matches, above or below; empty for the fields
        /// that need no value.
        public var comparisonOperator: String
        public var value: String?

        enum CodingKeys: String, CodingKey {
            case field, header, value
            case comparisonOperator = "operator"
        }

        public init(field: String = "from", header: String? = nil, comparisonOperator: String = "contains", value: String? = "") {
            self.field = field
            self.header = header
            self.comparisonOperator = comparisonOperator
            self.value = value
        }

        public init(from decoder: Decoder) throws {
            let container = try decoder.container(keyedBy: CodingKeys.self)
            field = try container.decode(String.self, forKey: .field)
            header = try container.decodeIfPresent(String.self, forKey: .header)
            comparisonOperator = try container.decodeIfPresent(String.self, forKey: .comparisonOperator) ?? ""
            value = try container.decodeIfPresent(String.self, forKey: .value)
        }

        /// The fields that need no value: the condition is the field.
        public static let bareFields: Set<String> = ["sender-known", "needs-reply", "any"]

        public var isBare: Bool { Self.bareFields.contains(field) }

        /// What a field can be compared with: a score is above or below a
        /// number, a priority is one of three, everything else matches text.
        public static func operators(for field: String) -> [String] {
            if field == "score" { return ["above", "below"] }
            if field == "priority" { return ["equals"] }
            if bareFields.contains(field) { return [""] }
            return ["contains", "equals", "matches"]
        }
    }

    /// Action is one thing done to a message a rule matched.
    public struct Action: Codable, Hashable, Sendable {
        /// move, markRead, flag, forward or delete.
        public var kind: String
        public var folderId: String?
        public var address: String?

        public init(kind: String = "move", folderId: String? = "", address: String? = nil) {
            self.kind = kind
            self.folderId = folderId
            self.address = address
        }
    }

    public var name: String
    public var enabled: Bool
    public var stop: Bool
    public var conditions: [Condition]
    public var actions: [Action]

    public init(
        name: String = "", enabled: Bool = true, stop: Bool = false, conditions: [Condition] = [Condition()],
        actions: [Action] = [Action()]
    ) {
        self.name = name
        self.enabled = enabled
        self.stop = stop
        self.conditions = conditions
        self.actions = actions
    }

    public static let fields = ["from", "to", "subject", "header", "score", "sender-known", "category", "priority", "needs-reply", "any"]
    public static let actionKinds = ["move", "markRead", "flag", "forward", "delete"]
    public static let priorities = ["high", "normal", "low"]

    /// As the server takes it: each part only where it means something, so
    /// a header name left from a field changed since is not sent.
    var variables: JSONValue {
        .object([
            "name": .string(name), "enabled": .bool(enabled), "stop": .bool(stop),
            "conditions": .array(conditions.map { condition in
                .object([
                    "field": .string(condition.field),
                    "header": .string(condition.field == "header" ? condition.header ?? "" : ""),
                    "operator": .string(condition.isBare ? "" : condition.comparisonOperator),
                    "value": .string(condition.value ?? ""),
                ])
            }),
            "actions": .array(actions.map { action in
                .object([
                    "kind": .string(action.kind),
                    "folderId": .string(action.kind == "move" ? action.folderId ?? "" : ""),
                    "address": .string(action.kind == "forward" ? action.address ?? "" : ""),
                ])
            }),
        ])
    }
}

/// MailRuleTrial is what the rules would do with one recent message: the
/// places in the list of the rules that match it.
public struct MailRuleTrial: Codable, Hashable, Sendable {
    public struct Item: Codable, Hashable, Sendable {
        public struct Mail: Codable, Hashable, Sendable {
            public let from: String?
            public let sender: String?
            public let subject: String?
            public let receivedAt: String?
        }
        public let id: String
        public let mail: Mail?
    }
    public let matched: [Int]
    public let item: Item
}

extension Documents {
    public static let updateMailbox = """
        mutation ($mailboxId: String!, $name: String, $signatureText: String, $signatureHtml: String, $autoReply: MailboxAutoReplyInput) {
          UpdateMailbox(mailboxId: $mailboxId, name: $name, signatureText: $signatureText, signatureHtml: $signatureHtml, autoReply: $autoReply) {
            mailbox { id }
          }
        }
        """
    public static let updateMailboxRules = """
        mutation ($mailboxId: String!, $rules: [MailboxRuleInput!]) {
          UpdateMailbox(mailboxId: $mailboxId, rules: $rules) {
            mailbox { id }
          }
        }
        """
    public static let testMailboxRules = """
        query ($mailboxId: String!, $rules: [MailboxRuleInput!]!, $first: Int) {
          TestMailboxRules(mailboxId: $mailboxId, rules: $rules, first: $first) {
            matched
            item { id mail { from sender subject receivedAt } }
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

    /// Replaces the mailbox's rules with these, in this order.
    public func updateMailboxRules(_ mailboxId: String, rules: [MailRule]) async throws {
        struct Answer: Decodable, Sendable { let UpdateMailbox: JSONValue? }
        _ = try await execute(
            Documents.updateMailboxRules,
            variables: ["mailboxId": .string(mailboxId), "rules": .array(rules.map(\.variables))], as: Answer.self)
    }

    /// What these rules would do with the newest messages in the Inbox;
    /// nothing is moved.
    public func testMailboxRules(_ mailboxId: String, rules: [MailRule], first: Int = 20) async throws -> [MailRuleTrial] {
        struct Answer: Decodable, Sendable { let TestMailboxRules: [MailRuleTrial] }
        return try await execute(
            Documents.testMailboxRules,
            variables: ["mailboxId": .string(mailboxId), "rules": .array(rules.map(\.variables)), "first": .number(Double(first))],
            as: Answer.self
        ).TestMailboxRules
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
