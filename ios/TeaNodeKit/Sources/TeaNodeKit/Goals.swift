import Foundation

/// AgentGoal is something the agent works toward on its own, in a
/// conversation of its own.
public struct AgentGoal: Codable, Hashable, Sendable, Identifiable {
    public struct Activity: Codable, Hashable, Sendable, Identifiable {
        public let id: String
        public let createdAt: String
        public let goalActivityKind: String
        public let activityHeadline: String
        public let activityDetail: String
    }

    public let conversationId: String
    public let goalTitle: String
    public let goalDescription: String
    /// working, waiting, paused, met or dropped.
    public let goalState: String
    public let goalStatus: String
    /// When its turns are, in the person's zone: a cron line such as
    /// "30 7 * * 1-5" or a moment "@at 2030-09-12 09:00". Empty means the
    /// agent keeps its own pace.
    public let goalTimes: [String]
    /// What a message must match for it to take a turn when mail arrives;
    /// empty for none.
    public let goalMailConditions: [MailRule.Condition]
    /// person, agent or built_in (the daily brief and asset estimates).
    public let goalWrittenBy: String
    /// Whether one quiet notification is sent when a turn's first card is
    /// posted.
    public let isGoalNotifiedWhenPosted: Bool
    public let goalSetAt: String?
    public let goalNextAt: String?
    public let lastAt: String?
    public let goalOriginTitle: String?
    public let activity: [Activity?]?

    enum CodingKeys: String, CodingKey {
        case conversationId, goalTitle, goalDescription, goalState, goalStatus, goalTimes, goalMailConditions, goalWrittenBy
        case isGoalNotifiedWhenPosted, goalSetAt, goalNextAt, lastAt, goalOriginTitle, activity
    }

    public init(
        conversationId: String, goalTitle: String, goalDescription: String = "", goalState: String = "working", goalStatus: String = "",
        goalTimes: [String] = [], goalMailConditions: [MailRule.Condition] = [], goalWrittenBy: String = "person",
        isGoalNotifiedWhenPosted: Bool = false, goalSetAt: String? = nil, goalNextAt: String? = nil, lastAt: String? = nil,
        goalOriginTitle: String? = nil, activity: [Activity?]? = nil
    ) {
        self.conversationId = conversationId
        self.goalTitle = goalTitle
        self.goalDescription = goalDescription
        self.goalState = goalState
        self.goalStatus = goalStatus
        self.goalTimes = goalTimes
        self.goalMailConditions = goalMailConditions
        self.goalWrittenBy = goalWrittenBy
        self.isGoalNotifiedWhenPosted = isGoalNotifiedWhenPosted
        self.goalSetAt = goalSetAt
        self.goalNextAt = goalNextAt
        self.lastAt = lastAt
        self.goalOriginTitle = goalOriginTitle
        self.activity = activity
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        conversationId = try container.decode(String.self, forKey: .conversationId)
        goalTitle = try container.decode(String.self, forKey: .goalTitle)
        goalDescription = try container.decodeIfPresent(String.self, forKey: .goalDescription) ?? ""
        goalState = try container.decode(String.self, forKey: .goalState)
        goalStatus = try container.decodeIfPresent(String.self, forKey: .goalStatus) ?? ""
        goalTimes = try container.decodeIfPresent([String].self, forKey: .goalTimes) ?? []
        goalMailConditions = try container.decodeIfPresent([MailRule.Condition].self, forKey: .goalMailConditions) ?? []
        goalWrittenBy = try container.decodeIfPresent(String.self, forKey: .goalWrittenBy) ?? "person"
        isGoalNotifiedWhenPosted = try container.decodeIfPresent(Bool.self, forKey: .isGoalNotifiedWhenPosted) ?? false
        goalSetAt = try container.decodeIfPresent(String.self, forKey: .goalSetAt)
        goalNextAt = try container.decodeIfPresent(String.self, forKey: .goalNextAt)
        lastAt = try container.decodeIfPresent(String.self, forKey: .lastAt)
        goalOriginTitle = try container.decodeIfPresent(String.self, forKey: .goalOriginTitle)
        activity = try container.decodeIfPresent([Activity?].self, forKey: .activity)
    }

