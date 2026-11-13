package com.teanode.app.notifications

import android.content.Context
import android.content.Intent
import com.teanode.kit.Pkce
import java.security.MessageDigest

/**
 * OwnIntentSeal marks the intents the app builds for its own notifications and widgets with a secret kept in its private
 * preferences. MainActivity can be started by any app, so the notification extras it carries out (which account to
 * switch to, which conversation to open) are honored only on an intent that bears the secret.
 */
object OwnIntentSeal {
    const val EXTRA_SEAL = "com.teanode.app.extra.OWN_INTENT_SEAL"
    private const val PREFERENCES_NAME = "teanode-own-intent"
    private const val SEAL_KEY = "seal"

    private fun seal(context: Context): String {
        val preferences = context.applicationContext.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE)
        preferences.getString(SEAL_KEY, null)?.let { kept -> return kept }
        return Pkce.randomText().also { made -> preferences.edit().putString(SEAL_KEY, made).apply() }
    }

    fun mark(context: Context, intent: Intent): Intent = intent.putExtra(EXTRA_SEAL, seal(context))

    fun isOwn(context: Context, intent: Intent): Boolean {
        val shown = intent.getStringExtra(EXTRA_SEAL) ?: return false
        return MessageDigest.isEqual(shown.toByteArray(), seal(context).toByteArray())
    }
}
