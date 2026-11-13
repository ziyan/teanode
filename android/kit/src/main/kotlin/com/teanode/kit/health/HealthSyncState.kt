package com.teanode.kit.health

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.location.InstantAsEpochMillisSerializer
import com.teanode.kit.location.ServerTime
import kotlinx.serialization.Serializable
import java.io.File
import java.net.URLEncoder
import java.time.Instant
import java.time.ZoneId

// What the phone remembers between runs about copying Health to the server: where it got to in each type (the
// platform's changes token, an opaque text), which days of which types are waiting to be worked out again, whether Health
// is turned on and the last erase of the server's copy the phone knows of, and what has been sent. The phone keeps no copy
// of the health data itself. Mirrors ios/TeaNodeKit HealthSyncState.swift.

/** HealthPendingDay is one type on one day whose summary must be worked out again: a new sample landed on it, or the server deleted one from it. */
@Serializable
data class HealthPendingDay(
    val healthType: String,
    /** "YYYY-MM-DD", the phone's calendar day. */
    val day: String,
) : Comparable<HealthPendingDay> {
    override fun compareTo(other: HealthPendingDay): Int = compareValuesBy(this, other, { pending -> pending.healthType }, { pending -> pending.day })
}

/** HealthSyncState is everything the phone keeps about Health syncing. */
@Serializable
class HealthSyncState(
    /** Whose it is, like the location outbox: one file for each account. */
    var accountKey: String? = null,
) {
    /** The platform's changes token for each type, opaque here. */
    var anchors: Map<String, String> = emptyMap()
        private set

    /** The days waiting to be worked out again. */
    var pendingDays: Set<HealthPendingDay> = emptySet()
        private set

    /** Whether the person has Health turned on in the app. */
    var isTurnedOn: Boolean = false
        private set

    /**
     * The time the person last erased all of their Health on the server, as far as this phone knows, written exactly as
     * the server wrote it (RFC 3339, with its fractions of a second: the server compares what it is told with its own to
     * the fraction). Null if the person never erased, or while Health is off.
     */
    var healthErasedAt: String? = null
        private set

    /** Whether Health was turned off because the server told of an erase newer than [healthErasedAt]. Kept so that the settings can say why the switch is off. */
    var isStoppedByErase: Boolean = false
        private set

    /** How many readings and summaries the server refused one by one, so were left out for good since Health was turned on. */
    var droppedEntryCount: Int = 0
        private set

    /** When a batch was last taken by the server. */
    @Serializable(with = InstantAsEpochMillisSerializer::class)
    var lastSentAt: Instant? = null
        private set
    var sentSampleCount: Int = 0
        private set
    var sentDeletedSampleCount: Int = 0
        private set
    var sentDaySummaryCount: Int = 0
        private set
    var sentRouteCount: Int = 0
        private set

    /** Whether the first full read of everything has finished. */
    var isHistoryDone: Boolean = false
        private set

    /** The workouts whose route the server took, the latest last. Only the latest are kept. */
    var sentRouteHealthSampleIds: List<String> = emptyList()
        private set

    // Turning on and off

    /**
     * Notes that the person turned Health on, and the last erase of the server's copy the server told of (null if
     * never). Anchors already kept stay, so turning it off and on does not read everything twice. Does nothing when
     * Health is already on.
     */
    fun turnOn(healthErasedAt: String?) {
        if (isTurnedOn) return
        isTurnedOn = true
        isStoppedByErase = false
        this.healthErasedAt = healthErasedAt
    }

    /** Forgets everything: anchors, waiting days, counts and the erase time. Turning Health on again reads from the beginning, which the server takes as the same samples. */
    fun turnOff() {
        reset()
        isStoppedByErase = false
    }

    /** Whether an answer names an erase on the server that is later than the one this phone knows of (any erase, if it knows of none). */
    fun isErasedLater(than: String?): Boolean = ServerTime.isLater(than, healthErasedAt)

    /**
     * The rule for an erase on the server. If the answer names a later one, the person erased everything after the phone
     * last heard of it: what the phone remembers is of data that is gone, so the state resets and Health is off until
     * the person turns it on again, with [isStoppedByErase] set for the notice. Returns whether it reset.
     */
    fun applyServerErase(healthErasedAt: String?): Boolean {
        if (!isTurnedOn || !isErasedLater(healthErasedAt)) return false
        reset()
        isStoppedByErase = true
        return true
    }

    private fun reset() {
        anchors = emptyMap()
        pendingDays = emptySet()
        isTurnedOn = false
        healthErasedAt = null
        lastSentAt = null
        sentSampleCount = 0
        sentDeletedSampleCount = 0
        sentDaySummaryCount = 0
        sentRouteCount = 0
        droppedEntryCount = 0
        isHistoryDone = false
        sentRouteHealthSampleIds = emptyList()
    }

    /** Notes that every type was read to its end. */
    fun markHistoryDone() {
        isHistoryDone = true
    }

    // Anchors

    fun anchor(healthType: String): String? = anchors[healthType]

    /** Saves where reading got to, once the server has taken what was read. Null forgets it, so the type is read from the beginning. */
    fun setAnchor(anchor: String?, healthType: String) {
        anchors = if (anchor == null) anchors - healthType else anchors + (healthType to anchor)
    }

    // Days to work out again

    /** Notes that a type's summary must be worked out again for every day a reading from [start] to [end] touches. */
    fun markPending(healthType: String, start: Instant, end: Instant, zone: ZoneId) {
        val later = if (end.isBefore(start)) start else end
        pendingDays = pendingDays + HealthDays.days(start, later, zone).map { day -> HealthPendingDay(healthType, day) }
    }

    /** Notes the days of the samples the server deleted, which it answered with their types and times. */
    fun markPending(deletedSamples: List<HealthDeletedSample>, zone: ZoneId) {
        for (deleted in deletedSamples) {
            val start = deleted.startedInstant ?: continue
            markPending(deleted.healthType, start, deleted.endedInstant ?: start, zone)
        }
    }

    fun markPending(day: HealthPendingDay) {
        pendingDays = pendingDays + day
    }

    /** The waiting days of a type, earliest first. */
    fun pendingDays(healthType: String): List<String> = pendingDays.filter { pending -> pending.healthType == healthType }.map { pending -> pending.day }.sorted()

    /** Takes days out once their summaries have been sent. */
    fun clearPending(days: Collection<HealthPendingDay>) {
        pendingDays = pendingDays - days.toSet()
    }

    // Sending

    /** Notes entries of batches that the server refused one by one, which were left out. */
    fun recordDropped(entryCount: Int) {
        droppedEntryCount += maxOf(0, entryCount)
    }

    /** Notes that the server took a batch. The readings counted are the ones that were new to it. */
    fun recordSent(batch: HealthBatch, result: ReportHealthResult, at: Instant) {
        lastSentAt = at
        sentSampleCount += result.newSampleCount
        sentDeletedSampleCount += result.deletedSampleCount
        sentDaySummaryCount += batch.daySummaries.size
        sentRouteCount += batch.routes.size
    }

    /**
     * Takes the server's answer to a batch. If the answer names an erase of all of the person's Health later than the
     * one this phone knows of, the server stored nothing: the state is reset and this answers false. Else the batch is
     * noted as sent and this answers true, and the caller may move the anchors of what the batch carried.
     */
    fun accept(result: ReportHealthResult, batch: HealthBatch, at: Instant): Boolean {
        if (applyServerErase(result.healthErasedAt)) return false
        recordSent(batch, result, at)
        return true
    }

    // Routes

    fun hasSentRoute(healthSampleId: String): Boolean = sentRouteHealthSampleIds.contains(healthSampleId)

    /** Notes that the server took a workout's route. A route the server could not match to a workout yet is not noted, so it is sent again. */
    fun recordRouteSent(healthSampleId: String) {
        if (sentRouteHealthSampleIds.contains(healthSampleId)) return
        val all = sentRouteHealthSampleIds + healthSampleId
        sentRouteHealthSampleIds = if (all.size > SENT_ROUTE_COUNT_MOST) all.drop(all.size - SENT_ROUTE_COUNT_MOST) else all
    }

    // The file

    /** The file's contents. */
    fun encoded(): String = TeaNodeJson.encodeToString(serializer(), this)

    /** Writes the state to the account's file in a directory (the app's private storage, left out of backups). */
    fun save(directory: File) {
        val key = accountKey ?: return
        directory.mkdirs()
        val file = File(directory, fileName(key))
        val temporary = File(directory, fileName(key) + ".tmp")
        temporary.writeText(encoded())
        if (!temporary.renameTo(file)) {
            file.writeText(encoded())
            temporary.delete()
        }
    }

    companion object {
        /** How many workouts the state remembers a route sent for. */
        const val SENT_ROUTE_COUNT_MOST = 200

        /** The name of the file an account's state is kept in: one file for each account. The key is encoded, so no key can name another file or a directory. */
        fun fileName(accountKey: String): String {
            val safe = URLEncoder.encode(accountKey, "UTF-8").replace("%", "_").replace("+", "_").replace(".", "_").replace("*", "_").replace("-", "_")
            return "health-$safe.json"
        }

        /** A state read from a file's contents. */
        fun decoded(text: String): HealthSyncState = TeaNodeJson.decodeFromString(serializer(), text)

        /** The account's state from a directory; a new one if there is no file, it cannot be read, or it is of another account. */
        fun load(accountKey: String, directory: File): HealthSyncState {
            val state = runCatching { decoded(File(directory, fileName(accountKey)).readText()) }.getOrNull()
            return if (state != null && state.accountKey == accountKey) state else HealthSyncState(accountKey)
        }

        /** The accounts Health is turned on for, from the state files in a directory. */
        fun accountKeysTurnedOn(directory: File): Set<String> {
            val names = directory.list().orEmpty()
            val keys = mutableSetOf<String>()
            for (name in names) {
                if (!name.startsWith("health-") || !name.endsWith(".json")) continue
                val state = runCatching { decoded(File(directory, name).readText()) }.getOrNull() ?: continue
                val accountKey = state.accountKey ?: continue
                if (state.isTurnedOn && fileName(accountKey) == name) keys.add(accountKey)
            }
            return keys
        }

        /** Deletes an account's state file, which turns Health off for it. */
        fun remove(accountKey: String, directory: File) {
            File(directory, fileName(accountKey)).delete()
        }
    }
}
