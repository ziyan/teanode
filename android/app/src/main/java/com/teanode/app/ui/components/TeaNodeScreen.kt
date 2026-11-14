package com.teanode.app.ui.components

import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.RowScope
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.Menu
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import com.teanode.app.R

/** DrawerController opens the navigation drawer. The shell provides one through [LocalDrawer]; a screen outside the shell has none. */
class DrawerController(val open: () -> Unit)

val LocalDrawer = staticCompositionLocalOf<DrawerController?> { null }

/**
 * TeaNodeScreen is the frame of every screen: a top app bar with the title, a way back (or, on the first screen of a
 * part, the button that opens the drawer), the screen's actions, and the app-wide snackbars. The bar slides away as a
 * list scrolls and comes back when it scrolls the other way.
 *
 * @param onBack goes back; give it on every screen but a part's first, which shows the drawer button instead.
 * @param actions icon buttons and menus at the end of the bar.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TeaNodeScreen(
    title: String,
    modifier: Modifier = Modifier,
    onBack: (() -> Unit)? = null,
    actions: @Composable RowScope.() -> Unit = {},
    floatingActionButton: @Composable () -> Unit = {},
    bottomBar: @Composable () -> Unit = {},
    content: @Composable (contentPadding: PaddingValues) -> Unit,
) {
    val scrollBehavior = TopAppBarDefaults.enterAlwaysScrollBehavior()
    val drawer = LocalDrawer.current
    Scaffold(
        modifier = modifier.nestedScroll(scrollBehavior.nestedScrollConnection),
        topBar = {
            TopAppBar(
                title = { Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                navigationIcon = {
                    if (onBack != null) {
                        IconButton(onClick = onBack) {
                            Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.common_back))
                        }
                    } else if (drawer != null) {
                        IconButton(onClick = drawer.open) {
                            Icon(Icons.Outlined.Menu, contentDescription = stringResource(R.string.common_open_menu))
                        }
                    }
                },
                actions = actions,
                scrollBehavior = scrollBehavior,
            )
        },
        snackbarHost = { TeaNodeSnackbarHost() },
        floatingActionButton = floatingActionButton,
        bottomBar = bottomBar,
        content = content,
    )
}
