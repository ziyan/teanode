package com.teanode.app.mail

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.graphQLField
import com.teanode.app.demo.text
import com.teanode.kit.mail.ServerTime
import kotlinx.serialization.json.JsonObject
import java.time.Instant
import java.time.temporal.ChronoUnit

/**
 * The invented mail of demo mode: one mailbox with its folders, a handful of conversations that show every mark a row can
 * carry, the messages of a conversation, a mailing list, rules and a mail program's password. Everything is made up.
 */
object MailDemoData {
    private val now: Instant = Instant.now()

    private fun hoursAgo(hours: Long): String = ServerTime.text(now.minus(hours, ChronoUnit.HOURS))
    private fun daysAgo(days: Long): String = ServerTime.text(now.minus(days, ChronoUnit.DAYS))

    val mailboxes: String = """
        [{"mailbox":{"id":"mailbox-1","name":"Alex Example","signatureText":"Alex","signatureHtml":"",
          "addresses":[{"address":"alex@example.com","domain":"example.com","domainId":"domain-1"},{"address":"alex.work@example.com","domain":"example.com","domainId":"domain-1"}],
          "autoReply":{"enabled":false,"from":null,"until":null,"subject":"","text":"","html":"","sameDomainOnly":false},
          "agent":{"granted":true,"draftReplies":true,"triage":{"enabled":true}},
          "rules":[
            {"name":"Receipts","enabled":true,"stop":false,"conditions":[{"field":"subject","header":"","operator":"contains","value":"receipt"}],"actions":[{"kind":"move","folderId":"folder-receipts","address":""}]},
            {"name":"Newsletters","enabled":false,"stop":true,"conditions":[{"field":"header","header":"List-Id","operator":"contains","value":"news"},{"field":"sender-known","header":null,"operator":null,"value":null}],"actions":[{"kind":"markRead","folderId":"","address":""},{"kind":"forward","folderId":"","address":"archive@example.com"}]}
          ]},
          "folders":[
            {"id":"folder-inbox","mailboxId":"mailbox-1","parentId":"","name":"Inbox","kind":"inbox","pinnedAt":null,"unread":3,"total":14,"noteCount":0},
            {"id":"folder-drafts","mailboxId":"mailbox-1","parentId":"","name":"Drafts","kind":"drafts","pinnedAt":null,"unread":0,"total":1,"noteCount":0},
            {"id":"folder-sent","mailboxId":"mailbox-1","parentId":"","name":"Sent","kind":"sent","pinnedAt":null,"unread":0,"total":22,"noteCount":0},
            {"id":"folder-archive","mailboxId":"mailbox-1","parentId":"","name":"Archive","kind":"archive","pinnedAt":null,"unread":0,"total":60,"noteCount":0},
            {"id":"folder-junk","mailboxId":"mailbox-1","parentId":"","name":"Junk","kind":"junk","pinnedAt":null,"unread":0,"total":2,"noteCount":0},
            {"id":"folder-trash","mailboxId":"mailbox-1","parentId":"","name":"Trash","kind":"trash","pinnedAt":null,"unread":0,"total":4,"noteCount":0},
            {"id":"folder-receipts","mailboxId":"mailbox-1","parentId":"","name":"Receipts","kind":"","pinnedAt":"2030-01-01T00:00:00Z","unread":1,"total":9,"noteCount":0},
            {"id":"folder-travel","mailboxId":"mailbox-1","parentId":"","name":"Travel","kind":"","pinnedAt":null,"unread":0,"total":5,"noteCount":0},
            {"id":"folder-trains","mailboxId":"mailbox-1","parentId":"folder-travel","name":"Trains","kind":"","pinnedAt":null,"unread":0,"total":2,"noteCount":0}
          ],"unread":4,"starredUnread":1,"priorityUnread":2,"maxMessageSize":26214400}]
    """.trimIndent()

    private const val GOOD_VERDICT = """{"dmarc":{"result":"pass"},"dkims":[],"spamFilter":{"score":0.4}}"""
    private const val BAD_VERDICT = """{"dmarc":{"result":"fail"},"dkims":[{"result":"fail"}],"spamFilter":{"score":9.1}}"""

