package com.teanode.app.ui.components

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import com.teanode.app.session.ErrorWords
import com.teanode.app.session.LocalAppSession
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * LoadState is one read of the server a screen shows: nothing yet, loading, the value, or why it failed. A refresh
 * keeps the old value on screen while the new one comes; a refresh that fails tells the person in a snackbar and keeps
 * what was there, since stale rows are better than none.
 */
@Stable
class LoadState<Value>(private val scope: CoroutineScope, private val load: suspend () -> Value) {
    /** The last value read; null until the first read succeeds. */
    var value: Value? by mutableStateOf(null)
        private set

    /** Why the last read failed; null after a read that succeeded. */
    var error: Throwable? by mutableStateOf(null)
        private set

    /** Whether the first read (or a retry after a failure) is on its way. */
    var isLoading: Boolean by mutableStateOf(false)
        private set

    /** Whether a pull to refresh is on its way. */
    var isRefreshing: Boolean by mutableStateOf(false)
        private set

    /** The failure of a refresh that kept the old value, until a screen has told the person. */
    var refreshError: Throwable? by mutableStateOf(null)
        private set

    private var job: Job? = null

    internal fun start() {
        if (value == null && !isLoading) retry()
    }

    /** Reads again from the start, showing the loading state: for "Try again". */
    fun retry() {
        run(isRefresh = false)
    }

    /** Reads again with the old value still shown: for a pull to refresh, or after the person changed something. */
    fun refresh() {
        run(isRefresh = value != null)
    }

    /** Replaces the value without asking the server, for a change the screen has just made. */
    fun update(change: (Value) -> Value) {
        value = value?.let(change)
    }

    fun clearRefreshError() {
        refreshError = null
    }

    private fun run(isRefresh: Boolean) {
        job?.cancel()
        if (isRefresh) isRefreshing = true else isLoading = true
        error = null
        job = scope.launch {
            try {
                value = load()
                refreshError = null
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                if (isRefresh) refreshError = failure else error = failure
            } finally {
                isLoading = false
                isRefreshing = false
            }
        }
    }
}

/**
 * Reads from the server when the screen opens, and again when a key changes. Show it with [LoadedContent].
 *
 *     val goals = rememberLoad { connection.execute<GoalsAnswer>(Documents.GOALS) }
 *     LoadedContent(goals, isEmpty = { it.isEmpty() }, empty = { EmptyState(...) }) { list -> ... }
 */
@Composable
fun <Value> rememberLoad(vararg keys: Any?, load: suspend () -> Value): LoadState<Value> {
    val scope = rememberCoroutineScope()
    val latestLoad by rememberUpdatedState(load)
    val state = remember(*keys) { LoadState(scope) { latestLoad() } }
    LaunchedEffect(state) { state.start() }
    return state
}

/**
 * LoadedContent draws a [LoadState]: the loading state, the error state with "Try again", the empty state, or the
 * content inside a pull to refresh. A refresh that failed shows in the snackbar over the old content.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun <Value> LoadedContent(
    state: LoadState<Value>,
    modifier: Modifier = Modifier,
    isEmpty: (Value) -> Boolean = { false },
    empty: @Composable () -> Unit = {},
    content: @Composable (Value) -> Unit,
) {
    val session = LocalAppSession.current
    val snackbar = LocalSnackbar.current
    val refreshError = state.refreshError
    LaunchedEffect(refreshError) {
        if (refreshError != null) {
            state.clearRefreshError()
            if (!ErrorWords.isCancellation(refreshError)) snackbar.showProblem(session.describeLoadProblem(refreshError))
        }
    }
    val value = state.value
    val error = state.error
    when {
        value != null -> PullToRefreshBox(isRefreshing = state.isRefreshing, onRefresh = { state.refresh() }, modifier = modifier.fillMaxSize()) {
            if (isEmpty(value)) empty() else content(value)
        }
        error != null -> ErrorState(session.describeLoadProblem(error), onRetry = { state.retry() }, modifier = modifier)
        else -> LoadingState(modifier)
    }
}
