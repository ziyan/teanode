import ActivityKit
import AppIntents
import SwiftUI
import TeaNodeKit
import WidgetKit

/// TeaNodeWidgets are TeaNode on the Home Screen, the Lock Screen and in
/// Control Center: asking or calling the agent, and what is next.
@main
struct TeaNodeWidgets: WidgetBundle {
    var body: some Widget {
        AgentWidget()
        WaitingWidget()
        NextWidget()
        CallControl()
        TurnActivityWidget()
    }
}

// MARK: Reading

/// GlanceEntry is one moment of a widget: what it shows, or why it shows
/// nothing.
struct GlanceEntry: TimelineEntry {
    let date: Date
    let glance: Glance?
    let isConnected: Bool

    static let sample = GlanceEntry(
        date: Date(),
        glance: Glance(
            events: [
                .init(id: "1", title: String(localized: "Team lunch"), location: "", startsAt: Date().addingTimeInterval(3600), endsAt: Date().addingTimeInterval(7200), isAllDay: false),
                .init(id: "2", title: String(localized: "Dentist"), location: "", startsAt: Date().addingTimeInterval(26 * 3600), endsAt: Date().addingTimeInterval(27 * 3600), isAllDay: false),
            ], unreadCount: 3, agentName: "", waitingCount: 2,
            newestWaiting: AgentWaitingInteraction(
                id: "w1", conversationId: "", interactionKind: "question", interactionText: String(localized: "Which day suits you for the dentist?"),
                createdAt: "")),
        isConnected: true)
}

/// GlanceProvider reads the glance with the sign-in the app keeps, every
/// fifteen minutes, and again at the end of the next event.
struct GlanceProvider: TimelineProvider {
    func placeholder(in context: Context) -> GlanceEntry { .sample }

    func getSnapshot(in context: Context, completion: @escaping @Sendable (GlanceEntry) -> Void) {
        if context.isPreview {
            completion(.sample)
            return
        }
        Task { completion(await Self.read()) }
    }

    func getTimeline(in context: Context, completion: @escaping @Sendable (Timeline<GlanceEntry>) -> Void) {
        Task {
            let entry = await Self.read()
            var next = Date().addingTimeInterval(15 * 60)
            if let ending = entry.glance?.events.first?.endsAt, ending < next { next = ending }
            completion(Timeline(entries: [entry], policy: .after(next)))
        }
    }

    static func read() async -> GlanceEntry {
        guard let account = try? KeychainAccountStore().load() else {
            return GlanceEntry(date: Date(), glance: nil, isConnected: false)
        }
        let connection = Connection(account: account, transport: URLSessionTransport(), store: KeychainAccountStore())
        let glance = try? await connection.glance()
        return GlanceEntry(date: Date(), glance: glance, isConnected: true)
    }
}

// MARK: The agent

/// AgentWidget opens the conversation with the agent, or a call with it.
struct AgentWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "agent", provider: GlanceProvider()) { entry in
            AgentWidgetView(entry: entry)
                .containerBackground(.fill.tertiary, for: .widget)
        }
        .configurationDisplayName("Your agent")
        .description("Ask your agent or call it.")
        .supportedFamilies([.systemSmall, .accessoryCircular, .accessoryRectangular])
    }
}

struct AgentWidgetView: View {
    let entry: GlanceEntry
    @Environment(\.widgetFamily) private var family

    private var agentTitle: String {
        let name = entry.glance?.agentName ?? ""
        return name.isEmpty ? String(localized: "Your agent") : name
    }

