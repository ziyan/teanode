package com.teanode.app.passwords

import android.app.Activity
import android.app.PendingIntent
import android.app.assist.AssistStructure
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.os.CancellationSignal
import android.service.autofill.AutofillService
import android.service.autofill.Dataset
import android.service.autofill.FillCallback
import android.service.autofill.FillRequest
import android.service.autofill.FillResponse
import android.service.autofill.SaveCallback
import android.service.autofill.SaveRequest
import android.text.InputType
import android.view.autofill.AutofillId
import android.view.autofill.AutofillValue
import android.view.View
import android.widget.RemoteViews
import androidx.lifecycle.lifecycleScope
import com.teanode.app.MainActivity
import com.teanode.app.R
import com.teanode.kit.passwords.PasswordItem
import com.teanode.kit.passwords.PasswordsCache
import com.teanode.kit.passwords.PasswordsItemFields
import com.teanode.kit.passwords.PasswordsOpener
import com.teanode.kit.passwords.PasswordsSiteMatch
import com.teanode.kit.passwords.FilePasswordsStorage
import java.io.File
import kotlinx.coroutines.launch

// Autofill for Passwords, the Android counterpart of the iPhone's AutoFill extension. Android runs an autofill service in the
// app's own process, so it reads the same copy of the items the app keeps, opens only the logins it fills, and never touches
// the network. The account key comes from the Keystore behind the fingerprint, so every fill asks for it.

/** What a page asked to fill: the fields, and the page it is. */
internal class AutofillFields(
    val userNameIds: List<AutofillId>,
    val passwordIds: List<AutofillId>,
    /** The fields a one-time code goes in. */
    val oneTimeCodeIds: List<AutofillId>,
    /** The web address of the page when a browser says it (`https://example.com`), else null. */
    val webAddress: String?,
    val packageName: String?,
)

/** Reads an assist structure for the fields a login fills and the page they are on. */
internal object AutofillStructureParser {
    fun parse(structure: AssistStructure): AutofillFields {
        val userNameIds = mutableListOf<AutofillId>()
        val passwordIds = mutableListOf<AutofillId>()
        val oneTimeCodeIds = mutableListOf<AutofillId>()
        var webDomain: String? = null
        var webScheme: String? = null
        fun visit(node: AssistStructure.ViewNode) {
            val id = node.autofillId
            if (id != null && node.autofillType == View.AUTOFILL_TYPE_TEXT) {
                val hints = node.autofillHints?.toList() ?: emptyList()
                val inputType = node.inputType
                val isPassword = hints.contains(View.AUTOFILL_HINT_PASSWORD) ||
                    (inputType and InputType.TYPE_MASK_CLASS == InputType.TYPE_CLASS_TEXT &&
                        (inputType and InputType.TYPE_MASK_VARIATION == InputType.TYPE_TEXT_VARIATION_PASSWORD ||
                            inputType and InputType.TYPE_MASK_VARIATION == InputType.TYPE_TEXT_VARIATION_WEB_PASSWORD ||
                            inputType and InputType.TYPE_MASK_VARIATION == InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD))
                val isUserName = hints.contains(View.AUTOFILL_HINT_USERNAME) || hints.contains(View.AUTOFILL_HINT_EMAIL_ADDRESS) ||
                    (inputType and InputType.TYPE_MASK_VARIATION == InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS) ||
                    (node.htmlInfo?.attributes?.any { attribute -> attribute.first == "autocomplete" && attribute.second.contains("username") } == true)
                val isOneTimeCode = hints.any { hint -> hint.equals("smsOTPCode", true) || hint.equals("oneTimeCode", true) || hint.equals("one-time-code", true) }
                if (isOneTimeCode) oneTimeCodeIds.add(id) else if (isPassword) passwordIds.add(id) else if (isUserName) userNameIds.add(id)
            }
            node.webDomain?.takeIf { domain -> domain.isNotEmpty() }?.let { domain -> webDomain = domain; webScheme = node.webScheme }
            for (index in 0 until node.childCount) visit(node.getChildAt(index))
        }
        for (index in 0 until structure.windowNodeCount) visit(structure.getWindowNodeAt(index).rootViewNode)
        val address = webDomain?.let { domain -> "${webScheme ?: "https"}://$domain" }
        return AutofillFields(userNameIds, passwordIds, oneTimeCodeIds, address, structure.activityComponent?.packageName)
    }
}

