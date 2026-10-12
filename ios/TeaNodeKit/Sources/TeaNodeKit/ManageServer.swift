import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

// The dashboard's operator pages for the server and the agent: what this
// instance is and which version it runs, the server's own settings, the
// services it talks to, and every agent on it with what they cost, the runs
// they made, and the operator's agent settings, skills and source types.

// MARK: - Who may open these pages

extension OperatorAccess {
    /// Whether a server permission is held, the way the dashboard's
    /// hasPermission reads it: everywhere, never through a domain.
    public func holdsEverywhere(_ permission: String) -> Bool {
        everywherePermissions.contains(permission)
    }

    /// The server's pages, its settings and the operator's agent settings
    /// need server:manage.
    public var canManageServer: Bool { holdsEverywhere("server:manage") }
    /// Everybody's agents, their limits and what they cost need agent:audit.
    public var canAuditAgents: Bool { holdsEverywhere("agent:audit") }
    /// The runs are content, and are behind agent:act.
    public var canActAsAgents: Bool { holdsEverywhere("agent:act") }

    /// Whether the agents page is offered at all, as the dashboard's rail
    /// decides it.
    public var canOpenAgentAdmin: Bool { canAuditAgents || canActAsAgents }

    /// The agents page's tabs this person may open, in the dashboard's
    /// order: the runs need agent:act, and the operator's settings, General
    /// among them, need server:manage.
    public var agentAdminTabs: [AgentAdminTab] {
        AgentAdminTab.allCases.filter { tab in
            if tab == .runs { return canActAsAgents }
            if tab.isOperatorSettings { return canManageServer }
            return true
        }
    }
}

/// ServerSettingsTab is one tab of the dashboard's Server page, in its
/// order: what this instance is, what is still missing, then the settings in
/// the order mail moves through them.
public enum ServerSettingsTab: String, CaseIterable, Sendable, Identifiable {
    case about, setup, identity, mail, sending, listeners, certificates, storage, resolver, scanning, spam, sessions, sso

    public var id: String { rawValue }

    /// The tabs that configure the services this server talks to rather
    /// than the server itself.
    public static let integrationTabs: [ServerSettingsTab] = [.sending, .certificates, .storage, .scanning, .spam, .sso]
}

/// AgentAdminTab is one tab of the dashboard's agents page.
public enum AgentAdminTab: String, CaseIterable, Sendable, Identifiable {
    case general, usage, runs, models, tools, skills, sources

    public var id: String { rawValue }

    /// Whether it is one of the operator's agent settings, which need
    /// server:manage.
    public var isOperatorSettings: Bool { self != .usage && self != .runs }
}

// MARK: - Words the forms share

/// SettingsList is how a list is typed into one field, as the dashboard's
/// forms take it.
public enum SettingsList {
    /// Comma separated, as the server's forms and the configuration file
    /// write a list.
    public static func items(_ typed: String) -> [String] {
        typed.split(separator: ",").map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty }
    }

    /// Commas or lines, as the agent's forms take a list.
    public static func lineItems(_ typed: String) -> [String] {
        typed.split(whereSeparator: { $0 == "," || $0 == "\n" }).map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
    }

    /// A list as one field shows it.
    public static func text(_ items: [String]?) -> String {
        (items ?? []).joined(separator: ", ")
    }

    /// A list as JSON for a variable.
    static func value(_ items: [String]) -> JSONValue {
        .array(items.map { .string($0) })
    }

    /// What a number field holds, as the dashboard's Number(typed) || 0
    /// reads it.
    public static func number(_ typed: String) -> Double {
        let number = Double(typed.trimmingCharacters(in: .whitespaces)) ?? 0
        return number.isFinite ? number : 0
    }

    /// A field's digits only, as the dashboard's numeric fields keep them.
    public static func digits(_ typed: String) -> String {
        typed.filter(\.isNumber)
    }

    /// A price kept a number while it is typed: a comma for a decimal point,
    /// and one point at most, since "1,50" read as a number is nothing at all
    /// and was saved as no price.
    public static func price(_ typed: String) -> String {
        let kept = typed.replacingOccurrences(of: ",", with: ".").filter { $0.isNumber || $0 == "." }
        let parts = kept.split(separator: ".", omittingEmptySubsequences: false)
        guard let whole = parts.first else { return "" }
        return parts.count > 1 ? "\(whole).\(parts.dropFirst().joined())" : String(whole)
    }

    /// A stored number as a field shows it, empty for zero.
    public static func text(_ number: Double) -> String {
        guard number != 0 else { return "" }
        return number == number.rounded() && abs(number) < 1e15 ? String(Int(number)) : String(number)
    }
}

// MARK: - About this server

/// ServerStatus is the instance being talked to: its name, the version it
/// runs, when it started, what started it, and the settings changed since
/// that a restart would pick up.
public struct ServerStatus: Codable, Hashable, Sendable {
    public let instance: String
    public let version: String
    public let commit: String
    public let startedAt: String
    public let uptimeSeconds: Int
    public let pendingRestart: [String]?
    /// container, systemd or unknown.
    public let supervision: String
    public let isRestarting: Bool

    enum CodingKeys: String, CodingKey {
        case instance, version, commit, startedAt, uptimeSeconds, pendingRestart, supervision
        case isRestarting = "restarting"
    }

    public init(
        instance: String, version: String, commit: String = "", startedAt: String = "", pendingRestart: [String] = [],
        supervision: String = "container"
    ) {
        self.instance = instance
        self.version = version
        self.commit = commit
        self.startedAt = startedAt
        self.uptimeSeconds = 0
        self.pendingRestart = pendingRestart
        self.supervision = supervision
        self.isRestarting = false
    }

    /// The commit, as short as the dashboard shows it.
    public var shortCommit: String { String(commit.prefix(12)) }
    /// Whether nothing recognizable started it, so nothing may start it
    /// again.
    public var isUnsupervised: Bool { supervision == "unknown" }
}

/// ServerUpgrade is what the release list says: what is running, what is
/// newest, whether this deployment can install it, and how the last check
/// and the last upgrade went.
public struct ServerUpgrade: Codable, Hashable, Sendable {
    public let current: String
    public let latest: String?
    public let isAvailable: Bool
    public let notes: String?
    public let url: String?
    public let checkedAt: String?
    public let attemptedAt: String?
    public let isChecking: Bool?
    /// Why the last upgrade did not finish.
    public let errorMessage: String?
    /// Why the last check failed.
    public let checkError: String?
    public let isApplicable: Bool
    /// What stands in the way of an upgrade from here.
    public let refusalReason: String?
    public let isAutomatic: Bool?
    public let isEnabled: Bool?
    /// The hours an automatic upgrade may run in.
    public let upgradeWindow: String?
    public let isUpgrading: Bool

    enum CodingKeys: String, CodingKey {
        case current, latest, notes, url, checkedAt, attemptedAt, checkError
        case isAvailable = "available"
        case isChecking = "checking"
        case errorMessage = "error"
        case isApplicable = "applicable"
        case refusalReason = "reason"
        case isAutomatic = "automatic"
        case isEnabled = "enabled"
        case upgradeWindow = "window"
        case isUpgrading = "upgrading"
    }

    public init(current: String, latest: String? = nil, isAvailable: Bool = false, isUpgrading: Bool = false, errorMessage: String? = nil) {
        self.current = current
        self.latest = latest
        self.isAvailable = isAvailable
        self.notes = nil
        self.url = nil
        self.checkedAt = nil
        self.attemptedAt = nil
        self.isChecking = nil
        self.errorMessage = errorMessage
        self.checkError = nil
        self.isApplicable = true
        self.refusalReason = nil
        self.isAutomatic = nil
        self.isEnabled = true
        self.upgradeWindow = nil
        self.isUpgrading = isUpgrading
    }

    /// Whether an upgrade can be started from here: there is one, and this
    /// deployment can install it.
    public var canUpgrade: Bool { isAvailable && isApplicable }
}

/// ServerConnectionFailure tells a connection that dropped from an answer.
/// An answer is anything the server managed to say; only a request that
/// never got one, or a gateway's page standing in for a server that is not
/// there, is a server that went away.
public enum ServerConnectionFailure {
    public static func isLostConnection(_ error: Error) -> Bool {
        if error is URLError { return true }
        if let teaNodeError = error as? TeaNodeError {
            // A gateway's 502 or 503 is a page, not a GraphQL answer.
            return teaNodeError == .unreadableAnswer
        }
        return false
    }
}

/// ServerUpgradeWatch decides, poll by poll, how an upgrade is going while
/// the server downloads, swaps and restarts: still going, back as a newer
/// version, stopped with a reason, or gone, which is the restart.
public struct ServerUpgradeWatch: Sendable {
    /// UpgradeVerdict is what one poll says about the upgrade.
    public enum UpgradeVerdict: Equatable, Sendable {
        case keepWaiting
        /// A different version answered: it already restarted.
        case cameBackUpgraded
        /// It stopped before restarting, with the reason when it gave one.
        case stopped(errorMessage: String?)
        /// It stopped answering for long enough to be restarting.
        case serverGone
    }

    /// The version answering before the upgrade began.
    public let previousVersion: String?
    private var lostConnectionCount = 0
    /// Whether the server has said, in so many words, that it is upgrading.
    /// Until it has, a run of failed requests is more likely a proxy than
    /// the restart, which cannot come before the download.
    private var hasConfirmedUpgrading = false

    public init(previousVersion: String?) {
        self.previousVersion = previousVersion
    }

    /// The bound on the whole wait: the download and the swap.
    public static let timeoutSeconds: Double = 15 * 60
    public static let pollSeconds: Double = 2

    public mutating func observe(_ upgrade: ServerUpgrade) -> UpgradeVerdict {
        lostConnectionCount = 0
        if upgrade.isUpgrading {
            hasConfirmedUpgrading = true
            return .keepWaiting
        }
        if let previousVersion, !previousVersion.isEmpty, !upgrade.current.isEmpty, upgrade.current != previousVersion {
            return .cameBackUpgraded
        }
        let errorMessage = upgrade.errorMessage.flatMap { $0.isEmpty ? nil : $0 }
        return .stopped(errorMessage: errorMessage)
    }

    public mutating func observeFailure(isLostConnection: Bool) -> UpgradeVerdict {
        guard isLostConnection else {
            lostConnectionCount = 0
            return .keepWaiting
        }
        lostConnectionCount += 1
        return lostConnectionCount >= (hasConfirmedUpgrading ? 3 : 5) ? .serverGone : .keepWaiting
    }
}

/// ServerWaits are how long the About page waits for the server.
public enum ServerWaits {
    /// A server that has not answered in this long after a restart is not
    /// coming back on its own.
    public static let restartTimeoutSeconds: Double = 90
    public static let restartPollSeconds: Double = 1.5
    /// A release check has a thirty-second timeout of its own on the server.
    public static let checkTimeoutSeconds: Double = 40
    public static let checkPollSeconds: Double = 1
}

// MARK: - Setup

/// ServerAddresses is what the outside world sees this server as.
public struct ServerAddresses: Codable, Hashable, Sendable {
    public let ipv4: String?
    public let ipv6: String?
    public let errorMessage: String?

    enum CodingKeys: String, CodingKey {
        case ipv4, ipv6
        case errorMessage = "error"
    }

    public var isKnown: Bool { !(ipv4 ?? "").isEmpty || !(ipv6 ?? "").isEmpty }
}

/// OutgoingIdentity is what a receiver sees when this server reaches out:
/// the address it sends from, the reverse name of that address and whether
/// it resolves back, and the name it greets with.
public struct OutgoingIdentity: Codable, Hashable, Sendable {
    public let address: String?
    /// direct, proxy or relay.
    public let via: String
    public let reverseName: String?
    public let forwardAddresses: [String]?
    public let isConfirmed: Bool
    public let helloName: String?
    public let helloAddresses: [String]?
    public let doesHelloMatch: Bool
    /// Why there is nothing to check, such as a relay sending as itself.
    public let uncheckableReason: String?

    enum CodingKeys: String, CodingKey {
        case address, via, reverseName, forwardAddresses, helloName, helloAddresses
        case isConfirmed = "confirmed"
        case doesHelloMatch = "helloMatches"
        case uncheckableReason = "unknown"
    }
}

/// SubmissionAccess is the address a mail client is told to send through:
/// what is set, and what it comes to.
public struct SubmissionAccess: Codable, Hashable, Sendable {
    public let host: String?
    public let port: Int?
    public let effectiveHost: String
    public let effectivePort: String
}

/// MailProgramAccess is the address a mail program is told to read mail
/// at: what is set, and what it comes to.
public struct MailProgramAccess: Codable, Hashable, Sendable {
    public let host: String?
    public let port: Int?
    public let tlsPort: Int?
    public let effectiveHost: String
    public let effectivePort: String
    public let effectiveTlsPort: String

    /// Whether anything listens for mail programs at all.
    public var isListening: Bool { !effectivePort.isEmpty || !effectiveTlsPort.isEmpty }
}

/// ServerSetup is the Setup tab: what an operator needs to finish setting
/// the server up.
public struct ServerSetup: Hashable, Sendable {
    public let addresses: ServerAddresses?
    public let outgoingIdentity: OutgoingIdentity?
    public let submission: SubmissionAccess?
    public let mailProgram: MailProgramAccess?
}

/// SubmissionAccessDraft is the submission address as its form holds it.
/// Both empty means following the server.
public struct SubmissionAccessDraft: Hashable, Sendable {
    public var host = ""
    public var portText = ""

    public init() {}

    public init(_ submission: SubmissionAccess) {
        host = submission.host ?? ""
        portText = (submission.port ?? 0) > 0 ? String(submission.port ?? 0) : ""
    }

    var variables: [String: JSONValue] {
        ["submission": .object(["host": .string(host), "port": .number(SettingsList.number(portText))])]
    }
}

/// MailProgramAccessDraft is the mail program address as its form holds
/// it. All empty means following the server.
public struct MailProgramAccessDraft: Hashable, Sendable {
    public var host = ""
    public var portText = ""
    public var tlsPortText = ""

    public init() {}

    public init(_ mailProgram: MailProgramAccess) {
        host = mailProgram.host ?? ""
        portText = (mailProgram.port ?? 0) > 0 ? String(mailProgram.port ?? 0) : ""
        tlsPortText = (mailProgram.tlsPort ?? 0) > 0 ? String(mailProgram.tlsPort ?? 0) : ""
    }

    var variables: [String: JSONValue] {
        [
            "imap": .object([
                "host": .string(host), "port": .number(SettingsList.number(portText)),
                "tlsPort": .number(SettingsList.number(tlsPortText)),
            ])
        ]
    }
}

// MARK: - The server's settings

