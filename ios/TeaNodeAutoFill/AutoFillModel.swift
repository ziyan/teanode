import Combine
import Foundation
import TeaNodeKit

// What the AutoFill extension knows and does. It reads the phone's copy of
// Passwords and the account key behind Face ID, never the network, and it
// never derives the passphrase key. The one exception is saving a passkey the
// person has just made: that needs the server, so the model opens a
// PasswordsSession with the signed-in connection, saves the login, and only
// then hands the passkey to the website.

/// AutoFillUnlockOutcome is how an attempt to open Passwords ended.
private enum AutoFillUnlockOutcome {
    case unlocked
    /// Face ID was cancelled or failed; `problemText` is set for a failure.
    case notGiven(problemText: String?)
    case needsApp
}

@MainActor
final class AutoFillModel: ObservableObject {
    @Published private(set) var screen = AutoFillScreen.starting
    @Published var searchText = ""
    @Published var problem: AutoFillProblem?
    /// Why Face ID did not open Passwords, shown on the locked screen; nil when the person cancelled it.
    @Published private(set) var lockedProblemText: String?
    @Published private(set) var isWorking = false
    /// Whether the app has made a copy of Passwords on this phone, for the configuration screen.
    @Published private(set) var hasSetUpPasswords = false
    @Published private(set) var logins: [AutoFillLoginSummary] = []

    var completion = AutoFillCompletion.ignoring

    private var request: AutoFillRequest?
    private var listing = AutoFillListing(requestedSites: [], passkeyAssertion: nil, isOneTimeCode: false)
    private var signedInAccount: Account?
    private var cacheFileURL: URL?
    private var cacheContents: PasswordsCacheContents?
    private var opener: PasswordsOpener?
    private var accountKey: Data?

    // MARK: Starting

    func start(_ newRequest: AutoFillRequest) {
        request = newRequest
        switch newRequest {
        case .configuration:
            hasSetUpPasswords = Self.hasCopyOfPasswords()
            screen = .configuration
        case .registerPasskey(let registration):
            screen = .registration(registration)
        case .list(let newListing):
            listing = newListing
            screen = .starting
            Task { await unlockAndContinue() }
        case .providePassword, .providePasskey:
            screen = .starting
            Task { await unlockAndContinue() }
        }
    }

    /// The "Unlock with Face ID" button.
    func unlockTapped() {
        guard !isWorking else { return }
        screen = .starting
        Task { await unlockAndContinue() }
    }

    func cancel() {
        completion.cancel(.userCanceled)
    }

    func finishConfiguration() {
        completion.finishConfiguration()
    }

    /// Whether the app has made a copy of Passwords for the signed-in account
    /// on this phone. Only looks for the file; nothing is read.
    private static func hasCopyOfPasswords() -> Bool {
        guard let account = try? KeychainAccountStore().load(),
            let fileURL = AutoFillLocation.cacheFile(signInAccountKey: account.accountKey)
        else { return false }
        return FileManager.default.fileExists(atPath: fileURL.path)
    }

    private func unlockAndContinue() async {
        switch await unlock() {
        case .unlocked:
            lockedProblemText = nil
            await continueRequest()
        case .notGiven(let problemText):
            lockedProblemText = problemText
            screen = .locked
        case .needsApp:
            screen = .needsApp
        }
    }

    private func continueRequest() async {
        guard let request else { return }
        switch request {
        case .list:
            screen = .list
        case .providePassword(let recordId, let userName, let requestedSites):
            guard let login = loginToFill(recordId: recordId, userName: userName, requestedSites: requestedSites) else {
                completion.cancel(.credentialIdentityNotFound)
                return
            }
            await fill(loginId: login.id, kind: .password, isFromList: false)
        case .providePasskey(let assertion, let recordId, let credentialId):
            guard let login = loginWithPasskey(recordId: recordId, credentialId: credentialId) else {
                completion.cancel(.credentialIdentityNotFound)
                return
            }
            listing.passkeyAssertion = assertion
            await fill(loginId: login.id, kind: .passkey(credentialId: credentialId), isFromList: false)
        case .registerPasskey, .configuration:
            break
        }
    }

    private func loginToFill(recordId: String?, userName: String, requestedSites: [String]) -> AutoFillLoginSummary? {
        if let recordId, let byRecord = logins.first(where: { $0.id == recordId && $0.hasPassword }) { return byRecord }
        return logins.first { login in
            login.hasPassword && login.userName == userName
                && requestedSites.contains { PasswordsSiteMatch.isRelatedOrBetter(sites: login.sites, requested: $0) }
        }
    }

