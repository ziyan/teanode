package com.teanode.kit.photos

import com.teanode.kit.ServerAddress
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Instant

class PhotoBackupTest {
    private fun item(id: String, created: String? = "2026-03-01T10:00:00Z", resources: List<PhotoBackupResource> = listOf(original())) = PhotoBackupItem(
        localIdentifier = id, creationInstant = created?.let { text -> Instant.parse(text) }, resources = resources,
    )

    private fun original() = PhotoBackupResource(PhotoBackupFileRole.Original, "IMG_1.jpg", "image/jpeg", true, 1000)

    private fun edited() = PhotoBackupResource(PhotoBackupFileRole.Edited, "IMG_1.jpg", "image/jpeg", true, 900)

    @Test
    fun theNewestItemsGoFirstAndEachOriginalBeforeItsOtherFiles() {
        val uploads = PhotoBackupPlanner.plan(
            listOf(item("old", "2025-01-01T00:00:00Z"), item("new", resources = listOf(edited(), original())), item("undated", null)), emptyMap(),
        )
        assertEquals(listOf("new|original", "new|edited", "old|original", "undated|original"), uploads.map { upload -> upload.key })
    }

    @Test
    fun aFileOnTheServerIsNotSentAgainButAnEditMadeLaterIs() {
        val sent = Instant.parse("2026-03-02T00:00:00Z").toEpochMilli()
        val records = mapOf(
            "a" to PhotoBackupRecord("local:a", mutableMapOf("original" to PhotoBackupRoleRecord(isOnServer = true), "edited" to PhotoBackupRoleRecord(isOnServer = true, assetModificationEpochMS = sent))),
        )
        val unchanged = item("a", resources = listOf(original(), edited())).copy(modificationInstant = Instant.ofEpochMilli(sent))
        assertTrue(PhotoBackupPlanner.plan(listOf(unchanged), records).isEmpty())
        val editedAgain = unchanged.copy(modificationInstant = Instant.ofEpochMilli(sent + 60_000))
        assertEquals(listOf("a|edited"), PhotoBackupPlanner.plan(listOf(editedAgain), records).map { upload -> upload.key })
    }

    @Test
    fun aFileRefusedThreeTimesOrSkippedIsLeftAlone() {
        val refused = mapOf("a" to PhotoBackupRecord("local:a", mutableMapOf("original" to PhotoBackupRoleRecord(refusalCount = 3))))
        assertTrue(PhotoBackupPlanner.plan(listOf(item("a")), refused).isEmpty())
        val skipped = mapOf("a" to PhotoBackupRecord("local:a", mutableMapOf("original" to PhotoBackupRoleRecord(skippedAtEpochMS = 1))))
        assertTrue(PhotoBackupPlanner.plan(listOf(item("a")), skipped).isEmpty())
        assertEquals(1, PhotoBackupPlanner.plan(listOf(item("a")), emptyMap()).size)
        assertTrue(PhotoBackupPlanner.plan(listOf(item("a")), emptyMap(), setOf("a|original")).isEmpty())
    }

    @Test
    fun theHeadersSayWhatTheServerNeedsAndNothingUnsafe() {
        val resource = PhotoBackupResource(PhotoBackupFileRole.Original, "café/über.jpg", "image/jpeg", true)
        val headers = PhotoBackupPlanner.headers(item("a").copy(location = PhotoBackupLocation(12.3456789, -100.5), isFavorite = true), resource, "local:a")
        assertEquals("local:a", headers[PhotoBackupHeader.CLIENT_ID])
        assertEquals("caf___ber.jpg", headers[PhotoBackupHeader.FILENAME])
        assertEquals("2026-03-01T10:00:00Z", headers[PhotoBackupHeader.TAKEN_AT])
        assertEquals("12.345679", headers[PhotoBackupHeader.LATITUDE])
        assertEquals("true", headers[PhotoBackupHeader.IS_FAVORITE])
        assertEquals("android", headers[PhotoBackupHeader.SOURCE])
        val outOfRange = PhotoBackupPlanner.headers(item("a").copy(location = PhotoBackupLocation(91.0, 0.0)), resource, "local:a")
        assertNull(outOfRange[PhotoBackupHeader.LATITUDE])
    }

