package com.teanode.kit.account

import com.teanode.kit.Connection
import com.teanode.kit.circles.PersonPronoun
import com.teanode.kit.photos.ServerTime
import com.teanode.kit.settings.trimmedText
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import java.time.Instant
import java.time.ZoneOffset
import java.util.Base64

/**
 * The units the person reads measurements in. Automatic follows the phone's language and region and is sent as empty;
 * metric is degrees Celsius and kilometers; us is degrees Fahrenheit and miles.
 */
enum class MeasurementSystem(val serverName: String) {
    Automatic(""), Metric("metric"), Us("us");

    companion object {
        fun fromServerName(serverName: String?): MeasurementSystem = entries.firstOrNull { system -> system.serverName == (serverName ?: "") } ?: Automatic
    }
}

/** Whether the time zone follows the browser the dashboard is open in, or stays the one chosen. */
enum class TimezoneMode(val serverName: String) {
    Auto("auto"), Fixed("fixed");

    companion object {
        fun fromServerName(serverName: String?): TimezoneMode = entries.firstOrNull { mode -> mode.serverName == serverName } ?: Auto
    }
}

/** The person's own account: what to call them, what they sign in with, where the server writes to them, and their time zone. */
@Serializable
data class AccountProfile(
    val id: String,
    val username: String,
    val name: String? = null,
    val email: String? = null,
    val timezone: String? = null,
    /** auto or fixed; empty is auto. */
    val timezoneMode: String? = null,
    /** Where the person's picture is, a path to read with the app's token; null when they have none. */
    val avatarUrl: String? = null,
    /** Which of the twelve colors their initial is drawn on without a picture. */
    val avatarColorIndex: Int? = null,
    /** How the person says they are described: he, she or they; empty or absent until they say. */
    val pronoun: String? = null,
    /** The units the person reads measurements in: metric, us, or empty or absent to follow the phone's language and region. */
    val measurementSystem: String? = null,
) {
    val chosenMeasurementSystem: MeasurementSystem get() = MeasurementSystem.fromServerName(measurementSystem)

    val chosenPronoun: PersonPronoun get() = PersonPronoun.fromServerName(pronoun)

    /** What the person is called on screen: their name, or the username when they have not given one. */
    val displayName: String get() = name?.takeIf { text -> text.isNotEmpty() } ?: username

    val chosenTimezoneMode: TimezoneMode get() = TimezoneMode.fromServerName(timezoneMode)
}

/** The profile as its form holds it, and what saving it sends. */
data class ProfileDraft(
    val profile: AccountProfile,
    val name: String = profile.name ?: "",
    val username: String = profile.username,
    val email: String = profile.email ?: "",
    val timezone: String = profile.timezone ?: "",
    val timezoneMode: TimezoneMode = profile.chosenTimezoneMode,
    val pronoun: PersonPronoun = profile.chosenPronoun,
    val measurementSystem: MeasurementSystem = profile.chosenMeasurementSystem,
) {
    /** Whether saving changes what the person signs in with, which is asked about first. */
    val isRenaming: Boolean get() = trimmedText(username) != profile.username

    val hasChanges: Boolean
        get() = isRenaming || trimmedText(name) != (profile.name ?: "") || trimmedText(email) != (profile.email ?: "") ||
            trimmedText(timezone) != (profile.timezone ?: "") || timezoneMode != profile.chosenTimezoneMode ||
            pronoun != profile.chosenPronoun || measurementSystem != profile.chosenMeasurementSystem

    val canSave: Boolean get() = hasChanges && trimmedText(username).isNotEmpty()

    /** Chooses a mode. A fixed zone starts from the one the account has, or the phone's when it has none, rather than from nothing. */
    fun choosing(mode: TimezoneMode, deviceTimezone: String): ProfileDraft {
        val startingZone = if (mode == TimezoneMode.Fixed && timezone.isEmpty()) {
            if ((profile.timezone ?: "").isEmpty()) deviceTimezone else profile.timezone ?: ""
        } else {
            timezone
        }
        return copy(timezoneMode = mode, timezone = startingZone)
    }

    /** As the server takes it: trimmed, and no zone of its own when it follows the browser. */
    val variables: Map<String, JsonElement>
        get() = variablesOf(
            "userId" to profile.id, "name" to trimmedText(name), "email" to trimmedText(email), "username" to trimmedText(username),
            "timezone" to if (timezoneMode == TimezoneMode.Fixed) trimmedText(timezone) else "", "timezoneMode" to timezoneMode.serverName,
            "pronoun" to pronoun.serverName, "measurementSystem" to measurementSystem.serverName,
        )
}

