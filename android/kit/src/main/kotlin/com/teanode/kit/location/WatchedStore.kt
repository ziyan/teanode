package com.teanode.kit.location

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive

/**
 * WatchedStore is a branch, near the person, of a store their lists need something from with reminders on, for the
 * phone to watch: walking into it is a visit there, which brings the store's reminder. The recorder reads it here so
 * that Location does not depend on the Shopping part; the document is the iPhone app's.
 */
@Serializable
data class WatchedStore(
    val storeName: String,
    val storeKey: String,
    /** The branch's name on the map. */
    val placeName: String,
    val osmType: String,
    val osmId: Long,
    val osmCategory: String = "",
    val latitudeDegrees: Double,
    val longitudeDegrees: Double,
    val radiusMeters: Double,
    val distanceMeters: Double = 0.0,
    /** The person's place for the branch, when they have one. */
    val placeId: String? = null,
)

object WatchedStoreDocuments {
    const val WATCHED_STORES = """
        query (${'$'}latitudeDegrees: Float, ${'$'}longitudeDegrees: Float) {
          ListWatchedStores(latitudeDegrees: ${'$'}latitudeDegrees, longitudeDegrees: ${'$'}longitudeDegrees) {
            storeName storeKey placeName osmType osmId osmCategory latitudeDegrees longitudeDegrees radiusMeters distanceMeters placeId
          }
        }
        """
}

@Serializable private class ListWatchedStoresAnswer(@SerialName("ListWatchedStores") val stores: List<WatchedStore>)

/** The branches near a position (where the person was last reported to be when null) of the stores their lists need something from. */
suspend fun Connection.watchedStores(latitudeDegrees: Double?, longitudeDegrees: Double?): List<WatchedStore> {
    val variables = buildMap {
        if (latitudeDegrees != null && longitudeDegrees != null) {
            put("latitudeDegrees", JsonPrimitive(latitudeDegrees))
            put("longitudeDegrees", JsonPrimitive(longitudeDegrees))
        }
    }
    return execute(WatchedStoreDocuments.WATCHED_STORES, ListWatchedStoresAnswer.serializer(), variables).stores
}