    var body: some View {
        switch family {
        case .accessoryCircular:
            ZStack {
                AccessoryWidgetBackground()
                Image("Logo").resizable().scaledToFit().padding(10).widgetAccentable()
            }
        case .accessoryRectangular:
            VStack(alignment: .leading, spacing: 2) {
                Label(agentTitle, image: "Logo").font(.headline).widgetAccentable()
                Text("Ask anything").font(.caption).foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        default:
            VStack(alignment: .leading, spacing: 0) {
                Image("Logo").resizable().frame(width: 32, height: 32)
                Spacer()
                Text(agentTitle).font(.headline).lineLimit(1)
                Text("Ask anything").font(.footnote).foregroundStyle(.secondary)
                Spacer(minLength: 10)
                HStack(spacing: 8) {
                    Button(intent: OpenSectionIntent(part: .conversation)) {
                        Image(systemName: "text.bubble").frame(maxWidth: .infinity)
                    }
                    .accessibilityLabel("Ask your agent")
                    Button(intent: CallAgentIntent()) {
                        Image(systemName: "waveform").frame(maxWidth: .infinity)
                    }
                    .accessibilityLabel("Call your agent")
                }
                .buttonStyle(.bordered)
                .buttonBorderShape(.capsule)
                .tint(.accentColor)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

// MARK: What waits

/// WaitingWidget is how many questions and approvals wait for the person,
/// and the newest; a tap opens its conversation.
struct WaitingWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "waiting", provider: GlanceProvider()) { entry in
            WaitingWidgetView(entry: entry)
                .containerBackground(.fill.tertiary, for: .widget)
        }
        .configurationDisplayName("Waiting for you")
        .description("The questions and approvals your agent waits on.")
        .supportedFamilies([.systemSmall, .accessoryCircular, .accessoryRectangular])
    }
}

struct WaitingWidgetView: View {
    let entry: GlanceEntry
    @Environment(\.widgetFamily) private var family

    private var count: Int { entry.glance?.waitingCount ?? 0 }

    private var opening: OpenConversationIntent {
        OpenConversationIntent(conversationId: entry.glance?.newestWaiting?.conversationId ?? "")
    }

    var body: some View {
        switch family {
        case .accessoryCircular:
            ZStack {
                AccessoryWidgetBackground()
                VStack(spacing: 0) {
                    Text(count, format: .number).font(.title2.weight(.semibold)).widgetAccentable()
                    Image(systemName: "questionmark.bubble").font(.caption2)
                }
            }
            .accessibilityLabel(Text("\(count) waiting for you"))
        case .accessoryRectangular:
            Button(intent: opening) {
                VStack(alignment: .leading, spacing: 1) {
                    Label("\(count) waiting", systemImage: "questionmark.bubble").font(.headline).widgetAccentable()
                    Text(entry.glance?.newestWaiting?.interactionText ?? String(localized: "Nothing waits for you"))
                        .font(.caption)
                        .lineLimit(2)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .buttonStyle(.plain)
        default:
            Button(intent: opening) {
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        Image(systemName: "questionmark.bubble").foregroundStyle(count > 0 ? AnyShapeStyle(.orange) : AnyShapeStyle(.secondary))
                        Spacer()
                        Text(count, format: .number).font(.title.weight(.semibold))
                    }
                    Spacer(minLength: 4)
                    if let newest = entry.glance?.newestWaiting {
                        Text(newest.interactionKind == "approval" ? String(localized: "Approve?") : String(localized: "Asks you"))
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(.orange)
                        Text(newest.interactionText).font(.footnote).lineLimit(3)
                    } else {
                        Text("Nothing waits for you").font(.footnote).foregroundStyle(.secondary)
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            }
            .buttonStyle(.plain)
        }
    }
}

// MARK: What is next

/// NextWidget is what is next: the coming events and the unread mail.
struct NextWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "next", provider: GlanceProvider()) { entry in
            NextWidgetView(entry: entry)
                .containerBackground(.fill.tertiary, for: .widget)
        }
        .configurationDisplayName("Up next")
        .description("Your next events and unread mail.")
        .supportedFamilies([.systemSmall, .systemMedium, .systemLarge, .accessoryRectangular])
    }
}

struct NextWidgetView: View {
    let entry: GlanceEntry
    @Environment(\.widgetFamily) private var family

    private var shownCount: Int {
        switch family {
        case .systemLarge: 6
        case .systemMedium: 3
        default: 1
        }
    }