/** The change-password form: the password in use and the new one, typed twice. */
data class PasswordChangeDraft(val currentPassword: String = "", val newPassword: String = "", val newPasswordAgain: String = "") {
    /** Whether the second typing differs from the first, once there is one. */
    val doesConfirmationDiffer: Boolean get() = newPasswordAgain.isNotEmpty() && newPassword != newPasswordAgain

    val canChange: Boolean get() = currentPassword.isNotEmpty() && newPassword.isNotEmpty() && newPassword == newPasswordAgain
}

/** Whether notifications reach a phone. */
enum class PhoneNotificationState { NotRegistered, LastFailed, Notified }

/** One phone the app is connected on, as the Phones list shows it. */
@Serializable
data class PairedPhone(
    val id: String,
    val phoneName: String,
    val phoneModel: String,
    /** The model as people say it; the identifier itself when the server does not know it. Absent from a server that predates it. */
    val phoneModelName: String? = null,
    /** The kinds it is told about; the server lists every kind when it chose none. */
    val notificationKinds: List<String> = emptyList(),
    val canBeNotified: Boolean = false,
    val lastSeenAt: String? = null,
    val lastDeliveredAt: String? = null,
    val lastDeliveryError: String = "",
) {
    /** What the Phones list shows for the model. */
    val modelDisplayName: String get() = phoneModelName?.takeIf { name -> name.isNotEmpty() } ?: phoneModel

    val notificationState: PhoneNotificationState
        get() = when {
            !canBeNotified -> PhoneNotificationState.NotRegistered
            lastDeliveryError.isNotEmpty() -> PhoneNotificationState.LastFailed
            else -> PhoneNotificationState.Notified
        }
}

/** The person's phones, and what the server offers them. */
@Serializable
data class PairedPhoneList(
    val phones: List<PairedPhone>,
    /** Whether this server sends notifications to phones at all. */
    val isPushEnabled: Boolean,
    /** Every kind a phone may choose, in order. */
    val notificationKinds: List<String>,
)

/** One phone's name and notification kinds as its form holds them. */
data class PairedPhoneDraft(
    val phone: PairedPhone,
    val phoneName: String = phone.phoneName,
    val notificationKinds: Set<String> = phone.notificationKinds.toSet(),
) {
    val hasNameChanged: Boolean get() = trimmedText(phoneName) != phone.phoneName

    val hasNotificationKindsChanged: Boolean get() = notificationKinds != phone.notificationKinds.toSet()

    val canSave: Boolean get() = trimmedText(phoneName).isNotEmpty() && (hasNameChanged || hasNotificationKindsChanged)

    /** The chosen kinds in the order the server lists every kind. */
    fun orderedNotificationKinds(allNotificationKinds: List<String>): List<String> = allNotificationKinds.filter { kind -> notificationKinds.contains(kind) }
}

/** An authenticator that can sign in as the person. */
@Serializable
data class Passkey(
    val id: String,
    val name: String,
    val createdAt: String? = null,
    val usedAt: String? = null,
    /** The address it was last used from. */
    val ip: String? = null,
    val backupEligible: Boolean? = null,
    /** Whether it is synced to the person's other devices, rather than kept on one. */
    val backupState: Boolean? = null,
    val transports: List<String>? = null,
) {
    val createdInstant: Instant? get() = instantOf(createdAt)

    /** When it last signed in; null when it never has. */
    val lastUsedInstant: Instant? get() = instantOf(usedAt)

    val isSynced: Boolean get() = backupState == true

    private fun instantOf(text: String?): Instant? {
        // The server writes a time never set as the first year, which is no time at all.
        val instant = ServerTime.instant(text) ?: return null
        return instant.takeIf { found -> found.atZone(ZoneOffset.UTC).year > 1 }
    }
}

/** Whether this server offers passkeys, and how many one account may have. */
@Serializable
data class PasskeyPolicy(val enabled: Boolean, val maximumPerUser: Int)

@Serializable private class GetCurrentUserAnswer(val GetCurrentUser: AccountProfile? = null)

@Serializable private class UpdateUserAnswer(val UpdateUser: AccountProfile)

@Serializable private class SetUserAvatarAnswer(val SetUserAvatar: AccountProfile)

@Serializable private class RemoveUserAvatarAnswer(val RemoveUserAvatar: AccountProfile)

@Serializable private class ListAgentPhonesAnswer(val ListAgentPhones: PairedPhoneList)

@Serializable private class PasskeysAnswer(val GetPasskeyPolicy: PasskeyPolicy? = null, val ListPasskeys: List<Passkey?> = emptyList())


