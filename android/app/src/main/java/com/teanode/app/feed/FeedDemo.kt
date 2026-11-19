package com.teanode.app.feed

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.graphQLField
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.chats.AgentReference
import com.teanode.kit.feed.AgentFeedChoice
import com.teanode.kit.feed.AgentFeedFactRow
import com.teanode.kit.feed.AgentFeedItemView
import com.teanode.kit.feed.AgentFeedPage
import com.teanode.kit.feed.AgentFeedSourceLink
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant

private fun minutesAgo(minutes: Long): String = Instant.now().minusSeconds(minutes * 60).toString()

private fun choice(key: String, label: String, role: String) = AgentFeedChoice(key, label, role)

/** The identifiers of the demo cards the screenshot tests open one by one. */
object FeedDemoIDs {
    const val SIGN_IN = "demo-sign-in"
    const val WEATHER = "demo-weather"
    const val NEWS = "demo-news"
    const val PENDING = "demo-pending"
    const val SETTLED = "demo-settled"
    const val REPORT = "demo-report"
    const val MAIL_ORGANIZE = "demo-mail-organize"
    const val CODING_OPTIONS = "demo-coding-options"
    const val CODING_OPEN = "demo-coding-open"
    const val IDEA = "demo-idea"
    const val UNKNOWN_SYMBOL = "demo-unknown-symbol"
    const val BRIEF_AGENDA = "demo-brief-agenda"
    const val BRIEF_WEATHER = "demo-brief-weather"
    const val BRIEF_TECH = "demo-brief-tech"
    const val BRIEF_HEADLINES = "demo-brief-headlines"
    const val BRIEF_MENTIONS = "demo-brief-mentions"
    const val BRIEF_REVIEWS = "demo-brief-reviews"
    const val BRIEF_MAIL = "demo-brief-mail"
    const val BAD_LAYOUT = "demo-bad-layout"
    const val BRIEF_STACK_KEY = "schedule:brief:run-1"
    const val ROBOTS_STACK_KEY = "goal:robots"
    const val IDEAS_STACK_KEY = "speak_first:ideas"
}

private fun todayAt(hour: Int, minute: Int): String = java.time.LocalDate.now().atTime(hour, minute).toString() + ":00"

private val briefAgendaContent = """{"agendaEntries":[
  {"startsAt":"${todayAt(9, 30)}","endsAt":"${todayAt(10, 0)}","agendaTitle":"Planning call","agendaEntryKind":"meeting","agendaDetail":"Room 2, with Alex and Sam"},
  {"startsAt":"${todayAt(10, 0)}","endsAt":"${todayAt(12, 0)}","agendaTitle":"Write the launch notes","agendaEntryKind":"focus","agendaDetail":"No meetings until lunch"},
  {"startsAt":"${todayAt(12, 0)}","endsAt":"${todayAt(13, 0)}","agendaTitle":"Lunch","agendaEntryKind":"break"},
  {"startsAt":"${todayAt(14, 0)}","endsAt":"${todayAt(14, 30)}","agendaTitle":"File the print shop receipt","agendaEntryKind":"task","agendaDetail":"It is in your inbox","agendaRecordReference":{"itemId":"abc123","subject":"Receipt from the print shop"}},
  {"startsAt":"${todayAt(16, 0)}","endsAt":"${todayAt(17, 0)}","agendaTitle":"Review with the design team","agendaEntryKind":"meeting"}]}"""

private val briefWeatherContent = """{"placeName":"Lisbon","currentTemperature":18.4,"temperatureUnit":"C","conditionText":"Mostly sunny","conditionSymbol":"cloud.sun","highTemperature":21,"lowTemperature":13,
  "hourlyForecast":[{"forecastHour":"${todayAt(8, 0)}","forecastTemperature":16,"forecastSymbol":"sun.max","precipitationChancePercent":0},
    {"forecastHour":"${todayAt(10, 0)}","forecastTemperature":18,"forecastSymbol":"cloud.sun","precipitationChancePercent":5},
    {"forecastHour":"${todayAt(12, 0)}","forecastTemperature":20,"forecastSymbol":"cloud.sun","precipitationChancePercent":10},
    {"forecastHour":"${todayAt(14, 0)}","forecastTemperature":21,"forecastSymbol":"cloud","precipitationChancePercent":20},
    {"forecastHour":"${todayAt(16, 0)}","forecastTemperature":19,"forecastSymbol":"cloud.rain","precipitationChancePercent":45},
    {"forecastHour":"${todayAt(18, 0)}","forecastTemperature":17,"forecastSymbol":"cloud.rain","precipitationChancePercent":30},
    {"forecastHour":"${todayAt(20, 0)}","forecastTemperature":15,"forecastSymbol":"moon","precipitationChancePercent":0}]}"""

