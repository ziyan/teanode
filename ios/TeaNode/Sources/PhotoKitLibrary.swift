import CoreLocation
import Foundation
import Network
import Photos
import TeaNodeKit
import UniformTypeIdentifiers

/// PhotoKitLibrary is the phone's photo library as backup reads it. It holds no
/// state: every call reads PhotoKit afresh, and no PHAsset or PHAssetResource
/// leaves a call, only values backup can send between threads. Calls run off
/// the main actor, as PhotoKit's reads can take long on a large library.
struct PhotoKitLibrary: PhotoBackupLibrary {
    // MARK: Access

    func access() -> PhotoBackupAccess {
        Self.access(for: PHPhotoLibrary.authorizationStatus(for: .readWrite))
    }

    /// Asks the person, the first time, for access to the whole library.
    static func requestAccess() async -> PhotoBackupAccess {
        access(for: await PHPhotoLibrary.requestAuthorization(for: .readWrite))
    }

    static func access(for status: PHAuthorizationStatus) -> PhotoBackupAccess {
        switch status {
        case .authorized: .full
        case .limited: .limited
        case .notDetermined: .notDetermined
        case .denied, .restricted: .denied
        @unknown default: .denied
        }
    }

    // MARK: Reading

    func beginListing(includesHidden: Bool) async throws -> PhotoBackupListingStart {
        // Taken before the first batch is read, so that whatever changes while
        // the listing runs is read again by a later pass rather than missed.
        let changeToken = Self.archivedCurrentChangeToken()
        let totalCount = PHAsset.fetchAssets(with: Self.fetchOptions(includesHidden: includesHidden)).count
        return PhotoBackupListingStart(totalCount: totalCount, changeToken: changeToken)
    }

    /// One batch of the library, newest first, after the resume point. Where a
    /// batch ends is decided by `PhotoBackupListingWindow`, which never ends it
    /// inside a moment, so that photos made at one moment (a burst), whose order
    /// the library does not promise, are listed together and the next batch
    /// starts strictly before that moment. Photos with no creation date come with
    /// the last batch.
    func listBatch(after resumePoint: PhotoBackupResumePoint?, limit: Int, includesHidden: Bool) async throws -> PhotoBackupListingBatch {
        let options = Self.fetchOptions(includesHidden: includesHidden)
        let datedOnly = NSPredicate(format: "creationDate != nil")
        var predicates: [NSPredicate] = [datedOnly]
        if let existing = options.predicate { predicates.append(existing) }
        if let resumePoint {
            predicates.append(
                NSPredicate(format: "creationDate <= %@", PhotoBackupListingWindow.newestCreationDate(after: resumePoint) as NSDate))
        }
        options.predicate = NSCompoundPredicate(andPredicateWithSubpredicates: predicates)
        options.sortDescriptors = [NSSortDescriptor(key: "creationDate", ascending: false)]
        let result = PHAsset.fetchAssets(with: options)
        let takeCount = PhotoBackupListingWindow.takeCount(candidateCount: result.count, limit: limit) { position in
            result.object(at: position).creationDate ?? Date.distantPast
        }
        var entries: [PhotoBackupLibraryEntry] = []
        var position = 0
        while position < takeCount {
            let asset = result.object(at: position)
            entries.append(PhotoBackupLibraryEntry(localIdentifier: asset.localIdentifier, creationDate: asset.creationDate))
            position += 1
        }
        let isEndOfDatedItems = takeCount >= result.count
        if isEndOfDatedItems {
            // The fetch leaves out the photos with no creation date; they are
            // listed with the last batch.
            entries.append(contentsOf: Self.entriesWithoutCreationDate(includesHidden: includesHidden))
        }
        return PhotoBackupListingBatch(entries: entries, isLast: isEndOfDatedItems)
    }

    /// The items the library holds no creation date for, which are rare. Asked
    /// for directly, since where the library sorts a missing date is not promised.
    private static func entriesWithoutCreationDate(includesHidden: Bool) -> [PhotoBackupLibraryEntry] {
        let options = fetchOptions(includesHidden: includesHidden)
        let undatedOnly = NSPredicate(format: "creationDate == nil")
        var predicates: [NSPredicate] = [undatedOnly]
        if let existing = options.predicate { predicates.append(existing) }
        options.predicate = NSCompoundPredicate(andPredicateWithSubpredicates: predicates)
        let result = PHAsset.fetchAssets(with: options)
        var entries: [PhotoBackupLibraryEntry] = []
        var position = 0
        while position < result.count {
            let asset = result.object(at: position)
            entries.append(PhotoBackupLibraryEntry(localIdentifier: asset.localIdentifier, creationDate: nil))
            position += 1
        }
        return entries
    }

