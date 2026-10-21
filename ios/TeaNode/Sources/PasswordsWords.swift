import Foundation
import SwiftUI
import TeaNodeKit
import UIKit
import UniformTypeIdentifiers

// The words of the Passwords screens that are used in more than one place, and
// small helpers over the opened items that the list, the item screen and the
// search share.

/// PasswordsWords names the parts of Passwords in the person's language.
enum PasswordsWords {
    static var title: String { String(localized: "Passwords") }

    static func kindName(_ itemKind: PasswordsItemKind) -> String {
        switch itemKind {
        case .login: String(localized: "Login")
        case .card: String(localized: "Card")
        case .note: String(localized: "Note")
        }
    }

    static func kindPluralName(_ itemKind: PasswordsItemKind) -> String {
        switch itemKind {
        case .login: String(localized: "Logins")
        case .card: String(localized: "Cards")
        case .note: String(localized: "Notes")
        }
    }

    static func kindSystemImage(_ itemKind: PasswordsItemKind) -> String {
        switch itemKind {
        case .login: "key"
        case .card: "creditcard"
        case .note: "note.text"
        }
    }

    /// "15 minutes", "1 hour", "1 day".
    static func lockAfterText(minutes: Int) -> String {
        switch minutes {
        case 1: String(localized: "1 minute")
        case 5: String(localized: "5 minutes")
        case 15: String(localized: "15 minutes")
        case 30: String(localized: "30 minutes")
        case 60: String(localized: "1 hour")
        case 240: String(localized: "4 hours")
        case 1440: String(localized: "1 day")
        case 10080: String(localized: "1 week")
        default: String(localized: "\(minutes) minutes")
        }
    }

    /// The lock times the settings offer.
    static let lockAfterChoices = [1, 5, 15, 30, 60, 240, 1440, 10080]

    /// What a failed call says, in the person's language. The Passwords
    /// session throws errors whose own text is English only, so the cases the
    /// person can act on are worded here; the server's own reason comes through
    /// as it is, and anything else as the sentence the caller wrote.
    static func problemText(_ error: Error, orSay fallback: String) -> String {
        if let passwordsError = error as? PasswordsError { return text(for: passwordsError) }
        if let cryptoError = error as? PasswordsCryptoError { return text(for: cryptoError, orSay: fallback) }
        if let keyStoreError = error as? PasswordsKeyStoreError { return text(for: keyStoreError, orSay: fallback) }
        return error.refusalReason(orSay: fallback).asReasonSentence
    }

    private static func text(for error: PasswordsError) -> String {
        switch error {
        case .locked: String(localized: "Passwords is locked.")
        case .needsPassphrase: String(localized: "Type your passphrase to open Passwords.")
        case .notSetUp: String(localized: "Passwords is not set up.")
        case .alreadySetUp: String(localized: "Passwords is already set up.")
        case .wrongPassphrase: String(localized: "That passphrase is not right.")
        case .wrongRecoveryKey: String(localized: "That recovery key is not right.")
        case .itemChangedOnAnotherDevice: String(localized: "This item changed on another device. Open it again.")
        case .itemNotFound: String(localized: "That item is not here any more.")
        case .onlyOwnerCanChangeItem: String(localized: "Only the person who owns an item can change it.")
        case .itemKindCannotChange: String(localized: "An item cannot change from a login to a note or a card.")
        case .cardCannotBeUsedByAgent: String(localized: "A card cannot be used by the agent.")
        case .recipientKeyChangedSinceRead: String(localized: "That person's key changed while you were sharing. Open the share again and compare the new safety code.")
        case .tooManyAttempts: String(localized: "Too many attempts. Wait a while and try again.")
        case .unknownItemKind: String(localized: "This item is of a kind this version cannot open. Update the app.")
        case .invalidServerAnswer: String(localized: "The server sent something this app cannot read.")
        case .ownerKeyChanged: String(localized: "The key of the person who shared this changed. Compare the safety codes with them, then trust the new key.")
        case .ownerKeyUnknown: String(localized: "This iPhone has not seen the key of the person who shared this yet. Open Passwords and pull to refresh.")
        }
    }

    private static func text(for error: PasswordsCryptoError, orSay fallback: String) -> String {
        switch error {
        case .invalidRecoveryKey:
            return String(localized: "That is not a recovery key. It is 52 letters and digits, in groups of four.")
        case .iterationCountTooLow:
            return String(localized: "The server asked for a passphrase check that is too weak. Passwords stays closed.")
        case .iterationCountTooHigh:
            return String(localized: "The server asked for a passphrase check that is far too slow. Passwords stays closed.")
        default:
            return fallback
        }
    }

