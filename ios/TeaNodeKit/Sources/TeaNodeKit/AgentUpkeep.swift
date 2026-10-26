import Foundation

// The agent page's upkeep tabs, as the dashboard has them: the sources it
// reads, its memory and the checks of it, the dreams, the schedules, the
// alerts muted, the connections, and what it did on its own. The documents
// are the dashboard's, field for field, so a phone and a browser change the
// same things in the same way.

// MARK: - The agent's own settings

/// AgentUpkeepPreferences is the part of the agent's settings these tabs
/// change: when it may dream and whether it is bootstrapping, and whether,
/// when and how often it alerts the person.
public struct AgentUpkeepPreferences: Codable, Hashable, Sendable {
    public let id: String
    public let enabled: Bool
    public let operatorDisabledAt: String?
    public let dreamFrom: String?
    public let dreamUntil: String?
    public let dreamBootstrap: Bool?
    public let isAlertsEnabled: Bool
    public let alertQuietStart: String?
    public let alertQuietEnd: String?
    public let alertDailyMost: Int
    public let lookAroundWeekday: String?
    public let lookAroundTime: String?

    public init(
        id: String, enabled: Bool = true, operatorDisabledAt: String? = nil, dreamFrom: String? = nil, dreamUntil: String? = nil,
        dreamBootstrap: Bool? = nil, isAlertsEnabled: Bool = true, alertQuietStart: String? = nil, alertQuietEnd: String? = nil,
        alertDailyMost: Int = 0, lookAroundWeekday: String? = nil, lookAroundTime: String? = nil
    ) {
        self.id = id
        self.enabled = enabled
        self.operatorDisabledAt = operatorDisabledAt
        self.dreamFrom = dreamFrom
        self.dreamUntil = dreamUntil
        self.dreamBootstrap = dreamBootstrap
        self.isAlertsEnabled = isAlertsEnabled
        self.alertQuietStart = alertQuietStart
        self.alertQuietEnd = alertQuietEnd
        self.alertDailyMost = alertDailyMost
        self.lookAroundWeekday = lookAroundWeekday
        self.lookAroundTime = lookAroundTime
    }

    /// Whether the agent runs at all: on, and not turned off by the
    /// operator. Alerts are listed only then.
    public var isRunning: Bool { enabled && (operatorDisabledAt ?? "").isEmpty }

    /// The hours a dream may start in, with the server's defaults where
    /// none are set.
    public var shownDreamFrom: String { UpkeepClock.orDefault(dreamFrom, "01:00") }
    public var shownDreamUntil: String { UpkeepClock.orDefault(dreamUntil, "06:00") }

    /// The alerts' quiet hours and daily most, with the defaults the
    /// dashboard shows where none are set.
    public var shownAlertQuietStart: String { UpkeepClock.orDefault(alertQuietStart, "22:00") }
    public var shownAlertQuietEnd: String { UpkeepClock.orDefault(alertQuietEnd, "07:00") }
    public var shownAlertDailyMost: Int { alertDailyMost > 0 ? alertDailyMost : 5 }

    /// The days the weekly look around may be set to, as the server keeps
    /// them, Monday first.
    public static let lookAroundWeekdays = ["monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"]

    /// The day and time the agent looks around the person's town, with the
    /// defaults the dashboard shows where none are set.
    public var shownLookAroundWeekday: String {
        Self.lookAroundWeekdays.contains(lookAroundWeekday ?? "") ? lookAroundWeekday ?? "thursday" : "thursday"
    }
    public var shownLookAroundTime: String { UpkeepClock.orDefault(lookAroundTime, "17:00") }
}

/// UpkeepClock reads and writes the "HH:MM" times the agent's hours are
/// kept in, in the person's own zone.
public enum UpkeepClock {
    /// The hour and minute a time says, or nil for anything else.
    public static func hourAndMinute(_ text: String) -> (hour: Int, minute: Int)? {
        let parts = text.split(separator: ":")
        guard parts.count == 2, let hour = Int(parts[0]), let minute = Int(parts[1]), (0..<24).contains(hour), (0..<60).contains(minute)
        else { return nil }
        return (hour, minute)
    }

    /// An hour and a minute as the server keeps them.
    public static func text(hour: Int, minute: Int) -> String {
        String(format: "%02d:%02d", hour, minute)
    }

    static func orDefault(_ text: String?, _ fallback: String) -> String {
        guard let text, hourAndMinute(text) != nil else { return fallback }
        return text
    }
}

// MARK: - Sources

extension KnowledgeSource {
    /// Specification is what a source was added as: the installed type
    /// and what was filled in for it, or the folder, the computer and the
    /// format for one added without a type, or the mailbox of sent mail.
    public struct Specification: Codable, Hashable, Sendable {
        public var type: String
        public var settings: JSONValue?
        public var computer: String
        public var path: String
        public var format: String
        public var mailboxId: String
        public var readEveryCheckout: Bool

        enum CodingKeys: String, CodingKey {
            case type, settings, computer, path, format, mailboxId, readEveryCheckout
        }

        public init(
            type: String = "", settings: JSONValue? = nil, computer: String = "", path: String = "", format: String = "", mailboxId: String = "",
            readEveryCheckout: Bool = false
        ) {
            self.type = type
            self.settings = settings
            self.computer = computer
            self.path = path
            self.format = format
            self.mailboxId = mailboxId
            self.readEveryCheckout = readEveryCheckout
        }

        public init(from decoder: Decoder) throws {
            let container = try decoder.container(keyedBy: CodingKeys.self)
            type = try container.decodeIfPresent(String.self, forKey: .type) ?? ""
            settings = try container.decodeIfPresent(JSONValue.self, forKey: .settings)
            computer = try container.decodeIfPresent(String.self, forKey: .computer) ?? ""
            path = try container.decodeIfPresent(String.self, forKey: .path) ?? ""
            format = try container.decodeIfPresent(String.self, forKey: .format) ?? ""
            mailboxId = try container.decodeIfPresent(String.self, forKey: .mailboxId) ?? ""
            readEveryCheckout = try container.decodeIfPresent(Bool.self, forKey: .readEveryCheckout) ?? false
        }

        /// What was filled in for the source's type, as an object. An older
        /// server sent it as the text of one.
        public var storedSettings: [String: JSONValue] {
            KnowledgeSourceType.storedSettings(settings)
        }
    }

    /// Shape is what a source added without a type is to the person: what
    /// the server files it under (the kind) and what the computer reads it
    /// with (the format), as one choice.
    public enum Shape: String, CaseIterable, Sendable {
        case files, journal, records, sent

        public var kind: String {
            switch self {
            case .files: "computer"
            case .journal, .records: "archive"
            case .sent: "sent"
            }
        }

        public var format: String {
            switch self {
            case .files: "files"
            case .journal: "journal"
            case .records: "records"
            case .sent: ""
            }
        }
    }

    /// The shape a stored source has: sent mail by its kind, anything on a
    /// computer by its format, and a format this app does not know as
    /// files, which is what the computer reads when a source says nothing.
    public var shape: Shape {
        if kind == "sent" { return .sent }
        switch specification.format {
        case "journal": return .journal
        case "records": return .records
        default: return .files
        }
    }

    /// Whether the source is a bank or card linked on the Finance tab,
    /// which is listed, synced and removed there instead.
    public var isFinance: Bool { kind == "finance" }

    /// The line under a source's name that says where it reads: the
    /// mailbox of sent mail by its name, the folder and the computer, or
    /// for a type of commands what was filled in for it.
    public func whereItReads(mailboxNames: [String: String]) -> String {
        if kind == "sent", !specification.mailboxId.isEmpty {
            return mailboxNames[specification.mailboxId] ?? ""
        }
        var whereText = specification.path
        if whereText.isEmpty, !specification.type.isEmpty {
            whereText = specification.storedSettings
                .sorted { $0.key < $1.key }
                .compactMap { settingName, settingValue -> String? in
                    switch settingValue {
                    case .null, .bool(false): return nil
                    case .string(let text) where text.isEmpty: return nil
                    case .array(let items) where items.isEmpty: return nil
                    default: return "\(KnowledgeSourceType.label(settingName)): \(KnowledgeSourceType.text(of: settingValue))"
                    }
                }
                .joined(separator: " · ")
        }
        return [whereText, specification.computer].filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

/// KnowledgeSourceAttachments is what became of one source's pictures and
/// files: how many wait for a dream to decide about them, how many it
/// decided against opening, and how many it opened and read.
public struct KnowledgeSourceAttachments: Codable, Hashable, Sendable {
    public let sourceId: String
    public let undecided: Int
    public let declined: Int
    public let described: Int

    public init(sourceId: String, undecided: Int = 0, declined: Int = 0, described: Int = 0) {
        self.sourceId = sourceId
        self.undecided = undecided
        self.declined = declined
        self.described = described
    }

    public var hasAny: Bool { undecided > 0 || declined > 0 || described > 0 }
}

/// DeclinedAttachment is one file a source's reading decided against
/// opening, with the reason it gave.
public struct DeclinedAttachment: Codable, Hashable, Sendable, Identifiable {
    public let documentId: String
    public let name: String
    public let contentType: String
    public let channel: String
    public let thread: String
    /// Where the file's bytes are on the server, or empty where they are not
    /// kept.
    public let path: String
    public let declined: String

    public init(documentId: String, name: String, contentType: String = "", channel: String = "", thread: String = "", path: String = "", declined: String) {
        self.documentId = documentId
        self.name = name
        self.contentType = contentType
        self.channel = channel
        self.thread = thread
        self.path = path
        self.declined = declined
    }

    public var id: String { documentId }

    /// The thread and the channel it came from, where it says.
    public var placeText: String {
        [thread, channel].filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }.joined(separator: " · ")
    }

    /// The files in the order they came, each run gathered under the reason
    /// it shares, so the reason is said once rather than once a file.
    public static func groupedByReason(_ files: [DeclinedAttachment]) -> [(declinedReason: String, files: [DeclinedAttachment])] {
        var order: [String] = []
        var byReason: [String: [DeclinedAttachment]] = [:]
        for file in files {
            if byReason[file.declined] == nil {
                order.append(file.declined)
                byReason[file.declined] = []
            }
            byReason[file.declined]?.append(file)
        }
        return order.map { (declinedReason: $0, files: byReason[$0] ?? []) }
    }
}

/// KnowledgeSourceType is an installed type of source: a file of
/// declarations saying how a source is read, and the settings and secrets a
/// source of it is added with.
public struct KnowledgeSourceType: Codable, Hashable, Sendable, Identifiable {
    /// Setting is one value a source of the type is added with.
    public struct Setting: Codable, Hashable, Sendable {
        public let name: String
        public let description: String
        /// string, path, array (a list of text), boolean or integer.
        public let settingType: String
        public let pattern: String
        public let itemPattern: String
        /// What leaving it unset means; null when there is no default.
        public let defaultValue: JSONValue?
        public let isRequired: Bool
        public let minimum: Int?
        public let maximum: Int?

        enum CodingKeys: String, CodingKey {
            case name, description, settingType, pattern, itemPattern, isRequired, minimum, maximum
            case defaultValue = "default"
        }

        public init(
            name: String, description: String = "", settingType: String = "string", pattern: String = "", itemPattern: String = "",
            defaultValue: JSONValue? = nil, isRequired: Bool = false, minimum: Int? = nil, maximum: Int? = nil
        ) {
            self.name = name
            self.description = description
            self.settingType = settingType
            self.pattern = pattern
            self.itemPattern = itemPattern
            self.defaultValue = defaultValue
            self.isRequired = isRequired
            self.minimum = minimum
            self.maximum = maximum
        }