/// ServerSettings are the server's own settings and the services it talks
/// to, as the settings tabs show them. A secret is never read back: each
/// says only whether one is set.
public struct ServerSettings: Codable, Hashable, Sendable {
    /// ObjectStore is where a copy of every stored message goes.
    public struct ObjectStore: Codable, Hashable, Sendable {
        public let isEnabled: Bool
        public let bucket: String
        public let region: String
        public let endpoint: String?
        public let isPathStyle: Bool
        public let accessKeyId: String?
        public let hasSecretAccessKey: Bool
        public let credentialsFile: String?

        enum CodingKeys: String, CodingKey {
            case bucket, region, endpoint, accessKeyId, hasSecretAccessKey, credentialsFile
            case isEnabled = "enabled"
            case isPathStyle = "pathStyle"
        }
    }

    /// Route53 solves the dns-01 challenge with an AWS hosted zone.
    public struct Route53: Codable, Hashable, Sendable {
        public let isEnabled: Bool
        public let zoneId: String
        public let region: String
        public let accessKeyId: String?
        public let hasSecretAccessKey: Bool
        public let credentialsFile: String?

        enum CodingKeys: String, CodingKey {
            case zoneId, region, accessKeyId, hasSecretAccessKey, credentialsFile
            case isEnabled = "enabled"
        }
    }

    /// Scanner is an optional service at a host and a port.
    public struct Scanner: Codable, Hashable, Sendable {
        public let isEnabled: Bool
        public let host: String
        public let port: Int

        enum CodingKeys: String, CodingKey {
            case host, port
            case isEnabled = "enabled"
        }
    }

    /// Antispam is the spam filter: which engine, and the built-in one's
    /// parts and how far its classifier has learned.
    public struct Antispam: Codable, Hashable, Sendable {
        public let isEnabled: Bool
        /// builtin or spamd, or empty for the server's choice.
        public let engine: String
        public let effectiveEngine: String
        public let host: String
        public let port: Int
        public let isSignalsEnabled: Bool
        public let isDnsEnabled: Bool
        public let isBayesEnabled: Bool
        public let isRulesEnabled: Bool
        public let bayesMinimumMessageCount: Int
        public let bayesLearnedSpamCount: Int
        public let bayesLearnedHamCount: Int

        enum CodingKeys: String, CodingKey {
            case engine, effectiveEngine, host, port
            case isEnabled = "enabled"
            case isSignalsEnabled = "signalsEnabled"
            case isDnsEnabled = "dnsEnabled"
            case isBayesEnabled = "bayesEnabled"
            case isRulesEnabled = "rulesEnabled"
            case bayesMinimumMessageCount = "bayesMinimumMessages"
            case bayesLearnedSpamCount = "bayesLearnedSpam"
            case bayesLearnedHamCount = "bayesLearnedHam"
        }

        public var bayesLearnedCount: Int { bayesLearnedSpamCount + bayesLearnedHamCount }
    }

    /// Relay is the mail server outgoing mail is handed to.
    public struct Relay: Codable, Hashable, Sendable {
        public let isEnabled: Bool
        public let host: String
        public let port: Int
        /// starttls, tls or none.
        public let security: String
        public let username: String?
        public let hasPassword: Bool

        enum CodingKeys: String, CodingKey {
            case host, port, security, username, hasPassword
            case isEnabled = "enabled"
        }
    }

    /// Proxy carries this server's own SMTP out through another address.
    public struct Proxy: Codable, Hashable, Sendable {
        public let socks5: String?
    }

    /// Certificates is what this server obtains certificates for, and how.
    public struct Certificates: Codable, Hashable, Sendable {
        public let isPerDomain: Bool
        public let hosts: [String]?
        public let isAcmeEnabled: Bool
        public let acmeEmail: String?
        public let acmeDirectoryUrl: String?
        public let acmeChallenge: String?
        public let certificateFile: String?
        public let privateKeyFile: String?

        enum CodingKeys: String, CodingKey {
            case hosts, acmeEmail, acmeDirectoryUrl, acmeChallenge, certificateFile, privateKeyFile
            case isPerDomain = "perDomain"
            case isAcmeEnabled = "acmeEnabled"
        }
    }

    /// SMTPLimits are the limits every message meets.
    public struct SMTPLimits: Codable, Hashable, Sendable {
        public let maxMessageSize: String
        public let maxRecipientsIncoming: Int
        public let maxRecipientsOutgoing: Int
        public let greylistDelay: String
        public let authRateLimit: Int
        public let authRateBurst: Int
        public let trustedSenders: [String]?
    }

    /// Resolver is how this server asks DNS questions.
    public struct Resolver: Codable, Hashable, Sendable {
        public let nameserver: String
        public let checkInterval: String
        public let externalAddressServices: [String]?
    }

    /// Session is how long a sign-in lasts.
    public struct Session: Codable, Hashable, Sendable {
        public let lifetime: String
    }

    /// Passkey is how this server introduces itself to an authenticator.
    public struct Passkey: Codable, Hashable, Sendable {
        public let isEnabled: Bool
        public let relyingPartyId: String?
        public let displayName: String?
        public let origins: [String]?
        public let maximumPerUser: Int

        enum CodingKeys: String, CodingKey {
            case relyingPartyId, displayName, origins, maximumPerUser
            case isEnabled = "enabled"
        }
    }

    /// Listen is the addresses this server binds.
    public struct Listen: Codable, Hashable, Sendable {
        public let smtpIncoming: String
        public let smtpOutgoing: String
        public let imap: String?
        public let imaps: String?
        public let http: String
        public let https: String
        public let debug: String?
    }

    /// Identity is what this server calls itself.
    public struct Identity: Codable, Hashable, Sendable {
        public let name: String
        public let mailServers: [String]?
        public let externalAddresses: [String]?
        public let logLevel: String
        public let dataDirectory: String
    }

    /// MessageStorage is where mail is kept on this machine.
    public struct MessageStorage: Codable, Hashable, Sendable {
        public let directory: String
        public let spoolRetention: String
    }

    /// GeoIP is the optional database that places a sender's address.
    public struct GeoIP: Codable, Hashable, Sendable {
        public let isEnabled: Bool
        public let databaseFile: String?

        enum CodingKeys: String, CodingKey {
            case databaseFile
            case isEnabled = "enabled"
        }
    }

    /// SSOProvider is one identity provider people may sign in through.
    public struct SSOProvider: Codable, Hashable, Sendable {
        public let id: String
        public let name: String
        public let issuer: String
        public let clientId: String
        public let hasClientSecret: Bool
        public let groupsClaim: String?
        public let shouldCreateUsers: Bool

        enum CodingKeys: String, CodingKey {
            case id, name, issuer, clientId, hasClientSecret, groupsClaim
            case shouldCreateUsers = "createUsers"
        }
    }

    /// SSO is the identity providers.
    public struct SSO: Codable, Hashable, Sendable {
        public let providers: [SSOProvider]
    }

    public let s3: ObjectStore
    public let route53: Route53
    public let antivirus: Scanner
    public let antispam: Antispam
    public let relay: Relay
    public let proxy: Proxy?
    public let certificates: Certificates
    public let smtp: SMTPLimits
    public let resolver: Resolver
    public let session: Session
    public let passkey: Passkey
    public let listen: Listen
    public let sso: SSO
    public let identity: Identity
    public let storage: MessageStorage
    public let geoip: GeoIP
}

/// RelayPreset is a provider whose SMTP endpoint people point the relay
/// at, with the host, port and security it documents.
public struct RelayPreset: Hashable, Sendable, Identifiable {
    public let label: String
    public let host: String
    public let port: Int
    public let security: String
    public let username: String?

    public var id: String { label }

    public static let all = [
        RelayPreset(label: "Gmail", host: "smtp.gmail.com", port: 587, security: "starttls", username: nil),
        RelayPreset(label: "Amazon SES", host: "email-smtp.us-east-1.amazonaws.com", port: 587, security: "starttls", username: nil),
        RelayPreset(label: "Postmark", host: "smtp.postmarkapp.com", port: 587, security: "starttls", username: nil),
        RelayPreset(label: "Resend", host: "smtp.resend.com", port: 465, security: "tls", username: "resend"),
    ]
}

/// SecretChange is what a form does with a secret the server will not read
/// back: keep it, replace it with what was typed, or clear it.
public enum SecretChange {
    /// The secret's key and value for the variables, or nothing to keep it.
    static func entry(typed: String, isClearing: Bool) -> JSONValue? {
        if isClearing { return .string("") }
        return typed.isEmpty ? nil : .string(typed)
    }
}

/// SMTPLimitsDraft is the Mail tab's limits as the form holds them.
public struct SMTPLimitsDraft: Hashable, Sendable {
    public var maxMessageSize: String
    public var maxRecipientsIncomingText: String
    public var maxRecipientsOutgoingText: String
    public var greylistDelay: String
    public var authRateLimitText: String
    public var authRateBurstText: String
    public var trustedSendersText: String

    public init(_ smtp: ServerSettings.SMTPLimits) {
        maxMessageSize = smtp.maxMessageSize
        maxRecipientsIncomingText = String(smtp.maxRecipientsIncoming)
        maxRecipientsOutgoingText = String(smtp.maxRecipientsOutgoing)
        greylistDelay = smtp.greylistDelay
        authRateLimitText = String(smtp.authRateLimit)
        authRateBurstText = String(smtp.authRateBurst)
        trustedSendersText = SettingsList.text(smtp.trustedSenders)
    }

    public var variables: [String: JSONValue] {
        [
            "smtp": .object([
                "maxMessageSize": .string(maxMessageSize),
                "maxRecipientsIncoming": .number(SettingsList.number(maxRecipientsIncomingText)),
                "maxRecipientsOutgoing": .number(SettingsList.number(maxRecipientsOutgoingText)),
                "greylistDelay": .string(greylistDelay),
                "authRateLimit": .number(SettingsList.number(authRateLimitText)),
                "authRateBurst": .number(SettingsList.number(authRateBurstText)),
                "trustedSenders": SettingsList.value(SettingsList.items(trustedSendersText)),
            ])
        ]
    }
}

/// ResolverDraft is how this server asks DNS questions, as the form holds
/// it.
public struct ResolverDraft: Hashable, Sendable {
    public var nameserver: String
    public var checkInterval: String
    public var externalAddressServicesText: String

    public init(_ resolver: ServerSettings.Resolver) {
        nameserver = resolver.nameserver
        checkInterval = resolver.checkInterval
        externalAddressServicesText = SettingsList.text(resolver.externalAddressServices)
    }

    public var variables: [String: JSONValue] {
        [
            "resolver": .object([
                "nameserver": .string(nameserver), "checkInterval": .string(checkInterval),
                "externalAddressServices": SettingsList.value(SettingsList.items(externalAddressServicesText)),
            ])
        ]
    }
}

/// SessionDraft is how long a sign-in lasts, as the form holds it.
public struct SessionDraft: Hashable, Sendable {
    public var lifetime: String

    public init(_ session: ServerSettings.Session) {
        lifetime = session.lifetime
    }

    public var variables: [String: JSONValue] { ["session": .object(["lifetime": .string(lifetime)])] }
}

/// PasskeyDraft is the passkey settings as the form holds them.
public struct PasskeyDraft: Hashable, Sendable {
    public var isEnabled: Bool
    public var relyingPartyId: String
    public var displayName: String
    public var originsText: String
    public var maximumPerUserText: String

    public init(_ passkey: ServerSettings.Passkey) {
        isEnabled = passkey.isEnabled
        relyingPartyId = passkey.relyingPartyId ?? ""
        displayName = passkey.displayName ?? ""
        originsText = SettingsList.text(passkey.origins)
        maximumPerUserText = String(passkey.maximumPerUser)
    }

    public var variables: [String: JSONValue] {
        [
            "passkey": .object([
                "enabled": .bool(isEnabled), "relyingPartyId": .string(relyingPartyId), "displayName": .string(displayName),
                "origins": SettingsList.value(SettingsList.items(originsText)),
                "maximumPerUser": .number(SettingsList.number(maximumPerUserText)),
            ])
        ]
    }
}

/// ListenDraft is the addresses this server binds, as the form holds them.
/// It is the one form that asks before it saves, naming the dashboard's
/// address, since a wrong one loses the dashboard after the restart.
public struct ListenDraft: Hashable, Sendable {
    public var smtpIncoming: String
    public var smtpOutgoing: String
    public var imap: String
    public var imaps: String
    public var http: String
    public var https: String
    public var debug: String

    public init(_ listen: ServerSettings.Listen) {
        smtpIncoming = listen.smtpIncoming
        smtpOutgoing = listen.smtpOutgoing
        imap = listen.imap ?? ""
        imaps = listen.imaps ?? ""
        http = listen.http
        https = listen.https
        debug = listen.debug ?? ""
    }

    public var variables: [String: JSONValue] {
        [
            "listen": .object([
                "smtpIncoming": .string(smtpIncoming), "smtpOutgoing": .string(smtpOutgoing), "imap": .string(imap),
                "imaps": .string(imaps), "http": .string(http), "https": .string(https), "debug": .string(debug),
            ])
        ]
    }
}

/// IdentityDraft is what this server calls itself, as the form holds it.
/// The data directory is shown and never sent.
public struct IdentityDraft: Hashable, Sendable {
    /// The levels the configuration accepts, in upper case as it stores
    /// them.
    public static let logLevels = ["DEBUG", "INFO", "NOTICE", "WARNING", "ERROR", "CRITICAL"]

    public var name: String
    public var mailServersText: String
    public var externalAddressesText: String
    public var logLevel: String

    public init(_ identity: ServerSettings.Identity) {
        name = identity.name
        mailServersText = SettingsList.text(identity.mailServers)
        externalAddressesText = SettingsList.text(identity.externalAddresses)
        logLevel = identity.logLevel.uppercased()
    }

    public var variables: [String: JSONValue] {
        [
            "identity": .object([
                "name": .string(name), "mailServers": SettingsList.value(SettingsList.items(mailServersText)),
                "externalAddresses": SettingsList.value(SettingsList.items(externalAddressesText)), "logLevel": .string(logLevel),
            ])
        ]
    }
}

/// MessageStorageDraft is where mail is kept on this machine, as the form
/// holds it.
public struct MessageStorageDraft: Hashable, Sendable {
    public var directory: String
    public var spoolRetention: String

    public init(_ storage: ServerSettings.MessageStorage) {
        directory = storage.directory
        spoolRetention = storage.spoolRetention
    }

    public var variables: [String: JSONValue] {
        ["storage": .object(["directory": .string(directory), "spoolRetention": .string(spoolRetention)])]
    }
}

/// GeoIPDraft is the sender location database, as the form holds it.
public struct GeoIPDraft: Hashable, Sendable {
    public var isEnabled: Bool
    public var databaseFile: String

