import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// MailboxEntry is one of the person's mailboxes with its folders and the
/// unread counts the dashboard's rail shows.
public struct MailboxEntry: Codable, Hashable, Sendable {
    public let mailbox: Mailbox?
    public let folders: [MailFolder?]
    public let unread: Int64
    public let starredUnread: Int64
    public let priorityUnread: Int64
    /// The most a message may be, in bytes; zero when there is no limit.
    public let maxMessageSize: Int64

    public var presentFolders: [MailFolder] { folders.compactMap { $0 } }
}

/// Mailbox is a mailbox: its name, signature, addresses, auto-reply and
/// what its agent may do.
public struct Mailbox: Codable, Hashable, Sendable, Identifiable {
    public struct Address: Codable, Hashable, Sendable {
        public let address: String
        public let domain: String
        public let domainId: String
    }

    public struct AutoReply: Codable, Hashable, Sendable {
        public let enabled: Bool
        public let from: String?
        public let until: String?
        public let subject: String
        public let text: String
        public let html: String
        public let sameDomainOnly: Bool
    }

    public struct AgentAccess: Codable, Hashable, Sendable {
        public struct Triage: Codable, Hashable, Sendable { public let enabled: Bool }
        public let granted: Bool
        public let draftReplies: Bool
        public let triage: Triage?

        /// Whether the agent sorts this mailbox, so Sort as and the Priority
        /// view are offered.
        public var isSorting: Bool { granted && triage?.enabled == true }
    }

    public let id: String
    public let name: String
    public let signatureText: String
    public let signatureHtml: String
    public let addresses: [Address?]
    public let autoReply: AutoReply?
    public let agent: AgentAccess?
    /// What sorts mail as it arrives, in the order the rules run.
    public let rules: [MailRule]?

    public var sendingAddresses: [String] { addresses.compactMap { $0?.address } }
}

/// MailFolder is a folder: a system one by its kind, or one of the owner's.
public struct MailFolder: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let mailboxId: String
    public let parentId: String
    public let name: String
    /// inbox, drafts, sent, archive, junk, trash, or empty for the owner's.
    public let kind: String
    public let pinnedAt: String?
    public let unread: Int64
    public let total: Int64
    public let noteCount: Int64

    public init(
        id: String, mailboxId: String, parentId: String = "", name: String, kind: String = "", pinnedAt: String? = nil,
        unread: Int64 = 0, total: Int64 = 0, noteCount: Int64 = 0
    ) {
        self.id = id
        self.mailboxId = mailboxId
        self.parentId = parentId
        self.name = name
        self.kind = kind
        self.pinnedAt = pinnedAt
        self.unread = unread
        self.total = total
        self.noteCount = noteCount
    }

    /// A folder that holds only notes, or an empty one named Notes, which
    /// the dashboard shows as notes.
    public var isNotes: Bool {
        kind.isEmpty && ((total > 0 && noteCount == total) || (total == 0 && name.lowercased() == "notes"))
    }
}

/// MailFolders orders folders as the dashboard's rail does.
public enum MailFolders {
    static let systemOrder = ["inbox", "drafts", "sent", "archive", "junk", "trash"]

    /// The folders in tree order with their depth: the system folders in
    /// their order, then the owner's alphabetically, each followed by its
    /// children.
    public static func rows(_ folders: [MailFolder]) -> [(folder: MailFolder, depth: Int)] {
        let known = Set(folders.map(\.id))
        var children: [String: [MailFolder]] = [:]
        for folder in folders {
            let parent = known.contains(folder.parentId) ? folder.parentId : ""
            children[parent, default: []].append(folder)
        }
        func ordered(_ list: [MailFolder]) -> [MailFolder] {
            list.sorted { left, right in
                let leftRank = systemOrder.firstIndex(of: left.kind) ?? systemOrder.count
                let rightRank = systemOrder.firstIndex(of: right.kind) ?? systemOrder.count
                if leftRank != rightRank { return leftRank < rightRank }
                return left.name.localizedStandardCompare(right.name) == .orderedAscending
            }
        }
        var rows: [(folder: MailFolder, depth: Int)] = []
        func walk(_ parent: String, _ depth: Int) {
            for folder in ordered(children[parent] ?? []) {
                rows.append((folder, depth))
                walk(folder.id, depth + 1)
            }
        }
        walk("", 0)
        return rows
    }

    public static func folder(kind: String, in folders: [MailFolder]) -> MailFolder? {
        folders.first { $0.kind == kind }
    }
}

/// MailMessage is a message as the server keeps it, without its content.
public struct MailMessage: Codable, Hashable, Sendable {
    public struct Verdicts: Codable, Hashable, Sendable {
        public struct Outcome: Codable, Hashable, Sendable { public let result: String }
        public struct Spam: Codable, Hashable, Sendable { public let score: Double }
        public let spf: Outcome?
        public let dkims: [Outcome?]?
        public let dmarc: Outcome?
        public let spamFilter: Spam?
    }

    public let id: String
    public let from: String
    public let fromName: String
    public let sender: String
    public let subject: String
    public let recipients: [String]
    public let receivedAt: String
    public let size: Int64
    /// How many attachments the message carries; nil from a server that does not count them.
    public var attachmentCount: Int? = nil
    public let messageId: String?
    public let listKey: String?
    public let listName: String?
    public let logoDomain: String
    public let authenticationResults: Verdicts?

    /// Who it is from, in the words the list shows.
    public var shownSender: String {
        if !fromName.isEmpty { return fromName }
        if !from.isEmpty { return from }
        return sender
    }
}

/// MailVerdict is how far a message can be trusted to come from who it
/// says, as the dashboard's shield shows it.
public enum MailVerdict: String, Sendable {
    case good, warn, bad, unknown

