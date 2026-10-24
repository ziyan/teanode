import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

// The backup of an iPhone's photo library to the server, as far as it needs
// neither PhotoKit nor UIKit: what a library item is, which of its files still
// have to go up and with which headers, and the file in the app group that
// remembers what went up. PhotoBackupCoordinator.swift runs a pass over these;
// the app target supplies PhotoKit and the background URLSession.

/// PhotoBackupFailure is why a file did not go up, or why a pass could not
/// go on, without any words of the app's own: the app target writes each one
/// in the person's language. It is kept in the index, so it is Codable.
public enum PhotoBackupFailure: Codable, Equatable, Sendable {
    /// The server did not accept the device password.
    case signInRefused
    /// The server took the file and did not say which photo it was.
    case serverDidNotSayWhichPhoto
    /// The server will not take this file, with its reason when it gave one.
    case fileRefused(serverReason: String?)
    /// The server's own words for a problem that is not the file's.
    case serverReason(String)
    /// The server has no room left to keep the file.
    case serverOutOfSpace
    /// The photo library gave a file with nothing in it.
    case fileIsEmpty
    /// The photo is no longer in the photo library.
    case fileIsNotInLibrary
    /// The file is in iCloud and the network costs data.
    case fileIsInICloudOnMobileData
    /// The photo library did not give the file in time (a download from iCloud
    /// that does not finish): backup went on without it and tries again later.
    case fileTookTooLongToRead
    /// The operating system's own words, already in the person's language.
    case systemDescription(String)
}

/// PhotoBackupFileRole is which of a photo's files an upload carries, in the
/// words the server's X-Photo-File-Role header uses.
public enum PhotoBackupFileRole: String, Codable, CaseIterable, Sendable {
    /// The file the camera made.
    case original
    /// The version after the person edited it.
    case edited
    /// A Live Photo's video.
    case liveVideo

    /// The order a photo's files go up in: the original first, so that a
    /// photo is on the server as soon as possible.
    var uploadOrder: Int {
        switch self {
        case .original: 0
        case .liveVideo: 1
        case .edited: 2
        }
    }
}

/// PhotoBackupMediaKind is what a library item is, in the words the server's
/// X-Photo-Kind header uses.
public enum PhotoBackupMediaKind: String, Codable, Sendable {
    case picture
    case video
    case livePhoto
}

/// PhotoBackupResource is one file the library holds for an item.
public struct PhotoBackupResource: Equatable, Sendable {
    public let role: PhotoBackupFileRole
    /// The name the library gives the file, such as IMG_0001.HEIC.
    public let originalFilename: String
    /// The media type of the file, such as image/heic.
    public let contentType: String
    /// Whether the file is on the phone, rather than only in iCloud. A hint:
    /// it decides whether getting it may use the network.
    public let isLocallyAvailable: Bool
    /// The size of the file as the library reports it, when it does: a large
    /// video that does not fit in the room left is not exported to find out.
    public let fileSizeBytes: Int64?

    public init(role: PhotoBackupFileRole, originalFilename: String, contentType: String, isLocallyAvailable: Bool, fileSizeBytes: Int64? = nil) {
        self.role = role
        self.originalFilename = originalFilename
        self.contentType = contentType
        self.isLocallyAvailable = isLocallyAvailable
        self.fileSizeBytes = fileSizeBytes
    }
}

/// PhotoBackupLocation is where a photo was taken.
public struct PhotoBackupLocation: Equatable, Sendable {
    public let latitude: Double
    public let longitude: Double

    public init(latitude: Double, longitude: Double) {
        self.latitude = latitude
        self.longitude = longitude
    }
}

/// PhotoBackupItem is one photo, video or Live Photo of the phone's library,
/// as far as backing it up needs to know.
public struct PhotoBackupItem: Equatable, Sendable {
    public let localIdentifier: String
    /// The identifier iCloud gives the item, the same on every device of the
    /// library; nil when the library has none for it.
    public let cloudIdentifier: String?
    public let creationDate: Date?
    /// When the library last changed the item, an edit included.
    public let modificationDate: Date?
    public let location: PhotoBackupLocation?
    public let isFavorite: Bool
    public let isScreenshot: Bool
    public let mediaKind: PhotoBackupMediaKind
    public let resources: [PhotoBackupResource]

    public init(
        localIdentifier: String, cloudIdentifier: String? = nil, creationDate: Date? = nil, modificationDate: Date? = nil,
        location: PhotoBackupLocation? = nil, isFavorite: Bool = false, isScreenshot: Bool = false,
        mediaKind: PhotoBackupMediaKind = .picture, resources: [PhotoBackupResource]
    ) {
        self.localIdentifier = localIdentifier
        self.cloudIdentifier = cloudIdentifier
        self.creationDate = creationDate
        self.modificationDate = modificationDate
        self.location = location
        self.isFavorite = isFavorite
        self.isScreenshot = isScreenshot
        self.mediaKind = mediaKind
        self.resources = resources
    }
}

/// PhotoBackupServerPhoto is a photo the server already has, as FindPhotos
/// answers.
public struct PhotoBackupServerPhoto: Codable, Equatable, Sendable {
    public let id: String
    public let clientPhotoId: String
    public let photoKind: String
    /// The roles of the files the server holds for the photo, by
    /// PhotoBackupFileRole's raw value. The original, always, for a server that
    /// does not say.
    public let photoFileRoles: [String]

