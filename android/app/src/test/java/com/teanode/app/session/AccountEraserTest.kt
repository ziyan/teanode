package com.teanode.app.session

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import com.teanode.app.passwords.PasswordsAutofillPointer
import com.teanode.kit.Account
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.health.HealthSyncState
import com.teanode.kit.location.LocationOutbox
import com.teanode.kit.notifications.PayloadKeyring
import com.teanode.kit.recordings.LocalRecording
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.File

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class AccountEraserTest {
    private val context: Context = ApplicationProvider.getApplicationContext()
    private val secrets = MemorySecretStore()
    private val stopped = mutableListOf<String>()
    private val alex = Account(ServerAddress.parse("https://mail.example.com"), "client", "access", "refresh", 0, "alex")
    private val sam = Account(ServerAddress.parse("https://tea.example.org"), "client", "access", "refresh", 0, "sam")

    private fun write(path: String, text: String = "x") = File(context.filesDir, path).also { file -> file.parentFile?.mkdirs(); file.writeText(text) }

    private fun fill(account: Account) {
        val key = account.accountKey
        write("passwords/" + android.net.Uri.encode(key) + "/passwords.json")
        write("photo-backup-" + key.hashCode().toUInt().toString(16) + ".json")
        write("location/" + LocationOutbox.fileName(key))
        write("health/" + HealthSyncState.fileName(key))
        val recording = LocalRecording("rec-" + account.username, key, null, recordingTitle = "t", recordingOrigin = "capture", startedAtEpochMS = 0)
        write("recordings/rec-${account.username}/manifest.json", TeaNodeJson.encodeToString(LocalRecording.serializer(), recording))
        write("recordings/rec-${account.username}/m1.original")
        PayloadKeyring(secrets).current(key)
        secrets.write("photoBackupCredential|$key", "secret".toByteArray())
        context.getSharedPreferences("teanode-chats", Context.MODE_PRIVATE).edit().putString("draft|$key|main", "unsent").commit()
        context.getSharedPreferences("mail", Context.MODE_PRIVATE).edit().putString("mail-pending-send.${account.username}", "{}").commit()
    }

    private fun existsFor(account: Account): Boolean = listOf(
        "passwords/" + android.net.Uri.encode(account.accountKey), "photo-backup-" + account.accountKey.hashCode().toUInt().toString(16) + ".json",
        "location/" + LocationOutbox.fileName(account.accountKey), "health/" + HealthSyncState.fileName(account.accountKey), "recordings/rec-" + account.username,
    ).any { path -> File(context.filesDir, path).exists() }

    @Test fun `signing out of one account erases its files, preferences and secrets and leaves the other's`() {
        fill(alex)
        fill(sam)
        PasswordsAutofillPointer.write(context, alex.accountKey)
        File(context.cacheDir, "chat-files").mkdirs()
        File(context.cacheDir, "chat-files/camera.jpg").writeText("x")

        AccountEraser(context, secrets, stopLocation = { stopped.add(it) }).erase(alex, isLastAccount = false)

        assertFalse(existsFor(alex))
        assertTrue(existsFor(sam))
        assertNull(PayloadKeyring(secrets).stored(alex.accountKey))
        assertNotNull(PayloadKeyring(secrets).stored(sam.accountKey))
        assertNull(secrets.read("photoBackupCredential|${alex.accountKey}"))
        assertNotNull(secrets.read("photoBackupCredential|${sam.accountKey}"))
        assertFalse(File(context.filesDir, "passwords/active").exists())
        assertFalse(File(context.cacheDir, "chat-files").exists())
        assertFalse(context.getSharedPreferences("teanode-chats", Context.MODE_PRIVATE).contains("draft|${alex.accountKey}|main"))
        assertTrue(context.getSharedPreferences("teanode-chats", Context.MODE_PRIVATE).contains("draft|${sam.accountKey}|main"))
        assertFalse(context.getSharedPreferences("mail", Context.MODE_PRIVATE).contains("mail-pending-send.alex"))
        assertTrue(context.getSharedPreferences("mail", Context.MODE_PRIVATE).contains("mail-pending-send.sam"))
    }

    @Test fun `the location service and geofences of the account are stopped`() {
        AccountEraser(context, secrets, stopLocation = { stopped.add(it) }).erase(alex, isLastAccount = false)
        assertEquals(listOf(alex.accountKey), stopped)
    }

    @Test fun `the pointer of another account stays`() {
        PasswordsAutofillPointer.write(context, sam.accountKey)
        AccountEraser(context, secrets, stopLocation = { stopped.add(it) }).erase(alex, isLastAccount = false)
        assertEquals(sam.accountKey, PasswordsAutofillPointer.read(context))
    }

    @Test fun `signing out of the last account erases everything but the look of the app`() {
        fill(alex)
        write("unknown-future-file.json")
        context.getSharedPreferences("teanode", Context.MODE_PRIVATE).edit().putString("appearance", "dark").putString("drawerId", "d").commit()
        context.getSharedPreferences("calendar", Context.MODE_PRIVATE).edit().putStringSet("other-key", setOf("a")).commit()

        AccountEraser(context, secrets, stopLocation = { stopped.add(it) }).erase(alex, isLastAccount = true)

        assertEquals(emptyList<String>(), context.filesDir.listFiles().orEmpty().map { file -> file.name }.filter { name -> name != "secrets" })
        assertEquals("dark", context.getSharedPreferences("teanode", Context.MODE_PRIVATE).getString("appearance", null))
        assertFalse(context.getSharedPreferences("teanode", Context.MODE_PRIVATE).contains("drawerId"))
        assertTrue(context.getSharedPreferences("calendar", Context.MODE_PRIVATE).all.isEmpty())
    }
}