    private fun item(
        id: String, mailId: String, seen: Boolean, flagged: Boolean, subject: String, from: String, fromName: String, receivedAt: String,
        summary: String, priority: String = "normal", needsReply: Boolean = false, attachments: Int = 0, verdict: String = GOOD_VERDICT,
        draft: Boolean = false, proposals: String = "[]", folderId: String = "folder-inbox",
    ): String = """
        {"id":"$id","folderId":"$folderId","mailId":"$mailId","seen":$seen,"flagged":$flagged,"answered":false,"forwarded":false,"draft":$draft,"addedAt":"$receivedAt","subscriptionId":"",
         "insight":{"category":"personal","priority":"$priority","needsReply":$needsReply,"summary":"$summary","actionItems":[],"notes":"","model":"demo","runId":"","proposals":$proposals},
         "mail":{"id":"$mailId","from":"$from","fromName":"$fromName","sender":"","subject":"$subject","recipients":["alex@example.com"],"receivedAt":"$receivedAt","size":4200,"attachmentCount":$attachments,
           "messageId":"<$mailId@example.org>","listKey":"","listName":"","logoDomain":"","authenticationResults":$verdict}}
    """.trimIndent()

    private fun thread(id: String, count: Int, unread: Int, flagged: Boolean, participants: String, item: String, hasDraft: Boolean = false): String =
        """{"threadId":"thread-$id","count":$count,"unread":$unread,"flagged":$flagged,"participants":$participants,"itemIds":["$id"],"hasDraft":$hasDraft,"item":$item}"""

    private val lunchItem: String
        get() = item("item-lunch", "mail-lunch", false, false, "Lunch on Friday?", "sam@example.org", "Sam Fixture", hoursAgo(1), "Sam suggests the noodle place at noon and asks if Friday still works.", "high", true)

    private val invoiceItem: String
        get() = item("item-invoice", "mail-invoice", false, true, "Your receipt for order 1042", "orders@example.org", "Example Books", hoursAgo(3), "Receipt for two books, paid by card.", attachments = 1)

    val inboxThreads: List<Pair<String, String>>
        get() = listOf(
            "item-lunch" to thread("item-lunch", 3, 1, false, """["Sam Fixture","Alex"]""", lunchItem),
            "item-invoice" to thread("item-invoice", 1, 1, true, """["Example Books"]""", invoiceItem),
            "item-phish" to thread(
                "item-phish", 1, 1, false, """["Account Team"]""",
                item("item-phish", "mail-phish", false, false, "Verify your account now", "support@examp1e-login.test", "Account Team", hoursAgo(5), "Asks for a password; the sender could not be verified.", verdict = BAD_VERDICT),
            ),
            "item-trip" to thread(
                "item-trip", 2, 0, false, """["Pat Sample","Alex"]""",
                item("item-trip", "mail-trip", true, false, "Train times for the trip", "pat@example.net", "Pat Sample", daysAgo(1), "Pat lists the 9:10 and 10:40 trains and asks which to book.", needsReply = true),
            ),
            "item-draft" to thread(
                "item-draft", 1, 0, false, """["Alex"]""", item("item-draft", "mail-draft", true, false, "Notes for Monday", "alex@example.com", "Alex", daysAgo(2), "", draft = true), hasDraft = true,
            ),
            "item-newsletter" to thread(
                "item-newsletter", 1, 0, false, """["Garden Weekly"]""",
                item("item-newsletter", "mail-newsletter", true, false, "This week in the garden", "news@example.net", "Garden Weekly", daysAgo(9), "Seasonal planting tips."),
            ),
        )

