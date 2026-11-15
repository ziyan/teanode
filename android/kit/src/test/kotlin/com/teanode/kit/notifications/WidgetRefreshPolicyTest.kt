package com.teanode.kit.notifications

import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class WidgetRefreshPolicyTest {
    @Test
    fun theFirstChangeIsDrawnAndABurstAfterItIsNot() {
        val policy = WidgetRefreshPolicy(minimumIntervalMS = 10_000L)
        assertTrue(policy.shouldRefresh(100_000L))
        assertFalse(policy.shouldRefresh(103_000L))
        assertFalse(policy.shouldRefresh(109_999L))
        assertTrue(policy.shouldRefresh(110_000L))
    }

    @Test
    fun aChangeOfAccountIsNeverHeldBack() {
        val policy = WidgetRefreshPolicy(minimumIntervalMS = 10_000L)
        assertTrue(policy.shouldRefresh(100_000L))
        assertTrue(policy.shouldRefresh(100_001L, isForced = true))
    }

    @Test
    fun aClockThatWentBackDoesNotStopTheWidgetsForever() {
        val policy = WidgetRefreshPolicy(minimumIntervalMS = 10_000L)
        assertTrue(policy.shouldRefresh(500_000L))
        assertTrue(policy.shouldRefresh(100_000L))
    }
}
