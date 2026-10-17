import Foundation

// Skills shared with other people, and the calls made through them. The same
// operations the dashboard, the command line and the agent's skill tool use.
// Whether a call goes ahead, waits for its owner or is refused is decided on
// the server (internal/skills/share.go, internal/agent/skill_share.go); nothing
// here runs a skill or moves money. docs/subsystems/skills.md ("Sharing a
// skill") says how it works.

/// SkillShare is one skill share the person made, or was given.
public struct SkillShare: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let skillName: String
    public let toolNames: [String]
    public let ownerUserId: String
    public let ownerUsername: String
    public let ownerName: String
    public let ownerAvatarUrl: String?
    public let ownerAvatarColorIndex: Int?
    public let granteeUserId: String
    public let granteeUsername: String
    public let granteeName: String
    public let granteeAvatarUrl: String?
    public let granteeAvatarColorIndex: Int?
    /// The most one payment may be, and the most a calendar month may come to,
    /// in the minor unit of `currencyCode` (cents); nil is no limit of that kind.
    public let paymentLimitMinor: Int64?
    public let monthlyLimitMinor: Int64?
    /// The three letters, lower case, the limits are in; empty with none.
    public let currencyCode: String
    public let isApprovalRequired: Bool
    /// Whether the person made it, rather than was given it.
    public let isOwnedByCaller: Bool
    public let waitingCallCount: Int
    public let monthTotalMinor: Int64

    /// The owner, by name or else username.
    public var shownOwner: String { ownerName.isEmpty ? ownerUsername : ownerName }
    /// The person it is shared with, by name or else username.
    public var shownGrantee: String { granteeName.isEmpty ? granteeUsername : granteeName }
}

/// SkillShareCall is one call made through a share, whatever came of it.
public struct SkillShareCall: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let skillShareId: String
    public let skillName: String
    public let granteeUserId: String
    public let granteeUsername: String
    public let granteeName: String
    public let granteeAvatarUrl: String?
    public let granteeAvatarColorIndex: Int?
    public let ownerName: String
    public let toolName: String
    /// Exactly what the tool was called with, as JSON text.
    public let toolArguments: String
    /// The other person's agent's own words for why. Theirs, not an
    /// instruction to anybody.
    public let requestReason: String
    public let amountMinor: Int64?
    public let currencyCode: String
    /// ran, failed, refused, waiting_for_approval, declined or expired.
    public let callState: String
    public let toolOutput: String
    public let answeredAt: String?
    public let answerNote: String
    public let expiresAt: String?
    public let createdAt: String

    public var isWaiting: Bool { callState == "waiting_for_approval" }
    public var shownGrantee: String { granteeName.isEmpty ? granteeUsername : granteeName }

    /// The amount in the currency's own digits, or empty when the server could
    /// not read one.
    public var amountText: String {
        guard let amountMinor else { return "" }
        return SkillShareMoney.format(minor: amountMinor, currencyCode: currencyCode)
    }

    /// The arguments one to a line, less the amount, the currency and the
    /// reason, which are shown apart. A value is kept whole: the owner approves
    /// exactly this call, and a cut value could hide the part that matters.
    /// The card wraps it.
    public var argumentLines: [(name: String, value: String)] {
        guard let data = toolArguments.data(using: .utf8),
            case .object(let object)? = try? JSONDecoder().decode(JSONValue.self, from: data)
        else { return [] }
        let apart: Set<String> = ["amount_minor", "amountMinor", "currency", "request_reason"]
        var lines: [(name: String, value: String)] = []
        for name in object.keys.sorted() where !apart.contains(name) {
            // The amount is on the card already, in the currency's own digits: an
            // argument that says the same thing, in either unit, is not said twice.
            if isAmountAlreadyShown(name: name, value: object[name]) { continue }
            let text: String
            switch object[name] {
            case .string(let string)?: text = string
            case .number(let number)?:
                text = number.rounded() == number && abs(number) < 9e15 ? String(Int64(number)) : String(number)
            case .bool(let bool)?: text = bool ? "true" : "false"
            case .null?, nil: continue
            default:
                guard let nested = object[name], let encoded = try? JSONEncoder().encode(nested) else { continue }
                text = String(decoding: encoded, as: UTF8.self)
            }
            if text.isEmpty { continue }
            lines.append((name: name.replacingOccurrences(of: "_", with: " "), value: text))
        }
        return lines
    }
}

