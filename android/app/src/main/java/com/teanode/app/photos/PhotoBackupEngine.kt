package com.teanode.app.photos

import android.Manifest
import android.content.ContentUris
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.provider.MediaStore
import androidx.core.content.ContextCompat
import androidx.work.BackoffPolicy
import androidx.work.CoroutineWorker
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import com.teanode.app.TeaNodeApplication
import com.teanode.kit.Connection
import com.teanode.kit.photos.PhotoBackupCredential
import com.teanode.kit.photos.PhotoBackupFailure
import com.teanode.kit.photos.PhotoBackupFileRole
import com.teanode.kit.photos.PhotoBackupHeader
import com.teanode.kit.photos.PhotoBackupItem
import com.teanode.kit.photos.PhotoBackupLocation
import com.teanode.kit.photos.PhotoBackupMediaKind
import com.teanode.kit.photos.PhotoBackupOutcome
import com.teanode.kit.photos.PhotoBackupPhase
import com.teanode.kit.photos.PhotoBackupPlanner
import com.teanode.kit.photos.PhotoBackupRecord
import com.teanode.kit.photos.PhotoBackupRequest
import com.teanode.kit.photos.PhotoBackupResource
import com.teanode.kit.photos.PhotoBackupRoleRecord
import com.teanode.kit.photos.PhotoBackupServerUpload
import com.teanode.kit.photos.PhotoBackupStatus
import com.teanode.kit.photos.PhotoBackupTimeLeft
import com.teanode.kit.photos.PhotoBackupUpload
import com.teanode.kit.photos.createPhotoBackupCredential
import com.teanode.kit.photos.revokePhotoBackupCredential
import com.teanode.kit.photos.findPhotos
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody
import okio.BufferedSink
import okio.source
import java.io.File
import java.io.IOException
import java.security.MessageDigest
import java.time.Instant
import java.util.concurrent.TimeUnit

/**
 * What the person chose for photo backup, kept on the phone per account: on or off, paused, and whether mobile data may
 * be used. The device password backup signs in with is kept sealed with the Keystore, never in these preferences.
 */
class PhotoBackupSettings(private val context: Context, private val accountKey: String) {
    private val preferences = context.getSharedPreferences("teanode.photoBackup", Context.MODE_PRIVATE)

    private fun key(name: String) = "$accountKey|$name"

    var isEnabled: Boolean
        get() = preferences.getBoolean(key("isEnabled"), false)
        set(isOn) = preferences.edit().putBoolean(key("isEnabled"), isOn).apply()
    var isPaused: Boolean
        get() = preferences.getBoolean(key("isPaused"), false)
        set(isOn) = preferences.edit().putBoolean(key("isPaused"), isOn).apply()
    var usesMobileData: Boolean
        get() = preferences.getBoolean(key("usesMobileData"), false)
        set(isOn) = preferences.edit().putBoolean(key("usesMobileData"), isOn).apply()
    var includesHiddenFolders: Boolean
        get() = preferences.getBoolean(key("includesHiddenFolders"), false)
        set(isOn) = preferences.edit().putBoolean(key("includesHiddenFolders"), isOn).apply()
    var lastPassFinishedEpochMS: Long?
        get() = preferences.getLong(key("lastPassFinishedEpochMS"), -1).takeIf { time -> time >= 0 }
        set(time) = preferences.edit().putLong(key("lastPassFinishedEpochMS"), time ?: -1).apply()

    private fun credentialName() = "photoBackupCredential|$accountKey"

    fun credential(): PhotoBackupCredential? =
        runCatching { Json.decodeFromString(PhotoBackupCredential.serializer(), (com.teanode.app.session.KeystoreSecretStore(context).read(credentialName()) ?: return null).decodeToString()) }.getOrNull()

    fun saveCredential(credential: PhotoBackupCredential) {
        com.teanode.app.session.KeystoreSecretStore(context).write(credentialName(), Json.encodeToString(PhotoBackupCredential.serializer(), credential).toByteArray())
    }

    fun forgetCredential() = com.teanode.app.session.KeystoreSecretStore(context).delete(credentialName())

    // What the phone remembers of each item: its client id and which files the server has.
    private val recordsFile get() = File(context.filesDir, "photo-backup-" + accountKey.hashCode().toUInt().toString(16) + ".json")
    private val recordsSerializer = MapSerializer(String.serializer(), PhotoBackupRecord.serializer())

