package com.teanode.kit.photos

import kotlinx.serialization.Serializable
import java.time.Instant
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.ZonedDateTime
import java.util.Locale

/** PhotoError is why a photo could not be read or saved, without any words: the app writes each in the person's language. */
sealed class PhotoError : Exception() {
    /** The server did not serve the file. */
    data object FileCouldNotBeRead : PhotoError()

    /** The server says there is no such file for this person. Another copy of the photo may stand in for it. */
    data object FileNotAvailable : PhotoError()

    /** The photo has no file and no preview yet. */
    data object NothingToSaveYet : PhotoError()

    /** The person has no address book to put a new contact in. */
    data object NoAddressBook : PhotoError()
}

/**
 * Photo is one item in the person's library: a picture, a video or a Live Photo. The keys are the server's. The three
 * preview addresses are paths on the server, empty until that preview is made; they carry the time the photo last
 * changed, so a new preview is a new address.
 */
@Serializable
data class Photo(
    val id: String,
    val createdAt: String,
    /** "picture", "video" or "livePhoto". */
    val photoKind: String,
    val takenAt: String? = null,
    /** How far the place it was taken was from UTC, in minutes, if known. */
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
    /** "waiting", "ready" or "failed". */
    val photoProcessingState: String = "ready",
    val trashedAt: String? = null,
    val smallPreviewUrl: String = "",
    val largePreviewUrl: String = "",
    val videoPreviewUrl: String = "",
    /** The roles of the files the server holds: "original", and "edited" and "liveVideo" when it has them. */
    val photoFileRoles: List<String> = emptyList(),
    /** The server's plain reason the last try at making previews failed; empty unless it did. */
    val processingErrorMessage: String = "",
    /** False for a photo read through an album shared with the person: no favorite mark or place of theirs, no Trash. */
    val isOwnedByCaller: Boolean = true,
    val ownerName: String = "",
    // The description's fields come in the same object (ReadPhoto asks for them).
    val photoDescription: String = "",
    val descriptionVisibleText: String = "",
    val describedAt: String? = null,
    val isDescriptionRequested: Boolean = false,
    val photoDocumentKind: String = "",
) {
    val hasEditedFile: Boolean get() = "edited" in photoFileRoles
    val hasLiveVideoFile: Boolean get() = "liveVideo" in photoFileRoles
    val isVideo: Boolean get() = photoKind == "video"
    val isLivePhoto: Boolean get() = photoKind == "livePhoto"
    val isWaiting: Boolean get() = photoProcessingState == "waiting"
    val isFailed: Boolean get() = photoProcessingState == "failed"
    val isTrashed: Boolean get() = trashedAt != null

    /** The picture a grid cell shows: the small preview, or the large one while only that exists. */
    val gridPreviewPath: String? get() = smallPreviewUrl.ifEmpty { largePreviewUrl.ifEmpty { null } }

    /** The picture the viewer shows: the large preview, or the small one until it is made. */
    val viewerPreviewPath: String? get() = largePreviewUrl.ifEmpty { smallPreviewUrl.ifEmpty { null } }

    val videoPath: String? get() = videoPreviewUrl.ifEmpty { null }

    /** The name to give the file when it is shared or saved. */
    val filename: String get() = originalFilename.ifEmpty { if (isVideo) "video.mp4" else "photo.jpg" }

    /** The description's own fields, as the viewer reads them. */
    val description: PhotoDescription
        get() = PhotoDescription(photoDescription, descriptionVisibleText, describedAt, isDescriptionRequested, photoDocumentKind)
}

/** PhotoFile is one file a photo is made of: its original, a version edited on a phone, or a Live Photo's video. */
@Serializable
data class PhotoFile(
    val photoFileRole: String,
    val contentType: String = "",
    val fileSizeBytes: Long = 0,
    val originalFilename: String = "",
    val fileUrl: String = "",
)

/** PhotoPreview is one smaller copy the server made of a photo. */
@Serializable
data class PhotoPreview(val photoPreviewSize: String, val contentType: String = "", val previewSizeBytes: Long = 0, val previewUrl: String = "")

