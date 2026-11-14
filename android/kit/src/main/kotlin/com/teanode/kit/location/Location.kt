package com.teanode.kit.location

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import java.time.Duration
import java.time.Instant

// Where the person goes, the places they name and the visits they make, as the server keeps them. The phone reports
// points (one reported position each) and visits (LocationOutbox.kt holds what waits to be reported); the screens read
// them back here. Mirrors ios/TeaNodeKit Location.swift.

/** PlaceLabel is what a place is to the person: the fixed list the server checks, which the agent reads for "at home" and "at work". */
enum class PlaceLabel(val serverWord: String) {
    Home("home"),
    Work("work"),
    School("school"),
    Family("family"),
    Gym("gym");

    companion object {
        fun fromServerWord(word: String): PlaceLabel? = entries.firstOrNull { label -> label.serverWord == word }
    }
}

/** LocationShareLevel is how much of a location somebody may see: where the person is now, or that and their visits. "owner" is the person's own. */
enum class LocationShareLevel(val serverWord: String) {
    Current("current"),
    History("history"),
    Owner("owner");

    companion object {
        /** The levels a location can be shared at, least to most. */
        val grantable: List<LocationShareLevel> = listOf(Current, History)

        fun fromServerWord(word: String): LocationShareLevel? = entries.firstOrNull { level -> level.serverWord == word }
    }
}

/** LocationPoint is one position, as the server keeps it. */
@Serializable
data class LocationPoint(
    val latitudeDegrees: Double,
    val longitudeDegrees: Double,
    val accuracyMeters: Double,
    val altitudeMeters: Double? = null,
    val speedMetersPerSecond: Double? = null,
    val courseDegrees: Double? = null,
    /** RFC 3339. */
    val measuredAt: String,
    /** "phone" or "browser". */
    val locationSource: String = "phone",
) {
    val measuredInstant: Instant? get() = ServerTime.instant(measuredAt)
}

/** LocationPlace is a place with a name and a radius, belonging to one person. */
@Serializable
data class LocationPlace(
    val id: String,
    val userId: String = "",
    /** What the person sees: their own name if they gave one, otherwise the map's name, the street address or the city. */
    val placeName: String,
    val isNamedByPerson: Boolean = false,
    /** "" or one of PlaceLabel's words. */
    val placeLabel: String = "",
    val latitudeDegrees: Double,
    val longitudeDegrees: Double,
    val radiusMeters: Double = 75.0,
    /** "node", "way", "relation" or "". */
    val osmType: String = "",
    val osmId: Long? = null,
    val osmName: String = "",
    /** Such as "amenity=cafe". */
    val osmCategory: String = "",
    val addressText: String = "",
    /** Whether the person typed the address, so the map no longer names the place. */
    val isAddressTyped: Boolean = false,
    val visitCount: Int = 0,
    val lastVisitAt: String? = null,
    /** Why the name could not be looked up, when it could not. */
    val namingErrorMessage: String = "",
    val createdAt: String = "",
    val updatedAt: String = "",
) {
    val label: PlaceLabel? get() = PlaceLabel.fromServerWord(placeLabel)
    val lastVisitInstant: Instant? get() = ServerTime.instant(lastVisitAt)

    /** Whether the person named or labeled it, which is what puts it first in a list. */
    val isOfPersonsOwn: Boolean get() = isNamedByPerson || label != null
}

/** The first of the person's photos taken during a visit, as the visit lists them. */
@Serializable
data class VisitPhoto(
    val id: String,
    val createdAt: String = "",
    val photoKind: String = "picture",
    val takenAt: String? = null,
    val takenAtOffsetMinutes: Int? = null,
    val widthPixels: Int = 0,
    val heightPixels: Int = 0,
    val durationMS: Int = 0,
    val latitude: Double? = null,
    val longitude: Double? = null,
    val placeName: String = "",
    val cameraMake: String = "",
    val cameraModel: String = "",
    val originalFilename: String = "",
    val isFavorite: Boolean = false,
    val isScreenshot: Boolean = false,
    val photoProcessingState: String = "ready",
    val trashedAt: String? = null,
    val smallPreviewUrl: String = "",
    val largePreviewUrl: String = "",
    val videoPreviewUrl: String = "",
    val photoFileRoles: List<String> = emptyList(),
    val processingErrorMessage: String = "",
    val isOwnedByCaller: Boolean = true,
    val ownerName: String = "",
)

/** PlaceVisit is one visit to a place: from arriving to leaving. */
@Serializable
data class PlaceVisit(
    val id: String,
    val place: LocationPlace,
    /** RFC 3339; null when the phone did not see the arrival. */
    val arrivedAt: String? = null,
    /** RFC 3339; null while the person is still there. */
    val departedAt: String? = null,
    /** Set when both times are. */
    val durationSeconds: Double? = null,
    val latitudeDegrees: Double,
    val longitudeDegrees: Double,
    val accuracyMeters: Double = 0.0,
    /** "visit", "region", "check_in" or "browser". */
    val visitSource: String = "visit",
    /** How many of the person's own photos were taken during the visit; null for another person's visit or without Photos. */
    val photoCount: Int? = null,
    /** The first few of them, earliest first; null when [photoCount] is. */
    val photos: List<VisitPhoto>? = null,
    /** RFC 3339: when the photos counted end; null when [photoCount] is. */
    val photosTakenBefore: String? = null,
) {
    val arrivedInstant: Instant? get() = ServerTime.instant(arrivedAt)
    val departedInstant: Instant? get() = ServerTime.instant(departedAt)

    /** Whether the person is still there, as far as the server knows. */
    val isOpen: Boolean get() = departedAt == null

    /** When it is placed on a day: the arrival, or the departure when the arrival was not seen. */
    val anchorInstant: Instant? get() = arrivedInstant ?: departedInstant

    /** How long the visit was, when both ends are known. */
    val visitDuration: Duration?
        get() {
            if (durationSeconds != null) return Duration.ofMillis((durationSeconds * 1000).toLong())
            val arrived = arrivedInstant ?: return null
            val departed = departedInstant ?: return null
            return Duration.ofMillis(maxOf(0L, departed.toEpochMilli() - arrived.toEpochMilli()))
        }
}

