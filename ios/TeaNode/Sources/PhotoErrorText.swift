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
            serverReason.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "The server would not take this file.")
        case .serverReason(let serverReason):
            serverReason
        case .fileIsEmpty:
            String(localized: "The photo library gave an empty file.")
        case .fileIsNotInLibrary:
            String(localized: "This photo is no longer in your photo library.")
        case .fileIsInICloudOnMobileData:
            String(localized: "The file is in iCloud and this network costs data.")
        case .systemDescription(let systemText):
            systemText
        }
    }
}
