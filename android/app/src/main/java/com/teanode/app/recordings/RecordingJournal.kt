package com.teanode.app.recordings

import android.content.Context
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.recordings.LocalRecording
import java.io.File
import java.security.MessageDigest

/**
 * The recording journal: what the phone holds of every recording before the server has all of it. Each recording is a
 * folder with its manifest (a JSON file, replaced atomically so a crash never leaves half of one) and its audio files.
 * The recorder and the uploader both write it, so a change reads the manifest, changes it and writes it in one step.
 */
class RecordingJournal(context: Context) {
    private val directory = File(context.filesDir, "recordings").apply { mkdirs() }

    private fun folder(recordingId: String) = File(directory, recordingId)

    private fun manifest(recordingId: String) = File(folder(recordingId), "manifest.json")

    @Synchronized
    fun create(recording: LocalRecording) {
        check(!manifest(recording.id).exists()) { "This local recording already exists." }
        write(recording)
    }

    @Synchronized
    fun save(recording: LocalRecording) = write(recording)

    @Synchronized
    fun read(recordingId: String): LocalRecording? =
        runCatching { TeaNodeJson.decodeFromString(LocalRecording.serializer(), manifest(recordingId).readText()) }.getOrNull()

    /** Reads, changes and writes one recording while no other writer does; returns what was written, or null when there is none. */
    @Synchronized
    fun update(recordingId: String, change: (LocalRecording) -> LocalRecording): LocalRecording? {
        val changed = change(read(recordingId) ?: return null)
        write(changed)
        return changed
    }

    @Synchronized
    fun recordings(accountKey: String): List<LocalRecording> =
        (directory.listFiles() ?: emptyArray()).mapNotNull { entry -> read(entry.name) }.filter { recording -> recording.accountKey == accountKey }.sortedByDescending { recording -> recording.startedAtEpochMS }

    private fun write(recording: LocalRecording) {
        folder(recording.id).mkdirs()
        val temporary = File(folder(recording.id), "manifest.json.new")
        temporary.writeText(TeaNodeJson.encodeToString(LocalRecording.serializer(), recording))
        check(temporary.renameTo(manifest(recording.id))) { "The recording could not be saved." }
    }

    /** Where an original is kept: next to the manifest, named after its media id. */
    fun originalFile(recordingId: String, mediaId: String): File = File(folder(recordingId), "$mediaId.original")

    /** Lets go of an original the server holds, verified. Nothing but the phone's copy goes. */
    fun releaseUploadedOriginal(recordingId: String, mediaId: String) {
        originalFile(recordingId, mediaId).delete()
    }

    /** Takes a whole recording off the phone: its manifest and every file. */
    @Synchronized
    fun remove(recordingId: String) {
        folder(recordingId).deleteRecursively()
    }

    companion object {
        fun sha256(file: File): String {
            val digest = MessageDigest.getInstance("SHA-256")
            file.inputStream().use { stream ->
                val buffer = ByteArray(1 shl 20)
                while (true) {
                    val read = stream.read(buffer)
                    if (read < 0) break
                    digest.update(buffer, 0, read)
                }
            }
            return digest.digest().joinToString("") { byte -> "%02x".format(byte) }
        }

        fun sha256(bytes: ByteArray): String = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { byte -> "%02x".format(byte) }
    }
}
