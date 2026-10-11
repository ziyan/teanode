import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// AgentSettings is the person's agent as the dashboard's agent page reads
/// it: the agent's own settings, every mailbox, calendar and address book it
/// may be given, what the server offers, and today's budget.
public struct AgentSettings: Codable, Equatable, Sendable {
    /// Agent is the agent's own settings; there is none until the person
    /// turns it on.
    public struct Agent: Codable, Hashable, Sendable {
        public let id: String
        public let name: String
        public let enabled: Bool
        public let instructions: String?
        /// The language it talks in, and the one it works in; empty follows
        /// the account.
        public let language: String?
        public let knowledgeLanguage: String?
        /// The model for conversations; empty is the server's choice.
        public let askModel: String?
        /// The voice answers are read aloud in on a call; empty is the
        /// server's.
        public let speechVoice: String?
        /// When an operator switched it off; while set, the person cannot
        /// turn it on.
        public let operatorDisabledAt: String?
        /// The families and tools that always ask first.
        public let confirm: [String]
        /// The kinds of action needing the person's word that the agent may
        /// take when they are not there.
        public let unattendedAllowedRisks: [String]?
        public let isMemoryCheckEnabled: Bool
        public let isIdeasEnabled: Bool
        public let voice: Voice?
        public let categories: [Category]

        public var isOperatorDisabled: Bool { !(operatorDisabledAt ?? "").isEmpty }
    }

    /// Voice is how the agent sounds when it writes for the person.
    public struct Voice: Codable, Hashable, Sendable {
        /// formal or casual; empty is neutral.
        public let tone: String?
        /// short or long; empty is medium.
        public let length: String?
        public let greeting: String?
        public let signoff: String?

        public init(tone: String? = nil, length: String? = nil, greeting: String? = nil, signoff: String? = nil) {
            self.tone = tone
            self.length = length
            self.greeting = greeting
            self.signoff = signoff
        }
    }

    /// Category is one of the person's own categories, with the line that
    /// says what belongs in it.
    public struct Category: Codable, Hashable, Sendable {
        public var name: String
        public var description: String?

        public init(name: String = "", description: String? = "") {
            self.name = name
            self.description = description
        }
    }

    /// MailboxSource is one of the person's mailboxes, granted or not.
    public struct MailboxSource: Codable, Hashable, Sendable, Identifiable {
        public let mailboxId: String
        public let name: String
        public let addresses: [String]
        /// Null until the mailbox has been granted once.
        public let policy: MailboxPolicy?

        public var id: String { mailboxId }
        public var isGranted: Bool { policy?.granted == true }
        /// The policy the forms start from: the stored one, or the
        /// dashboard's default.
        public var shownPolicy: MailboxPolicy { policy ?? .defaultPolicy }
    }

    /// CollectionSource is one of the person's calendars or address books:
    /// one switch, no policy.
    public struct CollectionSource: Codable, Hashable, Sendable, Identifiable {
        public let id: String
        public let name: String
        /// calendar or addressBook.
        public let kind: String
        public let granted: Bool
        /// Events in the next month for a calendar, people for an address
        /// book.
        public let items: Int

        public var isCalendar: Bool { kind == "calendar" }
    }

    /// Allowed is what the server offers, resolved against what is
    /// configured.
    public struct Allowed: Codable, Hashable, Sendable {
        public let enabled: Bool
        public let triage: Bool
        public let summaries: Bool
        public let draftReplies: Bool
        public let search: Bool
        public let research: Bool
        public let autoReply: Bool
        public let ask: Bool
        public let schedules: Bool
        public let browser: Bool
        public let connectedServers: Bool

        /// Nothing offered: what a server that answers without saying is
        /// read as.
        public static let nothing = Allowed(
            enabled: false, triage: false, summaries: false, draftReplies: false, search: false, research: false, autoReply: false, ask: false,
            schedules: false, browser: false, connectedServers: false)
    }

