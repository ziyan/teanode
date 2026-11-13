package com.teanode.app.knowledge

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.DemoResponse
import com.teanode.app.demo.graphQLField
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant

private fun daysAgo(days: Long): String = Instant.now().minusSeconds(days * 86_400).toString()

private fun node(path: String, kind: String, name: String, summary: String, pinned: Boolean = false, contactId: String? = null, aliases: String = "[]", overview: String? = null): String =
    """{"id":"$path","path":"$path","kind":"$kind","name":"$name","summary":"$summary","aliases":$aliases,"pinned":$pinned,"dormant":false,"contactId":${contactId?.let { id -> "\"$id\"" }},
        "overview":${overview?.let { text -> "\"$text\"" }},"overviewWrittenAt":${if (overview != null) "\"${daysAgo(6)}\"" else "null"},"modifiedAt":"${daysAgo(2)}"}"""

private fun row(path: String, kind: String, name: String, summary: String, children: Int = 0, hint: String = "", pinned: Boolean = false): String =
    """{"node":${node(path, kind, name, summary, pinned)},"hint":"$hint","children":$children}"""

private val rootRows = listOf(
    row("self", "person", "", "Alex lives with Sam and works on a small team. Likes quiet mornings and long walks.", pinned = true),
    row("people", "folder", "People", "The people in Alex's life.", children = 5),
    row("projects", "folder", "Projects", "What Alex is working on.", children = 3),
    row("places", "folder", "Places", "Where things happen.", children = 2),
    row("garden", "thing", "The garden", "A small back garden with raised beds and a shed.", pinned = true),
    row("travel", "period", "Travel plans", "A long weekend at the lake in June."),
)

private fun pageJson(path: String): String {
    val isPerson = path.startsWith("people/")
    val name = when (path) {
        "people/sam-example" -> "Sam Example"
        "projects/garden-planner" -> "Garden planner"
        else -> path.substringAfterLast('/').replaceFirstChar { letter -> letter.uppercase() }
    }
    val kind = if (isPerson) "person" else "project"
    val summary = if (isPerson) "Sam lives with Alex. Works in a bookshop and cooks on Sundays." else "A plan for the beds: what goes where, and what to order."
    return """{"node":${node(path, kind, name, summary, pinned = path == "people/sam-example", contactId = if (isPerson) "card-1" else null, aliases = if (isPerson) "[\"Sammy\"]" else "[]", overview = "A longer story of **$name**, written from mail, conversations and the calendar. It is read about once a week.\\n\\n| Day | Plan |\\n| --- | --- |\\n| Monday | Dig the beds |\\n| Friday | Plant the seed potatoes |")},
        "facts":[
          {"id":"f1","number":1,"kind":"fact","text":"Lives with Alex in the flat above the bakery.","happenedAt":"2024-05","inferred":false,"evidence":[{"kind":"conversation","id":"c1","quote":"we moved in on a Saturday"}],"audiences":["triage","reply"]},
          {"id":"f2","number":2,"kind":"preference","text":"Prefers tea in the afternoon and coffee only before ten.","happenedAt":null,"inferred":true,"evidence":[{"kind":"mail","id":"m1","quote":""}],"audiences":[]},
          {"id":"f3","number":3,"kind":"decision","text":"Decided to repaint the kitchen in the spring.","happenedAt":"2026-10-12","inferred":false,"evidence":[{"kind":"calendar_event","id":"e1","quote":"kitchen paint day"}],"audiences":[]},
          {"id":"f4","number":4,"kind":"reflection","text":"Plans slip in spring when the garden needs attention.","happenedAt":null,"inferred":true,"evidence":[{"kind":"dream","id":"d1","quote":"reflection: pattern"},{"kind":"memory","id":"","quote":"projects/garden-planner#3"}],"audiences":[]},
          {"id":"f5","number":5,"kind":"fact","text":"Keeps the receipts in a blue folder.","happenedAt":null,"inferred":false,"evidence":[{"kind":"document","id":"doc7[page 2]","quote":""},{"kind":"document","id":"doc8","quote":""},{"kind":"document","id":"doc9","quote":""}],"audiences":[]}],
        "folded":[{"into":1,"fact":{"id":"f9","number":9,"text":"Moved in during May."}}],
        "children":[${node("$path/notes", "thing", "Notes", "Small things worth keeping.")}],
        "attachments":[{"documentId":"doc7","name":"folder.jpg","contentType":"image/jpeg","channel":"photos","path":""},{"documentId":"doc8","name":"seed-order.pdf","contentType":"application/pdf","channel":"mail","path":"/files/doc8"},{"documentId":"doc9","name":"bed-plan.docx","contentType":"application/vnd.openxmlformats-officedocument.wordprocessingml.document","channel":"mail","path":"/files/doc9"}],
        "contact":${if (isPerson) """{"id":"card-1","name":"Sam Example","emails":["sam@example.com"],"organization":""}""" else "null"},
        "recordLinks":[
          {"id":"l1","nodePath":"$path","nodeName":"$name","nodeKind":"$kind","recordKind":"mail","recordId":"m1","recordRelation":"wrote","linkStatus":"stated","passageStartMS":0,"passageEndMS":0,"transcriptRunId":"","note":"","happenedAt":null,"createdAt":"${daysAgo(9)}","recordTitle":"Dinner on Friday?","recordHappenedAt":"${daysAgo(3)}","dashboardPath":"/mailbox/starred/m1","isUnavailable":false},
          {"id":"l2","nodePath":"$path","nodeName":"$name","nodeKind":"$kind","recordKind":"calendar_event","recordId":"e1","recordRelation":"attended","linkStatus":"proposed","passageStartMS":0,"passageEndMS":0,"transcriptRunId":"","note":"","happenedAt":null,"createdAt":"${daysAgo(9)}","recordTitle":"Kitchen paint day","recordHappenedAt":"${daysAgo(10)}","dashboardPath":"/calendar?event=e1","isUnavailable":false},
          {"id":"l3","nodePath":"$path","nodeName":"$name","nodeKind":"$kind","recordKind":"recording_passage","recordId":"r1","recordRelation":"spoke_in","linkStatus":"stated","passageStartMS":1500,"passageEndMS":4000,"transcriptRunId":"","passageCount":12,"note":"","happenedAt":null,"createdAt":"${daysAgo(9)}","recordTitle":"Planning the week","recordHappenedAt":"${daysAgo(5)}","dashboardPath":"/recordings/r1","isUnavailable":false}],
        "recordLinkCount":5,
        "evidenceRecords":[{"factId":"f3","evidenceIndex":0,"recordKind":"calendar_event","recordId":"e1","passageStartMS":0,"passageEndMS":0,"transcriptRunId":"","recordTitle":"Kitchen paint day","dashboardPath":"/calendar?event=e1","isUnavailable":false}]}"""
}

