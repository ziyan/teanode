package com.teanode.kit.circles

import com.teanode.kit.Connection
import com.teanode.kit.access.AccessUserDraft
import com.teanode.kit.account.setAccountPronoun
import com.teanode.kit.settings.trimmedText
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

/** What a circle is for: how screens describe a circle ("Family", "Colleagues"), where the word circle itself would read oddly. */
enum class CircleKind(val serverName: String) {
    Family("family"), Friends("friends"), Colleagues("colleagues");

    companion object {
        fun fromServerName(serverName: String): CircleKind? = entries.firstOrNull { kind -> kind.serverName == serverName }
    }
}

/** What somebody may do in a circle. An admin manages who is in it; a role never decides what anybody sees of anybody else. */
enum class CircleMemberRole(val serverName: String) { Admin("admin"), Member("member") }

/** Somebody in a circle, as the circle lists them. */
@Serializable
data class CircleMember(
    val userId: String,
    /** What they sign in with. */
    val username: String,
    /** What to call them, when they have said. */
    val name: String? = null,
    /** Where their picture is, a path to read with the app's token; null when they have none. */
    val avatarUrl: String? = null,
    /** Which of the twelve colors their initial is drawn on without a picture. */
    val avatarColorIndex: Int? = null,
    /** admin or member. */
    val memberRole: String,
    val joinedAt: String,
    /** What this person is to the caller, such as parent or spouse; empty when neither has said, and for the caller's own row. */
    val relationshipKind: String = "",
    /** masculine, feminine or neutral: the gender of the word to show for the relationship. Empty when there is none. */
    val relationshipTermGender: String = "",
    /** The relationship in English, such as father; shown only for a kind the app does not know. */
    val relationshipWord: String = "",
    /** The caller's own contact that is this person, or empty. Private to the caller. */
    val linkedContactId: String = "",
) {
    val isAdmin: Boolean get() = memberRole == CircleMemberRole.Admin.serverName

    /** What a list of people calls them: their name, or the username when they have none. */
    val displayName: String get() = name?.takeIf { text -> text.isNotEmpty() } ?: username

    /** Whether the caller has a contact that is this person. */
    val isLinkedToContact: Boolean get() = linkedContactId.isNotEmpty()

    /** Whether they are somebody to the caller by relationship. */
    val hasRelationship: Boolean get() = relationshipKind.isNotEmpty()

    /** The kind, or null when there is none or the app does not know it. */
    val knownRelationshipKind: RelationshipKind? get() = RelationshipKind.fromServerName(relationshipKind)

    /** The gender of the word for the relationship; neutral when the server said none. */
    val knownRelationshipTermGender: RelationshipTermGender get() = RelationshipTermGender.fromServerName(relationshipTermGender)
}

/**
 * What one person is to another, in words that do not depend on who they are: a parent, a spouse. The server keeps it
 * in these names; the app words each in its own language and in the gender of the person it is said of.
 */
enum class RelationshipKind(val serverName: String) {
    Spouse("spouse"), Partner("partner"), Parent("parent"), Child("child"), Sibling("sibling"), Grandparent("grandparent"),
    Grandchild("grandchild"), AuntOrUncle("aunt_or_uncle"), NieceOrNephew("niece_or_nephew"), Cousin("cousin"),
    ParentInLaw("parent_in_law"), ChildInLaw("child_in_law"), SiblingInLaw("sibling_in_law"), StepParent("step_parent"),
    StepChild("step_child"), Friend("friend"), Colleague("colleague");

    /** What the other person is to the first: a parent's child. */
    val opposite: RelationshipKind
        get() = when (this) {
            Spouse -> Spouse
            Partner -> Partner
            Parent -> Child
            Child -> Parent
            Sibling -> Sibling
            Grandparent -> Grandchild
            Grandchild -> Grandparent
            AuntOrUncle -> NieceOrNephew
            NieceOrNephew -> AuntOrUncle
            Cousin -> Cousin
            ParentInLaw -> ChildInLaw
            ChildInLaw -> ParentInLaw
            SiblingInLaw -> SiblingInLaw
            StepParent -> StepChild
            StepChild -> StepParent
            Friend -> Friend
            Colleague -> Colleague
        }