    /// MailboxPolicy is what the agent does in one mailbox.
    public struct MailboxPolicy: Codable, Hashable, Sendable {
        /// Triage is the sorting.
        public struct Triage: Codable, Hashable, Sendable {
            public var enabled: Bool
            /// none, recent or all; empty is recent.
            public var backfill: String?
            /// direct or any; empty is direct.
            public var replyExpectation: String?

            public init(enabled: Bool = false, backfill: String? = nil, replyExpectation: String? = nil) {
                self.enabled = enabled
                self.backfill = backfill
                self.replyExpectation = replyExpectation
            }
        }

        /// Summaries is the summarizing.
        public struct Summaries: Codable, Hashable, Sendable {
            public var enabled: Bool
            /// How long a conversation is before it gets a summary; zero is
            /// three.
            public var minimumMessages: Int?
            /// brief or detailed; empty is brief.
            public var style: String?

            public init(enabled: Bool = false, minimumMessages: Int? = nil, style: String? = nil) {
                self.enabled = enabled
                self.minimumMessages = minimumMessages
                self.style = style
            }
        }

        /// Hours are the person's working hours, as "HH:MM", on the days
        /// given (0 is Sunday).
        public struct Hours: Codable, Hashable, Sendable {
            public var from: String
            public var until: String
            public var days: [Int]

            public init(from: String, until: String, days: [Int]) {
                self.from = from
                self.until = until
                self.days = days
            }

            /// What answering outside hours starts from: nine to half past
            /// five, Monday to Friday.
            public static let workingHours = Hours(from: "09:00", until: "17:30", days: [1, 2, 3, 4, 5])
        }

        /// AutoReply is the answering on the person's behalf.
        public struct AutoReply: Codable, Hashable, Sendable {
            public var enabled: Bool
            public var guidance: String?
            /// known, everyone or list; empty is known.
            public var scope: String?
            public var allow: [String]?
            public var never: [String]?
            /// Answer only in these categories; empty is any.
            public var categories: [String]?
            /// always, outsideHours or whenAway; empty is always.
            public var when: String?
            public var hours: Hours?
            /// How long a reply waits in Drafts; zero is ten.
            public var holdMinutes: Int?
            /// The most replies in a day; zero is twenty.
            public var dailyLimit: Int?

            public init(enabled: Bool = false) {
                self.enabled = enabled
            }
        }

        /// Alerts is whether what arrives may be told to the person unasked.
        public struct Alerts: Codable, Hashable, Sendable {
            public var enabled: Bool

            public init(enabled: Bool) {
                self.enabled = enabled
            }
        }

        public var granted: Bool
        public var triage: Triage?
        public var summaries: Summaries?
        public var draftReplies: Bool
        public var search: Bool
        public var research: Bool
        public var autoReply: AutoReply?
        public var alerts: Alerts?

        public init(
            granted: Bool, triage: Triage? = nil, summaries: Summaries? = nil, draftReplies: Bool, search: Bool, research: Bool,
            autoReply: AutoReply? = nil, alerts: Alerts? = nil
        ) {
            self.granted = granted
            self.triage = triage
            self.summaries = summaries
            self.draftReplies = draftReplies
            self.search = search
            self.research = research
            self.autoReply = autoReply
            self.alerts = alerts
        }

        /// The dashboard's policy for a mailbox never granted.
        public static let defaultPolicy = MailboxPolicy(granted: false, draftReplies: true, search: true, research: false)

        /// Alerts follow the sorting; a policy that never said is on.
        public var isAlertsEnabled: Bool { alerts?.enabled ?? true }

        /// The policy as the server takes it: granted, and the answering
        /// dropped when nothing in it is set, with working hours only when
        /// it answers outside them.
        public var cleaned: MailboxPolicy {
            var policy = self
            policy.granted = true
            let reply = autoReply ?? AutoReply()
            if reply.enabled || !(reply.guidance ?? "").isEmpty {
                var kept = reply
                kept.hours = reply.when == "outsideHours" ? reply.hours ?? .workingHours : nil
                policy.autoReply = kept
            } else {
                policy.autoReply = nil
            }
            return policy
        }