    public static func of(_ results: MailMessage.Verdicts?) -> MailVerdict {
        guard let results else { return .unknown }
        let dmarc = results.dmarc?.result.lowercased() ?? ""
        let spf = results.spf?.result.lowercased() ?? ""
        let dkims = (results.dkims ?? []).compactMap { $0?.result.lowercased() }
        let isDKIMPassed = dkims.contains("pass")
        if dmarc == "fail" || spf == "fail" || spf == "hardfail" || (dkims.contains("fail") && !isDKIMPassed) { return .bad }
        if let score = results.spamFilter?.score, score >= 5 { return .warn }
        if dmarc == "pass" || (spf == "pass" && isDKIMPassed) { return .good }
        return .unknown
    }

    /// "SPF pass · DKIM pass · DMARC pass · spam 0.4".
    public static func detail(_ results: MailMessage.Verdicts?) -> String {
        guard let results else { return "" }
        var parts: [String] = []
        if let spf = results.spf?.result, !spf.isEmpty { parts.append("SPF \(spf)") }
        let dkims = (results.dkims ?? []).compactMap { $0?.result }.filter { !$0.isEmpty }
        if !dkims.isEmpty { parts.append("DKIM \(dkims.joined(separator: "/"))") }
        if let dmarc = results.dmarc?.result, !dmarc.isEmpty { parts.append("DMARC \(dmarc)") }
        if let score = results.spamFilter?.score { parts.append(String(format: "spam %.1f", score)) }
        return parts.joined(separator: " · ")
    }
}

/// MailInsight is what the agent made of a message when it sorted it.
public struct MailInsight: Codable, Hashable, Sendable {
    public let category: String
    public let priority: String
    public let needsReply: Bool
    public let summary: String
    public let actionItems: [String]
    public let notes: String?
    public let model: String?
    public let runId: String?
    public let proposals: [MailProposal]?
}

/// MailProposal is an event or a contact the agent found in a message and
/// offers to add.
public struct MailProposal: Codable, Hashable, Sendable {
    public let kind: String
    public let because: String
    public let status: String
    public let summary: String
    public let starts: String
    public let ends: String
    public let location: String
    public let allDay: Bool
    public let name: String
    public let organization: String
    public let title: String
    public let emails: [String]
    public let phones: [String]
    public let note: String
    public let contactId: String
}

/// MailItem is a message in a folder, with its flags.
public struct MailItem: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let folderId: String
    public let mailId: String
    public let seen: Bool
    public let flagged: Bool
    public let answered: Bool
    public let forwarded: Bool
    public let draft: Bool
    public let addedAt: String
    public let subscriptionId: String?
    public let insight: MailInsight?
    public let mail: MailMessage?
}

/// MailThread is a conversation as one row of a folder.
public struct MailThread: Codable, Hashable, Sendable, Identifiable {
    public let threadId: String
    public let count: Int
    public let unread: Int
    public let flagged: Bool
    public let participants: [String]
    public let itemIds: [String]
    public let hasDraft: Bool
    public let item: MailItem?

    public var id: String { item?.id ?? threadId }
}

extension MailItem {
    /// The item with its flags changed, as it reads once the server has
    /// them.
    public func with(isSeen: Bool? = nil, isFlagged: Bool? = nil) -> MailItem {
        MailItem(
            id: id, folderId: folderId, mailId: mailId, seen: isSeen ?? seen, flagged: isFlagged ?? flagged, answered: answered,
            forwarded: forwarded, draft: draft, addedAt: addedAt, subscriptionId: subscriptionId, insight: insight, mail: mail)
    }
}

extension MailThread {
    /// The row with its flags changed: read clears its unread count.
    public func with(isSeen: Bool? = nil, isFlagged: Bool? = nil) -> MailThread {
        MailThread(
            threadId: threadId, count: count, unread: isSeen.map { $0 ? 0 : max(1, unread) } ?? unread, flagged: isFlagged ?? flagged,
            participants: participants, itemIds: itemIds, hasDraft: hasDraft, item: item?.with(isSeen: isSeen, isFlagged: isFlagged))
    }
}

public struct MailThreadPage: Codable, Hashable, Sendable {
    public let total: Int64
    public let threads: [MailThread?]
}

/// MailThreadView is a whole conversation, newest first, across folders.
public struct MailThreadView: Codable, Hashable, Sendable {
    public struct Entry: Codable, Hashable, Sendable {
        public let folderId: String
        public let folderName: String
        public let folderKind: String
        public let item: MailItem?
    }

    public struct Summary: Codable, Hashable, Sendable {
        public let summary: String
        public let messageCount: Int
        public let stale: Bool
        public let pending: Bool
    }

    /// A reply the agent is holding back to send.
    public struct HeldReply: Codable, Hashable, Sendable {
        public let id: String
        public let status: String
        public let subject: String
        public let to: String
        public let text: String
        public let sendAfter: String?
        public let draftItemId: String
    }

    public let threadId: String
    public let subject: String
    public let truncated: Bool
    public let summary: Summary?
    public let heldReply: HeldReply?
    public let items: [Entry?]

    public var entries: [Entry] { items.compactMap { $0 } }
}

/// MailContent is a message's text, its cleaned HTML, its headers and its
/// attachments.
public struct MailContent: Codable, Hashable, Sendable {
    public struct Attachment: Codable, Hashable, Sendable {
        public let index: Int
        public let filename: String
        public let contentType: String
        public let size: Int
        public let inline: Bool
    }

    public struct Header: Codable, Hashable, Sendable {
        public let key: String
        public let value: String
    }