        public var isBoolean: Bool { settingType == "boolean" }

        /// The words a pattern such as ^(mine|public|all)$ allows, for a
        /// setting that takes one of a few; nil for any other pattern.
        public var choices: [String]? { KnowledgeSourceType.choices(in: pattern) }

        /// What is wrong with what was typed for it, or nil. Said before the
        /// form is sent, so the person hears which box it is.
        public func problem(in draft: Draft) -> SettingProblem? {
            if isBoolean { return nil }
            let text = (draft.texts[name] ?? "").trimmingCharacters(in: .whitespaces)
            if text.isEmpty { return isRequired ? .missing : nil }
            if settingType == "integer" {
                guard let number = Int(text) else { return .notWhole }
                if let minimum, number < minimum { return .tooSmall(minimum) }
                if let maximum, number > maximum { return .tooLarge(maximum) }
                return nil
            }
            if settingType == "array" {
                let refused = KnowledgeSourceType.listOf(text).first { !KnowledgeSourceType.matches(itemPattern, $0) }
                return refused.map { .itemRefused($0) }
            }
            return KnowledgeSourceType.matches(pattern, text) ? nil : .refused
        }
    }

    /// Secret is a value each source of the type keeps sealed, such as a
    /// token, filled in on the source and never shown again.
    public struct Secret: Codable, Hashable, Sendable {
        public let key: String
        public let description: String
        public let isOptional: Bool

        public init(key: String, description: String = "", isOptional: Bool = false) {
            self.key = key
            self.description = description
            self.isOptional = isOptional
        }
    }

    /// SettingProblem is what is wrong with one setting's box.
    public enum SettingProblem: Hashable, Sendable {
        case missing
        case notWhole
        case tooSmall(Int)
        case tooLarge(Int)
        case itemRefused(String)
        case refused
    }

    /// Draft is what a source's settings hold while they are typed: the
    /// text in each box, and whether each switch is on.
    public struct Draft: Hashable, Sendable {
        public var texts: [String: String] = [:]
        public var switches: [String: Bool] = [:]

        public init(texts: [String: String] = [:], switches: [String: Bool] = [:]) {
            self.texts = texts
            self.switches = switches
        }
    }

    public let name: String
    public let description: String
    public let isLocal: Bool
    /// The reader built into the server that reads it (files, journal,
    /// sent, web), or empty for a type that runs commands on a computer.
    public let reader: String
    public let requires: [String]
    public let readable: Bool
    public let problem: String?
    public let settings: [Setting]
    public let secrets: [Secret]

    public init(
        name: String, description: String = "", isLocal: Bool = false, reader: String = "", requires: [String] = [], readable: Bool = true,
        problem: String? = nil, settings: [Setting] = [], secrets: [Secret] = []
    ) {
        self.name = name
        self.description = description
        self.isLocal = isLocal
        self.reader = reader
        self.requires = requires
        self.readable = readable
        self.problem = problem
        self.settings = settings
        self.secrets = secrets
    }

    public var id: String { name }

    /// Whether a source of the type is read on one of the person's
    /// computers, and so has to say which.
    public var isReadOnComputer: Bool { reader == "files" || reader == "journal" || reader.isEmpty }

    /// Whether it reads the person's own sent mail, whose mailbox is chosen
    /// from their mailboxes rather than typed.
    public var isSentMail: Bool { reader == "sent" }

    /// The readers built into the server, in the order a person is offered
    /// them.
    static let builtInReaders = ["files", "journal", "sent", "web"]

    /// The types a person may add a source of, in the order offered: the
    /// built-in readers first, then the types that run commands, each by
    /// name. A type whose file no longer parses is left out.
    public static func offered(_ sourceTypes: [KnowledgeSourceType]) -> [KnowledgeSourceType] {
        func rank(_ sourceType: KnowledgeSourceType) -> Int {
            builtInReaders.firstIndex(of: sourceType.reader) ?? builtInReaders.count
        }
        return sourceTypes.filter(\.readable).sorted {
            rank($0) != rank($1) ? rank($0) < rank($1) : $0.name.localizedStandardCompare($1.name) == .orderedAscending
        }
    }

    /// The form's boxes filled in from what a source already says, or empty
    /// for a new one so each shows its default as a placeholder.
    public func draft(from stored: [String: JSONValue] = [:]) -> Draft {
        var draft = Draft()
        for setting in settings {
            let storedValue = stored[setting.name]
            if setting.isBoolean {
                if case .bool(let isOn) = storedValue {
                    draft.switches[setting.name] = isOn
                } else {
                    draft.switches[setting.name] = setting.defaultValue == .bool(true)
                }
            } else {
                draft.texts[setting.name] = Self.text(of: storedValue)
            }
        }
        return draft
    }

    /// The first setting with something wrong in its box, and what, leaving
    /// out the ones named in skipped.
    public func firstProblem(in draft: Draft, skipping skipped: Set<String> = []) -> (setting: Setting, problem: SettingProblem)? {
        for setting in settings where !skipped.contains(setting.name) {
            if let problem = setting.problem(in: draft) { return (setting, problem) }
        }
        return nil
    }

    /// The settings as the server takes them. A box left empty is left
    /// out, so the type's default applies; a switch is always said.
    public func values(from draft: Draft) -> [String: JSONValue] {
        var settingValues: [String: JSONValue] = [:]
        for setting in settings {
            if setting.isBoolean {
                settingValues[setting.name] = .bool(draft.switches[setting.name] == true)
                continue
            }
            let text = (draft.texts[setting.name] ?? "").trimmingCharacters(in: .whitespaces)
            if text.isEmpty { continue }
            switch setting.settingType {
            case "integer": settingValues[setting.name] = Int(text).map { .number(Double($0)) } ?? .string(text)
            case "array": settingValues[setting.name] = .array(Self.listOf(text).map { .string($0) })
            default: settingValues[setting.name] = .string(text)
            }
        }
        return settingValues
    }

    /// A source's settings as an object, from the object itself or from the
    /// text of one.
    public static func storedSettings(_ stored: JSONValue?) -> [String: JSONValue] {
        switch stored {
        case .object(let object):
            return object
        case .string(let text):
            guard let parsed = try? JSONDecoder().decode(JSONValue.self, from: Data(text.utf8)), case .object(let object) = parsed else { return [:] }
            return object
        default:
            return [:]
        }
    }

    /// A JSON value as a person would type it: a list as its items
    /// separated by commas.
    public static func text(of settingValue: JSONValue?) -> String {
        switch settingValue {
        case .none, .null: return ""
        case .string(let text): return text
        case .bool(let isOn): return isOn ? "true" : "false"
        case .number(let number): return number == number.rounded() && abs(number) < 1e15 ? String(Int(number)) : String(number)
        case .array(let items): return items.map { text(of: $0) }.joined(separator: ", ")
        case .object:
            guard let encoded = try? JSONEncoder().encode(settingValue) else { return "" }
            return String(decoding: encoded, as: UTF8.self)
        }
    }

    static func listOf(_ text: String) -> [String] {
        text.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
    }

    /// Whether text matches one of the type's patterns, which are written
    /// for the server's expressions. A pattern this phone cannot compile is
    /// left to the server to judge.
    static func matches(_ pattern: String, _ text: String) -> Bool {
        guard !pattern.isEmpty, let expression = try? NSRegularExpression(pattern: pattern) else { return true }
        return expression.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)) != nil
    }

    static let acronyms: Set<String> = ["url", "id", "api", "cql", "mb"]

    /// A setting's name as a label: its words split and the first one
    /// capitalized, a run of capitals such as MB kept whole.
    public static func label(_ name: String) -> String {
        var words: [String] = []
        var current = ""
        let characters = Array(name)
        for (index, character) in characters.enumerated() {
            let previous = index > 0 ? characters[index - 1] : nil
            let next = index + 1 < characters.count ? characters[index + 1] : nil
            let startsWord =
                character.isUppercase
                && ((previous.map { $0.isLowercase || $0.isNumber } ?? false)
                    || ((previous?.isUppercase ?? false) && (next?.isLowercase ?? false)))
            if startsWord, !current.isEmpty {
                words.append(current)
                current = ""
            }
            current.append(character)
        }
        if !current.isEmpty { words.append(current) }
        let shown = words.enumerated().map { index, word -> String in
            if acronyms.contains(word.lowercased()) { return word.uppercased() }
            if index > 0, !(word.count >= 2 && word.allSatisfy(\.isUppercase)) { return word.lowercased() }
            return word
        }
        let joined = shown.joined(separator: " ")
        return joined.prefix(1).uppercased() + joined.dropFirst()
    }

    /// A description written as a phrase, as a sentence.
    public static func asSentence(_ description: String) -> String {
        let trimmed = description.trimmingCharacters(in: .whitespacesAndNewlines)
        let capitalized = trimmed.prefix(1).uppercased() + trimmed.dropFirst()
        guard let last = capitalized.last else { return capitalized }
        return ".!?)".contains(last) ? capitalized : capitalized + "."
    }

    /// The words a pattern such as ^(mine|public|all)$ allows.
    public static func choices(in pattern: String) -> [String]? {
        guard pattern.hasPrefix("^("), pattern.hasSuffix(")$") else { return nil }
        let inner = pattern.dropFirst(2).dropLast(2)
        let words = inner.split(separator: "|", omittingEmptySubsequences: false).map(String.init)
        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "_-"))
        guard words.count >= 2, words.allSatisfy({ !$0.isEmpty && $0.unicodeScalars.allSatisfy { allowed.contains($0) && $0.isASCII } }) else {
            return nil
        }
        return words
    }
}

/// KnowledgeSourceSecret says whether one of a source's sealed values is
/// kept; the value itself is never sent back.
public struct KnowledgeSourceSecret: Codable, Hashable, Sendable {
    public let key: String
    public let isSet: Bool
}

/// KnowledgeSourceChange is a source added or changed. The server writes
/// only the fields a call names and keeps the rest, so a field left nil
/// keeps what the source already says.
public struct KnowledgeSourceChange: Hashable, Sendable {
    public var sourceId: String?
    public var name: String?
    public var kind: String?
    public var computer: String?
    public var path: String?
    public var format: String?
    public var enabled: Bool?
    public var mailboxId: String?
    public var rootPath: String?
    public var cron: String?
    public var readEveryCheckout: Bool?
    /// The installed type it is of, and what was filled in for it.
    public var type: String?
    public var settings: [String: JSONValue]?

    public init(
        sourceId: String? = nil, name: String? = nil, kind: String? = nil, computer: String? = nil, path: String? = nil, format: String? = nil,
        enabled: Bool? = nil, mailboxId: String? = nil, rootPath: String? = nil, cron: String? = nil, readEveryCheckout: Bool? = nil,
        type: String? = nil, settings: [String: JSONValue]? = nil
    ) {
        self.sourceId = sourceId
        self.name = name
        self.kind = kind
        self.computer = computer
        self.path = path
        self.format = format
        self.enabled = enabled
        self.mailboxId = mailboxId
        self.rootPath = rootPath
        self.cron = cron
        self.readEveryCheckout = readEveryCheckout
        self.type = type
        self.settings = settings
    }