        var variables: JSONValue {
            var fields: [String: JSONValue] = [
                "granted": .bool(granted), "draftReplies": .bool(draftReplies), "search": .bool(search), "research": .bool(research),
            ]
            if let triage {
                fields["triage"] = .object([
                    "enabled": .bool(triage.enabled), "backfill": .string(triage.backfill ?? ""),
                    "replyExpectation": .string(triage.replyExpectation ?? ""),
                ])
            }
            if let summaries {
                fields["summaries"] = .object([
                    "enabled": .bool(summaries.enabled), "minimumMessages": .number(Double(summaries.minimumMessages ?? 0)),
                    "style": .string(summaries.style ?? ""),
                ])
            }
            if let reply = autoReply {
                var replyFields: [String: JSONValue] = [
                    "enabled": .bool(reply.enabled), "guidance": .string(reply.guidance ?? ""), "scope": .string(reply.scope ?? ""),
                    "allow": .array((reply.allow ?? []).map { .string($0) }), "never": .array((reply.never ?? []).map { .string($0) }),
                    "categories": .array((reply.categories ?? []).map { .string($0) }), "when": .string(reply.when ?? ""),
                    "holdMinutes": .number(Double(reply.holdMinutes ?? 0)), "dailyLimit": .number(Double(reply.dailyLimit ?? 0)),
                ]
                replyFields["hours"] =
                    reply.hours.map { hours in
                        .object(["from": .string(hours.from), "until": .string(hours.until), "days": .array(hours.days.map { .number(Double($0)) })])
                    } ?? .null
                fields["autoReply"] = .object(replyFields)
            }
            if let alerts {
                fields["alerts"] = .object(["enabled": .bool(alerts.enabled)])
            }
            return .object(fields)
        }
    }

    /// LanguageChoice is a language the agent may talk or work in, named in
    /// itself.
    public struct LanguageChoice: Hashable, Sendable, Identifiable {
        public let code: String
        public let name: String
        public var id: String { code }
    }

    public let agent: Agent?
    public let sources: [MailboxSource]
    public let collections: [CollectionSource]
    public let allowed: Allowed
    public let budget: AgentStatus.Budget?
    /// The models the person may pick for conversations.
    public let choices: [String]
    /// The zone and language the agent resolves for the person, and the
    /// language its notes are written in.
    public let timezone: String
    public let language: String
    public let knowledgeLanguage: String
    /// The fixed categories followed by the person's own.
    public let categories: [String]

    enum CodingKeys: String, CodingKey {
        case agent, sources, collections, allowed, budget, choices, timezone, language, knowledgeLanguage, categories
    }

