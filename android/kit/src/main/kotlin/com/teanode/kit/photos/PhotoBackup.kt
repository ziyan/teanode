package com.teanode.kit.photos

import com.teanode.kit.Connection
import com.teanode.kit.HTTPRequest
import com.teanode.kit.ServerAddress
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant
import java.util.Base64

/** Why a file did not go up, without any words: the app says each in the person's language. */
sealed class PhotoBackupFailure {
    data object SignInRefused : PhotoBackupFailure()

    data object ServerDidNotSayWhichPhoto : PhotoBackupFailure()

    data class FileRefused(val serverReason: String?) : PhotoBackupFailure()

    data class ServerReason(val reason: String) : PhotoBackupFailure()

    data object ServerOutOfSpace : PhotoBackupFailure()

    data object FileIsEmpty : PhotoBackupFailure()

    data object FileIsNotInLibrary : PhotoBackupFailure()

    data object FileTookTooLongToRead : PhotoBackupFailure()

    data class SystemDescription(val description: String) : PhotoBackupFailure()
}

/** The files a photo is made of. The original goes first, then a Live Photo's video, then an edited version. */
enum class PhotoBackupFileRole(val serverName: String, val uploadOrder: Int) {
    Original("original", 0),
    LiveVideo("liveVideo", 1),
    Edited("edited", 2),
}

enum class PhotoBackupMediaKind(val serverName: String) {
    Picture("picture"),
    Video("video"),
    LivePhoto("livePhoto"),
}

/** One file of a library item the phone could send. */
data class PhotoBackupResource(
    val role: PhotoBackupFileRole,
    val originalFilename: String,
    val contentType: String,
    val isLocallyAvailable: Boolean,
    val fileSizeBytes: Long? = null,
)

data class PhotoBackupLocation(val latitude: Double, val longitude: Double)

/** One picture or video of the phone's library, as the backup reads it. */
data class PhotoBackupItem(
    val localIdentifier: String,
    val cloudIdentifier: String? = null,
    val creationInstant: Instant? = null,
    val modificationInstant: Instant? = null,
    val location: PhotoBackupLocation? = null,
    val isFavorite: Boolean = false,
    val isScreenshot: Boolean = false,
    val mediaKind: PhotoBackupMediaKind = PhotoBackupMediaKind.Picture,
    val resources: List<PhotoBackupResource>,
    /** Whether the item is in a folder the person keeps out of sight (a folder named Hidden, or one starting with a dot). */
    val isInHiddenFolder: Boolean = false,
)

/** What the server answers when asked which of these client ids it already has. */
@Serializable
data class PhotoBackupServerPhoto(
    val id: String,
    val clientPhotoId: String,
    val photoKind: String = "picture",
    val photoFileRoles: List<String> = listOf("original"),
)

/** The headers the upload endpoint is spoken to with. */
object PhotoBackupHeader {
    const val CLIENT_ID = "X-Photo-Client-ID"
    const val FILE_ROLE = "X-Photo-File-Role"
    const val KIND = "X-Photo-Kind"
    const val FILENAME = "X-Photo-Filename"
    const val TAKEN_AT = "X-Photo-Taken-At"
    const val LATITUDE = "X-Photo-Latitude"
    const val LONGITUDE = "X-Photo-Longitude"
    const val IS_FAVORITE = "X-Photo-Is-Favorite"
    const val IS_SCREENSHOT = "X-Photo-Is-Screenshot"
    const val SOURCE = "X-Photo-Source"
    const val DECLARED_SHA256 = "X-Photo-Declared-SHA256"
    const val CONTENT_TYPE = "Content-Type"
    const val UPLOAD_OFFSET = "Upload-Offset"
    const val UPLOAD_COMPLETE = "Upload-Complete"
    const val UPLOAD_LENGTH = "Upload-Length"
    const val PHOTO_ID = "X-Photo-ID"
}

