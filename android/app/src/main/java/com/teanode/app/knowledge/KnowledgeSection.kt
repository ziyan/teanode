package com.teanode.app.knowledge

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import com.teanode.app.search.TakePartSearch
import com.teanode.kit.search.SearchArea

/**
 * The first screen of Knowledge: the top folder of the pages the agent keeps, with what other people shared under it
 * (ios KnowledgeViews.swift, KnowledgeScreen).
 */
@Composable
fun KnowledgeSection(initialSearch: String = "") {
    // The words the Search screen's Show all carries.
    var askedSearch by remember { mutableStateOf(initialSearch) }
    TakePartSearch(SearchArea.Memory) { search -> askedSearch = search.words }
    KnowledgeFolderScreen(folderPath = "", initialSearch = askedSearch)
}
