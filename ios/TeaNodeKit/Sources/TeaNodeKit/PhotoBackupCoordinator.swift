import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

// A pass of the photo backup, behind small protocols so that everything but
// PhotoKit and the background URLSession runs, and is tested, without a phone:
// the app target supplies the library and the transfer, this file decides what
// to read, what to send, how much to have in flight, and what a server answer
// means.

// MARK: The library

/// PhotoBackupAccess is how much of the photo library the person let the app
/// read. Backup needs all of it.
public enum PhotoBackupAccess: Equatable, Sendable {
    case notDetermined
    case denied
    /// The person chose which photos the app sees.
    case limited
    case full
}

/// PhotoBackupListing is the library read from the start, minus what the index
/// already has.
public struct PhotoBackupListing: Sendable {
    public let items: [PhotoBackupItem]
    /// How many items the library holds, listed or not.
    public let totalCount: Int
    /// The library's change token as it was before the listing began, archived,
    /// so that nothing changed during the listing is missed.
    public let changeToken: Data?

    public init(items: [PhotoBackupItem], totalCount: Int, changeToken: Data?) {
        self.items = items
        self.totalCount = totalCount
        self.changeToken = changeToken
    }
}

/// PhotoBackupChanges is what the library gained or changed since a token.
public struct PhotoBackupChanges: Sendable {
    /// Items added or changed, read as they are now. Deleted items are not
    /// here: deleting on the phone leaves the server's copy alone.
    public let items: [PhotoBackupItem]
    public let totalCount: Int
    public let changeToken: Data?

    public init(items: [PhotoBackupItem], totalCount: Int, changeToken: Data?) {
        self.items = items
        self.totalCount = totalCount
        self.changeToken = changeToken
    }
}

/// PhotoBackupLibraryError is what the library says when it cannot do as asked.
public enum PhotoBackupLibraryError: Error, Equatable, Sendable {
    /// The saved token is too old to list changes from; list everything again.
    case changeTokenExpired
    /// The file is not there to read right now (iCloud cannot be reached, the
    /// phone is locked); try again later.
    case unavailable(PhotoBackupFailure)
    /// The file cannot be read at all.
    case failed(PhotoBackupFailure)
}

/// PhotoBackupExportedFile is a file written for an upload.
public struct PhotoBackupExportedFile: Equatable, Sendable {
    /// The name of the file in the outgoing directory.
    public let fileName: String
    public let sizeBytes: Int64

    public init(fileName: String, sizeBytes: Int64) {
        self.fileName = fileName
        self.sizeBytes = sizeBytes
    }
}

/// PhotoBackupLibrary is the phone's photo library as backup reads it.
public protocol PhotoBackupLibrary: Sendable {
    func access() -> PhotoBackupAccess
    /// Every item except those whose local identifier is in `known`, sorted by
    /// creation date, oldest first.
    func listItems(skipping known: Set<String>, includesHidden: Bool) async throws -> PhotoBackupListing
    /// The items with these identifiers, as they are now. One that is no longer
    /// in the library is left out.
    func items(withLocalIdentifiers localIdentifiers: [String], includesHidden: Bool) async throws -> [PhotoBackupItem]
    /// Throws `PhotoBackupLibraryError.changeTokenExpired` when the token is too old.
    func changes(since token: Data, includesHidden: Bool) async throws -> PhotoBackupChanges
    /// Writes the file of one upload into `directory` under a name of its own,
    /// downloading it from iCloud if need be. On a network that costs (mobile
    /// data, a personal hotspot) it downloads only if `mayUseExpensiveNetwork`,
    /// and otherwise throws `PhotoBackupLibraryError.unavailable`. The file is
    /// found again in the library as the person's setting for the Hidden album
    /// says (`includesHidden`): a hidden photo is not exported unless allowed.
    func export(_ upload: PhotoBackupUpload, to directory: URL, mayUseExpensiveNetwork: Bool, includesHidden: Bool) async throws -> PhotoBackupExportedFile
}

// MARK: The transfer

/// PhotoBackupJob is what the background session keeps with each of its tasks,
/// so that a task finished while the app was not running can be matched to the
/// item it was for.
public struct PhotoBackupJob: Codable, Equatable, Sendable {
    public let accountKey: String
    public let localIdentifier: String
    public let role: PhotoBackupFileRole
    public let clientPhotoId: String
    public let filename: String
    public let assetModificationDate: Date?
    /// The file to send, by its name in the outgoing directory.
    public let fileName: String
    public let sizeBytes: Int64

    public var key: String { PhotoBackupUpload.key(localIdentifier: localIdentifier, role: role) }

