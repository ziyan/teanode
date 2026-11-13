package com.teanode.app.health

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import com.teanode.kit.Connection
import com.teanode.kit.accessSession
import com.teanode.kit.health.HealthCategory
import com.teanode.kit.health.HealthShareView
import com.teanode.kit.health.HealthTypeView
import com.teanode.kit.health.eraseHealthData
import com.teanode.kit.health.healthShares
import com.teanode.kit.health.healthTypes
import kotlinx.coroutines.CancellationException

/** Somebody who shares Health with the person. */
data class HealthSharer(val userId: String, val displayName: String)

/** The kinds of one group of the Health app. */
data class HealthTypeGroup(val healthCategory: HealthCategory, val types: List<HealthTypeView>) {
    val id: String get() = healthCategory.serverWord
}

/** What the model says in the snackbar. */
sealed class HealthNotice {
    data class Done(val text: String) : HealthNotice()
    data class Problem(val text: String) : HealthNotice()
}

/** The words the model says, in the person's language. */
class HealthModelWords(
    val describeProblem: (Throwable) -> String,
    val notSharedWithYou: String,
    val erasedText: (Int) -> String,
    val couldNotErase: String,
    val typeName: (HealthTypeView) -> String,
)

/**
 * HealthModel is the Health screen: the kinds of Health data the person has copied to their server, grouped as the Health
 * app groups them, and somebody who shares Health with them, chosen in the bar, whose shared kinds are shown instead.
 */
class HealthModel(val connection: Connection, private val words: HealthModelWords) : ViewModel() {
    var types: List<HealthTypeView> by mutableStateOf(emptyList())
        private set
    var shares: List<HealthShareView> by mutableStateOf(emptyList())
        private set
    var ownUserId: String? by mutableStateOf(null)
        private set
    var hasLoaded: Boolean by mutableStateOf(false)
        private set
    private var activeLoadCount = 0
    var loadFailedText: String? by mutableStateOf(null)
        private set

    /** Whose data is shown: null for the person's own. */
    var selectedUserId: String? by mutableStateOf(null)
        private set
    var notice: HealthNotice? by mutableStateOf(null)

    /** The workouts whose route was opened, by sample, so the route screen can say what it is a route of. */
    val routeRequests: MutableMap<String, HealthRouteRequest> = mutableMapOf()

    val isViewingOwn: Boolean get() = selectedUserId == null

    /** The people who share Health with the person, by name. The person's own shares are told apart by who they are, so none are listed until that is known. */
    val sharers: List<HealthSharer>
        get() {
            val own = ownUserId ?: return emptyList()
            val found = LinkedHashMap<String, HealthSharer>()
            for (share in shares) {
                if (share.ownerUserId == own || share.ownerUserId in found) continue
                val name = share.ownerName.orEmpty()
                found[share.ownerUserId] = HealthSharer(share.ownerUserId, name.ifEmpty { share.ownerUsername })
            }
            return found.values.sortedBy { sharer -> sharer.displayName.lowercase() }
        }

    val selectedSharer: HealthSharer? get() = sharers.firstOrNull { sharer -> sharer.userId == selectedUserId }

    val groups: List<HealthTypeGroup>
        get() = HealthCategory.entries.mapNotNull { category ->
            val inCategory = types.filter { type -> type.healthCategoryValue == category }
            if (inCategory.isEmpty()) null else HealthTypeGroup(category, inCategory)
        }

    fun name(type: HealthTypeView): String = words.typeName(type)

    /** Reads the person's kinds, or the chosen person's. */
    suspend fun load() {
        activeLoadCount += 1
        try {
            if (ownUserId == null) runCatching { connection.accessSession() }.getOrNull()?.let { session -> ownUserId = session.userId }
            runCatching { connection.healthShares() }.getOrNull()?.let { read ->
                shares = read
                // Somebody who stopped sharing is no longer chosen.
                if (selectedUserId != null && selectedSharer == null) {
                    selectedUserId = null
                    notice = HealthNotice.Problem(words.notSharedWithYou)
                }
            }
            val requestedUserId = selectedUserId
            try {
                val read = connection.healthTypes(requestedUserId)
                // The person may have chosen somebody else while this was read.
                if (requestedUserId != selectedUserId) return
                types = read
                loadFailedText = null
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                if (requestedUserId != selectedUserId) return
                if (types.isEmpty()) loadFailedText = words.describeProblem(error) else notice = HealthNotice.Problem(words.describeProblem(error))
            }
        } finally {
            activeLoadCount -= 1
            hasLoaded = true
        }
    }

    /** Chooses whose Health is shown. */
    suspend fun select(userId: String?) {
        if (userId == selectedUserId) return
        selectedUserId = userId
        types = emptyList()
        loadFailedText = null
        hasLoaded = false
        load()
    }

    /** Erases one kind's readings, summaries and routes. True when the server did. */
    suspend fun erase(healthType: String): Boolean = try {
        val result = connection.eraseHealthData(healthType)
        notice = HealthNotice.Done(words.erasedText(result.erasedSampleCount))
        load()
        true
    } catch (error: Exception) {
        if (error is CancellationException) throw error
        notice = HealthNotice.Problem(words.couldNotErase)
        false
    }

    fun say(text: String, isProblem: Boolean) {
        notice = if (isProblem) HealthNotice.Problem(text) else HealthNotice.Done(text)
    }
}

class HealthModelFactory(private val connection: Connection, private val words: HealthModelWords) : ViewModelProvider.Factory {
    @Suppress("UNCHECKED_CAST")
    override fun <Model : ViewModel> create(modelClass: Class<Model>): Model = HealthModel(connection, words) as Model
}

/** What the route screen shows of a workout above its path. */
data class HealthRouteRequest(
    val healthSampleId: String,
    val titleText: String,
    val startedAt: java.time.Instant?,
    val durationSeconds: Double,
    val distanceMeters: Double?,
    val energyKilocalories: Double?,
) {
    companion object
}

/** The request of a workout's route screen, from the reading that opens it. */
fun HealthRouteRequest.Companion.of(sample: com.teanode.kit.health.HealthSampleView, text: HealthText): HealthRouteRequest {
    val started = sample.startedInstant
    val ended = sample.endedInstant ?: started
    val duration = if (started == null || ended == null) 0.0 else maxOf(0.0, (ended.toEpochMilli() - started.toEpochMilli()) / 1000.0)
    return HealthRouteRequest(
        sample.healthSampleId, text.workoutName(sample.workoutActivityType), started, duration, HealthText.metadataNumber(sample.sampleMetadata[HealthText.TOTAL_DISTANCE_KEY]),
        HealthText.metadataNumber(sample.sampleMetadata[HealthText.TOTAL_ENERGY_KEY]),
    )
}
