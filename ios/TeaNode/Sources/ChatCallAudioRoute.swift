import AVFoundation
import AVKit
import Combine
import SwiftUI

/// ChatCallAudioRoute is where a call's sound goes now: the phone's earpiece,
/// its speaker, headphones on a wire, a Bluetooth headset such as AirPods, or
/// a car, as the call's audio button shows it.
enum ChatCallAudioRoute: Equatable {
    case phone
    case speaker
    case headphones
    case bluetooth(String)
    case car(String)
    case other(String)

    /// The route the sound takes now.
    static var current: ChatCallAudioRoute {
        guard let output = AVAudioSession.sharedInstance().currentRoute.outputs.first else { return .phone }
        switch output.portType {
        case .builtInReceiver: return .phone
        case .builtInSpeaker: return .speaker
        case .headphones, .usbAudio: return .headphones
        case .bluetoothHFP, .bluetoothA2DP, .bluetoothLE: return .bluetooth(output.portName)
        case .carAudio: return .car(output.portName)
        default: return .other(output.portName)
        }
    }

    var systemImage: String {
        switch self {
        case .phone: "iphone"
        case .speaker: "speaker.wave.3.fill"
        case .headphones: "headphones"
        case .bluetooth(let name): name.localizedCaseInsensitiveContains("AirPods") ? "airpods" : "headphones"
        case .car: "car.fill"
        case .other: "speaker.wave.2.fill"
        }
    }

    /// The route's name, as the system's list of routes calls it.
    var name: String {
        switch self {
        case .phone: String(localized: "iPhone")
        case .speaker: String(localized: "Speaker")
        case .headphones: String(localized: "Headphones")
        case .bluetooth(let name), .car(let name), .other(let name): name
        }
    }
}

/// ChatCallAudioRouteButton chooses where a call's sound goes: the phone, the
/// speaker, or a headset, from the system's own list of routes (the one the
/// Phone app shows), which also moves the microphone to a headset that has
/// one. It is drawn as the call's other buttons are, with the route's symbol,
/// and follows the route as it changes: AirPods put in or taken out, a car
/// connected.
struct ChatCallAudioRouteButton: View {
    @State private var route = ChatCallAudioRoute.current
    @ScaledMetric(relativeTo: .title2) private var diameter: CGFloat = 54

    private var glass: Glass {
        if route == .phone { return Glass.regular.interactive() }
        return Glass.regular.tint(Color.primary.opacity(0.25)).interactive()
    }

    var body: some View {
        ZStack {
            Image(systemName: route.systemImage)
                .font(.title2)
                .symbolRenderingMode(.hierarchical)
                .foregroundStyle(Color.primary)
                .accessibilityHidden(true)
            // The system's button, drawn clear over the symbol: it opens the
            // list of routes.
            ChatCallRoutePicker(routeName: route.name)
        }
        .frame(width: diameter, height: diameter)
        .contentShape(.circle)
        .glassEffect(glass, in: .circle)
        .onReceive(NotificationCenter.default.publisher(for: AVAudioSession.routeChangeNotification).receive(on: RunLoop.main)) { _ in
            route = .current
        }
        .onAppear { route = .current }
    }
}

/// ChatCallRoutePicker is the system's route picker with its own glyph made
/// clear, so the button under it shows the route.
private struct ChatCallRoutePicker: UIViewRepresentable {
    let routeName: String

    func makeUIView(context: Context) -> AVRoutePickerView {
        let picker = AVRoutePickerView()
        picker.tintColor = .clear
        picker.activeTintColor = .clear
        picker.prioritizesVideoDevices = false
        return picker
    }

    func updateUIView(_ picker: AVRoutePickerView, context: Context) {
        picker.accessibilityLabel = String(localized: "Audio output")
        picker.accessibilityValue = routeName
    }
}
