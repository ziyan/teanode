package com.teanode.app.screenshots

import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import com.teanode.app.demo.DemoHost
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.CountBadge
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeDivider
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.relativeTimeText
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of the shared components, so every area draws its lists, states and messages the same way. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class ComponentsScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private val now = 1_800_000_000_000L

    @Composable
    private fun RowsScreen() {
        TeaNodeScreen(
            title = "Rows and avatars",
            onBack = {},
            actions = { IconButton(onClick = {}) { Icon(Icons.Outlined.MoreVert, contentDescription = "More") } },
        ) { contentPadding ->
            LazyColumn(Modifier.padding(contentPadding)) {
                item { TeaNodeSectionHeader("People") }
                item {
                    TeaNodeRow(
                        headline = "Alex Example", supporting = "Thanks, see you at the meeting tomorrow morning.", overline = "alex@example.com",
                        leading = { AvatarView("Alex Example", null, null) },
                        trailing = { Text(relativeTimeText(now - 5 * 60_000L, now)) }, onClick = {},
                    )
                }
                item {
                    TeaNodeRow(
                        headline = "Sam Sample", supporting = "Two files attached", leading = { AvatarView("Sam Sample", null, 7) },
                        trailing = { CountBadge(3) }, onClick = {}, isHeadlineBold = true,
                    )
                }
                item { TeaNodeRow(headline = "中文名字", leading = { AvatarView("中文名字", null, 11) }, trailing = { Text(relativeTimeText(now - 30 * 3_600_000L, now)) }) }
                item { TeaNodeDivider() }
                item { TeaNodeSectionHeader("Places") }
                item { TeaNodeNavigationRow("Inbox", onClick = {}, leadingIcon = Icons.Outlined.Inbox, supporting = "12 unread") }
                item { TeaNodeNavigationRow("A folder with a name long enough that it has to wrap onto a second line", onClick = {}, leadingIcon = Icons.Outlined.Inbox) }
            }
        }
    }

    @Test fun `rows light`() {
        composeRule.setContent { DemoHost(appearance = Look.Light.appearance) { RowsScreen() } }
        composeRule.photograph("components-rows-light")
    }

    @Test fun `rows dark`() {
        composeRule.setContent { DemoHost(appearance = Look.Dark.appearance) { RowsScreen() } }
        composeRule.photograph("components-rows-dark")
    }

    @Test fun `rows at the largest text`() {
        composeRule.setContent { DemoHost(appearance = Look.Light.appearance) { WithFontScale(2f) { RowsScreen() } } }
        composeRule.photograph("components-rows-largest-text-light")
    }

    @Test fun `loading state`() {
        composeRule.setContent { DemoHost(appearance = Look.Light.appearance) { TeaNodeScreen("Loading", onBack = {}) { padding -> LoadingState(Modifier.padding(padding)) } } }
        composeRule.photograph("components-loading-light")
    }

    @Test fun `empty state`() {
        composeRule.setContent {
            DemoHost(appearance = Look.Light.appearance) {
                TeaNodeScreen("Inbox", onBack = {}) { padding ->
                    EmptyState(Icons.Outlined.Inbox, "Nothing here", Modifier.padding(padding), description = "Mail that arrives shows up here.", actionLabel = "Write mail", onAction = {})
                }
            }
        }
        composeRule.photograph("components-empty-light")
    }

    @Test fun `error state dark`() {
        composeRule.setContent {
            DemoHost(appearance = Look.Dark.appearance) {
                TeaNodeScreen("Inbox", onBack = {}) { padding -> ErrorState("Check your connection and try again.", onRetry = {}, modifier = Modifier.padding(padding)) }
            }
        }
        composeRule.photograph("components-error-dark")
    }

    @Test fun `problem snackbar`() {
        composeRule.setContent {
            DemoHost(appearance = Look.Light.appearance) {
                val snackbar = LocalSnackbar.current
                LaunchedEffect(Unit) { snackbar.showProblem("That file is larger than the server takes.") }
                RowsScreen()
            }
        }
        composeRule.waitForIdle()
        composeRule.photograph("components-snackbar-problem-light")
    }

    @Test fun `done snackbar with an action`() {
        composeRule.setContent {
            DemoHost(appearance = Look.Dark.appearance) {
                val snackbar = LocalSnackbar.current
                LaunchedEffect(Unit) { snackbar.show("Message archived", actionLabel = "Undo") {} }
                RowsScreen()
            }
        }
        composeRule.waitForIdle()
        composeRule.photograph("components-snackbar-done-dark")
    }

    @Test fun `confirmation dialog`() {
        composeRule.setContent {
            DemoHost(appearance = Look.Light.appearance) {
                RowsScreen()
                ConfirmationDialog(
                    title = "Delete Sam Sample?", text = "This cannot be undone.", confirmLabel = "Delete", isDestructive = true,
                    onConfirm = {}, onDismiss = {},
                )
            }
        }
        composeRule.waitForIdle()
        composeRule.photograph("components-confirmation-light")
    }
}
