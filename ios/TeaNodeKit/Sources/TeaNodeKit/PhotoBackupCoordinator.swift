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

/// PhotoBackupLibraryEntry is one item of the library as a listing sees it:
/// which it is and when it was made, nothing that costs a read of its files.
public struct PhotoBackupLibraryEntry: Equatable, Sendable {
    public let localIdentifier: String
    public let creationDate: Date?

    public init(localIdentifier: String, creationDate: Date?) {
        self.localIdentifier = localIdentifier
        self.creationDate = creationDate
    }
}

/// PhotoBackupListingStart is what the library says when a listing of all of
/// it begins.
public struct PhotoBackupListingStart: Sendable {
    /// How many items the library holds.
    public let totalCount: Int
    /// The library's change token as it is now, archived, to be kept once the
    /// listing has ended so that nothing changed during it is missed.
    public let changeToken: Data?

    public init(totalCount: Int, changeToken: Data?) {
        self.totalCount = totalCount
        self.changeToken = changeToken
    }
}

/// PhotoBackupListingBatch is the next stretch of a listing of the library.
public struct PhotoBackupListingBatch: Sendable {
    /// Up to the number asked for, newest first, each after the one before.
    public let entries: [PhotoBackupLibraryEntry]
    /// Whether the library has nothing after these.
    public let isLast: Bool

    public init(entries: [PhotoBackupLibraryEntry], isLast: Bool) {
        self.entries = entries
        self.isLast = isLast
    }
}

/// PhotoBackupChanges is what the library gained or changed since a token.
public struct PhotoBackupChanges: Sendable {
    /// Items added or changed. Deleted items are not here: deleting on the
    /// phone leaves the server's copy alone.
    public let localIdentifiers: [String]
    public let totalCount: Int
    public let changeToken: Data?

    public init(localIdentifiers: [String], totalCount: Int, changeToken: Data?) {
        self.localIdentifiers = localIdentifiers
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
    public let outgoingFilename: String
    public let sizeBytes: Int64
    /// The SHA-256 of the file's bytes in lowercase hexadecimal, sent with the
    /// upload; nil when it could not be worked out, and then the file is sent
    /// without it.
    public let sha256: String?

    public init(outgoingFilename: String, sizeBytes: Int64, sha256: String? = nil) {
        self.outgoingFilename = outgoingFilename
        self.sizeBytes = sizeBytes
        self.sha256 = sha256
    }
}

/// PhotoBackupLibrary is the phone's photo library as backup reads it.
public protocol PhotoBackupLibrary: Sendable {
    func access() -> PhotoBackupAccess
    /// Begins a listing of all of the library: how many items it holds, and its
    /// change token as it is now. Cheap, whatever the library's size.
    func beginListing(includesHidden: Bool) async throws -> PhotoBackupListingStart
    /// The next `limit` items of the library, newest first, after `resumePoint`
    /// (from the start when nil). Cheap: it names the items and reads nothing of
    /// their files. A library whose items change between two calls still gives
    /// each item after the resume point once.
    func listBatch(after resumePoint: PhotoBackupResumePoint?, limit: Int, includesHidden: Bool) async throws -> PhotoBackupListingBatch
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
    public let outgoingFilename: String
    public let sizeBytes: Int64

    public var key: String { PhotoBackupUpload.key(localIdentifier: localIdentifier, role: role) }

    public init(
        accountKey: String, localIdentifier: String, role: PhotoBackupFileRole, clientPhotoId: String, filename: String,
        assetModificationDate: Date?, outgoingFilename: String, sizeBytes: Int64
    ) {
        self.accountKey = accountKey
        self.localIdentifier = localIdentifier
        self.role = role
        self.clientPhotoId = clientPhotoId
        self.filename = filename
        self.assetModificationDate = assetModificationDate
        self.outgoingFilename = outgoingFilename
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
    /// Whether the phone can reach the network at all, and whether it costs.
    func state() async -> PhotoBackupNetworkState
}

extension PhotoBackupNetwork {
    /// For a network that only knows whether it costs: it is taken to be reachable.
    public func state() async -> PhotoBackupNetworkState {
        PhotoBackupNetworkState(isSatisfied: true, isExpensive: await isExpensive())
    }
}

// MARK: How much is in flight

/// PhotoBackupBudget bounds the uploads queued at once, and the temporary files
/// they are read from. The background session holds them all, and iOS goes on
/// sending them after the person has left the app, so the more files are queued,
/// the longer uploads continue with nobody watching; each queued file is a copy
/// in the app's outgoing folder, so the bytes are also held to a share of the
/// room the phone has left.
public struct PhotoBackupBudget: Equatable, Sendable {
    public var maximumUploadCount: Int
    public var maximumOutstandingBytes: Int64

    public static let standard = PhotoBackupBudget(maximumUploadCount: 200, maximumOutstandingBytes: 10 * 1024 * 1024 * 1024)

    /// The share of the free space on the volume that the queued copies may
    /// take: a quarter, so that a full queue never fills the phone.
    public static let maximumShareOfFreeSpace = 0.25

    public init(maximumUploadCount: Int, maximumOutstandingBytes: Int64) {
        self.maximumUploadCount = maximumUploadCount
        self.maximumOutstandingBytes = maximumOutstandingBytes
    }

