import Foundation

/// ChartShares is what the app's charts need that is not drawing: how many
/// groups get a slice and a color of their own, which slice a touch on a
/// donut lands in, and which point of a line is nearest a touch.
public enum ChartShares {
    /// How many groups, largest first, get a slice and a color of their
    /// own. The rest share one slice, so the colors stay few enough to
    /// tell apart.
    public static let ownSliceLimit = 8

    /// The slice a group is drawn in, by its rank among the groups, largest
    /// first: its own, or the one the rest share, numbered `ownSliceLimit`.
    public static func sliceIndex(ofRank rank: Int) -> Int {
        min(rank, ownSliceLimit)
    }

    /// The slices' amounts: each of the first `ownSliceLimit` on its own,
    /// then the rest added up as one, when there is a rest.
    public static func sliceAmounts(_ amounts: [Decimal]) -> [Decimal] {
        guard amounts.count > ownSliceLimit else { return amounts }
        let restAmount = amounts.dropFirst(ownSliceLimit).reduce(Decimal(0), +)
        return Array(amounts.prefix(ownSliceLimit)) + [restAmount]
    }

    /// The slice a donut's angle selection falls in. Swift Charts reports
    /// the selection as a running total from the start of the first slice,
    /// so it is the first slice whose running total reaches it.
    public static func sliceIndex(atRunningTotal runningTotal: Double, amounts: [Double]) -> Int? {
        guard !amounts.isEmpty else { return nil }
        var reachedAmount = 0.0
        for (index, amount) in amounts.enumerated() {
            reachedAmount += amount
            if runningTotal <= reachedAmount { return index }
        }
        return amounts.count - 1
    }

    /// The point nearest the date a finger is on, for a line chart: a
    /// touch between two points picks whichever is closer.
    public static func nearestIndex(to date: Date, in dates: [Date]) -> Int? {
        dates.indices.min { left, right in
            abs(dates[left].timeIntervalSince(date)) < abs(dates[right].timeIntervalSince(date))
        }
    }
}