    /// Reads what the server sent, taking what it left out as empty, so a
    /// server that answers with less is read as offering less.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        agent = try container.decodeIfPresent(Agent.self, forKey: .agent)
        sources = try container.decodeIfPresent([MailboxSource].self, forKey: .sources) ?? []
        collections = try container.decodeIfPresent([CollectionSource].self, forKey: .collections) ?? []
        allowed = try container.decodeIfPresent(Allowed.self, forKey: .allowed) ?? .nothing
        budget = try container.decodeIfPresent(AgentStatus.Budget.self, forKey: .budget)
        choices = try container.decodeIfPresent([String].self, forKey: .choices) ?? []
        timezone = try container.decodeIfPresent(String.self, forKey: .timezone) ?? ""
        language = try container.decodeIfPresent(String.self, forKey: .language) ?? ""
        knowledgeLanguage = try container.decodeIfPresent(String.self, forKey: .knowledgeLanguage) ?? ""
        categories = try container.decodeIfPresent([String].self, forKey: .categories) ?? []
    }

    /// The kinds of action needing the person's word, in the order the
    /// settings show them.
    public static let unattendedRisks = ["outward", "money", "destructive", "granting", "listed"]

    /// The languages offered by name; any other code may be typed.
    public static let languages: [LanguageChoice] = [
        LanguageChoice(code: "en", name: "English"), LanguageChoice(code: "de", name: "Deutsch"), LanguageChoice(code: "es", name: "Español"),
        LanguageChoice(code: "fr", name: "Français"), LanguageChoice(code: "it", name: "Italiano"), LanguageChoice(code: "ja", name: "日本語"),
        LanguageChoice(code: "ko", name: "한국어"), LanguageChoice(code: "nl", name: "Nederlands"), LanguageChoice(code: "pt", name: "Português"),
        LanguageChoice(code: "ru", name: "Русский"), LanguageChoice(code: "zh", name: "中文"),
    ]

    /// How many of the fixed categories the dashboard names in its hint.
    public static let namedFixedCategoryCount = 9

    /// The longest name a category may have.
    public static let categoryNameLength = 40

    /// The longest name the agent may have.
    public static let agentNameLength = 64

    /// The fixed categories, the first few, as the dashboard names them in
    /// its hint.
    public var fixedCategoryNames: [String] {
        Array(categories.prefix(Self.namedFixedCategoryCount))
    }

    /// One mailbox, by its id.
    public func source(_ mailboxId: String) -> MailboxSource? {
        sources.first { $0.mailboxId == mailboxId }
    }

    /// A list typed with commas or one to a line, as the server takes it.
    public static func splitList(_ text: String) -> [String] {
        text.split(whereSeparator: { $0 == "," || $0 == "\n" })
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
    }

    /// A list as it is typed.
    public static func joinList(_ entries: [String]?) -> String {
        (entries ?? []).joined(separator: ", ")
    }

    /// A number as it is typed; anything else, and nothing, is zero, which
    /// the server reads as its default.
    public static func wholeNumber(_ text: String) -> Int {
        max(0, Int(text.trimmingCharacters(in: .whitespaces)) ?? 0)
    }

    /// The hour and minute of an "HH:MM" time; nil for anything else.
    public static func clockParts(_ text: String) -> (hour: Int, minute: Int)? {
        let parts = text.split(separator: ":")
        guard parts.count == 2, let hour = Int(parts[0]), let minute = Int(parts[1]), (0..<24).contains(hour), (0..<60).contains(minute) else {
            return nil
        }
        return (hour, minute)
    }

    /// An hour and minute as "HH:MM".
    public static func clockText(hour: Int, minute: Int) -> String {
        let hourText = hour < 10 ? "0\(hour)" : "\(hour)"
        let minuteText = minute < 10 ? "0\(minute)" : "\(minute)"
        return "\(hourText):\(minuteText)"
    }
}

/// AgentChange is what one save changes about the agent; what is left nil
/// stays as it is.
public struct AgentChange: Hashable, Sendable {
    public var enabled: Bool?
    public var name: String?
    public var instructions: String?
    public var language: String?
    public var knowledgeLanguage: String?
    public var voice: AgentSettings.Voice?
    public var categories: [AgentSettings.Category]?
    public var confirm: [String]?
    public var unattendedAllowedRisks: [String]?
    public var askModel: String?
    public var speechVoice: String?
    public var isMemoryCheckEnabled: Bool?
    public var isIdeasEnabled: Bool?

    public init(
        enabled: Bool? = nil, name: String? = nil, instructions: String? = nil, language: String? = nil, knowledgeLanguage: String? = nil,
        voice: AgentSettings.Voice? = nil, categories: [AgentSettings.Category]? = nil, confirm: [String]? = nil,
        unattendedAllowedRisks: [String]? = nil, askModel: String? = nil, speechVoice: String? = nil, isMemoryCheckEnabled: Bool? = nil,
        isIdeasEnabled: Bool? = nil
    ) {
        self.enabled = enabled
        self.name = name
        self.instructions = instructions
        self.language = language
        self.knowledgeLanguage = knowledgeLanguage
        self.voice = voice
        self.categories = categories
        self.confirm = confirm
        self.unattendedAllowedRisks = unattendedAllowedRisks
        self.askModel = askModel
        self.speechVoice = speechVoice
        self.isMemoryCheckEnabled = isMemoryCheckEnabled
        self.isIdeasEnabled = isIdeasEnabled
    }