private fun neighboursJson(path: String): String =
    """{"node":${node(path, "person", "Sam Example", "")},"parent":${node("people", "folder", "People", "The people in Alex's life.")},
        "neighbours":[
          {"node":${node("projects/garden-planner", "project", "Garden planner", "")},"relation":"works_on","outward":true,"note":"Plans the beds together","weight":1.0,"status":"stated"},
          {"node":${node("places/lake-house", "place", "Lake house", "")},"relation":"located_in","outward":false,"note":"","weight":0.5,"status":"proposed"}],
        "children":1,"links":4}"""

private fun sharedPage(path: String): String =
    """{"ownerUserId":"ada-id","ownerUsername":"ada","ownerName":"Ada Example","ownerAvatarUrl":null,"ownerAvatarColorIndex":2,"path":"$path","name":"A grandparent","nodeKind":"person","aliases":["Gran"],
        "opening":"Lives by the sea. **Keeps** a tidy garden.","isQuoteShared":true,
        "facts":[{"number":3,"factKind":"fact","text":"Next appointment is on Tuesday.","happenedAt":null,"isInferred":false,"evidenceSource":"from Ada's mail","evidenceQuote":"see you Tuesday at ten"},
                 {"number":4,"factKind":"preference","text":"Likes the window seat.","happenedAt":null,"isInferred":true,"evidenceSource":"from a conversation","evidenceQuote":""}],
        "edges":[{"fromPath":"$path","toPath":"people/a-grandparent/doctor","relation":"knows","isProposed":true}],
        "children":[{"path":"people/a-grandparent/doctor","name":"The doctor","opening":"Sees Gran every spring."}]}"""