    var variables: [String: JSONValue] {
        var variables: [String: JSONValue] = [:]
        let texts: [(String, String?)] = [
            ("sourceId", sourceId), ("name", name), ("kind", kind), ("computer", computer), ("path", path), ("format", format),
            ("mailboxId", mailboxId), ("rootPath", rootPath), ("cron", cron), ("type", type),
        ]
        for (variableName, text) in texts {
            if let text { variables[variableName] = .string(text) }
        }
        if let enabled { variables["enabled"] = .bool(enabled) }
        if let readEveryCheckout { variables["readEveryCheckout"] = .bool(readEveryCheckout) }
        if let settings { variables["settings"] = .object(settings) }
        return variables
    }
}

// MARK: - Dreams

/// AgentDream is one dream: when it ran, what it did, what was still
/// waiting when it stopped, and what its model calls cost.
public struct AgentDream: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    /// The job that ran it; every model call it made is a run of that job.
    public let jobId: String?
    public let startedAt: String
    public let finishedAt: String?
    public let digested: Int
    public let filed: Int
    public let merged: Int
    public let rewritten: Int
    public let moved: Int
    public let dormant: Int
    public let embedded: Int
    public let backlog: Int
    public let coarse: Bool
    public let strengthened: Int
    public let associated: Int
    public let rehearsed: Int
    public let gaps: Int
    public let unknown: Int
    public let revised: Int
    public let overviewsWritten: Int
    public let themesMade: Int
    public let themesUpdated: Int
    public let reflectionsWritten: Int
    public let lastError: String?
    public let cost: Double
    public let currency: String?

    /// OutcomeKind is one of the things a dream counts.
    public enum OutcomeKind: String, Sendable {
        case read, filed, rewritten, merged, moved, retired, embedded, strengthened, associated, rehearsed, gaps, unknown, revised,
            overviewsWritten, themesMade, themesUpdated, reflectionsWritten
    }

    /// DreamOutcome is one count of what a dream did.
    public struct DreamOutcome: Hashable, Sendable {
        public let outcomeKind: OutcomeKind
        public let outcomeCount: Int
    }

    /// What it did, the counts that are not zero, in the dashboard's order.
    /// The questions it asked itself come with how many it could not answer
    /// and could not try, even at zero: "12 rehearsed" alone would read as
    /// twelve answered.
    public var outcomes: [DreamOutcome] {
        var outcomes: [DreamOutcome] = []
        func add(_ outcomeKind: OutcomeKind, _ outcomeCount: Int, isShownAtZero: Bool = false) {
            if outcomeCount > 0 || isShownAtZero { outcomes.append(DreamOutcome(outcomeKind: outcomeKind, outcomeCount: outcomeCount)) }
        }
        add(.read, digested)
        add(.filed, filed)
        add(.rewritten, rewritten)
        add(.merged, merged)
        add(.moved, moved)
        add(.retired, dormant)
        add(.embedded, embedded)
        add(.strengthened, strengthened)
        add(.associated, associated)
        if rehearsed > 0 {
            add(.rehearsed, rehearsed)
            add(.gaps, gaps, isShownAtZero: true)
            add(.unknown, unknown, isShownAtZero: true)
        }
        add(.revised, revised)
        add(.overviewsWritten, overviewsWritten)
        add(.themesMade, themesMade)
        add(.themesUpdated, themesUpdated)
        add(.reflectionsWritten, reflectionsWritten)
        return outcomes
    }

    /// How many minutes it took, or nil while it still works.
    public var minutesTaken: Int? {
        guard let started = ServerTime.date(startedAt), let finished = ServerTime.date(finishedAt) else { return nil }
        return max(0, Int((finished.timeIntervalSince(started) / 60).rounded()))
    }
}

/// AgentDreamState is whether a dream waits for a worker or runs now.
public struct AgentDreamState: Codable, Hashable, Sendable {
    /// queued while it waits for a worker, running while it works, empty
    /// when there is neither.
    public let dreamJobStatus: String
    public let queuedAt: String?
    public let startedAt: String?

    public var isQueued: Bool { dreamJobStatus == "queued" }
    public var isRunning: Bool { dreamJobStatus == "running" }
    public var isIdle: Bool { dreamJobStatus.isEmpty }
}

/// ReadingProgress is how far the dreams have got through what was
/// indexed, at the pace of the last dreams, and what that cost.
public struct ReadingProgress: Codable, Hashable, Sendable {
    public let waiting: Int
    public let read: Int
    public let perHour: Double
    public let hoursLeft: Double
    public let bootstrapping: Bool
    public let spent: Double
    public let costLeft: Double
    public let currency: String?
    /// How many dreams the pace was measured over.
    public let dreams: Int

    public init(
        waiting: Int, read: Int, perHour: Double = 0, hoursLeft: Double = 0, bootstrapping: Bool = false, spent: Double = 0, costLeft: Double = 0,
        currency: String? = nil, dreams: Int = 0
    ) {
        self.waiting = waiting
        self.read = read
        self.perHour = perHour
        self.hoursLeft = hoursLeft
        self.bootstrapping = bootstrapping
        self.spent = spent
        self.costLeft = costLeft
        self.currency = currency
        self.dreams = dreams
    }

    /// ReadingSpan is how long the rest takes, in the unit it is said in.
    public enum ReadingSpan: Hashable, Sendable {
        case minutes(Int)
        case hours(Int)
        case days(Int)
    }

    public var total: Int { read + waiting }

    /// The share read, rounded down, and never a hundred while something
    /// waits: a bar that says finished beside seven hundred left is the
    /// one a person believes.
    public var readPercent: Int {
        if total == 0 { return 0 }
        if waiting == 0 { return 100 }
        return min(99, Int((Double(read) / Double(total) * 100).rounded(.down)))
    }

    /// How long the rest takes at the measured pace, or nil with nothing
    /// waiting or no pace yet.
    public var timeLeft: ReadingSpan? {
        guard waiting > 0, perHour > 0 else { return nil }
        if hoursLeft < 1 { return .minutes(max(1, Int((hoursLeft * 60).rounded()))) }
        if hoursLeft < 48 { return .hours(Int(hoursLeft.rounded())) }
        return .days(Int((hoursLeft / 24).rounded()))
    }
}

// MARK: - Schedules

/// AgentSchedule is something the agent does at set times: a cron line in
/// the person's zone, what to do, and where the answer goes.
public struct AgentSchedule: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public let cron: String
    public let prompt: String
    /// drawer, a turn in the conversation, or mail.
    public let deliver: String
    public let enabled: Bool
    public let lastRunAt: String?
    public let nextRunAt: String?
    /// The conversation its answers go to: a goal's, for a schedule that
    /// runs a goal.
    public let conversationId: String?

    public init(
        id: String, name: String, cron: String, prompt: String = "", deliver: String = "drawer", enabled: Bool = true, lastRunAt: String? = nil,
        nextRunAt: String? = nil, conversationId: String? = nil
    ) {
        self.id = id
        self.name = name
        self.cron = cron
        self.prompt = prompt
        self.deliver = deliver
        self.enabled = enabled
        self.lastRunAt = lastRunAt
        self.nextRunAt = nextRunAt
        self.conversationId = conversationId
    }

    public var isDeliveredByMail: Bool { deliver == "mail" }
}

/// DailyBrief is the morning brief: an ordinary schedule with a known
/// name, set by a time and the days of the week.
public enum DailyBrief {
    /// The schedule the brief is kept as.
    public static let scheduleName = "Daily brief"
    static let defaultCron = "30 7 * * 1-5"

    /// The brief among the schedules, if it was ever set.
    public static func schedule(in schedules: [AgentSchedule]) -> AgentSchedule? {
        schedules.first { $0.name == scheduleName }
    }

    /// The time a cron line such as "30 7 * * 1-5" says, as "HH:MM".
    public static func time(fromCron cron: String?) -> String {
        let fields = (cron ?? defaultCron).split(separator: " ", omittingEmptySubsequences: true).map(String.init)
        let hour = fields.count > 1 ? fields[1] : "7"
        let minute = fields.count > 0 ? fields[0] : "30"
        return String(repeating: "0", count: max(0, 2 - hour.count)) + hour + ":" + String(repeating: "0", count: max(0, 2 - minute.count)) + minute
    }

    /// The days a cron line says, as the numbers the server takes, Monday 1
    /// to Sunday 7; weekdays when there is no line or its days cannot be
    /// read.
    public static func dayNumbers(fromCron cron: String?) -> [Int] {
        let fields = (cron ?? defaultCron).split(separator: " ", omittingEmptySubsequences: true).map(String.init)
        guard fields.count == 5, let dayNumbers = ScheduleWeekday.dayNumbers(fromCronField: fields[4]), !dayNumbers.isEmpty else {
            return Array(1...5)
        }
        return dayNumbers
    }
}

/// ScheduleWeekday is a day of the week as the server numbers it, Monday 1
/// to Sunday 7, and how a locale names it.
public enum ScheduleWeekday {
    /// Every day of the week, Monday first.
    public static let allDayNumbers = Array(1...7)

    /// The days a cron line's weekday field says, Monday 1 to Sunday 7 in
    /// order: "*", a number, a range or a list of them, with 0 and 7 both
    /// Sunday as cron has them. Nil for anything else, such as a step.
    public static func dayNumbers(fromCronField cronField: String) -> [Int]? {
        if cronField == "*" { return allDayNumbers }
        var dayNumbers = Set<Int>()
        for piece in cronField.split(separator: ",", omittingEmptySubsequences: false) {
            let ends = piece.split(separator: "-", omittingEmptySubsequences: false)
            guard (1...2).contains(ends.count), let low = Int(ends[0]), let high = Int(ends[ends.count - 1]), (0...7).contains(low),
                (0...7).contains(high), low <= high
            else { return nil }
            for cronDay in low...high {
                dayNumbers.insert(cronDay == 0 ? 7 : cronDay)
            }
        }
        return dayNumbers.sorted()
    }

    /// The days of a week in the order a calendar starts it, for a
    /// calendar's firstWeekday (1 Sunday, 2 Monday).
    public static func weekOrder(firstWeekday: Int) -> [Int] {
        let firstDayNumber = dayNumber(fromCalendarWeekday: firstWeekday)
        return (0..<7).map { offset in (firstDayNumber - 1 + offset) % 7 + 1 }
    }

    /// The weekday a Calendar numbers this day as, Sunday 1 to Saturday 7.
    public static func calendarWeekday(fromDayNumber dayNumber: Int) -> Int {
        dayNumber % 7 + 1
    }

    /// The day a Calendar weekday is, Monday 1 to Sunday 7.
    public static func dayNumber(fromCalendarWeekday calendarWeekday: Int) -> Int {
        (calendarWeekday + 5) % 7 + 1
    }

    /// The day's name in the locale, as it stands on its own: "Friday".
    public static func name(_ dayNumber: Int, locale: Locale = .current) -> String {
        symbol(dayNumber, in: calendar(locale).standaloneWeekdaySymbols)
    }

    /// The day's short name in the locale: "Fri".
    public static func shortName(_ dayNumber: Int, locale: Locale = .current) -> String {
        symbol(dayNumber, in: calendar(locale).shortStandaloneWeekdaySymbols)
    }

    /// Some days' short names as the locale lists them: "Mon, Wed, and Fri".
    public static func shortNamesText(_ dayNumbers: [Int], locale: Locale = .current) -> String {
        dayNumbers.sorted().map { shortName($0, locale: locale) }.formatted(.list(type: .and).locale(locale))
    }

