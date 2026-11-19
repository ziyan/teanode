import SwiftUI
import TeaNodeKit
import UIKit

// The structured layouts of a feed card: a list of rows, the weather, a day's
// agenda. A card whose content cannot be read is drawn as text by the card
// itself (AgentFeedItem.layout).

/// Opens one of the person's own records, as a message's reference names it:
/// a mail, a page of the agent's knowledge, a finance transaction. A reference
/// naming anything else opens nothing.
@MainActor
func openFeedRecord(_ reference: AgentReference, in app: AppModel) {
    if let itemId = reference.itemId, !itemId.isEmpty {
        app.openedMailItemId = itemId
    } else if let path = reference.path, !path.isEmpty {
        app.openedKnowledgePath = path
    } else if reference.financeTransactionId != nil {
        app.openedFinancePart = .transactions
        app.openedSection = .finance
    }
}

/// A symbol this phone has, or the one given instead.
private func feedSymbolName(_ name: String, fallback: String) -> String {
    name.isEmpty || UIImage(systemName: name) == nil ? fallback : name
}

// MARK: List

/// FeedListLayout is a list card's rows, each with its symbol, title, detail
/// and meta, told apart by a rule. A row with an address or a record opens it.
struct FeedListLayout: View {
    let listRows: [AgentFeedListRow]
    /// The thumbnails of the rows that have one, by row index.
    var thumbnails: [Int: AgentFeedMedia] = [:]

    var body: some View {
        VStack(spacing: 0) {
            ForEach(Array(listRows.enumerated()), id: \.offset) { index, listRow in
                if index > 0 { Divider() }
                FeedListLine(listRow: listRow, thumbnail: thumbnails[index])
            }
        }
    }
}

/// FeedListLine is one row of a list card.
struct FeedListLine: View {
    let listRow: AgentFeedListRow
    /// A small picture for the row's leading edge, in place of its symbol.
    var thumbnail: AgentFeedMedia?
    @Environment(AppModel.self) private var app
    @ScaledMetric(relativeTo: .body) private var thumbnailSize: CGFloat = 56
    @Environment(\.openURL) private var openURL

    var body: some View {
        if listRow.isOpenable {
            Button {
                open()
            } label: {
                content
            }
            .buttonStyle(.plain)
            .accessibilityAddTraits(listRow.url == nil ? AccessibilityTraits() : AccessibilityTraits.isLink)
        } else {
            content
        }
    }

    private var content: some View {
        HStack(alignment: .top, spacing: 10) {
            leadingPicture
            VStack(alignment: .leading, spacing: 2) {
                Text(listRow.rowTitle)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.primary)
                    .multilineTextAlignment(.leading)
                if !listRow.rowDetail.isEmpty {
                    Text(listRow.rowDetail)
                        .font(.footnote)
                        .foregroundStyle(Color.secondary)
                        .multilineTextAlignment(.leading)
                }
                if !listRow.rowMeta.isEmpty {
                    Text(listRow.rowMeta)
                        .font(.caption)
                        .foregroundStyle(Color.secondary)
                }
            }
            Spacer(minLength: 4)
            if listRow.isOpenable {
                Image(systemName: "chevron.forward")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color(uiColor: .tertiaryLabel))
                    .padding(.top, 3)
                    .accessibilityHidden(true)
            }
        }
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(.rect)
    }

    /// The row's thumbnail, square and rounded, or its symbol.
    @ViewBuilder
    private var leadingPicture: some View {
        if let thumbnail {
            FeedMediaImage(path: thumbnail.imageUrl)
                .frame(width: thumbnailSize, height: thumbnailSize)
                .clipShape(.rect(cornerRadius: 10, style: .continuous))
                .accessibilityHidden(true)
        } else {
            Image(systemName: feedSymbolName(listRow.rowSymbol, fallback: "doc.text"))
                .symbolRenderingMode(.hierarchical)
                .font(.subheadline)
                .foregroundStyle(Color.secondary)
                .frame(minWidth: 24)
                .padding(.top, 2)
                .accessibilityHidden(true)
        }
    }

    /// An address opens in the app's own browser through the environment's
    /// openURL; a record opens in its part of the app.
    private func open() {
        if let url = listRow.url {
            openURL(url)
        } else if let reference = listRow.rowRecordReference {
            openFeedRecord(reference, in: app)
        }
    }
}

