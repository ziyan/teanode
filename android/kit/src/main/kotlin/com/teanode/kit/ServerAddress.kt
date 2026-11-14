package com.teanode.kit

import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/**
 * ServerAddress is the TeaNode server a person typed or scanned, made into the address the app talks to: a scheme, a
 * host and perhaps a port, and nothing else.
 *
 * People type `mail.example.com`, paste the dashboard's address with a page on the end, or scan a link that names the
 * server. All of these mean the same server.
 */
@Serializable(with = ServerAddressSerializer::class)
class ServerAddress private constructor(
    /** The server's origin, such as `https://mail.example.com:10443`. */
    val origin: HttpUrl,
) {
    /** The host alone, as a person reads it: "mail.example.com". */
    val host: String get() = origin.host

    /** The address of one of the server's endpoints, such as `/api/v1/graphql`. A query after "?" stays a query. */
    fun endpoint(path: String): HttpUrl {
        val pathPart = path.substringBefore('?')
        val queryPart = if (path.contains('?')) path.substringAfter('?') else null
        val builder = origin.newBuilder().encodedPath(if (pathPart.startsWith("/")) pathPart else "/$pathPart")
        if (queryPart != null) builder.encodedQuery(queryPart)
        return builder.build()
    }

    /** The websocket address of one of the server's endpoints: `wss` for `https`, `ws` for `http`. */
    fun websocketEndpoint(path: String): String {
        val written = endpoint(path).toString()
        return if (origin.scheme == "http") "ws" + written.removePrefix("http") else "wss" + written.removePrefix("https")
    }

    override fun toString(): String = origin.toString().removeSuffix("/")

    override fun equals(other: Any?): Boolean = other is ServerAddress && other.origin == origin

    override fun hashCode(): Int = origin.hashCode()

    companion object {
        /**
         * Reads what a person typed. Without a scheme it is taken to be https. Plain http is refused except on the
         * person's own machine, where a development server has no certificate.
         */
        fun parse(text: String): ServerAddress {
            val trimmed = text.trim()
            if (trimmed.isEmpty()) throw ServerAddressException(ServerAddressError.IsEmpty)
            val written = if (trimmed.contains("://")) trimmed else "https://$trimmed"
            val parsed = written.toHttpUrlOrNull() ?: throw ServerAddressException(ServerAddressError.IsNotAnAddress)
            val host = parsed.host.lowercase()
            if (host.isEmpty() || host.contains(" ") || host.startsWith(".") || host.endsWith(".")) {
                throw ServerAddressException(ServerAddressError.IsNotAnAddress)
            }
            if (parsed.username.isNotEmpty() || parsed.password.isNotEmpty()) {
                throw ServerAddressException(ServerAddressError.HasCredentials)
            }
            if (parsed.scheme == "http" && !isLoopback(host)) {
                throw ServerAddressException(ServerAddressError.IsNotEncrypted)
            }
            // Only where the server is: a page, a query or a fragment someone pasted along with it is not part of it.
            // HttpUrl drops a port that is the scheme's default by itself.
            val origin = HttpUrl.Builder().scheme(parsed.scheme).host(host).port(parsed.port).build()
            return ServerAddress(origin)
        }

        fun parseOrNull(text: String): ServerAddress? = try {
            parse(text)
        } catch (_: ServerAddressException) {
            null
        }

        internal fun isLoopback(host: String): Boolean =
            host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]" || host.endsWith(".localhost")
    }
}

/** ServerAddressError is why what a person typed is not a server's address. */
enum class ServerAddressError { IsEmpty, IsNotAnAddress, IsNotEncrypted, HasCredentials }

class ServerAddressException(val reason: ServerAddressError) : Exception(reason.name)

object ServerAddressSerializer : KSerializer<ServerAddress> {
    override val descriptor = PrimitiveSerialDescriptor("ServerAddress", PrimitiveKind.STRING)
    override fun deserialize(decoder: Decoder): ServerAddress = ServerAddress.parse(decoder.decodeString())
    override fun serialize(encoder: Encoder, value: ServerAddress) = encoder.encodeString(value.toString())
}
