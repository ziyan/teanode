package com.teanode.app.settings

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.DemoResponse
import com.teanode.app.demo.graphQLField

/** Answers a document that asks for several fields at once with each field's value. */
private fun graphQLFields(vararg fields: Pair<String, String>): DemoAnswerer {
    val asked = fields.map { field -> field.first to Regex("""\b${Regex.escape(field.first)}\b\s*[({]""") }
    return DemoAnswerer { request ->
        if (request.document.isNotEmpty() && asked.all { (_, pattern) -> pattern.containsMatchIn(request.document) }) {
            DemoResponse("""{"data":{${fields.joinToString(",") { field -> "\"${field.first}\":${field.second}" }}}}""")
        } else {
            null
        }
    }
}

private const val PROFILE = """{"id":"demo-user","username":"robin","name":"Robin Example","email":"robin@example.com","avatarUrl":null,"avatarColorIndex":4,"timezone":"Europe/Berlin","timezoneMode":"auto","pronoun":"","measurementSystem":""}"""

private const val CIRCLE_MEMBERS = """[
  {"userId":"demo-user","username":"robin","name":"Robin Example","avatarUrl":null,"avatarColorIndex":4,"memberRole":"admin","joinedAt":"2030-01-01T00:00:00Z","relationshipKind":"","relationshipTermGender":"","relationshipWord":"","linkedContactId":""},
  {"userId":"user-alex","username":"alex","name":"Alex Sample","avatarUrl":null,"avatarColorIndex":7,"memberRole":"member","joinedAt":"2030-01-02T00:00:00Z","relationshipKind":"parent","relationshipTermGender":"feminine","relationshipWord":"mother","linkedContactId":""},
  {"userId":"user-sam","username":"sam","name":"","avatarUrl":null,"avatarColorIndex":2,"memberRole":"member","joinedAt":"2030-01-03T00:00:00Z","relationshipKind":"","relationshipTermGender":"","relationshipWord":"","linkedContactId":""}
]"""

private const val CIRCLES = """[
  {"id":"circle-home","name":"Home","circleKind":"family","createdAt":"2030-01-01T00:00:00Z","memberRole":"admin","members":$CIRCLE_MEMBERS,
   "pendingInvitations":[{"id":"invite-pat","circleId":"circle-home","circleName":"Home","circleKind":"family","invitedByName":"Robin Example","invitedUsername":"pat","memberRole":"member","createdAt":"2030-01-04T00:00:00Z","expiresAt":"2999-01-11T00:00:00Z"}]},
  {"id":"circle-club","name":"Book club","circleKind":"friends","createdAt":"2030-01-01T00:00:00Z","memberRole":"member","members":[
    {"userId":"user-alex","username":"alex","name":"Alex Sample","avatarUrl":null,"avatarColorIndex":7,"memberRole":"admin","joinedAt":"2030-01-02T00:00:00Z"},
    {"userId":"demo-user","username":"robin","name":"Robin Example","avatarUrl":null,"avatarColorIndex":4,"memberRole":"member","joinedAt":"2030-01-02T00:00:00Z"}],"pendingInvitations":[]}
]"""

/**
 * What demo mode answers for Settings: one answer per document the screens send, so every screen and state can be
 * reached and photographed without a server. Most specific first.
 */
private const val AGENT_SETTINGS = """{
  "agent":{"id":"agent-1","name":"Juniper","enabled":true,"instructions":"I am the treasurer of the sailing club; anything from the club is important; sign as R.","language":"","knowledgeLanguage":"en","askModel":"","speechVoice":"","operatorDisabledAt":null,"confirm":["calendar","mail_send"],"unattendedAllowedRisks":["money"],"isMemoryCheckEnabled":true,"isIdeasEnabled":false,
    "voice":{"tone":"formal","length":null,"greeting":"Hello","signoff":null},"categories":[{"name":"club","description":"The sailing club"}]},
  "sources":[
    {"mailboxId":"mailbox-1","name":"Robin","addresses":["robin@example.com"],"policy":{"granted":true,"draftReplies":true,"search":false,"research":false,"triage":{"enabled":true,"backfill":"","replyExpectation":"any"},"summaries":null,"autoReply":{"enabled":true,"guidance":"Confirm appointments.","scope":"known","when":"outsideHours","hours":{"from":"09:00","until":"17:30","days":[1,2,3,4,5]}},"alerts":null}},
    {"mailboxId":"mailbox-2","name":"Club","addresses":["club@example.org","info@example.org"],"policy":null}],
  "collections":[{"id":"calendar-1","name":"Home","kind":"calendar","granted":true,"items":12},{"id":"book-1","name":"Contacts","kind":"addressBook","granted":false,"items":40}],
  "allowed":{"enabled":true,"triage":true,"summaries":true,"draftReplies":true,"search":false,"research":true,"autoReply":true,"ask":true,"schedules":true,"browser":false,"connectedServers":false},
  "budget":{"used":1200,"limit":100000,"resetsAt":"2030-01-02T00:00:00Z","cost":0.25,"costLimit":0,"currency":"USD"},
  "choices":["model-a","model-b"],"timezone":"Europe/Berlin","language":"en","knowledgeLanguage":"en",
  "categories":["personal","work","newsletter","notification","receipt","promotion","social","invitation","phishing","junk","other","club"]}"""

