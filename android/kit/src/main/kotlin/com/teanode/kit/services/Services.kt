package com.teanode.kit.services

import com.teanode.kit.Connection
import com.teanode.kit.ServiceCapabilities
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

/** Which services of the server are on, as the services page edits them. */
@Serializable
data class ServiceSelection(
    val isMailRoutingEnabled: Boolean,
    val isMailboxEnabled: Boolean,
    val isCalendarEnabled: Boolean,
    val isRemindersEnabled: Boolean,
    val isContactsEnabled: Boolean,
    val isPhotosEnabled: Boolean = false,
) {
    companion object {
        fun of(capabilities: ServiceCapabilities): ServiceSelection = ServiceSelection(
            capabilities.isMailRoutingEnabled, capabilities.isMailboxEnabled, capabilities.isCalendarEnabled, capabilities.isRemindersEnabled,
            capabilities.isContactsEnabled, capabilities.isPhotosEnabled,
        )
    }
}

/** A password a calendar, reminders or contacts app signs in with. */
@Serializable
data class SyncCredential(
    val id: String,
    val credentialName: String,
    val hasCalendarAccess: Boolean,
    val hasRemindersAccess: Boolean,
    val hasContactsAccess: Boolean,
    /** When the password was made. Absent only in an answer that predates it. */
    val createdAt: String? = null,
    /** When the password last signed anything in; null until it first does. */
    val lastUsedAt: String? = null,
)

@Serializable
data class CreatedSyncCredential(val username: String, val password: String, val principalPath: String)

@Serializable
data class CreatedSyncProfile(val downloadLink: String, val expiresAt: String)

@Serializable private class ServiceSetup(val selectedServices: ServiceSelection)

@Serializable private class GetServiceSetupAnswer(val GetServiceSetup: ServiceSetup)

@Serializable private class ListSyncCredentialsAnswer(val ListSyncCredentials: List<SyncCredential>)

@Serializable private class CreateSyncCredentialAnswer(val CreateSyncCredential: CreatedSyncCredential)

@Serializable private class CreateSyncSetupProfileAnswer(val CreateSyncSetupProfile: CreatedSyncProfile)

suspend fun Connection.serviceSelection(): ServiceSelection = execute<GetServiceSetupAnswer>(ServiceDocuments.SERVICE_SETUP).GetServiceSetup.selectedServices

suspend fun Connection.updateServices(selection: ServiceSelection) {
    execute<JsonObject>(
        ServiceDocuments.UPDATE_SERVICES,
        variablesOf(
            "isMailRoutingEnabled" to selection.isMailRoutingEnabled, "isMailboxEnabled" to selection.isMailboxEnabled,
            "isCalendarEnabled" to selection.isCalendarEnabled, "isRemindersEnabled" to selection.isRemindersEnabled,
            "isContactsEnabled" to selection.isContactsEnabled, "isPhotosEnabled" to selection.isPhotosEnabled,
        ),
    )
}

suspend fun Connection.syncCredentials(): List<SyncCredential> = execute<ListSyncCredentialsAnswer>(ServiceDocuments.SYNC_CREDENTIALS).ListSyncCredentials

suspend fun Connection.createSyncCredential(credentialName: String, hasCalendarAccess: Boolean, hasRemindersAccess: Boolean, hasContactsAccess: Boolean): CreatedSyncCredential =
    execute<CreateSyncCredentialAnswer>(
        ServiceDocuments.CREATE_SYNC_CREDENTIAL,
        variablesOf(
            "credentialName" to credentialName, "hasCalendarAccess" to hasCalendarAccess, "hasRemindersAccess" to hasRemindersAccess,
            "hasContactsAccess" to hasContactsAccess,
        ),
    ).CreateSyncCredential

suspend fun Connection.revokeSyncCredential(credentialId: String) {
    execute<JsonObject>(ServiceDocuments.REVOKE_SYNC_CREDENTIAL, variablesOf("credentialId" to credentialId))
}

suspend fun Connection.createSyncSetupProfile(credentialName: String, hasCalendarAccess: Boolean, hasRemindersAccess: Boolean, hasContactsAccess: Boolean): CreatedSyncProfile =
    execute<CreateSyncSetupProfileAnswer>(
        ServiceDocuments.CREATE_SYNC_SETUP_PROFILE,
        variablesOf(
            "credentialName" to credentialName, "hasCalendarAccess" to hasCalendarAccess, "hasRemindersAccess" to hasRemindersAccess,
            "hasContactsAccess" to hasContactsAccess,
        ),
    ).CreateSyncSetupProfile