private val briefTechContent = """{"listRows":[
  {"rowTitle":"A warehouse robot learns to unload trucks","rowDetail":"No ramp needed, and it works through a shift.","rowMeta":"Example Robotics Weekly","rowSymbol":"newspaper","rowUrl":"https://example.com/robots"},
  {"rowTitle":"A small model that runs on a phone","rowDetail":"The maker says it matches last year's large ones on reading tasks.","rowMeta":"Example AI Review","rowSymbol":"brain","rowUrl":"https://example.com/small-model"},
  {"rowTitle":"A faster way to build large projects","rowMeta":"Example Dev Notes","rowSymbol":"terminal","rowUrl":"https://example.org/build"}]}"""

private val briefHeadlinesContent = """{"listRows":[
  {"rowTitle":"Rail strike called off after a late deal","rowMeta":"Example Daily","rowUrl":"https://example.com/rail"},
  {"rowTitle":"City council approves the new bike lanes","rowMeta":"Example Times","rowUrl":"https://example.org/bike-lanes"},
  {"rowTitle":"Storm warning for the coast tonight","rowMeta":"Example Weather Desk","rowSymbol":"cloud.rain","rowUrl":"https://example.org/storm"}]}"""

private val briefMentionsContent = """{"listRows":[
  {"rowTitle":"Sam mentioned you","rowDetail":"Can you take a look at the launch notes before lunch?","rowMeta":"#launch, 08:41","rowSymbol":"person"},
  {"rowTitle":"Alex mentioned you","rowDetail":"Thanks for the quick fix, merging now.","rowMeta":"#builds, yesterday","rowSymbol":"person"}]}"""

private val briefReviewsContent = """{"listRows":[
  {"rowTitle":"Add retry to the upload client","rowDetail":"Review requested by Alex","rowMeta":"example/app, 2 days open","rowSymbol":"chevron.left.forwardslash.chevron.right","rowUrl":"https://example.com/review/41"},
  {"rowTitle":"Fix the empty state of the search page","rowDetail":"Assigned to you","rowMeta":"example/web, 5 days open","rowSymbol":"exclamationmark.triangle","rowUrl":"https://example.com/issues/12"}]}"""

private val briefMailContent = """{"listRows":[
  {"rowTitle":"Receipt from the print shop","rowDetail":"Your order of twenty posters is ready.","rowMeta":"orders@example.com","rowSymbol":"envelope","rowRecordReference":{"itemId":"abc123","subject":"Receipt from the print shop"}},
  {"rowTitle":"Lake house booking confirmed","rowDetail":"June 12 to June 15, two guests.","rowMeta":"stays@example.com","rowSymbol":"envelope","rowRecordReference":{"itemId":"def456","subject":"Lake house booking confirmed"}}]}"""

private fun briefCard(id: String, position: Int, title: String, body: String, symbol: String, layout: String, content: String, minutes: Long = 420) = AgentFeedItemView(
    feedItemId = id, feedItemKind = "report", feedSourceKind = "schedule", feedSourceId = "brief", feedSourceTitle = "Morning brief", title = title, body = body, pictureSymbol = symbol,
    feedCardLayout = layout, layoutContent = content, feedStackKey = FeedDemoIDs.BRIEF_STACK_KEY, feedStackTitle = "Morning brief, Wed Nov 19", stackPosition = position,
    postedAt = minutesAgo(minutes), updatedAt = minutesAgo(minutes),
)

