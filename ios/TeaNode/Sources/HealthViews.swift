import SwiftUI
import TeaNodeKit

// The Health screen: the kinds of Apple Health data the person has copied to
// their server, grouped as the Health app groups them. A kind opens its chart
// and readings; a workout opens its route. Somebody who shares Health with the
// person can be chosen in the bar, and then only the kinds and charts they
// share are shown.

/// HealthDestination is where the Health screen can push to.
enum HealthDestination: Hashable {
    case type(String)
    case route(HealthRouteRequest)
    case settings
}

/// HealthRouteRequest is a workout whose route is opened: what the screen
/// says about it above the map.
struct HealthRouteRequest: Hashable {
    let healthSampleId: String
    let titleText: String
    let startedAt: Date?
    let durationSeconds: Double
    let distanceMeters: Double?
    let energyKilocalories: Double?
}

extension HealthRouteRequest {
    init(sample: HealthSampleView) {
        let started = sample.startedDate
        let ended = sample.endedDate ?? started
        self.init(
            healthSampleId: sample.healthSampleId,
            titleText: HealthWords.workoutName(sample.workoutActivityType),
            startedAt: started,
            durationSeconds: max(0, (ended ?? Date()).timeIntervalSince(started ?? Date())),
            distanceMeters: HealthText.metadataNumber(sample.sampleMetadata[HealthWorkoutMetadata.totalDistanceMeters]),
            energyKilocalories: HealthText.metadataNumber(sample.sampleMetadata[HealthWorkoutMetadata.totalEnergyBurnedKilocalories]))
    }
}

/// HealthScreen is the person's Health data.
struct HealthScreen: View {
    @State private var model: HealthModel
    @State private var path: [HealthDestination] = []
    @State private var isSettingUp = false
    @Environment(AppModel.self) private var app
    private let sync = HealthSync.shared

    init(connection: Connection) {
        _model = State(initialValue: HealthModel(connection: connection))
    }

    var body: some View {
        NavigationStack(path: $path) {
            content
                .navigationTitle(HealthWords.title)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    MoreBackButton()
                    ToolbarItemGroup(placement: .topBarTrailing) {
                        if !model.sharers.isEmpty {
                            HealthPersonMenu(model: model)
                        }
                        NavigationLink(value: HealthDestination.settings) {
                            Label("Health settings", systemImage: "gearshape")
                        }
                    }
                }
                .navigationDestination(for: HealthDestination.self) { destination in
                    switch destination {
                    case .type(let healthType): typeScreen(healthType)
                    case .route(let request): HealthRouteScreen(connection: model.connection, request: request)
                    case .settings: HealthSettingsScreen(connection: model.connection)
                    }
                }
        }
        .sheet(isPresented: $isSettingUp) {
            HealthSetupSheet()
        }
        .toast(done: $model.doneText, problem: $model.problemText)
        .task {
            await model.load()
            #if DEBUG
                openDemoScreen()
            #endif
        }
        // Coming back from the settings, where Health may have been erased.
        .onChange(of: path.isEmpty) { _, isEmpty in
            if isEmpty { Task { await model.load() } }
        }
        // Health turned on from this screen: the sheet closing, a pass ending
        // and, while nothing is shown yet, the first data reaching the server
        // each read again, so that a new person sees their data arrive
        // without leaving the screen.
        .onChange(of: isSettingUp) { _, isSheetOpen in
            if !isSheetOpen { Task { await model.load() } }
        }
        .onChange(of: sync.isSyncing) { _, isSyncing in
            if !isSyncing { Task { await model.load() } }
        }
        .onChange(of: sync.sentSampleCount) {
            if model.types.isEmpty { Task { await model.load() } }
        }
        .onChange(of: app.openedHealthLink, initial: true) { _, link in
            guard let link else { return }
            app.openedHealthLink = nil
            if link.isSettings {
                path = [.settings]
            } else if let healthType = link.healthType {
                path = [.type(healthType)]
            } else {
                path = []
            }
        }
    }

    @ViewBuilder
    private func typeScreen(_ healthType: String) -> some View {
        if let type = model.types.first(where: { $0.healthType == healthType }) {
            HealthTypeScreen(model: model, type: type)
        } else if !model.hasLoaded {
            ProgressView()
        } else {
            // A link to a kind that does not exist, or that was erased or is
            // not shared; the way back is the bar's.
            ContentUnavailableView {
                Label("This kind of data is not here", systemImage: AppSection.health.systemImage)
            } description: {
                Text("It may have been erased, or it is not shared with you.")
            }
        }
    }

    @ViewBuilder
    private var content: some View {
        if let loadFailedText = model.loadFailedText {
            LoadFailedView(problemText: loadFailedText) { await model.load() }
        } else if !model.hasLoaded {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if model.types.isEmpty {
            HealthEmptyView(model: model) { isSettingUp = true }
        } else {
            List {
                ForEach(model.groups) { group in
                    Section {
                        ForEach(group.types) { type in
                            NavigationLink(value: HealthDestination.type(type.healthType)) {
                                HealthTypeRow(model: model, type: type)
                            }
                        }
                    } header: {
                        Text(verbatim: HealthWords.categoryName(group.healthCategory))
                    }
                }
            }
            .refreshable { await model.load() }
        }
    }

    #if DEBUG
        /// The screens a screenshot names: `-TeaNodeScreen health-steps` (the
        /// steps chart), `health-sleep` (a night's stages), `health-heart` (a
        /// line with its band), `health-route` (a workout on the map),
        /// `health-person` (somebody sharing Health with the person).
        private func openDemoScreen() {
            guard DemoServer.isRequested, path.isEmpty else { return }
            switch DemoServer.screen {
            case "health-steps": path = [.type("stepCount")]
            case "health-sleep": path = [.type("sleepAnalysis")]
            case "health-heart": path = [.type("heartRate")]
            case "health-route": path = [.route(HealthDemo.runRouteRequest)]
            case "health-person": Task { await model.select(userId: HealthDemo.sharingPersonId) }
            default: break
            }
        }
    #endif
}

