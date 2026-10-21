import Combine
import Foundation
import TeaNodeKit

// What the AutoFill extension knows and does. It reads the header of the
// phone's copy of Passwords, the summary of the logins the app wrote and the
// account key behind Face ID, never the network, and it never derives the
// passphrase key. It does not read the items to draw a list (under its memory
// limit a copy full of large shared items could not be read): the one login a
// request picks is read from its own file and opened. The one exception to
// the network is saving a passkey the person has just made: that needs the
// server, so the model opens a PasswordsSession that loads items on demand
// with the signed-in connection, saves the login, and only then hands the
// passkey to the website.
//
// What Face ID opened is kept only for the request it was given for: the
// account key, the opener and the summaries are dropped when the request
// ends (completed, cancelled, or replaced by the next one), so a request
// never runs on the Face ID of an earlier one.

/// AutoFillUnlockOutcome is how an attempt to open Passwords ended.
private enum AutoFillUnlockOutcome {
    case unlocked
    /// Face ID was cancelled or failed; `problemText` is set for a failure.
    case notGiven(problemText: String?)
    case needsApp
    /// The request ended or was replaced while Face ID was open; nothing was kept.
    case superseded
}

@MainActor
final class AutoFillModel: ObservableObject {
    @Published private(set) var autoFillScreen = AutoFillScreen.starting
    @Published var searchText = "" {
        didSet { refreshSections() }
    }
    @Published var problemMessage: AutoFillProblem?
    /// Why Face ID did not open Passwords, shown on the locked screen; nil when the person cancelled it.
    @Published private(set) var lockedProblemText: String?
    @Published private(set) var isWorking = false
    /// Whether the app has made a copy of Passwords on this phone, for the configuration screen.
    @Published private(set) var hasSetUpPasswords = false
    @Published private(set) var logins: [AutoFillLoginSummary] = []
    /// The list as it is drawn: worked out when the logins, the request or the search words change, not on each redraw.
    @Published private(set) var sections: [AutoFillSection] = []
    /// Set when the person chose a login saved for another site than the one asking: nothing is filled until they say yes.
    @Published var siteQuestion: AutoFillSiteQuestion?

    var autoFillCompletion = AutoFillCompletion.ignoring

    private var request: AutoFillRequest?
    private var autoFillListing = AutoFillListing(requestedSites: [], passkeyAssertion: nil, isOneTimeCode: false) {
        didSet { siteLevels = [:] }
    }
    /// How well each login fits the sites asking, by item id (`AutoFillIndex.levels`), worked out once per request.
    private var siteLevels: [String: PasswordsSiteMatchLevel] = [:]
    private var signedInAccount: Account?
    private var cache: PasswordsCache?
    /// The header of the copy (who it is for and the key pair); it holds none of the items.
    private var cacheHeader: PasswordsCacheContents?
    private var opener: PasswordsOpener?
    private var accountKey: Data?
    /// Counts the requests and the endings of requests. An `await` in the middle
    /// of a request (Face ID, reading the copy, opening the items) can come
    /// back after the person cancelled or the next request started; the code
    /// that resumes compares this number with the one it started with
    /// (`isStillCurrent`) and stops, so it neither refills what was forgotten
    /// nor ends the request a second time.
    private var requestGeneration = 0

    // MARK: Starting

    func start(_ newRequest: AutoFillRequest) {
        // Nothing opened for an earlier request is used for this one: it asks for Face ID itself.
        forgetOpenedPasswords()
        searchText = ""
        problemMessage = nil
        siteQuestion = nil
        lockedProblemText = nil
        isWorking = false
        request = newRequest
        switch newRequest {
        case .configuration:
            hasSetUpPasswords = Self.hasCopyOfPasswords()
            autoFillScreen = .configuration
        case .registerPasskey(let registration):
            autoFillScreen = .registration(registration)
        case .list(let newListing):
            autoFillListing = newListing
            sections = []
            autoFillScreen = .starting
            Task { await unlockAndContinue() }
        case .providePassword, .providePasskey:
            autoFillScreen = .starting
            Task { await unlockAndContinue() }
        }
    }

    /// The "Unlock with Face ID" button.
    func unlockTapped() {
        guard !isWorking else { return }
        autoFillScreen = .starting
        Task { await unlockAndContinue() }
    }

    func cancel() {
        complete { $0.cancel(.userCanceled) }
    }

    func finishConfiguration() {
        complete { $0.finishConfiguration() }
    }

    /// Ends the request with one call on the completion, then forgets what was opened for it.
    private func complete(_ ending: (AutoFillCompletion) -> Void) {
        ending(autoFillCompletion)
        forgetOpenedPasswords()
        // The sheet is going away: not an empty list in its last moment.
        autoFillScreen = .starting
    }