// MARK: Weather

/// FeedWeatherLayout is a weather card: the temperature now, large, with the
/// condition's symbol, the words and the day's high and low, and a strip of
/// the hours ahead.
struct FeedWeatherLayout: View {
    let weather: AgentFeedWeather

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if !weather.placeName.isEmpty {
                Text(weather.placeName)
                    .font(.footnote)
                    .opacity(0.85)
            }
            HStack(spacing: 12) {
                Image(systemName: feedSymbolName(weather.conditionSymbol, fallback: "cloud.sun"))
                    .symbolRenderingMode(.multicolor)
                    .font(.largeTitle)
                    .accessibilityHidden(true)
                Text(weather.shown(weather.currentTemperature))
                    .font(.largeTitle.weight(.semibold))
                    .monospacedDigit()
                VStack(alignment: .leading, spacing: 2) {
                    if !weather.conditionText.isEmpty {
                        Text(weather.conditionText)
                            .font(.subheadline)
                    }
                    if !highLowText.isEmpty {
                        Text(highLowText)
                            .font(.footnote)
                            .opacity(0.85)
                    }
                }
            }
            .accessibilityElement(children: .combine)
            if !weather.hourlyForecast.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 16) {
                        ForEach(Array(weather.hourlyForecast.enumerated()), id: \.offset) { _, hour in
                            FeedHourColumn(hour: hour, weather: weather)
                        }
                    }
                    .padding(.vertical, 2)
                }
            }
        }
        // White on a sky: the gradient is dark enough at every condition for
        // white text to read, in light and dark mode alike.
        .foregroundStyle(Color.white)
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(skyGradient, in: .rect(cornerRadius: 14, style: .continuous))
    }

    private var skyGradient: LinearGradient {
        LinearGradient(colors: FeedWeatherSky.colors(for: weather.condition), startPoint: .topLeading, endPoint: .bottomTrailing)
    }

    private var highLowText: String {
        let high = weather.shown(weather.highTemperature)
        let low = weather.shown(weather.lowTemperature)
        if !high.isEmpty, !low.isEmpty { return String(localized: "High \(high), low \(low)") }
        if !high.isEmpty { return String(localized: "High \(high)") }
        if !low.isEmpty { return String(localized: "Low \(low)") }
        return ""
    }
}

/// FeedWeatherSky is the colors a weather card is drawn on, by condition:
/// each is dark enough for white text.
enum FeedWeatherSky {
    static func colors(for condition: AgentFeedWeatherCondition) -> [Color] {
        switch condition {
        case .clearDay: [Color(red: 0.08, green: 0.31, blue: 0.62), Color(red: 0.17, green: 0.47, blue: 0.78)]
        case .clearNight: [Color(red: 0.06, green: 0.11, blue: 0.24), Color(red: 0.16, green: 0.18, blue: 0.42)]
        case .cloudy: [Color(red: 0.29, green: 0.33, blue: 0.40), Color(red: 0.38, green: 0.44, blue: 0.50)]
        case .rain: [Color(red: 0.15, green: 0.23, blue: 0.32), Color(red: 0.29, green: 0.40, blue: 0.50)]
        case .snow: [Color(red: 0.24, green: 0.35, blue: 0.47), Color(red: 0.35, green: 0.44, blue: 0.56)]
        case .storm: [Color(red: 0.14, green: 0.12, blue: 0.23), Color(red: 0.29, green: 0.23, blue: 0.40)]
        }
    }
}

/// FeedHourColumn is one hour of the forecast: the hour, its symbol, the
/// temperature, and the chance of rain when there is one.
struct FeedHourColumn: View {
    let hour: AgentFeedHour
    let weather: AgentFeedWeather

