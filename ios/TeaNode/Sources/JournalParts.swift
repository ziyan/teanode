import MapKit
import SwiftUI
import TeaNodeKit

// The pieces the day, week, month and year screens are made of.

/// JournalRecordOpener takes the person to a record a day mentions, in the
/// part of the app that shows it.
@MainActor
enum JournalRecordOpener {
    static func open(_ opening: JournalRecordOpening, in app: AppModel) {
        switch opening {
        case .mailItem(let itemId):
            app.openedMailItemId = itemId
        case .contact(let contactId):
            app.openedContactId = contactId
        case .photo(let photoId):
            app.openedSearchedPhoto = SearchedPhoto(photoId: photoId, words: "")
        case .path(let path):
            app.show(path: path)
        case .part(let name):
            if let section = AppSection(rawValue: name) { app.openedSection = section }
        case .nowhere:
            break
        }
    }

    /// Opens the day's photos in Photos, the library narrowed to that day.
    static func openPhotos(ofDay day: String, in app: AppModel) {
        guard let date = JournalDays.date(day) else { return }
        var photoFilter = PhotoFilter()
        photoFilter.firstDay = date
        photoFilter.lastDay = date
        app.openedPhotoFilter = photoFilter
    }
}

/// JournalOpenRow is a row that opens something when it can: with a chevron
/// at its end, as a row that opens a page has. A row with nowhere to go is
/// only its content.
struct JournalOpenRow<Content: View>: View {
    let isOpenable: Bool
    let open: () -> Void
    let content: Content

    init(isOpenable: Bool, open: @escaping () -> Void, @ViewBuilder content: () -> Content) {
        self.isOpenable = isOpenable
        self.open = open
        self.content = content()
    }

    var body: some View {
        if isOpenable {
            Button(action: open) {
                HStack(spacing: 8) {
                    content
                    Spacer(minLength: 0)
                    Image(systemName: "chevron.right")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.tertiary)
                        .accessibilityHidden(true)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        } else {
            content
        }
    }
}

/// JournalRecordRow is a row for one record of the day: a symbol, a title,
/// and a line under it, opening the record where the app has a screen for it.
struct JournalRecordRow: View {
    let symbolName: String
    let title: String
    var detail = ""
    var opening: JournalRecordOpening = .nowhere
    /// What a tap does, when the screen opens some records itself (a photo
    /// of the day opens in the viewer over it); else the record is opened in
    /// the part of the app that shows it.
    var openRecord: ((JournalRecordOpening) -> Void)?
    @Environment(AppModel.self) private var app

