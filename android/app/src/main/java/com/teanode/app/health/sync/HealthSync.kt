package com.teanode.app.health.sync

import android.content.Context
import androidx.health.connect.client.HealthConnectClient
import androidx.health.connect.client.PermissionController
import androidx.health.connect.client.changes.DeletionChange
import androidx.health.connect.client.changes.UpsertionChange
import androidx.health.connect.client.permission.HealthPermission
import androidx.health.connect.client.records.Record
import androidx.health.connect.client.request.ChangesTokenRequest
import androidx.health.connect.client.request.ReadRecordsRequest
import androidx.health.connect.client.time.TimeRangeFilter
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import com.teanode.app.TeaNodeApplication
import com.teanode.app.health.HealthPassProgress
import com.teanode.app.health.HealthSyncSnapshot
import com.teanode.app.health.HealthSyncing
import com.teanode.kit.Connection
import com.teanode.kit.health.HealthBatch
import com.teanode.kit.health.HealthBatchSender
import com.teanode.kit.health.HealthCategorySample
import com.teanode.kit.health.HealthDaySummaries
import com.teanode.kit.health.HealthDays
import com.teanode.kit.health.HealthPendingDay
import com.teanode.kit.health.HealthReading
import com.teanode.kit.health.HealthReportFailure
import com.teanode.kit.health.HealthRetryGate
import com.teanode.kit.health.HealthSampleInput
import com.teanode.kit.health.HealthSyncState
import com.teanode.kit.health.HealthTypes
import com.teanode.kit.health.HealthWorkoutSample
import com.teanode.kit.health.eraseHealthData
import com.teanode.kit.health.healthSettings
import com.teanode.kit.health.reportHealth
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.io.File
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.util.concurrent.TimeUnit

/**
 * HealthSync copies what Health Connect holds to the server, as the iPhone app's `HealthSync` does for HealthKit: each kind is
 * read once from the start of its history, then only what Health Connect's changes token says changed, the readings are sent
 * under HealthKit's type names, and the summary of every day a reading touched is worked out here and sent too.
 * Its state (the tokens, the days waiting, the counts) is the kit's [HealthSyncState], kept in a file for each account.
 *
 * Not verified on a phone with Health Connect: the reading and the changes token. The batching, retry and state are the kit's, with JVM tests.
 */
class HealthSync private constructor(private val context: Context) : HealthSyncing {
    private val mutex = Mutex()
    private val application get() = context.applicationContext as TeaNodeApplication
    private val directory = File(context.filesDir, "health")
    private val retryGate = HealthRetryGate()
    private val zone: ZoneId get() = ZoneId.systemDefault()
    private val snapshotFlow = MutableStateFlow(HealthSyncSnapshot(isAvailable = isAvailable()))
    override val snapshot: StateFlow<HealthSyncSnapshot> = snapshotFlow.asStateFlow()

    private val connection: Connection? get() = application.session.connection.value
    private val accountKey: String? get() = connection?.accountKey

    private fun isAvailable(): Boolean = HealthConnectClient.getSdkStatus(context) == HealthConnectClient.SDK_AVAILABLE
    private fun client(): HealthConnectClient = HealthConnectClient.getOrCreate(context)

    override val requiredPermissions: Set<String> =
        HealthConnectKinds.all.map { kind -> HealthPermission.getReadPermission(kind.recordClass) }.toSet() + HealthPermission.PERMISSION_READ_HEALTH_DATA_HISTORY

    override suspend fun grantedPermissions(): Set<String> = if (isAvailable()) client().permissionController.getGrantedPermissions() else emptySet()

    /** The permission screen's contract, for the setup sheet to launch. */
    fun permissionContract() = PermissionController.createRequestPermissionResultContract()

    private fun load(): HealthSyncState? = accountKey?.let { key -> HealthSyncState.load(key, directory) }

    private fun publish(state: HealthSyncState?, isSyncing: Boolean = false, problemText: String? = null, progress: HealthPassProgress? = null) {
        snapshotFlow.value = HealthSyncSnapshot(
            isOn = state?.isTurnedOn ?: false, isAvailable = isAvailable(), isSyncing = isSyncing, lastSentAt = state?.lastSentAt,
            sentSampleCount = state?.sentSampleCount ?: 0, pendingDayCount = state?.pendingDays?.size ?: 0, isHistoryDone = state?.isHistoryDone ?: false,
            problemText = problemText, passProgress = progress, isStoppedByErase = state?.isStoppedByErase ?: false, droppedEntryCount = state?.droppedEntryCount ?: 0,
        )
    }