    /// As the server takes it, as the dashboard sends it: the name and the
    /// languages trimmed, every part of the voice said, and categories
    /// without a name left out.
    var variables: [String: JSONValue] {
        var variables: [String: JSONValue] = [:]
        if let enabled { variables["enabled"] = .bool(enabled) }
        if let name { variables["name"] = .string(name.trimmingCharacters(in: .whitespacesAndNewlines)) }
        if let instructions { variables["instructions"] = .string(instructions) }
        if let language { variables["language"] = .string(language.trimmingCharacters(in: .whitespacesAndNewlines)) }
        if let knowledgeLanguage { variables["knowledgeLanguage"] = .string(knowledgeLanguage.trimmingCharacters(in: .whitespacesAndNewlines)) }
        if let voice {
            variables["voice"] = .object([
                "tone": .string(voice.tone ?? ""), "length": .string(voice.length ?? ""), "greeting": .string(voice.greeting ?? ""),
                "signoff": .string(voice.signoff ?? ""),
            ])
        }
        if let categories {
            variables["categories"] = .array(
                categories.filter { !$0.name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }.map { category in
                    .object(["name": .string(category.name), "description": .string(category.description ?? "")])
                })
        }
        if let confirm { variables["confirm"] = .array(confirm.map { .string($0) }) }
        if let unattendedAllowedRisks { variables["unattendedAllowedRisks"] = .array(unattendedAllowedRisks.map { .string($0) }) }
        if let askModel { variables["askModel"] = .string(askModel) }
        if let speechVoice { variables["speechVoice"] = .string(speechVoice) }
        if let isMemoryCheckEnabled { variables["isMemoryCheckEnabled"] = .bool(isMemoryCheckEnabled) }
        if let isIdeasEnabled { variables["isIdeasEnabled"] = .bool(isIdeasEnabled) }
        return variables
    }
}

/// AgentVoiceDraft is how the agent writes, as its form holds it.
public struct AgentVoiceDraft: Hashable, Sendable {
    public var tone: String
    public var length: String
    public var greeting: String
    public var signoff: String

    public init(_ voice: AgentSettings.Voice?) {
        tone = voice?.tone ?? ""
        length = voice?.length ?? ""
        greeting = voice?.greeting ?? ""
        signoff = voice?.signoff ?? ""
    }

    public var voice: AgentSettings.Voice { AgentSettings.Voice(tone: tone, length: length, greeting: greeting, signoff: signoff) }

    /// The tones, the first being neutral.
    public static let tones = ["", "formal", "casual"]
    /// The lengths, the first being medium.
    public static let lengths = ["", "short", "long"]
}

/// AgentTool is one tool the agent may use, as the "ask me first" list
/// shows it.
public struct AgentTool: Codable, Hashable, Sendable, Identifiable {
    public let name: String
    public let family: String
    public let risk: String
    public let description: String
    /// Whether it asks every time, whatever the list says.
    public let confirms: Bool
    public let core: Bool
    /// The verbs a tool takes, for the tools that are one thing with
    /// several.
    public let actions: [String]?

    public var id: String { name }

    /// The families, in the order their tools first come.
    public static func families(_ tools: [AgentTool]) -> [String] {
        var families: [String] = []
        for tool in tools where !families.contains(tool.family) {
            families.append(tool.family)
        }
        return families
    }
}

/// AgentConfirmDraft is the "ask me first" list as its form holds it: the
/// names of the families and tools that must ask before they run. A tool
/// that is not named follows its family.
public struct AgentConfirmDraft: Hashable, Sendable {
    /// The names, in the order they were added, each once.
    public private(set) var askFirstNames: [String]

    public init(confirm: [String]) {
        var names: [String] = []
        for name in confirm where !names.contains(name) {
            names.append(name)
        }
        askFirstNames = names
    }

    public func isAskingFirst(_ name: String) -> Bool { askFirstNames.contains(name) }

    public mutating func setAskingFirst(_ name: String, isAskingFirst: Bool) {
        if isAskingFirst {
            if !askFirstNames.contains(name) { askFirstNames.append(name) }
        } else {
            askFirstNames.removeAll { $0 == name }
        }
    }