    public let mailId: String
    public let available: Bool
    public let text: String
    public let html: String
    public let hasRemoteContent: Bool?
    public let imagesAllowed: Bool?
    public let rawHeaders: String?
    public let headers: [Header?]?
    public let attachments: [Attachment?]

    public var presentAttachments: [Attachment] { attachments.compactMap { $0 } }

    /// A header's value, by its name in any case.
    public func header(_ name: String) -> String? {
        (headers ?? []).compactMap { $0 }.first { $0.key.lowercased() == name.lowercased() }?.value
    }
}

/// MailDraft is a draft whole, read to go on writing it.
public struct MailDraft: Codable, Hashable, Sendable {
    public let itemId: String
    public let mailId: String
    public let from: String
    public let to: [String]
    public let cc: [String]
    public let bcc: [String]
    public let subject: String
    public let html: String
    public let text: String
    public let replyToItemId: String
    public let forwardItemId: String
    public let attachments: [MailContent.Attachment?]
}

/// MailSubscription is a mailing list that writes to the mailbox.
public struct MailSubscription: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let key: String
    public let name: String
    public let from: String
    public let count: Int
    public let unread: Int
    public let lastAt: String
    public let lastItemId: String
    public let oneClick: Bool
    public let unsubscribe: [String]
    public let logoDomain: String
    public let requestedAt: String?
    public let method: String
    public let failed: Bool
    public let error: String
    public let stripped: Bool
    public let mutedAt: String?
    public let imagesAt: String?

    /// How the person can leave: one click, a mail, a page of the sender's,
    /// or no way at all.
    public var leaveKind: String {
        if oneClick { return "oneClick" }
        if unsubscribe.contains(where: { $0.lowercased().hasPrefix("mailto:") }) { return "mail" }
        if unsubscribe.contains(where: { $0.lowercased().hasPrefix("http") }) { return "link" }
        return "none"
    }

    public var leavePage: URL? {
        unsubscribe.first { $0.lowercased().hasPrefix("http") }.flatMap(URL.init(string:))
    }
}

public struct MailSubscriptionPage: Codable, Hashable, Sendable {
    public let total: Int64
    public let subscribed: Int64
    public let left: Int64
    public let subscriptions: [MailSubscription?]
}

/// MailInvitation is a calendar invitation a message carries.
public struct MailInvitation: Codable, Hashable, Sendable {
    public struct Attendee: Codable, Hashable, Sendable {
        public let address: String
        public let name: String?
        public let participation: String?
    }

    public let id: String
    public let method: String
    public let summary: String
    public let location: String
    public let startsAt: String
    public let endsAt: String
    public let allDay: Bool
    public let cancelled: Bool
    public let organizer: String
    public let participation: String
    public let attendees: [Attendee?]
}

/// MailFilter is what a folder's list is narrowed to.
public struct MailFilter: Hashable, Sendable {
    public var searchText = ""
    public var isUnreadOnly = false
    public var isStarredOnly = false
    public var isEverywhere = false
    public var from = ""
    public var to = ""
    public var subject = ""
    public var since: Date?
    public var before: Date?
    /// nil for any, true for with, false for without.
    public var hasAttachment: Bool?

    public init() {}

    /// Whether anything narrows the list.
    public var isNarrowing: Bool {
        !searchText.isEmpty || isUnreadOnly || isStarredOnly || !from.isEmpty || !to.isEmpty || !subject.isEmpty || since != nil
            || before != nil || hasAttachment != nil
    }
}

/// MailWords are the small rules for replies and forwards.
public enum MailWords {
    /// "Re: " in front, unless it is there already.
    public static func replySubject(_ subject: String) -> String {
        subject.lowercased().hasPrefix("re:") ? subject : "Re: \(subject)"
    }

    public static func forwardSubject(_ subject: String) -> String {
        subject.lowercased().hasPrefix("fwd:") ? subject : "Fwd: \(subject)"
    }

    /// The bare address in "Name <address>".
    public static func bareAddress(_ written: String) -> String {
        if let open = written.lastIndex(of: "<"), let close = written.lastIndex(of: ">"), open < close {
            return String(written[written.index(after: open)..<close]).trimmingCharacters(in: .whitespaces)
        }
        return written.trimmingCharacters(in: .whitespaces)
    }

    /// Addresses typed into a field, split on commas, semicolons and lines.
    public static func addresses(_ typed: String) -> [String] {
        typed.split(whereSeparator: { $0 == "," || $0 == ";" || $0.isNewline })
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
    }

    /// Who a reply goes to and from: the Reply-To or the sender; the address
    /// of the person's that it was written to; and for reply all, everyone
    /// else it went to.
    public static func reply(
        to message: MailMessage, replyTo: String?, own: [String], isReplyAll: Bool
    ) -> (to: [String], cc: [String], from: String?) {
        let ownBare = Set(own.map { $0.lowercased() })
        let recipient = replyTo.flatMap { $0.isEmpty ? nil : $0 } ?? (message.from.isEmpty ? message.sender : message.from)
        let from = message.recipients.map { bareAddress($0) }.first { ownBare.contains($0.lowercased()) }
        var cc: [String] = []
        if isReplyAll {
            let toBare = bareAddress(recipient).lowercased()
            cc = message.recipients.filter { address in
                let bare = bareAddress(address).lowercased()
                return !ownBare.contains(bare) && bare != toBare
            }
        }
        return ([recipient], cc, from)
    }

    /// Plain text as HTML paragraphs, the way the dashboard writes it.
    public static func html(ofText text: String) -> String {
        text.components(separatedBy: "\n\n").map { paragraph in
            "<p>" + escaped(paragraph).replacingOccurrences(of: "\n", with: "<br>") + "</p>"
        }.joined()
    }