/** One file to send, with everything its creation request says. */
data class PhotoBackupUpload(
    val localIdentifier: String,
    val role: PhotoBackupFileRole,
    val clientPhotoId: String,
    val filename: String,
    val contentType: String,
    val isLocallyAvailable: Boolean,
    val fileSizeBytes: Long?,
    val assetModificationInstant: Instant?,
    val headers: Map<String, String>,
) {
    val key: String get() = key(localIdentifier, role)

    companion object {
        fun key(localIdentifier: String, role: PhotoBackupFileRole): String = "$localIdentifier|${role.serverName}"
    }
}

/** What the phone remembers of one file of an item: on the server, refused, skipped by the person. */
@Serializable
data class PhotoBackupRoleRecord(
    var isOnServer: Boolean = false,
    var serverPhotoId: String? = null,
    var assetModificationEpochMS: Long? = null,
    var refusalCount: Int = 0,
    var refusalReason: String? = null,
    var originalFilename: String = "",
    var skippedAtEpochMS: Long? = null,
)

/** What the phone remembers of one item: its client id and each file's record. */
@Serializable
data class PhotoBackupRecord(
    var clientPhotoId: String,
    var roles: MutableMap<String, PhotoBackupRoleRecord> = mutableMapOf(),
) {
    val serverPhotoId: String? get() = roles.values.firstNotNullOfOrNull { role -> role.serverPhotoId }
    val isOriginalOnServer: Boolean get() = roles[PhotoBackupFileRole.Original.serverName]?.isOnServer == true
}

/** What the backup decides to send: pure, so it is tested without a phone. */
object PhotoBackupPlanner {
    /** A file the server refused this many times is left alone until the person retries it. */
    const val MAXIMUM_REFUSAL_COUNT = 3

    /** The id that names an item across phones: the cloud id when the library has one, else a local one. */
    fun clientPhotoId(item: PhotoBackupItem): String = item.cloudIdentifier?.takeIf { identifier -> identifier.isNotEmpty() } ?: ("local:" + item.localIdentifier)

    /** The files that still need to go up, newest items first, each item's original before its other files. */
    fun plan(items: List<PhotoBackupItem>, records: Map<String, PhotoBackupRecord>, inFlight: Set<String> = emptySet()): List<PhotoBackupUpload> {
        val ordered = items.sortedWith { first, second ->
            val firstDate = first.creationInstant
            val secondDate = second.creationInstant
            when {
                firstDate != null && secondDate != null && firstDate != secondDate -> secondDate.compareTo(firstDate)
                firstDate == null && secondDate != null -> 1
                firstDate != null && secondDate == null -> -1
                else -> first.localIdentifier.compareTo(second.localIdentifier)
            }
        }
        val uploads = mutableListOf<PhotoBackupUpload>()
        for (item in ordered) {
            val record = records[item.localIdentifier]
            val clientPhotoId = record?.clientPhotoId ?: clientPhotoId(item)
            for (resource in item.resources.sortedBy { candidate -> candidate.role.uploadOrder }) {
                if (!needsUpload(resource.role, item, record) || PhotoBackupUpload.key(item.localIdentifier, resource.role) in inFlight) continue
                uploads.add(
                    PhotoBackupUpload(
                        item.localIdentifier, resource.role, clientPhotoId, resource.originalFilename, resource.contentType, resource.isLocallyAvailable,
                        resource.fileSizeBytes, item.modificationInstant, headers(item, resource, clientPhotoId),
                    ),
                )
            }
        }
        return uploads
    }

    fun preferringLocalFiles(uploads: List<PhotoBackupUpload>): List<PhotoBackupUpload> =
        uploads.filter { upload -> upload.isLocallyAvailable } + uploads.filter { upload -> !upload.isLocallyAvailable }

    fun needsUpload(role: PhotoBackupFileRole, item: PhotoBackupItem, record: PhotoBackupRecord?): Boolean {
        val state = record?.roles?.get(role.serverName) ?: return true
        if (state.skippedAtEpochMS != null || state.refusalCount >= MAXIMUM_REFUSAL_COUNT) return false
        if (!state.isOnServer) return true
        // An edit made after the file went up sends the edited version again.
        val modified = item.modificationInstant
        val sent = state.assetModificationEpochMS
        return role == PhotoBackupFileRole.Edited && modified != null && sent != null && modified.toEpochMilli() - sent > 1000
    }

