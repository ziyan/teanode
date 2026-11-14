package com.teanode.kit.photos

import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/** What the description part of a photo's info offers the person. */
sealed class PhotoDescriptionOffer {
    /** A description is on its way, or was just asked for. */
    data object Waiting : PhotoDescriptionOffer()

    /** Nothing has been written about the photo yet, and the server can write it. */
    data object Describe : PhotoDescriptionOffer()

    /** The photo has a description, and the server can write a new one. */
    data object DescribeAgain : PhotoDescriptionOffer()

    /** Describing is off on this server; [reasonText] is the server's reason, empty when it gave none. */
    data class Unavailable(val reasonText: String) : PhotoDescriptionOffer()

    /** Nothing to offer: the photo is somebody else's, or the library is not known yet. */
    data object Nothing : PhotoDescriptionOffer()
}

/** Which of the description's buttons and words the info shows (ios PhotoInfoView.descriptionSection). */
object PhotoDescriptionOffers {
    /**
     * Describing is the owner's: the server refuses it for a photo shared with the person. [library] is null until the
     * library has been read, and then no reason is given for a missing offer.
     */
    fun offer(photo: Photo, description: PhotoDescription, library: PhotoLibrary?, isAsking: Boolean): PhotoDescriptionOffer {
        if (description.isDescriptionRequested || isAsking) return PhotoDescriptionOffer.Waiting
        if (!photo.isOwnedByCaller) return PhotoDescriptionOffer.Nothing
        val isAvailable = library?.isDescriptionAvailable == true
        return when {
            description.isDescribed -> if (isAvailable) PhotoDescriptionOffer.DescribeAgain else PhotoDescriptionOffer.Nothing
            isAvailable -> PhotoDescriptionOffer.Describe
            library != null -> PhotoDescriptionOffer.Unavailable(library.descriptionUnavailableMessage)
            else -> PhotoDescriptionOffer.Nothing
        }
    }

    /** Whether the line under the description explains what describing does: only before the first description. */
    fun isHintShown(photo: Photo, description: PhotoDescription, library: PhotoLibrary?): Boolean =
        photo.isOwnedByCaller && !description.isDescribed && !description.isDescriptionRequested && library?.isDescriptionAvailable == true
}

/** The people in a photo, as the info lists them. */
object PhotoFaces {
    /** Faces the server put in a group: the ones that are a person. */
    fun people(faces: List<PhotoFace>): List<PhotoFace> = faces.filter { face -> face.isGrouped }

    /** The name of a face's person, or null for one nobody has named. */
    fun shownName(face: PhotoFace): String? = face.contactName.trim().ifEmpty { null }

    /**
     * The names of the people in somebody else's photo, each once: all that is theirs to see, since the people are the
     * owner's to open and correct.
     */
    fun namesInSharedPhoto(faces: List<PhotoFace>): List<String> = faces.mapNotNull { face -> shownName(face) }.distinct()
}

/** How a position and a window of time are written. */
object PhotoPositionText {
    /** Where a photo was taken as numbers, for a position no city is near; null when it has none. */
    fun coordinates(photo: Photo): String? {
        val latitude = photo.latitude ?: return null
        val longitude = photo.longitude ?: return null
        return String.format(Locale.ROOT, "%.4f, %.4f", latitude, longitude)
    }

    /**
     * The window of time a filter narrows the library to, for its chip: both ends when there are two, in order, else the
     * one there is.
     */
    fun windowText(takenAfter: Instant?, takenBefore: Instant?, zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String? {
        val formatter = DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT).withLocale(locale).withZone(zone)
        if (takenAfter != null && takenBefore != null && takenAfter < takenBefore) return formatter.format(takenAfter) + " - " + formatter.format(takenBefore)
        val only = takenAfter ?: takenBefore ?: return null
        return formatter.format(only)
    }
}
