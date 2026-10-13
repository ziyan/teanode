import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// ManageServerTests cover the operator's server and agent pages: reading
/// the server's answers, what is sent to change the settings, and what the
/// pages decide from them.
final class ManageServerTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering body: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "operator")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentBody(_ request: URLRequest) throws -> [String: JSONValue] {
        try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
    }

    private func sentVariables(_ request: URLRequest) throws -> [String: JSONValue] {
        guard case .object(let variables) = try sentBody(request)["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    private func sentDocument(_ request: URLRequest) throws -> String {
        try sentBody(request)["query"]?.stringValue ?? ""
    }

    // MARK: Who may open the pages

    func testTheAgentsPageOffersTheTabsThePermissionsOpen() {
        let auditor = OperatorAccess(isOperator: true, everywherePermissions: ["agent:audit"])
        XCTAssertTrue(auditor.canOpenAgentAdmin)
        XCTAssertEqual(auditor.agentAdminTabs, [.usage], "General is one of the operator's settings and needs server:manage")
        let everything = OperatorAccess(isOperator: true, everywherePermissions: ["agent:audit", "agent:act", "server:manage"])
        XCTAssertEqual(everything.agentAdminTabs, AgentAdminTab.allCases)
        let actor = OperatorAccess(isOperator: true, everywherePermissions: ["agent:act"])
        XCTAssertEqual(actor.agentAdminTabs, [.usage, .runs])
        XCTAssertFalse(OperatorAccess(isOperator: true, domainPermissions: [.init(domainId: "domain-1", permissions: ["server:manage"])]).canManageServer,
            "a server permission is never held through a domain")
        XCTAssertFalse(OperatorAccess(isOperator: false).canOpenAgentAdmin)
    }

    // MARK: About

    func testReadsTheInstanceAndWhatARestartWouldPickUp() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"GetServerStatus":{"instance":"node-a","version":"1.2.3","commit":"0123456789abcdef0123","startedAt":"2030-01-02T03:04:05Z",
                  "uptimeSeconds":3600,"pendingRestart":["listen.https"],"supervision":"unknown","restarting":false}}}
                """#)
        let serverStatus = try await connection.serverStatus()
        XCTAssertEqual(serverStatus.instance, "node-a")
        XCTAssertEqual(serverStatus.shortCommit, "0123456789ab")
        XCTAssertEqual(serverStatus.pendingRestart, ["listen.https"])
        XCTAssertTrue(serverStatus.isUnsupervised)
        XCTAssertFalse(serverStatus.isRestarting)
    }

    func testReadsTheUpgradeAndAsksForACheckOnlyWhenTold() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"GetUpgrade":{"current":"1.2.3","latest":"1.3.0","available":true,"notes":"- Faster","url":"https://example.com/releases/1.3.0",
                  "checkedAt":"2030-01-02T03:04:05Z","attemptedAt":"2030-01-02T03:04:05Z","checking":true,"error":null,"checkError":null,
                  "applicable":false,"reason":"this runs in a container","automatic":false,"enabled":true,"window":null,"upgrading":false}}}
                """#)
        let upgrade = try await connection.serverUpgrade(isChecking: true)
        XCTAssertEqual(upgrade.latest, "1.3.0")
        XCTAssertTrue(upgrade.isAvailable)
        XCTAssertFalse(upgrade.canUpgrade, "a deployment that cannot install it gets the reason, not the button")
        XCTAssertEqual(upgrade.refusalReason, "this runs in a container")
        XCTAssertEqual(upgrade.isChecking, true)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["check": .bool(true)])
    }

    func testAnUpgradeIsAppliedToTheVersionShown() async throws {
        let (connection, fake) = connection(
            answering: #"{"data":{"ApplyUpgrade":{"current":"1.2.3","latest":"1.3.0","available":true,"applicable":true,"upgrading":true}}}"#)
        try await connection.applyServerUpgrade(version: "1.3.0")
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["version": .string("1.3.0")])
    }

    func testTheUpgradeWatchTellsARestartFromAFailure() {
        var watch = ServerUpgradeWatch(previousVersion: "1.2.3")
        XCTAssertEqual(watch.observe(ServerUpgrade(current: "1.2.3", isUpgrading: true)), .keepWaiting)
        XCTAssertEqual(watch.observe(ServerUpgrade(current: "1.3.0")), .cameBackUpgraded, "a different version answering already restarted")

        var failing = ServerUpgradeWatch(previousVersion: "1.2.3")
        XCTAssertEqual(failing.observe(ServerUpgrade(current: "1.2.3", errorMessage: "checksum mismatch")), .stopped(errorMessage: "checksum mismatch"))
    }

    func testTheUpgradeWatchWaitsLongerBeforeTheServerSaidItWasUpgrading() {
        var unconfirmed = ServerUpgradeWatch(previousVersion: "1.2.3")
        for _ in 0..<4 { XCTAssertEqual(unconfirmed.observeFailure(isLostConnection: true), .keepWaiting) }
        XCTAssertEqual(unconfirmed.observeFailure(isLostConnection: true), .serverGone)

        var confirmed = ServerUpgradeWatch(previousVersion: "1.2.3")
        _ = confirmed.observe(ServerUpgrade(current: "1.2.3", isUpgrading: true))
        XCTAssertEqual(confirmed.observeFailure(isLostConnection: true), .keepWaiting)
        XCTAssertEqual(confirmed.observeFailure(isLostConnection: true), .keepWaiting)
        XCTAssertEqual(confirmed.observeFailure(isLostConnection: false), .keepWaiting, "an answer breaks the run")
        XCTAssertEqual(confirmed.observeFailure(isLostConnection: true), .keepWaiting)
        XCTAssertEqual(confirmed.observeFailure(isLostConnection: true), .keepWaiting)
        XCTAssertEqual(confirmed.observeFailure(isLostConnection: true), .serverGone)
    }

    func testOnlyAConnectionThatGotNoAnswerIsALostOne() {
        XCTAssertTrue(ServerConnectionFailure.isLostConnection(URLError(.networkConnectionLost)))
        XCTAssertTrue(ServerConnectionFailure.isLostConnection(TeaNodeError.unreadableAnswer), "a gateway's page stands in for a server that is not there")
        XCTAssertFalse(ServerConnectionFailure.isLostConnection(TeaNodeError.signedOut))
        XCTAssertFalse(ServerConnectionFailure.isLostConnection(TeaNodeError.refused("not allowed")))
    }

    // MARK: Setup

    func testReadsTheSetupAndSendsWhatMailClientsAreTold() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"GetServerAddresses":{"ipv4":"192.0.2.10","ipv6":null,"error":null},
                  "GetOutgoingIdentity":{"address":"192.0.2.10","via":"direct","reverseName":"mail.example.com","forwardAddresses":["192.0.2.11"],
                    "confirmed":false,"helloName":"mail.example.com","helloAddresses":["192.0.2.10"],"helloMatches":true,"unknown":null},
                  "GetSettings":{"submission":{"host":null,"port":null,"effectiveHost":"mail.example.com","effectivePort":"587"},
                    "imap":{"host":"","port":0,"tlsPort":10993,"effectiveHost":"mail.example.com","effectivePort":"","effectiveTlsPort":"10993"}}}}
                """#)
        let setup = try await connection.serverSetup()
        XCTAssertTrue(setup.addresses?.isKnown ?? false)
        XCTAssertEqual(setup.outgoingIdentity?.isConfirmed, false)
        XCTAssertEqual(setup.outgoingIdentity?.doesHelloMatch, true)
        XCTAssertEqual(setup.submission?.effectivePort, "587")
        XCTAssertTrue(setup.mailProgram?.isListening ?? false)

        let mailProgramDraft = MailProgramAccessDraft(setup.mailProgram!)
        XCTAssertEqual(mailProgramDraft.portText, "", "a port of zero follows the server")
        XCTAssertEqual(mailProgramDraft.tlsPortText, "10993")
        try await connection.updateMailProgramAccess(mailProgramDraft)
        XCTAssertEqual(
            try sentVariables(fake.sent[1]),
            ["imap": .object(["host": .string(""), "port": .number(0), "tlsPort": .number(10993)])])

        var submissionDraft = SubmissionAccessDraft(setup.submission!)
        submissionDraft.portText = "10587"
        try await connection.updateSubmissionAccess(submissionDraft)
        XCTAssertEqual(try sentVariables(fake.sent[2]), ["submission": .object(["host": .string(""), "port": .number(10587)])])
    }

    // MARK: The server's settings

    static let serverSettingsAnswer = #"""
        {"s3":{"enabled":false,"bucket":"","region":"us-east-1","endpoint":null,"pathStyle":false,"accessKeyId":null,"hasSecretAccessKey":false,"credentialsFile":"/etc/aws/credentials"},
         "route53":{"enabled":false,"zoneId":"","region":"","accessKeyId":"AKEXAMPLE","hasSecretAccessKey":true,"credentialsFile":null},
         "antivirus":{"enabled":true,"host":"clamav","port":3310},
         "antispam":{"enabled":true,"engine":"","effectiveEngine":"builtin","host":"","port":783,"signalsEnabled":true,"dnsEnabled":true,
           "bayesEnabled":true,"rulesEnabled":false,"bayesMinimumMessages":200,"bayesLearnedSpam":30,"bayesLearnedHam":20},
         "relay":{"enabled":true,"host":"smtp.example.net","port":0,"security":"","username":"robot","hasPassword":true},
         "proxy":{"socks5":null},
         "certificates":{"perDomain":true,"hosts":["mail.example.com"],"acmeEnabled":true,"acmeEmail":"postmaster@example.com",
           "acmeDirectoryUrl":null,"acmeChallenge":"","certificateFile":null,"privateKeyFile":null},
         "smtp":{"maxMessageSize":"50MB","maxRecipientsIncoming":50,"maxRecipientsOutgoing":100,"greylistDelay":"5m","authRateLimit":10,
           "authRateBurst":5,"trustedSenders":["10.0.0.0/8"]},
         "resolver":{"nameserver":"","checkInterval":"6h","externalAddressServices":null},
         "session":{"lifetime":"720h"},
         "passkey":{"enabled":true,"relyingPartyId":"mail.example.com","displayName":"Example Mail","origins":null,"maximumPerUser":0},
         "listen":{"smtpIncoming":":25","smtpOutgoing":":587","imap":"","imaps":":993","http":":80","https":":443","debug":null},
         "sso":{"providers":[{"id":"provider-one","name":"Provider One","issuer":"https://login.example.net/","clientId":"client-1",
           "hasClientSecret":true,"groupsClaim":null,"createUsers":false}]},
         "identity":{"name":"mail.example.com","mailServers":null,"externalAddresses":null,"logLevel":"notice","dataDirectory":"/var/lib/example"},
         "storage":{"directory":"mail","spoolRetention":"30d"},
         "geoip":{"enabled":false,"databaseFile":null}}
        """#

    private func serverSettings() throws -> ServerSettings {
        try JSONDecoder().decode(ServerSettings.self, from: Data(Self.serverSettingsAnswer.utf8))
    }

    func testReadsTheServersSettingsAndSavesOneSectionAtATime() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"GetSettings":"# + Self.serverSettingsAnswer + "}}")
        let settings = try await connection.serverSettings()
        XCTAssertEqual(settings.antispam.bayesLearnedCount, 50)
        XCTAssertTrue(settings.route53.hasSecretAccessKey)
        XCTAssertEqual(settings.sso.providers.first?.hasClientSecret, true)

        let (saving, saved) = self.connection(answering: #"{"data":{"UpdateSettings":"# + Self.serverSettingsAnswer + "}}")
        _ = try await saving.updateServerSettings(SessionDraft(settings.session).variables)
        XCTAssertEqual(try sentVariables(saved.sent[0]), ["session": .object(["lifetime": .string("720h")])])
        XCTAssertTrue(try sentDocument(saved.sent[0]).contains("UpdateSettings("))
        XCTAssertTrue(try sentDocument(fake.sent[0]).contains("geoip { enabled databaseFile }"))
    }

    func testASecretIsKeptReplacedOrClearedButNeverSentBack() throws {
        let settings = try serverSettings()
        var relay = RelayDraft(settings.relay)
        XCTAssertEqual(relay.portText, "587", "a relay without a port starts at the submission port")
        XCTAssertEqual(relay.security, "starttls")
        XCTAssertNil(relay.variables["relay"]?["password"], "an empty box keeps the stored password")
        relay.password = "a-new-password"
        XCTAssertEqual(relay.variables["relay"]?["password"], .string("a-new-password"))
        relay.isClearingPassword = true
        XCTAssertEqual(relay.variables["relay"]?["password"], .string(""), "clearing sends it empty")

        var store = ObjectStoreDraft(settings.s3)
        XCTAssertNil(store.variables["s3"]?["secretAccessKey"])
        store.isClearingSecret = true
        XCTAssertEqual(store.variables["s3"]?["secretAccessKey"], .string(""))
    }

    func testARelayPresetFillsInWhatTheProviderDocuments() throws {
        var relay = RelayDraft(try serverSettings().relay)
        relay.apply(RelayPreset.all.first { $0.label == "Resend" }!)
        XCTAssertEqual(relay.host, "smtp.resend.com")
        XCTAssertEqual(relay.portText, "465")
        XCTAssertEqual(relay.security, "tls")
        XCTAssertEqual(relay.username, "resend")
        relay.apply(RelayPreset.all.first { $0.label == "Gmail" }!)
        XCTAssertEqual(relay.username, "resend", "a provider without a fixed username leaves it alone")
    }

    func testTheFormsSendWhatTheDashboardSends() throws {
        let settings = try serverSettings()
        var smtp = SMTPLimitsDraft(settings.smtp)
        smtp.trustedSendersText = "10.0.0.0/8, , 192.168.1.5 "
        smtp.maxRecipientsIncomingText = "not a number"
        XCTAssertEqual(smtp.variables["smtp"]?["trustedSenders"], .array([.string("10.0.0.0/8"), .string("192.168.1.5")]))
        XCTAssertEqual(smtp.variables["smtp"]?["maxRecipientsIncoming"], .number(0))

        let identity = IdentityDraft(settings.identity)
        XCTAssertEqual(identity.logLevel, "NOTICE", "upper case, as the configuration stores it")
        XCTAssertEqual(identity.variables["identity"]?["mailServers"], .array([]))
        XCTAssertNil(identity.variables["identity"]?["dataDirectory"], "the data directory is shown and never sent")

        var proxy = ProxyDraft(settings.proxy)
        proxy.socks5 = " 127.0.0.1:1080 "
        XCTAssertEqual(proxy.variables, ["proxy": .object(["socks5": .string("127.0.0.1:1080")])])

        XCTAssertEqual(CertificatesDraft(settings.certificates).acmeChallenge, "http-01")
        XCTAssertEqual(AntispamDraft(settings.antispam).engine, "builtin", "an empty engine shows what the server resolved it to")

        let listen = ListenDraft(settings.listen)
        XCTAssertEqual(listen.variables["listen"]?["debug"], .string(""))
    }

    func testIdentityProvidersSendABlankSecretAsNothing() throws {
        var sso = SSODraft(try serverSettings().sso)
        XCTAssertTrue(sso.canSave)
        let provider = sso.variables["sso"]?["providers"]
        guard case .array(let providers) = provider else { return XCTFail("no providers") }
        XCTAssertEqual(providers.first?["clientSecret"], .null, "blank keeps the secret stored for this id")
        XCTAssertEqual(providers.first?["groupsClaim"], .null)
        XCTAssertEqual(sso.providers[0].redirectAddress(serverOrigin: "https://mail.example.com"), "https://mail.example.com/api/v1/sso/provider-one/callback")
        sso.providers.append(SSOProviderDraft())
        XCTAssertFalse(sso.canSave, "a provider without an id, a name, an issuer and a client id is not saved")
    }

    func testListsAndPricesAreReadAsTyped() {
        XCTAssertEqual(SettingsList.items("a, b,,c "), ["a", "b", "c"])
        XCTAssertEqual(SettingsList.lineItems("a\nb, c"), ["a", "b", "c"])
        XCTAssertEqual(SettingsList.price("1,50"), "1.50")
        XCTAssertEqual(SettingsList.price("1.2.3x"), "1.23")
        XCTAssertEqual(SettingsList.digits("10a5"), "105")
        XCTAssertEqual(SettingsList.text(0), "")
        XCTAssertEqual(SettingsList.text(2.5), "2.5")
        XCTAssertEqual(SettingsList.text(3), "3")
    }

    // MARK: The operator's agent settings

    static let agentSettingsAnswer = #"""
        {"enabled":true,"instructions":"Be brief.","allowPrivateAddresses":["192.168.1.10"],"skipCertificateCheck":[],
         "providers":[
           {"name":"main","kind":"openai","baseUrl":"","hasApiKey":true,"hasRefreshToken":false,"account":"","enabled":true,"allow":[],"deny":[],
            "pricingInput":1.25,"pricingOutput":10,"pricingCacheRead":0,"pricingCacheWrite":0,
            "modelPricing":[{"model":"model-large*","input":2,"output":8,"cacheRead":0.5,"cacheWrite":0}],"planUsage":null},
           {"name":"decider","kind":"typesafe","baseUrl":"","hasApiKey":true,"hasRefreshToken":false,"account":"","enabled":true,"allow":[],"deny":[],
            "pricingInput":0,"pricingOutput":0,"pricingCacheRead":0,"pricingCacheWrite":0,"modelPricing":[],
            "planUsage":{"planName":"plus","observedAt":"2030-01-02T03:04:05Z","windows":[{"usedPercent":140,"windowMinutes":300,"resetsAt":"2030-01-02T05:00:00Z"}]}}
         ],
         "models":{"default":"main:model-large","fast":"main:model-small","quick":"main:model-small","embedding":"","triage":"main:model-small","research":"","summarize":"",
           "reply":"","ask":"","schedule":"","compact":"","scan":"","synthesize":"","decide":"decider:judge","categorize":"","embeddingDimensions":0,
           "choices":["main:model-large"]},
         "features":{"triage":true,"summaries":true,"draftReplies":true,"search":true,"research":true,"autoReply":false,"ask":true,"schedules":true,
           "browser":false,"connectedServers":true,"computer":true,"chatApps":true,"skills":true,"subagents":true,"remember":true,"knowledge":true,"dreaming":true},
         "limits":{"maxBodyCharacters":20000,"dailyTokensPerAgent":1000000,"monthlyTokensPerServer":0,"dailyCostPerAgent":2.5,"monthlyCostPerServer":0,
           "maxRoundsPerAsk":20,"maxRoundsPerResearch":10,"maxRoundsPerReply":5,"maxRoundsPerDream":30,"maxToolCallsPerRun":50,"requestTimeout":"2m",
           "concurrency":4,"scanConcurrency":2,"rewriteConcurrency":2,"dreamShare":0.3,"ingestChunksPerRun":500,"embeddingTokensPerDay":0,
           "maxRoundsPerSubagent":0,"goalTurnsPerDay":0,"goalTurnsAlone":0,"goalsInProgress":0,"backgroundWakesAlone":0},
         "retention":{"runs":"720h","corrections":"2160h"},"currency":"USD","search":{"kind":"brave","hasApiKey":true},
         "tools":{"disabled":["shell"],"confirm":["mail"],"catalog":[
           {"name":"mail_send","family":"mail","risk":"outward","description":"Sends mail.","confirms":true,"core":true,"actions":null},
           {"name":"shell","family":"computer","risk":"local","description":"Runs a command.","confirms":true,"core":false,"actions":["run","stop"]}]},
         "voice":{"enabled":false,"provider":"","transcriptionModel":"","silenceMS":0,"speechModel":"","speechVoice":"","askModel":""},
         "browser":{"enabled":false,"cdpEndpoint":"","attachTabs":true,"allowPrivateAddresses":[],"idleTimeout":"5m","maxContexts":4},
         "mcpServers":[{"name":"tracker","transport":"","effectiveTransport":"http","url":"https://tracker.example.com/mcp","command":"","args":[],
           "envNames":["API_TOKEN"],"workingDir":"","auth":"static","effectiveAuth":"static","hasAuthorization":true,"oauthClientId":"",
           "hasOauthClientSecret":false,"oauthScopes":[],"oauthAuthorizationUrl":"","oauthTokenUrl":"","oauthRedirect":"server","headless":true,
           "location":"server","readOnly":["list_items"],"disabled":[],"timeout":"30s","enabled":true}],
         "finance":{"offeredProviders":["simplefin"],"plaid":{"environment":"","clientId":"","hasSecret":false,"countryCodes":[],"products":[]}},
         "works":["triage"],"families":["mail","computer"],"kinds":["openai","typesafe","openai-codex"]}
        """#

    private func agentSettings() throws -> AgentServerSettings {
        try JSONDecoder().decode(AgentServerSettings.self, from: Data(Self.agentSettingsAnswer.utf8))
    }

    func testReadsTheOperatorsAgentSettingsAndSavesASection() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"UpdateSettings":{"agent":"# + Self.agentSettingsAnswer + "}}}")
        let settings = try agentSettings()
        XCTAssertEqual(settings.providers.count, 2)
        XCTAssertEqual(settings.tools.catalog.map(\.name), ["mail_send", "shell"])
        XCTAssertEqual(settings.features["autoReply"], false)
        XCTAssertEqual(settings.mcpServers.first?.isHeadless, true)

        var general = AgentGeneralDraft(settings)
        general.skipCertificateCheckText = "camera.example.com\n192.0.2.20"
        let saved = try await connection.updateAgentServerSettings(general.agentParameters)
        XCTAssertEqual(saved.instructions, "Be brief.")
        let sent = try sentVariables(fake.sent[0])
        XCTAssertEqual(sent["agent"]?["skipCertificateCheck"], .array([.string("camera.example.com"), .string("192.0.2.20")]))
    }

    func testARenamedProviderTakesItsModelsWithIt() throws {
        let settings = try agentSettings()
        var draft = AgentProviderDraft(settings.providers[0])
        XCTAssertEqual(draft.pricingInputText, "1.25")
        XCTAssertEqual(draft.modelPrices.first?.cacheReadText, "0.5")
        draft.name = "primary"
        draft.apiKey = ""
        let section = draft.agentParameters(replacingProviderAt: 0, in: settings)
        guard case .array(let providers) = section["providers"] else { return XCTFail("no providers") }
        XCTAssertEqual(providers.count, 2, "the list is sent whole")
        XCTAssertEqual(providers[0]["name"], .string("primary"))
        XCTAssertEqual(providers[0]["previousName"], .string("main"))
        XCTAssertEqual(providers[0]["apiKey"], .string(""), "a blank key keeps the stored one")
        XCTAssertEqual(providers[1]["name"], .string("decider"))
        XCTAssertNil(providers[1]["previousName"])
        XCTAssertEqual(section["models"]?["default"], .string("primary:model-large"))
        XCTAssertEqual(section["models"]?["decide"], .string("decider:judge"))
        XCTAssertEqual(section["models"]?["choices"], .array([.string("primary:model-large")]))

        var unrenamed = AgentProviderDraft(settings.providers[0])
        unrenamed.isEnabled = false
        XCTAssertNil(unrenamed.agentParameters(replacingProviderAt: 0, in: settings)["models"], "the models are sent only for a rename")

        let removed = AgentProviderDraft.agentParameters(removingProviderAt: 0, in: settings)
        guard case .array(let kept) = removed["providers"] else { return XCTFail("no providers") }
        XCTAssertEqual(kept.map { $0["name"] }, [.string("decider")])
    }

    func testAProviderOnAPlanIsSavedOnlyOnceSignedIn() {
        var draft = AgentProviderDraft()
        draft.name = "plan"
        draft.providerKind = AgentProviderSettings.signedInKind
        XCTAssertFalse(draft.canSave)
        draft.hasRefreshToken = true
        XCTAssertTrue(draft.canSave)
    }

    func testTheModelsAndTheFeaturesAreSentWhole() throws {
        let settings = try agentSettings()
        var models = settings.models
        models[fieldName: "research"] = "main:model-large"
        XCTAssertEqual(models.research, "main:model-large")
        XCTAssertEqual(models[fieldName: "not-a-field"], "")
        XCTAssertEqual(models.quick, "main:model-small")
        models[fieldName: "quick"] = ""
        XCTAssertEqual(models.agentParameters["models"]?["quick"], .string(""))
        XCTAssertEqual(models.agentParameters["models"]?["research"], .string("main:model-large"))
        XCTAssertEqual(models.agentParameters["models"]?["embeddingDimensions"], .number(0))
        XCTAssertTrue(AgentFeatureSwitches.isOn("newFeature", in: settings.features), "a feature the server did not say is on")
        XCTAssertFalse(AgentFeatureSwitches.isOn("autoReply", in: settings.features))
        XCTAssertEqual(AgentFeatureSwitches.agentParameters(["triage": false]), ["features": .object(["triage": .bool(false)])])
    }

    func testADecidingProvidersModelsAreOfferedOnlyForDeciding() throws {
        let settings = try agentSettings()
        let choices = AgentModelChoices(knownModels: ["main:model-large", "decider:judge"], providers: settings.providers)
        XCTAssertEqual(choices.forWork, ["main:model-large"])
        XCTAssertEqual(choices.forDeciding, ["decider:judge"])
        XCTAssertEqual(AgentModelChoices.listed(["a:one"], chosen: "b:two"), ["b:two", "a:one"])
    }

    func testThePlanWindowsSayWhatIsLeft() throws {
        let window = try agentSettings().providers[1].planUsage!.windows[0]
        let before = ServerTime.date("2030-01-02T04:00:00Z")!
        XCTAssertEqual(window.shownUsedPercent(now: before), 100, "clamped to the whole allowance")
        XCTAssertEqual(window.shownUsedPercent(now: before.addingTimeInterval(7200)), 0, "a window whose reset has passed is whole again")
    }

    func testTheToolPolicyIsOneWordPerName() throws {
        var policy = AgentToolPolicyDraft(try agentSettings())
        XCTAssertEqual(policy.names, ["mail", "computer", "mail_send", "shell"])
        XCTAssertEqual(policy.word("mail"), "confirm")
        XCTAssertEqual(policy.word("shell"), "off")
        policy.wordByName["computer"] = "off"
        policy.wordByName["shell"] = "allow"
        XCTAssertEqual(
            policy.agentParameters,
            ["tools": .object(["disabled": .array([.string("computer")]), "confirm": .array([.string("mail")])])])
    }

    func testTheLimitsAreSentAsNumbersAndTheTimeoutAsADuration() throws {
        var limits = AgentLimitsDraft(try agentSettings())
        XCTAssertEqual(limits.limitTexts["dreamShare"], "0.3")
        XCTAssertEqual(limits.limitTexts["monthlyTokensPerServer"], "0")
        limits.limitTexts["concurrency"] = "8"
        limits.currency = "EUR"
        let section = limits.agentParameters
        XCTAssertEqual(section["limits"]?["concurrency"], .number(8))
        XCTAssertEqual(section["limits"]?["requestTimeout"], .string("2m"))
        XCTAssertEqual(section["currency"], .string("EUR"))
        XCTAssertEqual(section["retention"]?["runs"], .string("720h"))
    }

    func testAKeyIsSentOnlyWhenOneIsTyped() throws {
        let settings = try agentSettings()
        var search = AgentWebSearchDraft(settings.search)
        XCTAssertEqual(search.agentParameters, ["search": .object(["kind": .string("brave")])])
        search.apiKey = "a-search-key"
        XCTAssertEqual(search.agentParameters["search"]?["apiKey"], .string("a-search-key"))

        var finance = AgentFinanceDraft(settings.finance)
        finance.setOffered("plaid", isOffered: true)
        finance.setProduct("liabilities", isOn: true)
        finance.countryCodesText = "us, gb"
        let section = finance.agentParameters
        XCTAssertEqual(section["finance"]?["offeredProviders"], .array([.string("plaid"), .string("simplefin")]), "in the server's order")
        XCTAssertEqual(section["finance"]?["plaid"]?["products"], .array([.string("transactions"), .string("liabilities")]))
        XCTAssertEqual(section["finance"]?["plaid"]?["countryCodes"], .array([.string("US"), .string("GB")]))
        XCTAssertNil(section["finance"]?["plaid"]?["secret"])
        XCTAssertTrue(finance.hasProduct("transactions"), "transactions is always on")
    }

    func testAConnectedServerKeepsItsSecretsAndCarriesItsPreviousName() throws {
        let settings = try agentSettings()
        var draft = ConnectedServerDraft(settings.mcpServers[0])
        XCTAssertEqual(draft.envText, "API_TOKEN=")
        XCTAssertFalse(draft.isStdio)
        XCTAssertTrue(draft.showsAuthorization)
        draft.name = "issues"
        draft.argsText = "--port  8080"
        let section = draft.agentParameters(replacingServerAt: 0, in: settings)
        guard case .array(let servers) = section["mcpServers"] else { return XCTFail("no servers") }
        XCTAssertEqual(servers[0]["previousName"], .string("tracker"))
        XCTAssertEqual(servers[0]["authorization"], .string(""))
        XCTAssertEqual(servers[0]["args"], .array([.string("--port"), .string("8080")]))

        var added = ConnectedServerDraft()
        added.name = "local"
        added.command = "example-server"
        XCTAssertTrue(added.isStdio, "a command and no address is a command")
        added.isHeadless = true
        added.setLocation("computer")
        XCTAssertFalse(added.isHeadless, "a server on the person's computer cannot run with nobody there")
        let appended = added.agentParameters(replacingServerAt: nil, in: settings)
        guard case .array(let both) = appended["mcpServers"] else { return XCTFail("no servers") }
        XCTAssertEqual(both.count, 2)
        XCTAssertEqual(both[0]["env"], .array([.string("API_TOKEN=")]), "a stored server keeps its environment by name")
        XCTAssertEqual(both[0]["oauthRedirect"], .string(""))
    }

    func testAskingAProviderAndSigningInToAPlan() async throws {
        let (testing, tested) = connection(answering: #"{"data":{"TestAgentProvider":[{"name":"main:model-large","contextLength":200000},null]}}"#)
        let models = try await testing.testAgentProvider(name: "main")
        XCTAssertEqual(models, ["main:model-large"])
        XCTAssertEqual(try sentVariables(tested.sent[0]), ["name": .string("main")])

        let (signing, signed) = connection(
            answering: #"""
                {"data":{"BeginAgentProviderSignIn":{"signInId":"sign-in-1","userCode":"ABCD-1234","verificationAddress":"https://example.com/device",
                  "isSignedIn":false,"account":"","plan":""}}}
                """#)
        let signIn = try await signing.beginProviderSignIn(provider: " plan ")
        XCTAssertEqual(signIn.userCode, "ABCD-1234")
        XCTAssertEqual(try sentVariables(signed.sent[0]), ["provider": .string("plan")])
    }

    // MARK: Everybody's agents

    func testReadsEveryAgentTheUsageAndTheJobsGivenUpOn() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{
                  "ListAgents":[{"agentId":"agent-1","userId":"user-1","username":"person-one","name":"Helper","enabled":true,"operatorDisabledAt":"2030-01-01T00:00:00Z",
                    "dailyTokens":0,"dailyCost":1.5,"dead":1,"queued":2,"lastRunAt":null,
                    "sources":[{"mailboxId":"mailbox-1","name":"Inbox","policy":{"granted":true}},{"mailboxId":"mailbox-2","name":"Shared","policy":null}],
                    "today":{"used":1200,"limit":0,"resetsAt":"2030-01-02T00:00:00Z","cost":0.4,"costLimit":1.5,"currency":"USD"},
                    "totals":{"promptTokens":10,"completionTokens":20,"cacheReadTokens":30,"cacheWriteTokens":40,"calls":3}}],
                  "AgentServerUsage":[{"key":"2030-01-01","cost":0.25,"currency":"USD","totals":{"promptTokens":100,"completionTokens":50,"cacheReadTokens":0,"cacheWriteTokens":0,"calls":2}}],
                  "ListAgentDeadLetters":[{"id":"job-1","agentId":"agent-1","mailboxId":null,"kind":"remember","subjectId":"conversation-1","attempts":3,"error":"timed out","finishedAt":"2030-01-01T01:00:00Z"}]
                }}
                """#)
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let firstDay = calendar.date(from: DateComponents(year: 2030, month: 1, day: 1))!
        let range = AgentUsageRange(firstDay: firstDay, lastDay: firstDay)
        XCTAssertEqual(range.bounds(calendar: calendar).until, "2030-01-02T00:00:00Z", "the last day is counted through its end")
        let overview = try await connection.agentAdminOverview(grouping: .model, range: range)
        let agent = overview.agents[0]
        XCTAssertTrue(agent.isSwitchedOff)
        XCTAssertEqual(agent.grantedMailboxCount, 1)
        XCTAssertTrue(agent.isBoundByCost)
        XCTAssertEqual(agent.deadJobCount, 1)
        XCTAssertEqual(agent.totals.totalTokenCount, 100)
        XCTAssertEqual(overview.usageRows[0].totals.callCount, 2)
        XCTAssertEqual(overview.deadJobs[0].jobKind, "remember")
        XCTAssertEqual(overview.username(agentId: "agent-1"), "person-one")
        XCTAssertEqual(overview.username(agentId: "agent-9"), "agent-9")
        XCTAssertEqual(try sentVariables(fake.sent[0])["by"], .string("model"))

        let limit = AgentLimitDraft(agent)
        XCTAssertEqual(limit.tokensText, "", "no token limit of its own shows the server's default")
        XCTAssertEqual(limit.costText, "1.5")
    }

    func testSetsALimitAndTheSwitch() async throws {
        let summary = #"""
            {"agentId":"agent-1","userId":"user-1","username":"person-one","name":"","enabled":true,"operatorDisabledAt":null,"dailyTokens":5000,
             "dailyCost":0,"dead":0,"queued":0,"lastRunAt":null,"sources":[],"today":null,
             "totals":{"promptTokens":0,"completionTokens":0,"cacheReadTokens":0,"cacheWriteTokens":0,"calls":0}}
            """#
        let (limiting, limited) = connection(answering: #"{"data":{"SetAgentLimit":"# + summary + "}}")
        var draft = AgentLimitDraft(try JSONDecoder().decode(AgentAdminSummary.self, from: Data(summary.utf8)))
        draft.costText = SettingsList.price("2,50")
        try await limiting.setAgentLimit(agentId: "agent-1", draft: draft)
        XCTAssertEqual(
            try sentVariables(limited.sent[0]), ["agentId": .string("agent-1"), "dailyTokens": .number(5000), "dailyCost": .number(2.5)])

        let (switching, switched) = connection(answering: #"{"data":{"SetAgentDisabled":"# + summary + "}}")
        try await switching.setAgentSwitchedOff(agentId: "agent-1", isSwitchedOff: true)
        XCTAssertEqual(try sentVariables(switched.sent[0]), ["agentId": .string("agent-1"), "disabled": .bool(true)])
    }

    func testEveryRunIsFilteredOnTheServer() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ListAllAgentRuns":{"total":1,"runs":[{"id":"run-1","agentId":"agent-1","title":"Sorted a message","jobKind":"triage",
                  "lastAt":"2030-01-01T00:00:00Z","usage":{"promptTokens":100,"cacheReadTokens":50,"completionTokens":10,"cost":0.01}}]}}}
                """#)
        let page = try await connection.serverAgentRuns(filter: ServerAgentRunFilter(agentId: "agent-1", jobKind: "triage", query: "  sorted "), offset: 50)
        XCTAssertEqual(page.runs[0].tokenCount, 160)
        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            [
                "first": .number(50), "offset": .number(50), "agentId": .string("agent-1"), "jobId": .null,
                "kinds": .array([.string("triage")]), "query": .string("sorted"),
            ])
    }

    func testAJobGivenUpOnOpensItsLatestRunOrWhatItRemembered() async throws {
        let job = try JSONDecoder().decode(
            AgentDeadJob.self,
            from: Data(#"{"id":"job-1","agentId":"agent-1","kind":"remember","subjectId":"conversation-1","attempts":3}"#.utf8))
        let (withRun, _) = connection(
            answering: #"{"data":{"ListAllAgentRuns":{"total":1,"runs":[{"id":"run-9","agentId":"agent-1","title":"","jobKind":"remember","lastAt":"2030-01-01T00:00:00Z","usage":{"promptTokens":0,"completionTokens":0,"cost":0}}]}}}"#)
        let opened = try await withRun.deadJobConversationId(job)
        XCTAssertEqual(opened, "run-9")
        let (withoutRun, asked) = connection(answering: #"{"data":{"ListAllAgentRuns":{"total":0,"runs":[]}}}"#)
        let remembered = try await withoutRun.deadJobConversationId(job)
        XCTAssertEqual(remembered, "conversation-1")
        XCTAssertEqual(try sentVariables(asked.sent[0])["jobId"], .string("job-1"))
    }

    // MARK: Skills and source types

    func testReadsTheSkillsAndWhichKeysAreFilledIn() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"ListAgentSkills":[{"name":"cameras","description":"Looks at cameras.","version":"1.0.0","publisher":"example","enabled":false,
                  "readable":true,"problem":null,"scope":"","secrets":["api_key","host"],"personalSecrets":[],
                  "tools":[{"name":"camera_snapshot","description":"A picture.","kind":"http","needsComputer":false},
                           {"name":"camera_clip","description":"A clip.","kind":"command","needsComputer":true}]}]}}
                """#)
        let skills = try await connection.installedSkills()
        XCTAssertTrue(skills[0].doesRunCommands)
        XCTAssertTrue(skills[0].hasSecrets)
        XCTAssertEqual(skills[0].filledSecretCount(["cameras/api_key", "other/host"]), 1)

        let (reading, _) = self.connection(
            answering: #"{"data":{"GetSettings":{"agent":{"skillSecrets":[{"skill":"cameras","key":"api_key","hasValue":true},{"skill":"cameras","key":"host","hasValue":false}]}}}}"#)
        let filled = try await reading.filledSkillSecrets()
        XCTAssertEqual(filled, ["cameras/api_key"])
    }

    func testOnlyTheKeysTypedAreSent() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"UpdateSettings":{"agent":{"skillSecrets":[]}}}}"#)
        var draft = SkillSecretsDraft(skillName: "cameras")
        draft.valueByKey["host"] = "camera.example.com"
        XCTAssertEqual(draft.typedKeys(["api_key", "host"]), ["host"])
        try await connection.saveSkillSecrets(draft, keys: ["api_key", "host"])
        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            ["agent": .object(["skillSecrets": .array([.object(["skill": .string("cameras"), "key": .string("host"), "value": .string("camera.example.com")])])])])
    }

    func testTheRegistryIsFilteredAndSaysWhenSomethingIsBehind() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"SearchAgentSkills":[{"name":"weather","description":"Forecasts.","version":"2.0.0","tags":["outdoors"],"installed":"1.0.0","newer":true},
                  {"name":"dictionary","description":"Words.","version":"1.0.0","tags":[],"installed":null,"newer":false}]}}
                """#)
        let offers = try await connection.skillRegistry()
        XCTAssertTrue(offers[0].isInstalled)
        XCTAssertFalse(offers[0].isCurrent)
        XCTAssertFalse(offers[1].isInstalled)
        XCTAssertTrue(offers[0].matches("OUTDOORS"))
        XCTAssertFalse(offers[1].matches("outdoors"))
        XCTAssertTrue(RegistryOffer.isBehind(installedCount: 1, offers: offers))
        XCTAssertFalse(RegistryOffer.isBehind(installedCount: 0, offers: offers))
    }

    func testReadsTheSourceTypesAndRemovesOne() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"ListAgentSourceTypes":[{"name":"notes-folder","description":"A folder of notes.","version":"","publisher":"","url":"",
                  "isLocal":true,"installedAt":"2030-01-01T00:00:00Z","reader":"files","runs":["computer"],"requires":[],"guide":"  ",
                  "readable":true,"problem":null,"settings":[],"secrets":[]}]}}
                """#)
        let sourceTypes = try await connection.installedSourceTypes()
        XCTAssertEqual(sourceTypes[0].reader, "files")
        XCTAssertTrue(sourceTypes[0].isLocal)
        XCTAssertFalse(sourceTypes[0].hasGuide)

        let (removing, removed) = self.connection(answering: #"{"data":{"RemoveAgentSourceType":true}}"#)
        try await removing.removeSourceType(name: "notes-folder")
        XCTAssertEqual(try sentVariables(removed.sent[0]), ["name": .string("notes-folder")])
    }
}

extension ManageServerTests {
    func testRecordingSizesRemainByteSizeStringsAndTranscriptionHasNoDefault() throws {
        let settings = try JSONDecoder().decode(AgentServerSettings.self, from: Data(Self.agentSettingsAnswer.utf8))
        var models = settings.models
        models.transcription = "audio-service:speaker-model"
        var draft = AgentLimitsDraft(settings)
        draft.limitTexts["maxRecordingMediaBytes"] = "3 GB"
        draft.limitTexts["maxRecordingAccountBytes"] = "60 GB"
        let limits = draft.agentParameters["limits"]
        XCTAssertEqual(limits?["maxRecordingMediaBytes"], .string("3 GB"))
        XCTAssertEqual(limits?["maxRecordingAccountBytes"], .string("60 GB"))
        XCTAssertEqual(models[fieldName: "transcription"], "audio-service:speaker-model")
        XCTAssertEqual(models.renaming(provider: "audio-service", to: "new-audio")[fieldName: "transcription"], "new-audio:speaker-model")
        XCTAssertEqual(models.agentParameters["models"]?["transcription"], .string("audio-service:speaker-model"))
        XCTAssertTrue(AgentModelAssignments.workFieldNames.contains("transcription"))
    }
}
