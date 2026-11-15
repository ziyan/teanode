package com.teanode.kit.health

/** Whether Health Connect can be read on this phone, and what to do when it cannot (the SDK status Health Connect answers with). */
enum class HealthConnectAvailability {
    Available,

    /** Not installed, or not offered on this device: Google Play can install it where it can run. */
    Missing,

    /** Installed, but too old for this app to read. */
    NeedsUpdate;

    /** Whether the person can fix it from Google Play. */
    val canInstallOrUpdate: Boolean get() = this != Available

    companion object {
        // The values of HealthConnectClient.SDK_UNAVAILABLE, SDK_UNAVAILABLE_PROVIDER_UPDATE_REQUIRED and SDK_AVAILABLE.
        const val SDK_UNAVAILABLE = 1
        const val SDK_UNAVAILABLE_PROVIDER_UPDATE_REQUIRED = 2
        const val SDK_AVAILABLE = 3

        fun fromSdkStatus(sdkStatus: Int): HealthConnectAvailability = when (sdkStatus) {
            SDK_AVAILABLE -> Available
            SDK_UNAVAILABLE_PROVIDER_UPDATE_REQUIRED -> NeedsUpdate
            else -> Missing
        }

        const val HEALTH_CONNECT_PACKAGE = "com.google.android.apps.healthdata"

        /** The Google Play page that installs or updates Health Connect, which sends the person back to the app after (Google's documented link). */
        const val PLAY_STORE_URI = "market://details?id=$HEALTH_CONNECT_PACKAGE&url=healthconnect%3A%2F%2Fonboarding"

        /** The same page on the web, for a phone with no Google Play app to open the first. */
        const val PLAY_STORE_WEB_URL = "https://play.google.com/store/apps/details?id=$HEALTH_CONNECT_PACKAGE"
    }
}

/** The kinds of exercise that decide which HealthKit type a Health Connect distance, speed, power or cadence record is kept under. */
enum class HealthSport { Running, Walking, Cycling, Swimming, Rowing, DownhillSnowSports, PaddleSports, SkatingSports, Other }

/**
 * Health Connect has one record for distance, speed, power and cadence whatever the sport; HealthKit has a type for each sport.
 * A record inside an exercise session takes the session's sport. Without one, only a distance is kept (as walking and running,
 * what a phone counts by itself); a speed or power with no sport says nothing about which type it is, so it is left out.
 */
object HealthSportTypes {
    fun distanceType(sport: HealthSport?): String = when (sport) {
        HealthSport.Cycling -> "distanceCycling"
        HealthSport.Swimming -> "distanceSwimming"
        HealthSport.Rowing -> "distanceRowing"
        HealthSport.DownhillSnowSports -> "distanceDownhillSnowSports"
        HealthSport.PaddleSports -> "distancePaddleSports"
        HealthSport.SkatingSports -> "distanceSkatingSports"
        else -> "distanceWalkingRunning"
    }

    fun speedType(sport: HealthSport?): String? = when (sport) {
        HealthSport.Running -> "runningSpeed"
        HealthSport.Cycling -> "cyclingSpeed"
        HealthSport.Rowing -> "rowingSpeed"
        HealthSport.PaddleSports -> "paddleSportsSpeed"
        else -> null
    }

    fun powerType(sport: HealthSport?): String? = when (sport) {
        HealthSport.Running -> "runningPower"
        HealthSport.Cycling -> "cyclingPower"
        else -> null
    }

    /** Pedaling cadence (revolutions a minute) is cycling by its name; there is no session to look up. */
    const val CYCLING_CADENCE_TYPE = "cyclingCadence"

    val distanceTypes: List<String> = HealthSport.entries.map { sport -> distanceType(sport) }.distinct()
    val speedTypes: List<String> = HealthSport.entries.mapNotNull { sport -> speedType(sport) }
    val powerTypes: List<String> = HealthSport.entries.mapNotNull { sport -> powerType(sport) }
}

/** Resting energy, which Health Connect does not keep as energy over time: it is the total burned over an interval with the active part taken out. */
object HealthRestingEnergy {
    /** The resting kilocalories of an interval: its total burned energy less its active energy; null when nothing is left (the total did not include a resting part, or the sources disagree). */
    fun restingKilocalories(totalKilocalories: Double, activeKilocalories: Double): Double? {
        val resting = totalKilocalories - activeKilocalories
        return if (resting > 0.0) resting else null
    }
}