    @Synchronized
    fun records(): MutableMap<String, PhotoBackupRecord> =
        runCatching { Json.decodeFromString(recordsSerializer, recordsFile.readText()).toMutableMap() }.getOrDefault(mutableMapOf())

    @Synchronized
    fun saveRecords(records: Map<String, PhotoBackupRecord>) {
        val temporary = File(recordsFile.path + ".new")
        temporary.writeText(Json.encodeToString(recordsSerializer, records))
        temporary.renameTo(recordsFile)
    }
}

/** What the phone's library holds, read from MediaStore: every picture and video, with their files. */
object MediaStoreLibrary {
    fun hasFullAccess(context: Context): Boolean =
        if (Build.VERSION.SDK_INT >= 33) {
            ContextCompat.checkSelfPermission(context, Manifest.permission.READ_MEDIA_IMAGES) == PackageManager.PERMISSION_GRANTED &&
                ContextCompat.checkSelfPermission(context, Manifest.permission.READ_MEDIA_VIDEO) == PackageManager.PERMISSION_GRANTED
        } else {
            ContextCompat.checkSelfPermission(context, Manifest.permission.READ_EXTERNAL_STORAGE) == PackageManager.PERMISSION_GRANTED
        }

    /** Whether the person allowed only some photos (Android 14's "select photos"), which backup cannot read the rest of. */
    fun hasLimitedAccess(context: Context): Boolean =
        Build.VERSION.SDK_INT >= 34 && !hasFullAccess(context) &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.READ_MEDIA_VISUAL_USER_SELECTED) == PackageManager.PERMISSION_GRANTED

    /** Whether the person allowed reading where pictures were taken (their EXIF position), which is a separate permission. */
    fun hasMediaLocationAccess(context: Context): Boolean =
        Build.VERSION.SDK_INT < 29 || ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_MEDIA_LOCATION) == PackageManager.PERMISSION_GRANTED

    val permissions: Array<String>
        get() = when {
            Build.VERSION.SDK_INT >= 34 -> arrayOf(Manifest.permission.READ_MEDIA_IMAGES, Manifest.permission.READ_MEDIA_VIDEO, Manifest.permission.READ_MEDIA_VISUAL_USER_SELECTED, Manifest.permission.ACCESS_MEDIA_LOCATION)
            Build.VERSION.SDK_INT >= 33 -> arrayOf(Manifest.permission.READ_MEDIA_IMAGES, Manifest.permission.READ_MEDIA_VIDEO, Manifest.permission.ACCESS_MEDIA_LOCATION)
            else -> arrayOf(Manifest.permission.READ_EXTERNAL_STORAGE, Manifest.permission.ACCESS_MEDIA_LOCATION)
        }

    /** Whether a folder is one the person keeps out of sight: named Hidden, or starting with a dot. */
    fun isHiddenFolder(relativePath: String): Boolean = relativePath.split('/').any { part -> part.startsWith(".") || part.equals("Hidden", ignoreCase = true) }

    /** Where a picture was taken, from its EXIF, when the person allowed it. Videos carry theirs elsewhere and are left out. */
    fun locationOf(context: Context, localIdentifier: String): PhotoBackupLocation? {
        if (localIdentifier.startsWith("video:") || !hasMediaLocationAccess(context)) return null
        return runCatching {
            val uri = if (Build.VERSION.SDK_INT >= 29) MediaStore.setRequireOriginal(uriOf(localIdentifier)) else uriOf(localIdentifier)
            context.contentResolver.openInputStream(uri)?.use { stream -> androidx.exifinterface.media.ExifInterface(stream).latLong?.let { position -> PhotoBackupLocation(position[0], position[1]) } }
        }.getOrNull()
    }

    fun items(context: Context, includesHiddenFolders: Boolean = false): List<PhotoBackupItem> {
        val items = mutableListOf<PhotoBackupItem>()
        for (isVideo in listOf(false, true)) {
            val collection = if (isVideo) MediaStore.Video.Media.EXTERNAL_CONTENT_URI else MediaStore.Images.Media.EXTERNAL_CONTENT_URI
            val projection = arrayOf(
                MediaStore.MediaColumns._ID, MediaStore.MediaColumns.DISPLAY_NAME, MediaStore.MediaColumns.MIME_TYPE, MediaStore.MediaColumns.SIZE,
                MediaStore.MediaColumns.DATE_TAKEN, MediaStore.MediaColumns.DATE_ADDED, MediaStore.MediaColumns.DATE_MODIFIED, MediaStore.MediaColumns.RELATIVE_PATH,
            )
            context.contentResolver.query(collection, projection, null, null, MediaStore.MediaColumns.DATE_ADDED + " DESC")?.use { cursor ->
                while (cursor.moveToNext()) {
                    val id = cursor.getLong(0)
                    val name = cursor.getString(1).orEmpty()
                    val contentType = cursor.getString(2).orEmpty()
                    val sizeBytes = cursor.getLong(3)
                    val takenMS = cursor.getLong(4).takeIf { time -> time > 0 } ?: (cursor.getLong(5) * 1000)
                    val folder = cursor.getString(7).orEmpty()
                    val identifier = (if (isVideo) "video:" else "image:") + id
                    if (!includesHiddenFolders && isHiddenFolder(folder)) continue
                    items.add(
                        PhotoBackupItem(
                            localIdentifier = identifier, creationInstant = if (takenMS > 0) Instant.ofEpochMilli(takenMS) else null,
                            modificationInstant = Instant.ofEpochSecond(cursor.getLong(6)),
                            isScreenshot = folder.contains("Screenshots", ignoreCase = true), isInHiddenFolder = isHiddenFolder(folder),
                            mediaKind = if (isVideo) PhotoBackupMediaKind.Video else PhotoBackupMediaKind.Picture,
                            resources = listOf(PhotoBackupResource(PhotoBackupFileRole.Original, name, contentType, true, sizeBytes)),
                        ),
                    )
                }
            }
        }
        return items
    }

    fun uriOf(localIdentifier: String) = ContentUris.withAppendedId(
        if (localIdentifier.startsWith("video:")) MediaStore.Video.Media.EXTERNAL_CONTENT_URI else MediaStore.Images.Media.EXTERNAL_CONTENT_URI, localIdentifier.substringAfter(':').toLong(),
    )
}