    fun headers(item: PhotoBackupItem, resource: PhotoBackupResource, clientPhotoId: String): Map<String, String> {
        val headers = mutableMapOf(
            PhotoBackupHeader.CLIENT_ID to clientPhotoId, PhotoBackupHeader.FILE_ROLE to resource.role.serverName,
            PhotoBackupHeader.KIND to item.mediaKind.serverName, PhotoBackupHeader.IS_FAVORITE to item.isFavorite.toString(),
            PhotoBackupHeader.IS_SCREENSHOT to item.isScreenshot.toString(), PhotoBackupHeader.SOURCE to "android",
        )
        val filename = headerSafeFilename(resource.originalFilename)
        if (filename.isNotEmpty()) headers[PhotoBackupHeader.FILENAME] = filename
        if (resource.contentType.isNotEmpty()) headers[PhotoBackupHeader.CONTENT_TYPE] = resource.contentType
        item.creationInstant?.let { instant -> headers[PhotoBackupHeader.TAKEN_AT] = ServerTime.text(instant) }
        item.location?.let { location ->
            if (kotlin.math.abs(location.latitude) <= 90 && kotlin.math.abs(location.longitude) <= 180) {
                headers[PhotoBackupHeader.LATITUDE] = "%.6f".format(java.util.Locale.ROOT, location.latitude)
                headers[PhotoBackupHeader.LONGITUDE] = "%.6f".format(java.util.Locale.ROOT, location.longitude)
            }
        }
        return headers
    }

    /** A file name that is safe in a header: printable ASCII, no slashes, at most 200 characters. */
    fun headerSafeFilename(name: String): String =
        name.trim().map { character -> if (character.code < 0x20 || character.code > 0x7E || character == '/' || character == '\\') '_' else character }.joinToString("").take(200)
}

/** What became of one send. */
sealed class PhotoBackupOutcome {
    data class Uploaded(val serverPhotoId: String) : PhotoBackupOutcome()

    data object SignInNeeded : PhotoBackupOutcome()

    data class Refused(val failure: PhotoBackupFailure) : PhotoBackupOutcome()

    data class RetryLater(val failure: PhotoBackupFailure?, val isConnectivityFailure: Boolean, val retryAfterSeconds: Int? = null) : PhotoBackupOutcome()

    /** The upload is no longer on the server (it expired): start the file again. */
    data object UploadMovedOn : PhotoBackupOutcome()

    data object Cancelled : PhotoBackupOutcome()

    companion object {
        const val LONGEST_RETRY_AFTER_SECONDS = 3600
        private const val PASSWORD_WITHOUT_PHOTOS_REASON = "does not grant photos"

        /** A wait the server asked for, in seconds (a number, or an HTTP date), never longer than an hour. */
        fun retryAfterSeconds(header: String?, nowEpochSeconds: Long = Instant.now().epochSecond): Int? {
            val text = header?.trim().orEmpty()
            if (text.isEmpty()) return null
            text.toIntOrNull()?.let { seconds -> return if (seconds > 0) minOf(seconds, LONGEST_RETRY_AFTER_SECONDS) else null }
            val date = try {
                java.time.ZonedDateTime.parse(text, java.time.format.DateTimeFormatter.RFC_1123_DATE_TIME).toEpochSecond()
            } catch (_: Exception) {
                return null
            }
            val seconds = date - nowEpochSeconds
            return if (seconds > 0) minOf(seconds, LONGEST_RETRY_AFTER_SECONDS.toLong()).toInt() else null
        }

        fun interpret(
            statusCode: Int?,
            photoIdHeader: String?,
            bodyText: String?,
            errorDescription: String?,
            isConnectivityFailure: Boolean,
            isCancelled: Boolean = false,
            retryAfterHeader: String? = null,
            nowEpochSeconds: Long = Instant.now().epochSecond,
        ): PhotoBackupOutcome {
            if (isCancelled) return Cancelled
            if (errorDescription != null) return RetryLater(PhotoBackupFailure.SystemDescription(errorDescription), isConnectivityFailure)
            if (statusCode == null) return RetryLater(null, true)
            val reason = bodyText?.trim()?.ifEmpty { null }
            return when (statusCode) {
                200, 201 -> {
                    val photoId = photoIdHeader?.trim().orEmpty()
                    if (photoId.isNotEmpty()) Uploaded(photoId) else RetryLater(PhotoBackupFailure.ServerDidNotSayWhichPhoto, false)
                }
                401 -> SignInNeeded
                403 -> if (reason?.lowercase()?.contains(PASSWORD_WITHOUT_PHOTOS_REASON) == true) SignInNeeded else RetryLater(reason?.let { text -> PhotoBackupFailure.ServerReason(text) }, false)
                404, 409, 410 -> UploadMovedOn
                // The server has no room: its own words are not needed, and the wait it asks for stands.
                507 -> RetryLater(PhotoBackupFailure.ServerOutOfSpace, false, retryAfterSeconds(retryAfterHeader, nowEpochSeconds))
                408, 425, 429, in 500..599 -> RetryLater(reason?.let { text -> PhotoBackupFailure.ServerReason(text) }, false, retryAfterSeconds(retryAfterHeader, nowEpochSeconds))
                in 400..499 -> Refused(PhotoBackupFailure.FileRefused(reason))
                else -> RetryLater(null, false)
            }
        }
    }
}

