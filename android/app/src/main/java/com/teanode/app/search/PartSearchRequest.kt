package com.teanode.app.search

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import com.teanode.kit.search.SearchArea
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * PartSearch is a search the Search screen asks a part to show: the words typed there, in the part of the area (ios
 * PartSearch and SearchedPhoto). [photoId] is a photo found, which Photos opens in its list of what the words found.
 */
data class PartSearch(val area: SearchArea, val words: String, val photoId: String? = null)

/**
 * PartSearchRequest holds a [PartSearch] until the part has taken it: the part puts the words in its own search field
 * as it does for a link, and the request is gone after that.
 */
object PartSearchRequest {
    private val pendingSearch = MutableStateFlow<PartSearch?>(null)

    /** The search to show, until the part that shows it has taken it with [consume]. */
    val pending: StateFlow<PartSearch?> = pendingSearch.asStateFlow()

    fun ask(search: PartSearch) {
        pendingSearch.value = search
    }

    fun consume() {
        pendingSearch.value = null
    }
}

/** Calls [onSearch] once with the search asked for the part of [area], as soon as the part is on screen. */
@Composable
fun TakePartSearch(area: SearchArea, onSearch: (PartSearch) -> Unit) {
    val request by PartSearchRequest.pending.collectAsState()
    LaunchedEffect(request) {
        request?.takeIf { search -> search.area == area }?.let { search ->
            onSearch(search)
            PartSearchRequest.consume()
        }
    }
}
