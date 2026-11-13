package com.teanode.app

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import com.teanode.app.calls.OpeningRequests
import com.teanode.app.demo.DemoServer
import com.teanode.app.notifications.NotificationBuilder
import com.teanode.app.notifications.NotificationOpening
import com.teanode.app.notifications.OwnIntentSeal
import com.teanode.app.ui.components.openWebPage
import com.teanode.kit.ConnectLink
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.appRedirectScheme
import com.teanode.kit.notifications.NotificationInfo
import com.teanode.kit.notifications.NotificationPress
import kotlinx.coroutines.flow.MutableSharedFlow

/**
 * MainActivity is the app's one window. It hands the links Android opens it with to the session: the server's sign-in
 * page returning with a code (`com.teanode.app:/oauth`), a connect code (`https://teanode.com/app/connect#...`), and the
 * app's own places (`teanode://app/...`).
 */
class MainActivity : ComponentActivity() {
    private val newIntents = MutableSharedFlow<Intent>(extraBufferCapacity = 8)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        val application = application as TeaNodeApplication
        // Demo mode answers from invented data instead of a server, for screenshots and trying a screen without one.
        val isDemo = BuildConfig.DEBUG && intent.getBooleanExtra(DemoServer.EXTRA_DEMO, false)
        if (isDemo) application.enterDemo()
        val session = application.session
        session.openSignInPage = { address -> openWebPage(this, address, isEphemeral = true) }
        if (savedInstanceState == null) handle(intent)
        val startRoute = if (isDemo) intent.getStringExtra(DemoServer.EXTRA_ROUTE) else null
        setContent { AppRoot(session = session, startRoute = startRoute, newIntents = newIntents) }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handle(intent)
    }

    private fun openedFromNotification(infoText: String, actionId: String?) {
        val info = runCatching { TeaNodeJson.decodeFromString(NotificationInfo.serializer(), infoText) }.getOrNull() ?: return
        val application = application as TeaNodeApplication
        val kept = application.push.accountStore.accounts()
        val press = NotificationPress.decide(info, actionId ?: NotificationPress.DEFAULT_ACTION, null, kept.accounts.map { account -> account.accountKey })
        when (press) {
            is NotificationPress.Open -> {
                kept.accounts.firstOrNull { account -> account.accountKey == press.accountKey }?.let { account -> application.session.switchTo(account) }
                NotificationOpening.open(press.destination)
            }
            is NotificationPress.UnknownAccount -> NotificationOpening.tell(
                getString(if (press.doesNameAccount) R.string.notifications_account_gone else R.string.notifications_account_unknown),
            )
            else -> Unit
        }
    }

    private fun handle(intent: Intent) {
        // A notification, or one of its buttons that opens the app, pressed: the kit decides where it goes.
        // Another app can start this activity with any extras: only an intent the app built for itself is believed.
        if (OwnIntentSeal.isOwn(this, intent)) intent.getStringExtra(NotificationBuilder.EXTRA_INFO)?.let { text -> openedFromNotification(text, intent.getStringExtra(NotificationBuilder.EXTRA_ACTION)) }
        if (intent.getBooleanExtra("com.teanode.app.extra.CALL_AGENT", false)) OpeningRequests.offerCall()
        val address: Uri = intent.data ?: return
        val session = (application as TeaNodeApplication).session
        when {
            address.scheme == appRedirectScheme -> session.completeSignIn(address.toString())
            address.scheme == "https" -> ConnectLink.parse(address.toString())?.let { link -> session.open(link) }
            address.scheme == "teanode" -> newIntents.tryEmit(intent)
        }
    }
}
