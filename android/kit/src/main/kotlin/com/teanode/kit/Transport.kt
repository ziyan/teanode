package com.teanode.kit

import kotlinx.coroutines.suspendCancellableCoroutine
import okhttp3.Call
import okhttp3.Callback
import okhttp3.MediaType.Companion.toMediaTypeOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import java.io.File
import java.io.IOException
import java.util.concurrent.TimeUnit
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/** HTTPRequest is what the app sends the server. */
class HTTPRequest(
    val url: String,
    val method: String = "POST",
    val headers: Map<String, String> = emptyMap(),
    val body: ByteArray? = null,
    /** How long the whole call may take; null keeps the client's own limit. */
    val timeoutMS: Long? = null,
) {
    fun header(name: String): String? = headers.entries.firstOrNull { entry -> entry.key.equals(name, ignoreCase = true) }?.value

    fun with(header: String, headerValue: String): HTTPRequest =
        HTTPRequest(url, method, headers.filterKeys { name -> !name.equals(header, ignoreCase = true) } + (header to headerValue), body, timeoutMS)
}

/** HTTPResponse is what came back. */
class HTTPResponse(val statusCode: Int, val body: ByteArray = ByteArray(0)) {
    val text: String get() = body.toString(Charsets.UTF_8)
}

/** HTTPTransport sends a request and hands back what came back. The app uses OkHttp; tests stand in a server of their own. */
interface HTTPTransport {
    suspend fun send(request: HTTPRequest): HTTPResponse

    /**
     * Reads the answer to a request into a file. For an answer that may be hundreds of megabytes, which send would
     * hold whole in memory. A transport that has no streaming of its own, such as a test's, writes what send answered.
     */
    suspend fun download(request: HTTPRequest, destination: File): HTTPResponse {
        val response = send(request)
        destination.writeBytes(response.body)
        return HTTPResponse(response.statusCode)
    }
}

/** OkHttpTransport sends through OkHttp. */
class OkHttpTransport(val client: OkHttpClient = defaultClient) : HTTPTransport {
    override suspend fun send(request: HTTPRequest): HTTPResponse = call(request) { response ->
        HTTPResponse(response.code, response.body?.bytes() ?: ByteArray(0))
    }

    override suspend fun download(request: HTTPRequest, destination: File): HTTPResponse = call(request) { response ->
        response.body?.byteStream()?.use { input -> destination.outputStream().use { output -> input.copyTo(output) } }
        HTTPResponse(response.code)
    }

    private suspend fun <Answer> call(request: HTTPRequest, read: (Response) -> Answer): Answer {
        val prepared = client.newBuilder().apply {
            request.timeoutMS?.let { limit -> callTimeout(limit, TimeUnit.MILLISECONDS) }
        }.build()
        val call = prepared.newCall(build(request))
        return suspendCancellableCoroutine { continuation ->
            continuation.invokeOnCancellation { call.cancel() }
            call.enqueue(object : Callback {
                override fun onFailure(call: Call, e: IOException) {
                    if (continuation.isActive) continuation.resumeWithException(e)
                }

                override fun onResponse(call: Call, response: Response) {
                    response.use {
                        try {
                            continuation.resume(read(response))
                        } catch (failure: IOException) {
                            if (continuation.isActive) continuation.resumeWithException(failure)
                        }
                    }
                }
            })
        }
    }

    private fun build(request: HTTPRequest): Request {
        val contentType = request.header("Content-Type")?.toMediaTypeOrNull()
        val body = request.body?.toRequestBody(contentType)
            ?: if (request.method == "POST" || request.method == "PUT") ByteArray(0).toRequestBody(contentType) else null
        return Request.Builder().url(request.url).method(request.method, body).apply {
            request.headers.forEach { (name, headerValue) -> header(name, headerValue) }
        }.build()
    }

    companion object {
        val defaultClient: OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(60, TimeUnit.SECONDS)
            .writeTimeout(60, TimeUnit.SECONDS)
            .build()
    }
}

/** TeaNodeError is what can go wrong talking to a server, in words a person can act on. */
sealed class TeaNodeError(message: String) : Exception(message) {
    /** The server answered something this app cannot read. */
    data object UnreadableAnswer : TeaNodeError("The server answered something this app cannot read.")

    /** The server refused, with its reason. */
    data class Refused(val reason: String) : TeaNodeError(reason)

    /** The sign-in has ended: the person removed the phone, or signed it out. Connecting again is the way back. */
    data object SignedOut : TeaNodeError("This phone is no longer signed in. Connect it again.")

    /** The connect code was used, expired, or was made for another server. */
    data object ConnectCodeSpent : TeaNodeError("That code has been used or has expired. Show a new one on the dashboard.")

    /** A durable recording deletion refuses upload retries. */
    data object RecordingDeleted : TeaNodeError("This recording was deleted. Its upload cannot be resumed.")

    /**
     * Whether the server refused because what was asked for is not there, or is not the person's to see: it answers
     * both the same way ("api: not found"). Every other failure says nothing about whether the thing is there.
     */
    val isNotFound: Boolean
        get() = this is Refused && (reason.contains("api: not found") || reason.contains("db: not found"))
}

/** Whether this is the server answering that what was asked for is not there, rather than a call that did not get through. */
val Throwable.isNotFoundRefusal: Boolean get() = (this as? TeaNodeError)?.isNotFound ?: false

/**
 * The server's own reason when it answered and refused, since the reason says why; otherwise the sentence the caller
 * wrote for a call that did not get through, which says what to do next.
 */
fun Throwable.refusalReason(orSay: String): String {
    val known = this as? TeaNodeError.Refused
    return if (known != null && known.reason.isNotEmpty()) known.reason else orSay
}