    public init(
        accountKey: String, localIdentifier: String, role: PhotoBackupFileRole, clientPhotoId: String, filename: String,
        assetModificationDate: Date?, fileName: String, sizeBytes: Int64
    ) {
        self.accountKey = accountKey
        self.localIdentifier = localIdentifier
        self.role = role
        self.clientPhotoId = clientPhotoId
        self.filename = filename
        self.assetModificationDate = assetModificationDate
        self.fileName = fileName
        self.sizeBytes = sizeBytes
    }
}

/// PhotoBackupTransfer is the background URLSession that sends the files.
public protocol PhotoBackupTransfer: Sendable {
    /// Queues the file. It is sent whenever the system allows, even with the
    /// app closed.
    func start(request: URLRequest, job: PhotoBackupJob, fileURL: URL) async throws
    /// The uploads the session still holds, unfinished.
    func queuedJobs() async -> [PhotoBackupJob]
    func cancelAll() async
}

/// PhotoBackupServerLookup asks the server which photos it already has.
public protocol PhotoBackupServerLookup: Sendable {
    func findPhotos(clientPhotoIds: [String]) async throws -> [PhotoBackupServerPhoto]
}

/// PhotoBackupNetwork says whether the phone is on a network that costs: mobile
/// data or a personal hotspot.
public protocol PhotoBackupNetwork: Sendable {
    func isExpensive() async -> Bool
}

// MARK: How much is in flight

/// PhotoBackupBudget bounds the uploads queued at once, and the temporary files
/// they are read from.
public struct PhotoBackupBudget: Equatable, Sendable {
    public var maximumUploadCount: Int
    public var maximumOutstandingBytes: Int64

    public static let standard = PhotoBackupBudget(maximumUploadCount: 4, maximumOutstandingBytes: 2 * 1024 * 1024 * 1024)

    public init(maximumUploadCount: Int, maximumOutstandingBytes: Int64) {
        self.maximumUploadCount = maximumUploadCount
        self.maximumOutstandingBytes = maximumOutstandingBytes
    }

    /// Whether another file may be exported at all.
    public func allowsAnother(outstandingCount: Int, outstandingBytes: Int64) -> Bool {
        outstandingCount == 0 || (outstandingCount < maximumUploadCount && outstandingBytes < maximumOutstandingBytes)
    }

    /// Whether a file of this size, already exported, may join the queue. The
    /// first file always may, however large, or a video over the limit could
    /// never be sent.
    public func allowsFile(sizeBytes: Int64, outstandingCount: Int, outstandingBytes: Int64) -> Bool {
        outstandingCount == 0 || outstandingBytes + sizeBytes <= maximumOutstandingBytes
    }
}

// MARK: What the server answered

/// PhotoBackupOutcome is what one finished upload comes to.
public enum PhotoBackupOutcome: Equatable, Sendable {
    /// The server has the file and says which photo it belongs to.
    case uploaded(serverPhotoId: String)
    /// The server does not accept the device password.
    case signInNeeded
    /// The server will not take this file, however often it is sent.
    case refused(failure: PhotoBackupFailure)
    /// It did not work this time; the file is sent again by a later pass.
    case retryLater(failure: PhotoBackupFailure?, isConnectivityFailure: Bool)

    /// What the server says when the device password is right but was made
    /// without access to photos: the person signs in again for a new one.
    static let passwordWithoutPhotosReason = "does not grant photos"

    /// Reads a finished task. `errorDescription` is the transport's error, if
    /// any; `bodyText` the start of the answer's body, which is the server's
    /// reason for a refusal. Only a 401, and a 403 that says the device
    /// password does not grant photos, mean signing in again: any other 403
    /// (photos are not available on this server, the account has no photo
    /// library, uploads need HTTPS) is the server's reason to show, and the
    /// file is sent again later.
    public static func interpret(
        statusCode: Int?, photoIdHeader: String?, bodyText: String?, errorDescription: String?, isConnectivityFailure: Bool
    ) -> PhotoBackupOutcome {
        if let errorDescription {
            return .retryLater(failure: .systemDescription(errorDescription), isConnectivityFailure: isConnectivityFailure)
        }
        guard let statusCode else { return .retryLater(failure: nil, isConnectivityFailure: true) }
        let reason = bodyText?.trimmingCharacters(in: .whitespacesAndNewlines).nilIfEmpty
        switch statusCode {
        case 200, 201:
            if let photoIdHeader = photoIdHeader?.trimmingCharacters(in: .whitespacesAndNewlines), !photoIdHeader.isEmpty {
                return .uploaded(serverPhotoId: photoIdHeader)
            }
            return .retryLater(failure: .serverDidNotSayWhichPhoto, isConnectivityFailure: false)
        case 401:
            return .signInNeeded
        case 403:
            if reason?.lowercased().contains(passwordWithoutPhotosReason) == true { return .signInNeeded }
            return .retryLater(failure: reason.map { .serverReason($0) }, isConnectivityFailure: false)
        case 408, 425, 429, 500...599:
            return .retryLater(failure: reason.map { .serverReason($0) }, isConnectivityFailure: false)
        case 400...499:
            return .refused(failure: .fileRefused(serverReason: reason))
        default:
            return .retryLater(failure: nil, isConnectivityFailure: false)
        }
    }
}

