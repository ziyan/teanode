package com.teanode.kit.circles

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class CirclesTest {
    private val terms = listOf(
        RelationshipTerm("parent", "parent", "neutral", "parent"), RelationshipTerm("mother", "parent", "feminine", "mother"),
        RelationshipTerm("friend", "friend", "neutral", "friend"),
    )

    private fun member(kind: String = "", gender: String = "") =
        CircleMember("u2", "sam", memberRole = "member", joinedAt = "2030-01-01T00:00:00Z", relationshipKind = kind, relationshipTermGender = gender)

    @Test
    fun theTermFollowsTheMembersGenderAndFallsBackToNeutral() {
        assertEquals("", PersonRelationshipDraft.termFor(member(), terms))
        assertEquals("mother", PersonRelationshipDraft.termFor(member("parent", "feminine"), terms))
        assertEquals("parent", PersonRelationshipDraft.termFor(member("parent", "masculine"), terms))
    }

    @Test
    fun theSheetAsksForAPronounOnlyWhenTheOtherSideHasGenderedWords() {
        val draft = PersonRelationshipDraft.of(member(), terms, PersonPronoun.NotSet).copy(selectedTerm = "parent")
        assertTrue(draft.isPronounAsked)
        assertNull(draft.pronounToSet)
        assertEquals(PersonPronoun.She, draft.copy(pronoun = PersonPronoun.She).pronounToSet)
        assertEquals(RelationshipKind.Child, draft.oppositeKind)
        assertFalse(draft.copy(selectedTerm = "friend").isPronounAsked)
        assertFalse(draft.copy(originalPronoun = PersonPronoun.He).isPronounAsked)
    }

    @Test
    fun removingNeedsAnEmptyTermAfterAChange() {
        val draft = PersonRelationshipDraft.of(member("parent", "feminine"), terms, PersonPronoun.They)
        assertFalse(draft.hasChanges)
        assertTrue(draft.copy(selectedTerm = "").isRemoving)
    }

    @Test
    fun friendsAreNeverOfferedToShare() {
        val invitation = CircleInvitation("i", "c", "Book club", "friends", "Alex", "sam", "member", "x", "y")
        assertNull(CircleCalendarOffer.accepting(invitation, "cal", true))
        val family = invitation.copy(circleKind = "family")
        assertTrue(CircleCalendarOffer.accepting(family, null, true)!!.isLocationOffered)
        assertNull(CircleCalendarOffer.accepting(family, null, false))
    }

    @Test
    fun everyKindHasAnOpposite() {
        RelationshipKind.entries.forEach { kind -> assertEquals(kind, kind.opposite.opposite) }
    }

    @Test
    fun aCircleNeedsAName() {
        assertFalse(CircleDraft(" ").canSubmit)
        assertEquals("Home", CircleDraft(" Home ").trimmedName)
        assertTrue(CircleAccountDraft("sam", password = "pw").canSubmit)
    }
}