    public var id: String { conversationId }
    public var isOpen: Bool { goalState == "working" || goalState == "waiting" }
    /// The person switched it off: no turns, no mail.
    public var isPaused: Bool { goalState == "paused" }
    /// Run now is offered only to a goal that is working.
    public var canRunNow: Bool { goalState == "working" }
    /// Whether it takes turns on a clock or for mail, not only at its own pace.
    public var hasTimes: Bool { !goalTimes.isEmpty || !goalMailConditions.isEmpty }
}

/// GoalMoment is one moment a goal takes a turn, as "@at 2030-09-12 09:00"
/// in the person's zone.
public struct GoalMoment: Hashable, Sendable {
    public var year: Int
    public var month: Int
    public var day: Int
    public var hour: Int
    public var minute: Int

    public init(year: Int, month: Int, day: Int, hour: Int, minute: Int) {
        self.year = year
        self.month = month
        self.day = day
        self.hour = hour
        self.minute = minute
    }

    /// Reads "@at 2030-09-12 09:00" (seconds are ignored), or nil for any
    /// other text.
    public init?(text: String) {
        let pieces = text.split(separator: " ", omittingEmptySubsequences: true).map(String.init)
        guard pieces.count == 3, pieces[0] == "@at" else { return nil }
        let dayPieces = pieces[1].split(separator: "-", omittingEmptySubsequences: false).compactMap { Int($0) }
        let clockPieces = pieces[2].split(separator: ":", omittingEmptySubsequences: false).compactMap { Int($0) }
        guard dayPieces.count == 3, clockPieces.count >= 2, (1...12).contains(dayPieces[1]), (1...31).contains(dayPieces[2]),
            (0..<24).contains(clockPieces[0]), (0..<60).contains(clockPieces[1])
        else { return nil }
        self.init(year: dayPieces[0], month: dayPieces[1], day: dayPieces[2], hour: clockPieces[0], minute: clockPieces[1])
    }

    /// As the server takes it.
    public var text: String {
        func twoDigits(_ number: Int) -> String { number < 10 ? "0\(number)" : String(number) }
        return "@at \(year)-\(twoDigits(month))-\(twoDigits(day)) \(twoDigits(hour)):\(twoDigits(minute))"
    }

    /// The moment as a date in the calendar's zone, for a date picker.
    public func date(calendar: Calendar = .current) -> Date {
        calendar.date(from: DateComponents(year: year, month: month, day: day, hour: hour, minute: minute)) ?? Date(timeIntervalSinceReferenceDate: 0)
    }

    /// A date picker's date as a moment, in the calendar's zone.
    public init(date: Date, calendar: Calendar = .current) {
        let parts = calendar.dateComponents([.year, .month, .day, .hour, .minute], from: date)
        self.init(year: parts.year ?? 2000, month: parts.month ?? 1, day: parts.day ?? 1, hour: parts.hour ?? 0, minute: parts.minute ?? 0)
    }

    private var utcDate: Date {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC") ?? calendar.timeZone
        return calendar.date(from: DateComponents(year: year, month: month, day: day, hour: hour, minute: minute)) ?? Date(timeIntervalSinceReferenceDate: 0)
    }

    private var utcTimeZone: TimeZone { TimeZone(identifier: "UTC") ?? .current }

    /// The day as the locale writes it, "Dec 11" or "11 Dec", with the
    /// year when asked.
    public func dayText(locale: Locale = .current, includesYear: Bool = false) -> String {
        var style = Date.FormatStyle().month(.abbreviated).day()
        if includesYear { style = Date.FormatStyle().year().month(.abbreviated).day() }
        style.locale = locale
        style.timeZone = utcTimeZone
        return utcDate.formatted(style)
    }