    public init(_ geoip: ServerSettings.GeoIP) {
        isEnabled = geoip.isEnabled
        databaseFile = geoip.databaseFile ?? ""
    }

    public var variables: [String: JSONValue] {
        ["geoip": .object(["enabled": .bool(isEnabled), "databaseFile": .string(databaseFile)])]
    }
}

/// RelayDraft is the outgoing relay as the form holds it. The password is
/// never read back: empty keeps the stored one, and clearing it is
/// deliberate.
public struct RelayDraft: Hashable, Sendable {
    public var isEnabled: Bool
    public var host: String
    public var portText: String
    public var security: String
    public var username: String
    public var password = ""
    public var isClearingPassword = false

    public init(_ relay: ServerSettings.Relay) {
        isEnabled = relay.isEnabled
        host = relay.host
        portText = String(relay.port > 0 ? relay.port : 587)
        security = relay.security.isEmpty ? "starttls" : relay.security
        username = relay.username ?? ""
    }

    /// Fills in the host, port and security a provider documents; the
    /// username only when the provider has a fixed one.
    public mutating func apply(_ preset: RelayPreset) {
        host = preset.host
        portText = String(preset.port)
        security = preset.security
        if let presetUsername = preset.username { username = presetUsername }
    }

    public var variables: [String: JSONValue] {
        var relay: [String: JSONValue] = [
            "enabled": .bool(isEnabled), "host": .string(host), "port": .number(SettingsList.number(portText)),
            "security": .string(security), "username": .string(username),
        ]
        relay["password"] = SecretChange.entry(typed: password, isClearing: isClearingPassword)
        return ["relay": .object(relay)]
    }
}

/// ObjectStoreDraft is the object store as the form holds it.
public struct ObjectStoreDraft: Hashable, Sendable {
    public var isEnabled: Bool
    public var bucket: String
    public var region: String
    public var endpoint: String
    public var isPathStyle: Bool
    public var accessKeyId: String
    public var secretAccessKey = ""
    public var isClearingSecret = false

    public init(_ store: ServerSettings.ObjectStore) {
        isEnabled = store.isEnabled
        bucket = store.bucket
        region = store.region
        endpoint = store.endpoint ?? ""
        isPathStyle = store.isPathStyle
        accessKeyId = store.accessKeyId ?? ""
    }

    public var variables: [String: JSONValue] {
        var store: [String: JSONValue] = [
            "enabled": .bool(isEnabled), "bucket": .string(bucket), "region": .string(region), "endpoint": .string(endpoint),
            "pathStyle": .bool(isPathStyle), "accessKeyId": .string(accessKeyId),
        ]
        store["secretAccessKey"] = SecretChange.entry(typed: secretAccessKey, isClearing: isClearingSecret)
        return ["s3": .object(store)]
    }
}

/// Route53Draft is the DNS challenge solver as the form holds it.
public struct Route53Draft: Hashable, Sendable {
    public var isEnabled: Bool
    public var zoneId: String
    public var region: String
    public var accessKeyId: String
    public var secretAccessKey = ""
    public var isClearingSecret = false

    public init(_ route53: ServerSettings.Route53) {
        isEnabled = route53.isEnabled
        zoneId = route53.zoneId
        region = route53.region
        accessKeyId = route53.accessKeyId ?? ""
    }

    public var variables: [String: JSONValue] {
        var route53: [String: JSONValue] = [
            "enabled": .bool(isEnabled), "zoneId": .string(zoneId), "region": .string(region), "accessKeyId": .string(accessKeyId),
        ]
        route53["secretAccessKey"] = SecretChange.entry(typed: secretAccessKey, isClearing: isClearingSecret)
        return ["route53": .object(route53)]
    }
}

/// CertificatesDraft is what this server obtains certificates for, and how,
/// as the form holds it.
public struct CertificatesDraft: Hashable, Sendable {
    public static let challenges = ["http-01", "dns-01"]

    public var hostsText: String
    public var isPerDomain: Bool
    public var isAcmeEnabled: Bool
    public var acmeEmail: String
    public var acmeChallenge: String
    public var acmeDirectoryUrl: String
    public var certificateFile: String
    public var privateKeyFile: String

    public init(_ certificates: ServerSettings.Certificates) {
        hostsText = SettingsList.text(certificates.hosts)
        isPerDomain = certificates.isPerDomain
        isAcmeEnabled = certificates.isAcmeEnabled
        acmeEmail = certificates.acmeEmail ?? ""
        acmeChallenge = (certificates.acmeChallenge ?? "").isEmpty ? "http-01" : certificates.acmeChallenge ?? "http-01"
        acmeDirectoryUrl = certificates.acmeDirectoryUrl ?? ""
        certificateFile = certificates.certificateFile ?? ""
        privateKeyFile = certificates.privateKeyFile ?? ""
    }

    public var variables: [String: JSONValue] {
        [
            "certificates": .object([
                "perDomain": .bool(isPerDomain), "hosts": SettingsList.value(SettingsList.items(hostsText)),
                "acmeEnabled": .bool(isAcmeEnabled), "acmeEmail": .string(acmeEmail), "acmeDirectoryUrl": .string(acmeDirectoryUrl),
                "acmeChallenge": .string(acmeChallenge), "certificateFile": .string(certificateFile),
                "privateKeyFile": .string(privateKeyFile),
            ])
        ]
    }
}

/// ProxyDraft is the outgoing SOCKS5 proxy as the form holds it.
public struct ProxyDraft: Hashable, Sendable {
    public var socks5: String

    public init(_ proxy: ServerSettings.Proxy?) {
        socks5 = proxy?.socks5 ?? ""
    }

    public var variables: [String: JSONValue] {
        ["proxy": .object(["socks5": .string(socks5.trimmingCharacters(in: .whitespacesAndNewlines))])]
    }
}

/// ScannerDraft is the virus scanner as the form holds it.
public struct ScannerDraft: Hashable, Sendable {
    public var isEnabled: Bool
    public var host: String
    public var portText: String

    public init(_ scanner: ServerSettings.Scanner) {
        isEnabled = scanner.isEnabled
        host = scanner.host
        portText = String(scanner.port)
    }

    public var variables: [String: JSONValue] {
        ["antivirus": .object(["enabled": .bool(isEnabled), "host": .string(host), "port": .number(SettingsList.number(portText))])]
    }
}

/// AntispamDraft is the spam filter as the form holds it. The engine starts
/// as what the server resolved an empty setting to, so an operator who never
/// chose can see what runs.
public struct AntispamDraft: Hashable, Sendable {
    public static let engines = ["builtin", "spamd"]

    public var isEnabled: Bool
    public var engine: String
    public var host: String
    public var portText: String
    public var isSignalsEnabled: Bool
    public var isDnsEnabled: Bool
    public var isBayesEnabled: Bool
    public var isRulesEnabled: Bool
    public var bayesMinimumMessagesText: String

    public init(_ antispam: ServerSettings.Antispam) {
        isEnabled = antispam.isEnabled
        engine = antispam.engine.isEmpty ? antispam.effectiveEngine : antispam.engine
        host = antispam.host
        portText = String(antispam.port)
        isSignalsEnabled = antispam.isSignalsEnabled
        isDnsEnabled = antispam.isDnsEnabled
        isBayesEnabled = antispam.isBayesEnabled
        isRulesEnabled = antispam.isRulesEnabled
        bayesMinimumMessagesText = String(antispam.bayesMinimumMessageCount)
    }

    public var isSpamd: Bool { engine == "spamd" }
    public var bayesMinimumMessageCount: Int { Int(SettingsList.number(bayesMinimumMessagesText)) }

    public var variables: [String: JSONValue] {
        [
            "antispam": .object([
                "enabled": .bool(isEnabled), "engine": .string(engine), "host": .string(host),
                "port": .number(SettingsList.number(portText)), "signalsEnabled": .bool(isSignalsEnabled),
                "dnsEnabled": .bool(isDnsEnabled), "bayesEnabled": .bool(isBayesEnabled), "rulesEnabled": .bool(isRulesEnabled),
                "bayesMinimumMessages": .number(Double(bayesMinimumMessageCount)),
            ])
        ]
    }
}

/// SSOProviderDraft is one identity provider as the form holds it. The
/// client secret is written and never read back: blank keeps the stored one.
public struct SSOProviderDraft: Hashable, Sendable, Identifiable {
    public let id: UUID
    public var providerId: String
    public var name: String
    public var issuer: String
    public var clientId: String
    public var clientSecret = ""
    public var hasClientSecret: Bool
    public var groupsClaim: String
    public var shouldCreateUsers: Bool

    public init() {
        id = UUID()
        providerId = ""
        name = ""
        issuer = ""
        clientId = ""
        hasClientSecret = false
        groupsClaim = ""
        shouldCreateUsers = false
    }

    public init(_ provider: ServerSettings.SSOProvider) {
        id = UUID()
        providerId = provider.id
        name = provider.name
        issuer = provider.issuer
        clientId = provider.clientId
        hasClientSecret = provider.hasClientSecret
        groupsClaim = provider.groupsClaim ?? ""
        shouldCreateUsers = provider.shouldCreateUsers
    }

    /// Where the provider sends a person back, to register at the provider.
    public func redirectAddress(serverOrigin: String) -> String {
        let trimmedId = providerId.trimmingCharacters(in: .whitespaces)
        return "\(serverOrigin)/api/v1/sso/\(trimmedId.isEmpty ? "id" : trimmedId)/callback"
    }

    /// Whether the fields the server needs are filled in.
    public var isComplete: Bool {
        [providerId, name, issuer, clientId].allSatisfy { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
    }

    var parameters: JSONValue {
        let trimmedClaim = groupsClaim.trimmingCharacters(in: .whitespaces)
        return .object([
            "id": .string(providerId.trimmingCharacters(in: .whitespaces)), "name": .string(name.trimmingCharacters(in: .whitespaces)),
            "issuer": .string(issuer.trimmingCharacters(in: .whitespaces)),
            "clientId": .string(clientId.trimmingCharacters(in: .whitespaces)),
            "clientSecret": clientSecret.isEmpty ? .null : .string(clientSecret),
            "groupsClaim": trimmedClaim.isEmpty ? .null : .string(trimmedClaim), "createUsers": .bool(shouldCreateUsers),
        ])
    }
}

/// SSODraft is the identity providers as the form holds them: the list as
/// given replaces the stored one.
public struct SSODraft: Hashable, Sendable {
    public var providers: [SSOProviderDraft]

    public init(_ sso: ServerSettings.SSO) {
        providers = sso.providers.map(SSOProviderDraft.init)
    }

    public var canSave: Bool { providers.allSatisfy(\.isComplete) }

    public var variables: [String: JSONValue] {
        ["sso": .object(["providers": .array(providers.map(\.parameters))])]
    }
}

// MARK: - The operator's agent settings

/// AgentPlanUsage is what a provider paid for by subscription last said of
/// its allowance: the plan's name, and each window it is spent over.
public struct AgentPlanUsage: Codable, Hashable, Sendable {
    /// PlanWindow is one allowance and how much of it is used.
    public struct PlanWindow: Codable, Hashable, Sendable {
        public let usedPercent: Int
        public let windowMinutes: Int
        public let resetsAt: String?

        /// Whether its reset has passed since the reading, so the whole
        /// allowance is there again.
        public func hasReset(now: Date) -> Bool {
            guard let resetDate = ServerTime.date(resetsAt) else { return false }
            return resetDate <= now
        }

        /// What is used, as a percentage, nothing once it has reset.
        public func shownUsedPercent(now: Date) -> Int {
            hasReset(now: now) ? 0 : max(0, min(100, usedPercent))
        }
    }

    public let planName: String
    public let observedAt: String
    public let windows: [PlanWindow]
}

/// AgentProviderSettings is a model service, as the operator's list shows
/// it. Its key is never read back.
public struct AgentProviderSettings: Codable, Hashable, Sendable {
    /// ModelPrice is the price of the models matching one pattern.
    public struct ModelPrice: Codable, Hashable, Sendable {
        public let model: String
        public let input: Double
        public let output: Double
        public let cacheRead: Double
        public let cacheWrite: Double

        public init(model: String, input: Double, output: Double, cacheRead: Double = 0, cacheWrite: Double = 0) {
            self.model = model
            self.input = input
            self.output = output
            self.cacheRead = cacheRead
            self.cacheWrite = cacheWrite
        }
    }

    public let name: String
    public let providerKind: String
    public let baseUrl: String
    public let hasApiKey: Bool
    /// A provider on a person's plan is signed in to, not keyed.
    public let hasRefreshToken: Bool
    public let account: String
    public let isEnabled: Bool
    public let allow: [String]
    public let deny: [String]
    public let pricingInput: Double
    public let pricingOutput: Double
    public let pricingCacheRead: Double
    public let pricingCacheWrite: Double
    public let modelPricing: [ModelPrice]?
    public let planUsage: AgentPlanUsage?

    enum CodingKeys: String, CodingKey {
        case name, baseUrl, hasApiKey, hasRefreshToken, account, allow, deny, pricingInput, pricingOutput, pricingCacheRead
        case pricingCacheWrite, modelPricing, planUsage
        case providerKind = "kind"
        case isEnabled = "enabled"
    }

    public init(name: String, providerKind: String, hasApiKey: Bool = true, isEnabled: Bool = true) {
        self.name = name
        self.providerKind = providerKind
        self.baseUrl = ""
        self.hasApiKey = hasApiKey
        self.hasRefreshToken = false
        self.account = ""
        self.isEnabled = isEnabled
        self.allow = []
        self.deny = []
        self.pricingInput = 0
        self.pricingOutput = 0
        self.pricingCacheRead = 0
        self.pricingCacheWrite = 0
        self.modelPricing = []
        self.planUsage = nil
    }

    /// The kind that runs on a person's plan and is signed in to rather
    /// than given a key.
    public static let signedInKind = "openai-codex"

    public var isSignedInKind: Bool { providerKind == Self.signedInKind }

    /// Whether Test can ask it anything: it has a key or a sign-in, or it is
    /// of the openai kind, which a local server answers without either.
    public var canBeTested: Bool { hasApiKey || hasRefreshToken || providerKind == "openai" }

