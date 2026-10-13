import Foundation

/// RecordingTimelinePieces maps the recording's one timeline onto the originals it was made from. Each original sits at its
/// `timelineStartMS`, and between two of them there may be a stretch nobody captured. The player keeps one control over all
/// of them: a position becomes an original and an offset into it, and a position in a gap becomes the start of the next one.
public enum RecordingTimelinePieces {
    public struct Location: Equatable, Sendable {
        public let media: RecordingMedia
        /// How far into the original, in milliseconds.
        public let offsetMS: Int64
        /// Where on the timeline the position really lands: a position in a gap is moved to the start of the original after it.
        public let timelineMS: Int64
    }

    /// The originals that are uploaded completely, in the order they sit on the timeline.
    public static func playable(_ mediaFiles: [RecordingMedia]) -> [RecordingMedia] {
        mediaFiles.filter { $0.recordingUploadState == "complete" }.sorted { $0.timelineStartMS < $1.timelineStartMS }
    }

    /// Where an original ends on the timeline. One that does not say how long it is ends where the next one starts.
    public static func endMS(of media: RecordingMedia, in pieces: [RecordingMedia]) -> Int64 {
        if media.mediaDurationMS > 0 { return media.timelineStartMS + media.mediaDurationMS }
        let following = pieces.first { $0.timelineStartMS > media.timelineStartMS }
        return following?.timelineStartMS ?? media.timelineStartMS
    }

    public static func locate(timelineMS requestedMS: Int64, in mediaFiles: [RecordingMedia]) -> Location? {
        let pieces = playable(mediaFiles)
        guard let first = pieces.first else { return nil }
        let positionMS = max(0, requestedMS)
        if positionMS < first.timelineStartMS { return Location(media: first, offsetMS: 0, timelineMS: first.timelineStartMS) }
        for (index, piece) in pieces.enumerated().reversed() where positionMS >= piece.timelineStartMS {
            let pieceEndMS = endMS(of: piece, in: pieces)
            if positionMS < pieceEndMS { return Location(media: piece, offsetMS: positionMS - piece.timelineStartMS, timelineMS: positionMS) }
            if index + 1 < pieces.count {
                let next = pieces[index + 1]
                return Location(media: next, offsetMS: 0, timelineMS: next.timelineStartMS)
            }
            return Location(media: piece, offsetMS: pieceEndMS - piece.timelineStartMS, timelineMS: pieceEndMS)
        }
        return nil
    }

    /// The original that plays when this one ends, if there is one.
    public static func original(after media: RecordingMedia, in mediaFiles: [RecordingMedia]) -> RecordingMedia? {
        let pieces = playable(mediaFiles)
        guard let index = pieces.firstIndex(where: { $0.id == media.id }), index + 1 < pieces.count else { return nil }
        return pieces[index + 1]
    }
}
