package com.teanode.kit.manage

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

class StorageUsageTest {
    private fun days(vararg sizes: Pair<String, Long>, count: Int = 2): String =
        (1..count).joinToString(",") { day ->
            """{"sampledOn":"2030-05-0$day","databaseSizeBytes":5900,"fileSizeBytes":4000,"totalSizeBytes":9900,"breakdownSizes":[${sizes.joinToString(",") { size -> """{"sizeKey":"${size.first}","sizeBytes":${size.second + day}}""" }}]}"""
        }

    private fun answer(breakdownBy: String = "area"): StorageUsage = TeaNodeJson.decodeFromString(
        StorageUsageAnswer.serializer(),
        """{"StorageUsage":{"breakdownBy":"$breakdownBy","databaseSizeBytes":6000,"fileSizeBytes":4000,"totalSizeBytes":10000,
          "usageRows":[{"rowKey":"search","rowLabel":"search","rowSizeBytes":3000,"rowTableCount":7,"rowFileSizes":[]}],
          "historyDays":[${days("a" to 70, "b" to 60, "c" to 50, "d" to 40, "e" to 30, "f" to 20, "g" to 10)}],
          "disks":[
            {"diskLabel":"dataDirectory","isDiskMeasured":true,"diskTotalBytes":20000,"diskFreeBytes":2000,"diskUsedBytes":18000,"projectedFullOn":"2030-05-21","daysUntilFull":20,"isProjectedFull":true},
            {"diskLabel":"fileDirectory","isDiskMeasured":true,"diskTotalBytes":20000,"diskFreeBytes":2000,"diskUsedBytes":18000,"projectedFullOn":"2030-05-11","daysUntilFull":10,"isProjectedFull":true},
            {"diskLabel":"objectStore","isDiskMeasured":false,"diskTotalBytes":null,"diskFreeBytes":null,"diskUsedBytes":null,"projectedFullOn":null,"daysUntilFull":null,"isProjectedFull":false}],
          "growth":{"growthSampleCount":12,"isGrowthProjectable":true,"growthBytesPerDay":100.5,"currentSizeBytes":10000,"projectedSize30DaysBytes":13000,"projectedSize90DaysBytes":19000,"projectedSize365DaysBytes":46000,"areFilesCounted":true}}}""",
    ).StorageUsage

    @Test
    fun readsTheSizesTheDisksAndTheGrowthInOneAnswer() {
        val usage = answer()
        assertEquals(10000, usage.totalSizeBytes)
        assertEquals(7, usage.usageRows[0].rowTableCount)
        assertEquals(100.5, usage.growth.growthBytesPerDay)
        assertEquals(46000, usage.growth.projectedSize365DaysBytes)
    }

    @Test
    fun theDisksThatFillSoonAreListedSoonestFirstAndAnObjectStoreIsNotMeasured() {
        val usage = answer()
        assertEquals(listOf("fileDirectory", "dataDirectory"), usage.fillingDisks.map { disk -> disk.diskLabel })
        val objectStore = usage.disks.first { disk -> disk.diskLabel == "objectStore" }
        assertFalse(objectStore.isDiskMeasured)
        assertNull(objectStore.usedFraction)
        assertEquals(0.9, usage.disks[0].usedFraction!!, 0.0001)
    }

    @Test
    fun theLargestFiveAreDrawnApartAndTheRestTogether() {
        val points = answer().chartPoints
        assertEquals(setOf("a", "b", "c", "d", "e", StorageUsage.REST_SERIES_KEY), points.map { point -> point.seriesKey }.toSet())
        assertEquals(listOf(32L, 34L), points.filter { point -> point.seriesKey == StorageUsage.REST_SERIES_KEY }.map { point -> point.sizeBytes }, "f and g, each day")
        assertEquals(12, points.size)
    }

    @Test
    fun aPersonViewDrawsTheDatabaseAndTheFilesBecausePeopleAreNotSampled() {
        val points = answer("person").chartPoints
        assertEquals(setOf(StorageUsage.DATABASE_SERIES_KEY, StorageUsage.FILES_SERIES_KEY), points.map { point -> point.seriesKey }.toSet())
        assertEquals(listOf(4000L, 4000L), points.filter { point -> point.seriesKey == StorageUsage.FILES_SERIES_KEY }.map { point -> point.sizeBytes })
    }
}