    public static func escaped(_ text: String) -> String {
        text.replacingOccurrences(of: "&", with: "&amp;").replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;").replacingOccurrences(of: "\"", with: "&quot;")
    }

    /// Lines with "> " in front, for a plain reply's quote.
    public static func quoted(_ text: String) -> String {
        text.split(separator: "\n", omittingEmptySubsequences: false).map { "> \($0)" }.joined(separator: "\n")
    }

    /// The images a message's sender keeps on their own server, which the
    /// server sends held back (`data-blocked-src`), pointed at this server's
    /// proxy instead, so they never load from the sender.
    public static func showingRemoteImages(_ html: String, mailId: String) -> String {
        guard let pattern = try? NSRegularExpression(pattern: #"data-blocked-src="([^"]*)"\s+src="[^"]*""#) else { return html }
        var result = ""
        var last = html.startIndex
        for match in pattern.matches(in: html, range: NSRange(html.startIndex..., in: html)) {
            guard let whole = Range(match.range, in: html), let held = Range(match.range(at: 1), in: html) else { continue }
            result += html[last..<whole.lowerBound]
            let original = decodedAttribute(String(html[held]))
            let encoded = original.addingPercentEncoding(withAllowedCharacters: .alphanumerics.union(CharacterSet(charactersIn: "-_.!~*'()"))) ?? ""
            result += #"src="/api/v1/mail/\#(mailId)/remote?url=\#(encoded)""#
            last = whole.upperBound
        }
        result += html[last...]
        return result
    }

    /// Undoes the server's attribute escaping, in the order the dashboard
    /// does: the named characters first, the ampersand last.
    static func decodedAttribute(_ text: String) -> String {
        text.replacingOccurrences(of: "&lt;", with: "<").replacingOccurrences(of: "&gt;", with: ">")
            .replacingOccurrences(of: "&#34;", with: "\"").replacingOccurrences(of: "&#39;", with: "'")
            .replacingOccurrences(of: "&#13;", with: "\r").replacingOccurrences(of: "&amp;", with: "&")
    }
}

