package com.teanode.app.knowledge

import androidx.compose.runtime.Composable

/**
 * The first screen of Knowledge: the top folder of the pages the agent keeps, with what other people shared under it
 * (ios KnowledgeViews.swift, KnowledgeScreen).
 */
@Composable
fun KnowledgeSection(initialSearch: String = "") {
    KnowledgeFolderScreen(folderPath = "", initialSearch = initialSearch)
}
