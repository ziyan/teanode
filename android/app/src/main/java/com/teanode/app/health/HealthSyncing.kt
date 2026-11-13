package com.teanode.app.health

import androidx.compose.runtime.compositionLocalOf
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.time.Instant

/** How far a pass over the person's Health data has got: how many of the kinds it reads are done. */
data class HealthPassProgress(val typesDone: Int, val typesTotal: Int)

/** What the copying of Health to the server is doing, for the settings to show. */
data class HealthSyncSnapshot(
    val isOn: Boolean = false,
    val isAvailable: Boolean = true,
    val isSyncing: Boolean = false,
    val lastSentAt: Instant? = null,
    val sentSampleCount: Int = 0,
    val pendingDayCount: Int = 0,
    val isHistoryDone: Boolean = false,
    val problemText: String? = null,
    val passProgress: HealthPassProgress? = null,
    val isStoppedByErase: Boolean = false,
    val droppedEntryCount: Int = 0,
)

/**
 * HealthSyncing is what the Health screens ask of the part that copies the phone's Health Connect data to the server:
 * whether it is on, what it has sent, and the ways to turn it on and off, send now, and erase. The real one is
 * [com.teanode.app.health.sync.HealthSync]; demo mode and the screenshot tests use [DemoHealthSyncing].
 */
interface HealthSyncing {
    val snapshot: StateFlow<HealthSyncSnapshot>

    /** The permissions Health Connect has to grant before anything can be read. */
    val requiredPermissions: Set<String>

    /** Turns copying on once the person has answered Health Connect's permission screen with [grantedPermissions]; false when nothing useful was granted. */
    suspend fun turnOn(grantedPermissions: Set<String>): Boolean

    fun turnOff()
    suspend fun syncNow()

    /** Erases everything Health holds on the server for the person and stops copying. */
    suspend fun eraseEverything()

    /** The permissions Health Connect holds for the app now. */
    suspend fun grantedPermissions(): Set<String>
}

/** The syncing the Health screens talk to; the shell's section provides it. */
val LocalHealthSyncing = compositionLocalOf<HealthSyncing> { DemoHealthSyncing() }

/** DemoHealthSyncing stands in for the real copying in demo mode and the screenshot tests: it keeps whatever it is told and sends nothing. */
class DemoHealthSyncing(initial: HealthSyncSnapshot = HealthSyncSnapshot(isOn = true, lastSentAt = Instant.now().minusSeconds(600), sentSampleCount = 18_420, isHistoryDone = true)) : HealthSyncing {
    private val state = MutableStateFlow(initial)
    override val snapshot: StateFlow<HealthSyncSnapshot> = state.asStateFlow()
    override val requiredPermissions: Set<String> = emptySet()

    override suspend fun turnOn(grantedPermissions: Set<String>): Boolean {
        state.value = state.value.copy(isOn = true, isStoppedByErase = false)
        return true
    }

    override fun turnOff() {
        state.value = state.value.copy(isOn = false)
    }

    override suspend fun syncNow() {
        state.value = state.value.copy(lastSentAt = Instant.now())
    }

    override suspend fun eraseEverything() {
        state.value = state.value.copy(isOn = false, sentSampleCount = 0, lastSentAt = null)
    }

    override suspend fun grantedPermissions(): Set<String> = emptySet()
}
