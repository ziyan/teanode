package com.teanode.kit.contacts

import com.teanode.kit.Connection
import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive

// A person's card: who somebody is to the person, and a few items of everything of theirs that can be seen elsewhere in
// the app, read in one round trip (ReadPersonCard). The sections that hang on the account (where they are, their
// health, the albums they shared) need an account that shares a circle with the person; those that hang on the
// person's own contact for them (details, mail, photos of them, memory, recordings) need the contact.
//
// The sections that belong to other parts of the app (location, health, finance, photos) are read here only as far as
// the card draws them: a few fields each, ignoring the rest of what the shared fields ask for.

/** PersonCardTarget names whose card to read: an account in the person's circles, or one of their own contacts, which may or may not be one. */
sealed class PersonCardTarget {
    abstract val id: String

    /** The arguments ReadPersonCard takes for it: an account by its id, a contact by its address book and its id, never both. */
    abstract val variables: Map<String, JsonElement>

    data class Account(val userId: String) : PersonCardTarget() {
        override val id: String get() = "account/$userId"
        override val variables: Map<String, JsonElement> get() = mapOf("userId" to JsonPrimitive(userId))
    }

    data class Contact(val addressBookId: String, val contactId: String) : PersonCardTarget() {
        override val id: String get() = "contact/$addressBookId/$contactId"
        override val variables: Map<String, JsonElement>
            get() = mapOf("addressBookId" to JsonPrimitive(addressBookId), "contactId" to JsonPrimitive(contactId))
    }
}

/**
 * PersonCardSectionState says whether a section of the card has something to show. A state this app does not know is
 * read as unavailable, which leaves the section out.
 */
@Serializable(with = PersonCardSectionStateSerializer::class)
enum class PersonCardSectionState(val wireName: String) {
    /** Read; its items may still be none. */
    Shown("shown"),

    /** The account has not shared this part with the person. */
    NotShared("notShared"),

    /** The section is found through the person's contact for the account, and none is linked. */
    NeedsContact("needsContact"),

    /** The person may not use this part, or there is no account to read it from. The card leaves the section out. */
    Unavailable("unavailable"),

    /** It failed or did not answer in time. */
    Failed("failed");

    /** Whether the card draws the section's own rows: not one it leaves out, and not one that waits for a contact, which the card asks for once for all of them. */
    val isListed: Boolean get() = this != Unavailable && this != NeedsContact

    companion object {
        fun named(wireName: String): PersonCardSectionState = entries.firstOrNull { state -> state.wireName == wireName } ?: Unavailable
    }
}

object PersonCardSectionStateSerializer : KSerializer<PersonCardSectionState> {
    override val descriptor = PrimitiveSerialDescriptor("PersonCardSectionState", PrimitiveKind.STRING)
    override fun deserialize(decoder: Decoder): PersonCardSectionState = PersonCardSectionState.named(decoder.decodeString())
    override fun serialize(encoder: Encoder, value: PersonCardSectionState) = encoder.encodeString(value.wireName)
}

/** PersonCardCircle is a circle the person and the other share. */
@Serializable
data class PersonCardCircle(val circleId: String, val circleName: String = "", /** family, friends or colleagues. */ val circleKind: String = "") {
    val id: String get() = circleId
}

/** PersonCardLocationSection is where they are, as the Location part shows it. */
@Serializable
data class PersonCardLocationSection(
    val sectionState: PersonCardSectionState,
    val location: com.teanode.kit.location.PersonLocation? = null,
    /** Their visits today, newest first, when they share where they have been. */
    val todayVisits: List<com.teanode.kit.location.PlaceVisit>? = null,
    val hasMoreTodayVisits: Boolean = false,
)

/** PersonCardHealthSection is the health they share with the person: each kind with its latest day, steps first. */
@Serializable
data class PersonCardHealthSection(
    val sectionState: PersonCardSectionState,
    val healthTypes: List<com.teanode.kit.health.HealthTypeView>? = null,
    val hasMore: Boolean = false,
) {
    companion object {
        /** Whether the kind's latest day is the day given, written "YYYY-MM-DD" in the phone's calendar. Only then is it said to be today. */
        fun isToday(type: com.teanode.kit.health.HealthTypeView, today: java.time.LocalDate): Boolean = type.latestDaySummary?.summaryDay == today.toString()
    }
}

/** PersonCardFinanceSection is the finance accounts they share with the person, each as the level it was shared at shows it. */
@Serializable
data class PersonCardFinanceSection(
    val sectionState: PersonCardSectionState,
    /** Spending or transactions when they share all of their finance with the person, else empty or absent (an older server). */
    val allFinanceShareLevel: String? = null,
    val financeAccounts: List<com.teanode.kit.finance.SharedFinanceAccount>? = null,
    val hasMore: Boolean = false,
)

/**
 * PersonCardPhotosSection is photos of them in the person's library, and the albums they shared with the person. The
 * two have their own states: photos of a person are found through the contact, albums through the account.
 */
@Serializable
data class PersonCardPhotosSection(
    val photosState: PersonCardSectionState,
    val photos: List<com.teanode.kit.photos.Photo>? = null,
    val hasMorePhotos: Boolean = false,
    val albumsState: PersonCardSectionState,
    val albums: List<com.teanode.kit.photos.PhotoAlbum>? = null,
    val hasMoreAlbums: Boolean = false,
)