    public init(id: String, clientPhotoId: String, photoKind: String = "picture", photoFileRoles: [String] = ["original"]) {
        self.id = id
        self.clientPhotoId = clientPhotoId
        self.photoKind = photoKind
        self.photoFileRoles = photoFileRoles
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        clientPhotoId = try container.decode(String.self, forKey: .clientPhotoId)
        photoKind = try container.decode(String.self, forKey: .photoKind)
        photoFileRoles = try container.decodeIfPresent([String].self, forKey: .photoFileRoles) ?? ["original"]
    }
}

/// PhotoBackupHeader names the request headers an upload describes its file
/// with. The server reads them (internal/photo/headers.go).
public enum PhotoBackupHeader {
    public static let clientId = "X-Photo-Client-ID"
    public static let fileRole = "X-Photo-File-Role"
    public static let kind = "X-Photo-Kind"
    public static let filename = "X-Photo-Filename"
    public static let takenAt = "X-Photo-Taken-At"
    public static let latitude = "X-Photo-Latitude"
    public static let longitude = "X-Photo-Longitude"
    public static let isFavorite = "X-Photo-Is-Favorite"
    public static let isScreenshot = "X-Photo-Is-Screenshot"
    public static let source = "X-Photo-Source"
    /// The SHA-256 of the file, in lowercase hexadecimal. With it the server
    /// can tell, before the bytes have all arrived, that the file is one the
    /// person already has, and does not count it against their room.
    public static let declaredSha256 = "X-Photo-Declared-SHA256"
    public static let contentType = "Content-Type"
    /// The resumable upload protocol's own headers: where the bytes of a request
    /// go in the upload, whether they are the last, and how long the file is.
    public static let uploadOffset = "Upload-Offset"
    public static let uploadComplete = "Upload-Complete"
    public static let uploadLength = "Upload-Length"
    /// The server's answer to a finished upload names the photo in this.
    public static let photoId = "X-Photo-ID"
}

/// PhotoBackupUpload is one file still to go up: which item and role, and the
/// headers that describe it.
public struct PhotoBackupUpload: Equatable, Sendable {
    public let localIdentifier: String
    public let role: PhotoBackupFileRole
    public let clientPhotoId: String
    public let filename: String
    public let contentType: String
    public let isLocallyAvailable: Bool
    /// The size the library reports for the file, nil when it does not say.
    public let fileSizeBytes: Int64?
    /// The item's modification date when this was planned, kept in the index
    /// once the file is up so that a later edit is noticed.
    public let assetModificationDate: Date?
    public let headers: [String: String]

    /// What names this upload among the others, a task of the background
    /// session included.
    public var key: String { PhotoBackupUpload.key(localIdentifier: localIdentifier, role: role) }

    public static func key(localIdentifier: String, role: PhotoBackupFileRole) -> String {
        "\(localIdentifier)|\(role.rawValue)"
    }
}

/// PhotoBackupPlanner decides what is still to be uploaded and how each file
/// is described. It reads the index's records but changes nothing.
public enum PhotoBackupPlanner {
    /// How often the server may refuse a file before it is left alone. Network
    /// failures and server errors do not count, only a refusal of the file
    /// itself (too large, not a picture the server keeps, no room).
    public static let maximumRefusalCount = 3

    /// How the server is told which photo this is: iCloud's identifier when
    /// there is one, so that a second iPhone on the same library finds its
    /// photos already there, and otherwise the phone's own.
    public static func clientPhotoId(for item: PhotoBackupItem) -> String {
        if let cloudIdentifier = item.cloudIdentifier, !cloudIdentifier.isEmpty { return cloudIdentifier }
        return "local:" + item.localIdentifier
    }

    /// The uploads still to do, newest photo first (what the person took
    /// last is what they look for first) and, within one photo, the original
    /// before its video and its edit. `inFlight` holds the keys of uploads
    /// already queued, which are not planned again.
    public static func plan(items: [PhotoBackupItem], records: [String: PhotoBackupRecord], inFlight: Set<String> = []) -> [PhotoBackupUpload] {
        let ordered = items.sorted { first, second in
            switch (first.creationDate, second.creationDate) {
            case (let firstDate?, let secondDate?):
                if firstDate != secondDate { return firstDate > secondDate }
            case (nil, _?): return false
            case (_?, nil): return true
            case (nil, nil): break
            }
            return first.localIdentifier < second.localIdentifier
        }
        var uploads: [PhotoBackupUpload] = []
        for item in ordered {
            let record = records[item.localIdentifier]
            // A photo keeps the name it first went up under, even if iCloud
            // names it later.
            let clientPhotoId = record?.clientPhotoId ?? clientPhotoId(for: item)
            for resource in item.resources.sorted(by: { $0.role.uploadOrder < $1.role.uploadOrder }) {
                guard needsUpload(resource.role, of: item, record: record),
                    !inFlight.contains(PhotoBackupUpload.key(localIdentifier: item.localIdentifier, role: resource.role))
                else { continue }
                uploads.append(
                    PhotoBackupUpload(
                        localIdentifier: item.localIdentifier, role: resource.role, clientPhotoId: clientPhotoId,
                        filename: resource.originalFilename, contentType: resource.contentType,
                        isLocallyAvailable: resource.isLocallyAvailable, fileSizeBytes: resource.fileSizeBytes,
                        assetModificationDate: item.modificationDate,
                        headers: headers(for: item, resource: resource, clientPhotoId: clientPhotoId)))
            }
        }
        return uploads
    }

