package com.teanode.kit.recordings

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import java.util.Base64
import java.util.UUID
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString

/** A place small texts are kept under a key: preferences in the app, a map in the tests. */
interface RecordingDraftStorage {
    fun read(storageKey: String): String?
    fun write(storageKey: String, text: String)
    fun remove(storageKey: String)
}

/** The drafts kept in a map, for tests. */
class InMemoryRecordingDraftStorage : RecordingDraftStorage {
    private val texts = mutableMapOf<String, String>()
    override fun read(storageKey: String): String? = texts[storageKey]
    override fun write(storageKey: String, text: String) { texts[storageKey] = text }
    override fun remove(storageKey: String) { texts.remove(storageKey) }
}

/**
 * RecordingTextCorrectionDraft keeps the exact request of a text correction through an unknown outcome: the client
 * correction ID and the text stay as they were sent, so sending again is the same correction and not a second one.
 */
@Serializable
data class RecordingTextCorrectionDraft(
    val accountKey: String,
    val recordingId: String,
    val recordingTranscriptSegmentId: String,
    val clientCorrectionId: String,
    val correctedTranscriptText: String,
    val expectedSourceGeneration: Long,
    /** The request was sent, and whether the server took it is not known. The text cannot change before it is sent again. */
    val hasSubmittedRequest: Boolean = false,
) {
    companion object {
        fun start(accountKey: String, recordingId: String, recordingTranscriptSegmentId: String, correctedTranscriptText: String, expectedSourceGeneration: Long) =
            RecordingTextCorrectionDraft(accountKey, recordingId, recordingTranscriptSegmentId, UUID.randomUUID().toString(), correctedTranscriptText, expectedSourceGeneration)
    }
}

/** The text correction drafts of one phone, one per passage and account. */
class RecordingTextCorrectionDrafts(private val storage: RecordingDraftStorage) {
    fun read(accountKey: String, segmentId: String): RecordingTextCorrectionDraft? {
        val encoded = storage.read(storageKey(accountKey, segmentId)) ?: return null
        val draft = try { TeaNodeJson.decodeFromString<RecordingTextCorrectionDraft>(encoded) } catch (_: Exception) { return null }
        return if (draft.accountKey == accountKey && draft.recordingTranscriptSegmentId == segmentId) draft else null
    }

    fun save(draft: RecordingTextCorrectionDraft) {
        storage.write(storageKey(draft.accountKey, draft.recordingTranscriptSegmentId), TeaNodeJson.encodeToString(draft))
    }

    /** Drops the draft, unless a newer one for the same passage has taken its place. */
    fun finish(draft: RecordingTextCorrectionDraft) {
        if (read(draft.accountKey, draft.recordingTranscriptSegmentId)?.clientCorrectionId == draft.clientCorrectionId) {
            storage.remove(storageKey(draft.accountKey, draft.recordingTranscriptSegmentId))
        }
    }

    companion object {
        fun storageKey(accountKey: String, segmentId: String): String {
            val encoder = Base64.getEncoder()
            return "recording.text-correction." + encoder.encodeToString(accountKey.toByteArray()) + "." + encoder.encodeToString(segmentId.toByteArray())
        }
    }
}

/** What opening the editor of a passage found. */
sealed class RecordingTextCorrectionStart {
    /** A request that had no answer was in fact saved: there is nothing left to retry. */
    data object AlreadySaved : RecordingTextCorrectionStart()

    /** The draft to edit, or to send again when [RecordingTextCorrectionDraft.hasSubmittedRequest] is set. */
    data class Editing(val draft: RecordingTextCorrectionDraft) : RecordingTextCorrectionStart()
}

/** What sending a kept draft came to. */
sealed class RecordingDraftOutcome<Draft> {
    class Saved<Draft> : RecordingDraftOutcome<Draft>()

    /** The server refused it, so nothing was saved and the draft may be edited again. */
    data class Refused<Draft>(val draft: Draft, val error: TeaNodeError.Refused) : RecordingDraftOutcome<Draft>()

    /** No answer was read: it may or may not be saved, so only sending the same one again is safe. */
    data class Unconfirmed<Draft>(val draft: Draft, val error: Throwable) : RecordingDraftOutcome<Draft>()
}

