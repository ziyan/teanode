import SwiftUI
import TeaNodeKit
import WebRTC

// The screens of a call in a chat: the call screen, shown over the app while
// the phone is on one; the banner of a chat with a call going; a call's line
// in the conversation; and the transcript of a call that was transcribed. The
// calls themselves are ChatCallManager's.

// MARK: Words

/// ChatCallText is what the screens say of a call, in the person's language.
/// ChatCallLineKind and ChatCallLine (the Kit) decide which.
enum ChatCallText {
    static func lineTitle(_ kind: ChatCallLineKind) -> String {
        switch kind {
        case .going: String(localized: "Call going")
        case .ended(let durationText): String(localized: "Call, \(durationText)")
        case .missed: String(localized: "Missed call")
        case .noAnswer: String(localized: "No answer")
        case .declined: String(localized: "Declined call")
        case .failed: String(localized: "Call failed")
        }
    }

    static func bannerTitle(onCount: Int) -> String {
        String(localized: "Call going, \(onCount) on it")
    }

    /// The symbol of a call's line.
    static func lineSymbol(_ kind: ChatCallLineKind) -> String {
        switch kind {
        case .going: "phone.fill"
        case .ended: "phone"
        case .missed, .noAnswer: "phone.arrow.down.left"
        case .declined: "phone.down"
        case .failed: "exclamationmark.triangle"
        }
    }
}

// MARK: The picture of a person

/// ChatCallVideoView draws a track of pictures. SwiftUI has nothing that
/// draws WebRTC's frames, so this is the one UIKit view of a call: Google's
/// Metal renderer.
struct ChatCallVideoView: UIViewRepresentable {
    let track: RTCVideoTrack?
    /// The person's own picture is drawn mirrored, as in a mirror.
    var isMirrored = false

    @MainActor
    final class Coordinator {
        var track: RTCVideoTrack?
    }

    func makeCoordinator() -> Coordinator {
        Coordinator()
    }

    func makeUIView(context: Context) -> RTCMTLVideoView {
        let view = RTCMTLVideoView(frame: .zero)
        view.videoContentMode = .scaleAspectFill
        view.clipsToBounds = true
        return view
    }

    func updateUIView(_ view: RTCMTLVideoView, context: Context) {
        view.transform = isMirrored ? CGAffineTransform(scaleX: -1, y: 1) : CGAffineTransform.identity
        guard context.coordinator.track !== track else { return }
        context.coordinator.track?.remove(view)
        context.coordinator.track = track
        track?.add(view)
    }

    static func dismantleUIView(_ view: RTCMTLVideoView, coordinator: Coordinator) {
        coordinator.track?.remove(view)
        coordinator.track = nil
    }
}

/// ChatCallTile is one person on the call: their picture when their camera is
/// on, otherwise their avatar, with their name over it.
struct ChatCallTile: View {
    let member: ChatCallMemberView
    let isOwn: Bool
    /// Whether the person's camera is on.
    let isVideoOn: Bool
    let track: RTCVideoTrack?

    private var name: String {
        if isOwn { return String(localized: "You") }
        return member.name
    }