    private fun messageEntries(itemId: String): String {
        if (itemId == "item-invoice") return """[{"folderId":"folder-inbox","folderName":"Inbox","folderKind":"inbox","item":$invoiceItem}]"""
        val first = item(
            "item-lunch", "mail-lunch", false, false, "Lunch on Friday?", "sam@example.org", "Sam Fixture", hoursAgo(1), "Sam suggests the noodle place at noon and asks if Friday still works.", "high", true,
            proposals = """[{"kind":"event","because":"Friday at noon works for me","status":"","summary":"Lunch with Sam","starts":"2030-01-04T12:00:00Z","ends":"2030-01-04T13:00:00Z","location":"The noodle place","allDay":false,"name":"","organization":"","title":"","emails":[],"phones":[],"note":"","contactId":""}]""",
        )
        val second = item("item-lunch-2", "mail-lunch-2", true, false, "Re: Lunch on Friday?", "alex@example.com", "Alex", hoursAgo(20), "Alex asks where to meet.", folderId = "folder-sent")
        val third = item("item-lunch-3", "mail-lunch-3", true, false, "Lunch on Friday?", "sam@example.org", "Sam Fixture", hoursAgo(26), "Sam asks about lunch.")
        return """[
            {"folderId":"folder-inbox","folderName":"Inbox","folderKind":"inbox","item":$first},
            {"folderId":"folder-sent","folderName":"Sent","folderKind":"sent","item":$second},
            {"folderId":"folder-inbox","folderName":"Inbox","folderKind":"inbox","item":$third}]"""
    }

    fun threadView(itemId: String): String {
        val subject = if (itemId == "item-invoice") "Your receipt for order 1042" else "Lunch on Friday?"
        val summary = if (itemId == "item-invoice") "null" else """{"summary":"Sam proposes lunch on Friday at noon; Alex has asked where to meet.","messageCount":3,"stale":false,"pending":false}"""
        val sendAfter = ServerTime.text(now.plus(1, ChronoUnit.HOURS))
        val held = if (itemId == "item-lunch") """{"id":"reply-1","status":"held","subject":"Re: Lunch on Friday?","to":"sam@example.org","text":"Friday at noon works. See you at the noodle place.","sendAfter":"$sendAfter","draftItemId":"item-reply-draft"}""" else "null"
        return """{"threadId":"thread-$itemId","subject":"$subject","truncated":false,"summary":$summary,"heldReply":$held,"items":${messageEntries(itemId)}}"""
    }

    fun content(mailId: String): String = when (mailId) {
        "mail-invoice" -> """{"mailId":"mail-invoice","available":true,"text":"Thank you for your order.\n\nTwo books, 38.00 total, paid by card ending 4242.","html":"","hasRemoteContent":true,"imagesAllowed":false,"rawHeaders":"Received: from mail.example.org\nSubject: Your receipt for order 1042","headers":[{"key":"Reply-To","value":"orders@example.org"}],"attachments":[{"index":0,"filename":"receipt-1042.pdf","contentType":"application/pdf","size":48213,"inline":false}]}"""
        "mail-lunch-2" -> """{"mailId":"mail-lunch-2","available":true,"text":"Sounds good. Where should we meet?\n\nAlex","html":"","hasRemoteContent":false,"imagesAllowed":false,"rawHeaders":"","headers":[],"attachments":[]}"""
        "mail-newsletter", "mail-newsletter-2" -> """{"mailId":"$mailId","available":true,"text":"This week: prune the roses, start the tomatoes indoors, and put the bulbs in the ground before the first frost.","html":"","hasRemoteContent":false,"imagesAllowed":false,"rawHeaders":"","headers":[],"attachments":[]}"""
        "mail-lunch-3" -> """{"mailId":"mail-lunch-3","available":true,"text":"Are you free for lunch this week?","html":"","hasRemoteContent":false,"imagesAllowed":false,"rawHeaders":"","headers":[],"attachments":[]}"""
        else -> """{"mailId":"$mailId","available":true,"text":"Hi Alex,\n\nHow about the noodle place on Friday at noon? It is a short walk from the office, and they take reservations for four.\n\nSam","html":"","hasRemoteContent":false,"imagesAllowed":false,"rawHeaders":"Received: from mail.example.org\nSubject: Lunch on Friday?\nFrom: Sam Fixture <sam@example.org>","headers":[{"key":"Reply-To","value":"sam@example.org"}],"attachments":[]}"""
    }

    val invitation: String = """{"id":"invitation-1","method":"REQUEST","summary":"Team lunch","location":"The noodle place","startsAt":"2030-01-04T12:00:00Z","endsAt":"2030-01-04T13:00:00Z","allDay":false,"cancelled":false,"organizer":"sam@example.org","participation":"NEEDS-ACTION","attendees":[]}"""