    private static func text(for error: PasswordsKeyStoreError, orSay fallback: String) -> String {
        switch error {
        case .canceled: String(localized: "Face ID did not open Passwords. Type your passphrase instead.")
        case .unavailable: String(localized: "This iPhone cannot keep the key behind Face ID. Set a passcode and Face ID in Settings first.")
        case .failed: fallback
        }
    }

    /// Who a share is with: a person's name, or "Everyone in Home" for a circle.
    static func granteeName(of share: PasswordShareView) -> String {
        if share.granteeCircleId != nil {
            let circleName = share.granteeName
            return String(localized: "Everyone in \(circleName)")
        }
        return share.granteeName
    }

    /// Whom an item the person owns is shared with, for the Share row: "Sam and
    /// Alex", "Home", or "Home, 1 waiting" when somebody the share reaches has
    /// not set up Passwords or still waits for the owner to confirm their key.
    /// A circle is named by the circle, not by everyone in it.
    static func shareSummary(of shares: [PasswordShareView]) -> String {
        if shares.isEmpty { return String(localized: "Not shared") }
        let names = ListFormatter.localizedString(byJoining: shares.map(\.granteeName))
        let waitingUserIds = Set(shares.flatMap(\.recipients).filter { $0.publicKey == nil || !$0.hasKey }.map(\.userId))
        if waitingUserIds.isEmpty { return names }
        return String(localized: "\(names), \(waitingUserIds.count) waiting")
    }

    /// "Copied" sentence for a field: "Password copied."
    static func copiedText(_ fieldName: String) -> String {
        String(localized: "\(fieldName) copied.")
    }
}

/// PasswordsFilter is which items the list shows.
enum PasswordsFilter: Hashable {
    case everything
    case kind(PasswordsItemKind)
    case shared
    case usableByAgent
    case folder(String)

    var filterTitle: String {
        switch self {
        case .everything: PasswordsWords.title
        case .kind(let itemKind): PasswordsWords.kindPluralName(itemKind)
        case .shared: String(localized: "Shared")
        case .usableByAgent: String(localized: "Used by the agent")
        case .folder(let name): name
        }
    }

    func includes(_ item: PasswordItem) -> Bool {
        switch self {
        case .everything: true
        case .kind(let itemKind): item.itemKind == itemKind
        case .shared: item.isSharedWithMe || !item.shares.isEmpty
        case .usableByAgent: item.isUsableByAgent
        case .folder(let name): item.folder == name
        }
    }
}

extension PasswordItem {
    /// The folder the item is in; empty for none.
    var folder: String {
        switch fields {
        case .login(let login): login.folder
        case .card(let card): card.folder
        case .note(let note): note.folder
        }
    }

    /// The line under the name in a list: the user name of a login, the end of
    /// a card number, the first line of a note.
    var subtitle: String {
        switch fields {
        case .login(let login): return login.userName
        case .card(let card): return PasswordsFormat.maskedCardNumber(card.number)
        case .note(let note):
            return note.text.split(whereSeparator: \.isNewline).first.map { String($0) } ?? ""
        }
    }

    /// Whether the item matches words typed in the search field. A password,
    /// card number or security code is never searched.
    func matches(words: String) -> Bool {
        let trimmed = words.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { return true }
        var haystacks: [String] = [name, folder, ownerName]
        switch fields {
        case .login(let login):
            haystacks.append(login.userName)
            haystacks.append(contentsOf: login.sites)
            haystacks.append(contentsOf: login.passkeys.map(\.relyingPartyId))
        case .card(let card): haystacks += [card.cardholderName, card.brand]
        case .note: break
        }
        return haystacks.contains { $0.localizedStandardContains(trimmed) }
    }

    /// The addresses of a login, each opened in Safari.
    var sites: [String] {
        if case .login(let login) = fields { return login.sites }
        return []
    }
}

/// PasswordsValueText shows a value the person stored (a user name, a site, a
/// password, a note) so that a line never ends in a hyphen SwiftUI made up:
/// the value may end a line only after "@", ".", "/" and the like, or inside a
/// string too long to be a word (see `PasswordsDisplayText`). It reads aloud as
/// the value itself. Use it for every stored value that is shown; the copy
/// buttons copy the value, never what is shown here.
struct PasswordsValueText: View {
    let storedValue: String
    var isProse = false

    init(_ storedValue: String, isProse: Bool = false) {
        self.storedValue = storedValue
        self.isProse = isProse
    }

    var body: some View {
        Text(verbatim: PasswordsDisplayText.breakable(storedValue, isProse: isProse))
            .accessibilityLabel(storedValue)
    }
}