/** PersonLocation is where one person is: the latest point and the visit in progress. The person's own, or somebody sharing with them. */
@Serializable
data class PersonLocation(
    val userId: String,
    val username: String,
    val name: String? = null,
    val avatarUrl: String? = null,
    val avatarColorIndex: Int? = null,
    /** "owner" for the person's own, else current or history. */
    val shareLevel: String = "owner",
    /** What the person's own shares give the caller: current, history, or "" for nothing; null from a server that does not say. */
    val grantedShareLevel: String? = null,
    val latestPoint: LocationPoint? = null,
    val openVisit: PlaceVisit? = null,
    /** The pause in effect, when the person has paused. */
    val pause: LocationPause? = null,
    /** The caller's mute of this person's arrived and left notices; null when not muted. */
    val notificationMute: LocationNotificationMute? = null,
) {
    val level: LocationShareLevel get() = LocationShareLevel.fromServerWord(shareLevel) ?: LocationShareLevel.Current

    /** Whether the caller sees nothing of this person: they are listed, but the server sends no position, visit or pause. */
    val isHiddenFromCaller: Boolean get() = shareLevel.isEmpty()

    val displayName: String get() = if (name.isNullOrEmpty()) username else name

    /** Whether the caller muted this person's notices and the mute has not ended at an instant. */
    fun isNotificationMuted(at: Instant = Instant.now()): Boolean = notificationMute?.isInEffect(at) ?: false

    /** Whether the person has paused sharing where they are, at an instant. */
    fun isPaused(at: Instant = Instant.now()): Boolean = pause?.isInEffect(at) ?: false
}

/** LocationNotificationMute is the caller's mute of the arrived and left notices of one person sharing with them. */
@Serializable
data class LocationNotificationMute(
    /** RFC 3339; null mutes until the caller unmutes. */
    val mutedUntil: String? = null,
) {
    val mutedUntilInstant: Instant? get() = ServerTime.instant(mutedUntil)

    /** Whether the mute goes on until the caller unmutes. */
    val isOpenEnded: Boolean get() = mutedUntil == null

    /** Whether it is in effect at an instant: no end, or an end not yet reached. */
    fun isInEffect(at: Instant): Boolean {
        if (isOpenEnded) return true
        val until = mutedUntilInstant ?: return false
        return at.isBefore(until)
    }

    companion object {
        fun until(mutedUntilInstant: Instant?): LocationNotificationMute = LocationNotificationMute(mutedUntilInstant?.let { ServerTime.text(it) })
    }
}

/** FoundPlace is a named place near a position, from OpenStreetMap, which a check-in can be made at. */
@Serializable
data class FoundPlace(
    val placeName: String,
    val osmType: String,
    val osmId: Long,
    val osmCategory: String = "",
    val addressText: String = "",
    val latitudeDegrees: Double,
    val longitudeDegrees: Double,
    val distanceMeters: Double,
    /** The person's own place there, if they have one. */
    val placeId: String? = null,
    /** "" or one of PlaceLabel's words; set only on the person's own place. */
    val placeLabel: String = "",
    /** Whether placeName is the person's own name for their own place. */
    val isNamedByPerson: Boolean = false,
) {
    /** OpenStreetMap's own identity for it. */
    val id: String get() = "$osmType/$osmId"

    val label: PlaceLabel? get() = PlaceLabel.fromServerWord(placeLabel)

    /** The kind of place it is, from the OpenStreetMap tag ("amenity=cafe" is "cafe"); empty when there is none. */
    val categoryValue: String get() = LocationWords.categoryValue(osmCategory)
}

@Serializable
data class PlaceVisitPage(val placeVisits: List<PlaceVisit>, val nextOffset: Int? = null)

@Serializable
data class PlacePage(val places: List<LocationPlace>, val nextOffset: Int? = null)

@Serializable
data class FoundPlacePage(val foundPlaces: List<FoundPlace>, val nextOffset: Int? = null)

/** ReportLocationResult is what the server took of a batch. */
@Serializable
data class ReportLocationResult(
    val newPointCount: Int,
    val newVisitCount: Int,
    val updatedVisitCount: Int,
    /** The pause in effect after the report, so the phone learns of one set elsewhere. */
    val pause: LocationPause? = null,
    /** How many positions and visits the server dropped because they fell in a pause. */
    val pausedPointCount: Int = 0,
    val pausedVisitCount: Int = 0,
)

/** EraseLocationHistoryResult is what an erase removed. */
@Serializable
data class EraseLocationHistoryResult(val pointCount: Int, val visitCount: Int, val placeCount: Int)

