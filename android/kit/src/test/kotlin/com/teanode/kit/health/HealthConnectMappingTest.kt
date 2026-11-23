package com.teanode.kit.health

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class HealthConnectMappingTest {
    @Test
    fun theSdkStatusSaysWhetherToInstallOrUpdate() {
        assertEquals(HealthConnectAvailability.Available, HealthConnectAvailability.fromSdkStatus(3))
        assertEquals(HealthConnectAvailability.Missing, HealthConnectAvailability.fromSdkStatus(1))
        assertEquals(HealthConnectAvailability.NeedsUpdate, HealthConnectAvailability.fromSdkStatus(2))
        assertEquals(HealthConnectAvailability.Missing, HealthConnectAvailability.fromSdkStatus(0))
        assertFalse(HealthConnectAvailability.Available.canInstallOrUpdate)
        assertTrue(HealthConnectAvailability.Missing.canInstallOrUpdate)
        assertTrue(HealthConnectAvailability.NeedsUpdate.canInstallOrUpdate)
    }

    @Test
    fun thePlayLinksNameHealthConnect() {
        assertTrue(HealthConnectAvailability.PLAY_STORE_URI.startsWith("market://details?id=com.google.android.apps.healthdata"))
        assertTrue(HealthConnectAvailability.PLAY_STORE_WEB_URL.endsWith("id=com.google.android.apps.healthdata"))
    }

    @Test
    fun aDistanceTakesItsSportAndOtherwiseIsWalkingAndRunning() {
        assertEquals("distanceCycling", HealthSportTypes.distanceType(HealthSport.Cycling))
        assertEquals("distanceSwimming", HealthSportTypes.distanceType(HealthSport.Swimming))
        assertEquals("distanceWalkingRunning", HealthSportTypes.distanceType(HealthSport.Running))
        assertEquals("distanceWalkingRunning", HealthSportTypes.distanceType(HealthSport.Walking))
        assertEquals("distanceWalkingRunning", HealthSportTypes.distanceType(HealthSport.Other))
        assertEquals("distanceWalkingRunning", HealthSportTypes.distanceType(null))
    }

    @Test
    fun aSpeedOrPowerWithoutASportIsLeftOut() {
        assertNull(HealthSportTypes.speedType(null))
        assertNull(HealthSportTypes.speedType(HealthSport.Other))
        assertNull(HealthSportTypes.speedType(HealthSport.Swimming))
        assertEquals("runningSpeed", HealthSportTypes.speedType(HealthSport.Running))
        assertEquals("cyclingSpeed", HealthSportTypes.speedType(HealthSport.Cycling))
        assertNull(HealthSportTypes.powerType(HealthSport.Rowing))
        assertEquals("cyclingPower", HealthSportTypes.powerType(HealthSport.Cycling))
        assertEquals("runningPower", HealthSportTypes.powerType(HealthSport.Running))
    }

    @Test
    fun everyTypeAKindCanGiveIsInTheTableWithTheUnitTheReadingCarries() {
        val expectedUnits = mapOf(
            "m" to HealthSportTypes.distanceTypes, "m/s" to HealthSportTypes.speedTypes, "W" to HealthSportTypes.powerTypes,
            "count/min" to listOf(HealthSportTypes.CYCLING_CADENCE_TYPE), "kcal" to listOf("basalEnergyBurned"),
        )
        for ((unit, healthTypes) in expectedUnits) for (healthType in healthTypes) {
            val info = HealthTypes.info(healthType)
            assertNotNull(info, "$healthType is not in the table")
            assertEquals(unit, info!!.quantityUnit, healthType)
        }
    }

    @Test
    fun restingEnergyIsWhatIsLeftAfterTheActiveEnergy() {
        assertEquals(60.0, HealthRestingEnergy.restingKilocalories(100.0, 40.0)!!, 0.0001)
        assertNull(HealthRestingEnergy.restingKilocalories(40.0, 40.0))
        assertNull(HealthRestingEnergy.restingKilocalories(30.0, 40.0))
    }
}