/** The seven cards of a 7 AM brief, in the order the server keeps them. */
val demoBriefCards: List<AgentFeedItemView> = listOf(
    briefCard(FeedDemoIDs.BRIEF_AGENDA, 0, "Your day", "Five things on today's plan, starting with the planning call at 09:30.", "calendar", "agenda", briefAgendaContent),
    briefCard(FeedDemoIDs.BRIEF_WEATHER, 1, "Weather in Lisbon", "18 C and mostly sunny, with rain likely after four.", "cloud.sun", "weather", briefWeatherContent),
    briefCard(FeedDemoIDs.BRIEF_TECH, 2, "Tech news", "Three stories on robots, small models and builds.", "sparkles", "list", briefTechContent),
    briefCard(FeedDemoIDs.BRIEF_HEADLINES, 3, "Headlines", "Rail, bike lanes and a storm warning.", "newspaper", "list", briefHeadlinesContent),
    briefCard(FeedDemoIDs.BRIEF_MENTIONS, 4, "Mentions in chat", "Sam and Alex wrote to you.", "text.bubble", "list", briefMentionsContent),
    briefCard(FeedDemoIDs.BRIEF_REVIEWS, 5, "Open reviews", "One review is waiting for you and one issue is assigned to you.", "chevron.left.forwardslash.chevron.right", "list", briefReviewsContent),
    briefCard(FeedDemoIDs.BRIEF_MAIL, 6, "Mail worth reading", "Two messages you will want to see.", "envelope", "list", briefMailContent),
)

private fun withStack(card: AgentFeedItemView, count: Int, unread: Int, key: String, title: String = "") = card.copy(feedStackKey = key, feedStackTitle = title, stackItemCount = count, stackUnreadCount = unread)

private val signInChoices = listOf(choice("me", "That's me", "dismiss"), choice("secure", "Secure my account", "action"))

/** A card of every kind and state the feed shows, newest first, invented. A stack is its top card. */
private val demoPlainCards: List<AgentFeedItemView> = listOf(
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.SIGN_IN, feedItemKind = "alert", feedSourceKind = "alert", title = "New sign-in from a laptop",
        body = "Someone signed in to your account from a laptop in Lisbon at 08:12. Was this you?", pictureSymbol = "lock.shield",
        factRows = listOf(AgentFeedFactRow("Device", "Laptop, Firefox"), AgentFeedFactRow("Place", "Lisbon"), AgentFeedFactRow("Time", "08:12")),
        choices = signInChoices, feedUrgency = "now", postedAt = minutesAgo(6), updatedAt = minutesAgo(6),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.CODING_OPTIONS, feedItemKind = "decision", feedSourceKind = "coding_session", feedSourceId = "workstation/%3",
        title = "Claude Code on workstation asks", body = "Tests failed in two packages. Run them again with the race detector?", pictureSymbol = "terminal",
        choices = listOf(choice("option:1", "Yes, run again", "action"), choice("option:2", "No, stop here", "action")),
        feedUrgency = "now", postedAt = minutesAgo(14), updatedAt = minutesAgo(14),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.MAIL_ORGANIZE, feedItemKind = "mail_organize", feedSourceKind = "schedule", feedSourceTitle = "Weekly mail tidy-up",
        title = "Tidy your mail this week", body = "Forty-one newsletters and receipts reached your inbox this week. I can file them for you and keep doing it.",
        pictureSymbol = "tray.full",
        factRows = listOf(AgentFeedFactRow("New folder", "Newsletters"), AgentFeedFactRow("New folder", "Receipts"), AgentFeedFactRow("New rule", "Mail from news@example.com"), AgentFeedFactRow("Messages moved", "41")),
        choices = listOf(choice("approve", "Approve", "action"), choice("discuss", "Discuss", "discuss"), choice("not-now", "Not now", "dismiss")),
        feedUrgency = "today", postedAt = minutesAgo(50), updatedAt = minutesAgo(50),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.PENDING, feedItemKind = "decision", feedSourceKind = "agent", title = "Sort receipts into a folder",
        body = "Eleven receipts are in your inbox. Approve to move them into a Receipts folder.", pictureSymbol = "tray",
        choices = listOf(choice("approve", "Approve", "action"), choice("not-now", "Not now", "dismiss")),
        chosenChoiceKey = "approve", chosenAt = minutesAgo(1), isOutcomePending = true, isRead = true, postedAt = minutesAgo(90), updatedAt = minutesAgo(1),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.SETTLED, feedItemKind = "decision", feedSourceKind = "alert", title = "Sign-in from a new phone",
        body = "A phone signed in to your account yesterday evening.", pictureSymbol = "lock.shield", choices = signInChoices,
        chosenChoiceKey = "secure", outcomeText = "Signed out the other session and asked for a new password.", isRead = true,
        postedAt = minutesAgo(60 * 20), updatedAt = minutesAgo(60 * 19),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.WEATHER, feedItemKind = "update", feedSourceKind = "goal", feedSourceTitle = "Weather where I travel", title = "Lisbon, today",
        body = "Mostly sunny, with a light wind from the sea in the afternoon.", pictureSymbol = "cloud.sun",
        factRows = listOf(AgentFeedFactRow("High", "21 C"), AgentFeedFactRow("Low", "14 C"), AgentFeedFactRow("Rain", "5 percent")),
        sourceLinks = listOf(AgentFeedSourceLink("Forecast", "https://example.com/forecast")), isRead = true, postedAt = minutesAgo(180), updatedAt = minutesAgo(120),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.NEWS, feedItemKind = "news", feedSourceKind = "goal", feedSourceTitle = "News about humanoid robots", title = "A warehouse robot learns to unload trucks",
        body = "A maker of humanoid robots showed one unloading boxes from a truck without a ramp. Read the [full story](https://example.com/robots) for the details.",
        pictureSymbol = "newspaper", sourceLinks = listOf(AgentFeedSourceLink("The full story", "https://example.com/robots"), AgentFeedSourceLink("The maker's announcement", "https://example.org/announcement")),
        isLiked = true, isRead = true, postedAt = minutesAgo(60 * 5), updatedAt = minutesAgo(60 * 5),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.REPORT, feedItemKind = "report", feedSourceKind = "schedule", feedSourceTitle = "Monday summary", title = "Your week ahead",
        body = "Three meetings, one trip on Thursday, and a receipt that needs filing.", pictureSymbol = "calendar",
        recordReferences = listOf(AgentReference(itemId = "abc123", subject = "Receipt from the print shop"), AgentReference(recordingId = "rec1", recordingTitle = "Planning call"), AgentReference(photoId = "p1", photoTitle = "Whiteboard photo")),
        isRead = true, postedAt = minutesAgo(60 * 26), updatedAt = minutesAgo(60 * 26),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.CODING_OPEN, feedItemKind = "decision", feedSourceKind = "coding_session", feedSourceId = "workstation/%5",
        title = "Codex on workstation asks", body = "Which name should the new command have? It needs a few words, not an option.", pictureSymbol = "terminal",
        choices = listOf(choice("open-session", "Open session", "open")), isRead = true, postedAt = minutesAgo(60 * 30), updatedAt = minutesAgo(60 * 30),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.IDEA, feedItemKind = "idea", feedSourceKind = "speak_first", title = "Plan the lake house weekend",
        body = "Three emails mention a long weekend in June. Want me to draft a packing list?", pictureSymbol = "🏡",
        choices = listOf(choice("discuss", "Talk it over", "discuss")), isRead = true, postedAt = minutesAgo(60 * 48), updatedAt = minutesAgo(60 * 48),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.UNKNOWN_SYMBOL, feedItemKind = "update", feedSourceKind = "agent", title = "Backup finished",
        body = "Last night's backup took 14 minutes and kept 30 days of history.", pictureSymbol = "externaldrive.badge.checkmark", isRead = true,
        postedAt = minutesAgo(60 * 60), updatedAt = minutesAgo(60 * 60),
    ),
)

