package com.teanode.kit.notifications

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive

/** AppUpdateChannel is where this copy of the app was installed from, and so where its updates come from. */
enum class AppUpdateChannel(val channelName: String) {
    /** Testers' builds, installed from Firebase's tester distribution. */
    Android("android"),
}

/** AppUpdate is the newest app on a channel, as the server reads it. */
@Serializable
data class AppUpdate(
    val updateChannel: String,
    val version: String,
    /** Empty where the channel shows people a version and nothing else. */
    val buildNumber: String = "",
    val installUrl: String,
) {
    /** The update as the person reads it: 1.2.0, or 1.2.0 (345) with a build. */
    val versionText: String get() = if (buildNumber.isEmpty()) version else "$version ($buildNumber)"

    /** What a dismissal remembers: this update and no other, so that the next one is offered again. */
    val dismissalKey: String get() = "$updateChannel $versionText"

    /** Whether this is newer than the app that is running. The version comes first; the build only tells builds of one version apart. */
    fun isNewer(runningVersion: String, runningBuild: String): Boolean {
        val order = order(version, runningVersion)
        if (order != 0) return order > 0
        val build = buildNumber.toIntOrNull() ?: return false
        val running = runningBuild.toIntOrNull() ?: return false
        return build > running
    }

    private companion object {
        /** Below zero, zero or above, as one version comes before, with or after another: 1.10.0 is after 1.9.0, and 1.2 is 1.2.0. */
        fun order(left: String, right: String): Int {
            val leftParts = left.split(".").filter { part -> part.isNotEmpty() }.map { part -> part.toIntOrNull() ?: 0 }
            val rightParts = right.split(".").filter { part -> part.isNotEmpty() }.map { part -> part.toIntOrNull() ?: 0 }
            for (index in 0 until maxOf(leftParts.size, rightParts.size)) {
                val leftPart = leftParts.getOrElse(index) { 0 }
                val rightPart = rightParts.getOrElse(index) { 0 }
                if (leftPart != rightPart) return leftPart - rightPart
            }
            return 0
        }
    }
}

object AppUpdateDocuments {
    const val APP_UPDATE = """
        query (${'$'}updateChannel: String!) {
          GetAppUpdate(updateChannel: ${'$'}updateChannel) { updateChannel version buildNumber installUrl }
        }
        """
}

@Serializable
private class AppUpdateAnswer(@SerialName("GetAppUpdate") val update: AppUpdate? = null)

/** The newest app on a channel; null when there is none to offer, or this server does not look for releases. */
suspend fun Connection.appUpdate(channel: AppUpdateChannel = AppUpdateChannel.Android): AppUpdate? =
    execute(AppUpdateDocuments.APP_UPDATE, AppUpdateAnswer.serializer(), mapOf("updateChannel" to JsonPrimitive(channel.channelName))).update
