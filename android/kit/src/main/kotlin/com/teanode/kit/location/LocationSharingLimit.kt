package com.teanode.kit.location

import kotlinx.serialization.Serializable

// What limits how much of other people's location the person sees: their own phone's location permission, the
// recording switch and a pause. The server applies the rule; this file reads its answer and decides what to tell the
// person about it. Mirrors ios/TeaNodeKit LocationSharingLimit.swift.

/** LocationPhonePermission is the phone's location permission as the server takes it: all the time, while using, or off. */
enum class LocationPhonePermission(val serverWord: String) {
    Always("always"),
    WhileUsing("while_using"),
    Off("off"),
}

/** LocationPermissionReport is what the phone tells the server about its own permission and recording switch. */
data class LocationPermissionReport(val phonePermission: LocationPhonePermission, val isRecordingOn: Boolean)

/** LocationSharingLimit is the most of anybody's location the person may see, and why it is less than everything. */
@Serializable
data class LocationSharingLimit(
    /** "history", "current", or "" for nothing. */
    val shareLevelLimit: String,
    /** "", "paused", "recording_off", "phone_location_off" or "phone_while_using". */
    val sharingLimitReason: String,
    /** "always", "while_using" or "off"; null when the app never reported. */
    val phonePermission: String? = null,
    val isRecordingOn: Boolean? = null,
    /** RFC 3339; null when the app never reported. */
    val reportedAt: String? = null,
)

/** LocationReducedPerson is somebody the person sees less of because of what the person shares back with them. */
data class LocationReducedPerson(
    val userId: String,
    val displayName: String,
    /** Whether the person sees nothing of them, else only where they are and not their visits. */
    val isSeeingNothing: Boolean,
)

/** LocationSharingBanner is the one notice the top of the people list shows. */
sealed class LocationSharingBanner {
    data object Paused : LocationSharingBanner()
    data object RecordingOff : LocationSharingBanner()
    data object PhoneLocationOff : LocationSharingBanner()
    data object PhoneWhileUsing : LocationSharingBanner()

    /** No limit of the person's own, but they share less back than others share with them. */
    data class SharesLess(val people: List<LocationReducedPerson>) : LocationSharingBanner()
}

/**
 * LocationSharingNotice decides which banner applies. The server's rule: what the person sees of somebody is the lowest
 * of what that person's shares give them, what the person shares back, and the person's own limit.
 */
object LocationSharingNotice {
    /** The most people named in the banner before "and N more". */
    const val MOST_NAMED_PEOPLE = 3

    /** Levels in order: "" is nothing, current is where they are, history adds their visits. Any other level ("owner") is above all. */
    fun rank(shareLevel: String): Int = when (shareLevel) {
        "" -> 0
        "current" -> 1
        "history" -> 2
        else -> 3
    }

    /** The people the person sees less of because of what they share back. A person whose granted level the server did not say is left out. */
    fun reducedPeople(limit: LocationSharingLimit?, people: List<PersonLocation>): List<LocationReducedPerson> {
        val limitRank = limit?.let { rank(it.shareLevelLimit) } ?: rank("history")
        val reduced = mutableListOf<LocationReducedPerson>()
        for (person in people) {
            val grantedShareLevel = person.grantedShareLevel ?: continue
            val possibleRank = minOf(rank(grantedShareLevel), limitRank)
            if (rank(person.shareLevel) >= possibleRank) continue
            reduced.add(LocationReducedPerson(person.userId, person.displayName, person.shareLevel.isEmpty()))
        }
        return reduced
    }

    /** The banner that applies, or null for none. A limit of the person's own comes first. */
    fun banner(limit: LocationSharingLimit?, people: List<PersonLocation>): LocationSharingBanner? {
        when (limit?.sharingLimitReason) {
            "paused" -> return LocationSharingBanner.Paused
            "recording_off" -> return LocationSharingBanner.RecordingOff
            "phone_location_off" -> return LocationSharingBanner.PhoneLocationOff
            "phone_while_using" -> return LocationSharingBanner.PhoneWhileUsing
        }
        val reduced = reducedPeople(limit, people)
        if (reduced.isEmpty()) return null
        return LocationSharingBanner.SharesLess(reduced)
    }
}