/** PersonCardMail is one mail with the person, newest first. */
@Serializable
data class PersonCardMail(
    val mailboxItemId: String,
    val mailboxId: String = "",
    /** The folder it is in, as the mailbox pages address it. */
    val folderPath: String = "",
    val subject: String = "",
    val fromAddress: String = "",
    val fromName: String = "",
    /** Whether they sent it; otherwise the person sent it to them. */
    val isFromPerson: Boolean = false,
    val isUnread: Boolean = false,
    /** RFC 3339. */
    val receivedAt: String = "",
    /** What the person's agent wrote about it on one line; empty when it has not read it. */
    val snippet: String = "",
) {
    /** Who sent it, for a line under the subject: the name, else the address. */
    val senderText: String get() = if (fromName.isEmpty()) fromAddress else fromName
}

/** PersonCardMailSection is the person's recent mail from or to any address on their contact for the other. */
@Serializable
data class PersonCardMailSection(
    val sectionState: PersonCardSectionState,
    val mails: List<PersonCardMail>? = null,
    val hasMore: Boolean = false,
)

/** PersonCardRecording is one recording they speak in, newest first. */
@Serializable
data class PersonCardRecording(
    val recordingId: String,
    val title: String = "",
    /** RFC 3339; null when it is not known. */
    val recordedAt: String? = null,
    val durationMS: Int = 0,
    /** How many passages they speak. */
    val spokenPassageCount: Int = 0,
)

/** PersonCardRecordingSection is the recordings a passage of which the person confirmed as spoken by a speaker they tied to their contact for the other. */
@Serializable
data class PersonCardRecordingSection(
    val sectionState: PersonCardSectionState,
    val recordings: List<PersonCardRecording>? = null,
    val hasMore: Boolean = false,
)

/** PersonCardMemoryPage is one page of memory about them. */
@Serializable
data class PersonCardMemoryPage(
    val nodePath: String,
    val nodeName: String = "",
    /** What kind of page it is, such as person or topic. */
    val nodeKind: String = "",
    /** The page's first lines, without Markdown marks. */
    val summary: String = "",
    val updatedAt: String = "",
    val isMatchedByName: Boolean? = null,
)

/** PersonCardMemorySection is the pages of memory tied to the person's contact for the other, their own page first. */
@Serializable
data class PersonCardMemorySection(
    val sectionState: PersonCardSectionState,
    val pages: List<PersonCardMemoryPage>? = null,
    val hasMore: Boolean = false,
)

/** PersonCard is one person's card. */
@Serializable
data class PersonCard(
    /** The account, as a member of the person's circles; null for a contact that is not one. */
    val person: com.teanode.kit.circles.CircleMember? = null,
    /** The person's own contact for them: the one asked for, or the one linked to the account. Null when there is none. */
    val contact: ContactDetail? = null,
    /** The circles both are in; none for a contact without an account. */
    val sharedCircles: List<PersonCardCircle>? = null,
    /** Whether a chat with them can be opened. */
    val canMessage: Boolean = false,
    val location: PersonCardLocationSection? = null,
    val health: PersonCardHealthSection? = null,
    val finance: PersonCardFinanceSection? = null,
    val photos: PersonCardPhotosSection? = null,
    val mail: PersonCardMailSection? = null,
    val recordings: PersonCardRecordingSection? = null,
    val memory: PersonCardMemorySection? = null,
) {
    /** What to call them: the account's name, else the contact's, else the first address on the contact. */
    val displayName: String
        get() {
            if (person != null) return person.displayName
            if (!contact?.name.isNullOrEmpty()) return contact?.name ?: ""
            return contact?.emails?.firstOrNull() ?: ""
        }

    /** Whether the person has a contact for them. */
    val hasContact: Boolean get() = contact != null

    /** Whether any section is waiting for a contact to be linked to the account: the card then offers to link one, once. */
    val needsContact: Boolean
        get() = listOf(photos?.photosState, mail?.sectionState, recordings?.sectionState, memory?.sectionState)
            .any { state -> state == PersonCardSectionState.NeedsContact }

    /** The first address on the contact, which Mail writes to. */
    val mailAddress: String? get() = contact?.emails?.firstOrNull { address -> address.isNotEmpty() }

    /** The number the first phone on the contact calls: only digits and the plus sign are kept, as a telephone address takes them. */
    val callAddress: String?
        get() {
            val phone = contact?.phones?.firstOrNull { number -> number.isNotEmpty() } ?: return null
            val number = phone.filter { character -> character.isDigit() || character == '+' }
            return if (number.isEmpty()) null else "tel:$number"
        }

    /** Directions to their latest position, when they share it. */
    val directionsAddress: String?
        get() {
            val point = location?.location?.latestPoint ?: return null
            return "geo:${point.latitudeDegrees},${point.longitudeDegrees}?q=${point.latitudeDegrees},${point.longitudeDegrees}"
        }

    companion object {
        /** How many items each section is asked to keep. */
        const val ITEM_LIMIT = 4
    }
}

@Serializable
private class PersonCardAnswer(@SerialName("ReadPersonCard") val card: PersonCard)

/**
 * Reads a person's card: who they are to the person and a few items from each part of the app that concerns them. A
 * section that failed says so and the rest is there; somebody the person shares no circle with, and no contact of the
 * person's, is answered as not found.
 */
suspend fun Connection.readPersonCard(target: PersonCardTarget, itemLimit: Int = PersonCard.ITEM_LIMIT): PersonCard {
    val variables = LinkedHashMap(target.variables)
    variables["itemLimit"] = JsonPrimitive(itemLimit)
    return execute<PersonCardAnswer>(PersonCardDocuments.PERSON_CARD, variables).card
}