    private var isRinging: Bool {
        !member.isOn && member.ringState == "ringing"
    }

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            RoundedRectangle(cornerRadius: 18, style: .continuous).fill(Color(.secondarySystemBackground))
            picture
            label
                .padding(10)
        }
        .clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(Color(.separator).opacity(0.5), lineWidth: 0.5)
        }
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var picture: some View {
        if isVideoOn, let track {
            ChatCallVideoView(track: track, isMirrored: isOwn)
        } else if isVideoOn {
            // The picture is on its way.
            Image(systemName: "video.fill")
                .font(.largeTitle)
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            AvatarView(name: member.name, avatarUrl: member.avatarUrl, colorIndex: member.avatarColorIndex, size: 96)
                .opacity(isRinging ? 0.5 : 1)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private var label: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(name)
                .font(.footnote.weight(.semibold))
                .lineLimit(1)
            if isRinging {
                Text("Ringing…")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        // Over a picture, so a thick blur and not see-through glass.
        .background(.regularMaterial, in: .rect(cornerRadius: 12, style: .continuous))
    }
}

// MARK: The call screen

/// ChatCallScreen is shown over the whole app while the phone is on a call:
/// the people on it, and the controls on glass.
struct ChatCallScreen: View {
    let manager: ChatCallManager
    let session: ChatCallSession
    @State private var isConfirmingTranscription = false

    private var status: ChatCallTranscriptionStatus {
        guard let call = session.call else { return .notAsked }
        return ChatCallTranscriptionStatus(view: call, ownUserId: session.ownUserId)
    }

    /// The people on the call and those still being rung, the person first.
    private var tileMembers: [ChatCallMemberView] {
        guard let call = session.call else { return [] }
        let shown = call.members.filter { $0.isOn || $0.ringState == "ringing" }
        return shown.filter { $0.userId == session.ownUserId } + shown.filter { $0.userId != session.ownUserId }
    }

    var body: some View {
        VStack(spacing: 12) {
            header
            transcription
            tiles
            controls
        }
        .padding(.horizontal, 16)
        .padding(.top, 8)
        .padding(.bottom, 8)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(.systemBackground).ignoresSafeArea())
        .alert("Something went wrong", isPresented: problemBinding, presenting: manager.problemText) { _ in
            Button("OK", role: .cancel) {}
        } message: { text in
            Text(text)
        }
    }

    private var problemBinding: Binding<Bool> {
        Binding(
            get: { manager.problemText != nil },
            set: { if !$0 { manager.problemText = nil } })
    }

    // MARK: Head

    private var header: some View {
        VStack(spacing: 2) {
            Text(session.title)
                .font(.headline)
                .lineLimit(1)
            statusLine
        }
        .frame(maxWidth: .infinity)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var statusLine: some View {
        if session.phase == .connected, let answeredDate = session.call?.answeredDate {
            Text(answeredDate, style: .timer)
                .font(.subheadline)
                .monospacedDigit()
                .foregroundStyle(.secondary)
        } else {
            Text(statusText)
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
    }

    private var statusText: String {
        switch session.phase {
        case .connecting: String(localized: "Connecting…")
        case .reconnecting: String(localized: "Reconnecting…")
        case .connected: session.isOutgoing ? String(localized: "Calling…") : ""
        }
    }

    // MARK: Transcription

    @ViewBuilder
    private var transcription: some View {
        switch status {
        case .notAsked:
            EmptyView()
        case .transcribing:
            Label("Transcribing", systemImage: "text.bubble")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
                .background(.regularMaterial, in: .capsule)
        case .asking(let askerName):
            TranscriptionPrompt(askerName: askerName) { isConsenting in
                Task { await manager.answerTranscription(isConsenting: isConsenting) }
            }
        }
    }

    // MARK: The people

    @ViewBuilder
    private var tiles: some View {
        let members = tileMembers
        if members.isEmpty {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            let columns = ChatCallGrid.columnCount(tileCount: members.count)
            let rows = stride(from: 0, to: members.count, by: columns).map { Array(members[$0..<min($0 + columns, members.count)]) }
            VStack(spacing: 8) {
                ForEach(rows.indices, id: \.self) { rowIndex in
                    HStack(spacing: 8) {
                        ForEach(rows[rowIndex]) { member in
                            tile(for: member)
                        }
                    }
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func tile(for member: ChatCallMemberView) -> some View {
        let isOwn = member.userId == session.ownUserId
        return ChatCallTile(
            member: member, isOwn: isOwn, isVideoOn: isOwn ? session.isCameraOn : member.isVideoOn,
            track: isOwn ? session.localVideoTrack : session.remoteVideoTracks[member.userId])
    }

    // MARK: Controls

    private var controls: some View {
        GlassEffectContainer(spacing: 12) {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 12) {
                    mediaControls
                    callControls
                }
                VStack(spacing: 12) {
                    HStack(spacing: 12) { mediaControls }
                    HStack(spacing: 12) { callControls }
                }
            }
        }
        .frame(maxWidth: .infinity)
    }

    /// Mute, the camera and which camera.
    @ViewBuilder
    private var mediaControls: some View {
        ChatCallControlButton(
            systemImage: session.isMuted ? "mic.slash.fill" : "mic.fill", label: session.isMuted ? String(localized: "Unmute") : String(localized: "Mute"),
            isActive: session.isMuted
        ) {
            manager.setMuted(!session.isMuted)
        }
        ChatCallControlButton(
            systemImage: session.isCameraOn ? "video.fill" : "video.slash.fill", label: String(localized: "Camera"),
            value: session.isCameraOn ? String(localized: "On") : String(localized: "Off"), isActive: session.isCameraOn
        ) {
            Task { await manager.setCamera(isOn: !session.isCameraOn) }
        }
        ChatCallControlButton(systemImage: "arrow.triangle.2.circlepath.camera", label: String(localized: "Flip camera")) {
            manager.flipCamera()
        }
        .disabled(!session.isCameraOn)
    }

    /// Where the sound goes, the transcript and hanging up.
    @ViewBuilder
    private var callControls: some View {
        // Where the sound goes: the phone, the speaker or a headset.
        ChatCallAudioRouteButton()
        ChatCallControlButton(systemImage: "text.bubble", label: String(localized: "Transcribe")) {
            isConfirmingTranscription = true
        }
        .disabled(!status.canAsk)
        // A confirmation hangs on the control that asks.
        .confirmationDialog("Transcribe this call?", isPresented: $isConfirmingTranscription, titleVisibility: .visible) {
            Button("Ask everyone") {
                Task { await manager.askTranscription() }
            }
        } message: {
            Text("Everybody on the call is asked. Only the voices of those who agree are transcribed, and the transcript is posted in the chat.")
        }
        ChatCallControlButton(systemImage: "phone.down.fill", label: String(localized: "Hang up"), isDestructive: true) {
            manager.endCall()
        }
    }
}

/// ChatCallControlButton is one round control of the call screen, floating on
/// glass over the people.
struct ChatCallControlButton: View {
    let systemImage: String
    let label: String
    /// What a screen reader says of its state ("On"), when it has one.
    var value = ""
    var isActive = false
    var isDestructive = false
    let action: () -> Void
    @ScaledMetric(relativeTo: .title2) private var diameter: CGFloat = 54

    private var glass: Glass {
        if isActive { return Glass.regular.tint(Color.primary.opacity(0.25)).interactive() }
        return Glass.regular.interactive()
    }

    private var symbolColor: Color {
        isDestructive ? Color.red : Color.primary
    }

    var body: some View {
        Button(action: action) {
            Image(systemName: systemImage)
                .font(.title2)
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(symbolColor)
                .frame(width: diameter, height: diameter)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .glassEffect(glass, in: .circle)
        .accessibilityLabel(label)
        .accessibilityValue(value)
    }
}

/// TranscriptionPrompt asks the person whether their voice may be transcribed
/// when somebody on the call asked for it: Allow, or Not me.
struct TranscriptionPrompt: View {
    let askerName: String
    let onAnswer: (Bool) -> Void

    private var askerText: String {
        askerName.isEmpty ? String(localized: "Somebody") : askerName
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("\(askerText) asked to transcribe this call")
                .font(.subheadline)
            Text("Only your own voice is transcribed, and only if you allow it.")
                .font(.footnote)
                .foregroundStyle(.secondary)
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 12) { buttons }
                VStack(spacing: 8) { buttons }
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 18, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(Color(.separator).opacity(0.5), lineWidth: 0.5)
        }
    }

    @ViewBuilder
    private var buttons: some View {
        Button {
            onAnswer(true)
        } label: {
            Text("Allow")
                .frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .buttonBorderShape(.capsule)
        .controlSize(.large)
        Button {
            onAnswer(false)
        } label: {
            Text("Not me")
                .frame(maxWidth: .infinity)
        }
        .buttonStyle(.bordered)
        .buttonBorderShape(.capsule)
        .controlSize(.large)
    }
}

/// ChatCallPresenter shows the call screen over the app while the phone is on
/// a call, and tells what went wrong when a call could not be started.
struct ChatCallPresenter: ViewModifier {
    func body(content: Content) -> some View {
        let manager = ChatCallManager.shared
        let isOnCall = manager.session != nil
        let problemText = manager.problemText
        return content
            .fullScreenCover(isPresented: Binding(get: { isOnCall }, set: { _ in })) {
                if let session = manager.session {
                    ChatCallScreen(manager: manager, session: session)
                }
            }
            .alert(
                "Something went wrong",
                // On a call, its screen tells it, being on top.
                isPresented: Binding(get: { problemText != nil && !isOnCall }, set: { if !$0 { manager.problemText = nil } }),
                presenting: problemText
            ) { _ in
                Button("OK", role: .cancel) {}
            } message: { text in
                Text(text)
            }
    }
}

// MARK: In a chat

/// ChatCallButton is the call button in the bar of a chat with people: it
/// starts an audio call, or joins the one going.
struct ChatCallButton: View {
    let model: ConversationModel

    var body: some View {
        Button {
            guard let conversationId = model.conversationId else { return }
            let title = model.title
            Task { await ChatCallManager.shared.startCall(conversationId: conversationId, title: title) }
        } label: {
            Label("Start a call", systemImage: "phone")
        }
        .disabled(!model.canWrite || model.conversationId == nil)
    }
}

/// ChatCallBanner is the line at the top of a chat that has a call going: how
/// many are on it, and Join. It is hidden while this phone is on that call.
struct ChatCallBanner: View {
    let model: ConversationModel

    var body: some View {
        let manager = ChatCallManager.shared
        if let conversationId = model.conversationId, let callId = manager.goingCallIds[conversationId],
            let call = manager.callViews[callId], call.isGoing, !manager.isOnCall(callId)
        {
            HStack(spacing: 12) {
                Label {
                    Text(ChatCallText.bannerTitle(onCount: call.onCount))
                } icon: {
                    Image(systemName: "phone.fill")
                }
                .font(.footnote)
                .foregroundStyle(.secondary)
                Spacer(minLength: 0)
                Button("Join") {
                    Task { await manager.join(callId: callId, conversationId: conversationId, title: model.title) }
                }
                .buttonStyle(.borderedProminent)
                .buttonBorderShape(.capsule)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            // A thick blur, as the other banners: the messages scroll under it.
            .background(.regularMaterial, in: .rect(cornerRadius: 14, style: .continuous))
            .accessibilityElement(children: .contain)
        }
    }
}

/// ChatCallLineCard is a call's line in the conversation, drawn from the call:
/// how it went, who was on it, Join while it goes, and the transcript once
/// there is one.
struct ChatCallLineCard: View {
    let line: TranscriptLine
    let model: ConversationModel
    @Environment(\.messagePresenter) private var presenter
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// The newest the app has heard of the call, or the call as the message
    /// carried it.
    private var words: ChatCallLine? {
        if let view = ChatCallManager.shared.callViews[line.chatCallId] {
            return ChatCallLine(view: view, ownUserId: model.ownUserId)
        }
        if let record = line.chatCall {
            return ChatCallLine(record: record, ownUserId: model.ownUserId)
        }
        return nil
    }

    private func name(of userId: String?) -> String? {
        guard let userId, !userId.isEmpty else { return nil }
        if userId == model.ownUserId { return String(localized: "You") }
        return model.chat?.members.first { $0.userId == userId }?.displayName
    }

    /// Who the line says: who called when nobody picked up, who was on the
    /// call otherwise.
    private func detail(for kind: ChatCallLineKind) -> String? {
        let starterId = ChatCallManager.shared.callViews[line.chatCallId]?.startedByUserId ?? line.chatCall?.startedByUserId
        switch kind {
        case .missed:
            guard let caller = name(of: starterId) else { return nil }
            return String(localized: "\(caller) called")
        case .noAnswer, .declined, .failed:
            return nil
        case .going, .ended:
            let userIds: [String]
            if let view = ChatCallManager.shared.callViews[line.chatCallId] {
                userIds = view.members.filter { $0.joinedAt != nil }.map(\.userId)
            } else {
                userIds = (line.chatCall?.members ?? []).filter { $0.joinedAt != nil }.map(\.userId)
            }
            let names = userIds.compactMap { name(of: $0) }
            return names.isEmpty ? nil : names.formatted(.list(type: .and, width: .short))
        }
    }

    var body: some View {
        if let words {
            let layout = dynamicTypeSize.isAccessibilitySize ? AnyLayout(VStackLayout(alignment: .leading, spacing: 10)) : AnyLayout(HStackLayout(spacing: 12))
            CardSurface {
                layout {
                    HStack(spacing: 12) {
                        Image(systemName: ChatCallText.lineSymbol(words.kind))
                            .font(.title3)
                            .symbolRenderingMode(.hierarchical)
                            .foregroundStyle(symbolColor(words.kind))
                            .frame(minWidth: 28)
                            .accessibilityHidden(true)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(ChatCallText.lineTitle(words.kind))
                                .font(.subheadline.weight(.semibold))
                            if let detailText = detail(for: words.kind) {
                                Text(detailText)
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                    if dynamicTypeSize.isAccessibilitySize == false {
                        Spacer(minLength: 0)
                    }
                    trailingAction(words)
                }
            }
            .accessibilityElement(children: .contain)
        }
    }

    /// A missed call is red, with its symbol and words as well.
    private func symbolColor(_ kind: ChatCallLineKind) -> Color {
        switch kind {
        case .missed, .noAnswer, .failed: Color.red
        default: Color.primary
        }
    }

    @ViewBuilder
    private func trailingAction(_ words: ChatCallLine) -> some View {
        if words.isGoing {
            if !ChatCallManager.shared.isOnCall(words.callId), let conversationId = model.conversationId {
                Button("Join") {
                    let title = model.title
                    Task { await ChatCallManager.shared.join(callId: words.callId, conversationId: conversationId, title: title) }
                }
                .buttonStyle(.bordered)
                .buttonBorderShape(.capsule)
            }
        } else if words.isTranscriptDone {
            Button {
                presenter?.present(.callTranscript(callId: words.callId, title: model.title))
            } label: {
                Text("Transcript")
            }
            .buttonStyle(.bordered)
            .buttonBorderShape(.capsule)
        }
    }
}

// MARK: The transcript

/// ChatCallTranscriptSheet lists what was said on a call that was transcribed,
/// each line under its speaker.
struct ChatCallTranscriptSheet: View {
    let connection: Connection
    let callId: String
    let title: String
    @Environment(\.dismiss) private var dismiss
    @State private var detail: ChatCallDetailView?
    @State private var problemText: String?

    var body: some View {
        NavigationStack {
            Group {
                if let detail {
                    if detail.transcript.isEmpty {
                        ContentUnavailableView("No transcript", systemImage: "text.bubble", description: Text("Nothing was said by the people who agreed to be transcribed."))
                    } else {
                        transcript(detail)
                    }
                } else if let problemText {
                    ContentUnavailableView("Cannot read the transcript", systemImage: "wifi.exclamationmark", description: Text(problemText))
                } else {
                    ProgressView()
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
        .task {
            do {
                detail = try await connection.readChatCall(callId: callId)
            } catch {
                problemText = AppModel.shared.describe(error)
            }
        }
    }

    private func transcript(_ detail: ChatCallDetailView) -> some View {
        List {
            ForEach(detail.transcript) { segment in
                VStack(alignment: .leading, spacing: 4) {
                    HStack(alignment: .firstTextBaseline) {
                        Text(segment.speakerName)
                            .font(.subheadline.weight(.semibold))
                        Text(segment.startText)
                            .font(.caption)
                            .monospacedDigit()
                            .foregroundStyle(.secondary)
                    }
                    Text(segment.text)
                        .font(.body)
                }
                .accessibilityElement(children: .combine)
            }
        }
        .listStyle(.plain)
    }
}
