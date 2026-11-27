package com.teanode.kit.manage

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.access.OperatorAccess
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Instant

class AgentServerSettingsTest {
    private val settingsText = """
        {"enabled":true,"instructions":"Be brief.","allowPrivateAddresses":["192.0.2.10"],"skipCertificateCheck":[],
         "providers":[
           {"name":"main","kind":"openai","baseUrl":"","hasApiKey":true,"hasRefreshToken":false,"account":"","enabled":true,
            "models":[
              {"model":"model-large","name":"Large","contextWindow":0,"reportedContextWindow":200000,"isEnabled":true,"usedFor":["default"],
               "pricing":{"input":2,"output":8,"cacheRead":0.5,"cacheWrite":0,"audioInput":4.5}},
              {"model":"model-small","name":"","contextWindow":64000,"reportedContextWindow":0,"isEnabled":false,"usedFor":["fast","quick"],
               "pricing":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"audioInput":0}}],
            "pricingInput":1.25,"pricingOutput":10,"pricingCacheRead":0,"pricingCacheWrite":0,"pricingAudioInput":6,"planUsage":null},
           {"name":"decider","kind":"typesafe","baseUrl":"","hasApiKey":true,"hasRefreshToken":false,"account":"","enabled":true,
            "models":[{"model":"judge","name":"Judge","contextWindow":0,"reportedContextWindow":0,"isEnabled":true,"usedFor":["decide"],"pricing":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"audioInput":0}}],
            "pricingInput":0,"pricingOutput":0,"pricingCacheRead":0,"pricingCacheWrite":0,"pricingAudioInput":0,
            "planUsage":{"planName":"plus","observedAt":"2030-01-02T03:04:05Z","windows":[{"usedPercent":140,"windowMinutes":300,"resetsAt":"2030-01-02T05:00:00Z"}]}}
         ],
         "models":{"default":"main:model-large","fast":"main:model-small","quick":"main:model-small","embedding":"","triage":"main:model-small","research":"","summarize":"",
           "reply":"","ask":"","schedule":"","compact":"","scan":"","synthesize":"","decide":"decider:judge","categorize":"","embeddingDimensions":0,
           "choices":["main:model-large"]},
         "features":{"triage":true,"autoReply":false,"browser":false},
         "limits":{"maxBodyCharacters":20000,"dailyTokensPerAgent":1000000,"monthlyTokensPerServer":0,"dailyCostPerAgent":2.5,"monthlyCostPerServer":0,
           "maxRoundsPerAsk":20,"maxRoundsPerResearch":10,"maxRoundsPerReply":5,"maxRoundsPerDream":30,"maxToolCallsPerRun":50,"requestTimeout":"2m",
           "concurrency":4,"scanConcurrency":2,"rewriteConcurrency":2,"dreamShare":0.3,"ingestChunksPerRun":500,"embeddingTokensPerDay":0,
           "maxRoundsPerSubagent":0,"goalShare":0,"goalTurnsAlone":0,"backgroundWakesAlone":0},
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
    """.trimIndent()

    private val settings: AgentServerSettings = TeaNodeJson.decodeFromString(AgentServerSettings.serializer(), settingsText)

    private fun JsonObject.text(vararg path: String): String? {
        var current: kotlinx.serialization.json.JsonElement = this
        for (key in path) current = (current as? JsonObject)?.get(key) ?: return null
        return (current as? JsonPrimitive)?.content
    }

    @Test
    fun theAgentsPageOffersTheTabsThePermissionsOpen() {
        val auditor = OperatorAccess(isOperator = true, everywherePermissions = listOf("agent:audit"))
        assertTrue(auditor.canOpenAgentAdmin)
        assertEquals(listOf(AgentAdminTab.Usage), auditor.agentAdminTabs)
        assertEquals(AgentAdminTab.entries, OperatorAccess(true, listOf("agent:audit", "agent:act", "server:manage")).agentAdminTabs)
        assertEquals(listOf(AgentAdminTab.Usage, AgentAdminTab.Runs), OperatorAccess(true, listOf("agent:act")).agentAdminTabs)
        assertFalse(OperatorAccess(true, domainPermissions = listOf(OperatorAccess.DomainPermissions("domain-1", listOf("server:manage")))).canManageServer)
    }

