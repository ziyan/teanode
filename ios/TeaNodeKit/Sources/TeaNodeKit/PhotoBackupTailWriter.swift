import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

// The rest of a file that was cut off, written beside it so that a background
// upload, which reads a file, can send it. A protocol, so that a test can make
// the write fail or stand still half way, which a real disk will not do on cue.

/// PhotoBackupOutgoingFile is how every file the backup writes for an upload
/// is kept on the phone: the exported file and the rest of it alike.
public enum PhotoBackupOutgoingFile {
    /// Readable by the app alone, and once the phone has been unlocked after
    /// starting, which is when a background upload can run. The protection
    /// class is the phone's; the file system of a test machine has none.
    public static var attributes: [FileAttributeKey: Any] {
        var attributes: [FileAttributeKey: Any] = [.posixPermissions: 0o600]
        #if canImport(UIKit)
            attributes[.protectionKey] = FileProtectionType.completeUntilFirstUserAuthentication
        #endif
        return attributes
    }
}

/// PhotoBackupTailWriter writes the bytes of a file from an offset to its end
/// into a new file.
public protocol PhotoBackupTailWriter: Sendable {
    /// Writes the bytes of `sourceURL` from `offsetBytes` to its end into a new
    /// file at `destinationURL`. `isCancelled` is asked between pieces: when it
    /// says yes the write stops with a `CancellationError`. A write that does
    /// not finish, whatever the reason, leaves no file behind.
    func writeTail(of sourceURL: URL, from offsetBytes: Int64, to destinationURL: URL, isCancelled: @Sendable () -> Bool) throws
}

/// PhotoBackupFileTailWriter copies the tail of a file a piece at a time, so
/// that a long video is never held in memory.
public struct PhotoBackupFileTailWriter: PhotoBackupTailWriter {
    /// How much of a file is read and written at a time.
    static let pieceBytes = 1 << 20

    public init() {}

    public func writeTail(of sourceURL: URL, from offsetBytes: Int64, to destinationURL: URL, isCancelled: @Sendable () -> Bool) throws {
        guard FileManager.default.createFile(atPath: destinationURL.path, contents: nil, attributes: PhotoBackupOutgoingFile.attributes) else {
            throw CocoaError(.fileWriteUnknown)
        }
        do {
            try copy(from: sourceURL, offsetBytes: offsetBytes, to: destinationURL, isCancelled: isCancelled)
        } catch {
            // A tail cut short is not a tail: nothing may send it, and a full
            // disk should not stay full of it.
            try? FileManager.default.removeItem(at: destinationURL)
            throw error
        }
    }

    private func copy(from sourceURL: URL, offsetBytes: Int64, to destinationURL: URL, isCancelled: @Sendable () -> Bool) throws {
        let reader = try FileHandle(forReadingFrom: sourceURL)
        defer { try? reader.close() }
        let writer = try FileHandle(forWritingTo: destinationURL)
        defer { try? writer.close() }
        try reader.seek(toOffset: UInt64(offsetBytes))
        var isMore = true
        while isMore {
            if isCancelled() { throw CancellationError() }
            // A pool of its own for each piece, so that what a read hands back
            // is freed with it.
            isMore = try Self.inAutoreleasePool {
                guard let piece = try reader.read(upToCount: Self.pieceBytes), !piece.isEmpty else { return false }
                try writer.write(contentsOf: piece)
                return true
            }
        }
    }

    private static func inAutoreleasePool<Result>(_ body: () throws -> Result) rethrows -> Result {
        #if canImport(Darwin)
            return try autoreleasepool(invoking: body)
        #else
            return try body()
        #endif
    }
}
