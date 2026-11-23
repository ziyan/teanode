package com.teanode.kit.passwords

import java.time.Instant

/**
 * A password made for one device, sync or mail. Both lists show it the same way: when it was made, when it last signed
 * anything in, and whether it looks abandoned.
 */
interface DevicePassword {
    val passwordCreatedAt: String?
    val passwordLastUsedAt: String?
}

private fun instantOf(text: String?): Instant? = text?.let { value -> runCatching { Instant.parse(value) }.getOrNull() }

/** A password nobody used within this long of being made was probably replaced by another one made right after it. */
const val DEVICE_PASSWORD_NEVER_USED_GRACE_SECONDS: Long = 60 * 60

/** A password silent this long is more likely forgotten than occasional. */
const val DEVICE_PASSWORD_LONG_UNUSED_SECONDS: Long = 90L * 24 * 60 * 60

/**
 * The password was never used more than an hour after it was made, or has not been used for ninety days. Only a hint:
 * nothing is deleted for the person.
 */
fun DevicePassword.isMaybeSafeToDelete(now: Instant = Instant.now()): Boolean {
    instantOf(passwordLastUsedAt)?.let { lastUsedAt -> return now.epochSecond - lastUsedAt.epochSecond > DEVICE_PASSWORD_LONG_UNUSED_SECONDS }
    val createdAt = instantOf(passwordCreatedAt) ?: return false
    return now.epochSecond - createdAt.epochSecond > DEVICE_PASSWORD_NEVER_USED_GRACE_SECONDS
}

/** The newest password first, so the one just made is at the top. A password with no readable creation time goes last. */
fun <Password : DevicePassword> List<Password>.newestFirst(): List<Password> =
    sortedByDescending { password -> instantOf(password.passwordCreatedAt)?.toEpochMilli() ?: Long.MIN_VALUE }