    @Test
    fun listsAndPricesAreReadAsTyped() {
        assertEquals(listOf("a", "b", "c"), SettingsList.lineItems("a\nb, c"))
        assertEquals("1.50", SettingsList.price("1,50"))
        assertEquals("1.23", SettingsList.price("1.2.3x"))
        assertEquals("105", SettingsList.digits("10a5"))
        assertEquals("", SettingsList.text(0.0))
        assertEquals("2.5", SettingsList.text(2.5))
        assertEquals("3", SettingsList.text(3.0))
        assertEquals(JsonPrimitive(3L), SettingsList.numberValue(3.0))
    }

    @Test
    fun readsTheOperatorsAgentSettingsAndSavesASection() {
        assertEquals(2, settings.providers.size)
        assertEquals(listOf("mail_send", "shell"), settings.tools.catalog.map { tool -> tool.name })
        assertEquals(false, settings.features["autoReply"])
        assertTrue(settings.mcpServers.first().isHeadless)
        assertNull(settings.recording)
        val general = AgentGeneralDraft.of(settings).copy(skipCertificateCheckText = "camera.example.com\n192.0.2.20")
        assertEquals(JsonArray(listOf(JsonPrimitive("camera.example.com"), JsonPrimitive("192.0.2.20"))), general.agentParameters["skipCertificateCheck"])
    }

    @Test
    fun aServerThatDoesNotSayHasBothRecordingOptionsOn() {
        val unsaid = AgentServerRecordingDraft.of(null)
        assertTrue(unsaid.isLiveTranscriptionEnabled && unsaid.isAccurateWordsEnabled)
        val draft = AgentServerRecordingDraft(isLiveTranscriptionEnabled = true, isAccurateWordsEnabled = false)
        assertEquals("false", draft.agentParameters.text("recording", "isAccurateWordsEnabled"))
    }

    @Test
    fun aRenamedProviderTakesItsModelsWithIt() {
        val draft = AgentProviderDraft.of(settings.providers[0])
        assertEquals("1.25", draft.pricingInputText)
        assertFalse(draft.hasBillingApiKey)
        val section = draft.copy(name = "primary").agentParameters(0, settings)
        val providers = section["providers"] as JsonArray
        assertEquals(2, providers.size)
        assertEquals("primary", providers[0].jsonObject.text("name"))
        assertEquals("main", providers[0].jsonObject.text("previousName"))
        assertEquals("", providers[0].jsonObject.text("apiKey"))
        assertEquals("", providers[0].jsonObject.text("billingApiKey"))
        assertEquals("6", providers[0].jsonObject.text("pricingAudioInput"))
        assertNull(providers[0].jsonObject["allow"])
        assertNull(providers[0].jsonObject["deny"])
        assertNull(providers[0].jsonObject["modelPricing"])
        assertNull(providers[0].jsonObject["models"])
        assertNull(providers[1].jsonObject.text("previousName"))
        assertEquals("primary:model-large", section.text("models", "default"))
        assertEquals("decider:judge", section.text("models", "decide"))
        assertNull(draft.copy(isEnabled = false).agentParameters(0, settings)["models"])
        val kept = AgentProviderDraft.agentParameters(0, settings)["providers"] as JsonArray
        assertEquals(listOf("decider"), kept.map { provider -> provider.jsonObject.text("name") })
    }

