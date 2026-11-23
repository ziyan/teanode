package com.teanode.app.session

import android.content.Context
import android.net.Uri
import com.teanode.app.passwords.AndroidPasswordsKeyStore
import com.teanode.app.passwords.PasswordsAutofillPointer
import com.teanode.kit.Account
import com.teanode.kit.SecretStore
import com.teanode.kit.health.HealthSyncState
import com.teanode.kit.location.LocationOutbox
import com.teanode.kit.notifications.PayloadKeyring
import java.io.File
import java.security.KeyStore

/**
 * AccountEraser removes what the phone kept for an account when it signs out of it, so the next person to hold the phone
 * finds none of it: the Passwords copy, its pointer for autofill and the Keystore key behind the fingerprint, the
 * notification key, the photo backup's records and password, the Health and location files, local recordings, unsent
 * drafts and every preference entry of the account, and the pictures and files in the cache.
 *
 * When it was the last account, everything the app keeps in its private files and preferences goes, except the look
 * of the app (appearance and tab bar).
 */
class AccountEraser(
    private val context: Context,
    private val secrets: SecretStore,
    private val stopLocation: (accountKey: String) -> Unit = { accountKey -> com.teanode.app.location.recording.LocationRecorder.get(context).stopFollowing(accountKey) },
) {
    fun erase(account: Account, isLastAccount: Boolean) {
        val accountKey = account.accountKey
        // The location service and the geofences go first, so nothing writes the account's files again after they are erased.
        runCatching { stopLocation(accountKey) }
        // Passwords: the sealed copy, the autofill pointer when it points here, and the key that opens it with the fingerprint.
        File(context.filesDir, "passwords/" + Uri.encode(accountKey)).deleteRecursively()
        if (PasswordsAutofillPointer.read(context) == accountKey) File(context.filesDir, "passwords/active").delete()
        runCatching { AndroidPasswordsKeyStore(context, accountKey).delete() }
        // Notification key and the photo backup's sealed password.
        runCatching { PayloadKeyring(secrets).forget(accountKey) }
        runCatching { secrets.delete("photoBackupCredential|$accountKey") }
        File(context.filesDir, "photo-backup-" + accountKey.hashCode().toUInt().toString(16) + ".json").delete()
        File(context.filesDir, "location/" + LocationOutbox.fileName(accountKey)).delete()
        File(context.filesDir, "health/" + HealthSyncState.fileName(accountKey)).delete()
        eraseLocalRecordings(accountKey)
        erasePreferenceEntries(accountKey, account.username)
        eraseCache()
        if (isLastAccount) eraseEverything()
    }

    private fun eraseLocalRecordings(accountKey: String) {
        val directory = File(context.filesDir, "recordings")
        for (folder in directory.listFiles().orEmpty()) {
            val manifest = File(folder, "manifest.json")
            val owner = runCatching { com.teanode.kit.TeaNodeJson.decodeFromString(com.teanode.kit.recordings.LocalRecording.serializer(), manifest.readText()).accountKey }.getOrNull()
            if (owner == accountKey) folder.deleteRecursively()
        }
    }

    /** Preference entries are named after the account's key, or after its user name (`mail-pending-send.<user name>`). */
    private fun erasePreferenceEntries(accountKey: String, username: String) {
        for (name in preferenceNames()) {
            val preferences = context.getSharedPreferences(name, Context.MODE_PRIVATE)
            val doomed = preferences.all.keys.filter { key -> key.contains(accountKey) || (username.isNotEmpty() && key.endsWith(".$username")) }
            if (doomed.isNotEmpty()) preferences.edit().apply { doomed.forEach { key -> remove(key) } }.commit()
        }
    }

    private fun eraseCache() {
        File(context.cacheDir, "chat-files").deleteRecursively()
        context.cacheDir.listFiles { file -> file.name.startsWith("Voice message") }.orEmpty().forEach { file -> file.delete() }
    }

    private fun eraseEverything() {
        for (name in preferenceNames()) {
            if (name == OWN_INTENT_PREFERENCES) continue
            val preferences = context.getSharedPreferences(name, Context.MODE_PRIVATE)
            val editor = preferences.edit()
            for (key in preferences.all.keys) if (name != APP_PREFERENCES || key !in LOOK_KEYS) editor.remove(key)
            editor.commit()
        }
        for (entry in context.filesDir.listFiles().orEmpty()) if (entry.name != "secrets") entry.deleteRecursively()
        runCatching {
            val keyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
            keyStore.aliases().toList().filter { alias -> alias.startsWith("teanode.passwords.") }.forEach { alias -> keyStore.deleteEntry(alias) }
        }
    }

    private fun preferenceNames(): List<String> =
        File(context.applicationInfo.dataDir, "shared_prefs").listFiles().orEmpty().filter { file -> file.name.endsWith(".xml") }.map { file -> file.name.removeSuffix(".xml") }

    private companion object {
        const val APP_PREFERENCES = "teanode"
        const val OWN_INTENT_PREFERENCES = "teanode-own-intent"
        // The keys of AppSession's appearance and tab bar choices.
        val LOOK_KEYS = setOf("appearance", com.teanode.kit.TabBarChoice.STORAGE_KEY)
    }
}
