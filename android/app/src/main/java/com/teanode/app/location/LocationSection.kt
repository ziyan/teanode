package com.teanode.app.location

import androidx.compose.runtime.Composable
import com.teanode.app.shell.SectionPlaceholder
import com.teanode.kit.AppSection

/**
 * The first screen of Location. Replace the placeholder with the real screen; take the connection from
 * `LocalConnection.current`, tell the person things with `LocalSnackbar.current`, and draw inside `TeaNodeScreen`.
 */
@Composable
fun LocationSection() {
    SectionPlaceholder(AppSection.Location)
}
