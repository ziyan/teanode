package com.teanode.app.session

import androidx.compose.runtime.staticCompositionLocalOf
import com.teanode.kit.Connection

/** The app's account and what it knows about the server. Available everywhere inside `AppRoot` and `DemoHost`. */
val LocalAppSession = staticCompositionLocalOf<AppSession> { error("no AppSession: draw the screen inside AppRoot or DemoHost") }

/**
 * The connection of the account in use. Every screen inside the signed-in shell has one; ask for it with
 * `LocalConnection.current`.
 */
val LocalConnection = staticCompositionLocalOf<Connection> { error("no Connection: this screen is drawn outside the signed-in app") }

/** The same connection where there may be none, such as an avatar drawn on the sign-in screen. */
val LocalConnectionOrNull = staticCompositionLocalOf<Connection?> { null }
