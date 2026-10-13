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
        RecordingControl()
        TurnActivityWidget()
        CallActivityWidget()
        RecordingActivityWidget()
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
        .description("Ask your agent, call it, or start a recording.")
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
                    // Opens TeaNode recording, as the Control Center button and
                    // the app icon's menu do.
                    Button(intent: StartRecordingIntent()) {
                        Image(systemName: "mic").frame(maxWidth: .infinity)
                    }
                    .accessibilityLabel("Start recording")
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
                    Text(entry.glance?.newestWaiting.map { PlainText.of($0.interactionText) } ?? String(localized: "Nothing waits for you"))
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
                        Text(PlainText.of(newest.interactionText)).font(.footnote).lineLimit(3)
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

// MARK: A call

/// CallActivityWidget is a call with the agent on the lock screen and in
/// the Dynamic Island: how long it has run, and mute and hang up.
struct CallActivityWidget: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: CallActivityAttributes.self) { context in
            // As a call banner reads: who, round, then the name with how long
            // it has run under it, both on the left, then the two buttons.
            HStack(spacing: 14) {
                Image("Logo").resizable().scaledToFill().frame(width: 52, height: 52).clipShape(.circle)
                VStack(alignment: .leading, spacing: 1) {
                    Text(context.attributes.agentTitle).font(.title3.weight(.semibold)).lineLimit(1)
                    CallDuration(startedAt: context.attributes.startedAt, alignment: .leading)
                        .font(.title3)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 8)
                CallButtons(isMuted: context.state.isMuted)
            }
            .padding(.horizontal, 18)
            .padding(.vertical, 16)
            .activityBackgroundTint(nil)
        } dynamicIsland: { context in
            DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Image("Logo").resizable().frame(width: 28, height: 28)
                }
                DynamicIslandExpandedRegion(.center) {
                    VStack(spacing: 2) {
                        Text(context.attributes.agentTitle).font(.headline).lineLimit(1)
                        CallDuration(startedAt: context.attributes.startedAt, alignment: .center).font(.subheadline).foregroundStyle(.secondary)
                    }
                }
                DynamicIslandExpandedRegion(.bottom) {
                    CallButtons(isMuted: context.state.isMuted, isOnBlack: true)
                }
            } compactLeading: {
                Image(systemName: context.state.isMuted ? "mic.slash.fill" : "waveform").foregroundStyle(.tint)
            } compactTrailing: {
                // Wide enough for a call past an hour, 1:02:30.
                CallDuration(startedAt: context.attributes.startedAt, alignment: .trailing).frame(minWidth: 44, maxWidth: 64)
            } minimal: {
                Image(systemName: "waveform").foregroundStyle(.tint)
            }
        }
    }
}

/// CallDuration counts up from when the call began. A running timer takes
/// all the width it is offered, so where its digits sit is said outright:
/// trailing pushed the lock screen's under the buttons, away from the name.
struct CallDuration: View {
    let startedAt: Date
    var alignment: TextAlignment = .leading

    var body: some View {
        Text(timerInterval: startedAt...Date.distantFuture, countsDown: false)
            .monospacedDigit()
            .multilineTextAlignment(alignment)
            .frame(maxWidth: .infinity, alignment: frameAlignment)
    }

    private var frameAlignment: Alignment {
        switch alignment {
        case .center: .center
        case .trailing: .trailing
        default: .leading
        }
    }
}

/// CallButtons are a call's mute and hang up, run in the app.
struct CallButtons: View {
    let isMuted: Bool
    /// Whether the buttons sit on the Dynamic Island, which is black in
    /// light and dark alike, rather than on the lock screen's banner, which
    /// follows the appearance.
    var isOnBlack = false

    /// The circle and the glyph: semantic on the banner, so they read in
    /// light and dark; light on the island, where a semantic dark would not
    /// show. Muted fills the mute button's circle harder, to read at a glance.
    private var circleFill: Color { isOnBlack ? .white.opacity(0.2) : Color.primary.opacity(0.14) }
    private var mutedCircleFill: Color { isOnBlack ? .white.opacity(0.4) : Color.primary.opacity(0.3) }
    private var glyphColor: Color { isOnBlack ? .white : .primary }