    @Test
    fun aProviderOnAPlanIsSavedOnlyOnceSignedIn() {
        val draft = AgentProviderDraft(name = "plan", providerKind = AgentProviderSettings.SIGNED_IN_KIND)
        assertFalse(draft.canSave)
        assertTrue(draft.copy(hasRefreshToken = true).canSave)
        assertFalse(AgentProviderDraft().canSave)
    }

    @Test
    fun theModelsAndTheFeaturesAreSentWhole() {
        val models = settings.models.assigning("research", "main:model-large")
        assertEquals("main:model-large", models.research)
        assertEquals("", models.assignment("not-a-field"))
        assertEquals("", models.assigning("quick", "").parameters.text("quick"))
        assertEquals("main:model-large", models.agentParameters.text("models", "research"))
        assertEquals("0", models.agentParameters.text("models", "embeddingDimensions"))
        assertTrue(AgentFeatureSwitches.isOn("newFeature", settings.features))
        assertFalse(AgentFeatureSwitches.isOn("autoReply", settings.features))
        assertEquals("false", AgentFeatureSwitches.agentParameters(mapOf("triage" to false)).text("features", "triage"))
    }

    @Test
    fun aProvidersModelsAreReadWithTheirOwnSettings() {
        val large = settings.providers[0].models[0]
        assertEquals("Large", large.displayName)
        assertEquals(200000, large.reportedContextWindow)
        assertEquals(listOf("default"), large.usedFor)
        assertEquals(4.5, large.pricing.audioInput, 0.0)
        val small = settings.providers[0].models[1]
        assertEquals("model-small", small.displayName)
        assertEquals(64000, small.contextWindow)
        assertFalse(small.isEnabled)
    }

    @Test
    fun aModelEntryIsSavedWithoutWhatTheServerOwns() {
        val draft = AgentModelEntryDraft.of(settings.providers[0].models[0]).copy(name = " Big ", contextWindowText = "128000", outputText = "9.5")
        assertTrue(draft.canSave)
        val json = draft.entry.toJson()
        assertEquals("model-large", json.text("model"))
        assertEquals("Big", json.text("name"))
        assertEquals("128000", json.text("contextWindow"))
        assertEquals("9.5", json.text("pricing", "output"))
        assertEquals("true", json.text("isEnabled"))
        assertNull(json["usedFor"])
        assertNull(json["reportedContextWindow"])
    }

    @Test
    fun aContextWindowIsNoneOrAtLeastAThousand() {
        val draft = AgentModelEntryDraft(model = "model-large")
        assertTrue(draft.canSave)
        assertFalse(draft.copy(contextWindowText = "999").canSave)
        assertTrue(draft.copy(contextWindowText = "1000").canSave)
        assertFalse(draft.copy(model = " ").canSave)
        assertEquals(0, draft.entry.contextWindow)
    }

    @Test
    fun theRolePickersListTheModelsThatAreOnAsProviderAndModel() {
        val offered = AgentModelChoices.offered(settings.providers)
        assertEquals(listOf("main:model-large", "decider:judge"), offered.map { option -> option.value })
        assertEquals(listOf("main · Large", "decider · Judge"), offered.map { option -> option.label })
        val choices = AgentModelChoices.of(offered, settings.providers)
        assertEquals(listOf("main:model-large"), choices.forWork.map { option -> option.value })
        assertEquals(listOf("decider:judge"), choices.forDeciding.map { option -> option.value })
        val listed = AgentModelChoices.listed(offered, "other:gone", settings.providers)
        assertEquals("other:gone", listed.first().value)
        assertEquals("other · gone", listed.first().label)
        assertEquals("main · model-small", AgentModelChoices.optionFor("main:model-small", settings.providers).label)
        assertEquals(offered, AgentModelChoices.listed(offered, "main:model-large", settings.providers))
    }

    @Test
    fun theLiveListOffersWhatThisProviderHasNotAdded() {
        val live = listOf("main:model-large", "main:model-new", "decider:judge", "main:model-new", "main:")
        assertEquals(listOf("model-new"), AgentModelChoices.idsToAdd(live, settings.providers[0]))
        assertEquals(emptyList<String>(), AgentModelChoices.idsToAdd(live, settings.providers[1]))
    }