    private static func symbol(_ dayNumber: Int, in symbols: [String]) -> String {
        let symbolIndex = calendarWeekday(fromDayNumber: dayNumber) - 1
        return symbols.indices.contains(symbolIndex) ? symbols[symbolIndex] : String(dayNumber)
    }

    private static func calendar(_ locale: Locale) -> Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.locale = locale
        return calendar
    }
}

/// ScheduleTiming is when a cron line of the plain kind comes round: a
/// minute and an hour on some days of the week, every week. Any other line,
/// with steps, days of the month or months, has no timing and is shown as
/// it is written.
public struct ScheduleTiming: Hashable, Sendable {
    public let hour: Int
    public let minute: Int
    /// The days it comes on, Monday 1 to Sunday 7, in order.
    public let dayNumbers: [Int]

    /// DayPattern is how the days read to a person.
    public enum DayPattern: Hashable, Sendable {
        case everyDay, weekdays, weekends
        case oneDay(Int)
        case someDays([Int])
    }

    /// Reads "minute hour * * days", or nil for any other line.
    public init?(cron: String) {
        let fields = cron.split(separator: " ", omittingEmptySubsequences: true).map(String.init)
        guard fields.count == 5, fields[2] == "*", fields[3] == "*", fields[0].allSatisfy(\.isASCIIDigit), fields[1].allSatisfy(\.isASCIIDigit),
            let minute = Int(fields[0]), let hour = Int(fields[1]), (0..<60).contains(minute), (0..<24).contains(hour),
            let dayNumbers = ScheduleWeekday.dayNumbers(fromCronField: fields[4])
        else { return nil }
        self.hour = hour
        self.minute = minute
        self.dayNumbers = dayNumbers
    }

    public var dayPattern: DayPattern { Self.dayPattern(for: dayNumbers) }

    /// How some days read to a person: every day, weekdays, weekends, one
    /// day, or a few. The days are Monday 1 to Sunday 7, in order.
    public static func dayPattern(for dayNumbers: [Int]) -> DayPattern {
        switch dayNumbers {
        case ScheduleWeekday.allDayNumbers: .everyDay
        case Array(1...5): .weekdays
        case [6, 7]: .weekends
        case let oneDayNumbers where oneDayNumbers.count == 1: .oneDay(oneDayNumbers[0])
        default: .someDays(dayNumbers)
        }
    }

    /// The time of day as the locale writes it: "7:30 AM", "07:30".
    public func timeText(locale: Locale = .current) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC") ?? calendar.timeZone
        let date = calendar.date(from: DateComponents(year: 2001, month: 1, day: 1, hour: hour, minute: minute)) ?? Date(timeIntervalSinceReferenceDate: 0)
        return date.formatted(Date.FormatStyle(date: .omitted, time: .shortened, locale: locale, calendar: calendar, timeZone: calendar.timeZone))
    }
}

extension Character {
    fileprivate var isASCIIDigit: Bool { isASCII && isNumber }
}

// MARK: - Alerts

/// AgentAlert is something the agent told the person unasked, with the
/// messages it was about and what muting it could mean.
public struct AgentAlert: Codable, Hashable, Sendable, Identifiable {
    /// Covered is one message an alert was about.
    public struct Covered: Codable, Hashable, Sendable {
        public let mailId: String
        public let subject: String
        public let fromAddress: String
    }

    public let id: String
    public let alertText: String
    public let sentAt: String
    public let subjectKey: String
    public let isUrgent: Bool
    public let covered: [Covered]
    /// What each scope would mute for it, the default first.
    public let muteChoices: [AlertMuteChoice]
}

/// AlertMuteChoice is one way an alert can be muted: a scope (subjectKey,
/// sender, domain, kind or spendingCategory) and what it names.
public struct AlertMuteChoice: Codable, Hashable, Sendable {
    public let muteScope: String
    public let muteTarget: String

    public init(muteScope: String, muteTarget: String) {
        self.muteScope = muteScope
        self.muteTarget = muteTarget
    }
}

/// AgentAlertMute is something the person asked not to be told about.
public struct AgentAlertMute: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let muteScope: String
    public let muteTarget: String
    public let createdAt: String
}

// MARK: - Follow-ups

/// AgentFollowUp is something the agent keeps track of from the person's
/// mail until something closes it: a bill, a parcel, a deadline or a coming
/// event. What a kind does not use is nil. Only the ones still being watched
/// are listed, so followUpState is open, overdue or stalled.
public struct AgentFollowUp: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    /// bill, parcel, deadline or event.
    public let followUpKind: String
    public let followUpTitle: String
    public let payee: String?
    public let carrier: String?
    /// A decimal such as "84.20", in currencyCode.
    public let amount: String?
    public let currencyCode: String?
    public let trackingNumber: String?
    /// A day written YYYY-MM-DD.
    public let dueOn: String?
    public let place: String?
    /// The page a follow-up found on the web came from, nil for what mail said.
    public let sourceUrl: String?
    public let followUpState: String

    /// The kinds a person can mute, all of them first, as the server's kind
    /// mutes name them.
    public static let muteKinds = ["follow_up", "bill", "parcel", "deadline", "event", "nearby"]

    public var isOverdue: Bool { followUpState == "overdue" }
    public var isStalled: Bool { followUpState == "stalled" }

    /// The page it was found on, when it is a web page: only http and https
    /// are linked, since the address came from a stranger's page.
    public var sourcePageURL: URL? {
        guard let sourceUrl, let address = URL(string: sourceUrl), let scheme = address.scheme?.lowercased(),
              scheme == "https" || scheme == "http", address.host != nil else { return nil }
        return address
    }

    /// The day it is due as the start of that day in the calendar given, nil
    /// when it has none. Built from its parts rather than parsed, since a
    /// bare date read as UTC midnight is the evening before anywhere west of
    /// Greenwich.
    public func dueDate(calendar: Calendar = .current) -> Date? {
        guard let dueOn else { return nil }
        let parts = dueOn.split(separator: "-").compactMap { Int($0) }
        guard parts.count == 3 else { return nil }
        return calendar.date(from: DateComponents(year: parts[0], month: parts[1], day: parts[2]))
    }

    /// The nearest day first, the ones with no day last, and the server's
    /// order within a day.
    public static func ordered(_ followUps: [AgentFollowUp]) -> [AgentFollowUp] {
        followUps.enumerated().sorted { left, right in
            let leftDay = left.element.dueOn ?? "9999-12-31"
            let rightDay = right.element.dueOn ?? "9999-12-31"
            return leftDay == rightDay ? left.offset < right.offset : leftDay < rightDay
        }.map(\.element)
    }
}

/// NewAgentFollowUp is something the person asks the agent to follow. The
/// fields a kind does not use are kept while the kind is changed, so a slip
/// does not lose what was typed, and are left out of what is sent.
public struct NewAgentFollowUp: Hashable, Sendable {
    /// The kinds a person can add, in the order they are offered.
    public static let kinds = ["bill", "parcel", "deadline", "event"]

    /// bill, parcel, deadline or event.
    public var followUpKind = "bill"
    public var followUpTitle = ""
    /// A day written YYYY-MM-DD, empty for none.
    public var dueOn = ""
    public var payee = ""
    public var amount = ""
    public var currencyCode = ""
    public var carrier = ""
    public var trackingNumber = ""
    public var place = ""

    public init() {}

    /// The server's own rule: a title, a day unless it is a parcel, and an
    /// amount for a bill.
    public var canAdd: Bool {
        if followUpTitle.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return false }
        if followUpKind != "parcel" && dueOn.isEmpty { return false }
        if followUpKind == "bill" && amount.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return false }
        return true
    }

    /// What the mutation is given: only the fields the kind uses, and none
    /// that were left empty.
    public var variables: [String: JSONValue] {
        var given: [String: JSONValue] = [
            "followUpKind": .string(followUpKind),
            "followUpTitle": .string(followUpTitle.trimmingCharacters(in: .whitespacesAndNewlines)),
        ]
        var usedFields = [("dueOn", dueOn)]
        switch followUpKind {
        case "bill": usedFields += [("payee", payee), ("amount", amount), ("currencyCode", currencyCode)]
        case "parcel": usedFields += [("carrier", carrier), ("trackingNumber", trackingNumber)]
        case "event": usedFields += [("place", place)]
        default: break
        }
        for (name, text) in usedFields {
            let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty { given[name] = .string(trimmed) }
        }
        return given
    }

    /// A day written YYYY-MM-DD, from its parts in the calendar given, as
    /// the day the person picked and not the one a time zone moves it to.
    public static func dayText(_ date: Date, calendar: Calendar = .current) -> String {
        let parts = calendar.dateComponents([.year, .month, .day], from: date)
        return String(format: "%04d-%02d-%02d", parts.year ?? 0, parts.month ?? 0, parts.day ?? 0)
    }
}

// MARK: - Connections

/// AgentConnectedServer is a server the operator connected for everyone,
/// and whether this person has connected to it.
public struct AgentConnectedServer: Codable, Hashable, Sendable, Identifiable {
    public let name: String
    public let transport: String
    /// none or shared for one nobody connects to, user for one that takes
    /// the person's credential, oauth for one they authorize.
    public let auth: String
    /// Where an authorization comes back: the server or a computer.
    public let oauthRedirect: String
    public let headless: Bool
    public let enabled: Bool
    /// connected, error, pending or empty.
    public let status: String
    public let lastError: String?
    public let tools: Int

    public var id: String { name }

    /// Whether it is the person's to connect, rather than shared.
    public var isPersonal: Bool { auth == "user" || auth == "oauth" }
    public var isConnected: Bool { status == "connected" }
    public var doesAuthorizeInBrowser: Bool { auth == "oauth" }
}

/// AgentSkillSecret is a value an installed skill asks this person for,
/// and whether it is kept.
public struct AgentSkillSecret: Codable, Hashable, Sendable, Identifiable {
    public let skill: String
    public let key: String
    public let description: String
    public let set: Bool

    public var id: String { "\(skill).\(key)" }
}

/// AgentReach is which of the person's computers a skill or a connected
/// server makes its requests through, or this server where it names none.
public struct AgentReach: Codable, Hashable, Sendable, Identifiable {
    /// skill or server.
    public let kind: String
    public let name: String
    public let computerName: String
    /// Whether it runs on a computer, and so has no server to go through.
    public let isOnComputer: Bool

    public var id: String { "\(kind).\(name)" }
}

/// AgentChatChannel is the person's own bot in a chat app: whether its
/// token is kept, the chat it is linked to or the code that links one, and
/// whether it runs.
public struct AgentChatChannel: Codable, Hashable, Sendable, Identifiable {
    public let kind: String
    public let hasToken: Bool
    public let botName: String?
    public let linked: Bool
    public let linkedName: String?
    public let linkCode: String?
    public let enabled: Bool
    public let running: Bool
    public let lastError: String?

    public var id: String { kind }

    /// The chat apps a bot can be set up in, in the order offered.
    public static let kinds = ["telegram", "discord"]
}

// MARK: - Activity

