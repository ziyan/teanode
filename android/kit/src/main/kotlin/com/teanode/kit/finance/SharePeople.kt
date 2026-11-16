package com.teanode.kit.finance

import com.teanode.kit.Connection
import com.teanode.kit.graphQLField
import kotlinx.serialization.Serializable

/** The people and circles a person can share something with (finance, a shopping list): the same documents the calendar's sharing sends. */
object SharePeopleDocuments {
    const val SHARE_PEOPLE = """query { ListCirclePeople { userId username name avatarUrl avatarColorIndex } }"""
    const val SHARE_CIRCLES = """query { ListCircles { id name members { userId } } }"""
}

/** Somebody the person shares a circle with, and so may share with. */
@Serializable
data class SharePerson(
    val userId: String,
    val username: String,
    val name: String? = null,
    val avatarUrl: String? = null,
    val avatarColorIndex: Int? = null,
) {
    val id: String get() = userId
    val displayName: String get() = if (name.isNullOrEmpty()) username else name

    /** How a picker names them: the name and, in brackets, the username, so two people with one name can be told apart. */
    val pickerLabel: String
        get() {
            val trimmed = name?.trim().orEmpty()
            return if (trimmed.isEmpty() || trimmed == username) username else "$trimmed ($username)"
        }
}

/** A circle the person is in, which something can be shared with whole. */
@Serializable
data class ShareCircle(val id: String, val name: String, val members: List<Member> = emptyList()) {
    @Serializable
    data class Member(val userId: String)

    val memberCount: Int get() = members.size
}

/** The people the person shares a circle with. */
suspend fun Connection.sharePeople(): List<SharePerson> = graphQLField<List<SharePerson>>(SharePeopleDocuments.SHARE_PEOPLE, "ListCirclePeople").orEmpty()

/** The circles the person is in. */
suspend fun Connection.shareCircles(): List<ShareCircle> = graphQLField<List<ShareCircle>>(SharePeopleDocuments.SHARE_CIRCLES, "ListCircles").orEmpty()
