package com.teanode.app.chats

import android.net.Uri
import com.teanode.app.finance.FinanceRoutes
import com.teanode.app.knowledge.KnowledgeRoutes
import com.teanode.app.mail.MailRoutes
import com.teanode.app.search.PartSearch
import com.teanode.app.search.PartSearchRequest
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection
import com.teanode.kit.chats.AgentReference
import com.teanode.kit.search.SearchArea

/** Whether [openReference] has somewhere to take the reference: a thread named without one of its messages has not. */
fun canOpenReference(reference: AgentReference): Boolean =
    reference.financeTransactionId != null || reference.recordingId != null || reference.photoId != null || reference.path != null || reference.itemId != null

/**
 * Opens what a chip above a sent message names, where the app keeps it: a finance transaction, a recording, a photo
 * (in the library's viewer), a page of memory, a mail.
 */
fun openReference(navigator: AppNavigator, reference: AgentReference) {
    val financeTransactionId = reference.financeTransactionId
    val recordingId = reference.recordingId
    val photoId = reference.photoId
    val path = reference.path
    val itemId = reference.itemId
    when {
        financeTransactionId != null -> navigator.openInSection(AppSection.Finance, FinanceRoutes.transaction(financeTransactionId))
        recordingId != null -> navigator.openInSection(AppSection.Recordings, Routes.child(AppSection.Recordings, "detail/${Uri.encode(recordingId)}"))
        photoId != null -> {
            PartSearchRequest.ask(PartSearch(SearchArea.Photos, "", photoId))
            navigator.openSection(AppSection.Photos)
        }
        path != null -> navigator.openInSection(AppSection.Knowledge, KnowledgeRoutes.page(path))
        itemId != null -> navigator.openInSection(AppSection.Mail, MailRoutes.thread(itemId, null))
    }
}