    /// The uploads with the files that are on the phone first, the ones that
    /// have to come down from iCloud after them, each group in the order it came
    /// in: the queue fills fast with what needs no download, and a file that is
    /// slow or stuck in iCloud does not stand in front of the rest.
    public static func preferringLocalFiles(_ uploads: [PhotoBackupUpload]) -> [PhotoBackupUpload] {
        uploads.filter(\.isLocallyAvailable) + uploads.filter { !$0.isLocallyAvailable }
    }

    /// Whether one role of an item has to go up: it is not on the server, or it
    /// is the edit and the item was changed since the edit went up. A file the
    /// server refused too often is left alone.
    static func needsUpload(_ role: PhotoBackupFileRole, of item: PhotoBackupItem, record: PhotoBackupRecord?) -> Bool {
        guard let state = record?.roles[role.rawValue] else { return true }
        if state.refusalCount >= maximumRefusalCount { return false }
        if !state.isOnServer { return true }
        if role == .edited, let modified = item.modificationDate, let sent = state.assetModificationDate, modified.timeIntervalSince(sent) > 1 {
            return true
        }
        return false
    }

    /// The headers that describe one file of an item.
    public static func headers(for item: PhotoBackupItem, resource: PhotoBackupResource, clientPhotoId: String) -> [String: String] {
        var headers: [String: String] = [
            PhotoBackupHeader.clientId: clientPhotoId,
            PhotoBackupHeader.fileRole: resource.role.rawValue,
            PhotoBackupHeader.kind: item.mediaKind.rawValue,
            PhotoBackupHeader.isFavorite: item.isFavorite ? "true" : "false",
            PhotoBackupHeader.isScreenshot: item.isScreenshot ? "true" : "false",
            PhotoBackupHeader.source: "iphone",
        ]
        let filename = headerSafeFilename(resource.originalFilename)
        if !filename.isEmpty { headers[PhotoBackupHeader.filename] = filename }
        if !resource.contentType.isEmpty { headers[PhotoBackupHeader.contentType] = resource.contentType }
        if let creationDate = item.creationDate {
            headers[PhotoBackupHeader.takenAt] = rfc3339(creationDate)
        }
        if let location = item.location, abs(location.latitude) <= 90, abs(location.longitude) <= 180 {
            headers[PhotoBackupHeader.latitude] = coordinateText(location.latitude)
            headers[PhotoBackupHeader.longitude] = coordinateText(location.longitude)
        }
        return headers
    }

    /// A date as RFC 3339 in UTC, such as 2026-10-15T22:42:00Z. The library's
    /// creation date is an instant and does not say in which zone the photo was
    /// taken, so no offset is claimed: the trailing Z tells the server that the
    /// instant is known and the offset is not, and the server keeps the offset
    /// the file's own EXIF says. Written by hand so that it reads the same on
    /// every platform and in every locale.
    public static func rfc3339(_ date: Date) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(secondsFromGMT: 0) ?? .current
        let parts = calendar.dateComponents([.year, .month, .day, .hour, .minute, .second], from: date)
        return String(
            format: "%04d-%02d-%02dT%02d:%02d:%02dZ", parts.year ?? 1970, parts.month ?? 1, parts.day ?? 1, parts.hour ?? 0, parts.minute ?? 0,
            parts.second ?? 0)
    }

    /// Six decimals, with a point whatever the phone's language.
    static func coordinateText(_ degrees: Double) -> String {
        String(format: "%.6f", locale: Locale(identifier: "en_US_POSIX"), degrees)
    }

    /// A file name that an HTTP header can carry: printable ASCII, without a
    /// path. Anything else becomes an underscore; the name is only shown.
    static func headerSafeFilename(_ name: String) -> String {
        let trimmed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let safe = trimmed.unicodeScalars.map { scalar -> Character in
            if scalar.value < 0x20 || scalar.value > 0x7E || scalar == "/" || scalar == "\\" { return "_" }
            return Character(scalar)
        }
        return String(safe.prefix(200))
    }
}

/// PhotoBackupPartialUpload is the upload the server made for a file that has
/// not been sent whole yet. It is kept in the index so that a file cut off part
/// way (paused, the app killed, a connection the system gave up on) goes on from
/// the byte the server has, instead of being sent again from the first.
public struct PhotoBackupPartialUpload: Codable, Equatable, Sendable {
    /// The path of the upload on the server, such as `/api/v1/photos/uploads/<id>`.
    /// Only the path is kept: the server's own address is the account's, and the
    /// scheme and host in the server's answer depend on its proxy.
    public let uploadPath: String
    /// The size of the file the upload was made for. A file exported again with
    /// another size is not the one the server has a part of.
    public let sizeBytes: Int64
    /// The SHA-256 of that file, in lowercase hexadecimal. The same digest told
    /// the server what the file would hash to; an exported file that hashes to
    /// something else is not the one the server has a part of.
    public let sha256: String

