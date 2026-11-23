package com.teanode.app.calls

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class OpeningRequestsTest {
    @Before fun reset() {
        OpeningRequests.declineOffer()
        OpeningRequests.consume()
    }

    @Test fun `an intent only offers a call`() {
        OpeningRequests.offerCall()
        assertTrue(OpeningRequests.isCallOffered.value)
        assertFalse(OpeningRequests.isCallRequested.value)
    }

    @Test fun `accepting the offer requests the call`() {
        OpeningRequests.offerCall()
        OpeningRequests.acceptOffer()
        assertTrue(OpeningRequests.isCallRequested.value)
        assertFalse(OpeningRequests.isCallOffered.value)
    }

    @Test fun `declining the offer requests nothing`() {
        OpeningRequests.offerCall()
        OpeningRequests.declineOffer()
        assertFalse(OpeningRequests.isCallRequested.value)
        assertFalse(OpeningRequests.isCallOffered.value)
    }

    @Test fun `accepting with no offer requests nothing`() {
        OpeningRequests.acceptOffer()
        assertFalse(OpeningRequests.isCallRequested.value)
    }
}