extension Documents {
    public static let mailboxes = """
        query {
          ListMailboxes {
            mailbox {
              id name signatureText signatureHtml
              addresses { address domain domainId }
              autoReply { enabled from until subject text html sameDomainOnly }
              agent { granted draftReplies triage { enabled } }
              rules { name enabled stop conditions { field header operator value } actions { kind folderId address } }
            }
            folders { id mailboxId parentId name kind pinnedAt unread total noteCount }
            unread starredUnread priorityUnread maxMessageSize
          }
        }
        """
    static let mailItemFields = """
        id folderId mailId seen flagged answered forwarded draft addedAt subscriptionId
        insight { category priority needsReply summary actionItems notes model runId
          proposals { kind because status summary starts ends location allDay name organization title emails phones note contactId } }
        mail {
          id from fromName sender subject recipients receivedAt size attachmentCount messageId listKey listName logoDomain
          authenticationResults { spf { result } dkims { result } dmarc { result } spamFilter { score } }
        }
        """
    public static let mailThreads = """
        query ($folderId: String, $mailboxId: String, $unread: Boolean, $flagged: Boolean, $priority: String, $search: String,
          $from: String, $to: String, $subject: String, $since: DateTime, $before: DateTime, $hasAttachment: Boolean, $first: Int, $offset: Int) {
          ListMailboxThreads(folderId: $folderId, mailboxId: $mailboxId, unread: $unread, flagged: $flagged, priority: $priority,
            search: $search, from: $from, to: $to, subject: $subject, since: $since, before: $before, hasAttachment: $hasAttachment,
            first: $first, offset: $offset) {
            total
            threads { threadId count unread flagged participants itemIds hasDraft item { \(mailItemFields) } }
          }
        }
        """
    public static let mailThread = """
        query ($itemId: String!) {
          GetMailboxThread(itemId: $itemId) {
            threadId subject truncated
            summary { summary messageCount stale pending }
            heldReply { id status subject to text sendAfter draftItemId }
            items { folderId folderName folderKind item { \(mailItemFields) } }
          }
        }
        """
    public static let mailItem = """
        query ($itemId: String!) { GetMailboxItem(itemId: $itemId) { \(mailItemFields) } }
        """
    public static let mailContent = """
        query ($mailId: String!) {
          GetMailContent(mailId: $mailId) {
            mailId available text html hasRemoteContent imagesAllowed rawHeaders
            headers { key value }
            attachments { index filename contentType size inline }
          }
        }
        """
    public static let setMailFlags = """
        mutation ($itemIds: [String!]!, $seen: Boolean, $flagged: Boolean) {
          SetMailboxItemFlags(itemIds: $itemIds, seen: $seen, flagged: $flagged)
        }
        """
    public static let moveMail = """
        mutation ($itemIds: [String!]!, $folderId: String!) {
          MoveMailboxItems(itemIds: $itemIds, folderId: $folderId) { id folderId mailId }
        }
        """
    public static let deleteMail = """
        mutation ($itemIds: [String!]!) { DeleteMailboxItems(itemIds: $itemIds) }
        """
    public static let reportJunk = """
        mutation ($itemIds: [String!]!, $notJunk: Boolean) { ReportMailboxJunk(itemIds: $itemIds, notJunk: $notJunk) }
        """
    public static let emptyTrash = """
        mutation ($mailboxId: String!) { EmptyMailboxTrash(mailboxId: $mailboxId) }
        """
    public static let newestMailItems = """
        query ($folderId: String, $first: Int) {
          ListMailboxItems(folderId: $folderId, first: $first) { items { id folderId mailId addedAt } }
        }
        """
    public static let sortMail = """
        mutation ($itemIds: [String!]!, $category: String, $priority: String, $needsReply: Boolean) {
          SortMailboxItems(itemIds: $itemIds, category: $category, priority: $priority, needsReply: $needsReply)
        }
        """
    public static let agentCategories = """
        query { ReadAgent { categories } }
        """
    public static let showMailImages = """
        mutation ($itemIds: [String!]!, $show: Boolean!) { ShowMailboxItemImages(itemIds: $itemIds, show: $show) }
        """
    public static let cancelAgentReply = """
        mutation ($replyId: String!) { CancelAgentReply(replyId: $replyId) { id status } }
        """
    public static let mailDraft = """
        query ($itemId: String!) {
          GetMailboxDraft(itemId: $itemId) {
            itemId mailId from to cc bcc subject html text replyToItemId forwardItemId
            attachments { index filename contentType size inline }
          }
        }
        """
    public static let saveMailDraft = """
        mutation ($mailboxId: String!, $message: MailboxMessageParametersInput!) {
          SaveMailboxDraft(mailboxId: $mailboxId, message: $message) { id }
        }
        """
    public static let draftReply = """
        mutation ($itemId: String!, $instructions: String) { DraftReply(itemId: $itemId, instructions: $instructions) { text } }
        """
    public static let sendMail = """
        mutation ($mailboxId: String!, $submissionId: String!, $message: MailboxMessageParametersInput!) {
          SendMailboxMessage(mailboxId: $mailboxId, submissionId: $submissionId, message: $message) {
            mail { id } item { id folderId }
          }
        }
        """
    public static let mailSubmission = """
        query ($mailboxId: String!, $submissionId: String!) {
          GetMailboxSubmission(mailboxId: $mailboxId, submissionId: $submissionId) { mailId sentItemId }
        }
        """
    public static let cancelMailSubmission = """
        mutation ($mailboxId: String!, $submissionId: String!) {
          CancelMailboxSubmission(mailboxId: $mailboxId, submissionId: $submissionId) { mailId sentItemId }
        }
        """
    static let subscriptionFields = """
        id key name from count unread lastAt lastItemId oneClick unsubscribe logoDomain
        requestedAt method failed error stripped mutedAt imagesAt
        """
    public static let mailSubscriptions = """
        query ($mailboxId: String!, $first: Int, $offset: Int, $left: Boolean, $matching: String) {
          ListMailboxSubscriptions(mailboxId: $mailboxId, first: $first, offset: $offset, left: $left, matching: $matching) {
            total subscribed left
            subscriptions { \(subscriptionFields) }
          }
        }
        """
    public static let readMailSubscription = """
        query ($mailboxId: String!, $key: String!) {
          ReadMailboxSubscription(mailboxId: $mailboxId, key: $key) {
            threadId subject truncated
            items { folderId folderName folderKind item { \(mailItemFields) } }
          }
        }
        """
    public static let showSubscriptionImages = """
        mutation ($mailboxId: String!, $key: String!, $show: Boolean!) {
          ShowMailboxSubscriptionImages(mailboxId: $mailboxId, key: $key, show: $show) { key imagesAt }
        }
        """
    public static let muteSubscription = """
        mutation ($mailboxId: String!, $key: String!, $muted: Boolean!) {
          MuteMailboxSubscription(mailboxId: $mailboxId, key: $key, muted: $muted) { key mutedAt }
        }
        """
    public static let unsubscribe = """
        mutation ($mailboxId: String!, $key: String!) {
          UnsubscribeMailboxSubscription(mailboxId: $mailboxId, key: $key) { key requestedAt method failed error }
        }
        """
    public static let mailInvitation = """
        query ($itemId: String!) {
          GetMailInvitation(itemId: $itemId) {
            id method summary location startsAt endsAt allDay cancelled organizer participation
            attendees { address name participation }
          }
        }
        """
    public static let answerInvitation = """
        mutation ($requestId: String!, $itemId: String!, $answer: String!) {
          AnswerMailInvitation(requestId: $requestId, itemId: $itemId, answer: $answer) { id participation }
        }
        """
    public static let setProposalStatus = """
        mutation ($itemId: String!, $index: Int!, $status: String!) {
          SetMailProposalStatus(itemId: $itemId, index: $index, status: $status) { proposals { status } }
        }
        """

    /// The mail documents, for the server's schema check.
    public static let mail: [String: String] = [
        "mailboxes": mailboxes, "mailItem": mailItem, "mailThreads": mailThreads, "mailThread": mailThread, "mailContent": mailContent,
        "setMailFlags": setMailFlags, "moveMail": moveMail, "deleteMail": deleteMail, "reportJunk": reportJunk,
        "emptyTrash": emptyTrash, "newestMailItems": newestMailItems, "sortMail": sortMail, "agentCategories": agentCategories,
        "showMailImages": showMailImages, "cancelAgentReply": cancelAgentReply, "mailDraft": mailDraft,
        "saveMailDraft": saveMailDraft, "draftReply": draftReply, "sendMail": sendMail, "mailSubmission": mailSubmission,
        "cancelMailSubmission": cancelMailSubmission, "mailSubscriptions": mailSubscriptions,
        "readMailSubscription": readMailSubscription, "showSubscriptionImages": showSubscriptionImages,
        "muteSubscription": muteSubscription, "unsubscribe": unsubscribe, "mailInvitation": mailInvitation,
        "answerInvitation": answerInvitation, "setProposalStatus": setProposalStatus,
    ]
}

/// MailOutgoing is a message being written, as the server takes it.
public struct MailOutgoing: Codable, Hashable, Sendable {
    public var from = ""
    public var to: [String] = []
    public var cc: [String] = []
    public var bcc: [String] = []
    public var subject = ""
    public var htmlContent = ""
    public var textContent = ""
    public var replyToItemId = ""
    public var forwardItemId = ""
    public var forwardAttachments: [Int] = []
    public var draftItemId = ""
    public var keepAttachments: [Int] = []

