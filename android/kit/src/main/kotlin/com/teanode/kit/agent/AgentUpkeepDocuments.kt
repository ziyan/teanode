package com.teanode.kit.agent

/** The GraphQL documents of the agent's upkeep pages (memory, dreams, schedules, alerts, connections, activity). Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object AgentUpkeepDocuments {
    val AGENT_UPKEEP_FIELDS = """
id enabled operatorDisabledAt dreamFrom dreamUntil dreamBootstrap isAlertsEnabled alertQuietStart alertQuietEnd alertDailyMost lookAroundWeekday lookAroundTime
""".trimEnd()

    val AGENT_UPKEEP = """
query { ReadAgent { agent { ${AGENT_UPKEEP_FIELDS} } } }
""".trimEnd()

    val UPDATE_AGENT_UPKEEP = """
mutation (${'$'}dreamFrom: String, ${'$'}dreamUntil: String, ${'$'}isAlertsEnabled: Boolean, ${'$'}alertQuietStart: String, ${'$'}alertQuietEnd: String, ${'$'}alertDailyMost: Int, ${'$'}lookAroundWeekday: String, ${'$'}lookAroundTime: String) {
  UpdateAgent(dreamFrom: ${'$'}dreamFrom, dreamUntil: ${'$'}dreamUntil, isAlertsEnabled: ${'$'}isAlertsEnabled, alertQuietStart: ${'$'}alertQuietStart, alertQuietEnd: ${'$'}alertQuietEnd, alertDailyMost: ${'$'}alertDailyMost, lookAroundWeekday: ${'$'}lookAroundWeekday, lookAroundTime: ${'$'}lookAroundTime) {
    agent { ${AGENT_UPKEEP_FIELDS} }
  }
}
""".trimEnd()

    val ATTACHED_COMPUTERS = """
query { ReadAgentComputers { computers { name description } } }
""".trimEnd()

    val KNOWLEDGE_SOURCE_FIELDS = """
id kind name specification { type settings computer path format mailboxId readEveryCheckout }
rootPath enabled cron lastRunAt lastError documentCount chunkCount refusedCount more
checkoutsKeptToProfile filesKeptToProfile
""".trimEnd()

    val KNOWLEDGE_SOURCE_DETAILS = """
query {
  ListAgentKnowledgeSources { ${KNOWLEDGE_SOURCE_FIELDS} }
  ListAgentSourceAttachments { sourceId undecided declined described }
}
""".trimEnd()

    val KNOWLEDGE_SOURCE_TYPES = """
query {
  ListAgentSourceTypes {
    name description isLocal reader requires readable problem
    settings { name description settingType pattern itemPattern default isRequired minimum maximum }
    secrets { key description isOptional }
  }
}
""".trimEnd()

    val SAVE_KNOWLEDGE_SOURCE = """
mutation (${'$'}sourceId: String, ${'$'}kind: String, ${'$'}name: String, ${'$'}computer: String, ${'$'}path: String, ${'$'}format: String, ${'$'}enabled: Boolean, ${'$'}mailboxId: String, ${'$'}rootPath: String, ${'$'}cron: String, ${'$'}readEveryCheckout: Boolean, ${'$'}type: String, ${'$'}settings: JSON) {
  SaveAgentKnowledgeSource(sourceId: ${'$'}sourceId, kind: ${'$'}kind, name: ${'$'}name, computer: ${'$'}computer, path: ${'$'}path, format: ${'$'}format, enabled: ${'$'}enabled, mailboxId: ${'$'}mailboxId, rootPath: ${'$'}rootPath, cron: ${'$'}cron, readEveryCheckout: ${'$'}readEveryCheckout, type: ${'$'}type, settings: ${'$'}settings) { id name }
}
""".trimEnd()

    val KNOWLEDGE_SOURCE_SECRETS = """
query (${'$'}sourceId: String!) { ListAgentKnowledgeSourceSecrets(sourceId: ${'$'}sourceId) { key isSet } }
""".trimEnd()

    val SET_KNOWLEDGE_SOURCE_SECRET = """
mutation (${'$'}sourceId: String!, ${'$'}key: String!, ${'$'}value: String!) {
  SetAgentKnowledgeSourceSecret(sourceId: ${'$'}sourceId, key: ${'$'}key, value: ${'$'}value) { key isSet }
}
""".trimEnd()

    val DECLINED_ATTACHMENTS = """
query (${'$'}sourceId: String!) {
  ListAgentDeclinedAttachments(sourceId: ${'$'}sourceId, first: 100) { documentId name contentType channel thread path declined }
}
""".trimEnd()

    val AGENT_DREAMS = """
query (${'$'}first: Int) {
  ListAgentDreams(first: ${'$'}first) {
    id jobId startedAt finishedAt digested filed merged rewritten moved dormant embedded backlog coarse
    strengthened associated rehearsed gaps unknown revised overviewsWritten themesMade themesUpdated reflectionsWritten lastError cost currency
  }
}
""".trimEnd()

    val AGENT_DREAM_STATE = """
query { AgentDreamState { dreamJobStatus queuedAt startedAt } }
""".trimEnd()

    val READING_PROGRESS = """
query { AgentReadingProgress { waiting read perHour hoursLeft bootstrapping spent costLeft currency dreams } }
""".trimEnd()

    val DREAM_NOW = """
mutation { DreamAgentNow }
""".trimEnd()

    val DREAM_BOOTSTRAP = """
mutation (${'$'}on: Boolean!) { DreamAgentNow(bootstrap: ${'$'}on) }
""".trimEnd()

    val REREAD_DOCUMENTS = """
mutation (${'$'}minutes: Int!) { RereadAgentDocuments(minutes: ${'$'}minutes) }
""".trimEnd()

    val AGENT_SCHEDULES = """
query { ListAgentSchedules { id name cron prompt deliver enabled lastRunAt nextRunAt conversationId mailConditions { field header operator value } } }
""".trimEnd()

    val SAVE_AGENT_SCHEDULE = """
mutation (${'$'}scheduleId: String, ${'$'}name: String, ${'$'}cron: String, ${'$'}mailConditions: [MailboxRuleConditionInput!], ${'$'}prompt: String, ${'$'}deliver: String, ${'$'}enabled: Boolean) {
  SaveAgentSchedule(scheduleId: ${'$'}scheduleId, name: ${'$'}name, cron: ${'$'}cron, mailConditions: ${'$'}mailConditions, prompt: ${'$'}prompt, deliver: ${'$'}deliver, enabled: ${'$'}enabled) { id }
}
""".trimEnd()

    val DELETE_AGENT_SCHEDULE = """
mutation (${'$'}scheduleId: String!) { DeleteAgentSchedule(scheduleId: ${'$'}scheduleId) }
""".trimEnd()

    val RUN_AGENT_SCHEDULE = """
mutation (${'$'}scheduleId: String!) { RunAgentSchedule(scheduleId: ${'$'}scheduleId) }
""".trimEnd()

    val SET_DAILY_BRIEF = """
mutation (${'$'}enabled: Boolean!, ${'$'}at: String, ${'$'}days: [Int!]) {
  SetAgentBrief(enabled: ${'$'}enabled, at: ${'$'}at, days: ${'$'}days) { id }
}
""".trimEnd()

    val RUN_DAILY_BRIEF = """
mutation { RunAgentBriefNow { id name } }
""".trimEnd()

    val AGENT_ALERTS = """
query {
  ListAgentAlerts(first: 20) { id alertText sentAt subjectKey isUrgent
    covered { mailId subject fromAddress }
    muteChoices { muteScope muteTarget } }
  ListAgentAlertMutes { id muteScope muteTarget createdAt }
}
""".trimEnd()

    val MUTE_AGENT_ALERT = """
mutation (${'$'}alertId: String, ${'$'}muteScope: String) {
  MuteAgentAlert(alertId: ${'$'}alertId, muteScope: ${'$'}muteScope) { id muteScope muteTarget }
}
""".trimEnd()

    val UNMUTE_AGENT_ALERT = """
mutation (${'$'}muteId: String!) { UnmuteAgentAlert(muteId: ${'$'}muteId) }
""".trimEnd()

    val AGENT_FOLLOW_UPS = """
query {
  ListAgentFollowUps(isOpenOnly: true) { id followUpKind followUpTitle payee carrier amount currencyCode trackingNumber dueOn place sourceUrl followUpState }
}
""".trimEnd()

    val STOP_AGENT_FOLLOW_UP = """
mutation (${'$'}followUpId: String!) { StopAgentFollowUp(followUpId: ${'$'}followUpId) { id followUpState } }
""".trimEnd()

    val ADD_AGENT_FOLLOW_UP = """
mutation (${'$'}followUpKind: String!, ${'$'}followUpTitle: String!, ${'$'}dueOn: String, ${'$'}payee: String, ${'$'}amount: String, ${'$'}currencyCode: String, ${'$'}carrier: String, ${'$'}trackingNumber: String, ${'$'}place: String) {
  AddAgentFollowUp(followUpKind: ${'$'}followUpKind, followUpTitle: ${'$'}followUpTitle, dueOn: ${'$'}dueOn, payee: ${'$'}payee, amount: ${'$'}amount, currencyCode: ${'$'}currencyCode, carrier: ${'$'}carrier, trackingNumber: ${'$'}trackingNumber, place: ${'$'}place) { id followUpTitle }
}
""".trimEnd()

    val MUTE_AGENT_ALERT_KIND = """
mutation (${'$'}muteTarget: String) {
  MuteAgentAlert(muteScope: "kind", muteTarget: ${'$'}muteTarget) { id muteScope muteTarget }
}
""".trimEnd()

    val LEARNED_FACTS = """
query (${'$'}days: Int, ${'$'}first: Int) {
  ListAgentLearned(days: ${'$'}days, first: ${'$'}first) {
    fact { id number kind text happenedAt inferred evidence { kind id quote } audiences }
    path
    name
  }
}
""".trimEnd()

    val AGENT_CORRECTIONS = """
query { ListAgentCorrections(first: 50) { id createdAt kind said } }
""".trimEnd()

    val MEMORY_CHECK_QUESTION_FIELDS = """
id createdAt modifiedAt questionKind questionText expectedAnswer outdatedAnswer questionState isAnswerFiledAfter conversationId answeredAt
""".trimEnd()

    val MEMORY_CHECK_QUESTIONS = """
query { ListAgentEvaluationQuestions { ${MEMORY_CHECK_QUESTION_FIELDS} } }
""".trimEnd()

    val MEMORY_CHECK_RUNS = """
query (${'$'}first: Int) {
  ListAgentEvaluationRuns(first: ${'$'}first) {
    id startedAt finishedAt questionCount cost
    sourceScores { answerFrom answeredCount scorePercent scorePercentWithoutFiledAfter filedAfterCount }
  }
}
""".trimEnd()

    val MEMORY_CHECK_ANSWERS = """
query (${'$'}runId: String!) {
  ListAgentEvaluationAnswers(runId: ${'$'}runId) { id runId questionId answerFrom answerVerdict verdictReason answerText }
}
""".trimEnd()

    val UPDATE_MEMORY_CHECK_QUESTION = """
mutation (${'$'}questionId: String!, ${'$'}questionKind: String, ${'$'}questionText: String, ${'$'}expectedAnswer: String, ${'$'}outdatedAnswer: String, ${'$'}questionState: String) {
  UpdateAgentEvaluationQuestion(questionId: ${'$'}questionId, questionKind: ${'$'}questionKind, questionText: ${'$'}questionText, expectedAnswer: ${'$'}expectedAnswer, outdatedAnswer: ${'$'}outdatedAnswer, questionState: ${'$'}questionState) { ${MEMORY_CHECK_QUESTION_FIELDS} }
}
""".trimEnd()

    val START_MEMORY_CHECK = """
mutation (${'$'}speakFirstReason: String!) { SpeakFirstNow(speakFirstReason: ${'$'}speakFirstReason) }
""".trimEnd()

    val GRADE_MEMORY_NOW = """
mutation { EvaluateAgentMemoryNow { id startedAt } }
""".trimEnd()

    val CONNECTED_SERVERS = """
query { ListAgentServers { name transport auth oauthRedirect headless enabled status lastError tools } }
""".trimEnd()

    val CONNECT_SERVER = """
mutation (${'$'}server: String!, ${'$'}credential: String) {
  ConnectAgentServer(server: ${'$'}server, credential: ${'$'}credential) { name status lastError tools }
}
""".trimEnd()

    val DISCONNECT_SERVER = """
mutation (${'$'}server: String!) { DisconnectAgentServer(server: ${'$'}server) { name status } }
""".trimEnd()

    val SKILL_SECRETS = """
query { ListAgentSkillSecrets { skill key description set } }
""".trimEnd()

    val SET_SKILL_SECRET = """
mutation (${'$'}skill: String!, ${'$'}key: String!, ${'$'}value: String!) {
  SetAgentSkillSecret(skill: ${'$'}skill, key: ${'$'}key, value: ${'$'}value) { skill key set }
}
""".trimEnd()

    val CLEAR_SKILL_SECRET = """
mutation (${'$'}skill: String!, ${'$'}key: String!) { ClearAgentSkillSecret(skill: ${'$'}skill, key: ${'$'}key) }
""".trimEnd()

    val AGENT_REACHES = """
query {
  ListAgentReaches { kind name computerName isOnComputer }
  ReadAgentComputers { computers { name description } }
}
""".trimEnd()

    val SET_AGENT_REACH = """
mutation (${'$'}kind: String!, ${'$'}name: String!, ${'$'}computerName: String!) {
  SetAgentReach(kind: ${'$'}kind, name: ${'$'}name, computerName: ${'$'}computerName) { kind name computerName }
}
""".trimEnd()

    val CHAT_CHANNEL_FIELDS = """
kind hasToken botName linked linkedName linkCode enabled running lastError
""".trimEnd()

    val CHAT_CHANNELS = """
query { ListAgentChannels { ${CHAT_CHANNEL_FIELDS} } }
""".trimEnd()

    val SET_CHAT_CHANNEL = """
mutation (${'$'}kind: String!, ${'$'}token: String, ${'$'}enabled: Boolean) {
  SetAgentChannel(kind: ${'$'}kind, token: ${'$'}token, enabled: ${'$'}enabled) { ${CHAT_CHANNEL_FIELDS} }
}
""".trimEnd()

    val UNLINK_CHAT_CHANNEL = """
mutation (${'$'}kind: String!) { UnlinkAgentChannel(kind: ${'$'}kind) { kind } }
""".trimEnd()

    val REMOVE_CHAT_CHANNEL = """
mutation (${'$'}kind: String!) { RemoveAgentChannel(kind: ${'$'}kind) }
""".trimEnd()

    val BACKGROUND_WORK = """
query {
  ListAgentBackgroundWork(first: 20) { id workKind workStatus title errorMessage runIds createdAt finishedAt }
}
""".trimEnd()

    val BACKGROUND_WORK_RESULT = """
query (${'$'}id: String!) {
  GetAgentBackgroundWork(id: ${'$'}id) { id workKind workStatus title errorMessage resultText runIds createdAt finishedAt }
}
""".trimEnd()

    val STOP_BACKGROUND_WORK = """
mutation (${'$'}id: String!) { StopAgentBackgroundWork(id: ${'$'}id) { id workStatus } }
""".trimEnd()

    val AGENT_RUNS = """
query (${'$'}first: Int, ${'$'}offset: Int, ${'$'}jobId: String, ${'$'}kinds: [String!], ${'$'}query: String) {
  ListAgentRuns(first: ${'$'}first, offset: ${'$'}offset, jobId: ${'$'}jobId, kinds: ${'$'}kinds, query: ${'$'}query) {
    total
    runs { id title jobKind lastAt usage { promptTokens cacheReadTokens completionTokens cost } }
  }
}
""".trimEnd()

    val AGENT_REPLIES = """
query (${'$'}first: Int) {
  ListAgentReplies(first: ${'$'}first) {
    total
    replies { id createdAt mailboxId mailId status reason subject to sendAfter sentAt }
  }
}
""".trimEnd()

}