    @Test
    fun thePlanWindowsSayWhatIsLeft() {
        val window = settings.providers[1].planUsage!!.windows[0]
        val before = Instant.parse("2030-01-02T04:00:00Z")
        assertEquals(100, window.shownUsedPercent(before))
        assertEquals(0, window.shownUsedPercent(before.plusSeconds(7200)))
    }

    @Test
    fun theToolPolicyIsOneWordPerName() {
        var policy = AgentToolPolicyDraft.of(settings)
        assertEquals(listOf("mail", "computer", "mail_send", "shell"), policy.names)
        assertEquals("confirm", policy.word("mail"))
        assertEquals("off", policy.word("shell"))
        policy = policy.setting("computer", "off").setting("shell", "allow")
        val tools = policy.agentParameters["tools"]!!.jsonObject
        assertEquals(JsonArray(listOf(JsonPrimitive("computer"))), tools["disabled"])
        assertEquals(JsonArray(listOf(JsonPrimitive("mail"))), tools["confirm"])
    }

    @Test
    fun theLimitsAreSentAsNumbersAndTheTimeoutAsADuration() {
        val limits = AgentLimitsDraft.of(settings)
        assertEquals("0.3", limits.limitTexts["dreamShare"])
        assertEquals("0", limits.limitTexts["monthlyTokensPerServer"])
        val section = limits.copy(limitTexts = limits.limitTexts + ("concurrency" to "8"), currency = "EUR").agentParameters
        assertEquals("8", section.text("limits", "concurrency"))
        assertEquals("2m", section.text("limits", "requestTimeout"))
        assertEquals("EUR", section.text("currency"))
        assertEquals("720h", section.text("retention", "runs"))
    }

    @Test
    fun recordingSizesRemainByteSizeStringsAndTranscriptionHasNoDefault() {
        val limits = AgentLimitsDraft.of(settings)
        val sent = limits.copy(limitTexts = limits.limitTexts + ("maxRecordingMediaBytes" to "3 GB")).agentParameters
        assertEquals("3 GB", sent.text("limits", "maxRecordingMediaBytes"))
        assertEquals("0", limits.limitTexts["maxRecordingAccountBytes"])
        val models = settings.models.assigning("transcription", "audio-service:speaker-model")
        assertEquals("new-audio:speaker-model", models.renamingProvider("audio-service", "new-audio").assignment("transcription"))
        assertTrue(AgentModelAssignments.WORK_FIELD_NAMES.contains("transcription"))
    }

    @Test
    fun aKeyIsSentOnlyWhenOneIsTyped() {
        val search = AgentWebSearchDraft(settings.search.searchKind)
        assertNull(search.agentParameters.text("search", "apiKey"))
        assertEquals("a-search-key", search.copy(apiKey = "a-search-key").agentParameters.text("search", "apiKey"))
        val finance = AgentFinanceDraft.of(settings.finance).settingOffered("plaid", true).settingProduct("liabilities", true).copy(countryCodesText = "us, gb")
        val section = finance.agentParameters["finance"]!!.jsonObject
        assertEquals(JsonArray(listOf(JsonPrimitive("plaid"), JsonPrimitive("simplefin"))), section["offeredProviders"])
        val plaid = section["plaid"]!!.jsonObject
        assertEquals(JsonArray(listOf(JsonPrimitive("transactions"), JsonPrimitive("liabilities"))), plaid["products"])
        assertEquals(JsonArray(listOf(JsonPrimitive("US"), JsonPrimitive("GB"))), plaid["countryCodes"])
        assertNull(plaid["secret"])
        assertTrue(finance.hasProduct("transactions"))
    }

