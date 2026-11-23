package com.teanode.kit

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * The GraphQL the app sends. These documents are the same text as the iPhone app's (ios/TeaNodeKit): the server's
 * tests check the iPhone app's documents against its schema, so a document copied from there is checked too.
 */
object Documents {
    const val SESSION = """query { GetSession { username } }"""

    const val ACCESS_SESSION = """query { GetSession { userId permissions { everywhere } impersonatorUsername } }"""

    const val SERVICE_CAPABILITIES =
        """query { GetServiceCapabilities { isMailRoutingEnabled isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled isPhotosEnabled isAssistantEnabled hasPendingServiceRestart hasCompletedSetup } }"""

    /** What a server that predates Photos is asked, since it refuses a field it does not have. */
    const val SERVICE_CAPABILITIES_BEFORE_PHOTOS =
        """query { GetServiceCapabilities { isMailRoutingEnabled isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled isAssistantEnabled hasPendingServiceRestart hasCompletedSetup } }"""
}

/** ServiceCapabilities is the hosted functionality running on one server. */
@Serializable
data class ServiceCapabilities(
    val isMailRoutingEnabled: Boolean,
    val isMailboxEnabled: Boolean,
    val isCalendarEnabled: Boolean,
    val isRemindersEnabled: Boolean,
    val isContactsEnabled: Boolean,
    /** Whether the server keeps a photo library. False in an answer from a server that predates Photos. */
    val isPhotosEnabled: Boolean = false,
    val isAssistantEnabled: Boolean,
    val hasPendingServiceRestart: Boolean,
    val hasCompletedSetup: Boolean,
) {
    companion object {
        /** What a server that predates the capabilities question is taken to offer. */
        val legacy = ServiceCapabilities(
            isMailRoutingEnabled = true, isMailboxEnabled = true, isCalendarEnabled = true, isRemindersEnabled = true,
            isContactsEnabled = true, isAssistantEnabled = true, hasPendingServiceRestart = false, hasCompletedSetup = true,
        )

        /** Offers nothing; what is drawn until the server has said. */
        val unavailable = ServiceCapabilities(
            isMailRoutingEnabled = false, isMailboxEnabled = false, isCalendarEnabled = false, isRemindersEnabled = false,
            isContactsEnabled = false, isAssistantEnabled = false, hasPendingServiceRestart = false, hasCompletedSetup = false,
        )
    }
}

/** AccessSession is who the token signs in as, and what they may do. */
@Serializable
data class AccessSession(
    val userId: String? = null,
    val permissions: AccessSessionPermissions? = null,
    /** The operator signed in as this account, when somebody is. */
    val impersonatorUsername: String? = null,
) {
    /** The part of the person's permissions the app reads: the ones held everywhere. */
    @Serializable
    data class AccessSessionPermissions(val everywhere: List<String>)

    /** Whether the person holds this permission everywhere, such as user:manage. */
    fun holds(permissionKey: String): Boolean = permissions?.everywhere?.contains(permissionKey) ?: false

    /** Whether the person may keep a photo library and back a device up to it. */
    val canUsePhotos: Boolean get() = holds("photos:use")
}

@Serializable
private class SessionAnswer(@SerialName("GetSession") val session: SessionFields)

@Serializable
private class SessionFields(val username: String? = null)

@Serializable
private class AccessSessionAnswer(@SerialName("GetSession") val session: AccessSession)

@Serializable
private class ServiceCapabilitiesAnswer(@SerialName("GetServiceCapabilities") val capabilities: ServiceCapabilities)

/** Who the token signs in as. */
suspend fun Connection.username(): String =
    execute(Documents.SESSION, SessionAnswer.serializer()).session.username.orEmpty()

/** Who is signed in, and what they may do. */
suspend fun Connection.accessSession(): AccessSession =
    execute(Documents.ACCESS_SESSION, AccessSessionAnswer.serializer()).session

/** What the server offers. A server that predates a question is asked the older one, or taken to offer everything. */
suspend fun Connection.serviceCapabilities(): ServiceCapabilities {
    try {
        try {
            return execute(Documents.SERVICE_CAPABILITIES, ServiceCapabilitiesAnswer.serializer()).capabilities
        } catch (error: TeaNodeError.Refused) {
            if (!(error.reason.contains("Cannot query field") && error.reason.contains("isPhotosEnabled"))) throw error
            return execute(Documents.SERVICE_CAPABILITIES_BEFORE_PHOTOS, ServiceCapabilitiesAnswer.serializer()).capabilities
        }
    } catch (error: TeaNodeError.Refused) {
        if (error.reason.contains("Cannot query field") && error.reason.contains("GetServiceCapabilities")) return ServiceCapabilities.legacy
        throw error
    }
}