/// AgentBackgroundWork is a survey or subagent the agent started and did
/// not wait for: where it stands, and when finished, what came of it.
public struct AgentBackgroundWork: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    /// survey or subagent.
    public let workKind: String
    /// queued, running, done, failed or stopped.
    public let workStatus: String
    public let title: String
    public let errorMessage: String
    public let runIds: [String]
    public let createdAt: String
    public let finishedAt: String?
    /// The report or the answer; read only when one piece is opened.
    public let resultText: String?

    public init(
        id: String, workKind: String = "survey", workStatus: String, title: String = "", errorMessage: String = "", runIds: [String] = [],
        createdAt: String = "", finishedAt: String? = nil, resultText: String? = nil
    ) {
        self.id = id
        self.workKind = workKind
        self.workStatus = workStatus
        self.title = title
        self.errorMessage = errorMessage
        self.runIds = runIds
        self.createdAt = createdAt
        self.finishedAt = finishedAt
        self.resultText = resultText
    }

    /// Whether it may still be stopped.
    public var isStoppable: Bool { workStatus == "queued" || workStatus == "running" }
    /// Whether there is something to show: what came of it, or why not.
    public var isOpenable: Bool { workStatus == "done" || workStatus == "failed" }
    public var isSurvey: Bool { workKind == "survey" }
}

/// AgentRunSummary is one run the agent made: a model call of some kind,
/// with what it was about and what it used. Its id opens its transcript.
public struct AgentRunSummary: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let title: String
    public let jobKind: String?
    public let lastAt: String
    public let usage: AgentUsage

    /// The kinds a run can be, for a filter over runs: a page holds one
    /// page of runs, so the kinds on it are not the kinds there are.
    public static let jobKinds = [
        "triage", "reply", "research", "summarize", "remember", "ingest", "recording", "dream", "schedule", "extract", "draft", "describe", "compact",
        "evaluate", "survey", "subagent", "mcp", "goal", "background", "speak_first", "alert", "categorize", "statement_import",
        "read_receipt", "describe_photo", "tax_document", "tax_mail", "watch", "follow_up_sweep", "nearby", "journal",
    ]

    /// Every token it used: read fresh, read from the cache, and written.
    public var tokenCount: Int { usage.promptTokens + (usage.cacheReadTokens ?? 0) + usage.completionTokens }
}

/// AgentRunPage is one page of the runs, and how many match in all.
public struct AgentRunPage: Codable, Hashable, Sendable {
    public let total: Int
    public let runs: [AgentRunSummary]
}

/// AgentReply is a reply the agent answered for the person, is holding
/// back to send, or decided against, with why.
public struct AgentReply: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let createdAt: String
    public let mailboxId: String?
    public let mailId: String?
    /// held, sent, cancelled, refused or failed.
    public let status: String
    public let reason: String?
    public let subject: String
    public let to: String?
    public let sendAfter: String?
    public let sentAt: String?

    public var isHeld: Bool { status == "held" }
}

/// AgentReplyPage is the newest replies, and how many there are.
public struct AgentReplyPage: Codable, Hashable, Sendable {
    public let total: Int
    public let replies: [AgentReply]
}

// MARK: - Memory

/// AgentLearnedFact is a fact the agent filed lately, with the page it is
/// on.
public struct AgentLearnedFact: Codable, Hashable, Sendable, Identifiable {
    public let fact: KnowledgeFact
    public let path: String
    public let name: String

    public var id: String { fact.id }
}

/// AgentCorrection is something the agent learned from the person's own
/// hands, such as a message they filed elsewhere.
public struct AgentCorrection: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let createdAt: String
    public let kind: String
    public let said: String
}

/// MemoryCheckQuestion is a question about the person that they answered,
/// which the agent is graded against.
public struct MemoryCheckQuestion: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let createdAt: String
    public let modifiedAt: String
    /// direct, paraphrase, changed, multihop or abstain.
    public let questionKind: String
    public let questionText: String
    public let expectedAnswer: String
    /// What used to be true, for a question about a change.
    public let outdatedAnswer: String?
    /// asked, confirmed, corrected, unsure or dropped.
    public let questionState: String
    public let isAnswerFiledAfter: Bool
    public let conversationId: String?
    public let answeredAt: String?

    public init(
        id: String, createdAt: String = "", modifiedAt: String = "", questionKind: String = "direct", questionText: String = "",
        expectedAnswer: String = "", outdatedAnswer: String? = nil, questionState: String = "confirmed", isAnswerFiledAfter: Bool = false,
        conversationId: String? = nil, answeredAt: String? = nil
    ) {
        self.id = id
        self.createdAt = createdAt
        self.modifiedAt = modifiedAt
        self.questionKind = questionKind
        self.questionText = questionText
        self.expectedAnswer = expectedAnswer
        self.outdatedAnswer = outdatedAnswer
        self.questionState = questionState
        self.isAnswerFiledAfter = isAnswerFiledAfter
        self.conversationId = conversationId
        self.answeredAt = answeredAt
    }

    public static let questionKinds = ["direct", "paraphrase", "changed", "multihop", "abstain"]
    public static let questionStates = ["asked", "confirmed", "corrected", "unsure", "dropped"]

    public var isDropped: Bool { questionState == "dropped" }

    /// The questions shown: a dropped one is kept so a run that answered it
    /// can still name it, but it is out of every run since.
    public static func visible(_ questions: [MemoryCheckQuestion], isShowingDropped: Bool) -> [MemoryCheckQuestion] {
        isShowingDropped ? questions : questions.filter { !$0.isDropped }
    }
}

/// MemoryCheckQuestionChange is what an edit of a question changed; a
/// field left nil is not sent, so an edit does not write back an answer
/// changed elsewhere a moment before.
public struct MemoryCheckQuestionChange: Hashable, Sendable {
    public var questionKind: String?
    public var questionText: String?
    public var expectedAnswer: String?
    public var outdatedAnswer: String?
    public var questionState: String?

    public init(
        questionKind: String? = nil, questionText: String? = nil, expectedAnswer: String? = nil, outdatedAnswer: String? = nil,
        questionState: String? = nil
    ) {
        self.questionKind = questionKind
        self.questionText = questionText
        self.expectedAnswer = expectedAnswer
        self.outdatedAnswer = outdatedAnswer
        self.questionState = questionState
    }

    /// Only what differs between a question and its edited copy.
    public init(from question: MemoryCheckQuestion, to edited: MemoryCheckQuestionChange) {
        if let kind = edited.questionKind, kind != question.questionKind { questionKind = kind }
        if let text = edited.questionText, text != question.questionText { questionText = text }
        if let answer = edited.expectedAnswer, answer != question.expectedAnswer { expectedAnswer = answer }
        if let outdated = edited.outdatedAnswer, outdated != (question.outdatedAnswer ?? "") { outdatedAnswer = outdated }
        if let editedState = edited.questionState, editedState != question.questionState { questionState = editedState }
    }

    public var isEmpty: Bool { variables.isEmpty }

    var variables: [String: JSONValue] {
        var variables: [String: JSONValue] = [:]
        if let questionKind { variables["questionKind"] = .string(questionKind) }
        if let questionText { variables["questionText"] = .string(questionText) }
        if let expectedAnswer { variables["expectedAnswer"] = .string(expectedAnswer) }
        if let outdatedAnswer { variables["outdatedAnswer"] = .string(outdatedAnswer) }
        if let questionState { variables["questionState"] = .string(questionState) }
        return variables
    }
}

/// MemoryCheckScore is how one source scored in a grading: memory, the
/// documents, or both together.
public struct MemoryCheckScore: Codable, Hashable, Sendable {
    /// memory, sources or both.
    public let answerFrom: String
    public let answeredCount: Int
    public let scorePercent: Double
    public let scorePercentWithoutFiledAfter: Double
    public let filedAfterCount: Int

    public init(answerFrom: String, answeredCount: Int, scorePercent: Double, scorePercentWithoutFiledAfter: Double = 0, filedAfterCount: Int = 0) {
        self.answerFrom = answerFrom
        self.answeredCount = answeredCount
        self.scorePercent = scorePercent
        self.scorePercentWithoutFiledAfter = scorePercentWithoutFiledAfter
        self.filedAfterCount = filedAfterCount
    }

    /// The sources, in the order the server reports them.
    public static let answerSources = ["memory", "sources", "both"]
}

/// MemoryCheckRun is one grading of the agent against the questions.
public struct MemoryCheckRun: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let startedAt: String
    public let finishedAt: String?
    public let questionCount: Int
    public let cost: Double
    public let sourceScores: [MemoryCheckScore]

    public init(id: String, startedAt: String, finishedAt: String? = nil, questionCount: Int = 0, cost: Double = 0, sourceScores: [MemoryCheckScore] = []) {
        self.id = id
        self.startedAt = startedAt
        self.finishedAt = finishedAt
        self.questionCount = questionCount
        self.cost = cost
        self.sourceScores = sourceScores
    }

    public var isFinished: Bool { finishedAt != nil }

    /// The score from one source, when it answered any.
    public func score(from answerFrom: String) -> MemoryCheckScore? {
        sourceScores.first { $0.answerFrom == answerFrom && $0.answeredCount > 0 }
    }

    /// MemoryCheckPoint is one finished grading's score, for a line over
    /// time.
    public struct MemoryCheckPoint: Hashable, Sendable, Identifiable {
        public let runId: String
        public let startedAt: String
        public let scorePercent: Double
        public var id: String { runId }
    }

    /// One source's score in each finished grading, oldest first. A
    /// grading that did not answer from it is a gap, not a zero.
    public static func scoreHistory(_ runs: [MemoryCheckRun], answerFrom: String) -> [MemoryCheckPoint] {
        runs.filter(\.isFinished)
            .compactMap { run in run.score(from: answerFrom).map { MemoryCheckPoint(runId: run.id, startedAt: run.startedAt, scorePercent: $0.scorePercent) } }
            .sorted { $0.startedAt < $1.startedAt }
    }

    /// How many points the score moved from the first grading to the last,
    /// rounded first so it is the difference of the two percents shown;
    /// nil with fewer than two.
    public static func scoreChangePoints(_ history: [MemoryCheckPoint]) -> Int? {
        guard history.count >= 2, let first = history.first, let last = history.last else { return nil }
        return Int(last.scorePercent.rounded()) - Int(first.scorePercent.rounded())
    }
}

/// MemoryCheckAnswer is what the agent answered to one question in a
/// grading, from one source, and what the grader made of it.
public struct MemoryCheckAnswer: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let runId: String
    public let questionId: String
    public let answerFrom: String
    /// correct, partial, not_known, missed, stale, invented, wrong or
    /// ungraded.
    public let answerVerdict: String
    public let verdictReason: String
    public let answerText: String

    public init(id: String, runId: String = "", questionId: String, answerFrom: String = "memory", answerVerdict: String, verdictReason: String = "", answerText: String = "") {
        self.id = id
        self.runId = runId
        self.questionId = questionId
        self.answerFrom = answerFrom
        self.answerVerdict = answerVerdict
        self.verdictReason = verdictReason
        self.answerText = answerText
    }

    /// The verdicts worth reading first: memory did not carry the fact, or
    /// the answer said something that is not so. "Not known" is left out:
    /// it is given only where it is the right answer.
    public var isMissedOrWrong: Bool { ["missed", "wrong", "invented", "stale"].contains(answerVerdict) }
    public var isRight: Bool { answerVerdict == "correct" || answerVerdict == "not_known" }
}

// MARK: - Documents