/**
 * The logins the person has for a page, most fitting first: the exact origin first, then related hosts, never a lookalike
 * (the rule of PasswordsSiteMatch, shared with the dashboard, the agent and the iPhone). A native app is not a web page, so
 * no login is offered for one.
 */
internal fun loginsFor(items: List<PasswordItem>, requested: String?): List<PasswordItem> {
    if (requested == null) return emptyList()
    val logins = items.filter { item -> !item.isDeleted && item.fields is PasswordsItemFields.Login }
        .sortedWith(compareBy(String.CASE_INSENSITIVE_ORDER) { item -> item.name })
    return PasswordsSiteMatch.sorted(logins, requested, isPageAddress = true) { item -> item.sites() }
}

private fun PasswordItem.sites(): List<String> = (fields as PasswordsItemFields.Login).fields.sites

/** Where the app writes which sign-in the autofill service reads the copy of. */
internal object PasswordsAutofillPointer {
    private fun file(context: Context) = File(context.filesDir, "passwords/active")

    fun write(context: Context, signInAccountKey: String) {
        file(context).apply { parentFile?.mkdirs() }.writeText(signInAccountKey)
    }

    fun read(context: Context): String? = file(context).takeIf { file -> file.exists() }?.readText()?.takeIf { text -> text.isNotEmpty() }

    fun directory(context: Context, signInAccountKey: String) = File(context.filesDir, "passwords/" + android.net.Uri.encode(signInAccountKey))
}

/**
 * The autofill service: offers a locked entry for a page with a login form, which opens [PasswordsAutofillActivity] to ask
 * for the fingerprint and then fills. Passkeys through the Credential Manager are not part of this port.
 */
class PasswordsAutofillService : AutofillService() {
    override fun onFillRequest(request: FillRequest, cancellationSignal: CancellationSignal, callback: FillCallback) {
        val structure = request.fillContexts.lastOrNull()?.structure
        val fields = structure?.let { assist -> AutofillStructureParser.parse(assist) }
        // Only a page with a password field, and only a web page: a login is for a site.
        if (fields == null || (fields.passwordIds.isEmpty() && fields.oneTimeCodeIds.isEmpty()) || fields.webAddress == null || PasswordsAutofillPointer.read(this) == null) {
            callback.onSuccess(null)
            return
        }
        val intent = Intent(this, PasswordsAutofillActivity::class.java)
            .putExtra(PasswordsAutofillActivity.EXTRA_WEB_ADDRESS, fields.webAddress)
            .putParcelableArrayListExtra(PasswordsAutofillActivity.EXTRA_USER_NAME_IDS, ArrayList(fields.userNameIds))
            .putParcelableArrayListExtra(PasswordsAutofillActivity.EXTRA_PASSWORD_IDS, ArrayList(fields.passwordIds))
            .putParcelableArrayListExtra(PasswordsAutofillActivity.EXTRA_ONE_TIME_CODE_IDS, ArrayList(fields.oneTimeCodeIds))
        val pending = PendingIntent.getActivity(this, 0, intent, PendingIntent.FLAG_CANCEL_CURRENT or PendingIntent.FLAG_MUTABLE)
        val presentation = RemoteViews(packageName, R.layout.passwords_autofill_item).apply {
            setTextViewText(R.id.passwords_autofill_title, getString(R.string.passwords_title))
            setTextViewText(R.id.passwords_autofill_detail, getString(R.string.passwords_autofill_locked_entry))
        }
        val response = FillResponse.Builder()
            .setAuthentication((fields.userNameIds + fields.passwordIds + fields.oneTimeCodeIds).toTypedArray(), pending.intentSender, presentation)
            .build()
        callback.onSuccess(response)
    }

    override fun onSaveRequest(request: SaveRequest, callback: SaveCallback) {
        // Saving a login from a page the person typed is not part of this port: the app's editor makes logins.
        callback.onSuccess()
    }
}

/**
 * Asks for the fingerprint (the account key's Keystore key opens only for it), opens the copy of the items without the
 * network, and answers the autofill request with the logins that fit the page.
 */
