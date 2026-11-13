package com.teanode.app.ideas

import androidx.compose.runtime.Composable
import com.teanode.app.shell.SectionPlaceholder
import com.teanode.kit.AppSection

/**
 * The first screen of Ideas. Replace the placeholder with the real screen; take the connection from
 * `LocalConnection.current`, tell the person things with `LocalSnackbar.current`, and draw inside `TeaNodeScreen`.
 */
@Composable
fun IdeasSection() {
    SectionPlaceholder(AppSection.Ideas)
}
