package com.teanode.app.settings.manage

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.DemoResponse
import com.teanode.app.demo.graphQLField
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull

/** The operator's agent settings as the invented server stores them. */
private const val AGENT_SETTINGS = """{
  "enabled":true,"instructions":"Never answer a parent automatically.","allowPrivateAddresses":["192.0.2.10"],"skipCertificateCheck":[],
  "providers":[
    {"name":"main","kind":"openai","baseUrl":"","hasApiKey":true,"hasBillingApiKey":true,"hasRefreshToken":false,"account":"","enabled":true,"allow":[],"deny":["legacy-*"],
     "pricingInput":1.25,"pricingOutput":10,"pricingCacheRead":0.125,"pricingCacheWrite":0,"pricingAudioInput":6,
     "modelPricing":[{"model":"model-large*","input":2,"output":8,"cacheRead":0.5,"cacheWrite":0,"audioInput":4.5}],"planUsage":null},
    {"name":"plan","kind":"openai-codex","baseUrl":"","hasApiKey":false,"hasRefreshToken":true,"account":"robin@example.com","enabled":true,"allow":[],"deny":[],
     "pricingInput":0,"pricingOutput":0,"pricingCacheRead":0,"pricingCacheWrite":0,"pricingAudioInput":0,"modelPricing":[],
     "planUsage":{"planName":"plus","observedAt":"2999-01-02T03:04:05Z","windows":[{"usedPercent":42,"windowMinutes":300,"resetsAt":"2999-01-02T05:00:00Z"},{"usedPercent":91,"windowMinutes":10080,"resetsAt":"2999-01-05T05:00:00Z"}]}},
    {"name":"local","kind":"openai","baseUrl":"http://192.0.2.30:8000/v1","hasApiKey":false,"hasRefreshToken":false,"account":"","enabled":false,"allow":["small-*"],"deny":[],
     "pricingInput":0,"pricingOutput":0,"pricingCacheRead":0,"pricingCacheWrite":0,"pricingAudioInput":0,"modelPricing":[],"planUsage":null}
  ],
  "models":{"default":"main:model-large","fast":"main:model-small","quick":"main:model-small","embedding":"main:embedding-small","triage":"","research":"main:model-large","summarize":"",
    "reply":"","ask":"","schedule":"","compact":"","scan":"","synthesize":"","decide":"","categorize":"","transcription":"main:speaker-model","embeddingDimensions":1536,"choices":["main:model-large","main:model-small"]},
  "features":{"triage":true,"summaries":true,"draftReplies":true,"search":true,"research":true,"autoReply":false,"ask":true,"schedules":true,"browser":false,"connectedServers":true,"computer":true,
    "chatApps":true,"skills":true,"subagents":true,"remember":true,"knowledge":true,"dreaming":true},
  "limits":{"dailyTokensPerAgent":1000000,"monthlyTokensPerServer":0,"dailyCostPerAgent":2.5,"monthlyCostPerServer":0},
  "retention":{"runs":"720h","corrections":"2160h"},"currency":"USD","search":{"kind":"brave","hasApiKey":true},
  "tools":{"disabled":["shell"],"confirm":["mail"],"catalog":[
    {"name":"mail_send","family":"mail","risk":"outward","description":"Sends mail.","confirms":true,"core":true,"actions":null},
    {"name":"mail_search","family":"mail","risk":"low","description":"Searches mail.","confirms":false,"core":false,"actions":null},
    {"name":"shell","family":"computer","risk":"local","description":"Runs a command on the attached computer.","confirms":true,"core":false,"actions":["run","stop"]}]},
  "voice":{"enabled":true,"provider":"main","transcriptionModel":"gpt-4o-transcribe","silenceMS":600,"speechModel":"gpt-4o-mini-tts","speechVoice":"cedar","askModel":""},
  "recording":{"isLiveTranscriptionEnabled":true,"isAccurateWordsEnabled":false},
  "browser":{"enabled":true,"cdpEndpoint":"chrome:9222","attachTabs":true,"allowPrivateAddresses":[],"idleTimeout":"5m","maxContexts":4},
  "mcpServers":[
    {"name":"tracker","transport":"","effectiveTransport":"http","url":"https://tracker.example.com/mcp","command":"","args":[],"envNames":["API_TOKEN"],"workingDir":"","auth":"static","effectiveAuth":"static",
     "hasAuthorization":true,"oauthClientId":"","hasOauthClientSecret":false,"oauthScopes":[],"oauthAuthorizationUrl":"","oauthTokenUrl":"","oauthRedirect":"","headless":true,"location":"","readOnly":["list_items"],"disabled":[],"timeout":"30s","enabled":true},
    {"name":"notes-files","transport":"stdio","effectiveTransport":"stdio","url":"","command":"example-notes-server","args":["--root","notes"],"envNames":[],"workingDir":"","auth":"","effectiveAuth":"none",
     "hasAuthorization":false,"oauthClientId":"","hasOauthClientSecret":false,"oauthScopes":[],"oauthAuthorizationUrl":"","oauthTokenUrl":"","oauthRedirect":"","headless":false,"location":"computer","readOnly":[],"disabled":["delete_note"],"timeout":"","enabled":false}
  ],
  "finance":{"offeredProviders":["plaid","simplefin"],"plaid":{"environment":"sandbox","clientId":"example-client","hasSecret":true,"countryCodes":["US"],"products":["transactions","investments"]}},
  "works":["triage"],"families":["mail","computer","servers"],"kinds":["openai","typesafe","openai-codex"]}"""

