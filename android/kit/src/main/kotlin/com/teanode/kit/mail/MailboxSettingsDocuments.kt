package com.teanode.kit.mail


/** The GraphQL documents, the same text as the iPhone app's (ios/TeaNodeKit): the server's tests check that text against its schema. */
object MailboxSettingsDocuments {
    private const val D = "$"

    const val UPDATE_MAILBOX = """mutation (${D}mailboxId: String!, ${D}name: String, ${D}signatureText: String, ${D}signatureHtml: String, ${D}autoReply: MailboxAutoReplyInput) {
  UpdateMailbox(mailboxId: ${D}mailboxId, name: ${D}name, signatureText: ${D}signatureText, signatureHtml: ${D}signatureHtml, autoReply: ${D}autoReply) {
    mailbox { id }
  }
}"""

    const val UPDATE_MAILBOX_RULES = """mutation (${D}mailboxId: String!, ${D}rules: [MailboxRuleInput!]) {
  UpdateMailbox(mailboxId: ${D}mailboxId, rules: ${D}rules) {
    mailbox { id }
  }
}"""

    const val TEST_MAILBOX_RULES = """query (${D}mailboxId: String!, ${D}rules: [MailboxRuleInput!]!, ${D}first: Int) {
  TestMailboxRules(mailboxId: ${D}mailboxId, rules: ${D}rules, first: ${D}first) {
    matched
    item { id mail { from sender subject receivedAt } }
  }
}"""

    const val CREATE_MAIL_FOLDER = """mutation (${D}mailboxId: String!, ${D}name: String!, ${D}parentId: String) {
  CreateMailboxFolder(mailboxId: ${D}mailboxId, name: ${D}name, parentId: ${D}parentId) { id }
}"""

    const val UPDATE_MAIL_FOLDER = """mutation (${D}folderId: String!, ${D}name: String, ${D}parentId: String) {
  UpdateMailboxFolder(folderId: ${D}folderId, name: ${D}name, parentId: ${D}parentId) { id }
}"""

    const val PIN_MAIL_FOLDER = """mutation (${D}folderId: String!, ${D}pinned: Boolean!) { SetMailboxFolderPinned(folderId: ${D}folderId, pinned: ${D}pinned) { id } }"""

    const val DELETE_MAIL_FOLDER = """mutation (${D}folderId: String!) { DeleteMailboxFolder(folderId: ${D}folderId) }"""

    const val MAIL_APP_PASSWORDS = """query (${D}mailboxId: String!) {
  ListMailboxAppPasswords(mailboxId: ${D}mailboxId) { id name createdAt lastUsedAt }
  GetMailProgramSettings { imapHost imapPort imapsPort submissionHost submissionPort davHost }
}"""

    const val CREATE_MAIL_APP_PASSWORD = """mutation (${D}mailboxId: String!, ${D}name: String!) {
  CreateMailboxAppPassword(mailboxId: ${D}mailboxId, name: ${D}name) { password username appPassword { id name } }
}"""

    const val CREATE_SETUP_PROFILE = """mutation (${D}mailboxId: String!, ${D}deviceName: String!) {
  CreateMailboxSetupProfile(mailboxId: ${D}mailboxId, deviceName: ${D}deviceName) { downloadLink expiresAt hasCalendarsAndContacts }
}"""

    const val DELETE_MAIL_APP_PASSWORD = """mutation (${D}appPasswordId: String!) { DeleteMailboxAppPassword(appPasswordId: ${D}appPasswordId) }"""

}