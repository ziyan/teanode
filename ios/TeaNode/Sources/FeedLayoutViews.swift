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

    var body: some View {
        VStack(spacing: 0) {
            ForEach(Array(listRows.enumerated()), id: \.offset) { index, listRow in
                if index > 0 { Divider() }
                FeedListLine(listRow: listRow)
            }
        }
    }
}

/// FeedListLine is one row of a list card.
struct FeedListLine: View {
    let listRow: AgentFeedListRow
    @Environment(AppModel.self) private var app
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
            Image(systemName: feedSymbolName(listRow.rowSymbol, fallback: "doc.text"))
                .symbolRenderingMode(.hierarchical)
                .font(.subheadline)
                .foregroundStyle(Color.secondary)
                .frame(minWidth: 24)
                .padding(.top, 2)
                .accessibilityHidden(true)
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
                    .foregroundStyle(.secondary)
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
                            .foregroundStyle(.secondary)
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
        .frame(maxWidth: .infinity, alignment: .leading)
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

/// FeedHourColumn is one hour of the forecast: the hour, its symbol, the
/// temperature, and the chance of rain when there is one.
struct FeedHourColumn: View {
    let hour: AgentFeedHour
    let weather: AgentFeedWeather

    var body: some View {
        VStack(spacing: 4) {
            Text(hourText)
                .font(.caption)
                .foregroundStyle(.secondary)
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
                    .foregroundStyle(.secondary)
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

/// FeedAgendaLayout is a day plan: a vertical timeline of entries, each with
/// its start and end, a marker colored by its kind, its title and detail.
struct FeedAgendaLayout: View {
    let agendaEntries: [AgentFeedAgendaEntry]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(Array(agendaEntries.enumerated()), id: \.offset) { _, entry in
                FeedAgendaLine(entry: entry)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// FeedAgendaLine is one entry of a day plan.
struct FeedAgendaLine: View {
    let entry: AgentFeedAgendaEntry
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
            .frame(minWidth: 62, alignment: .trailing)
            Capsule()
                .fill(markerColor)
                .frame(width: 4)
                .accessibilityHidden(true)
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
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }

    /// A meeting is the accent; the rest are told apart by their words too,
    /// and a break is the quietest.
    private var markerColor: Color {
        switch entry.agendaEntryKind {
        case .meeting: Color.accentColor
        case .focus: Color.primary
        case .task: Color(uiColor: .tertiaryLabel)
        case .`break`: Color.secondary.opacity(0.5)
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
