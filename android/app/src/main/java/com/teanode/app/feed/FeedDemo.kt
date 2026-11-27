package com.teanode.app.feed

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.DemoResponse
import com.teanode.app.demo.graphQLField
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.feed.AgentFeedChoice
import com.teanode.kit.feed.AgentFeedChartBar
import com.teanode.kit.feed.AgentFeedChartBlock
import com.teanode.kit.feed.AgentFeedChartMarker
import com.teanode.kit.feed.AgentFeedChartSeries
import com.teanode.kit.feed.AgentFeedFactRow
import com.teanode.kit.feed.AgentFeedItemView
import com.teanode.kit.feed.AgentFeedMediaView
import com.teanode.kit.feed.AgentFeedPage
import com.teanode.kit.feed.AgentFeedSourceLink
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.Shader
import java.io.ByteArrayOutputStream
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
    const val MEDIA_PORTRAIT = "demo-media-portrait"
    const val MEDIA_LANDSCAPE = "demo-media-landscape"
    const val MEDIA_GRID_TWO = "demo-media-grid-two"
    const val MEDIA_GRID_THREE = "demo-media-grid-three"
    const val MEDIA_GRID_FOUR = "demo-media-grid-four"
    const val MEDIA_ROW_SIX = "demo-media-row-six"
    const val MEDIA_VIDEO = "demo-media-video"
    const val MEDIA_PICTURE = "demo-media-picture"
    const val MEDIA_UNSIZED = "demo-media-unsized"
    const val RESOLVED = "demo-resolved"
    const val CHART_SERIES = "demo-chart-series"
    const val CHART_BARS = "demo-chart-bars"
    const val CAMERA_STILL = "demo-camera-still"
    const val ANSWERED_FOLDED = "demo-answered-folded"
    const val BRIEF_STACK_KEY = "schedule:brief:run-1"
    const val ROBOTS_STACK_KEY = "goal:robots"
    const val IDEAS_STACK_KEY = "speak_first:ideas"
    const val DISMISSED_CHOICES = "demo-dismissed-choices"
    const val DISMISSED_WATERING_NEW = "demo-dismissed-watering-new"
    const val DISMISSED_WATERING_OLD = "demo-dismissed-watering-old"
    const val DISMISSED_NEWS = "demo-dismissed-news"
    const val WATERING_STACK_KEY = "goal:garden-watering"
}

/** A picture of a card, drawn on the spot by [feedDemoPictureBytes] at the size it says. */
private fun media(key: String, kind: String = "photo", width: Int = 1600, height: Int = 900, caption: String = "", site: String = "", link: String = "", rowIndex: Int? = null) = AgentFeedMediaView(
    mediaKey = key, feedMediaKind = kind, imageUrl = "/demo/feed/media/$key/$width/$height", videoUrl = if (kind == "video") "/demo/feed/video/$key" else "", linkUrl = link,
    siteName = site, mediaCaption = caption, widthPixels = width, heightPixels = height, listRowIndex = rowIndex,
)

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
    feedItemId = id, feedItemKind = "report", feedSourceKind = "goal", feedSourceId = "brief", feedSourceTitle = "Morning brief", title = title, body = body, pictureSymbol = symbol,
    feedCardLayout = layout, layoutContent = content, feedStackKey = FeedDemoIDs.BRIEF_STACK_KEY, feedStackTitle = "Morning brief, Wed Nov 19", stackPosition = position,
    postedAt = minutesAgo(minutes), updatedAt = minutesAgo(minutes),
)