/** The device password backup signs in with: a sync credential that grants photos only. */
@Serializable
data class PhotoBackupCredential(val credentialId: String, val credentialName: String, val username: String, val password: String) {
    val authorizationHeader: String get() = "Basic " + Base64.getEncoder().encodeToString("$username:$password".toByteArray())
}

/** What the server says of a resumable upload. */
sealed class PhotoBackupServerUpload {
    data class Receiving(val offsetBytes: Long) : PhotoBackupServerUpload()

    data class Complete(val serverPhotoId: String) : PhotoBackupServerUpload()

    data object Gone : PhotoBackupServerUpload()
}

/** The requests of the resumable upload protocol (create, ask, continue), as the iPhone app sends them. */
object PhotoBackupRequest {
    const val UPLOAD_PATH = "/api/v1/photos/uploads"
    const val SMALL_REQUEST_TIMEOUT_MS = 20_000L

    fun isUploadPath(path: String): Boolean {
        val prefix = "$UPLOAD_PATH/"
        if (!path.startsWith(prefix)) return false
        val identifier = path.removePrefix(prefix)
        return identifier.isNotEmpty() && '/' !in identifier && '?' !in identifier
    }

    fun declaredDigest(sha256: String?): String? = sha256?.lowercase()?.takeIf { digest -> digest.length == 64 && digest.all { character -> character in '0'..'9' || character in 'a'..'f' } }

    fun creation(server: ServerAddress, credential: PhotoBackupCredential, upload: PhotoBackupUpload, sizeBytes: Long, sha256: String? = null): HTTPRequest {
        val headers = upload.headers.toMutableMap()
        declaredDigest(sha256)?.let { digest -> headers[PhotoBackupHeader.DECLARED_SHA256] = digest }
        headers[PhotoBackupHeader.UPLOAD_COMPLETE] = "?0"
        headers[PhotoBackupHeader.UPLOAD_LENGTH] = sizeBytes.toString()
        headers["Authorization"] = credential.authorizationHeader
        return HTTPRequest(server.endpoint(UPLOAD_PATH).toString(), "POST", headers, null, SMALL_REQUEST_TIMEOUT_MS)
    }

    fun status(server: ServerAddress, credential: PhotoBackupCredential, uploadPath: String): HTTPRequest =
        HTTPRequest(server.endpoint(uploadPath).toString(), "HEAD", mapOf("Authorization" to credential.authorizationHeader), null, SMALL_REQUEST_TIMEOUT_MS)

