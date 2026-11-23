package com.teanode.app.ui.components

import android.content.ActivityNotFoundException
import android.content.Context
import android.net.Uri
import androidx.browser.customtabs.CustomTabsIntent

/**
 * Opens a web page in the phone's browser as a tab over the app (Custom Tabs), so the person does not leave it. The
 * iPhone app does the same with its in-app browser. A phone with no browser does nothing.
 */
fun openWebPage(context: Context, address: String, isEphemeral: Boolean = false) {
    val intent = CustomTabsIntent.Builder().setShowTitle(true).setEphemeralBrowsingEnabled(isEphemeral).build()
    try {
        intent.launchUrl(context, Uri.parse(address))
    } catch (_: ActivityNotFoundException) {
        // No browser to open it in.
    }
}
