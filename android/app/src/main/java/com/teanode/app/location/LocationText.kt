package com.teanode.app.location

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import com.teanode.app.R
import com.teanode.app.ui.components.RelativeTime
import com.teanode.kit.location.FoundPlace
import com.teanode.kit.location.LocationNotificationMute
import com.teanode.kit.location.LocationPause
import com.teanode.kit.location.LocationPauseChoice
import com.teanode.kit.location.LocationShare
import com.teanode.kit.location.LocationShareLevel
import com.teanode.kit.location.LocationPlace
import com.teanode.kit.location.LocationWords
import com.teanode.kit.location.PersonLocation
import com.teanode.kit.location.PlaceLabel
import com.teanode.kit.location.PlaceVisit
import com.teanode.kit.location.TravelMode
import com.teanode.kit.location.TravelPeriod
import com.teanode.kit.location.TravelPeriodKind
import com.teanode.kit.location.TripLeg
import java.text.NumberFormat
import java.time.Duration
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale
import kotlin.math.roundToInt

/** DistanceUnits is how distances are written: the phone's own habit, or what the person chose in the settings. */
enum class DistanceUnits { Automatic, Metric, US }

/** The units distances are written in. The Settings part may provide the person's choice; without one the phone's region decides. */
val LocalDistanceUnits = compositionLocalOf { DistanceUnits.Automatic }