private const val SKILLS = """[
  {"name":"weather","description":"Forecasts for a place and a day.","version":"1.2.0","publisher":"Example Skills","enabled":true,"readable":true,"problem":null,"scope":"","secrets":["api_key"],"personalSecrets":[],
   "tools":[{"name":"get_weather","description":"Reads the forecast.","kind":"http","needsComputer":false}]},
  {"name":"cameras","description":"Looks at cameras on the home network and says what is there.","version":"0.4.1","publisher":"Example Skills","enabled":false,"readable":true,"problem":null,"scope":"person","secrets":["host"],"personalSecrets":["pin"],
   "tools":[{"name":"snapshot","description":"Takes a picture.","kind":"command","needsComputer":true},{"name":"list_cameras","description":"Lists the cameras.","kind":"command","needsComputer":true}]},
  {"name":"broken-skill","description":"A skill whose file cannot be read.","version":"0.1.0","publisher":"","enabled":true,"readable":false,"problem":"the declaration is missing a name","scope":"","secrets":[],"personalSecrets":[],"tools":[]}
]"""

private const val SOURCE_TYPES = """[
  {"name":"notes-folder","description":"A folder of notes.","version":"","publisher":"","url":"","isLocal":true,"installedAt":"2030-01-01T00:00:00Z","reader":"files","runs":["computer"],"requires":[],
   "guide":"# Notes folder\n\nReads every Markdown file in a folder on a computer.\n\n- Runs on the computer that is attached.\n- Never changes a file.","readable":true,"problem":null,"settings":[],"secrets":[]},
  {"name":"web-feed","description":"Articles from a feed address.","version":"1.0.0","publisher":"Example Skills","url":"","isLocal":false,"installedAt":"2030-01-02T00:00:00Z","reader":"web","runs":["server"],"requires":[],
   "guide":"","readable":true,"problem":null,"settings":[],"secrets":[]},
  {"name":"mail-archive","description":"Old mail from an archive.","version":"0.2.0","publisher":"Example Skills","url":"","isLocal":false,"installedAt":"2030-01-03T00:00:00Z","reader":"","runs":["computer"],"requires":["archive-tool"],
   "guide":"","readable":false,"problem":"the archive tool is not installed","settings":[],"secrets":[]}
]"""

/** One conversation of the invented oversight list: whose, what, when, and whether a turn is in flight. */
private class OversightDemoRun(val id: String, val agentId: String, val title: String, val jobKind: String, val lastAt: String, val ownerUsername: String, val ownerName: String, val isRunning: Boolean, val usageJson: String) {
    val json: String get() = """{"id":"$id","agentId":"$agentId","title":"$title","jobKind":"$jobKind","lastAt":"$lastAt","ownerUsername":"$ownerUsername","ownerName":"$ownerName","isRunning":$isRunning,"usage":$usageJson}"""
}

private val OVERSIGHT_DEMO_RUNS: List<OversightDemoRun> = listOf(
    OversightDemoRun("demo-observed", "agent-1", "Planning the trip", "named", "2030-01-05T09:10:00Z", "robin", "Robin Hale", true, """{"promptTokens":4200,"completionTokens":900,"cost":0.02,"cacheReadTokens":1500}"""),
    OversightDemoRun("run-1", "agent-1", "Sorted new mail", "triage", "2030-01-05T09:00:00Z", "robin", "Robin Hale", false, """{"promptTokens":1200,"completionTokens":300,"cost":0.004,"cacheReadTokens":800}"""),
    OversightDemoRun("run-2", "agent-2", "Looked up the club meeting day", "research", "2030-01-05T08:00:00Z", "alex", "Alex Moreno", false, """{"promptTokens":9000,"completionTokens":1500,"cost":0.05,"cacheReadTokens":0}"""),
    OversightDemoRun("demo-observed", "agent-2", "Main conversation", "main", "2030-01-05T07:45:00Z", "alex", "Alex Moreno", false, """{"promptTokens":3100,"completionTokens":600,"cost":0.01,"cacheReadTokens":null}"""),
    OversightDemoRun("run-3", "agent-1", "Morning brief", "schedule", "2030-01-05T07:30:00Z", "robin", "Robin Hale", false, """{"promptTokens":2500,"completionTokens":700,"cost":0.0,"cacheReadTokens":null}"""),
)