    func items(withLocalIdentifiers localIdentifiers: [String], includesHidden: Bool) async throws -> [PhotoBackupItem] {
        Self.items(for: Self.assets(withLocalIdentifiers: localIdentifiers, includesHidden: includesHidden))
    }

    func changes(since tokenData: Data, includesHidden: Bool) async throws -> PhotoBackupChanges {
        guard let token = try? NSKeyedUnarchiver.unarchivedObject(ofClass: PHPersistentChangeToken.self, from: tokenData) else {
            throw PhotoBackupLibraryError.changeTokenExpired
        }
        let library = PHPhotoLibrary.shared()
        let newToken = Self.archivedCurrentChangeToken()
        var localIdentifiers: Set<String> = []
        do {
            let changes = try library.fetchPersistentChanges(since: token)
            // PHPersistentChangeFetchResult is an NSFastEnumeration, so its
            // elements may come as Any.
            for case let change as PHPersistentChange in changes {
                let details = try change.changeDetails(for: .asset)
                // A photo deleted on the phone is left on the server: only what
                // was added or changed is read.
                localIdentifiers.formUnion(details.insertedLocalIdentifiers)
                localIdentifiers.formUnion(details.updatedLocalIdentifiers)
            }
        } catch {
            // Whatever the library says (the token expired, its details are
            // gone, or it is a token this library does not know, from another
            // device or a restored backup), changes since it cannot be read:
            // the library is listed again, skipping what the index has.
            throw PhotoBackupLibraryError.changeTokenExpired
        }
        // The identifiers only: the items are built a batch at a time, and the
        // ones the library no longer lists (deleted, not the person's own, in
        // the Hidden album when it is left out) are dropped when they are asked for.
        let totalCount = PHAsset.fetchAssets(with: Self.fetchOptions(includesHidden: includesHidden)).count
        return PhotoBackupChanges(localIdentifiers: Array(localIdentifiers), totalCount: totalCount, changeToken: newToken)
    }

    private static func archivedCurrentChangeToken() -> Data? {
        try? NSKeyedArchiver.archivedData(withRootObject: PHPhotoLibrary.shared().currentChangeToken, requiringSecureCoding: true)
    }

    /// Pictures and videos of the person's own library: not the photos of other
    /// people's shared albums, and the Hidden album only if allowed.
    private static func fetchOptions(includesHidden: Bool) -> PHFetchOptions {
        let options = PHFetchOptions()
        options.predicate = NSPredicate(
            format: "mediaType == %ld OR mediaType == %ld", PHAssetMediaType.image.rawValue, PHAssetMediaType.video.rawValue)
        options.includeHiddenAssets = includesHidden
        options.includeAllBurstAssets = true
        options.includeAssetSourceTypes = [.typeUserLibrary]
        return options
    }

    private static func assets(withLocalIdentifiers localIdentifiers: [String], includesHidden: Bool) -> [PHAsset] {
        guard !localIdentifiers.isEmpty else { return [] }
        let result = PHAsset.fetchAssets(withLocalIdentifiers: localIdentifiers, options: fetchOptions(includesHidden: includesHidden))
        var assets: [PHAsset] = []
        result.enumerateObjects { asset, _, _ in assets.append(asset) }
        return assets
    }

    // MARK: Turning assets into items

    private static func items(for assets: [PHAsset]) -> [PhotoBackupItem] {
        let cloudIdentifiers = cloudIdentifiers(for: assets.map(\.localIdentifier))
        return assets.compactMap { asset in
            let resources = backupResources(of: asset)
            guard !resources.isEmpty else { return nil }
            let coordinate = asset.location?.coordinate
            let isLivePhoto = asset.mediaSubtypes.contains(.photoLive) && resources.contains { $0.role == .liveVideo }
            return PhotoBackupItem(
                localIdentifier: asset.localIdentifier, cloudIdentifier: cloudIdentifiers[asset.localIdentifier], creationDate: asset.creationDate,
                modificationDate: asset.modificationDate,
                location: coordinate.map { PhotoBackupLocation(latitude: $0.latitude, longitude: $0.longitude) }, isFavorite: asset.isFavorite,
                isScreenshot: asset.mediaSubtypes.contains(.photoScreenshot),
                mediaKind: isLivePhoto ? .livePhoto : (asset.mediaType == .video ? .video : .picture), resources: resources)
        }
    }

