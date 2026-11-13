package com.teanode.app.passwords

import android.app.Activity
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.os.CancellationSignal
import android.os.OutcomeReceiver
import androidx.annotation.RequiresApi
import androidx.credentials.CreatePublicKeyCredentialRequest
import androidx.credentials.CreatePublicKeyCredentialResponse
import androidx.credentials.GetCredentialResponse
import androidx.credentials.GetPublicKeyCredentialOption
import androidx.credentials.PublicKeyCredential
import androidx.credentials.exceptions.ClearCredentialException
import androidx.credentials.exceptions.CreateCredentialCancellationException
import androidx.credentials.exceptions.CreateCredentialException
import androidx.credentials.exceptions.CreateCredentialUnknownException
import androidx.credentials.exceptions.GetCredentialCancellationException
import androidx.credentials.exceptions.GetCredentialException
import androidx.credentials.exceptions.GetCredentialUnknownException
import androidx.credentials.provider.AuthenticationAction
import androidx.credentials.provider.BeginCreateCredentialRequest
import androidx.credentials.provider.BeginCreateCredentialResponse
import androidx.credentials.provider.BeginCreatePublicKeyCredentialRequest
import androidx.credentials.provider.BeginGetCredentialRequest
import androidx.credentials.provider.BeginGetCredentialResponse
import androidx.credentials.provider.BeginGetPublicKeyCredentialOption
import androidx.credentials.provider.CallingAppInfo
import androidx.credentials.provider.CreateEntry
import androidx.credentials.provider.CredentialProviderService
import androidx.credentials.provider.PendingIntentHandler
import androidx.credentials.provider.ProviderClearCredentialStateRequest
import androidx.credentials.provider.PublicKeyCredentialEntry
import androidx.lifecycle.lifecycleScope
import com.teanode.app.MainActivity
import com.teanode.app.R
import com.teanode.app.TeaNodeApplication
import com.teanode.kit.passwords.ConnectionPasswordsApi
import com.teanode.kit.passwords.FilePasswordsStorage
import com.teanode.kit.passwords.PasswordItem
import com.teanode.kit.passwords.PasswordsCache
import com.teanode.kit.passwords.PasswordsItemFields
import com.teanode.kit.passwords.PasswordsLoginFields
import com.teanode.kit.passwords.PasswordsOpener
import com.teanode.kit.passwords.PasswordsPasskeyCaller
import com.teanode.kit.passwords.PasswordsPasskeyFields
import com.teanode.kit.passwords.PasswordsPasskeyRequestException
import com.teanode.kit.passwords.PasswordsPasskeyRequestFailure
import com.teanode.kit.passwords.PasswordsPasskeyRequests
import com.teanode.kit.passwords.PasswordsSession
import java.security.MessageDigest
import java.time.Instant
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.launch

// Passkeys through the Credential Manager, on Android 14 and later: the counterpart of the iPhone's AutoFill extension for
// passkeys. The system sends a website's request as JSON; PasswordsPasskeyRequests (the kit, tested without a phone) reads it
// and writes the answer. The service below only answers "what can you offer" with a locked entry, which opens
// PasswordsCredentialActivity: it asks for the fingerprint (the account key's Keystore key opens only for it), opens the copy
// of the items without the network, and lists, signs with, or makes a passkey. Making one saves the login to the server first,
// and only then does the website get the passkey.

/** What the Credential Manager asks of this provider: the service says where to unlock, and the activity does the work. */
@RequiresApi(34)
class PasswordsCredentialProviderService : CredentialProviderService() {
    override fun onBeginGetCredentialRequest(
        request: BeginGetCredentialRequest,
        cancellationSignal: CancellationSignal,
        callback: OutcomeReceiver<BeginGetCredentialResponse, GetCredentialException>,
    ) {
        val hasPasskeyOption = request.beginGetCredentialOptions.any { option -> option is BeginGetPublicKeyCredentialOption }
        if (!hasPasskeyOption || PasswordsAutofillPointer.read(this) == null) {
            callback.onResult(BeginGetCredentialResponse())
            return
        }
        val unlock = PasswordsCredentialIntents.pending(this, PasswordsCredentialActivity.ACTION_UNLOCK, null)
        callback.onResult(BeginGetCredentialResponse(authenticationActions = listOf(AuthenticationAction(getString(R.string.passwords_credential_unlock), unlock))))
    }

