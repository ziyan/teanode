package com.teanode.kit.chats

/** Where a file shared into the app can go, and which files fit each place (ios/TeaNodeShare). */
enum class ShareDestination { Agent, TaxDocuments, Statements, Recording }

object SharedFiles {
    /** The most one message carries (the server's maxAttachmentBytes, 25 MB unless the operator changed it). */
    const val BYTES_MOST = 25 shl 20

    private val statementExtensions = setOf("ofx", "qfx", "qbo")

    /** A paystub or a tax form is a PDF or a picture. */
    fun isTaxDocument(contentType: String): Boolean = contentType == "application/pdf" || contentType.startsWith("image/")

    /** A statement is an OFX file, by its name or by what it starts with. */
    fun isStatement(name: String, startOfFile: ByteArray): Boolean {
        if (name.substringAfterLast('.', "").lowercase() in statementExtensions) return true
        val start = String(startOfFile.copyOf(minOf(startOfFile.size, 1024)), Charsets.UTF_8).uppercase()
        return start.contains("OFXHEADER") || start.contains("<OFX>")
    }

    /** A recording is made from sound or video. */
    fun isRecordingMedia(contentType: String): Boolean = contentType.startsWith("audio/") || contentType.startsWith("video/")

    /** The destinations a set of files can go to, the agent always first. */
    fun destinations(files: List<Triple<String, String, ByteArray>>, canRecord: Boolean): List<ShareDestination> {
        val offered = mutableListOf(ShareDestination.Agent)
        if (files.any { (_, contentType, _) -> isTaxDocument(contentType) }) offered.add(ShareDestination.TaxDocuments)
        if (files.any { (name, _, data) -> isStatement(name, data) }) offered.add(ShareDestination.Statements)
        if (canRecord && files.any { (_, contentType, _) -> isRecordingMedia(contentType) }) offered.add(ShareDestination.Recording)
        return offered
    }

    /** What a failure partway through says: how many files went before it, so trying again is known to send only the rest. */
    fun partialFailure(reason: String, handledCount: Int, totalCount: Int, fileName: String): String =
        if (handledCount == 0) reason else "$handledCount of $totalCount files went; $fileName did not: $reason Send again for the rest."
}