    /// As the API takes it back: the key blank, which keeps the stored one.
    var storedParameters: [String: JSONValue] {
        [
            "name": .string(name), "kind": .string(providerKind), "baseUrl": .string(baseUrl), "apiKey": .string(""),
            "enabled": .bool(isEnabled), "allow": SettingsList.value(allow), "deny": SettingsList.value(deny),
            "pricingInput": .number(pricingInput), "pricingOutput": .number(pricingOutput),
            "pricingCacheRead": .number(pricingCacheRead), "pricingCacheWrite": .number(pricingCacheWrite),
            "modelPricing": .array((modelPricing ?? []).map(AgentProviderDraft.priceValue)),
        ]
    }
}

/// AgentModelAssignments assign work to models, each written
/// provider:model, and list the models a person may pick.
public struct AgentModelAssignments: Codable, Hashable, Sendable {
    public var `default`: String
    public var fast: String
    public var quick: String
    public var embedding: String
    public var triage: String
    public var research: String
    public var summarize: String
    public var reply: String
    public var ask: String
    public var schedule: String
    public var compact: String
    public var scan: String
    public var synthesize: String
    public var decide: String
    public var categorize: String
    public var embeddingDimensions: Int
    public var choices: [String]

    /// The four set first, by field name. Quick is the model
    /// a typed turn starts on; empty starts it on the conversations model.
    public static let baseFieldNames = ["default", "fast", "quick", "embedding"]
    /// One per kind of work, for whoever spends differently on each.
    public static let workFieldNames = ["triage", "research", "summarize", "reply", "ask", "schedule", "compact", "scan", "synthesize"]
    /// Every assignment by field name, the deciding ones last.
    public static let allFieldNames = baseFieldNames + workFieldNames + ["decide", "categorize"]

    /// One assignment by its field name; empty for a name it does not have.
    public subscript(fieldName fieldName: String) -> String {
        get {
            switch fieldName {
            case "default": self.default
            case "fast": fast
            case "quick": quick
            case "embedding": embedding
            case "triage": triage
            case "research": research
            case "summarize": summarize
            case "reply": reply
            case "ask": ask
            case "schedule": schedule
            case "compact": compact
            case "scan": scan
            case "synthesize": synthesize
            case "decide": decide
            case "categorize": categorize
            default: ""
            }
        }
        set {
            switch fieldName {
            case "default": self.default = newValue
            case "fast": fast = newValue
            case "quick": quick = newValue
            case "embedding": embedding = newValue
            case "triage": triage = newValue
            case "research": research = newValue
            case "summarize": summarize = newValue
            case "reply": reply = newValue
            case "ask": ask = newValue
            case "schedule": schedule = newValue
            case "compact": compact = newValue
            case "scan": scan = newValue
            case "synthesize": synthesize = newValue
            case "decide": decide = newValue
            case "categorize": categorize = newValue
            default: break
            }
        }
    }

    /// Every assignment written provider:model renamed from one provider to
    /// another, as a renamed provider takes its models with it.
    public func renaming(provider previousName: String, to newName: String) -> AgentModelAssignments {
        let rename: (String) -> String = { model in
            model.hasPrefix(previousName + ":") ? newName + model.dropFirst(previousName.count) : model
        }
        var renamed = self
        for fieldName in Self.allFieldNames {
            renamed[fieldName: fieldName] = rename(renamed[fieldName: fieldName])
        }
        renamed.choices = choices.map(rename)
        return renamed
    }

    var parameters: JSONValue {
        .object([
            "default": .string(self.default), "fast": .string(fast), "quick": .string(quick), "embedding": .string(embedding),
            "triage": .string(triage), "research": .string(research), "summarize": .string(summarize), "reply": .string(reply),
            "ask": .string(ask), "schedule": .string(schedule), "compact": .string(compact), "scan": .string(scan), "synthesize": .string(synthesize),
            "decide": .string(decide), "categorize": .string(categorize), "embeddingDimensions": .number(Double(embeddingDimensions)),
            "choices": SettingsList.value(choices),
        ])
    }
}

extension AgentModelAssignments {
    /// The agent section that saves the assignments whole, as the models
    /// form does.
    public var agentParameters: [String: JSONValue] { ["models": parameters] }
}

/// AgentFeatureSwitches is what people may turn on, by feature, as the
/// features form holds it. A feature the server did not say is on.
public enum AgentFeatureSwitches {
    public static func isOn(_ feature: String, in features: [String: Bool]) -> Bool { features[feature] != false }

    public static func agentParameters(_ features: [String: Bool]) -> [String: JSONValue] {
        ["features": .object(features.mapValues { .bool($0) })]
    }
}

/// AgentModelChoices are the models the providers offer, split the way the
/// models form offers them: a deciding provider (kind typesafe) answers only
/// the decide and categorize slots, and the server refuses it anywhere else.
public struct AgentModelChoices: Hashable, Sendable {
    public let forWork: [String]
    public let forDeciding: [String]

    public init(knownModels: [String], providers: [AgentProviderSettings]) {
        let deciderNames = Set(providers.filter { $0.providerKind == "typesafe" }.map(\.name))
        let isDecider: (String) -> Bool = { model in
            deciderNames.contains(String(model.split(separator: ":", maxSplits: 1).first ?? ""))
        }
        forWork = knownModels.filter { !isDecider($0) }
        forDeciding = knownModels.filter(isDecider)
    }

    /// What a picker lists: the known models, with the one chosen first
    /// when the list does not know it.
    public static func listed(_ known: [String], chosen: String) -> [String] {
        chosen.isEmpty || known.contains(chosen) ? known : [chosen] + known
    }
}

/// AgentLimits are the caps on what people and the whole server may spend,
/// how far one run may go, and what unattended work may take.
public struct AgentLimits: Codable, Hashable, Sendable {
    public let maxBodyCharacters: Int
    public let dailyTokensPerAgent: Int
    public let dailyCostPerAgent: Double
    public let monthlyCostPerServer: Double
    public let monthlyTokensPerServer: Int
    public let maxRoundsPerAsk: Int
    public let maxRoundsPerResearch: Int
    public let maxRoundsPerReply: Int
    public let maxToolCallsPerRun: Int
    public let requestTimeout: String
    public let concurrency: Int
    public let maxRoundsPerDream: Int
    public let scanConcurrency: Int
    public let rewriteConcurrency: Int
    public let dreamShare: Double
    public let ingestChunksPerRun: Int
    public let embeddingTokensPerDay: Int
    public let maxRoundsPerSubagent: Int
    public let goalTurnsPerDay: Int
    public let goalTurnsAlone: Int
    public let goalsInProgress: Int
    public let backgroundWakesAlone: Int
}

/// AgentRetention is how long run transcripts and corrections are kept.
public struct AgentRetention: Codable, Hashable, Sendable {
    public var runs: String
    public var corrections: String
}

/// AgentServerSettings are the operator's agent settings: whether people
/// may have an agent, the house instructions, the providers and models,
/// what people may turn on, the limits, the tools and the services they
/// reach, and the finance providers. Every secret comes back as set or not.
public struct AgentServerSettings: Codable, Hashable, Sendable {
    /// WebSearch is the service web_search asks.
    public struct WebSearch: Codable, Hashable, Sendable {
        public let searchKind: String
        public let hasApiKey: Bool

        enum CodingKeys: String, CodingKey {
            case hasApiKey
            case searchKind = "kind"
        }
    }

    /// ToolPolicy is the tools that are off and the tools that ask first,
    /// by name, and every tool there is.
    public struct ToolPolicy: Codable, Hashable, Sendable {
        public let disabled: [String]
        public let confirm: [String]
        public let catalog: [AgentTool]
    }

    /// Voice is talking to the agent.
    public struct Voice: Codable, Hashable, Sendable {
        public let isEnabled: Bool
        public let provider: String
        public let transcriptionModel: String
        public let silenceMS: Int
        public let speechModel: String
        public let speechVoice: String
        public let askModel: String

        enum CodingKeys: String, CodingKey {
            case provider, transcriptionModel, silenceMS, speechModel, speechVoice, askModel
            case isEnabled = "enabled"
        }
    }

    /// Browser is the headless browser beside the server.
    public struct Browser: Codable, Hashable, Sendable {
        public let isEnabled: Bool
        public let cdpEndpoint: String
        public let canAttachTabs: Bool
        public let allowPrivateAddresses: [String]
        public let idleTimeout: String
        public let maxContexts: Int

        enum CodingKeys: String, CodingKey {
            case cdpEndpoint, allowPrivateAddresses, idleTimeout, maxContexts
            case isEnabled = "enabled"
            case canAttachTabs = "attachTabs"
        }
    }

    /// Plaid is the operator's Plaid account.
    public struct Plaid: Codable, Hashable, Sendable {
        public let environment: String
        public let clientId: String
        public let hasSecret: Bool
        public let countryCodes: [String]
        public let products: [String]
    }

    /// Finance is which providers people may link their institutions
    /// through.
    public struct Finance: Codable, Hashable, Sendable {
        public let offeredProviders: [String]
        public let plaid: Plaid
    }

    public let isEnabled: Bool
    public let instructions: String
    public let allowPrivateAddresses: [String]
    public let skipCertificateCheck: [String]
    public let providers: [AgentProviderSettings]
    public let models: AgentModelAssignments
    /// What people may turn on, by feature; one not said is on.
    public let features: [String: Bool]
    public let limits: AgentLimits
    public let retention: AgentRetention
    public let currency: String
    public let search: WebSearch
    public let tools: ToolPolicy
    public let voice: Voice
    public let browser: Browser
    public let mcpServers: [ConnectedServerSettings]
    public let finance: Finance
    public let works: [String]?
    public let families: [String]
    public let kinds: [String]

    enum CodingKeys: String, CodingKey {
        case instructions, allowPrivateAddresses, skipCertificateCheck, providers, models, features, limits, retention, currency
        case search, tools, voice, browser, mcpServers, finance, works, families, kinds
        case isEnabled = "enabled"
    }

    /// The features in the order the dashboard lists them.
    public static let featureNames = [
        "triage", "summaries", "draftReplies", "search", "research", "autoReply", "ask", "schedules", "browser",
        "connectedServers", "computer", "chatApps", "skills", "subagents", "remember", "knowledge", "dreaming",
    ]
    /// The finance providers and Plaid products, in the order the server
    /// lists them.
    public static let financeProviders = ["plaid", "simplefin"]
    public static let plaidProducts = ["transactions", "investments", "liabilities"]
    /// The voices an answer can be read in; marin when none is chosen.
    public static let speechVoices = [
        "marin", "cedar", "alloy", "ash", "ballad", "coral", "echo", "fable", "nova", "onyx", "sage", "shimmer", "verse",
    ]
}

/// ConnectedServerSettings is one connected server the operator declared.
/// Its secrets are never read back.
public struct ConnectedServerSettings: Codable, Hashable, Sendable {
    public let name: String
    public let transport: String
    public let effectiveTransport: String
    public let url: String
    public let command: String
    public let args: [String]
    public let envNames: [String]
    public let workingDir: String
    public let auth: String
    public let effectiveAuth: String
    public let hasAuthorization: Bool
    public let oauthClientId: String
    public let hasOauthClientSecret: Bool
    public let oauthScopes: [String]
    public let oauthAuthorizationUrl: String
    public let oauthTokenUrl: String
    public let oauthRedirect: String
    public let isHeadless: Bool
    public let location: String
    public let readOnly: [String]
    public let disabled: [String]
    public let timeout: String
    public let isEnabled: Bool

    enum CodingKeys: String, CodingKey {
        case name, transport, effectiveTransport, url, command, args, envNames, workingDir, auth, effectiveAuth, hasAuthorization
        case oauthClientId, hasOauthClientSecret, oauthScopes, oauthAuthorizationUrl, oauthTokenUrl, oauthRedirect, location
        case readOnly, disabled, timeout
        case isHeadless = "headless"
        case isEnabled = "enabled"
    }

    /// As the API takes it back: secrets blank, which keeps them, and the
    /// environment as names with blank values, which keeps those too.
    var storedParameters: [String: JSONValue] {
        [
            "name": .string(name), "transport": .string(transport), "url": .string(url), "command": .string(command),
            "args": SettingsList.value(args), "env": SettingsList.value(envNames.map { "\($0)=" }), "workingDir": .string(workingDir),
            "auth": .string(auth), "authorization": .string(""), "oauthClientId": .string(oauthClientId),
            "oauthClientSecret": .string(""), "oauthScopes": SettingsList.value(oauthScopes),
            "oauthAuthorizationUrl": .string(oauthAuthorizationUrl), "oauthTokenUrl": .string(oauthTokenUrl),
            "oauthRedirect": .string(oauthRedirect == "computer" ? "computer" : ""), "headless": .bool(isHeadless),
            "location": .string(location == "computer" ? "computer" : ""), "readOnly": SettingsList.value(readOnly),
            "disabled": SettingsList.value(disabled), "timeout": .string(timeout), "enabled": .bool(isEnabled),
        ]
    }
}

/// AgentGeneralDraft is whether people may have an agent, the house
/// instructions and the network exceptions, as the form holds them.
public struct AgentGeneralDraft: Hashable, Sendable {
    public var isEnabled: Bool
    public var instructions: String
    public var allowPrivateAddressesText: String
    public var skipCertificateCheckText: String

    public init(_ settings: AgentServerSettings) {
        isEnabled = settings.isEnabled
        instructions = settings.instructions
        allowPrivateAddressesText = SettingsList.text(settings.allowPrivateAddresses)
        skipCertificateCheckText = SettingsList.text(settings.skipCertificateCheck)
    }

    public var agentParameters: [String: JSONValue] {
        [
            "enabled": .bool(isEnabled), "instructions": .string(instructions),
            "allowPrivateAddresses": SettingsList.value(SettingsList.lineItems(allowPrivateAddressesText)),
            "skipCertificateCheck": SettingsList.value(SettingsList.lineItems(skipCertificateCheckText)),
        ]
    }
}

/// AgentProviderDraft is a provider as its editor holds it. The key is
/// never read back: blank keeps the stored one.
public struct AgentProviderDraft: Hashable, Sendable {
    /// ModelPriceDraft is one row of per-model prices as typed.
    public struct ModelPriceDraft: Hashable, Sendable, Identifiable {
        public let id: UUID
        public var model = ""
        public var inputText = ""
        public var outputText = ""
        public var cacheReadText = ""
        public var cacheWriteText = ""

        public init() {
            id = UUID()
        }

        public init(_ price: AgentProviderSettings.ModelPrice) {
            id = UUID()
            model = price.model
            inputText = SettingsList.text(price.input)
            outputText = SettingsList.text(price.output)
            cacheReadText = SettingsList.text(price.cacheRead)
            cacheWriteText = SettingsList.text(price.cacheWrite)
        }
    }

    public var name = ""
    public var providerKind = "openai"
    public var baseUrl = ""
    public var apiKey = ""
    public var hasApiKey = false
    public var hasRefreshToken = false
    public var isEnabled = true
    public var allowText = ""
    public var denyText = ""
    public var pricingInputText = ""
    public var pricingOutputText = ""
    public var pricingCacheReadText = ""
    public var pricingCacheWriteText = ""
    public var modelPrices: [ModelPriceDraft] = []

