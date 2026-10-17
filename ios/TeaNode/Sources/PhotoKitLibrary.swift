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

    func listItems(skipping known: Set<String>, includesHidden: Bool) async throws -> PhotoBackupListing {
        // Taken before the listing, so that whatever changes while it runs is
        // read again by the next pass rather than missed.
        let changeToken = Self.archivedCurrentChangeToken()
        let options = Self.fetchOptions(includesHidden: includesHidden)
        options.sortDescriptors = [NSSortDescriptor(key: "creationDate", ascending: true)]
        let result = PHAsset.fetchAssets(with: options)
        var assets: [PHAsset] = []
        result.enumerateObjects { asset, _, _ in
            if !known.contains(asset.localIdentifier) { assets.append(asset) }
        }
        return PhotoBackupListing(items: Self.items(for: assets), totalCount: result.count, changeToken: changeToken)
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
        } catch let error as PHPhotosError where error.code == .persistentChangeTokenExpired || error.code == .persistentChangeDetailsUnavailable {
            throw PhotoBackupLibraryError.changeTokenExpired
        }
        let assets = Self.assets(withLocalIdentifiers: Array(localIdentifiers), includesHidden: includesHidden)
        let totalCount = PHAsset.fetchAssets(with: Self.fetchOptions(includesHidden: includesHidden)).count
        return PhotoBackupChanges(items: Self.items(for: assets), totalCount: totalCount, changeToken: newToken)
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
                modificationDate: asset.modificationDate, timeZone: .current,
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
                    role: role, originalFilename: resource.originalFilename, contentType: contentType(of: resource), isLocallyAvailable: true))
        }
        return resources
    }

    private static func contentType(of resource: PHAssetResource) -> String {
        if let type = UTType(resource.uniformTypeIdentifier), let mimeType = type.preferredMIMEType { return mimeType }
        let fileExtension = (resource.originalFilename as NSString).pathExtension
        return UTType(filenameExtension: fileExtension)?.preferredMIMEType ?? "application/octet-stream"
    }

    // MARK: Exporting

    func export(_ upload: PhotoBackupUpload, to directory: URL, mayUseExpensiveNetwork: Bool) async throws -> PhotoBackupExportedFile {
        let fileName = UUID().uuidString
        let fileURL = directory.appendingPathComponent(fileName)
        do {
            try await Self.write(upload, to: fileURL, mayUseExpensiveNetwork: mayUseExpensiveNetwork)
        } catch let error as PhotoBackupLibraryError {
            try? FileManager.default.removeItem(at: fileURL)
            throw error
        } catch {
            try? FileManager.default.removeItem(at: fileURL)
            throw Self.libraryError(for: error)
        }
        do {
            // Readable once the phone has been unlocked after starting, which is
            // when a background upload can run.
            try FileManager.default.setAttributes(
                [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication, .posixPermissions: 0o600], ofItemAtPath: fileURL.path)
            let attributes = try FileManager.default.attributesOfItem(atPath: fileURL.path)
            let sizeBytes = (attributes[.size] as? NSNumber)?.int64Value ?? 0
            guard sizeBytes > 0 else {
                try? FileManager.default.removeItem(at: fileURL)
                throw PhotoBackupLibraryError.failed("The photo library gave an empty file.")
            }
            return PhotoBackupExportedFile(fileName: fileName, sizeBytes: sizeBytes)
        } catch let error as PhotoBackupLibraryError {
            throw error
        } catch {
            try? FileManager.default.removeItem(at: fileURL)
            throw PhotoBackupLibraryError.unavailable(error.localizedDescription)
        }
    }

    /// Writes the file. It is first asked for without the network; if the file
    /// is in iCloud only, and the network may be used, it is asked for again
    /// with it.
    private static func write(_ upload: PhotoBackupUpload, to fileURL: URL, mayUseExpensiveNetwork: Bool) async throws {
        guard let resource = resource(of: upload) else {
            throw PhotoBackupLibraryError.failed("This photo is no longer in the library.")
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
            throw PhotoBackupLibraryError.unavailable("The file is in iCloud and this network costs data.")
        }
        try? FileManager.default.removeItem(at: fileURL)
        options.isNetworkAccessAllowed = true
        try await manager.writeData(for: resource, toFile: fileURL, options: options)
    }

    /// The resource an upload is of, read again from the library.
    private static func resource(of upload: PhotoBackupUpload) -> PHAssetResource? {
        let result = PHAsset.fetchAssets(withLocalIdentifiers: [upload.localIdentifier], options: fetchOptions(includesHidden: true))
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
                return .failed(error.localizedDescription)
            default:
                return .unavailable(error.localizedDescription)
            }
        }
        return .unavailable(error.localizedDescription)
    }
}

/// DeviceNetwork says whether the phone's network costs, which is mobile data
/// or a personal hotspot.
struct DeviceNetwork: PhotoBackupNetwork {
    func isExpensive() async -> Bool {
        await withCheckedContinuation { (continuation: CheckedContinuation<Bool, Never>) in
            let reader = PathReader(continuation)
            reader.start()
        }
    }
}

/// PathReader takes the first network path NWPathMonitor reports, and gives up
/// after a second, as no path then means no network to judge.
private final class PathReader: @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<Bool, Never>?
    private let monitor = NWPathMonitor()
    private let queue = DispatchQueue(label: "com.teanode.app.photo-backup.network")

    init(_ continuation: CheckedContinuation<Bool, Never>) {
        self.continuation = continuation
    }

    func start() {
        monitor.pathUpdateHandler = { [self] path in finish(path.isExpensive) }
        monitor.start(queue: queue)
        queue.asyncAfter(deadline: .now() + 1) { [self] in finish(false) }
    }

    private func finish(_ isExpensive: Bool) {
        let waiting = lock.withLock {
            let waiting = continuation
            continuation = nil
            return waiting
        }
        guard let waiting else { return }
        monitor.pathUpdateHandler = nil
        monitor.cancel()
        waiting.resume(returning: isExpensive)
    }
}
