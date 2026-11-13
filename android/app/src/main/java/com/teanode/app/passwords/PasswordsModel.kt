package com.teanode.app.passwords

import android.content.ClipData
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.os.Build
import android.os.PersistableBundle
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import com.teanode.app.R
import com.teanode.app.session.AppSession
import com.teanode.app.ui.components.SnackbarController
import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import com.teanode.kit.accessSession
import com.teanode.kit.passwords.ConnectionPasswordsApi
import com.teanode.kit.passwords.FilePasswordsStorage
import com.teanode.kit.passwords.InMemoryPasswordsStorage
import com.teanode.kit.passwords.PasswordItem
import com.teanode.kit.passwords.PasswordItemVersion
import com.teanode.kit.passwords.PasswordsCache
import com.teanode.kit.passwords.PasswordsCrypto
import com.teanode.kit.passwords.PasswordsCryptoException
import com.teanode.kit.passwords.PasswordsCryptoFailure
import com.teanode.kit.passwords.PasswordsError
import com.teanode.kit.passwords.PasswordsItemFields
import com.teanode.kit.passwords.PasswordsOpenedVersions
import com.teanode.kit.passwords.PasswordsSession
import com.teanode.kit.passwords.PasswordsSharingCheck
import com.teanode.kit.passwords.folders
import java.util.WeakHashMap
import kotlinx.coroutines.CancellationException

/** Where Passwords is for the person on this phone. */
enum class PasswordsPhase {
    /** The first read of the setup has not finished. */
    Loading,

    /** The first read failed; [PasswordsModel.failedText] says why. */
    Failed,

    /** The person has not set Passwords up. */
    NotSetUp,

    /** Set up a moment ago: the recovery key is on screen until the person says they saved it. */
    RecoveryKey,

    /** Set up, and closed. A passphrase, the recovery key or the fingerprint opens it. */
    Locked,

    /** Open: the items are shown. */
    Unlocked,
}

/** What demo mode starts Passwords in: a screen's photograph names it. */
enum class PasswordsDemoStart { Unlocked, Locked, NotSetUp, LockedWithBiometry }

/**
 * PasswordsModel is Passwords on this phone: whether it is set up, locked or open, and the person's items while it is
 * open. It owns the session (the kit's PasswordsSession), which holds the account key in memory only while open, keeps the
 * encrypted copy of the items in the app's private storage (which the autofill service, in the same process, reads) and
 * joins both to the server. The account key can also wait behind the fingerprint in the Android Keystore, so the
 * passphrase is typed once.
 */