    /// The time of day as the locale writes it: "9:00 AM", "09:00".
    public func timeText(locale: Locale = .current) -> String {
        var style = Date.FormatStyle(date: .omitted, time: .shortened)
        style.locale = locale
        style.timeZone = utcTimeZone
        return utcDate.formatted(style)
    }
}

/// GoalTime is one of a goal's times: a plain weekly line, a moment, or a
/// line of any other kind that is shown as it is written.
public enum GoalTime: Hashable, Sendable {
    case weekly(ScheduleTiming)
    case moment(GoalMoment)
    case other(String)

    public init(_ text: String) {
        if let moment = GoalMoment(text: text) {
            self = .moment(moment)
        } else if let timing = ScheduleTiming(cron: text) {
            self = .weekly(timing)
        } else {
            self = .other(text)
        }
    }

    /// As the server takes it.
    public var text: String {
        switch self {
        case .weekly(let timing): timing.cron
        case .moment(let moment): moment.text
        case .other(let line): line
        }
    }
}

/// AgentGoalChange is what a goal is started with or changed by: only what
/// is not nil is sent, and an empty list of times or conditions takes them
/// away.
public struct AgentGoalChange: Hashable, Sendable {
    public var goalTitle: String?
    public var goalDescription: String?
    public var goalTimes: [String]?
    public var goalMailConditions: [MailRule.Condition]?
    public var isGoalNotifiedWhenPosted: Bool?

    public init(
        goalTitle: String? = nil, goalDescription: String? = nil, goalTimes: [String]? = nil, goalMailConditions: [MailRule.Condition]? = nil,
        isGoalNotifiedWhenPosted: Bool? = nil
    ) {
        self.goalTitle = goalTitle
        self.goalDescription = goalDescription
        self.goalTimes = goalTimes
        self.goalMailConditions = goalMailConditions
        self.isGoalNotifiedWhenPosted = isGoalNotifiedWhenPosted
    }

    /// The variables to send: only what is set.
    var variables: [String: JSONValue] {
        var variables: [String: JSONValue] = [:]
        if let goalTitle { variables["goalTitle"] = .string(goalTitle) }
        if let goalDescription { variables["goalDescription"] = .string(goalDescription) }
        if let goalTimes { variables["goalTimes"] = .array(goalTimes.map { .string($0) }) }
        if let goalMailConditions { variables["goalMailConditions"] = GoalMailTrigger.variables(goalMailConditions) }
        if let isGoalNotifiedWhenPosted { variables["isGoalNotifiedWhenPosted"] = .bool(isGoalNotifiedWhenPosted) }
        return variables
    }
}