    var body: some View {
        JournalOpenRow(isOpenable: opening.isOpenable, open: open) {
            HStack(spacing: 12) {
                Image(systemName: symbolName)
                    .frame(width: 24)
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: title)
                    if !detail.isEmpty {
                        Text(verbatim: detail)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    private func open() {
        if let openRecord {
            openRecord(opening)
        } else {
            JournalRecordOpener.open(opening, in: app)
        }
    }
}

/// JournalNarrativeView is what the agent wrote, in the writing face, with
/// each record it mentions in bold, as the words the story wrote for it or its
/// title, which opens the record.
struct JournalNarrativeView: View {
    let narrative: JournalNarrative
    let open: (JournalRecord) -> Void

    private var paragraphs: [[JournalNarrativePiece]] {
        JournalNarrativeText.paragraphs(of: narrative.narrative, references: narrative.references)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ForEach(Array(paragraphs.enumerated()), id: \.offset) { _, pieces in
                Text(attributed(pieces))
                    .fontDesign(.serif)
                    .lineSpacing(4)
            }
        }
        .textSelection(.enabled)
        .environment(
            \.openURL,
            OpenURLAction { address in
                guard address.scheme == Self.scheme, let record = record(opening: address) else { return .systemAction }
                open(record)
                return .handled
            })
    }

    /// The address a mention carries, which only this view reads.
    private static let scheme = "teanode-journal-record"

    private func attributed(_ pieces: [JournalNarrativePiece]) -> AttributedString {
        var result = AttributedString()
        for piece in pieces {
            switch piece {
            case .words(let words):
                result.append(AttributedString(words))
            case .mention(let record, let words):
                var mention = AttributedString(words.isEmpty ? record.title : words)
                // A record the app has no screen for is only its title.
                if JournalRecordOpening.of(record: record).isOpenable {
                    mention.inlinePresentationIntent = .stronglyEmphasized
                    mention.link = address(of: record)
                }
                result.append(mention)
            }
        }
        return result
    }

    private func address(of record: JournalRecord) -> URL? {
        var components = URLComponents()
        components.scheme = Self.scheme
        components.host = "record"
        components.queryItems = [URLQueryItem(name: "reference", value: record.reference)]
        return components.url
    }

    private func record(opening address: URL) -> JournalRecord? {
        guard let reference = URLComponents(url: address, resolvingAgainstBaseURL: false)?.queryItems?.first(where: { $0.name == "reference" })?.value
        else { return nil }
        return narrative.references.first { $0.reference == reference }
    }
}

/// JournalVisitsMap is a small map of where the day went. It is only to look
/// at: the Location part has the whole map.
struct JournalVisitsMap: View {
    let visits: [JournalVisit]

    private var coordinates: [CLLocationCoordinate2D] {
        visits.filter { !($0.latitude == 0 && $0.longitude == 0) }.map { CLLocationCoordinate2D(latitude: $0.latitude, longitude: $0.longitude) }
    }

    private var position: MapCameraPosition {
        guard let region = LocationMapRegion.fitting(coordinates) else { return .automatic }
        return .region(region)
    }

    var body: some View {
        Map(initialPosition: position, interactionModes: []) {
            ForEach(visits.filter { !($0.latitude == 0 && $0.longitude == 0) }) { visit in
                Marker(visit.placeName, coordinate: CLLocationCoordinate2D(latitude: visit.latitude, longitude: visit.longitude))
            }
        }
        .frame(height: 160)
        .accessibilityLabel("Map of the places of the day")
    }

    /// Whether any visit has a place to draw.
    static func hasPlaces(_ visits: [JournalVisit]) -> Bool {
        visits.contains { !($0.latitude == 0 && $0.longitude == 0) }
    }
}

/// JournalProportionPart is one part of a bar: a color, and how much of the
/// whole it is.
struct JournalProportionPart: Identifiable {
    let id: Int
    let color: Color
    let weight: Int
}

/// JournalProportionBar is parts side by side, each as long as its share of
/// the whole.
struct JournalProportionBar: View {
    let parts: [JournalProportionPart]

    private var totalWeight: Int { max(1, parts.reduce(0) { $0 + $1.weight }) }

    var body: some View {
        GeometryReader { proxy in
            HStack(spacing: 0) {
                ForEach(parts) { part in
                    Rectangle()
                        .fill(part.color)
                        .frame(width: proxy.size.width * Double(part.weight) / Double(totalWeight))
                }
            }
        }
        .frame(height: 10)
        .clipShape(RoundedRectangle(cornerRadius: 3))
    }
}

/// JournalSleepBar is a night's stages side by side, as long as the time in
/// each.
struct JournalSleepBar: View {
    let stages: [JournalSleepStage]

    private var parts: [JournalProportionPart] {
        stages.enumerated().map { JournalProportionPart(id: $0.offset, color: JournalText.sleepStageColor($0.element.sleepStage), weight: $0.element.minutes) }
    }

    var body: some View {
        JournalProportionBar(parts: parts)
            .accessibilityElement()
            .accessibilityLabel("Sleep stages")
            .accessibilityValue(Text(verbatim: stages.map { "\(JournalText.sleepStageName($0.sleepStage)) \(JournalText.durationText(minutes: $0.minutes))" }.joined(separator: ", ")))
    }
}

/// JournalMoodBar is how many days had each mood, side by side.
struct JournalMoodBar: View {
    /// Five numbers, awful first.
    let moodCounts: [Int]

    private var parts: [JournalProportionPart] {
        JournalMoodLevel.allCases.compactMap { level in
            let count = level.rawValue <= moodCounts.count ? moodCounts[level.rawValue - 1] : 0
            return count > 0 ? JournalProportionPart(id: level.rawValue, color: JournalText.moodColor(level), weight: count) : nil
        }
    }

    /// "2 Low, 5 Okay, 13 Good, 5 Great".
    static func summaryText(_ moodCounts: [Int]) -> String {
        JournalMoodLevel.allCases.compactMap { level in
            let count = level.rawValue <= moodCounts.count ? moodCounts[level.rawValue - 1] : 0
            return count > 0 ? "\(count) \(JournalText.moodName(level))" : nil
        }.joined(separator: ", ")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            JournalProportionBar(parts: parts)
            Text(verbatim: Self.summaryText(moodCounts))
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }
}

/// JournalMoodStrip is the days of a month as small bars in their mood's
/// color, in order.
struct JournalMoodStrip: View {
    /// Each day's mood: 0 for none, 1 to 5.
    let dayMoods: [Int]

    var body: some View {
        HStack(spacing: 1) {
            ForEach(Array(dayMoods.enumerated()), id: \.offset) { _, moodLevel in
                Rectangle()
                    .fill(JournalText.dayMoodColor(moodLevel) ?? Color(.tertiarySystemFill))
                    .frame(maxWidth: .infinity)
            }
        }
        .frame(height: 6)
        .clipShape(RoundedRectangle(cornerRadius: 2))
        .accessibilityHidden(true)
    }
}