/** The seven cards of a 7 AM brief, in the order the server keeps them. */
val demoBriefCards: List<AgentFeedItemView> = listOf(
    briefCard(FeedDemoIDs.BRIEF_AGENDA, 0, "Your day", "Five things on today's plan, starting with the planning call at 09:30.", "calendar", "agenda", briefAgendaContent),
    briefCard(FeedDemoIDs.BRIEF_WEATHER, 1, "Weather in Lisbon", "18 C and mostly sunny, with rain likely after four.", "cloud.sun", "weather", briefWeatherContent),
    briefCard(FeedDemoIDs.BRIEF_TECH, 2, "Tech news", "Three stories on robots, small models and builds.", "sparkles", "list", briefTechContent)
        .copy(feedMedia = (0..2).map { index -> media("tech-$index", "picture", 800, 800, link = "https://example.com/story-$index", rowIndex = index) }),
    briefCard(FeedDemoIDs.BRIEF_HEADLINES, 3, "Headlines", "Rail, bike lanes and a storm warning.", "newspaper", "list", briefHeadlinesContent)
        .copy(feedMedia = listOf(media("headline-0", "picture", 800, 800, rowIndex = 0), media("headline-2", "picture", 800, 800, rowIndex = 2))),
    briefCard(FeedDemoIDs.BRIEF_MENTIONS, 4, "Mentions in chat", "Sam and Alex wrote to you.", "text.bubble", "list", briefMentionsContent),
    briefCard(FeedDemoIDs.BRIEF_REVIEWS, 5, "Open reviews", "One review is waiting for you and one issue is assigned to you.", "chevron.left.forwardslash.chevron.right", "list", briefReviewsContent),
    briefCard(FeedDemoIDs.BRIEF_MAIL, 6, "Mail worth reading", "Two messages you will want to see.", "envelope", "list", briefMailContent)
        .copy(feedMedia = listOf(media("mail-0", "photo", 800, 800, rowIndex = 0))),
)

