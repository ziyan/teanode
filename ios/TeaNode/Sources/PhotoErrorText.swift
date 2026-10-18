import Foundation
import TeaNodeKit

// The words for the errors TeaNodeKit throws for photos. The Kit names what went
// wrong and has no English that can reach the screen; each is written here,
// through the String Catalog, in the person's language. A status code is never
// shown. The server's own reason is shown when it gave one.

extension PhotoError {
    var localizedText: String {
        switch self {
        case .fileCouldNotBeRead:
            String(localized: "The file could not be read. Try again in a moment.")
        case .fileNotAvailable:
            String(localized: "This file is not available. It may have been deleted, or it is no longer shared with you.")
        case .nothingToSaveYet:
            String(localized: "This photo has nothing to save yet. Try again in a moment.")
        case .noAddressBook:
            String(localized: "There is no address book to put a new contact in.")
        }
    }
}

extension PhotoBackupFailure {
    var localizedText: String {
        switch self {
        case .signInRefused:
            String(localized: "The server did not accept this iPhone's backup password.")
        case .serverDidNotSayWhichPhoto:
            String(localized: "The server took a file but did not say which photo it was.")
        case .fileRefused(let serverReason):
            serverReason.flatMap { $0.isEmpty ? nil : Self.sentence($0) } ?? String(localized: "The server would not take this file.")
        case .serverReason(let serverReason):
            Self.sentence(serverReason)
        case .fileIsEmpty:
            String(localized: "The photo library gave an empty file.")
        case .fileIsNotInLibrary:
            String(localized: "This photo is no longer in your photo library.")
        case .fileIsInICloudOnMobileData:
            String(localized: "The file is in iCloud and this network costs data.")
        case .fileTookTooLongToRead:
            String(localized: "A file took too long to come from your photo library or iCloud. Backup went on without it and tries it again later.")
        case .systemDescription(let systemText):
            Self.sentence(systemText)
        }
    }

    /// A reason the server or the system gave, which may be a lowercase phrase
    /// such as "photo storage is unavailable", written as the sentence the
    /// screen shows: a capital first letter and a full stop.
    private static func sentence(_ reason: String) -> String {
        let trimmed = reason.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let first = trimmed.first else { return trimmed }
        let capitalized = first.uppercased() + trimmed.dropFirst()
        return capitalized.last.map { ".!?。".contains($0) } == true ? capitalized : capitalized + "."
    }
}
