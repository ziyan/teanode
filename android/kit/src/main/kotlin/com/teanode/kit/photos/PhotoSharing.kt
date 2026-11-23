package com.teanode.kit.photos

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZoneOffset

/** The kind of item a filter keeps. */
enum class PhotoKindFilter(val serverName: String) {
    All(""),
    Picture("picture"),
    LivePhoto("livePhoto"),
    Video("video"),
}

/** Photos taken near a place: the place and how far from it counts as near. */
data class PhotoNearFilter(
    val latitudeDegrees: Double,
    val longitudeDegrees: Double,
    val distanceKilometers: Double = DEFAULT_DISTANCE_KILOMETERS,
    val placeName: String = "",
) {
    companion object {
        const val DEFAULT_DISTANCE_KILOMETERS = 1.0

        /** Near a photo, when it has a position. */
        fun near(photo: Photo, distanceKilometers: Double = DEFAULT_DISTANCE_KILOMETERS): PhotoNearFilter? {
            val latitude = photo.latitude ?: return null
            val longitude = photo.longitude ?: return null
            return PhotoNearFilter(latitude, longitude, distanceKilometers, photo.placeName.trim())
        }
    }
}

/**
 * PhotoFilter narrows the timeline: by kind, by a first and last day (in the phone's zone), by a place, by whose photos.
 * Setting a day clears an exact moment that came from the person's own date picking.
 */
data class PhotoFilter(
    val photoKindFilter: PhotoKindFilter = PhotoKindFilter.All,
    val firstDay: LocalDate? = null,
    val lastDay: LocalDate? = null,
    val photoNearFilter: PhotoNearFilter? = null,
    val accountUserId: String? = null,
    val accountName: String = "",
    val takenAfter: Instant? = null,
    val takenBefore: Instant? = null,
) {
    val isActive: Boolean
        get() = photoKindFilter != PhotoKindFilter.All || firstDay != null || lastDay != null || photoNearFilter != null ||
            !accountUserId.isNullOrEmpty() || takenAfter != null || takenBefore != null

    /** A first day after the last day finds nothing: the screen says so rather than listing nothing. */
    val isRangeBackwards: Boolean get() = firstDay != null && lastDay != null && firstDay > lastDay

    /** The variables the server's ListPhotos takes for this filter. */
    fun variables(zone: ZoneId = ZoneId.systemDefault()): Map<String, JsonElement> {
        val variables = mutableMapOf<String, JsonElement>()
        if (photoKindFilter != PhotoKindFilter.All) variables["photoKind"] = JsonPrimitive(photoKindFilter.serverName)
        if (takenAfter != null) {
            variables["takenAfter"] = JsonPrimitive(ServerTime.text(takenAfter))
        } else if (firstDay != null) {
            variables["takenAfter"] = JsonPrimitive(ServerTime.text(firstDay.atStartOfDay(zone).toInstant()))
        }
        if (takenBefore != null) {
            variables["takenBefore"] = JsonPrimitive(ServerTime.text(takenBefore))
        } else if (lastDay != null) {
            variables["takenBefore"] = JsonPrimitive(ServerTime.text(lastDay.plusDays(1).atStartOfDay(zone).toInstant()))
        }
        if (!accountUserId.isNullOrEmpty()) variables["accountUserId"] = JsonPrimitive(accountUserId)
        // The server wants all three of the place or none.
        if (photoNearFilter != null) {
            variables["nearLatitude"] = JsonPrimitive(photoNearFilter.latitudeDegrees)
            variables["nearLongitude"] = JsonPrimitive(photoNearFilter.longitudeDegrees)
            variables["nearDistanceKilometers"] = JsonPrimitive(photoNearFilter.distanceKilometers)
        }
        return variables
    }
}

/** Writing a date a person picked as the server reads it, in the zone the photo was taken in. */
object PhotoDateChange {
    fun takenAtText(instant: Instant, photo: Photo, phoneZone: ZoneId = ZoneId.systemDefault()): String {
        val offsetSeconds = photo.takenAtOffsetMinutes?.let { minutes -> minutes * 60 } ?: phoneZone.rules.getOffset(instant).totalSeconds
        return takenAtText(instant, offsetSeconds)
    }

    fun takenAtText(instant: Instant, offsetSeconds: Int): String {
        val offset = ZoneOffset.ofTotalSeconds(offsetSeconds)
        val local = instant.atOffset(offset)
        val sign = if (offsetSeconds < 0) "-" else "+"
        val absolute = kotlin.math.abs(offsetSeconds)
        return "%04d-%02d-%02dT%02d:%02d:%02d%s%02d:%02d".format(
            local.year, local.monthValue, local.dayOfMonth, local.hour, local.minute, local.second, sign, absolute / 3600, (absolute % 3600) / 60,
        )
    }

    fun zone(photo: Photo, phoneZone: ZoneId = ZoneId.systemDefault()): ZoneId = PhotoTimeline.moment(photo, phoneZone)?.zone ?: phoneZone
}

/** What a person an album is shared with may do. */
enum class PhotoShareLevel(val serverName: String) {
    View("view"),
    Contribute("contribute"),
}

/** PhotoAlbumShare is one person or circle an album is shared with. */
@Serializable
data class PhotoAlbumShare(
    val id: String,
    val photoAlbumId: String,
    val granteeUserId: String? = null,
    val granteeUsername: String = "",
    val granteeName: String = "",
    val granteeAvatarUrl: String? = null,
    val granteeAvatarColorIndex: Int = 0,
    val granteeCircleId: String? = null,
    val granteeCircleName: String = "",
    val shareLevel: String = "view",
    val isLocationIncluded: Boolean = false,
    val isFaceNamesIncluded: Boolean = false,
    val createdAt: String = "",
) {
    val isWithCircle: Boolean get() = granteeCircleId != null
    val knownShareLevel: PhotoShareLevel get() = PhotoShareLevel.entries.firstOrNull { level -> level.serverName == shareLevel } ?: PhotoShareLevel.View
    val granteeDisplayName: String get() = if (isWithCircle) granteeCircleName else granteeName.ifEmpty { granteeUsername }
}

/** Who a new share goes to. */
sealed class PhotoShareTarget {
    data class Person(val userId: String) : PhotoShareTarget()

    data class Circle(val circleId: String) : PhotoShareTarget()
}

/** The choices of the share sheet. */
data class PhotoShareDraft(
    val target: PhotoShareTarget? = null,
    val photoShareLevel: PhotoShareLevel = PhotoShareLevel.View,
    val isLocationIncluded: Boolean = false,
    val isFaceNamesIncluded: Boolean = false,
) {
    val canSubmit: Boolean get() = target != null

    fun variables(photoAlbumId: String): Map<String, JsonElement> {
        val variables = mutableMapOf<String, JsonElement>(
            "photoAlbumId" to JsonPrimitive(photoAlbumId), "shareLevel" to JsonPrimitive(photoShareLevel.serverName),
            "isLocationIncluded" to JsonPrimitive(isLocationIncluded), "isFaceNamesIncluded" to JsonPrimitive(isFaceNamesIncluded),
        )
        when (target) {
            is PhotoShareTarget.Person -> variables["granteeUserId"] = JsonPrimitive(target.userId)
            is PhotoShareTarget.Circle -> variables["granteeCircleId"] = JsonPrimitive(target.circleId)
            null -> Unit
        }
        return variables
    }
}
