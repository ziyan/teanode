package com.teanode.kit.manage

/** The GraphQL documents of managing the server. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object ManageServerDocuments {
    val MANAGE_SERVER_SETTINGS_FIELDS = """
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
places { providerKind providerUrl mapStyleUrl }
""".trimEnd()

    val MANAGE_AGENT_SETTINGS_FIELDS = """
agent {
  enabled instructions allowPrivateAddresses skipCertificateCheck
  providers { name kind baseUrl hasApiKey hasBillingApiKey hasRefreshToken account enabled allow deny pricingInput pricingOutput pricingCacheRead pricingCacheWrite pricingAudioInput modelPricing { model input output cacheRead cacheWrite audioInput } planUsage { planName observedAt windows { usedPercent windowMinutes resetsAt } } }
  models { default fast quick embedding triage research summarize reply ask schedule compact scan synthesize decide categorize transcription embeddingDimensions choices }
  features { triage summaries draftReplies search research autoReply ask schedules browser connectedServers computer chatApps skills subagents remember knowledge dreaming }
  limits { maxBodyCharacters dailyTokensPerAgent monthlyTokensPerServer dailyCostPerAgent monthlyCostPerServer maxRoundsPerAsk maxRoundsPerResearch maxRoundsPerReply maxRoundsPerDream maxToolCallsPerRun requestTimeout concurrency scanConcurrency rewriteConcurrency dreamShare ingestChunksPerRun embeddingTokensPerDay maxRoundsPerSubagent goalTurnsPerDay goalTurnsAlone goalsInProgress backgroundWakesAlone maxRecordingMediaBytes maxRecordingAccountBytes }
  retention { runs corrections }
  currency
  search { kind hasApiKey }
  tools { disabled confirm catalog { name family risk description confirms core actions } }
  voice { enabled provider transcriptionModel silenceMS speechModel speechVoice askModel }
  recording { isLiveTranscriptionEnabled isAccurateWordsEnabled }
  browser { enabled cdpEndpoint attachTabs allowPrivateAddresses idleTimeout maxContexts }
  mcpServers { name transport effectiveTransport url command args envNames workingDir auth effectiveAuth hasAuthorization oauthClientId hasOauthClientSecret oauthScopes oauthAuthorizationUrl oauthTokenUrl oauthRedirect headless location readOnly disabled timeout enabled }
  finance { offeredProviders plaid { environment clientId hasSecret countryCodes products } }
  works families kinds
}
""".trimEnd()

    val MANAGE_AGENT_SUMMARY_FIELDS = """
agentId userId username name enabled operatorDisabledAt dailyTokens dailyCost dead queued lastRunAt
sources { mailboxId name policy { granted } }
today { used limit resetsAt cost costLimit currency }
totals { promptTokens completionTokens cacheReadTokens cacheWriteTokens audioInputTokens calls }
""".trimEnd()

    val MANAGE_SKILL_FIELDS = """
name description version publisher enabled readable problem scope secrets personalSecrets
tools { name description kind needsComputer }
""".trimEnd()

    val MANAGE_SOURCE_TYPE_FIELDS = """
name description version publisher url isLocal installedAt reader runs requires guide readable problem
settings { name description settingType pattern itemPattern default isRequired minimum maximum }
secrets { key description isOptional }
""".trimEnd()

    val MANAGE_PROVIDER_SIGN_IN_FIELDS = """
signInId userCode verificationAddress isSignedIn account plan
""".trimEnd()

    val MANAGE_SERVER_STATUS = """
{
  GetServerStatus {
    instance version commit startedAt uptimeSeconds
    pendingRestart supervision restarting
  }
}
""".trimEnd()

    val MANAGE_RESTART_SERVER = """
mutation { RestartServer { started instance supervision } }
""".trimEnd()

    val MANAGE_SERVER_UPGRADE = """
query (${'$'}check: Boolean) {
  GetUpgrade(check: ${'$'}check) {
    current latest available notes url checkedAt attemptedAt checking error checkError applicable reason automatic enabled window upgrading
  }
}
""".trimEnd()

    val MANAGE_APPLY_UPGRADE = """
mutation (${'$'}version: String) {
  ApplyUpgrade(version: ${'$'}version) {
    current latest available applicable reason upgrading
  }
}
""".trimEnd()

    val MANAGE_SERVER_SETUP = """
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
""".trimEnd()

    val MANAGE_UPDATE_SUBMISSION = """
mutation (${'$'}submission: SubmissionParametersInput) {
  UpdateSettings(submission: ${'$'}submission) {
    submission { host port effectiveHost effectivePort }
  }
}
""".trimEnd()

    val MANAGE_UPDATE_MAIL_PROGRAM = """
mutation (${'$'}imap: IMAPParametersInput!) {
  UpdateSettings(imap: ${'$'}imap) { imap { host port tlsPort effectiveHost effectivePort effectiveTlsPort } }
}
""".trimEnd()

    val MANAGE_SERVER_SETTINGS = """
{ GetSettings { ${MANAGE_SERVER_SETTINGS_FIELDS} } }
""".trimEnd()

    val MANAGE_UPDATE_SERVER_SETTINGS = """
mutation (
  ${'$'}s3: S3ParametersInput
  ${'$'}route53: Route53ParametersInput
  ${'$'}antivirus: ServiceParametersInput
  ${'$'}antispam: AntispamParametersInput
  ${'$'}relay: RelayParametersInput
  ${'$'}proxy: ProxyParametersInput
  ${'$'}certificates: CertificateParametersInput
  ${'$'}smtp: SMTPParametersInput
  ${'$'}resolver: ResolverParametersInput
  ${'$'}session: SessionParametersInput
  ${'$'}passkey: PasskeyParametersInput
  ${'$'}listen: ListenParametersInput
  ${'$'}sso: SSOParametersInput
  ${'$'}identity: IdentityParametersInput
  ${'$'}storage: StorageParametersInput
  ${'$'}geoip: GeoIPParametersInput
  ${'$'}places: PlacesParametersInput
) {
  UpdateSettings(
    s3: ${'$'}s3
    route53: ${'$'}route53
    antivirus: ${'$'}antivirus
    antispam: ${'$'}antispam
    relay: ${'$'}relay
    proxy: ${'$'}proxy
    certificates: ${'$'}certificates
    smtp: ${'$'}smtp
    resolver: ${'$'}resolver
    session: ${'$'}session
    passkey: ${'$'}passkey
    listen: ${'$'}listen
    sso: ${'$'}sso
    identity: ${'$'}identity
    storage: ${'$'}storage
    geoip: ${'$'}geoip
    places: ${'$'}places
  ) {
    ${MANAGE_SERVER_SETTINGS_FIELDS}
  }
}
""".trimEnd()

    val MANAGE_AGENT_SETTINGS = """
{ GetSettings { ${MANAGE_AGENT_SETTINGS_FIELDS} } }
""".trimEnd()

    val MANAGE_UPDATE_AGENT_SETTINGS = """
mutation (${'$'}agent: AgentParametersInput) {
  UpdateSettings(agent: ${'$'}agent) { ${MANAGE_AGENT_SETTINGS_FIELDS} }
}
""".trimEnd()

    val MANAGE_AGENT_MODELS = """
{ ListAgentModels { name contextLength canTranscribe hasSpeakerDiarization hasSegmentTimestamps hasSpeakerReferences maximumAudioBytes maximumSpeakerReferenceCount } }
""".trimEnd()

    val MANAGE_TEST_AGENT_PROVIDER = """
query (${'$'}name: String!) { TestAgentProvider(name: ${'$'}name) { name contextLength } }
""".trimEnd()

    val MANAGE_BEGIN_PROVIDER_SIGN_IN = """
mutation (${'$'}provider: String!) { BeginAgentProviderSignIn(provider: ${'$'}provider) { ${MANAGE_PROVIDER_SIGN_IN_FIELDS} } }
""".trimEnd()

    val MANAGE_FINISH_PROVIDER_SIGN_IN = """
mutation (${'$'}signInId: String!) { FinishAgentProviderSignIn(signInId: ${'$'}signInId) { ${MANAGE_PROVIDER_SIGN_IN_FIELDS} } }
""".trimEnd()

    val MANAGE_AGENT_ADMIN = """
query (${'$'}by: String, ${'$'}since: DateTime, ${'$'}until: DateTime) {
  ListAgents { ${MANAGE_AGENT_SUMMARY_FIELDS} }
  AgentServerUsage(by: ${'$'}by, since: ${'$'}since, until: ${'$'}until) { key cost currency billedAmount billedCurrencyCode totals { promptTokens completionTokens cacheReadTokens cacheWriteTokens audioInputTokens calls } }
  ListAgentDeadLetters { id agentId mailboxId kind subjectId attempts error finishedAt }
}
""".trimEnd()

    val MANAGE_SET_AGENT_LIMIT = """
mutation (${'$'}agentId: String!, ${'$'}dailyTokens: Int64, ${'$'}dailyCost: Float) {
  SetAgentLimit(agentId: ${'$'}agentId, dailyTokens: ${'$'}dailyTokens, dailyCost: ${'$'}dailyCost) { ${MANAGE_AGENT_SUMMARY_FIELDS} }
}
""".trimEnd()

    val MANAGE_SET_AGENT_DISABLED = """
mutation (${'$'}agentId: String!, ${'$'}disabled: Boolean!) {
  SetAgentDisabled(agentId: ${'$'}agentId, disabled: ${'$'}disabled) { ${MANAGE_AGENT_SUMMARY_FIELDS} }
}
""".trimEnd()

    val MANAGE_RETRY_AGENT_JOB = """
mutation (${'$'}jobId: String!) { RetryAgentJob(jobId: ${'$'}jobId) { id } }
""".trimEnd()

    val MANAGE_ALL_AGENT_RUNS = """
query (${'$'}first: Int, ${'$'}offset: Int, ${'$'}agentId: String, ${'$'}jobId: String, ${'$'}kinds: [String!], ${'$'}query: String) {
  ListAllAgentRuns(first: ${'$'}first, offset: ${'$'}offset, agentId: ${'$'}agentId, jobId: ${'$'}jobId, kinds: ${'$'}kinds, query: ${'$'}query) {
    total
    runs { id agentId title jobKind lastAt usage { promptTokens cacheReadTokens completionTokens cost } }
  }
}
""".trimEnd()

    val MANAGE_INSTALLED_SKILLS = """
query {
  ListAgentSkills { ${MANAGE_SKILL_FIELDS} }
}
""".trimEnd()

    val MANAGE_SKILL_REGISTRY = """
query { SearchAgentSkills { name description version tags installed newer } }
""".trimEnd()

    val MANAGE_FILLED_SKILL_SECRETS = """
query { GetSettings { agent { skillSecrets { skill key hasValue } } } }
""".trimEnd()

    val MANAGE_INSTALL_SKILL = """
mutation (${'$'}name: String!) {
  InstallAgentSkill(name: ${'$'}name) { name version }
}
""".trimEnd()

    val MANAGE_REMOVE_SKILL = """
mutation (${'$'}name: String!) { RemoveAgentSkill(name: ${'$'}name) }
""".trimEnd()

    val MANAGE_SET_SKILL_ENABLED = """
mutation (${'$'}name: String!, ${'$'}enabled: Boolean!) {
  SetAgentSkillEnabled(name: ${'$'}name, enabled: ${'$'}enabled) { name enabled }
}
""".trimEnd()

    val MANAGE_SET_SKILL_SCOPE = """
mutation (${'$'}name: String!, ${'$'}scope: String!) {
  SetAgentSkillScope(name: ${'$'}name, scope: ${'$'}scope) { name scope }
}
""".trimEnd()

    val MANAGE_SAVE_SKILL_SECRETS = """
mutation (${'$'}agent: AgentParametersInput) {
  UpdateSettings(agent: ${'$'}agent) { agent { skillSecrets { skill key hasValue } } }
}
""".trimEnd()

    val MANAGE_INSTALLED_SOURCE_TYPES = """
query { ListAgentSourceTypes { ${MANAGE_SOURCE_TYPE_FIELDS} } }
""".trimEnd()

    val MANAGE_SOURCE_TYPE_REGISTRY = """
query { SearchAgentSourceTypes { name description version tags installed newer } }
""".trimEnd()

    val MANAGE_INSTALL_SOURCE_TYPE = """
mutation (${'$'}name: String!) { InstallAgentSourceType(name: ${'$'}name) { name version } }
""".trimEnd()

    val MANAGE_REMOVE_SOURCE_TYPE = """
mutation (${'$'}name: String!) { RemoveAgentSourceType(name: ${'$'}name) }
""".trimEnd()

}