    /// iCloud's name for each photo, where the library has one. Asked in
    /// batches; a photo iCloud does not know yet is named by the phone instead.
    private static func cloudIdentifiers(for localIdentifiers: [String]) -> [String: String] {
        var identifiers: [String: String] = [:]
        let library = PHPhotoLibrary.shared()
        var start = 0
        while start < localIdentifiers.count {
            let batch = Array(localIdentifiers[start..<min(start + 200, localIdentifiers.count)])
            start += batch.count
            for (localIdentifier, mapping) in library.cloudIdentifierMappings(forLocalIdentifiers: batch) {
                if case .success(let cloudIdentifier) = mapping, !cloudIdentifier.stringValue.isEmpty {
                    identifiers[localIdentifier] = cloudIdentifier.stringValue
                }
            }
        }
        return identifiers
    }

    /// The files of an asset that backup sends, by role. The original is the
    /// photo or the video; the edit is the full size photo or video the library
    /// made when the person edited it; a Live Photo's video is its paired video,
    /// the original one (the server keeps one video per photo, and a backup
    /// keeps what the camera made). Other resources, such as the alternate photo
    /// of a RAW pair or the adjustment data, are not sent.
    private static func backupResources(of asset: PHAsset) -> [PhotoBackupResource] {
        var original: PHAssetResource?
        var edited: PHAssetResource?
        var pairedVideo: PHAssetResource?
        for resource in PHAssetResource.assetResources(for: asset) {
            switch resource.type {
            case .photo, .video:
                if original == nil { original = resource }
            case .fullSizePhoto, .fullSizeVideo:
                if edited == nil { edited = resource }
            case .pairedVideo:
                if pairedVideo == nil { pairedVideo = resource }
            default:
                break
            }
        }
        var resources: [PhotoBackupResource] = []
        for (role, resource) in [(PhotoBackupFileRole.original, original), (.liveVideo, pairedVideo), (.edited, edited)] {
            guard let resource else { continue }
            // PhotoKit does not say whether a file is on the phone; the export
            // finds out, and waits on a network that costs.
            resources.append(
                PhotoBackupResource(
                    role: role, originalFilename: resource.originalFilename, contentType: contentType(of: resource), isLocallyAvailable: true,
                    fileSizeBytes: fileSizeBytes(of: resource)))
        }
        return resources
    }

    /// The size of the file as PhotoKit knows it, where it says: it has no
    /// public property for it, but the resource answers `fileSize` on the
    /// versions that do. The size lets a large video wait for room without
    /// being exported to find out how large it is.
    private static func fileSizeBytes(of resource: PHAssetResource) -> Int64? {
        let fileSizeKey = "fileSize"
        guard resource.responds(to: NSSelectorFromString(fileSizeKey)) else { return nil }
        guard let size = (resource.value(forKey: fileSizeKey) as? NSNumber)?.int64Value, size > 0 else { return nil }
        return size
    }

    private static func contentType(of resource: PHAssetResource) -> String {
        if let type = UTType(resource.uniformTypeIdentifier), let mimeType = type.preferredMIMEType { return mimeType }
        let fileExtension = (resource.originalFilename as NSString).pathExtension
        return UTType(filenameExtension: fileExtension)?.preferredMIMEType ?? "application/octet-stream"
    }

    // MARK: Exporting

    func export(_ upload: PhotoBackupUpload, to directory: URL, mayUseExpensiveNetwork: Bool, includesHidden: Bool) async throws -> PhotoBackupExportedFile {
        let outgoingFilename = UUID().uuidString
        let fileURL = directory.appendingPathComponent(outgoingFilename)
        do {
            try await Self.write(upload, to: fileURL, mayUseExpensiveNetwork: mayUseExpensiveNetwork, includesHidden: includesHidden)
        } catch let error as PhotoBackupLibraryError {
            try? FileManager.default.removeItem(at: fileURL)
            throw error
        } catch {
            try? FileManager.default.removeItem(at: fileURL)
            throw Self.libraryError(for: error)
        }
        do {
            // The same attributes as the rest of a file written beside it: readable
            // once the phone has been unlocked after starting, which is when a
            // background upload can run.
            try FileManager.default.setAttributes(PhotoBackupOutgoingFile.attributes, ofItemAtPath: fileURL.path)
            let attributes = try FileManager.default.attributesOfItem(atPath: fileURL.path)
            let sizeBytes = (attributes[.size] as? NSNumber)?.int64Value ?? 0
            guard sizeBytes > 0 else {
                try? FileManager.default.removeItem(at: fileURL)
                throw PhotoBackupLibraryError.failed(.fileIsEmpty)
            }
            // Read once more, in pieces, for the digest the upload declares: a
            // picture the server already has (kept from a conversation, say) is
            // then recognized and does not need room again.
            return PhotoBackupExportedFile(outgoingFilename: outgoingFilename, sizeBytes: sizeBytes, sha256: PhotoFileDigest.sha256(ofFileAt: fileURL))
        } catch let error as PhotoBackupLibraryError {
            throw error
        } catch {
            try? FileManager.default.removeItem(at: fileURL)
            throw PhotoBackupLibraryError.unavailable(.systemDescription(error.localizedDescription))
        }
    }

