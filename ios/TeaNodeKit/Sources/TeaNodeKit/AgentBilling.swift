import Foundation

// MARK: - Model prices checked against the provider's bill

/// AgentBilledPrice is one price the check set beside the bill: what the
/// settings say for a model and a kind of token, and what the bill implies,
/// each per million tokens.
public struct AgentBilledPrice: Codable, Hashable, Sendable, Identifiable {
    public let model: String
    /// input, cachedInput, cacheWrite, output or audioInput.
    public let tokenKind: String
    public let configuredPerMillion: Double
    public let billedPerMillion: Double
    public let dayCount: Int
    /// Whether the configured price comes from an entry whose model is a
    /// pattern covering other models too. The check reports such a price and
    /// never changes it.
    public let isPattern: Bool

    public var id: String { "\(model)/\(tokenKind)" }

    public init(
        model: String, tokenKind: String, configuredPerMillion: Double, billedPerMillion: Double, dayCount: Int, isPattern: Bool = false
    ) {
        self.model = model
        self.tokenKind = tokenKind
        self.configuredPerMillion = configuredPerMillion
        self.billedPerMillion = billedPerMillion
        self.dayCount = dayCount
        self.isPattern = isPattern
    }

    /// Whether the settings and the bill disagree by more than rounding.
    public var isChanged: Bool { configuredPerMillion != billedPerMillion }
}

/// AgentPriceChange is one price a check set from the bill.
public struct AgentPriceChange: Codable, Hashable, Sendable, Identifiable {
    public let model: String
    public let tokenKind: String
    public let beforePerMillion: Double
    public let afterPerMillion: Double

    public var id: String { "\(model)/\(tokenKind)" }

    public init(model: String, tokenKind: String, beforePerMillion: Double, afterPerMillion: Double) {
        self.model = model
        self.tokenKind = tokenKind
        self.beforePerMillion = beforePerMillion
        self.afterPerMillion = afterPerMillion
    }
}

/// AgentBillingCheck is the last check of one provider's prices against its
/// bill.
public struct AgentBillingCheck: Codable, Hashable, Sendable, Identifiable {
    public let providerName: String
    /// Go writes its zero time for a provider never checked.
    public let checkedAt: String
    /// Why the bill could not be read, such as a refused key; empty when it was.
    public let errorMessage: String
    public let compared: [AgentBilledPrice]
    public let changed: [AgentPriceChange]

    public var id: String { providerName }

    public init(
        providerName: String, checkedAt: String, errorMessage: String = "", compared: [AgentBilledPrice] = [], changed: [AgentPriceChange] = []
    ) {
        self.providerName = providerName
        self.checkedAt = checkedAt
        self.errorMessage = errorMessage
        self.compared = compared
        self.changed = changed
    }

    /// When it was checked; nil for a provider that never was.
    public var checkedDate: Date? {
        guard let date = ServerTime.date(checkedAt), Calendar(identifier: .gregorian).component(.year, from: date) > 1 else { return nil }
        return date
    }
}

extension Documents {
    static let agentBillingCheckFields = """
        providerName checkedAt errorMessage
        compared { model tokenKind configuredPerMillion billedPerMillion dayCount isPattern }
        changed { model tokenKind beforePerMillion afterPerMillion }
        """
    public static let agentBillingChecks = """
        query { AgentBillingChecks { \(agentBillingCheckFields) } }
        """
    public static let checkAgentProviderPrices = """
        mutation { CheckAgentProviderPrices { \(agentBillingCheckFields) } }
        """
}

extension Connection {
    /// What the last check of each provider that has a billing key compared
    /// and changed. A provider never checked is listed with no time.
    public func agentBillingChecks() async throws -> [AgentBillingCheck] {
        struct Answer: Decodable, Sendable { let AgentBillingChecks: [AgentBillingCheck] }
        return try await execute(Documents.agentBillingChecks, as: Answer.self).AgentBillingChecks
    }

    /// Checks the prices of every provider that has a billing key against
    /// its bill now, changes the ones that differ, and answers with what was
    /// compared and changed. The server takes no provider: one check covers
    /// them all.
    public func checkAgentProviderPrices() async throws -> [AgentBillingCheck] {
        struct Answer: Decodable, Sendable { let CheckAgentProviderPrices: [AgentBillingCheck] }
        return try await execute(Documents.checkAgentProviderPrices, as: Answer.self).CheckAgentProviderPrices
    }
}
