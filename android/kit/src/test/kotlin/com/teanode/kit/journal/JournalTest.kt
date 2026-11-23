package com.teanode.kit.journal

import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.calendar.fakeConnection
import com.teanode.kit.calendar.sentBody
import com.teanode.kit.calendar.sentVariables
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class JournalTest {
    private fun query(fake: com.teanode.kit.FakeServer, index: Int = 0): String = (sentBody(fake, index)["query"] as JsonPrimitive).content

    // MARK: Reading

    @Test
    fun aPageOfTheTimelineIsDecoded() = runBlocking {
        val answer = """
            {"data":{"ListJournalDays":{"days":[
              {"day":"2026-10-22","isToday":false,
               "mood":{"moodLevel":4,"moodWords":"tired, but the release is out","moodSource":"health"},
               "weather":{"weatherCode":61,"weatherKind":"rain","highCelsius":14.5,"lowCelsius":8,"precipitationMM":3.2,"placeName":"Riverside"},
               "headline":"Release day.","wordsExcerpt":"Shipped it.","hasWords":true,"facts":null,
               "coverPhotoId":"","photoIds":["photo-b","photo-c"],"hasNarrative":true},
              null,
              {"day":"2026-10-21","isToday":false,"mood":null,"weather":null,"headline":"","wordsExcerpt":"","hasWords":false,
               "facts":null,"coverPhotoId":"photo-a","photoIds":null,"hasNarrative":false}
            ],"totalCount":2,"today":"2026-10-26","firstDay":"2026-03-02"}}}
        """
        val (connection, fake) = fakeConnection { answer }
        val page = connection.journalDays(first = 14, offset = 28)
        assertEquals(listOf("2026-10-22", "2026-10-21"), page.days.map { day -> day.day })
        assertEquals("2026-10-26", page.today)
        assertEquals(JournalMoodLevel.Good, page.days[0].mood?.level)
        assertEquals(true, page.days[0].mood?.isFromHealth)
        assertEquals(JournalWeatherKind.Rain, page.days[0].weather?.kind)
        assertEquals("photo-b", page.days[0].shownPhotoId)
        // A null list is an empty one, and a cover wins over the first photo.
        assertEquals(emptyList<String>(), page.days[1].photoIds)
        assertEquals("photo-a", page.days[1].shownPhotoId)
        val sent = sentVariables(fake)
        assertEquals(JsonPrimitive(14), sent["first"])
        assertEquals(JsonPrimitive(28), sent["offset"])
        assertNull(sent["fromDay"])
    }

    private val dayAnswer = """
        {"data":{"ReadJournalDay":{
          "day":"2026-10-22","timeZone":"America/Los_Angeles","isToday":false,"weekday":"thursday","weekStart":"2026-10-19",
          "words":{"original":"Shipped it.","modifiedAt":"2026-10-22T22:48:03.123456789Z","isTouchUpOn":true,"touchedUp":"Shipped it, at last.",
                   "touchUpHow":"warmer","isTouchedUpStale":false,"touchUpPending":false,"journalNotes":null},
          "mood":null,"weather":null,"facts":null,
          "narrative":{"headline":"Release day.","narrative":"A release day.\n\nYou walked through [[place_visit:v1]].","references":[
              {"recordKind":"place_visit","recordId":"v1","reference":"place_visit:v1","title":"Riverside Park","happenedAt":null,"dashboardPath":"/location?day=2026-10-22","isUnavailable":false}],
            "writtenAt":"2026-10-23T03:12:00Z","isStale":false,"narrativePending":true,"isBeyondDepth":false,"failure":"",
            "corrections":[{"correctionText":"It was Sam at lunch.","createdAt":"2026-10-23T08:00:00Z"}],"sourceCount":71},
          "choices":[{"linkId":"l1","linkSource":"agent","linkReason":"You were asked to decide","isHidden":false,
                      "record":{"recordKind":"mail","recordId":"m1","reference":"mail:m1","title":"Lena: mockups","happenedAt":null,"dashboardPath":"/mailbox/x/y/m1","isUnavailable":false}}],
          "pins":null,"coverPhotoId":"photo-a","codingSessions":[],"documents":[],"documentTotal":0,"conversations":[],"chats":[],
          "mail":[],"mailTotal":0,"recordings":[],"photos":[{"photoId":"photo-a","takenAt":"2026-10-22T17:28:00Z","placeName":"","isVideo":false}],
          "photoTotal":23,"visits":[],"events":[],"reminders":[],"transactions":[],"health":null,"shoppingTrips":[],"notes":[],"memoryPages":[],
          "unavailableSections":["location"],"moments":[]}}}
    """

    @Test
    fun aDayIsDecodedWithItsWordsAndNarrative() = runBlocking {
        val (connection, fake) = fakeConnection { dayAnswer }
        val day = connection.journalDay("2026-10-22")
        assertEquals("2026-10-19", day.weekStart)
        val words = day.words!!
        assertEquals("2026-10-22T22:48:03.123456789Z", words.modifiedAt)
        assertEquals("Shipped it, at last.", words.shownText)
        assertTrue(words.isTouchedUpShown)
        assertEquals(emptyList<JournalNote>(), words.journalNotes)
        assertEquals(listOf("It was Sam at lunch."), day.narrative!!.corrections.map { correction -> correction.correctionText })
        assertTrue(day.isPending)
        assertEquals(emptyList<JournalLink>(), day.pins)
        assertEquals("Lena: mockups", day.choices.first().record?.title)
        assertEquals(listOf("location"), day.unavailableSections)
        assertEquals(23, day.photoTotal)
        assertEquals(JsonPrimitive("2026-10-22"), sentVariables(fake)["day"])
    }

    @Test
    fun theOriginalIsShownUntilThereIsATouchedUpText() {
        val words = TeaNodeJson.decodeFromString(
            JournalWords.serializer(),
            """{"original":"A line.","modifiedAt":null,"isTouchUpOn":true,"touchedUp":"","touchUpHow":"","isTouchedUpStale":false,"touchUpPending":true,"journalNotes":[]}""",
        )
        assertEquals("A line.", words.shownText)
        assertFalse(words.isTouchedUpShown)
        assertTrue(words.hasOriginal)
    }

    @Test
    fun aPeriodIsDecodedWithItsDaysAndMonths() = runBlocking {
        val answer = """
            {"data":{"ReadJournalPeriod":{"periodKind":"year","periodStart":"2026-01-01","periodEnd":"2026-12-31","isCurrent":true,
              "lookingBack":null,"narrative":null,"choices":[],"days":[],
              "months":[{"periodStart":"2026-10-01","headline":"The release month.","dayMoods":[0,4,5,3],"placeCount":11,"photoCount":240}],
              "totals":null,"moodCounts":[0,2,5,13,5],"dayCount":25,
              "chapters":[{"fromDay":"2026-03-01","untilDay":"2026-05-31","title":"Spring","detail":"The move."}]}}}
        """
        val (connection, fake) = fakeConnection { answer }
        val period = connection.journalPeriod(JournalPeriodKind.Year, "2026-10-22")
        assertEquals(JournalPeriodKind.Year, period.kind)
        assertEquals(listOf(0, 4, 5, 3), period.months.first().dayMoods)
        assertEquals(listOf(0, 2, 5, 13, 5), period.moodCounts)
        assertEquals("Spring", period.chapters.first().title)
        assertEquals(JsonPrimitive("year"), sentVariables(fake)["periodKind"])
        assertEquals(JsonPrimitive("2026-10-22"), sentVariables(fake)["day"])
    }

    @Test
    fun theSettingsAreDecoded() = runBlocking {
        val (connection, _) = fakeConnection {
            """{"data":{"ReadJournalSettings":{"isWeatherOn":true,"isEveningQuestionOn":false,"eveningQuestionTime":"21:00","weekStartsOn":"sunday","narrativeDepthDays":92,"modelProviderName":"Example Provider"}}}"""
        }
        val settings = connection.journalSettings()
        assertEquals("21:00", settings.eveningQuestionTime)
        assertEquals("sunday", settings.weekStartsOn)
        assertEquals("Example Provider", settings.modelProviderName)
    }

    @Test
    fun anEmptyAnswerIsNotAJournalDay() {
        val (connection, _) = fakeConnection { """{"data":{"ReadJournalDay":null}}""" }
        val failure = assertThrows(TeaNodeError::class.java) { runBlocking { connection.journalDay("2026-10-22") } }
        assertEquals(TeaNodeError.UnreadableAnswer, failure)
    }

    // MARK: Writing

    @Test
    fun savingWordsSendsTheTimeTheyWereReadAtExactlyAsItCame() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"SetJournalWords":{"day":"2026-10-22","words":{"modifiedAt":"2026-10-22T23:01:00.5Z"}}}}""" }
        val savedAt = connection.setJournalWords(JournalTarget.Day("2026-10-22"), "New words.", "2026-10-22T22:48:03.123456789Z")
        // The time the save made is what the next save is based on.
        assertEquals("2026-10-22T23:01:00.5Z", savedAt)
        val sent = sentVariables(fake)
        assertEquals(JsonPrimitive("2026-10-22"), sent["day"])
        assertEquals(JsonPrimitive("New words."), sent["words"])
        assertEquals(JsonPrimitive("2026-10-22T22:48:03.123456789Z"), sent["baseModifiedAt"])
        assertNull(sent["periodKind"])
        assertTrue(query(fake).contains("SetJournalWords("))
    }

    @Test
    fun aFirstSaveSendsNoTimeToCheck() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"SetJournalWords":{"day":"2026-10-22"}}}""" }
        connection.setJournalWords(JournalTarget.Day("2026-10-22"), "First words.", null)
        assertEquals(JsonNull, sentVariables(fake)["baseModifiedAt"])
    }

    @Test
    fun lookingBackIsSavedForThePeriodContainingTheDay() = runBlocking {
        val (connection, fake) = fakeConnection {
            """{"data":{"SetJournalLookingBack":{"periodStart":"2026-10-19","lookingBack":{"modifiedAt":"2026-10-25T20:00:00Z"}}}}"""
        }
        val savedAt = connection.setJournalWords(JournalTarget.Period(JournalPeriodKind.Week, "2026-10-22"), "Too much desk.", null)
        assertEquals("2026-10-25T20:00:00Z", savedAt)
        val sent = sentVariables(fake)
        assertEquals(JsonPrimitive("week"), sent["periodKind"])
        assertEquals(JsonPrimitive("2026-10-22"), sent["day"])
        assertEquals(JsonPrimitive("Too much desk."), sent["lookingBack"])
        assertNull(sent["words"])
        assertTrue(query(fake).contains("SetJournalLookingBack("))
    }

    @Test
    fun aMoodIsSetAndClearedByItsLevel() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"SetJournalMood":{"day":"2026-10-22"}}}""" }
        connection.setJournalMood("2026-10-22", 4)
        connection.setJournalMood("2026-10-22", null, "")
        assertEquals(JsonPrimitive(4), sentVariables(fake, 0)["moodLevel"])
        assertNull(sentVariables(fake, 0)["moodWords"])
        assertEquals(JsonNull, sentVariables(fake, 1)["moodLevel"])
        assertEquals(JsonPrimitive(""), sentVariables(fake, 1)["moodWords"])
    }

    @Test
    fun touchingUpNamesTheDayOrThePeriodAndHowOrNothing() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"TouchUpJournalWords":{"day":"2026-10-22","periodKind":""}}}""" }
        connection.touchUpJournalWords(JournalTarget.Day("2026-10-22"), "shorter")
        connection.touchUpJournalWords(JournalTarget.Period(JournalPeriodKind.Month, "2026-10-22"), null)
        assertEquals(JsonPrimitive("shorter"), sentVariables(fake, 0)["how"])
        assertNull(sentVariables(fake, 0)["periodKind"])
        assertEquals(JsonNull, sentVariables(fake, 1)["how"])
        assertEquals(JsonPrimitive("month"), sentVariables(fake, 1)["periodKind"])
    }

    @Test
    fun aCorrectionAndItsRemovalNameTheNarrative() = runBlocking {
        val (connection, fake) = fakeConnection {
            """{"data":{"CorrectJournalNarrative":{"narrativePending":true},"RemoveJournalCorrection":{"narrativePending":true},"WriteJournalNarrative":{"narrativePending":true}}}"""
        }
        connection.correctJournalNarrative(JournalTarget.Day("2026-10-22"), "It was Sam at lunch.")
        connection.removeJournalCorrection(JournalTarget.Day("2026-10-22"), 1)
        connection.writeJournalNarrative(JournalTarget.Period(JournalPeriodKind.Week, "2026-10-22"))
        assertEquals(JsonPrimitive("It was Sam at lunch."), sentVariables(fake, 0)["correctionText"])
        assertEquals(JsonPrimitive(1), sentVariables(fake, 1)["correctionIndex"])
        assertEquals(JsonPrimitive("week"), sentVariables(fake, 2)["periodKind"])
    }

    @Test
    fun settingsAreSentOnlyWhenChanged() = runBlocking {
        val (connection, fake) = fakeConnection {
            """{"data":{"UpdateJournalSettings":{"isWeatherOn":false,"isEveningQuestionOn":true,"eveningQuestionTime":"20:30","weekStartsOn":"monday","narrativeDepthDays":92,"modelProviderName":""}}}"""
        }
        val saved = connection.updateJournalSettings(isWeatherOn = false)
        assertFalse(saved.isWeatherOn)
        val sent = sentVariables(fake)
        assertEquals(JsonPrimitive(false), sent["isWeatherOn"])
        assertNull(sent["isEveningQuestionOn"])
        assertNull(sent["eveningQuestionTime"])
    }

    @Test
    fun erasingTheJournalSaysWhatWentAway() = runBlocking {
        val (connection, _) = fakeConnection { """{"data":{"EraseJournal":{"dayCount":120,"periodCount":30,"linkCount":400}}}""" }
        val erased = connection.eraseJournal()
        assertEquals(120, erased.dayCount)
        assertEquals(400, erased.linkCount)
    }

    // MARK: Shopping trips, pins and choices

    private val tripsJson = """
        "shoppingTrips":[
          {"tripId":"trip-1","storeName":"Corner Market","listNames":["Groceries","Household"],"startedAt":"2026-10-22T19:10:00Z","finishedAt":"2026-10-22T19:50:00Z","dashboardPath":"/shopping"},
          {"tripId":"trip-2","storeName":"","listNames":null,"startedAt":"2026-10-22T12:00:00Z","finishedAt":null,"dashboardPath":"/shopping"}]
    """

    private val momentsJson = """
        "moments":[
          {"at":"2026-10-22T09:00:00Z","recordKind":"mail","recordId":"m1","title":"Lena: mockups","detail":"","dashboardPath":""},
          {"at":"2026-10-22T12:00:00Z","recordKind":"shopping_trip","recordId":"trip-2","title":"Shopping","detail":"","dashboardPath":"/shopping"},
          {"at":"2026-10-22T14:00:00Z","recordKind":"contact","recordId":"c1","title":"Sam","detail":"","dashboardPath":""},
          {"at":"2026-10-22T19:10:00Z","recordKind":"shopping_trip","recordId":"trip-1","title":"Shopping at Corner Market","detail":"for Groceries and Household","dashboardPath":"/shopping"},
          {"at":"2026-10-22T20:00:00Z","recordKind":"","recordId":"","title":"Something without a record","detail":"","dashboardPath":""}]
    """

    private val pinJson = """
        "pins":[{"linkId":"pin-1","linkSource":"person","linkReason":"","isHidden":false,
                 "record":{"recordKind":"contact","recordId":"c1","reference":"contact:c1","title":"Sam","happenedAt":null,"dashboardPath":"","isUnavailable":false}}]
    """

    @Test
    fun shoppingTripsAreDecodedWithTheirStoreAndOpenShopping() = runBlocking {
        val (connection, fake) = fakeConnection { dayAnswer.replace("\"shoppingTrips\":[]", tripsJson) }
        val day = connection.journalDay("2026-10-22")
        assertEquals(listOf("trip-1", "trip-2"), day.shoppingTrips.map { trip -> trip.tripId })
        assertEquals("Corner Market", day.shoppingTrips[0].storeName)
        assertEquals(listOf("Groceries", "Household"), day.shoppingTrips[0].listNames)
        assertEquals("2026-10-22T19:50:00Z", day.shoppingTrips[0].finishedAt)
        // A trip with no store and a null list of lists.
        assertEquals("", day.shoppingTrips[1].storeName)
        assertEquals(emptyList<String>(), day.shoppingTrips[1].listNames)
        assertNull(day.shoppingTrips[1].finishedAt)
        assertEquals(JournalRecordOpening.Part("shopping"), day.shoppingTrips[0].opening)
        // The day asks for what the screen reads, and no longer for the counts.
        val sentQuery = query(fake)
        assertTrue(sentQuery.contains("shoppingTrips { tripId storeName listNames startedAt finishedAt dashboardPath }"))
        assertFalse(sentQuery.contains("boughtCount"))
        assertFalse(sentQuery.contains("itemCount"))
    }

    @Test
    fun aMomentCanBePinnedUnlessItsRecordIsPinnedOrChosenAlready() = runBlocking {
        val (connection, _) = fakeConnection { dayAnswer.replace("\"moments\":[]", momentsJson).replace("\"pins\":null", pinJson) }
        val day = connection.journalDay("2026-10-22")
        assertEquals(setOf("mail:m1", "contact:c1"), day.placedReferences)
        val placed = day.placedReferences
        // The mail is a choice of the agent, the contact is a pin; the trips are neither, and a moment with no record has nothing to pin.
        assertEquals(listOf(false, true, false, true, false), day.moments.map { moment -> moment.canPin(placed) })
        assertEquals("shopping_trip:trip-2", day.moments[1].reference)
    }

    @Test
    fun aChoiceThePersonHidCanBePinnedAgain() = runBlocking {
        val (connection, _) = fakeConnection { dayAnswer.replace("\"moments\":[]", momentsJson).replace("\"isHidden\":false", "\"isHidden\":true") }
        val day = connection.journalDay("2026-10-22")
        assertEquals(emptySet<String>(), day.placedReferences)
        assertTrue(day.moments[0].canPin(day.placedReferences))
    }

    @Test
    fun pinningARecordNamesTheDayAndTheRecord() = runBlocking {
        val answer = """
            {"data":{"PinJournalRecord":{"linkId":"pin-9","linkSource":"person","linkReason":"","isHidden":false,
              "record":{"recordKind":"shopping_trip","recordId":"trip-1","reference":"shopping_trip:trip-1","title":"Shopping at Corner Market","happenedAt":null,"dashboardPath":"/shopping","isUnavailable":false}}}}
        """
        val (connection, fake) = fakeConnection { answer }
        val pinned = connection.pinJournalRecord("2026-10-22", "shopping_trip", "trip-1")
        assertEquals("pin-9", pinned.linkId)
        assertEquals("person", pinned.linkSource)
        assertEquals("shopping_trip:trip-1", pinned.record?.reference)
        val sent = sentVariables(fake)
        assertEquals(JsonPrimitive("2026-10-22"), sent["day"])
        assertEquals(JsonPrimitive("shopping_trip"), sent["recordKind"])
        assertEquals(JsonPrimitive("trip-1"), sent["recordId"])
        assertNull(sent["linkReason"])
        assertTrue(query(fake).contains("PinJournalRecord("))
    }

    @Test
    fun removingALinkNamesItAndRefusesAnAnswerThatDidNotRemoveIt() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"RemoveJournalLink":true}}""" }
        connection.removeJournalLink("pin-9")
        assertEquals(JsonPrimitive("pin-9"), sentVariables(fake)["linkId"])
        assertTrue(query(fake).contains("RemoveJournalLink("))
        val (refusing, _) = fakeConnection { """{"data":{"RemoveJournalLink":false}}""" }
        val failure = assertThrows(TeaNodeError::class.java) { runBlocking { refusing.removeJournalLink("pin-9") } }
        assertEquals(TeaNodeError.UnreadableAnswer, failure)
    }

    @Test
    fun removingAStoryNamesTheDayOrThePeriod() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"DeleteJournalNarrative":{"narrativePending":false}}}""" }
        connection.deleteJournalNarrative(JournalTarget.Day("2026-10-22"))
        connection.deleteJournalNarrative(JournalTarget.Period(JournalPeriodKind.Month, "2026-10-22"))
        assertEquals(JsonPrimitive("2026-10-22"), sentVariables(fake, 0)["day"])
        assertNull(sentVariables(fake, 0)["periodKind"])
        assertEquals(JsonPrimitive("month"), sentVariables(fake, 1)["periodKind"])
        assertTrue(query(fake).contains("DeleteJournalNarrative("))
    }

    // MARK: Searching

    @Test
    fun aSearchPageIsDecodedWithItsDaysAndPeriods() = runBlocking {
        val answer = """
            {"data":{"SearchJournal":{"totalCount":45,"hits":[
              {"day":"2026-10-22","periodKind":"","headline":"Release day.","snippet":"…walked through the park before the rain."},
              {"day":"2026-10-19","periodKind":"week","headline":"","snippet":"The walk through the park was the best part."},
              null]}}}
        """
        val (connection, fake) = fakeConnection { answer }
        val page = connection.searchJournal("park", 20, 40)
        assertEquals(45, page.totalCount)
        assertEquals(listOf("|2026-10-22", "week|2026-10-19"), page.hits.map { hit -> hit.id })
        assertNull(page.hits[0].kind)
        assertEquals(JournalPeriodKind.Week, page.hits[1].kind)
        assertEquals("…walked through the park before the rain.", page.hits[0].snippet)
        val sent = sentVariables(fake)
        assertEquals(JsonPrimitive("park"), sent["query"])
        assertEquals(JsonPrimitive(20), sent["first"])
        assertEquals(JsonPrimitive(40), sent["offset"])
        assertTrue(query(fake).contains("SearchJournal("))
    }

    @Test
    fun aSearchWithNoHitsIsAnEmptyPageAndASearchAnswerOfNothingIsNot() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"SearchJournal":{"totalCount":0,"hits":null}}}""" }
        val page = connection.searchJournal("nothing")
        assertEquals(emptyList<JournalSearchHit>(), page.hits)
        assertEquals(JsonPrimitive(JournalSearch.PAGE_SIZE), sentVariables(fake)["first"])
        assertEquals(JsonPrimitive(0), sentVariables(fake)["offset"])
        val (empty, _) = fakeConnection { """{"data":{"SearchJournal":null}}""" }
        val failure = assertThrows(TeaNodeError::class.java) { runBlocking { empty.searchJournal("nothing") } }
        assertEquals(TeaNodeError.UnreadableAnswer, failure)
    }

    @Test
    fun theWordsToSearchForAreTrimmedAndCutToWhatTheServerTakes() {
        assertNull(JournalSearch.normalized(""))
        assertNull(JournalSearch.normalized("  \n "))
        assertEquals("the park", JournalSearch.normalized("  the park \n"))
        assertEquals(JournalSearch.QUERY_CHARACTER_LIMIT, JournalSearch.normalized("a".repeat(250))?.codePointCount(0, JournalSearch.normalized("a".repeat(250))!!.length))
        // Counted as the server counts, in characters of the text and not in bytes: 200 of a wide character is kept whole.
        assertEquals("公".repeat(200), JournalSearch.normalized("公".repeat(300)))
        // A cut that ends on a space does not keep it.
        assertEquals("a".repeat(199), JournalSearch.normalized("a".repeat(199) + " " + "b"))
    }

    @Test
    fun theNextPageOfHitsFollowsTheLastWithoutRepeatingAHit() {
        val first = JournalSearchHit("2026-10-22", "", "", "")
        val second = JournalSearchHit("2026-10-19", "week", "", "")
        val third = JournalSearchHit("2026-10-18", "", "", "")
        assertEquals(listOf(first, second, third), JournalSearch.appending(listOf(second, third), listOf(first, second)))
        assertEquals(listOf(first), JournalSearch.appending(emptyList(), listOf(first)))
    }

    // MARK: Work by project

    private val factsWithProjects = """
        {"photoCount":3,"placeCount":2,"walkedMeters":0,"stepCount":0,"sleepMinutes":0,"workoutMinutes":0,"codingSessionCount":236,"workProjectCount":3,
         "codingMinutes":370,"meetingCount":0,"meetingMinutes":0,"recordingCount":0,"conversationCount":2,"chatMessageCount":0,"mailReceivedCount":0,
         "mailSentCount":0,"documentCount":0,"transactionCount":0,"spentMinorUnits":0,"spentCurrency":"USD","shoppingTripCount":0,"memoryPageCount":0,
         "mainPlaceName":""}
    """

    private val dayWithProjects = """
        {"data":{"ReadJournalDay":{
          "day":"2026-10-22","timeZone":"UTC","isToday":false,"weekday":"thursday","weekStart":"2026-10-19",
          "words":null,"mood":null,"weather":null,"facts":$factsWithProjects,"narrative":null,"choices":[],"pins":[],"coverPhotoId":"",
          "workProjects":[
            {"name":"sample-app","firstAt":"2026-10-22T09:20:00Z","lastAt":"2026-10-22T17:05:00Z","activeMinutes":130,"stretchCount":3,
             "topics":["Release branch","Fix the flaky upload test","","Rename the config keys","Update the changelog","Tidy the build scripts","Remove the old fixtures"],
             "shipped":[
               {"documentId":"request-1","title":"Fix the flaky upload test","address":"https://git.example.com/sample/sample-app/pulls/12","happenedAt":"2026-10-22T15:40:00Z","isDraft":false},
               {"documentId":"request-2","title":"Rename the config keys","address":"javascript:alert(1)","happenedAt":"2026-10-22T16:10:00Z","isDraft":true}],
             "summary":"Cut the release branch.","documentId":"doc-1"},
            {"name":"notes","firstAt":"2026-10-22T18:00:00Z","lastAt":"2026-10-22T18:30:00Z","activeMinutes":20,"stretchCount":1,
             "topics":null,"shipped":null,"summary":"","documentId":"doc-9"}],
          "codingSessions":[],"documents":[],"documentTotal":0,
          "conversations":[
            {"conversationId":"conversation-main","title":"Main chat","isMain":true,"firstAt":"2026-10-22T18:00:00Z","lastAt":"2026-10-22T18:20:00Z","messageCount":6},
            {"conversationId":"conversation-2","title":"Draft the announcement","isMain":false,"firstAt":"2026-10-22T11:05:00Z","lastAt":"2026-10-22T11:30:00Z","messageCount":4}],
          "chats":[],"mail":[],"mailTotal":0,"recordings":[],"photos":[],"photoTotal":0,"visits":[],"events":[],"reminders":[],"transactions":[],
          "health":null,"shoppingTrips":[],"notes":[],"memoryPages":[],"unavailableSections":[],"moments":[]}}}
    """

    @Test
    fun theDaysWorkIsDecodedByProject() = runBlocking {
        val (connection, fake) = fakeConnection { dayWithProjects }
        val day = connection.journalDay("2026-10-22")
        assertEquals(3, day.facts?.projectCount)
        assertEquals(listOf("sample-app", "notes"), day.workProjects.map { project -> project.name })
        val project = day.workProjects.first()
        assertEquals(130, project.activeMinutes)
        assertEquals("Cut the release branch.", project.summary)
        assertEquals(listOf("Fix the flaky upload test", "Rename the config keys"), project.shipped.map { shipped -> shipped.title })
        assertTrue(project.shipped[1].isDraft)
        // A null list of topics or requests is an empty one.
        assertEquals(emptyList<String>(), day.workProjects[1].topics)
        assertEquals(emptyList<JournalShipped>(), day.workProjects[1].shipped)
        // The query asks for what the screen reads.
        val sentQuery = query(fake)
        assertTrue(sentQuery.contains("workProjects"))
        assertTrue(sentQuery.contains("workProjectCount"))
        assertTrue(sentQuery.contains("isMain"))
    }

    @Test
    fun onlyAWebPageIsAddressOfARequest() = runBlocking {
        val (connection, _) = fakeConnection { dayWithProjects }
        val shipped = connection.journalDay("2026-10-22").workProjects.first().shipped
        assertEquals("https://git.example.com/sample/sample-app/pulls/12", shipped[0].webAddress)
        // An address that runs a script is no link.
        assertNull(shipped[1].webAddress)
    }

    @Test
    fun theMainConversationIsKnown() = runBlocking {
        val (connection, _) = fakeConnection { dayWithProjects }
        assertEquals(listOf(true, false), connection.journalDay("2026-10-22").conversations.map { conversation -> conversation.isMainChat })
    }

    @Test
    fun anOlderServerSendsNoProjectsAndNoMainConversation() = runBlocking {
        // The answer of a server from before the fields: no workProjects, no workProjectCount, no isMain.
        val (connection, _) = fakeConnection { dayAnswer }
        val day = connection.journalDay("2026-10-22")
        assertEquals(emptyList<JournalWorkProject>(), day.workProjects)
        assertNull(day.facts)
        val decoded = TeaNodeJson.decodeFromString(
            JournalConversation.serializer(),
            """{"conversationId":"c1","title":"Old","firstAt":"2026-10-22T09:00:00Z","lastAt":"2026-10-22T09:10:00Z","messageCount":2}""",
        )
        assertNull(decoded.isMain)
        assertFalse(decoded.isMainChat)
        val oldFacts = TeaNodeJson.decodeFromString(JournalFacts.serializer(), factsWithProjects.replace("\"workProjectCount\":3,", ""))
        assertNull(oldFacts.workProjectCount)
        assertEquals(0, oldFacts.projectCount)
    }

    @Test
    fun aProjectListsFourTopicsAndCountsTheRest() = runBlocking {
        val (connection, _) = fakeConnection { dayWithProjects }
        val day = connection.journalDay("2026-10-22")
        val topics = day.workProjects.first().shownTopics()
        // The blank topic is not one, so six are named and four are listed.
        assertEquals(listOf("Release branch", "Fix the flaky upload test", "Rename the config keys", "Update the changelog"), topics.shown)
        assertEquals(2, topics.hiddenCount)
        val few = day.workProjects.last().shownTopics()
        assertEquals(emptyList<String>(), few.shown)
        assertEquals(0, few.hiddenCount)
    }

    @Test
    fun aLongListShowsItsFirstRowsUntilAskedForAll() {
        val rows = (1..9).toList()
        assertEquals(listOf(1, 2, 3, 4, 5, 6), JournalRows.shown(rows, isShowingAll = false))
        assertEquals(rows, JournalRows.shown(rows, isShowingAll = true))
        assertTrue(JournalRows.hasMore(rows))
        assertFalse(JournalRows.hasMore((1..6).toList()))
        assertEquals(rows, JournalRows.shown(rows, JournalRows.MOMENT_LIMIT, isShowingAll = false))
        assertEquals(12, JournalRows.MOMENT_LIMIT)
    }

    @Test
    fun theDocumentsAskForTheProjects() {
        assertTrue(JournalDocuments.DAY.contains("workProjectCount"))
        assertTrue(JournalDocuments.DAY.contains("workProjects"))
        assertTrue(JournalDocuments.DAY.contains("shipped { documentId title address happenedAt isDraft }"))
        assertTrue(JournalDocuments.DAY.contains("conversations { conversationId title isMain"))
        assertTrue(JournalDocuments.DAYS.contains("workProjectCount"))
    }

    @Test
    fun everyDocumentNamesItsOperation() {
        val names = listOf(
            JournalDocuments.DAYS to "ListJournalDays(", JournalDocuments.DAY to "ReadJournalDay(", JournalDocuments.PERIOD to "ReadJournalPeriod(",
            JournalDocuments.SETTINGS to "ReadJournalSettings", JournalDocuments.SET_WORDS to "SetJournalWords(", JournalDocuments.SET_MOOD to "SetJournalMood(",
            JournalDocuments.SET_LOOKING_BACK to "SetJournalLookingBack(", JournalDocuments.TOUCH_UP_WORDS to "TouchUpJournalWords(",
            JournalDocuments.STOP_TOUCH_UP to "StopJournalTouchUp(", JournalDocuments.CORRECT_NARRATIVE to "CorrectJournalNarrative(",
            JournalDocuments.REMOVE_CORRECTION to "RemoveJournalCorrection(", JournalDocuments.WRITE_NARRATIVE to "WriteJournalNarrative(",
            JournalDocuments.UPDATE_SETTINGS to "UpdateJournalSettings(", JournalDocuments.ERASE to "EraseJournal",
            JournalDocuments.DELETE_NARRATIVE to "DeleteJournalNarrative(", JournalDocuments.PIN_RECORD to "PinJournalRecord(",
            JournalDocuments.REMOVE_LINK to "RemoveJournalLink(", JournalDocuments.SEARCH to "SearchJournal(",
        )
        for ((document, name) in names) assertTrue(document.contains(name), name)
    }
}