    /// Whether a tool asks first, by its own name or its family's.
    public func doesToolAskFirst(_ tool: AgentTool) -> Bool {
        isAskingFirst(tool.name) || isAskingFirst(tool.family)
    }
}

/// AgentUnattendedDraft is which kinds of action the agent may take when
/// the person is not there, as its form holds it.
public struct AgentUnattendedDraft: Hashable, Sendable {
    public var allowedRisks: Set<String>

    public init(_ agent: AgentSettings.Agent?) {
        allowedRisks = Set(agent?.unattendedAllowedRisks ?? [])
    }

    public func isAllowed(_ unattendedRisk: String) -> Bool { allowedRisks.contains(unattendedRisk) }

    public mutating func setAllowed(_ unattendedRisk: String, isAllowed: Bool) {
        if isAllowed {
            allowedRisks.insert(unattendedRisk)
        } else {
            allowedRisks.remove(unattendedRisk)
        }
    }

    /// As the server takes them, in the order the settings show them.
    public var orderedAllowedRisks: [String] { AgentSettings.unattendedRisks.filter { allowedRisks.contains($0) } }
}

extension Documents {
    /// What every read and change of the agent answers with.
    static let agentSettingsFields = """
        agent { id name enabled instructions language knowledgeLanguage askModel speechVoice operatorDisabledAt confirm unattendedAllowedRisks
          isMemoryCheckEnabled isIdeasEnabled
          voice { tone length greeting signoff }
          categories { name description } }
        sources { mailboxId name addresses policy { granted draftReplies search research
          triage { enabled backfill replyExpectation }
          summaries { enabled minimumMessages style }
          autoReply { enabled guidance scope allow never categories when hours { from until days } holdMinutes dailyLimit }
          alerts { enabled } } }
        collections { id name kind granted items }
        allowed { enabled triage summaries draftReplies search research autoReply ask schedules browser connectedServers }
        budget { used limit resetsAt cost costLimit currency }
        choices timezone language knowledgeLanguage categories
        """
    public static let agentSettings = """
        query { ReadAgent { \(agentSettingsFields) } }
        """
    public static let updateAgentSettings = """
        mutation ($enabled: Boolean, $name: String, $instructions: String, $language: String, $knowledgeLanguage: String, $voice: AgentVoiceInput,
          $categories: [AgentCategoryInput!], $confirm: [String!], $unattendedAllowedRisks: [String!], $askModel: String, $speechVoice: String,
          $isMemoryCheckEnabled: Boolean, $isIdeasEnabled: Boolean, $forget: Boolean) {
          UpdateAgent(enabled: $enabled, name: $name, instructions: $instructions, language: $language, knowledgeLanguage: $knowledgeLanguage,
            voice: $voice, categories: $categories, confirm: $confirm, unattendedAllowedRisks: $unattendedAllowedRisks, askModel: $askModel,
            speechVoice: $speechVoice, isMemoryCheckEnabled: $isMemoryCheckEnabled, isIdeasEnabled: $isIdeasEnabled, forget: $forget) {
            \(agentSettingsFields)
          }
        }
        """
    public static let grantAgentMailbox = """
        mutation ($mailboxId: String!, $policy: AgentMailboxInput) {
          GrantAgentMailbox(mailboxId: $mailboxId, policy: $policy) { \(agentSettingsFields) }
        }
        """
    public static let revokeAgentMailbox = """
        mutation ($mailboxId: String!) { RevokeAgentMailbox(mailboxId: $mailboxId) { \(agentSettingsFields) } }
        """
    public static let grantAgentCollection = """
        mutation ($kind: String!, $id: String!, $granted: Boolean!) {
          GrantAgentSource(kind: $kind, id: $id, granted: $granted) { \(agentSettingsFields) }
        }
        """
    public static let agentTools = """
        query { ListAgentTools { name family risk description confirms core actions } }
        """
    public static let speechVoices = """
        query { ReadAgentVoice { isVoiceAvailable sampleRate speechVoice speechVoices serverSpeechVoice } }
        """
}