/** Sends the draft's own request, and reads the answer only if it is the same correction. */
suspend fun Connection.correctRecordingTranscriptText(draft: RecordingTextCorrectionDraft): RecordingTranscriptTextCorrection {
    if (accountKey != draft.accountKey) throw TeaNodeError.SignedOut
    val correction = correctRecordingTranscriptText(
        draft.recordingId, draft.recordingTranscriptSegmentId, draft.expectedSourceGeneration, draft.clientCorrectionId, draft.correctedTranscriptText,
    )
    if (correction.recordingId != draft.recordingId || correction.recordingTranscriptSegmentId != draft.recordingTranscriptSegmentId ||
        correction.clientCorrectionId != draft.clientCorrectionId || correction.correctedTranscriptText != draft.correctedTranscriptText
    ) {
        throw TeaNodeError.UnreadableAnswer
    }
    return correction
}

/**
 * Opens the draft of a passage: the one kept from before, or a new one from the passage's text. A kept draft that was sent
 * without an answer is looked for in the correction history first, since the server may have saved it.
 */
suspend fun Connection.startRecordingTextCorrection(
    drafts: RecordingTextCorrectionDrafts,
    recordingId: String,
    segmentId: String,
    displayedTranscriptText: String,
): RecordingTextCorrectionStart {
    val sourceGeneration = recording(recordingId).recording.sourceGeneration
    var pending = drafts.read(accountKey, segmentId)
    if (pending != null && pending.hasSubmittedRequest) {
        val saved = recordingTextCorrections(recordingId).any { correction ->
            correction.clientCorrectionId == pending!!.clientCorrectionId && correction.recordingTranscriptSegmentId == pending!!.recordingTranscriptSegmentId &&
                correction.correctedTranscriptText == pending!!.correctedTranscriptText
        }
        if (saved) {
            drafts.finish(pending)
            return RecordingTextCorrectionStart.AlreadySaved
        }
    }
    pending = when {
        pending == null -> RecordingTextCorrectionDraft.start(accountKey, recordingId, segmentId, displayedTranscriptText, sourceGeneration)
        !pending.hasSubmittedRequest -> pending.copy(expectedSourceGeneration = sourceGeneration)
        else -> pending
    }
    drafts.save(pending)
    return RecordingTextCorrectionStart.Editing(pending)
}

/**
 * Sends the draft. It is kept as sent before the call, so a call that gets no answer can be repeated as the same
 * correction; a refusal, which says it was not saved, frees the text to be edited with a fresh source generation.
 */
suspend fun Connection.submitRecordingTextCorrection(
    drafts: RecordingTextCorrectionDrafts,
    draft: RecordingTextCorrectionDraft,
    editedText: String,
): RecordingDraftOutcome<RecordingTextCorrectionDraft> {
    val sent = (if (draft.hasSubmittedRequest) draft else draft.copy(correctedTranscriptText = editedText)).copy(hasSubmittedRequest = true)
    drafts.save(sent)
    return try {
        correctRecordingTranscriptText(sent)
        drafts.finish(sent)
        RecordingDraftOutcome.Saved()
    } catch (error: TeaNodeError.Refused) {
        val refreshedGeneration = try { recording(sent.recordingId).recording.sourceGeneration } catch (_: Exception) { sent.expectedSourceGeneration }
        val reopened = sent.copy(hasSubmittedRequest = false, expectedSourceGeneration = refreshedGeneration)
        drafts.save(reopened)
        RecordingDraftOutcome.Refused(reopened, error)
    } catch (error: kotlinx.coroutines.CancellationException) {
        throw error
    } catch (error: Throwable) {
        RecordingDraftOutcome.Unconfirmed(sent, error)
    }
}

/**
 * RecordingSpeakerDraft keeps the exact request that names a passage's speaker through an unknown outcome, as the text
 * correction draft does.
 */
@Serializable
data class RecordingSpeakerDraft(
    val accountKey: String,
    val recordingId: String,
    val recordingTranscriptSegmentId: String,
    val recordingTranscriptionAttemptId: String,
    val localSpeakerId: String,
    val segmentStartMS: Long,
    val segmentEndMS: Long,
    val clientCorrectionId: String,
    val expectedSourceGeneration: Long,
    val confirmedName: String = "",
    val isWholeGroup: Boolean = false,
    val passageStartMS: Long = segmentStartMS,
    val passageEndMS: Long = segmentEndMS,
    val hasSubmittedRequest: Boolean = false,
    /** The person said the speaker is them: the name may then be empty and the server uses the account's own. */
    val isAccountOwner: Boolean? = null,
    /** The contact the name was chosen from, which the naming ties the speaker to. */
    val contactAddressBookId: String? = null,
    val contactId: String? = null,
) {
    companion object {
        fun start(accountKey: String, recordingId: String, segment: RecordingTranscriptSegment, expectedSourceGeneration: Long) = RecordingSpeakerDraft(
            accountKey, recordingId, segment.id, segment.recordingTranscriptionAttemptId, segment.localSpeakerId, segment.segmentStartMS, segment.segmentEndMS,
            UUID.randomUUID().toString(), expectedSourceGeneration,
        )
    }
}