    /// The sheet went away: forget what Face ID opened, whether or not the
    /// request was completed, and make any request still waiting stop.
    func sheetDidDisappear() {
        forgetOpenedPasswords()
    }

    /// Whether the request that was running when `generation` was read is still the one running.
    private func isStillCurrent(_ generation: Int) -> Bool {
        generation == requestGeneration
    }

    /// Drops the account key, the opener, the copy of the items and the
    /// summaries, and ends every request still waiting on an `await`.
    private func forgetOpenedPasswords() {
        requestGeneration += 1
        siteQuestion = nil
        opener = nil
        accountKey = nil
        cacheHeader = nil
        cache = nil
        signedInAccount = nil
        logins = []
        siteLevels = [:]
        sections = []
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
        let generation = requestGeneration
        switch await unlock() {
        case .superseded:
            return
        case .unlocked:
            lockedProblemText = nil
            await continueRequest(generation: generation)
        case .notGiven(let problemText):
            lockedProblemText = problemText
            autoFillScreen = .locked
        case .needsApp:
            autoFillScreen = .needsApp
        }
    }

    private func continueRequest(generation: Int) async {
        guard let request else { return }
        switch request {
        case .list:
            let isLoaded = await loadLoginSummaries()
            guard isStillCurrent(generation) else { return }
            autoFillScreen = isLoaded ? .list : .needsApp
        case .providePassword(let itemId, let userName, let requestedSites):
            let foundLogin = await loginToFill(itemId: itemId, userName: userName, requestedSites: requestedSites)
            guard isStillCurrent(generation) else { return }
            guard let login = foundLogin else {
                complete { $0.cancel(.credentialIdentityNotFound) }
                return
            }
            fill(login: login, rowKind: .password, isFromList: false)
        case .providePasskey(let assertion, let itemId, let credentialId):
            let foundLogin = await loginWithPasskey(itemId: itemId, credentialId: credentialId)
            guard isStillCurrent(generation) else { return }
            guard let login = foundLogin else {
                complete { $0.cancel(.credentialIdentityNotFound) }
                return
            }
            autoFillListing.passkeyAssertion = assertion
            fill(login: login, rowKind: .passkey(credentialId: credentialId), isFromList: false)
        case .registerPasskey, .configuration:
            break
        }
    }

    /// The login the system picked, opened. With the item id the app gave the
    /// system only that item is opened, and it must still be for the page
    /// (the exact origin when the system named a full address, the host when it
    /// named a domain): a suggestion made before the login's sites changed is
    /// not filled into a site it is no longer for. Without an id, the logins
    /// are searched by user name and site.
    private func loginToFill(itemId: String?, userName: String, requestedSites: [AutoFillRequestedSite]) async -> PasswordsLoginFields? {
        let login: PasswordsLoginFields?
        if let itemId {
            login = try? openLogin(id: itemId)
        } else {
            guard await loadLoginSummaries() else { return nil }
            let found = logins.first { summary in
                summary.hasPassword && summary.userName == userName && Self.isLogin(sites: summary.sites, for: requestedSites)
            }
            login = found.flatMap { try? openLogin(id: $0.id) }
        }
        guard let login, !login.password.isEmpty, Self.isLogin(sites: login.sites, for: requestedSites) else { return nil }
        return login
    }

    /// Whether a login's sites are for the sites the system named. Nothing named (an app) leaves the system's pick as it is.
    private static func isLogin(sites: [String], for requestedSites: [AutoFillRequestedSite]) -> Bool {
        requestedSites.isEmpty
            || requestedSites.contains {
                PasswordsSiteMatch.level(sites: sites, requested: $0.address, isPageAddress: $0.isPageAddress) == .exactHost
            }
    }

    /// The login that holds the passkey the system picked: with the item id only that item is opened.
    private func loginWithPasskey(itemId: String?, credentialId: Data) async -> PasswordsLoginFields? {
        let login: PasswordsLoginFields?
        if let itemId {
            login = try? openLogin(id: itemId)
        } else {
            guard await loadLoginSummaries() else { return nil }
            let found = logins.first { summary in summary.passkeys.contains { $0.credentialId == credentialId } }
            login = found.flatMap { try? openLogin(id: $0.id) }
        }
        guard let login, login.passkeys.contains(where: { AutoFillBase64URL.decode($0.credentialId) == credentialId }) else { return nil }
        return login
    }

    // MARK: Opening Passwords