/** LocationShare is one share of the person's location, with one person or every member of one circle. */
@Serializable
data class LocationShare(
    val id: String,
    val granteeUserId: String? = null,
    val granteeUsername: String? = null,
    val granteeName: String? = null,
    val granteeCircleId: String? = null,
    val granteeCircleName: String? = null,
    /** current or history. */
    val shareLevel: String,
    val createdAt: String = "",
) {
    val level: LocationShareLevel? get() = LocationShareLevel.fromServerWord(shareLevel)
    val isWithCircle: Boolean get() = granteeCircleId != null

    /** Who it is with: the person's name, the username when they have no name, or the circle's name. */
    val granteeDisplayName: String
        get() {
            if (isWithCircle) return granteeCircleName.orEmpty()
            if (!granteeName.isNullOrEmpty()) return granteeName
            return granteeUsername.orEmpty()
        }
}

/** LocationEvent is the server telling a viewer that a person they can see (themselves included) has a new position or visit. */
@Serializable
data class LocationEvent(val userId: String)

/** What kind of circle: family, friends or colleagues. */
enum class CircleKind(val serverWord: String) {
    Family("family"),
    Friends("friends"),
    Colleagues("colleagues");

    companion object {
        fun fromServerWord(word: String): CircleKind? = entries.firstOrNull { kind -> kind.serverWord == word }
    }
}

/** A person in a circle, as the circle lists them (the fields Location and Health read). */
@Serializable
data class CircleMember(
    val userId: String,
    val username: String,
    val name: String? = null,
    val avatarUrl: String? = null,
    val avatarColorIndex: Int? = null,
    val memberRole: String = "member",
) {
    val displayName: String get() = if (name.isNullOrEmpty()) username else name
}

/** PeopleCircle is a circle: a set of people on one server who know each other, named and described by its kind. */
@Serializable
data class PeopleCircle(
    val id: String,
    val name: String,
    /** family, friends or colleagues. */
    val circleKind: String,
    val createdAt: String = "",
    /** The caller's own role in it: admin or member. */
    val memberRole: String = "member",
    val members: List<CircleMember> = emptyList(),
) {
    val knownCircleKind: CircleKind? get() = CircleKind.fromServerWord(circleKind)
}

/**
 * LocationSetup is how far the person has got with setting Location up the first time: allowing it, labeling the place
 * they live as home, and, for each family circle they are in, sharing where they are with it. It is worked out from
 * what the server and the phone say and never stored; only the person's "Not now" is kept.
 */
data class LocationSetup(
    /** Whether the phone may record while the app is closed (all the time). */
    val isPermissionDone: Boolean,
    /** Whether a place labeled home exists. */
    val isHomeDone: Boolean,
    /** The family circles to offer sharing with, every one the person is in. */
    val familyCircles: List<PeopleCircle>,
    /** The identifiers of the family circles the person's location is already shared with. */
    val sharedFamilyCircleIds: Set<String>,
) {
    /** Whether the person's location is already shared with this circle. */
    fun isShared(circle: PeopleCircle): Boolean = sharedFamilyCircleIds.contains(circle.id)

    /** Whether the share rows are part of the setup: the person is in a family circle. */
    val isShareOffered: Boolean get() = familyCircles.isNotEmpty()

    /** Whether the location is shared with every family circle. */
    val isShareDone: Boolean get() = familyCircles.all { circle -> sharedFamilyCircleIds.contains(circle.id) }

    /** Whether every row that applies is done. */
    val isFinished: Boolean get() = isPermissionDone && isHomeDone && isShareDone

    /** Whether the setup is shown in place of the lists: no position yet, no "Not now", and something is left to do. */
    fun isShown(isDismissed: Boolean, hasLatestPoint: Boolean): Boolean = !isDismissed && !hasLatestPoint && !isFinished

    companion object {
        fun from(isPermissionDone: Boolean, homePlaces: List<LocationPlace>, circles: List<PeopleCircle>, shares: List<LocationShare>): LocationSetup {
            val families = circles.filter { circle -> circle.knownCircleKind == CircleKind.Family }
            val sharedCircleIds = shares.mapNotNull { share -> share.granteeCircleId }.toSet()
            return LocationSetup(
                isPermissionDone = isPermissionDone,
                isHomeDone = homePlaces.any { place -> place.label == PlaceLabel.Home },
                familyCircles = families,
                sharedFamilyCircleIds = families.map { circle -> circle.id }.toSet().intersect(sharedCircleIds),
            )
        }
    }
}

/** PlaceChange is what SavePlace is told: only the fields given change. With no place identifier it makes a place at the coordinates. */
data class PlaceChange(
    val placeId: String? = null,
    val placeName: String? = null,
    /** "" takes the label off. */
    val placeLabel: String? = null,
    val latitudeDegrees: Double? = null,
    val longitudeDegrees: Double? = null,
    val radiusMeters: Double? = null,
    /** The address the person chose, sent with the position it belongs to. */
    val addressText: String? = null,
) {
    val variables: Map<String, JsonElement>
        get() = buildMap {
            placeId?.let { put("placeId", JsonPrimitive(it)) }
            placeName?.let { put("placeName", JsonPrimitive(it.trim())) }
            placeLabel?.let { put("placeLabel", JsonPrimitive(it)) }
            latitudeDegrees?.let { put("latitudeDegrees", JsonPrimitive(it)) }
            longitudeDegrees?.let { put("longitudeDegrees", JsonPrimitive(it)) }
            radiusMeters?.let { put("radiusMeters", JsonPrimitive(it)) }
            addressText?.let { put("addressText", JsonPrimitive(it.trim())) }
        }
}

/**
 * CheckInTarget is where a check-in is made: a place of the person's own, a place found nearby, or just a position,
 * where the server uses the person's own place there or makes a new one and names it afterwards.
 */
sealed class CheckInTarget {
    data class OwnPlace(val placeId: String) : CheckInTarget()
    data class Found(val foundPlace: FoundPlace) : CheckInTarget()

