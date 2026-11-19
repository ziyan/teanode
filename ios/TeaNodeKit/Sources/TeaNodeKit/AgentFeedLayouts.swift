import Foundation

/// The structured layouts of a feed card (docs/planning/agent-feed-execplan.md,
/// "Card layouts"): a list of rows, the weather, a day's agenda. The server
/// sends each as a JSON object in a string, and the card's body stays filled
/// in Markdown, so a card whose content cannot be read is drawn as text.

/// AgentFeedLayout is how a card is drawn.
public enum AgentFeedLayout: Equatable, Sendable {
    case text
    case list([AgentFeedListRow])
    case weather(AgentFeedWeather)
    case agenda([AgentFeedAgendaEntry])
}

/// AgentFeedFlexibleText reads a value the server may write as text or as a
/// number (a temperature, a percent), as the words to show.
struct AgentFeedFlexibleText {
    static func decode<Key: CodingKey>(_ container: KeyedDecodingContainer<Key>, forKey key: Key) -> String {
        if let text = try? container.decodeIfPresent(String.self, forKey: key) { return text.trimmingCharacters(in: .whitespaces) }
        if let number = try? container.decodeIfPresent(Double.self, forKey: key) {
            return number == number.rounded() ? String(Int(number)) : String(format: "%.1f", number)
        }
        return ""
    }
}

/// AgentFeedListRow is one row of a list card: news, a mention, a review, a
/// mail.
public struct AgentFeedListRow: Decodable, Hashable, Sendable {
    public let rowTitle: String
    public let rowDetail: String
    /// The source of a story, or the channel and time of a mention.
    public let rowMeta: String
    public let rowSymbol: String
    /// An http or https address the row opens in the app's browser.
    public let rowUrl: String
    /// A record of the person's own the row opens, as a message's reference.
    public let rowRecordReference: AgentReference?

    private enum CodingKeys: String, CodingKey {
        case rowTitle, rowDetail, rowMeta, rowSymbol, rowUrl, rowRecordReference
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        rowTitle = (try? container.decodeIfPresent(String.self, forKey: .rowTitle)) ?? ""
        rowDetail = (try? container.decodeIfPresent(String.self, forKey: .rowDetail)) ?? ""
        rowMeta = (try? container.decodeIfPresent(String.self, forKey: .rowMeta)) ?? ""
        rowSymbol = (try? container.decodeIfPresent(String.self, forKey: .rowSymbol)) ?? ""
        rowUrl = (try? container.decodeIfPresent(String.self, forKey: .rowUrl)) ?? ""
        rowRecordReference = (try? container.decodeIfPresent(AgentReference.self, forKey: .rowRecordReference)) ?? nil
    }

    public init(rowTitle: String, rowDetail: String = "", rowMeta: String = "", rowSymbol: String = "", rowUrl: String = "", rowRecordReference: AgentReference? = nil) {
        self.rowTitle = rowTitle
        self.rowDetail = rowDetail
        self.rowMeta = rowMeta
        self.rowSymbol = rowSymbol
        self.rowUrl = rowUrl
        self.rowRecordReference = rowRecordReference
    }

    /// The web address the row opens; only http and https are followed.
    public var url: URL? {
        guard let url = URL(string: rowUrl), let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https" else { return nil }
        return url
    }

    /// Whether pressing the row opens something.
    public var isOpenable: Bool { url != nil || rowRecordReference != nil }
}

/// AgentFeedHour is one hour of a weather card's forecast.
public struct AgentFeedHour: Decodable, Hashable, Sendable {
    /// The hour as the server wrote it, an ISO time.
    public let forecastHour: String
    public let forecastTemperature: String
    public let forecastSymbol: String
    /// 0 when the server gives none.
    public let precipitationChancePercent: Int