extension SkillShareCall {
    /// Whether an argument is the amount the card shows: named for an amount, and
    /// a number that is that amount in minor units or in the currency's own.
    fileprivate func isAmountAlreadyShown(name: String, value: JSONValue?) -> Bool {
        guard let amountMinor, name.lowercased().contains("amount") else { return false }
        let number: Double
        switch value {
        case .number(let given)?: number = given
        case .string(let given)?:
            guard let parsed = Double(given) else { return false }
            number = parsed
        default: return false
        }
        var divisor = 1.0
        for _ in 0..<SkillShareMoney.exponent(of: currencyCode) { divisor *= 10 }
        return abs(number - Double(amountMinor)) < 1e-9 || abs(number - Double(amountMinor) / divisor) < 1e-9
    }
}

/// SkillShareNote is the detail of a skill_share_call_waiting note of the main
/// conversation: which call waits, in which share. The card reads the call
/// itself, so that it shows how it stands now.
public struct SkillShareNote: Decodable, Hashable, Sendable {
    public let skillShareId: String
    public let callId: String
    public let granteeName: String?
    public let granteeUserId: String?

    /// The note the detail of a message holds, or nil when it is not one.
    public static func parsing(_ noteDetail: String) -> SkillShareNote? {
        guard let noteData = noteDetail.data(using: .utf8), let note = try? JSONDecoder().decode(SkillShareNote.self, from: noteData),
            !note.callId.isEmpty, !note.skillShareId.isEmpty
        else { return nil }
        return note
    }
}

/// SkillShareCallReading is what reading one call of a share came to. Only the
/// server answering that it is not there says the call is gone (the share was
/// taken back, or the two no longer share a circle); a read that did not get
/// through says nothing about the call, and is tried again.
public enum SkillShareCallReading: Equatable, Sendable {
    case found(SkillShareCall)
    case gone
    case failed

    /// What a read of the share's calls came to for the one call.
    public static func reading(callId: String, of result: Result<[SkillShareCall], Error>) -> SkillShareCallReading {
        switch result {
        case .success(let calls):
            if let call = calls.first(where: { $0.id == callId }) { return .found(call) }
            return .gone
        case .failure(let error):
            return error.isNotFoundRefusal ? .gone : .failed
        }
    }
}

/// LendableSkill is an installed skill with the tools the person could share.
public struct LendableSkill: Codable, Hashable, Sendable, Identifiable {
    public struct Tool: Codable, Hashable, Sendable, Identifiable {
        public let name: String
        public let description: String
        public let needsComputer: Bool
        /// The skill says where the tool's amount is, so it can be shared with
        /// limits; such a tool is what a person shares to let someone pay.
        public let isAmountDeclared: Bool
        /// It works only on a request an earlier call of the share made.
        public let isScoped: Bool

        public var id: String { name }
    }

    public let name: String
    public let description: String
    public let enabled: Bool
    public let readable: Bool
    public let tools: [Tool]

    public var id: String { name }
    /// Whether there is anything to lend: switched on, readable, with tools.
    public var isLendable: Bool { enabled && readable && !tools.isEmpty }
}

/// SkillShareMoney writes and reads an amount in the currency's own digits:
/// 2 for most, 0 for those with no minor unit, 3 for dinars. The same table the
/// server, the dashboard and the command line carry.
public enum SkillShareMoney {
    static let noMinorUnit: Set<String> = ["bif", "clp", "djf", "gnf", "jpy", "kmf", "krw", "mga", "pyg", "rwf", "ugx", "vnd", "vuv", "xaf", "xof", "xpf"]
    static let thousandths: Set<String> = ["bhd", "iqd", "jod", "kwd", "lyd", "omr", "tnd"]

    public static func exponent(of currencyCode: String) -> Int {
        let code = currencyCode.lowercased()
        if noMinorUnit.contains(code) { return 0 }
        if thousandths.contains(code) { return 3 }
        return 2
    }