    /// Reads the header of the phone's copy and the account key (Face ID). Does nothing when
    /// it has been done for this request. It reads no item: the list comes from the summary the
    /// app wrote (`loadLoginSummaries`), and the one login a request picks is read and opened
    /// when it is needed.
    private func unlock() async -> AutoFillUnlockOutcome {
        if opener != nil { return .unlocked }
        let generation = requestGeneration
        guard let account = try? KeychainAccountStore().load(),
            let fileURL = AutoFillLocation.cacheFile(signInAccountKey: account.accountKey)
        else { return .needsApp }
        // The keys pinned for other people are in the keychain group of Passwords, not in the shared folder.
        let cache = PasswordsCache.forThisBundle(fileURL: fileURL, signInAccountKey: account.accountKey)
        let loadedHeader = await Task.detached(priority: .userInitiated) { try? cache.loadHeader() }.value
        guard isStillCurrent(generation) else { return .superseded }
        guard let header = loadedHeader else { return .needsApp }

        let promptReason = String(localized: "Fill your logins from TeaNode Passwords.")
        let keptKey: Data?
        do {
            keptKey = try await PasswordsKeychainKeyStore.forThisBundle().load(signInAccountKey: account.accountKey, promptReason: promptReason)
        } catch {
            guard isStillCurrent(generation) else { return .superseded }
            if let keyStoreError = error as? PasswordsKeyStoreError, keyStoreError == .canceled { return .notGiven(problemText: nil) }
            return .notGiven(problemText: String(localized: "Face ID did not unlock Passwords. Try again."))
        }
        // The request ended while Face ID was open: keep nothing it opened.
        guard isStillCurrent(generation) else { return .superseded }
        guard let keptKey else { return .needsApp }
        let newOpener: PasswordsOpener
        do {
            newOpener = try PasswordsOpener(accountKey: keptKey, contents: header)
        } catch {
            return .needsApp
        }
        signedInAccount = account
        self.cache = cache
        cacheHeader = header
        opener = newOpener
        accountKey = keptKey
        return .unlocked
    }

    /// Reads the summary of the logins the app wrote (sealed with the account key), for the list and
    /// for a search by user name. Does nothing when it has been done. False when there is no summary
    /// that opens (the app has not written one for this copy yet): the person is asked to open the app.
    private func loadLoginSummaries() async -> Bool {
        if !logins.isEmpty { return true }
        guard let cache, let accountKey, let userId = cacheHeader?.userId else { return false }
        let generation = requestGeneration
        let loadedSummary = await Task.detached(priority: .userInitiated) { () -> PasswordsAutoFillSummary? in
            guard let data = try? cache.loadSummaryData() else { return nil }
            return try? PasswordsAutoFillSummary.open(data, accountKey: accountKey, userId: userId)
        }.value
        // A summary of a request that has ended is not kept.
        guard isStillCurrent(generation) else { return false }
        guard let loadedSummary else { return false }
        logins = AutoFillIndex.loginSummaries(of: loadedSummary)
        siteLevels = AutoFillIndex.levels(logins: logins, listing: autoFillListing)
        refreshSections()
        return true
    }

    // MARK: The list

    /// Works out the sections of the list again, from the levels worked out once for the request.
    private func refreshSections() {
        sections = AutoFillIndex.sections(logins: logins, levels: siteLevels, listing: autoFillListing, searchText: searchText)
    }

    var emptyListTitle: String {
        autoFillListing.isOneTimeCode ? String(localized: "No one-time codes yet") : String(localized: "No logins yet")
    }

    var emptyListDescription: String {
        autoFillListing.isOneTimeCode
            ? String(localized: "Add a one-time code to a login in TeaNode Passwords and it shows up here.")
            : String(localized: "Logins you save in TeaNode Passwords show up here.")
    }

    /// The person chose a row. A password or a one-time code of a login that is not saved for the exact
    /// site asking (one from "Related sites", "All logins" or a search) is filled only after they confirm.
    func select(_ row: AutoFillRow) {
        guard !isWorking else { return }
        if let question = question(askedBeforeFilling: row) {
            siteQuestion = question
            return
        }
        fillChosen(row)
    }

    /// The person said yes to filling the login into the site that asked.
    func confirmSiteQuestion(_ question: AutoFillSiteQuestion) {
        siteQuestion = nil
        guard !isWorking else { return }
        fillChosen(question.row)
    }

    private func question(askedBeforeFilling row: AutoFillRow) -> AutoFillSiteQuestion? {
        switch row.rowKind {
        case .passkey:
            // A passkey row is already for the website that asked.
            return nil
        case .password, .oneTimeCode:
            break
        }
        guard let login = logins.first(where: { $0.id == row.itemId }),
            AutoFillIndex.isSiteMismatch(level: siteLevels[login.id] ?? PasswordsSiteMatchLevel.none, listing: autoFillListing),
            let requestedSite = AutoFillIndex.requestedSiteText(listing: autoFillListing)
        else { return nil }
        let savedSites = AutoFillIndex.savedSitesText(login: login)
        let message =
            savedSites.isEmpty
            ? String(localized: "It is not saved for any site.") : String(localized: "It is saved for \(savedSites).")
        return AutoFillSiteQuestion(
            row: row, title: String(localized: "Fill \(login.displayName) into \(requestedSite)?"), message: message)
    }