/// PasswordsAppSwitcherCover hides what Passwords shows while the app is not in
/// front, so the picture iOS keeps of it for the app switcher (and what shows
/// for a moment when the app comes back) holds no password. Every screen that
/// can show a secret has it: the Passwords screen and each sheet over it, since
/// a sheet is drawn above the screen's own cover. It stays on after the app is
/// back until the lock time has been checked (`PasswordsModel.isHoldingCover`).
struct PasswordsAppSwitcherCover: ViewModifier {
    let model: PasswordsModel
    @Environment(\.scenePhase) private var scenePhase

    private var isCovering: Bool {
        let isOpen = model.phase == .unlocked || model.phase == .recoveryKey
        return isOpen && (scenePhase != .active || model.isHoldingCover)
    }

    func body(content: Content) -> some View {
        content.overlay {
            if isCovering {
                Color(.systemBackground)
                    .ignoresSafeArea()
                    .accessibilityHidden(true)
            }
        }
    }
}

extension View {
    /// Covers the view while the app is away or Passwords has not yet been checked for locking.
    func passwordsAppSwitcherCover(model: PasswordsModel) -> some View {
        modifier(PasswordsAppSwitcherCover(model: model))
    }
}

/// PasswordsFormat writes values for the screen.
enum PasswordsFormat {
    /// "•••• 4242" for a card number: only the last four digits show.
    static func maskedCardNumber(_ number: String) -> String {
        let digits = number.filter(\.isNumber)
        guard digits.count >= 4 else { return digits.isEmpty ? "" : "••••" }
        return "•••• " + String(digits.suffix(4))
    }

    /// A card number in groups of four, as it is printed.
    static func groupedCardNumber(_ number: String) -> String {
        let digits = number.filter(\.isNumber)
        var groups: [String] = []
        var current = ""
        for digit in digits {
            current.append(digit)
            if current.count == 4 {
                groups.append(current)
                current = ""
            }
        }
        if !current.isEmpty { groups.append(current) }
        return groups.joined(separator: " ")
    }

    /// "09/29" for a card's month and year; empty when neither is known.
    static func expiryText(month: String, year: String) -> String {
        let trimmedMonth = month.trimmingCharacters(in: .whitespaces)
        let trimmedYear = year.trimmingCharacters(in: .whitespaces)
        if trimmedMonth.isEmpty && trimmedYear.isEmpty { return "" }
        let paddedMonth = trimmedMonth.count == 1 ? "0" + trimmedMonth : trimmedMonth
        let shortYear = trimmedYear.count == 4 ? String(trimmedYear.suffix(2)) : trimmedYear
        return paddedMonth + "/" + shortYear
    }

    /// A one-time code in two halves: "123 456".
    static func groupedOneTimeCode(_ code: String) -> String {
        let half = code.count / 2
        guard half > 0 else { return code }
        return String(code.prefix(half)) + " " + String(code.dropFirst(half))
    }

    /// The recovery key in rows of four groups: the 13 groups it is written in
    /// do not fit a phone's width on one line.
    static func recoveryKeyLines(_ recoveryKeyText: String) -> [String] {
        let groups = recoveryKeyText.split(separator: " ").map(String.init)
        var lines: [String] = []
        var index = 0
        while index < groups.count {
            lines.append(groups[index..<min(index + 4, groups.count)].joined(separator: " "))
            index += 4
        }
        return lines
    }

    /// The address to open for a login's site; a site written without a scheme is taken as https.
    static func url(ofSite site: String) -> URL? {
        let trimmed = site.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        let written = trimmed.contains("://") ? trimmed : "https://" + trimmed
        guard let url = URL(string: written), let scheme = url.scheme?.lowercased(), scheme == "https" || scheme == "http" else {
            return nil
        }
        return url
    }

    /// A site shown without its scheme: "mail.example.com".
    static func shownSite(_ site: String) -> String {
        var text = site.trimmingCharacters(in: .whitespacesAndNewlines)
        for prefix in ["https://", "http://"] where text.lowercased().hasPrefix(prefix) {
            text = String(text.dropFirst(prefix.count))
        }
        if text.hasSuffix("/") { text.removeLast() }
        return text
    }
}

/// PasswordsClipboard copies a secret so it does not stay: it is local to this
/// iPhone (Universal Clipboard does not carry it to other devices) and is
/// removed after a minute and a half.
enum PasswordsClipboard {
    static let secondsKept: TimeInterval = 90

    @MainActor
    static func copy(_ text: String) {
        UIPasteboard.general.setItems(
            [[UTType.plainText.identifier: text]],
            options: [.localOnly: true, .expirationDate: Date().addingTimeInterval(secondsKept)])
    }
}