    override fun onBeginCreateCredentialRequest(
        request: BeginCreateCredentialRequest,
        cancellationSignal: CancellationSignal,
        callback: OutcomeReceiver<BeginCreateCredentialResponse, CreateCredentialException>,
    ) {
        val creation = request as? BeginCreatePublicKeyCredentialRequest
        if (creation == null || PasswordsAutofillPointer.read(this) == null) {
            callback.onError(CreateCredentialUnknownException())
            return
        }
        val parsed = try {
            PasswordsPasskeyRequests.creation(creation.requestJson)
        } catch (_: PasswordsPasskeyRequestException) {
            callback.onError(CreateCredentialUnknownException())
            return
        }
        val pending = PasswordsCredentialIntents.pending(this, PasswordsCredentialActivity.ACTION_CREATE, null)
        val entry = CreateEntry.Builder(parsed.userName, pending)
            .setDescription(getString(R.string.passwords_credential_save_in, parsed.relyingPartyName.ifEmpty { parsed.relyingPartyId }))
            .build()
        callback.onResult(BeginCreateCredentialResponse(createEntries = listOf(entry)))
    }

    override fun onClearCredentialStateRequest(
        request: ProviderClearCredentialStateRequest,
        cancellationSignal: CancellationSignal,
        callback: OutcomeReceiver<Void?, ClearCredentialException>,
    ) {
        // Nothing here is kept as a signed-in state of a website.
        callback.onResult(null)
    }
}

/** The pending intents the system fires when the person picks an entry. They have to be mutable: the system adds the request to them. */
internal object PasswordsCredentialIntents {
    private val requestCodes = AtomicInteger(1)

    fun pending(context: Context, action: String, extras: Bundle?): PendingIntent {
        val intent = Intent(context, PasswordsCredentialActivity::class.java).setAction(action)
        extras?.let { bundle -> intent.putExtras(bundle) }
        return PendingIntent.getActivity(context, requestCodes.getAndIncrement(), intent, PendingIntent.FLAG_MUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    }
}

/**
 * The passkeys the person was just asked to unlock for, kept in this process for a minute so that choosing one does not ask for
 * the fingerprint a second time. A passkey is taken out when it is used.
 */
internal object PasswordsCredentialHolder {
    private const val KEPT_MILLISECONDS = 60_000L
    private class Held(val passkey: PasswordsPasskeyFields, val untilEpochMS: Long)

    private val held = HashMap<String, Held>()

    private fun key(itemId: String, credentialId: String) = "$itemId|$credentialId"

    @Synchronized
    fun hold(itemId: String, passkey: PasswordsPasskeyFields) {
        val now = System.currentTimeMillis()
        held.values.removeAll { entry -> entry.untilEpochMS < now }
        held[key(itemId, passkey.credentialId)] = Held(passkey, now + KEPT_MILLISECONDS)
    }

