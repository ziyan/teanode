import SwiftUI
import TeaNodeKit

// The notice at the top of the people on the Location screen when the person
// sees less of others than they share: their own phone's permission, the
// recording switch, a pause, or what they share back.

/// LocationSharingBannerSection is a section of the list over the people:
/// what the person cannot see and why, with the one action that fixes it. It
/// draws nothing while the server says nothing limits them.
struct LocationSharingBannerSection: View {
    let model: LocationModel
    /// Opens the person's own shares.
    let showSharing: () -> Void
    private let recorder = LocationRecorder.shared

    var body: some View {
        if let banner = model.sharingBanner {
            Section {
                Label {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(verbatim: Self.titleText(of: banner))
                            .font(.headline)
                            .foregroundStyle(.primary)
                        ForEach(Self.bodyLines(of: banner), id: \.self) { line in
                            Text(verbatim: line)
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                        }
                    }
                } icon: {
                    Image(systemName: Self.symbolName(of: banner))
                        .symbolRenderingMode(.hierarchical)
                        .foregroundStyle(.primary)
                }
                Button(Self.actionTitle(of: banner), systemImage: Self.actionSymbolName(of: banner)) {
                    perform(banner)
                }
                .actionColor()
            }
        }
    }

    private func perform(_ banner: LocationSharingBanner) {
        switch banner {
        case .paused:
            recorder.resumeRecording()
            model.doneText = String(localized: "Recording resumed")
        case .recordingOff:
            recorder.setRecording(true)
        case .phoneLocationOff, .phoneWhileUsing:
            LocationSettingsView.openSettings()
        case .sharesLess:
            showSharing()
        }
    }

    static func titleText(of banner: LocationSharingBanner) -> String {
        switch banner {
        case .paused, .recordingOff, .phoneLocationOff: String(localized: "You can't see anyone's location")
        case .phoneWhileUsing: String(localized: "You can't see anyone's visits")
        case .sharesLess: String(localized: "You see less of some people")
        }
    }

    /// The lines under the title: one sentence, or one line for each person
    /// seen less of, the first few by name.
    static func bodyLines(of banner: LocationSharingBanner) -> [String] {
        switch banner {
        case .paused:
            return [String(localized: "Your location is paused. Resume to see where people are again.")]
        case .recordingOff:
            return [String(localized: "Recording is off. Turn it on to see where people are again.")]
        case .phoneLocationOff:
            return [String(localized: "Location is off for TeaNode. Set it to Always in Settings to see where people are and their visits.")]
        case .phoneWhileUsing:
            return [
                String(
                    localized:
                        "Location is set to While Using, so TeaNode can't record your visits. You see where people are now, but not where they've been. Set it to Always in Settings to see visits again."
                )
            ]
        case .sharesLess(let people):
            var lines: [String] = []
            for person in people.prefix(LocationSharingNotice.mostNamedPeople) {
                if person.isSeeingNothing {
                    lines.append(String(localized: "You don't share your location with \(person.displayName), so you can't see theirs."))
                } else {
                    lines.append(
                        String(localized: "You share only where you are with \(person.displayName), so you can't see their visits."))
                }
            }
            let moreCount = people.count - LocationSharingNotice.mostNamedPeople
            if moreCount > 0 { lines.append(String(localized: "and \(moreCount) more")) }
            return lines
        }
    }

    static func symbolName(of banner: LocationSharingBanner) -> String {
        switch banner {
        case .paused: "pause.circle"
        case .recordingOff, .phoneLocationOff: "location.slash"
        case .phoneWhileUsing: "location.circle"
        case .sharesLess: "eye.slash"
        }
    }

    static func actionTitle(of banner: LocationSharingBanner) -> String {
        switch banner {
        case .paused: String(localized: "Resume")
        case .recordingOff: String(localized: "Turn On Recording")
        case .phoneLocationOff, .phoneWhileUsing: String(localized: "Open Settings")
        case .sharesLess: String(localized: "Sharing")
        }
    }

    static func actionSymbolName(of banner: LocationSharingBanner) -> String {
        switch banner {
        case .paused: "play.circle"
        case .recordingOff: "location"
        case .phoneLocationOff, .phoneWhileUsing: "gearshape"
        case .sharesLess: "person.2"
        }
    }
}
