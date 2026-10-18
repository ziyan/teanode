import TeaNodeKit

/// RelationshipWords words what somebody is to somebody in the app's language,
/// from the kind and the gender of the word the server sends. The circles
/// screen, the Relationship sheet and the note in the conversation share it, so
/// a father is "father" in all three and "父" in Japanese in all three.
enum RelationshipWords {
    /// The word for a kind, in the gender asked: "mother" for a parent in a
    /// feminine word. A cousin, a friend and a colleague are one word.
    static func word(_ kind: RelationshipKind, _ termGender: RelationshipTermGender) -> String {
        switch kind {
        case .spouse:
            return pick(termGender, neutral: String(localized: "spouse"), masculine: String(localized: "husband"), feminine: String(localized: "wife"))
        case .partner:
            return pick(
                termGender, neutral: String(localized: "partner"), masculine: String(localized: "boyfriend"), feminine: String(localized: "girlfriend"))
        case .parent:
            return pick(termGender, neutral: String(localized: "parent"), masculine: String(localized: "father"), feminine: String(localized: "mother"))
        case .child:
            return pick(termGender, neutral: String(localized: "child"), masculine: String(localized: "son"), feminine: String(localized: "daughter"))
        case .sibling:
            return pick(termGender, neutral: String(localized: "sibling"), masculine: String(localized: "brother"), feminine: String(localized: "sister"))
        case .grandparent:
            return pick(
                termGender, neutral: String(localized: "grandparent"), masculine: String(localized: "grandfather"),
                feminine: String(localized: "grandmother"))
        case .grandchild:
            return pick(
                termGender, neutral: String(localized: "grandchild"), masculine: String(localized: "grandson"),
                feminine: String(localized: "granddaughter"))
        case .auntOrUncle:
            return pick(
                termGender, neutral: String(localized: "aunt or uncle"), masculine: String(localized: "uncle"), feminine: String(localized: "aunt"))
        case .nieceOrNephew:
            return pick(
                termGender, neutral: String(localized: "niece or nephew"), masculine: String(localized: "nephew"),
                feminine: String(localized: "niece"))
        case .cousin:
            return String(localized: "cousin")
        case .parentInLaw:
            return pick(
                termGender, neutral: String(localized: "parent-in-law"), masculine: String(localized: "father-in-law"),
                feminine: String(localized: "mother-in-law"))
        case .childInLaw:
            return pick(
                termGender, neutral: String(localized: "child-in-law"), masculine: String(localized: "son-in-law"),
                feminine: String(localized: "daughter-in-law"))
        case .siblingInLaw:
            return pick(
                termGender, neutral: String(localized: "sibling-in-law"), masculine: String(localized: "brother-in-law"),
                feminine: String(localized: "sister-in-law"))
        case .stepParent:
            return pick(
                termGender, neutral: String(localized: "step-parent"), masculine: String(localized: "stepfather"),
                feminine: String(localized: "stepmother"))
        case .stepChild:
            return pick(
                termGender, neutral: String(localized: "stepchild"), masculine: String(localized: "stepson"),
                feminine: String(localized: "stepdaughter"))
        case .friend:
            return String(localized: "friend")
        case .colleague:
            return String(localized: "colleague")
        }
    }

    private static func pick(_ termGender: RelationshipTermGender, neutral: String, masculine: String, feminine: String) -> String {
        switch termGender {
        case .masculine: return masculine
        case .feminine: return feminine
        case .neutral: return neutral
        }
    }

    /// The word as the server sent it, in the app's language; the server's
    /// English word for a kind this app does not know.
    static func word(kindName: String?, genderName: String?, englishWord: String?) -> String {
        guard let kind = RelationshipKind(rawValue: kindName ?? "") else { return englishWord ?? "" }
        return word(kind, RelationshipTermGender(rawValue: genderName ?? "") ?? .neutral)
    }

    /// What the member is to the person, worded, or nil when they have no
    /// relationship.
    static func word(of member: CircleMember) -> String? {
        guard member.hasRelationship else { return nil }
        return word(kindName: member.relationshipKind, genderName: member.relationshipTermGender, englishWord: member.relationshipWord)
    }

    /// One of the words the server offers, as the Relationship picker lists it.
    static func word(of term: RelationshipTerm) -> String {
        word(kindName: term.relationshipKind, genderName: term.termGender, englishWord: term.word)
    }

    /// The word starting a line, as a row of the circle shows it: "Father".
    /// The other languages have no capitals, so they are left as they are.
    static func capitalized(_ word: String) -> String {
        word.prefix(1).uppercased() + String(word.dropFirst())
    }
}

extension PersonPronoun {
    /// How the profile and the Relationship sheet name each choice.
    var title: String {
        switch self {
        case .notSet: String(localized: "Not set")
        case .he: String(localized: "He")
        case .she: String(localized: "She")
        case .they: String(localized: "They")
        }
    }
}
