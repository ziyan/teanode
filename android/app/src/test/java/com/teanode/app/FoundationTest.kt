package com.teanode.app

import com.google.zxing.BarcodeFormat
import com.google.zxing.qrcode.QRCodeWriter
import com.teanode.app.demo.DemoServer
import com.teanode.app.demo.graphQLField
import com.teanode.app.shell.Areas
import com.teanode.app.shell.Routes
import com.teanode.app.signin.QrCodeDecoder
import com.teanode.app.ui.components.sizedPicturePath
import com.teanode.kit.AppSection
import com.teanode.kit.ConnectLink
import com.teanode.kit.Connection
import com.teanode.kit.ServerAddress
import com.teanode.kit.accessSession
import com.teanode.kit.serviceCapabilities
import com.teanode.kit.username
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

class RoutesTest {
    @Test
    fun aPartsPlacesAllStartWithItsName() {
        assertEquals("mail/home", Routes.home(AppSection.Mail))
        assertEquals("agent/home", Routes.home(AppSection.Agent))
        assertEquals("mail/thread/{threadId}", Routes.child(AppSection.Mail, "thread/{threadId}"))
        assertEquals("teanode://app/mail/home", Routes.deepLink("mail/home"))
    }

    @Test
    fun onlyAPartsFirstScreenCountsAsHome() {
        assertTrue(Routes.isHome("mail/home"))
        assertTrue(Routes.isHome(Routes.SEARCH))
        assertFalse(Routes.isHome("mail/thread/{threadId}"))
        assertFalse(Routes.isHome("mail/folder/home"), "a place that happens to be called home deeper in a part is not the first screen")
        assertFalse(Routes.isHome(null))
        assertEquals(AppSection.Mail, Routes.sectionOf("mail/thread/{threadId}"))
        assertNull(Routes.sectionOf("search/home"))
    }

    @Test
    fun everyPartHasOneAreaAndTheyAreInTheirOwnPackages() {
        val sections = Areas.all.mapNotNull { area -> area.section }
        assertEquals(AppSection.entries.toSet(), sections.toSet())
        assertEquals(sections.size, sections.toSet().size)
        assertEquals(1, Areas.all.count { area -> area.section == null }, "search is the one area outside a part")
    }
}

class DemoServerTest {
    private val connection = Connection(DemoServer.account, DemoServer.transport())

    @Test
    fun theDemoServerSaysWhoIsSignedInAndWhatItOffers() = runBlocking {
        assertEquals("robin", connection.username())
        assertTrue(connection.accessSession().holds("journal:use"))
        assertTrue(connection.serviceCapabilities().isMailboxEnabled)
    }

    @Test
    fun aDocumentNobodyAnswersIsRefusedByName() {
        val failure = org.junit.jupiter.api.Assertions.assertThrows(com.teanode.kit.TeaNodeError.Refused::class.java) {
            runBlocking { connection.execute<Map<String, String>>("query { ListGoals { id } }") }
        }
        assertTrue(failure.reason.contains("ListGoals"), failure.reason)
    }

    @Test
    fun aFieldIsMatchedAsAWholeWord() {
        val answerer = graphQLField("ReadAgent", """{"name":"Tea"}""")
        val transport = com.teanode.app.demo.DemoTransport(listOf(answerer))
        val asked = { document: String ->
            runBlocking {
                String(
                    transport.send(
                        com.teanode.kit.HTTPRequest(
                            "https://demo.example.com/api/v1/graphql", "POST", emptyMap(),
                            """{"query":${kotlinx.serialization.json.JsonPrimitive(document)},"variables":{}}""".toByteArray(),
                        ),
                    ).body,
                )
            }
        }
        assertTrue(asked("query { ReadAgent { name } }").contains(""""ReadAgent":{"name":"Tea"}"""))
        assertTrue(asked("query { ReadAgentSettings { name } }").contains("errors"), "a longer name is another field")
    }
}

class QrCodeDecoderTest {
    private fun luminanceOf(text: String, size: Int): ByteArray {
        val matrix = QRCodeWriter().encode(text, BarcodeFormat.QR_CODE, size, size)
        return ByteArray(size * size) { position -> if (matrix.get(position % size, position / size)) 0 else 255.toByte() }
    }

    @Test
    fun aConnectCodeIsReadBackFromACameraFrame() {
        val link = ConnectLink(ServerAddress.parse("mail.example.com:10443"), "abc123").url
        val text = QrCodeDecoder().decode(luminanceOf(link, 400), 400, 400)
        assertEquals(link, text)
        assertNotNull(ConnectLink.parse(text!!))
    }

    @Test
    fun aFrameWithNoCodeSaysNothing() {
        assertNull(QrCodeDecoder().decode(ByteArray(200 * 200) { 128.toByte() }, 200, 200))
    }

    @Test
    fun aFramesRowPaddingIsSkipped() {
        val width = 300
        val stride = 320
        val plain = luminanceOf("hello", width)
        val padded = ByteArray(stride * width)
        for (row in 0 until width) System.arraycopy(plain, row * width, padded, row * stride, width)
        assertEquals("hello", QrCodeDecoder().decode(padded, width, width, stride))
    }
}

class AvatarPictureTest {
    @Test
    fun theSmallPictureServesEverythingUpTo64Pixels() {
        assertEquals("/api/v1/avatar/u1?v=3&size=64", sizedPicturePath("/api/v1/avatar/u1?v=3", 64f))
        assertEquals("/api/v1/avatar/u1?size=512", sizedPicturePath("/api/v1/avatar/u1", 120f))
    }
}

/** Every string has a translation in each language, with the same placeholders, so an area cannot ship English only. */
class StringsTest {
    private val resources = File("src/main/res")

    private fun strings(folder: String): Map<String, String> {
        val result = HashMap<String, String>()
        val files = File(resources, folder).listFiles { file -> Regex("(.*_)?strings\\.xml").matches(file.name) }.orEmpty().toList()
        for (file in files) {
            val document = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(file)
            val nodes = document.getElementsByTagName("string")
            for (position in 0 until nodes.length) {
                val node = nodes.item(position)
                result[node.attributes.getNamedItem("name").nodeValue] = node.textContent
            }
        }
        return result
    }

    private fun placeholders(text: String): List<String> = Regex("%(\\d+\\$)?[sd]").findAll(text).map { match -> match.value.replace(Regex("^%\\d+\\$"), "%") }.sorted().toList()

    @Test
    fun everyLanguageHasEveryString() {
        val english = strings("values")
        assertTrue(english.isNotEmpty())
        for (folder in listOf("values-zh-rCN", "values-ja")) {
            val translated = strings(folder)
            assertEquals(english.keys.sorted(), translated.keys.sorted(), "strings missing or extra in $folder")
            for ((name, text) in english) assertEquals(placeholders(text), placeholders(translated.getValue(name)), "placeholders of $name in $folder")
        }
    }

    @Test
    fun noStringUsesADash() {
        for (folder in listOf("values", "values-zh-rCN", "values-ja")) {
            for ((name, text) in strings(folder)) {
                assertFalse(text.contains('–') || text.contains('—'), "$name in $folder has an en or em dash")
            }
        }
    }
}