    @Synchronized
    fun take(itemId: String, credentialId: String): PasswordsPasskeyFields? {
        val entry = held.remove(key(itemId, credentialId)) ?: return null
        return if (entry.untilEpochMS >= System.currentTimeMillis()) entry.passkey else null
    }
}

/** What the activity found: the opened copy, the account key's session and the sign-in it belongs to. */
private class OpenedVault(val signInAccountKey: String, val accountKey: ByteArray, val cache: PasswordsCache, val opener: PasswordsOpener, val userId: String, val items: List<PasswordItem>)

/** Who is asking, as the kit wants it: a browser the allowlist names, else an app named by its signing certificate. */
@RequiresApi(34)
internal fun passkeyCaller(context: Context, info: CallingAppInfo, clientDataHash: ByteArray?): PasswordsPasskeyCaller {
    val allowlist = runCatching { context.assets.open("passwords_passkey_privileged_apps.json").bufferedReader().use { reader -> reader.readText() } }.getOrDefault("""{"apps":[]}""")
    val origin = runCatching { info.getOrigin(allowlist) }.getOrNull()
    if (origin != null) {
        // A browser names the page and builds the client data itself, so it has to bring the hash.
        if (clientDataHash == null) throw PasswordsPasskeyRequestException(PasswordsPasskeyRequestFailure.UnknownCaller)
        return PasswordsPasskeyCaller(webOrigin = origin.trimEnd('/'), packageName = info.packageName, certificateSha256 = null, clientDataHash = clientDataHash)
    }
    val signature = info.signingInfo.apkContentsSigners.firstOrNull() ?: throw PasswordsPasskeyRequestException(PasswordsPasskeyRequestFailure.UnknownCaller)
    val certificate = MessageDigest.getInstance("SHA-256").digest(signature.toByteArray())
    return PasswordsPasskeyCaller(webOrigin = null, packageName = info.packageName, certificateSha256 = certificate, clientDataHash = null)
}

/**
 * Asks for the fingerprint, then does what the system asked: lists the passkeys that fit ([ACTION_UNLOCK]), signs with the one
 * the person picked ([ACTION_USE]), or makes a passkey and saves it to the server ([ACTION_CREATE]).
 */
@RequiresApi(34)
class PasswordsCredentialActivity : androidx.activity.ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        lifecycleScope.launch {
            when (intent.action) {
                ACTION_UNLOCK -> listPasskeys()
                ACTION_USE -> signIn()
                ACTION_CREATE -> createPasskey()
                else -> cancelGet()
            }
        }
    }

    private fun cancelGet() {
        val result = Intent()
        PendingIntentHandler.setGetCredentialException(result, GetCredentialCancellationException())
        setResult(Activity.RESULT_OK, result)
        finish()
    }

    private fun failGet() {
        val result = Intent()
        PendingIntentHandler.setGetCredentialException(result, GetCredentialUnknownException())
        setResult(Activity.RESULT_OK, result)
        finish()
    }

    private fun cancelCreate() {
        val result = Intent()
        PendingIntentHandler.setCreateCredentialException(result, CreateCredentialCancellationException())
        setResult(Activity.RESULT_OK, result)
        finish()
    }

    private fun failCreate() {
        val result = Intent()
        PendingIntentHandler.setCreateCredentialException(result, CreateCredentialUnknownException(getString(R.string.passwords_credential_save_failed)))
        setResult(Activity.RESULT_OK, result)
        finish()
    }

    /** Asks for the fingerprint and opens the copy of the items; null when the person cancelled or there is nothing to open (the app is opened then). */
    private suspend fun openVault(): OpenedVault? {
        val signInAccountKey = PasswordsAutofillPointer.read(this) ?: return null
        val keyStore = AndroidPasswordsKeyStore(applicationContext, signInAccountKey)
        val accountKey = try {
            keyStore.load(getString(R.string.passwords_open_your_passwords))
        } catch (_: PasswordsKeyStoreException) {
            return null
        }
        if (accountKey == null) {
            // No key is kept behind the fingerprint: the app has to be opened and Passwords unlocked with the passphrase there.
            startActivity(Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            return null
        }
        val cache = PasswordsCache(FilePasswordsStorage(PasswordsAutofillPointer.directory(this, signInAccountKey)))
        val contents = cache.load() ?: return null
        val opener = PasswordsOpener.fromPrivateKey(accountKey, contents.userId, contents.privateKeyByAccountKey, contents.publicKey, cache.loadPins(contents.userId))
        return OpenedVault(signInAccountKey, accountKey, cache, opener, contents.userId, opener.openAll(contents.items).items.filter { item -> !item.isDeleted })
    }

    private fun PasswordItem.loginFields(): PasswordsLoginFields? = (fields as? PasswordsItemFields.Login)?.fields

    // Listing

    private suspend fun listPasskeys() {
        val begin = PendingIntentHandler.retrieveBeginGetCredentialRequest(intent)
        val vault = if (begin == null) null else openVault()
        if (begin == null || vault == null) {
            cancelGet()
            return
        }
        val entries = mutableListOf<PublicKeyCredentialEntry>()
        for (option in begin.beginGetCredentialOptions.filterIsInstance<BeginGetPublicKeyCredentialOption>()) {
            val request = try {
                PasswordsPasskeyRequests.assertion(option.requestJson)
            } catch (_: PasswordsPasskeyRequestException) {
                continue
            }
            for (item in vault.items.filter { item -> !item.isSharedWithMe }) {
                val login = item.loginFields() ?: continue
                for (passkey in login.passkeys.filter { passkey -> PasswordsPasskeyRequests.isAllowed(passkey, request) }) {
                    PasswordsCredentialHolder.hold(item.id, passkey)
                    val use = PasswordsCredentialIntents.pending(
                        this, ACTION_USE, Bundle().apply { putString(EXTRA_ITEM_ID, item.id); putString(EXTRA_CREDENTIAL_ID, passkey.credentialId) },
                    )
                    entries.add(
                        PublicKeyCredentialEntry.Builder(this, passkey.userName.ifEmpty { login.userName }.ifEmpty { item.name }, use, option)
                            .setDisplayName(item.name)
                            .build(),
                    )
                }
            }
        }
        val result = Intent()
        PendingIntentHandler.setBeginGetCredentialResponse(result, BeginGetCredentialResponse(credentialEntries = entries))
        setResult(Activity.RESULT_OK, result)
        finish()
    }

    // Signing in

    private suspend fun signIn() {
        val request = PendingIntentHandler.retrieveProviderGetCredentialRequest(intent)
        val itemId = intent.getStringExtra(EXTRA_ITEM_ID)
        val credentialId = intent.getStringExtra(EXTRA_CREDENTIAL_ID)
        val option = request?.credentialOptions?.filterIsInstance<GetPublicKeyCredentialOption>()?.firstOrNull()
        if (request == null || itemId == null || credentialId == null || option == null) {
            failGet()
            return
        }
        // Kept from the moment the list was made, so the fingerprint is asked once; if that has passed, it is asked again.
        val passkey = PasswordsCredentialHolder.take(itemId, credentialId) ?: run {
            val vault = openVault()
            vault?.items?.firstOrNull { item -> item.id == itemId }?.loginFields()?.passkeys?.firstOrNull { held -> held.credentialId == credentialId }
        }
        if (passkey == null) {
            cancelGet()
            return
        }
        try {
            val caller = passkeyCaller(this, request.callingAppInfo, option.clientDataHash)
            val response = PasswordsPasskeyRequests.assertionResponse(passkey, PasswordsPasskeyRequests.assertion(option.requestJson), caller)
            val result = Intent()
            PendingIntentHandler.setGetCredentialResponse(result, GetCredentialResponse(PublicKeyCredential(response)))
            setResult(Activity.RESULT_OK, result)
            finish()
        } catch (_: Exception) {
            failGet()
        }
    }

    // Making a passkey

    private suspend fun createPasskey() {
        val request = PendingIntentHandler.retrieveProviderCreateCredentialRequest(intent)
        val creation = request?.callingRequest as? CreatePublicKeyCredentialRequest
        if (request == null || creation == null) {
            failCreate()
            return
        }
        try {
            val parsed = PasswordsPasskeyRequests.creation(creation.requestJson)
            val caller = passkeyCaller(this, request.callingAppInfo, creation.clientDataHash)
            val vault = openVault()
            if (vault == null) {
                cancelCreate()
                return
            }
            val allPasskeys = vault.items.filter { item -> !item.isSharedWithMe }.flatMap { item -> item.loginFields()?.passkeys.orEmpty() }
            if (PasswordsPasskeyRequests.isExcluded(allPasskeys, parsed)) {
                setResult(Activity.RESULT_OK, Intent().also { result -> PendingIntentHandler.setCreateCredentialException(result, CreateCredentialUnknownException(getString(R.string.passwords_credential_already_here))) })
                finish()
                return
            }
            val connection = (application as TeaNodeApplication).session.connection.value
            if (connection == null) {
                setResult(Activity.RESULT_OK, Intent().also { result -> PendingIntentHandler.setCreateCredentialException(result, CreateCredentialUnknownException(getString(R.string.passwords_credential_open_app))) })
                finish()
                return
            }
            val created = PasswordsPasskeyRequests.createPasskey(parsed, caller, Instant.now())
            // Only the login this saves into is written; the app does the syncing. The website gets the passkey only once the server has it.
            val session = PasswordsSession(ConnectionPasswordsApi(connection), vault.cache, vault.userId)
            session.open(vault.accountKey)
            val existing = vault.items.firstOrNull { item -> !item.isSharedWithMe && item.loginFields()?.let { login -> PasswordsPasskeyRequests.isLoginFor(login, parsed) } == true }
            if (existing != null) {
                // An item the server marked has a key somebody who lost access may hold: the app gives it a new one first.
                if (existing.needsNewKey) throw IllegalStateException("The login needs a new key")
                val login = PasswordsPasskeyRequests.loginKeeping(created.passkey, parsed, existing.loginFields())
                session.save(PasswordsItemFields.Login(login), existing.id, existing.revision)
            } else {
                session.save(PasswordsItemFields.Login(PasswordsPasskeyRequests.loginKeeping(created.passkey, parsed, null)))
            }
            val result = Intent()
            PendingIntentHandler.setCreateCredentialResponse(result, CreatePublicKeyCredentialResponse(created.responseJson))
            setResult(Activity.RESULT_OK, result)
            finish()
        } catch (error: PasswordsPasskeyRequestException) {
            val message = if (error.failure == PasswordsPasskeyRequestFailure.NoSupportedAlgorithm) R.string.passwords_credential_unsupported else R.string.passwords_credential_save_failed
            setResult(Activity.RESULT_OK, Intent().also { result -> PendingIntentHandler.setCreateCredentialException(result, CreateCredentialUnknownException(getString(message))) })
            finish()
        } catch (_: Exception) {
            failCreate()
        }
    }

    companion object {
        const val ACTION_UNLOCK = "com.teanode.app.action.PASSKEY_UNLOCK"
        const val ACTION_USE = "com.teanode.app.action.PASSKEY_USE"
        const val ACTION_CREATE = "com.teanode.app.action.PASSKEY_CREATE"
        const val EXTRA_ITEM_ID = "com.teanode.app.extra.PASSKEY_ITEM_ID"
        const val EXTRA_CREDENTIAL_ID = "com.teanode.app.extra.PASSKEY_CREDENTIAL_ID"
    }
}