    @Test
    fun theClientIdIsTheCloudIdWhenThereIsOne() {
        assertEquals("local:a", PhotoBackupPlanner.clientPhotoId(item("a")))
        assertEquals("cloud-1", PhotoBackupPlanner.clientPhotoId(item("a").copy(cloudIdentifier = "cloud-1")))
    }

    @Test
    fun theServersAnswersMeanWhatTheIphoneAppTakesThemToMean() {
        assertEquals(PhotoBackupOutcome.Uploaded("p1"), PhotoBackupOutcome.interpret(201, " p1 ", null, null, false))
        assertEquals(PhotoBackupOutcome.RetryLater(PhotoBackupFailure.ServerDidNotSayWhichPhoto, false), PhotoBackupOutcome.interpret(200, null, null, null, false))
        assertEquals(PhotoBackupOutcome.SignInNeeded, PhotoBackupOutcome.interpret(401, null, null, null, false))
        assertEquals(PhotoBackupOutcome.SignInNeeded, PhotoBackupOutcome.interpret(403, null, "this password does not grant photos", null, false))
        assertEquals(PhotoBackupOutcome.UploadMovedOn, PhotoBackupOutcome.interpret(410, null, null, null, false))
        assertEquals(PhotoBackupOutcome.RetryLater(PhotoBackupFailure.ServerOutOfSpace, false, 120), PhotoBackupOutcome.interpret(507, null, null, null, false, retryAfterHeader = "120"))
        assertEquals(PhotoBackupOutcome.Refused(PhotoBackupFailure.FileRefused("too big")), PhotoBackupOutcome.interpret(413, null, "too big", null, false))
        assertEquals(PhotoBackupOutcome.Cancelled, PhotoBackupOutcome.interpret(null, null, null, null, false, isCancelled = true))
        assertEquals(PhotoBackupOutcome.RetryLater(null, true), PhotoBackupOutcome.interpret(null, null, null, null, false))
    }

    @Test
    fun aWaitTheServerAsksForIsNeverLongerThanAnHour() {
        assertEquals(3600, PhotoBackupOutcome.retryAfterSeconds("99999"))
        assertNull(PhotoBackupOutcome.retryAfterSeconds("0"))
        assertNull(PhotoBackupOutcome.retryAfterSeconds("soon"))
        assertEquals(30, PhotoBackupOutcome.retryAfterSeconds("Thu, 01 Jan 1970 00:01:30 GMT", nowEpochSeconds = 60))
    }

    @Test
    fun anUploadsStateIsReadFromItsHeaders() {
        assertEquals(PhotoBackupServerUpload.Receiving(300), PhotoBackupRequest.interpretState(204, "300", "?0", null, null, null).first)
        assertEquals(PhotoBackupServerUpload.Complete("p1"), PhotoBackupRequest.interpretState(204, "300", "?1", "p1", null, null).first)
        assertEquals(PhotoBackupServerUpload.Gone, PhotoBackupRequest.interpretState(404, null, null, null, null, null).first)
        assertEquals(PhotoBackupOutcome.RetryLater(null, false), PhotoBackupRequest.interpretState(204, "x", null, null, null, null).second)
        assertTrue(PhotoBackupRequest.isUploadPath("/api/v1/photos/uploads/abc"))
        assertFalse(PhotoBackupRequest.isUploadPath("/api/v1/photos/uploads/abc/def"))
        assertFalse(PhotoBackupRequest.isUploadPath("/elsewhere/abc"))
    }

    @Test
    fun theCreationRequestAnnouncesTheLengthAndSignsInWithTheDevicePassword() {
        val server = ServerAddress.parse("https://tea.example.com")
        val credential = PhotoBackupCredential("c1", "Phone", "device", "secret")
        val upload = PhotoBackupPlanner.plan(listOf(item("a")), emptyMap()).single()
        val request = PhotoBackupRequest.creation(server, credential, upload, 1000, "A".repeat(64))
        assertEquals("POST", request.method)
        assertEquals("1000", request.header(PhotoBackupHeader.UPLOAD_LENGTH))
        assertEquals("?0", request.header(PhotoBackupHeader.UPLOAD_COMPLETE))
        assertEquals("a".repeat(64), request.header(PhotoBackupHeader.DECLARED_SHA256))
        assertEquals("Basic ZGV2aWNlOnNlY3JldA==", request.header("Authorization"))
        assertNull(PhotoBackupRequest.creation(server, credential, upload, 1000, "xyz").header(PhotoBackupHeader.DECLARED_SHA256))
    }
}