    private func fillChosen(_ row: AutoFillRow) {
        isWorking = true
        Task {
            do {
                fill(login: try openLogin(id: row.itemId), rowKind: row.rowKind, isFromList: true)
            } catch {
                tellFillFailed()
            }
            isWorking = false
        }
    }

    // MARK: Filling

    /// Hands what the request wants from the one login to the system. From the
    /// list a failure is told to the person; when the system picked the login
    /// there is no list to go back to, so the request ends.
    private func fill(login: PasswordsLoginFields, rowKind: AutoFillRowKind, isFromList: Bool) {
        do {
            switch rowKind {
            case .password:
                guard !login.password.isEmpty else { throw AutoFillError.nothingToFill }
                complete { $0.fillPassword(login.userName, login.password) }
            case .oneTimeCode:
                let settings = try PasswordsOneTimeCode.parse(uri: login.oneTimeCodeUri)
                let code = PasswordsOneTimeCode.code(settings: settings, date: Date())
                complete { $0.fillOneTimeCode(code) }
            case .passkey(let credentialId):
                guard let assertion = autoFillListing.passkeyAssertion else { throw AutoFillError.nothingToFill }
                let signed = try sign(login: login, assertion: assertion, credentialId: credentialId)
                complete { $0.fillPasskeyAssertion(signed) }
            }
        } catch {
            if isFromList {
                tellFillFailed()
            } else {
                complete { $0.cancel(.failed) }
            }
        }
    }

    private func tellFillFailed() {
        problemMessage = AutoFillProblem(
            title: String(localized: "Could not fill this login"),
            message: String(localized: "TeaNode could not read this item. Open TeaNode, unlock Passwords, and try again."))
    }

    /// Reads one login from its own file in the copy and opens it. A deleted item, a note or a card is not one.
    private func openLogin(id: String) throws -> PasswordsLoginFields {
        guard let opener, let view = try cache?.loadItem(id: id), view.deletedAt == nil else { throw AutoFillError.itemNotFound }
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
        guard case .registration(let registration) = autoFillScreen, !isWorking else { return }
        isWorking = true
        let generation = requestGeneration
        Task {
            await saveNewPasskey(registration)
            if isStillCurrent(generation) { isWorking = false }
        }
    }

    private func saveNewPasskey(_ registration: AutoFillPasskeyRegistration) async {
        let generation = requestGeneration
        switch await unlock() {
        case .superseded:
            return
        case .unlocked:
            break
        case .notGiven(let problemText):
            if let problemText {
                problemMessage = AutoFillProblem(title: String(localized: "Could not save the passkey"), message: problemText)
            }
            return
        case .needsApp:
            autoFillScreen = .needsApp
            return
        }
        guard await loadLoginSummaries() else {
            guard isStillCurrent(generation) else { return }
            autoFillScreen = .needsApp
            return
        }
        guard isStillCurrent(generation) else { return }
        guard let signedInAccount, let cache, let accountKey, let userId = cacheHeader?.userId else {
            autoFillScreen = .needsApp
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
            // Only the logins this saves into are read, one file each; the app does the syncing.
            let session = PasswordsSession(api: connection, cache: cache, userId: userId, isLoadingItemsOnDemand: true)
            try await session.open(accountKey: accountKey)
            try await keep(passkey, for: registration, in: session)
            // The request ended while the passkey was being saved: the website is not waiting any more.
            guard isStillCurrent(generation) else { return }
            // Only now, with the passkey on the server, does the website get it.
            let createdPasskey = AutoFillCreatedPasskey(
                relyingPartyId: registration.relyingPartyId, clientDataHash: registration.clientDataHash,
                credentialId: created.credentialId, attestationObject: created.attestationObject)
            complete { $0.fillPasskeyRegistration(createdPasskey) }
        } catch {
            guard isStillCurrent(generation) else { return }
            problemMessage = AutoFillProblem(title: String(localized: "Could not save the passkey"), message: Self.savingProblemText(for: error))
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
            // An item the server marked has a key somebody who lost access may hold: the app gives it a new one first.
            guard !item.needsNewKey else { throw AutoFillError.loginNeedsNewKey }
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
        if let autoFillError = error as? AutoFillError, autoFillError == .loginNeedsNewKey {
            return String(localized: "Open Passwords in TeaNode first: this login needs a new key")
        }
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