    /** Reads the file again for the account now signed in, so the screens show its state. */
    fun refresh() = publish(load())

    override suspend fun turnOn(grantedPermissions: Set<String>): Boolean {
        val asked = connection ?: return false
        if (!isAvailable() || HealthConnectKinds.all.none { kind -> grantedPermissions.contains(HealthPermission.getReadPermission(kind.recordClass)) }) return false
        mutex.withLock {
            val state = load() ?: return false
            val erasedAt = asked.healthSettings().healthErasedAt
            state.turnOn(erasedAt)
            state.save(directory)
            publish(state)
        }
        schedule()
        syncNow()
        return true
    }

    override fun turnOff() {
        val key = accountKey ?: return
        HealthSyncState.remove(key, directory)
        WorkManager.getInstance(context).cancelUniqueWork(WORK_NAME)
        publish(HealthSyncState(key))
    }

    override suspend fun eraseEverything() {
        val asked = connection ?: return
        asked.eraseHealthData()
        turnOff()
    }

    /** Runs the copying every hour while Health is on, which wakes the app without a screen. */
    private fun schedule() {
        WorkManager.getInstance(context).enqueueUniquePeriodicWork(WORK_NAME, ExistingPeriodicWorkPolicy.UPDATE, PeriodicWorkRequestBuilder<HealthSyncWorker>(1, TimeUnit.HOURS).build())
    }

    /** One pass over every kind the person allowed: new readings, then the summaries of the days they touched. */
    override suspend fun syncNow() {
        val asked = connection ?: return
        if (!isAvailable() || !retryGate.canTry(Instant.now())) return
        mutex.withLock {
            val state = load()?.takeIf { each -> each.isTurnedOn } ?: return
            val granted = grantedPermissions()
            val kinds = HealthConnectKinds.all.filter { kind -> granted.contains(HealthPermission.getReadPermission(kind.recordClass)) }
            val since = Instant.now().minus(Duration.ofDays(if (granted.contains(HealthPermission.PERMISSION_READ_HEALTH_DATA_HISTORY)) HISTORY_DAYS else RECENT_DAYS))
            try {
                for ((index, kind) in kinds.withIndex()) {
                    publish(state, isSyncing = true, progress = HealthPassProgress(index, kinds.size))
                    if (!readKind(asked, state, kind, since)) { publish(state); return }
                    sendPendingDays(asked, state, kind)
                    state.save(directory)
                }
                state.markHistoryDone()
                state.save(directory)
                retryGate.didSucceed()
                publish(state)
            } catch (error: CancellationException) {
                throw error
            } catch (error: Exception) {
                retryGate.didFail(Instant.now())
                state.save(directory)
                publish(state, problemText = if (HealthReportFailure.classify(error) is HealthReportFailure.Refused) (error as? com.teanode.kit.TeaNodeError.Refused)?.reason else null)
            }
        }
    }

    /** Sends the readings of one kind that are new (all of them the first time), then keeps the new changes token. Returns false when the server said the person erased everything. */
    private suspend fun readKind(asked: Connection, state: HealthSyncState, kind: HealthConnectKind, since: Instant): Boolean {
        val healthConnect = client()
        val token = state.anchor(kind.healthType)
        val samples = mutableListOf<HealthSampleInput>()
        val deletedIds = mutableListOf<String>()
        var nextToken: String? = null
        var isExpired = token == null
        if (token != null) {
            var current = token
            while (true) {
                val changes = healthConnect.getChanges(current!!)
                if (changes.changesTokenExpired) { isExpired = true; break }
                for (change in changes.changes) {
                    when (change) {
                        is UpsertionChange -> samples += kind.samples(change.record)
                        is DeletionChange -> deletedIds += change.recordId
                    }
                }
                current = changes.nextChangesToken
                if (!changes.hasMore) break
            }
            nextToken = current
        }
        if (isExpired) {
            // Asked before reading, so a change made while it reads is in the next pass and not lost.
            nextToken = healthConnect.getChangesToken(ChangesTokenRequest(setOf(kind.recordClass)))
            samples.clear()
            var pageToken: String? = null
            do {
                val page = healthConnect.readRecords(ReadRecordsRequest(kind.recordClass, TimeRangeFilter.between(since, Instant.now()), pageSize = PAGE_SIZE, pageToken = pageToken))
                for (record in page.records) samples += kind.samples(record)
                pageToken = page.pageToken
            } while (pageToken != null)
        }
        for (sample in samples) state.markPending(kind.healthType, sample.startedAt, sample.endedAt, zone)
        val requests = HealthBatch.requests(samples = samples, deletedHealthSampleIds = deletedIds)
        state.recordDropped(requests.rejectedEntryCount)
        for (batch in requests.requests) if (!send(asked, state, batch)) return false
        state.setAnchor(nextToken, kind.healthType)
        return true
    }