    public init() {}

    var variables: JSONValue {
        var fields: [String: JSONValue] = [
            "from": .string(from), "to": .array(to.map { .string($0) }), "cc": .array(cc.map { .string($0) }),
            "bcc": .array(bcc.map { .string($0) }), "subject": .string(subject), "htmlContent": .string(htmlContent),
            "textContent": .string(textContent),
        ]
        if !replyToItemId.isEmpty { fields["replyToItemId"] = .string(replyToItemId) }
        if !forwardItemId.isEmpty {
            fields["forwardItemId"] = .string(forwardItemId)
            fields["forwardAttachments"] = .array(forwardAttachments.map { .number(Double($0)) })
        }
        if !draftItemId.isEmpty {
            fields["draftItemId"] = .string(draftItemId)
            fields["keepAttachments"] = .array(keepAttachments.map { .number(Double($0)) })
        }
        return .object(fields)
    }
}

/// MailSending is a send whose answer may have been lost, kept on the phone
/// with its own id, so it is asked about before it is sent again.
public struct MailSending: Codable, Hashable, Sendable {
    public let mailboxId: String
    public let submissionId: String
    public let message: MailOutgoing

    public init(mailboxId: String, message: MailOutgoing) {
        self.mailboxId = mailboxId
        self.message = message
        self.submissionId = (0..<16).map { _ in String(format: "%02x", UInt8.random(in: 0...255)) }.joined()
    }
}

extension Connection {
    public func mailboxes() async throws -> [MailboxEntry] {
        struct Answer: Decodable, Sendable { let ListMailboxes: [MailboxEntry?] }
        return try await execute(Documents.mailboxes, as: Answer.self).ListMailboxes.compactMap { $0 }
    }

    /// A page of a folder's conversations, or of the whole mailbox's for
    /// Starred, Priority and searching everywhere.
    public func mailThreads(
        folderId: String?, mailboxId: String?, filter: MailFilter, isPriority: Bool, first: Int, offset: Int
    ) async throws -> MailThreadPage {
        struct Answer: Decodable, Sendable { let ListMailboxThreads: MailThreadPage }
        var variables: [String: JSONValue] = ["first": .number(Double(first)), "offset": .number(Double(offset))]
        if let folderId { variables["folderId"] = .string(folderId) }
        if let mailboxId { variables["mailboxId"] = .string(mailboxId) }
        if filter.isUnreadOnly { variables["unread"] = .bool(true) }
        if filter.isStarredOnly { variables["flagged"] = .bool(true) }
        if isPriority { variables["priority"] = .string("high") }
        for (key, value) in [("search", filter.searchText), ("from", filter.from), ("to", filter.to), ("subject", filter.subject)]
        where !value.trimmingCharacters(in: .whitespaces).isEmpty {
            variables[key] = .string(value.trimmingCharacters(in: .whitespaces))
        }
        let local = Calendar.current
        if let since = filter.since {
            variables["since"] = .string(CalendarDates.text(local.startOfDay(for: since)))
        }
        if let before = filter.before, let next = local.date(byAdding: .day, value: 1, to: local.startOfDay(for: before)) {
            variables["before"] = .string(CalendarDates.text(next))
        }
        if let hasAttachment = filter.hasAttachment { variables["hasAttachment"] = .bool(hasAttachment) }
        return try await execute(Documents.mailThreads, variables: variables, as: Answer.self).ListMailboxThreads
    }

    public func mailThread(itemId: String) async throws -> MailThreadView {
        struct Answer: Decodable, Sendable { let GetMailboxThread: MailThreadView }
        return try await execute(Documents.mailThread, variables: ["itemId": .string(itemId)], as: Answer.self).GetMailboxThread
    }

    public func mailItem(_ itemId: String) async throws -> MailItem {
        struct Answer: Decodable, Sendable { let GetMailboxItem: MailItem }
        return try await execute(Documents.mailItem, variables: ["itemId": .string(itemId)], as: Answer.self).GetMailboxItem
    }

    public func mailContent(mailId: String) async throws -> MailContent {
        struct Answer: Decodable, Sendable { let GetMailContent: MailContent }
        return try await execute(Documents.mailContent, variables: ["mailId": .string(mailId)], as: Answer.self).GetMailContent
    }

    public func setMailFlags(_ itemIds: [String], isSeen: Bool? = nil, isFlagged: Bool? = nil) async throws {
        struct Answer: Decodable, Sendable { let SetMailboxItemFlags: Int }
        var variables: [String: JSONValue] = ["itemIds": .array(itemIds.map { .string($0) })]
        if let isSeen { variables["seen"] = .bool(isSeen) }
        if let isFlagged { variables["flagged"] = .bool(isFlagged) }
        _ = try await execute(Documents.setMailFlags, variables: variables, as: Answer.self)
    }

    /// Moves messages; each comes back as a new item in its new folder.
    public func moveMail(_ itemIds: [String], to folderId: String) async throws -> [MailMoved] {
        struct Answer: Decodable, Sendable { let MoveMailboxItems: [MailMoved?] }
        return try await execute(Documents.moveMail, variables: ["itemIds": .array(itemIds.map { .string($0) }), "folderId": .string(folderId)], as: Answer.self)
            .MoveMailboxItems.compactMap { $0 }
    }

    /// Moves messages to Trash, or out of Trash for good.
    public func deleteMail(_ itemIds: [String]) async throws {
        struct Answer: Decodable, Sendable { let DeleteMailboxItems: Int }
        _ = try await execute(Documents.deleteMail, variables: ["itemIds": .array(itemIds.map { .string($0) })], as: Answer.self)
    }