class PasswordsModel(
    val connection: Connection,
    private val context: Context,
    val isDemo: Boolean,
    private val keyStore: PasswordsKeyStore,
    storage: com.teanode.kit.passwords.PasswordsStorage,
    private val iterationCount: Int,
    private val demoStart: PasswordsDemoStart = PasswordsDemoStart.Unlocked,
) {
    var phase by mutableStateOf(PasswordsPhase.Loading)
        private set
    var failedText by mutableStateOf<String?>(null)
        private set

    /** The recovery key as the person writes it down, while [phase] is [PasswordsPhase.RecoveryKey]. */
    var recoveryKeyText by mutableStateOf<String?>(null)
        private set

    /** The items that are not deleted, by name. */
    var items by mutableStateOf<List<PasswordItem>>(emptyList())
        private set

    /** The items deleted in the last 30 days, the latest first. */
    var deletedItems by mutableStateOf<List<PasswordItem>>(emptyList())
        private set

    /** How many items could not be opened on this phone: damaged, or saved by a newer app. */
    var unreadableItemCount by mutableStateOf(0)
        private set

    /** The person's own safety code, made from the key pair this phone opened; null while locked. */
    var ownSafetyCode by mutableStateOf<String?>(null)
        private set
    var isSyncing by mutableStateOf(false)
        private set

    /** Whether the account key waits behind the fingerprint on this phone. */
    var hasBiometricKey by mutableStateOf(false)
        private set
    var lockAfterMinutes by mutableStateOf(PasswordsSession.DEFAULT_LOCK_AFTER_MINUTES)
        private set

    /** The earlier versions of the items whose list the person opened, by item id. Their fields are in the clear, so they stay in memory only. */
    val versionsByItemId = mutableStateMapOf<String, PasswordsOpenedVersions>()

    /** Where a message goes: the screen in front gives its snackbars. */
    var snackbar: SnackbarController? = null

    private var session: PasswordsSession? = null
    private var appSession: AppSession? = null
    private var passphraseOfSetUp: String? = null
    private var isStarting = false
    private var isOpeningWithBiometry = false
    private val cache = PasswordsCache(storage)
    private val preferences = context.getSharedPreferences("teanode-passwords", Context.MODE_PRIVATE)
    private val biometricDefaultsKey get() = "biometric." + connection.accountKey

    private fun text(resource: Int, vararg arguments: Any): String = context.getString(resource, *arguments)

    val biometryName: String get() = text(R.string.passwords_biometry_name)

    /** Whether this phone can keep the account key behind the fingerprint. */
    val canUseBiometry: Boolean get() = isDemo || keyStore.isBiometryAvailable

    /** The folders the items are in, by name. */
    val folders: List<String> get() = items.folders()

    /** An item of the open Passwords, deleted ones included. */
    fun item(id: String): PasswordItem? = items.firstOrNull { item -> item.id == id } ?: deletedItems.firstOrNull { item -> item.id == id }

    fun attach(snackbar: SnackbarController, appSession: AppSession) {
        this.snackbar = snackbar
        this.appSession = appSession
    }

    fun say(message: String) {
        snackbar?.show(message)
    }

    // Starting

    /**
     * Reads whether Passwords is set up, and opens it with the fingerprint when the account key waits behind it. Called
     * each time the screen appears.
     */
    suspend fun start() {
        if (isStarting) return
        isStarting = true
        try {
            val current = session
            if (current != null && phase != PasswordsPhase.Failed) {
                lockIfAskRequired()
                if (phase == PasswordsPhase.Locked && hasBiometricKey && !isDemo) unlockWithBiometry()
                return
            }
            phase = PasswordsPhase.Loading
            failedText = null
            try {
                val userId = connection.accessSession().userId
                if (userId.isNullOrEmpty()) throw PasswordsError.InvalidServerAnswer
                val newSession = PasswordsSession(ConnectionPasswordsApi(connection), cache, userId)
                val setup = newSession.readSetup()
                session = newSession
                lockAfterMinutes = setup.lockAfterMinutes
                hasBiometricKey = preferences.getBoolean(biometricDefaultsKey, false)
                // Without the choice on record, a key still kept is a leftover of an earlier install: drop it.
                if (!hasBiometricKey && !isDemo) keyStore.delete()
                phase = if (setup.isSetUp) PasswordsPhase.Locked else PasswordsPhase.NotSetUp
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                failedText = problemText(error, text(R.string.passwords_check_connection))
                phase = PasswordsPhase.Failed
                return
            }
            if (isDemo) {
                startDemo()
                return
            }
            if (phase == PasswordsPhase.Locked && hasBiometricKey) unlockWithBiometry()
        } finally {
            isStarting = false
        }
    }

    private suspend fun startDemo() {
        when (demoStart) {
            PasswordsDemoStart.Unlocked -> if (phase == PasswordsPhase.Locked) runCatching { unlock(PasswordsDemoStore.PASSPHRASE, false) }
            PasswordsDemoStart.LockedWithBiometry -> hasBiometricKey = true
            PasswordsDemoStart.Locked, PasswordsDemoStart.NotSetUp -> Unit
        }
    }

    private fun requireSession(): PasswordsSession = session ?: throw PasswordsError.Locked

    // Setting up

    /** Sets Passwords up with a passphrase. The recovery key comes back on the model ([recoveryKeyText], phase [PasswordsPhase.RecoveryKey]) and is shown once. */
    suspend fun setUp(passphrase: String) {
        val recoveryKey = requireSession().setUp(passphrase, iterationCount)
        passphraseOfSetUp = passphrase
        recoveryKeyText = recoveryKey
        phase = PasswordsPhase.RecoveryKey
    }

    /**
     * Ends the setup once the person says they saved the recovery key, and opens Passwords. With `shouldRemember` the
     * account key is also put behind the fingerprint, which needs the passphrase once more.
     */
    suspend fun finishSetUp(shouldRemember: Boolean) {
        if (phase != PasswordsPhase.RecoveryKey) return
        val current = session ?: return
        val passphrase = passphraseOfSetUp
        passphraseOfSetUp = null
        recoveryKeyText = null
        if (shouldRemember && passphrase != null) {
            runCatching { current.unlockWithPassphrase(passphrase) }.getOrNull()?.let { unlocked -> keep(unlocked.accountKey) }
        }
        say(text(R.string.passwords_set_up_and_open))
        opened()
    }

    // Unlocking

    /** Opens Passwords with the passphrase. With `shouldRemember` the account key is also kept behind the fingerprint on this phone. */
    suspend fun unlock(passphrase: String, shouldRemember: Boolean) {
        val unlocked = requireSession().unlockWithPassphrase(passphrase)
        if (shouldRemember) keep(unlocked.accountKey)
        opened()
    }

    /** Opens Passwords with the recovery key. */
    suspend fun unlockWithRecoveryKey(recoveryKey: String) {
        requireSession().unlockWithRecoveryKey(recoveryKey)
        say(text(R.string.passwords_opened_with_recovery_key))
        opened()
    }

    /** Opens Passwords with the account key kept behind the fingerprint. Nothing is said when the person cancels. */
    suspend fun unlockWithBiometry() {
        val current = session ?: return
        if (phase != PasswordsPhase.Locked || isOpeningWithBiometry) return
        isOpeningWithBiometry = true
        try {
            val accountKey = keyStore.load(text(R.string.passwords_open_your_passwords))
            if (accountKey == null) {
                setBiometricKept(false)
                return
            }
            current.open(accountKey)
            opened()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: PasswordsKeyStoreException) {
            if (error.failure == PasswordsKeyStoreFailure.Canceled) return
            // The key is forgotten only when it is gone for good (the fingers or faces the phone knows changed).
            if (error.failure == PasswordsKeyStoreFailure.KeyLost) forgetKeptKey()
            snackbar?.showProblem(text(R.string.passwords_biometry_could_not_open, biometryName))
        } catch (error: PasswordsError.NeedsPassphrase) {
            // Passwords was set up again elsewhere: the kept key opens nothing.
            forgetKeptKey()
            snackbar?.showProblem(problemText(error, ""))
        } catch (error: Throwable) {
            snackbar?.showProblem(problemText(error, text(R.string.passwords_biometry_could_not_open, biometryName)))
        } finally {
            isOpeningWithBiometry = false
        }
    }

    /** Puts the account key behind the fingerprint. Needs the passphrase, as the account key is only ever read from it. */
    suspend fun enableBiometry(passphrase: String) {
        val unlocked = requireSession().unlockWithPassphrase(passphrase)
        keyStore.save(unlocked.accountKey)
        setBiometricKept(true)
        say(text(R.string.passwords_biometry_enabled, biometryName))
    }

    fun disableBiometry() {
        forgetKeptKey()
        say(text(R.string.passwords_biometry_disabled))
    }

    private fun setBiometricKept(isKept: Boolean) {
        preferences.edit().putBoolean(biometricDefaultsKey, isKept).apply()
        hasBiometricKey = isKept
    }

    private suspend fun keep(accountKey: ByteArray) {
        try {
            keyStore.save(accountKey)
            setBiometricKept(true)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            setBiometricKept(false)
            snackbar?.showProblem(problemText(error, text(R.string.passwords_biometry_could_not_set_up, biometryName)))
        }
    }

    private fun forgetKeptKey() {
        keyStore.delete()
        setBiometricKept(false)
    }

    /** Passwords is open: the items are shown from the copy at once, then brought up to date. */
    private suspend fun opened() {
        phase = PasswordsPhase.Unlocked
        session?.let { current -> lockAfterMinutes = current.lockAfterMinutes }
        reload()
        sync()
    }

    // Locking

    fun lock() {
        session?.lock()
        items = emptyList()
        deletedItems = emptyList()
        unreadableItemCount = 0
        versionsByItemId.clear()
        ownSafetyCode = null
        if (phase == PasswordsPhase.Unlocked) phase = PasswordsPhase.Locked
    }

    /** Locks when the lock time has passed since Passwords was last used. */
    fun lockIfAskRequired() {
        val current = session ?: return
        if (phase == PasswordsPhase.Unlocked && current.isAskRequired()) lock()
    }

    /** The person is using Passwords (searching, looking), so the lock time runs from now. */
    fun noteUse() {
        if (phase == PasswordsPhase.Unlocked) session?.noteUse()
    }

    // Reading

    /** Brings the copy up to date with the server and shows what changed. */
    suspend fun sync(isFull: Boolean = false) {
        val current = session ?: return
        if (phase != PasswordsPhase.Unlocked || isSyncing) return
        isSyncing = true
        try {
            current.sync(isFull)
            lockAfterMinutes = current.lockAfterMinutes
            reload()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            handleSyncFailure(error)
        } finally {
            isSyncing = false
        }
    }

    private fun handleSyncFailure(error: Throwable) {
        when (error) {
            is PasswordsError.NotSetUp -> {
                forgetKeptKey()
                markClosed(PasswordsPhase.NotSetUp)
            }
            is PasswordsError.NeedsPassphrase -> {
                forgetKeptKey()
                markClosed(PasswordsPhase.Locked)
                snackbar?.showProblem(problemText(error, ""))
            }
            else -> snackbar?.showProblem(problemText(error, text(R.string.passwords_brought_up_to_date_failed)))
        }
    }

    private fun markClosed(closedPhase: PasswordsPhase) {
        items = emptyList()
        deletedItems = emptyList()
        unreadableItemCount = 0
        versionsByItemId.clear()
        ownSafetyCode = null
        phase = closedPhase
    }

    /** Opens the items of the copy for the lists. */
    private fun reload() {
        val current = session ?: return
        try {
            val opened = current.list()
            items = opened.items.filter { item -> !item.isDeleted }
            deletedItems = opened.items.filter { item -> item.isDeleted }.sortedByDescending { item -> item.deletedAtEpochMS ?: 0L }
            unreadableItemCount = opened.unreadableItemIds.size
            ownSafetyCode = runCatching { current.ownSafetyCode() }.getOrNull()
        } catch (error: Throwable) {
            report(error, text(R.string.passwords_items_not_opened))
        }
    }

    /** Says why something failed on the screen in front, and notes a session that turned out to be closed. */
    fun report(error: Throwable, fallback: String) {
        if (error is CancellationException) return
        if (error is PasswordsError.Locked && phase == PasswordsPhase.Unlocked) markClosed(PasswordsPhase.Locked)
        snackbar?.showProblem(problemText(error, fallback))
    }

    // Copying

    /** Copies a value to the clipboard, where it stays for a minute and a half, and says so. */
    fun copy(value: String, fieldName: String) {
        PasswordsClipboard.copy(context, value)
        say(text(R.string.passwords_copied, fieldName))
        noteUse()
    }

    // Changing items

    suspend fun save(fields: PasswordsItemFields, id: String? = null, baseRevision: Long? = null): PasswordItem {
        val current = requireSession()
        try {
            val saved = current.save(fields, id, baseRevision)
            reload()
            return saved
        } catch (error: PasswordsError.ItemChangedOnAnotherDevice) {
            sync()
            throw error
        }
    }

    suspend fun delete(item: PasswordItem) {
        requireSession().delete(item.id)
        reload()
    }

    suspend fun restore(item: PasswordItem) {
        requireSession().restore(item.id)
        reload()
    }

    /** Deletes an item from Recently deleted for good. It cannot be brought back. */
    suspend fun purge(item: PasswordItem) {
        requireSession().purge(item.id)
        reload()
    }

    suspend fun setUsableByAgent(item: PasswordItem, isUsable: Boolean) {
        try {
            requireSession().setUsableByAgent(item.id, isUsable)
        } catch (error: Throwable) {
            // The agent was stopped but the new key could not be made yet: the item says so.
            reload()
            throw error
        }
        reload()
    }

    /** Tries again to give an item a new key, after the agent was stopped and its re-key failed. */
    suspend fun giveNewKey(item: PasswordItem) {
        try {
            requireSession().giveNewKey(item.id)
            say(text(R.string.passwords_new_key_done))
            reload()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            report(error, text(R.string.passwords_new_key_failed))
        }
    }

    fun itemIdsNeedingNewKey(): List<String> = session?.itemIdsNeedingNewKey() ?: emptyList()

    /** Reads the earlier versions of an item the person owns from the server and opens them. False when they could not be read. */
    suspend fun loadVersions(item: PasswordItem): Boolean {
        val current = session ?: return false
        return try {
            versionsByItemId[item.id] = current.versions(item.id)
            noteUse()
            true
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            report(error, text(R.string.passwords_versions_could_not_read))
            false
        }
    }

    /** Puts an earlier version's content back as the item's own. Who it is shared with stays as it is now, and the item is given a new key at once. */
    suspend fun restoreVersion(version: PasswordItemVersion, item: PasswordItem) {
        try {
            requireSession().restoreVersion(item.id, version.id)
        } catch (error: PasswordsError.ItemChangedOnAnotherDevice) {
            sync()
            throw error
        }
        reload()
    }

    fun forgetVersions(itemId: String) {
        versionsByItemId.remove(itemId)
    }

    // Settings

    suspend fun changePassphrase(currentPassphrase: String, newPassphrase: String) {
        requireSession().changePassphrase(currentPassphrase, newPassphrase, iterationCount)
    }

    /** Changes the passphrase for a person who forgot the old one, proving it with the recovery key. */
    suspend fun changePassphraseWithRecoveryKey(recoveryKey: String, newPassphrase: String) {
        requireSession().changePassphraseWithRecoveryKey(recoveryKey, newPassphrase, iterationCount)
    }

    /** Makes a new recovery key (the old one stops working) and returns it as the person writes it down. */
    suspend fun replaceRecoveryKey(passphrase: String): String = requireSession().replaceRecoveryKey(passphrase)

    suspend fun updateLockAfterMinutes(minutes: Int) {
        lockAfterMinutes = requireSession().updateLockAfterMinutes(minutes)
    }

    /**
     * Erases Passwords on the server and on this phone. Works while locked, which is the way out for a person who lost both
     * the passphrase and the recovery key.
     */
    suspend fun erase() {
        requireSession().erase()
        forgetKeptKey()
        markClosed(PasswordsPhase.NotSetUp)
        say(text(R.string.passwords_erased_done))
    }

    // Words

    /**
     * What a failed call says, in the person's language. The Passwords session throws errors whose own text is English
     * only, so the cases the person can act on are worded here; the server's own reason comes through as it is, and
     * anything else as the sentence the caller wrote.
     */
    fun problemText(error: Throwable, fallback: String): String = when (error) {
        is PasswordsError -> when (error) {
            PasswordsError.Locked -> text(R.string.passwords_error_locked)
            PasswordsError.NeedsPassphrase -> text(R.string.passwords_error_needs_passphrase)
            PasswordsError.NotSetUp -> text(R.string.passwords_error_not_set_up)
            PasswordsError.AlreadySetUp -> text(R.string.passwords_error_already_set_up)
            PasswordsError.WrongPassphrase -> text(R.string.passwords_error_wrong_passphrase)
            PasswordsError.WrongRecoveryKey -> text(R.string.passwords_error_wrong_recovery_key)
            PasswordsError.ItemChangedOnAnotherDevice -> text(R.string.passwords_error_changed_elsewhere)
            PasswordsError.ItemNotFound -> text(R.string.passwords_error_item_not_found)
            PasswordsError.OnlyOwnerCanChangeItem -> text(R.string.passwords_error_only_owner)
            PasswordsError.ItemKindCannotChange -> text(R.string.passwords_error_kind_cannot_change)
            PasswordsError.CardCannotBeUsedByAgent -> text(R.string.passwords_error_card_agent)
            PasswordsError.RecipientKeyChangedSinceRead -> text(R.string.passwords_error_recipient_key)
            PasswordsError.TooManyAttempts -> text(R.string.passwords_error_too_many)
            is PasswordsError.UnknownItemKind -> text(R.string.passwords_error_unknown_kind)
            PasswordsError.InvalidServerAnswer -> text(R.string.passwords_error_invalid_answer)
            is PasswordsError.ItemNeedsNewKey -> text(R.string.passwords_error_new_key)
            is PasswordsError.SharingCouldNotBeChecked -> text(R.string.passwords_error_unchecked)
            PasswordsError.AnsweredAboutAnotherItem -> text(R.string.passwords_error_another_item)
            PasswordsError.RestoredAnotherVersion -> text(R.string.passwords_error_restored_other)
        }
        is PasswordsCryptoException -> when (error.failure) {
            PasswordsCryptoFailure.InvalidRecoveryKey -> text(R.string.passwords_error_recovery_key_invalid)
            PasswordsCryptoFailure.IterationCountTooLow -> text(R.string.passwords_error_iterations_low)
            PasswordsCryptoFailure.IterationCountTooHigh -> text(R.string.passwords_error_iterations_high)
            else -> fallback
        }
        is PasswordsKeyStoreException -> fallback
        is TeaNodeError.Refused -> appSession?.describe(error)?.takeIf { described -> described.isNotBlank() } ?: error.reason
        else -> appSession?.describe(error)?.takeIf { described -> described.isNotBlank() } ?: fallback
    }

    companion object {
        private val models = WeakHashMap<Connection, PasswordsModel>()

        /** The model of the connection in use: one for each, so every Passwords screen shares one open session. */
        fun of(connection: Connection, context: Context, isDemo: Boolean, demoStart: PasswordsDemoStart = PasswordsDemoStart.Unlocked): PasswordsModel =
            synchronized(models) {
                models.getOrPut(connection) {
                    if (isDemo) {
                        PasswordsDemoStore.reset(isSetUp = demoStart != PasswordsDemoStart.NotSetUp)
                        PasswordsModel(
                            connection, context.applicationContext, true, MemoryPasswordsKeyStore(), InMemoryPasswordsStorage(),
                            PasswordsDemoStore.iterationCount, demoStart,
                        )
                    } else {
                        val directory = PasswordsAutofillPointer.directory(context.applicationContext, connection.accountKey)
                        // The autofill service, in this same process, reads the copy of the sign-in in use.
                        PasswordsAutofillPointer.write(context.applicationContext, connection.accountKey)
                        PasswordsModel(
                            connection, context.applicationContext, false, AndroidPasswordsKeyStore(context.applicationContext, connection.accountKey),
                            FilePasswordsStorage(directory), PasswordsCrypto.PASSPHRASE_ITERATION_COUNT,
                        )
                    }
                }
            }

        /** Forgets the model of a sign-in that has ended, so a new one starts with nothing in memory. */
        fun forget(connection: Connection) {
            synchronized(models) { models.remove(connection) }
        }
    }
}