    public init() {}

    public init(_ provider: AgentProviderSettings) {
        name = provider.name
        providerKind = provider.providerKind
        baseUrl = provider.baseUrl
        hasApiKey = provider.hasApiKey
        hasRefreshToken = provider.hasRefreshToken
        isEnabled = provider.isEnabled
        allowText = SettingsList.text(provider.allow)
        denyText = SettingsList.text(provider.deny)
        pricingInputText = SettingsList.text(provider.pricingInput)
        pricingOutputText = SettingsList.text(provider.pricingOutput)
        pricingCacheReadText = SettingsList.text(provider.pricingCacheRead)
        pricingCacheWriteText = SettingsList.text(provider.pricingCacheWrite)
        modelPrices = (provider.modelPricing ?? []).map(ModelPriceDraft.init)
    }

    public var isSignedInKind: Bool { providerKind == AgentProviderSettings.signedInKind }

    /// Whether it can be saved: it has a name, and a provider on a plan has
    /// been signed in to, since the sign-in is what saves one.
    public var canSave: Bool {
        !name.trimmingCharacters(in: .whitespaces).isEmpty && (!isSignedInKind || hasRefreshToken)
    }

    static func priceValue(_ price: AgentProviderSettings.ModelPrice) -> JSONValue {
        .object([
            "model": .string(price.model), "input": .number(price.input), "output": .number(price.output),
            "cacheRead": .number(price.cacheRead), "cacheWrite": .number(price.cacheWrite),
        ])
    }

    var parameters: [String: JSONValue] {
        let prices = modelPrices.filter { !$0.model.trimmingCharacters(in: .whitespaces).isEmpty }.map { row in
            AgentProviderSettings.ModelPrice(
                model: row.model.trimmingCharacters(in: .whitespaces), input: SettingsList.number(row.inputText),
                output: SettingsList.number(row.outputText), cacheRead: SettingsList.number(row.cacheReadText),
                cacheWrite: SettingsList.number(row.cacheWriteText))
        }
        return [
            "name": .string(name.trimmingCharacters(in: .whitespaces)), "kind": .string(providerKind),
            "baseUrl": .string(baseUrl.trimmingCharacters(in: .whitespaces)), "apiKey": .string(apiKey), "enabled": .bool(isEnabled),
            "allow": SettingsList.value(SettingsList.lineItems(allowText)), "deny": SettingsList.value(SettingsList.lineItems(denyText)),
            "pricingInput": .number(SettingsList.number(pricingInputText)),
            "pricingOutput": .number(SettingsList.number(pricingOutputText)),
            "pricingCacheRead": .number(SettingsList.number(pricingCacheReadText)),
            "pricingCacheWrite": .number(SettingsList.number(pricingCacheWriteText)), "modelPricing": .array(prices.map(Self.priceValue)),
        ]
    }

    /// The agent section that saves the provider list with this draft added,
    /// or put in place of the provider at the index given. The list is sent
    /// whole; a renamed provider takes its model assignments with it, or the
    /// server would refuse the settings for naming one that is gone.
    public func agentParameters(replacingProviderAt index: Int?, in settings: AgentServerSettings) -> [String: JSONValue] {
        var providers = settings.providers.map(\.storedParameters)
        var section: [String: JSONValue] = [:]
        if let index, settings.providers.indices.contains(index) {
            let previousName = settings.providers[index].name
            var edited = parameters
            edited["previousName"] = .string(previousName)
            providers[index] = edited
            let newName = name.trimmingCharacters(in: .whitespaces)
            if previousName != newName {
                section["models"] = settings.models.renaming(provider: previousName, to: newName).parameters
            }
        } else {
            providers.append(parameters)
        }
        section["providers"] = .array(providers.map { .object($0) })
        return section
    }

    /// The agent section that saves the provider list without the provider
    /// at the index given.
    public static func agentParameters(removingProviderAt index: Int, in settings: AgentServerSettings) -> [String: JSONValue] {
        let providers = settings.providers.enumerated().filter { $0.offset != index }.map { JSONValue.object($0.element.storedParameters) }
        return ["providers": .array(providers)]
    }
}

/// AgentLimitsDraft is the limits, the retention and the currency as their
/// form holds them, each limit as typed.
public struct AgentLimitsDraft: Hashable, Sendable {
    /// The limits in the order the form groups them; requestTimeout is the
    /// one written as a duration rather than a number.
    public static let limitGroups: [[String]] = [
        ["dailyTokensPerAgent", "monthlyTokensPerServer", "maxBodyCharacters"],
        ["dailyCostPerAgent", "monthlyCostPerServer"],
        ["maxRoundsPerAsk", "maxRoundsPerResearch", "maxRoundsPerReply", "maxToolCallsPerRun"],
        ["maxRoundsPerDream", "scanConcurrency", "rewriteConcurrency", "dreamShare"],
        ["ingestChunksPerRun", "embeddingTokensPerDay"],
        ["maxRoundsPerSubagent", "goalTurnsPerDay", "goalTurnsAlone", "goalsInProgress", "backgroundWakesAlone"],
        ["requestTimeout", "concurrency"],
    ]

    public var limitTexts: [String: String]
    public var retention: AgentRetention
    public var currency: String

    public init(_ settings: AgentServerSettings) {
        let limits = settings.limits
        let numbers: [String: Double] = [
            "maxBodyCharacters": Double(limits.maxBodyCharacters), "dailyTokensPerAgent": Double(limits.dailyTokensPerAgent),
            "monthlyTokensPerServer": Double(limits.monthlyTokensPerServer), "dailyCostPerAgent": limits.dailyCostPerAgent,
            "monthlyCostPerServer": limits.monthlyCostPerServer, "maxRoundsPerAsk": Double(limits.maxRoundsPerAsk),
            "maxRoundsPerResearch": Double(limits.maxRoundsPerResearch), "maxRoundsPerReply": Double(limits.maxRoundsPerReply),
            "maxToolCallsPerRun": Double(limits.maxToolCallsPerRun), "concurrency": Double(limits.concurrency),
            "maxRoundsPerDream": Double(limits.maxRoundsPerDream), "scanConcurrency": Double(limits.scanConcurrency),
            "rewriteConcurrency": Double(limits.rewriteConcurrency), "dreamShare": limits.dreamShare,
            "ingestChunksPerRun": Double(limits.ingestChunksPerRun), "embeddingTokensPerDay": Double(limits.embeddingTokensPerDay),
            "maxRoundsPerSubagent": Double(limits.maxRoundsPerSubagent), "goalTurnsPerDay": Double(limits.goalTurnsPerDay),
            "goalTurnsAlone": Double(limits.goalTurnsAlone), "goalsInProgress": Double(limits.goalsInProgress),
            "backgroundWakesAlone": Double(limits.backgroundWakesAlone),
        ]
        // Zero is said as 0, as the dashboard's form shows a stored limit.
        var limitTexts = numbers.mapValues { number in number == 0 ? "0" : SettingsList.text(number) }
        limitTexts["requestTimeout"] = limits.requestTimeout
        self.limitTexts = limitTexts
        retention = settings.retention
        currency = settings.currency
    }

    public var agentParameters: [String: JSONValue] {
        var limits: [String: JSONValue] = [:]
        for name in Self.limitGroups.joined() {
            let typed = limitTexts[name] ?? ""
            limits[name] = name == "requestTimeout" ? .string(typed) : .number(SettingsList.number(typed))
        }
        return [
            "limits": .object(limits),
            "retention": .object(["runs": .string(retention.runs), "corrections": .string(retention.corrections)]),
            "currency": .string(currency),
        ]
    }
}

/// AgentToolPolicyDraft is one word per family and per tool, allow, confirm
/// or off, as the tool policy form holds them. A tool's word follows its
/// family's unless it says otherwise.
public struct AgentToolPolicyDraft: Hashable, Sendable {
    public static let words = ["allow", "confirm", "off"]

    /// Every name the policy covers: the families, then the tools.
    public let names: [String]
    public var wordByName: [String: String]

    public init(_ settings: AgentServerSettings) {
        names = settings.families + settings.tools.catalog.map(\.name)
        var wordByName: [String: String] = [:]
        for name in names {
            if settings.tools.disabled.contains(name) {
                wordByName[name] = "off"
            } else if settings.tools.confirm.contains(name) {
                wordByName[name] = "confirm"
            } else {
                wordByName[name] = "allow"
            }
        }
        self.wordByName = wordByName
    }

    public func word(_ name: String) -> String { wordByName[name] ?? "allow" }

    public var agentParameters: [String: JSONValue] {
        [
            "tools": .object([
                "disabled": SettingsList.value(names.filter { word($0) == "off" }),
                "confirm": SettingsList.value(names.filter { word($0) == "confirm" }),
            ])
        ]
    }
}

/// AgentWebSearchDraft is the web search service as its form holds it.
public struct AgentWebSearchDraft: Hashable, Sendable {
    /// Empty for none.
    public var searchKind: String
    public var apiKey = ""

    public init(_ search: AgentServerSettings.WebSearch) {
        searchKind = search.searchKind
    }

    public var agentParameters: [String: JSONValue] {
        var search: [String: JSONValue] = ["kind": .string(searchKind)]
        if !apiKey.isEmpty { search["apiKey"] = .string(apiKey) }
        return ["search": .object(search)]
    }
}

/// AgentBrowserDraft is the headless browser as its form holds it.
public struct AgentBrowserDraft: Hashable, Sendable {
    public var isEnabled: Bool
    public var cdpEndpoint: String
    public var canAttachTabs: Bool
    public var allowPrivateAddressesText: String
    public var idleTimeout: String
    public var maxContextsText: String

    public init(_ browser: AgentServerSettings.Browser) {
        isEnabled = browser.isEnabled
        cdpEndpoint = browser.cdpEndpoint
        canAttachTabs = browser.canAttachTabs
        allowPrivateAddressesText = SettingsList.text(browser.allowPrivateAddresses)
        idleTimeout = browser.idleTimeout
        maxContextsText = String(browser.maxContexts)
    }

    public var agentParameters: [String: JSONValue] {
        [
            "browser": .object([
                "enabled": .bool(isEnabled), "cdpEndpoint": .string(cdpEndpoint), "attachTabs": .bool(canAttachTabs),
                "allowPrivateAddresses": SettingsList.value(SettingsList.lineItems(allowPrivateAddressesText)),
                "idleTimeout": .string(idleTimeout), "maxContexts": .number(SettingsList.number(maxContextsText)),
            ])
        ]
    }
}

/// AgentServerVoiceDraft is talking to the agent, as the operator's form
/// holds it.
public struct AgentServerVoiceDraft: Hashable, Sendable {
    public var isEnabled: Bool
    /// A provider of kind openai by name, or empty for the first one.
    public var provider: String
    public var transcriptionModel: String
    public var silenceMSText: String
    public var speechModel: String
    /// Empty for marin.
    public var speechVoice: String
    public var askModel: String

    public init(_ voice: AgentServerSettings.Voice) {
        isEnabled = voice.isEnabled
        provider = voice.provider
        transcriptionModel = voice.transcriptionModel
        silenceMSText = voice.silenceMS > 0 ? String(voice.silenceMS) : ""
        speechModel = voice.speechModel
        speechVoice = voice.speechVoice
        askModel = voice.askModel
    }

    public var agentParameters: [String: JSONValue] {
        [
            "voice": .object([
                "enabled": .bool(isEnabled), "provider": .string(provider),
                "transcriptionModel": .string(transcriptionModel.trimmingCharacters(in: .whitespaces)),
                "silenceMS": .number(SettingsList.number(silenceMSText)),
                "speechModel": .string(speechModel.trimmingCharacters(in: .whitespaces)), "speechVoice": .string(speechVoice),
                "askModel": .string(askModel.trimmingCharacters(in: .whitespaces)),
            ])
        ]
    }
}

/// AgentFinanceDraft is the finance providers and the Plaid account as
/// their form holds them. The Plaid secret is never read back.
public struct AgentFinanceDraft: Hashable, Sendable {
    public var offeredProviders: [String]
    public var environment: String
    public var clientId: String
    public var secret = ""
    public var countryCodesText: String
    public var products: [String]

    public init(_ finance: AgentServerSettings.Finance) {
        offeredProviders = finance.offeredProviders
        environment = finance.plaid.environment
        clientId = finance.plaid.clientId
        countryCodesText = SettingsList.text(finance.plaid.countryCodes)
        products = finance.plaid.products
    }

    public var isOfferingPlaid: Bool { offeredProviders.contains("plaid") }

    /// Whether a product is on. Transactions is what every finance source
    /// is read with, so it always is.
    public func hasProduct(_ product: String) -> Bool { product == "transactions" || products.contains(product) }

    public mutating func setOffered(_ provider: String, isOffered: Bool) {
        offeredProviders.removeAll { $0 == provider }
        if isOffered { offeredProviders.append(provider) }
    }

    public mutating func setProduct(_ product: String, isOn: Bool) {
        var kept = products.filter { $0 != product && $0 != "transactions" } + ["transactions"]
        if isOn { kept.append(product) }
        products = kept
    }

    public var agentParameters: [String: JSONValue] {
        // In the order the server lists them, whatever order they were
        // ticked in.
        let ordered: ([String], [String]) -> [String] = { chosen, order in order.filter { chosen.contains($0) } }
        var plaid: [String: JSONValue] = [
            "environment": .string(environment), "clientId": .string(clientId),
            "countryCodes": SettingsList.value(SettingsList.lineItems(countryCodesText).map { $0.uppercased() }),
            "products": SettingsList.value(ordered(products, AgentServerSettings.plaidProducts)),
        ]
        if !secret.isEmpty { plaid["secret"] = .string(secret) }
        return [
            "finance": .object([
                "offeredProviders": SettingsList.value(ordered(offeredProviders, AgentServerSettings.financeProviders)),
                "plaid": .object(plaid),
            ])
        ]
    }
}

/// ConnectedServerDraft is a connected server as its editor holds it. The
/// secrets are never read back: blank keeps the stored ones, and the
/// environment's stored values are kept the same way, by name.
public struct ConnectedServerDraft: Hashable, Sendable {
    public var name = ""
    /// http, stdio, or empty to infer it.
    public var transport = ""
    public var url = ""
    public var command = ""
    public var argsText = ""
    public var envText = ""
    public var workingDir = ""
    /// none, static, user, oauth, or empty to infer it.
    public var auth = ""
    public var authorization = ""
    public var hasAuthorization = false
    public var oauthClientId = ""
    public var oauthClientSecret = ""
    public var hasOauthClientSecret = false
    public var oauthScopesText = ""
    public var oauthAuthorizationUrl = ""
    public var oauthTokenUrl = ""
    /// Empty for this server's address, computer for the attached computer.
    public var oauthRedirect = ""
    public var isHeadless = false
    /// Empty for this server, computer for the person's attached computer.
    public var location = ""
    public var readOnlyText = ""
    public var disabledText = ""
    public var timeout = ""
    public var isEnabled = true

