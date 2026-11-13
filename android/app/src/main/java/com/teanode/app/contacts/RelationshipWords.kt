package com.teanode.app.contacts

import android.content.Context
import com.teanode.app.R
import com.teanode.kit.contacts.PersonCardMember
import com.teanode.kit.contacts.RelationshipKind
import com.teanode.kit.contacts.RelationshipTermGender

/**
 * RelationshipWords words what somebody is to somebody in the app's language, from the kind and the gender of the word the
 * server sends, so a father is "father" in the circles, in the person's card and in a conversation.
 */
object RelationshipWords {
    /** The word for a kind, in the gender asked: "mother" for a parent in a feminine word. A cousin, a friend and a colleague are one word. */
    fun word(context: Context, kind: RelationshipKind, termGender: RelationshipTermGender): String {
        fun pick(neutral: Int, masculine: Int, feminine: Int): String = context.getString(
            when (termGender) {
                RelationshipTermGender.Masculine -> masculine
                RelationshipTermGender.Feminine -> feminine
                RelationshipTermGender.Neutral -> neutral
            },
        )
        return when (kind) {
            RelationshipKind.Spouse -> pick(R.string.contacts_relationship_spouse, R.string.contacts_relationship_husband, R.string.contacts_relationship_wife)
            RelationshipKind.Partner -> pick(R.string.contacts_relationship_partner, R.string.contacts_relationship_boyfriend, R.string.contacts_relationship_girlfriend)
            RelationshipKind.Parent -> pick(R.string.contacts_relationship_parent, R.string.contacts_relationship_father, R.string.contacts_relationship_mother)
            RelationshipKind.Child -> pick(R.string.contacts_relationship_child, R.string.contacts_relationship_son, R.string.contacts_relationship_daughter)
            RelationshipKind.Sibling -> pick(R.string.contacts_relationship_sibling, R.string.contacts_relationship_brother, R.string.contacts_relationship_sister)
            RelationshipKind.Grandparent -> pick(R.string.contacts_relationship_grandparent, R.string.contacts_relationship_grandfather, R.string.contacts_relationship_grandmother)
            RelationshipKind.Grandchild -> pick(R.string.contacts_relationship_grandchild, R.string.contacts_relationship_grandson, R.string.contacts_relationship_granddaughter)
            RelationshipKind.AuntOrUncle -> pick(R.string.contacts_relationship_aunt_or_uncle, R.string.contacts_relationship_uncle, R.string.contacts_relationship_aunt)
            RelationshipKind.NieceOrNephew -> pick(R.string.contacts_relationship_niece_or_nephew, R.string.contacts_relationship_nephew, R.string.contacts_relationship_niece)
            RelationshipKind.Cousin -> context.getString(R.string.contacts_relationship_cousin)
            RelationshipKind.ParentInLaw -> pick(R.string.contacts_relationship_parent_in_law, R.string.contacts_relationship_father_in_law, R.string.contacts_relationship_mother_in_law)
            RelationshipKind.ChildInLaw -> pick(R.string.contacts_relationship_child_in_law, R.string.contacts_relationship_son_in_law, R.string.contacts_relationship_daughter_in_law)
            RelationshipKind.SiblingInLaw -> pick(R.string.contacts_relationship_sibling_in_law, R.string.contacts_relationship_brother_in_law, R.string.contacts_relationship_sister_in_law)
            RelationshipKind.StepParent -> pick(R.string.contacts_relationship_step_parent, R.string.contacts_relationship_stepfather, R.string.contacts_relationship_stepmother)
            RelationshipKind.StepChild -> pick(R.string.contacts_relationship_stepchild, R.string.contacts_relationship_stepson, R.string.contacts_relationship_stepdaughter)
            RelationshipKind.Friend -> context.getString(R.string.contacts_relationship_friend)
            RelationshipKind.Colleague -> context.getString(R.string.contacts_relationship_colleague)
        }
    }

    /** The word as the server sent it, in the app's language; the server's English word for a kind this app does not know. */
    fun word(context: Context, kindName: String?, genderName: String?, englishWord: String?): String {
        val kind = RelationshipKind.named(kindName) ?: return englishWord ?: ""
        return word(context, kind, RelationshipTermGender.named(genderName) ?: RelationshipTermGender.Neutral)
    }

    /** What the member is to the person, worded, or null when they have no relationship. */
    fun word(context: Context, member: PersonCardMember): String? =
        if (!member.hasRelationship) null else word(context, member.relationshipKind, member.relationshipTermGender, member.relationshipWord)

    /** The word starting a line, as a row of the circle shows it: "Father". The other languages have no capitals, so they are left as they are. */
    fun capitalized(word: String): String = word.replaceFirstChar { first -> first.uppercase() }
}