/** PhotoAlbumReference is an album a photo is in, as the photo says it: enough to name it and to open it. */
@Serializable
data class PhotoAlbumReference(val id: String, val albumTitle: String)

/**
 * PhotoDescription is what a model wrote about a picture. The words are text a model wrote from a picture that may carry
 * words meant for it: shown, never obeyed.
 */
data class PhotoDescription(
    val photoDescription: String = "",
    val descriptionVisibleText: String = "",
    val describedAt: String? = null,
    val isDescriptionRequested: Boolean = false,
    /** Empty until described, then "none", "receipt", "statement" or "taxDocument". */
    val photoDocumentKind: String = "",
) {
    val isDescribed: Boolean get() = describedAt != null
}

/**
 * How a description that is on its way is waited for: asked about every few seconds while the person stays on the photo,
 * and every fifteen seconds once two minutes have passed, when the screen also says it is taking longer than usual.
 */
object PhotoDescriptionPolling {
    const val INTERVAL_SECONDS = 3
    const val SLOW_AFTER_SECONDS = 120
    const val SLOW_INTERVAL_SECONDS = 15

    fun isSlow(waitedSeconds: Int): Boolean = waitedSeconds >= SLOW_AFTER_SECONDS

    fun secondsBeforeAsking(waitedSeconds: Int): Int = if (isSlow(waitedSeconds)) SLOW_INTERVAL_SECONDS else INTERVAL_SECONDS
}

/** The person a face group's contact is on this server (the badge on a person). */
@Serializable
data class PhotoAccountPerson(
    val userId: String = "",
    val username: String = "",
    val name: String = "",
    val avatarUrl: String? = null,
    val avatarColorIndex: Int = 0,
)

/** PhotoFace is one face found in a photo. The box is in fractions of the picture from its top left corner. */
@Serializable
data class PhotoFace(
    val id: String,
    val photoId: String,
    val boxLeftFraction: Double = 0.0,
    val boxTopFraction: Double = 0.0,
    val boxWidthFraction: Double = 0.0,
    val boxHeightFraction: Double = 0.0,
    /** Empty for a face too small or too uncertain to be grouped. */
    val photoFaceGroupId: String = "",
    val contactId: String = "",
    val contactName: String = "",
    val isOwnContact: Boolean = false,
    val facePictureUrl: String = "",
) {
    val isGrouped: Boolean get() = photoFaceGroupId.isNotEmpty()
}

/** PhotoFaceGroup is the faces the server believes are one person. It is nobody until the person names it by choosing a contact. */
@Serializable
data class PhotoFaceGroup(
    val id: String,
    val createdAt: String = "",
    val updatedAt: String = "",
    val contactId: String = "",
    val contactAddressBookId: String = "",
    val contactName: String = "",
    val isOwnContact: Boolean = false,
    val faceCount: Int = 0,
    val photoCount: Int = 0,
    val facePictureUrl: String = "",
    val coverPhotoFaceId: String = "",
    val isHidden: Boolean = false,
    val accountUserId: String = "",
    val account: PhotoAccountPerson? = null,
) {
    val isInCircles: Boolean get() = accountUserId.isNotEmpty() && account != null
    val trimmedContactName: String get() = contactName.trim()
    val isNamed: Boolean get() = trimmedContactName.isNotEmpty()
}

/** What naming a group answered: a contact names one person, so a contact that named another group merges the two. */
@Serializable
data class NamedPhotoFaceGroup(
    val photoFaceGroup: PhotoFaceGroup,
    val mergedPhotoFaceGroupId: String = "",
    val isContactPictureSet: Boolean = false,
) {
    val isMerged: Boolean get() = mergedPhotoFaceGroupId.isNotEmpty()
}

/** A place of the person's own, as far as a photo's place shows it. */
@Serializable
data class PhotoOwnPlace(val id: String = "", val placeName: String = "", val latitudeDegrees: Double = 0.0, val longitudeDegrees: Double = 0.0)

/** What the server knows of where a photo was taken: the person's own place the position is inside, or else the street address. */
@Serializable
data class PhotoPositionPlace(val locationPlace: PhotoOwnPlace? = null, val addressText: String = "")