    private func loginWithPasskey(recordId: String?, credentialId: Data) -> AutoFillLoginSummary? {
        if let recordId, let byRecord = logins.first(where: { $0.id == recordId && $0.passkeys.contains { $0.credentialId == credentialId } }) {
            return byRecord
        }
        return logins.first { login in login.passkeys.contains { $0.credentialId == credentialId } }
    }

    // MARK: Opening Passwords

    /// Reads the phone's copy and the account key (Face ID), and works out
    /// the summaries. Does nothing when it has been done.
    private func unlock() async -> AutoFillUnlockOutcome {
        if opener != nil { return .unlocked }
        guard let account = try? KeychainAccountStore().load(),
            let fileURL = AutoFillLocation.cacheFile(signInAccountKey: account.accountKey)
        else { return .needsApp }
        let cache = PasswordsCache(fileURL: fileURL)
        let loadedContents = await Task.detached(priority: .userInitiated) { try? cache.load() }.value
        guard let contents = loadedContents else { return .needsApp }

        let promptReason = String(localized: "Fill your logins from TeaNode Passwords.")
        let keptKey: Data?
        do {
            keptKey = try await PasswordsKeychainKeyStore().load(signInAccountKey: account.accountKey, promptReason: promptReason)
        } catch {
            if let keyStoreError = error as? PasswordsKeyStoreError, keyStoreError == .canceled { return .notGiven(problemText: nil) }
            return .notGiven(problemText: String(localized: "Face ID did not unlock Passwords. Try again."))
        }
        guard let keptKey else { return .needsApp }
        let newOpener: PasswordsOpener
        do {
            newOpener = try PasswordsOpener(accountKey: keptKey, contents: contents)
        } catch {
            return .needsApp
        }
        let views = contents.items
        let summaries = await Task.detached(priority: .userInitiated) {
            AutoFillIndex.loginSummaries(views: views, opener: newOpener)
        }.value
        signedInAccount = account
        cacheFileURL = fileURL
        cacheContents = contents
        opener = newOpener
        accountKey = keptKey
        logins = summaries
        return .unlocked
    }

    // MARK: The list

    var sections: [AutoFillSection] {
        AutoFillIndex.sections(logins: logins, listing: listing, searchText: searchText)
    }

    var emptyListTitle: String {
        listing.isOneTimeCode ? String(localized: "No one-time codes yet") : String(localized: "No logins yet")
    }

    var emptyListDescription: String {
        listing.isOneTimeCode
            ? String(localized: "Add a one-time code to a login in TeaNode Passwords and it shows up here.")
            : String(localized: "Logins you save in TeaNode Passwords show up here.")
    }

    /// The person chose a row.
    func select(_ row: AutoFillRow) {
        guard !isWorking else { return }
        isWorking = true
        Task {
            await fill(loginId: row.loginId, kind: row.kind, isFromList: true)
            isWorking = false
        }
    }

    // MARK: Filling

    /// Opens the one login and hands what the request wants to the system.
    /// From the list a failure is told to the person; when the system picked
    /// the login there is no list to go back to, so the request ends.
    private func fill(loginId: String, kind: AutoFillRowKind, isFromList: Bool) async {
        do {
            let login = try openLogin(id: loginId)
            switch kind {
            case .password:
                guard !login.password.isEmpty else { throw AutoFillError.nothingToFill }
                completion.fillPassword(login.userName, login.password)
            case .oneTimeCode:
                let settings = try PasswordsOneTimeCode.parse(uri: login.oneTimeCodeUri)
                completion.fillOneTimeCode(PasswordsOneTimeCode.code(settings: settings, date: Date()))
            case .passkey(let credentialId):
                guard let assertion = listing.passkeyAssertion else { throw AutoFillError.nothingToFill }
                completion.fillPasskeyAssertion(try sign(login: login, assertion: assertion, credentialId: credentialId))
            }
        } catch {
            if isFromList {
                problem = AutoFillProblem(
                    title: String(localized: "Could not fill this login"),
                    message: String(localized: "TeaNode could not read this item. Open TeaNode, unlock Passwords, and try again."))
            } else {
                completion.cancel(.failed)
            }
        }
    }

    private func openLogin(id: String) throws -> PasswordsLoginFields {
        guard let opener, let view = cacheContents?.item(id: id) else { throw AutoFillError.itemNotFound }
        guard case .login(let login) = try opener.open(view).fields else { throw AutoFillError.itemNotFound }
        return login
    }