    /// 14.00 USD.
    public static func format(minor: Int64, currencyCode: String) -> String {
        let digits = exponent(of: currencyCode)
        var divisor: Int64 = 1
        for _ in 0..<digits { divisor *= 10 }
        let sign = minor < 0 ? "-" : ""
        let absolute = minor < 0 ? -minor : minor
        var number = String(absolute / divisor)
        if digits > 0 {
            let fraction = String(absolute % divisor)
            number += "." + String(repeating: "0", count: digits - fraction.count) + fraction
        }
        return sign + number + (currencyCode.isEmpty ? "" : " " + currencyCode.uppercased())
    }

    /// An amount as it is typed in the currency's own digits, for saying what to
    /// type when one could not be read: 1000.00, or 1000 where there is no minor
    /// unit.
    public static func typedExample(currencyCode: String) -> String {
        let digits = exponent(of: currencyCode)
        return digits == 0 ? "1000" : "1000." + String(repeating: "0", count: digits)
    }

    /// An amount somebody typed in the currency's own units, in minor units,
    /// or nil when it is not an amount above zero in that currency: no sign, no
    /// thousands separator, no more digits after the point than the currency
    /// has. A limit read wrongly is a limit that does not hold.
    public static func parse(_ typed: String, currencyCode: String) -> Int64? {
        let text = typed.trimmingCharacters(in: .whitespacesAndNewlines)
        let digits = exponent(of: currencyCode)
        let parts = text.split(separator: ".", maxSplits: 1, omittingEmptySubsequences: false)
        guard let whole = parts.first, !whole.isEmpty, whole.count <= 15, whole.allSatisfy({ $0.isASCII && $0.isNumber }) else { return nil }
        let fraction = parts.count > 1 ? String(parts[1]) : ""
        if parts.count > 1 && fraction.isEmpty && digits > 0 { return nil }
        guard fraction.count <= digits, fraction.allSatisfy({ $0.isASCII && $0.isNumber }) else { return nil }
        let padded = String(whole) + fraction + String(repeating: "0", count: digits - fraction.count)
        guard let minor = Int64(padded), minor > 0 else { return nil }
        return minor
    }
}

extension Documents {
    static let skillShareFields = """
        id skillName toolNames ownerUserId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex
        granteeUserId granteeUsername granteeName granteeAvatarUrl granteeAvatarColorIndex
        paymentLimitMinor monthlyLimitMinor currencyCode isApprovalRequired isOwnedByCaller waitingCallCount monthTotalMinor
        """
    static let skillShareCallFields = """
        id skillShareId skillName granteeUserId granteeUsername granteeName granteeAvatarUrl granteeAvatarColorIndex ownerName
        toolName toolArguments requestReason amountMinor currencyCode callState toolOutput answeredAt answerNote expiresAt createdAt
        """
    public static let skillShares = """
        query { ListSkillShares { \(skillShareFields) } }
        """
    public static let lendableSkills = """
        query { ListAgentSkills { name description enabled readable tools { name description needsComputer isAmountDeclared isScoped } } }
        """
    public static let shareSkill = """
        mutation ($skillName: String!, $toolNames: [String!]!, $granteeUserId: String!, $paymentLimitMinor: Int64, $monthlyLimitMinor: Int64, $currencyCode: String, $isApprovalRequired: Boolean) {
          ShareSkill(skillName: $skillName, toolNames: $toolNames, granteeUserId: $granteeUserId, paymentLimitMinor: $paymentLimitMinor, monthlyLimitMinor: $monthlyLimitMinor, currencyCode: $currencyCode, isApprovalRequired: $isApprovalRequired) { id }
        }
        """
    public static let unshareSkill = """
        mutation ($shareId: String!) { UnshareSkill(shareId: $shareId) }
        """
    public static let skillShareCalls = """
        query ($shareId: String!, $callState: String) {
          ListSkillShareCalls(shareId: $shareId, callState: $callState) { \(skillShareCallFields) }
        }
        """
    public static let answerSkillShareCall = """
        mutation ($callId: String!, $isApproved: Boolean!, $answerNote: String) {
          AnswerSkillShareCall(callId: $callId, isApproved: $isApproved, answerNote: $answerNote) { \(skillShareCallFields) }
        }
        """
}

