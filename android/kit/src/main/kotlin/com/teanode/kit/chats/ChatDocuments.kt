package com.teanode.kit.chats

/**
 * The GraphQL Chats sends. These documents are the same text as the iPhone app's (ios/TeaNodeKit Operations.swift,
 * Chats.swift, Waiting.swift, LinkPreview.swift): the server's tests check the iPhone app's documents against its
 * schema, so a document copied from there is checked too.
 */
object ChatDocuments {
    /** What every read of a chat answers with. Written out in full, since the server's test of the app's documents fills in one level of shared fields. */
    private const val CHAT_FIELDS = """
        chatId chatKind circleId title isCircleChat unreadCount notificationLevel isRemoved
        members { userId username name avatarUrl avatarColorIndex memberRole isDeletedAccount isRemoved readThroughMessageId isAgentReading isAgentAskableByOthers agentId agentName isAgentActive }
        """

    /** What every read of a message answers with, in every conversation. */
    private const val MESSAGE_FIELDS = """
        id createdAt role content name toolCallId
        attachments { id name contentType size isRemoved audioDurationMS audioWaveform }
        references { itemId threadId subject from path name recordingId recordingTitle photoId photoTitle financeTransactionId postedOn amount currencyCode merchantName description agentMessageId quotedRole quotedText }
        reactions { emoji reactorKind reactedAt reactorUserId reactorName }
        authorKind authorUserId authorName authorOwnerName authorAvatarUrl authorAvatarColorIndex authorAgentId agentRunId isMine isAgentWork editedAt isDeleted
        mentionedUserIds isAllMentioned isVoiceMessage transcriptText transcriptState
        callId call { id startedByUserId startedAt answeredAt endedAt endReason transcriptionAskedByUserId transcriptState summaryMessageId members { userId ringState joinedAt leftAt isVideoOn } }
        """

    const val AGENT = """
        query { ReadAgent { agent { id enabled name } } }
        """

    const val CONVERSATIONS = """
        query (${'$'}archived: Boolean, ${'$'}query: String) {
          ListAgentConversations(archived: ${'$'}archived, query: ${'$'}query) { id kind jobKind title summary lastAt archivedAt goal goalState goalNote chat { $CHAT_FIELDS } }
        }
        """

    const val CONVERSATION = """
        query (${'$'}conversationId: String, ${'$'}first: Int, ${'$'}offset: Int) {
          ReadAgentConversation(conversationId: ${'$'}conversationId, first: ${'$'}first, offset: ${'$'}offset) {
            conversation { id kind jobKind title summary lastAt archivedAt goal goalState goalNote chat { $CHAT_FIELDS } }
            actingAs
            messages {
              $MESSAGE_FIELDS
              toolCalls { id name arguments }
              usage { promptTokens completionTokens cost }
            }
            total
            todos { id text doneAt }
          }
          ListAgentInteractions(conversationId: ${'$'}conversationId) { runId callId interactionKind toolName interactionText interactionChoices toolRisk createdAt }
        }
        """

    const val ASK = """
        mutation (${'$'}conversationId: String, ${'$'}message: String!, ${'$'}surface: String, ${'$'}attachmentIds: [String!], ${'$'}references: [AgentReferenceInput!], ${'$'}drawerId: String, ${'$'}interruptedAnswer: InterruptedAnswerInput, ${'$'}addressedAgentIds: [String!], ${'$'}mentionedUserIds: [String!], ${'$'}isAllMentioned: Boolean, ${'$'}voiceMessageAttachmentId: String) {
          AskAgent(conversationId: ${'$'}conversationId, message: ${'$'}message, surface: ${'$'}surface, attachmentIds: ${'$'}attachmentIds, references: ${'$'}references, drawerId: ${'$'}drawerId, interruptedAnswer: ${'$'}interruptedAnswer, addressedAgentIds: ${'$'}addressedAgentIds, mentionedUserIds: ${'$'}mentionedUserIds, isAllMentioned: ${'$'}isAllMentioned, voiceMessageAttachmentId: ${'$'}voiceMessageAttachmentId) { runId conversationId }
        }
        """

    const val VOICE = """
        query { ReadAgentVoice { isVoiceAvailable sampleRate isVoiceMessageAvailable } }
        """

    const val UPDATE_CONVERSATION = """
        mutation (${'$'}conversationId: String!, ${'$'}title: String, ${'$'}archived: Boolean) {
          UpdateAgentConversation(conversationId: ${'$'}conversationId, title: ${'$'}title, archived: ${'$'}archived) { id }
        }
        """

    const val DELETE_CONVERSATION = """
        mutation (${'$'}conversationId: String!) { DeleteAgentConversation(conversationId: ${'$'}conversationId) }
        """

    const val SET_MAIN_CONVERSATION = """
        mutation (${'$'}conversationId: String) { SetAgentMainConversation(conversationId: ${'$'}conversationId) { id } }
        """