    /** A position may carry how precise the device says it is, in meters; negative or not a number means unknown and is not sent. */
    data class Position(val latitudeDegrees: Double, val longitudeDegrees: Double, val accuracyMeters: Double? = null) : CheckInTarget()

    val variables: Map<String, JsonElement>
        get() = when (this) {
            is OwnPlace -> mapOf("placeId" to JsonPrimitive(placeId))
            is Position -> buildMap {
                put("latitudeDegrees", JsonPrimitive(latitudeDegrees))
                put("longitudeDegrees", JsonPrimitive(longitudeDegrees))
                if (accuracyMeters != null && accuracyMeters.isFinite() && accuracyMeters >= 0) put("accuracyMeters", JsonPrimitive(accuracyMeters))
            }
            is Found -> mapOf(
                "latitudeDegrees" to JsonPrimitive(foundPlace.latitudeDegrees), "longitudeDegrees" to JsonPrimitive(foundPlace.longitudeDegrees),
                "placeName" to JsonPrimitive(foundPlace.placeName), "osmType" to JsonPrimitive(foundPlace.osmType),
                "osmId" to JsonPrimitive(foundPlace.osmId), "osmCategory" to JsonPrimitive(foundPlace.osmCategory),
                "addressText" to JsonPrimitive(foundPlace.addressText),
            )
        }
}

/** The location documents. Copied from the iPhone app's `Documents` (ios/TeaNodeKit Location.swift), whose text the server's tests check. */
object LocationDocuments {
    // Each shared list of fields is written out in full, nested ones too: the server's test of the app's documents fills in one level of them.
    private const val POINT_FIELDS = """
        latitudeDegrees longitudeDegrees accuracyMeters altitudeMeters speedMetersPerSecond courseDegrees measuredAt locationSource
        """
    private const val PLACE_FIELDS = """
        id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
        visitCount lastVisitAt namingErrorMessage createdAt updatedAt
        """
    private const val PLACE_VISIT_FIELDS = """
        id arrivedAt departedAt durationSeconds latitudeDegrees longitudeDegrees accuracyMeters visitSource
        photoCount photosTakenBefore
        photos {
          id createdAt photoKind takenAt takenAtOffsetMinutes widthPixels heightPixels durationMS latitude longitude placeName cameraMake cameraModel
          originalFilename isFavorite isScreenshot photoProcessingState trashedAt smallPreviewUrl largePreviewUrl videoPreviewUrl
          photoFileRoles processingErrorMessage isOwnedByCaller ownerName
        }
        place {
          id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
          visitCount lastVisitAt namingErrorMessage createdAt updatedAt
        }
        """
    private const val PAUSE_FIELDS = """
        id startedAt endedAt
        """
    private const val PERSON_LOCATION_FIELDS = """
        userId username name avatarUrl avatarColorIndex shareLevel grantedShareLevel
        pause { id startedAt endedAt }
        notificationMute { mutedUntil }
        latestPoint { latitudeDegrees longitudeDegrees accuracyMeters altitudeMeters speedMetersPerSecond courseDegrees measuredAt locationSource }
        openVisit {
          id arrivedAt departedAt durationSeconds latitudeDegrees longitudeDegrees accuracyMeters visitSource
          place {
            id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
            visitCount lastVisitAt namingErrorMessage createdAt updatedAt
          }
        }
        """
    private const val SHARING_LIMIT_FIELDS = """
        shareLevelLimit sharingLimitReason phonePermission isRecordingOn reportedAt
        """
    private const val FOUND_PLACE_FIELDS = """
        placeName osmType osmId osmCategory addressText latitudeDegrees longitudeDegrees distanceMeters placeId placeLabel isNamedByPerson
        """

