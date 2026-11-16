package com.teanode.kit.passwords

/**
 * PasswordsLockPolicy decides when Passwords asks for the passphrase (or the fingerprint) again. The person chooses the
 * time (`lockAfterMinutes`, 1 to 10080); the time runs from the last use, not from the unlock, so a person who keeps
 * using Passwords is not asked in the middle of it.
 */
object PasswordsLockPolicy {
    const val MINIMUM_LOCK_AFTER_MINUTES = 1
    const val MAXIMUM_LOCK_AFTER_MINUTES = 10080

    /**
     * How far in the future a last-used time may be before it is not believed. A phone's clock is corrected by a second
     * or two now and then; one set back by hours must not keep Passwords open for those hours.
     */
    const val CLOCK_SKEW_TOLERANCE_SECONDS = 5.0

    /** The lock time kept within what the server accepts. */
    fun clamped(lockAfterMinutes: Int): Int = minOf(maxOf(lockAfterMinutes, MINIMUM_LOCK_AFTER_MINUTES), MAXIMUM_LOCK_AFTER_MINUTES)

    /** Whether the person must be asked again: never used since the app started, used longer ago than the lock time, or used "in the future". */
    fun isAskRequired(lockAfterMinutes: Int, lastUsedAtEpochMS: Long?, nowEpochMS: Long): Boolean {
        if (lastUsedAtEpochMS == null) return true
        val elapsedSeconds = (nowEpochMS - lastUsedAtEpochMS) / 1000.0
        if (elapsedSeconds < -CLOCK_SKEW_TOLERANCE_SECONDS) return true
        return elapsedSeconds >= clamped(lockAfterMinutes) * 60.0
    }

    /** The moment Passwords locks if it is not used again. */
    fun lockAtEpochMS(lockAfterMinutes: Int, lastUsedAtEpochMS: Long): Long = lastUsedAtEpochMS + clamped(lockAfterMinutes) * 60_000L
}
