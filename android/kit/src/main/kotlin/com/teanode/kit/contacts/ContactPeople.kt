package com.teanode.kit.contacts

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** The people the person shares a circle with, for choosing who a contact is. */
object ContactPeopleDocuments {
    const val CHAT_PEOPLE = "query { ListCirclePeople { userId username name avatarUrl avatarColorIndex } }"

    /** The circles with only what the address books need of them: a circle is offered a book when it has none. */
    const val CIRCLES = "query { ListCircles { id name circleKind } }"
}

/** ContactCircle is a circle of the person's, as far as the address books need it. */
@Serializable
data class ContactCircle(val id: String, val name: String = "", /** family, friends or colleagues. */ val circleKind: String = "") {
    val isFamily: Boolean get() = circleKind == "family"
}

@Serializable
private class PeopleAnswer(@SerialName("ListCirclePeople") val people: List<ContactAccount>)

@Serializable
private class CirclesAnswer(@SerialName("ListCircles") val circles: List<ContactCircle>)

/** The people in the person's circles. */
suspend fun Connection.chatPeople(): List<ContactAccount> = execute<PeopleAnswer>(ContactPeopleDocuments.CHAT_PEOPLE).people

/** The person's circles. */
suspend fun Connection.circles(): List<ContactCircle> = execute<CirclesAnswer>(ContactPeopleDocuments.CIRCLES).circles