    const val LOCATION = """
        query (${'$'}userId: String) { GetLocation(userId: ${'$'}userId) { $PERSON_LOCATION_FIELDS } }
        """
    const val LOCATION_VISITS = """
        query (${'$'}userId: String, ${'$'}from: DateTime, ${'$'}until: DateTime, ${'$'}placeId: String, ${'$'}placeLabel: String, ${'$'}offset: Int, ${'$'}limitCount: Int) {
          ListLocationVisits(userId: ${'$'}userId, from: ${'$'}from, until: ${'$'}until, placeId: ${'$'}placeId, placeLabel: ${'$'}placeLabel, offset: ${'$'}offset, limitCount: ${'$'}limitCount) {
            placeVisits { $PLACE_VISIT_FIELDS }
            nextOffset
          }
        }
        """
    const val LOCATION_PLACES = """
        query (${'$'}userId: String, ${'$'}placeLabel: String, ${'$'}query: String, ${'$'}offset: Int, ${'$'}limitCount: Int) {
          ListPlaces(userId: ${'$'}userId, placeLabel: ${'$'}placeLabel, query: ${'$'}query, offset: ${'$'}offset, limitCount: ${'$'}limitCount) {
            places { $PLACE_FIELDS }
            nextOffset
          }
        }
        """
    const val NEARBY_PLACES = """
        query (${'$'}latitudeDegrees: Float!, ${'$'}longitudeDegrees: Float!, ${'$'}radiusMeters: Float) {
          ListNearbyPlaces(latitudeDegrees: ${'$'}latitudeDegrees, longitudeDegrees: ${'$'}longitudeDegrees, radiusMeters: ${'$'}radiusMeters) { $FOUND_PLACE_FIELDS }
        }
        """
    const val SEARCH_PLACES = """
        query (${'$'}query: String!, ${'$'}latitudeDegrees: Float, ${'$'}longitudeDegrees: Float, ${'$'}placeId: String, ${'$'}radiusMeters: Float, ${'$'}offset: Int, ${'$'}limitCount: Int) {
          SearchPlaces(query: ${'$'}query, latitudeDegrees: ${'$'}latitudeDegrees, longitudeDegrees: ${'$'}longitudeDegrees, placeId: ${'$'}placeId, radiusMeters: ${'$'}radiusMeters, offset: ${'$'}offset, limitCount: ${'$'}limitCount) {
            foundPlaces { $FOUND_PLACE_FIELDS }
            nextOffset
          }
        }
        """
    const val SEARCH_ADDRESSES = """
        query (${'$'}query: String!, ${'$'}latitudeDegrees: Float, ${'$'}longitudeDegrees: Float, ${'$'}placeId: String, ${'$'}limitCount: Int) {
          SearchAddresses(query: ${'$'}query, latitudeDegrees: ${'$'}latitudeDegrees, longitudeDegrees: ${'$'}longitudeDegrees, placeId: ${'$'}placeId, limitCount: ${'$'}limitCount) { $FOUND_PLACE_FIELDS }
        }
        """
    const val LOCATION_POINTS = """
        query (${'$'}userId: String, ${'$'}from: DateTime!, ${'$'}until: DateTime!) {
          ListLocationPoints(userId: ${'$'}userId, from: ${'$'}from, until: ${'$'}until) { $POINT_FIELDS }
        }
        """
    const val REPORT_LOCATION = """
        mutation (${'$'}agentPhoneId: String, ${'$'}points: [ReportedPointInput!]!, ${'$'}visits: [ReportedVisitInput!]!, ${'$'}activities: [ReportedActivityInput!]!) {
          ReportLocation(agentPhoneId: ${'$'}agentPhoneId, points: ${'$'}points, visits: ${'$'}visits, activities: ${'$'}activities) {
            newPointCount newVisitCount updatedVisitCount pausedPointCount pausedVisitCount
            pause { id startedAt endedAt }
          }
        }
        """

    /** The same report for a batch with no motion activity: a server that has no `activities` argument takes it. */
    const val REPORT_LOCATION_WITHOUT_ACTIVITIES = """
        mutation (${'$'}agentPhoneId: String, ${'$'}points: [ReportedPointInput!]!, ${'$'}visits: [ReportedVisitInput!]!) {
          ReportLocation(agentPhoneId: ${'$'}agentPhoneId, points: ${'$'}points, visits: ${'$'}visits) {
            newPointCount newVisitCount updatedVisitCount pausedPointCount pausedVisitCount
            pause { id startedAt endedAt }
          }
        }
        """
    const val LOCATION_PAUSES = """
        query (${'$'}userId: String, ${'$'}from: DateTime!, ${'$'}until: DateTime!) {
          ListLocationPauses(userId: ${'$'}userId, from: ${'$'}from, until: ${'$'}until) { $PAUSE_FIELDS }
        }
        """
    const val PAUSE_LOCATION = """
        mutation (${'$'}endedAt: DateTime) { PauseLocation(endedAt: ${'$'}endedAt) { $PAUSE_FIELDS } }
        """
    const val RESUME_LOCATION = """
        mutation { ResumeLocation }
        """
    const val SAVE_PLACE = """
        mutation (${'$'}placeId: String, ${'$'}placeName: String, ${'$'}placeLabel: String, ${'$'}latitudeDegrees: Float, ${'$'}longitudeDegrees: Float, ${'$'}radiusMeters: Float, ${'$'}addressText: String) {
          SavePlace(placeId: ${'$'}placeId, placeName: ${'$'}placeName, placeLabel: ${'$'}placeLabel, latitudeDegrees: ${'$'}latitudeDegrees, longitudeDegrees: ${'$'}longitudeDegrees, radiusMeters: ${'$'}radiusMeters, addressText: ${'$'}addressText) {
            $PLACE_FIELDS
          }
        }
        """
    const val FORGET_PLACE = """
        mutation (${'$'}placeId: String!) { ForgetPlace(placeId: ${'$'}placeId) }
        """
    const val FORGET_PLACE_VISIT = """
        mutation (${'$'}placeVisitId: String!) { ForgetPlaceVisit(placeVisitId: ${'$'}placeVisitId) }
        """
    const val CHECK_IN = """
        mutation (${'$'}placeId: String, ${'$'}latitudeDegrees: Float, ${'$'}longitudeDegrees: Float, ${'$'}accuracyMeters: Float, ${'$'}placeName: String, ${'$'}osmType: String, ${'$'}osmId: Int64, ${'$'}osmCategory: String, ${'$'}addressText: String) {
          CheckIn(placeId: ${'$'}placeId, latitudeDegrees: ${'$'}latitudeDegrees, longitudeDegrees: ${'$'}longitudeDegrees, accuracyMeters: ${'$'}accuracyMeters, placeName: ${'$'}placeName, osmType: ${'$'}osmType, osmId: ${'$'}osmId, osmCategory: ${'$'}osmCategory, addressText: ${'$'}addressText) {
            $PLACE_VISIT_FIELDS
          }
        }
        """
    const val MUTE_LOCATION_NOTIFICATIONS = """
        mutation (${'$'}userId: String!, ${'$'}mutedUntil: DateTime) {
          MuteLocationNotifications(userId: ${'$'}userId, mutedUntil: ${'$'}mutedUntil) { mutedUntil }
        }
        """
    const val UNMUTE_LOCATION_NOTIFICATIONS = """
        mutation (${'$'}userId: String!) { UnmuteLocationNotifications(userId: ${'$'}userId) }
        """
    const val ERASE_LOCATION_HISTORY = """
        mutation (${'$'}from: DateTime, ${'$'}until: DateTime) {
          EraseLocationHistory(from: ${'$'}from, until: ${'$'}until) { pointCount visitCount placeCount }
        }
        """