    /** Whether the kind has a word for each gender. A cousin, a friend and a colleague are the same word whoever they are. */
    val hasGenderedWords: Boolean get() = this != Cousin && this != Friend && this != Colleague

    companion object {
        fun fromServerName(serverName: String): RelationshipKind? = entries.firstOrNull { kind -> kind.serverName == serverName }
    }
}

/** The gender of a word for a relationship: "wife" is feminine, "husband" masculine, "spouse" neutral. It belongs to the word chosen. */
enum class RelationshipTermGender(val serverName: String) {
    Masculine("masculine"), Feminine("feminine"), Neutral("neutral");

    companion object {
        fun fromServerName(serverName: String): RelationshipTermGender = entries.firstOrNull { gender -> gender.serverName == serverName } ?: Neutral
    }
}

/**
 * How a person says they are described. It genders the words for their side of a relationship ("son" rather than
 * "child") and how their agent speaks of them. Not set reads as they, and is sent as empty.
 */
enum class PersonPronoun(val serverName: String) {
    NotSet(""), He("he"), She("she"), They("they");

    /** The gender of the words for somebody described this way: neutral until they say, and for they. */
    val termGender: RelationshipTermGender
        get() = when (this) {
            He -> RelationshipTermGender.Masculine
            She -> RelationshipTermGender.Feminine
            They, NotSet -> RelationshipTermGender.Neutral
        }

    companion object {
        /** The three a person can choose, without not set. */
        val choices: List<PersonPronoun> = listOf(He, She, They)

        fun fromServerName(serverName: String?): PersonPronoun = entries.firstOrNull { pronoun -> pronoun.serverName == (serverName ?: "") } ?: NotSet
    }
}

/** One word a person can pick for somebody: its kind and the gender of the word. "wife" is a spouse, said in a feminine word. */
@Serializable
data class RelationshipTerm(
    /** What is sent to set it: wife, father, aunt_or_uncle. */
    val term: String,
    val relationshipKind: String,
    val termGender: String,
    /** The word in English, for a kind the app does not know. */
    val word: String,
) {
    val knownRelationshipKind: RelationshipKind? get() = RelationshipKind.fromServerName(relationshipKind)
    val knownTermGender: RelationshipTermGender get() = RelationshipTermGender.fromServerName(termGender)
}

/**
 * What the Relationship sheet holds for one other member: the term chosen (empty for none), and, when the person has
 * not said how they are described and the other side has gendered words, the pronoun they pick for themselves.
 */
data class PersonRelationshipDraft(
    val memberUserId: String,
    /** Every term the server offers, in the order it offers them. */
    val terms: List<RelationshipTerm>,
    /** The term the member has now, or empty for none. */
    val originalTerm: String,
    /** How the person described themselves before the sheet opened. */
    val originalPronoun: PersonPronoun,
    /** The term chosen; empty removes the relationship. */
    val selectedTerm: String = originalTerm,
    val pronoun: PersonPronoun = originalPronoun,
) {
    /** The kind of the term chosen, or null for none. */
    val selectedKind: RelationshipKind? get() = terms.firstOrNull { term -> term.term == selectedTerm }?.knownRelationshipKind

    /** What the member will see the person as: the opposite of the kind chosen. */
    val oppositeKind: RelationshipKind? get() = selectedKind?.opposite

    /** The gender of the word the member will see for the person, which is how the person is described. */
    val oppositeTermGender: RelationshipTermGender get() = pronoun.termGender

    /**
     * Whether another term was picked. The term shown at first follows how the member is described, which may not be
     * the word the person chose ("mother" where they said "parent"), so it is saved again only when the person
     * picked another one.
     */
    val isTermChanged: Boolean get() = selectedTerm != originalTerm

    val isRemoving: Boolean get() = isTermChanged && selectedTerm.isEmpty()

    /** Whether the sheet asks how the person should be described: they have not said, and the word the member will see has genders. */
    val isPronounAsked: Boolean get() = originalPronoun == PersonPronoun.NotSet && (oppositeKind?.hasGenderedWords ?: false)

    /** The pronoun to set before the relationship, or null when none is. */
    val pronounToSet: PersonPronoun? get() = if (isPronounAsked && pronoun != PersonPronoun.NotSet) pronoun else null

    val hasChanges: Boolean get() = isTermChanged || pronounToSet != null

    val canSave: Boolean get() = hasChanges

    companion object {
        fun of(member: CircleMember, terms: List<RelationshipTerm>, ownPronoun: PersonPronoun): PersonRelationshipDraft =
            PersonRelationshipDraft(member.userId, terms, termFor(member, terms), ownPronoun)

        /**
         * The term that says what the member is now. The member's gender follows how they are described, which may
         * differ from the word the person chose, so a kind with no word of that gender falls back to the neutral one.
         */
        fun termFor(member: CircleMember, terms: List<RelationshipTerm>): String {
            if (!member.hasRelationship) return ""
            val termsOfKind = terms.filter { term -> term.relationshipKind == member.relationshipKind }
            termsOfKind.firstOrNull { term -> term.termGender == member.relationshipTermGender }?.let { exact -> return exact.term }
            termsOfKind.firstOrNull { term -> term.termGender == RelationshipTermGender.Neutral.serverName }?.let { neutral -> return neutral.term }
            return termsOfKind.firstOrNull()?.term ?: ""
        }
    }
}

