package com.teanode.app

import android.app.Activity
import android.app.Application
import android.os.Bundle
import com.teanode.app.calls.CallService
import com.teanode.app.notifications.NotificationChannels
import com.teanode.app.notifications.PushSystem
import com.teanode.app.demo.DemoSession
import com.teanode.app.session.AppSession
import com.teanode.app.session.KeystoreSecretStore
import com.teanode.kit.OkHttpSubscriptions
import com.teanode.kit.OkHttpTransport
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch

/** TeaNodeApplication holds the one [AppSession] of the process. */
class TeaNodeApplication : Application() {
    lateinit var session: AppSession
        private set

    private val secrets by lazy { KeystoreSecretStore(this) }
    private val transport = OkHttpTransport()
    private val subscriptions = OkHttpSubscriptions()
    private var startedActivityCount = 0

    /**
     * What the phone does when a message arrives and nobody is looking at the app: the keys that open a notification,
     * the registration with the servers, the calls. Made when first asked for, by the messaging service or the app.
     */
    val push: PushSystem by lazy {
        PushSystem(
            context = this, secrets = secrets, transport = transport, preferences = getSharedPreferences("teanode", MODE_PRIVATE),
            subscriptions = subscriptions, sessionProvider = { session }, isAppInFront = { startedActivityCount > 0 },
        )
    }

    override fun onCreate() {
        super.onCreate()
        session = AppSession(
            context = this,
            secrets = secrets,
            transport = transport,
            subscriptions = subscriptions,
            preferences = getSharedPreferences("teanode", MODE_PRIVATE),
        )
        session.restore()
        Thread { runCatching { com.teanode.app.chats.ChatFilesCache.clean(this) } }.start()
        NotificationChannels.create(this)
        CallService.createChannels(this)
        // The widgets read the account in use: another account, or none, is drawn at once.
        kotlinx.coroutines.CoroutineScope(kotlinx.coroutines.SupervisorJob() + kotlinx.coroutines.Dispatchers.Main).launch {
            session.connection.map { connection -> connection?.accountKey }.distinctUntilChanged().drop(1).collect {
                com.teanode.app.widgets.WidgetRefresh.all(this@TeaNodeApplication, isForced = true)
            }
        }
        registerActivityLifecycleCallbacks(object : ActivityLifecycleCallbacks {
            override fun onActivityStarted(activity: Activity) {
                startedActivityCount += 1
            }

            override fun onActivityStopped(activity: Activity) {
                startedActivityCount -= 1
                // The app was left: what the person did in it (an event, mail read, a question answered) is on the widgets.
                if (startedActivityCount == 0) com.teanode.app.widgets.WidgetRefresh.all(this@TeaNodeApplication)
            }

            override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) {}
            override fun onActivityResumed(activity: Activity) {}
            override fun onActivityPaused(activity: Activity) {}
            override fun onActivitySaveInstanceState(activity: Activity, outState: Bundle) {}
            override fun onActivityDestroyed(activity: Activity) {}
        })
    }

    /** Replaces the session with the demo's, signed in to the invented server (debug builds only). */
    fun enterDemo() {
        if (session.isDemo) return
        session = DemoSession.create(this)
    }
}