    // Sharing where the person is with a circle or a person, and seeing the people who share with them.
    const val SHARED_LOCATIONS = """
        query { ListSharedLocations { $PERSON_LOCATION_FIELDS } }
        """
    const val LOCATION_SHARES = """
        query {
          ListLocationShares { id granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName shareLevel createdAt }
        }
        """
    const val SHARE_LOCATION = """
        mutation (${'$'}granteeUserId: String, ${'$'}granteeCircleId: String, ${'$'}shareLevel: String!) {
          ShareLocation(granteeUserId: ${'$'}granteeUserId, granteeCircleId: ${'$'}granteeCircleId, shareLevel: ${'$'}shareLevel) {
            id granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName shareLevel createdAt
          }
        }
        """
    const val UNSHARE_LOCATION = """
        mutation (${'$'}shareId: String!) { UnshareLocation(shareId: ${'$'}shareId) }
        """
    const val LOCATION_SHARING_LIMIT = """
        query { ReadLocationSharingLimit { $SHARING_LIMIT_FIELDS } }
        """
    const val REPORT_LOCATION_PERMISSION = """
        mutation (${'$'}phonePermission: String!, ${'$'}isRecordingOn: Boolean!) {
          ReportLocationPermission(phonePermission: ${'$'}phonePermission, isRecordingOn: ${'$'}isRecordingOn) { $SHARING_LIMIT_FIELDS }
        }
        """
    const val LOCATION_EVENTS = """
        subscription { LocationEvents { userId } }
        """
}

private fun JsonElement?.orNull(): JsonElement = this ?: JsonNull

@Serializable private class GetLocationAnswer(@SerialName("GetLocation") val location: PersonLocation)
@Serializable private class ListLocationVisitsAnswer(@SerialName("ListLocationVisits") val page: PlaceVisitPage)
@Serializable private class ListPlacesAnswer(@SerialName("ListPlaces") val page: PlacePage)
@Serializable private class ListNearbyPlacesAnswer(@SerialName("ListNearbyPlaces") val foundPlaces: List<FoundPlace>)
@Serializable private class SearchPlacesAnswer(@SerialName("SearchPlaces") val page: FoundPlacePage)
@Serializable private class SearchAddressesAnswer(@SerialName("SearchAddresses") val foundPlaces: List<FoundPlace>)
@Serializable private class ListLocationPointsAnswer(@SerialName("ListLocationPoints") val points: List<LocationPoint>)
@Serializable private class ReportLocationAnswer(@SerialName("ReportLocation") val result: ReportLocationResult)
@Serializable private class SavePlaceAnswer(@SerialName("SavePlace") val place: LocationPlace)
@Serializable private class CheckInAnswer(@SerialName("CheckIn") val visit: PlaceVisit)
@Serializable private class EraseLocationHistoryAnswer(@SerialName("EraseLocationHistory") val result: EraseLocationHistoryResult)
@Serializable private class ListLocationPausesAnswer(@SerialName("ListLocationPauses") val pauses: List<LocationPause>)
@Serializable private class PauseLocationAnswer(@SerialName("PauseLocation") val pause: LocationPause)
@Serializable private class ResumeLocationAnswer(@SerialName("ResumeLocation") val didEnd: Boolean? = null)
@Serializable private class MuteLocationAnswer(@SerialName("MuteLocationNotifications") val mute: LocationNotificationMute? = null)
@Serializable private class UnmuteLocationAnswer(@SerialName("UnmuteLocationNotifications") val didEnd: Boolean? = null)
@Serializable private class ListSharedLocationsAnswer(@SerialName("ListSharedLocations") val people: List<PersonLocation>)
@Serializable private class ReadSharingLimitAnswer(@SerialName("ReadLocationSharingLimit") val limit: LocationSharingLimit)
@Serializable private class ReportPermissionAnswer(@SerialName("ReportLocationPermission") val limit: LocationSharingLimit)
@Serializable private class ListLocationSharesAnswer(@SerialName("ListLocationShares") val shares: List<LocationShare>)
@Serializable private class ShareLocationAnswer(@SerialName("ShareLocation") val share: LocationShare)
@Serializable private class IgnoredAnswer(@SerialName("ForgetPlace") val forgetPlace: Boolean? = null)
@Serializable private class IgnoredVisitAnswer(@SerialName("ForgetPlaceVisit") val forgetPlaceVisit: Boolean? = null)
@Serializable private class IgnoredUnshareAnswer(@SerialName("UnshareLocation") val unshareLocation: Boolean? = null)

/** Where the person is: the latest point and the visit in progress. */
suspend fun Connection.location(userId: String? = null): PersonLocation {
    val variables = buildMap { userId?.let { put("userId", JsonPrimitive(it)) } }
    return execute(LocationDocuments.LOCATION, GetLocationAnswer.serializer(), variables).location
}