    public init() {}

    public init(_ server: ConnectedServerSettings) {
        name = server.name
        transport = server.transport
        url = server.url
        command = server.command
        argsText = server.args.joined(separator: " ")
        envText = server.envNames.map { "\($0)=" }.joined(separator: "\n")
        workingDir = server.workingDir
        auth = server.auth
        hasAuthorization = server.hasAuthorization
        oauthClientId = server.oauthClientId
        hasOauthClientSecret = server.hasOauthClientSecret
        oauthScopesText = SettingsList.text(server.oauthScopes)
        oauthAuthorizationUrl = server.oauthAuthorizationUrl
        oauthTokenUrl = server.oauthTokenUrl
        oauthRedirect = server.oauthRedirect == "computer" ? "computer" : ""
        isHeadless = server.isHeadless
        location = server.location
        readOnlyText = SettingsList.text(server.readOnly)
        disabledText = SettingsList.text(server.disabled)
        timeout = server.timeout
        isEnabled = server.isEnabled
    }

    private var hasCommand: Bool { !command.trimmingCharacters(in: .whitespaces).isEmpty }
    private var hasUrl: Bool { !url.trimmingCharacters(in: .whitespaces).isEmpty }

    /// Whether it runs a command: said so, or inferred from a command and
    /// no address.
    public var isStdio: Bool { transport == "stdio" || (transport.isEmpty && hasCommand && !hasUrl) }
    /// Whether where the command runs is asked.
    public var asksLocation: Bool { transport == "stdio" || (transport.isEmpty && hasCommand) }
    public var showsUrl: Bool { transport != "stdio" }
    public var showsCommand: Bool { transport != "http" }
    public var showsAuthorization: Bool { auth == "static" || auth.isEmpty }
    public var isOAuth: Bool { auth == "oauth" }
    /// A server on the person's computer is there only while they are, so a
    /// run with nobody present cannot use it.
    public var canBeHeadless: Bool { location != "computer" }

    public var canSave: Bool { !name.trimmingCharacters(in: .whitespaces).isEmpty && (hasUrl || hasCommand) }

    public mutating func setLocation(_ newLocation: String) {
        location = newLocation
        if newLocation == "computer" { isHeadless = false }
    }

    var parameters: [String: JSONValue] {
        [
            "name": .string(name.trimmingCharacters(in: .whitespaces)), "transport": .string(transport),
            "url": .string(url.trimmingCharacters(in: .whitespaces)), "command": .string(command.trimmingCharacters(in: .whitespaces)),
            "args": SettingsList.value(argsText.split(whereSeparator: \.isWhitespace).map(String.init)),
            "env": SettingsList.value(envText.split(separator: "\n").map(String.init).filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }),
            "workingDir": .string(workingDir.trimmingCharacters(in: .whitespaces)), "auth": .string(auth),
            "authorization": .string(authorization), "oauthClientId": .string(oauthClientId.trimmingCharacters(in: .whitespaces)),
            "oauthClientSecret": .string(oauthClientSecret), "oauthScopes": SettingsList.value(SettingsList.lineItems(oauthScopesText)),
            "oauthAuthorizationUrl": .string(oauthAuthorizationUrl.trimmingCharacters(in: .whitespaces)),
            "oauthTokenUrl": .string(oauthTokenUrl.trimmingCharacters(in: .whitespaces)), "oauthRedirect": .string(oauthRedirect),
            "headless": .bool(isHeadless), "location": .string(location),
            "readOnly": SettingsList.value(SettingsList.lineItems(readOnlyText)),
            "disabled": SettingsList.value(SettingsList.lineItems(disabledText)),
            "timeout": .string(timeout.trimmingCharacters(in: .whitespaces)), "enabled": .bool(isEnabled),
        ]
    }

    /// The agent section that saves the server list with this draft added,
    /// or put in place of the server at the index given, carrying its
    /// previous name so its stored secrets follow a rename.
    public func agentParameters(replacingServerAt index: Int?, in settings: AgentServerSettings) -> [String: JSONValue] {
        var servers = settings.mcpServers.map(\.storedParameters)
        if let index, settings.mcpServers.indices.contains(index) {
            var edited = parameters
            edited["previousName"] = .string(settings.mcpServers[index].name)
            servers[index] = edited
        } else {
            servers.append(parameters)
        }
        return ["mcpServers": .array(servers.map { .object($0) })]
    }

    /// The agent section that saves the server list without the server at
    /// the index given.
    public static func agentParameters(removingServerAt index: Int, in settings: AgentServerSettings) -> [String: JSONValue] {
        let servers = settings.mcpServers.enumerated().filter { $0.offset != index }.map { JSONValue.object($0.element.storedParameters) }
        return ["mcpServers": .array(servers)]
    }
}

/// ProviderSignIn is a sign-in to a provider that runs on a person's plan:
/// a one-time code to type on the service's own page, and, once typed, the
/// account and plan it signed in as.
public struct ProviderSignIn: Codable, Hashable, Sendable {
    public let signInId: String
    public let userCode: String
    public let verificationAddress: String
    public let isSignedIn: Bool
    public let account: String
    public let plan: String
}

// MARK: - Everybody's agents

/// AgentUsageTotals are tokens and calls added up.
public struct AgentUsageTotals: Codable, Hashable, Sendable {
    public let promptTokens: Int
    public let completionTokens: Int
    public let cacheReadTokens: Int
    public let cacheWriteTokens: Int
    public let callCount: Int

    enum CodingKeys: String, CodingKey {
        case promptTokens, completionTokens, cacheReadTokens, cacheWriteTokens
        case callCount = "calls"
    }

    public init(promptTokens: Int, completionTokens: Int, cacheReadTokens: Int = 0, cacheWriteTokens: Int = 0, callCount: Int = 0) {
        self.promptTokens = promptTokens
        self.completionTokens = completionTokens
        self.cacheReadTokens = cacheReadTokens
        self.cacheWriteTokens = cacheWriteTokens
        self.callCount = callCount
    }

    /// Every token, read, written and cached.
    public var totalTokenCount: Int { promptTokens + completionTokens + cacheReadTokens + cacheWriteTokens }
}

/// AgentUsageRow is one row of the server's usage: a day, a kind of run, a
/// model or an agent, what it used and what it cost.
public struct AgentUsageRow: Codable, Hashable, Sendable {
    /// What the row is by; empty for the total.
    public let usageKey: String
    public let cost: Double
    public let currency: String?
    public let totals: AgentUsageTotals

    enum CodingKeys: String, CodingKey {
        case cost, currency, totals
        case usageKey = "key"
    }

    public init(usageKey: String, cost: Double, currency: String? = nil, totals: AgentUsageTotals) {
        self.usageKey = usageKey
        self.cost = cost
        self.currency = currency
        self.totals = totals
    }
}

/// AgentUsageGrouping is what the usage is added up by.
public enum AgentUsageGrouping: String, CaseIterable, Sendable {
    case day, kind, model, agent
}

/// AgentUsageRange is the days the usage covers, in the person's own zone:
/// from the start of the first through the end of the last.
public struct AgentUsageRange: Hashable, Sendable {
    public var firstDay: Date
    public var lastDay: Date

    /// Thirty days back to today, as the dashboard opens it.
    public static func lastThirtyDays(now: Date = .now, calendar: Calendar = .current) -> AgentUsageRange {
        AgentUsageRange(firstDay: calendar.startOfDay(for: now.addingTimeInterval(-30 * 86400)), lastDay: calendar.startOfDay(for: now))
    }

    public init(firstDay: Date, lastDay: Date) {
        self.firstDay = firstDay
        self.lastDay = lastDay
    }

    /// The start of the first day and the start of the day after the last,
    /// as the server takes them.
    public func bounds(calendar: Calendar = .current) -> (since: String, until: String) {
        let since = calendar.startOfDay(for: firstDay)
        let lastStart = calendar.startOfDay(for: lastDay)
        let until = calendar.date(byAdding: .day, value: 1, to: lastStart) ?? lastStart.addingTimeInterval(86400)
        let formatter = ISO8601DateFormatter()
        return (formatter.string(from: since), formatter.string(from: until))
    }
}

/// AgentBudgetToday is what an agent has used of the day and what bounds
/// it.
public struct AgentBudgetToday: Codable, Hashable, Sendable {
    public let used: Int
    public let limit: Int
    public let resetsAt: String?
    public let cost: Double
    public let costLimit: Double
    public let currency: String?
}

/// AgentAdminSummary is somebody's agent as the operator sees it: whose it
/// is, whether it is on, what it may reach, what it costs, and its two
/// controls, a limit and a switch. Tokens and counts, never content.
public struct AgentAdminSummary: Codable, Hashable, Sendable, Identifiable {
    /// MailboxSource is a mailbox the agent may be given.
    public struct MailboxSource: Codable, Hashable, Sendable {
        /// MailboxPolicy is whether it was given.
        public struct MailboxPolicy: Codable, Hashable, Sendable {
            public let isGranted: Bool

            enum CodingKeys: String, CodingKey {
                case isGranted = "granted"
            }
        }

        public let mailboxId: String
        public let name: String
        public let policy: MailboxPolicy?
    }

    public let agentId: String
    public let userId: String
    public let username: String
    public let name: String
    public let isEnabled: Bool
    public let operatorDisabledAt: String?
    public let dailyTokens: Int
    public let dailyCost: Double
    public let sources: [MailboxSource]
    public let today: AgentBudgetToday?
    public let lastRunAt: String?
    public let deadJobCount: Int
    public let queuedJobCount: Int
    public let totals: AgentUsageTotals

    enum CodingKeys: String, CodingKey {
        case agentId, userId, username, name, operatorDisabledAt, dailyTokens, dailyCost, sources, today, lastRunAt, totals
        case isEnabled = "enabled"
        case deadJobCount = "dead"
        case queuedJobCount = "queued"
    }

    public var id: String { agentId }
    /// Whether an operator switched it off.
    public var isSwitchedOff: Bool { !(operatorDisabledAt ?? "").isEmpty }
    public var grantedMailboxCount: Int { sources.filter { $0.policy?.isGranted == true }.count }
    /// Whether the day is bounded in money, which is then how it is said.
    public var isBoundByCost: Bool { (today?.costLimit ?? 0) > 0 }
}

/// AgentDeadJob is a job given up on: what kind, whose, after how many
/// attempts, and why.
public struct AgentDeadJob: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let agentId: String
    public let mailboxId: String?
    public let jobKind: String
    public let subjectId: String?
    public let attemptCount: Int
    public let errorMessage: String?
    public let finishedAt: String?

    enum CodingKeys: String, CodingKey {
        case id, agentId, mailboxId, subjectId, finishedAt
        case jobKind = "kind"
        case attemptCount = "attempts"
        case errorMessage = "error"
    }
}

/// AgentAdminOverview is the agents page's one read: every agent, the
/// server's usage over a range, and the jobs given up on.
public struct AgentAdminOverview: Hashable, Sendable {
    public let agents: [AgentAdminSummary]
    public let usageRows: [AgentUsageRow]
    public let deadJobs: [AgentDeadJob]

    public init(agents: [AgentAdminSummary], usageRows: [AgentUsageRow], deadJobs: [AgentDeadJob]) {
        self.agents = agents
        self.usageRows = usageRows
        self.deadJobs = deadJobs
    }

    /// Whose an agent is, by its id, or the id when it is not known.
    public func username(agentId: String) -> String {
        agents.first { $0.agentId == agentId }?.username ?? agentId
    }
}

/// AgentLimitDraft is one agent's daily limits as the dialog holds them:
/// tokens, money, or both, each empty for the server's default.
public struct AgentLimitDraft: Hashable, Sendable {
    public var tokensText: String
    public var costText: String

    public init(_ summary: AgentAdminSummary) {
        tokensText = summary.dailyTokens > 0 ? String(summary.dailyTokens) : ""
        costText = summary.dailyCost > 0 ? SettingsList.text(summary.dailyCost) : ""
    }

    public var dailyTokens: Int { Int(SettingsList.number(tokensText)) }
    public var dailyCost: Double { SettingsList.number(costText) }
}

/// ServerAgentRun is one run any agent made: whose, what it was about, its
/// kind, when it last spoke and what it used. Its id opens its transcript.
public struct ServerAgentRun: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let agentId: String
    public let title: String
    public let jobKind: String?
    public let lastAt: String
    public let usage: AgentUsage

    /// Every token it used: read fresh, read from the cache, and written.
    public var tokenCount: Int { usage.promptTokens + (usage.cacheReadTokens ?? 0) + usage.completionTokens }
}

/// ServerAgentRunPage is one page of every agent's runs, and how many match
/// in all.
public struct ServerAgentRunPage: Codable, Hashable, Sendable {
    public let total: Int
    public let runs: [ServerAgentRun]
}

/// ServerAgentRunFilter narrows every agent's runs, on the server: to one
/// agent, one job given up on, kinds of run, and words in the title.
public struct ServerAgentRunFilter: Hashable, Sendable {
    public var agentId: String?
    public var jobId: String?
    public var jobKind: String?
    public var query = ""

    public init(agentId: String? = nil, jobId: String? = nil, jobKind: String? = nil, query: String = "") {
        self.agentId = agentId
        self.jobId = jobId
        self.jobKind = jobKind
        self.query = query
    }

    func variables(first: Int, offset: Int) -> [String: JSONValue] {
        let trimmedQuery = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return [
            "first": .number(Double(first)), "offset": .number(Double(offset)), "agentId": agentId.map { .string($0) } ?? .null,
            "jobId": jobId.map { .string($0) } ?? .null, "kinds": jobKind.map { .array([.string($0)]) } ?? .null,
            "query": trimmedQuery.isEmpty ? .null : .string(trimmedQuery),
        ]
    }
}

// MARK: - Skills and source types

/// RegistryOffer is something a registry publishes, a skill or a source
/// type, and whether this server has it.
public struct RegistryOffer: Codable, Hashable, Sendable, Identifiable {
    public let name: String
    public let description: String
    public let version: String
    public let tags: [String]
    /// The version installed here, when one is.
    public let installed: String?
    public let isNewer: Bool

    enum CodingKeys: String, CodingKey {
        case name, description, version, tags, installed
        case isNewer = "newer"
    }

    public init(name: String, description: String = "", version: String, tags: [String] = [], installed: String? = nil, isNewer: Bool = false) {
        self.name = name
        self.description = description
        self.version = version
        self.tags = tags
        self.installed = installed
        self.isNewer = isNewer
    }