    val subscriptions: String
        get() = """
            {"total":2,"subscribed":2,"left":1,"subscriptions":[
              {"id":"sub-1","key":"garden","name":"Garden Weekly","from":"news@example.net","count":12,"unread":2,"lastAt":"${daysAgo(1)}","lastItemId":"item-newsletter","oneClick":true,"unsubscribe":["https://example.net/leave"],"logoDomain":"","requestedAt":null,"method":"","failed":false,"error":"","stripped":false,"mutedAt":null,"imagesAt":null},
              {"id":"sub-2","key":"deals","name":"Deals Digest","from":"deals@example.org","count":30,"unread":0,"lastAt":"${daysAgo(4)}","lastItemId":"item-deals","oneClick":false,"unsubscribe":[],"logoDomain":"","requestedAt":null,"method":"","failed":false,"error":"","stripped":true,"mutedAt":"${daysAgo(3)}","imagesAt":null}]}
        """.trimIndent()

    val leftSubscriptions: String
        get() = """
            {"total":1,"subscribed":2,"left":1,"subscriptions":[
              {"id":"sub-3","key":"coupons","name":"Coupon Club","from":"club@example.org","count":8,"unread":0,"lastAt":"${daysAgo(20)}","lastItemId":"item-coupons","oneClick":false,"unsubscribe":["mailto:leave@example.org"],"logoDomain":"","requestedAt":"${daysAgo(18)}","method":"mail","failed":true,"error":"the address bounced","stripped":false,"mutedAt":null,"imagesAt":null}]}
        """.trimIndent()

    val subscriptionRead: String
        get() = """
            {"threadId":"list-garden","subject":"Garden Weekly","truncated":false,"items":[
              {"folderId":"folder-inbox","folderName":"Inbox","folderKind":"inbox","item":${item("item-newsletter", "mail-newsletter", false, false, "This week in the garden", "news@example.net", "Garden Weekly", daysAgo(1), "Seasonal planting tips.")}},
              {"folderId":"folder-archive","folderName":"Archive","folderKind":"archive","item":${item("item-newsletter-2", "mail-newsletter-2", true, false, "Spring bulbs", "news@example.net", "Garden Weekly", daysAgo(8), "Which bulbs to plant now.", folderId = "folder-archive")}}]}
        """.trimIndent()

    val draft: String = """{"itemId":"item-draft","mailId":"mail-draft","from":"alex@example.com","to":["sam@example.org"],"cc":[],"bcc":[],"subject":"Notes for Monday","html":"<p>Short list for Monday.</p>","text":"Short list for Monday.\n\n-- \nAlex","replyToItemId":"","forwardItemId":"","attachments":[{"index":0,"filename":"agenda.txt","contentType":"text/plain","size":812,"inline":false}]}"""

    val appPasswords: String
        get() = """[{"id":"password-1","name":"Laptop mail program","createdAt":"${daysAgo(30)}","lastUsedAt":"${hoursAgo(2)}"},{"id":"password-2","name":"Tablet","createdAt":"${daysAgo(10)}","lastUsedAt":null}]"""

    val ruleTrials: String
        get() = """
            [{"matched":[0],"item":{"id":"item-invoice","mail":{"from":"orders@example.org","sender":"","subject":"Your receipt for order 1042","receivedAt":"${hoursAgo(3)}"}}},
             {"matched":[],"item":{"id":"item-lunch","mail":{"from":"sam@example.org","sender":"","subject":"Lunch on Friday?","receivedAt":"${hoursAgo(1)}"}}}]
        """.trimIndent()

    /** The conversations a list asks for, narrowed by what it sent. */
    fun threadPage(variables: JsonObject): String {
        val isPriority = variables.text("priority") == "high"
        val isFlagged = variables.text("flagged") == "true"
        val isUnread = variables.text("unread") == "true"
        val search = variables.text("search")?.lowercase().orEmpty()
        var rows = inboxThreads.map { pair -> pair.second }
        if (isPriority) rows = rows.filter { row -> row.contains("\"priority\":\"high\"") }
        if (isFlagged) rows = rows.filter { row -> row.contains("\"flagged\":true") }
        if (isUnread) rows = rows.filter { row -> !row.contains("\"unread\":0,") }
        if (search.isNotEmpty()) rows = rows.filter { row -> row.lowercase().contains(search) }
        val folderId = variables.text("folderId")
        if (folderId != null && folderId != "folder-inbox") rows = if (folderId == "folder-trash" || folderId == "folder-junk") emptyList() else rows.take(2)
        return """{"total":${rows.size},"threads":[${rows.joinToString(",")}]}"""
    }

