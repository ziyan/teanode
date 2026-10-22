import Foundation

// A person's card: who somebody is to the person, and a few items of
// everything of theirs that can be seen elsewhere in the app, read in one
// round trip (ReadPersonCard, internal/api/v1api/apigraph/person_card.go).
// The sections that hang on the account (where they are, their health, the
// albums they shared) need an account that shares a circle with the person;
// those that hang on the person's own contact for them (details, mail,
// photos of them, memory, recordings) need the contact.

/// PersonCardTarget names whose card to read: an account in the person's
/// circles, or one of their own contacts, which may or may not be one.
public enum PersonCardTarget: Hashable, Sendable, Identifiable {
    case account(userId: String)
    case contact(addressBookId: String, contactId: String)

    public var id: String {
        switch self {
        case .account(let userId): "account/\(userId)"
        case .contact(let addressBookId, let contactId): "contact/\(addressBookId)/\(contactId)"
        }
    }

    /// The arguments ReadPersonCard takes for it: an account by its id, a
    /// contact by its address book and its id, never both.
    var variables: [String: JSONValue] {
        switch self {
        case .account(let userId): ["userId": .string(userId)]
        case .contact(let addressBookId, let contactId): ["addressBookId": .string(addressBookId), "contactId": .string(contactId)]
        }
    }
}

/// PersonCardSectionState says whether a section of the card has something to
/// show. A state this app does not know is read as unavailable, which leaves
/// the section out.
public enum PersonCardSectionState: String, Sendable {
    /// Read; its items may still be none.
    case shown
    /// The account has not shared this part with the person.
    case notShared
    /// The section is found through the person's contact for the account, and
    /// none is linked.
    case needsContact
    /// The person may not use this part, or there is no account to read it
    /// from. The card leaves the section out.
    case unavailable
    /// It failed or did not answer in time.
    case failed
}

extension PersonCardSectionState {
    /// Whether the card draws the section's own rows: not one it leaves out,
    /// and not one that waits for a contact, which the card asks for once for
    /// all of them.
    public var isListed: Bool { self != .unavailable && self != .needsContact }
}

extension PersonCardSectionState: Decodable {
    public init(from decoder: Decoder) throws {
        let text = try decoder.singleValueContainer().decode(String.self)
        self = PersonCardSectionState(rawValue: text) ?? .unavailable
    }
}

/// PersonCardCircle is a circle the person and the other share.
public struct PersonCardCircle: Decodable, Hashable, Sendable, Identifiable {
    public let circleId: String
    public let circleName: String
    /// family, friends or colleagues.
    public let circleKind: String

    public var id: String { circleId }
    public var knownCircleKind: CircleKind? { CircleKind(rawValue: circleKind) }
}

/// PersonCardLocationSection is where they are, as the Location part shows it.
public struct PersonCardLocationSection: Decodable, Hashable, Sendable {
    public let sectionState: PersonCardSectionState
    /// Their shared location with its share level, latest point and the visit
    /// they are in; nil unless shown.
    public let location: PersonLocation?
    /// Their visits today, newest first, when they share where they have been.
    public let todayVisits: [PlaceVisit]
    /// Whether there were more visits today than these.
    public let hasMoreTodayVisits: Bool

    private enum CodingKeys: String, CodingKey {
        case sectionState, location, todayVisits, hasMoreTodayVisits
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        sectionState = try container.decode(PersonCardSectionState.self, forKey: .sectionState)
        location = try container.decodeIfPresent(PersonLocation.self, forKey: .location)
        todayVisits = try container.decodeIfPresent([PlaceVisit].self, forKey: .todayVisits) ?? []
        hasMoreTodayVisits = try container.decodeIfPresent(Bool.self, forKey: .hasMoreTodayVisits) ?? false
    }
}

/// PersonCardHealthSection is the health they share with the person: each kind with
/// its latest day, steps first.
public struct PersonCardHealthSection: Decodable, Hashable, Sendable {
    public let sectionState: PersonCardSectionState
    public let healthTypes: [HealthTypeView]
    public let hasMore: Bool

    private enum CodingKeys: String, CodingKey {
        case sectionState, healthTypes, hasMore
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        sectionState = try container.decode(PersonCardSectionState.self, forKey: .sectionState)
        healthTypes = try container.decodeIfPresent([HealthTypeView].self, forKey: .healthTypes) ?? []
        hasMore = try container.decodeIfPresent(Bool.self, forKey: .hasMore) ?? false
    }

