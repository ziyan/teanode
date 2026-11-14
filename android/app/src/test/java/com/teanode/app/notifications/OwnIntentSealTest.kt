package com.teanode.app.notifications

import android.content.Context
import android.content.Intent
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class OwnIntentSealTest {
    private val context: Context = ApplicationProvider.getApplicationContext()

    @Test fun `an intent the app marked is its own`() {
        assertTrue(OwnIntentSeal.isOwn(context, OwnIntentSeal.mark(context, Intent())))
    }

    @Test fun `an intent with the notification extras but no seal is not`() {
        val forged = Intent().putExtra(NotificationBuilder.EXTRA_INFO, "{}").putExtra(NotificationBuilder.EXTRA_ACTION, "open")
        assertFalse(OwnIntentSeal.isOwn(context, forged))
    }

    @Test fun `a guessed seal is not accepted`() {
        assertFalse(OwnIntentSeal.isOwn(context, Intent().putExtra(OwnIntentSeal.EXTRA_SEAL, "guess")))
    }

    @Test fun `the seal stays the same between calls`() {
        val first = OwnIntentSeal.mark(context, Intent()).getStringExtra(OwnIntentSeal.EXTRA_SEAL)
        val second = OwnIntentSeal.mark(context, Intent()).getStringExtra(OwnIntentSeal.EXTRA_SEAL)
        assertEquals(first, second)
    }
}
