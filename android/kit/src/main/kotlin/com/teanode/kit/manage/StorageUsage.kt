package com.teanode.kit.manage

import com.teanode.kit.Connection
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable

/** How the current size is broken down: the database by what its tables are for, the files by kind, or the files by person. */
enum class StorageBreakdown(val serverName: String) { Area("area"), Kind("kind"), Person("person") }

/** A size under a key: an area, a kind of file. */
@Serializable
data class StorageKeySize(val sizeKey: String, val sizeBytes: Long = 0)

/** One row of the breakdown asked for. */
@Serializable
data class StorageUsageRow(
    /** The area, the kind of file, or the person's id; empty for the files no account owns. */
    val rowKey: String = "",
    /** A person's name for a person row; the key otherwise. */
    val rowLabel: String = "",
    val rowSizeBytes: Long = 0,
    /** How many tables an area holds; zero for the rest. */
    val rowTableCount: Int = 0,
    /** A person's files split by kind; empty for the rest. */
    val rowFileSizes: List<StorageKeySize> = emptyList(),
)

/** What one day's sample held. */
@Serializable
data class StorageHistoryDay(
    /** The day as YYYY-MM-DD, in UTC. */
    val sampledOn: String,
    val databaseSizeBytes: Long = 0,
    val fileSizeBytes: Long = 0,
    val totalSizeBytes: Long = 0,
    /** That day's areas or kinds, as the breakdown asked for; empty for a person, which is not sampled. */
    val breakdownSizes: List<StorageKeySize> = emptyList(),
)

/** A disk the server writes to. An object store is not measured: its sizes are null, and nothing is said about when it fills. */
@Serializable
data class StorageDisk(
    /** dataDirectory, fileDirectory or objectStore. */
    val diskLabel: String,
    val isDiskMeasured: Boolean = false,
    val diskTotalBytes: Long? = null,
    val diskFreeBytes: Long? = null,
    val diskUsedBytes: Long? = null,
    /** The day it fills at the recent growth, YYYY-MM-DD in UTC; null when not growing, with too little history, or not measured. */
    val projectedFullOn: String? = null,
    val daysUntilFull: Double? = null,
    /** Whether it fills within thirty days, which is when the operators are warned. */
    val isProjectedFull: Boolean = false,
) {
    /** What share of the disk is used, from 0 to 1; null when it is not measured. */
    val usedFraction: Double?
        get() {
            val total = diskTotalBytes ?: return null
            val used = diskUsedBytes ?: return null
            if (!isDiskMeasured || total <= 0) return null
            return (used.toDouble() / total).coerceIn(0.0, 1.0)
        }
}

/** The line fitted through the last thirty days. */
@Serializable
data class StorageGrowth(
    val growthSampleCount: Int = 0,
    val isGrowthProjectable: Boolean = false,
    val growthBytesPerDay: Double = 0.0,
    val currentSizeBytes: Long = 0,
    val projectedSize30DaysBytes: Long = 0,
    val projectedSize90DaysBytes: Long = 0,
    val projectedSize365DaysBytes: Long = 0,
    /** Whether the files are in the totals: they are when on a disk the server measures, not when in an object store. */
    val areFilesCounted: Boolean = true,
)

/** One piece of a stacked column: a day, the series it belongs to, and its size. */
data class StorageChartPoint(val sampledOn: String, val seriesKey: String, val sizeBytes: Long)

/** One answer of the server: the sizes now, the days sampled, the disks and the growth. */
@Serializable
data class StorageUsage(
    val breakdownBy: String = "area",
    val databaseSizeBytes: Long = 0,
    val fileSizeBytes: Long = 0,
    val totalSizeBytes: Long = 0,
    val usageRows: List<StorageUsageRow> = emptyList(),
    val historyDays: List<StorageHistoryDay> = emptyList(),
    val disks: List<StorageDisk> = emptyList(),
    val growth: StorageGrowth = StorageGrowth(),
) {
    /** The disks that fill within thirty days, soonest first: what the banner on top of the screen is about. */
    val fillingDisks: List<StorageDisk>
        get() = disks.filter { disk -> disk.isProjectedFull && disk.projectedFullOn != null }.sortedBy { disk -> disk.daysUntilFull ?: 0.0 }

    /**
     * The days as stacked columns: the largest areas or kinds of the newest day apart and the rest together, or the database and the
     * files for a person, who is not sampled.
     */
    val chartPoints: List<StorageChartPoint>
        get() {
            val newest = historyDays.lastOrNull() ?: return emptyList()
            if (breakdownBy == StorageBreakdown.Person.serverName) {
                return historyDays.flatMap { day ->
                    listOf(StorageChartPoint(day.sampledOn, DATABASE_SERIES_KEY, day.databaseSizeBytes), StorageChartPoint(day.sampledOn, FILES_SERIES_KEY, day.fileSizeBytes))
                }
            }
            val drawn = newest.breakdownSizes.sortedByDescending { size -> size.sizeBytes }.take(CHART_SERIES_LIMIT).map { size -> size.sizeKey }
            val points = mutableListOf<StorageChartPoint>()
            for (day in historyDays) {
                for (key in drawn) points += StorageChartPoint(day.sampledOn, key, day.breakdownSizes.firstOrNull { size -> size.sizeKey == key }?.sizeBytes ?: 0)
                val restBytes = day.breakdownSizes.filter { size -> size.sizeKey !in drawn }.sumOf { size -> size.sizeBytes }
                if (day.breakdownSizes.size > drawn.size) points += StorageChartPoint(day.sampledOn, REST_SERIES_KEY, restBytes)
            }
            return points
        }

    companion object {
        /** The most series the chart draws apart: the largest few, and the rest as one. */
        const val CHART_SERIES_LIMIT = 5
        const val REST_SERIES_KEY = "rest"
        const val DATABASE_SERIES_KEY = "database"
        const val FILES_SERIES_KEY = "files"
    }
}

@Serializable internal class StorageUsageAnswer(val StorageUsage: StorageUsage)

/** What the database and the files hold now, broken down as asked, with the days sampled over the range, the disks and the growth. Needs server:manage. */
suspend fun Connection.storageUsage(breakdown: StorageBreakdown, sinceText: String, untilText: String): StorageUsage =
    execute<StorageUsageAnswer>(ManageServerDocuments.STORAGE_USAGE, variablesOf("by" to breakdown.serverName, "since" to sinceText, "until" to untilText)).StorageUsage