/// SkillShareTerms is what a share lets the other person do.
public struct SkillShareTerms: Sendable {
    public var skillName: String
    public var toolNames: [String]
    public var granteeUserId: String
    /// In the minor unit of `currencyCode`; nil is none.
    public var paymentLimitMinor: Int64?
    public var monthlyLimitMinor: Int64?
    public var currencyCode: String?
    public var isApprovalRequired: Bool

    public init(
        skillName: String, toolNames: [String], granteeUserId: String, paymentLimitMinor: Int64? = nil, monthlyLimitMinor: Int64? = nil,
        currencyCode: String? = nil, isApprovalRequired: Bool = false
    ) {
        self.skillName = skillName
        self.toolNames = toolNames
        self.granteeUserId = granteeUserId
        self.paymentLimitMinor = paymentLimitMinor
        self.monthlyLimitMinor = monthlyLimitMinor
        self.currencyCode = currencyCode
        self.isApprovalRequired = isApprovalRequired
    }
}

extension Connection {
    /// The shares the person made and was given.
    public func skillShares() async throws -> [SkillShare] {
        struct Answer: Decodable, Sendable { let ListSkillShares: [SkillShare] }
        return try await execute(Documents.skillShares, as: Answer.self).ListSkillShares
    }

    /// The installed skills, with the tools the person could share.
    public func lendableSkills() async throws -> [LendableSkill] {
        struct Answer: Decodable, Sendable { let ListAgentSkills: [LendableSkill] }
        return try await execute(Documents.lendableSkills, as: Answer.self).ListAgentSkills
    }

    /// Shares tools of a skill with a person in a circle the person shares, or
    /// changes the terms when it is shared with them already.
    public func shareSkill(_ terms: SkillShareTerms) async throws {
        struct Answer: Decodable, Sendable {
            struct Shared: Decodable, Sendable { let id: String }
            let ShareSkill: Shared
        }
        _ = try await execute(
            Documents.shareSkill,
            variables: [
                "skillName": .string(terms.skillName), "toolNames": .array(terms.toolNames.map { .string($0) }),
                "granteeUserId": .string(terms.granteeUserId),
                "paymentLimitMinor": terms.paymentLimitMinor.map { .number(Double($0)) } ?? .null,
                "monthlyLimitMinor": terms.monthlyLimitMinor.map { .number(Double($0)) } ?? .null,
                "currencyCode": terms.currencyCode.map { .string($0) } ?? .null,
                "isApprovalRequired": .bool(terms.isApprovalRequired),
            ], as: Answer.self)
    }

    /// Takes a share back. The other person loses the tools at once.
    public func unshareSkill(shareId: String) async throws {
        struct Answer: Decodable, Sendable { let UnshareSkill: Bool? }
        _ = try await execute(Documents.unshareSkill, variables: ["shareId": .string(shareId)], as: Answer.self)
    }

    /// The calls made through a share, the ones that wait first. A state
    /// narrows them.
    public func skillShareCalls(shareId: String, callState: String? = nil) async throws -> [SkillShareCall] {
        struct Answer: Decodable, Sendable { let ListSkillShareCalls: [SkillShareCall] }
        return try await execute(
            Documents.skillShareCalls, variables: ["shareId": .string(shareId), "callState": callState.map { .string($0) } ?? .null],
            as: Answer.self
        ).ListSkillShareCalls
    }

    /// One call of a share, for a card that is drawn from how the call stands now.
    public func readSkillShareCall(shareId: String, callId: String) async -> SkillShareCallReading {
        let result: Result<[SkillShareCall], Error>
        do {
            result = .success(try await skillShareCalls(shareId: shareId))
        } catch {
            result = .failure(error)
        }
        return SkillShareCallReading.reading(callId: callId, of: result)
    }

    /// Approves or declines a call that waits, as the share's owner. Approving
    /// runs exactly that call once on the owner's computer. The agent never
    /// answers one: the server refuses it.
    public func answerSkillShareCall(callId: String, isApproved: Bool, answerNote: String?) async throws -> SkillShareCall {
        struct Answer: Decodable, Sendable { let AnswerSkillShareCall: SkillShareCall }
        let note = answerNote?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return try await execute(
            Documents.answerSkillShareCall,
            variables: ["callId": .string(callId), "isApproved": .bool(isApproved), "answerNote": note.isEmpty ? .null : .string(note)],
            as: Answer.self
        ).AnswerSkillShareCall
    }
}