/** An invitation to join a circle: to the person it was sent to, and to the circle's admins as one still waiting. */
@Serializable
data class CircleInvitation(
    val id: String,
    val circleId: String,
    val circleName: String,
    /** family, friends or colleagues. */
    val circleKind: String,
    val invitedByName: String,
    val invitedUsername: String,
    /** The role they join with: admin or member. */
    val memberRole: String,
    val createdAt: String,
    val expiresAt: String,
) {
    val isAdminInvitation: Boolean get() = memberRole == CircleMemberRole.Admin.serverName
    val knownCircleKind: CircleKind? get() = CircleKind.fromServerName(circleKind)
}

/**
 * The question asked once, as a person joins a family or a group of colleagues: share their calendar with everyone in
 * it, or not now; for a family also whether to share where they are. Friends are not asked.
 */
data class CircleCalendarOffer(
    val circleId: String,
    val circleName: String,
    /** The person's own calendar of events, which is what would be shared; null when they have none. */
    val calendarId: String?,
    /** Whether where the person is can be shared too: a family, to a person who may use Location. */
    val isLocationOffered: Boolean,
) {
    /** Whether the calendar is part of the question. */
    val isCalendarOffered: Boolean get() = calendarId != null

    companion object {
        /** The offer for an invitation just accepted, or null for a circle of friends and when there is nothing to offer. */
        fun accepting(invitation: CircleInvitation, calendarId: String?, canShareLocation: Boolean): CircleCalendarOffer? {
            if (invitation.circleKind == CircleKind.Friends.serverName) return null
            val isLocationOffered = invitation.circleKind == CircleKind.Family.serverName && canShareLocation
            if (calendarId == null && !isLocationOffered) return null
            return CircleCalendarOffer(invitation.circleId, invitation.circleName, calendarId, isLocationOffered)
        }
    }
}

/** A circle: a set of people on one server who know each other, named and described by its kind. */
@Serializable
data class PeopleCircle(
    val id: String,
    val name: String,
    /** family, friends or colleagues. */
    val circleKind: String,
    val createdAt: String,
    /** The caller's own role in it: admin or member. */
    val memberRole: String,
    val members: List<CircleMember>,
    /** Who is still being waited for. Empty for somebody who is not an admin. */
    val pendingInvitations: List<CircleInvitation> = emptyList(),
) {
    val isAdmin: Boolean get() = memberRole == CircleMemberRole.Admin.serverName
    val knownCircleKind: CircleKind? get() = CircleKind.fromServerName(circleKind)

    fun member(userId: String): CircleMember? = members.firstOrNull { member -> member.userId == userId }
}

