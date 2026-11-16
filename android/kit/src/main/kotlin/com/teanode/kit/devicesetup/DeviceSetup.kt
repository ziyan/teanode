package com.teanode.kit.devicesetup

import com.teanode.kit.Connection
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeError
import com.teanode.kit.photos.ServerTime
import com.teanode.kit.services.SyncCredential
import com.teanode.kit.settings.trimmedText
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import java.time.Instant

/** One address of a mailbox. */
@Serializable
data class DeviceSetupAddress(val address: String, val domain: String = "", val domainId: String = "")

/** An owned mailbox available for a setup profile. */
@Serializable
data class DeviceSetupMailbox(val id: String, val name: String, val addresses: List<DeviceSetupAddress> = emptyList())

/** Only the services the current account can configure. */
@Serializable
data class DeviceSetupCapabilities(
    val canSetUpMail: Boolean,
    val canSetUpCalendar: Boolean,
    val canSetUpReminders: Boolean,
    val canSetUpContacts: Boolean,
    val mailboxes: List<DeviceSetupMailbox> = emptyList(),
) {
    val hasAvailableService: Boolean get() = canSetUpMail || canSetUpCalendar || canSetUpReminders || canSetUpContacts
}

/** The exact services the person selected. */
data class DeviceSetupSelection(
    val deviceName: String,
    val mailboxId: String? = null,
    val hasMailAccess: Boolean = false,
    val hasCalendarAccess: Boolean = false,
    val hasRemindersAccess: Boolean = false,
    val hasContactsAccess: Boolean = false,
) {
    fun retainingAvailableServices(capabilities: DeviceSetupCapabilities): DeviceSetupSelection {
        val isMailKept = capabilities.canSetUpMail && capabilities.mailboxes.any { mailbox -> mailbox.id == mailboxId }
        return copy(
            hasCalendarAccess = hasCalendarAccess && capabilities.canSetUpCalendar,
            hasRemindersAccess = hasRemindersAccess && capabilities.canSetUpReminders,
            hasContactsAccess = hasContactsAccess && capabilities.canSetUpContacts,
            mailboxId = if (isMailKept) mailboxId else null,
            hasMailAccess = if (isMailKept) hasMailAccess else false,
        )
    }

    fun canCreateProfile(capabilities: DeviceSetupCapabilities): Boolean {
        val trimmedName = trimmedText(deviceName)
        if (trimmedName.isEmpty() || trimmedName.toByteArray().size > 128) return false
        if (!(hasMailAccess || hasCalendarAccess || hasRemindersAccess || hasContactsAccess)) return false
        if (hasCalendarAccess && !capabilities.canSetUpCalendar) return false
        if (hasRemindersAccess && !capabilities.canSetUpReminders) return false
        if (hasContactsAccess && !capabilities.canSetUpContacts) return false
        return !hasMailAccess || (capabilities.canSetUpMail && capabilities.mailboxes.any { mailbox -> mailbox.id == mailboxId })
    }

    val variables: Map<String, JsonElement>
        get() = variablesOf(
            "deviceName" to trimmedText(deviceName), "mailboxId" to if (hasMailAccess) mailboxId else null,
            "hasMailAccess" to hasMailAccess, "hasCalendarAccess" to hasCalendarAccess, "hasRemindersAccess" to hasRemindersAccess,
            "hasContactsAccess" to hasContactsAccess, "isDownloadOnly" to true,
        )

    companion object {
        fun starting(deviceName: String, capabilities: DeviceSetupCapabilities, preselectedMailboxId: String? = null): DeviceSetupSelection {
            val mailboxId = capabilities.mailboxes.firstOrNull { mailbox -> mailbox.id == preselectedMailboxId }?.id
            return DeviceSetupSelection(
                deviceName, mailboxId, capabilities.canSetUpMail && mailboxId != null, capabilities.canSetUpCalendar,
                capabilities.canSetUpReminders, capabilities.canSetUpContacts,
            )
        }
    }
}

/** The password a mail app signs in with, made for a setup profile. */
@Serializable
data class DeviceSetupMailAppPassword(val id: String, val mailboxId: String, val name: String, val createdAt: String? = null, val lastUsedAt: String? = null)

/** A made profile, keeping mail and collection revocation metadata separate. */
@Serializable
data class CreatedDeviceSetupProfile(
    val downloadPath: String,
    val downloadLink: String,
    val expiresAt: String,
    val mailboxAppPassword: DeviceSetupMailAppPassword? = null,
    val syncCredential: SyncCredential? = null,
) {
    fun hasExpired(now: Instant = Instant.now()): Boolean {
        val expiration = ServerTime.instant(expiresAt) ?: return true
        return !expiration.isAfter(now)
    }

    /** The address to download the profile from, when it is the server's own and has not expired. */
    fun downloadAddress(server: ServerAddress): String {
        val address = validatedDownloadAddress(server)
        if (hasExpired()) throw TeaNodeError.UnreadableAnswer
        return address
    }

    internal fun validatedDownloadAddress(server: ServerAddress): String {
        val parsed = downloadLink.toHttpUrlOrNull() ?: throw TeaNodeError.UnreadableAnswer
        val isOwn = parsed.username.isEmpty() && parsed.password.isEmpty() && parsed.scheme == server.origin.scheme &&
            parsed.host == server.origin.host && parsed.port == server.origin.port && parsed.encodedPath == downloadPath &&
            parsed.query == null && parsed.fragment == null && downloadPath.startsWith("/api/v1/mailbox/profile/") &&
            downloadPath.endsWith(".mobileconfig") && ServerTime.instant(expiresAt) != null
        if (!isOwn) throw TeaNodeError.UnreadableAnswer
        return downloadLink
    }
}

@Serializable private class GetDeviceSetupCapabilitiesAnswer(val GetDeviceSetupCapabilities: DeviceSetupCapabilities)

@Serializable private class CreateDeviceSetupProfileAnswer(val CreateDeviceSetupProfile: CreatedDeviceSetupProfile)

suspend fun Connection.deviceSetupCapabilities(): DeviceSetupCapabilities =
    execute<GetDeviceSetupCapabilitiesAnswer>(DeviceSetupDocuments.DEVICE_SETUP_CAPABILITIES).GetDeviceSetupCapabilities

/** Makes the profile for what was selected, and checks the answer is what was asked for before anything is offered for download. */
suspend fun Connection.createDeviceSetupProfile(selection: DeviceSetupSelection, capabilities: DeviceSetupCapabilities): CreatedDeviceSetupProfile {
    if (!selection.canCreateProfile(capabilities)) throw TeaNodeError.UnreadableAnswer
    val profile = execute<CreateDeviceSetupProfileAnswer>(DeviceSetupDocuments.CREATE_DEVICE_SETUP_PROFILE, selection.variables).CreateDeviceSetupProfile
    profile.validatedDownloadAddress(account.server)
    if ((profile.mailboxAppPassword != null) != selection.hasMailAccess ||
        (profile.syncCredential != null) != (selection.hasCalendarAccess || selection.hasRemindersAccess || selection.hasContactsAccess)
    ) {
        throw TeaNodeError.UnreadableAnswer
    }
    if (selection.hasMailAccess && profile.mailboxAppPassword?.mailboxId != selection.mailboxId) throw TeaNodeError.UnreadableAnswer
    profile.syncCredential?.let { sync ->
        if (sync.hasCalendarAccess != selection.hasCalendarAccess || sync.hasRemindersAccess != selection.hasRemindersAccess ||
            sync.hasContactsAccess != selection.hasContactsAccess
        ) {
            throw TeaNodeError.UnreadableAnswer
        }
    }
    return profile
}