extension Documents {
    static let agentUpkeepFields = """
        id enabled operatorDisabledAt dreamFrom dreamUntil dreamBootstrap isAlertsEnabled alertQuietStart alertQuietEnd alertDailyMost lookAroundWeekday lookAroundTime
        """
    public static let agentUpkeep = """
        query { ReadAgent { agent { \(agentUpkeepFields) } } }
        """
    public static let updateAgentUpkeep = """
        mutation ($dreamFrom: String, $dreamUntil: String, $isAlertsEnabled: Boolean, $alertQuietStart: String, $alertQuietEnd: String, $alertDailyMost: Int, $lookAroundWeekday: String, $lookAroundTime: String) {
          UpdateAgent(dreamFrom: $dreamFrom, dreamUntil: $dreamUntil, isAlertsEnabled: $isAlertsEnabled, alertQuietStart: $alertQuietStart, alertQuietEnd: $alertQuietEnd, alertDailyMost: $alertDailyMost, lookAroundWeekday: $lookAroundWeekday, lookAroundTime: $lookAroundTime) {
            agent { \(agentUpkeepFields) }
          }
        }
        """
    public static let attachedComputers = """
        query { ReadAgentComputers { computers { name description } } }
        """

    static let knowledgeSourceFields = """
        id kind name specification { type settings computer path format mailboxId readEveryCheckout }
        rootPath enabled cron lastRunAt lastError documentCount chunkCount refusedCount more
        checkoutsKeptToProfile filesKeptToProfile
        """
    public static let knowledgeSourceDetails = """
        query {
          ListAgentKnowledgeSources { \(knowledgeSourceFields) }
          ListAgentSourceAttachments { sourceId undecided declined described }
        }
        """
    public static let knowledgeSourceTypes = """
        query {
          ListAgentSourceTypes {
            name description isLocal reader requires readable problem
            settings { name description settingType pattern itemPattern default isRequired minimum maximum }
            secrets { key description isOptional }
          }
        }
        """
    public static let saveKnowledgeSource = """
        mutation ($sourceId: String, $kind: String, $name: String, $computer: String, $path: String, $format: String, $enabled: Boolean, $mailboxId: String, $rootPath: String, $cron: String, $readEveryCheckout: Boolean, $type: String, $settings: JSON) {
          SaveAgentKnowledgeSource(sourceId: $sourceId, kind: $kind, name: $name, computer: $computer, path: $path, format: $format, enabled: $enabled, mailboxId: $mailboxId, rootPath: $rootPath, cron: $cron, readEveryCheckout: $readEveryCheckout, type: $type, settings: $settings) { id name }
        }
        """
    public static let knowledgeSourceSecrets = """
        query ($sourceId: String!) { ListAgentKnowledgeSourceSecrets(sourceId: $sourceId) { key isSet } }
        """
    public static let setKnowledgeSourceSecret = """
        mutation ($sourceId: String!, $key: String!, $value: String!) {
          SetAgentKnowledgeSourceSecret(sourceId: $sourceId, key: $key, value: $value) { key isSet }
        }
        """
    public static let declinedAttachments = """
        query ($sourceId: String!) {
          ListAgentDeclinedAttachments(sourceId: $sourceId, first: 100) { documentId name contentType channel thread path declined }
        }
        """

    public static let agentDreams = """
        query ($first: Int) {
          ListAgentDreams(first: $first) {
            id jobId startedAt finishedAt digested filed merged rewritten moved dormant embedded backlog coarse
            strengthened associated rehearsed gaps unknown revised overviewsWritten themesMade themesUpdated reflectionsWritten lastError cost currency
          }
        }
        """
    public static let agentDreamState = """
        query { AgentDreamState { dreamJobStatus queuedAt startedAt } }
        """
    public static let readingProgress = """
        query { AgentReadingProgress { waiting read perHour hoursLeft bootstrapping spent costLeft currency dreams } }
        """
    public static let dreamNow = """
        mutation { DreamAgentNow }
        """
    public static let dreamBootstrap = """
        mutation ($on: Boolean!) { DreamAgentNow(bootstrap: $on) }
        """
    public static let rereadDocuments = """
        mutation ($minutes: Int!) { RereadAgentDocuments(minutes: $minutes) }
        """

    public static let agentSchedules = """
        query { ListAgentSchedules { id name cron prompt deliver enabled lastRunAt nextRunAt conversationId } }
        """
    public static let saveAgentSchedule = """
        mutation ($scheduleId: String, $name: String, $cron: String, $prompt: String, $deliver: String, $enabled: Boolean) {
          SaveAgentSchedule(scheduleId: $scheduleId, name: $name, cron: $cron, prompt: $prompt, deliver: $deliver, enabled: $enabled) { id }
        }
        """
    public static let deleteAgentSchedule = """
        mutation ($scheduleId: String!) { DeleteAgentSchedule(scheduleId: $scheduleId) }
        """
    public static let runAgentSchedule = """
        mutation ($scheduleId: String!) { RunAgentSchedule(scheduleId: $scheduleId) }
        """
    public static let setDailyBrief = """
        mutation ($enabled: Boolean!, $at: String, $days: [Int!]) {
          SetAgentBrief(enabled: $enabled, at: $at, days: $days) { id }
        }
        """
    public static let runDailyBrief = """
        mutation { RunAgentBriefNow { id name } }
        """

    public static let agentAlerts = """
        query {
          ListAgentAlerts(first: 20) { id alertText sentAt subjectKey isUrgent
            covered { mailId subject fromAddress }
            muteChoices { muteScope muteTarget } }
          ListAgentAlertMutes { id muteScope muteTarget createdAt }
        }
        """
    public static let muteAgentAlert = """
        mutation ($alertId: String, $muteScope: String) {
          MuteAgentAlert(alertId: $alertId, muteScope: $muteScope) { id muteScope muteTarget }
        }
        """
    public static let unmuteAgentAlert = """
        mutation ($muteId: String!) { UnmuteAgentAlert(muteId: $muteId) }
        """
    public static let agentFollowUps = """
        query {
          ListAgentFollowUps(isOpenOnly: true) { id followUpKind followUpTitle payee carrier amount currencyCode trackingNumber dueOn place sourceUrl followUpState }
        }
        """
    public static let stopAgentFollowUp = """
        mutation ($followUpId: String!) { StopAgentFollowUp(followUpId: $followUpId) { id followUpState } }
        """
    public static let addAgentFollowUp = """
        mutation ($followUpKind: String!, $followUpTitle: String!, $dueOn: String, $payee: String, $amount: String, $currencyCode: String, $carrier: String, $trackingNumber: String, $place: String) {
          AddAgentFollowUp(followUpKind: $followUpKind, followUpTitle: $followUpTitle, dueOn: $dueOn, payee: $payee, amount: $amount, currencyCode: $currencyCode, carrier: $carrier, trackingNumber: $trackingNumber, place: $place) { id followUpTitle }
        }
        """
    public static let muteAgentAlertKind = """
        mutation ($muteTarget: String) {
          MuteAgentAlert(muteScope: "kind", muteTarget: $muteTarget) { id muteScope muteTarget }
        }
        """

    public static let learnedFacts = """
        query ($days: Int, $first: Int) {
          ListAgentLearned(days: $days, first: $first) {
            fact { id number kind text happenedAt inferred evidence { kind id quote } audiences }
            path
            name
          }
        }
        """
    public static let agentCorrections = """
        query { ListAgentCorrections(first: 50) { id createdAt kind said } }
        """
    static let memoryCheckQuestionFields = """
        id createdAt modifiedAt questionKind questionText expectedAnswer outdatedAnswer questionState isAnswerFiledAfter conversationId answeredAt
        """
    public static let memoryCheckQuestions = """
        query { ListAgentEvaluationQuestions { \(memoryCheckQuestionFields) } }
        """
    public static let memoryCheckRuns = """
        query ($first: Int) {
          ListAgentEvaluationRuns(first: $first) {
            id startedAt finishedAt questionCount cost
            sourceScores { answerFrom answeredCount scorePercent scorePercentWithoutFiledAfter filedAfterCount }
          }
        }
        """
    public static let memoryCheckAnswers = """
        query ($runId: String!) {
          ListAgentEvaluationAnswers(runId: $runId) { id runId questionId answerFrom answerVerdict verdictReason answerText }
        }
        """
    public static let updateMemoryCheckQuestion = """
        mutation ($questionId: String!, $questionKind: String, $questionText: String, $expectedAnswer: String, $outdatedAnswer: String, $questionState: String) {
          UpdateAgentEvaluationQuestion(questionId: $questionId, questionKind: $questionKind, questionText: $questionText, expectedAnswer: $expectedAnswer, outdatedAnswer: $outdatedAnswer, questionState: $questionState) { \(memoryCheckQuestionFields) }
        }
        """
    public static let startMemoryCheck = """
        mutation ($speakFirstReason: String!) { SpeakFirstNow(speakFirstReason: $speakFirstReason) }
        """
    public static let gradeMemoryNow = """
        mutation { EvaluateAgentMemoryNow { id startedAt } }
        """

    public static let connectedServers = """
        query { ListAgentServers { name transport auth oauthRedirect headless enabled status lastError tools } }
        """
    public static let connectServer = """
        mutation ($server: String!, $credential: String) {
          ConnectAgentServer(server: $server, credential: $credential) { name status lastError tools }
        }
        """
    public static let disconnectServer = """
        mutation ($server: String!) { DisconnectAgentServer(server: $server) { name status } }
        """
    public static let skillSecrets = """
        query { ListAgentSkillSecrets { skill key description set } }
        """
    public static let setSkillSecret = """
        mutation ($skill: String!, $key: String!, $value: String!) {
          SetAgentSkillSecret(skill: $skill, key: $key, value: $value) { skill key set }
        }
        """
    public static let clearSkillSecret = """
        mutation ($skill: String!, $key: String!) { ClearAgentSkillSecret(skill: $skill, key: $key) }
        """
    public static let agentReaches = """
        query {
          ListAgentReaches { kind name computerName isOnComputer }
          ReadAgentComputers { computers { name description } }
        }
        """
    public static let setAgentReach = """
        mutation ($kind: String!, $name: String!, $computerName: String!) {
          SetAgentReach(kind: $kind, name: $name, computerName: $computerName) { kind name computerName }
        }
        """
    static let chatChannelFields = """
        kind hasToken botName linked linkedName linkCode enabled running lastError
        """
    public static let chatChannels = """
        query { ListAgentChannels { \(chatChannelFields) } }
        """
    public static let setChatChannel = """
        mutation ($kind: String!, $token: String, $enabled: Boolean) {
          SetAgentChannel(kind: $kind, token: $token, enabled: $enabled) { \(chatChannelFields) }
        }
        """
    public static let unlinkChatChannel = """
        mutation ($kind: String!) { UnlinkAgentChannel(kind: $kind) { kind } }
        """
    public static let removeChatChannel = """
        mutation ($kind: String!) { RemoveAgentChannel(kind: $kind) }
        """

    public static let backgroundWork = """
        query {
          ListAgentBackgroundWork(first: 20) { id workKind workStatus title errorMessage runIds createdAt finishedAt }
        }
        """
    public static let backgroundWorkResult = """
        query ($id: String!) {
          GetAgentBackgroundWork(id: $id) { id workKind workStatus title errorMessage resultText runIds createdAt finishedAt }
        }
        """
    public static let stopBackgroundWork = """
        mutation ($id: String!) { StopAgentBackgroundWork(id: $id) { id workStatus } }
        """
    public static let agentRuns = """
        query ($first: Int, $offset: Int, $jobId: String, $kinds: [String!], $query: String) {
          ListAgentRuns(first: $first, offset: $offset, jobId: $jobId, kinds: $kinds, query: $query) {
            total
            runs { id title jobKind lastAt usage { promptTokens cacheReadTokens completionTokens cost } }
          }
        }
        """
    public static let agentReplies = """
        query ($first: Int) {
          ListAgentReplies(first: $first) {
            total
            replies { id createdAt mailboxId mailId status reason subject to sendAfter sentAt }
          }
        }
        """
}