// MARK: The device password

/// PhotoBackupCredential is the device password backup signs in with.
public struct PhotoBackupCredential: Codable, Equatable, Sendable {
    /// The password's id on the server, to revoke it by.
    public let credentialId: String
    public let credentialName: String
    public let username: String
    public let password: String

    public init(credentialId: String, credentialName: String, username: String, password: String) {
        self.credentialId = credentialId
        self.credentialName = credentialName
        self.username = username
        self.password = password
    }

    /// The Authorization header value for HTTP Basic.
    public var authorizationHeader: String {
        "Basic " + Data("\(username):\(password)".utf8).base64EncodedString()
    }
}

/// PhotoBackupCredentialStore keeps the password in the keychain, one per
/// account, where it can be read after the phone's first unlock because a
/// background upload runs with the phone locked.
public struct PhotoBackupCredentialStore: Sendable {
    private let secrets: any SecretStore

    public init(secrets: any SecretStore) {
        self.secrets = secrets
    }

    static func name(accountKey: String) -> String {
        "photoBackupCredential|" + accountKey
    }

    public func load(accountKey: String) throws -> PhotoBackupCredential? {
        guard let data = try secrets.read(Self.name(accountKey: accountKey)) else { return nil }
        return try JSONDecoder().decode(PhotoBackupCredential.self, from: data)
    }

    public func save(_ credential: PhotoBackupCredential, accountKey: String) throws {
        try secrets.write(JSONEncoder().encode(credential), as: Self.name(accountKey: accountKey))
    }

    public func delete(accountKey: String) throws {
        try secrets.delete(Self.name(accountKey: accountKey))
    }
}

// MARK: The person's choices

/// PhotoBackupPreferences are the switches of the Photo Backup screen. Backup
/// is on for one account at a time.
public struct PhotoBackupPreferences {
    private let defaults: UserDefaults

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    static let enabledAccountKeyName = "photoBackup.enabledAccountKey"
    static let usesMobileDataName = "photoBackup.usesMobileData"
    static let includesHiddenName = "photoBackup.includesHidden"

    /// The account whose photos this phone backs up; nil when backup is off.
    public var enabledAccountKey: String? {
        get { defaults.string(forKey: Self.enabledAccountKeyName) }
        nonmutating set {
            if let newValue { defaults.set(newValue, forKey: Self.enabledAccountKeyName) } else { defaults.removeObject(forKey: Self.enabledAccountKeyName) }
        }
    }

    /// Off unless the person turns it on.
    public var usesMobileData: Bool {
        get { defaults.bool(forKey: Self.usesMobileDataName) }
        nonmutating set { defaults.set(newValue, forKey: Self.usesMobileDataName) }
    }

    /// Off unless the person turns it on: the Hidden album is hidden for a reason.
    public var includesHidden: Bool {
        get { defaults.bool(forKey: Self.includesHiddenName) }
        nonmutating set { defaults.set(newValue, forKey: Self.includesHiddenName) }
    }
}

// MARK: The request

/// PhotoBackupRequest is the request that sends one file.
public enum PhotoBackupRequest {
    public static let uploadPath = "/api/v1/photos/uploads"

    /// A POST to the upload endpoint with the headers that describe the file
    /// and the device password. The body is the file, given to the session
    /// separately. It does not carry the resumable-upload headers: URLSession
    /// adds its own for a server that answers with the 104, and a second,
    /// differently written, set would only confuse the server.
    public static func make(server: ServerAddress, credential: PhotoBackupCredential, upload: PhotoBackupUpload, usesMobileData: Bool) -> URLRequest {
        var request = URLRequest(url: server.endpoint(uploadPath))
        request.httpMethod = "POST"
        for (name, value) in upload.headers { request.setValue(value, forHTTPHeaderField: name) }
        request.setValue(credential.authorizationHeader, forHTTPHeaderField: "Authorization")
        request.allowsCellularAccess = usesMobileData
        #if !canImport(FoundationNetworking)
            request.allowsExpensiveNetworkAccess = usesMobileData
        #endif
        return request
    }
}

// MARK: Status

