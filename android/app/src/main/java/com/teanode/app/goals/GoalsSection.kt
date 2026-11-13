package com.teanode.app.goals

import androidx.compose.runtime.Composable
import com.teanode.app.shell.SectionPlaceholder
import com.teanode.kit.AppSection

/**
 * The first screen of Goals. Replace the placeholder with the real screen; take the connection from
 * `LocalConnection.current`, tell the person things with `LocalSnackbar.current`, and draw inside `TeaNodeScreen`.
 */
@Composable
fun GoalsSection() {
    SectionPlaceholder(AppSection.Goals)
}
