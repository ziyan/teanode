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
  "agent":{"id":"agent-1","name":"Juniper","enabled":true,"instructions":"I am the treasurer of the sailing club; anything from the club is important; sign as R.","language":"","knowledgeLanguage":"en","askModel":"","speechVoice":"","operatorDisabledAt":null,"confirm":["calendar","mail_send"],"unattendedAllowedRisks":["money"],"isSuggestionsEnabled":true,"dreamFrom":"01:00","dreamUntil":"06:00","isAlertsEnabled":true,
    "voice":{"tone":"formal","length":null,"greeting":"Hello","signoff":null},"categories":[{"name":"club","description":"The sailing club"}]},
  "sources":[
    {"mailboxId":"mailbox-1","name":"Robin","addresses":["robin@example.com"],"policy":{"granted":true,"draftReplies":true,"search":false,"research":false,"triage":{"enabled":true,"backfill":"","replyExpectation":"any"},"summaries":null,"autoReply":{"enabled":true,"guidance":"Confirm appointments.","scope":"known","when":"outsideHours","hours":{"from":"09:00","until":"17:30","days":[1,2,3,4,5]}},"alerts":null}},
    {"mailboxId":"mailbox-2","name":"Club","addresses":["club@example.org","info@example.org"],"policy":null}],
  "collections":[{"id":"calendar-1","name":"Home","kind":"calendar","granted":true,"items":12},{"id":"book-1","name":"Contacts","kind":"addressBook","granted":false,"items":40}],
  "allowed":{"enabled":true,"triage":true,"summaries":true,"draftReplies":true,"search":false,"research":true,"autoReply":true,"ask":true,"schedules":true,"browser":false,"connectedServers":false},
  "budget":{"used":1200,"limit":100000,"resetsAt":"2030-01-02T00:00:00Z","cost":0.25,"costLimit":0,"currency":"USD"},
  "choices":["model-a","model-b"],"timezone":"Europe/Berlin","language":"en","knowledgeLanguage":"en",
  "categories":["personal","work","newsletter","notification","receipt","promotion","social","invitation","phishing","junk","other","club"]}"""

val settingsDemoAnswerers: List<DemoAnswerer> = com.teanode.app.settings.manage.manageDemoAnswerers + listOf(
    graphQLFields(
        "ListAgents" to """[
          {"agentId":"agent-1","userId":"demo-user","username":"robin","name":"Juniper","enabled":true,"dailyTokens":100000,"dailyCost":0,"dead":0,"queued":2,"today":{"used":1200,"limit":100000,"cost":0.25,"costLimit":0,"currency":"USD"}},
          {"agentId":"agent-2","userId":"user-alex","username":"alex","name":"","enabled":true,"operatorDisabledAt":"2030-01-02T00:00:00Z","dailyTokens":0,"dailyCost":2.5,"dead":3,"queued":0,"today":{"used":0,"limit":0,"cost":0,"costLimit":2.5,"currency":"USD"}}
        ]""",
        "AgentServerUsage" to """[
          {"key":"2030-01-05","cost":0.42,"currency":"USD","billedAmount":0.4,"billedCurrencyCode":"USD","totals":{"promptTokens":120000,"completionTokens":30000,"cacheReadTokens":80000,"cacheWriteTokens":0,"audioInputTokens":0,"calls":64}},
          {"key":"2030-01-04","cost":0.18,"currency":"USD","totals":{"promptTokens":50000,"completionTokens":9000,"cacheReadTokens":20000,"cacheWriteTokens":0,"audioInputTokens":0,"calls":21}}
        ]""",
        "ListAgentDeadLetters" to """[{"id":"dead1","agentId":"agent-2","kind":"triage","attempts":3,"error":"The model did not answer."}]""",
    ),
    // What the database and the files hold, for the Disk usage screen: invented sizes growing steadily, a disk that fills in about
    // twenty days and an object store that is not measured.
    graphQLField("StorageUsage", """{"breakdownBy":"area","databaseSizeBytes":79456894976,"fileSizeBytes":286689067008,"totalSizeBytes":366145961984,"usageRows":[{"rowKey":"search","rowLabel":"search","rowSizeBytes":33285996544,"rowTableCount":3,"rowFileSizes":[]},{"rowKey":"memory","rowLabel":"memory","rowSizeBytes":25769803776,"rowTableCount":4,"rowFileSizes":[]},{"rowKey":"health","rowLabel":"health","rowSizeBytes":9663676416,"rowTableCount":5,"rowFileSizes":[]},{"rowKey":"conversations","rowLabel":"conversations","rowSizeBytes":3221225472,"rowTableCount":6,"rowFileSizes":[]},{"rowKey":"mail","rowLabel":"mail","rowSizeBytes":2147483648,"rowTableCount":7,"rowFileSizes":[]},{"rowKey":"photos","rowLabel":"photos","rowSizeBytes":1073741824,"rowTableCount":8,"rowFileSizes":[]},{"rowKey":"other","rowLabel":"other","rowSizeBytes":1073741824,"rowTableCount":9,"rowFileSizes":[]}],"historyDays":[{"sampledOn":"2030-01-01","databaseSizeBytes":76235669504,"fileSizeBytes":282394099712,"totalSizeBytes":358629769216,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33285996544},{"sizeKey":"memory","sizeBytes":25769803776},{"sizeKey":"health","sizeBytes":9663676416},{"sizeKey":"conversations","sizeBytes":3221225472},{"sizeKey":"mail","sizeBytes":2147483648},{"sizeKey":"photos","sizeBytes":1073741824},{"sizeKey":"other","sizeBytes":1073741824}]},{"sampledOn":"2030-01-02","databaseSizeBytes":76504104960,"fileSizeBytes":282930970624,"totalSizeBytes":359435075584,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33312840089},{"sizeKey":"memory","sizeBytes":25796647321},{"sizeKey":"health","sizeBytes":9690519961},{"sizeKey":"conversations","sizeBytes":3248069017},{"sizeKey":"mail","sizeBytes":2174327193},{"sizeKey":"photos","sizeBytes":1100585369},{"sizeKey":"other","sizeBytes":1100585369}]},{"sampledOn":"2030-01-03","databaseSizeBytes":76772540416,"fileSizeBytes":283467841536,"totalSizeBytes":360240381952,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33339683635},{"sizeKey":"memory","sizeBytes":25823490867},{"sizeKey":"health","sizeBytes":9717363507},{"sizeKey":"conversations","sizeBytes":3274912563},{"sizeKey":"mail","sizeBytes":2201170739},{"sizeKey":"photos","sizeBytes":1127428915},{"sizeKey":"other","sizeBytes":1127428915}]},{"sampledOn":"2030-01-04","databaseSizeBytes":77040975872,"fileSizeBytes":284004712448,"totalSizeBytes":361045688320,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33366527180},{"sizeKey":"memory","sizeBytes":25850334412},{"sizeKey":"health","sizeBytes":9744207052},{"sizeKey":"conversations","sizeBytes":3301756108},{"sizeKey":"mail","sizeBytes":2228014284},{"sizeKey":"photos","sizeBytes":1154272460},{"sizeKey":"other","sizeBytes":1154272460}]},{"sampledOn":"2030-01-05","databaseSizeBytes":77309411328,"fileSizeBytes":284541583360,"totalSizeBytes":361850994688,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33393370726},{"sizeKey":"memory","sizeBytes":25877177958},{"sizeKey":"health","sizeBytes":9771050598},{"sizeKey":"conversations","sizeBytes":3328599654},{"sizeKey":"mail","sizeBytes":2254857830},{"sizeKey":"photos","sizeBytes":1181116006},{"sizeKey":"other","sizeBytes":1181116006}]},{"sampledOn":"2030-01-06","databaseSizeBytes":77577846784,"fileSizeBytes":285078454272,"totalSizeBytes":362656301056,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33420214272},{"sizeKey":"memory","sizeBytes":25904021504},{"sizeKey":"health","sizeBytes":9797894144},{"sizeKey":"conversations","sizeBytes":3355443200},{"sizeKey":"mail","sizeBytes":2281701376},{"sizeKey":"photos","sizeBytes":1207959552},{"sizeKey":"other","sizeBytes":1207959552}]},{"sampledOn":"2030-01-07","databaseSizeBytes":77846282240,"fileSizeBytes":285615325184,"totalSizeBytes":363461607424,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33447057817},{"sizeKey":"memory","sizeBytes":25930865049},{"sizeKey":"health","sizeBytes":9824737689},{"sizeKey":"conversations","sizeBytes":3382286745},{"sizeKey":"mail","sizeBytes":2308544921},{"sizeKey":"photos","sizeBytes":1234803097},{"sizeKey":"other","sizeBytes":1234803097}]},{"sampledOn":"2030-01-08","databaseSizeBytes":78114717696,"fileSizeBytes":286152196096,"totalSizeBytes":364266913792,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33473901363},{"sizeKey":"memory","sizeBytes":25957708595},{"sizeKey":"health","sizeBytes":9851581235},{"sizeKey":"conversations","sizeBytes":3409130291},{"sizeKey":"mail","sizeBytes":2335388467},{"sizeKey":"photos","sizeBytes":1261646643},{"sizeKey":"other","sizeBytes":1261646643}]},{"sampledOn":"2030-01-09","databaseSizeBytes":78383153152,"fileSizeBytes":286689067008,"totalSizeBytes":365072220160,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33500744908},{"sizeKey":"memory","sizeBytes":25984552140},{"sizeKey":"health","sizeBytes":9878424780},{"sizeKey":"conversations","sizeBytes":3435973836},{"sizeKey":"mail","sizeBytes":2362232012},{"sizeKey":"photos","sizeBytes":1288490188},{"sizeKey":"other","sizeBytes":1288490188}]},{"sampledOn":"2030-01-10","databaseSizeBytes":78651588608,"fileSizeBytes":287225937920,"totalSizeBytes":365877526528,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33527588454},{"sizeKey":"memory","sizeBytes":26011395686},{"sizeKey":"health","sizeBytes":9905268326},{"sizeKey":"conversations","sizeBytes":3462817382},{"sizeKey":"mail","sizeBytes":2389075558},{"sizeKey":"photos","sizeBytes":1315333734},{"sizeKey":"other","sizeBytes":1315333734}]},{"sampledOn":"2030-01-11","databaseSizeBytes":78920024064,"fileSizeBytes":287762808832,"totalSizeBytes":366682832896,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33554432000},{"sizeKey":"memory","sizeBytes":26038239232},{"sizeKey":"health","sizeBytes":9932111872},{"sizeKey":"conversations","sizeBytes":3489660928},{"sizeKey":"mail","sizeBytes":2415919104},{"sizeKey":"photos","sizeBytes":1342177280},{"sizeKey":"other","sizeBytes":1342177280}]},{"sampledOn":"2030-01-12","databaseSizeBytes":79188459520,"fileSizeBytes":288299679744,"totalSizeBytes":367488139264,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33581275545},{"sizeKey":"memory","sizeBytes":26065082777},{"sizeKey":"health","sizeBytes":9958955417},{"sizeKey":"conversations","sizeBytes":3516504473},{"sizeKey":"mail","sizeBytes":2442762649},{"sizeKey":"photos","sizeBytes":1369020825},{"sizeKey":"other","sizeBytes":1369020825}]},{"sampledOn":"2030-01-13","databaseSizeBytes":79456894976,"fileSizeBytes":288836550656,"totalSizeBytes":368293445632,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33608119091},{"sizeKey":"memory","sizeBytes":26091926323},{"sizeKey":"health","sizeBytes":9985798963},{"sizeKey":"conversations","sizeBytes":3543348019},{"sizeKey":"mail","sizeBytes":2469606195},{"sizeKey":"photos","sizeBytes":1395864371},{"sizeKey":"other","sizeBytes":1395864371}]},{"sampledOn":"2030-01-14","databaseSizeBytes":79725330432,"fileSizeBytes":289373421568,"totalSizeBytes":369098752000,"breakdownSizes":[{"sizeKey":"search","sizeBytes":33634962636},{"sizeKey":"memory","sizeBytes":26118769868},{"sizeKey":"health","sizeBytes":10012642508},{"sizeKey":"conversations","sizeBytes":3570191564},{"sizeKey":"mail","sizeBytes":2496449740},{"sizeKey":"photos","sizeBytes":1422707916},{"sizeKey":"other","sizeBytes":1422707916}]}],"disks":[{"diskLabel":"dataDirectory","isDiskMeasured":true,"diskTotalBytes":429496729600,"diskFreeBytes":42949672960,"diskUsedBytes":386547056640,"projectedFullOn":"2030-01-30","daysUntilFull":20.0,"isProjectedFull":true},{"diskLabel":"objectStore","isDiskMeasured":false,"diskTotalBytes":null,"diskFreeBytes":null,"diskUsedBytes":null,"projectedFullOn":null,"daysUntilFull":null,"isProjectedFull":false}],"growth":{"growthSampleCount":14,"isGrowthProjectable":true,"growthBytesPerDay":805306368,"currentSizeBytes":366145961984,"projectedSize30DaysBytes":389768282112,"projectedSize90DaysBytes":439160406016,"projectedSize365DaysBytes":660351221760,"areFilesCounted":true}}"""),
    graphQLField(
        "ListAgentEvaluationRuns",
        """[
          {"id":"run2","startedAt":"2030-01-05T02:00:00Z","finishedAt":"2030-01-05T02:09:00Z","questionCount":12,"cost":0.31,"sourceScores":[{"answerFrom":"memory","answeredCount":12,"scorePercent":83.0,"scorePercentWithoutFiledAfter":88.0,"filedAfterCount":1},{"answerFrom":"sources","answeredCount":12,"scorePercent":58.0,"scorePercentWithoutFiledAfter":60.0,"filedAfterCount":1}]},
          {"id":"run1","startedAt":"2030-01-02T02:00:00Z","finishedAt":"2030-01-02T02:08:00Z","questionCount":10,"cost":0.27,"sourceScores":[{"answerFrom":"memory","answeredCount":10,"scorePercent":70.0,"scorePercentWithoutFiledAfter":70.0,"filedAfterCount":0}]}
        ]""",
    ),
    graphQLField(
        "ListAgentEvaluationAnswers",
        """[
          {"id":"ans1","runId":"run2","questionId":"q1","answerFrom":"memory","answerVerdict":"correct","verdictReason":"Matches the expected answer.","answerText":"The club meets on Thursdays."},
          {"id":"ans2","runId":"run2","questionId":"q2","answerFrom":"memory","answerVerdict":"missed","verdictReason":"The agent said it did not know.","answerText":"I do not know."}
        ]""",
    ),
    graphQLField(
        "ListAgentSourceTypes",
        """[{"name":"webfeed","description":"reads a feed of articles from a web address","isLocal":false,"reader":"feed","requires":[],"readable":true,"problem":null,
          "settings":[{"name":"feedUrl","description":"where the feed is","settingType":"string","pattern":"","itemPattern":"","isRequired":true},{"name":"maximumItems","description":"how many items to keep","settingType":"integer","pattern":"","itemPattern":"","isRequired":false,"minimum":1,"maximum":500}],
          "secrets":[{"key":"accessToken","description":"the token the feed asks for","isOptional":true}]}]""",
    ),
    // The access session the People, Groups and Roles pages read: an operator, so their buttons show.
    DemoAnswerer { request ->
        if (request.document.contains("impersonatorUsername")) {
            DemoResponse("""{"data":{"GetSession":{"userId":"demo-user","permissions":{"everywhere":["circle:use","location:use","health:use","journal:use","passwords:use","shopping:use","photos:use","user:manage","group:manage","role:manage","audit:read","server:manage","agent:audit","agent:act","domain:manage"]},"impersonatorUsername":null}}}""")
        } else {
            null
        }
    },
    graphQLFields(
        "GetServerAddresses" to """{"ipv4":"192.0.2.10","ipv6":null,"error":null}""",
        "GetOutgoingIdentity" to """{"address":"192.0.2.10","via":"direct","reverseName":"mail.example.com","forwardAddresses":["192.0.2.11"],"confirmed":false,"helloName":"mail.example.com","helloAddresses":["192.0.2.10"],"helloMatches":true,"unknown":null}""",
        "GetSettings" to """{"submission":{"host":null,"port":0,"effectiveHost":"mail.example.com","effectivePort":"587"},"imap":{"host":null,"port":0,"tlsPort":0,"effectiveHost":"mail.example.com","effectivePort":"143","effectiveTlsPort":"993"}}""",
    ),
    graphQLField("GetServerStatus", """{"instance":"server-1","version":"1.42.0","commit":"abcdef0123456789abcdef","startedAt":"2030-01-01T00:00:00Z","uptimeSeconds":345600,"pendingRestart":["listen.https"],"supervision":"container","restarting":false}"""),
    graphQLField("GetUpgrade", """{"current":"1.42.0","latest":"1.43.0","available":true,"notes":"Fixes and a faster mail queue.","checkedAt":"2030-01-05T00:00:00Z","applicable":true,"automatic":false,"enabled":true,"upgrading":false}"""),
    graphQLField(
        "GetSettings",
        """{"s3":{"enabled":true,"bucket":"example-mail","region":"eu-west-1","endpoint":null,"pathStyle":false,"accessKeyId":"AKIAEXAMPLE","hasSecretAccessKey":true,"credentialsFile":""},
          "route53":{"enabled":false,"zoneId":"","region":"","accessKeyId":"","hasSecretAccessKey":false,"credentialsFile":""},
          "antivirus":{"enabled":false,"host":"","port":3310},
          "antispam":{"enabled":true,"engine":"builtin","effectiveEngine":"builtin","host":"","port":0,"signalsEnabled":true,"dnsEnabled":true,"bayesEnabled":true,"rulesEnabled":true,"bayesMinimumMessages":200,"bayesLearnedSpam":1200,"bayesLearnedHam":3400},
          "relay":{"enabled":true,"host":"smtp.example.com","port":587,"security":"starttls","username":"relay-user","hasPassword":true},
          "proxy":{"socks5":""},
          "certificates":{"perDomain":true,"hosts":["mail.example.com"],"acmeEnabled":true,"acmeEmail":"admin@example.com","acmeDirectoryUrl":"https://acme.example.com/directory","acmeChallenge":"http","certificateFile":"","privateKeyFile":""},
          "smtp":{"maxMessageSize":52428800,"maxRecipientsIncoming":100,"maxRecipientsOutgoing":50,"greylistDelay":300,"authRateLimit":5,"authRateBurst":10,"trustedSenders":["partner.example.com"]},
          "resolver":{"nameserver":"1.1.1.1","checkInterval":3600,"externalAddressServices":["https://addr.example.com"]},
          "session":{"lifetime":2592000},
          "passkey":{"enabled":true,"relyingPartyId":"example.com","displayName":"Example","origins":["https://mail.example.com"],"maximumPerUser":10},
          "listen":{"smtpIncoming":":25","smtpOutgoing":":587","imap":":143","imaps":":993","http":":80","https":":443","debug":""},
          "sso":{"providers":[{"id":"idp","name":"Example SSO","issuer":"https://sso.example.com","clientId":"teanode","hasClientSecret":true,"groupsClaim":"groups","createUsers":true}]},
          "identity":{"name":"Example Mail","mailServers":["mail.example.com"],"externalAddresses":["203.0.113.5"],"logLevel":"info","dataDirectory":"/var/lib/example"},
          "storage":{"directory":"/var/lib/example/mail","spoolRetention":"168h"},
          "geoip":{"enabled":false,"databaseFile":""},
          "places":{"providerKind":"nominatim","providerUrl":"https://places.example.com","mapStyleUrl":""}}""".replace("\n", " "),
    ),
    graphQLField(
        "ListAgents",
        """[
          {"agentId":"agent-1","userId":"demo-user","username":"robin","name":"Juniper","enabled":true,"dailyTokens":100000,"dailyCost":0,"dead":0,"queued":2,"today":{"used":1200,"limit":100000,"cost":0.25,"costLimit":0,"currency":"USD"}}
        ]""",
    ),
    graphQLField(
        "ListSkillShares",
        """[
          {"id":"ss1","skillName":"Travel booking","toolNames":["search_flights","book_flight"],"ownerUserId":"demo-user","ownerUsername":"robin","ownerName":"Robin Example","granteeUserId":"user-alex","granteeUsername":"alex","granteeName":"Alex Sample","paymentLimitMinor":50000,"monthlyLimitMinor":200000,"currencyCode":"usd","isApprovalRequired":true,"isOwnedByCaller":true,"waitingCallCount":1,"monthTotalMinor":12000},
          {"id":"ss2","skillName":"Recipes","toolNames":["find_recipe"],"ownerUserId":"user-sam","ownerUsername":"sam","ownerName":"","granteeUserId":"demo-user","granteeUsername":"robin","granteeName":"Robin Example","currencyCode":"","isApprovalRequired":false,"isOwnedByCaller":false,"waitingCallCount":0,"monthTotalMinor":0}
        ]""",
    ),
    graphQLField(
        "ListSkillShareCalls",
        """[{"id":"call1","skillShareId":"ss1","skillName":"Travel booking","granteeUserId":"user-alex","granteeUsername":"alex","granteeName":"Alex Sample","ownerName":"Robin Example","toolName":"book_flight","toolArguments":"{\"destination\":\"Example Coast\",\"depart_on\":\"2030-01-11\",\"amount_minor\":18900}","amountArgumentName":"amount_minor","requestReason":"A flight to the coast on Friday.","amountMinor":18900,"currencyCode":"usd","callState":"waiting_for_approval","toolOutput":"","answerNote":"","createdAt":"2030-01-05T09:00:00Z"}]""",
    ),
    graphQLField("ListAgentSkills", """[{"name":"Travel booking","description":"Search and book flights","enabled":true,"readable":true,"tools":[{"name":"search_flights","description":"Search flights"},{"name":"book_flight","description":"Book a flight","isAmountDeclared":true}]}]"""),
    graphQLField(
        "ListAgentDreams",
        """[
          {"id":"d1","startedAt":"2030-01-05T01:10:00Z","finishedAt":"2030-01-05T01:48:00Z","digested":24,"filed":12,"merged":2,"rewritten":3,"moved":0,"dormant":1,"embedded":40,"backlog":0,"coarse":false,"strengthened":5,"associated":4,"rehearsed":3,"gaps":1,"unknown":0,"revised":0,"overviewsWritten":1,"themesMade":0,"themesUpdated":0,"reflectionsWritten":2,"cost":0.42,"currency":"USD"},
          {"id":"d2","startedAt":"2030-01-04T01:05:00Z","finishedAt":"2030-01-04T01:07:00Z","digested":0,"filed":0,"merged":0,"rewritten":0,"moved":0,"dormant":0,"embedded":0,"backlog":0,"coarse":false,"strengthened":0,"associated":0,"rehearsed":0,"gaps":0,"unknown":0,"revised":0,"overviewsWritten":0,"themesMade":0,"themesUpdated":0,"reflectionsWritten":0,"lastError":"The model did not answer.","cost":0,"currency":"USD"}
        ]""",
    ),
    graphQLField("AgentDreamState", """{"dreamJobStatus":""}"""),
    graphQLField("AgentReadingProgress", """{"waiting":120,"read":480,"perHour":40,"hoursLeft":3,"bootstrapping":false,"spent":1.2,"costLeft":0.4,"currency":"USD","dreams":3}"""),
    graphQLFields(
        "ListAgentAlerts" to """[
          {"id":"a1","alertText":"Your flight tomorrow was moved to 9:40.","sentAt":"2030-01-05T07:00:00Z","subjectKey":"flight","isUrgent":true,"covered":[{"mailId":"m1","subject":"Flight change","fromAddress":"airline@example.com"}],"muteChoices":[{"muteScope":"sender","muteTarget":"airline@example.com"}]},
          {"id":"a2","alertText":"A new invoice arrived.","sentAt":"2030-01-04T07:00:00Z","subjectKey":"invoice","isUrgent":false,"covered":[],"muteChoices":[]}
        ]""",
        "ListAgentAlertMutes" to """[{"id":"mute1","muteScope":"sender","muteTarget":"news@example.com","createdAt":"2030-01-02T00:00:00Z"}]""",
    ),
    graphQLField("ListAgentLearned", """[{"fact":{"id":"f1","number":1,"kind":"preference","text":"Prefers tea in the afternoon.","inferred":false,"evidence":[{"kind":"message","id":"m1","quote":"I never drink coffee after lunch."}]},"path":"people/robin","name":"Robin"},{"fact":{"id":"f2","number":2,"kind":"plan","text":"Plans a trip to the coast in spring.","inferred":true},"path":"plans/coast","name":"Coast trip"}]"""),
    graphQLField("ListAgentCorrections", """[{"id":"c1","createdAt":"2030-01-03T00:00:00Z","kind":"fact","said":"The club meets on Thursdays, not Fridays."}]"""),
    graphQLField("ListAgentEvaluationQuestions", """[{"id":"q1","questionText":"Which day does the club meet?","expectedAnswer":"Thursday","questionState":"confirmed","questionKind":"direct"}]"""),
    graphQLField(
        "ListAgentServers",
        """[{"name":"notes","transport":"http","auth":"user","oauthRedirect":"","headless":false,"enabled":true,"status":"connected","tools":6},{"name":"calendar-tools","transport":"http","auth":"oauth","oauthRedirect":"","headless":false,"enabled":true,"status":"error","lastError":"The server did not answer.","tools":0},{"name":"team-wiki","transport":"http","auth":"none","oauthRedirect":"","headless":false,"enabled":true,"status":"connected","tools":4},{"name":"ticket-tracker","transport":"http","auth":"user","oauthRedirect":"","headless":true,"enabled":true,"status":"","tools":0}]""",
    ),
    graphQLField(
        "ListAgentFollowUps",
        """[
          {"id":"fu1","followUpKind":"bill","followUpTitle":"Water bill","payee":"City Water","amount":"84.20","currencyCode":"USD","dueOn":"2030-01-12","followUpState":"overdue"},
          {"id":"fu2","followUpKind":"parcel","followUpTitle":"Books from the shop","carrier":"Example Post","trackingNumber":"EX123456","followUpState":"stalled"},
          {"id":"fu3","followUpKind":"event","followUpTitle":"Open day at the school","place":"Main hall","dueOn":"2030-02-03","sourceUrl":"https://www.example.com/school/open-day","followUpState":"open"}
        ]""",
    ),
    graphQLField(
        "ListAgentReplies",
        """{"total":3,"replies":[
          {"id":"rp1","createdAt":"2030-01-05T08:00:00Z","status":"held","subject":"Lunch on Friday","to":"sam@example.com","sendAfter":"2030-01-05T10:00:00Z"},
          {"id":"rp2","createdAt":"2030-01-04T08:00:00Z","sentAt":"2030-01-04T08:05:00Z","status":"sent","subject":"Meeting notes","to":"alex@example.com"},
          {"id":"rp3","createdAt":"2030-01-03T08:00:00Z","status":"refused","reason":"It asked for a decision only you can make.","subject":"Contract question","to":"robin@example.com"}
        ]}""",
    ),
    graphQLField("GetAgentBackgroundWork", """{"id":"w2","workKind":"survey","workStatus":"done","title":"Trip ideas","errorMessage":"","resultText":"## Three options\n\n- A weekend by the coast\n- A train trip inland\n- A quiet cabin","runIds":["r1","r2"],"createdAt":"2030-01-04T09:00:00Z","finishedAt":"2030-01-04T09:05:00Z"}"""),
    graphQLField("ListAgentSkillSecrets", """[{"skill":"weather","key":"WEATHER_API_KEY","description":"The key for the weather service","set":true},{"skill":"tracker","key":"TRACKER_TOKEN","description":"","set":false}]"""),
    graphQLFields(
        "ListAgentReaches" to """[{"kind":"skill","name":"weather","computerName":"","isOnComputer":false},{"kind":"server","name":"ticket-tracker","computerName":"","isOnComputer":true},{"kind":"server","name":"notes","computerName":"old-box","isOnComputer":true}]""",
        "ReadAgentComputers" to """{"computers":[{"name":"studio","system":"Linux","description":"The desk computer"},{"name":"laptop","system":"Linux","description":""}]}""",
    ),
    graphQLField(
        "ListAgentChannels",
        """[{"kind":"telegram","hasToken":true,"botName":"example_helper_bot","linked":false,"linkCode":"4821","enabled":true,"running":true},{"kind":"discord","hasToken":true,"botName":"Helper","linked":true,"linkedName":"Alex","enabled":false,"running":false}]""",
    ),
    graphQLField(
        "ListAgentBackgroundWork",
        """[{"id":"w1","workKind":"survey","workStatus":"running","title":"Survey of the club mail","errorMessage":"","runIds":[],"createdAt":"2030-01-05T09:00:00Z"},{"id":"w2","workKind":"survey","workStatus":"done","title":"Trip ideas","errorMessage":"","runIds":["r1"],"createdAt":"2030-01-04T09:00:00Z","finishedAt":"2030-01-04T09:05:00Z","resultText":"Three options found."}]""",
    ),
    graphQLField(
        "ListAgentRuns",
        """{"total":2,"runs":[{"id":"r1","title":"Sorted new mail","jobKind":"triage","lastAt":"2030-01-05T09:00:00Z","usage":{"promptTokens":1200,"completionTokens":300,"cost":0.004,"cacheReadTokens":800}},{"id":"r2","title":"Morning brief","jobKind":"schedule","lastAt":"2030-01-05T08:00:00Z","usage":{"promptTokens":4000,"completionTokens":900,"cost":0.02}}]}""",
    ),
    graphQLFields(
        "ListAgentKnowledgeSources" to """[
          {"id":"src1","kind":"folder","name":"Notes folder","rootPath":"notes","enabled":true,"cron":"0 * * * *","lastRunAt":"2030-01-05T09:00:00Z","lastError":"","documentCount":120,"chunkCount":900,"refusedCount":2,"more":false},
          {"id":"src2","kind":"repository","name":"Club website","rootPath":"club","enabled":false,"cron":"","lastError":"The path is not readable.","documentCount":0,"chunkCount":0,"refusedCount":0,"more":false}
        ]""",
        "ListAgentSourceAttachments" to """[{"sourceId":"src1","undecided":3,"declined":1,"described":5}]""",
    ),
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