/** A circle's name and kind as the form holds them. A rename sends the name alone. */
data class CircleDraft(val name: String = "", val circleKind: CircleKind = CircleKind.Family) {
    val trimmedName: String get() = trimmedText(name)

    /** Whether the form can be sent: a circle needs a name. */
    val canSubmit: Boolean get() = trimmedName.isNotEmpty()

    companion object {
        fun of(circle: PeopleCircle): CircleDraft = CircleDraft(circle.name, circle.knownCircleKind ?: CircleKind.Family)
    }
}

/** Who to invite and with which role, as the invite form holds them. */
data class CircleInvitationDraft(val username: String = "", val isAdmin: Boolean = false) {
    val trimmedUsername: String get() = trimmedText(username)

    val canSubmit: Boolean get() = trimmedUsername.isNotEmpty()

    val memberRole: CircleMemberRole get() = if (isAdmin) CircleMemberRole.Admin else CircleMemberRole.Member
}

/**
 * An account an operator makes for somebody while putting them in a circle: the account is made with CreateUser and put
 * in the circle with AddCircleMember, so there is no invitation for the person to accept.
 */
data class CircleAccountDraft(val username: String = "", val name: String = "", val password: String = "", val isAdmin: Boolean = false) {
    val trimmedUsername: String get() = trimmedText(username)

    /** Whether the form can be sent: a username and a password to sign in with, since nobody has set this account up. */
    val canSubmit: Boolean get() = trimmedUsername.isNotEmpty() && password.isNotEmpty()

    val memberRole: CircleMemberRole get() = if (isAdmin) CircleMemberRole.Admin else CircleMemberRole.Member

    /** The account as the people page makes one, so that both send the same CreateUser variables. */
    val accountDraft: AccessUserDraft get() = AccessUserDraft(username = trimmedUsername, password = password, name = trimmedText(name))
}

@Serializable private class ListCirclesAnswer(val ListCircles: List<PeopleCircle>)

@Serializable private class ReadCircleAnswer(val ReadCircle: PeopleCircle)

@Serializable private class ListCircleInvitationsAnswer(val ListCircleInvitations: List<CircleInvitation>)

@Serializable private class CreatedCircle(val id: String)

@Serializable private class CreateCircleAnswer(val CreateCircle: CreatedCircle)

@Serializable private class ListRelationshipTermsAnswer(val ListRelationshipTerms: List<RelationshipTerm>)

@Serializable private class SetPersonRelationshipAnswer(val SetPersonRelationship: CircleMember)

@Serializable private class RemovePersonRelationshipAnswer(val RemovePersonRelationship: Boolean)

/** The circles the person is in, with their members and, for an admin, the invitations still waiting. */
suspend fun Connection.circles(): List<PeopleCircle> = execute<ListCirclesAnswer>(CircleDocuments.CIRCLES).ListCircles

/** One circle. A circle the person is not in is answered as not found. */
suspend fun Connection.circle(circleId: String): PeopleCircle = execute<ReadCircleAnswer>(CircleDocuments.CIRCLE, variablesOf("circleId" to circleId)).ReadCircle

/** The invitations to join a circle that are waiting for the person's answer. */
suspend fun Connection.circleInvitations(): List<CircleInvitation> = execute<ListCircleInvitationsAnswer>(CircleDocuments.CIRCLE_INVITATIONS).ListCircleInvitations

/** Makes a circle with the person as its admin, and answers its id. */
suspend fun Connection.createCircle(draft: CircleDraft): String =
    execute<CreateCircleAnswer>(CircleDocuments.CREATE_CIRCLE, variablesOf("name" to draft.trimmedName, "circleKind" to draft.circleKind.serverName)).CreateCircle.id

/** Renames a circle. Admins only. */
suspend fun Connection.renameCircle(circleId: String, name: String) {
    execute<JsonObject>(CircleDocuments.RENAME_CIRCLE, variablesOf("circleId" to circleId, "name" to trimmedText(name)))
}

/** Deletes a circle for everyone in it. Admins only; nobody's account goes with it. */
suspend fun Connection.deleteCircle(circleId: String) {
    execute<JsonObject>(CircleDocuments.DELETE_CIRCLE, variablesOf("circleId" to circleId))
}

