package com.teanode.app.contacts

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import com.teanode.kit.contacts.RelationshipKind
import com.teanode.kit.contacts.RelationshipTermGender
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** The words a relationship is said in, from the kind and the gender the server sends. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ContactsLogicTest {
    private val context: Context = ApplicationProvider.getApplicationContext()

    @Test
    fun aKindIsWordedInTheGenderAsked() {
        assertEquals("mother", RelationshipWords.word(context, RelationshipKind.Parent, RelationshipTermGender.Feminine))
        assertEquals("father", RelationshipWords.word(context, RelationshipKind.Parent, RelationshipTermGender.Masculine))
        assertEquals("parent", RelationshipWords.word(context, RelationshipKind.Parent, RelationshipTermGender.Neutral))
        assertEquals("cousin", RelationshipWords.word(context, RelationshipKind.Cousin, RelationshipTermGender.Feminine))
    }

    @Test
    fun aKindThisAppDoesNotKnowKeepsTheServersEnglishWord() {
        assertEquals("godmother", RelationshipWords.word(context, "godparent", "feminine", "godmother"))
        assertEquals("sister", RelationshipWords.word(context, "sibling", "feminine", "sister"))
        assertEquals("Father", RelationshipWords.capitalized("father"))
    }
}