    /** Sends a batch, noting the server's answer. False when it named an erase this phone had not heard of, which turned copying off. */
    private suspend fun send(asked: Connection, state: HealthSyncState, batch: HealthBatch): Boolean {
        val outcome = HealthBatchSender.sendSplittingInvalid(batch) { each -> asked.reportHealth(each, healthErasedAt = state.healthErasedAt) }
        state.recordDropped(outcome.droppedEntryCount)
        if (!state.accept(outcome.result, batch, Instant.now())) { state.save(directory); WorkManager.getInstance(context).cancelUniqueWork(WORK_NAME); return false }
        state.markPending(outcome.result.deletedSamples, zone)
        return true
    }

    /** Works out again, from Health Connect, the summary of each day of a kind that a new or deleted reading touched, and sends them. */
    private suspend fun sendPendingDays(asked: Connection, state: HealthSyncState, kind: HealthConnectKind) {
        val days = state.pendingDays(kind.healthType)
        if (days.isEmpty()) return
        val healthConnect = client()
        val workedOutAt = Instant.now()
        val first = HealthDays.startOfDay(days.first(), zone) ?: return
        val last = HealthDays.interval(days.last(), zone)?.end ?: return
        val samples = mutableListOf<HealthSampleInput>()
        var pageToken: String? = null
        do {
            // A day before the first is read as well, for a sleep that began the evening before.
            val page = healthConnect.readRecords(ReadRecordsRequest(kind.recordClass, TimeRangeFilter.between(first.minus(Duration.ofDays(1)), last), pageSize = PAGE_SIZE, pageToken = pageToken))
            for (record in page.records) samples += kind.samples(record)
            pageToken = page.pageToken
        } while (pageToken != null)
        val daySet = days.toSet()
        val info = HealthTypes.info(kind.healthType)
        val summaries = when {
            kind.healthType == "sleepAnalysis" -> HealthDaySummaries.sleepSummaries(samples.mapNotNull { sample -> HealthCategorySample.of(sample) }, zone, daySet)
            kind.healthType == "workout" -> HealthDaySummaries.workoutSummaries(samples.mapNotNull { sample -> HealthWorkoutSample.of(sample) }, zone, daySet)
            else -> {
                val readings = samples.mapNotNull { sample -> sample.quantityValue?.let { value -> HealthReading(sample.startedAt, value) } }
                HealthDaySummaries.readingStatistics(readings, zone, daySet).map { (day, statistics) ->
                    val sum = readings.filter { reading -> HealthDays.text(reading.startedAt, zone) == day }.sumOf { reading -> reading.value }
                    HealthDaySummaries.quantitySummary(kind.healthType, day, statistics.copy(sumValue = sum), info?.quantityUnit)
                }
            }
        }.map { summary -> summary.workedOut(workedOutAt) }
        // A day with nothing left is sent as no summary, so the server's stays until its readings go.
        for (batch in HealthBatch.requests(daySummaries = summaries).requests) if (!send(asked, state, batch)) return
        state.clearPending(days.map { day -> HealthPendingDay(kind.healthType, day) })
    }

    companion object {
        private const val WORK_NAME = "teanode-health-sync"
        private const val PAGE_SIZE = 1000
        private const val HISTORY_DAYS = 365L
        private const val RECENT_DAYS = 29L

        @Volatile private var instance: HealthSync? = null

        fun get(context: Context): HealthSync = instance ?: synchronized(this) {
            instance ?: HealthSync(context.applicationContext).also { created -> instance = created }
        }
    }
}
