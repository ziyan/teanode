import Foundation

/// PhotoPreviewCache keeps the bytes of photo previews on disk, so a grid read
/// again, or scrolled back to, does not fetch every picture again. An
/// `NSCache` in memory sits in front of the files.
///
/// A file is named by a hash of the account and the address it was read from.
/// The address carries the time the photo last changed, so a preview made
/// again is a new address and the old file simply ages out. The files together
/// stay under a limit, 500 MB by default: when a write takes them over it,
/// the files least recently used (the oldest by modification date; a read
/// brings a file's date up to now) are removed until they fit again.
///
/// The files are only a cache. Any failure to read or write one is ignored and
/// the preview is fetched from the server instead.
public actor PhotoPreviewCache {
    /// How much the files may take together, unless the app says otherwise.
    public static let defaultLimitBytes = 500 * 1024 * 1024

    /// Where the files are.
    public nonisolated let directory: URL
    private let limitBytes: Int
    private let memory = NSCache<NSString, NSData>()
    private var inFlight: [String: Task<Data, Error>] = [:]
    /// The size of the files, counted once from the directory and then kept up
    /// to date as files are written and removed.
    private var knownBytes: Int?

    /// - Parameters:
    ///   - directory: where to keep the files; the app's caches directory when nil. A test gives a temporary one.
    ///   - limitBytes: the most the files may take together.
    ///   - memoryLimitBytes: the most the memory in front of them holds.
    public init(directory: URL? = nil, limitBytes: Int = PhotoPreviewCache.defaultLimitBytes, memoryLimitBytes: Int = 64 * 1024 * 1024) {
        let caches = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first ?? FileManager.default.temporaryDirectory
        self.directory = directory ?? caches.appendingPathComponent("PhotoPreviews", isDirectory: true)
        self.limitBytes = limitBytes
        memory.totalCostLimit = memoryLimitBytes
    }

    /// The bytes at this address on the server: from memory, from a file, or
    /// else fetched through the connection, so the request is signed, and kept.
    public func data(for path: String, using connection: Connection) async throws -> Data {
        let name = Self.fileName(accountKey: connection.accountKey, path: path)
        if let held = memory.object(forKey: name as NSString) {
            return Data(referencing: held)
        }
        if let stored = readFile(named: name) {
            memory.setObject(NSData(data: stored), forKey: name as NSString, cost: stored.count)
            return stored
        }
        if let fetching = inFlight[name] {
            return try await fetching.value
        }
        let fetching = Task { try await connection.download(path) }
        inFlight[name] = fetching
        defer { inFlight[name] = nil }
        let fetched = try await fetching.value
        memory.setObject(NSData(data: fetched), forKey: name as NSString, cost: fetched.count)
        writeFile(fetched, named: name)
        return fetched
    }

    /// Forgets everything: the files and what is held in memory.
    public func removeAll() {
        memory.removeAllObjects()
        try? FileManager.default.removeItem(at: directory)
        knownBytes = 0
    }

    /// How much the files take together.
    public func sizeBytes() -> Int {
        currentBytes()
    }

    /// The file name for an address: 32 hexadecimal digits, made of two FNV-1a
    /// hashes of 64 bits that start from different values. The cache needs a
    /// name that is the same on every launch (Swift's own hash is not) and that
    /// two different addresses will not share by accident; it has no one to
    /// defend against, so a cryptographic digest, which CryptoKit gives on the
    /// phone only, is not needed.
    nonisolated static func fileName(accountKey: String, path: String) -> String {
        var first: UInt64 = 0xcbf2_9ce4_8422_2325
        var second: UInt64 = 0x84222325_cbf29ce4
        for byte in "\(accountKey)\n\(path)".utf8 {
            first = (first ^ UInt64(byte)) &* 0x0000_0100_0000_01b3
            second = (second ^ UInt64(byte)) &* 0x0000_0100_0000_01b3
        }
        return String(format: "%016llx%016llx", first, second)
    }

    private func readFile(named name: String) -> Data? {
        let address = directory.appendingPathComponent(name)
        guard let stored = try? Data(contentsOf: address) else { return nil }
        // Used now, so it is the last to go.
        try? FileManager.default.setAttributes([.modificationDate: Date()], ofItemAtPath: address.path)
        return stored
    }

    private func writeFile(_ bytes: Data, named name: String) {
        let manager = FileManager.default
        // Counted before the file is written, so it is not counted twice.
        let bytesBefore = currentBytes()
        do {
            try manager.createDirectory(at: directory, withIntermediateDirectories: true)
            let address = directory.appendingPathComponent(name)
            let replacedBytes = ((try? manager.attributesOfItem(atPath: address.path))?[.size] as? NSNumber)?.intValue ?? 0
            try bytes.write(to: address, options: .atomic)
            knownBytes = max(0, bytesBefore - replacedBytes) + bytes.count
        } catch {
            return
        }
        if currentBytes() > limitBytes { removeOldestFiles() }
    }

    private func currentBytes() -> Int {
        if let knownBytes { return knownBytes }
        let counted = filesInDirectory().reduce(0) { $0 + $1.sizeBytes }
        knownBytes = counted
        return counted
    }

    /// Removes the files least recently used until the rest fit.
    private func removeOldestFiles() {
        var files = filesInDirectory().sorted { $0.modifiedAt < $1.modifiedAt }
        var remainingBytes = files.reduce(0) { $0 + $1.sizeBytes }
        while remainingBytes > limitBytes, !files.isEmpty {
            let oldest = files.removeFirst()
            if (try? FileManager.default.removeItem(at: oldest.address)) != nil {
                remainingBytes -= oldest.sizeBytes
                memory.removeObject(forKey: oldest.address.lastPathComponent as NSString)
            }
        }
        knownBytes = remainingBytes
    }

    private struct StoredFile {
        let address: URL
        let sizeBytes: Int
        let modifiedAt: Date
    }

    private func filesInDirectory() -> [StoredFile] {
        let keys: [URLResourceKey] = [.fileSizeKey, .contentModificationDateKey]
        guard let addresses = try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: keys) else { return [] }
        return addresses.compactMap { address in
            guard let values = try? address.resourceValues(forKeys: Set(keys)), let sizeBytes = values.fileSize else { return nil }
            return StoredFile(address: address, sizeBytes: sizeBytes, modifiedAt: values.contentModificationDate ?? .distantPast)
        }
    }
}