private val demoBadLayoutCard = AgentFeedItemView(
    feedItemId = FeedDemoIDs.BAD_LAYOUT, feedItemKind = "update", feedSourceKind = "agent", title = "A card in a layout this app does not know",
    body = "The agent sent a layout the app cannot draw, so the card shows its text instead.", pictureSymbol = "newspaper",
    feedCardLayout = "carousel", layoutContent = "{\"slides\":[]}", isRead = true, postedAt = minutesAgo(60 * 70), updatedAt = minutesAgo(60 * 70),
)

/** The feed's entries, newest first: a stack is its top card with the count of the pile under it. */
val demoFeedCards: List<AgentFeedItemView> = buildList {
    for (card in demoPlainCards) {
        when (card.feedItemId) {
            FeedDemoIDs.PENDING -> {
                add(withStack(demoBriefCards.first(), 7, 7, FeedDemoIDs.BRIEF_STACK_KEY, "Morning brief, Wed Nov 19"))
                add(card)
            }
            FeedDemoIDs.NEWS -> add(withStack(card, 4, 0, FeedDemoIDs.ROBOTS_STACK_KEY))
            FeedDemoIDs.IDEA -> add(withStack(card, 2, 1, FeedDemoIDs.IDEAS_STACK_KEY))
            else -> add(card)
        }
    }
    add(demoBadLayoutCard)
}