/// PhotoBackupStatus is what the Photo Backup screen shows.
public struct PhotoBackupStatus: Equatable, Sendable {
    public enum Phase: Equatable, Sendable {
        /// Backup is not turned on.
        case off
        /// Nothing is being done: up to date, or not yet started.
        case idle
        /// Reading the library.
        case listing
        /// Exporting and sending files.
        case uploading
        /// Files are left that wait for a Wi-Fi network or for iCloud.
        case waitingForNetwork
        /// The library cannot be read as backup needs.
        case needsAccess(PhotoBackupAccess)
        /// The server does not accept the device password.
        case needsSignIn
    }

    public var phase: Phase
    public var backedUpItemCount: Int
    public var libraryItemCount: Int
    /// Files this pass set out to send, and how many are settled.
    public var plannedFileCount: Int
    public var finishedFileCount: Int
    /// Files queued with the session now.
    public var queuedFileCount: Int
    /// The file being exported or sent first.
    public var currentFilename: String?
    public var refusedFileCount: Int
    /// Files the last pass could not send and left for later: they wait for
    /// the network, for iCloud, or for the server to answer again.
    public var leftForLaterFileCount: Int
    public var lastFailure: PhotoBackupFailure?
    public var lastPassFinishedAt: Date?

    public var isEnabled: Bool { phase != .off }

    public init(
        phase: Phase = .off, backedUpItemCount: Int = 0, libraryItemCount: Int = 0, plannedFileCount: Int = 0, finishedFileCount: Int = 0,
        queuedFileCount: Int = 0, currentFilename: String? = nil, refusedFileCount: Int = 0, leftForLaterFileCount: Int = 0,
        lastFailure: PhotoBackupFailure? = nil, lastPassFinishedAt: Date? = nil
    ) {
        self.phase = phase
        self.backedUpItemCount = backedUpItemCount
        self.libraryItemCount = libraryItemCount
        self.plannedFileCount = plannedFileCount
        self.finishedFileCount = finishedFileCount
        self.queuedFileCount = queuedFileCount
        self.currentFilename = currentFilename
        self.refusedFileCount = refusedFileCount
        self.leftForLaterFileCount = leftForLaterFileCount
        self.lastFailure = lastFailure
        self.lastPassFinishedAt = lastPassFinishedAt
    }

    /// How far the backup has got, from 0 to 1; nil while the library has not
    /// been counted.
    public var fractionBackedUp: Double? {
        guard libraryItemCount > 0 else { return nil }
        return min(1, Double(backedUpItemCount) / Double(libraryItemCount))
    }
}

/// PhotoBackupPassResult says how a pass went.
public struct PhotoBackupPassResult: Equatable, Sendable {
    public var wasAlreadyRunning = false
    public var plannedFileCount = 0
    public var uploadedFileCount = 0
    public var refusedFileCount = 0
    /// Files left for a later pass: they wait for Wi-Fi or iCloud, or could not
    /// be queued.
    public var deferredFileCount = 0
    /// Every file the pass left for later: those above, and those the server
    /// or the network failed on this time, which a later pass sends again.
    public var leftForLaterFileCount = 0
    public var wasStopped = false

    public init() {}
}

// MARK: The pass