/** PhotoDetail is a photo with the files and previews the server holds for it, the albums it is in, and its faces. */
@Serializable
data class PhotoDetail(
    val photo: Photo,
    val photoFiles: List<PhotoFile> = emptyList(),
    val photoPreviews: List<PhotoPreview> = emptyList(),
    val albums: List<PhotoAlbumReference> = emptyList(),
    val faces: List<PhotoFace> = emptyList(),
) {
    val photoDescription: PhotoDescription get() = photo.description
    val originalFile: PhotoFile? get() = photoFiles.firstOrNull { file -> file.photoFileRole == "original" }

    fun file(photoFileRole: String): PhotoFile? = photoFiles.firstOrNull { file -> file.photoFileRole == photoFileRole }
}

/** PhotoLibrary is the counts and limits of the person's library, and what the server offers for it. */
@Serializable
data class PhotoLibrary(
    val photoCount: Int,
    val videoCount: Int,
    val favoriteCount: Int,
    val trashedCount: Int,
    val usedBytes: Long,
    val maximumFileBytes: Long = 0,
    val maximumAccountBytes: Long = 0,
    val trashRetentionDays: Int,
    val isDescriptionAvailable: Boolean = false,
    val descriptionUnavailableMessage: String = "",
    val isFaceRecognitionAvailable: Boolean = false,
    val faceRecognitionUnavailableMessage: String = "",
    val isPhotoFaceRecognitionEnabled: Boolean = false,
    val isPhotoReadingEnabled: Boolean = false,
) {
    val itemCount: Int get() = photoCount + videoCount
}

