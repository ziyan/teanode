import Foundation

// Bringing the contacts on the phone into the address book: which of them
// the address book already has, and the cards that are sent for the rest.
// The phone's contacts arrive here as plain values, so that this is the
// same on every platform and can be tested without the Contacts framework.

/// ContactIdentity is what tells one contact from another: the name and the
/// ways to reach them, whether they came from the phone or the address book.
public struct ContactIdentity: Hashable, Sendable {
    public let name: String
    public let emails: [String]
    public let phones: [String]

    public init(name: String, emails: [String], phones: [String]) {
        self.name = name
        self.emails = emails
        self.phones = phones
    }
}

extension ContactSummary {
    /// What the contact is matched by when the phone's contacts are imported.
    public var identity: ContactIdentity {
        ContactIdentity(name: name ?? "", emails: emails ?? [], phones: phones ?? [])
    }
}

/// ContactMatcher decides which contacts are already in the address book, so
/// that importing twice does not make every contact twice.
///
/// A contact is already there when it shares an email address (compared in
/// lower case) or a phone number (compared by its digits) with one that is.
/// A contact with neither is compared by its whole name instead, since that
/// is all there is to go on.
public struct ContactMatcher: Sendable {
    /// How many digits at the end of a number are compared when both numbers
    /// have at least that many: the digits before them are a country code or
    /// a trunk prefix, which one place writes and another leaves out.
    static let comparedDigitCount = 10

    private var emails: Set<String> = []
    private var wholeNumbers: Set<String> = []
    private var numberEnds: Set<String> = []
    private var namesWithoutAddresses: Set<String> = []

    public init(existing: [ContactIdentity]) {
        for contact in existing {
            let contactEmails = contact.emails.map(Self.normalizedEmail).filter { !$0.isEmpty }
            let contactNumbers = contact.phones.map(Self.digits).filter { !$0.isEmpty }
            emails.formUnion(contactEmails)
            for number in contactNumbers {
                wholeNumbers.insert(number)
                if number.count >= Self.comparedDigitCount {
                    numberEnds.insert(String(number.suffix(Self.comparedDigitCount)))
                }
            }
            if contactEmails.isEmpty, contactNumbers.isEmpty {
                let name = Self.normalizedName(contact.name)
                if !name.isEmpty { namesWithoutAddresses.insert(name) }
            }
        }
    }

    /// Whether the address book already has this contact.
    public func isAlreadyPresent(_ candidate: ContactIdentity) -> Bool {
        let candidateEmails = candidate.emails.map(Self.normalizedEmail).filter { !$0.isEmpty }
        let candidateNumbers = candidate.phones.map(Self.digits).filter { !$0.isEmpty }
        if candidateEmails.contains(where: emails.contains) { return true }
        for number in candidateNumbers {
            if number.count >= Self.comparedDigitCount {
                if numberEnds.contains(String(number.suffix(Self.comparedDigitCount))) { return true }
            } else if wholeNumbers.contains(number) {
                return true
            }
        }
        if candidateEmails.isEmpty, candidateNumbers.isEmpty {
            return namesWithoutAddresses.contains(Self.normalizedName(candidate.name))
        }
        return false
    }

    static func normalizedEmail(_ email: String) -> String {
        email.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
    }

    static func digits(_ phone: String) -> String {
        phone.filter { ("0"..."9").contains($0) }
    }

    static func normalizedName(_ name: String) -> String {
        name.split(whereSeparator: \.isWhitespace).joined(separator: " ").lowercased()
    }
}

/// ContactCard is a vCard on its way to the address book.
public enum ContactCard {
    /// The most a card may be, in bytes of UTF-8. The server refuses a card
    /// above 1 MiB, and the request that carries it is also held to 1 MiB
    /// with the card's line breaks and quotes written out as escapes, so a
    /// card is kept to half of that.
    public static let maximumByteCount = 512 * 1024

    /// Whether the server will take a card this large.
    public static func fits(_ card: String) -> Bool {
        card.utf8.count <= maximumByteCount
    }

    /// The card without its picture. A photograph is written into a card as
    /// text a third larger than the picture, which would make a card of
    /// hundreds of kilobytes for the sake of a thumbnail the address book
    /// list does not need.
    public static func removingPhoto(from card: String) -> String {
        var kept: [String] = []
        var isInPhoto = false
        // Split on the byte, since a CRLF is one Character to Swift and
        // would not be found by splitting on "\n".
        for slice in card.utf8.split(separator: UInt8(ascii: "\n"), omittingEmptySubsequences: false) {
            let line = String(decoding: slice, as: UTF8.self)
            // A folded line goes on from the one before it.
            if line.hasPrefix(" ") || line.hasPrefix("\t") {
                if !isInPhoto { kept.append(line) }
                continue
            }
            isInPhoto = isPhotoLine(line)
            if !isInPhoto { kept.append(line) }
        }
        return kept.joined(separator: "\n")
    }

    /// Whether a line starts the PHOTO property, which may carry a group
    /// ("item1.PHOTO") and parameters (";ENCODING=b").
    private static func isPhotoLine(_ line: String) -> Bool {
        guard let end = line.firstIndex(where: { $0 == ";" || $0 == ":" }) else { return false }
        let name = line[..<end]
        let withoutGroup = name.split(separator: ".", omittingEmptySubsequences: false).last ?? name
        return withoutGroup.uppercased() == "PHOTO"
    }
}

extension Documents {
    public static let importContact = """
        mutation ($addressBookId: String!, $card: String!) {
          SaveContact(addressBookId: $addressBookId, card: $card) { id name }
        }
        """
}

extension Connection {
    /// Adds a contact from a whole vCard, which is how the phone writes one.
    /// The server reads the card itself; a card whose UID the address book
    /// already has replaces that contact instead of making another.
    public func importContact(addressBookId: String, card: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let id: String }
            let SaveContact: Saved
        }
        _ = try await execute(
            Documents.importContact, variables: ["addressBookId": .string(addressBookId), "card": .string(card)], as: Answer.self)
    }
}