    /// Whether the kind's latest day is the day given, written "YYYY-MM-DD" in
    /// the phone's calendar. Only then is it said to be today.
    public static func isToday(_ type: HealthTypeView, today: Date, calendar: Calendar) -> Bool {
        guard let summaryDay = type.latestDaySummary?.summaryDay else { return false }
        return summaryDay == HealthDays.text(of: today, calendar: calendar)
    }
}

/// PersonCardFinanceSection is the finance accounts they share with the
/// person, each as the level it was shared at shows it. The agent is never
/// shown it.
public struct PersonCardFinanceSection: Decodable, Hashable, Sendable {
    public let sectionState: PersonCardSectionState
    public let financeAccounts: [SharedFinanceAccount]
    public let hasMore: Bool

    private enum CodingKeys: String, CodingKey {
        case sectionState, financeAccounts, hasMore
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        sectionState = try container.decode(PersonCardSectionState.self, forKey: .sectionState)
        financeAccounts = try container.decodeIfPresent([SharedFinanceAccount].self, forKey: .financeAccounts) ?? []
        hasMore = try container.decodeIfPresent(Bool.self, forKey: .hasMore) ?? false
    }
}

/// PersonCardPhotosSection is photos of them in the person's library, and the albums
/// they shared with the person. The two have their own states: photos of a
/// person are found through the contact, albums through the account.
public struct PersonCardPhotosSection: Decodable, Hashable, Sendable {
    public let photosState: PersonCardSectionState
    public let photos: [Photo]
    public let hasMorePhotos: Bool
    public let albumsState: PersonCardSectionState
    public let albums: [PhotoAlbum]
    public let hasMoreAlbums: Bool

    private enum CodingKeys: String, CodingKey {
        case photosState, photos, hasMorePhotos, albumsState, albums, hasMoreAlbums
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        photosState = try container.decode(PersonCardSectionState.self, forKey: .photosState)
        photos = try container.decodeIfPresent([Photo].self, forKey: .photos) ?? []
        hasMorePhotos = try container.decodeIfPresent(Bool.self, forKey: .hasMorePhotos) ?? false
        albumsState = try container.decode(PersonCardSectionState.self, forKey: .albumsState)
        albums = try container.decodeIfPresent([PhotoAlbum].self, forKey: .albums) ?? []
        hasMoreAlbums = try container.decodeIfPresent(Bool.self, forKey: .hasMoreAlbums) ?? false
    }
}

/// PersonCardMail is one mail with the person, newest first.
public struct PersonCardMail: Decodable, Hashable, Sendable, Identifiable {
    public let mailboxItemId: String
    public let mailboxId: String
    /// The folder it is in, as the mailbox pages address it.
    public let folderPath: String
    public let subject: String
    public let fromAddress: String
    public let fromName: String
    /// Whether they sent it; otherwise the person sent it to them.
    public let isFromPerson: Bool
    public let isUnread: Bool
    /// RFC 3339.
    public let receivedAt: String
    /// What the person's agent wrote about it on one line; empty when it has
    /// not read it.
    public let snippet: String

    public var id: String { mailboxItemId }
    public var receivedDate: Date? { ServerTime.date(receivedAt) }

    /// Who sent it, for a line under the subject: the name, else the address.
    public var senderText: String { fromName.isEmpty ? fromAddress : fromName }
}

/// PersonCardMailSection is the person's recent mail from or to any address on
/// their contact for the other.
public struct PersonCardMailSection: Decodable, Hashable, Sendable {
    public let sectionState: PersonCardSectionState
    public let mails: [PersonCardMail]
    public let hasMore: Bool

    private enum CodingKeys: String, CodingKey {
        case sectionState, mails, hasMore
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        sectionState = try container.decode(PersonCardSectionState.self, forKey: .sectionState)
        mails = try container.decodeIfPresent([PersonCardMail].self, forKey: .mails) ?? []
        hasMore = try container.decodeIfPresent(Bool.self, forKey: .hasMore) ?? false
    }
}

/// PersonCardRecording is one recording they speak in, newest first.
public struct PersonCardRecording: Decodable, Hashable, Sendable, Identifiable {
    public let recordingId: String
    public let title: String
    /// RFC 3339; nil when it is not known.
    public let recordedAt: String?
    public let durationMS: Int
    /// How many passages they speak.
    public let spokenPassageCount: Int