/// PhotoBackupCoordinator runs backup: it reads the library (all of it the
/// first time, the changes since then after that), asks the server which
/// photos it has, plans the uploads, and keeps a few files exported and queued
/// with the background session until none is left. Uploads finish in the
/// session, perhaps while the app is closed; `finished` is how the session
/// reports them.
public actor PhotoBackupCoordinator {
    public struct Settings: Equatable, Sendable {
        public var usesMobileData: Bool
        public var includesHidden: Bool

        public init(usesMobileData: Bool, includesHidden: Bool) {
            self.usesMobileData = usesMobileData
            self.includesHidden = includesHidden
        }
    }

    private struct Outstanding {
        let sizeBytes: Int64
        let filename: String
    }

    private static let findPhotosBatchSize = 500

    private let index: PhotoBackupIndex
    private let library: any PhotoBackupLibrary
    private let transfer: any PhotoBackupTransfer
    private let lookup: (any PhotoBackupServerLookup)?
    private let network: any PhotoBackupNetwork
    private let outgoingDirectory: URL
    private let server: ServerAddress
    private let accountKey: String
    private let credential: PhotoBackupCredential
    private let budget: PhotoBackupBudget
    private let now: @Sendable () -> Date
    private let fileManager: FileManager

    private var outstanding: [String: Outstanding] = [:]
    private var waiters: [CheckedContinuation<Void, Never>] = []
    private var isRunning = false
    private var isStopRequested = false
    private var settings = Settings(usesMobileData: false, includesHidden: false)
    private var phase: PhotoBackupStatus.Phase = .idle
    private var plannedFileCount = 0
    private var finishedFileCount = 0
    private var uploadedFileCount = 0
    private var refusedFileCount = 0
    private var leftForLaterFileCount = 0
    private var currentFilename: String?
    private var statusHandler: (@Sendable (PhotoBackupStatus) -> Void)?

    public init(
        index: PhotoBackupIndex, library: any PhotoBackupLibrary, transfer: any PhotoBackupTransfer, lookup: (any PhotoBackupServerLookup)?,
        network: any PhotoBackupNetwork, outgoingDirectory: URL, server: ServerAddress, accountKey: String, credential: PhotoBackupCredential,
        budget: PhotoBackupBudget = .standard, now: @escaping @Sendable () -> Date = { Date() }, fileManager: FileManager = .default
    ) {
        self.index = index
        self.library = library
        self.transfer = transfer
        self.lookup = lookup
        self.network = network
        self.outgoingDirectory = outgoingDirectory
        self.server = server
        self.accountKey = accountKey
        self.credential = credential
        self.budget = budget
        self.now = now
        self.fileManager = fileManager
    }

    public func setStatusHandler(_ handler: (@Sendable (PhotoBackupStatus) -> Void)?) {
        statusHandler = handler
    }

    /// The status now, read from the index and the pass in progress.
    public func status() async -> PhotoBackupStatus {
        let summary = await index.summary()
        var shownPhase = phase
        if summary.isSignInNeeded { shownPhase = .needsSignIn }
        return PhotoBackupStatus(
            phase: shownPhase, backedUpItemCount: summary.backedUpItemCount, libraryItemCount: summary.libraryItemCount,
            plannedFileCount: plannedFileCount, finishedFileCount: finishedFileCount, queuedFileCount: outstanding.count,
            currentFilename: currentFilename, refusedFileCount: summary.refusedFileCount, leftForLaterFileCount: leftForLaterFileCount,
            lastFailure: summary.lastFailure, lastPassFinishedAt: summary.lastPassFinishedAt)
    }

    /// Asks a running pass to stop: nothing more is exported. Files already
    /// queued with the session stay there unless the caller cancels them.
    public func stop() {
        isStopRequested = true
        resumeWaiters()
    }

    public var isPassRunning: Bool { isRunning }

    /// Runs one pass, which ends when every planned file has been queued and
    /// the session has finished them, or when stopped.
    public func runPass(settings: Settings) async -> PhotoBackupPassResult {
        var result = PhotoBackupPassResult()
        guard !isRunning else {
            result.wasAlreadyRunning = true
            return result
        }
        isRunning = true
        isStopRequested = false
        self.settings = settings
        plannedFileCount = 0
        finishedFileCount = 0
        uploadedFileCount = 0
        refusedFileCount = 0
        leftForLaterFileCount = 0
        result = await performPass()
        result.uploadedFileCount = uploadedFileCount
        result.refusedFileCount = refusedFileCount
        result.leftForLaterFileCount = leftForLaterFileCount
        result.wasStopped = isStopRequested
        isRunning = false
        currentFilename = nil
        await index.flush()
        await publish()
        return result
    }

    private func performPass() async -> PhotoBackupPassResult {
        var result = PhotoBackupPassResult()
        await index.adopt(accountKey: accountKey)
        let access = library.access()
        guard access == .full else {
            phase = .needsAccess(access)
            return result
        }
        if await index.summary().isSignInNeeded {
            phase = .needsSignIn
            return result
        }
        await index.clearErrorUnlessSignInIsNeeded()
        phase = .listing
        await publish()

        // What the session still holds is the truth about what is in flight.
        outstanding.removeAll()
        let queuedJobs = await transfer.queuedJobs()
        for job in queuedJobs {
            outstanding[job.key] = Outstanding(sizeBytes: job.sizeBytes, filename: job.filename)
        }
        removeLeftoverFiles(keeping: Set(queuedJobs.map(\.fileName)))

        let items: [PhotoBackupItem]
        let totalCount: Int
        let newToken: Data?
        do {
            let read = try await readLibrary()
            items = read.items
            totalCount = read.totalCount
            newToken = read.changeToken
        } catch {
            await index.setError(failure: Self.failure(of: error))
            phase = .idle
            return result
        }
        await index.setLibraryItemCount(totalCount)
        await askServerWhichPhotosItHas(items)

        let uploads = await index.plan(items: items, inFlight: Set(outstanding.keys))
        plannedFileCount = uploads.count
        result.plannedFileCount = uploads.count
        // Every file of this batch is now in the index as still to send, so a
        // pass that ends early, or a file that has to wait, loses nothing: the
        // next pass reads those items again. That is what lets the library's
        // token move on here.
        await index.markPending(uploads)
        if let newToken { await index.setChangeToken(newToken) }
        var position = 0
        while position < uploads.count, !isStopRequested {
            let upload = uploads[position]
            if !budget.allowsAnother(outstandingCount: outstanding.count, outstandingBytes: outstandingBytes) {
                phase = .uploading
                await publish()
                await waitForCompletion()
                continue
            }
            // A file that has to come down from iCloud uses the network, so on
            // a network that costs it waits, unless the person allowed it.
            if !upload.isLocallyAvailable, !settings.usesMobileData, await network.isExpensive() {
                result.deferredFileCount += 1
                leftForLaterFileCount += 1
                position += 1
                continue
            }
            phase = .uploading
            currentFilename = upload.filename
            await publish()
            switch await queue(upload) {
            case .queued:
                position += 1
            case .mustWait:
                await waitForCompletion()
            case .deferred:
                result.deferredFileCount += 1
                leftForLaterFileCount += 1
                position += 1
            case .refused:
                position += 1
            }
        }

        currentFilename = nil
        while !outstanding.isEmpty, !isStopRequested {
            phase = .uploading
            await publish()
            await waitForCompletion()
        }
        if !isStopRequested {
            await index.finishPass(at: now())
        }
        // A pass that leaves files for later ends waiting for the network, not
        // idle: the files are still to send, and the screen must not say that
        // everything is backed up.
        if await index.summary().isSignInNeeded {
            phase = .needsSignIn
        } else {
            phase = leftForLaterFileCount > 0 ? .waitingForNetwork : .idle
        }
        return result
    }

    private struct LibraryRead {
        let items: [PhotoBackupItem]
        let totalCount: Int
        let changeToken: Data?
    }

    private func readLibrary() async throws -> LibraryRead {
        if let token = await index.changeToken {
            do {
                let changes = try await library.changes(since: token, includesHidden: settings.includesHidden)
                var items = changes.items
                // Files left over from earlier passes go again.
                let changedIdentifiers = Set(items.map(\.localIdentifier))
                let pendingIdentifiers = await index.pendingLocalIdentifiers().filter { !changedIdentifiers.contains($0) }
                if !pendingIdentifiers.isEmpty {
                    let pending = try await library.items(withLocalIdentifiers: pendingIdentifiers, includesHidden: settings.includesHidden)
                    items.append(contentsOf: pending)
                    let found = Set(pending.map(\.localIdentifier))
                    await index.forgetPending(of: pendingIdentifiers.filter { !found.contains($0) })
                }
                return LibraryRead(items: items, totalCount: changes.totalCount, changeToken: changes.changeToken)
            } catch PhotoBackupLibraryError.changeTokenExpired {
                // Too old: read everything again, skipping what the index has.
            }
        }
        let listing = try await library.listItems(skipping: await index.knownLocalIdentifiers(), includesHidden: settings.includesHidden)
        var items = listing.items
        // What the index has is skipped by the listing; what it has still to
        // send is read again.
        let pendingIdentifiers = await index.pendingLocalIdentifiers()
        if !pendingIdentifiers.isEmpty {
            let pending = try await library.items(withLocalIdentifiers: pendingIdentifiers, includesHidden: settings.includesHidden)
            items.append(contentsOf: pending)
            let found = Set(pending.map(\.localIdentifier))
            await index.forgetPending(of: pendingIdentifiers.filter { !found.contains($0) })
        }
        return LibraryRead(items: items, totalCount: listing.totalCount, changeToken: listing.changeToken)
    }

    /// Photos the server has already (from an earlier install, or a second
    /// iPhone on the same library) are marked done and not sent. The question
    /// is a convenience: if it cannot be answered the files are sent anyway
    /// and the server stores each once.
    private func askServerWhichPhotosItHas(_ items: [PhotoBackupItem]) async {
        guard let lookup else { return }
        let clientPhotoIds = await index.clientPhotoIdsToAsk(about: items)
        var found: [PhotoBackupServerPhoto] = []
        var start = 0
        while start < clientPhotoIds.count, !isStopRequested {
            let batch = Array(clientPhotoIds[start..<min(start + Self.findPhotosBatchSize, clientPhotoIds.count)])
            start += batch.count
            guard let answer = try? await lookup.findPhotos(clientPhotoIds: batch) else { break }
            found.append(contentsOf: answer)
        }
        if !found.isEmpty { await index.markServerHas(found, for: items) }
    }

    private enum QueueResult {
        case queued
        /// Too much is in flight to take this file now.
        case mustWait
        case deferred
        case refused
    }

    /// Exports one file and hands it to the session.
    private func queue(_ upload: PhotoBackupUpload) async -> QueueResult {
        let exported: PhotoBackupExportedFile
        do {
            exported = try await library.export(
                upload, to: outgoingDirectory, mayUseExpensiveNetwork: settings.usesMobileData, includesHidden: settings.includesHidden)
        } catch PhotoBackupLibraryError.unavailable {
            return .deferred
        } catch PhotoBackupLibraryError.failed(let failure) {
            await index.markRefused(localIdentifier: upload.localIdentifier, clientPhotoId: upload.clientPhotoId, role: upload.role, failure: failure)
            refusedFileCount += 1
            finishedFileCount += 1
            await index.setError(failure: failure)
            return .refused
        } catch {
            return .deferred
        }
        if isStopRequested {
            removeTemporaryFile(named: exported.fileName)
            return .deferred
        }
        if !budget.allowsFile(sizeBytes: exported.sizeBytes, outstandingCount: outstanding.count, outstandingBytes: outstandingBytes) {
            removeTemporaryFile(named: exported.fileName)
            return .mustWait
        }
        let job = PhotoBackupJob(
            accountKey: accountKey, localIdentifier: upload.localIdentifier, role: upload.role, clientPhotoId: upload.clientPhotoId,
            filename: upload.filename, assetModificationDate: upload.assetModificationDate, fileName: exported.fileName,
            sizeBytes: exported.sizeBytes)
        let request = PhotoBackupRequest.make(server: server, credential: credential, upload: upload, usesMobileData: settings.usesMobileData)
        outstanding[job.key] = Outstanding(sizeBytes: exported.sizeBytes, filename: upload.filename)
        do {
            try await transfer.start(request: request, job: job, fileURL: outgoingDirectory.appendingPathComponent(exported.fileName))
        } catch {
            outstanding[job.key] = nil
            removeTemporaryFile(named: exported.fileName)
            await index.setError(failure: Self.failure(of: error))
            return .deferred
        }
        return .queued
    }

    /// The session reports a finished upload. It may arrive while no pass runs,
    /// when the app was started to hear of it.
    public func finished(_ job: PhotoBackupJob, outcome: PhotoBackupOutcome) async {
        outstanding[job.key] = nil
        removeTemporaryFile(named: job.fileName)
        switch outcome {
        case .uploaded(let serverPhotoId):
            await index.markUploaded(
                localIdentifier: job.localIdentifier, clientPhotoId: job.clientPhotoId, role: job.role, serverPhotoId: serverPhotoId,
                assetModificationDate: job.assetModificationDate)
            uploadedFileCount += 1
            finishedFileCount += 1
        case .signInNeeded:
            await index.setError(failure: .signInRefused, isSignInNeeded: true)
            isStopRequested = true
            await transfer.cancelAll()
            outstanding.removeAll()
        case .refused(let failure):
            await index.markRefused(localIdentifier: job.localIdentifier, clientPhotoId: job.clientPhotoId, role: job.role, failure: failure)
            refusedFileCount += 1
            finishedFileCount += 1
            await index.setError(failure: failure)
        case .retryLater(let failure, let isConnectivityFailure):
            if let failure, !isConnectivityFailure { await index.setError(failure: failure) }
            // The file is still to send and nothing in this pass sends it
            // again: it is counted, so that the pass ends waiting for the
            // network, and one that finished with no pass running puts the
            // screen there too.
            leftForLaterFileCount += 1
            if !isRunning { phase = .waitingForNetwork }
        }
        resumeWaiters()
        await publish()
    }

    // MARK: Waiting

    private var outstandingBytes: Int64 {
        outstanding.values.reduce(0) { $0 + $1.sizeBytes }
    }

    /// Suspends until an upload finishes or the pass is stopped; at once if
    /// nothing is outstanding, as nothing would wake it.
    private func waitForCompletion() async {
        if isStopRequested || outstanding.isEmpty { return }
        await withCheckedContinuation { continuation in
            waiters.append(continuation)
        }
    }

    private func resumeWaiters() {
        let resumed = waiters
        waiters.removeAll()
        for continuation in resumed { continuation.resume() }
    }

    private func publish() async {
        guard let statusHandler else { return }
        statusHandler(await status())
    }

    /// Deletes the files in the outgoing folder that no queued upload refers
    /// to: left by a pass that was killed after exporting a file and before
    /// the session took it, or by a task the session lost.
    private func removeLeftoverFiles(keeping queuedFileNames: Set<String>) {
        let names = (try? fileManager.contentsOfDirectory(atPath: outgoingDirectory.path)) ?? []
        for name in names where !queuedFileNames.contains(name) {
            removeTemporaryFile(named: name)
        }
    }

    private func removeTemporaryFile(named name: String) {
        let safeName = (name as NSString).lastPathComponent
        guard !safeName.isEmpty, safeName != ".", safeName != ".." else { return }
        try? fileManager.removeItem(at: outgoingDirectory.appendingPathComponent(safeName))
    }

    private static func failure(of error: Error) -> PhotoBackupFailure {
        switch error {
        case PhotoBackupLibraryError.unavailable(let failure), PhotoBackupLibraryError.failed(let failure): failure
        default: .systemDescription(error.localizedDescription)
        }
    }
}