    public init(uploadPath: String, sizeBytes: Int64, sha256: String) {
        self.uploadPath = uploadPath
        self.sizeBytes = sizeBytes
        self.sha256 = sha256
    }
}

/// PhotoBackupRoleRecord is what the index knows of one file of an item.
public struct PhotoBackupRoleRecord: Codable, Equatable, Sendable {
    public var isOnServer: Bool
    /// The server's photo this file belongs to, once it said.
    public var serverPhotoId: String?
    /// The item's modification date when the file went up.
    public var assetModificationDate: Date?
    /// How often the server refused the file itself.
    public var refusalCount: Int
    public var refusalFailure: PhotoBackupFailure?
    /// The upload the server holds a part of this file in, while there is one.
    /// Nil in an index written before uploads could be continued.
    public var partialUpload: PhotoBackupPartialUpload?

    public init(
        isOnServer: Bool = false, serverPhotoId: String? = nil, assetModificationDate: Date? = nil, refusalCount: Int = 0,
        refusalFailure: PhotoBackupFailure? = nil, partialUpload: PhotoBackupPartialUpload? = nil
    ) {
        self.isOnServer = isOnServer
        self.serverPhotoId = serverPhotoId
        self.assetModificationDate = assetModificationDate
        self.refusalCount = refusalCount
        self.refusalFailure = refusalFailure
        self.partialUpload = partialUpload
    }
}

/// PhotoBackupRecord is what the index knows of one item of the library.
public struct PhotoBackupRecord: Codable, Equatable, Sendable {
    /// The name the item went up under (or will), kept so that it never changes.
    public var clientPhotoId: String
    /// By PhotoBackupFileRole's raw value, because a JSON object needs text keys.
    public var roles: [String: PhotoBackupRoleRecord]
    /// Where the item stands in the line of items still to send: the lower, the
    /// sooner. The listing hands them out newest photo first, so the line is
    /// newest first without the index holding every item's date. Nil for an item
    /// that was never planned (one the server already had).
    public var sendOrder: Int?

    public init(clientPhotoId: String, roles: [String: PhotoBackupRoleRecord] = [:], sendOrder: Int? = nil) {
        self.clientPhotoId = clientPhotoId
        self.roles = roles
        self.sendOrder = sendOrder
    }

    public var serverPhotoId: String? { roles.values.compactMap(\.serverPhotoId).first }
    public var isOriginalOnServer: Bool { roles[PhotoBackupFileRole.original.rawValue]?.isOnServer == true }
}

/// PhotoBackupResumePoint is where a listing of the whole library stopped: the
/// creation date of the last item it handled. The library is listed newest
/// first, so the next batch holds the items made before this date (see
/// `PhotoBackupListingWindow`). A saved index that still has the identifier of
/// that item, which an earlier version kept, is read all the same.
public struct PhotoBackupResumePoint: Codable, Equatable, Sendable {
    /// The item's creation date as seconds since the reference date, kept
    /// exactly: the index's other dates are written to the second, and a
    /// rounded date would skip the items made within the same second. The
    /// reference date is the one the library keeps its dates against, so the
    /// number comes back as the very same date.
    public let creationSecondsSinceReferenceDate: Double

    public init(creationDate: Date) {
        creationSecondsSinceReferenceDate = creationDate.timeIntervalSinceReferenceDate
    }

    public var creationDate: Date { Date(timeIntervalSinceReferenceDate: creationSecondsSinceReferenceDate) }
}

/// PhotoBackupListingProgress is a listing of the whole library that has begun
/// and not ended, kept in the index so that a pass which stops (iOS ends the
/// background task, the person leaves, the app is killed) is followed by one
/// that goes on from there.
public struct PhotoBackupListingProgress: Codable, Equatable, Sendable {
    /// The library's change token as it was when the listing began, so that
    /// whatever changed meanwhile is read once the listing has ended.
    public var changeToken: Data?
    /// The last item handled; nil until the first batch is done.
    public var resumePoint: PhotoBackupResumePoint?
    /// How many items the listing has gone through, the ones it skipped included.
    public var listedItemCount: Int
    /// How many items the library held when the listing began.
    public var libraryItemCount: Int
}

/// PhotoBackupSummary is what the index says about the backup as a whole.
public struct PhotoBackupSummary: Equatable, Sendable {
    /// Items whose original is on the server.
    public var backedUpItemCount: Int
    /// Items the library held at the last look.
    public var libraryItemCount: Int
    /// Files the server refused often enough to be left alone.
    public var refusedFileCount: Int
    public var lastFailure: PhotoBackupFailure?
    /// The server refused the sign-in: the device password is wrong or revoked.
    public var isSignInNeeded: Bool
    public var lastPassFinishedAt: Date?
}