    const val IDEAS = """
        query (${'$'}ideaStatuses: [String!], ${'$'}language: String) {
          ListAgentIdeas(ideaStatuses: ${'$'}ideaStatuses, language: ${'$'}language) { ideas { id emoji headline body openingRequest } }
        }
        """

    const val START_IDEA = """
        mutation (${'$'}ideaId: String!, ${'$'}conversationId: String, ${'$'}language: String) {
          StartAgentIdea(ideaId: ${'$'}ideaId, conversationId: ${'$'}conversationId, language: ${'$'}language) { conversation { id } openingRequest }
        }
        """

    const val DEVICES = """
        query {
          ReadAgentTab { attached title url }
          ReadAgentComputers { computers { name system description } }
          ReadAgent { budget { used limit resetsAt cost costLimit currency } timezone }
        }
        """

    const val BACKGROUND_COMMANDS = """
        query (${'$'}conversationId: String) {
          ListAgentBackgroundCommands(conversationId: ${'$'}conversationId) {
            computer id command directory conversationId startedAt endedAt isRunning exitCode stopReason
          }
        }
        """

    const val BACKGROUND_COMMAND = """
        query (${'$'}computer: String!, ${'$'}id: String!) {
          ReadAgentBackgroundCommand(computer: ${'$'}computer, id: ${'$'}id) {
            computer id command directory conversationId startedAt endedAt isRunning exitCode stopReason
            stdout stderr isStdoutTruncated isStderrTruncated stdoutByteCount stderrByteCount
          }
        }
        """

    const val STOP_BACKGROUND_COMMAND = """
        mutation (${'$'}computer: String!, ${'$'}id: String!) {
          StopAgentBackgroundCommand(computer: ${'$'}computer, id: ${'$'}id) { id isRunning stopReason exitCode endedAt }
        }
        """

    const val SHARE_ATTACHMENT = """
        query (${'$'}attachmentId: String!) { ShareAgentAttachment(attachmentId: ${'$'}attachmentId) }
        """

    const val MARK_IDEAS_SHOWN = """
        mutation (${'$'}ideaIds: [String!]!) { MarkAgentIdeasShown(ideaIds: ${'$'}ideaIds) }
        """

    const val START_CONVERSATION = """
        mutation (${'$'}title: String) { StartAgentConversation(title: ${'$'}title) { id kind title summary lastAt } }
        """

    const val RESOLVE_CONFIRMATION = """
        mutation (${'$'}runId: String!, ${'$'}callId: String!, ${'$'}approve: Boolean!) { ResolveAgentConfirmation(runId: ${'$'}runId, callId: ${'$'}callId, approve: ${'$'}approve) }
        """

    const val ANSWER_QUESTION = """
        mutation (${'$'}runId: String!, ${'$'}callId: String!, ${'$'}answer: String!) { AnswerAgentQuestion(runId: ${'$'}runId, callId: ${'$'}callId, answer: ${'$'}answer) }
        """

    const val STOP_RUN = """
        mutation (${'$'}runId: String!) { StopAgentRun(runId: ${'$'}runId) }
        """

    const val ANSWER_LOCATION = """
        mutation (${'$'}runId: String!, ${'$'}callId: String!, ${'$'}latitudeDegrees: Float, ${'$'}longitudeDegrees: Float, ${'$'}accuracyMeters: Float, ${'$'}measuredAt: String, ${'$'}errorMessage: String) {
          AnswerAgentLocation(runId: ${'$'}runId, callId: ${'$'}callId, latitudeDegrees: ${'$'}latitudeDegrees, longitudeDegrees: ${'$'}longitudeDegrees, accuracyMeters: ${'$'}accuracyMeters, measuredAt: ${'$'}measuredAt, errorMessage: ${'$'}errorMessage)
        }
        """

    const val REACT_TO_MESSAGE = """
        mutation (${'$'}messageId: String!, ${'$'}emoji: String!) { ReactToAgentMessage(messageId: ${'$'}messageId, emoji: ${'$'}emoji) }
        """

    const val REPORT_PRESENCE = """
        mutation (${'$'}isVisible: Boolean!, ${'$'}idleSeconds: Int!, ${'$'}reporterId: String, ${'$'}conversationId: String) {
          ReportAgentPresence(isVisible: ${'$'}isVisible, idleSeconds: ${'$'}idleSeconds, reporterId: ${'$'}reporterId, conversationId: ${'$'}conversationId)
        }
        """

    const val ANSWER_CODING_QUESTION = """
        mutation (${'$'}computer: String, ${'$'}paneId: String!, ${'$'}questionFingerprint: String!, ${'$'}optionNumbers: [Int!], ${'$'}optionLabels: [String!], ${'$'}freeText: String) {
          AnswerAgentHerdrQuestion(computer: ${'$'}computer, paneId: ${'$'}paneId, questionFingerprint: ${'$'}questionFingerprint, optionNumbers: ${'$'}optionNumbers, optionLabels: ${'$'}optionLabels, freeText: ${'$'}freeText) { isAnswerAccepted answeredWith }
        }
        """