    fun continuation(server: ServerAddress, credential: PhotoBackupCredential, uploadPath: String, offsetBytes: Long, body: ByteArray): HTTPRequest =
        HTTPRequest(
            server.endpoint(uploadPath).toString(), "PATCH",
            mapOf(
                PhotoBackupHeader.CONTENT_TYPE to "application/partial-upload", PhotoBackupHeader.UPLOAD_OFFSET to offsetBytes.toString(),
                PhotoBackupHeader.UPLOAD_COMPLETE to "?1", "Authorization" to credential.authorizationHeader,
            ),
            body,
        )

    /** The upload path the server created, from its answer to a creation (201 with a Location), or the reason not. */
    fun interpretCreation(statusCode: Int, locationHeader: String?, bodyText: String?, retryAfterHeader: String?): Pair<String?, PhotoBackupOutcome?> {
        if (statusCode != 201) return null to interpretOtherwise(statusCode, bodyText?.take(2048), retryAfterHeader)
        val location = locationHeader?.trim().orEmpty()
        val path = (if (location.contains("://")) "/" + location.substringAfter("://").substringAfter('/', "") else location).substringBefore('?')
        if (!isUploadPath(path)) return null to PhotoBackupOutcome.RetryLater(null, false)
        return path to null
    }

    /** What an answer to asking about an upload means. */
    fun interpretState(
        statusCode: Int,
        offsetHeader: String?,
        completeHeader: String?,
        photoIdHeader: String?,
        bodyText: String?,
        retryAfterHeader: String?,
    ): Pair<PhotoBackupServerUpload?, PhotoBackupOutcome?> = when (statusCode) {
        200, 204 -> {
            val offsetBytes = offsetHeader?.trim()?.toLongOrNull()
            if (offsetBytes == null || offsetBytes < 0) {
                null to PhotoBackupOutcome.RetryLater(null, false)
            } else if (completeHeader?.trim() != "?1") {
                PhotoBackupServerUpload.Receiving(offsetBytes) to null
            } else {
                val photoId = photoIdHeader?.trim().orEmpty()
                if (photoId.isEmpty()) null to PhotoBackupOutcome.RetryLater(PhotoBackupFailure.ServerDidNotSayWhichPhoto, false) else PhotoBackupServerUpload.Complete(photoId) to null
            }
        }
        403, 404, 410 -> PhotoBackupServerUpload.Gone to null
        else -> null to interpretOtherwise(statusCode, bodyText, retryAfterHeader)
    }

    private fun interpretOtherwise(statusCode: Int, bodyText: String?, retryAfterHeader: String?): PhotoBackupOutcome {
        val outcome = PhotoBackupOutcome.interpret(statusCode, null, bodyText, null, false, retryAfterHeader = retryAfterHeader)
        if (outcome != PhotoBackupOutcome.UploadMovedOn) return outcome
        val reason = bodyText?.trim()?.ifEmpty { null }
        return PhotoBackupOutcome.RetryLater(reason?.let { text -> PhotoBackupFailure.ServerReason(text) }, false)
    }
}

/** How a backup pass is waiting or working: what the status screen says. */
sealed class PhotoBackupPhase {
    data object Off : PhotoBackupPhase()

    data object Idle : PhotoBackupPhase()

    data object Listing : PhotoBackupPhase()

    data object Uploading : PhotoBackupPhase()

    data object WaitingForNetwork : PhotoBackupPhase()

    data object WaitingForServer : PhotoBackupPhase()

    /** The person has not allowed access to the library (or only some of it). */
    data class NeedsAccess(val isLimited: Boolean) : PhotoBackupPhase()

    data object NeedsSignIn : PhotoBackupPhase()

    data object Paused : PhotoBackupPhase()
}

/** What the backup screen shows. */
data class PhotoBackupStatus(
    val phase: PhotoBackupPhase = PhotoBackupPhase.Off,
    val backedUpItemCount: Int = 0,
    val libraryItemCount: Int = 0,
    val plannedFileCount: Int = 0,
    val finishedFileCount: Int = 0,
    val refusedFileCount: Int = 0,
    val skippedFileCount: Int = 0,
    val lastFailure: PhotoBackupFailure? = null,
    val lastPassFinishedEpochMS: Long? = null,
    val estimatedSecondsLeft: Int? = null,
) {
    val isEnabled: Boolean get() = phase != PhotoBackupPhase.Off
    val shownFinishedFileCount: Int get() = minOf(finishedFileCount, plannedFileCount)
    val itemsLeftCount: Int get() = maxOf(0, libraryItemCount - backedUpItemCount)
}