/** The dismissed cards of the demo, the most recently dismissed first; two share a stack, so they are drawn as a deck. Invented. */
val demoDismissedCards: List<AgentFeedItemView> = listOf(
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.DISMISSED_CHOICES, feedItemKind = "decision", feedSourceKind = "agent", title = "Renew the library card",
        body = "Your library card runs out on Friday. Renewing takes one step.", pictureSymbol = "book",
        choices = listOf(choice("approve", "Renew it", "action"), choice("not-now", "Not now", "dismiss")),
        isDismissed = true, dismissedAt = minutesAgo(35), postedAt = minutesAgo(60 * 30), updatedAt = minutesAgo(60 * 30),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.DISMISSED_WATERING_NEW, feedItemKind = "report", feedSourceKind = "goal", feedSourceTitle = "Garden watering",
        title = "Watering plan for Thursday", body = "No rain until the weekend. Water the beds before eight in the morning.", pictureSymbol = "cloud.sun",
        feedStackKey = FeedDemoIDs.WATERING_STACK_KEY, feedStackTitle = "Garden watering",
        isRead = true, isDismissed = true, dismissedAt = minutesAgo(60 * 3), postedAt = minutesAgo(60 * 26), updatedAt = minutesAgo(60 * 26),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.DISMISSED_WATERING_OLD, feedItemKind = "report", feedSourceKind = "goal", feedSourceTitle = "Garden watering",
        title = "Watering plan for Tuesday", body = "A shower is likely at noon, so the beds can wait until evening.", pictureSymbol = "cloud.rain",
        feedStackKey = FeedDemoIDs.WATERING_STACK_KEY, feedStackTitle = "Garden watering",
        isRead = true, isDismissed = true, dismissedAt = minutesAgo(60 * 3 + 1), postedAt = minutesAgo(60 * 74), updatedAt = minutesAgo(60 * 74),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.DISMISSED_NEWS, feedItemKind = "news", feedSourceKind = "agent", title = "A new bakery opens on Mill Street",
        body = "It opens on Saturday with a free loaf for the first fifty visitors.", pictureSymbol = "newspaper",
        factRows = listOf(AgentFeedFactRow("Opens", "Saturday, 7:00"), AgentFeedFactRow("Where", "Mill Street")),
        isRead = true, isLiked = true, isDismissed = true, dismissedAt = minutesAgo(60 * 28), postedAt = minutesAgo(60 * 50), updatedAt = minutesAgo(60 * 50),
    ),
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
        feedItemId = FeedDemoIDs.RESOLVED, feedItemKind = "alert", feedSourceKind = "goal", feedSourceTitle = "Backup watch", title = "Last night's backup did not finish",
        body = "The backup stopped at 02:10 because the disk was full. Free some space and run it again.", pictureSymbol = "externaldrive",
        factRows = listOf(AgentFeedFactRow("Finished", "No"), AgentFeedFactRow("Disk free", "1.2 GB")),
        isResolved = true, resolvedAt = minutesAgo(25), resolutionNote = "The next run finished and the disk has 40 GB free.", resolvedByKind = "goal",
        isRead = true, postedAt = minutesAgo(60 * 9), updatedAt = minutesAgo(60 * 9),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.CHART_SERIES, feedItemKind = "report", feedSourceKind = "goal", feedSourceTitle = "Weekly money check", title = "Your checking balance is below last week's",
        body = "The rent went out on Wednesday. At this pace the week ends a little under last week.", pictureSymbol = "chart.line.uptrend.xyaxis",
        chartBlocks = listOf(
            AgentFeedChartBlock(
                chartKind = "series", chartTitle = "Checking balance", unit = "USD", provenance = "Read from the bank at 09:40 EST",
                xLabels = listOf("Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"),
                series = listOf(
                    AgentFeedChartSeries("This week", "measured", listOf(2450.0, 2410.0, 1310.0, 1275.0, null, null, null)),
                    AgentFeedChartSeries("Last week", "reference", listOf(2500.0, 2460.0, 2300.0, 2240.0, 2190.0, 2120.0, 2080.0)),
                    AgentFeedChartSeries("Expected", "projection", listOf(null, null, null, 1275.0, 1230.0, 1190.0, 1150.0)),
                ),
                markers = listOf(AgentFeedChartMarker("Wed", "Rent paid")),
            ),
        ),
        isRead = true, postedAt = minutesAgo(60 * 4), updatedAt = minutesAgo(60 * 4),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.CHART_BARS, feedItemKind = "report", feedSourceKind = "goal", feedSourceTitle = "Weekly money check", title = "Where this month's money went",
        body = "Groceries are the largest part so far.", pictureSymbol = "chart.bar",
        chartBlocks = listOf(
            AgentFeedChartBlock(
                chartKind = "bars", chartTitle = "Spending by category", unit = "USD", provenance = "Read from the bank at 09:40 EST, through yesterday",
                bars = listOf(AgentFeedChartBar("Groceries", 420.5), AgentFeedChartBar("Transport", 180.0), AgentFeedChartBar("Dining out", 96.25), AgentFeedChartBar("Subscriptions", 45.0)),
            ),
        ),
        isRead = true, postedAt = minutesAgo(60 * 4 + 5), updatedAt = minutesAgo(60 * 4 + 5),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.CAMERA_STILL, feedItemKind = "update", feedSourceKind = "goal", feedSourceTitle = "Doorstep watch", title = "A parcel was left at the front door",
        body = "A courier left a box at 14:02. It is on the mat, out of the rain.", pictureSymbol = "video",
        feedMedia = listOf(
            AgentFeedMediaView(
                mediaKey = "still", feedMediaKind = "camera_still", imageUrl = "/demo/feed/media/still/1600/900", widthPixels = 1600, heightPixels = 900,
                mediaCaption = "A box on the doormat", cameraName = "Front door", capturedAt = java.time.LocalDate.now().toString() + "T14:02:11+09:00",
                stillReason = "The camera at the front door, where the parcel was left", playbackUrl = "https://example.com/camera/front-door/playback", liveUrl = "https://example.com/camera/front-door/live",
            ),
        ),
        isRead = true, postedAt = minutesAgo(40), updatedAt = minutesAgo(40),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.ANSWERED_FOLDED, feedItemKind = "decision", feedSourceKind = "agent", title = "Move the dentist visit to Friday",
        body = "The clinic has a free slot on Friday at 10:30. Moving it keeps the same dentist.", pictureSymbol = "calendar",
        choices = listOf(choice("approve", "Move it", "action"), choice("not-now", "Keep Thursday", "dismiss")),
        chosenChoiceKey = "approve", outcomeText = "Moved to Friday at 10:30 and added to your calendar.", isRead = true, postedAt = minutesAgo(60 * 6), updatedAt = minutesAgo(60 * 5),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.MAIL_ORGANIZE, feedItemKind = "mail_organize", feedSourceKind = "goal", feedSourceTitle = "Weekly mail tidy-up",
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
        feedItemId = FeedDemoIDs.MEDIA_PORTRAIT, feedItemKind = "update", feedSourceKind = "agent", title = "Sunrise at the lake house",
        body = "Sam sent this one at six this morning.", pictureSymbol = "photo", feedMedia = listOf(media("portrait", "photo", 1200, 1500, caption = "The dock, just after sunrise")),
        isRead = true, postedAt = minutesAgo(200), updatedAt = minutesAgo(200),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.MEDIA_LANDSCAPE, feedItemKind = "update", feedSourceKind = "agent", title = "Your photos from yesterday",
        body = "Twelve photos from the park were added to your library.", pictureSymbol = "photo", feedMedia = listOf(media("landscape", "photo", 1600, 900, caption = "By the pond")),
        isRead = true, postedAt = minutesAgo(210), updatedAt = minutesAgo(210),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.MEDIA_GRID_TWO, feedItemKind = "update", feedSourceKind = "agent", title = "Two views of the garden",
        body = "Before and after the weekend's work.", pictureSymbol = "photo", feedMedia = listOf(media("two-a", caption = "Before"), media("two-b", caption = "After")),
        isRead = true, postedAt = minutesAgo(215), updatedAt = minutesAgo(215),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.MEDIA_GRID_THREE, feedItemKind = "update", feedSourceKind = "agent", title = "Three from the trip",
        body = "The first three photos of the weekend away.", pictureSymbol = "photo",
        feedMedia = listOf(media("three-a", caption = "Harbor"), media("three-b", caption = "Market"), media("three-c", caption = "Ferry")),
        isRead = true, postedAt = minutesAgo(220), updatedAt = minutesAgo(220),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.MEDIA_GRID_FOUR, feedItemKind = "update", feedSourceKind = "agent", title = "Four from the market",
        body = "Fruit, bread, flowers and cheese.", pictureSymbol = "photo", feedMedia = (0..3).map { index -> media("four-$index") },
        isRead = true, postedAt = minutesAgo(225), updatedAt = minutesAgo(225),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.MEDIA_ROW_SIX, feedItemKind = "update", feedSourceKind = "agent", title = "Six from the holiday album",
        body = "Scroll sideways for the rest.", pictureSymbol = "photo", feedMedia = (0..5).map { index -> media("six-$index", caption = "Day ${index + 1}") },
        isRead = true, postedAt = minutesAgo(230), updatedAt = minutesAgo(230),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.MEDIA_VIDEO, feedItemKind = "update", feedSourceKind = "agent", title = "A clip from the recital",
        body = "Thirty seconds, from the second row.", pictureSymbol = "video", feedMedia = listOf(media("clip", "video", 1920, 1080, caption = "The last song")),
        isRead = true, postedAt = minutesAgo(235), updatedAt = minutesAgo(235),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.MEDIA_UNSIZED, feedItemKind = "update", feedSourceKind = "agent", feedSourceTitle = "Your photos", title = "A photo with no size on record",
        body = "The server did not say how big it is, so it is drawn at 16:9.", pictureSymbol = "photo",
        feedMedia = listOf(AgentFeedMediaView(mediaKey = "unsized", feedMediaKind = "photo", imageUrl = "/demo/feed/media/unsized/1600/900")),
        isRead = true, postedAt = minutesAgo(238), updatedAt = minutesAgo(238),
    ),
    AgentFeedItemView(
        feedItemId = FeedDemoIDs.MEDIA_PICTURE, feedItemKind = "news", feedSourceKind = "goal", feedSourceTitle = "News about humanoid robots", title = "A warehouse robot learns to unload trucks",
        body = "A maker of humanoid robots showed one unloading boxes from a truck without a ramp.", pictureSymbol = "newspaper",
        feedMedia = listOf(media("story", "picture", 1200, 675, site = "Example Robotics Weekly", link = "https://example.com/robots")),
        isRead = true, postedAt = minutesAgo(240), updatedAt = minutesAgo(240),
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
        feedItemId = FeedDemoIDs.REPORT, feedItemKind = "report", feedSourceKind = "goal", feedSourceTitle = "Monday summary", title = "Your week ahead",
        body = "Three meetings, one trip on Thursday, and a receipt that needs filing.", pictureSymbol = "calendar",
        factRows = listOf(AgentFeedFactRow("Meetings", "3"), AgentFeedFactRow("Trips", "1 on Thursday"), AgentFeedFactRow("To file", "1 receipt")),
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

/** The demo card with this id, whichever list holds it. */
fun demoFeedCard(feedItemId: String): AgentFeedItemView = cardOf(feedItemId)

private fun cardOf(feedItemId: String): AgentFeedItemView =
    (demoFeedCards + demoBriefCards + demoDismissedCards + stackCards(FeedDemoIDs.ROBOTS_STACK_KEY)).firstOrNull { card -> card.feedItemId == feedItemId } ?: demoFeedCards.first()

/** What demo mode answers for Feed: every kind and state of card, and answers that change a card the way the server would. */
val feedDemoAnswerers: List<DemoAnswerer> = listOf(
    // A picture of a card: /demo/feed/media/<key>/<width>/<height>, drawn on the spot, a scene in a hue of its own.
    DemoAnswerer { request ->
        val parts = request.path.split('/')
        if (request.path.startsWith("/demo/feed/media/") && parts.size >= 7) DemoResponse(feedDemoPictureBytes(parts[4], parts[5].toIntOrNull() ?: 800, parts[6].toIntOrNull() ?: 450)) else null
    },
    graphQLField("ListAgentFeed") { variables ->
        // Dismissed only: each dismissed card is an entry of its own, never folded into a stack.
        if ((variables["isDismissedOnly"] as? JsonPrimitive)?.content == "true") {
            return@graphQLField TeaNodeJson.encodeToString(AgentFeedPage.serializer(), AgentFeedPage(demoDismissedCards, null, demoFeedCards.count { card -> !card.isRead }))
        }
        val page = AgentFeedPage(demoFeedCards, null, demoFeedCards.count { card -> !card.isRead })
        TeaNodeJson.encodeToString(AgentFeedPage.serializer(), page)
    },
    graphQLField("ListAgentFeedStack") { variables ->
        val cards = stackCards((variables["feedStackKey"] as? JsonPrimitive)?.content.orEmpty())
        TeaNodeJson.encodeToString(AgentFeedPage.serializer(), AgentFeedPage(cards, null, cards.count { card -> !card.isRead }))
    },
    graphQLField("ChooseAgentFeedItem") { variables ->
        json(answeredWith(cardOf((variables["feedItemId"] as? JsonPrimitive)?.content.orEmpty()), (variables["choiceKey"] as? JsonPrimitive)?.content.orEmpty()))
    },
    graphQLField("LikeAgentFeedItem") { variables ->
        json(cardOf((variables["feedItemId"] as? JsonPrimitive)?.content.orEmpty()).copy(isLiked = (variables["isLiked"] as? JsonPrimitive)?.content == "true"))
    },
    graphQLField("ResolveAgentFeedItem") { variables ->
        val note = (variables["resolutionNote"] as? JsonPrimitive)?.content.orEmpty()
        json(cardOf((variables["feedItemId"] as? JsonPrimitive)?.content.orEmpty()).copy(isResolved = true, resolvedAt = Instant.now().toString(), resolutionNote = note, resolvedByKind = "person"))
    },
    graphQLField("UnresolveAgentFeedItem") { variables ->
        json(cardOf((variables["feedItemId"] as? JsonPrimitive)?.content.orEmpty()).copy(isResolved = false, resolvedAt = null, resolutionNote = "", resolvedByKind = ""))
    },
    graphQLField("ShowLessLikeAgentFeedItem") { variables -> json(cardOf((variables["feedItemId"] as? JsonPrimitive)?.content.orEmpty()).copy(isDismissed = true)) },
    // The demo keeps no record of what was dismissed: it answers with the ids the server would, and the feed is read as it was.
    graphQLField("DismissAgentFeedItems") { variables ->
        val feedItemIds = (variables["feedItemIds"] as? JsonArray)?.map { each -> (each as JsonPrimitive).content }
        val feedStackKey = (variables["feedStackKey"] as? JsonPrimitive)?.content
        val dismissedIds = when {
            feedItemIds != null -> feedItemIds
            !feedStackKey.isNullOrEmpty() -> stackCards(feedStackKey).map { card -> card.feedItemId }
            else -> (demoFeedCards + demoBriefCards).filter { card -> card.isRead && !card.isAwaitingChoice }.map { card -> card.feedItemId }
        }
        TeaNodeJson.encodeToString(ListSerializer(String.serializer()), dismissedIds)
    },
    graphQLField("RestoreAgentFeedItems") { variables ->
        val feedItemIds = (variables["feedItemIds"] as? JsonArray)?.map { each -> (each as JsonPrimitive).content }.orEmpty()
        TeaNodeJson.encodeToString(ListSerializer(String.serializer()), feedItemIds)
    },
    graphQLField("MarkAgentFeedRead", "1"),
    graphQLField("DiscussAgentFeedItem", "\"demo-main\""),
)

/**
 * A picture for the demo: a sky, a sun and two hills in a hue taken from [key], at the size asked (a tenth of it, so
 * the demo stays light), so the pictures of a grid are told apart at a glance.
 */
fun feedDemoPictureBytes(key: String, width: Int, height: Int): ByteArray {
    val pixelWidth = maxOf(16, width / 10)
    val pixelHeight = maxOf(16, height / 10)
    val hue = (((key.hashCode() and 0xffff) * 47) % 360).toFloat()
    val bitmap = Bitmap.createBitmap(pixelWidth, pixelHeight, Bitmap.Config.ARGB_8888)
    val canvas = Canvas(bitmap)
    val sky = Paint().apply { shader = LinearGradient(0f, 0f, 0f, pixelHeight.toFloat(), Color.HSVToColor(floatArrayOf(hue, 0.45f, 0.98f)), Color.HSVToColor(floatArrayOf((hue + 30f) % 360f, 0.2f, 1f)), Shader.TileMode.CLAMP) }
    canvas.drawRect(0f, 0f, pixelWidth.toFloat(), pixelHeight.toFloat(), sky)
    canvas.drawCircle(pixelWidth * 0.72f, pixelHeight * 0.28f, pixelHeight * 0.12f, Paint().apply { color = Color.rgb(255, 236, 170) })
    canvas.drawCircle(pixelWidth * 0.25f, pixelHeight * 1.1f, pixelHeight * 0.55f, Paint().apply { color = Color.HSVToColor(floatArrayOf((hue + 120f) % 360f, 0.5f, 0.55f)) })
    canvas.drawCircle(pixelWidth * 0.8f, pixelHeight * 1.2f, pixelHeight * 0.6f, Paint().apply { color = Color.HSVToColor(floatArrayOf((hue + 150f) % 360f, 0.55f, 0.4f)) })
    val output = ByteArrayOutputStream()
    bitmap.compress(Bitmap.CompressFormat.PNG, 100, output)
    return output.toByteArray()
}