private fun field(name: String, valueJson: String, whenAsking: String): DemoAnswerer = DemoAnswerer { request ->
    if (request.document.contains(whenAsking) && Regex("""\b$name\b\s*[({]""").containsMatchIn(request.document)) DemoResponse("""{"data":{"$name":$valueJson}}""") else null
}

/**
 * What demo mode answers for the operator's administration: the agent settings, the providers' sign-in and price checks, every agent's
 * runs, and the skills and source types. They come before Settings' own answers, so a fuller read of a field they share wins.
 */
val manageDemoAnswerers: List<DemoAnswerer> = listOf(
    // The operator the pages are drawn for: every permission the agents page asks for.
    DemoAnswerer { request ->
        if (request.document.contains("manages")) {
            DemoResponse("""{"data":{"GetSession":{"manages":true,"permissions":{"everywhere":["user:manage","group:manage","role:manage","audit:read","server:manage","agent:audit","agent:act","domain:manage"],"byDomain":[]}}}}""")
        } else {
            null
        }
    },
    // The agent settings, read and saved: the document names its fields, which no other read of GetSettings asks for.
    DemoAnswerer { request ->
        when {
            request.document.contains("mcpServers") && request.document.contains("UpdateSettings") -> DemoResponse("""{"data":{"UpdateSettings":{"agent":$AGENT_SETTINGS}}}""")
            request.document.contains("mcpServers") -> DemoResponse("""{"data":{"GetSettings":{"agent":$AGENT_SETTINGS}}}""")
            request.document.contains("skillSecrets") && request.document.contains("UpdateSettings") -> DemoResponse("""{"data":{"UpdateSettings":{"agent":{"skillSecrets":[{"skill":"weather","key":"api_key","hasValue":true}]}}}}""")
            request.document.contains("skillSecrets") -> DemoResponse("""{"data":{"GetSettings":{"agent":{"skillSecrets":[{"skill":"weather","key":"api_key","hasValue":true},{"skill":"cameras","key":"host","hasValue":false}]}}}}""")
            else -> null
        }
    },
    graphQLField(
        "ListAgentModels",
        """[{"name":"main:model-large","canTranscribe":false},{"name":"main:model-small","canTranscribe":false},{"name":"main:embedding-small"},
          {"name":"main:speaker-model","canTranscribe":true,"hasSpeakerDiarization":true,"hasSegmentTimestamps":true,"hasSpeakerReferences":true,"maximumAudioBytes":26214400,"maximumSpeakerReferenceCount":4},
          {"name":"main:plain-transcriber","canTranscribe":true,"hasSpeakerDiarization":false,"hasSegmentTimestamps":true}]""",
    ),
    graphQLField("TestAgentProvider", """[{"name":"main:model-large","contextLength":200000},{"name":"main:model-small","contextLength":128000},{"name":"main:embedding-small","contextLength":8000}]"""),
    graphQLField("BeginAgentProviderSignIn", """{"signInId":"sign-in-1","userCode":"ABCD-1234","verificationAddress":"https://example.com/device","isSignedIn":false,"account":"","plan":""}"""),
    graphQLField("FinishAgentProviderSignIn", """{"signInId":"sign-in-1","userCode":"ABCD-1234","verificationAddress":"https://example.com/device","isSignedIn":true,"account":"robin@example.com","plan":"plus"}"""),
    graphQLField(
        "AgentBillingChecks",
        """[{"providerName":"main","checkedAt":"2030-01-05T03:00:00Z","errorMessage":"","compared":[
          {"model":"model-large","tokenKind":"input","configuredPerMillion":2.0,"billedPerMillion":2.0,"dayCount":9,"isPattern":false},
          {"model":"model-large","tokenKind":"output","configuredPerMillion":8.0,"billedPerMillion":8.4,"dayCount":9,"isPattern":false},
          {"model":"model-small*","tokenKind":"input","configuredPerMillion":0.2,"billedPerMillion":0.1,"dayCount":4,"isPattern":true}],
          "changed":[{"model":"model-large","tokenKind":"output","beforePerMillion":7.5,"afterPerMillion":8.0}]}]""",
    ),
    graphQLField("CheckAgentProviderPrices", """[{"providerName":"main","checkedAt":"2030-01-06T03:00:00Z","errorMessage":"","compared":[],"changed":[]}]"""),
    graphQLField("ListAllAgentRuns") { variables ->
        val isRunningOnly = (variables["isRunningOnly"] as? JsonPrimitive)?.booleanOrNull == true
        val kinds = (variables["kinds"] as? JsonArray)?.map { kind -> (kind as JsonPrimitive).content }.orEmpty()
        val matching = OVERSIGHT_DEMO_RUNS.filter { run -> (!isRunningOnly || run.isRunning) && (kinds.isEmpty() || kinds.contains(run.jobKind)) }
        """{"total":${matching.size},"totalCount":${matching.size},"runs":[${matching.joinToString(",") { run -> run.json }}]}"""
    },
    graphQLField(
        "ListFailedAgentTurns",
        """{"totalCount":2,"failedTurns":[
          {"conversationId":"demo-observed","conversationKind":"main","conversationTitle":null,"ownerUsername":"robin","ownerName":"Robin Hale","failedAt":"2030-01-05T08:40:00Z","errorText":"The model did not answer in time.","agentId":"agent-1"},
          {"conversationId":"demo-observed","conversationKind":"named","conversationTitle":"Club newsletter","ownerUsername":"alex","ownerName":"Alex Moreno","failedAt":"2030-01-04T17:05:00Z","errorText":"A tool call was refused: the mailbox is read-only.","agentId":"agent-2"}]}""",
    ),
    graphQLField(
        "GetUpgrade",
        """{"current":"1.42.0","latest":"1.43.0","available":true,"notes":"## 1.43.0\n\n- A faster mail queue.\n- Fixes the weekly plan bar on the providers page.\n- The agents page lists every run, newest first.\n\n### Upgrading\n\nNothing to do: the server restarts by itself.","url":"https://example.com/releases/1.43.0","checkedAt":"2030-01-05T00:00:00Z","applicable":true,"automatic":false,"enabled":true,"upgrading":false}""",
    ),
    graphQLField("RetryAgentJob", """{"id":"dead1"}"""),
    graphQLField("SetAgentLimit", """{"agentId":"agent-1","userId":"demo-user","username":"robin","name":"Juniper","enabled":true,"dailyTokens":100000,"dailyCost":0,"dead":0,"queued":2}"""),
    graphQLField("SetAgentDisabled", """{"agentId":"agent-2","userId":"user-alex","username":"alex","name":"","enabled":true,"dailyTokens":0,"dailyCost":2.5,"dead":3,"queued":0}"""),
    field("ListAgentSkills", SKILLS, "personalSecrets"),
    graphQLField(
        "SearchAgentSkills",
        """[{"name":"weather","description":"Forecasts for a place and a day.","version":"1.3.0","tags":["outdoors"],"installed":"1.2.0","newer":true},
          {"name":"cameras","description":"Looks at cameras on the home network.","version":"0.4.1","tags":["home"],"installed":"0.4.1","newer":false},
          {"name":"dictionary","description":"Looks a word up.","version":"2.0.0","tags":["words","reference"],"installed":null,"newer":false}]""",
    ),
    graphQLField("InstallAgentSkill", """{"name":"dictionary","version":"2.0.0"}"""),
    graphQLField("RemoveAgentSkill", "true"),
    graphQLField("SetAgentSkillEnabled", """{"name":"cameras","enabled":true}"""),
    graphQLField("SetAgentSkillScope", """{"name":"cameras","scope":"operator"}"""),
    field("ListAgentSourceTypes", SOURCE_TYPES, "installedAt"),
    graphQLField(
        "SearchAgentSourceTypes",
        """[{"name":"web-feed","description":"Articles from a feed address.","version":"1.1.0","tags":["web"],"installed":"1.0.0","newer":true},
          {"name":"calendar-notes","description":"Notes kept in calendar events.","version":"1.0.0","tags":["calendar"],"installed":null,"newer":false}]""",
    ),
    graphQLField("InstallAgentSourceType", """{"name":"calendar-notes","version":"1.0.0"}"""),
    graphQLField("RemoveAgentSourceType", "true"),
    graphQLField("AddCircleMember", """{"id":"circle-home"}"""),
)