/** The speaker naming drafts of one phone, one per passage and account. */
class RecordingSpeakerDrafts(private val storage: RecordingDraftStorage) {
    fun read(accountKey: String, segmentId: String): RecordingSpeakerDraft? {
        val encoded = storage.read(storageKey(accountKey, segmentId)) ?: return null
        val draft = try { TeaNodeJson.decodeFromString<RecordingSpeakerDraft>(encoded) } catch (_: Exception) { return null }
        return if (draft.accountKey == accountKey && draft.recordingTranscriptSegmentId == segmentId) draft else null
    }

    fun save(draft: RecordingSpeakerDraft) {
        storage.write(storageKey(draft.accountKey, draft.recordingTranscriptSegmentId), TeaNodeJson.encodeToString(draft))
    }

    fun finish(draft: RecordingSpeakerDraft) {
        if (read(draft.accountKey, draft.recordingTranscriptSegmentId)?.clientCorrectionId == draft.clientCorrectionId) {
            storage.remove(storageKey(draft.accountKey, draft.recordingTranscriptSegmentId))
        }
    }

    /**
     * The naming kept for a passage, settled first if it was sent without an answer from what the recording now holds:
     * saved, it is done and nothing is kept; not saved, it was rolled back, and it opens as a form again rather than locked
     * on a request that can only be refused for its old generation.
     */
    fun settled(accountKey: String, segmentId: String, speakers: RecordingSpeakersView?): RecordingSpeakerDraft? {
        val retained = read(accountKey, segmentId) ?: return null
        if (!retained.hasSubmittedRequest || speakers == null) return retained
        if (speakers.speakerCorrections.any { correction -> correction.clientCorrectionId == retained.clientCorrectionId && !correction.isRemoved }) {
            finish(retained)
            return null
        }
        val reopened = retained.copy(hasSubmittedRequest = false, expectedSourceGeneration = speakers.sourceGeneration)
        save(reopened)
        return reopened
    }

    companion object {
        fun storageKey(accountKey: String, segmentId: String): String {
            val encoder = Base64.getEncoder()
            return "recording.speaker-correction." + encoder.encodeToString(accountKey.toByteArray()) + "." + encoder.encodeToString(segmentId.toByteArray())
        }
    }
}

/**
 * Sends the naming. As the draft is kept as sent before the call, a call with no answer can be repeated as the same
 * correction; a refusal frees the form, with the recording's generation read again.
 */
suspend fun Connection.submitRecordingSpeakerNaming(drafts: RecordingSpeakerDrafts, draft: RecordingSpeakerDraft): RecordingDraftOutcome<RecordingSpeakerDraft> {
    if (accountKey != draft.accountKey) return RecordingDraftOutcome.Unconfirmed(draft, TeaNodeError.SignedOut)
    val sent = draft.copy(hasSubmittedRequest = true)
    drafts.save(sent)
    return try {
        correctRecordingSpeaker(
            sent.recordingId, sent.expectedSourceGeneration, sent.clientCorrectionId, sent.confirmedName, sent.isAccountOwner, sent.contactAddressBookId, sent.contactId,
            sent.recordingTranscriptionAttemptId, sent.localSpeakerId, if (sent.isWholeGroup) 0L else sent.passageStartMS, if (sent.isWholeGroup) Long.MAX_VALUE / 4 else sent.passageEndMS,
        )
        drafts.finish(sent)
        RecordingDraftOutcome.Saved()
    } catch (error: TeaNodeError.Refused) {
        val refreshedGeneration = try { recordingSpeakers(sent.recordingId).sourceGeneration } catch (_: Exception) { sent.expectedSourceGeneration }
        val reopened = sent.copy(hasSubmittedRequest = false, expectedSourceGeneration = refreshedGeneration)
        drafts.save(reopened)
        RecordingDraftOutcome.Refused(reopened, error)
    } catch (error: kotlinx.coroutines.CancellationException) {
        throw error
    } catch (error: Throwable) {
        RecordingDraftOutcome.Unconfirmed(sent, error)
    }
}