/// PhotoBackupIndex is the memory of the backup: which items went up, under
/// what name, and the library's change token, in one file in the app group
/// that is replaced whole on every write. If the file cannot be read it is
/// started over, keeping nothing: what the server already has is found again
/// by FindPhotos and a repeated upload of a file the server has is harmless.
public actor PhotoBackupIndex {
    /// The file as it is written.
    struct Contents: Codable {
        var version = 1
        var accountKey: String?
        var records: [String: PhotoBackupRecord] = [:]
        /// The library's archived change token, which only the app reads. Set
        /// only once the whole library has been listed.
        var changeToken: Data?
        /// The listing of the whole library, while it is under way.
        var listing: PhotoBackupListingProgress?
        /// The next order numbers for the items still to send (`sendOrder`):
        /// those of a listing count up from `nextLaterSendOrder`, newest photo
        /// first, and those of photos that came into the library afterwards
        /// count down from `nextEarlierSendOrder`, so they go before the rest.
        /// Optional so that a file written before they existed is still read;
        /// nil counts as 1 and 0.
        var nextLaterSendOrder: Int?
        var nextEarlierSendOrder: Int?
        var libraryItemCount = 0
        var lastFailure: PhotoBackupFailure?
        var isSignInNeeded = false
        var lastPassFinishedAt: Date?
    }

    public static let indexFilename = "index.json"

    /// Changes are written at most this often while a pass runs, and `flush`
    /// writes what is left when it ends. Rewriting a library's worth of
    /// records for every upload would cost more than the upload. A crash
    /// between two writes loses the changes made since the first of them and
    /// nothing before it: a file sent again is harmless, the server stores
    /// each once.
    public static let secondsBetweenWrites: TimeInterval = 30

    private let fileURL: URL
    private let fileManager: FileManager
    private let now: @Sendable () -> Date
    private var contents = Contents()
    private var unwrittenChangeCount = 0
    private var lastWriteAt: Date
    /// Items whose original is on the server, and files refused often enough
    /// to be left alone: kept up to date as records are written, so that the
    /// status, published after every upload, does not walk every record.
    private var backedUpItemCount = 0
    private var refusedFileCount = 0
    /// Files planned and not on the server, leaving out the refused ones.
    private var pendingFileCountKept = 0
    /// Whether the file existed and could not be read when the index was opened.
    public private(set) var wasStartedOver = false

    public init(directory: URL, fileManager: FileManager = .default, now: @escaping @Sendable () -> Date = { Date() }) throws {
        self.fileManager = fileManager
        self.now = now
        lastWriteAt = now()
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
        fileURL = directory.appendingPathComponent(Self.indexFilename)
        if fileManager.fileExists(atPath: fileURL.path) {
            do {
                let decoder = JSONDecoder()
                decoder.dateDecodingStrategy = .iso8601
                contents = try decoder.decode(Contents.self, from: Data(contentsOf: fileURL))
            } catch {
                try? fileManager.removeItem(at: fileURL)
                contents = Contents()
                wasStartedOver = true
            }
        }
        (backedUpItemCount, refusedFileCount, pendingFileCountKept) = Self.counts(of: contents.records.values)
    }

    /// How many records have their original on the server, how many of their
    /// files the server refused often enough to be left alone, and how many
    /// files are still to send.
    private static func counts(of records: some Sequence<PhotoBackupRecord>) -> (backedUp: Int, refused: Int, pending: Int) {
        var backedUp = 0
        var refused = 0
        var pending = 0
        for record in records {
            if record.isOriginalOnServer { backedUp += 1 }
            for state in record.roles.values {
                if state.refusalCount >= PhotoBackupPlanner.maximumRefusalCount {
                    refused += 1
                } else if !state.isOnServer {
                    pending += 1
                }
            }
        }
        return (backedUp, refused, pending)
    }

    /// The only way a record is written, so that the counters follow it.
    private func store(_ record: PhotoBackupRecord?, for localIdentifier: String) {
        if let old = contents.records[localIdentifier] {
            let (backedUp, refused, pending) = Self.counts(of: [old])
            backedUpItemCount -= backedUp
            refusedFileCount -= refused
            pendingFileCountKept -= pending
        }
        if let record {
            let (backedUp, refused, pending) = Self.counts(of: [record])
            backedUpItemCount += backedUp
            refusedFileCount += refused
            pendingFileCountKept += pending
        }
        contents.records[localIdentifier] = record
    }

    // MARK: Account

    public var accountKey: String? { contents.accountKey }

    /// Makes the index the memory of one account's backup. Another account's
    /// memory is dropped: its names mean nothing on this server.
    /// Forgets everything kept for an account that is signed out of, the file
    /// included. Another account's memory is left as it is.
    public func forget(accountKey: String) {
        guard contents.accountKey == accountKey else { return }
        contents = Contents()
        backedUpItemCount = 0
        refusedFileCount = 0
        pendingFileCountKept = 0
        unwrittenChangeCount = 0
        try? fileManager.removeItem(at: fileURL)
    }

    public func adopt(accountKey: String) {
        guard contents.accountKey != accountKey else { return }
        let hadAccount = contents.accountKey != nil
        contents = Contents()
        backedUpItemCount = 0
        refusedFileCount = 0
        pendingFileCountKept = 0
        contents.accountKey = accountKey
        if hadAccount { wasStartedOver = true }
        didChange(force: true)
    }

    // MARK: Reading

    public func record(forLocalIdentifier localIdentifier: String) -> PhotoBackupRecord? {
        contents.records[localIdentifier]
    }

    public func records(for localIdentifiers: [String]) -> [String: PhotoBackupRecord] {
        var found: [String: PhotoBackupRecord] = [:]
        for localIdentifier in localIdentifiers {
            if let record = contents.records[localIdentifier] { found[localIdentifier] = record }
        }
        return found
    }

    /// The identifiers, among these, that the index has no record of. A listing
    /// after an expired change token skips the items it has a record of, and
    /// asks the library for the others only.
    public func localIdentifiersWithoutRecord(among localIdentifiers: [String]) -> [String] {
        localIdentifiers.filter { contents.records[$0] == nil }
    }

    /// Every item the index has a record of.
    public func knownLocalIdentifiers() -> Set<String> {
        Set(contents.records.keys)
    }

    public var changeToken: Data? { contents.changeToken }

    /// The listing of the whole library that has begun and not ended, if any.
    public var listingProgress: PhotoBackupListingProgress? { contents.listing }

    /// How many files are still to send: those planned and not on the server,
    /// leaving out the ones the server refused often enough to be left alone.
    /// Kept up to date as records are written.
    public func pendingFileCount() -> Int {
        pendingFileCountKept
    }

    public func summary() -> PhotoBackupSummary {
        PhotoBackupSummary(
            backedUpItemCount: backedUpItemCount, libraryItemCount: contents.libraryItemCount, refusedFileCount: refusedFileCount,
            lastFailure: contents.lastFailure, isSignInNeeded: contents.isSignInNeeded,
            lastPassFinishedAt: contents.lastPassFinishedAt)
    }

    /// What of these items still has to go up. `inFlight` holds uploads that
    /// are already queued.
    public func plan(items: [PhotoBackupItem], inFlight: Set<String> = []) -> [PhotoBackupUpload] {
        PhotoBackupPlanner.plan(items: items, records: contents.records, inFlight: inFlight)
    }

    /// The items with a file still to send: planned by an earlier pass and not
    /// finished, because the network failed, iCloud was out of reach, or the
    /// app was closed. They are read again whatever the library's token says,
    /// in the order they are to go: the line `sendOrder` makes, newest photo
    /// first, and by identifier for items with the same place.
    public func pendingLocalIdentifiers() -> [String] {
        let pending: [(localIdentifier: String, sendOrder: Int)] = contents.records.compactMap { localIdentifier, record in
            record.roles.values.contains { !$0.isOnServer && $0.refusalCount < PhotoBackupPlanner.maximumRefusalCount }
                ? (localIdentifier, record.sendOrder ?? Int.max) : nil
        }
        return pending.sorted { first, second in
            first.sendOrder != second.sendOrder ? first.sendOrder < second.sendOrder : first.localIdentifier < second.localIdentifier
        }.map(\.localIdentifier)
    }

    /// The names, among these items', that the server has not been asked about
    /// and no upload has settled.
    public func clientPhotoIdsToAsk(about items: [PhotoBackupItem]) -> [String] {
        items.filter { contents.records[$0.localIdentifier] == nil }.map(PhotoBackupPlanner.clientPhotoId(for:))
    }

    // MARK: Writing

    /// Notes that the server already has these photos, found by FindPhotos:
    /// every file it says it holds is done, so that a reinstall does not send
    /// an edit or a Live Photo's video again. A file it does not hold still goes
    /// up.
    public func markServerHas(_ serverPhotos: [PhotoBackupServerPhoto], for items: [PhotoBackupItem]) {
        let byClientPhotoId = Dictionary(serverPhotos.map { ($0.clientPhotoId, $0) }, uniquingKeysWith: { first, _ in first })
        for item in items where contents.records[item.localIdentifier] == nil {
            let clientPhotoId = PhotoBackupPlanner.clientPhotoId(for: item)
            guard let found = byClientPhotoId[clientPhotoId] else { continue }
            var record = PhotoBackupRecord(clientPhotoId: clientPhotoId)
            record.roles[PhotoBackupFileRole.original.rawValue] = PhotoBackupRoleRecord(isOnServer: true, serverPhotoId: found.id)
            for photoFileRole in found.photoFileRoles where PhotoBackupFileRole(rawValue: photoFileRole) != nil {
                record.roles[photoFileRole] = PhotoBackupRoleRecord(isOnServer: true, serverPhotoId: found.id)
            }
            store(record, for: item.localIdentifier)
        }
        didChange()
    }

    /// Notes that these files are to be sent, so that a pass that ends before
    /// they are keeps them for the next one, however far the library's token has
    /// moved. The uploads come newest photo first, and the items take their
    /// place in the line of items to send in that order: after those already in
    /// it, or before them when `goesBeforeTheRest` (photos that came into the
    /// library since, which the person looks for first).
    ///
    /// Written at once unless `shouldWriteAtOnce` is false, which a listing of the
    /// whole library says: it writes a batch at a time, and rewriting the whole
    /// file for each would make the listing take longer the further it went. What
    /// is not written yet is written with the next change after
    /// `secondsBetweenWrites`, and by `flush` when the pass ends.
    public func markPending(_ uploads: [PhotoBackupUpload], goesBeforeTheRest: Bool = false, shouldWriteAtOnce: Bool = true) {
        var itemsToPlace: [String] = []
        var placed: Set<String> = []
        for upload in uploads {
            var record = contents.records[upload.localIdentifier] ?? PhotoBackupRecord(clientPhotoId: upload.clientPhotoId)
            if var state = record.roles[upload.role.rawValue] {
                // An edit made since the file went up is to be sent again: it
                // is still to send, and the index must say so for the pass that
                // reads the files still to send.
                if state.isOnServer {
                    state.isOnServer = false
                    record.roles[upload.role.rawValue] = state
                }
            } else {
                record.roles[upload.role.rawValue] = PhotoBackupRoleRecord()
            }
            if record.sendOrder == nil, placed.insert(upload.localIdentifier).inserted { itemsToPlace.append(upload.localIdentifier) }
            store(record, for: upload.localIdentifier)
        }
        if goesBeforeTheRest {
            contents.nextEarlierSendOrder = (contents.nextEarlierSendOrder ?? 0) - itemsToPlace.count
        }
        for (position, localIdentifier) in itemsToPlace.enumerated() {
            let sendOrder: Int
            if goesBeforeTheRest {
                sendOrder = (contents.nextEarlierSendOrder ?? 0) + 1 + position
            } else {
                sendOrder = contents.nextLaterSendOrder ?? 1
                contents.nextLaterSendOrder = sendOrder + 1
            }
            contents.records[localIdentifier]?.sendOrder = sendOrder
        }
        didChange(force: shouldWriteAtOnce)
    }

    /// Forgets the files still to be sent of items that are no longer in the
    /// library: deleted on the phone before they went up, so there is nothing
    /// to send. What the server already has stays.
    public func forgetPending(of localIdentifiers: [String]) {
        for localIdentifier in localIdentifiers {
            guard var record = contents.records[localIdentifier] else { continue }
            record.roles = record.roles.filter { $0.value.isOnServer }
            store(record.roles.isEmpty ? nil : record, for: localIdentifier)
        }
        didChange()
    }

    /// Notes that a file is on the server.
    public func markUploaded(upload: PhotoBackupUpload, serverPhotoId: String) {
        markUploaded(
            localIdentifier: upload.localIdentifier, clientPhotoId: upload.clientPhotoId, role: upload.role, serverPhotoId: serverPhotoId,
            assetModificationDate: upload.assetModificationDate)
    }

    public func markUploaded(
        localIdentifier: String, clientPhotoId: String, role: PhotoBackupFileRole, serverPhotoId: String, assetModificationDate: Date?
    ) {
        var record = contents.records[localIdentifier] ?? PhotoBackupRecord(clientPhotoId: clientPhotoId)
        // Only the edit is compared with a later modification date, so only
        // its date is kept: the file holds a record for every photo.
        record.roles[role.rawValue] = PhotoBackupRoleRecord(
            isOnServer: true, serverPhotoId: serverPhotoId, assetModificationDate: role == .edited ? assetModificationDate : nil)
        store(record, for: localIdentifier)
        didChange()
    }

    /// The upload the server made for this file, if one is kept.
    public func partialUpload(localIdentifier: String, role: PhotoBackupFileRole) -> PhotoBackupPartialUpload? {
        contents.records[localIdentifier]?.roles[role.rawValue]?.partialUpload
    }

    /// Notes the upload the server made for a file, so that a file cut off part
    /// way goes on from the byte the server has, also after the app was closed.
    /// Written with the next change after `secondsBetweenWrites`, and when the
    /// app goes to the background (`flush`): a crash in between loses the
    /// address, and the file then starts over in an upload of its own.
    public func markUploadStarted(localIdentifier: String, clientPhotoId: String, role: PhotoBackupFileRole, partialUpload: PhotoBackupPartialUpload) {
        var record = contents.records[localIdentifier] ?? PhotoBackupRecord(clientPhotoId: clientPhotoId)
        var state = record.roles[role.rawValue] ?? PhotoBackupRoleRecord()
        state.partialUpload = partialUpload
        record.roles[role.rawValue] = state
        store(record, for: localIdentifier)
        didChange()
    }

    /// Forgets the upload kept for a file: the server no longer has it, or the
    /// file is no longer the one it has a part of.
    public func forgetPartialUpload(localIdentifier: String, role: PhotoBackupFileRole) {
        guard var record = contents.records[localIdentifier], var state = record.roles[role.rawValue], state.partialUpload != nil else { return }
        state.partialUpload = nil
        record.roles[role.rawValue] = state
        store(record, for: localIdentifier)
        didChange()
    }

    /// The addresses of the uploads kept for these items, in any of their files.
    /// Read before the items are forgotten, so that the server can be told to
    /// drop what the app will never continue.
    public func partialUploadPaths(ofLocalIdentifiers localIdentifiers: [String]) -> [String] {
        localIdentifiers.flatMap { localIdentifier in
            (contents.records[localIdentifier]?.roles.values.compactMap { $0.partialUpload?.uploadPath }) ?? []
        }
    }

    /// Every address kept, for every file.
    public func allPartialUploadPaths() -> [String] {
        contents.records.values.flatMap { record in record.roles.values.compactMap { $0.partialUpload?.uploadPath } }
    }

    /// Forgets every address kept. Nothing else about the files changes: they
    /// are still to send, each as an upload of its own.
    public func forgetAllPartialUploads() {
        for (localIdentifier, var record) in contents.records {
            var isChanged = false
            for (roleName, var state) in record.roles where state.partialUpload != nil {
                state.partialUpload = nil
                record.roles[roleName] = state
                isChanged = true
            }
            if isChanged { store(record, for: localIdentifier) }
        }
        didChange()
    }

    /// Notes that the server refused a file itself. The upload it was sent in
    /// is over, whatever the server did with it.
    public func markRefused(localIdentifier: String, clientPhotoId: String, role: PhotoBackupFileRole, failure: PhotoBackupFailure) {
        var record = contents.records[localIdentifier] ?? PhotoBackupRecord(clientPhotoId: clientPhotoId)
        var state = record.roles[role.rawValue] ?? PhotoBackupRoleRecord()
        state.refusalCount += 1
        state.refusalFailure = failure
        state.partialUpload = nil
        record.roles[role.rawValue] = state
        store(record, for: localIdentifier)
        didChange()
    }

    /// Lets every refused file be tried again, which "Back up now" does. A
    /// file that is not on the server becomes a fresh file still to send, not
    /// nothing: with no record of the role it would be neither pending nor
    /// listed again.
    public func clearRefusals() {
        for (localIdentifier, var record) in contents.records {
            var isChanged = false
            for (roleName, var state) in record.roles where state.refusalCount > 0 {
                if state.isOnServer {
                    state.refusalCount = 0
                    state.refusalFailure = nil
                    record.roles[roleName] = state
                } else {
                    record.roles[roleName] = PhotoBackupRoleRecord()
                }
                isChanged = true
            }
            if isChanged { store(record, for: localIdentifier) }
        }
        didChange()
    }

    /// Sets the library's change token. A listing under way is dropped with it:
    /// the token is set to nothing when the next pass has to list the library
    /// again from the start (the Hidden album was included or left out).
    public func setChangeToken(_ token: Data?) {
        contents.changeToken = token
        contents.listing = nil
        didChange(force: true)
    }

    // MARK: Listing the whole library

    /// Notes that a listing of the whole library begins, with the change token
    /// as it is now. A listing already under way is replaced.
    public func beginListing(changeToken: Data?, libraryItemCount: Int) {
        contents.listing = PhotoBackupListingProgress(
            changeToken: changeToken, resumePoint: nil, listedItemCount: 0, libraryItemCount: libraryItemCount)
        contents.libraryItemCount = libraryItemCount
        didChange(force: true)
    }

    /// Notes that a batch of the listing is done: the items up to `resumePoint`
    /// are in the index (or were left out), and the next batch follows it.
    /// Written with the next change after `secondsBetweenWrites`, not at once:
    /// the file is rewritten whole, and doing that for each batch would make the
    /// listing take longer the further it went. A listing that is cut short
    /// loses at most those seconds of work, and `flush` writes the rest.
    public func recordListed(batchItemCount: Int, resumePoint: PhotoBackupResumePoint?) {
        guard var listing = contents.listing else { return }
        listing.listedItemCount += batchItemCount
        if let resumePoint { listing.resumePoint = resumePoint }
        contents.listing = listing
        didChange()
    }

    /// Notes that the whole library has been listed: its change token, as it
    /// was when the listing began, is the one to read changes from.
    public func finishListing() {
        guard let listing = contents.listing else { return }
        contents.changeToken = listing.changeToken
        contents.listing = nil
        didChange(force: true)
    }

    public func setLibraryItemCount(_ count: Int) {
        contents.libraryItemCount = count
        didChange()
    }

    /// Records what went wrong, to be shown. A sign-in refusal is remembered
    /// apart: it stops the backup until the person signs in again. Only an
    /// explicit `isSignInNeeded` changes that: a problem recorded while it is
    /// set (the uploads cancelled because of it, say) leaves it as it is.
    public func setError(failure: PhotoBackupFailure?, isSignInNeeded: Bool? = nil) {
        contents.lastFailure = failure
        if let isSignInNeeded { contents.isSignInNeeded = isSignInNeeded }
        // A refused sign-in is written at once, as it stops the backup; any
        // other problem is written with the next change.
        didChange(force: isSignInNeeded == true)
    }

    /// Clears the last error and the refused sign-in with it.
    public func clearError() {
        guard contents.lastFailure != nil || contents.isSignInNeeded else { return }
        setError(failure: nil, isSignInNeeded: false)
    }

    /// Clears the last error, unless it is the server not accepting the device
    /// password: that waits for the person to sign in again.
    public func clearErrorUnlessSignInIsNeeded() {
        guard !contents.isSignInNeeded else { return }
        clearError()
    }

    public func finishPass(at date: Date) {
        contents.lastPassFinishedAt = date
        didChange(force: true)
    }

    /// Writes what has not been written.
    public func flush() {
        guard unwrittenChangeCount > 0 else { return }
        write()
    }

    private func didChange(force: Bool = false) {
        unwrittenChangeCount += 1
        if force || now().timeIntervalSince(lastWriteAt) >= Self.secondsBetweenWrites { write() }
    }

    /// Replaces the file whole, so a crash leaves the old one or the new one.
    /// The text is compact: no spaces, and slashes in identifiers unescaped.
    private func write() {
        lastWriteAt = now()
        do {
            let encoder = JSONEncoder()
            encoder.dateEncodingStrategy = .iso8601
            encoder.outputFormatting = [.withoutEscapingSlashes]
            try encoder.encode(contents).write(to: fileURL, options: .atomic)
            unwrittenChangeCount = 0
        } catch {
            // Kept in memory, tried again with the next write: the worst
            // outcome is that some files are sent twice after a restart.
        }
    }
}