    /// Signs the website's challenge with the passkey. The person has just
    /// been verified by Face ID, so the authenticator data says so.
    private func sign(login: PasswordsLoginFields, assertion: AutoFillPasskeyAssertion, credentialId: Data) throws
        -> AutoFillSignedAssertion
    {
        guard
            let passkey = login.passkeys.first(where: {
                $0.relyingPartyId == assertion.relyingPartyId && AutoFillBase64URL.decode($0.credentialId) == credentialId
            })
        else { throw AutoFillError.relyingPartyDoesNotMatch }
        guard let privateKey = Data(base64Encoded: passkey.privateKey), let userHandle = AutoFillBase64URL.decode(passkey.userHandle) else {
            throw AutoFillError.unreadablePasskey
        }
        let authenticatorData = PasswordsPasskey.assertionAuthenticatorData(relyingPartyId: assertion.relyingPartyId, isVerified: true)
        let signature = try PasswordsPasskey.sign(
            privateKey: privateKey, authenticatorData: authenticatorData, clientDataHash: assertion.clientDataHash)
        return AutoFillSignedAssertion(
            userHandle: userHandle, relyingPartyId: assertion.relyingPartyId, signature: signature,
            clientDataHash: assertion.clientDataHash, authenticatorData: authenticatorData, credentialId: credentialId)
    }

    // MARK: Saving a new passkey

    /// The "Save passkey" button.
    func savePasskey() {
        guard case .registration(let registration) = screen, !isWorking else { return }
        isWorking = true
        Task {
            await saveNewPasskey(registration)
            isWorking = false
        }
    }

    private func saveNewPasskey(_ registration: AutoFillPasskeyRegistration) async {
        switch await unlock() {
        case .unlocked:
            break
        case .notGiven(let problemText):
            if let problemText {
                problem = AutoFillProblem(title: String(localized: "Could not save the passkey"), message: problemText)
            }
            return
        case .needsApp:
            screen = .needsApp
            return
        }
        guard let signedInAccount, let cacheFileURL, let accountKey, let userId = cacheContents?.userId else {
            screen = .needsApp
            return
        }
        do {
            let created = try PasswordsPasskey.register(relyingPartyId: registration.relyingPartyId, isVerified: true)
            let passkey = PasswordsPasskeyFields(
                credentialId: AutoFillBase64URL.encode(created.credentialId), relyingPartyId: registration.relyingPartyId,
                userHandle: AutoFillBase64URL.encode(registration.userHandle), userName: registration.userName,
                userDisplayName: registration.userName, privateKey: created.privateKey.base64EncodedString(),
                createdAt: ISO8601DateFormatter().string(from: Date()))
            let connection = Connection(account: signedInAccount, transport: URLSessionTransport(), store: KeychainAccountStore())
            let session = PasswordsSession(api: connection, cache: PasswordsCache(fileURL: cacheFileURL), userId: userId)
            try await session.open(accountKey: accountKey)
            try await keep(passkey, for: registration, in: session)
            // Only now, with the passkey on the server, does the website get it.
            completion.fillPasskeyRegistration(
                AutoFillCreatedPasskey(
                    relyingPartyId: registration.relyingPartyId, clientDataHash: registration.clientDataHash,
                    credentialId: created.credentialId, attestationObject: created.attestationObject))
        } catch {
            problem = AutoFillProblem(title: String(localized: "Could not save the passkey"), message: Self.savingProblemText(for: error))
        }
    }

    /// Puts the passkey into the person's login for this website and user
    /// name, replacing an older passkey of the same user there, or into a new
    /// login when there is none.
    private func keep(_ passkey: PasswordsPasskeyFields, for registration: AutoFillPasskeyRegistration, in session: PasswordsSession)
        async throws
    {
        let existing = logins.first { login in
            !login.isSharedWithMe && login.userName.caseInsensitiveCompare(registration.userName) == .orderedSame
                && PasswordsSiteMatch.isMatch(sites: login.sites, requested: registration.relyingPartyId)
        }
        if let existing {
            let item = try await session.item(id: existing.id)
            guard case .login(var login) = item.fields else { throw AutoFillError.itemNotFound }
            login.passkeys.removeAll { $0.relyingPartyId == passkey.relyingPartyId && $0.userHandle == passkey.userHandle }
            login.passkeys.append(passkey)
            _ = try await session.save(fields: .login(login), id: item.id, baseRevision: item.revision)
        } else {
            let login = PasswordsLoginFields(
                name: registration.relyingPartyId, userName: registration.userName, sites: ["https://" + registration.relyingPartyId],
                passkeys: [passkey])
            _ = try await session.save(fields: .login(login))
        }
    }

    private static func savingProblemText(for error: Error) -> String {
        if let passwordsError = error as? PasswordsError {
            switch passwordsError {
            case .itemChangedOnAnotherDevice:
                return String(localized: "This login changed on another device. Open TeaNode to refresh it, then try again.")
            case .needsPassphrase, .notSetUp, .locked:
                return String(localized: "Open TeaNode and unlock Passwords once, then try again.")
            default:
                break
            }
        }
        return String(localized: "TeaNode could not save the passkey, so it was not created. Check your connection and try again.")
    }
}
