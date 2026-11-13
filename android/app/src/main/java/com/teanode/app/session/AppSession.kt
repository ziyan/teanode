package com.teanode.app.session

import android.content.Context
import android.content.SharedPreferences
import android.os.Build
import com.teanode.kit.Account
import com.teanode.kit.AccountRing
import com.teanode.kit.AppSection
import com.teanode.kit.ConnectLink
import com.teanode.kit.Connection
import com.teanode.kit.HTTPTransport
import com.teanode.kit.Pkce
import com.teanode.kit.SecretAccountStore
import com.teanode.kit.SecretStore
import com.teanode.kit.SectionFacts
import com.teanode.kit.ServerAddress
import com.teanode.kit.ServerAddressError
import com.teanode.kit.ServerAddressException
import com.teanode.kit.SignIn
import com.teanode.kit.SubscriptionTransport
import com.teanode.kit.TabBarChoice
import com.teanode.kit.TeaNodeError
import com.teanode.kit.accessSession
import com.teanode.kit.serviceCapabilities
import com.teanode.kit.username
import com.teanode.app.R
import com.teanode.app.ui.theme.AppearanceChoice
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable

/** PendingConnection is a sign-in waiting for the person to say it is the right server and the right account. */
data class PendingConnection(val account: Account)

/**
 * AppSession is the app's account and what it knows about the server: which accounts are signed in, the connection of
 * the one in use, the parts the server offers, and the sign-in in progress. It is the Android side of the iPhone app's
 * `AppModel` (the account, session and navigation parts); each area keeps its own model.
 *
 * One lives for the whole process (`TeaNodeApplication`). Screens read it through `LocalAppSession`.
 */