/** Visits, newest first, a page at a time. [offset] is the previous page's `nextOffset`. */
suspend fun Connection.locationVisits(
    userId: String? = null, from: Instant? = null, until: Instant? = null, placeId: String? = null, placeLabel: String? = null,
    offset: Int = 0, limitCount: Int? = null,
): PlaceVisitPage {
    val variables = buildMap {
        userId?.let { put("userId", JsonPrimitive(it)) }
        from?.let { put("from", JsonPrimitive(ServerTime.text(it))) }
        until?.let { put("until", JsonPrimitive(ServerTime.text(it))) }
        placeId?.let { put("placeId", JsonPrimitive(it)) }
        placeLabel?.let { put("placeLabel", JsonPrimitive(it)) }
        if (offset > 0) put("offset", JsonPrimitive(offset))
        limitCount?.let { put("limitCount", JsonPrimitive(it)) }
    }
    return execute(LocationDocuments.LOCATION_VISITS, ListLocationVisitsAnswer.serializer(), variables).page
}

/** The person's places, a page at a time. */
suspend fun Connection.locationPlaces(
    userId: String? = null, placeLabel: String? = null, query: String? = null, offset: Int = 0, limitCount: Int? = null,
): PlacePage {
    val variables = buildMap {
        userId?.let { put("userId", JsonPrimitive(it)) }
        placeLabel?.let { put("placeLabel", JsonPrimitive(it)) }
        if (!query.isNullOrEmpty()) put("query", JsonPrimitive(query))
        if (offset > 0) put("offset", JsonPrimitive(offset))
        limitCount?.let { put("limitCount", JsonPrimitive(it)) }
    }
    return execute(LocationDocuments.LOCATION_PLACES, ListPlacesAnswer.serializer(), variables).page
}

/** Named places near a position, nearest first, for a check-in. With the place provider off the server answers the person's own places there. */
suspend fun Connection.nearbyPlaces(latitudeDegrees: Double, longitudeDegrees: Double, radiusMeters: Double? = null): List<FoundPlace> {
    val variables = buildMap {
        put("latitudeDegrees", JsonPrimitive(latitudeDegrees))
        put("longitudeDegrees", JsonPrimitive(longitudeDegrees))
        radiusMeters?.let { put("radiusMeters", JsonPrimitive(it)) }
    }
    return execute(LocationDocuments.NEARBY_PLACES, ListNearbyPlacesAnswer.serializer(), variables).foundPlaces
}

/** Places matching words near a position, from OpenStreetMap. With no position and no place, near where the person is now. */
suspend fun Connection.searchPlaces(
    query: String, latitudeDegrees: Double? = null, longitudeDegrees: Double? = null, placeId: String? = null, radiusMeters: Double? = null,
    offset: Int = 0, limitCount: Int? = null,
): FoundPlacePage {
    val variables = buildMap {
        put("query", JsonPrimitive(query))
        latitudeDegrees?.let { put("latitudeDegrees", JsonPrimitive(it)) }
        longitudeDegrees?.let { put("longitudeDegrees", JsonPrimitive(it)) }
        placeId?.let { put("placeId", JsonPrimitive(it)) }
        radiusMeters?.let { put("radiusMeters", JsonPrimitive(it)) }
        if (offset > 0) put("offset", JsonPrimitive(offset))
        limitCount?.let { put("limitCount", JsonPrimitive(it)) }
    }
    return execute(LocationDocuments.SEARCH_PLACES, SearchPlacesAnswer.serializer(), variables).page
}

/** Addresses matching words, anywhere in the world, nearest first to the given position, else to the place, else to the person's latest position. */
suspend fun Connection.searchAddresses(
    query: String, latitudeDegrees: Double? = null, longitudeDegrees: Double? = null, placeId: String? = null, limitCount: Int? = null,
): List<FoundPlace> {
    val variables = buildMap {
        put("query", JsonPrimitive(query))
        latitudeDegrees?.let { put("latitudeDegrees", JsonPrimitive(it)) }
        longitudeDegrees?.let { put("longitudeDegrees", JsonPrimitive(it)) }
        placeId?.let { put("placeId", JsonPrimitive(it)) }
        limitCount?.let { put("limitCount", JsonPrimitive(it)) }
    }
    return execute(LocationDocuments.SEARCH_ADDRESSES, SearchAddressesAnswer.serializer(), variables).foundPlaces
}

/** The points of a stretch of at most two days, for drawing the path. */
suspend fun Connection.locationPoints(userId: String? = null, from: Instant, until: Instant): List<LocationPoint> {
    val variables = buildMap {
        put("from", JsonPrimitive(ServerTime.text(from)))
        put("until", JsonPrimitive(ServerTime.text(until)))
        userId?.let { put("userId", JsonPrimitive(it)) }
    }
    return execute(LocationDocuments.LOCATION_POINTS, ListLocationPointsAnswer.serializer(), variables).points
}

/** Hands the server a batch. Safe to send twice: the server knows a point by when it was measured and a visit by its key. */
suspend fun Connection.reportLocation(batch: LocationBatch, agentPhoneId: String? = null): ReportLocationResult {
    val variables = batch.variables.toMutableMap()
    agentPhoneId?.let { variables["agentPhoneId"] = JsonPrimitive(it) }
    val document = if (batch.activities.isEmpty()) LocationDocuments.REPORT_LOCATION_WITHOUT_ACTIVITIES else LocationDocuments.REPORT_LOCATION
    return execute(document, ReportLocationAnswer.serializer(), variables).result
}

/** Names, labels, moves or resizes a place; with no identifier in the change, makes one at its coordinates. */
suspend fun Connection.savePlace(change: PlaceChange): LocationPlace =
    execute(LocationDocuments.SAVE_PLACE, SavePlaceAnswer.serializer(), change.variables).place

/** Deletes a place and its visits. */
suspend fun Connection.forgetPlace(placeId: String) {
    execute(LocationDocuments.FORGET_PLACE, IgnoredAnswer.serializer(), mapOf("placeId" to JsonPrimitive(placeId)))
}

