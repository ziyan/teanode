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
            if let missing = missingPermission {
                Section {
                    Label {
                        Text(missing.explanation)
                    } icon: {
                        Image(systemName: "exclamationmark.triangle")
                    }
                    Button(missing.actionTitle, systemImage: missing.actionSymbol) { missing.perform() }
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
                Button("Erase history", systemImage: "trash", role: .destructive) {
                    isConfirmingErase = true
                }
                .disabled(isErasing)
                .confirmationDialog("Erase everything recorded of where you have been?", isPresented: $isConfirmingErase, titleVisibility: .visible) {
                    Button("Erase history", role: .destructive) {
                        Task {
                            isErasing = true
                            await model.eraseHistory()
                            isErasing = false
                        }
                    }
                } message: {
                    Text("Every visit, place and position is deleted from your server. This cannot be undone.")
                }
            } footer: {
                Text("Your agent and the people you share with can no longer see what is erased.")
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
                    LocationRecorder.shared.requestAlways()
                    dismiss()
                } label: {
                    Text("Continue").frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                Button {
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
    }
}