    @Test
    fun aConnectedServerKeepsItsSecretsAndCarriesItsPreviousName() {
        val draft = ConnectedServerDraft.of(settings.mcpServers[0])
        assertEquals("API_TOKEN=", draft.envText)
        assertFalse(draft.isStdio)
        assertTrue(draft.showsAuthorization)
        val section = draft.copy(name = "issues", argsText = "--port  8080").agentParameters(0, settings)
        val servers = section["mcpServers"] as JsonArray
        assertEquals("tracker", servers[0].jsonObject.text("previousName"))
        assertEquals("", servers[0].jsonObject.text("authorization"))
        assertEquals(JsonArray(listOf(JsonPrimitive("--port"), JsonPrimitive("8080"))), servers[0].jsonObject["args"])
        val added = ConnectedServerDraft(name = "local", command = "example-server")
        assertTrue(added.isStdio)
        assertFalse(added.copy(isHeadless = true).settingLocation("computer").isHeadless)
        val appended = added.agentParameters(null, settings)["mcpServers"] as JsonArray
        assertEquals(2, appended.size)
        assertEquals(JsonArray(listOf(JsonPrimitive("API_TOKEN="))), appended[0].jsonObject["env"])
        assertEquals("", appended[0].jsonObject.text("oauthRedirect"))
    }

    @Test
    fun onlyTheKeysTypedAreSent() {
        val draft = SkillSecretsDraft("cameras", mapOf("host" to "camera.example.com"))
        assertEquals(listOf("host"), draft.typedKeys(listOf("api_key", "host")))
        val sent = (draft.agentParameters(listOf("api_key", "host"))["skillSecrets"] as JsonArray)[0].jsonObject
        assertEquals("cameras", sent.text("skill"))
        assertEquals("host", sent.text("key"))
        assertEquals("camera.example.com", sent.text("value"))
    }

    @Test
    fun theRegistryIsFilteredAndSaysWhenSomethingIsBehind() {
        val offers = listOf(
            RegistryOffer("weather", "Forecasts.", "2.0.0", listOf("outdoors"), installed = "1.0.0", isNewer = true),
            RegistryOffer("dictionary", "Words.", "1.0.0"),
        )
        assertTrue(offers[0].isInstalled)
        assertFalse(offers[0].isCurrent)
        assertFalse(offers[1].isInstalled)
        assertTrue(offers[0].matches("OUTDOORS"))
        assertFalse(offers[1].matches("outdoors"))
        assertTrue(RegistryOffer.isBehind(1, offers))
        assertFalse(RegistryOffer.isBehind(0, offers))
    }

    @Test
    fun aSourceTypeWithABlankGuideHasNone() {
        val sourceType = TeaNodeJson.decodeFromString(InstalledSourceType.serializer(), """{"name":"notes-folder","isLocal":true,"reader":"files","runs":["computer"],"guide":"  "}""")
        assertTrue(sourceType.isLocal)
        assertFalse(sourceType.hasGuide)
    }

    @Test
    fun everyRunIsFilteredOnTheServer() {
        val variables = ServerAgentRunFilter(agentId = "agent-1", jobKind = "dream", query = "  tidy ").variables(50, 100)
        assertEquals("agent-1", (variables["agentId"] as JsonPrimitive).content)
        assertEquals(JsonArray(listOf(JsonPrimitive("dream"))), variables["kinds"])
        assertEquals("tidy", (variables["query"] as JsonPrimitive).content)
        assertTrue(ServerAgentRunFilter().variables(50, 0)["query"] is kotlinx.serialization.json.JsonNull)
    }

    @Test
    fun aBillingCheckThatNeverRanHasNoTime() {
        val never = AgentBillingCheck("main", "0001-01-01T00:00:00Z")
        assertNull(never.checkedInstant)
        assertEquals(Instant.parse("2030-01-02T03:04:05Z"), AgentBillingCheck("main", "2030-01-02T03:04:05Z").checkedInstant)
    }
}