    public var id: String { name }
    public var isInstalled: Bool { !(installed ?? "").isEmpty }
    /// Installed, and as new as the registry has.
    public var isCurrent: Bool { isInstalled && !isNewer }

    /// Whether a filter's words find it, in its name, description or tags.
    public func matches(_ filterText: String) -> Bool {
        let words = filterText.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !words.isEmpty else { return true }
        return ([name, description] + tags).contains { $0.lowercased().contains(words) }
    }

    /// Whether the registry has a newer version of something installed.
    public static func isBehind(installedCount: Int, offers: [RegistryOffer]) -> Bool {
        installedCount > 0 && offers.contains(where: \.isNewer)
    }
}

/// InstalledSkill is a skill the operator installed: what it brings, the
/// keys it asks for, who fills them in, and whether it is offered.
public struct InstalledSkill: Codable, Hashable, Sendable, Identifiable {
    /// SkillTool is one tool the skill declares.
    public struct SkillTool: Codable, Hashable, Sendable {
        public let name: String
        public let description: String
        public let toolKind: String
        public let doesNeedComputer: Bool

        enum CodingKeys: String, CodingKey {
            case name, description
            case toolKind = "kind"
            case doesNeedComputer = "needsComputer"
        }
    }

    public let name: String
    public let description: String
    public let version: String
    public let publisher: String
    public let isEnabled: Bool
    public let isReadable: Bool
    public let problem: String?
    /// Empty for as the skill says, operator, or person.
    public let skillScope: String
    /// The keys the operator fills in.
    public let secrets: [String]
    /// The keys each person fills in on their own agent page.
    public let personalSecrets: [String]
    public let tools: [SkillTool]

    enum CodingKeys: String, CodingKey {
        case name, description, version, publisher, problem, secrets, personalSecrets, tools
        case isEnabled = "enabled"
        case isReadable = "readable"
        case skillScope = "scope"
    }

    public var id: String { name }
    /// Whether one of its tools runs commands on a person's computer.
    public var doesRunCommands: Bool { tools.contains(where: \.doesNeedComputer) }
    /// Whether it asks for any values, so who fills them in is a choice.
    public var hasSecrets: Bool { !secrets.isEmpty || !personalSecrets.isEmpty }

    /// How many of the operator's keys are filled in, from the set of
    /// "skill/key" names that are.
    public func filledSecretCount(_ filledSecretNames: Set<String>) -> Int {
        secrets.filter { filledSecretNames.contains("\(name)/\($0)") }.count
    }
}

/// SkillSecretsDraft is the values typed for a skill's keys. Only what was
/// typed is sent: a key left blank keeps its value.
public struct SkillSecretsDraft: Hashable, Sendable {
    public let skillName: String
    public var valueByKey: [String: String] = [:]

    public init(skillName: String) {
        self.skillName = skillName
    }

    /// The keys with something typed, in the order the skill asks for them.
    public func typedKeys(_ keys: [String]) -> [String] {
        keys.filter { !(valueByKey[$0] ?? "").isEmpty }
    }

    public func agentParameters(_ keys: [String]) -> [String: JSONValue] {
        let typed = typedKeys(keys).map { key -> JSONValue in
            .object(["skill": .string(skillName), "key": .string(key), "value": .string(valueByKey[key] ?? "")])
        }
        return ["skillSecrets": .array(typed)]
    }
}

/// InstalledSourceType is how a knowledge source is read, as the operator
/// installed it: from the registry, or a local file of their own.
public struct InstalledSourceType: Codable, Hashable, Sendable, Identifiable {
    public let name: String
    public let description: String
    public let version: String
    public let publisher: String
    public let isLocal: Bool
    /// files, journal, sent, web, or empty for a type that runs commands.
    public let reader: String
    /// Where it runs: server, computer.
    public let runs: [String]
    public let requires: [String]
    /// What it says about itself, in Markdown.
    public let guide: String
    public let isReadable: Bool
    public let problem: String?

    enum CodingKeys: String, CodingKey {
        case name, description, version, publisher, isLocal, reader, runs, requires, guide, problem
        case isReadable = "readable"
    }

    public var id: String { name }
    public var hasGuide: Bool { !guide.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
}

// MARK: - Documents

extension Documents {
    /// What every read and change of the server's settings answers with.
    static let manageServerSettingsFields = """
        s3 { enabled bucket region endpoint pathStyle accessKeyId hasSecretAccessKey credentialsFile }
        route53 { enabled zoneId region accessKeyId hasSecretAccessKey credentialsFile }
        antivirus { enabled host port }
        antispam { enabled engine effectiveEngine host port signalsEnabled dnsEnabled bayesEnabled rulesEnabled bayesMinimumMessages bayesLearnedSpam bayesLearnedHam }
        relay { enabled host port security username hasPassword }
        proxy { socks5 }
        certificates { perDomain hosts acmeEnabled acmeEmail acmeDirectoryUrl acmeChallenge certificateFile privateKeyFile }
        smtp { maxMessageSize maxRecipientsIncoming maxRecipientsOutgoing greylistDelay authRateLimit authRateBurst trustedSenders }
        resolver { nameserver checkInterval externalAddressServices }
        session { lifetime }
        passkey { enabled relyingPartyId displayName origins maximumPerUser }
        listen { smtpIncoming smtpOutgoing imap imaps http https debug }
        sso { providers { id name issuer clientId hasClientSecret groupsClaim createUsers } }
        identity { name mailServers externalAddresses logLevel dataDirectory }
        storage { directory spoolRetention }
        geoip { enabled databaseFile }
        """
    /// What every read and change of the operator's agent settings answers
    /// with.
    static let manageAgentSettingsFields = """
        agent {
          enabled instructions allowPrivateAddresses skipCertificateCheck
          providers { name kind baseUrl hasApiKey hasRefreshToken account enabled allow deny pricingInput pricingOutput pricingCacheRead pricingCacheWrite modelPricing { model input output cacheRead cacheWrite } planUsage { planName observedAt windows { usedPercent windowMinutes resetsAt } } }
          models { default fast quick embedding triage research summarize reply ask schedule compact scan synthesize decide categorize embeddingDimensions choices }
          features { triage summaries draftReplies search research autoReply ask schedules browser connectedServers computer chatApps skills subagents remember knowledge dreaming }
          limits { maxBodyCharacters dailyTokensPerAgent monthlyTokensPerServer dailyCostPerAgent monthlyCostPerServer maxRoundsPerAsk maxRoundsPerResearch maxRoundsPerReply maxRoundsPerDream maxToolCallsPerRun requestTimeout concurrency scanConcurrency rewriteConcurrency dreamShare ingestChunksPerRun embeddingTokensPerDay maxRoundsPerSubagent goalTurnsPerDay goalTurnsAlone goalsInProgress backgroundWakesAlone }
          retention { runs corrections }
          currency
          search { kind hasApiKey }
          tools { disabled confirm catalog { name family risk description confirms core actions } }
          voice { enabled provider transcriptionModel silenceMS speechModel speechVoice askModel }
          browser { enabled cdpEndpoint attachTabs allowPrivateAddresses idleTimeout maxContexts }
          mcpServers { name transport effectiveTransport url command args envNames workingDir auth effectiveAuth hasAuthorization oauthClientId hasOauthClientSecret oauthScopes oauthAuthorizationUrl oauthTokenUrl oauthRedirect headless location readOnly disabled timeout enabled }
          finance { offeredProviders plaid { environment clientId hasSecret countryCodes products } }
          works families kinds
        }
        """
    /// What every read and change of an agent on the agents page answers
    /// with.
    static let manageAgentSummaryFields = """
        agentId userId username name enabled operatorDisabledAt dailyTokens dailyCost dead queued lastRunAt
        sources { mailboxId name policy { granted } }
        today { used limit resetsAt cost costLimit currency }
        totals { promptTokens completionTokens cacheReadTokens cacheWriteTokens calls }
        """
    /// What every read of an installed skill answers with.
    static let manageSkillFields = """
        name description version publisher enabled readable problem scope secrets personalSecrets
        tools { name description kind needsComputer }
        """
    /// What every read of an installed source type answers with.
    static let manageSourceTypeFields = """
        name description version publisher url isLocal installedAt reader runs requires guide readable problem
        settings { name description settingType pattern itemPattern default isRequired minimum maximum }
        secrets { key description isOptional }
        """
    /// What every step of a provider sign-in answers with.
    static let manageProviderSignInFields = """
        signInId userCode verificationAddress isSignedIn account plan
        """

    public static let manageServerStatus = """
        {
          GetServerStatus {
            instance version commit startedAt uptimeSeconds
            pendingRestart supervision restarting
          }
        }
        """
    public static let manageRestartServer = """
        mutation { RestartServer { started instance supervision } }
        """
    public static let manageServerUpgrade = """
        query ($check: Boolean) {
          GetUpgrade(check: $check) {
            current latest available notes url checkedAt attemptedAt checking error checkError applicable reason automatic enabled window upgrading
          }
        }
        """
    public static let manageApplyUpgrade = """
        mutation ($version: String) {
          ApplyUpgrade(version: $version) {
            current latest available applicable reason upgrading
          }
        }
        """
    public static let manageServerSetup = """
        {
          GetServerAddresses { ipv4 ipv6 error }
          GetOutgoingIdentity {
            address via reverseName forwardAddresses confirmed
            helloName helloAddresses helloMatches unknown
          }
          GetSettings {
            submission { host port effectiveHost effectivePort }
            imap { host port tlsPort effectiveHost effectivePort effectiveTlsPort }
          }
        }
        """
    public static let manageUpdateSubmission = """
        mutation ($submission: SubmissionParametersInput) {
          UpdateSettings(submission: $submission) {
            submission { host port effectiveHost effectivePort }
          }
        }
        """
    public static let manageUpdateMailProgram = """
        mutation ($imap: IMAPParametersInput!) {
          UpdateSettings(imap: $imap) { imap { host port tlsPort effectiveHost effectivePort effectiveTlsPort } }
        }
        """
    public static let manageServerSettings = """
        { GetSettings { \(manageServerSettingsFields) } }
        """
    public static let manageUpdateServerSettings = """
        mutation (
          $s3: S3ParametersInput
          $route53: Route53ParametersInput
          $antivirus: ServiceParametersInput
          $antispam: AntispamParametersInput
          $relay: RelayParametersInput
          $proxy: ProxyParametersInput
          $certificates: CertificateParametersInput
          $smtp: SMTPParametersInput
          $resolver: ResolverParametersInput
          $session: SessionParametersInput
          $passkey: PasskeyParametersInput
          $listen: ListenParametersInput
          $sso: SSOParametersInput
          $identity: IdentityParametersInput
          $storage: StorageParametersInput
          $geoip: GeoIPParametersInput
        ) {
          UpdateSettings(
            s3: $s3
            route53: $route53
            antivirus: $antivirus
            antispam: $antispam
            relay: $relay
            proxy: $proxy
            certificates: $certificates
            smtp: $smtp
            resolver: $resolver
            session: $session
            passkey: $passkey
            listen: $listen
            sso: $sso
            identity: $identity
            storage: $storage
            geoip: $geoip
          ) {
            \(manageServerSettingsFields)
          }
        }
        """
    public static let manageAgentSettings = """
        { GetSettings { \(manageAgentSettingsFields) } }
        """
    public static let manageUpdateAgentSettings = """
        mutation ($agent: AgentParametersInput) {
          UpdateSettings(agent: $agent) { \(manageAgentSettingsFields) }
        }
        """
    public static let manageAgentModels = """
        { ListAgentModels { name contextLength } }
        """
    public static let manageTestAgentProvider = """
        query ($name: String!) { TestAgentProvider(name: $name) { name contextLength } }
        """
    public static let manageBeginProviderSignIn = """
        mutation ($provider: String!) { BeginAgentProviderSignIn(provider: $provider) { \(manageProviderSignInFields) } }
        """
    public static let manageFinishProviderSignIn = """
        mutation ($signInId: String!) { FinishAgentProviderSignIn(signInId: $signInId) { \(manageProviderSignInFields) } }
        """
    public static let manageAgentAdmin = """
        query ($by: String, $since: DateTime, $until: DateTime) {
          ListAgents { \(manageAgentSummaryFields) }
          AgentServerUsage(by: $by, since: $since, until: $until) { key cost currency totals { promptTokens completionTokens cacheReadTokens cacheWriteTokens calls } }
          ListAgentDeadLetters { id agentId mailboxId kind subjectId attempts error finishedAt }
        }
        """
    public static let manageSetAgentLimit = """
        mutation ($agentId: String!, $dailyTokens: Int64, $dailyCost: Float) {
          SetAgentLimit(agentId: $agentId, dailyTokens: $dailyTokens, dailyCost: $dailyCost) { \(manageAgentSummaryFields) }
        }
        """
    public static let manageSetAgentDisabled = """
        mutation ($agentId: String!, $disabled: Boolean!) {
          SetAgentDisabled(agentId: $agentId, disabled: $disabled) { \(manageAgentSummaryFields) }
        }
        """
    public static let manageRetryAgentJob = """
        mutation ($jobId: String!) { RetryAgentJob(jobId: $jobId) { id } }
        """
    public static let manageAllAgentRuns = """
        query ($first: Int, $offset: Int, $agentId: String, $jobId: String, $kinds: [String!], $query: String) {
          ListAllAgentRuns(first: $first, offset: $offset, agentId: $agentId, jobId: $jobId, kinds: $kinds, query: $query) {
            total
            runs { id agentId title jobKind lastAt usage { promptTokens cacheReadTokens completionTokens cost } }
          }
        }
        """
    public static let manageInstalledSkills = """
        query {
          ListAgentSkills { \(manageSkillFields) }
        }
        """
    public static let manageSkillRegistry = """
        query { SearchAgentSkills { name description version tags installed newer } }
        """
    public static let manageFilledSkillSecrets = """
        query { GetSettings { agent { skillSecrets { skill key hasValue } } } }
        """
    public static let manageInstallSkill = """
        mutation ($name: String!) {
          InstallAgentSkill(name: $name) { name version }
        }
        """
    public static let manageRemoveSkill = """
        mutation ($name: String!) { RemoveAgentSkill(name: $name) }
        """
    public static let manageSetSkillEnabled = """
        mutation ($name: String!, $enabled: Boolean!) {
          SetAgentSkillEnabled(name: $name, enabled: $enabled) { name enabled }
        }
        """
    public static let manageSetSkillScope = """
        mutation ($name: String!, $scope: String!) {
          SetAgentSkillScope(name: $name, scope: $scope) { name scope }
        }
        """
    public static let manageSaveSkillSecrets = """
        mutation ($agent: AgentParametersInput) {
          UpdateSettings(agent: $agent) { agent { skillSecrets { skill key hasValue } } }
        }
        """
    public static let manageInstalledSourceTypes = """
        query { ListAgentSourceTypes { \(manageSourceTypeFields) } }
        """
    public static let manageSourceTypeRegistry = """
        query { SearchAgentSourceTypes { name description version tags installed newer } }
        """
    public static let manageInstallSourceType = """
        mutation ($name: String!) { InstallAgentSourceType(name: $name) { name version } }
        """
    public static let manageRemoveSourceType = """
        mutation ($name: String!) { RemoveAgentSourceType(name: $name) }
        """
}

// MARK: - Connection

extension Connection {
    // MARK: About