/**
 * Invites a username to a circle. Admins only. The server answers the same whether or not the account exists or is
 * already in the circle, so a screen must not say the person was found.
 */
suspend fun Connection.inviteToCircle(circleId: String, draft: CircleInvitationDraft) {
    execute<JsonObject>(
        CircleDocuments.INVITE_TO_CIRCLE,
        variablesOf("circleId" to circleId, "username" to draft.trimmedUsername, "memberRole" to draft.memberRole.serverName),
    )
}

/** Puts an account in a circle at once, with no invitation to accept. Needs the permission to manage users. */
suspend fun Connection.addCircleMember(circleId: String, username: String, memberRole: CircleMemberRole) {
    execute<JsonObject>(CircleDocuments.ADD_CIRCLE_MEMBER, variablesOf("circleId" to circleId, "username" to username, "memberRole" to memberRole.serverName))
}

/** Joins the circle an invitation is to, or turns it down. */
suspend fun Connection.answerCircleInvitation(invitationId: String, isAccepted: Boolean) {
    execute<JsonObject>(CircleDocuments.ANSWER_CIRCLE_INVITATION, variablesOf("invitationId" to invitationId, "isAccepted" to isAccepted))
}

/** Takes back an invitation that has not been answered. Admins only. */
suspend fun Connection.withdrawCircleInvitation(invitationId: String) {
    execute<JsonObject>(CircleDocuments.WITHDRAW_CIRCLE_INVITATION, variablesOf("invitationId" to invitationId))
}

/** Makes somebody an admin or a member. Admins only; the last admin cannot be made a member while others remain. */
suspend fun Connection.setCircleMemberRole(circleId: String, userId: String, memberRole: CircleMemberRole) {
    execute<JsonObject>(CircleDocuments.SET_CIRCLE_MEMBER_ROLE, variablesOf("circleId" to circleId, "userId" to userId, "memberRole" to memberRole.serverName))
}

/** Takes somebody out of a circle. Admins only; they can be invited again. */
suspend fun Connection.removeCircleMember(circleId: String, userId: String) {
    execute<JsonObject>(CircleDocuments.REMOVE_CIRCLE_MEMBER, variablesOf("circleId" to circleId, "userId" to userId))
}

/** Leaves a circle. The last admin cannot leave while others remain, and a circle whose last member leaves is deleted. */
suspend fun Connection.leaveCircle(circleId: String) {
    execute<JsonObject>(CircleDocuments.LEAVE_CIRCLE, variablesOf("circleId" to circleId))
}

/** Every word a relationship can be said with, in the order to offer them. */
suspend fun Connection.relationshipTerms(): List<RelationshipTerm> =
    execute<ListRelationshipTermsAnswer>(CircleDocuments.RELATIONSHIP_TERMS).ListRelationshipTerms

/** Says what somebody in a circle with the person is to them, by one of the terms the server offers. Answers the member as the person now sees them. */
suspend fun Connection.setPersonRelationship(userId: String, term: String): CircleMember =
    execute<SetPersonRelationshipAnswer>(CircleDocuments.SET_PERSON_RELATIONSHIP, variablesOf("userId" to userId, "relationshipTerm" to term)).SetPersonRelationship

/** Takes away what the person and somebody are to each other, whichever of them said it. True when there was one. */
suspend fun Connection.removePersonRelationship(userId: String): Boolean =
    execute<RemovePersonRelationshipAnswer>(CircleDocuments.REMOVE_PERSON_RELATIONSHIP, variablesOf("userId" to userId)).RemovePersonRelationship

/** Saves the Relationship sheet: how the person is described first when the sheet asked, then the relationship, or its removal. */
suspend fun Connection.saveRelationship(draft: PersonRelationshipDraft, ownUserId: String) {
    draft.pronounToSet?.let { pronoun -> setAccountPronoun(ownUserId, pronoun) }
    if (!draft.isTermChanged) return
    if (draft.isRemoving) removePersonRelationship(draft.memberUserId) else setPersonRelationship(draft.memberUserId, draft.selectedTerm)
}