    const val READ_RUN = """
        query (${'$'}runId: String!, ${'$'}after: Int, ${'$'}wait: Int) {
          ReadAgentRun(runId: ${'$'}runId, after: ${'$'}after, wait: ${'$'}wait) { done events { kind runId sequence at text tool callId arguments risk note noteKind noteDetail error drawerId } }
        }
        """

    const val CONVERSATION_EVENTS = """
        subscription (${'$'}conversationId: String!) {
          AgentConversationEvents(conversationId: ${'$'}conversationId) {
            kind runId sequence at text tool callId arguments risk note noteKind noteDetail error drawerId messageId emoji reactorKind
            reactorUserId reactorName userId name authorAgentId authorOwnerName isMessageCut
            message { $MESSAGE_FIELDS }
          }
        }
        """

    const val CHAT_PEOPLE = """
        query { ListCirclePeople { userId username name avatarUrl avatarColorIndex } }
        """

    const val OPEN_DIRECT_CHAT = """
        mutation (${'$'}userId: String!) { OpenDirectChat(userId: ${'$'}userId) { id } }
        """

    const val CREATE_GROUP_CHAT = """
        mutation (${'$'}title: String, ${'$'}userIds: [String!]!) { CreateGroupChat(title: ${'$'}title, userIds: ${'$'}userIds) { id } }
        """

    const val RENAME_CHAT = """
        mutation (${'$'}conversationId: String!, ${'$'}title: String!) { RenameChat(conversationId: ${'$'}conversationId, title: ${'$'}title) { id } }
        """

    const val ADD_CHAT_MEMBERS = """
        mutation (${'$'}conversationId: String!, ${'$'}userIds: [String!]!) { AddChatMembers(conversationId: ${'$'}conversationId, userIds: ${'$'}userIds) { id } }
        """

    const val REMOVE_CHAT_MEMBER = """
        mutation (${'$'}conversationId: String!, ${'$'}userId: String!) { RemoveChatMember(conversationId: ${'$'}conversationId, userId: ${'$'}userId) { id } }
        """

    const val LEAVE_CHAT = """
        mutation (${'$'}conversationId: String!) { LeaveChat(conversationId: ${'$'}conversationId) }
        """

    const val EDIT_CHAT_MESSAGE = """
        mutation (${'$'}messageId: String!, ${'$'}message: String!) { EditChatMessage(messageId: ${'$'}messageId, message: ${'$'}message) { id } }
        """

    const val DELETE_CHAT_MESSAGE = """
        mutation (${'$'}messageId: String!) { DeleteChatMessage(messageId: ${'$'}messageId) }
        """

    const val TRANSCRIBE_CHAT_VOICE_MESSAGE = """
        mutation (${'$'}messageId: String!) { TranscribeChatVoiceMessage(messageId: ${'$'}messageId) { id transcriptState transcriptText } }
        """

    const val MARK_CHAT_READ = """
        mutation (${'$'}conversationId: String!, ${'$'}throughMessageId: String!) {
          MarkChatRead(conversationId: ${'$'}conversationId, throughMessageId: ${'$'}throughMessageId)
        }
        """

    const val SET_CHAT_NOTIFICATION_LEVEL = """
        mutation (${'$'}conversationId: String!, ${'$'}notificationLevel: String!) {
          SetChatNotificationLevel(conversationId: ${'$'}conversationId, notificationLevel: ${'$'}notificationLevel) { id }
        }
        """

    const val SET_CHAT_TYPING = """
        mutation (${'$'}conversationId: String!) { SetChatTyping(conversationId: ${'$'}conversationId) }
        """

    const val SET_CHAT_AGENT_READING = """
        mutation (${'$'}conversationId: String!, ${'$'}isAgentReading: Boolean!) {
          SetChatAgentReading(conversationId: ${'$'}conversationId, isAgentReading: ${'$'}isAgentReading) { id }
        }
        """

    const val SET_CHAT_AGENT_ASKABLE_BY_OTHERS = """
        mutation (${'$'}conversationId: String!, ${'$'}isAgentAskableByOthers: Boolean!) {
          SetChatAgentAskableByOthers(conversationId: ${'$'}conversationId, isAgentAskableByOthers: ${'$'}isAgentAskableByOthers) { id }
        }
        """

    const val WAITING = """
        query (${'$'}first: Int) {
          ReadAgentWaiting(first: ${'$'}first) { waitingCount interactions { id conversationId interactionKind interactionText createdAt } }
        }
        """

    const val READ_LINK_PREVIEWS = """
        query (${'$'}conversationId: String!, ${'$'}urls: [String!]!) {
          ReadLinkPreviews(conversationId: ${'$'}conversationId, urls: ${'$'}urls) {
            url cleanedUrl displayText isAvailable title description siteName imageUrl imageWidthPixels imageHeightPixels
          }
        }
        """
}