    /// Writes the file. It is first asked for without the network; if the file
    /// is in iCloud only, and the network may be used, it is asked for again
    /// with it.
    private static func write(_ upload: PhotoBackupUpload, to fileURL: URL, mayUseExpensiveNetwork: Bool, includesHidden: Bool) async throws {
        guard let resource = resource(of: upload, includesHidden: includesHidden) else {
            throw PhotoBackupLibraryError.failed(.fileIsNotInLibrary)
        }
        let manager = PHAssetResourceManager.default()
        let options = PHAssetResourceRequestOptions()
        options.isNetworkAccessAllowed = false
        do {
            try await manager.writeData(for: resource, toFile: fileURL, options: options)
            return
        } catch {
            guard isNetworkNeeded(error) else { throw error }
        }
        if !mayUseExpensiveNetwork, await DeviceNetwork().isExpensive() {
            throw PhotoBackupLibraryError.unavailable(.fileIsInICloudOnMobileData)
        }
        try? FileManager.default.removeItem(at: fileURL)
        options.isNetworkAccessAllowed = true
        try await manager.writeData(for: resource, toFile: fileURL, options: options)
    }

    /// The resource an upload is of, read again from the library. A hidden
    /// photo is found only if the person allowed the Hidden album, the same
    /// setting that listed it.
    private static func resource(of upload: PhotoBackupUpload, includesHidden: Bool) -> PHAssetResource? {
        let result = PHAsset.fetchAssets(withLocalIdentifiers: [upload.localIdentifier], options: fetchOptions(includesHidden: includesHidden))
        guard let asset = result.firstObject else { return nil }
        for resource in PHAssetResource.assetResources(for: asset) {
            switch (upload.role, resource.type) {
            case (.original, .photo), (.original, .video), (.edited, .fullSizePhoto), (.edited, .fullSizeVideo), (.liveVideo, .pairedVideo):
                return resource
            default:
                continue
            }
        }
        return nil
    }

    private static func isNetworkNeeded(_ error: Error) -> Bool {
        guard let error = error as? PHPhotosError else { return true }
        return error.code == .networkAccessRequired || error.code == .networkError || error.code == .invalidResource
            || error.code == .missingResource
    }

    /// A file that is not there and never will be is the file's fault; anything
    /// else (no signal, no room, a locked phone) is tried again later.
    private static func libraryError(for error: Error) -> PhotoBackupLibraryError {
        if let error = error as? PHPhotosError {
            switch error.code {
            case .identifierNotFound, .invalidResource, .missingResource:
                return .failed(.systemDescription(error.localizedDescription))
            default:
                return .unavailable(.systemDescription(error.localizedDescription))
            }
        }
        return .unavailable(.systemDescription(error.localizedDescription))
    }
}

/// DeviceNetwork says whether the phone's network costs, which is mobile data
/// or a personal hotspot.
struct DeviceNetwork: PhotoBackupNetwork {
    func isExpensive() async -> Bool {
        await state().isExpensive
    }

    func state() async -> PhotoBackupNetworkState {
        await withCheckedContinuation { (continuation: CheckedContinuation<PhotoBackupNetworkState, Never>) in
            let reader = PathReader(continuation)
            reader.start()
        }
    }
}

/// PathReader takes the first network path NWPathMonitor reports, and gives up
/// after a second, as no path then means no network to judge: it is taken to
/// be reachable and to cost nothing, so that backup tries rather than waits.
private final class PathReader: @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<PhotoBackupNetworkState, Never>?
    private let monitor = NWPathMonitor()
    private let queue = DispatchQueue(label: PhotoBackupEngine.identifierPrefix + ".photo-backup.network")

    init(_ continuation: CheckedContinuation<PhotoBackupNetworkState, Never>) {
        self.continuation = continuation
    }

    func start() {
        monitor.pathUpdateHandler = { [self] path in
            finish(PhotoBackupNetworkState(isSatisfied: path.status == .satisfied, isExpensive: path.isExpensive))
        }
        monitor.start(queue: queue)
        queue.asyncAfter(deadline: .now() + 1) { [self] in finish(PhotoBackupNetworkState(isSatisfied: true, isExpensive: false)) }
    }

    private func finish(_ state: PhotoBackupNetworkState) {
        let waiting = lock.withLock {
            let waiting = continuation
            continuation = nil
            return waiting
        }
        guard let waiting else { return }
        monitor.pathUpdateHandler = nil
        monitor.cancel()
        waiting.resume(returning: state)
    }
}
