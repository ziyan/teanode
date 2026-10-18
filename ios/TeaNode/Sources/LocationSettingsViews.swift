import SwiftUI
import TeaNodeKit

/// LocationSettingsView is what the person decides about being recorded:
/// the switch, what is missing for it to work while the app is closed, and
/// erasing what was recorded.
struct LocationSettingsView: View {
    let model: LocationModel
    @State private var isConfirmingErase = false
    @State private var isErasing = false
    private let recorder = LocationRecorder.shared

    var body: some View {
        Form {
            if let refusalText = recorder.refusalText {
                Section {
                    Label {
                        Text("The server did not take what this phone noticed, so recording has stopped: \(refusalText)")
                    } icon: {
                        Image(systemName: "exclamationmark.triangle")
                    }
                } footer: {
                    Text("Turn Record where I go off and on again to try once more.")
                }
            }
            if let missing = missingPermission {
                Section {
                    Label {
                        Text(missing.explanation)
                    } icon: {
                        Image(systemName: "exclamationmark.triangle")
                    }
                    Button(missing.actionTitle, systemImage: missing.actionSymbol) { missing.perform() }
                        .actionColor()
                }
            }
            Section {
                Toggle(
                    "Record where I go",
                    isOn: Binding(get: { recorder.isRecordingOn }, set: { recorder.setRecording($0) }))
                if recorder.unsentCount > 0 {
                    LabeledContent("Waiting to be sent") {
                        Text("\(recorder.unsentCount)")
                    }
                }
            } footer: {
                Text(
                    "TeaNode notes where you stop and when you leave, even when the app is closed. Turned off, it records nothing new. What is already recorded stays until you erase it.")
            }
            Section {
                Button(role: .destructive) {
                    isConfirmingErase = true
                } label: {
                    // The icon takes the tint unless it is colored too.
                    Label("Erase positions and visits", systemImage: "trash").foregroundStyle(.red)
                }
                .tint(.red)
                .actionColor(isDestructive: true)
                .disabled(isErasing)
                .confirmationDialog("Erase your positions and visits?", isPresented: $isConfirmingErase, titleVisibility: .visible) {
                    Button("Erase positions and visits", role: .destructive) {
                        Task {
                            isErasing = true
                            await model.eraseHistory()
                            isErasing = false
                        }
                    }
                } message: {
                    Text(
                        "Erases your positions and visits, and the places left with no visit that you did not name or label. Places you named or labeled stay. Anything your agent already learned from those days stays in its memory until you remove it. This cannot be undone."
                    )
                }
            } footer: {
                Text("The people you share with can no longer see what is erased.")
            }
        }
        .navigationTitle("Location settings")
        .navigationBarTitleDisplayMode(.inline)
    }

    /// What is missing for recording to work as the person expects, once.
    private var missingPermission: MissingLocationPermission? {
        if recorder.isAuthorizationUndecided {
            return MissingLocationPermission(
                explanation: String(localized: "TeaNode is not allowed to know where you are yet."),
                actionTitle: String(localized: "Allow location"), actionSymbol: "location") {
                    recorder.askForAuthorizationIfUndecided()
                }
        }
        if recorder.isLocationDenied {
            return MissingLocationPermission(
                explanation: String(localized: "Location is turned off for TeaNode, so it cannot show or record where you are."),
                actionTitle: String(localized: "Open Settings"), actionSymbol: "gearshape") { Self.openSettings() }
        }
        if !recorder.isAuthorizedAlways {
            return MissingLocationPermission(
                explanation: String(localized: "TeaNode follows you only while it is open. Allow location Always to record where you go while it is closed."),
                actionTitle: String(localized: "Open Settings"), actionSymbol: "gearshape") { Self.openSettings() }
        }
        return nil
    }

    static func openSettings() {
        guard let address = URL(string: UIApplication.openSettingsURLString) else { return }
        UIApplication.shared.open(address)
    }
}

struct MissingLocationPermission {
    let explanation: String
    let actionTitle: String
    let actionSymbol: String
    let perform: () -> Void
}

/// AlwaysExplanationSheet says why TeaNode asks for Always, before iOS asks.
struct AlwaysExplanationSheet: View {
    @Environment(\.dismiss) private var dismiss
    /// Whether the person pressed one of the two buttons. Swiping the sheet
    /// away is a "Not now" too: it would otherwise come back every time.
    @State private var isAnswered = false

    var body: some View {
        ScrollView {
            VStack(spacing: 16) {
                Image(systemName: "location.circle")
                    .font(.largeTitle)
                    .imageScale(.large)
                    .foregroundStyle(.tint)
                    .accessibilityHidden(true)
                Text("Record where you go")
                    .font(.title2.bold())
                    .multilineTextAlignment(.center)
                Text(
                    "TeaNode notes where you stop and when you leave, even when the app is closed. You can look back on it, share where you are with your family, and your agent knows your routine. On the next screen, choose Always Allow."
                )
                .multilineTextAlignment(.center)
                Button {
                    isAnswered = true
                    LocationRecorder.shared.requestAlways()
                    dismiss()
                } label: {
                    Text("Continue").frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                Button {
                    isAnswered = true
                    LocationRecorder.shared.declineAlwaysExplanation()
                    dismiss()
                } label: {
                    Text("Not now").frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .controlSize(.large)
            }
            .padding()
        }
        .presentationDetents([.medium, .large])
        .onDisappear {
            if !isAnswered { LocationRecorder.shared.declineAlwaysExplanation() }
        }
    }
}