    var body: some View {
        // Two round buttons alike, as a call banner has them: mute, which
        // shows the slashed microphone while muted, and close, which hangs up.
        HStack(spacing: 12) {
            Button(intent: ToggleCallMuteIntent()) {
                Image(systemName: isMuted ? "mic.slash.fill" : "mic.fill")
                    .font(.title2)
                    .foregroundStyle(glyphColor)
                    .frame(width: 52, height: 52)
                    .background(isMuted ? mutedCircleFill : circleFill, in: .circle)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(isMuted ? Text("Unmute") : Text("Mute"))
            Button(intent: EndCallIntent()) {
                Image(systemName: "xmark")
                    .font(.title2.weight(.medium))
                    .foregroundStyle(glyphColor)
                    .frame(width: 52, height: 52)
                    .background(circleFill, in: .circle)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Hang up")
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

/// RecordingControl starts a recording from Control Center, the Lock Screen or the Action button.
struct RecordingControl: ControlWidget {
    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: "recording") {
            ControlWidgetButton(action: StartRecordingIntent()) {
                Label("Start a recording", systemImage: "mic.fill")
            }
        }
        .displayName("Start a recording")
        .description("Opens TeaNode and starts recording.")
    }
}

#Preview("Up next", as: .systemMedium) {
    NextWidget()
} timeline: {
    GlanceEntry.sample
}

/// RecordingActivityWidget is a recording on the lock screen and in the
/// Dynamic Island, laid out as a call is: the logo, what it is with how long
/// it has run under it, then round pause or resume and stop buttons.
struct RecordingActivityWidget: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: RecordingActivityAttributes.self) { context in
            HStack(spacing: 14) {
                Image("Logo").resizable().scaledToFill().frame(width: 52, height: 52).clipShape(.circle)
                VStack(alignment: .leading, spacing: 1) {
                    Text("Recording").font(.title3.weight(.semibold)).lineLimit(1)
                    RecordingDuration(state: context.state, alignment: .leading)
                        .font(.title3)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 8)
                RecordingButtons(localRecordingId: context.attributes.localRecordingId, state: context.state)
            }
            .padding(.horizontal, 18)
            .padding(.vertical, 16)
            .activityBackgroundTint(nil)
        } dynamicIsland: { context in
            DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Image("Logo").resizable().frame(width: 28, height: 28)
                }
                DynamicIslandExpandedRegion(.center) {
                    VStack(spacing: 2) {
                        Text("Recording").font(.headline).lineLimit(1)
                        RecordingDuration(state: context.state, alignment: .center).font(.subheadline).foregroundStyle(.secondary)
                    }
                }
                DynamicIslandExpandedRegion(.bottom) {
                    RecordingButtons(localRecordingId: context.attributes.localRecordingId, state: context.state, isOnBlack: true)
                }
            } compactLeading: {
                Image(systemName: context.state.isRecording ? "waveform" : "pause.fill").foregroundStyle(.tint)
            } compactTrailing: {
                RecordingDuration(state: context.state, alignment: .trailing, isShowingPause: false).frame(minWidth: 44, maxWidth: 64)
            } minimal: {
                Image(systemName: context.state.isRecording ? "waveform" : "pause.fill").foregroundStyle(.tint)
            }
        }
    }
}

extension RecordingActivityAttributes.ContentState {
    var isRecording: Bool { recordingCaptureState == "active" }
}

/// RecordingDuration counts up while recording and holds still, saying
/// why, while paused or interrupted.
struct RecordingDuration: View {
    let state: RecordingActivityAttributes.ContentState
    var alignment: TextAlignment = .leading
    /// Whether a stopped clock says Paused or Interrupted before its time;
    /// the compact island has room for the time alone.
    var isShowingPause = true

    var body: some View {
        Group {
            if state.isRecording {
                Text(timerInterval: state.updatedAt.addingTimeInterval(-Double(state.elapsedMS) / 1000)...Date.distantFuture, countsDown: false)
            } else if isShowingPause {
                Text(verbatim: "\(pauseText) \(elapsedText)")
            } else {
                Text(verbatim: elapsedText)
            }
        }
        .monospacedDigit()
        .lineLimit(1)
        .multilineTextAlignment(alignment)
        .frame(maxWidth: .infinity, alignment: alignment == .center ? .center : alignment == .trailing ? .trailing : .leading)
    }

    private var pauseText: String {
        state.recordingCaptureState == "interrupted" ? String(localized: "Interrupted") : String(localized: "Paused")
    }

    private var elapsedText: String {
        Duration.milliseconds(state.elapsedMS).formatted(.time(pattern: state.elapsedMS >= 3_600_000 ? .hourMinuteSecond : .minuteSecond))
    }
}

/// RecordingButtons are a recording's pause or resume and stop, run in the
/// app, drawn as a call's mute and hang up are.
struct RecordingButtons: View {
    let localRecordingId: String
    let state: RecordingActivityAttributes.ContentState
    var isOnBlack = false

    var body: some View {
        HStack(spacing: 12) {
            Button(intent: ToggleRecordingPauseIntent(localRecordingId: localRecordingId, shouldResume: !state.isRecording)) {
                ActivityRoundGlyph(systemName: state.isRecording ? "pause.fill" : "mic.fill", isOnBlack: isOnBlack)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(state.isRecording ? Text("Pause recording") : Text("Resume recording"))
            Button(intent: StopRecordingIntent(localRecordingId: localRecordingId)) {
                ActivityRoundGlyph(systemName: "stop.fill", isOnBlack: isOnBlack, isDestructive: true)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Stop recording")
        }
    }
}

/// ActivityRoundGlyph is the round button a Live Activity offers: semantic
/// on the lock screen's banner, light on the Dynamic Island, which is black
/// in light and dark alike. Stop is drawn in red so it reads at a glance.
struct ActivityRoundGlyph: View {
    let systemName: String
    var isOnBlack = false
    var isDestructive = false

    var body: some View {
        Image(systemName: systemName)
            .font(.title2)
            .foregroundStyle(isDestructive ? Color.red : isOnBlack ? .white : .primary)
            .frame(width: 52, height: 52)
            .background(isOnBlack ? Color.white.opacity(0.2) : Color.primary.opacity(0.14), in: .circle)
    }
}

#Preview("Recording", as: .content, using: RecordingActivityAttributes(localRecordingId: "preview")) {
    RecordingActivityWidget()
} contentStates: {
    RecordingActivityAttributes.ContentState(recordingCaptureState: "active", elapsedMS: 754_000, updatedAt: .now)
    RecordingActivityAttributes.ContentState(recordingCaptureState: "paused", elapsedMS: 754_000, updatedAt: .now)
    RecordingActivityAttributes.ContentState(recordingCaptureState: "interrupted", elapsedMS: 3_754_000, updatedAt: .now)
}