    public var id: String { recordingId }
    public var recordedDate: Date? { ServerTime.date(recordedAt) }
}

/// PersonCardRecordingSection is the recordings a passage of which the person
/// confirmed as spoken by a speaker they tied to their contact for the other.
public struct PersonCardRecordingSection: Decodable, Hashable, Sendable {
    public let sectionState: PersonCardSectionState
    public let recordings: [PersonCardRecording]
    public let hasMore: Bool

    private enum CodingKeys: String, CodingKey {
        case sectionState, recordings, hasMore
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        sectionState = try container.decode(PersonCardSectionState.self, forKey: .sectionState)
        recordings = try container.decodeIfPresent([PersonCardRecording].self, forKey: .recordings) ?? []
        hasMore = try container.decodeIfPresent(Bool.self, forKey: .hasMore) ?? false
    }
}

/// PersonCardMemoryPage is one page of memory about them.
public struct PersonCardMemoryPage: Decodable, Hashable, Sendable, Identifiable {
    public let nodePath: String
    public let nodeName: String
    /// What kind of page it is, such as person or topic.
    public let nodeKind: String
    /// The page's first lines, without Markdown marks.
    public let summary: String
    public let updatedAt: String
    /// The page is not tied to the contact: it is the person page with the
    /// contact's name. Nil from a server older than it.
    public let isMatchedByName: Bool?

    public var id: String { nodePath }
}

/// PersonCardMemorySection is the pages of memory tied to the person's contact
/// for the other, their own page first.
public struct PersonCardMemorySection: Decodable, Hashable, Sendable {
    public let sectionState: PersonCardSectionState
    public let pages: [PersonCardMemoryPage]
    public let hasMore: Bool

    private enum CodingKeys: String, CodingKey {
        case sectionState, pages, hasMore
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        sectionState = try container.decode(PersonCardSectionState.self, forKey: .sectionState)
        pages = try container.decodeIfPresent([PersonCardMemoryPage].self, forKey: .pages) ?? []
        hasMore = try container.decodeIfPresent(Bool.self, forKey: .hasMore) ?? false
    }
}

/// PersonCard is one person's card.
public struct PersonCard: Decodable, Hashable, Sendable {
    /// The account, as a member of the person's circles; nil for a contact
    /// that is not one. Its relationship fields are only between the two.
    public let person: CircleMember?
    /// The person's own contact for them: the one asked for, or the one linked
    /// to the account. Nil when there is none.
    public let contact: ContactDetail?
    /// The circles both are in; none for a contact without an account.
    public let sharedCircles: [PersonCardCircle]
    /// Whether a chat with them can be opened.
    public let canMessage: Bool
    public let location: PersonCardLocationSection?
    public let health: PersonCardHealthSection?
    public let finance: PersonCardFinanceSection?
    public let photos: PersonCardPhotosSection?
    public let mail: PersonCardMailSection?
    public let recordings: PersonCardRecordingSection?
    public let memory: PersonCardMemorySection?

    /// How many items each section is asked to keep.
    public static let itemLimit = 4

    private enum CodingKeys: String, CodingKey {
        case person, contact, sharedCircles, canMessage, location, health, finance, photos, mail, recordings, memory
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        person = try container.decodeIfPresent(CircleMember.self, forKey: .person)
        contact = try container.decodeIfPresent(ContactDetail.self, forKey: .contact)
        sharedCircles = try container.decodeIfPresent([PersonCardCircle].self, forKey: .sharedCircles) ?? []
        canMessage = try container.decodeIfPresent(Bool.self, forKey: .canMessage) ?? false
        location = try container.decodeIfPresent(PersonCardLocationSection.self, forKey: .location)
        health = try container.decodeIfPresent(PersonCardHealthSection.self, forKey: .health)
        finance = try container.decodeIfPresent(PersonCardFinanceSection.self, forKey: .finance)
        photos = try container.decodeIfPresent(PersonCardPhotosSection.self, forKey: .photos)
        mail = try container.decodeIfPresent(PersonCardMailSection.self, forKey: .mail)
        recordings = try container.decodeIfPresent(PersonCardRecordingSection.self, forKey: .recordings)
        memory = try container.decodeIfPresent(PersonCardMemorySection.self, forKey: .memory)
    }