// MARK: - Calls

extension Connection {
    /// The agent's upkeep settings, or nil while the person has no agent.
    public func agentUpkeep() async throws -> AgentUpkeepPreferences? {
        struct Answer: Decodable, Sendable {
            struct Read: Decodable, Sendable { let agent: AgentUpkeepPreferences? }
            let ReadAgent: Read?
        }
        return try await execute(Documents.agentUpkeep, as: Answer.self).ReadAgent?.agent
    }

    /// Changes the dream hours or the alerts; what is not given stays as it
    /// is. Returns the settings as they are now.
    @discardableResult
    public func updateAgentUpkeep(
        dreamFrom: String? = nil, dreamUntil: String? = nil, isAlertsEnabled: Bool? = nil, alertQuietStart: String? = nil,
        alertQuietEnd: String? = nil, alertDailyMost: Int? = nil, lookAroundWeekday: String? = nil,
        lookAroundTime: String? = nil
    ) async throws -> AgentUpkeepPreferences? {
        struct Answer: Decodable, Sendable {
            struct Updated: Decodable, Sendable { let agent: AgentUpkeepPreferences? }
            let UpdateAgent: Updated?
        }
        var variables: [String: JSONValue] = [:]
        if let dreamFrom { variables["dreamFrom"] = .string(dreamFrom) }
        if let dreamUntil { variables["dreamUntil"] = .string(dreamUntil) }
        if let isAlertsEnabled { variables["isAlertsEnabled"] = .bool(isAlertsEnabled) }
        if let alertQuietStart { variables["alertQuietStart"] = .string(alertQuietStart) }
        if let alertQuietEnd { variables["alertQuietEnd"] = .string(alertQuietEnd) }
        if let alertDailyMost { variables["alertDailyMost"] = .number(Double(alertDailyMost)) }
        if let lookAroundWeekday { variables["lookAroundWeekday"] = .string(lookAroundWeekday) }
        if let lookAroundTime { variables["lookAroundTime"] = .string(lookAroundTime) }
        return try await execute(Documents.updateAgentUpkeep, variables: variables, as: Answer.self).UpdateAgent?.agent
    }

    /// The computers attached to the agent now.
    public func attachedComputers() async throws -> [AgentStatus.Computer] {
        struct Answer: Decodable, Sendable {
            struct Computers: Decodable, Sendable { let computers: [AgentStatus.Computer] }
            let ReadAgentComputers: Computers?
        }
        return try await execute(Documents.attachedComputers, as: Answer.self).ReadAgentComputers?.computers ?? []
    }

    /// The places the agent reads, with what became of each one's files.
    public func knowledgeSourceDetails() async throws -> (sources: [KnowledgeSource], attachments: [KnowledgeSourceAttachments]) {
        struct Answer: Decodable, Sendable {
            let ListAgentKnowledgeSources: [KnowledgeSource?]
            let ListAgentSourceAttachments: [KnowledgeSourceAttachments?]?
        }
        let answer = try await execute(Documents.knowledgeSourceDetails, as: Answer.self)
        return (answer.ListAgentKnowledgeSources.compactMap { $0 }, (answer.ListAgentSourceAttachments ?? []).compactMap { $0 })
    }

    /// The types of source installed on the server.
    public func knowledgeSourceTypes() async throws -> [KnowledgeSourceType] {
        struct Answer: Decodable, Sendable { let ListAgentSourceTypes: [KnowledgeSourceType?] }
        return try await execute(Documents.knowledgeSourceTypes, as: Answer.self).ListAgentSourceTypes.compactMap { $0 }
    }

