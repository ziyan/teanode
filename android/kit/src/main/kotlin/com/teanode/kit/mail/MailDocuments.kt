package com.teanode.kit.mail


/** The GraphQL documents, the same text as the iPhone app's (ios/TeaNodeKit): the server's tests check that text against its schema. */
object MailDocuments {
    private const val D = "$"

    const val MAILBOXES = """query {
  ListMailboxes {
    mailbox {
      id name signatureText signatureHtml
      addresses { address domain domainId }
      autoReply { enabled from until subject text html sameDomainOnly }
      agent { granted draftReplies triage { enabled } }
      rules { name enabled stop conditions { field header operator value } actions { kind folderId address } }
    }
    folders { id mailboxId parentId name kind pinnedAt unread total noteCount }
    unread starredUnread priorityUnread maxMessageSize
  }
}"""

    const val MAIL_ITEM_FIELDS = """id folderId mailId seen flagged answered forwarded draft addedAt subscriptionId
insight { category priority needsReply summary actionItems notes model runId
  proposals { kind because status summary starts ends location allDay name organization title emails phones note contactId } }
mail {
  id from fromName sender subject recipients receivedAt size attachmentCount messageId listKey listName logoDomain
  authenticationResults { spf { result } dkims { result } dmarc { result passedAtForwarderDomain } spamFilter { score } }
}"""

    const val MAIL_THREADS = """query (${D}folderId: String, ${D}mailboxId: String, ${D}unread: Boolean, ${D}flagged: Boolean, ${D}priority: String, ${D}search: String,
  ${D}from: String, ${D}to: String, ${D}subject: String, ${D}since: DateTime, ${D}before: DateTime, ${D}hasAttachment: Boolean, ${D}first: Int, ${D}offset: Int) {
  ListMailboxThreads(folderId: ${D}folderId, mailboxId: ${D}mailboxId, unread: ${D}unread, flagged: ${D}flagged, priority: ${D}priority,
    search: ${D}search, from: ${D}from, to: ${D}to, subject: ${D}subject, since: ${D}since, before: ${D}before, hasAttachment: ${D}hasAttachment,
    first: ${D}first, offset: ${D}offset) {
    total
    threads { threadId count unread flagged participants itemIds hasDraft item { ${MAIL_ITEM_FIELDS} } }
  }
}"""

    const val MAIL_THREAD = """query (${D}itemId: String!) {
  GetMailboxThread(itemId: ${D}itemId) {
    threadId subject truncated
    summary { summary messageCount stale pending }
    heldReply { id status subject to text sendAfter draftItemId }
    items { folderId folderName folderKind item { ${MAIL_ITEM_FIELDS} } }
  }
}"""

    const val MAIL_ITEM = """query (${D}itemId: String!) { GetMailboxItem(itemId: ${D}itemId) { ${MAIL_ITEM_FIELDS} } }"""

    const val MAIL_CONTENT = """query (${D}mailId: String!) {
  GetMailContent(mailId: ${D}mailId) {
    mailId available text html hasRemoteContent imagesAllowed rawHeaders
    headers { key value }
    attachments { index filename contentType size inline }
  }
}"""

    const val SET_MAIL_FLAGS = """mutation (${D}itemIds: [String!]!, ${D}seen: Boolean, ${D}flagged: Boolean) {
  SetMailboxItemFlags(itemIds: ${D}itemIds, seen: ${D}seen, flagged: ${D}flagged)
}"""

    const val MOVE_MAIL = """mutation (${D}itemIds: [String!]!, ${D}folderId: String!) {
  MoveMailboxItems(itemIds: ${D}itemIds, folderId: ${D}folderId) { id folderId mailId }
}"""

    const val DELETE_MAIL = """mutation (${D}itemIds: [String!]!) { DeleteMailboxItems(itemIds: ${D}itemIds) }"""

    const val REPORT_JUNK = """mutation (${D}itemIds: [String!]!, ${D}notJunk: Boolean) { ReportMailboxJunk(itemIds: ${D}itemIds, notJunk: ${D}notJunk) }"""

    const val EMPTY_TRASH = """mutation (${D}mailboxId: String!) { EmptyMailboxTrash(mailboxId: ${D}mailboxId) }"""

    const val NEWEST_MAIL_ITEMS = """query (${D}folderId: String, ${D}first: Int) {
  ListMailboxItems(folderId: ${D}folderId, first: ${D}first) { items { id folderId mailId addedAt } }
}"""

    const val SORT_MAIL = """mutation (${D}itemIds: [String!]!, ${D}category: String, ${D}priority: String, ${D}needsReply: Boolean) {
  SortMailboxItems(itemIds: ${D}itemIds, category: ${D}category, priority: ${D}priority, needsReply: ${D}needsReply)
}"""

    const val AGENT_CATEGORIES = """query { ReadAgent { categories } }"""