// MARK: When to start again

/// PhotoBackupNetworkState is what the network watcher reports of the path the
/// phone is on.
public struct PhotoBackupNetworkState: Equatable, Sendable {
    /// Whether the phone can reach the network at all.
    public let isSatisfied: Bool
    /// Whether the network costs data: mobile data or a personal hotspot.
    public let isExpensive: Bool

    public init(isSatisfied: Bool, isExpensive: Bool) {
        self.isSatisfied = isSatisfied
        self.isExpensive = isExpensive
    }
}

/// PhotoBackupResumePolicy decides whether a change of network starts a pass
/// again. Files left for later wait for the network; when it comes back, or a
/// network that does not cost data replaces one that does, the pass starts at
/// once, without waiting for the app to be opened.
public enum PhotoBackupResumePolicy {
    public static func shouldResume(
        from previous: PhotoBackupNetworkState?, to current: PhotoBackupNetworkState, usesMobileData: Bool, leftForLaterFileCount: Int,
        isPassRunning: Bool
    ) -> Bool {
        guard leftForLaterFileCount > 0, !isPassRunning, current.isSatisfied, usesMobileData || !current.isExpensive else { return false }
        guard let previous else { return true }
        if !previous.isSatisfied { return true }
        // A network that costs data was in use and one that does not has come.
        return previous.isExpensive && !current.isExpensive
    }
}

