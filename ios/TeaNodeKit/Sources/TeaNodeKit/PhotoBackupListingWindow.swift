import Foundation

/// PhotoBackupListingWindow decides where a batch of the listing of the photo
/// library ends. The library is listed newest first, a batch at a time, and
/// photos made at one moment (a burst) come in an order the library does not
/// promise. A batch that ended between two of them would leave the rest of the
/// burst to the next batch, which starts strictly after the last photo taken,
/// so those photos would be skipped. The rule lives here, apart from PhotoKit,
/// so that the tests run the very rule the app runs.
public enum PhotoBackupListingWindow {
    /// Photos made within this many seconds of each other are made at the same
    /// moment as far as the listing is concerned: two readings of one date need
    /// not agree to the last bit.
    public static let sameMomentSeconds: TimeInterval = 0.001

    /// The newest creation date the batch after `resumePoint` may hold. The
    /// resume point is the last photo of the batch before, and a photo made
    /// less than `sameMomentSeconds` after it was taken with that batch.
    public static func newestCreationDate(after resumePoint: PhotoBackupResumePoint) -> Date {
        resumePoint.creationDate.addingTimeInterval(-sameMomentSeconds)
    }

    /// How many of the photos with a creation date a batch takes, given the
    /// ones the library has left, newest first: `limit` of them, and then on
    /// for as long as the next was made at the moment of the one before it, so
    /// that a burst is never cut in two. `creationDateAt` gives the date of the
    /// photo at a position. It is read only for the positions from `limit` on,
    /// so a batch does not read the dates of everything the library has left.
    public static func takeCount(candidateCount: Int, limit: Int, creationDateAt: (Int) -> Date) -> Int {
        let minimumCount = max(1, limit)
        var count = 0
        while count < candidateCount {
            if count >= minimumCount, creationDateAt(count) <= creationDateAt(count - 1).addingTimeInterval(-sameMomentSeconds) { break }
            count += 1
        }
        return count
    }
}