val settingsDemoAnswerers: List<DemoAnswerer> = listOf(
    graphQLField("ReadAgentVoice", """{"isVoiceAvailable":true,"sampleRate":24000,"speechVoice":"","speechVoices":["alto","breeze","cedar"],"serverSpeechVoice":"alto"}"""),
    graphQLField("ReadAgent", AGENT_SETTINGS),
    graphQLField("UpdateAgent", AGENT_SETTINGS),
    graphQLField(
        "ListAgentTools",
        """[
          {"name":"calendar","family":"calendar","risk":"low","description":"Read and change events.","confirms":false,"core":false},
          {"name":"mail_send","family":"mail","risk":"outward","description":"Send a message.","confirms":true,"core":true},
          {"name":"mail_search","family":"mail","risk":"low","description":"Search mail.","confirms":false,"core":false}
        ]""",
    ),
    DemoAnswerer { request ->
        if (request.document.contains("manages")) {
            DemoResponse("""{"data":{"GetSession":{"manages":true,"permissions":{"everywhere":["user:manage","group:manage","role:manage","audit:read","server:manage","agent:audit","agent:act","domain:manage"],"byDomain":[]}}}}""")
        } else {
            null
        }
    },
    graphQLField("GetCurrentUser", PROFILE),
    graphQLField(
        "ListUsers",
        """[
          {"id":"demo-user","username":"robin","name":"Robin Example","email":"robin@example.com","avatarColorIndex":4,"hasPassword":true,"groupIds":["group-members","group-admins"],"createdAt":"2030-01-01T00:00:00Z"},
          {"id":"user-alex","username":"alex","name":"Alex Sample","email":"alex@example.com","avatarColorIndex":7,"hasPassword":true,"groupIds":["group-members"],"createdAt":"2030-01-02T00:00:00Z"},
          {"id":"user-sam","username":"sam","name":"","avatarColorIndex":2,"hasPassword":false,"groupIds":[],"createdAt":"2030-01-03T00:00:00Z"},
          {"id":"user-pat","username":"pat","name":"Pat Sample","avatarColorIndex":5,"disabledAt":"2030-01-04T00:00:00Z","hasPassword":true,"groupIds":["group-members"],"createdAt":"2030-01-03T00:00:00Z"}
        ]""",
    ),
    graphQLField(
        "ListGroups",
        """[
          {"id":"group-members","name":"Members","description":"Everyone who reads their own mailbox","idpGroup":"","userIds":["demo-user","user-alex","user-pat"],"roleIds":["role-reader"],"domainIds":["domain-1"]},
          {"id":"group-admins","name":"Admins","description":"","idpGroup":"admins","userIds":["demo-user"],"roleIds":["role-reader","role-operator"],"domainIds":["domain-1"]}
        ]""",
    ),
    graphQLField(
        "ListRoles",
        """[
          {"id":"role-reader","name":"Reader","description":"Reads and sends own mail","permissions":["mail:read","mail:write","mail:send","agent:use"]},
          {"id":"role-operator","name":"Operator","description":"Runs the server","permissions":["user:manage","group:manage","role:manage","audit:read","server:manage"]}
        ]""",
    ),
    graphQLField(
        "ListPermissions",
        """[
          {"key":"mail:read","kind":"domain"},{"key":"mail:write","kind":"domain"},{"key":"mail:send","kind":"domain"},{"key":"mail:audit-all","kind":"all-domains","widens":"mail:audit"},
          {"key":"agent:use","kind":"server"},{"key":"user:manage","kind":"server"},{"key":"group:manage","kind":"server"},{"key":"role:manage","kind":"server"},{"key":"audit:read","kind":"server"},{"key":"server:manage","kind":"server"}
        ]""",
    ),
    graphQLField("ListDomains", """[{"id":"domain-1","domain":"example.com"},{"id":"domain-2","domain":"example.org"}]"""),
    graphQLField(
        "ListAuditEvents",
        """{"total":3,"events":[
          {"id":"e1","createdAt":"2030-01-05T09:00:00Z","actorKind":"user","actorLabel":"Robin Example","sourceIp":"192.0.2.20","instance":"server-1","resourceType":"domain","resourceId":"domain-1","resourceLabel":"example.com","action":"update","before":{"name":"example.com","isEnabled":false},"after":{"name":"example.com","isEnabled":true}},
          {"id":"e2","createdAt":"2030-01-04T09:00:00Z","actorKind":"agent","actorLabel":"Robin Example","impersonatorLabel":"operator","resourceType":"user","resourceId":"user-sam","resourceLabel":"sam","action":"create","before":null,"after":{"username":"sam"}},
          {"id":"e3","createdAt":"2030-01-03T09:00:00Z","actorKind":"console","resourceType":"role","resourceId":"role-old","action":"delete","before":{"name":"Old"},"after":null}
        ]}""",
    ),
    graphQLField(
        "GetServiceSetup",
        """{"selectedServices":{"isMailRoutingEnabled":true,"isMailboxEnabled":true,"isCalendarEnabled":true,"isRemindersEnabled":false,"isContactsEnabled":true,"isPhotosEnabled":true}}""",
    ),
    graphQLField(
        "ListSyncCredentials",
        """[
          {"id":"sync-1","credentialName":"Example tablet","hasCalendarAccess":true,"hasRemindersAccess":false,"hasContactsAccess":true,"createdAt":"2030-01-02T00:00:00Z","lastUsedAt":"2030-01-05T00:00:00Z"},
          {"id":"sync-2","credentialName":"Spare phone","hasCalendarAccess":true,"hasRemindersAccess":false,"hasContactsAccess":false,"createdAt":"2030-01-01T00:00:00Z"}
        ]""",
    ),
    graphQLField(
        "ListAgentPhones",
        """{"phones":[
          {"id":"phone-1","phoneName":"Robin's phone","phoneModel":"model-1","phoneModelName":"Example Phone 12","notificationKinds":["message","approval","question","chat","circle"],"canBeNotified":true,"lastSeenAt":"2030-01-05T00:00:00Z","lastDeliveredAt":"2030-01-05T00:00:00Z","lastDeliveryError":""},
          {"id":"phone-2","phoneName":"Old tablet","phoneModel":"model-2","phoneModelName":"Example Tablet","notificationKinds":["message"],"canBeNotified":false,"lastSeenAt":"2030-01-01T00:00:00Z","lastDeliveryError":""},
          {"id":"phone-3","phoneName":"Spare phone","phoneModel":"model-3","notificationKinds":["message","approval","question","chat","circle"],"canBeNotified":true,"lastDeliveryError":"The device token is no longer valid."}
        ],"isPushEnabled":true,"notificationKinds":["message","approval","question","chat","circle"]}""",
    ),
    graphQLFields(
        "GetPasskeyPolicy" to """{"enabled":true,"maximumPerUser":10}""",
        "ListPasskeys" to """[
          {"id":"passkey-1","name":"Example laptop","createdAt":"2030-01-01T00:00:00Z","usedAt":"2030-01-05T00:00:00Z","ip":"192.0.2.10","backupEligible":true,"backupState":true},
          {"id":"passkey-2","name":"Security key","createdAt":"2030-01-02T00:00:00Z","usedAt":"0001-01-01T00:00:00Z","backupEligible":false,"backupState":false}
        ]""",
    ),
    graphQLField(
        "ListSessions",
        """[
          {"id":"session-1","current":true,"created":"2030-01-05T00:00:00Z","lastUsed":"2030-01-05T01:00:00Z","ip":"192.0.2.20","userAgent":"Mozilla/5.0 (X11; Linux x86_64) Firefox/140.0"},
          {"id":"session-2","current":false,"created":"2030-01-03T00:00:00Z","lastUsed":"2030-01-04T00:00:00Z","ip":"198.51.100.7","userAgent":"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/140.0.0.0 Safari/537.36","impersonatorUsername":"operator"},
          {"id":"session-3","current":false,"created":"2030-01-01T00:00:00Z","ip":"203.0.113.5","revoked":"2030-01-02T00:00:00Z"}
        ]""",
    ),
    graphQLField(
        "ListTokens",
        """[
          {"id":"tnt_one","name":"laptop","username":"robin","created":"2030-01-01T00:00:00Z","expires":"2999-04-01T00:00:00Z","lastUsed":"2030-01-02T00:00:00Z","lastUsedIp":"198.51.100.7"},
          {"id":"tnt_two","name":"","username":"robin","created":"2030-01-01T00:00:00Z"},
          {"id":"tnt_three","name":"old build","username":"robin","created":"2029-01-01T00:00:00Z","expires":"2029-04-01T00:00:00Z","revoked":"2029-02-01T00:00:00Z"}
        ]""",
    ),
    graphQLField(
        "ListApps",
        """[
          {"clientId":"demo","name":"","registeredName":"TeaNode for Android","redirectHosts":["app"],"registered":"2030-01-01T00:00:00Z","lastUsed":"2030-01-05T00:00:00Z","renewableUntil":"2999-04-01T00:00:00Z","tokenCount":2},
          {"clientId":"client-2","name":"My notes","registeredName":"Notes","redirectHosts":["notes.example.com"],"registered":"2030-01-01T00:00:00Z","tokenCount":1}
        ]""",
    ),
    graphQLField("ListCircleInvitations", """[{"id":"invite-1","circleId":"circle-work","circleName":"Colleagues","circleKind":"colleagues","invitedByName":"Alex Sample","invitedUsername":"robin","memberRole":"member","createdAt":"2030-01-04T00:00:00Z","expiresAt":"2999-01-11T00:00:00Z"}]"""),
    graphQLField("ListCircles", CIRCLES),
    graphQLField(
        "ListRelationshipTerms",
        """[
          {"term":"spouse","relationshipKind":"spouse","termGender":"neutral","word":"spouse"},
          {"term":"husband","relationshipKind":"spouse","termGender":"masculine","word":"husband"},
          {"term":"wife","relationshipKind":"spouse","termGender":"feminine","word":"wife"},
          {"term":"parent","relationshipKind":"parent","termGender":"neutral","word":"parent"},
          {"term":"mother","relationshipKind":"parent","termGender":"feminine","word":"mother"},
          {"term":"child","relationshipKind":"child","termGender":"neutral","word":"child"},
          {"term":"friend","relationshipKind":"friend","termGender":"neutral","word":"friend"}
        ]""",
    ),
    graphQLField(
        "ListSharing",
        """{"sharesMade":[
          {"shareKind":"calendar","shareId":"share-1","subjectId":"calendar-1","subjectName":"Family events","shareLevel":"details","shareScopes":[],"passwordItemCount":0,"granteeCircleId":"circle-home","granteeUsername":"","granteeName":"","granteeCircleName":"Home","createdAt":"2030-01-02T00:00:00Z"},
          {"shareKind":"location","shareId":"","subjectId":"","subjectName":"","shareLevel":"current","shareScopes":[],"passwordItemCount":0,"granteeUserId":"user-alex","granteeUsername":"alex","granteeName":"Alex Sample","granteeCircleName":"","createdAt":"2030-01-03T00:00:00Z"},
          {"shareKind":"photo_album","shareId":"share-3","subjectId":"album-1","subjectName":"Holiday","shareLevel":"contribute","shareScopes":["location","faceNames"],"passwordItemCount":0,"granteeUserId":"user-sam","granteeUsername":"sam","granteeName":"","granteeCircleName":"","createdAt":"2030-01-04T00:00:00Z"},
          {"shareKind":"passwords","shareId":"","subjectId":"","subjectName":"","shareLevel":"","shareScopes":[],"passwordItemCount":3,"granteeUserId":"user-alex","granteeUsername":"alex","granteeName":"Alex Sample","granteeCircleName":"","createdAt":"2030-01-04T00:00:00Z"}
        ],"sharesReceived":[
          {"shareKind":"recording","shareId":"share-4","subjectId":"recording-1","subjectName":"Team meeting","shareLevel":"transcript","shareScopes":[],"passwordItemCount":0,"ownerUserId":"user-alex","ownerUsername":"alex","ownerName":"Alex Sample","granteeCircleName":"","isDirect":true,"canDecline":true,"createdAt":"2030-01-05T00:00:00Z"},
          {"shareKind":"finance","shareId":"share-5","subjectId":"","subjectName":"","shareLevel":"spending","shareScopes":["allAccounts"],"passwordItemCount":0,"ownerUserId":"user-alex","ownerUsername":"alex","ownerName":"","granteeCircleId":"circle-home","granteeCircleName":"Home","isDirect":false,"canDecline":false,"createdAt":"2030-01-05T00:00:00Z"}
        ]}""",
    ),
)