/** The words Location says that depend on the time or the language, as the iPhone app's `LocationText` writes them. */
class LocationText(
    private val context: Context,
    private val locale: Locale = Locale.getDefault(),
    private val zone: ZoneId = ZoneId.systemDefault(),
    private val units: DistanceUnits = DistanceUnits.Automatic,
    private val nowEpochMS: () -> Long = System::currentTimeMillis,
) {
    private fun string(resource: Int, vararg arguments: Any): String = context.getString(resource, *arguments)

    // Places and labels

    /** The label's word. */
    fun labelTitle(label: PlaceLabel): String = string(
        when (label) {
            PlaceLabel.Home -> R.string.location_place_label_home
            PlaceLabel.Work -> R.string.location_place_label_work
            PlaceLabel.School -> R.string.location_place_label_school
            PlaceLabel.Family -> R.string.location_place_label_family
            PlaceLabel.Gym -> R.string.location_place_label_gym
        },
    )

    /** What a list calls it: the name the person gave, else for a labeled place the label's word, else the name the map or the address gave. */
    fun shownName(place: LocationPlace): String {
        val label = place.label
        if (!place.isNamedByPerson && label != null) return labelTitle(label)
        if (place.placeName.isNotEmpty()) return place.placeName
        if (place.addressText.isNotEmpty()) return place.addressText
        return string(R.string.location_a_place)
    }

    /** For a place that shows its label's word as its name, the name the map or the address gave, to say under it. */
    fun secondaryName(place: LocationPlace): String? {
        if (place.isNamedByPerson || place.label == null) return null
        if (place.placeName.isNotEmpty()) return place.placeName
        return place.addressText.ifEmpty { null }
    }

    fun shownName(found: FoundPlace): String {
        val label = found.label
        if (!found.isNamedByPerson && label != null) return labelTitle(label)
        return found.placeName
    }

    fun secondaryName(found: FoundPlace): String? {
        if (found.isNamedByPerson || found.label == null) return null
        if (found.placeName.isNotEmpty()) return found.placeName
        return found.addressText.ifEmpty { null }
    }

    /** The words saved as a place's address when the person picks this match: its address, or its name when the provider gave no address. */
    fun chosenAddressText(found: FoundPlace): String = found.addressText.ifEmpty { found.placeName }

    /** The kind of thing a map tag names, in plain words: "cafe" for "amenity=cafe". Empty when there is none worth saying. */
    fun category(osmCategory: String): String {
        val key = LocationWords.categoryValue(osmCategory)
        val resource = when (key) {
            "" -> return ""
            "cafe" -> R.string.location_category_cafe
            "restaurant" -> R.string.location_category_restaurant
            "fast_food" -> R.string.location_category_fast_food
            "pub", "bar" -> R.string.location_category_bar
            "school", "kindergarten" -> R.string.location_category_school
            "university", "college" -> R.string.location_category_university
            "library" -> R.string.location_category_library
            "park" -> R.string.location_category_park
            "playground" -> R.string.location_category_playground
            "pitch", "sports_centre", "stadium" -> R.string.location_category_sports
            "fitness_centre" -> R.string.location_category_gym
            "supermarket", "convenience", "greengrocer" -> R.string.location_category_grocery
            "bakery" -> R.string.location_category_bakery
            "clinic", "doctors", "dentist" -> R.string.location_category_clinic
            "hospital" -> R.string.location_category_hospital
            "pharmacy" -> R.string.location_category_pharmacy
            "place_of_worship" -> R.string.location_category_worship
            "cinema" -> R.string.location_category_cinema
            "museum" -> R.string.location_category_museum
            "bank" -> R.string.location_category_bank
            "post_office" -> R.string.location_category_post_office
            "fuel" -> R.string.location_category_fuel
            "parking" -> R.string.location_category_parking
            "station", "bus_station" -> R.string.location_category_station
            "hotel", "guest_house" -> R.string.location_category_hotel
            else -> {
                // A kind this app has no word for: the map's own, spelled out.
                val spelled = key.replace('_', ' ')
                return spelled.replaceFirstChar { first -> first.uppercase() }
            }
        }
        return string(resource)
    }

    // Share levels and pause choices

    fun levelTitle(level: LocationShareLevel): String = string(
        when (level) {
            LocationShareLevel.Current -> R.string.location_share_level_current
            LocationShareLevel.History -> R.string.location_share_level_history
            LocationShareLevel.Owner -> R.string.location_you
        },
    )

    /** What somebody a location is shared at this level can see. */
    fun levelExplanation(level: LocationShareLevel): String = when (level) {
        LocationShareLevel.Current -> string(R.string.location_share_explanation_current)
        LocationShareLevel.History -> string(R.string.location_share_explanation_history)
        LocationShareLevel.Owner -> ""
    }

    fun pauseChoiceTitle(choice: LocationPauseChoice): String = string(
        when (choice) {
            LocationPauseChoice.AnHour -> R.string.location_for_an_hour
            LocationPauseChoice.UntilTomorrowMorning -> R.string.location_until_tomorrow_morning
            LocationPauseChoice.UntilResumed -> R.string.location_until_i_resume
        },
    )

    /** The same three choices for muting; the last ends when the person unmutes. */
    fun muteChoiceTitle(choice: LocationPauseChoice): String = string(
        when (choice) {
            LocationPauseChoice.AnHour -> R.string.location_for_an_hour
            LocationPauseChoice.UntilTomorrowMorning -> R.string.location_until_tomorrow_morning
            LocationPauseChoice.UntilResumed -> R.string.location_until_i_unmute
        },
    )

    // Times

    /** How long a visit was, such as "1 hr, 25 min". */
    fun duration(duration: Duration): String {
        if (duration.seconds < 60) return string(R.string.location_under_a_minute)
        val days = duration.toDays()
        val hours = duration.toHours() % 24
        val minutes = duration.toMinutes() % 60
        val parts = mutableListOf<String>()
        if (days > 0) parts += string(R.string.location_duration_days, days)
        if (hours > 0 && parts.size < 2) parts += string(R.string.location_duration_hours, hours)
        if (minutes > 0 && parts.size < 2) parts += string(R.string.location_duration_minutes, minutes)
        return parts.joinToString(", ")
    }

    fun time(instant: Instant): String = DateTimeFormatter.ofLocalizedTime(FormatStyle.SHORT).withLocale(locale).format(instant.atZone(zone))

    private fun dateTime(instant: Instant): String =
        DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT).withLocale(locale).format(instant.atZone(zone))

    private fun today(): LocalDate = Instant.ofEpochMilli(nowEpochMS()).atZone(zone).toLocalDate()

    /** A day as a heading: "Today", "Yesterday", else "Thursday, October 15". */
    fun dayTitle(day: LocalDate): String {
        val today = today()
        if (day == today) return string(R.string.location_today)
        if (day == today.minusDays(1)) return string(R.string.location_yesterday)
        return DateTimeFormatter.ofPattern(android.text.format.DateFormat.getBestDateTimePattern(locale, "EEEEMMMMd"), locale).format(day)
    }

    /** When a visit began and ended, such as "14:05 to 15:30"; "Since 14:05" while the person is still there. */
    fun times(visit: PlaceVisit): String {
        val arrived = visit.arrivedInstant
        val departed = visit.departedInstant
        return when {
            arrived != null && departed != null -> string(R.string.location_time_to_time, time(arrived), time(departed))
            arrived != null -> string(R.string.location_since_time, time(arrived))
            departed != null -> string(R.string.location_until_time, time(departed))
            else -> ""
        }
    }

    /** When a pause ends, as the clock shows it: just the time when that is today, else with the day. */
    fun pauseEnd(end: Instant): String =
        if (end.atZone(zone).toLocalDate() == today()) time(end) else dateTime(end)

    /** The person's own pause: "Paused until 15:00", or until they resume. */
    fun pausedUntil(pause: LocationPause): String {
        val end = pause.endedInstant ?: return string(R.string.location_paused_until_you_resume)
        return string(R.string.location_paused_until, pauseEnd(end))
    }

    /** A person's notices the caller muted: "Notifications muted until 15:00", or without an end while the caller has to unmute. */
    fun mutedUntil(mute: LocationNotificationMute): String {
        val end = mute.mutedUntilInstant ?: return string(R.string.location_notifications_muted)
        return string(R.string.location_notifications_muted_until, pauseEnd(end))
    }

    /** What somebody who paused reads as to the people who share with them. */
    fun locationPaused(pause: LocationPause): String {
        val end = pause.endedInstant ?: return string(R.string.location_location_paused)
        return string(R.string.location_location_paused_until, pauseEnd(end))
    }

    /** When a pause began and ended, such as "14:00 to 15:00"; "Since 14:00" while it has no end. */
    fun times(pause: LocationPause): String {
        val started = pause.startedInstant ?: return ""
        val ended = pause.endedInstant ?: return string(R.string.location_since_time, time(started))
        if (ended.atZone(zone).toLocalDate() == started.atZone(zone).toLocalDate()) return string(R.string.location_time_to_time, time(started), time(ended))
        return string(R.string.location_time_to_time, time(started), dateTime(ended))
    }

    /** The times of a visit and, when it is over, how long it was. */
    fun summary(visit: PlaceVisit): String {
        val timesText = times(visit)
        val length = visit.visitDuration ?: return timesText
        return string(R.string.location_times_and_duration, timesText, duration(length))
    }

    /**
     * Where somebody sharing is, under their name, as Find My says it: the place they are at and how long ago their latest
     * position came ("Home · 1 min. ago"), either one alone when that is all there is, else that there is no position yet.
     */
    fun whereText(person: PersonLocation, isOwn: Boolean = false): String {
        // Listed, but the server sends nothing of them: the person shares nothing with them, or has no way to see anybody's.
        if (!isOwn && person.isHiddenFromCaller) return string(R.string.location_share_your_location_to_see_theirs)
        val now = Instant.ofEpochMilli(nowEpochMS())
        val pause = person.pause
        if (!isOwn && pause != null && pause.isInEffect(now)) return locationPaused(pause)
        val placeName = person.openVisit?.place?.let { place -> shownName(place) }
        val measured = person.latestPoint?.measuredInstant
        val agoText = measured?.let { instant ->
            // A reading at or a moment ahead of the phone's clock reads as "1 sec. ago".
            val capped = if (instant.isAfter(now.minusSeconds(1))) now.minusSeconds(1) else instant
            RelativeTime.text(context, capped.toEpochMilli(), now.toEpochMilli())
        }
        return when {
            placeName != null && agoText != null -> string(R.string.location_place_and_ago, placeName, agoText)
            placeName != null -> placeName
            agoText != null -> agoText
            isOwn -> string(R.string.location_no_position_recorded_yet)
            else -> string(R.string.location_no_position_yet)
        }
    }

    /** How far somebody sharing is from the person: "Nearby" within 200 m, else the distance; null without both positions. */
    fun distanceText(own: PersonLocation?, person: PersonLocation): String? {
        val ownPoint = own?.latestPoint ?: return null
        val point = person.latestPoint ?: return null
        val meters = com.teanode.kit.location.LocationGeometry.distanceMeters(
            ownPoint.latitudeDegrees, ownPoint.longitudeDegrees, point.latitudeDegrees, point.longitudeDegrees,
        )
        if (meters < 200) return string(R.string.location_nearby)
        return distance(meters)
    }

    private fun isUnitedStates(): Boolean = units == DistanceUnits.US ||
        (units == DistanceUnits.Automatic && locale.country in setOf("US", "LR", "MM"))

    /**
     * "11.8 km" or "7.3 mi", in the units the person chose. Metric says meters under a kilometer and US says feet under a
     * tenth of a mile, as a road sign would.
     */
    fun distance(meters: Double): String {
        val whole = NumberFormat.getIntegerInstance(locale)
        val oneDecimal = NumberFormat.getNumberInstance(locale).apply { maximumFractionDigits = 1; minimumFractionDigits = 0 }
        if (isUnitedStates()) {
            if (meters < 160.9344) return string(R.string.location_distance_feet, whole.format((meters * 3.28084).roundToInt()))
            return string(R.string.location_distance_miles, oneDecimal.format(meters / 1609.344))
        }
        if (meters < 1000) return string(R.string.location_distance_meters, whole.format(meters.roundToInt()))
        return string(R.string.location_distance_kilometers, oneDecimal.format(meters / 1000))
    }

    fun radius(meters: Double): String = distance(meters)

    /** "3 visits, last yesterday" for a place's row. */
    fun visits(place: LocationPlace): String {
        val countText = context.resources.getQuantityString(R.plurals.location_visit_count, place.visitCount, place.visitCount)
        val last = place.lastVisitInstant ?: return countText
        return string(R.string.location_visits_last, countText, RelativeTime.text(context, last.toEpochMilli(), nowEpochMS()))
    }

    // Travel

    fun travelModeTitle(travelMode: String): String {
        val mode = TravelMode.fromServerWord(travelMode) ?: return travelMode.replaceFirstChar { first -> first.uppercase() }
        return string(
            when (mode) {
                TravelMode.Walk -> R.string.location_travel_mode_walk
                TravelMode.Run -> R.string.location_travel_mode_run
                TravelMode.Bike -> R.string.location_travel_mode_bike
                TravelMode.Car -> R.string.location_travel_mode_car
                TravelMode.Bus -> R.string.location_travel_mode_bus
                TravelMode.Train -> R.string.location_travel_mode_train
                TravelMode.Boat -> R.string.location_travel_mode_boat
                TravelMode.Plane -> R.string.location_travel_mode_plane
            },
        )
    }

    fun summary(leg: TripLeg): String = string(R.string.location_distance_and_duration, distance(leg.distanceMeters), duration(Duration.ofSeconds(leg.durationSeconds.toLong())))

    fun title(period: TravelPeriod, kind: TravelPeriodKind): String {
        val start = period.startDay() ?: return period.periodStartDay
        return when (kind) {
            TravelPeriodKind.Day -> dayTitle(start)
            TravelPeriodKind.Week -> string(R.string.location_week_of, DateTimeFormatter.ofPattern(android.text.format.DateFormat.getBestDateTimePattern(locale, "MMMd"), locale).format(start))
            TravelPeriodKind.Month -> DateTimeFormatter.ofPattern(android.text.format.DateFormat.getBestDateTimePattern(locale, "MMMMy"), locale).format(start)
            TravelPeriodKind.Year -> start.year.toString()
        }
    }

    fun title(kind: TravelPeriodKind): String = string(
        when (kind) {
            TravelPeriodKind.Day -> R.string.location_travel_period_day
            TravelPeriodKind.Week -> R.string.location_travel_period_week
            TravelPeriodKind.Month -> R.string.location_travel_period_month
            TravelPeriodKind.Year -> R.string.location_travel_period_year
        },
    )

    // Sharing

    /** The words for a thing shared with: a person's name, or "Everyone in" a circle. */
    fun shareName(share: LocationShare): String =
        if (share.isWithCircle) string(R.string.location_everyone_in, share.granteeDisplayName) else share.granteeDisplayName

    fun phoneCouldNotTellText(): String = string(R.string.location_the_phone_could_not_tell_where)
    fun categoryAndDistance(category: String, distance: String): String = string(R.string.location_category_and_distance, category, distance)
    fun within(distanceText: String): String = string(R.string.location_within, distanceText)
    fun withinButRough(withinText: String): String = string(R.string.location_within_but_rough, withinText)
    fun everyoneIn(name: String): String = string(R.string.location_everyone_in, name)

    /** A reading worse than this many meters is rough: the check-in sheet says so and "Use where I am now" refuses it. */
    companion object {
        const val ROUGH_ACCURACY_METERS = 100.0
    }
}