    private enum CodingKeys: String, CodingKey {
        case forecastHour, forecastTemperature, forecastSymbol, precipitationChancePercent
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        forecastHour = (try? container.decodeIfPresent(String.self, forKey: .forecastHour)) ?? ""
        forecastTemperature = AgentFeedFlexibleText.decode(container, forKey: .forecastTemperature)
        forecastSymbol = (try? container.decodeIfPresent(String.self, forKey: .forecastSymbol)) ?? ""
        precipitationChancePercent = Int(Double(AgentFeedFlexibleText.decode(container, forKey: .precipitationChancePercent)) ?? 0)
    }

    public init(forecastHour: String, forecastTemperature: String, forecastSymbol: String = "", precipitationChancePercent: Int = 0) {
        self.forecastHour = forecastHour
        self.forecastTemperature = forecastTemperature
        self.forecastSymbol = forecastSymbol
        self.precipitationChancePercent = precipitationChancePercent
    }

    public var date: Date? { ServerTime.date(forecastHour) }
}

/// AgentFeedWeatherCondition is the sky a weather card is drawn for.
public enum AgentFeedWeatherCondition: String, Sendable {
    case clearDay, clearNight, cloudy, rain, snow, storm
}

/// AgentFeedWeather is the content of a weather card.
public struct AgentFeedWeather: Decodable, Hashable, Sendable {
    public let placeName: String
    public let currentTemperature: String
    /// C or F, with or without the degree sign.
    public let temperatureUnit: String
    public let conditionText: String
    public let conditionSymbol: String
    public let highTemperature: String
    public let lowTemperature: String
    public let hourlyForecast: [AgentFeedHour]

    private enum CodingKeys: String, CodingKey {
        case placeName, currentTemperature, temperatureUnit, conditionText, conditionSymbol, highTemperature, lowTemperature, hourlyForecast
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        placeName = (try? container.decodeIfPresent(String.self, forKey: .placeName)) ?? ""
        currentTemperature = AgentFeedFlexibleText.decode(container, forKey: .currentTemperature)
        temperatureUnit = (try? container.decodeIfPresent(String.self, forKey: .temperatureUnit)) ?? ""
        conditionText = (try? container.decodeIfPresent(String.self, forKey: .conditionText)) ?? ""
        conditionSymbol = (try? container.decodeIfPresent(String.self, forKey: .conditionSymbol)) ?? ""
        highTemperature = AgentFeedFlexibleText.decode(container, forKey: .highTemperature)
        lowTemperature = AgentFeedFlexibleText.decode(container, forKey: .lowTemperature)
        hourlyForecast = (try? container.decodeIfPresent([AgentFeedHour?].self, forKey: .hourlyForecast))?.compactMap { $0 } ?? []
    }

    public init(
        placeName: String = "", currentTemperature: String, temperatureUnit: String = "", conditionText: String = "", conditionSymbol: String = "",
        highTemperature: String = "", lowTemperature: String = "", hourlyForecast: [AgentFeedHour] = []
    ) {
        self.placeName = placeName
        self.currentTemperature = currentTemperature
        self.temperatureUnit = temperatureUnit
        self.conditionText = conditionText
        self.conditionSymbol = conditionSymbol
        self.highTemperature = highTemperature
        self.lowTemperature = lowTemperature
        self.hourlyForecast = hourlyForecast
    }

    /// What the sky is like, which picks the card's colors; see `AgentFeedWeatherCondition`.
    public var condition: AgentFeedWeatherCondition {
        let symbol = conditionSymbol.lowercased()
        let words = conditionText.lowercased()
        func mentions(_ names: [String]) -> Bool { names.contains { symbol.contains($0) || words.contains($0) } }
        if mentions(["bolt", "storm", "thunder"]) { return .storm }
        if mentions(["snow", "flurr", "sleet", "hail"]) { return .snow }
        if mentions(["rain", "drizzle", "shower"]) { return .rain }
        if mentions(["moon", "night"]) { return .clearNight }
        if mentions(["sun", "clear"]) { return .clearDay }
        return .cloudy
    }