extension Connection {
    /// The agent and everything its settings show.
    public func agentSettings() async throws -> AgentSettings {
        struct Answer: Decodable, Sendable { let ReadAgent: AgentSettings }
        return try await execute(Documents.agentSettings, as: Answer.self).ReadAgent
    }

    /// Changes the agent, making it if there is none, and answers with the
    /// settings as they are now.
    public func updateAgent(_ change: AgentChange) async throws -> AgentSettings {
        struct Answer: Decodable, Sendable { let UpdateAgent: AgentSettings }
        return try await execute(Documents.updateAgentSettings, variables: change.variables, as: Answer.self).UpdateAgent
    }

    /// Deletes the agent and everything it learned; the mail stays.
    public func forgetAgent() async throws -> AgentSettings {
        struct Answer: Decodable, Sendable { let UpdateAgent: AgentSettings }
        return try await execute(Documents.updateAgentSettings, variables: ["forget": .bool(true)], as: Answer.self).UpdateAgent
    }

    /// Lets the agent reach a mailbox. With no policy the server sets its
    /// default the first time and keeps the stored one after that; with
    /// one, it replaces the stored one.
    public func grantAgentMailbox(_ mailboxId: String, policy: AgentSettings.MailboxPolicy? = nil) async throws -> AgentSettings {
        struct Answer: Decodable, Sendable { let GrantAgentMailbox: AgentSettings }
        var variables: [String: JSONValue] = ["mailboxId": .string(mailboxId)]
        if let policy { variables["policy"] = policy.cleaned.variables }
        return try await execute(Documents.grantAgentMailbox, variables: variables, as: Answer.self).GrantAgentMailbox
    }

    /// Stops the agent reaching a mailbox: its queued work is cancelled,
    /// what it sorted stays.
    public func revokeAgentMailbox(_ mailboxId: String) async throws -> AgentSettings {
        struct Answer: Decodable, Sendable { let RevokeAgentMailbox: AgentSettings }
        return try await execute(Documents.revokeAgentMailbox, variables: ["mailboxId": .string(mailboxId)], as: Answer.self).RevokeAgentMailbox
    }

    /// Lets the agent read a calendar or an address book, or stops it.
    public func grantAgentCollection(_ collection: AgentSettings.CollectionSource, isGranted: Bool) async throws -> AgentSettings {
        struct Answer: Decodable, Sendable { let GrantAgentSource: AgentSettings }
        return try await execute(
            Documents.grantAgentCollection, variables: ["kind": .string(collection.kind), "id": .string(collection.id), "granted": .bool(isGranted)],
            as: Answer.self
        ).GrantAgentSource
    }

    /// Every tool the agent may use, for the "ask me first" list.
    public func agentTools() async throws -> [AgentTool] {
        struct Answer: Decodable, Sendable { let ListAgentTools: [AgentTool] }
        return try await execute(Documents.agentTools, as: Answer.self).ListAgentTools
    }

    /// Whether the server speaks, and the voices the person may choose.
    public func speechVoices() async throws -> VoiceAvailability {
        struct Answer: Decodable, Sendable { let ReadAgentVoice: VoiceAvailability }
        return try await execute(Documents.speechVoices, as: Answer.self).ReadAgentVoice
    }

    /// A few words in a voice, as a WAV file, to hear it before choosing.
    /// It is paid for like any speech.
    public func speechVoiceSample(_ speechVoice: String) async throws -> Data {
        var components = URLComponents(url: account.server.endpoint("/api/v1/agent/voice/sample"), resolvingAgainstBaseURL: false)
        components?.queryItems = [URLQueryItem(name: "speechVoice", value: speechVoice)]
        guard let address = components?.url else { throw TeaNodeError.unreadableAnswer }
        let (sampleData, response) = try await send(authorizing: URLRequest(url: address))
        guard response.statusCode == 200 else {
            struct Refusal: Decodable { let error: String? }
            let refusalReason = (try? JSONDecoder().decode(Refusal.self, from: sampleData))?.error
            throw TeaNodeError.refused(refusalReason ?? "The sample could not be played (\(response.statusCode)).")
        }
        return sampleData
    }
}