    /// The instance being talked to: its version, when it started, what
    /// started it, and what a restart would pick up.
    public func serverStatus() async throws -> ServerStatus {
        struct Answer: Decodable, Sendable { let GetServerStatus: ServerStatus }
        return try await execute(Documents.manageServerStatus, as: Answer.self).GetServerStatus
    }

    /// Asks the instance to restart. It answers and then goes away, so a
    /// connection that closes mid-reply is no failure; whether it worked is
    /// for the wait afterwards to say.
    public func restartServer() async throws {
        struct Answer: Decodable, Sendable { let RestartServer: JSONValue? }
        _ = try await execute(Documents.manageRestartServer, as: Answer.self)
    }

    /// What the release list says, and, when asked, a check of it started
    /// in the background. The server says whether it started one.
    public func serverUpgrade(isChecking: Bool = false) async throws -> ServerUpgrade {
        struct Answer: Decodable, Sendable { let GetUpgrade: ServerUpgrade }
        return try await execute(Documents.manageServerUpgrade, variables: ["check": .bool(isChecking)], as: Answer.self).GetUpgrade
    }

    /// Starts an upgrade to the version shown, so it installs what the
    /// operator agreed to rather than whatever is newest by then.
    public func applyServerUpgrade(version: String?) async throws {
        struct Answer: Decodable, Sendable { let ApplyUpgrade: JSONValue? }
        _ = try await execute(Documents.manageApplyUpgrade, variables: ["version": version.map { .string($0) } ?? .null], as: Answer.self)
    }

    // MARK: Setup

    /// The Setup tab: the address seen from outside, how outgoing mail
    /// identifies itself, and what mail clients and programs are told.
    public func serverSetup() async throws -> ServerSetup {
        struct Answer: Decodable, Sendable {
            struct Settings: Decodable, Sendable {
                let submission: SubmissionAccess?
                let imap: MailProgramAccess?
            }
            let GetServerAddresses: ServerAddresses?
            let GetOutgoingIdentity: OutgoingIdentity?
            let GetSettings: Settings?
        }
        let answer = try await execute(Documents.manageServerSetup, as: Answer.self)
        return ServerSetup(
            addresses: answer.GetServerAddresses, outgoingIdentity: answer.GetOutgoingIdentity,
            submission: answer.GetSettings?.submission, mailProgram: answer.GetSettings?.imap)
    }

    /// Sets what a mail client is told to send through.
    public func updateSubmissionAccess(_ draft: SubmissionAccessDraft) async throws {
        struct Answer: Decodable, Sendable { let UpdateSettings: JSONValue? }
        _ = try await execute(Documents.manageUpdateSubmission, variables: draft.variables, as: Answer.self)
    }

    /// Sets what a mail program is told to read mail at.
    public func updateMailProgramAccess(_ draft: MailProgramAccessDraft) async throws {
        struct Answer: Decodable, Sendable { let UpdateSettings: JSONValue? }
        _ = try await execute(Documents.manageUpdateMailProgram, variables: draft.variables, as: Answer.self)
    }

    // MARK: Settings

    /// The server's own settings and the services it talks to.
    public func serverSettings() async throws -> ServerSettings {
        struct Answer: Decodable, Sendable { let GetSettings: ServerSettings }
        return try await execute(Documents.manageServerSettings, as: Answer.self).GetSettings
    }

    /// Changes one or more sections of the server's settings, each keyed as
    /// UpdateSettings takes it, and answers with what is now stored.
    public func updateServerSettings(_ sections: [String: JSONValue]) async throws -> ServerSettings {
        struct Answer: Decodable, Sendable { let UpdateSettings: ServerSettings }
        return try await execute(Documents.manageUpdateServerSettings, variables: sections, as: Answer.self).UpdateSettings
    }

    /// The operator's agent settings.
    public func agentServerSettings() async throws -> AgentServerSettings {
        struct Answer: Decodable, Sendable {
            struct Settings: Decodable, Sendable { let agent: AgentServerSettings }
            let GetSettings: Settings
        }
        return try await execute(Documents.manageAgentSettings, as: Answer.self).GetSettings.agent
    }

    /// Changes part of the operator's agent settings, a section at a time,
    /// and answers with what is now stored.
    public func updateAgentServerSettings(_ agentParameters: [String: JSONValue]) async throws -> AgentServerSettings {
        struct Answer: Decodable, Sendable {
            struct Settings: Decodable, Sendable { let agent: AgentServerSettings }
            let UpdateSettings: Settings
        }
        return try await execute(
            Documents.manageUpdateAgentSettings, variables: ["agent": .object(agentParameters)], as: Answer.self
        ).UpdateSettings.agent
    }

    /// The models the enabled providers offer, written provider:model. A
    /// provider that cannot be reached contributes nothing.
    public func agentModelNames() async throws -> [String] {
        struct Answer: Decodable, Sendable {
            struct Model: Decodable, Sendable { let name: String }
            let ListAgentModels: [Model?]
        }
        return try await execute(Documents.manageAgentModels, as: Answer.self).ListAgentModels.compactMap { $0?.name }
    }

    /// Asks one provider what it offers, and says why when it cannot.
    public func testAgentProvider(name: String) async throws -> [String] {
        struct Answer: Decodable, Sendable {
            struct Model: Decodable, Sendable { let name: String }
            let TestAgentProvider: [Model?]
        }
        return try await execute(Documents.manageTestAgentProvider, variables: ["name": .string(name)], as: Answer.self)
            .TestAgentProvider.compactMap { $0?.name }
    }

    /// Asks the service for a one-time code to sign a provider in to a
    /// person's plan.
    public func beginProviderSignIn(provider: String) async throws -> ProviderSignIn {
        struct Answer: Decodable, Sendable { let BeginAgentProviderSignIn: ProviderSignIn }
        return try await execute(
            Documents.manageBeginProviderSignIn, variables: ["provider": .string(provider.trimmingCharacters(in: .whitespaces))],
            as: Answer.self
        ).BeginAgentProviderSignIn
    }

    /// Asks whether the code has been typed. The server waits up to half a
    /// minute before answering that it has not.
    public func finishProviderSignIn(signInId: String) async throws -> ProviderSignIn {
        struct Answer: Decodable, Sendable { let FinishAgentProviderSignIn: ProviderSignIn }
        return try await execute(Documents.manageFinishProviderSignIn, variables: ["signInId": .string(signInId)], as: Answer.self)
            .FinishAgentProviderSignIn
    }

    // MARK: Everybody's agents

    /// Every agent, the server's usage over the range added up by what is
    /// asked, and the jobs given up on.
    public func agentAdminOverview(grouping: AgentUsageGrouping, range: AgentUsageRange) async throws -> AgentAdminOverview {
        struct Answer: Decodable, Sendable {
            let ListAgents: [AgentAdminSummary?]
            let AgentServerUsage: [AgentUsageRow?]
            let ListAgentDeadLetters: [AgentDeadJob?]
        }
        let bounds = range.bounds()
        let answer = try await execute(
            Documents.manageAgentAdmin,
            variables: ["by": .string(grouping.rawValue), "since": .string(bounds.since), "until": .string(bounds.until)], as: Answer.self)
        return AgentAdminOverview(
            agents: answer.ListAgents.compactMap { $0 }, usageRows: answer.AgentServerUsage.compactMap { $0 },
            deadJobs: answer.ListAgentDeadLetters.compactMap { $0 })
    }

    /// Sets an agent's daily limits; zero returns either to the server's.
    @discardableResult
    public func setAgentLimit(agentId: String, draft: AgentLimitDraft) async throws -> AgentAdminSummary {
        struct Answer: Decodable, Sendable { let SetAgentLimit: AgentAdminSummary }
        return try await execute(
            Documents.manageSetAgentLimit,
            variables: [
                "agentId": .string(agentId), "dailyTokens": .number(Double(draft.dailyTokens)), "dailyCost": .number(draft.dailyCost),
            ], as: Answer.self
        ).SetAgentLimit
    }

    /// Switches an agent off, which stops what it has queued, or on again.
    @discardableResult
    public func setAgentSwitchedOff(agentId: String, isSwitchedOff: Bool) async throws -> AgentAdminSummary {
        struct Answer: Decodable, Sendable { let SetAgentDisabled: AgentAdminSummary }
        return try await execute(
            Documents.manageSetAgentDisabled, variables: ["agentId": .string(agentId), "disabled": .bool(isSwitchedOff)], as: Answer.self
        ).SetAgentDisabled
    }

    /// Queues a job given up on again.
    public func retryAgentJob(jobId: String) async throws {
        struct Answer: Decodable, Sendable { let RetryAgentJob: JSONValue? }
        _ = try await execute(Documents.manageRetryAgentJob, variables: ["jobId": .string(jobId)], as: Answer.self)
    }

    /// One page of every run any agent made, newest first, filtered on the
    /// server.
    public func serverAgentRuns(filter: ServerAgentRunFilter, first: Int = 50, offset: Int = 0) async throws -> ServerAgentRunPage {
        struct Answer: Decodable, Sendable { let ListAllAgentRuns: ServerAgentRunPage }
        return try await execute(Documents.manageAllAgentRuns, variables: filter.variables(first: first, offset: offset), as: Answer.self)
            .ListAllAgentRuns
    }

    /// The conversation that shows what a job given up on tried: its latest
    /// run, or, for a remember job that never ran, the conversation it was
    /// about. Nil when there is neither.
    public func deadJobConversationId(_ job: AgentDeadJob) async throws -> String? {
        let page = try await serverAgentRuns(filter: ServerAgentRunFilter(jobId: job.id), first: 1)
        if let run = page.runs.first { return run.id }
        if job.jobKind == "remember", let subjectId = job.subjectId, !subjectId.isEmpty { return subjectId }
        return nil
    }

    // MARK: Skills

    public func installedSkills() async throws -> [InstalledSkill] {
        struct Answer: Decodable, Sendable { let ListAgentSkills: [InstalledSkill?] }
        return try await execute(Documents.manageInstalledSkills, as: Answer.self).ListAgentSkills.compactMap { $0 }
    }

    public func skillRegistry() async throws -> [RegistryOffer] {
        struct Answer: Decodable, Sendable { let SearchAgentSkills: [RegistryOffer?] }
        return try await execute(Documents.manageSkillRegistry, as: Answer.self).SearchAgentSkills.compactMap { $0 }
    }

    /// Which of the keys the installed skills ask the operator for are
    /// filled in, as "skill/key", without the values.
    public func filledSkillSecrets() async throws -> Set<String> {
        struct Answer: Decodable, Sendable {
            struct Secret: Decodable, Sendable {
                let skill: String
                let key: String
                let hasValue: Bool
            }
            struct Agent: Decodable, Sendable { let skillSecrets: [Secret?]? }
            struct Settings: Decodable, Sendable { let agent: Agent }
            let GetSettings: Settings
        }
        let secrets = try await execute(Documents.manageFilledSkillSecrets, as: Answer.self).GetSettings.agent.skillSecrets ?? []
        return Set(secrets.compactMap { $0 }.filter(\.hasValue).map { "\($0.skill)/\($0.key)" })
    }

    public func installSkill(name: String) async throws {
        struct Answer: Decodable, Sendable { let InstallAgentSkill: JSONValue? }
        _ = try await execute(Documents.manageInstallSkill, variables: ["name": .string(name)], as: Answer.self)
    }

    public func removeSkill(name: String) async throws {
        struct Answer: Decodable, Sendable { let RemoveAgentSkill: JSONValue? }
        _ = try await execute(Documents.manageRemoveSkill, variables: ["name": .string(name)], as: Answer.self)
    }

    /// Offers a skill's tools to everybody again, or stops offering them
    /// while keeping it installed.
    public func setSkillEnabled(name: String, isEnabled: Bool) async throws {
        struct Answer: Decodable, Sendable { let SetAgentSkillEnabled: JSONValue? }
        _ = try await execute(Documents.manageSetSkillEnabled, variables: ["name": .string(name), "enabled": .bool(isEnabled)], as: Answer.self)
    }

    /// Who fills a skill's values in: empty as the skill says, operator, or
    /// person.
    public func setSkillScope(name: String, skillScope: String) async throws {
        struct Answer: Decodable, Sendable { let SetAgentSkillScope: JSONValue? }
        _ = try await execute(Documents.manageSetSkillScope, variables: ["name": .string(name), "scope": .string(skillScope)], as: Answer.self)
    }

    /// Keeps the values typed for a skill's keys, one each for everybody.
    public func saveSkillSecrets(_ draft: SkillSecretsDraft, keys: [String]) async throws {
        struct Answer: Decodable, Sendable { let UpdateSettings: JSONValue? }
        _ = try await execute(Documents.manageSaveSkillSecrets, variables: ["agent": .object(draft.agentParameters(keys))], as: Answer.self)
    }

    // MARK: Source types

    public func installedSourceTypes() async throws -> [InstalledSourceType] {
        struct Answer: Decodable, Sendable { let ListAgentSourceTypes: [InstalledSourceType?] }
        return try await execute(Documents.manageInstalledSourceTypes, as: Answer.self).ListAgentSourceTypes.compactMap { $0 }
    }

    public func sourceTypeRegistry() async throws -> [RegistryOffer] {
        struct Answer: Decodable, Sendable { let SearchAgentSourceTypes: [RegistryOffer?] }
        return try await execute(Documents.manageSourceTypeRegistry, as: Answer.self).SearchAgentSourceTypes.compactMap { $0 }
    }

    public func installSourceType(name: String) async throws {
        struct Answer: Decodable, Sendable { let InstallAgentSourceType: JSONValue? }
        _ = try await execute(Documents.manageInstallSourceType, variables: ["name": .string(name)], as: Answer.self)
    }

    /// Removes a source type. The server refuses while a source is of it,
    /// and says which.
    public func removeSourceType(name: String) async throws {
        struct Answer: Decodable, Sendable { let RemoveAgentSourceType: JSONValue? }
        _ = try await execute(Documents.manageRemoveSourceType, variables: ["name": .string(name)], as: Answer.self)
    }
}