/** The location words for this screen, in the phone's language and the person's units. */
@Composable
fun rememberLocationText(): LocationText {
    val context = LocalContext.current
    val units = LocalDistanceUnits.current
    return remember(context, units) { LocationText(context, units = units) }
}

/** The sentences the model says in the snackbar, in the person's language. */
class LocationModelWords(
    private val context: Context,
    private val text: LocationText,
    private val describeProblem: (Throwable) -> String,
) {
    private fun string(resource: Int, vararg arguments: Any): String = context.getString(resource, *arguments)

    val tryAgain: String get() = string(R.string.location_that_did_not_work)
    val forgotVisit: String get() = string(R.string.location_forgot_the_visit)
    val phoneCouldNotTell: String get() = string(R.string.location_phone_could_not_tell)
    val checkedInHere: String get() = string(R.string.location_checked_in_here)
    val positionsErased: String get() = string(R.string.location_positions_erased)
    val notificationsMuted: String get() = string(R.string.location_notifications_muted)
    val notificationsUnmuted: String get() = string(R.string.location_notifications_unmuted)
    val locationIsSetUp: String get() = string(R.string.location_is_set_up)
    val placeIsNowHome: String get() = string(R.string.location_place_is_now_home)

    val keptAddress: String get() = string(R.string.location_kept_the_address_as_typed)
    val unknownDistance: String get() = string(R.string.location_an_unknown_distance)
    val mapWillNameAgain: String get() = string(R.string.location_map_will_name_again)
    fun moved(name: String): String = string(R.string.location_moved, name)
    fun phoneRough(roughness: String): String = string(R.string.location_phone_can_only_place_you, roughness)

    fun describeLoadProblem(error: Throwable): String = describeProblem(error)
    fun forgot(name: String): String = string(R.string.location_forgot, name)
    fun checkedInAt(name: String): String = string(R.string.location_checked_in_at, name)
    fun erasedDay(dayText: String): String = string(R.string.location_erased_the_positions_of, dayText)
    fun sharedWith(name: String): String = string(R.string.location_shared_with, name)
    fun changedWhatCanSee(name: String): String = string(R.string.location_changed_what_can_see, name)
    fun stoppedSharingWith(name: String): String = string(R.string.location_stopped_sharing_with, name)
    fun phoneCouldNotTellText(): String = string(R.string.location_the_phone_could_not_tell_where)
    fun categoryAndDistance(category: String, distance: String): String = string(R.string.location_category_and_distance, category, distance)
    fun within(distanceText: String): String = string(R.string.location_within, distanceText)
    fun withinButRough(withinText: String): String = string(R.string.location_within_but_rough, withinText)
    fun everyoneIn(name: String): String = string(R.string.location_everyone_in, name)
    fun mutedUntil(mute: com.teanode.kit.location.LocationNotificationMute): String = text.mutedUntil(mute)
    fun shownName(place: LocationPlace): String = text.shownName(place)
    fun shareName(share: LocationShare): String = text.shareName(share)
    fun chosenAddressText(found: FoundPlace): String = text.chosenAddressText(found)
}
