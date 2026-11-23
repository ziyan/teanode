package com.teanode.app.location

import android.content.Context
import com.teanode.kit.Connection
import kotlinx.coroutines.runBlocking
import okhttp3.Interceptor
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.maplibre.android.MapLibre
import org.maplibre.android.module.http.HttpRequestUtil

/** The path of the server's map style: the server fetches the style and every file it names from the map host. */
internal const val MAP_STYLE_PATH = "/api/v1/map/style"

/**
 * ServerMapRequests is how the map asks for its files: only from the server of the account in use, with that account's
 * token, as the dashboard does. The server fetches the style, tiles, fonts and icons from the configured map host and
 * rewrites the style to name itself, so the phone never talks to the map host or to anyone else. A request for any
 * other address is refused here rather than sent.
 */
internal object ServerMapRequests {
    /** The connection of the account whose server the map is read from. */
    @Volatile
    var connection: Connection? = null

    private var isInstalled = false

    /** Starts MapLibre with this client, once for the app. */
    @Synchronized
    fun install(context: Context) {
        if (isInstalled) return
        MapLibre.getInstance(context.applicationContext)
        HttpRequestUtil.setOkHttpClient(client())
        isInstalled = true
    }

    /** The client every map file is asked for with. */
    internal fun client(): OkHttpClient =
        OkHttpClient.Builder()
            // A redirect could lead anywhere; the server's map answers without one.
            .followRedirects(false)
            .followSslRedirects(false)
            .addInterceptor(::onlyToTheServer)
            .build()

    private fun onlyToTheServer(chain: Interceptor.Chain): Response {
        val request = chain.request()
        val current = connection ?: return refused(chain, 404, "signed out")
        val server = current.account.server.endpoint("/")
        val requested = request.url
        if (requested.scheme != server.scheme || requested.host != server.host || requested.port != server.port) {
            return refused(chain, 404, "not a file of this server's map")
        }
        val token = try {
            runBlocking { current.accessToken() }
        } catch (error: Exception) {
            return refused(chain, 401, "signed out")
        }
        return chain.proceed(request.newBuilder().header("Authorization", "Bearer $token").build())
    }

    private fun refused(chain: Interceptor.Chain, statusCode: Int, message: String): Response =
        Response.Builder()
            .request(chain.request())
            .protocol(Protocol.HTTP_1_1)
            .code(statusCode)
            .message(message)
            .body(ByteArray(0).toResponseBody(null))
            .build()
}
