package com.teanode.kit.services

/** The GraphQL documents of the server's services. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object ServiceDocuments {
    val SERVICE_CAPABILITIES = """
query { GetServiceCapabilities { isMailRoutingEnabled isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled isPhotosEnabled isAssistantEnabled hasPendingServiceRestart hasCompletedSetup } }
""".trimEnd()

    val SERVICE_CAPABILITIES_BEFORE_PHOTOS = """
query { GetServiceCapabilities { isMailRoutingEnabled isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled isAssistantEnabled hasPendingServiceRestart hasCompletedSetup } }
""".trimEnd()

    val SERVICE_SETUP = """
query { GetServiceSetup { selectedServices { isMailRoutingEnabled isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled isPhotosEnabled } } }
""".trimEnd()

    val UPDATE_SERVICES = """
mutation (${'$'}isMailRoutingEnabled: Boolean, ${'$'}isMailboxEnabled: Boolean, ${'$'}isCalendarEnabled: Boolean, ${'$'}isRemindersEnabled: Boolean, ${'$'}isContactsEnabled: Boolean, ${'$'}isPhotosEnabled: Boolean) {
  UpdateServices(isMailRoutingEnabled: ${'$'}isMailRoutingEnabled, isMailboxEnabled: ${'$'}isMailboxEnabled, isCalendarEnabled: ${'$'}isCalendarEnabled, isRemindersEnabled: ${'$'}isRemindersEnabled, isContactsEnabled: ${'$'}isContactsEnabled, isPhotosEnabled: ${'$'}isPhotosEnabled) { isMailRoutingEnabled isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled isPhotosEnabled isAssistantEnabled hasPendingServiceRestart hasCompletedSetup }
}
""".trimEnd()

    val SYNC_CREDENTIALS = """
query { ListSyncCredentials { id credentialName hasCalendarAccess hasRemindersAccess hasContactsAccess createdAt lastUsedAt } }
""".trimEnd()

    val CREATE_SYNC_CREDENTIAL = """
mutation (${'$'}credentialName: String!, ${'$'}hasCalendarAccess: Boolean!, ${'$'}hasRemindersAccess: Boolean!, ${'$'}hasContactsAccess: Boolean!) {
  CreateSyncCredential(credentialName: ${'$'}credentialName, hasCalendarAccess: ${'$'}hasCalendarAccess, hasRemindersAccess: ${'$'}hasRemindersAccess, hasContactsAccess: ${'$'}hasContactsAccess) { username password principalPath }
}
""".trimEnd()

    val REVOKE_SYNC_CREDENTIAL = """
mutation (${'$'}credentialId: String!) { RevokeSyncCredential(credentialId: ${'$'}credentialId) }
""".trimEnd()

    val CREATE_SYNC_SETUP_PROFILE = """
mutation (${'$'}credentialName: String!, ${'$'}hasCalendarAccess: Boolean!, ${'$'}hasRemindersAccess: Boolean!, ${'$'}hasContactsAccess: Boolean!) {
  CreateSyncSetupProfile(credentialName: ${'$'}credentialName, hasCalendarAccess: ${'$'}hasCalendarAccess, hasRemindersAccess: ${'$'}hasRemindersAccess, hasContactsAccess: ${'$'}hasContactsAccess) { downloadLink expiresAt }
}
""".trimEnd()

}