/** What demo mode answers for Knowledge: folders and pages with facts, reflections, links and history, what others shared, a recall and a document. */
val knowledgeDemoAnswerers: List<DemoAnswerer> = listOf(
    graphQLField("AgentGraphChildren") { variables ->
        val path = (variables["path"] as? JsonPrimitive)?.content.orEmpty()
        if (path.isEmpty()) {
            """{"rows":[${rootRows.joinToString(",")}],"total":${rootRows.size}}"""
        } else {
            """{"rows":[${row("$path/sam-example", "person", "Sam Example", "Lives with Alex.", children = 1, hint = "Works in a bookshop")},${row("$path/ben-example", "person", "Ben Example", "A friend from the book club.")},
                ${row("$path/dana-example", "person", "Dana Example", "Sam's sister, lives two towns over.")}],"total":3}"""
        }
    },
    graphQLField("SearchAgentGraph") {
        """{"nodes":[${node("people/sam-example", "person", "Sam Example", "Lives with Alex.")},${node("projects/garden-planner", "project", "Garden planner", "")}],
            "facts":[{"fact":{"id":"f2","number":2,"text":"Prefers tea in the afternoon."},"path":"people/sam-example","name":"Sam Example"}],
            "moreNodeCount":0,"isMoreNodeCountLowerBound":false,"moreFactCount":0,"isMoreFactCountLowerBound":false,"nextOffset":0}"""
    },
    graphQLField("AgentGraphPage") { variables -> pageJson((variables["path"] as? JsonPrimitive)?.content ?: "people/sam-example") },
    graphQLField("AgentGraphNeighbours") { variables -> neighboursJson((variables["path"] as? JsonPrimitive)?.content ?: "people/sam-example") },
    graphQLField("ListAgentPageHistory") {
        """[{"revision":3,"actor":"person","change":"Edited the summary","before":"Sam lives with Alex.","createdAt":"${daysAgo(2)}"},
            {"revision":2,"actor":"dream","change":"Added fact #2","before":"","createdAt":"${daysAgo(8)}"},
            {"revision":1,"actor":"remember","change":"Made the page","before":"","createdAt":"${daysAgo(30)}"}]"""
    },
    graphQLField("SaveAgentNode", """{"id":"n","path":"people/sam-example"}"""),
    graphQLField("SaveAgentFact", """{"id":"f","number":6}"""),
    graphQLField("MoveAgentNode", """{"id":"n","path":"people/sam-example"}"""),
    graphQLField("MergeAgentNodes", """{"id":"n","path":"people/ben-example"}"""),
    graphQLField("MoveAgentFact", """{"id":"f","number":1}"""),
    graphQLField("LinkAgentNodes", "true"),
    graphQLField("UnlinkAgentNodes", "true"),
    graphQLField("DeleteAgentFact", "true"),
    graphQLField("DeleteAgentNode", "true"),
    graphQLField("ConfirmAgentRecord", "1"),
    graphQLField("UnlinkAgentRecord", "true"),
    graphQLField("SetAgentNodeContact", "true"),
    graphQLField("ListAgentRecordLinks") { """{"recordLinks":[],"recordLinkCount":0,"nextOffset":0}""" },
    graphQLField("ReadRecordMemoryLinks") {
        """[{"id":"l1","nodePath":"people/sam-example","nodeName":"Sam Example","nodeKind":"person","recordKind":"calendar_event","recordId":"e1","recordRelation":"attended","linkStatus":"stated","passageStartMS":0,"passageEndMS":0,"transcriptRunId":"","note":"","happenedAt":null,"createdAt":"${daysAgo(9)}","recordTitle":"","recordHappenedAt":null,"dashboardPath":"/calendar?event=e1","isUnavailable":false},
            {"id":"l2","nodePath":"projects/garden-planner","nodeName":"Garden planner","nodeKind":"project","recordKind":"calendar_event","recordId":"e1","recordRelation":"about","linkStatus":"proposed","passageStartMS":0,"passageEndMS":0,"transcriptRunId":"","note":"","happenedAt":null,"createdAt":"${daysAgo(9)}","recordTitle":"","recordHappenedAt":null,"dashboardPath":"/calendar?event=e1","isUnavailable":false}]"""
    },
    graphQLField("RecallAgentMemory") {
        """{"pages":[{"path":"people/sam-example","facts":[{"number":2,"text":"Prefers tea in the afternoon and coffee only before ten."},{"number":3,"text":"Decided to repaint the kitchen in the spring."}]},
            {"path":"projects/garden-planner","facts":[{"number":1,"text":"The beds by the shed get the afternoon sun."}]}]}"""
    },
    graphQLField("SearchAgentDocuments") {
        """{"passages":[{"documentId":"doc-1","externalId":"notes/garden.md","title":"Garden notes","author":"Alex","source":"Notes","happenedAt":null,"number":1,"text":"The beds by the shed get the afternoon sun; the north bed stays shaded until June."},
                        {"documentId":"doc-1","externalId":"notes/garden.md","title":"Garden notes","author":"Alex","source":"Notes","happenedAt":null,"number":2,"text":"Order the seed potatoes in February."},
                        {"documentId":"doc-2","externalId":"mail/42","title":"Seed order","author":"Sam","source":"Mail","happenedAt":null,"number":1,"text":"Your order of seed potatoes has shipped."}],
          "definitions":[{"symbol":"plantBeds","kind":"function","documentId":"doc-3","title":"planner.py"}],
          "directories":[{"directory":"notes/","sourceId":"source-notes","source":"Notes","passageCount":9}],
          "meaningful":false,"moreCount":3,"isMoreCountLowerBound":false,"nextOffset":20}"""
    },
    graphQLField("ReadAgentDocument") {
        """{"documentId":"doc-1","title":"Garden notes","author":"Alex","source":"Notes","happenedAt":null,"text":"The beds by the shed get the afternoon sun; the north bed stays shaded until June.\n\nOrder the seed potatoes in February, and the lettuce in March.","total":4200,"next":140}"""
    },
    graphQLField("ListAgentKnowledgeSources", """[{"id":"source-notes","name":"Notes"},{"id":"source-mail","name":"Mail"}]"""),
    graphQLField("ListSharedAgentPages") {
        """[{"ownerUserId":"ada-id","ownerUsername":"ada","ownerName":"Ada Example","ownerAvatarUrl":null,"ownerAvatarColorIndex":2,"path":"people/a-grandparent","name":"A grandparent","nodeKind":"person","opening":"Lives by the sea.","isQuoteShared":true},
            {"ownerUserId":"ada-id","ownerUsername":"ada","ownerName":"Ada Example","ownerAvatarUrl":null,"ownerAvatarColorIndex":2,"path":"projects/garden","name":"","nodeKind":"project","opening":"","isQuoteShared":false},
            {"ownerUserId":"ben-id","ownerUsername":"ben","ownerName":"","ownerAvatarUrl":null,"ownerAvatarColorIndex":4,"path":"projects/boat","name":"The boat","nodeKind":"project","opening":"Restoring the old boat.","isQuoteShared":false}]"""
    },
    graphQLField("SearchSharedAgentPages") {
        """{"hasMoreHits":true,"hits":[{"ownerUserId":"ada-id","ownerUsername":"ada","ownerName":"Ada Example","path":"people/a-grandparent","name":"A grandparent","opening":"Lives by the sea.","factNumber":0,"text":""},
            {"ownerUserId":"ada-id","ownerUsername":"ada","ownerName":"Ada Example","path":"people/a-grandparent","name":"A grandparent","opening":"","factNumber":3,"text":"Next appointment is on Tuesday."}]}"""
    },
    DemoAnswerer { request ->
        if (request.document.contains("ReadSharedAgentPage")) {
            val path = (request.variables["path"] as? JsonPrimitive)?.content ?: "people/a-grandparent"
            DemoResponse("""{"data":{"ReadSharedAgentPage":${sharedPage(path)}}}""")
        } else {
            null
        }
    },
    graphQLField("ListAgentPageShares") {
        """[{"id":"s1","path":"people","pageName":"People","granteeUserId":"ben-id","granteeUsername":"ben","granteeName":"Ben Example","granteeCircleId":null,"granteeCircleName":null,"isQuoteShared":false},
            {"id":"s2","path":"people/sam-example","pageName":"Sam Example","granteeUserId":null,"granteeUsername":null,"granteeName":null,"granteeCircleId":"circle-home","granteeCircleName":"Home","isQuoteShared":true},
            {"id":"s3","path":"projects","pageName":"Projects","granteeUserId":"cora-id","granteeUsername":"cora","granteeName":"Cora Example","granteeCircleId":null,"granteeCircleName":null,"isQuoteShared":false}]"""
    },
    graphQLField("ShareAgentPage", """{"id":"s4"}"""),
    graphQLField("UnshareAgentPage", "true"),
    graphQLField("PreviewAgentPageShare") {
        """{"page":${sharedPage("people/sam-example")},"reachedPageCount":4,"isReachedPageCountCut":false,"reachedPagePaths":["people/sam-example","people/sam-example/notes"],"withheldFactCount":3}"""
    },
    graphQLField("ListCirclePeople", """[{"userId":"ben-id","username":"ben","name":"Ben Example","avatarUrl":null,"avatarColorIndex":3},{"userId":"cora-id","username":"cora","name":"Cora Example","avatarUrl":null,"avatarColorIndex":5}]"""),
    graphQLField("ListAddressBooks", """[{"id":"book-1","name":"Contacts","contacts":3}]"""),
    graphQLField("ListContacts") {
        """[{"id":"card-1","name":"Sam Example","organization":"","emails":["sam@example.com"],"phones":[],"hasPhoto":false,"addresses":[],"accountUserId":null},
            {"id":"card-2","name":"Sam Examples","organization":"Bookshop","emails":["books@example.com"],"phones":[],"hasPhoto":false,"addresses":[],"accountUserId":null},
            {"id":"card-3","name":null,"organization":"","emails":["noreply@example.com"],"phones":[],"hasPhoto":false,"addresses":[],"accountUserId":null}]"""
    },
)