/** What the backup screen shows, shared between the worker that fills it and the screen that reads it. */
object PhotoBackupProgress {
    private val statusFlow = MutableStateFlow(PhotoBackupStatus())
    val status: StateFlow<PhotoBackupStatus> = statusFlow.asStateFlow()

    fun publish(change: (PhotoBackupStatus) -> PhotoBackupStatus) = statusFlow.update(change)

    private fun <Value> MutableStateFlow<Value>.update(change: (Value) -> Value) {
        while (true) {
            val before = value
            if (compareAndSet(before, change(before))) return
        }
    }

    fun reset(status: PhotoBackupStatus) {
        statusFlow.value = status
    }
}

/** Starts and stops the work that copies the library to the server. */
object PhotoBackupScheduler {
    private const val PERIODIC = "photo-backup-periodic"
    private const val NOW = "photo-backup-now"

    private fun constraints(usesMobileData: Boolean) =
        Constraints.Builder().setRequiredNetworkType(if (usesMobileData) NetworkType.CONNECTED else NetworkType.UNMETERED).build()

    /** Tries again every few hours and whenever the network comes back, while the person leaves backup on. */
    fun schedule(context: Context, usesMobileData: Boolean) {
        val request = PeriodicWorkRequestBuilder<PhotoBackupWorker>(3, TimeUnit.HOURS).setConstraints(constraints(usesMobileData))
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 1, TimeUnit.MINUTES).build()
        WorkManager.getInstance(context).enqueueUniquePeriodicWork(PERIODIC, ExistingPeriodicWorkPolicy.UPDATE, request)
    }

    fun backUpNow(context: Context, usesMobileData: Boolean) {
        val request = OneTimeWorkRequestBuilder<PhotoBackupWorker>().setConstraints(constraints(usesMobileData))
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS).build()
        WorkManager.getInstance(context).enqueueUniqueWork(NOW, ExistingWorkPolicy.REPLACE, request)
    }

    fun cancel(context: Context) {
        WorkManager.getInstance(context).cancelUniqueWork(PERIODIC)
        WorkManager.getInstance(context).cancelUniqueWork(NOW)
    }
}

/**
 * One pass of the backup: reads the library, asks the server which items it already has, and sends what it does not,
 * newest first, over the resumable upload protocol (create, ask the offset, send the rest) with the device password.
 */
class PhotoBackupWorker(private val context: Context, parameters: WorkerParameters) : CoroutineWorker(context, parameters) {
    private companion object {
        const val NOTIFICATION_CHANNEL = "photo-backup"
        const val NOTIFICATION_ID = 4102

        // Back up now and the periodic pass can start together; only one of them may make the device password.
        val credentialLock = kotlinx.coroutines.sync.Mutex()
    }

