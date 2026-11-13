package com.teanode.app.knowledge

import android.net.Uri
import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/** The places of Knowledge. A page's path and a document's id are passed in query arguments, encoded. */
object KnowledgeRoutes {
    val HOME: String = Routes.home(AppSection.Knowledge)
    val FOLDER: String = Routes.child(AppSection.Knowledge, "folder?path={path}")
    val PAGE: String = Routes.child(AppSection.Knowledge, "page?path={path}")
    val SHARED: String = Routes.child(AppSection.Knowledge, "shared?owner={owner}&path={path}")
    val RECALL: String = Routes.child(AppSection.Knowledge, "recall")
    val DOCUMENTS: String = Routes.child(AppSection.Knowledge, "documents")
    val DOCUMENT: String = Routes.child(AppSection.Knowledge, "document?id={id}")

    /** A folder of memory, such as `projects`. */
    fun folder(path: String): String = Routes.child(AppSection.Knowledge, "folder?path=${Uri.encode(path)}")

    /** The page of memory at a path, such as `people/ada`. */
    fun page(path: String): String = Routes.child(AppSection.Knowledge, "page?path=${Uri.encode(path)}")

    /** A page another person shared, read only. */
    fun shared(ownerUserId: String, path: String): String = Routes.child(AppSection.Knowledge, "shared?owner=${Uri.encode(ownerUserId)}&path=${Uri.encode(path)}")

    /** A document the agent's sources read, read a slice at a time. */
    fun document(documentId: String): String = Routes.child(AppSection.Knowledge, "document?id=${Uri.encode(documentId)}")
}

/** The routes of Knowledge. */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.knowledgeRoutes(navigator: AppNavigator) {
    composable(KnowledgeRoutes.HOME, deepLinks = Routes.deepLinks(KnowledgeRoutes.HOME)) { KnowledgeSection() }
    composable(KnowledgeRoutes.FOLDER) { entry -> KnowledgeFolderScreen(entry.arguments?.getString("path").orEmpty()) }
    composable(KnowledgeRoutes.PAGE, deepLinks = Routes.deepLinks(KnowledgeRoutes.PAGE)) { entry -> KnowledgePageScreen(entry.arguments?.getString("path").orEmpty()) }
    composable(KnowledgeRoutes.SHARED) { entry ->
        SharedKnowledgePageScreen(entry.arguments?.getString("owner").orEmpty(), entry.arguments?.getString("path").orEmpty())
    }
    composable(KnowledgeRoutes.RECALL) { RecallScreen() }
    composable(KnowledgeRoutes.DOCUMENTS) { DocumentsScreen() }
    composable(KnowledgeRoutes.DOCUMENT) { entry -> DocumentReaderScreen(entry.arguments?.getString("id").orEmpty()) }
}

/** What Knowledge plugs into the shell; listed in `Areas`. */
val knowledgeArea = Area(
    section = AppSection.Knowledge,
    routes = NavGraphBuilder::knowledgeRoutes,
    demoAnswerers = knowledgeDemoAnswerers,
)