    public func reportJunk(_ itemIds: [String], isNotJunk: Bool) async throws {
        struct Answer: Decodable, Sendable { let ReportMailboxJunk: Int }
        _ = try await execute(Documents.reportJunk, variables: ["itemIds": .array(itemIds.map { .string($0) }), "notJunk": .bool(isNotJunk)], as: Answer.self)
    }

    public func emptyTrash(mailboxId: String) async throws {
        struct Answer: Decodable, Sendable { let EmptyMailboxTrash: Int }
        _ = try await execute(Documents.emptyTrash, variables: ["mailboxId": .string(mailboxId)], as: Answer.self)
    }

    /// The newest items of a folder, for finding what an action put there.
    public func newestMailItems(folderId: String, first: Int = 200) async throws -> [MailMoved] {
        struct Answer: Decodable, Sendable {
            struct Page: Decodable, Sendable { let items: [MailMoved?] }
            let ListMailboxItems: Page?
        }
        return try await execute(Documents.newestMailItems, variables: ["folderId": .string(folderId), "first": .number(Double(first))], as: Answer.self)
            .ListMailboxItems?.items.compactMap { $0 } ?? []
    }

    public func sortMail(_ itemIds: [String], category: String?, priority: String?, needsReply: Bool?) async throws {
        struct Answer: Decodable, Sendable { let SortMailboxItems: Int }
        var variables: [String: JSONValue] = ["itemIds": .array(itemIds.map { .string($0) })]
        if let category { variables["category"] = .string(category) }
        if let priority { variables["priority"] = .string(priority) }
        if let needsReply { variables["needsReply"] = .bool(needsReply) }
        _ = try await execute(Documents.sortMail, variables: variables, as: Answer.self)
    }

    public func agentCategories() async throws -> [String] {
        struct Answer: Decodable, Sendable {
            struct Agent: Decodable, Sendable { let categories: [String]? }
            let ReadAgent: Agent?
        }
        return try await execute(Documents.agentCategories, as: Answer.self).ReadAgent?.categories ?? []
    }

    public func showMailImages(_ itemIds: [String]) async throws {
        struct Answer: Decodable, Sendable { let ShowMailboxItemImages: Int }
        _ = try await execute(Documents.showMailImages, variables: ["itemIds": .array(itemIds.map { .string($0) }), "show": .bool(true)], as: Answer.self)
    }