    const val SHOW_MAIL_IMAGES = """mutation (${D}itemIds: [String!]!, ${D}show: Boolean!) { ShowMailboxItemImages(itemIds: ${D}itemIds, show: ${D}show) }"""

    const val CANCEL_AGENT_REPLY = """mutation (${D}replyId: String!) { CancelAgentReply(replyId: ${D}replyId) { id status } }"""

    const val MAIL_DRAFT = """query (${D}itemId: String!) {
  GetMailboxDraft(itemId: ${D}itemId) {
    itemId mailId from to cc bcc subject html text replyToItemId forwardItemId
    attachments { index filename contentType size inline }
  }
}"""

    const val SAVE_MAIL_DRAFT = """mutation (${D}mailboxId: String!, ${D}message: MailboxMessageParametersInput!) {
  SaveMailboxDraft(mailboxId: ${D}mailboxId, message: ${D}message) { id }
}"""

    const val DRAFT_REPLY = """mutation (${D}itemId: String!, ${D}instructions: String) { DraftReply(itemId: ${D}itemId, instructions: ${D}instructions) { text } }"""

    const val SEND_MAIL = """mutation (${D}mailboxId: String!, ${D}submissionId: String!, ${D}message: MailboxMessageParametersInput!) {
  SendMailboxMessage(mailboxId: ${D}mailboxId, submissionId: ${D}submissionId, message: ${D}message) {
    mail { id } item { id folderId }
  }
}"""

    const val MAIL_SUBMISSION = """query (${D}mailboxId: String!, ${D}submissionId: String!) {
  GetMailboxSubmission(mailboxId: ${D}mailboxId, submissionId: ${D}submissionId) { mailId sentItemId }
}"""

    const val CANCEL_MAIL_SUBMISSION = """mutation (${D}mailboxId: String!, ${D}submissionId: String!) {
  CancelMailboxSubmission(mailboxId: ${D}mailboxId, submissionId: ${D}submissionId) { mailId sentItemId }
}"""

    const val SUBSCRIPTION_FIELDS = """id key name from count unread lastAt lastItemId oneClick unsubscribe logoDomain
requestedAt method failed error stripped mutedAt imagesAt"""

    const val MAIL_SUBSCRIPTIONS = """query (${D}mailboxId: String!, ${D}first: Int, ${D}offset: Int, ${D}left: Boolean, ${D}matching: String) {
  ListMailboxSubscriptions(mailboxId: ${D}mailboxId, first: ${D}first, offset: ${D}offset, left: ${D}left, matching: ${D}matching) {
    total subscribed left
    subscriptions { ${SUBSCRIPTION_FIELDS} }
  }
}"""

    const val READ_MAIL_SUBSCRIPTION = """query (${D}mailboxId: String!, ${D}key: String!) {
  ReadMailboxSubscription(mailboxId: ${D}mailboxId, key: ${D}key) {
    threadId subject truncated
    items { folderId folderName folderKind item { ${MAIL_ITEM_FIELDS} } }
  }
}"""

    const val SHOW_SUBSCRIPTION_IMAGES = """mutation (${D}mailboxId: String!, ${D}key: String!, ${D}show: Boolean!) {
  ShowMailboxSubscriptionImages(mailboxId: ${D}mailboxId, key: ${D}key, show: ${D}show) { key imagesAt }
}"""

    const val MUTE_SUBSCRIPTION = """mutation (${D}mailboxId: String!, ${D}key: String!, ${D}muted: Boolean!) {
  MuteMailboxSubscription(mailboxId: ${D}mailboxId, key: ${D}key, muted: ${D}muted) { key mutedAt }
}"""

    const val UNSUBSCRIBE = """mutation (${D}mailboxId: String!, ${D}key: String!) {
  UnsubscribeMailboxSubscription(mailboxId: ${D}mailboxId, key: ${D}key) { key requestedAt method failed error }
}"""

    const val MAIL_INVITATION = """query (${D}itemId: String!) {
  GetMailInvitation(itemId: ${D}itemId) {
    id method summary location startsAt endsAt allDay cancelled organizer participation
    attendees { address name participation }
  }
}"""

    const val ANSWER_INVITATION = """mutation (${D}requestId: String!, ${D}itemId: String!, ${D}answer: String!) {
  AnswerMailInvitation(requestId: ${D}requestId, itemId: ${D}itemId, answer: ${D}answer) { id participation }
}"""

    const val SET_PROPOSAL_STATUS = """mutation (${D}itemId: String!, ${D}index: Int!, ${D}status: String!) {
  SetMailProposalStatus(itemId: ${D}itemId, index: ${D}index, status: ${D}status) { proposals { status } }
}"""


    /** What a retried invitation answer asks first: whether the server already has the answer's request. */
    const val CALENDAR_REQUEST = """query (${D}requestId: String!) { GetCalendarRequest(requestId: ${D}requestId) { requestId isMissing } }"""
}