/// PhotoBackupSchedule says when the system is asked to run the background
/// processing task.
public enum PhotoBackupSchedule {
    /// When files were left for later the task is asked for soon, since they
    /// are waiting; otherwise it is only a look for new photos, once an hour.
    public static func earliestBeginDelaySeconds(leftForLaterFileCount: Int) -> TimeInterval {
        leftForLaterFileCount > 0 ? 15 * 60 : 60 * 60
    }
}

private extension String {
    var nilIfEmpty: String? { isEmpty ? nil : self }
}

// MARK: The server

extension Documents {
    public static let createPhotoBackupCredential = """
        mutation ($credentialName: String!) {
          CreateSyncCredential(credentialName: $credentialName, hasCalendarAccess: false, hasRemindersAccess: false, hasContactsAccess: false, hasPhotosAccess: true) {
            credential { id credentialName }
            username password
          }
        }
        """
    public static let findPhotos = """
        query ($clientPhotoIds: [String!]!) { FindPhotos(clientPhotoIds: $clientPhotoIds) { id clientPhotoId photoKind photoFileRoles } }
        """
}

/// CreatedPhotoBackupCredential is a new device password for backup, shown to
/// the app once.
struct CreatedPhotoBackupCredential: Decodable, Sendable {
    struct Credential: Decodable, Sendable {
        let id: String
        let credentialName: String
    }
    let credential: Credential
    let username: String
    let password: String
}

extension Connection {
    /// Makes a device password that may upload photos and nothing else, for
    /// this phone's backup to sign in with.
    public func createPhotoBackupCredential(named credentialName: String) async throws -> PhotoBackupCredential {
        struct Answer: Decodable, Sendable { let CreateSyncCredential: CreatedPhotoBackupCredential }
        let created = try await execute(
            Documents.createPhotoBackupCredential, variables: ["credentialName": .string(credentialName)], as: Answer.self
        ).CreateSyncCredential
        return PhotoBackupCredential(
            credentialId: created.credential.id, credentialName: created.credential.credentialName, username: created.username,
            password: created.password)
    }
}

extension Connection: PhotoBackupServerLookup {
    /// Which of the uploader's photos the library already has. At most 500 names.
    public func findPhotos(clientPhotoIds: [String]) async throws -> [PhotoBackupServerPhoto] {
        struct Answer: Decodable, Sendable { let FindPhotos: [PhotoBackupServerPhoto] }
        return try await execute(
            Documents.findPhotos, variables: ["clientPhotoIds": .array(clientPhotoIds.map { .string($0) })], as: Answer.self
        ).FindPhotos
    }
}