/** The person's own account; null on a server with no accounts. */
suspend fun Connection.accountProfile(): AccountProfile? =
    execute<GetCurrentUserAnswer>(AccountProfileDocuments.ACCOUNT_PROFILE).GetCurrentUser

/** Saves the profile as the draft has it, and returns it as the server now has it. */
suspend fun Connection.updateAccountProfile(draft: ProfileDraft): AccountProfile =
    execute<UpdateUserAnswer>(AccountProfileDocuments.UPDATE_ACCOUNT_PROFILE, draft.variables).UpdateUser

/** Says how the person is described, alone; not set clears it. Answers the account as the server now has it. */
suspend fun Connection.setAccountPronoun(userId: String, pronoun: PersonPronoun): AccountProfile =
    execute<UpdateUserAnswer>(AccountProfileDocuments.UPDATE_ACCOUNT_PRONOUN, variablesOf("userId" to userId, "pronoun" to pronoun.serverName)).UpdateUser

/** Sets the person's picture. The bytes are a PNG or JPEG the server takes at most 8 MiB of. */
suspend fun Connection.setAccountAvatar(content: ByteArray): AccountProfile =
    execute<SetUserAvatarAnswer>(AccountProfileDocuments.SET_ACCOUNT_AVATAR, variablesOf("content" to Base64.getEncoder().encodeToString(content))).SetUserAvatar

/** Takes the picture away; the initial on the person's color is shown again. */
suspend fun Connection.removeAccountAvatar(): AccountProfile = execute<RemoveUserAvatarAnswer>(AccountProfileDocuments.REMOVE_ACCOUNT_AVATAR).RemoveUserAvatar

/** Replaces the password, after the server checks the current one. The person's other browser sessions end. */
suspend fun Connection.changePassword(currentPassword: String, newPassword: String) {
    execute<kotlinx.serialization.json.JsonObject>(AccountProfileDocuments.CHANGE_PASSWORD, variablesOf("currentPassword" to currentPassword, "newPassword" to newPassword))
}

suspend fun Connection.pairedPhones(): PairedPhoneList = execute<ListAgentPhonesAnswer>(AccountProfileDocuments.PAIRED_PHONES).ListAgentPhones

/** Renames a phone; what it is told about stays as it is. */
suspend fun Connection.renamePhone(phoneId: String, phoneName: String) {
    execute<kotlinx.serialization.json.JsonObject>(AccountProfileDocuments.RENAME_PHONE, variablesOf("phoneId" to phoneId, "phoneName" to phoneName))
}

/** Sends a phone a notification that says it can be reached. */
suspend fun Connection.testPhone(phoneId: String) {
    execute<kotlinx.serialization.json.JsonObject>(AccountProfileDocuments.TEST_PHONE, variablesOf("phoneId" to phoneId))
}

/** Removes a phone: it is signed out and told nothing more. */
suspend fun Connection.removePhone(phoneId: String) {
    execute<kotlinx.serialization.json.JsonObject>(AccountProfileDocuments.REMOVE_PHONE, variablesOf("phoneId" to phoneId))
}

/** The person's passkeys and the server's policy for them. */
data class PasskeyList(val passkeys: List<Passkey>, val policy: PasskeyPolicy?)

suspend fun Connection.passkeys(): PasskeyList {
    val answer = execute<PasskeysAnswer>(AccountProfileDocuments.PASSKEYS)
    return PasskeyList(answer.ListPasskeys.filterNotNull(), answer.GetPasskeyPolicy)
}

/** Removes a passkey, so that authenticator can no longer sign in. */
suspend fun Connection.deletePasskey(passkeyId: String) {
    execute<kotlinx.serialization.json.JsonObject>(AccountProfileDocuments.DELETE_PASSKEY, variablesOf("passkeyId" to passkeyId))
}

/** Chooses which kinds of notification a phone gets; none is every kind. Copied from the iPhone app's Documents (Operations). */
private const val UPDATE_PHONE = """mutation (${'$'}phoneId: String!, ${'$'}notificationKinds: [String!]) {
  UpdateAgentPhone(phoneId: ${'$'}phoneId, notificationKinds: ${'$'}notificationKinds) { id notificationKinds canBeNotified }
}"""

suspend fun Connection.updatePhone(phoneId: String, notificationKinds: List<String>) {
    execute<kotlinx.serialization.json.JsonObject>(UPDATE_PHONE, variablesOf("phoneId" to phoneId, "notificationKinds" to notificationKinds))
}