    /// Adds a source or changes one, and returns its id.
    @discardableResult
    public func saveKnowledgeSource(_ change: KnowledgeSourceChange) async throws -> String {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let id: String }
            let SaveAgentKnowledgeSource: Saved
        }
        return try await execute(Documents.saveKnowledgeSource, variables: change.variables, as: Answer.self).SaveAgentKnowledgeSource.id
    }

    /// Reads a source again now.
    public func syncKnowledgeSource(_ sourceId: String) async throws {
        try await syncFinanceSource(sourceId)
    }

    /// Pauses reading a source, or reads it again; what it found is kept.
    public func switchKnowledgeSource(_ sourceId: String, isEnabled: Bool) async throws {
        try await switchFinanceSource(sourceId, isEnabled: isEnabled)
    }

    /// Stops reading a source and forgets the documents it found.
    public func deleteKnowledgeSource(_ sourceId: String) async throws {
        try await deleteFinanceSource(sourceId)
    }

    /// Which of a source's sealed values are kept.
    public func knowledgeSourceSecrets(_ sourceId: String) async throws -> [KnowledgeSourceSecret] {
        struct Answer: Decodable, Sendable { let ListAgentKnowledgeSourceSecrets: [KnowledgeSourceSecret?] }
        return try await execute(Documents.knowledgeSourceSecrets, variables: ["sourceId": .string(sourceId)], as: Answer.self)
            .ListAgentKnowledgeSourceSecrets.compactMap { $0 }
    }

    public func setKnowledgeSourceSecret(sourceId: String, key: String, value: String) async throws {
        struct Answer: Decodable, Sendable { let SetAgentKnowledgeSourceSecret: KnowledgeSourceSecret? }
        _ = try await execute(
            Documents.setKnowledgeSourceSecret, variables: ["sourceId": .string(sourceId), "key": .string(key), "value": .string(value)],
            as: Answer.self)
    }

    /// The files one source's reading decided against opening, and why.
    public func declinedAttachments(sourceId: String) async throws -> [DeclinedAttachment] {
        struct Answer: Decodable, Sendable { let ListAgentDeclinedAttachments: [DeclinedAttachment?] }
        return try await execute(Documents.declinedAttachments, variables: ["sourceId": .string(sourceId)], as: Answer.self)
            .ListAgentDeclinedAttachments.compactMap { $0 }
    }

    /// The newest dreams.
    public func agentDreams(first: Int = 60) async throws -> [AgentDream] {
        struct Answer: Decodable, Sendable { let ListAgentDreams: [AgentDream?] }
        return try await execute(Documents.agentDreams, variables: ["first": .number(Double(first))], as: Answer.self).ListAgentDreams.compactMap { $0 }
    }

    public func agentDreamState() async throws -> AgentDreamState {
        struct Answer: Decodable, Sendable { let AgentDreamState: AgentDreamState }
        return try await execute(Documents.agentDreamState, as: Answer.self).AgentDreamState
    }

    public func readingProgress() async throws -> ReadingProgress {
        struct Answer: Decodable, Sendable { let AgentReadingProgress: ReadingProgress }
        return try await execute(Documents.readingProgress, as: Answer.self).AgentReadingProgress
    }

    /// Queues a dream at once, whatever its hours say.
    public func dreamNow() async throws {
        struct Answer: Decodable, Sendable { let DreamAgentNow: Bool? }
        _ = try await execute(Documents.dreamNow, as: Answer.self)
    }

    /// Bootstraps, dreaming at every tick until nothing waits to be read,
    /// or goes back to the dream hours.
    public func setDreamBootstrap(_ isBootstrapping: Bool) async throws {
        struct Answer: Decodable, Sendable { let DreamAgentNow: Bool? }
        _ = try await execute(Documents.dreamBootstrap, variables: ["on": .bool(isBootstrapping)], as: Answer.self)
    }

    /// Puts back into the queue what a dream marked read in the last so
    /// many minutes, and returns how many documents that was.
    public func rereadDocuments(minutes: Int) async throws -> Int {
        struct Answer: Decodable, Sendable { let RereadAgentDocuments: Int }
        return try await execute(Documents.rereadDocuments, variables: ["minutes": .number(Double(minutes))], as: Answer.self).RereadAgentDocuments
    }

    public func agentSchedules() async throws -> [AgentSchedule] {
        struct Answer: Decodable, Sendable { let ListAgentSchedules: [AgentSchedule?] }
        return try await execute(Documents.agentSchedules, as: Answer.self).ListAgentSchedules.compactMap { $0 }
    }

    /// Adds a schedule, without an id, or changes one; what is not given
    /// stays as it is.
    public func saveAgentSchedule(
        scheduleId: String? = nil, name: String? = nil, cron: String? = nil, prompt: String? = nil, deliver: String? = nil, isEnabled: Bool? = nil
    ) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let id: String }
            let SaveAgentSchedule: Saved?
        }
        var variables: [String: JSONValue] = [:]
        if let scheduleId { variables["scheduleId"] = .string(scheduleId) }
        if let name { variables["name"] = .string(name) }
        if let cron { variables["cron"] = .string(cron) }
        if let prompt { variables["prompt"] = .string(prompt) }
        if let deliver { variables["deliver"] = .string(deliver) }
        if let isEnabled { variables["enabled"] = .bool(isEnabled) }
        _ = try await execute(Documents.saveAgentSchedule, variables: variables, as: Answer.self)
    }

    public func deleteAgentSchedule(_ scheduleId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteAgentSchedule: JSONValue? }
        _ = try await execute(Documents.deleteAgentSchedule, variables: ["scheduleId": .string(scheduleId)], as: Answer.self)
    }

    /// Runs a schedule now; the answer arrives where it says.
    public func runAgentSchedule(_ scheduleId: String) async throws {
        struct Answer: Decodable, Sendable { let RunAgentSchedule: JSONValue? }
        _ = try await execute(Documents.runAgentSchedule, variables: ["scheduleId": .string(scheduleId)], as: Answer.self)
    }

    /// Turns the morning brief on at a time and on days, or off; turned
    /// off, its schedule is kept.
    public func setDailyBrief(isEnabled: Bool, at time: String, days: [Int]) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let id: String }
            let SetAgentBrief: Saved?
        }
        var variables: [String: JSONValue] = ["enabled": .bool(isEnabled), "at": .string(time)]
        if !days.isEmpty { variables["days"] = .array(days.map { .number(Double($0)) }) }
        _ = try await execute(Documents.setDailyBrief, variables: variables, as: Answer.self)
    }

    /// Sends a brief now.
    public func runDailyBrief() async throws {
        struct Answer: Decodable, Sendable { let RunAgentBriefNow: JSONValue? }
        _ = try await execute(Documents.runDailyBrief, as: Answer.self)
    }

    /// What the agent told the person lately, and what they muted.
    public func agentAlerts() async throws -> (alerts: [AgentAlert], mutes: [AgentAlertMute]) {
        struct Answer: Decodable, Sendable {
            let ListAgentAlerts: [AgentAlert?]
            let ListAgentAlertMutes: [AgentAlertMute?]
        }
        let answer = try await execute(Documents.agentAlerts, as: Answer.self)
        return (answer.ListAgentAlerts.compactMap { $0 }, answer.ListAgentAlertMutes.compactMap { $0 })
    }

    /// Mutes what an alert was about, in one of its scopes.
    public func muteAgentAlert(_ alertId: String, muteScope: String) async throws {
        struct Answer: Decodable, Sendable { let MuteAgentAlert: JSONValue? }
        _ = try await execute(Documents.muteAgentAlert, variables: ["alertId": .string(alertId), "muteScope": .string(muteScope)], as: Answer.self)
    }

    public func unmuteAgentAlert(_ muteId: String) async throws {
        struct Answer: Decodable, Sendable { let UnmuteAgentAlert: JSONValue? }
        _ = try await execute(Documents.unmuteAgentAlert, variables: ["muteId": .string(muteId)], as: Answer.self)
    }

    /// What the agent is following for the person, the nearest day first:
    /// bills, parcels, deadlines and coming events, only those still being
    /// watched.
    public func agentFollowUps() async throws -> [AgentFollowUp] {
        struct Answer: Decodable, Sendable { let ListAgentFollowUps: [AgentFollowUp?] }
        let answer = try await execute(Documents.agentFollowUps, as: Answer.self)
        return AgentFollowUp.ordered(answer.ListAgentFollowUps.compactMap { $0 })
    }

    /// Stops following one: it is kept, and nothing more is said about it.
    public func stopAgentFollowUp(_ followUpId: String) async throws {
        struct Answer: Decodable, Sendable { let StopAgentFollowUp: JSONValue? }
        _ = try await execute(Documents.stopAgentFollowUp, variables: ["followUpId": .string(followUpId)], as: Answer.self)
    }

    /// Has the agent follow something the person names.
    public func addAgentFollowUp(_ followUp: NewAgentFollowUp) async throws {
        struct Answer: Decodable, Sendable { let AddAgentFollowUp: JSONValue? }
        _ = try await execute(Documents.addAgentFollowUp, variables: followUp.variables, as: Answer.self)
    }

    /// Stops the alerts about a kind of follow-up: one of
    /// AgentFollowUp.muteKinds.
    public func muteAgentAlertKind(_ muteTarget: String) async throws {
        struct Answer: Decodable, Sendable { let MuteAgentAlert: JSONValue? }
        _ = try await execute(Documents.muteAgentAlertKind, variables: ["muteTarget": .string(muteTarget)], as: Answer.self)
    }

    /// What the agent filed in the last so many days, newest first.
    public func learnedFacts(days: Int = 2, first: Int = 40) async throws -> [AgentLearnedFact] {
        struct Answer: Decodable, Sendable { let ListAgentLearned: [AgentLearnedFact?] }
        return try await execute(
            Documents.learnedFacts, variables: ["days": .number(Double(days)), "first": .number(Double(first))], as: Answer.self
        ).ListAgentLearned.compactMap { $0 }
    }

    public func agentCorrections() async throws -> [AgentCorrection] {
        struct Answer: Decodable, Sendable { let ListAgentCorrections: [AgentCorrection?] }
        return try await execute(Documents.agentCorrections, as: Answer.self).ListAgentCorrections.compactMap { $0 }
    }

    /// Every memory check question, the dropped ones too.
    public func memoryCheckQuestions() async throws -> [MemoryCheckQuestion] {
        struct Answer: Decodable, Sendable { let ListAgentEvaluationQuestions: [MemoryCheckQuestion?] }
        return try await execute(Documents.memoryCheckQuestions, as: Answer.self).ListAgentEvaluationQuestions.compactMap { $0 }
    }

    /// The newest gradings.
    public func memoryCheckRuns(first: Int = 16) async throws -> [MemoryCheckRun] {
        struct Answer: Decodable, Sendable { let ListAgentEvaluationRuns: [MemoryCheckRun?] }
        return try await execute(Documents.memoryCheckRuns, variables: ["first": .number(Double(first))], as: Answer.self)
            .ListAgentEvaluationRuns.compactMap { $0 }
    }

    public func memoryCheckAnswers(runId: String) async throws -> [MemoryCheckAnswer] {
        struct Answer: Decodable, Sendable { let ListAgentEvaluationAnswers: [MemoryCheckAnswer?] }
        return try await execute(Documents.memoryCheckAnswers, variables: ["runId": .string(runId)], as: Answer.self)
            .ListAgentEvaluationAnswers.compactMap { $0 }
    }

    public func updateMemoryCheckQuestion(_ questionId: String, change: MemoryCheckQuestionChange) async throws {
        struct Answer: Decodable, Sendable { let UpdateAgentEvaluationQuestion: JSONValue? }
        var variables = change.variables
        variables["questionId"] = .string(questionId)
        _ = try await execute(Documents.updateMemoryCheckQuestion, variables: variables, as: Answer.self)
    }

    /// Starts a memory check in the main conversation now.
    public func startMemoryCheck() async throws {
        struct Answer: Decodable, Sendable { let SpeakFirstNow: JSONValue? }
        _ = try await execute(Documents.startMemoryCheck, variables: ["speakFirstReason": .string("memory_check")], as: Answer.self)
    }

    /// Grades the agent against the questions now.
    public func gradeMemoryNow() async throws {
        struct Answer: Decodable, Sendable { let EvaluateAgentMemoryNow: JSONValue? }
        _ = try await execute(Documents.gradeMemoryNow, as: Answer.self)
    }

    public func connectedServers() async throws -> [AgentConnectedServer] {
        struct Answer: Decodable, Sendable { let ListAgentServers: [AgentConnectedServer?] }
        return try await execute(Documents.connectedServers, as: Answer.self).ListAgentServers.compactMap { $0 }
    }

    /// Connects to a server with the person's credential, and returns why
    /// it refused, if it did.
    public func connectServer(_ server: String, credential: String) async throws -> String? {
        struct Answer: Decodable, Sendable {
            struct Connected: Decodable, Sendable {
                let status: String
                let lastError: String?
            }
            let ConnectAgentServer: Connected
        }
        let connected = try await execute(
            Documents.connectServer, variables: ["server": .string(server), "credential": .string(credential)], as: Answer.self
        ).ConnectAgentServer
        guard connected.status == "error" else { return nil }
        return connected.lastError ?? ""
    }

    public func disconnectServer(_ server: String) async throws {
        struct Answer: Decodable, Sendable { let DisconnectAgentServer: JSONValue? }
        _ = try await execute(Documents.disconnectServer, variables: ["server": .string(server)], as: Answer.self)
    }

    public func skillSecrets() async throws -> [AgentSkillSecret] {
        struct Answer: Decodable, Sendable { let ListAgentSkillSecrets: [AgentSkillSecret?] }
        return try await execute(Documents.skillSecrets, as: Answer.self).ListAgentSkillSecrets.compactMap { $0 }
    }

    public func setSkillSecret(skill: String, key: String, value: String) async throws {
        struct Answer: Decodable, Sendable { let SetAgentSkillSecret: JSONValue? }
        _ = try await execute(Documents.setSkillSecret, variables: ["skill": .string(skill), "key": .string(key), "value": .string(value)], as: Answer.self)
    }

    public func clearSkillSecret(skill: String, key: String) async throws {
        struct Answer: Decodable, Sendable { let ClearAgentSkillSecret: JSONValue? }
        _ = try await execute(Documents.clearSkillSecret, variables: ["skill": .string(skill), "key": .string(key)], as: Answer.self)
    }

    /// The reach of each skill and server, and the computers attached.
    public func agentReaches() async throws -> (reaches: [AgentReach], computers: [AgentStatus.Computer]) {
        struct Answer: Decodable, Sendable {
            struct Computers: Decodable, Sendable { let computers: [AgentStatus.Computer] }
            let ListAgentReaches: [AgentReach?]
            let ReadAgentComputers: Computers?
        }
        let answer = try await execute(Documents.agentReaches, as: Answer.self)
        return (answer.ListAgentReaches.compactMap { $0 }, answer.ReadAgentComputers?.computers ?? [])
    }

    /// Sends a skill's or server's requests through a computer, or through
    /// this server with an empty name.
    public func setAgentReach(kind: String, name: String, computerName: String) async throws {
        struct Answer: Decodable, Sendable { let SetAgentReach: JSONValue? }
        _ = try await execute(
            Documents.setAgentReach, variables: ["kind": .string(kind), "name": .string(name), "computerName": .string(computerName)], as: Answer.self)
    }

    public func chatChannels() async throws -> [AgentChatChannel] {
        struct Answer: Decodable, Sendable { let ListAgentChannels: [AgentChatChannel?] }
        return try await execute(Documents.chatChannels, as: Answer.self).ListAgentChannels.compactMap { $0 }
    }

    /// Keeps a bot's token, turns a bot on or off, or both.
    public func setChatChannel(kind: String, token: String? = nil, isEnabled: Bool? = nil) async throws {
        struct Answer: Decodable, Sendable { let SetAgentChannel: AgentChatChannel? }
        var variables: [String: JSONValue] = ["kind": .string(kind)]
        if let token, !token.isEmpty { variables["token"] = .string(token) }
        if let isEnabled { variables["enabled"] = .bool(isEnabled) }
        _ = try await execute(Documents.setChatChannel, variables: variables, as: Answer.self)
    }

    public func unlinkChatChannel(kind: String) async throws {
        struct Answer: Decodable, Sendable { let UnlinkAgentChannel: JSONValue? }
        _ = try await execute(Documents.unlinkChatChannel, variables: ["kind": .string(kind)], as: Answer.self)
    }

    public func removeChatChannel(kind: String) async throws {
        struct Answer: Decodable, Sendable { let RemoveAgentChannel: JSONValue? }
        _ = try await execute(Documents.removeChatChannel, variables: ["kind": .string(kind)], as: Answer.self)
    }

    public func backgroundWork() async throws -> [AgentBackgroundWork] {
        struct Answer: Decodable, Sendable { let ListAgentBackgroundWork: [AgentBackgroundWork?] }
        return try await execute(Documents.backgroundWork, as: Answer.self).ListAgentBackgroundWork.compactMap { $0 }
    }

    /// One piece of work with what came of it.
    public func backgroundWorkResult(_ workId: String) async throws -> AgentBackgroundWork? {
        struct Answer: Decodable, Sendable { let GetAgentBackgroundWork: AgentBackgroundWork? }
        return try await execute(Documents.backgroundWorkResult, variables: ["id": .string(workId)], as: Answer.self).GetAgentBackgroundWork
    }

    public func stopBackgroundWork(_ workId: String) async throws {
        struct Answer: Decodable, Sendable { let StopAgentBackgroundWork: JSONValue? }
        _ = try await execute(Documents.stopBackgroundWork, variables: ["id": .string(workId)], as: Answer.self)
    }

    /// A page of the runs, newest first, narrowed to one dream's job, to
    /// some kinds, or to titles holding some words.
    public func agentRuns(first: Int = 50, offset: Int = 0, jobId: String? = nil, jobKinds: [String] = [], query: String = "") async throws
        -> AgentRunPage
    {
        struct Answer: Decodable, Sendable { let ListAgentRuns: AgentRunPage }
        var variables: [String: JSONValue] = ["first": .number(Double(first)), "offset": .number(Double(offset))]
        if let jobId, !jobId.isEmpty { variables["jobId"] = .string(jobId) }
        if !jobKinds.isEmpty { variables["kinds"] = .array(jobKinds.map { .string($0) }) }
        let words = query.trimmingCharacters(in: .whitespaces)
        if !words.isEmpty { variables["query"] = .string(words) }
        return try await execute(Documents.agentRuns, variables: variables, as: Answer.self).ListAgentRuns
    }

    /// The newest replies the agent answered, held or declined.
    public func agentReplies(first: Int = 20) async throws -> AgentReplyPage {
        struct Answer: Decodable, Sendable { let ListAgentReplies: AgentReplyPage }
        return try await execute(Documents.agentReplies, variables: ["first": .number(Double(first))], as: Answer.self).ListAgentReplies
    }
}
