package com.teanode.kit.contacts

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive

/** PersonVisit is one visit of somebody who shares where they have been, as the card's visits page lists it. */
@Serializable
data class PersonVisit(
    val id: String,
    val place: PersonPlace,
    /** RFC 3339; null when the phone did not see the arrival. */
    val arrivedAt: String? = null,
    /** RFC 3339; null while the person is still there. */
    val departedAt: String? = null,
    val durationSeconds: Double? = null,
    val photoCount: Int? = null,
) {
    /** Whether the person is still there, as far as the server knows. */
    val isOpen: Boolean get() = departedAt == null
}

/** PersonPlace is a place somebody goes to, with what the person sees it called. */
@Serializable
data class PersonPlace(
    val id: String,
    val placeName: String = "",
    val isNamedByPerson: Boolean = false,
    /** "" or one of home, work, school and the like. */
    val placeLabel: String = "",
    val addressText: String = "",
    val visitCount: Int = 0,
    val lastVisitAt: String? = null,
)

@Serializable
private class VisitsAnswer(@SerialName("ListLocationVisits") val page: VisitsPage) {
    @Serializable
    class VisitsPage(val placeVisits: List<PersonVisit> = emptyList(), val nextOffset: Int? = null)
}

@Serializable
private class PlacesAnswer(@SerialName("ListPlaces") val page: PlacesPage) {
    @Serializable
    class PlacesPage(val places: List<PersonPlace> = emptyList(), val nextOffset: Int? = null)
}

/** The visits of somebody who shares where they have been, between two times (RFC 3339). */
suspend fun Connection.personVisits(userId: String, fromText: String, untilText: String, limitCount: Int = 200): List<PersonVisit> =
    execute<VisitsAnswer>(
        PersonVisitsDocuments.LOCATION_VISITS,
        mapOf("userId" to JsonPrimitive(userId), "from" to JsonPrimitive(fromText), "until" to JsonPrimitive(untilText), "limitCount" to JsonPrimitive(limitCount)),
    ).page.placeVisits

/** The places of somebody who shares where they have been. */
suspend fun Connection.personPlaces(userId: String, limitCount: Int = 50): List<PersonPlace> =
    execute<PlacesAnswer>(
        PersonVisitsDocuments.LOCATION_PLACES, mapOf<String, JsonElement>("userId" to JsonPrimitive(userId), "limitCount" to JsonPrimitive(limitCount)),
    ).page.places