    var body: some View {
        VStack(spacing: 4) {
            Text(hourText)
                .font(.caption)
                .opacity(0.85)
            Image(systemName: feedSymbolName(hour.forecastSymbol, fallback: "cloud"))
                .symbolRenderingMode(.multicolor)
                .font(.title3)
                .accessibilityHidden(true)
            Text(weather.shown(hour.forecastTemperature))
                .font(.subheadline.weight(.semibold))
                .monospacedDigit()
            if hour.precipitationChancePercent > 0 {
                Text(verbatim: "\(hour.precipitationChancePercent)%")
                    .font(.caption2)
                    .opacity(0.85)
                    .accessibilityLabel(Text("\(hour.precipitationChancePercent) percent chance of rain"))
            }
        }
        .frame(minWidth: 44)
        .accessibilityElement(children: .combine)
    }

    private var hourText: String {
        guard let date = hour.date else { return hour.forecastHour }
        return date.formatted(.dateTime.hour())
    }
}

// MARK: Agenda

/// FeedAgendaLayout is a day plan: entries hang off a time rail, each with
/// its start and end, a dot colored by its kind, its title and detail.
struct FeedAgendaLayout: View {
    let agendaEntries: [AgentFeedAgendaEntry]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(agendaEntries.enumerated()), id: \.offset) { index, entry in
                FeedAgendaLine(entry: entry, isFirst: index == 0, isLast: index == agendaEntries.count - 1)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// FeedAgendaLine is one entry of a day plan.
struct FeedAgendaLine: View {
    let entry: AgentFeedAgendaEntry
    /// The rail starts at the first entry's dot and ends at the last one's.
    let isFirst: Bool
    let isLast: Bool
    @Environment(AppModel.self) private var app

    var body: some View {
        if let reference = entry.agendaRecordReference {
            Button {
                openFeedRecord(reference, in: app)
            } label: {
                content
            }
            .buttonStyle(.plain)
        } else {
            content
        }
    }

    private var content: some View {
        HStack(alignment: .top, spacing: 10) {
            VStack(alignment: .trailing, spacing: 0) {
                if let startDate = entry.startDate {
                    Text(startDate.formatted(date: .omitted, time: .shortened))
                        .font(.footnote.weight(.semibold))
                        .monospacedDigit()
                }
                if let endDate = entry.endDate {
                    Text(endDate.formatted(date: .omitted, time: .shortened))
                        .font(.caption)
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                }
            }
            .padding(.vertical, 6)
            .frame(minWidth: 62, alignment: .trailing)
            rail
            VStack(alignment: .leading, spacing: 2) {
                Text(entry.agendaTitle)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.primary)
                    .multilineTextAlignment(.leading)
                Text(detailText)
                    .font(.footnote)
                    .foregroundStyle(Color.secondary)
                    .multilineTextAlignment(.leading)
            }
            .padding(.vertical, 6)
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }

    /// The time rail: a thin line through the entries, and this entry's dot
    /// on it, level with its first line.
    private var rail: some View {
        VStack(spacing: 0) {
            Rectangle()
                .fill(isFirst ? Color.clear : Color(uiColor: .separator))
                .frame(width: 2, height: 10)
            Circle()
                .fill(markerColor)
                .frame(width: 10, height: 10)
            Rectangle()
                .fill(isLast ? Color.clear : Color(uiColor: .separator))
                .frame(width: 2)
                .frame(maxHeight: .infinity)
        }
        .frame(width: 10)
        .accessibilityHidden(true)
    }

    /// A dot a color per kind; the kind is also in the words under the title,
    /// so the color is never the only sign.
    private var markerColor: Color {
        switch entry.agendaEntryKind {
        case .meeting: Color.blue
        case .focus: Color.indigo
        case .task: Color.orange
        case .`break`: Color.gray
        }
    }

    private var kindText: String {
        switch entry.agendaEntryKind {
        case .meeting: String(localized: "Meeting")
        case .focus: String(localized: "Focus")
        case .task: String(localized: "Task")
        case .`break`: String(localized: "Break")
        }
    }

    /// The kind in words, then the detail.
    private var detailText: String {
        entry.agendaDetail.isEmpty ? kindText : "\(kindText) · \(entry.agendaDetail)"
    }
}