    /// A temperature with its degree sign and the unit's letter: "21°C".
    public func shown(_ temperature: String) -> String {
        guard !temperature.isEmpty else { return "" }
        let letter = temperatureUnit.replacingOccurrences(of: "°", with: "").trimmingCharacters(in: .whitespaces).uppercased()
        return temperature + "°" + (letter == "C" || letter == "F" ? letter : "")
    }
}

/// AgentFeedAgendaEntryKind is what an entry of a day plan is, which colors
/// its marker.
public enum AgentFeedAgendaEntryKind: String, Sendable {
    case meeting, focus, task, `break`
}

/// AgentFeedAgendaEntry is one entry of a day plan.
public struct AgentFeedAgendaEntry: Decodable, Hashable, Sendable {
    public let startsAt: String
    public let endsAt: String
    public let agendaTitle: String
    public let agendaEntryKindName: String
    public let agendaDetail: String
    public let agendaRecordReference: AgentReference?

    private enum CodingKeys: String, CodingKey {
        case startsAt, endsAt, agendaTitle, agendaDetail, agendaRecordReference
        case agendaEntryKindName = "agendaEntryKind"
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        startsAt = (try? container.decodeIfPresent(String.self, forKey: .startsAt)) ?? ""
        endsAt = (try? container.decodeIfPresent(String.self, forKey: .endsAt)) ?? ""
        agendaTitle = (try? container.decodeIfPresent(String.self, forKey: .agendaTitle)) ?? ""
        agendaEntryKindName = (try? container.decodeIfPresent(String.self, forKey: .agendaEntryKindName)) ?? ""
        agendaDetail = (try? container.decodeIfPresent(String.self, forKey: .agendaDetail)) ?? ""
        agendaRecordReference = (try? container.decodeIfPresent(AgentReference.self, forKey: .agendaRecordReference)) ?? nil
    }

    public init(
        startsAt: String, endsAt: String = "", agendaTitle: String, agendaEntryKindName: String = "task", agendaDetail: String = "",
        agendaRecordReference: AgentReference? = nil
    ) {
        self.startsAt = startsAt
        self.endsAt = endsAt
        self.agendaTitle = agendaTitle
        self.agendaEntryKindName = agendaEntryKindName
        self.agendaDetail = agendaDetail
        self.agendaRecordReference = agendaRecordReference
    }

    /// An entry of a kind this app does not know is drawn as a task.
    public var agendaEntryKind: AgentFeedAgendaEntryKind { AgentFeedAgendaEntryKind(rawValue: agendaEntryKindName) ?? .task }
    public var startDate: Date? { ServerTime.date(startsAt) }
    public var endDate: Date? { ServerTime.date(endsAt) }
}

private struct AgentFeedListContent: Decodable {
    let listRows: [AgentFeedListRow]
}

private struct AgentFeedAgendaContent: Decodable {
    let agendaEntries: [AgentFeedAgendaEntry]
}

extension AgentFeedItem {
    /// How the card is drawn. A layout this app does not know, content that
    /// does not read, or content with nothing in it is the text layout, so the
    /// card still shows its body.
    public var layout: AgentFeedLayout {
        let data = Data(layoutContent.utf8)
        let decoder = JSONDecoder()
        switch feedCardLayout {
        case "list":
            guard let content = try? decoder.decode(AgentFeedListContent.self, from: data), !content.listRows.isEmpty else { return .text }
            return .list(content.listRows)
        case "weather":
            guard let weather = try? decoder.decode(AgentFeedWeather.self, from: data), !weather.currentTemperature.isEmpty else { return .text }
            return .weather(weather)
        case "agenda":
            guard let content = try? decoder.decode(AgentFeedAgendaContent.self, from: data), !content.agendaEntries.isEmpty else { return .text }
            return .agenda(content.agendaEntries)
        default:
            return .text
        }
    }
}
