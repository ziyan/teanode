package com.teanode.kit.photos

import com.teanode.kit.Connection
import kotlinx.serialization.Serializable

/** A person an album can be shared with: somebody who shares a circle with the person. */
data class PhotoSharePerson(val userId: String, val username: String, val name: String, val avatarUrl: String?, val avatarColorIndex: Int) {
    val displayName: String get() = name.ifEmpty { username }
}

/** A circle an album can be shared with. */
data class PhotoShareCircle(val id: String, val name: String)

/** Who the share sheet offers: the circles and the people in them, apart from the person themself. */
data class PhotoShareTargets(val circles: List<PhotoShareCircle>, val people: List<PhotoSharePerson>) {
    /** The circles and people not shared with yet. */
    fun available(shares: List<PhotoAlbumShare>): PhotoShareTargets {
        val sharedCircleIds = shares.mapNotNull { share -> share.granteeCircleId }.toSet()
        val sharedUserIds = shares.mapNotNull { share -> share.granteeUserId }.toSet()
        return PhotoShareTargets(circles.filter { circle -> circle.id !in sharedCircleIds }, people.filter { person -> person.userId !in sharedUserIds })
    }
}

/** A narrower read of the person's circles than the Circles screen makes: only what choosing who to share with needs. */
const val PHOTO_SHARE_CIRCLES_DOCUMENT =
    "query { ListCircles { id name members { userId username name avatarUrl avatarColorIndex } } }"

@Serializable private class ShareMember(val userId: String, val username: String = "", val name: String = "", val avatarUrl: String? = null, val avatarColorIndex: Int = 0)

@Serializable private class ShareCircle(val id: String, val name: String = "", val members: List<ShareMember> = emptyList())

@Serializable private class ShareCirclesAnswer(val ListCircles: List<ShareCircle>)

suspend fun Connection.photoShareTargets(ownUserId: String?): PhotoShareTargets {
    val circles = execute<ShareCirclesAnswer>(PHOTO_SHARE_CIRCLES_DOCUMENT).ListCircles
    val people = circles.flatMap { circle -> circle.members }.filter { member -> member.userId != ownUserId }.distinctBy { member -> member.userId }
        .map { member -> PhotoSharePerson(member.userId, member.username, member.name, member.avatarUrl, member.avatarColorIndex) }
        .sortedBy { person -> person.displayName.lowercase() }
    return PhotoShareTargets(circles.map { circle -> PhotoShareCircle(circle.id, circle.name) }.sortedBy { circle -> circle.name.lowercase() }, people)
}