    /// What to call them: the account's name, else the contact's, else the
    /// first address on the contact.
    public var displayName: String {
        if let person { return person.displayName }
        if let name = contact?.name, !name.isEmpty { return name }
        return contact?.emails?.first ?? ""
    }

    /// Whether the person has a contact for them.
    public var hasContact: Bool { contact != nil }

    /// Whether any section is waiting for a contact to be linked to the
    /// account: the card then offers to link one, once.
    public var needsContact: Bool {
        let states = [
            photos?.photosState, mail?.sectionState, recordings?.sectionState, memory?.sectionState,
        ]
        return states.contains { $0 == PersonCardSectionState.needsContact }
    }

    /// The first address on the contact, which Mail writes to.
    public var mailAddress: String? {
        contact?.emails?.first { !$0.isEmpty }
    }

    /// The call the first number on the contact makes: only digits and the
    /// plus sign are kept, as a telephone address takes them.
    public var callURL: URL? {
        guard let phone = contact?.phones?.first(where: { !$0.isEmpty }) else { return nil }
        let number = phone.filter { $0.isNumber || $0 == "+" }
        return number.isEmpty ? nil : URL(string: "tel:\(number)")
    }

    /// Directions to their latest position, when they share it.
    public var directionsURL: URL? {
        guard let point = location?.location?.latestPoint else { return nil }
        return URL(string: "https://maps.apple.com/?daddr=\(point.latitudeDegrees),\(point.longitudeDegrees)")
    }
}

extension Documents {
    /// The visits of a card, without the photos a visit may carry: the card
    /// shows the place and the times. Written out in full, since the server's
    /// test of the app's documents fills in one level of shared fields.
    static let personCardVisitFields = """
        id arrivedAt departedAt durationSeconds latitudeDegrees longitudeDegrees accuracyMeters visitSource
        place {
          id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
          visitCount lastVisitAt namingErrorMessage createdAt updatedAt
        }
        """

    public static let personCard = """
        query ($userId: String, $addressBookId: String, $contactId: String, $itemLimit: Int) {
          ReadPersonCard(userId: $userId, addressBookId: $addressBookId, contactId: $contactId, itemLimit: $itemLimit) {
            person { userId username name avatarUrl avatarColorIndex memberRole joinedAt relationshipKind relationshipTermGender relationshipWord linkedContactId }
            contact {
              id addressBookId name organization emails phones note hasPhoto
              addresses { street locality region postalCode country }
              accountUserId account { userId username name avatarUrl avatarColorIndex }
            }
            sharedCircles { circleId circleName circleKind }
            canMessage
            location { sectionState location { \(personLocationFields) } todayVisits { \(personCardVisitFields) } hasMoreTodayVisits }
            health { sectionState healthTypes { \(healthTypeFields) } hasMore }
            finance {
              sectionState hasMore
              financeAccounts {
                financeAccountId ownerUserId ownerUsername ownerName financeShareLevel accountName accountMask accountKind currencyCode currentBalance balanceAt
                creditUsage { owedAmount creditLimitAmount creditLimitSource usageShare }
              }
            }
            photos { photosState photos { \(photoFields) } hasMorePhotos albumsState albums { \(photoAlbumFields) } hasMoreAlbums }
            mail { sectionState mails { mailboxItemId mailboxId folderPath subject fromAddress fromName isFromPerson isUnread receivedAt snippet } hasMore }
            recordings { sectionState recordings { recordingId title recordedAt durationMS spokenPassageCount } hasMore }
            memory { sectionState pages { nodePath nodeName nodeKind summary updatedAt isMatchedByName } hasMore }
          }
        }
        """
}

extension Connection {
    /// Reads a person's card: who they are to the person and a few items from
    /// each part of the app that concerns them. A section that failed says so
    /// and the rest is there; somebody the person shares no circle with, and
    /// no contact of the person's, is answered as not found.
    public func readPersonCard(_ target: PersonCardTarget, itemLimit: Int = PersonCard.itemLimit) async throws -> PersonCard {
        struct Answer: Decodable, Sendable { let ReadPersonCard: PersonCard }
        var variables = target.variables
        variables["itemLimit"] = .number(Double(itemLimit))
        return try await execute(Documents.personCard, variables: variables, as: Answer.self).ReadPersonCard
    }
}
