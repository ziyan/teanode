package com.teanode.app

import android.app.Application
import com.teanode.app.demo.DemoSession
import com.teanode.app.session.AppSession
import com.teanode.app.session.KeystoreSecretStore
import com.teanode.kit.OkHttpSubscriptions
import com.teanode.kit.OkHttpTransport

/** TeaNodeApplication holds the one [AppSession] of the process. */
class TeaNodeApplication : Application() {
    lateinit var session: AppSession
        private set

    override fun onCreate() {
        super.onCreate()
        session = AppSession(
            context = this,
            secrets = KeystoreSecretStore(this),
            transport = OkHttpTransport(),
            subscriptions = OkHttpSubscriptions(),
            preferences = getSharedPreferences("teanode", MODE_PRIVATE),
        )
        session.restore()
    }

    /** Replaces the session with the demo's, signed in to the invented server (debug builds only). */
    fun enterDemo() {
        if (session.isDemo) return
        session = DemoSession.create(this)
    }
}
