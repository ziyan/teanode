package com.teanode.kit.devicesetup

/** The GraphQL documents of device setup. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object DeviceSetupDocuments {
    val DEVICE_SETUP_CAPABILITIES = """
query { GetDeviceSetupCapabilities {
  canSetUpMail canSetUpCalendar canSetUpReminders canSetUpContacts
  mailboxes { id name addresses { address domain domainId } }
} }
""".trimEnd()

    val CREATE_DEVICE_SETUP_PROFILE = """
mutation (${'$'}deviceName: String!, ${'$'}mailboxId: String, ${'$'}hasMailAccess: Boolean!, ${'$'}hasCalendarAccess: Boolean!, ${'$'}hasRemindersAccess: Boolean!, ${'$'}hasContactsAccess: Boolean!, ${'$'}isDownloadOnly: Boolean) {
  CreateDeviceSetupProfile(deviceName: ${'$'}deviceName, mailboxId: ${'$'}mailboxId, hasMailAccess: ${'$'}hasMailAccess, hasCalendarAccess: ${'$'}hasCalendarAccess, hasRemindersAccess: ${'$'}hasRemindersAccess, hasContactsAccess: ${'$'}hasContactsAccess, isDownloadOnly: ${'$'}isDownloadOnly) {
    downloadPath downloadLink expiresAt
    mailboxAppPassword { id mailboxId name createdAt lastUsedAt }
    syncCredential { id credentialName hasCalendarAccess hasRemindersAccess hasContactsAccess createdAt lastUsedAt }
  }
}
""".trimEnd()

}