    private fun itemOf(itemId: String): String {
        val row = inboxThreads.firstOrNull { pair -> pair.first == itemId }?.second ?: inboxThreads.first().second
        return row.substringAfter("\"item\":").dropLast(1)
    }

    val answerers: List<DemoAnswerer> = listOf(
        graphQLField("ListMailboxes") { _ -> mailboxes },
        graphQLField("ListMailboxThreads") { variables -> threadPage(variables) },
        graphQLField("GetMailboxThread") { variables -> threadView(variables.text("itemId").orEmpty()) },
        graphQLField("GetMailboxItem") { variables -> itemOf(variables.text("itemId").orEmpty()) },
        graphQLField("GetMailContent") { variables -> content(variables.text("mailId").orEmpty()) },
        graphQLField("GetMailInvitation") { variables -> if (variables.text("itemId") == "item-lunch") invitation else "null" },
        graphQLField("GetMailboxDraft", draft),
        graphQLField("ListMailboxSubscriptions") { variables -> if (variables.text("left") == "true") leftSubscriptions else subscriptions },
        graphQLField("ReadMailboxSubscription") { _ -> subscriptionRead },
        graphQLField("ListMailboxAppPasswords") { _ -> appPasswords },
        graphQLField("TestMailboxRules") { _ -> ruleTrials },
        graphQLField("ReadAgent", """{"categories":["personal","work","newsletter","receipt","other"]}"""),
        graphQLField("ListCalendars", """[{"id":"calendar-1","calendarKind":"events","shareLevel":null,"timezone":"UTC"}]"""),
        graphQLField("GetCalendarRequest", "null"),
        graphQLField("SetMailboxItemFlags", "1"),
        graphQLField("MoveMailboxItems", "[]"),
        graphQLField("DeleteMailboxItems", "1"),
        graphQLField("ReportMailboxJunk", "1"),
        graphQLField("EmptyMailboxTrash", "1"),
        graphQLField("SortMailboxItems", "1"),
        graphQLField("ShowMailboxItemImages", "1"),
        graphQLField("CancelAgentReply", """{"id":"reply-1","status":"cancelled"}"""),
        graphQLField("SaveMailboxDraft", """{"id":"item-draft"}"""),
        graphQLField("DraftReply", """{"text":"Friday at noon works for me."}"""),
        graphQLField("SendMailboxMessage", """{"mail":{"id":"mail-sent"},"item":{"id":"item-sent","folderId":"folder-sent"}}"""),
        graphQLField("AnswerMailInvitation", """{"id":"invitation-1","participation":"ACCEPTED"}"""),
        graphQLField("SetMailProposalStatus", """{"proposals":[{"status":"accepted"}]}"""),
        graphQLField("UpdateMailbox", """{"mailbox":{"id":"mailbox-1"}}"""),
        graphQLField("CreateMailboxFolder", """{"id":"folder-new"}"""),
        graphQLField("UpdateMailboxFolder", """{"id":"folder-travel"}"""),
        graphQLField("SetMailboxFolderPinned", """{"id":"folder-travel"}"""),
        graphQLField("DeleteMailboxFolder", "1"),
        graphQLField("CreateMailboxAppPassword", """{"password":"demo-password-0000","username":"alex@example.com","appPassword":{"id":"password-3","name":"Phone"}}"""),
        graphQLField("DeleteMailboxAppPassword", "1"),
        graphQLField("ShowMailboxSubscriptionImages", """{"key":"garden","imagesAt":null}"""),
        graphQLField("MuteMailboxSubscription", """{"key":"garden","mutedAt":null}"""),
        graphQLField("UnsubscribeMailboxSubscription", """{"key":"garden","requestedAt":null,"method":"","failed":false,"error":""}"""),
    )
}

/**
 * What demo mode answers for Mail: one `graphQLField("<Field>", """<json>""")` per document the screens send, so every
 * screen and state can be reached and photographed without a server. Most specific first.
 */
val mailDemoAnswerers: List<DemoAnswerer> = MailDemoData.answerers + mailManageDemoAnswerers