class PasswordsAutofillActivity : androidx.activity.ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val webAddress = intent.getStringExtra(EXTRA_WEB_ADDRESS)
        val userNameIds = intent.getParcelableArrayListExtra<AutofillId>(EXTRA_USER_NAME_IDS) ?: arrayListOf()
        val passwordIds = intent.getParcelableArrayListExtra<AutofillId>(EXTRA_PASSWORD_IDS) ?: arrayListOf()
        val oneTimeCodeIds = intent.getParcelableArrayListExtra<AutofillId>(EXTRA_ONE_TIME_CODE_IDS) ?: arrayListOf()
        val signInAccountKey = PasswordsAutofillPointer.read(this)
        if (signInAccountKey == null || webAddress == null) {
            finish()
            return
        }
        lifecycleScope.launchWhenCreatedCompat {
            try {
                val keyStore = AndroidPasswordsKeyStore(applicationContext, signInAccountKey)
                val accountKey = keyStore.load(getString(R.string.passwords_open_your_passwords))
                if (accountKey == null) {
                    // No key is kept behind the fingerprint: the app has to be opened and Passwords unlocked with the passphrase there.
                    startActivity(Intent(this@PasswordsAutofillActivity, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                    setResult(Activity.RESULT_CANCELED)
                    finish()
                    return@launchWhenCreatedCompat
                }
                val cache = PasswordsCache(FilePasswordsStorage(PasswordsAutofillPointer.directory(this@PasswordsAutofillActivity, signInAccountKey)))
                val contents = cache.load()
                if (contents == null) {
                    setResult(Activity.RESULT_CANCELED)
                    finish()
                    return@launchWhenCreatedCompat
                }
                val opener = PasswordsOpener.fromPrivateKey(accountKey, contents.userId, contents.privateKeyByAccountKey, contents.publicKey, cache.loadPins(contents.userId))
                val logins = loginsFor(opener.openAll(contents.items).items, webAddress)
                val response = FillResponse.Builder()
                for (login in logins.take(MOST_LOGINS_OFFERED)) {
                    val fields = (login.fields as PasswordsItemFields.Login).fields
                    val dataset = Dataset.Builder()
                    val presentation = RemoteViews(packageName, R.layout.passwords_autofill_item).apply {
                        setTextViewText(R.id.passwords_autofill_title, login.name)
                        setTextViewText(R.id.passwords_autofill_detail, fields.userName)
                    }
                    for (id in userNameIds) dataset.setValue(id, AutofillValue.forText(fields.userName), presentation)
                    for (id in passwordIds) dataset.setValue(id, AutofillValue.forText(fields.password), presentation)
                    // The code of the moment, when the login has one and the page asks for it.
                    val codeSettings = runCatching { com.teanode.kit.passwords.PasswordsOneTimeCode.parse(fields.oneTimeCodeUri) }.getOrNull()
                    if (codeSettings != null) {
                        val code = com.teanode.kit.passwords.PasswordsOneTimeCode.code(codeSettings, System.currentTimeMillis() / 1000)
                        for (id in oneTimeCodeIds) dataset.setValue(id, AutofillValue.forText(code), presentation)
                    }
                    if (userNameIds.isEmpty() && passwordIds.isEmpty() && (codeSettings == null || oneTimeCodeIds.isEmpty())) continue
                    response.addDataset(dataset.build())
                }
                if (logins.isEmpty()) {
                    setResult(Activity.RESULT_CANCELED)
                } else {
                    setResult(Activity.RESULT_OK, Intent().putExtra(android.view.autofill.AutofillManager.EXTRA_AUTHENTICATION_RESULT, response.build()))
                }
            } catch (error: Throwable) {
                setResult(Activity.RESULT_CANCELED)
            }
            finish()
        }
    }

    companion object {
        const val EXTRA_WEB_ADDRESS = "com.teanode.app.extra.AUTOFILL_WEB_ADDRESS"
        const val EXTRA_USER_NAME_IDS = "com.teanode.app.extra.AUTOFILL_USER_NAME_IDS"
        const val EXTRA_PASSWORD_IDS = "com.teanode.app.extra.AUTOFILL_PASSWORD_IDS"
        const val EXTRA_ONE_TIME_CODE_IDS = "com.teanode.app.extra.AUTOFILL_ONE_TIME_CODE_IDS"
        private const val MOST_LOGINS_OFFERED = 10
    }
}

private fun androidx.lifecycle.LifecycleCoroutineScope.launchWhenCreatedCompat(block: suspend kotlinx.coroutines.CoroutineScope.() -> Unit) {
    this.launch { block() }
}