extension Documents {
    static let goalFields = """
        conversationId goalTitle goalDescription goalState goalStatus goalTimes goalMailConditions { field header operator value } goalWrittenBy isGoalNotifiedWhenPosted goalSetAt goalNextAt goalSurfacedAt lastAt
        """
    public static let goals = """
        query { ListAgentGoals { \(goalFields) } }
        """
    public static let goal = """
        query ($conversationId: String!) {
          GetAgentGoal(conversationId: $conversationId) {
            \(goalFields)
            goalOriginTitle
            activity { id createdAt goalActivityKind activityHeadline activityDetail }
          }
        }
        """
    public static let startGoal = """
        mutation ($goalTitle: String!, $goalDescription: String!, $goalTimes: [String!], $goalMailConditions: [MailboxRuleConditionInput!], $isGoalNotifiedWhenPosted: Boolean) {
          StartAgentGoal(goalTitle: $goalTitle, goalDescription: $goalDescription, goalTimes: $goalTimes, goalMailConditions: $goalMailConditions, isGoalNotifiedWhenPosted: $isGoalNotifiedWhenPosted) { conversationId }
        }
        """
    public static let updateGoal = """
        mutation ($conversationId: String!, $goalTitle: String, $goalDescription: String, $goalTimes: [String!], $goalMailConditions: [MailboxRuleConditionInput!], $isGoalNotifiedWhenPosted: Boolean) {
          UpdateAgentGoal(conversationId: $conversationId, goalTitle: $goalTitle, goalDescription: $goalDescription, goalTimes: $goalTimes, goalMailConditions: $goalMailConditions, isGoalNotifiedWhenPosted: $isGoalNotifiedWhenPosted) { conversationId }
        }
        """
    public static let runGoal = """
        mutation ($conversationId: String!) {
          RunAgentGoal(conversationId: $conversationId) { conversationId }
        }
        """
    public static let tellGoal = """
        mutation ($conversationId: String!, $text: String!) {
          TellAgentGoal(conversationId: $conversationId, text: $text) { conversationId }
        }
        """
    public static let setGoalState = """
        mutation ($conversationId: String!, $goalState: String!) {
          SetAgentGoalState(conversationId: $conversationId, goalState: $goalState) { conversationId }
        }
        """
}

extension Connection {
    public func goals() async throws -> [AgentGoal] {
        struct Answer: Decodable, Sendable { let ListAgentGoals: [AgentGoal?] }
        return try await execute(Documents.goals, as: Answer.self).ListAgentGoals.compactMap { $0 }
    }

    public func goal(_ conversationId: String) async throws -> AgentGoal? {
        struct Answer: Decodable, Sendable { let GetAgentGoal: AgentGoal? }
        return try await execute(Documents.goal, variables: ["conversationId": .string(conversationId)], as: Answer.self).GetAgentGoal
    }

    /// Starts a goal, with when it runs if the change says; returns its
    /// conversation.
    public func startGoal(_ change: AgentGoalChange) async throws -> String {
        struct Answer: Decodable, Sendable {
            struct Started: Decodable, Sendable { let conversationId: String }
            let StartAgentGoal: Started
        }
        var variables = change.variables
        if variables["goalTitle"] == nil { variables["goalTitle"] = .string("") }
        if variables["goalDescription"] == nil { variables["goalDescription"] = .string("") }
        return try await execute(Documents.startGoal, variables: variables, as: Answer.self).StartAgentGoal.conversationId
    }

    /// Changes a goal: only what the change holds is sent, and an empty list
    /// of times or mail conditions takes them away.
    public func updateGoal(_ conversationId: String, change: AgentGoalChange) async throws {
        struct Answer: Decodable, Sendable {
            struct Updated: Decodable, Sendable { let conversationId: String }
            let UpdateAgentGoal: Updated?
        }
        var variables = change.variables
        variables["conversationId"] = .string(conversationId)
        _ = try await execute(Documents.updateGoal, variables: variables, as: Answer.self)
    }

    /// Gives a working goal a turn now; the server refuses any other.
    public func runGoal(_ conversationId: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Ran: Decodable, Sendable { let conversationId: String }
            let RunAgentGoal: Ran?
        }
        _ = try await execute(Documents.runGoal, variables: ["conversationId": .string(conversationId)], as: Answer.self)
    }

    public func tellGoal(_ conversationId: String, text: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Told: Decodable, Sendable { let conversationId: String }
            let TellAgentGoal: Told?
        }
        _ = try await execute(Documents.tellGoal, variables: ["conversationId": .string(conversationId), "text": .string(text)], as: Answer.self)
    }

    /// Marks a goal met, dropped or paused, or opens it again as working.
    public func setGoalState(_ conversationId: String, to state: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Set: Decodable, Sendable { let conversationId: String }
            let SetAgentGoalState: Set?
        }
        _ = try await execute(Documents.setGoalState, variables: ["conversationId": .string(conversationId), "goalState": .string(state)], as: Answer.self)
    }
}
