import Foundation

// What a sample sends of its metadata. The app that wrote a sample decides what
// is in its HealthKit metadata, and some write a lot; the server refuses a
// sample whose metadata text is over a limit, and a refused sample is dropped
// for good with the anchor moved past it. So the phone keeps the metadata
// within the limit by dropping keys, never the sample.

/// HealthMetadataFit keeps the metadata of a sample within what the server takes.
public enum HealthMetadataFit {
    /// The most bytes of metadata JSON text the server takes for one sample
    /// (healthMetadataMostBytes in internal/api/v1api/apigraph/health.go).
    public static let byteCountMost = 8 * 1024

    /// The keys the app writes itself from what it knows of a sample: the
    /// totals of a workout, the classification of an electrocardiogram, the
    /// points of an audiogram, what a State of Mind entry says. The charts and
    /// the day summaries read these, so they are dropped last, after every key
    /// the app that wrote the sample put there.
    public static let ownKeys: Set<String> = [
        HealthWorkoutMetadata.totalEnergyBurnedKilocalories, HealthWorkoutMetadata.totalDistanceMeters, "workoutActivityTypeRawValue",
        "durationSeconds", "electrocardiogramClassification", "averageHeartRateBeatsPerMinute", "audiogramPoints", "stateOfMindKind",
        "stateOfMindValence", "stateOfMindLabels",
    ]

    /// The metadata within the most bytes. Over it, keys go one at a time,
    /// the one whose text is the longest first, those the writing app put
    /// there before the app's own, until it fits. A text with a null
    /// character in it loses the character, which the server would refuse.
    public static func fitting(_ metadata: [String: JSONValue], byteCountMost: Int = HealthMetadataFit.byteCountMost) -> [String: JSONValue] {
        var kept: [String: JSONValue] = [:]
        for (key, value) in metadata { kept[withoutNull(key)] = withoutNull(value) }
        while HealthJSON.text(kept).utf8.count > byteCountMost, !kept.isEmpty {
            let candidates = kept.keys.filter { !ownKeys.contains($0) }
            let droppable = candidates.isEmpty ? Array(kept.keys) : candidates
            let longestKey = droppable.sorted().max { byteCount(of: $0, in: kept) < byteCount(of: $1, in: kept) }
            guard let longestKey else { break }
            kept[longestKey] = nil
        }
        return kept
    }

    private static func byteCount(of key: String, in metadata: [String: JSONValue]) -> Int {
        HealthJSON.text([key: metadata[key] ?? .null]).utf8.count
    }

    /// Text with its null characters taken out: JSON writes one as \u0000,
    /// which the server refuses.
    public static func withoutNull(_ text: String) -> String {
        text.contains("\u{0}") ? text.replacingOccurrences(of: "\u{0}", with: "") : text
    }

    private static func withoutNull(_ value: JSONValue) -> JSONValue {
        switch value {
        case .string(let text): return .string(withoutNull(text))
        case .array(let elements): return .array(elements.map { withoutNull($0) })
        case .object(let members):
            return .object(Dictionary(members.map { (withoutNull($0.key), withoutNull($0.value)) }, uniquingKeysWith: { first, _ in first }))
        case .null, .bool, .number: return value
        }
    }
}