class AppSession(
    private val context: Context,
    private val secrets: SecretStore,
    private val transport: HTTPTransport,
    private val subscriptions: SubscriptionTransport?,
    private val preferences: SharedPreferences,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
    /** Demo mode: nothing here is a real sign-in, and nothing is kept. */
    val isDemo: Boolean = false,
) {
    private val store = SecretAccountStore(secrets)
    private val errorWords = ErrorWords(context)
    private var factsJob: Job? = null

    private val accountRing = MutableStateFlow(AccountRing())

    /** Every account signed in on this phone, and the one in use. */
    val accounts: StateFlow<AccountRing> = accountRing.asStateFlow()

    private val activeConnection = MutableStateFlow<Connection?>(null)

    /** The connection of the account in use; null while signed out. */
    val connection: StateFlow<Connection?> = activeConnection.asStateFlow()

    private val currentFacts = MutableStateFlow(SectionFacts())

    /** Which parts the server offers this person, as far as it has said. */
    val facts: StateFlow<SectionFacts> = currentFacts.asStateFlow()

    private val hasReadFactsFlag = MutableStateFlow(false)

    /** Whether the server has said once what it offers, so a screen can tell "not offered" from "not known yet". */
    val hasReadFacts: StateFlow<Boolean> = hasReadFactsFlag.asStateFlow()

    private val signedOutMessage = MutableStateFlow<String?>(null)

    /** Why the app was signed out, when the server ended the sign-in. */
    val signedOutText: StateFlow<String?> = signedOutMessage.asStateFlow()

    private val connectingFlag = MutableStateFlow(false)

    /** Whether a sign-in is being finished, for the "Connecting" cover on the sign-in screen. */
    val isConnecting: StateFlow<Boolean> = connectingFlag.asStateFlow()

    private val signInProblem = MutableStateFlow<String?>(null)

    /** What is wrong with the sign-in the person tried, said under the address field. */
    val problemText: StateFlow<String?> = signInProblem.asStateFlow()

    private val pending = MutableStateFlow<PendingConnection?>(null)

    /** A sign-in from a connect code, waiting for the person to confirm it is the right server and account. */
    val pendingConnection: StateFlow<PendingConnection?> = pending.asStateFlow()

    private val openedConnectLink = MutableStateFlow<ConnectLink?>(null)

    /** A connect link the phone was opened with, before the sign-in screen has used it. */
    val openedLink: StateFlow<ConnectLink?> = openedConnectLink.asStateFlow()

    private val addingAccount = MutableStateFlow(false)

    /** Whether the sign-in screen is shown over the signed-in app, to add another account. */
    val isAddingAccount: StateFlow<Boolean> = addingAccount.asStateFlow()

    private val tabBarChoice = MutableStateFlow(TabBarChoice.sections(preferences.getString(TabBarChoice.STORAGE_KEY, TabBarChoice.defaultText).orEmpty()))

    /** The parts the person keeps in the bottom bar, in their order. */
    val tabBarSections: StateFlow<List<AppSection>> = tabBarChoice.asStateFlow()

    private val waitingCounts = MutableStateFlow<Map<AppSection, Int>>(emptyMap())

    /** How many things wait for the person in each part (unread chats, coding sessions that ask): the badges on the bar and the drawer. */
    val badgeCounts: StateFlow<Map<AppSection, Int>> = waitingCounts.asStateFlow()

    private val appearanceChoice = MutableStateFlow(AppearanceChoice.fromStoredName(preferences.getString(APPEARANCE_KEY, null)))

    /** Light, dark or the system's, as chosen in Settings. */
    val appearance: StateFlow<AppearanceChoice> = appearanceChoice.asStateFlow()

    /** Opens the server's sign-in page in a browser tab; set by the activity, which owns the window. */
    var openSignInPage: ((String) -> Unit)? = null

    // MARK: Starting up

    /** Takes the account in use from the phone's storage; run once as the app starts. */
    fun restore() {
        if (activeConnection.value != null) return
        val ring = runCatching { store.accounts() }.getOrDefault(AccountRing())
        accountRing.value = ring
        ring.active?.let { account -> use(account) }
    }

    /** Puts an account to use: the whole app is drawn again for it. */
    private fun use(account: Account) {
        factsJob?.cancel()
        currentFacts.value = SectionFacts()
        hasReadFactsFlag.value = false
        activeConnection.value = Connection(account, transport, store, subscriptions)
        accountRing.value = runCatching { store.accounts() }.getOrDefault(AccountRing(listOf(account), account.accountKey))
        refreshFacts()
    }

    /** Asks the server what it offers this person and which permissions they hold. Run again to refresh. */
    fun refreshFacts() {
        val asked = activeConnection.value ?: return
        factsJob?.cancel()
        factsJob = scope.launch {
            val services = async { runCatching { asked.serviceCapabilities() } }
            val session = async { runCatching { asked.accessSession() } }
            val servicesOutcome = services.await()
            val sessionOutcome = session.await()
            if (activeConnection.value !== asked) return@launch
            servicesOutcome.exceptionOrNull()?.let { error -> signOutIfEnded(asked, error) }
            currentFacts.update { facts ->
                facts.copy(services = servicesOutcome.getOrNull() ?: facts.services, session = sessionOutcome.getOrNull() ?: facts.session)
            }
            hasReadFactsFlag.value = servicesOutcome.isSuccess
        }
    }

    /** Coding is offered once a computer runs coding sessions; the Coding part says so when it finds one. */
    fun noteCodingComputers() = currentFacts.update { facts -> facts.copy(hasCodingComputers = true) }

    /** Recordings is offered once the account has recorded or been shown a recording. */
    fun noteRecordingOwner() = currentFacts.update { facts -> facts.copy(hasRecordingOwner = true) }

    // MARK: Signing in

    /** Signs in with a password on the server's own page, in a browser tab; the page returns to [completeSignIn]. */
    fun startPasswordSignIn(serverText: String) {
        val server = try {
            ServerAddress.parse(serverText)
        } catch (failure: ServerAddressException) {
            signInProblem.value = addressProblemText(failure.reason)
            return
        }
        signInProblem.value = null
        scope.launch {
            connectingFlag.value = true
            try {
                val signIn = SignIn(server, transport)
                val clientId = signIn.register()
                val verifier = Pkce.randomText()
                val state = Pkce.randomText()
                // The code the page returns is worth nothing without the verifier this keeps; the process may be
                // stopped while the person types their password, so it is kept in the encrypted store.
                secrets.write(PENDING_SIGN_IN, Json.encodeToString(PendingSignIn.serializer(), PendingSignIn(server.toString(), clientId, verifier, state)).toByteArray())
                val address = signIn.authorizationAddress(clientId, Pkce.challenge(verifier), state)
                val launcher = openSignInPage ?: error("no browser to sign in with")
                launcher(address)
            } catch (error: Throwable) {
                if (!ErrorWords.isCancellation(error)) signInProblem.value = describe(error)
            } finally {
                connectingFlag.value = false
            }
        }
    }

    /** The server's sign-in page returned to the app with a code (or without one, when the person closed it). */
    fun completeSignIn(returned: String) {
        val kept = runCatching { Json.decodeFromString(PendingSignIn.serializer(), secrets.read(PENDING_SIGN_IN)!!.decodeToString()) }.getOrNull() ?: return
        secrets.delete(PENDING_SIGN_IN)
        val code = SignIn.authorizationCode(returned, kept.state)
        if (code == null) {
            signInProblem.value = context.getString(R.string.signin_not_signed_in)
            return
        }
        scope.launch {
            connectingFlag.value = true
            try {
                val server = ServerAddress.parse(kept.server)
                val account = SignIn(server, transport).exchangeAuthorizationCode(code, kept.verifier, kept.clientId)
                confirm(PendingConnection(account.copy(username = usernameOf(account))))
            } catch (error: Throwable) {
                if (!ErrorWords.isCancellation(error)) signInProblem.value = describe(error)
            } finally {
                connectingFlag.value = false
            }
        }
    }

    /** Takes a connect code the camera read or the person pasted. Nothing is kept until the person confirms. */
    fun connect(text: String) {
        val link = ConnectLink.parse(text)
        if (link == null) {
            signInProblem.value = context.getString(R.string.signin_not_a_link)
            return
        }
        val code = link.connectCode
        if (code == null) {
            signInProblem.value = context.getString(R.string.signin_link_has_no_code)
            return
        }
        scope.launch {
            connectingFlag.value = true
            try {
                val signIn = SignIn(link.server, transport)
                val clientId = signIn.register()
                val account = signIn.exchangeConnectCode(code, clientId, Build.MODEL ?: "Android")
                pending.value = PendingConnection(account.copy(username = usernameOf(account)))
                signInProblem.value = null
            } catch (error: Throwable) {
                if (!ErrorWords.isCancellation(error)) signInProblem.value = describe(error)
            } finally {
                connectingFlag.value = false
            }
        }
    }

    /** Asks a server being added whose its new sign-in is. That server ending the sign-in it just gave is its refusal, said as one. */
    private suspend fun usernameOf(account: Account): String = try {
        Connection(account, transport).username()
    } catch (error: TeaNodeError.SignedOut) {
        throw TeaNodeError.Refused(context.getString(R.string.signin_server_did_not_accept, account.server.host))
    }

    /** Keeps a sign-in the person confirmed. */
    fun confirm(connecting: PendingConnection) {
        pending.value = null
        store.add(connecting.account)
        addingAccount.value = false
        signedOutMessage.value = null
        openedConnectLink.value = null
        use(connecting.account)
    }

    /** Gives up a sign-in the person chose not to keep. */
    fun decline(connecting: PendingConnection) {
        pending.value = null
        scope.launch { SignIn(connecting.account.server, transport).revoke(connecting.account) }
    }

    /** Takes a link the phone was opened with. Signed in already, a connect code adds an account beside the others. */
    fun open(link: ConnectLink) {
        openedConnectLink.value = link
        if (activeConnection.value != null) addingAccount.value = true
    }

    /** The sign-in screen has used the link opened with the phone. */
    fun linkWasUsed() {
        openedConnectLink.value = null
    }

    fun startAddingAccount() {
        signInProblem.value = null
        addingAccount.value = true
    }

    fun stopAddingAccount() {
        openedConnectLink.value = null
        addingAccount.value = false
    }

    fun clearProblem() {
        signInProblem.value = null
    }

    fun notALinkText(): String = context.getString(R.string.signin_not_a_link)

    fun showProblem(text: String) {
        signInProblem.value = text
    }

    // MARK: Accounts

    /** Puts another sign-in to use. */
    fun switchTo(account: Account) {
        if (account.accountKey == activeConnection.value?.accountKey) return
        store.activate(account.accountKey)
        use(account)
    }

    /**
     * Signs out of the account in use, on the server too unless the server already ended the sign-in, and puts the
     * next one to use.
     */
    fun signOut(endedBecause: String? = null) {
        val leaving = activeConnection.value?.account
        scope.launch {
            if (leaving != null && endedBecause == null) SignIn(leaving.server, transport).revoke(leaving)
        }
        val gone = leaving?.server?.host.orEmpty()
        runCatching { store.delete() }
        val next = runCatching { store.load() }.getOrNull()
        factsJob?.cancel()
        currentFacts.value = SectionFacts()
        hasReadFactsFlag.value = false
        if (next != null) {
            use(next)
            if (endedBecause != null) signedOutMessage.value = context.getString(R.string.signin_signed_out_of, gone, endedBecause)
            return
        }
        activeConnection.value = null
        accountRing.value = AccountRing()
        signedOutMessage.value = endedBecause
    }

    /** Signs out of an account that is not the one in use. */
    fun signOut(other: Account) {
        if (other.accountKey == activeConnection.value?.accountKey) {
            signOut()
            return
        }
        scope.launch { SignIn(other.server, transport).revoke(other) }
        store.remove(other.accountKey)
        accountRing.value = store.accounts()
    }

    /**
     * What to show for an error from the server. An ended sign-in of the account in use signs the app out of it, since
     * nothing else will work until it connects again.
     */
    fun describe(error: Throwable): String {
        val asked = activeConnection.value
        if (error is TeaNodeError.SignedOut && asked != null) signOutIfEnded(asked, error)
        return errorWords.describe(error)
    }

    /** Why a read failed, in words that say what to do next. */
    fun describeLoadProblem(error: Throwable): String = errorWords.loadProblem(error)

    private fun signOutIfEnded(asked: Connection, error: Throwable) {
        if (error !is TeaNodeError.SignedOut || !asked.isSignedOut || asked !== activeConnection.value) return
        signOut(endedBecause = errorWords.describe(error))
    }

    private fun addressProblemText(reason: ServerAddressError): String = context.getString(
        when (reason) {
            ServerAddressError.IsNotEncrypted -> R.string.signin_address_not_encrypted
            ServerAddressError.HasCredentials -> R.string.signin_address_has_credentials
            else -> R.string.signin_address_not_an_address
        },
    )

    // MARK: Preferences

    fun setTabBarSections(sections: List<AppSection>) {
        val chosen = TabBarChoice.sections(TabBarChoice.text(sections))
        tabBarChoice.value = chosen
        preferences.edit().putString(TabBarChoice.STORAGE_KEY, TabBarChoice.text(chosen)).apply()
    }

    /** Tells the shell how many things wait in a part; zero takes the badge away. */
    fun setBadgeCount(section: AppSection, count: Int) = waitingCounts.update { counts ->
        if (count > 0) counts + (section to count) else counts - section
    }

    fun setAppearance(choice: AppearanceChoice) {
        appearanceChoice.value = choice
        preferences.edit().putString(APPEARANCE_KEY, choice.storedName).apply()
    }

    /** Puts the demo account to use, with every part of the app offered. */
    fun enterDemo(account: Account, facts: SectionFacts) {
        factsJob?.cancel()
        store.add(account)
        accountRing.value = store.accounts()
        activeConnection.value = Connection(account, transport, store, subscriptions)
        currentFacts.value = facts
        hasReadFactsFlag.value = true
    }

    @Serializable
    private class PendingSignIn(val server: String, val clientId: String, val verifier: String, val state: String)

    private companion object {
        const val PENDING_SIGN_IN = "pendingSignIn"
        const val APPEARANCE_KEY = "appearance"
        val Json = com.teanode.kit.TeaNodeJson
    }
}