    var body: some View {
        if !entry.isConnected {
            Text("Open TeaNode and connect it to your server first.")
                .font(.footnote)
                .foregroundStyle(.secondary)
        } else if family == .accessoryRectangular {
            VStack(alignment: .leading, spacing: 1) {
                if let event = entry.glance?.events.first {
                    Text(event.title.isEmpty ? String(localized: "(no title)") : event.title).font(.headline).lineLimit(1).widgetAccentable()
                    EventTime(event: event).font(.caption)
                } else {
                    Text("Nothing coming up").font(.headline)
                }
                if let unread = entry.glance?.unreadCount, unread > 0 {
                    Label("\(unread) unread", systemImage: "envelope").font(.caption).foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        } else {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Text("Up next").font(.caption.weight(.semibold)).foregroundStyle(.tint)
                    Spacer()
                    if let unread = entry.glance?.unreadCount, unread > 0 {
                        Button(intent: OpenSectionIntent(part: .mail)) {
                            Label("\(unread)", systemImage: "envelope.fill").font(.caption.weight(.semibold))
                        }
                        .buttonStyle(.plain)
                        .foregroundStyle(.secondary)
                        .accessibilityLabel("\(unread) unread")
                    }
                }
                let events = Array((entry.glance?.events ?? []).prefix(shownCount))
                if events.isEmpty {
                    Spacer()
                    Text("Nothing coming up").font(.subheadline).foregroundStyle(.secondary)
                    Spacer()
                } else {
                    Button(intent: OpenSectionIntent(part: .calendar)) {
                        VStack(alignment: .leading, spacing: family == .systemSmall ? 4 : 10) {
                            ForEach(events) { event in
                                EventRow(event: event, isCompact: family == .systemSmall)
                            }
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .buttonStyle(.plain)
                    Spacer(minLength: 0)
                }
            }
        }
    }
}

/// EventRow is one coming event: a bar of the accent color, its title and
/// its time.
struct EventRow: View {
    let event: Glance.Event
    let isCompact: Bool

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Capsule().fill(.tint).frame(width: 3)
            VStack(alignment: .leading, spacing: 1) {
                Text(event.title.isEmpty ? String(localized: "(no title)") : event.title)
                    .font(isCompact ? .headline : .subheadline.weight(.semibold))
                    .lineLimit(isCompact ? 3 : 1)
                EventTime(event: event).font(.caption).foregroundStyle(.secondary)
            }
        }
        .fixedSize(horizontal: false, vertical: true)
    }
}

/// EventTime says when an event is: its day when not today, and its time
/// or that it is all day.
struct EventTime: View {
    let event: Glance.Event

    var body: some View {
        let calendar = Calendar.current
        let isToday = calendar.isDateInToday(event.startsAt) || (event.startsAt < Date() && event.endsAt > Date())
        if event.isAllDay {
            Text(isToday ? String(localized: "Today, all day") : event.startsAt.formatted(.dateTime.weekday(.abbreviated).month().day()))
        } else if isToday {
            Text(event.startsAt, format: .dateTime.hour().minute())
        } else {
            Text(event.startsAt.formatted(.dateTime.weekday(.abbreviated).hour().minute()))
        }
    }
}

// MARK: A turn left running

/// TurnActivityWidget is a turn left running, on the lock screen and in the
/// Dynamic Island: the agent's name and one word for where the turn is.
struct TurnActivityWidget: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: AgentTurnAttributes.self) { context in
            HStack(spacing: 12) {
                Image("Logo").resizable().frame(width: 36, height: 36)
                VStack(alignment: .leading, spacing: 2) {
                    Text(context.attributes.agentTitle).font(.headline)
                    Label(TurnActivityWords.text(context.state.activityState), systemImage: TurnActivityWords.symbol(context.state.activityState))
                        .font(.subheadline)
                        .foregroundStyle(context.state.activityState == "waiting" ? AnyShapeStyle(.orange) : AnyShapeStyle(.secondary))
                }
                Spacer()
            }
            .padding()
            .activityBackgroundTint(nil)
        } dynamicIsland: { context in
            DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Image("Logo").resizable().frame(width: 28, height: 28)
                }
                DynamicIslandExpandedRegion(.center) {
                    VStack(spacing: 2) {
                        Text(context.attributes.agentTitle).font(.headline)
                        Text(TurnActivityWords.text(context.state.activityState)).font(.subheadline).foregroundStyle(.secondary)
                    }
                }
                DynamicIslandExpandedRegion(.trailing) {
                    Image(systemName: TurnActivityWords.symbol(context.state.activityState)).font(.title3)
                }
            } compactLeading: {
                Image("Logo").resizable().frame(width: 20, height: 20)
            } compactTrailing: {
                Image(systemName: TurnActivityWords.symbol(context.state.activityState))
                    .foregroundStyle(context.state.activityState == "waiting" ? AnyShapeStyle(.orange) : AnyShapeStyle(.tint))
            } minimal: {
                Image(systemName: TurnActivityWords.symbol(context.state.activityState))
            }
        }
    }
}

// MARK: Control Center

/// CallControl calls the agent from Control Center, the Lock Screen or the
/// Action button.
struct CallControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "call") {
            ControlWidgetButton(action: CallAgentIntent()) {
                Label("Call your agent", systemImage: "waveform")
            }
        }
        .displayName("Call your agent")
        .description("Starts a voice call with your agent.")
    }
}

#Preview("Up next", as: .systemMedium) {
    NextWidget()
} timeline: {
    GlanceEntry.sample
}
