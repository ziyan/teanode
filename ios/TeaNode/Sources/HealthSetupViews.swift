import SwiftUI
import TeaNodeKit

// Turning Health on the first time: one screen that says what is copied,
// where it goes and who reads it, before iOS asks which data TeaNode may read.

/// HealthSetupSheet is HealthSetupView in a sheet, for the places that turn
/// Health on: the empty Health screen and the switch in Health settings.
struct HealthSetupSheet: View {
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        HealthSetupView { dismiss() }
            .presentationDetents([.large])
    }
}

/// HealthSetupView says what Health does before the system asks. Continue
/// turns Health on, which opens Apple's permission screen; Cancel leaves it
/// off, and is there whatever is happening.
struct HealthSetupView: View {
    /// Called when the person has answered, either way.
    let onFinish: () -> Void
    @State private var isTurningOn = false
    /// What turning Health on is doing, so that Cancel can stop it.
    @State private var turnOnTask: Task<Void, Never>?
    /// Why Health could not be turned on, said above the buttons while the
    /// sheet stays open.
    @State private var problemText: String?

    var body: some View {
        ScrollView {
            VStack(spacing: 24) {
                Image(systemName: "heart.text.square")
                    .font(.largeTitle)
                    .imageScale(.large)
                    .foregroundStyle(.tint)
                    .accessibilityHidden(true)
                Text("Copy your Health data to your server")
                    .font(.title2.bold())
                    .multilineTextAlignment(.center)
                VStack(alignment: .leading, spacing: 20) {
                    HealthSetupPoint(
                        symbolName: "tray.and.arrow.up", title: "What is copied",
                        text:
                            "The Health data you allow on the next screen: steps, heart rate, sleep, workouts with their routes, and more. TeaNode copies your history first, then keeps it up to date.")
                    HealthSetupPoint(
                        symbolName: "server.rack", title: "Where it goes",
                        text: "Only to your own TeaNode server, and nowhere else.")
                    HealthSetupPoint(
                        symbolName: "sparkles", title: "Who reads it",
                        text:
                            "Your agent reads it only if you turn on Let the agent read Health data, and what it reads goes to the provider of its model. Nobody else sees it unless you share some of it.")
                }
                Text("You can turn Health off, or erase everything copied, in Health settings.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                if let problemText {
                    Label {
                        Text(verbatim: problemText)
                    } icon: {
                        Image(systemName: "exclamationmark.triangle")
                    }
                    .foregroundStyle(.primary)
                    .multilineTextAlignment(.leading)
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                Button {
                    turnOn()
                } label: {
                    if isTurningOn {
                        ProgressView().frame(maxWidth: .infinity)
                    } else {
                        Text("Continue").frame(maxWidth: .infinity)
                    }
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .disabled(isTurningOn)
                Button {
                    cancel()
                } label: {
                    Text("Cancel").frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .controlSize(.large)
            }
            .padding()
        }
        // Swiping the sheet away is a Cancel too.
        .onDisappear { turnOnTask?.cancel() }
    }

    /// Turns Health on and closes as soon as Apple's screen is answered and
    /// the server has been asked; the first pass over the data runs after
    /// that, in the background, and the Health settings show how far it has
    /// got. If Health could not be turned on the sheet stays, with why.
    private func turnOn() {
        isTurningOn = true
        problemText = nil
        turnOnTask = Task {
            await HealthSync.shared.turnOn()
            guard !Task.isCancelled else { return }
            isTurningOn = false
            if HealthSync.shared.isOn {
                onFinish()
            } else {
                problemText = HealthSync.shared.problemText ?? String(localized: "Health could not be turned on. Try again.")
            }
        }
    }

    /// Leaves Health as it was: whatever is being asked is dropped, and an
    /// answer that comes later turns nothing on.
    private func cancel() {
        turnOnTask?.cancel()
        turnOnTask = nil
        onFinish()
    }
}

/// HealthSetupPoint is one thing the setup screen says: a symbol, a heading
/// and a sentence or two.
struct HealthSetupPoint: View {
    /// The column the symbol is in grows with the text size, so a symbol never
    /// outgrows it.
    @ScaledMetric(relativeTo: .title3) private var symbolColumnWidth = 32.0
    let symbolName: String
    let title: LocalizedStringKey
    let text: LocalizedStringKey

    var body: some View {
        HStack(alignment: .top, spacing: 14) {
            Image(systemName: symbolName)
                .symbolRenderingMode(.hierarchical)
                .font(.title3)
                .foregroundStyle(.primary)
                .frame(width: symbolColumnWidth)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.headline)
                Text(text).foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .accessibilityElement(children: .combine)
    }
}
