package com.teanode.app.demo

import android.content.Context
import com.teanode.app.session.AppSession
import com.teanode.app.session.MemorySecretStore
import com.teanode.kit.Connection
import com.teanode.kit.SubscriptionTransport
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flow
import kotlinx.serialization.json.JsonElement

/** The demo's live feed never connects, which the screens show as they would a slow network. */
object DemoSubscriptions : SubscriptionTransport {
    override fun subscribe(connection: Connection, document: String, variables: Map<String, JsonElement>): Flow<JsonElement> =
        flow { awaitCancellation() }
}

object DemoSession {
    /**
     * A session signed in to the invented server, with nothing kept on the phone. Every part is offered and every
     * document is answered from the parts' demo data (`<Domain>Demo.kt`).
     */
    fun create(context: Context): AppSession {
        val preferences = context.getSharedPreferences("teanode-demo", Context.MODE_PRIVATE).also { kept -> kept.edit().clear().apply() }
        val session = AppSession(
            context = context.applicationContext,
            secrets = MemorySecretStore(),
            transport = DemoServer.transport(),
            subscriptions = DemoSubscriptions,
            preferences = preferences,
            isDemo = true,
        )
        session.enterDemo(DemoServer.account, DemoServer.facts)
        return session
    }
}