/** PasswordsClipboard copies a secret so it does not stay: it is marked sensitive (the system hides its preview) and removed after a minute and a half. */
object PasswordsClipboard {
    private val handler = android.os.Handler(android.os.Looper.getMainLooper())

    fun copy(context: Context, text: String) {
        val clipboard = context.getSystemService(ClipboardManager::class.java) ?: return
        val clip = ClipData.newPlainText("", text)
        if (Build.VERSION.SDK_INT >= 33) {
            clip.description.extras = PersistableBundle().apply { putBoolean(ClipDescription.EXTRA_IS_SENSITIVE, true) }
        }
        clipboard.setPrimaryClip(clip)
        handler.removeCallbacksAndMessages(null)
        handler.postDelayed({
            // Only what this app put there is taken away: something the person copied since stays.
            val held = clipboard.primaryClip?.getItemAt(0)?.text?.toString()
            if (held == text) {
                if (Build.VERSION.SDK_INT >= 28) clipboard.clearPrimaryClip() else clipboard.setPrimaryClip(ClipData.newPlainText("", ""))
            }
        }, com.teanode.kit.passwords.PasswordsFormat.CLIPBOARD_SECONDS_KEPT * 1000)
    }
}

/** The model of the connection in use, with this screen's snackbars attached. */
@Composable
internal fun rememberPasswordsModel(demoStart: PasswordsDemoStart = PasswordsDemoStart.Unlocked): PasswordsModel {
    val connection = com.teanode.app.session.LocalConnection.current
    val appSession = com.teanode.app.session.LocalAppSession.current
    val snackbar = com.teanode.app.ui.components.LocalSnackbar.current
    val context = LocalContext.current
    val model = remember(connection) { PasswordsModel.of(connection, context, appSession.isDemo, demoStart) }
    model.attach(snackbar, appSession)
    return model
}
