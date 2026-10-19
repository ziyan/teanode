import SwiftUI
import TeaNodeKit

// Health in Settings: the switch that copies Apple Health data to the server,
// what has been sent, whether the agent may read it, who it is shared with,
// and erasing it.

/// HealthSettingsRow is the row in Settings that opens the Health settings,
/// saying whether Health is on.
struct HealthSettingsRow: View {
    private let sync = HealthSync.shared

    var body: some View {
        HStack {
            Label {
                Text(verbatim: HealthWords.title)
            } icon: {
                Image(systemName: AppSection.health.systemImage)
            }
            .foregroundStyle(.primary)
            Spacer()
            if sync.isOn {
                Text("On").foregroundStyle(.secondary)
            } else {
                Text("Off").foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// HealthSettingsScreen is what the person decides about Health: copying it
/// to the server, the agent reading it, sharing it, and erasing it.
struct HealthSettingsScreen: View {
    @State private var model: HealthSettingsModel
    @State private var isSettingUp = false
    @State private var isSharing = false
    @State private var isConfirmingErase = false
    private let sync = HealthSync.shared

    init(connection: Connection) {
        _model = State(initialValue: HealthSettingsModel(connection: connection))
    }

    var body: some View {
        let snapshot = HealthSyncSnapshot.current()
        Form {
            if let loadFailedText = model.loadFailedText, model.settings == nil {
                loadFailedSection(loadFailedText)
            }
            copyingSection(snapshot)
            if snapshot.isOn {
                statusSection(snapshot)
            }
            serverSection
            agentSection
            aboutSection
            HealthSharingSections(model: model, isSharing: $isSharing)
            eraseSection
        }
        .navigationTitle(HealthWords.title)
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await model.load() }
        .task { await model.load() }
        .sheet(isPresented: $isSettingUp) { HealthSetupSheet() }
        .sheet(isPresented: $isSharing) { HealthShareSheet(model: model) }
        .toast(done: $model.doneText, problem: $model.problemText)
        #if DEBUG
            .onAppear {
                // `-TeaNodeScreen health-setup`: the setup over the settings.
                if DemoServer.isRequested, DemoServer.screen == "health-setup" { isSettingUp = true }
            }
        #endif
    }

    // MARK: Reading

    /// What the server holds could not be read: why, and the way to try again.
    private func loadFailedSection(_ loadFailedText: String) -> some View {
        Section {
            Label {
                Text(verbatim: loadFailedText.asReasonSentence)
            } icon: {
                Image(systemName: "wifi.exclamationmark")
            }
            .foregroundStyle(.primary)
            Button("Try again") { Task { await model.load() } }
                .actionColor()
        }
    }

    // MARK: Copying

    private func copyingSection(_ snapshot: HealthSyncSnapshot) -> some View {
        Section {
            Toggle(
                isOn: Binding(
                    get: { snapshot.isOn },
                    set: { isOn in
                        if isOn { isSettingUp = true } else { sync.turnOff() }
                    })
            ) {
                Text(verbatim: HealthWords.title)
            }
            .disabled(!snapshot.isAvailable)
        } footer: {
            if snapshot.isAvailable {
                VStack(alignment: .leading, spacing: 8) {
                    // Why the switch is off, even though the person did not
                    // turn it off: the server was erased from somewhere else.
                    if !snapshot.isOn, snapshot.isStoppedByErase {
                        Text(verbatim: HealthWords.erasedOnServerText)
                            .foregroundStyle(.primary)
                    }
                    Text("Copies the Health data you allow to your own TeaNode server. Turned off, it sends nothing new. What is already on your server stays until you erase it.")
                }
            } else {
                Text("Health is not available on this device.")
            }
        }
    }

    private func statusSection(_ snapshot: HealthSyncSnapshot) -> some View {
        Section {
            LabeledContent("Last sent") {
                if let lastSentAt = snapshot.lastSentAt {
                    Text(verbatim: lastSentAt.formatted(.relative(presentation: .named)))
                } else {
                    Text("Not yet")
                }
            }
            LabeledContent("Readings sent") {
                Text(snapshot.sentSampleCount, format: .number).monospacedDigit()
            }
            if let passProgress = snapshot.passProgress {
                readingProgressRow(passProgress)
            } else if !snapshot.isHistoryDone {
                LabeledContent("Older readings") {
                    Text("Still being read")
                }
            }
            if snapshot.pendingDayCount > 0 {
                LabeledContent("Days to be recalculated") {
                    Text(snapshot.pendingDayCount, format: .number).monospacedDigit()
                }
            }
            if snapshot.droppedEntryCount > 0 {
                LabeledContent("Left out") {
                    Text(snapshot.droppedEntryCount, format: .number).monospacedDigit()
                }
            }
            if let problemText = snapshot.problemText {
                Label {
                    Text(verbatim: problemText)
                } icon: {
                    Image(systemName: "exclamationmark.triangle")
                }
                .foregroundStyle(.primary)
            }
            Button(syncButtonTitle(snapshot), systemImage: "arrow.triangle.2.circlepath") {
                Task { await sync.syncNow() }
            }
            .actionColor()
            .disabled(snapshot.isSyncing)
        } header: {
            Text("Status")
        } footer: {
            if snapshot.droppedEntryCount > 0 || (snapshot.isHistoryDone && snapshot.sentSampleCount == 0) {
                VStack(alignment: .leading, spacing: 8) {
                    if snapshot.droppedEntryCount > 0 {
                        Text("Readings your server would not take are left out.")
                    }
                    if snapshot.isHistoryDone, snapshot.sentSampleCount == 0 {
                        Text("If readings you expect are missing, open Settings, then Health, then Data Access & Devices, then TeaNode, and allow them.")
                    }
                }
            }
        }
    }

    /// How far the first pass over every kind of data has got.
    private func readingProgressRow(_ passProgress: HealthPassProgress) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Reading your Health data")
            ProgressView(value: Double(passProgress.typesDone), total: Double(max(1, passProgress.typesTotal)))
            Text("\(passProgress.typesDone) of \(passProgress.typesTotal) kinds")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .monospacedDigit()
        }
        .accessibilityElement(children: .combine)
    }

    // MARK: On the server

    /// What the server keeps and when it last heard from the phone, as the
    /// dashboard says it.
    @ViewBuilder
    private var serverSection: some View {
        if let serverSettings = model.settings {
            Section {
                Text(verbatim: readingsKeptText(serverSettings.sampleCount))
                if let lastReceivedDate = serverSettings.lastReceivedDate {
                    LabeledContent("Last received") {
                        Text(verbatim: lastReceivedDate.formatted(.relative(presentation: .named)))
                    }
                }
            }
        }
    }

    private func readingsKeptText(_ sampleCount: Int) -> String {
        let countText = sampleCount.formatted()
        if sampleCount == 0 { return String(localized: "health-readings-kept-none", defaultValue: "No readings kept") }
        if sampleCount == 1 { return String(localized: "health-readings-kept-single", defaultValue: "1 reading kept") }
        return String(localized: "health-readings-kept-plural", defaultValue: "\(countText) readings kept")
    }

    private func syncButtonTitle(_ snapshot: HealthSyncSnapshot) -> String {
        snapshot.isSyncing ? String(localized: "Sending…") : String(localized: "Send now")
    }

    // MARK: The agent

    private var agentSection: some View {
        Section {
            Toggle(
                "Let the agent read Health data",
                isOn: Binding(
                    get: { model.settings?.canAgentReadHealth ?? false },
                    set: { canRead in Task { await model.setAgentCanRead(canRead) } })
            )
            .disabled(model.settings == nil || model.isBusy)
        } header: {
            Text("The agent")
        } footer: {
            Text(agentFooterText)
        }
    }

    /// Where what the agent reads goes (to the provider of its model), and
    /// whose Health it reads.
    private var agentFooterText: String {
        let providerName = model.settings?.modelProviderName ?? ""
        let ownOnlyText = String(localized: "The agent reads only your own Health data, never what other people share with you.")
        if providerName.isEmpty {
            return String(localized: "What the agent reads goes to the provider of its model.") + " " + ownOnlyText
        }
        return String(localized: "What the agent reads goes to \(providerName).") + " " + ownOnlyText
    }

    // MARK: About the person

    /// The facts the Health app holds about the person, read only.
    @ViewBuilder
    private var aboutSection: some View {
        if !model.aboutRows.isEmpty {
            Section {
                ForEach(model.aboutRows) { row in
                    LabeledContent(row.labelText) {
                        Text(verbatim: row.valueText)
                    }
                }
            } header: {
                Text("About you")
            } footer: {
                Text("Set in the Health app on your iPhone, and shown here as the app copied it. Change them there.")
            }
        }
    }

    // MARK: Erasing

    private var eraseSection: some View {
        Section {
            Button(role: .destructive) {
                isConfirmingErase = true
            } label: {
                // The icon takes the tint unless it is colored too.
                Label("Erase all Health data", systemImage: "trash").foregroundStyle(.red)
            }
            .tint(.red)
            .actionColor(isDestructive: true)
            .disabled(model.isBusy)
            .confirmationDialog("Erase all Health data?", isPresented: $isConfirmingErase, titleVisibility: .visible) {
                Button("Erase", role: .destructive) {
                    Task { await model.eraseEverything() }
                }
            } message: {
                Text(
                    "Erases every reading, day summary and route this server keeps, and the iPhone app stops sending until you turn Health on again. Anything your agent already learned stays in its memory until you remove it. This cannot be undone."
                )
            }
        }
    }
}