    /// This budget, with the outstanding bytes held to a quarter of the free
    /// space when that is less. Nil free space (it could not be read) leaves
    /// the budget as it is.
    public func limited(toFreeSpaceBytes freeSpaceBytes: Int64?) -> PhotoBackupBudget {
        guard let freeSpaceBytes else { return self }
        let share = Int64(Double(max(0, freeSpaceBytes)) * Self.maximumShareOfFreeSpace)
        var limited = self
        limited.maximumOutstandingBytes = min(maximumOutstandingBytes, share)
        return limited
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

    /// Whether the rest of an exported file, from the byte the server has, may
    /// be written beside it. The two exist together until the whole file is
    /// deleted, which no queued copy has counted, so the rest is held to half
    /// of the free space; free space that cannot be read allows it. A rest that
    /// does not fit is not written: the file is sent again from its first byte,
    /// which needs no room beyond the file.
    public static func allowsTail(sizeBytes: Int64, freeSpaceBytes: Int64?) -> Bool {
        guard let freeSpaceBytes else { return true }
        return sizeBytes <= max(0, freeSpaceBytes) / 2
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
    /// It did not work this time; the file is sent again by a later pass, not
    /// before `retryAfterSeconds` when the server said how long to wait (the
    /// Retry-After of a 503, which is how it refuses an upload only because of
    /// the account's other unfinished uploads).
    case retryLater(failure: PhotoBackupFailure?, isConnectivityFailure: Bool, retryAfterSeconds: Int? = nil)
    /// The bytes were sent to an upload that is not where the request said: the
    /// server has another number of bytes (409, as when the app was cut off
    /// after a part arrived and the answer to the question was an older one) or
    /// no such upload any more (404, 410). Nothing is wrong with the file or the
    /// server, so it shows no error and counts as no failure: the next pass asks
    /// where the upload is again, and a 404 makes it start a new one.
    case uploadMovedOn
    /// The task was cancelled by the app (backup turned off, a setting changed,
    /// a sign-in refused): not an error and not counted, and the file is still
    /// to send, as the index has it.
    case cancelled

    /// The longest wait a Retry-After is believed for: an hour. A server that
    /// asks for more is asked again after this.
    public static let longestRetryAfterSeconds = 3600

    /// How long a Retry-After header asks to wait: whole seconds, or an HTTP
    /// date, which is turned into the seconds from `now`. Nil when there is no
    /// header, it makes no sense, or the date has passed; never more than
    /// `longestRetryAfterSeconds`.
    public static func retryAfterSeconds(fromHeader header: String?, now: Date = Date()) -> Int? {
        guard let text = header?.trimmingCharacters(in: .whitespacesAndNewlines), !text.isEmpty else { return nil }
        if let seconds = Int(text) {
            return seconds > 0 ? min(seconds, longestRetryAfterSeconds) : nil
        }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        formatter.dateFormat = "EEE, dd MMM yyyy HH:mm:ss 'GMT'"
        guard let date = formatter.date(from: text) else { return nil }
        let seconds = Int(date.timeIntervalSince(now).rounded(.up))
        return seconds > 0 ? min(seconds, longestRetryAfterSeconds) : nil
    }

    /// What the server says when the device password is right but was made
    /// without access to photos: the person signs in again for a new one.
    static let passwordWithoutPhotosReason = "does not grant photos"

    /// Reads a finished task. `errorDescription` is the transport's error, if
    /// any; `bodyText` the start of the answer's body, which is the server's
    /// reason for a refusal. Only a 401, and a 403 that says the device
    /// password does not grant photos, mean signing in again: any other 403
    /// (photos are not available on this server, the account has no photo
    /// library, uploads need HTTPS) is the server's reason to show, and the
    /// file is sent again later. A 404, 409 or 410 is not the file's fault
    /// either (the upload was lost on the way, or the photo is being made): the
    /// upload "moved on" and the next pass asks where it is. Only a 400, 413,
    /// 415 or 422, and any other 4xx, is the server refusing the file. A 503 is the server's trouble, or the
    /// account's other unfinished uploads being in the way: the file is sent
    /// again later, after the Retry-After the answer carries, if it has one.
    public static func interpret(
        statusCode: Int?, photoIdHeader: String?, bodyText: String?, errorDescription: String?, isConnectivityFailure: Bool,
        isCancelled: Bool = false, retryAfterHeader: String? = nil, now: Date = Date()
    ) -> PhotoBackupOutcome {
        if isCancelled { return .cancelled }
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
        case 404, 409, 410:
            return .uploadMovedOn
        case 507:
            // The server has no room for the file: its own words are not
            // needed, and the wait it asks for stands.
            return .retryLater(
                failure: .serverOutOfSpace, isConnectivityFailure: false,
                retryAfterSeconds: retryAfterSeconds(fromHeader: retryAfterHeader, now: now))
        case 408, 425, 429, 500...599:
            return .retryLater(
                failure: reason.map { .serverReason($0) }, isConnectivityFailure: false,
                retryAfterSeconds: retryAfterSeconds(fromHeader: retryAfterHeader, now: now))
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
    static let isPausedName = "photoBackup.isPaused"

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

    /// Whether the person paused backup. It lasts until they resume, across
    /// launches, and is not the same as backup being off: nothing is forgotten,
    /// and the index and the place the listing got to stay where they are.
    public var isPaused: Bool {
        get { defaults.bool(forKey: Self.isPausedName) }
        nonmutating set { defaults.set(newValue, forKey: Self.isPausedName) }
    }
}

// MARK: The request

/// PhotoBackupRequest builds the requests that send one file, in the three steps
/// of the server's resumable upload protocol (internal/photo/upload.go): create
/// the upload, ask how much of it the server has, and send the rest.
public enum PhotoBackupRequest {
    public static let uploadPath = "/api/v1/photos/uploads"

    /// The longest the two small requests (creating an upload, asking where it
    /// is) wait for an answer. They are made while a pass is queueing files, or
    /// in the seconds the system gives a woken app, and must not hold it up.
    public static let smallRequestTimeoutSeconds: TimeInterval = 20

    /// The path of an upload on the server, if this is one the server made:
    /// what is kept in the index and asked about. Anything else is refused, so
    /// that an answer cannot send the device password to another path.
    public static func isUploadPath(_ path: String) -> Bool {
        let prefix = uploadPath + "/"
        guard path.hasPrefix(prefix) else { return false }
        let identifier = path.dropFirst(prefix.count)
        return !identifier.isEmpty && !identifier.contains("/") && !identifier.contains("?")
    }

    /// A POST that creates the upload and sends no bytes. It carries the headers
    /// that describe the file, the size it will have and, when known, the digest
    /// the server holds it to (a file that does not hash to it is refused) and
    /// uses to recognize a picture it already has. `Upload-Complete: ?0` says
    /// the bytes follow in a request of their own. The server answers 201 with
    /// the upload's address in `Location`, which is how the app learns it before
    /// any byte has been sent: the answer to a background upload is not seen
    /// until the upload ends.
    ///
    /// Only 64 hexadecimal digits are sent as the digest.
    public static func makeCreation(
        server: ServerAddress, credential: PhotoBackupCredential, upload: PhotoBackupUpload, sizeBytes: Int64, usesMobileData: Bool,
        sha256: String? = nil
    ) -> URLRequest {
        var request = URLRequest(url: server.endpoint(uploadPath))
        request.httpMethod = "POST"
        for (name, value) in upload.headers { request.setValue(value, forHTTPHeaderField: name) }
        if let digest = declaredDigest(sha256) { request.setValue(digest, forHTTPHeaderField: PhotoBackupHeader.declaredSha256) }
        request.setValue("?0", forHTTPHeaderField: PhotoBackupHeader.uploadComplete)
        request.setValue(String(sizeBytes), forHTTPHeaderField: PhotoBackupHeader.uploadLength)
        return prepared(request, credential: credential, usesMobileData: usesMobileData, isSmall: true)
    }

    /// A HEAD that asks how many bytes of the upload the server has kept.
    public static func makeStatus(server: ServerAddress, credential: PhotoBackupCredential, uploadPath: String, usesMobileData: Bool) -> URLRequest {
        var request = URLRequest(url: server.endpoint(uploadPath))
        request.httpMethod = "HEAD"
        return prepared(request, credential: credential, usesMobileData: usesMobileData, isSmall: true)
    }

    /// The longest the request that completes an upload waits for an answer. The
    /// server reads the parts back and checks the digest before it answers,
    /// which takes a while for a video. One that gets no answer in time has not
    /// failed: the server finishes what it began, and the next question about
    /// the upload says it is complete.
    public static let completionRequestTimeoutSeconds: TimeInterval = 60

    /// A PATCH that sends the rest of the file, from `offsetBytes`, and says it
    /// is the end, which makes the server complete the upload. The body is the
    /// file from that byte, given to the background session separately. The
    /// server takes the bytes only if the offset is where it is.
    public static func makeContinuation(
        server: ServerAddress, credential: PhotoBackupCredential, uploadPath: String, offsetBytes: Int64, usesMobileData: Bool
    ) -> URLRequest {
        var request = URLRequest(url: server.endpoint(uploadPath))
        request.httpMethod = "PATCH"
        request.setValue("application/partial-upload", forHTTPHeaderField: PhotoBackupHeader.contentType)
        request.setValue(String(offsetBytes), forHTTPHeaderField: PhotoBackupHeader.uploadOffset)
        request.setValue("?1", forHTTPHeaderField: PhotoBackupHeader.uploadComplete)
        return prepared(request, credential: credential, usesMobileData: usesMobileData, isSmall: false)
    }

    /// A PATCH with no body at the end of the file, which completes an upload
    /// that has every byte. It is made by the app, as a plain request, and not
    /// given to the background session: a task for a file of no bytes is not
    /// something iOS promises to run.
    public static func makeCompletion(
        server: ServerAddress, credential: PhotoBackupCredential, uploadPath: String, sizeBytes: Int64, usesMobileData: Bool
    ) -> URLRequest {
        var request = makeContinuation(
            server: server, credential: credential, uploadPath: uploadPath, offsetBytes: sizeBytes, usesMobileData: usesMobileData)
        request.timeoutInterval = completionRequestTimeoutSeconds
        return request
    }

    /// A DELETE that drops an upload the app will not continue.
    public static func makeCancellation(server: ServerAddress, credential: PhotoBackupCredential, uploadPath: String, usesMobileData: Bool) -> URLRequest {
        var request = URLRequest(url: server.endpoint(uploadPath))
        request.httpMethod = "DELETE"
        return prepared(request, credential: credential, usesMobileData: usesMobileData, isSmall: true)
    }

    private static func prepared(_ request: URLRequest, credential: PhotoBackupCredential, usesMobileData: Bool, isSmall: Bool) -> URLRequest {
        var request = request
        request.setValue(credential.authorizationHeader, forHTTPHeaderField: "Authorization")
        request.allowsCellularAccess = usesMobileData
        #if !canImport(FoundationNetworking)
            request.allowsExpensiveNetworkAccess = usesMobileData
        #endif
        if isSmall { request.timeoutInterval = smallRequestTimeoutSeconds }
        return request
    }

    /// The digest as the server is told it, in lowercase, or nil when it is not
    /// 64 hexadecimal digits. The index keeps the same text.
    public static func declaredDigest(_ sha256: String?) -> String? {
        guard let digest = sha256?.lowercased(), digest.count == 64, digest.allSatisfy({ $0.isASCII && $0.isHexDigit }) else { return nil }
        return digest
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
        /// Files are left because the server kept failing or asked to wait, on
        /// a network that lets them go: they wait for the server, not for Wi-Fi.
        case waitingForServer
        /// The library cannot be read as backup needs.
        case needsAccess(PhotoBackupAccess)
        /// The server does not accept the device password.
        case needsSignIn
        /// The person paused backup: nothing is listed or sent until they resume.
        case paused
    }

    public var phase: Phase
    public var backedUpItemCount: Int
    public var libraryItemCount: Int
    /// Files this pass set out to send, and how many are settled.
    public var plannedFileCount: Int
    public var finishedFileCount: Int
    /// Files queued with the session now.
    public var queuedFileCount: Int
    public var refusedFileCount: Int
    /// Files that could not be backed up and that the person chose to skip:
    /// not counted among the refused ones, and not tried again.
    public var skippedFileCount: Int
    /// Files the last pass could not send and left for later: they wait for
    /// the network, for iCloud, or for the server to answer again.
    public var leftForLaterFileCount: Int
    public var lastFailure: PhotoBackupFailure?
    public var lastPassFinishedAt: Date?
    /// Whether a pass is working now. When none is and files are queued, the
    /// system is sending them: iOS goes on with the app closed.
    public var isPassRunning: Bool
    /// Files still to send that are not queued with the session while no pass
    /// runs. They wait for the app to be opened, or for the night: files in
    /// iCloud, and files that did not fit in the queue.
    public var waitingFileCount: Int
    /// While a pass lists the whole library: how many items it has gone
    /// through, out of `libraryItemCount`. Nil when the pass does not list the
    /// whole library (it reads only what changed since the last one).
    public var listedItemCount: Int?
    /// Whether a listing of the whole library has begun and not ended, which
    /// leaves photos the index does not know of yet: work left even when no
    /// file is waiting.
    public var isListingUnfinished: Bool
    /// How long the items still to back up are estimated to take, from how fast
    /// the last minutes went; nil until there is enough progress to go on, and
    /// when nothing is uploading.
    public var estimatedSecondsLeft: Int?

    public var isEnabled: Bool { phase != .off }

    /// The files settled so far, for "N of M files": never more than the files
    /// planned, which can fall behind while the library grows during a pass.
    public var shownFinishedFileCount: Int { min(finishedFileCount, plannedFileCount) }

    /// How much of the progress the system shows for a pass the listing of the
    /// library is, the files being the rest.
    public static let listingShareOfProgress = 0.3

    /// How far a pass has got, from 0 to 1, for the progress the system shows
    /// for a background task. A pass that lists the whole library counts the
    /// listing as `listingShareOfProgress` of the work and the files as the
    /// rest, so that the progress moves while the library is read and iOS does
    /// not take the task for stalled.
    public var progressFraction: Double {
        let fileFractionWhenNoneIsPlanned: Double
        var listingFraction = 0.0
        if let listedItemCount {
            listingFraction = libraryItemCount > 0 ? min(1, Double(listedItemCount) / Double(libraryItemCount)) : 0
            fileFractionWhenNoneIsPlanned = listingFraction >= 1 ? 1 : 0
        } else {
            fileFractionWhenNoneIsPlanned = 0
        }
        let fileFraction = plannedFileCount > 0 ? min(1, Double(finishedFileCount) / Double(plannedFileCount)) : fileFractionWhenNoneIsPlanned
        guard listedItemCount != nil else { return fileFraction }
        return Self.listingShareOfProgress * listingFraction + (1 - Self.listingShareOfProgress) * fileFraction
    }

    /// Files are queued with the system and no pass is working: iOS sends them
    /// with the app closed, and the screen says so. Not said while the files
    /// wait for the network, which is said on its own, or when something needs
    /// the person.
    public var isUploadingInBackground: Bool {
        !isPassRunning && queuedFileCount > 0 && (phase == .idle || phase == .uploading)
    }

    /// Files are left that are not queued and no pass is working: they wait for
    /// the app to be opened, or for the night while the phone charges.
    public var hasFilesWaitingForTheApp: Bool {
        !isPassRunning && waitingFileCount > 0 && (phase == .idle || phase == .uploading)
    }

    /// Whether the person paused backup.
    public var isPaused: Bool { phase == .paused }

    /// How many items of the library are not backed up yet.
    public var itemsLeftCount: Int { max(0, libraryItemCount - backedUpItemCount) }

    /// Files are going up now: a pass lists the library or sends files, or the
    /// system sends the files queued with it. Not while the backup waits for the
    /// network, needs the person, is off, or has nothing to send.
    public var isUploading: Bool {
        phase == .uploading || phase == .listing || isUploadingInBackground
    }

    /// Whether the screen says how long is left, or that it is being worked out:
    /// while files go up and items remain.
    public var shouldShowTimeLeft: Bool { isUploading && itemsLeftCount > 0 }

    public init(
        phase: Phase = .off, backedUpItemCount: Int = 0, libraryItemCount: Int = 0, plannedFileCount: Int = 0, finishedFileCount: Int = 0,
        queuedFileCount: Int = 0, refusedFileCount: Int = 0, skippedFileCount: Int = 0, leftForLaterFileCount: Int = 0,
        lastFailure: PhotoBackupFailure? = nil, lastPassFinishedAt: Date? = nil, isPassRunning: Bool = false, waitingFileCount: Int = 0,
        listedItemCount: Int? = nil, isListingUnfinished: Bool = false, estimatedSecondsLeft: Int? = nil
    ) {
        self.phase = phase
        self.backedUpItemCount = backedUpItemCount
        self.libraryItemCount = libraryItemCount
        self.plannedFileCount = plannedFileCount
        self.finishedFileCount = finishedFileCount
        self.queuedFileCount = queuedFileCount
        self.refusedFileCount = refusedFileCount
        self.skippedFileCount = skippedFileCount
        self.leftForLaterFileCount = leftForLaterFileCount
        self.lastFailure = lastFailure
        self.lastPassFinishedAt = lastPassFinishedAt
        self.isPassRunning = isPassRunning
        self.waitingFileCount = waitingFileCount
        self.listedItemCount = listedItemCount
        self.isListingUnfinished = isListingUnfinished
        self.estimatedSecondsLeft = estimatedSecondsLeft
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
    /// The pass did not start because backup is paused.
    public var wasPaused = false

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
        /// The exported file the upload reads, which tells an upload from a
        /// later one of the same file.
        let outgoingFilename: String
    }

    private static let findPhotosBatchSize = 500
    /// How many items of the library are read, asked about, planned and written
    /// to the index at a time. A few hundred keep the first upload seconds away
    /// whatever the library's size, and the items in memory few.
    public static let standardBatchSize = 300
    /// How long a file may take to come out of the photo library while the
    /// library is still being listed: a download from iCloud that is slow waits
    /// for a later pass instead of holding the listing.
    static let listingExportTimeoutSeconds: TimeInterval = 60
    /// After this many uploads in a row that the server failed on (not the
    /// network), nothing more is queued and the pass ends waiting.
    static let serverFailureLimit = 3
    /// After this many times in a row that a file's upload moved on, the next
    /// is counted as the server failing. An upload found somewhere else is
    /// ordinary once or twice (the app was cut off as a part arrived), but one
    /// that is never where it is said to be would otherwise start passes for
    /// ever.
    static let uploadMovedOnLimit = 3

    private let index: PhotoBackupIndex
    private let library: any PhotoBackupLibrary
    private let transfer: any PhotoBackupTransfer
    private let uploads: any PhotoBackupUploads
    private let lookup: (any PhotoBackupServerLookup)?
    private let network: any PhotoBackupNetwork
    private let outgoingDirectory: URL
    private let server: ServerAddress
    private let accountKey: String
    private let credential: PhotoBackupCredential
    private let budget: PhotoBackupBudget
    private let idleTimeoutSeconds: TimeInterval
    private let batchSize: Int
    /// How long one file may take to come out of the photo library before it is
    /// given up on for now.
    private let exportTimeoutSeconds: TimeInterval
    /// The free space on the volume the files are written to, read before each
    /// export; nil when it cannot be read.
    private let freeSpaceBytes: @Sendable () -> Int64?
    private let now: @Sendable () -> Date
    /// Waits out an export's time limit. A test replaces it, so that a file
    /// is given up on when the test says and not after a stretch of real time
    /// that a slow machine can overrun for the files that are not stuck.
    private let sleepForSeconds: @Sendable (TimeInterval) async -> Void
    /// Waits out the idle time of a pass that nothing is finishing in. A test
    /// replaces it for the same reason as `sleepForSeconds`: the pass should
    /// end idle when the test says so, not after a stretch of real time.
    private let sleepForIdleSeconds: @Sendable (TimeInterval) async -> Void
    private let fileManager: FileManager
    private let tailWriter: any PhotoBackupTailWriter

    private var outstanding: [String: Outstanding] = [:]
    private var waiters: [CheckedContinuation<Void, Never>] = []
    private var isRunning = false
    private var isStopRequested = false
    /// Whether the person paused backup: no pass starts and nothing is queued.
    private var isPaused: Bool
    private var settings = Settings(usesMobileData: false, includesHidden: false)
    private var phase: PhotoBackupStatus.Phase = .idle
    private var plannedFileCount = 0
    private var finishedFileCount = 0
    private var uploadedFileCount = 0
    private var refusedFileCount = 0
    private var leftForLaterFileCount = 0
    /// How many items the pass has gone through while it lists the whole
    /// library; nil when it does not.
    private var listedItemCount: Int?
    /// Uploads finished (any way) since the coordinator was made, which tells
    /// a wait that ended because one finished from one that ran out of time.
    private var completionCount = 0
    /// Uploads in a row the server answered with a failure; any other answer
    /// starts the count again.
    private var consecutiveServerFailureCount = 0
    /// Whether the server failed, or asked to wait, for a file this pass left
    /// for later: what the screen then says is waited for.
    private var hasServerHeldBackFiles = false
    /// Whether a small request made to queue a file (creating its upload, asking
    /// where it is) found no network. The rest of the pass does not try the other
    /// files, which would each wait for a timeout to find the same.
    private var hasLostNetwork = false
    /// Nothing is queued before this time: the server said, with a Retry-After,
    /// how long to wait. Nil when it asked for nothing.
    private var retryNotBefore: Date?
    /// How many times in a row a pass has stopped because the server kept
    /// failing, which lengthens the wait before the next try. An upload that
    /// works starts it again.
    private var serverFailureStopCount = 0
    /// The export in progress, which stopping ends at once: a download from
    /// iCloud cannot be cancelled, and the pass must not wait for it.
    private var currentExport: ExportRace?
    /// The size each file had when it was exported in this pass, so that one
    /// held back for want of room is not exported again to find out.
    private var exportedSizes: [String: Int64] = [:]
    /// The uploads this pass has dealt with (queued, refused, or left for
    /// later), so that the files still to send, read again after the listing,
    /// are not tried a second time in the same pass.
    private var dealtWithKeys: Set<String> = []
    /// Whether the listing of this pass ended and set `plannedFileCount` from
    /// the files the index still has to send, which already holds every file
    /// the rest of the pass will deal with.
    private var isPlannedFileCountFromIndex = false
    /// Fed the backed up count each time the status is read, which is at least
    /// once per file while a pass runs, and each time the screen refreshes.
    private var timeLeft = PhotoBackupTimeLeft()
    private var statusHandler: (@Sendable (PhotoBackupStatus) -> Void)?
    /// The write of the rest of a file in progress, which stopping ends at once
    /// so that a long video is not copied for a pass that is over.
    private var currentTailCancellation: TailCancellation?
    /// How many times in a row each file's upload was found to have moved on
    /// (see `PhotoBackupOutcome.uploadMovedOn`), by the file's key. A file that
    /// goes up starts its count again.
    private var uploadMovedOnCountByKey: [String: Int] = [:]

    /// Waits for the given time on the clock; what both waits use unless a test
    /// replaces them. It returns early when the task is cancelled.
    public static let sleepInRealTime: @Sendable (TimeInterval) async -> Void = { seconds in
        try? await Task.sleep(nanoseconds: UInt64(max(0, seconds) * 1_000_000_000))
    }

    public init(
        index: PhotoBackupIndex, library: any PhotoBackupLibrary, transfer: any PhotoBackupTransfer, uploads: any PhotoBackupUploads,
        lookup: (any PhotoBackupServerLookup)?, network: any PhotoBackupNetwork, outgoingDirectory: URL, server: ServerAddress, accountKey: String,
        credential: PhotoBackupCredential, budget: PhotoBackupBudget = .standard, idleTimeoutSeconds: TimeInterval = 300, exportTimeoutSeconds: TimeInterval = 30 * 60,
        freeSpaceBytes: @escaping @Sendable () -> Int64? = { nil }, now: @escaping @Sendable () -> Date = { Date() },
        sleepForSeconds: @escaping @Sendable (TimeInterval) async -> Void = PhotoBackupCoordinator.sleepInRealTime,
        sleepForIdleSeconds: @escaping @Sendable (TimeInterval) async -> Void = PhotoBackupCoordinator.sleepInRealTime,
        fileManager: FileManager = .default, batchSize: Int = PhotoBackupCoordinator.standardBatchSize, isPaused: Bool = false,
        tailWriter: any PhotoBackupTailWriter = PhotoBackupFileTailWriter()
    ) {
        self.isPaused = isPaused
        self.index = index
        self.library = library
        self.transfer = transfer
        self.uploads = uploads
        self.lookup = lookup
        self.network = network
        self.outgoingDirectory = outgoingDirectory
        self.server = server
        self.accountKey = accountKey
        self.credential = credential
        self.budget = budget
        self.idleTimeoutSeconds = idleTimeoutSeconds
        self.batchSize = max(1, batchSize)
        self.exportTimeoutSeconds = exportTimeoutSeconds
        self.freeSpaceBytes = freeSpaceBytes
        self.now = now
        self.sleepForSeconds = sleepForSeconds
        self.sleepForIdleSeconds = sleepForIdleSeconds
        self.fileManager = fileManager
        self.tailWriter = tailWriter
    }

    public func setStatusHandler(_ handler: (@Sendable (PhotoBackupStatus) -> Void)?) {
        statusHandler = handler
    }

    /// The status now, read from the index and the pass in progress.
    public func status() async -> PhotoBackupStatus {
        let summary = await index.summary()
        let isListingUnfinished = await index.listingProgress != nil
        var shownPhase = phase
        if summary.isSignInNeeded { shownPhase = .needsSignIn }
        if isPaused { shownPhase = .paused }
        // While a pass works it is the one queueing the files; with none, what
        // is not with the session waits for the app to be opened or the night.
        var waitingFileCount = 0
        if !isRunning { waitingFileCount = max(0, await index.pendingFileCount() - outstanding.count) }
        var shownStatus = PhotoBackupStatus(
            phase: shownPhase, backedUpItemCount: summary.backedUpItemCount, libraryItemCount: summary.libraryItemCount,
            plannedFileCount: plannedFileCount, finishedFileCount: finishedFileCount, queuedFileCount: outstanding.count,
            refusedFileCount: summary.refusedFileCount, skippedFileCount: summary.skippedFileCount, leftForLaterFileCount: leftForLaterFileCount,
            lastFailure: summary.lastFailure, lastPassFinishedAt: summary.lastPassFinishedAt, isPassRunning: isRunning,
            waitingFileCount: waitingFileCount, listedItemCount: listedItemCount, isListingUnfinished: isListingUnfinished)
        timeLeft.record(date: now(), backedUpItemCount: shownStatus.backedUpItemCount, isUploading: shownStatus.isUploading)
        shownStatus.estimatedSecondsLeft = timeLeft.estimatedSecondsLeft(itemsLeftCount: shownStatus.itemsLeftCount)
        return shownStatus
    }

    /// The status, after asking the session what it holds now: with no pass
    /// running, uploads go on and finish without this actor hearing of it until
    /// the app is woken, so the screen asks.
    public func refreshedStatus() async -> PhotoBackupStatus {
        if !isRunning { await adoptQueuedJobs() }
        return await status()
    }

    /// What the session still holds is the truth about what is in flight.
    private func adoptQueuedJobs() async {
        outstanding.removeAll()
        for job in await transfer.queuedJobs() {
            outstanding[job.key] = Outstanding(sizeBytes: job.sizeBytes, filename: job.filename, outgoingFilename: job.outgoingFilename)
        }
    }

    /// Asks a running pass to stop: nothing more is exported. Files already
    /// queued with the session stay there unless the caller cancels them.
    public func stop() {
        isStopRequested = true
        currentExport?.finish(.failure(CancellationError()))
        currentTailCancellation?.cancel()
        resumeWaiters()
    }

    public var isPassRunning: Bool { isRunning }

    /// Pauses backup until `resume()`: a pass that is working is asked to stop,
    /// and the uploads the session holds are cancelled so that the phone stops
    /// sending, along with the files exported for them. What went up stays in
    /// the index, and so does the place the listing got to. A file cut off
    /// part way goes on from the byte the server has when the pass after the
    /// pause reaches it (the index keeps the address of its upload), unless the
    /// server no longer has the upload.
    ///
    /// A pass that is working ends a moment after this returns, and may have been
    /// about to queue one more file: the caller waits for the pass and calls
    /// this again, which cancels whatever got in and clears the exported files.
    public func pause() async {
        isPaused = true
        stop()
        await transfer.cancelAll()
        outstanding.removeAll()
        if !isRunning { removeLeftoverFiles(keeping: []) }
        await publish()
    }

    /// Lets passes start again. The caller starts one.
    public func resume() async {
        isPaused = false
        await publish()
    }

    /// When the server asked a pass to try again, if it did and the time has
    /// not come: the app starts a pass then.
    public var pendingRetryTime: Date? {
        guard let retryNotBefore, retryNotBefore > now() else { return nil }
        return retryNotBefore
    }

    /// Runs one pass, which ends when every planned file has been queued and
    /// the session has finished them, or when stopped.
    public func runPass(settings: Settings) async -> PhotoBackupPassResult {
        var result = PhotoBackupPassResult()
        guard !isRunning else {
            result.wasAlreadyRunning = true
            return result
        }
        guard !isPaused else {
            result.wasPaused = true
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
        consecutiveServerFailureCount = 0
        hasServerHeldBackFiles = false
        hasLostNetwork = false
        exportedSizes = [:]
        dealtWithKeys = []
        isPlannedFileCountFromIndex = false
        listedItemCount = nil
        result = await performPass()
        result.uploadedFileCount = uploadedFileCount
        result.refusedFileCount = refusedFileCount
        result.leftForLaterFileCount = leftForLaterFileCount
        result.wasStopped = isStopRequested
        isRunning = false
        listedItemCount = nil
        await index.flush()
        await publish()
        return result
    }

    /// Queues more files that are on the phone already, smallest first, and
    /// stops at `deadline`. This is what the app does in the short time iOS
    /// gives it when uploads have finished in the background: it does not list
    /// the library, wait for uploads, or touch iCloud, so it ends in seconds
    /// and the session has more to send when the app is suspended again.
    /// Reads the items still to send a batch at a time, in the order they are to
    /// go, and no further than the deadline allows. Returns how many files were
    /// queued. Nothing is done while a pass runs, which queues files itself.
    @discardableResult
    public func topUp(settings: Settings, until deadline: Date) async -> Int {
        guard !isRunning, !isPaused else { return 0 }
        isRunning = true
        isStopRequested = false
        hasLostNetwork = false
        self.settings = settings
        var queuedCount = 0
        defer { isRunning = false }
        await index.adopt(accountKey: accountKey)
        guard library.access() == .full, pendingRetryTime == nil else { return 0 }
        let summary = await index.summary()
        guard !summary.isSignInNeeded else { return 0 }
        await adoptQueuedJobs()
        let pendingIdentifiers = await index.pendingLocalIdentifiers()
        var start = 0
        var isBudgetSpent = false
        batches: while start < pendingIdentifiers.count, !isBudgetSpent, !isStopRequested, deadline.timeIntervalSince(now()) > 0 {
            let batch = Array(pendingIdentifiers[start..<min(start + batchSize, pendingIdentifiers.count)])
            start += batch.count
            guard let items = try? await library.items(withLocalIdentifiers: batch, includesHidden: settings.includesHidden) else { break batches }
            // Smallest first: the most files in the least time. A file whose size
            // the library does not say goes last.
            let uploads = await index.plan(items: items, inFlight: Set(outstanding.keys))
                .filter(\.isLocallyAvailable)
                .enumerated()
                .sorted { first, second in
                    let firstSize = first.element.fileSizeBytes ?? Int64.max
                    let secondSize = second.element.fileSizeBytes ?? Int64.max
                    return firstSize != secondSize ? firstSize < secondSize : first.offset < second.offset
                }
                .map(\.element)
            queueing: for upload in uploads where !isStopRequested {
                let secondsLeft = deadline.timeIntervalSince(now())
                if secondsLeft <= 0 {
                    isBudgetSpent = true
                    break queueing
                }
                // The server failing, or no network to make an upload with, is not
                // a reason to try the next file in the seconds there are.
                if hasLostNetwork || consecutiveServerFailureCount >= Self.serverFailureLimit || pendingRetryTime != nil {
                    isBudgetSpent = true
                    break queueing
                }
                let currentBudget = budget.limited(toFreeSpaceBytes: freeSpaceBytes())
                if !currentBudget.allowsAnother(outstandingCount: outstanding.count, outstandingBytes: outstandingBytes) {
                    isBudgetSpent = true
                    break queueing
                }
                if let sizeBytes = upload.fileSizeBytes,
                    !currentBudget.allowsFile(sizeBytes: sizeBytes, outstandingCount: outstanding.count, outstandingBytes: outstandingBytes)
                {
                    isBudgetSpent = true
                    break queueing
                }
                // No download, on any network: this is no time for one.
                switch await queue(upload, timeoutSeconds: min(exportTimeoutSeconds, secondsLeft), mayUseExpensiveNetwork: false) {
                case .queued: queuedCount += 1
                case .mustWait:
                    isBudgetSpent = true
                    break queueing
                case .deferred, .refused, .alreadyUploaded: continue queueing
                }
                if outstanding.count >= currentBudget.maximumUploadCount {
                    isBudgetSpent = true
                    break queueing
                }
            }
        }
        await index.flush()
        await publish()
        return queuedCount
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
        // While the server's wait runs nothing is sent, so its last problem
        // still stands and stays on the screen.
        if pendingRetryTime == nil { await index.clearErrorUnlessSignInIsNeeded() }
        phase = .listing
        await publish()

        await adoptQueuedJobs()
        removeLeftoverFiles(keeping: Set(outstanding.values.map(\.outgoingFilename)))
        // Asked once: the network does not change between two files in a way
        // that matters, and each question costs a look at the path.
        let networkState = await network.state()

        // Reading the library: all of it the first time, in batches, with the
        // uploads of each batch queued before the next is read; after that
        // only what changed. Everything read is in the index as still to send.
        do {
            try await readLibrary(networkState: networkState, result: &result)
        } catch {
            await index.setError(failure: Self.failure(of: error))
            phase = .idle
            return result
        }

        // Then the files still to send, which the index lists in the order they
        // are to go (those of earlier passes and those that waited for room
        // included), a batch at a time.
        var isCutShort = false
        var isIdle = false
        if !isStopRequested {
            let pendingIdentifiers = await index.pendingLocalIdentifiers()
            var start = 0
            while start < pendingIdentifiers.count, !isStopRequested {
                let batch = Array(pendingIdentifiers[start..<min(start + batchSize, pendingIdentifiers.count)])
                start += batch.count
                let items: [PhotoBackupItem]
                do {
                    items = try await library.items(withLocalIdentifiers: batch, includesHidden: settings.includesHidden)
                } catch {
                    await index.setError(failure: Self.failure(of: error))
                    phase = .idle
                    return result
                }
                // Deleted on the phone before it went up: nothing to send.
                let found = Set(items.map(\.localIdentifier))
                let deletedIdentifiers = batch.filter { !found.contains($0) }
                // The server holds a part of the file of a photo deleted
                // mid-way, and nothing will continue it.
                let abandonedPaths = await index.partialUploadPaths(ofLocalIdentifiers: deletedIdentifiers)
                await index.forgetPending(of: deletedIdentifiers)
                for abandonedPath in abandonedPaths { cancelOnServer(uploadPath: abandonedPath) }
                let uploads = PhotoBackupPlanner.preferringLocalFiles(await index.plan(items: items, inFlight: Set(outstanding.keys)))
                    .filter { !dealtWithKeys.contains($0.key) }
                // Counted already when the listing of this pass ended: its count
                // is the files the index has to send, these among them.
                if !isPlannedFileCountFromIndex { plannedFileCount += uploads.count }
                result.plannedFileCount += uploads.count
                let outcome = await queueUploads(uploads, networkState: networkState, mayWait: true, result: &result)
                isIdle = outcome.isIdle
                // What was not queued, because the server kept failing or nothing
                // finished for a long time, is left for a later pass.
                if outcome.position < uploads.count, !isStopRequested {
                    let leftCount = uploads.count - outcome.position
                    result.deferredFileCount += leftCount
                    leftForLaterFileCount += leftCount
                }
                if outcome.position < uploads.count || outcome.isIdle {
                    isCutShort = true
                    // The batches not read yet wait too: one item at least each.
                    if !isStopRequested {
                        let unreadCount = pendingIdentifiers.count - start
                        result.deferredFileCount += unreadCount
                        leftForLaterFileCount += unreadCount
                    }
                    break
                }
            }
        }

        // Then the pass waits for the session to finish what is queued, but
        // not for days: when nothing finishes for a while the pass ends and
        // leaves the uploads to the session, which reports each one when it is
        // done, with the app closed or not.
        while !outstanding.isEmpty, !isStopRequested, !isIdle {
            phase = .uploading
            await publish()
            if await !waitForCompletion() { isIdle = true }
        }
        if !isStopRequested, !isIdle, !isCutShort {
            await index.finishPass(at: now())
        }
        // A pass that leaves files for later ends waiting for the network, not
        // idle: the files are still to send, and the screen must not say that
        // everything is backed up.
        if await index.summary().isSignInNeeded {
            phase = .needsSignIn
        } else if isIdle, !outstanding.isEmpty {
            // Uploads are still with the session. If the network does not let
            // them run (no network, or mobile data that is not allowed) say so.
            let current = await network.state()
            if !current.isSatisfied || (current.isExpensive && !settings.usesMobileData) {
                leftForLaterFileCount += outstanding.count
                phase = .waitingForNetwork
            } else {
                // The network lets them run: the system goes on sending what is
                // queued, with the app closed or not, and the rest is queued as
                // those finish. That is uploading, not waiting for Wi-Fi.
                phase = .uploading
            }
        } else if leftForLaterFileCount > 0 {
            phase = await phaseForFilesLeft()
        } else {
            phase = .idle
        }
        return result
    }

    /// What files a pass left for later wait for. On a network that lets
    /// them go, after the server failed or asked to wait, that is the server:
    /// saying Wi-Fi there sends the person looking at a network that works.
    private func phaseForFilesLeft() async -> PhotoBackupStatus.Phase {
        guard hasServerHeldBackFiles || pendingRetryTime != nil else { return .waitingForNetwork }
        let current = await network.state()
        let isNetworkUsable = current.isSatisfied && (settings.usesMobileData || !current.isExpensive)
        return isNetworkUsable ? .waitingForServer : .waitingForNetwork
    }

    private struct QueueingOutcome {
        /// How many of the uploads have been dealt with: queued, refused, or
        /// left for later.
        var position = 0
        /// Nothing finished for `idleTimeoutSeconds`, so the pass should not wait.
        var isIdle = false
    }

    /// Exports the files one after another and hands them to the session, as far
    /// as the budget allows. With `mayWait` it waits for an upload to finish
    /// when the budget is spent, and goes on; without, it stops there, and
    /// what it did not reach stays in the index as still to send.
    private func queueUploads(
        _ uploads: [PhotoBackupUpload], networkState: PhotoBackupNetworkState, mayWait: Bool, result: inout PhotoBackupPassResult
    ) async -> QueueingOutcome {
        var outcome = QueueingOutcome()
        let timeoutSeconds = mayWait ? exportTimeoutSeconds : min(exportTimeoutSeconds, Self.listingExportTimeoutSeconds)
        let uploadingPhase: PhotoBackupStatus.Phase = mayWait ? .uploading : .listing
        while outcome.position < uploads.count, !isStopRequested {
            let upload = uploads[outcome.position]
            if consecutiveServerFailureCount >= Self.serverFailureLimit {
                holdBackAfterServerFailures()
                break
            }
            if pendingRetryTime != nil || hasLostNetwork { break }
            // The room left on the phone is read before each file is exported,
            // since every file queued is a copy of it.
            let currentBudget = budget.limited(toFreeSpaceBytes: freeSpaceBytes())
            if !currentBudget.allowsAnother(outstandingCount: outstanding.count, outstandingBytes: outstandingBytes) {
                guard mayWait else { break }
                phase = uploadingPhase
                await publish()
                if await !waitForCompletion() {
                    outcome.isIdle = true
                    break
                }
                continue
            }
            // A file known to be too large for the room left is not exported
            // to find that out, which for a large video costs minutes.
            if let sizeBytes = upload.fileSizeBytes ?? exportedSizes[upload.key],
                !currentBudget.allowsFile(sizeBytes: sizeBytes, outstandingCount: outstanding.count, outstandingBytes: outstandingBytes)
            {
                guard mayWait else { break }
                phase = uploadingPhase
                await publish()
                if await !waitForCompletion() {
                    outcome.isIdle = true
                    break
                }
                continue
            }
            // A file that has to come down from iCloud uses the network, so on
            // a network that costs it waits, unless the person allowed it.
            if !upload.isLocallyAvailable, !settings.usesMobileData, networkState.isExpensive {
                result.deferredFileCount += 1
                leftForLaterFileCount += 1
                dealtWithKeys.insert(upload.key)
                outcome.position += 1
                continue
            }
            phase = uploadingPhase
            await publish()
            switch await queue(upload, timeoutSeconds: timeoutSeconds) {
            case .queued, .refused, .alreadyUploaded:
                dealtWithKeys.insert(upload.key)
                outcome.position += 1
            case .mustWait:
                guard mayWait else { return outcome }
                if await !waitForCompletion() {
                    outcome.isIdle = true
                }
            case .deferred:
                result.deferredFileCount += 1
                leftForLaterFileCount += 1
                dealtWithKeys.insert(upload.key)
                outcome.position += 1
            }
            if outcome.isIdle { break }
        }
        return outcome
    }

    /// Reads the library into the index. The first time, and after a change
    /// token that is too old to read changes from, that is all of it, newest
    /// photo first, a batch at a time; after that only what changed since.
    /// Throws when the library cannot be read.
    private func readLibrary(networkState: PhotoBackupNetworkState, result: inout PhotoBackupPassResult) async throws {
        let includesHidden = settings.includesHidden
        // A listing a pass began and did not end goes on from where it stopped.
        if let listing = await index.listingProgress {
            try await listLibrary(resuming: listing, networkState: networkState, result: &result)
            return
        }
        if let token = await index.changeToken {
            var changes: PhotoBackupChanges?
            do {
                changes = try await library.changes(since: token, includesHidden: includesHidden)
            } catch PhotoBackupLibraryError.changeTokenExpired {
                // Too old: read everything again, skipping what the index has.
                changes = nil
            }
            if let changes {
                await index.setLibraryItemCount(changes.totalCount)
                var start = 0
                while start < changes.localIdentifiers.count, !isStopRequested {
                    let batch = Array(changes.localIdentifiers[start..<min(start + batchSize, changes.localIdentifiers.count)])
                    start += batch.count
                    let items = try await library.items(withLocalIdentifiers: batch, includesHidden: includesHidden)
                    // Before the rest: these are the newest photos, and what the
                    // person looks for first.
                    _ = await take(items, goesBeforeTheRest: true, shouldWriteAtOnce: true)
                }
                // Every file of what changed is now in the index as still to
                // send, so a pass that ends early, or a file that has to wait,
                // loses nothing: that is what lets the token move on here.
                if !isStopRequested, let newToken = changes.changeToken { await index.setChangeToken(newToken) }
                return
            }
        }
        try await listLibrary(resuming: nil, networkState: networkState, result: &result)
    }

    /// Lists all of the library, newest photo first, a batch at a time. For
    /// each batch it builds the items of the photos the index has no record of,
    /// asks the server which it has, plans, writes the rest to the index as
    /// still to send, and queues what the budget allows, before the next batch
    /// is read. Where it stopped is kept in the index, so that a pass that ends
    /// early is followed by one that goes on from there. The library's change
    /// token is kept only when the last batch is done.
    private func listLibrary(
        resuming existing: PhotoBackupListingProgress?, networkState: PhotoBackupNetworkState, result: inout PhotoBackupPassResult
    ) async throws {
        let includesHidden = settings.includesHidden
        var listedCount = 0
        var totalCount = 0
        var resumePoint: PhotoBackupResumePoint?
        if let existing {
            listedCount = existing.listedItemCount
            totalCount = existing.libraryItemCount
            resumePoint = existing.resumePoint
        } else {
            // Taken before the first batch is read, so that whatever changes
            // while the listing runs is read again by a later pass.
            let start = try await library.beginListing(includesHidden: includesHidden)
            totalCount = start.totalCount
            await index.beginListing(changeToken: start.changeToken, libraryItemCount: start.totalCount)
        }
        listedItemCount = listedCount
        await publish()
        while !isStopRequested {
            let batch = try await library.listBatch(after: resumePoint, limit: batchSize, includesHidden: includesHidden)
            // A batch holds a whole moment, so it can hold far more than
            // `batchSize` items (a burst of thousands made at one instant): it
            // is worked through a batch's worth at a time, so that the items
            // built at once, and the uploads waiting to be queued, stay as few
            // as in any other batch.
            let isEnd = batch.isLast || batch.entries.isEmpty
            let chunkStarts = batch.entries.isEmpty ? [0] : Array(stride(from: 0, to: batch.entries.count, by: batchSize))
            for chunkStart in chunkStarts {
                let chunk = Array(batch.entries[chunkStart..<min(chunkStart + batchSize, batch.entries.count)])
                // Items are built for the photos the index has no record of: the
                // others went up, or were planned, in an earlier pass.
                let newIdentifiers = await index.localIdentifiersWithoutRecord(among: chunk.map(\.localIdentifier))
                let items =
                    newIdentifiers.isEmpty
                    ? [] : try await library.items(withLocalIdentifiers: newIdentifiers, includesHidden: includesHidden)
                let uploads = await take(items, goesBeforeTheRest: false, shouldWriteAtOnce: false)
                let isLastChunk = chunkStart + batchSize >= batch.entries.count
                if isLastChunk {
                    // The resume point is saved once the whole batch is in the
                    // index. A pass stopped inside a moment saves none, and the
                    // next one lists the moment again, skipping what the index
                    // has by then.
                    let lastResumePoint = batch.entries.compactMap { entry in
                        entry.creationDate.map { PhotoBackupResumePoint(creationDate: $0) }
                    }.last
                    await index.recordListed(batchItemCount: batch.entries.count, resumePoint: lastResumePoint)
                    resumePoint = lastResumePoint ?? resumePoint
                    listedCount += batch.entries.count
                    listedItemCount = isEnd ? max(listedCount, totalCount) : min(listedCount, totalCount)
                    await publish()
                }
                // The uploads of this batch start before the next batch is read.
                let outcome = await queueUploads(uploads, networkState: networkState, mayWait: false, result: &result)
                plannedFileCount += outcome.position
                result.plannedFileCount += outcome.position
                if !isLastChunk, isStopRequested { return }
            }
            if isEnd {
                await index.finishListing()
                if listedCount < totalCount {
                    NSLog("Photo backup: the listing ended after %ld of %ld items the library counted", listedCount, totalCount)
                }
                // The count of files this pass set out to send has so far been
                // the ones queued, which left the bar near its end while most
                // of the library was still to go. From here it is what is done
                // and what the index still has to send.
                let pendingFileCount = await index.pendingFileCount()
                plannedFileCount = finishedFileCount + pendingFileCount
                isPlannedFileCountFromIndex = true
                return
            }
        }
    }

    /// Takes items read from the library into the index: asks the server which
    /// of the photos it has, plans the rest, and writes their files as still to
    /// send. Returns the uploads planned, newest photo first.
    private func take(_ items: [PhotoBackupItem], goesBeforeTheRest: Bool, shouldWriteAtOnce: Bool) async -> [PhotoBackupUpload] {
        await askServerWhichPhotosItHas(items)
        let uploads = PhotoBackupPlanner.preferringLocalFiles(await index.plan(items: items, inFlight: Set(outstanding.keys)))
        await index.markPending(uploads, goesBeforeTheRest: goesBeforeTheRest, shouldWriteAtOnce: shouldWriteAtOnce)
        return uploads
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
        /// The server turned out to have the whole file already, from an earlier
        /// try that was not heard to end: nothing is sent.
        case alreadyUploaded
        /// Too much is in flight to take this file now.
        case mustWait
        case deferred
        case refused
    }

    /// What is sent for one file, and from which file in the outgoing folder.
    private struct SendPlan {
        let request: URLRequest
        let outgoingFilename: String
        let sizeBytes: Int64
    }

    private enum Preparation {
        case send(SendPlan)
        /// The server has every byte and the photo exists.
        case alreadyUploaded(serverPhotoId: String)
        /// The server did not answer as needed, or the app was stopped.
        case notSent(PhotoBackupOutcome)
    }

    /// Exports one file and hands it to the session. A file that the library
    /// does not give within `timeoutSeconds` is left for later, so that one
    /// download from iCloud that never finishes cannot hold up the whole pass.
    private func queue(_ upload: PhotoBackupUpload, timeoutSeconds: TimeInterval, mayUseExpensiveNetwork: Bool? = nil) async -> QueueResult {
        let exported: PhotoBackupExportedFile
        do {
            exported = try await export(
                upload, timeoutSeconds: timeoutSeconds, mayUseExpensiveNetwork: mayUseExpensiveNetwork ?? settings.usesMobileData)
        } catch is CancellationError {
            // Stopped while the file was being read: nothing to show.
            return .deferred
        } catch PhotoBackupLibraryError.unavailable(let failure) {
            // Shown, so that a full disk or a locked phone is not a silence.
            // A file that waits for a network that is allowed to cost nothing
            // is the usual wait, not a problem.
            if failure != .fileIsInICloudOnMobileData { await index.setError(failure: failure) }
            return .deferred
        } catch PhotoBackupLibraryError.failed(let failure) {
            await index.markRefused(localIdentifier: upload.localIdentifier, clientPhotoId: upload.clientPhotoId, role: upload.role, failure: failure)
            refusedFileCount += 1
            finishedFileCount += 1
            await index.setError(failure: failure)
            return .refused
        } catch {
            await index.setError(failure: Self.failure(of: error))
            return .deferred
        }
        exportedSizes[upload.key] = exported.sizeBytes
        if isStopRequested {
            removeTemporaryFile(named: exported.outgoingFilename)
            return .deferred
        }
        let currentBudget = budget.limited(toFreeSpaceBytes: freeSpaceBytes())
        if !currentBudget.allowsFile(sizeBytes: exported.sizeBytes, outstandingCount: outstanding.count, outstandingBytes: outstandingBytes) {
            removeTemporaryFile(named: exported.outgoingFilename)
            return .mustWait
        }
        let plan: SendPlan
        switch await prepareSending(upload, exported: exported) {
        case .send(let prepared):
            plan = prepared
        case .alreadyUploaded(let serverPhotoId):
            removeTemporaryFile(named: exported.outgoingFilename)
            consecutiveServerFailureCount = 0
            uploadMovedOnCountByKey[upload.key] = nil
            uploadedFileCount += 1
            finishedFileCount += 1
            await index.markUploaded(upload: upload, serverPhotoId: serverPhotoId)
            return .alreadyUploaded
        case .notSent(let outcome):
            removeTemporaryFile(named: exported.outgoingFilename)
            return await settleUnsent(outcome, upload: upload)
        }
        if isStopRequested {
            removeTemporaryFile(named: plan.outgoingFilename)
            return .deferred
        }
        let job = PhotoBackupJob(
            accountKey: accountKey, localIdentifier: upload.localIdentifier, role: upload.role, clientPhotoId: upload.clientPhotoId,
            filename: upload.filename, assetModificationDate: upload.assetModificationDate, outgoingFilename: plan.outgoingFilename,
            sizeBytes: plan.sizeBytes)
        outstanding[job.key] = Outstanding(sizeBytes: plan.sizeBytes, filename: upload.filename, outgoingFilename: plan.outgoingFilename)
        do {
            try await transfer.start(request: plan.request, job: job, fileURL: outgoingDirectory.appendingPathComponent(plan.outgoingFilename))
        } catch {
            outstanding[job.key] = nil
            removeTemporaryFile(named: plan.outgoingFilename)
            await index.setError(failure: Self.failure(of: error))
            return .deferred
        }
        return .queued
    }

    /// Settles with the server where a file's upload stands and makes the
    /// request that sends what the server lacks.
    ///
    /// A file the index holds an upload for, with the size and digest the file
    /// has now, is continued: the server is asked how many bytes it has (it
    /// counts whole parts it has stored, so a request cut off in the middle of a
    /// part costs that part), and only the rest is sent. An upload the server no
    /// longer has, or one for a file that is not this file any more, is dropped
    /// and the file goes up as a new upload from its first byte. Every file
    /// goes up as an upload made first, with a small request of its own, because
    /// the address of an upload sent as one request with its bytes is not heard
    /// until the request ends, which is too late to continue it.
    ///
    /// The app may be stopped (paused, turned off) while any of the requests
    /// here waits for its answer, so it is asked again before each step that
    /// would leave something behind: a new upload, a file written, a task.
    private func prepareSending(_ upload: PhotoBackupUpload, exported: PhotoBackupExportedFile) async -> Preparation {
        let digest = PhotoBackupRequest.declaredDigest(exported.sha256)
        let usesMobileData = settings.usesMobileData
        var uploadPath: String?
        var offsetBytes: Int64 = 0
        if let kept = await index.partialUpload(localIdentifier: upload.localIdentifier, role: upload.role) {
            // Whether the server may still hold the upload, and so is told to
            // drop it when the address is forgotten.
            var isPossiblyOnServer = true
            if let digest, kept.sha256 == digest, kept.sizeBytes == exported.sizeBytes {
                let statusRequest = PhotoBackupRequest.makeStatus(
                    server: server, credential: credential, uploadPath: kept.uploadPath, usesMobileData: usesMobileData)
                switch await uploads.uploadState(statusRequest) {
                case .answered(.receiving(let receivedBytes)) where receivedBytes <= exported.sizeBytes:
                    uploadPath = kept.uploadPath
                    offsetBytes = receivedBytes
                case .answered(.complete(let serverPhotoId)):
                    return .alreadyUploaded(serverPhotoId: serverPhotoId)
                case .unsuccessful(let outcome):
                    // A refusal of this question is not a refusal of the file:
                    // the upload is dropped and a new one made, whose request
                    // has a body to read the reason from (a HEAD has none).
                    // Anything else (no network, a sign-in refused, the server
                    // failing) is why nothing is sent now.
                    guard case .refused = outcome else { return .notSent(outcome) }
                case .answered(.gone):
                    isPossiblyOnServer = false
                case .answered(.receiving):
                    break
                }
            }
            if uploadPath == nil {
                await forgetKeptUpload(of: upload, uploadPath: kept.uploadPath, isPossiblyOnServer: isPossiblyOnServer)
            }
        }
        if let keptPath = uploadPath, offsetBytes > 0, !PhotoBackupBudget.allowsTail(sizeBytes: exported.sizeBytes - offsetBytes, freeSpaceBytes: freeSpaceBytes()) {
            uploadPath = nil
            offsetBytes = 0
            await forgetKeptUpload(of: upload, uploadPath: keptPath, isPossiblyOnServer: true)
        }
        if isStopRequested { return .notSent(.cancelled) }
        if uploadPath == nil {
            switch await makeUpload(for: upload, exported: exported, digest: digest) {
            case .answered(let createdPath): uploadPath = createdPath
            case .unsuccessful(let outcome): return .notSent(outcome)
            }
        }
        guard let uploadPath else { return .notSent(.retryLater(failure: nil, isConnectivityFailure: false)) }
        if isStopRequested { return .notSent(.cancelled) }
        if offsetBytes == exported.sizeBytes {
            // The server has every byte and has not completed the upload. The
            // request that completes it has no body, and a background task for
            // a file of no bytes may not run, so it is made here, as a plain
            // request. If it gets no answer the server completes the upload
            // all the same, and the next HEAD says so.
            let completion = PhotoBackupRequest.makeCompletion(
                server: server, credential: credential, uploadPath: uploadPath, sizeBytes: exported.sizeBytes, usesMobileData: usesMobileData)
            switch await uploads.finishUpload(completion) {
            case .uploaded(let serverPhotoId): return .alreadyUploaded(serverPhotoId: serverPhotoId)
            case let outcome: return .notSent(outcome)
            }
        }
        guard offsetBytes > 0 else {
            return .send(sendingWholeFile(exported, uploadPath: uploadPath))
        }
        // Background uploads read a file, so the rest is written to one, and
        // the whole file is deleted as soon as it is: only the rest stays on the
        // phone while it is sent.
        let tailFilename = Self.tailFilename(of: exported.outgoingFilename)
        let exportedURL = outgoingDirectory.appendingPathComponent(exported.outgoingFilename)
        let tailURL = outgoingDirectory.appendingPathComponent(tailFilename)
        do {
            try await writeTail(of: exportedURL, from: offsetBytes, to: tailURL)
        } catch is CancellationError {
            removeTemporaryFile(named: tailFilename)
            return .notSent(.cancelled)
        } catch {
            // A full disk or a file that cannot be read again will fail the
            // same way at every pass if the rest is tried again. The upload has
            // bytes only a tail could continue from, so it is dropped, and the
            // whole file, still here, goes up as an upload of its own.
            NSLog("Photo backup: the rest of a file could not be written, so the file is sent again from its first byte")
            removeTemporaryFile(named: tailFilename)
            await forgetKeptUpload(of: upload, uploadPath: uploadPath, isPossiblyOnServer: true)
            if isStopRequested { return .notSent(.cancelled) }
            switch await makeUpload(for: upload, exported: exported, digest: digest) {
            case .answered(let createdPath): return .send(sendingWholeFile(exported, uploadPath: createdPath))
            case .unsuccessful(let outcome): return .notSent(outcome)
            }
        }
        removeTemporaryFile(named: exported.outgoingFilename)
        let request = PhotoBackupRequest.makeContinuation(
            server: server, credential: credential, uploadPath: uploadPath, offsetBytes: offsetBytes, usesMobileData: usesMobileData)
        return .send(SendPlan(request: request, outgoingFilename: tailFilename, sizeBytes: exported.sizeBytes - offsetBytes))
    }

    /// The request that sends all of the exported file to a new upload.
    private func sendingWholeFile(_ exported: PhotoBackupExportedFile, uploadPath: String) -> SendPlan {
        let request = PhotoBackupRequest.makeContinuation(
            server: server, credential: credential, uploadPath: uploadPath, offsetBytes: 0, usesMobileData: settings.usesMobileData)
        return SendPlan(request: request, outgoingFilename: exported.outgoingFilename, sizeBytes: exported.sizeBytes)
    }

    /// Makes the upload a file goes up in, and keeps its address (for a file
    /// that has a digest: an exported file that cannot be told from the one the
    /// server has a part of is not continued).
    private func makeUpload(for upload: PhotoBackupUpload, exported: PhotoBackupExportedFile, digest: String?) async -> PhotoBackupServiceAnswer<String> {
        let creationRequest = PhotoBackupRequest.makeCreation(
            server: server, credential: credential, upload: upload, sizeBytes: exported.sizeBytes, usesMobileData: settings.usesMobileData,
            sha256: digest)
        let answer = await uploads.createUpload(creationRequest)
        if case .answered(let createdPath) = answer, let digest {
            await index.markUploadStarted(
                localIdentifier: upload.localIdentifier, clientPhotoId: upload.clientPhotoId, role: upload.role,
                partialUpload: PhotoBackupPartialUpload(uploadPath: createdPath, sizeBytes: exported.sizeBytes, sha256: digest))
        }
        return answer
    }

    /// Forgets the address kept for a file, and when the server may still hold
    /// the upload asks it to drop it.
    private func forgetKeptUpload(of upload: PhotoBackupUpload, uploadPath: String, isPossiblyOnServer: Bool) async {
        await index.forgetPartialUpload(localIdentifier: upload.localIdentifier, role: upload.role)
        if isPossiblyOnServer { cancelOnServer(uploadPath: uploadPath) }
    }

    /// Asks the server to drop an upload the app will not continue. It is not
    /// waited for and its failure is not looked at: an upload nobody continues
    /// is cleaned up by the server in a week, and this only gives the room back
    /// sooner.
    private func cancelOnServer(uploadPath: String) {
        let request = PhotoBackupRequest.makeCancellation(
            server: server, credential: credential, uploadPath: uploadPath, usesMobileData: settings.usesMobileData)
        let uploads = uploads
        Task.detached { await uploads.cancelUpload(request) }
    }

    /// Asks the server to drop every upload the index holds an address for and
    /// forgets the addresses: what the app does when backup is turned off or the
    /// account goes, after which nothing will continue them. Waits for the
    /// requests (they are made together and are short), so that they are sent
    /// before the device password they carry is revoked.
    public func abandonKeptUploads() async {
        let paths = await index.allPartialUploadPaths()
        await index.forgetAllPartialUploads()
        await index.flush()
        let requests = paths.map {
            PhotoBackupRequest.makeCancellation(server: server, credential: credential, uploadPath: $0, usesMobileData: settings.usesMobileData)
        }
        let uploads = uploads
        await withTaskGroup(of: Void.self) { group in
            for request in requests { group.addTask { await uploads.cancelUpload(request) } }
        }
    }

    /// The name the rest of an exported file is written under, beside it in the
    /// outgoing folder, where a file nothing refers to is deleted by the next
    /// pass.
    static func tailFilename(of outgoingFilename: String) -> String {
        outgoingFilename + ".tail"
    }

    /// Writes the rest of a file beside it, on a task of its own so that the
    /// actor stays free to hear of finished uploads. Stopping, and the
    /// cancellation of the pass that asked, both end the write between two
    /// pieces; what was written is deleted by the writer.
    private func writeTail(of sourceURL: URL, from offsetBytes: Int64, to destinationURL: URL) async throws {
        let writer = tailWriter
        let cancellation = TailCancellation()
        currentTailCancellation = cancellation
        defer { currentTailCancellation = nil }
        try await withTaskCancellationHandler {
            try await Task.detached {
                try writer.writeTail(of: sourceURL, from: offsetBytes, to: destinationURL, isCancelled: { cancellation.isCancelled })
            }.value
        } onCancel: {
            cancellation.cancel()
        }
    }

    /// What follows when the server's answer to a small request, made before a
    /// file was queued, says it cannot be queued now. The file stays in the index
    /// as still to send, as when a queued upload fails.
    private func settleUnsent(_ outcome: PhotoBackupOutcome, upload: PhotoBackupUpload) async -> QueueResult {
        switch outcome {
        case .uploaded, .cancelled, .uploadMovedOn:
            // Moved on is what the request that completes an upload says when
            // the upload is being completed already or is gone: the question
            // about it is asked again by the next pass.
            return .deferred
        case .signInNeeded:
            await noteSignInNeeded()
            return .deferred
        case .refused(let failure):
            await noteRefused(
                localIdentifier: upload.localIdentifier, clientPhotoId: upload.clientPhotoId, role: upload.role, failure: failure)
            return .refused
        case .retryLater(let failure, let isConnectivityFailure, let retryAfterSeconds):
            // Without a network none of the files after this one can be made an
            // upload either, and each would wait out its timeout to find so.
            if isConnectivityFailure { hasLostNetwork = true }
            await noteRetryLater(failure: failure, isConnectivityFailure: isConnectivityFailure, retryAfterSeconds: retryAfterSeconds)
            return .deferred
        }
    }

    /// Asks the library for the file and waits for it at most `timeoutSeconds`.
    /// PhotoKit cannot cancel a download, so the wait is raced against a timer,
    /// and a file that arrives after the timer is deleted.
    private func export(_ upload: PhotoBackupUpload, timeoutSeconds: TimeInterval, mayUseExpensiveNetwork: Bool) async throws -> PhotoBackupExportedFile {
        let library = library
        let directory = outgoingDirectory
        let includesHidden = settings.includesHidden
        let sleepForSeconds = sleepForSeconds
        let race = ExportRace(removingLateFilesIn: directory)
        currentExport = race
        defer { currentExport = nil }
        return try await withCheckedThrowingContinuation { continuation in
            race.begin(continuation)
            // Tasks of their own, not the actor's: the actor must stay free to
            // hear of finished uploads while a file is being read.
            Task.detached {
                do {
                    let file = try await library.export(
                        upload, to: directory, mayUseExpensiveNetwork: mayUseExpensiveNetwork, includesHidden: includesHidden)
                    race.finish(.success(file))
                } catch {
                    race.finish(.failure(error))
                }
            }
            race.keep(
                timer: Task.detached {
                    await sleepForSeconds(timeoutSeconds)
                    guard !Task.isCancelled else { return }
                    race.finish(.failure(PhotoBackupLibraryError.unavailable(.fileTookTooLongToRead)))
                })
        }
    }

    /// After this many failures in a row the server is not asked again for
    /// `minimumServerFailureWaitSeconds`, then twice as long each time, up to
    /// `maximumServerFailureWaitSeconds`.
    static let minimumServerFailureWaitSeconds = 60.0
    static let maximumServerFailureWaitSeconds = 15.0 * 60

    /// A pass that stopped because the server failed several times in a row
    /// says when to try again, so that the app does it by itself instead of
    /// waiting to be opened. A longer wait the server asked for stands.
    private func holdBackAfterServerFailures() {
        let wait = min(Self.minimumServerFailureWaitSeconds * pow(2, Double(min(serverFailureStopCount, 10))), Self.maximumServerFailureWaitSeconds)
        serverFailureStopCount += 1
        let notBefore = now().addingTimeInterval(wait)
        retryNotBefore = max(retryNotBefore ?? notBefore, notBefore)
        consecutiveServerFailureCount = 0
        hasServerHeldBackFiles = true
    }

    /// The session reports a finished upload. It may arrive while no pass runs,
    /// when the app was started to hear of it.
    ///
    /// Returns whether the app should start a pass now: an upload went up, no
    /// pass is running, and files are still to send; or an upload moved on (see
    /// `PhotoBackupOutcome.uploadMovedOn`) and the file is to be asked about at
    /// once. A file the server refused or asked to wait for does not start one
    /// (a file that always fails would otherwise start passes as fast as the
    /// server answers): those are tried again when the time the server asked
    /// for has come, when the app is opened, or overnight.
    @discardableResult
    public func finished(_ job: PhotoBackupJob, outcome: PhotoBackupOutcome) async -> Bool {
        var hasMadeProgress = false
        var hasFoundUploadMovedOn = false
        // Only the entry of this upload: a cancelled task reports late, and by
        // then the same file may have been queued again under the same key.
        if outstanding[job.key]?.outgoingFilename == job.outgoingFilename { outstanding[job.key] = nil }
        removeTemporaryFile(named: job.outgoingFilename)
        switch outcome {
        case .cancelled:
            break
        case .uploaded(let serverPhotoId):
            consecutiveServerFailureCount = 0
            serverFailureStopCount = 0
            retryNotBefore = nil
            uploadMovedOnCountByKey[job.key] = nil
            hasMadeProgress = true
            // Counted before the index is written: the upload has left the
            // outstanding list, and a pass waiting on that list may end while
            // this waits for the index, short of the upload that let it end.
            uploadedFileCount += 1
            finishedFileCount += 1
            await index.markUploaded(
                localIdentifier: job.localIdentifier, clientPhotoId: job.clientPhotoId, role: job.role, serverPhotoId: serverPhotoId,
                assetModificationDate: job.assetModificationDate)
        case .signInNeeded:
            await noteSignInNeeded()
        case .refused(let failure):
            await noteRefused(localIdentifier: job.localIdentifier, clientPhotoId: job.clientPhotoId, role: job.role, failure: failure)
        case .uploadMovedOn:
            // The upload is somewhere else than the request said (the server
            // has more bytes, or has dropped it). That is no error to show and
            // no failure to count, and unlike a failure it is worth asking
            // about at once: the next pass asks where the upload is. Not for
            // ever, though: one that is never where it is said to be is
            // counted as the server failing.
            let movedOnCount = (uploadMovedOnCountByKey[job.key] ?? 0) + 1
            uploadMovedOnCountByKey[job.key] = movedOnCount
            if movedOnCount <= Self.uploadMovedOnLimit {
                hasFoundUploadMovedOn = true
            } else {
                await noteRetryLater(failure: nil, isConnectivityFailure: false, retryAfterSeconds: nil)
                leftForLaterFileCount += 1
                if !isRunning { phase = .waitingForServer }
            }
        case .retryLater(let failure, let isConnectivityFailure, let retryAfterSeconds):
            await noteRetryLater(failure: failure, isConnectivityFailure: isConnectivityFailure, retryAfterSeconds: retryAfterSeconds)
            // The file is still to send and nothing in this pass sends it
            // again: it is counted, so that the pass ends waiting for the
            // network, and one that finished with no pass running puts the
            // screen there too.
            leftForLaterFileCount += 1
            if !isRunning { phase = isConnectivityFailure ? .waitingForNetwork : .waitingForServer }
        }
        // With no pass running nobody writes the index afterwards (an upload
        // the system finished while the app was suspended): what this upload
        // came to is written now. During a pass the index is written at most
        // every `PhotoBackupIndex.secondsBetweenWrites`, when the pass ends,
        // and when the session has delivered its events or the app goes to the
        // background, so a pass does not rewrite it for every file.
        if !isRunning { await index.flush() }
        // Counted in the same step that wakes the waiters, with no await in
        // between. The awaits above let the pass run: if it began a wait after
        // the count had moved but before the wake, it would see no change when
        // woken, take that for a timeout and end the pass idle with files left.
        completionCount += 1
        resumeWaiters()
        await publish()
        // A pass that is running will not send a file again that it has dealt
        // with, so for an upload that moved on the answer is yes then too: the
        // app runs one more pass when this one ends.
        guard (hasMadeProgress && !isRunning) || hasFoundUploadMovedOn, !isStopRequested, pendingRetryTime == nil else { return false }
        return await index.pendingFileCount() > outstanding.count
    }

    // What a file that did not go up comes to, whether the answer was to its
    // upload or to the small request made before it was queued.

    private func noteSignInNeeded() async {
        await index.setError(failure: .signInRefused, isSignInNeeded: true)
        isStopRequested = true
        await transfer.cancelAll()
        outstanding.removeAll()
    }

    private func noteRefused(localIdentifier: String, clientPhotoId: String, role: PhotoBackupFileRole, failure: PhotoBackupFailure) async {
        consecutiveServerFailureCount = 0
        refusedFileCount += 1
        finishedFileCount += 1
        await index.markRefused(localIdentifier: localIdentifier, clientPhotoId: clientPhotoId, role: role, failure: failure)
        await index.setError(failure: failure)
    }

    private func noteRetryLater(failure: PhotoBackupFailure?, isConnectivityFailure: Bool, retryAfterSeconds: Int?) async {
        if !isConnectivityFailure {
            consecutiveServerFailureCount += 1
            hasServerHeldBackFiles = true
        }
        if let retryAfterSeconds {
            let notBefore = now().addingTimeInterval(TimeInterval(retryAfterSeconds))
            retryNotBefore = max(retryNotBefore ?? notBefore, notBefore)
        }
        if let failure, !isConnectivityFailure { await index.setError(failure: failure) }
    }

    // MARK: Waiting

    private var outstandingBytes: Int64 {
        outstanding.values.reduce(0) { $0 + $1.sizeBytes }
    }

    /// Suspends until an upload finishes or the pass is stopped; at once if
    /// nothing is outstanding, as nothing would wake it. Returns false when
    /// nothing finished for `idleTimeoutSeconds`: the uploads are waiting for
    /// something (the network, the server) and the pass should not.
    private func waitForCompletion() async -> Bool {
        if isStopRequested || outstanding.isEmpty { return true }
        let completionCountBefore = completionCount
        let seconds = idleTimeoutSeconds
        let sleepForIdleSeconds = sleepForIdleSeconds
        let timer = Task {
            await sleepForIdleSeconds(seconds)
            guard !Task.isCancelled else { return }
            await self.resumeWaiters()
        }
        await withCheckedContinuation { continuation in
            waiters.append(continuation)
        }
        timer.cancel()
        return isStopRequested || completionCount != completionCountBefore
    }

    /// Whether the pass is suspended until an upload finishes. Tests wait on
    /// this instead of a delay, since it is the moment the pass has decided it
    /// has no room for the next file and will not look again until one ends.
    var isWaitingForAnUploadToFinish: Bool { !waiters.isEmpty }

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

/// TailCancellation tells the write of the rest of a file to stop. It is read
/// from the task that writes and set from the actor, so it has a lock.
private final class TailCancellation: @unchecked Sendable {
    private let lock = NSLock()
    private var isStopped = false

    var isCancelled: Bool { lock.withLock { isStopped } }

    func cancel() {
        lock.withLock { isStopped = true }
    }
}

/// ExportRace is the one answer to a request for a file from the photo
/// library: the file, the library's error, or a timeout, whichever comes
/// first. The others are ignored, and a file that comes after the answer is
/// deleted, since nothing will send it.
private final class ExportRace: @unchecked Sendable {
    private let lock = NSLock()
    private let directory: URL
    private var continuation: CheckedContinuation<PhotoBackupExportedFile, Error>?
    private var timer: Task<Void, Never>?
    private var isFinished = false

    init(removingLateFilesIn directory: URL) {
        self.directory = directory
    }

    func begin(_ continuation: CheckedContinuation<PhotoBackupExportedFile, Error>) {
        lock.withLock { self.continuation = continuation }
    }

    func keep(timer: Task<Void, Never>) {
        let isAlreadyFinished = lock.withLock {
            if !isFinished { self.timer = timer }
            return isFinished
        }
        if isAlreadyFinished { timer.cancel() }
    }

    func finish(_ result: Result<PhotoBackupExportedFile, Error>) {
        let (waiting, timer) = lock.withLock { () -> (CheckedContinuation<PhotoBackupExportedFile, Error>?, Task<Void, Never>?) in
            guard !isFinished else { return (nil, nil) }
            isFinished = true
            defer { continuation = nil }
            return (continuation, self.timer)
        }
        guard let waiting else {
            if case .success(let file) = result {
                try? FileManager.default.removeItem(at: directory.appendingPathComponent((file.outgoingFilename as NSString).lastPathComponent))
            }
            return
        }
        timer?.cancel()
        waiting.resume(with: result)
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
    /// When files were left for later, or a listing of the library has not
    /// ended, the task is asked for soon, since there is work waiting; otherwise
    /// it is only a look for new photos, once an hour.
    public static func earliestBeginDelaySeconds(leftForLaterFileCount: Int, isListingUnfinished: Bool) -> TimeInterval {
        leftForLaterFileCount > 0 || isListingUnfinished ? 15 * 60 : 60 * 60
    }
}

/// PhotoBackupScreenReadPolicy decides when the Photo backup screen asks the
/// server for the size of the library: when the screen appears, when the app
/// comes to the foreground, and when backup finishes something, never in a
/// loop and never twice at once.
public enum PhotoBackupScreenReadPolicy {
    /// A read this soon after the last one is not made again, whatever asked.
    /// The screen's own numbers (what went up, what is left) come from the phone
    /// and are not read from the server.
    public static let minimumSecondsBetweenReads: TimeInterval = 15

    /// Whether to ask now. `mustRead` is the read the screen makes when it
    /// appears and the one the person asks for with Try again: made unless one
    /// is under way, whatever the last one was.
    public static func shouldRead(isReading: Bool, lastReadAt: Date?, now: Date, mustRead: Bool) -> Bool {
        guard !isReading else { return false }
        if mustRead { return true }
        guard let lastReadAt else { return true }
        return now.timeIntervalSince(lastReadAt) >= minimumSecondsBetweenReads
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