/** Deletes one of the person's own visits, as if they had never been there. The place and its other visits stay. */
suspend fun Connection.forgetPlaceVisit(placeVisitId: String) {
    execute(LocationDocuments.FORGET_PLACE_VISIT, IgnoredVisitAnswer.serializer(), mapOf("placeVisitId" to JsonPrimitive(placeVisitId)))
}

/** Records that the person is at a place now. */
suspend fun Connection.checkIn(target: CheckInTarget): PlaceVisit =
    execute(LocationDocuments.CHECK_IN, CheckInAnswer.serializer(), target.variables).visit

/** Erases positions and visits in a range (with neither end, all of them), and the places that leaves with no visit that the person did not name or label. */
suspend fun Connection.eraseLocationHistory(from: Instant? = null, until: Instant? = null): EraseLocationHistoryResult {
    val variables = buildMap {
        from?.let { put("from", JsonPrimitive(ServerTime.text(it))) }
        until?.let { put("until", JsonPrimitive(ServerTime.text(it))) }
    }
    return execute(LocationDocuments.ERASE_LOCATION_HISTORY, EraseLocationHistoryAnswer.serializer(), variables).result
}

/** The pauses that overlap a range, newest first: the person's own, or somebody sharing their history with the caller. */
suspend fun Connection.locationPauses(userId: String? = null, from: Instant, until: Instant): List<LocationPause> {
    val variables = buildMap {
        put("from", JsonPrimitive(ServerTime.text(from)))
        put("until", JsonPrimitive(ServerTime.text(until)))
        userId?.let { put("userId", JsonPrimitive(it)) }
    }
    return execute(LocationDocuments.LOCATION_PAUSES, ListLocationPausesAnswer.serializer(), variables).pauses
}

/** Pauses recording now, until the end given or (with none) until the person resumes. With a pause already in effect, changes its end. */
suspend fun Connection.pauseLocation(endedAt: Instant? = null): LocationPause {
    val variables = buildMap { endedAt?.let { put("endedAt", JsonPrimitive(ServerTime.text(it))) } }
    return execute(LocationDocuments.PAUSE_LOCATION, PauseLocationAnswer.serializer(), variables).pause
}

/** Ends the pause in effect now. True when there was one. */
suspend fun Connection.resumeLocation(): Boolean =
    execute(LocationDocuments.RESUME_LOCATION, ResumeLocationAnswer.serializer()).didEnd ?: false

/** Mutes the arrived and left notices of a person sharing with the caller, until the end given or (with none) until the caller unmutes. */
suspend fun Connection.muteLocationNotifications(userId: String, mutedUntil: Instant? = null): LocationNotificationMute? {
    val variables = buildMap {
        put("userId", JsonPrimitive(userId))
        mutedUntil?.let { put("mutedUntil", JsonPrimitive(ServerTime.text(it))) }
    }
    return execute(LocationDocuments.MUTE_LOCATION_NOTIFICATIONS, MuteLocationAnswer.serializer(), variables).mute
}

/** Ends the mute in effect. True when there was one. */
suspend fun Connection.unmuteLocationNotifications(userId: String): Boolean =
    execute(LocationDocuments.UNMUTE_LOCATION_NOTIFICATIONS, UnmuteLocationAnswer.serializer(), mapOf("userId" to JsonPrimitive(userId))).didEnd ?: false

/** Every person sharing their location with the caller. */
suspend fun Connection.sharedLocations(): List<PersonLocation> =
    execute(LocationDocuments.SHARED_LOCATIONS, ListSharedLocationsAnswer.serializer()).people

/** What limits the caller's view of the people sharing with them: their phone's location permission, the recording switch and a pause. */
suspend fun Connection.locationSharingLimit(): LocationSharingLimit =
    execute(LocationDocuments.LOCATION_SHARING_LIMIT, ReadSharingLimitAnswer.serializer()).limit

/** Tells the server the phone's location permission and whether the app is recording, and gets the limit that follows from them. */
suspend fun Connection.reportLocationPermission(report: LocationPermissionReport): LocationSharingLimit {
    val variables = mapOf(
        "phonePermission" to JsonPrimitive(report.phonePermission.serverWord), "isRecordingOn" to JsonPrimitive(report.isRecordingOn),
    )
    return execute(LocationDocuments.REPORT_LOCATION_PERMISSION, ReportPermissionAnswer.serializer(), variables).limit
}

/** The shares of the person's own location. */
suspend fun Connection.locationShares(): List<LocationShare> =
    execute(LocationDocuments.LOCATION_SHARES, ListLocationSharesAnswer.serializer()).shares

/** Shares the person's location with a person or a circle (one of the two), or changes the level of a share already made. */
suspend fun Connection.shareLocation(granteeUserId: String?, granteeCircleId: String?, level: LocationShareLevel): LocationShare {
    val variables = mapOf(
        "granteeUserId" to (granteeUserId?.let { JsonPrimitive(it) }.orNull()),
        "granteeCircleId" to (granteeCircleId?.let { JsonPrimitive(it) }.orNull()),
        "shareLevel" to JsonPrimitive(level.serverWord),
    )
    return execute(LocationDocuments.SHARE_LOCATION, ShareLocationAnswer.serializer(), variables).share
}

/** Takes a share back. */
suspend fun Connection.unshareLocation(shareId: String) {
    execute(LocationDocuments.UNSHARE_LOCATION, IgnoredUnshareAnswer.serializer(), mapOf("shareId" to JsonPrimitive(shareId)))
}