/** What a stack's own screen lists: the brief's seven in order, and for any other stack a few cards of the same source. */
private fun stackCards(feedStackKey: String): List<AgentFeedItemView> = when (feedStackKey) {
    FeedDemoIDs.BRIEF_STACK_KEY -> demoBriefCards
    FeedDemoIDs.IDEAS_STACK_KEY -> listOf(
        demoFeedCards.first { card -> card.feedItemId == FeedDemoIDs.IDEA }.copy(stackItemCount = 1, stackUnreadCount = 0),
        demoFeedCards.first { card -> card.feedItemId == FeedDemoIDs.IDEA }.copy(feedItemId = "demo-idea-older", title = "Make a list of what to fix at home", body = "Walk through each room and let me keep the list.", stackItemCount = 1, stackUnreadCount = 0, postedAt = minutesAgo(60 * 100), updatedAt = minutesAgo(60 * 100)),
    )
    else -> {
        val top = demoFeedCards.first { card -> card.feedItemId == FeedDemoIDs.NEWS }.copy(stackItemCount = 1)
        listOf(
            top,
            top.copy(feedItemId = "demo-news-2", title = "A small model that runs on a phone", body = "The maker says it matches last year's large ones.", isLiked = false, updatedAt = minutesAgo(60 * 9), postedAt = minutesAgo(60 * 9)),
            top.copy(feedItemId = "demo-news-3", title = "A faster way to build large projects", body = "A new build tool caches every step.", isLiked = false, updatedAt = minutesAgo(60 * 30), postedAt = minutesAgo(60 * 30)),
            top.copy(feedItemId = "demo-news-4", title = "A robot hand with a sense of touch", body = "Its fingers feel pressure as small as a coin.", isLiked = false, updatedAt = minutesAgo(60 * 52), postedAt = minutesAgo(60 * 52)),
        )
    }
}

private fun json(item: AgentFeedItemView): String = TeaNodeJson.encodeToString(AgentFeedItemView.serializer(), item)

/** What the demo makes of a choice: a dismiss closes the card, an action sends the agent to work (the card is pending). */
private fun answeredWith(item: AgentFeedItemView, choiceKey: String): AgentFeedItemView {
    val chosen = item.choices.firstOrNull { each -> each.choiceKey == choiceKey } ?: return item
    return if (chosen.choiceRole == "dismiss") item.copy(chosenChoiceKey = choiceKey, isDismissed = true) else item.copy(chosenChoiceKey = choiceKey, isOutcomePending = true, isRead = true)
}

private fun cardOf(feedItemId: String): AgentFeedItemView =
    (demoFeedCards + demoBriefCards + stackCards(FeedDemoIDs.ROBOTS_STACK_KEY)).firstOrNull { card -> card.feedItemId == feedItemId } ?: demoFeedCards.first()

/** What demo mode answers for Feed: every kind and state of card, and answers that change a card the way the server would. */
val feedDemoAnswerers: List<DemoAnswerer> = listOf(
    graphQLField("ListAgentFeed") {
        val page = AgentFeedPage(demoFeedCards, null, demoFeedCards.count { card -> !card.isRead })
        TeaNodeJson.encodeToString(AgentFeedPage.serializer(), page)
    },
    graphQLField("ListAgentFeedStack") { variables ->
        val cards = stackCards((variables["feedStackKey"] as? JsonPrimitive)?.content.orEmpty())
        TeaNodeJson.encodeToString(AgentFeedPage.serializer(), AgentFeedPage(cards, null, cards.count { card -> !card.isRead }))
    },
    graphQLField("GetAgentFeedItem") { variables -> json(cardOf((variables["feedItemId"] as? JsonPrimitive)?.content.orEmpty())) },
    graphQLField("ChooseAgentFeedItem") { variables ->
        json(answeredWith(cardOf((variables["feedItemId"] as? JsonPrimitive)?.content.orEmpty()), (variables["choiceKey"] as? JsonPrimitive)?.content.orEmpty()))
    },
    graphQLField("LikeAgentFeedItem") { variables ->
        json(cardOf((variables["feedItemId"] as? JsonPrimitive)?.content.orEmpty()).copy(isLiked = (variables["isLiked"] as? JsonPrimitive)?.content == "true"))
    },
    graphQLField("ShowLessLikeAgentFeedItem") { variables -> json(cardOf((variables["feedItemId"] as? JsonPrimitive)?.content.orEmpty()).copy(isDismissed = true)) },
    graphQLField("DismissAgentFeedItem") { variables -> json(cardOf((variables["feedItemId"] as? JsonPrimitive)?.content.orEmpty()).copy(isDismissed = true)) },
    graphQLField("MarkAgentFeedRead", "1"),
    graphQLField("DiscussAgentFeedItem", "\"demo-main\""),
)
