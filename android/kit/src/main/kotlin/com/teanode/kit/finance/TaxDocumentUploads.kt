package com.teanode.kit.finance

/** How far a file being added as a paystub or a W-2 has come (ios FinanceTaxViews.swift TaxDocumentUpload). */
enum class TaxDocumentUploadPhase { Uploading, Reading, StillReading, Failed }

/** TaxDocumentUpload is a file being added as a paystub or a W-2: its name and how far it is. */
data class TaxDocumentUpload(
    val uploadId: Long,
    val fileName: String,
    val agentAttachmentId: String? = null,
    val uploadPhase: TaxDocumentUploadPhase = TaxDocumentUploadPhase.Uploading,
    /** Why it failed, when it did. */
    val failureText: String? = null,
) {
    /** Whether nothing more will happen to it here. */
    val isFinished: Boolean
        get() = uploadPhase == TaxDocumentUploadPhase.StillReading || uploadPhase == TaxDocumentUploadPhase.Failed
}

object TaxDocumentUploads {
    /** Two minutes at most, every four seconds, as a receipt is waited for. */
    const val WAIT_PASS_COUNT = 30
    const val WAIT_PASS_MS = 4_000L

    /** The files with the ones the server has read (their attachment is on a paystub or a tax form now) taken out. */
    fun withoutRead(uploads: List<TaxDocumentUpload>, readAttachmentIds: Set<String>): List<TaxDocumentUpload> =
        uploads.filter { upload -> upload.agentAttachmentId?.let { attachmentId -> attachmentId in readAttachmentIds } != true }

    /** The files still waiting after the last pass, said to be still reading. */
    fun stillReading(uploads: List<TaxDocumentUpload>, waitingAttachmentIds: Set<String>): List<TaxDocumentUpload> =
        uploads.map { upload ->
            if (upload.agentAttachmentId?.let { attachmentId -> attachmentId in waitingAttachmentIds } == true) upload.copy(uploadPhase = TaxDocumentUploadPhase.StillReading) else upload
        }

    /** The attachments the paystubs and the tax forms were read from. */
    fun readAttachmentIds(paystubs: List<FinancePaystub>, taxForms: List<FinanceTaxFormView>): Set<String> =
        (paystubs.mapNotNull { paystub -> paystub.agentAttachmentId } + taxForms.mapNotNull { view -> view.financeTaxForm?.agentAttachmentId }).toSet()
}