/// HealthTypeRow is one kind of data: its name, the latest day it has, and
/// that day's value.
struct HealthTypeRow: View {
    let model: HealthModel
    let type: HealthTypeView

    var body: some View {
        LabeledContent {
            Text(verbatim: HealthText.latestAmountText(of: type) ?? "")
                .monospacedDigit()
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: model.name(of: type))
                if let latest = type.latestDaySummary {
                    Text(verbatim: HealthText.shortDayText(latest.summaryDay))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }
}

/// HealthPersonMenu chooses whose Health data the screen shows: the person's
/// own, or that of somebody who shares it.
struct HealthPersonMenu: View {
    let model: HealthModel

    var body: some View {
        Menu {
            Picker(
                "Showing",
                selection: Binding(
                    get: { model.selectedUserId ?? "" },
                    set: { chosen in Task { await model.select(userId: chosen.isEmpty ? nil : chosen) } })
            ) {
                Text("You").tag("")
                ForEach(model.sharers) { sharer in
                    Text(verbatim: sharer.displayName).tag(sharer.userId)
                }
            }
        } label: {
            Label("Showing", systemImage: "person.2")
        }
        .accessibilityValue(Text(verbatim: model.selectedSharer?.displayName ?? String(localized: "You")))
    }
}

/// HealthEmptyView is the screen with no data: what Health is, and the way to
/// turn it on.
struct HealthEmptyView: View {
    let model: HealthModel
    let turnOn: () -> Void
    private let sync = HealthSync.shared

    var body: some View {
        if let sharer = model.selectedSharer {
            ContentUnavailableView {
                Label(HealthWords.title, systemImage: AppSection.health.systemImage)
            } description: {
                Text("\(sharer.displayName) shares no Health data with you yet.")
            }
        } else {
            ContentUnavailableView {
                Label("No Health data yet", systemImage: AppSection.health.systemImage)
            } description: {
                if let problemText {
                    Text(verbatim: problemText)
                } else {
                    Text(descriptionText)
                }
            } actions: {
                if sync.isAvailable, !sync.isOn {
                    Button("Turn on Health", action: turnOn)
                        .buttonStyle(.borderedProminent)
                }
            }
        }
    }

    private var descriptionText: LocalizedStringKey {
        if !sync.isAvailable { return "Health is not available on this device." }
        if sync.isOn { return "Your Apple Health data is being copied to your server. It shows here as it arrives." }
        return "Health copies your Apple Health data to your own TeaNode server, and nowhere else. Turn on Health, then allow the data you want on Apple's Health permissions screen."
    }

    /// Why nothing is arriving: the copying met a problem, or it stopped
    /// because the server was erased from somewhere else.
    private var problemText: String? {
        if sync.isOn { return sync.problemText }
        return sync.isStoppedByErase ? HealthWords.erasedOnServerText : nil
    }
}

/// HealthNotAllowedView is what Health says to an account that may not use
/// it: that is a permission, not a service the server has turned off.
struct HealthNotAllowedView: View {
    var body: some View {
        ContentUnavailableView {
            Label("Health is not available to you", systemImage: AppSection.health.systemImage)
        } description: {
            Text("Your account is not allowed to copy or read Apple Health data. Whoever manages your TeaNode server can allow it.")
        }
    }
}
