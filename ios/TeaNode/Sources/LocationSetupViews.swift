import SwiftUI
import TeaNodeKit

// Setting Location up the first time, in place of the lists: rows that each
// finish in one tap and show as done when they are.

/// LocationSetupList is shown while the server has no position for the
/// person. What is done is read from the permission and the server, never
/// stored; only "Not now" is kept.
struct LocationSetupList: View {
    let model: LocationModel
    let recorder: LocationRecorder
    /// Starts the permission flow: When In Use, then Always.
    let allowLocation: () -> Void
    @State private var isWorking = false

    var body: some View {
        let setup = model.setup
        List {
            Section {
                LocationSetupRow(
                    title: String(localized: "Allow location"),
                    detail: String(localized: "First while you use the app, then Always, so your visits are recorded when it is closed."),
                    symbolName: "location", isDone: setup.isPermissionDone, isEnabled: !isWorking, action: allowLocation)
                LocationSetupRow(
                    title: String(localized: "This is home"),
                    detail: recorder.isAuthorizedToLocate
                        ? String(localized: "Labels the place you are at now as Home.") : String(localized: "Allow location first."),
                    symbolName: "house", isDone: setup.isHomeDone, isEnabled: !isWorking && recorder.isAuthorizedToLocate
                ) {
                    run { await model.markHome() }
                }
                ForEach(setup.familyCircles) { circle in
                    let isShared = setup.isShared(with: circle)
                    LocationSetupRow(
                        title: isShared ? String(localized: "Shared with \(circle.name)") : String(localized: "Share with \(circle.name)"),
                        detail: LocationShareLevel.history.title, symbolName: "person.2", isDone: isShared, isEnabled: !isWorking
                    ) {
                        run { await model.shareWithFamily(circle, at: .history) }
                    }
                    if !isShared {
                        Button {
                            run { await model.shareWithFamily(circle, at: .current) }
                        } label: {
                            Label("Only where I am now, with \(circle.name)", systemImage: "location.circle")
                        }
                        .disabled(isWorking)
                        .frame(minHeight: 44)
                    }
                }
            } header: {
                Text("Set up Location").font(.headline).textCase(nil)
            } footer: {
                Text("Place names come from OpenStreetMap: your server sends the position of each new place to the place provider to look up its name.")
            }
            Section {
                Button("Not now") { model.dismissSetup() }
            } footer: {
                Text("Not now hides this. You can still allow location in the settings, label a place under Places, and share under Sharing.")
            }
        }
    }

    private func run(_ work: @escaping () async -> Void) {
        isWorking = true
        Task {
            await work()
            isWorking = false
        }
    }
}

/// LocationSetupRow is one step: a plain row button while there is something
/// to do, and a quiet row with a check once it is done.
struct LocationSetupRow: View {
    let title: String
    let detail: String
    let symbolName: String
    let isDone: Bool
    var isEnabled = true
    let action: () -> Void

    var body: some View {
        if isDone {
            row(symbolName: "checkmark.circle.fill", titleStyle: .secondary)
                .accessibilityElement(children: .combine)
                .accessibilityValue("Done")
        } else {
            Button(action: action) {
                row(symbolName: symbolName, titleStyle: .primary)
            }
            .disabled(!isEnabled)
        }
    }

    private func row(symbolName: String, titleStyle: HierarchicalShapeStyle) -> some View {
        HStack(spacing: 12) {
            Image(systemName: symbolName)
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(isDone ? AnyShapeStyle(.secondary) : AnyShapeStyle(.tint))
                .frame(width: 28)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: title).foregroundStyle(titleStyle)
                Text(verbatim: detail).font(.subheadline).foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
        }
        .frame(minHeight: 44)
    }
}