    private val client = OkHttpClient.Builder().readTimeout(120, TimeUnit.SECONDS).writeTimeout(120, TimeUnit.SECONDS).callTimeout(0, TimeUnit.SECONDS).build()

    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val session = (context.applicationContext as TeaNodeApplication).session
        val connection = session.connection.value ?: return@withContext Result.success()
        val settings = PhotoBackupSettings(context, connection.accountKey)
        if (!settings.isEnabled || settings.isPaused) return@withContext Result.success()
        if (!MediaStoreLibrary.hasFullAccess(context)) {
            PhotoBackupProgress.publish { status -> status.copy(phase = PhotoBackupPhase.NeedsAccess(MediaStoreLibrary.hasLimitedAccess(context))) }
            return@withContext Result.success()
        }
        try {
            pass(connection, settings)
        } catch (failure: IOException) {
            PhotoBackupProgress.publish { status -> status.copy(phase = PhotoBackupPhase.WaitingForNetwork) }
            Result.retry()
        } catch (failure: Exception) {
            if (failure is kotlinx.coroutines.CancellationException) throw failure
            // Not the network: something on the phone. Shown on the backup screen rather than left as "listing".
            PhotoBackupProgress.publish { status ->
                status.copy(phase = PhotoBackupPhase.Idle, lastFailure = PhotoBackupFailure.SystemDescription(failure.message ?: failure.javaClass.simpleName))
            }
            Result.failure()
        }
    }

    private suspend fun pass(connection: Connection, settings: PhotoBackupSettings): Result {
        val credential = credentialFor(connection, settings)
        PhotoBackupProgress.publish { status -> status.copy(phase = PhotoBackupPhase.Listing) }
        val items = MediaStoreLibrary.items(context, settings.includesHiddenFolders)
        val records = settings.records()
        markWhatTheServerHas(connection, items, records)
        settings.saveRecords(records)
        // Where a picture was taken is read only for the ones about to go up: reading every file's header would take long.
        val firstPlan = PhotoBackupPlanner.plan(items, records)
        val unsent = firstPlan.map { upload -> upload.localIdentifier }.toSet()
        val located = items.map { item -> if (item.localIdentifier in unsent) item.copy(location = MediaStoreLibrary.locationOf(context, item.localIdentifier)) else item }
        val uploads = PhotoBackupPlanner.plan(located, records)
        if (uploads.isNotEmpty()) runCatching { setForeground(foregroundInfo(0, uploads.size)) }
        val backedUp = items.count { item -> records[item.localIdentifier]?.isOriginalOnServer == true }
        PhotoBackupProgress.publish { status -> status.copy(phase = PhotoBackupPhase.Uploading, libraryItemCount = items.size, backedUpItemCount = backedUp, plannedFileCount = uploads.size, finishedFileCount = 0) }
        val timeLeft = PhotoBackupTimeLeft()
        var finished = 0
        var refused = records.values.sumOf { record -> record.roles.values.count { role -> role.refusalCount >= PhotoBackupPlanner.MAXIMUM_REFUSAL_COUNT && role.skippedAtEpochMS == null } }
        var lastFailure: PhotoBackupFailure? = null
        for (upload in PhotoBackupPlanner.preferringLocalFiles(uploads)) {
            if (isStopped) return Result.retry()
            val outcome = send(settings, connection, credential, upload)
            val record = records.getOrPut(upload.localIdentifier) { PhotoBackupRecord(upload.clientPhotoId) }
            val role = record.roles.getOrPut(upload.role.serverName) { PhotoBackupRoleRecord() }
            role.originalFilename = upload.filename
            when (outcome) {
                is PhotoBackupOutcome.Uploaded -> {
                    role.isOnServer = true; role.serverPhotoId = outcome.serverPhotoId; role.assetModificationEpochMS = upload.assetModificationInstant?.toEpochMilli(); role.refusalCount = 0
                    finished += 1
                }
                is PhotoBackupOutcome.Refused -> {
                    role.refusalCount += 1; refused += 1; lastFailure = outcome.failure; finished += 1
                    role.refusalReason = (outcome.failure as? PhotoBackupFailure.FileRefused)?.serverReason ?: (outcome.failure as? PhotoBackupFailure.SystemDescription)?.description
                }
                PhotoBackupOutcome.SignInNeeded -> {
                    settings.forgetCredential()
                    settings.saveRecords(records)
                    PhotoBackupProgress.publish { status -> status.copy(phase = PhotoBackupPhase.NeedsSignIn, lastFailure = PhotoBackupFailure.SignInRefused) }
                    return Result.success()
                }
                is PhotoBackupOutcome.RetryLater -> {
                    settings.saveRecords(records)
                    PhotoBackupProgress.publish { status ->
                        status.copy(phase = if (outcome.isConnectivityFailure) PhotoBackupPhase.WaitingForNetwork else PhotoBackupPhase.WaitingForServer, lastFailure = outcome.failure)
                    }
                    return Result.retry()
                }
                PhotoBackupOutcome.UploadMovedOn, PhotoBackupOutcome.Cancelled -> Unit
            }
            val backedUpNow = items.count { item -> records[item.localIdentifier]?.isOriginalOnServer == true }
            timeLeft.record(System.currentTimeMillis() / 1000.0, backedUpNow, true)
            if (finished % 5 == 0) {
                settings.saveRecords(records)
                runCatching { setForeground(foregroundInfo(finished, uploads.size)) }
            }
            PhotoBackupProgress.publish { status ->
                status.copy(backedUpItemCount = backedUpNow, finishedFileCount = finished, refusedFileCount = refused, lastFailure = lastFailure, estimatedSecondsLeft = timeLeft.estimatedSecondsLeft(items.size - backedUpNow))
            }
        }
        settings.saveRecords(records)
        settings.lastPassFinishedEpochMS = System.currentTimeMillis()
        PhotoBackupProgress.publish { status ->
            status.copy(phase = PhotoBackupPhase.Idle, lastPassFinishedEpochMS = settings.lastPassFinishedEpochMS, estimatedSecondsLeft = null, skippedFileCount = records.values.sumOf { record -> record.roles.values.count { role -> role.skippedAtEpochMS != null } })
        }
        return Result.success()
    }

    /**
     * The device password backup signs in with, made once. A password the phone could not keep is taken back on the
     * server before the failure goes on, so a failing phone does not leave a new valid password behind on every try.
     */
    private suspend fun credentialFor(connection: Connection, settings: PhotoBackupSettings): PhotoBackupCredential = credentialLock.withLock {
        settings.credential()?.let { kept -> return@withLock kept }
        val created = connection.createPhotoBackupCredential(deviceName())
        try {
            settings.saveCredential(created)
        } catch (failure: Exception) {
            runCatching { connection.revokePhotoBackupCredential(created.credentialId) }
            throw IllegalStateException("the device password could not be kept on this phone", failure)
        }
        created
    }

    /** Asks the server which client ids it has, so items another phone or an earlier install sent are not sent again. */
    private suspend fun markWhatTheServerHas(connection: Connection, items: List<PhotoBackupItem>, records: MutableMap<String, PhotoBackupRecord>) {
        val unknown = items.filter { item -> records[item.localIdentifier]?.isOriginalOnServer != true }
        for (batch in unknown.chunked(200)) {
            val clientIds = batch.map { item -> records[item.localIdentifier]?.clientPhotoId ?: PhotoBackupPlanner.clientPhotoId(item) }
            val found = connection.findPhotos(clientIds).associateBy { photo -> photo.clientPhotoId }
            for ((item, clientId) in batch.zip(clientIds)) {
                val serverPhoto = found[clientId] ?: continue
                val record = records.getOrPut(item.localIdentifier) { PhotoBackupRecord(clientId) }
                for (roleName in serverPhoto.photoFileRoles) {
                    val role = record.roles.getOrPut(roleName) { PhotoBackupRoleRecord() }
                    role.isOnServer = true; role.serverPhotoId = serverPhoto.id
                    if (roleName == "edited") role.assetModificationEpochMS = item.modificationInstant?.toEpochMilli()
                }
            }
        }
    }

    /** The notification shown while a long pass runs, so the system keeps the upload going with the app closed. */
    private fun foregroundInfo(finished: Int, planned: Int): androidx.work.ForegroundInfo {
        val manager = context.getSystemService(android.app.NotificationManager::class.java)
        manager.createNotificationChannel(android.app.NotificationChannel(NOTIFICATION_CHANNEL, context.getString(com.teanode.app.R.string.photos_backup_channel_name), android.app.NotificationManager.IMPORTANCE_LOW))
        val notification = androidx.core.app.NotificationCompat.Builder(context, NOTIFICATION_CHANNEL).setSmallIcon(com.teanode.app.R.mipmap.ic_launcher)
            .setContentTitle(context.getString(com.teanode.app.R.string.photos_backup_notification_title)).setContentText(context.getString(com.teanode.app.R.string.photos_backup_notification_text, finished, planned))
            .setProgress(planned, finished, false).setOngoing(true).setOnlyAlertOnce(true).build()
        return if (Build.VERSION.SDK_INT >= 29) androidx.work.ForegroundInfo(NOTIFICATION_ID, notification, android.content.pm.ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC) else androidx.work.ForegroundInfo(NOTIFICATION_ID, notification)
    }

    private fun deviceName(): String = "Android " + (Build.MODEL ?: "phone")

    private fun sha256(upload: PhotoBackupUpload): Pair<String, Long>? {
        val digest = MessageDigest.getInstance("SHA-256")
        var total = 0L
        val input = context.contentResolver.openInputStream(MediaStoreLibrary.uriOf(upload.localIdentifier)) ?: return null
        input.use { stream ->
            val buffer = ByteArray(1 shl 20)
            while (true) {
                val read = stream.read(buffer)
                if (read < 0) break
                digest.update(buffer, 0, read)
                total += read
            }
        }
        return digest.digest().joinToString("") { byte -> "%02x".format(byte) } to total
    }

    /** Sends one file: creates the upload, asks how much the server has, and sends the rest. */
    private fun send(settings: PhotoBackupSettings, connection: Connection, credential: PhotoBackupCredential, upload: PhotoBackupUpload): PhotoBackupOutcome {
        val (digest, sizeBytes) = try { sha256(upload) } catch (failure: IOException) { null } ?: return PhotoBackupOutcome.Refused(PhotoBackupFailure.FileIsNotInLibrary)
        if (sizeBytes == 0L) return PhotoBackupOutcome.Refused(PhotoBackupFailure.FileIsEmpty)
        val server = connection.account.server
        val creation = PhotoBackupRequest.creation(server, credential, upload, sizeBytes, digest)
        val created = client.newCall(Request.Builder().url(creation.url).method("POST", ByteArray(0).let { empty -> RequestBody.create(null, empty) }).apply { creation.headers.forEach { (name, headerValue) -> header(name, headerValue) } }.build()).execute().use { response ->
            PhotoBackupRequest.interpretCreation(response.code, response.header("Location"), response.body?.string(), response.header("Retry-After"))
        }
        val uploadPath = created.first ?: return created.second ?: PhotoBackupOutcome.RetryLater(null, false)
        val statusRequest = PhotoBackupRequest.status(server, credential, uploadPath)
        val state = client.newCall(Request.Builder().url(statusRequest.url).head().apply { statusRequest.headers.forEach { (name, headerValue) -> header(name, headerValue) } }.build()).execute().use { response ->
            PhotoBackupRequest.interpretState(response.code, response.header(PhotoBackupHeader.UPLOAD_OFFSET), response.header(PhotoBackupHeader.UPLOAD_COMPLETE), response.header(PhotoBackupHeader.PHOTO_ID), null, response.header("Retry-After"))
        }
        val serverUpload = state.first ?: return state.second ?: PhotoBackupOutcome.RetryLater(null, false)
        val offsetBytes = when (serverUpload) {
            is PhotoBackupServerUpload.Complete -> return PhotoBackupOutcome.Uploaded(serverUpload.serverPhotoId)
            PhotoBackupServerUpload.Gone -> return PhotoBackupOutcome.UploadMovedOn
            is PhotoBackupServerUpload.Receiving -> serverUpload.offsetBytes
        }
        val continuation = PhotoBackupRequest.continuation(server, credential, uploadPath, offsetBytes, ByteArray(0))
        val body = object : RequestBody() {
            override fun contentType() = "application/partial-upload".toMediaTypeOrNull()
            override fun contentLength() = sizeBytes - offsetBytes
            override fun writeTo(sink: BufferedSink) {
                context.contentResolver.openInputStream(MediaStoreLibrary.uriOf(upload.localIdentifier))?.use { stream ->
                    stream.skip(offsetBytes)
                    sink.writeAll(stream.source())
                }
            }
        }
        return client.newCall(Request.Builder().url(continuation.url).method("PATCH", body).apply { continuation.headers.filterKeys { name -> name != "Content-Type" }.forEach { (name, headerValue) -> header(name, headerValue) } }.build()).execute().use { response ->
            PhotoBackupOutcome.interpret(response.code, response.header(PhotoBackupHeader.PHOTO_ID), response.body?.string()?.take(2048), null, false, retryAfterHeader = response.header("Retry-After"))
        }
    }
}