    public func cancelAgentReply(_ replyId: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Reply: Decodable, Sendable { let id: String }
            let CancelAgentReply: Reply?
        }
        _ = try await execute(Documents.cancelAgentReply, variables: ["replyId": .string(replyId)], as: Answer.self)
    }

    public func mailDraft(itemId: String) async throws -> MailDraft {
        struct Answer: Decodable, Sendable { let GetMailboxDraft: MailDraft }
        return try await execute(Documents.mailDraft, variables: ["itemId": .string(itemId)], as: Answer.self).GetMailboxDraft
    }

    /// Saves a draft and returns its item id.
    public func saveMailDraft(mailboxId: String, message: MailOutgoing) async throws -> String {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let id: String }
            let SaveMailboxDraft: Saved
        }
        return try await execute(Documents.saveMailDraft, variables: ["mailboxId": .string(mailboxId), "message": message.variables], as: Answer.self)
            .SaveMailboxDraft.id
    }

    public func draftReply(itemId: String, instructions: String) async throws -> String {
        struct Answer: Decodable, Sendable {
            struct Drafted: Decodable, Sendable { let text: String }
            let DraftReply: Drafted
        }
        var variables: [String: JSONValue] = ["itemId": .string(itemId)]
        if !instructions.isEmpty { variables["instructions"] = .string(instructions) }
        return try await execute(Documents.draftReply, variables: variables, as: Answer.self).DraftReply.text
    }

    /// Sends a message. One sent before whose answer was lost is asked about
    /// first and sent again only if the server never took it. Returns the
    /// sent item's id when the server says it.
    public func send(_ sending: MailSending, isRetry: Bool) async throws -> String? {
        struct Submission: Decodable, Sendable { let mailId: String; let sentItemId: String }
        if isRetry {
            struct Asked: Decodable, Sendable { let GetMailboxSubmission: Submission? }
            if let taken = try await execute(Documents.mailSubmission, variables: [
                "mailboxId": .string(sending.mailboxId), "submissionId": .string(sending.submissionId),
            ], as: Asked.self).GetMailboxSubmission {
                return taken.sentItemId
            }
        }
        struct Answer: Decodable, Sendable {
            struct Sent: Decodable, Sendable {
                struct Item: Decodable, Sendable { let id: String }
                let item: Item?
            }
            let SendMailboxMessage: Sent
        }
        return try await execute(Documents.sendMail, variables: [
            "mailboxId": .string(sending.mailboxId), "submissionId": .string(sending.submissionId), "message": sending.message.variables,
        ], as: Answer.self).SendMailboxMessage.item?.id
    }

    /// Stops a send; true when the server had already taken it.
    public func cancel(_ sending: MailSending) async throws -> Bool {
        struct Answer: Decodable, Sendable {
            struct Submission: Decodable, Sendable { let mailId: String }
            let CancelMailboxSubmission: Submission?
        }
        let answer = try await execute(Documents.cancelMailSubmission, variables: [
            "mailboxId": .string(sending.mailboxId), "submissionId": .string(sending.submissionId),
        ], as: Answer.self)
        return !(answer.CancelMailboxSubmission?.mailId ?? "").isEmpty
    }

    public func mailSubscriptions(mailboxId: String, isLeft: Bool, matching: String, first: Int, offset: Int) async throws -> MailSubscriptionPage {
        struct Answer: Decodable, Sendable { let ListMailboxSubscriptions: MailSubscriptionPage }
        var variables: [String: JSONValue] = [
            "mailboxId": .string(mailboxId), "first": .number(Double(first)), "offset": .number(Double(offset)), "left": .bool(isLeft),
        ]
        if !matching.isEmpty { variables["matching"] = .string(matching) }
        return try await execute(Documents.mailSubscriptions, variables: variables, as: Answer.self).ListMailboxSubscriptions
    }

    public func readMailSubscription(mailboxId: String, key: String) async throws -> MailThreadView {
        struct Answer: Decodable, Sendable {
            struct View: Decodable, Sendable {
                let threadId: String
                let subject: String
                let truncated: Bool
                let items: [MailThreadView.Entry?]
            }
            let ReadMailboxSubscription: View
        }
        let view = try await execute(Documents.readMailSubscription, variables: ["mailboxId": .string(mailboxId), "key": .string(key)], as: Answer.self)
            .ReadMailboxSubscription
        return MailThreadView(threadId: view.threadId, subject: view.subject, truncated: view.truncated, summary: nil, heldReply: nil, items: view.items)
    }

    public func showSubscriptionImages(mailboxId: String, key: String, isShown: Bool) async throws {
        struct Answer: Decodable, Sendable {
            struct Changed: Decodable, Sendable { let key: String }
            let ShowMailboxSubscriptionImages: Changed?
        }
        _ = try await execute(Documents.showSubscriptionImages, variables: [
            "mailboxId": .string(mailboxId), "key": .string(key), "show": .bool(isShown),
        ], as: Answer.self)
    }

    public func muteSubscription(mailboxId: String, key: String, isMuted: Bool) async throws {
        struct Answer: Decodable, Sendable {
            struct Changed: Decodable, Sendable { let key: String }
            let MuteMailboxSubscription: Changed?
        }
        _ = try await execute(Documents.muteSubscription, variables: [
            "mailboxId": .string(mailboxId), "key": .string(key), "muted": .bool(isMuted),
        ], as: Answer.self)
    }

    /// Leaves a list. A failure is recorded on the list rather than thrown.
    public func unsubscribe(mailboxId: String, key: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Changed: Decodable, Sendable { let key: String }
            let UnsubscribeMailboxSubscription: Changed?
        }
        _ = try await execute(Documents.unsubscribe, variables: ["mailboxId": .string(mailboxId), "key": .string(key)], as: Answer.self)
    }

    public func mailInvitation(itemId: String) async throws -> MailInvitation? {
        struct Answer: Decodable, Sendable { let GetMailInvitation: MailInvitation? }
        return try await execute(Documents.mailInvitation, variables: ["itemId": .string(itemId)], as: Answer.self).GetMailInvitation
    }

    /// Answers an invitation, with an id of its own so an answer whose reply
    /// was lost is not sent twice.
    public func answerInvitation(itemId: String, answer: String, requestId: String, isRetry: Bool) async throws {
        if isRetry {
            struct Asked: Decodable, Sendable {
                struct Request: Decodable, Sendable { let requestId: String }
                let GetCalendarRequest: Request?
            }
            if try await execute(Documents.calendarRequest, variables: ["requestId": .string(requestId)], as: Asked.self).GetCalendarRequest != nil {
                return
            }
        }
        struct Answer: Decodable, Sendable {
            struct Answered: Decodable, Sendable { let id: String }
            let AnswerMailInvitation: Answered?
        }
        _ = try await execute(Documents.answerInvitation, variables: [
            "requestId": .string(requestId), "itemId": .string(itemId), "answer": .string(answer),
        ], as: Answer.self)
    }

    public func setProposalStatus(itemId: String, index: Int, status: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Insight: Decodable, Sendable {}
            let SetMailProposalStatus: Insight?
        }
        _ = try await execute(Documents.setProposalStatus, variables: [
            "itemId": .string(itemId), "index": .number(Double(index)), "status": .string(status),
        ], as: Answer.self)
    }
}

extension Connection {
    /// Adds a file to a draft: to the one given, or to a new draft made
    /// around it. Returns the draft's item id and its attachments as the
    /// server now numbers them.
    public func attachToDraft(
        mailboxId: String, draftItemId: String?, name: String, contentType: String, data: Data
    ) async throws -> (itemId: String, attachments: [MailContent.Attachment]) {
        let boundary = "teanode-\(UUID().uuidString)"
        let path = draftItemId.map { "/api/v1/mailbox/drafts/\($0)/attachments" } ?? "/api/v1/mailbox/\(mailboxId)/drafts/attachments"
        var request = URLRequest(url: account.server.endpoint(path))
        request.httpMethod = draftItemId == nil ? "POST" : "PUT"
        request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        request.httpBody = Upload.body(boundary: boundary, name: name, contentType: contentType, data: data)
        let (answer, response) = try await send(authorizing: request)
        struct Uploaded: Decodable {
            let itemId: String?
            let attachments: [MailContent.Attachment]?
            let error: String?
        }
        let uploaded = try? JSONDecoder().decode(Uploaded.self, from: answer)
        guard response.statusCode == 200, let itemId = uploaded?.itemId else {
            if response.statusCode == 413 { throw TeaNodeError.refused("That file is larger than a message may be.") }
            throw TeaNodeError.refused(uploaded?.error ?? String(decoding: answer.prefix(300), as: UTF8.self))
        }
        return (itemId, uploaded?.attachments ?? [])
    }
}

/// MailMoved is where a message is now, after a move.
public struct MailMoved: Codable, Hashable, Sendable {
    public let id: String
    public let folderId: String
    public let mailId: String
    public let addedAt: String?
}
