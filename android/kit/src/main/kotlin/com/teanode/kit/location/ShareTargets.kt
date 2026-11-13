package com.teanode.kit.location

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** ShareTarget is who a thing is shared with: one person, or every member of one circle. */
sealed class ShareTarget {
    data class Person(val userId: String) : ShareTarget()
    data class Circle(val circleId: String) : ShareTarget()

    val granteeUserId: String? get() = (this as? Person)?.userId
    val granteeCircleId: String? get() = (this as? Circle)?.circleId
}

/** A person in a circle the caller shares with, whom a location or Health can be shared with. */
@Serializable
data class SharePerson(
    val userId: String,
    val username: String,
    val name: String? = null,
    val avatarUrl: String? = null,
    val avatarColorIndex: Int? = null,
) {
    val displayName: String get() = if (name.isNullOrEmpty()) username else name

    /** How a picker names them: the name and, in brackets, the username, so two people with one name can be told apart. */
    val pickerLabel: String get() = if (name.isNullOrEmpty() || name == username) username else "$name ($username)"
}

/** A circle the caller is in, which a location or Health can be shared with whole. */
@Serializable
data class ShareCircle(val id: String, val name: String, val members: List<ShareCircleMember> = emptyList()) {
    val memberCount: Int get() = members.size
}

@Serializable
data class ShareCircleMember(val userId: String)

object ShareTargetDocuments {
    const val CIRCLE_PEOPLE = """
        query { ListCirclePeople { userId username name avatarUrl avatarColorIndex } }
        """
    const val CIRCLES_FOR_SHARING = """
        query { ListCircles { id name members { userId } } }
        """

    /** What every read of a circle answers with. Written out in full, as the iPhone app does. */
    const val CIRCLES = """
        query { ListCircles { id name circleKind createdAt memberRole
        members { userId username name avatarUrl avatarColorIndex memberRole joinedAt relationshipKind relationshipTermGender relationshipWord linkedContactId }
        pendingInvitations { id circleId circleName circleKind invitedByName invitedUsername memberRole createdAt expiresAt } } }
        """
}

@Serializable private class ListCirclePeopleAnswer(@SerialName("ListCirclePeople") val people: List<SharePerson>)
@Serializable private class ListCirclesForSharingAnswer(@SerialName("ListCircles") val circles: List<ShareCircle>)
@Serializable private class ListCirclesAnswer(@SerialName("ListCircles") val circles: List<PeopleCircle>)

/** The people in the circles the caller is in, whom something can be shared with. */
suspend fun Connection.sharePeople(): List<SharePerson> =
    execute(ShareTargetDocuments.CIRCLE_PEOPLE, ListCirclePeopleAnswer.serializer()).people

/** The circles the caller is in, which something can be shared with whole. */
suspend fun Connection.shareCircles(): List<ShareCircle> =
    execute(ShareTargetDocuments.CIRCLES_FOR_SHARING, ListCirclesForSharingAnswer.serializer()).circles

/** The circles the caller is in, for the setup. */
suspend fun Connection.circles(): List<PeopleCircle> =
    execute(ShareTargetDocuments.CIRCLES, ListCirclesAnswer.serializer()).circles
