import Foundation

#if canImport(CryptoKit)
    import CryptoKit

    /// PhotoFileDigest works out the SHA-256 of a file the backup exports, to
    /// send with the upload. CryptoKit is on Apple's platforms only, so this is
    /// too.
    public enum PhotoFileDigest {
        /// How much of the file is read at a time, so that a long video is
        /// never held in memory whole.
        static let chunkSizeBytes = 1 << 20

        /// The digest in lowercase hexadecimal, or nil when the file cannot be
        /// read: the upload then goes without it, as it is only a help.
        public static func sha256(ofFileAt fileURL: URL) -> String? {
            guard let handle = try? FileHandle(forReadingFrom: fileURL) else { return nil }
            defer { try? handle.close() }
            var hasher = SHA256()
            // Each chunk is let go before the next is read: a read can hand back
            // memory that is only freed when an autorelease pool drains, and
            // without a pool per chunk a long video's would pile up until the
            // whole file was read.
            var isReadable = true
            while isReadable {
                let isMore: Bool = autoreleasepool {
                    guard let chunk = try? handle.read(upToCount: chunkSizeBytes) else {
                        isReadable = false
                        return false
                    }
                    if chunk.isEmpty { return false }
                    hasher.update(data: chunk)
                    return true
                }
                if !isMore { break }
            }
            if !isReadable { return nil }
            return hasher.finalize().map { String(format: "%02x", $0) }.joined()
        }
    }
#endif