/** PhotoAlbum is a named set of photos. Others' albums carry who owns them and what the person may do. */
@Serializable
data class PhotoAlbum(
    val id: String,
    val albumTitle: String,
    val createdAt: String = "",
    val updatedAt: String = "",
    val coverPhotoId: String? = null,
    val photoCount: Int = 0,
    val firstTakenAt: String? = null,
    val lastTakenAt: String? = null,
    val coverPreviewUrl: String = "",
    val ownerUserId: String? = null,
    val circleId: String? = null,
    val ownerUsername: String = "",
    val ownerName: String = "",
    val ownerAvatarUrl: String? = null,
    val ownerAvatarColorIndex: Int = 0,
    val circleName: String = "",
    val isOwnedByCaller: Boolean = true,
    val shareLevel: String = "",
    val canCallerContribute: Boolean = true,
    val canCallerManage: Boolean = true,
    val isLocationIncluded: Boolean = true,
) {
    val isCircleAlbum: Boolean get() = circleId != null

    /** An album somebody shared with the person directly (a circle's albums are the circle's). */
    val isSharedWithCaller: Boolean get() = !isOwnedByCaller && circleId == null

    /** Only the owner of an album shares it; a circle's album is already the circle's. */
    val canCallerShare: Boolean get() = canCallerManage && circleId == null

    /** Whether the person may take this photo out of the album: their own photo where they may contribute, anyone's where they manage. */
    fun canCallerRemove(photo: Photo): Boolean = if (photo.isOwnedByCaller) canCallerContribute else canCallerManage

    val ownerDisplayName: String get() = ownerName.ifEmpty { ownerUsername }

    fun dateRangeText(zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String? =
        PhotoAlbumText.dateRange(firstTakenAt, lastTakenAt, zone, locale)
}

/** The albums of one circle. */
data class PhotoCircleAlbums(val circleId: String, val circleName: String, val albums: List<PhotoAlbum>)

/** The albums the Albums screen shows, in its three groups. */
data class PhotoAlbumSections(val own: List<PhotoAlbum>, val sharedWithYou: List<PhotoAlbum>, val circles: List<PhotoCircleAlbums>) {
    val hasOthers: Boolean get() = sharedWithYou.isNotEmpty() || circles.isNotEmpty()

    companion object {
        fun of(albums: List<PhotoAlbum>): PhotoAlbumSections {
            val ownAlbums = albums.filter { album -> album.isOwnedByCaller && album.circleId == null }
            val shared = albums.filter { album -> album.isSharedWithCaller }
            val byCircle = albums.filter { album -> album.circleId != null }.groupBy { album -> album.circleId!! }
            val circles = byCircle.map { (circleId, members) -> PhotoCircleAlbums(circleId, members.first().circleName, members) }
                .sortedBy { circle -> circle.circleName.lowercase() }
            return PhotoAlbumSections(ownAlbums, shared, circles)
        }
    }
}

/** A group of photos the server suggests making an album of: a trip, an event. */
@Serializable
data class PhotoAlbumSuggestion(
    val takenAfter: String,
    val takenBefore: String,
    val placeNames: List<String> = emptyList(),
    val albumTitle: String,
    val photoCount: Int = 0,
    val samplePhotoIds: List<String> = emptyList(),
) {
    val id: String get() = "$takenAfter/$takenBefore"

    /** Whether the title only repeats the places, which the card then does not show twice. */
    val isTitleMadeOfPlaces: Boolean
        get() = placeNames.isNotEmpty() && placeNames.all { placeName ->
            placeName.split(", ").map { part -> part.trim() }.filter { part -> part.isNotEmpty() }.all { part -> albumTitle.contains(part) }
        }

    fun dateRangeText(zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String? =
        PhotoAlbumText.dateRange(takenAfter, takenBefore, zone, locale)

    companion object {
        const val SHOWN_SAMPLE_COUNT = 4
    }
}

object PhotoAlbumText {
    fun dateRange(first: String?, last: String?, zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String? {
        val firstInstant = ServerTime.instant(first) ?: return null
        val lastInstant = ServerTime.instant(last) ?: return null
        val format = java.time.format.DateTimeFormatter.ofLocalizedDate(java.time.format.FormatStyle.MEDIUM).withLocale(locale)
        val firstDate = firstInstant.atZone(zone).toLocalDate()
        val lastDate = maxOf(lastInstant, firstInstant).atZone(zone).toLocalDate()
        if (firstDate == lastDate) return format.format(firstDate)
        return "${format.format(firstDate)} - ${format.format(lastDate)}"
    }

    fun trimmedTitle(title: String): String = title.trim()
}

/** The server takes at most this many photo ids in a call; a longer list goes in batches. */
object PhotoBatches {
    const val MOST_PHOTO_IDS_PER_CALL = 500

    fun batches(photoIds: List<String>, size: Int = MOST_PHOTO_IDS_PER_CALL): List<List<String>> =
        if (size <= 0) emptyList() else photoIds.chunked(size)
}

/** A place the server found by name, for filtering photos taken near it or putting photos there. */
@Serializable
data class PhotoPlace(val placeName: String, val latitude: Double, val longitude: Double, val distanceKilometers: Double? = null)

/** The three parts of the library the person looks at. */
enum class PhotoCollection(val isFavoriteFilter: Boolean?, val isTrashedFilter: Boolean?) {
    Library(null, null),
    Favorites(true, null),
    Trash(null, true),
}

object PhotoSearch {
    const val MOST_RESULT_COUNT = 500
    const val MOST_PLACE_WORDS_LENGTH = 100

    /** The words to search for, or null when nothing was typed. */
    fun words(typed: String): String? = typed.trim().ifEmpty { null }
}

/** How the timeline reads a page at a time. */
object PhotoPaging {
    const val PAGE_SIZE = 200

    /** How many photos from the end of what is held a cell shows before the next page is asked for. */
    const val LOAD_AHEAD = 60

    fun hasMore(pageCount: Int, pageSize: Int = PAGE_SIZE): Boolean = pageCount >= pageSize

    fun shouldLoadMore(photoId: String, photos: List<Photo>, loadAhead: Int = LOAD_AHEAD): Boolean {
        val index = photos.indexOfLast { photo -> photo.id == photoId }
        return index >= 0 && index >= photos.size - loadAhead
    }

    fun appending(page: List<Photo>, photos: List<Photo>): List<Photo> {
        val known = photos.map { photo -> photo.id }.toSet()
        return photos + page.filter { photo -> photo.id !in known }
    }
}

/** Where a changed photo goes among the ones held, and whether a later page is needed to be sure of it. */
data class PhotoPlacement(val photos: List<Photo>, val pageCursorPhotoId: String?, val isBeyondHeldPhotos: Boolean) {
    companion object {
        fun place(photo: Photo, photos: List<Photo>, pageCursorPhotoId: String?, hasMore: Boolean): PhotoPlacement {
            val cursor = pageCursorPhotoId ?: photos.lastOrNull()?.id
            val oldIndex = photos.indexOfFirst { held -> held.id == photo.id }
            if (oldIndex < 0) return PhotoPlacement(photos, cursor, false)
            var nextCursor = cursor
            if (cursor == photo.id) nextCursor = if (oldIndex > 0) photos[oldIndex - 1].id else null
            val others = photos.toMutableList().also { list -> list.removeAt(oldIndex) }
            val newIndex = others.indexOfFirst { other -> PhotoTimeline.isListedBefore(photo, other) }.let { index -> if (index < 0) others.size else index }
            others.add(newIndex, photo)
            return PhotoPlacement(others, nextCursor, hasMore && others.size > 1 && newIndex == others.size - 1)
        }
    }
}

/** When a photo was taken and in which zone to read it. */
data class PhotoMoment(val instant: Instant, val zone: ZoneId, val isZoneOfPhoto: Boolean)

/** One calendar day of photos. */
data class PhotoDay(val id: String, val year: Int?, val month: Int?, val day: Int?, val photos: List<Photo>) {
    /** Noon UTC on the day, a moment that is the same calendar day in every zone but the extreme ones. */
    val noonUtc: Instant?
        get() = if (year == null || month == null || day == null) null else ZonedDateTime.of(year, month, day, 12, 0, 0, 0, ZoneOffset.UTC).toInstant()
}

/** One day's worth of photos in the Trash, with how long they stay. */
data class PhotoTrashDay(val id: String, val trashedAt: Instant?, val daysLeft: Int?, val photos: List<Photo>)

object PhotoTimeline {
    /** When the photo was taken, read in the zone of the place when it says it, else the phone's. */
    fun moment(photo: Photo, phoneZone: ZoneId = ZoneId.systemDefault()): PhotoMoment? {
        ServerTime.instant(photo.takenAt)?.let { takenAt ->
            val offsetMinutes = photo.takenAtOffsetMinutes
            return if (offsetMinutes != null) PhotoMoment(takenAt, ServerTime.zoneOfOffsetMinutes(offsetMinutes), true) else PhotoMoment(takenAt, phoneZone, false)
        }
        ServerTime.instant(photo.createdAt)?.let { createdAt -> return PhotoMoment(createdAt, phoneZone, false) }
        return null
    }

    /** A search lists by relevance, not by day: one day with no date. */
    fun rankedDay(photos: List<Photo>): List<PhotoDay> = if (photos.isEmpty()) emptyList() else listOf(PhotoDay("ranked", null, null, null, photos))

    fun trashDaysLeft(trashedAt: Instant?, retentionDays: Int, now: Instant): Int? {
        if (trashedAt == null || retentionDays <= 0) return null
        val elapsedDays = ((now.epochSecond - trashedAt.epochSecond) / 86_400).toInt()
        return maxOf(0, retentionDays - elapsedDays)
    }

    /** The Trash in runs of photos trashed on the same day (the phone's), newest first as the server lists them. */
    fun trashDays(photos: List<Photo>, retentionDays: Int, now: Instant = Instant.now(), phoneZone: ZoneId = ZoneId.systemDefault()): List<PhotoTrashDay> {
        val days = mutableListOf<PhotoTrashDay>()
        var runKey: String? = null
        var runPhotos = mutableListOf<Photo>()
        var runTrashedAt: Instant? = null
        var runOldestTrashedAt: Instant? = null
        fun closeRun() {
            val key = runKey ?: return
            days.add(PhotoTrashDay(key, runTrashedAt, trashDaysLeft(runOldestTrashedAt, retentionDays, now), runPhotos))
        }
        for (photo in photos) {
            val trashedAt = ServerTime.instant(photo.trashedAt)
            val key = trashedAt?.atZone(phoneZone)?.toLocalDate()?.toString() ?: "unknown"
            if (key != runKey) {
                closeRun()
                runKey = key
                runPhotos = mutableListOf()
                runTrashedAt = trashedAt
            }
            runPhotos.add(photo)
            runOldestTrashedAt = trashedAt
        }
        closeRun()
        return days
    }

    /** The photos in runs of calendar days, each read in the zone the photo was taken in. */
    fun days(photos: List<Photo>, phoneZone: ZoneId = ZoneId.systemDefault()): List<PhotoDay> {
        val order = mutableListOf<String>()
        val members = mutableMapOf<String, MutableList<Photo>>()
        val parts = mutableMapOf<String, java.time.LocalDate>()
        for (photo in photos) {
            var key = "unknown"
            moment(photo, phoneZone)?.let { moment ->
                val date = moment.instant.atZone(moment.zone).toLocalDate()
                key = date.toString()
                parts[key] = date
            }
            if (key !in members) order.add(key)
            members.getOrPut(key) { mutableListOf() }.add(photo)
        }
        return order.map { key ->
            val date = parts[key]
            PhotoDay(key, date?.year, date?.monthValue, date?.dayOfMonth, members.getValue(key))
        }
    }

    /** The order the server lists in: newest first, ties broken by id, the undated last. */
    fun isListedBefore(first: Photo, second: Photo): Boolean {
        val firstInstant = moment(first)?.instant
        val secondInstant = moment(second)?.instant
        if (firstInstant != secondInstant) {
            if (firstInstant == null) return false
            if (secondInstant == null) return true
            return firstInstant > secondInstant
        }
        return first.id > second.id
    }

    fun sorted(photos: List<Photo>): List<Photo> =
        photos.sortedWith { first, second -> if (isListedBefore(first, second)) -1 else if (isListedBefore(second, first)) 1 else 0 }

    /** The words that name a photo where it is quoted: its place and its date, else its file name. */
    fun referenceTitle(photo: Photo, phoneZone: ZoneId = ZoneId.systemDefault()): String {
        val parts = mutableListOf<String>()
        val placeName = photo.placeName.trim()
        if (placeName.isNotEmpty()) parts.add(placeName)
        if (photo.takenAt != null) {
            moment(photo, phoneZone)?.let { moment -> parts.add(moment.instant.atZone(moment.zone).toLocalDate().toString()) }
        }
        if (parts.isEmpty()) return photo.originalFilename.trim()
        return parts.joinToString(" · ")
    }

    /** A video's length as the player writes it: m:ss, or h:mm:ss from an hour. */
    fun durationText(milliseconds: Int): String {
        val totalSeconds = maxOf(0, (milliseconds + 500) / 1000)
        val hours = totalSeconds / 3600
        val minutes = (totalSeconds % 3600) / 60
        val seconds = totalSeconds % 60
        return if (hours > 0) "%d:%02d:%02d".format(hours, minutes, seconds) else "%d:%02d".format(minutes, seconds)
    }
}

/** The file extension a downloaded photo gets from the type the server says it is. */
object PhotoDownload {
    fun fileExtension(contentType: String): String? = when (contentType.substringBefore(';').trim().lowercase()) {
        "image/jpeg", "image/jpg" -> "jpg"
        "image/png" -> "png"
        "image/heic" -> "heic"
        "image/heif" -> "heif"
        "image/gif" -> "gif"
        "image/webp" -> "webp"
        "video/mp4" -> "mp4"
        "video/quicktime" -> "mov"
        else -> null
    }
}

/** Which copy of a photo a signed playback address is asked for. */
sealed class PhotoPlaybackSource {
    data class Preview(val photoPreviewSize: String) : PhotoPlaybackSource()

    data class File(val photoFileRole: String) : PhotoPlaybackSource()
}

/** What a person types to find a contact to name a face group, and the rules for offering a new one. */
object PhotoNaming {
    const val CONTACTS_PER_BOOK = 20

    fun trimmedName(name: String): String = name.trim()

    fun shownName(group: PhotoFaceGroup): String? = if (group.isNamed) group.trimmedContactName else null

    /** A new contact is offered when something is typed that no listed contact already is. */
    fun offersNewContact(typed: String, contactNames: List<String>): Boolean {
        val name = trimmedName(typed)
        if (name.isEmpty()) return false
        return contactNames.none { contactName -> contactName.equals(name, ignoreCase = true) }
    }
}
