package com.teanode.kit.agent

/** The GraphQL documents of Agent settings. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object AgentSettingsDocuments {
    val AGENT_SETTINGS_FIELDS = """
agent { id name enabled instructions language knowledgeLanguage askModel speechVoice operatorDisabledAt confirm unattendedAllowedRisks
  isMemoryCheckEnabled isIdeasEnabled
  voice { tone length greeting signoff }
  categories { name description } }
sources { mailboxId name addresses policy { granted draftReplies search research
  triage { enabled backfill replyExpectation }
  summaries { enabled minimumMessages style }
  autoReply { enabled guidance scope allow never categories when hours { from until days } holdMinutes dailyLimit }
  alerts { enabled } } }
collections { id name kind granted items }
allowed { enabled triage summaries draftReplies search research autoReply ask schedules browser connectedServers }
budget { used limit resetsAt cost costLimit currency }
choices timezone language knowledgeLanguage categories
""".trimEnd()

    val AGENT_SETTINGS = """
query { ReadAgent { ${AGENT_SETTINGS_FIELDS} } }
""".trimEnd()

    val UPDATE_AGENT_SETTINGS = """
mutation (${'$'}enabled: Boolean, ${'$'}name: String, ${'$'}instructions: String, ${'$'}language: String, ${'$'}knowledgeLanguage: String, ${'$'}voice: AgentVoiceInput,
  ${'$'}categories: [AgentCategoryInput!], ${'$'}confirm: [String!], ${'$'}unattendedAllowedRisks: [String!], ${'$'}askModel: String, ${'$'}speechVoice: String,
  ${'$'}isMemoryCheckEnabled: Boolean, ${'$'}isIdeasEnabled: Boolean, ${'$'}forget: Boolean) {
  UpdateAgent(enabled: ${'$'}enabled, name: ${'$'}name, instructions: ${'$'}instructions, language: ${'$'}language, knowledgeLanguage: ${'$'}knowledgeLanguage,
    voice: ${'$'}voice, categories: ${'$'}categories, confirm: ${'$'}confirm, unattendedAllowedRisks: ${'$'}unattendedAllowedRisks, askModel: ${'$'}askModel,
    speechVoice: ${'$'}speechVoice, isMemoryCheckEnabled: ${'$'}isMemoryCheckEnabled, isIdeasEnabled: ${'$'}isIdeasEnabled, forget: ${'$'}forget) {
    ${AGENT_SETTINGS_FIELDS}
  }
}
""".trimEnd()

    val GRANT_AGENT_MAILBOX = """
mutation (${'$'}mailboxId: String!, ${'$'}policy: AgentMailboxInput) {
  GrantAgentMailbox(mailboxId: ${'$'}mailboxId, policy: ${'$'}policy) { ${AGENT_SETTINGS_FIELDS} }
}
""".trimEnd()

    val REVOKE_AGENT_MAILBOX = """
mutation (${'$'}mailboxId: String!) { RevokeAgentMailbox(mailboxId: ${'$'}mailboxId) { ${AGENT_SETTINGS_FIELDS} } }
""".trimEnd()

    val GRANT_AGENT_COLLECTION = """
mutation (${'$'}kind: String!, ${'$'}id: String!, ${'$'}granted: Boolean!) {
  GrantAgentSource(kind: ${'$'}kind, id: ${'$'}id, granted: ${'$'}granted) { ${AGENT_SETTINGS_FIELDS} }
}
""".trimEnd()

    val AGENT_TOOLS = """
query { ListAgentTools { name family risk description confirms core actions } }
""".trimEnd()

    val SPEECH_PIECES = """
query (${'$'}messageText: String!) { ReadSpeechPieces(messageText: ${'$'}messageText) }
""".trimEnd()

    val SPEECH_VOICES = """
query { ReadAgentVoice { isVoiceAvailable sampleRate speechVoice speechVoices serverSpeechVoice } }
""".trimEnd()

}