/** Which client ids of a library the server already has, so the phone does not send them again. */
@Serializable private class FindPhotosAnswer(val FindPhotos: List<PhotoBackupServerPhoto>)

@Serializable private class CreatedCredential(val credential: CredentialName, val username: String, val password: String)

@Serializable private class CredentialName(val id: String, val credentialName: String)

@Serializable private class CreateCredentialAnswer(val CreateSyncCredential: CreatedCredential)

suspend fun Connection.findPhotos(clientPhotoIds: List<String>): List<PhotoBackupServerPhoto> =
    execute<FindPhotosAnswer>(
        PhotoDocuments.FIND_PHOTOS,
        mapOf("clientPhotoIds" to kotlinx.serialization.json.JsonArray(clientPhotoIds.map { identifier -> JsonPrimitive(identifier) })),
    ).FindPhotos

/** Makes the device password backup signs in with. It is shown once, so it is kept on the phone. */
suspend fun Connection.createPhotoBackupCredential(credentialName: String): PhotoBackupCredential {
    val created = execute<CreateCredentialAnswer>(PhotoDocuments.CREATE_PHOTO_BACKUP_CREDENTIAL, mapOf("credentialName" to JsonPrimitive(credentialName))).CreateSyncCredential
    return PhotoBackupCredential(created.credential.id, created.credential.credentialName, created.username, created.password)
}

/** One file the backup gave up on (the server refused it too often) or the person chose to skip. */
data class PhotoBackupRefusedFile(
    val localIdentifier: String,
    val clientPhotoId: String,
    val role: PhotoBackupFileRole,
    val filename: String,
    val refusalReason: String?,
    val refusalCount: Int,
    val skippedAtEpochMS: Long?,
) {
    val id: String get() = PhotoBackupUpload.key(localIdentifier, role)
    val isSkipped: Boolean get() = skippedAtEpochMS != null
}

/** The files the backup left alone, and what the person can do about each: try it again, or skip it for good. */
object PhotoBackupRefusals {
    private fun entries(records: Map<String, PhotoBackupRecord>): List<PhotoBackupRefusedFile> = records.flatMap { (localIdentifier, record) ->
        record.roles.mapNotNull { (roleName, state) ->
            val role = PhotoBackupFileRole.entries.firstOrNull { candidate -> candidate.serverName == roleName } ?: return@mapNotNull null
            PhotoBackupRefusedFile(localIdentifier, record.clientPhotoId, role, state.originalFilename.ifEmpty { localIdentifier }, state.refusalReason, state.refusalCount, state.skippedAtEpochMS)
        }
    }

    /** Files refused as often as the backup tries, not skipped by the person: they need an answer. */
    fun refused(records: Map<String, PhotoBackupRecord>): List<PhotoBackupRefusedFile> =
        entries(records).filter { file -> !file.isSkipped && file.refusalCount >= PhotoBackupPlanner.MAXIMUM_REFUSAL_COUNT }.sortedBy { file -> file.filename }

    fun skipped(records: Map<String, PhotoBackupRecord>): List<PhotoBackupRefusedFile> =
        entries(records).filter { file -> file.isSkipped }.sortedBy { file -> file.filename }

    /** Forgets the refusals and the skip of one file, so the next pass sends it again. */
    fun tryAgain(records: MutableMap<String, PhotoBackupRecord>, localIdentifier: String, role: PhotoBackupFileRole) {
        records[localIdentifier]?.roles?.get(role.serverName)?.let { state -> state.refusalCount = 0; state.refusalReason = null; state.skippedAtEpochMS = null }
    }

    fun skip(records: MutableMap<String, PhotoBackupRecord>, localIdentifier: String, role: PhotoBackupFileRole, nowEpochMS: Long) {
        records[localIdentifier]?.roles?.get(role.serverName)?.let { state -> state.skippedAtEpochMS = nowEpochMS }
    }
}
