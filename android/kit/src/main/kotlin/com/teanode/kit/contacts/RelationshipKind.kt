package com.teanode.kit.contacts

/** RelationshipKind is what somebody can be to the person, in the server's words. */
enum class RelationshipKind(val wireName: String) {
    Spouse("spouse"), Partner("partner"), Parent("parent"), Child("child"), Sibling("sibling"), Grandparent("grandparent"),
    Grandchild("grandchild"), AuntOrUncle("aunt_or_uncle"), NieceOrNephew("niece_or_nephew"), Cousin("cousin"),
    ParentInLaw("parent_in_law"), ChildInLaw("child_in_law"), SiblingInLaw("sibling_in_law"), StepParent("step_parent"),
    StepChild("step_child"), Friend("friend"), Colleague("colleague");

    companion object {
        /** The kind the server named, or null for one this app does not know. */
        fun named(wireName: String?): RelationshipKind? = entries.firstOrNull { kind -> kind.wireName == wireName }
    }
}

/** The gender of the word shown for a relationship. */
enum class RelationshipTermGender(val wireName: String) {
    Masculine("masculine"), Feminine("feminine"), Neutral("neutral");

    companion object {
        fun named(wireName: String?): RelationshipTermGender? = entries.firstOrNull { gender -> gender.wireName == wireName }
    }
}
