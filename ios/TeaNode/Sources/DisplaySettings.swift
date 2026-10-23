import ObjectiveC
import SwiftUI
import UIKit

/// Appearance is light or dark as chosen in Settings, or the system's, as the
/// dashboard's theme menu offers.
enum Appearance: String, CaseIterable, Identifiable {
    case system, light, dark

    static let storageKey = "appearance"

    var id: String { rawValue }

    var title: LocalizedStringKey {
        switch self {
        case .system: "System"
        case .light: "Light"
        case .dark: "Dark"
        }
    }

    private var interfaceStyle: UIUserInterfaceStyle {
        switch self {
        case .system: .unspecified
        case .light: .light
        case .dark: .dark
        }
    }

    /// The scheme SwiftUI is told to draw in; nil follows the system.
    var colorScheme: ColorScheme? {
        switch self {
        case .system: nil
        case .light: .light
        case .dark: .dark
        }
    }

    /// The appearance on screen now, as a choice: the one chosen, or for
    /// System the one the system shows at this moment (the scene's style,
    /// which a window's own style does not change). While that cannot be
    /// read, as early in a launch, it stays System.
    @MainActor
    var shownNow: Appearance {
        guard self == .system else { return self }
        let windowScenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        let windowScene = windowScenes.first { $0.activationState == .foregroundActive } ?? windowScenes.first
        switch windowScene?.traitCollection.userInterfaceStyle {
        case .dark: return .dark
        case .light: return .light
        default: return self
        }
    }

    /// Sets every window of the app and every sheet open in it. The app's
    /// root is also given `colorScheme` as its preferredColorScheme, for the
    /// pictures the app switcher keeps, which the windows' style appeared not
    /// to reach; but a preferredColorScheme of nil leaves an open sheet in the
    /// last choice rather than returning it to the system's, so the sheets
    /// are set here as well.
    @MainActor
    func apply() {
        StyleChangeRedraw.watchWindows()
        for scene in UIApplication.shared.connectedScenes {
            guard let windowScene = scene as? UIWindowScene else { continue }
            // Only what differs is set: this runs each time the app comes to
            // the front, and a window given its style again, even the same one,
            // may work out its bars' look anew as the app wakes.
            for window in windowScene.windows {
                if window.overrideUserInterfaceStyle != interfaceStyle { window.overrideUserInterfaceStyle = interfaceStyle }
                var presented = window.rootViewController?.presentedViewController
                while let controller = presented {
                    if controller.overrideUserInterfaceStyle != interfaceStyle { controller.overrideUserInterfaceStyle = interfaceStyle }
                    presented = controller.presentedViewController
                }
            }
        }
    }
}

/// StyleChangeRedraw lays a window out again at once when its light or dark
/// style changes. As the app leaves the screen, iOS draws it in light and in
/// dark for the pictures the app switcher keeps, and takes each picture as
/// soon as the style is set: the parts iOS draws itself had changed by then,
/// but a conversation's bubbles and composer were still in the other style
/// (a dark chat with light bubbles). Laying every view out when the style
/// changes is meant to have SwiftUI draw them in the new style before the
/// picture is taken.
@MainActor
enum StyleChangeRedraw {
    /// The windows already watched, held weakly so a closed one goes.
    private static let watchedWindows = NSHashTable<UIWindow>.weakObjects()

    static func watchWindows() {
        for scene in UIApplication.shared.connectedScenes {
            guard let windowScene = scene as? UIWindowScene else { continue }
            for window in windowScene.windows where !watchedWindows.contains(window) {
                watchedWindows.add(window)
                window.registerForTraitChanges([UITraitUserInterfaceStyle.self]) { (changedWindow: UIWindow, _: UITraitCollection) in
                    // Trait changes arrive on the main thread.
                    MainActor.assumeIsolated { layOutNow(changedWindow) }
                }
            }
        }
    }

    /// Marks a view and everything in it as needing layout, then lays it out.
    private static func layOutNow(_ view: UIView) {
        markForLayout(view)
        view.layoutIfNeeded()
    }

    private static func markForLayout(_ view: UIView) {
        view.setNeedsLayout()
        for subview in view.subviews {
            markForLayout(subview)
        }
    }
}

/// DisplayLanguage is the language the app is shown in, chosen in Settings,
/// or the one the system gives it.
enum DisplayLanguage: String, CaseIterable, Identifiable {
    case system = ""
    case english = "en"
    case chinese = "zh-Hans"
    case japanese = "ja"

    static let storageKey = "displayLanguage"

    var id: String { rawValue }

    /// Each language is named in itself, so it can be found whatever the
    /// app is shown in.
    var nativeName: String? {
        switch self {
        case .system: nil
        case .english: "English"
        case .chinese: "简体中文"
        case .japanese: "日本語"
        }
    }

    static var chosen: DisplayLanguage {
        DisplayLanguage(rawValue: UserDefaults.standard.string(forKey: storageKey) ?? "") ?? .system
    }

    /// What words, dates and numbers are set in.
    var locale: Locale {
        self == .system ? .current : Locale(identifier: rawValue)
    }

    /// The language asked of the server, for ideas written in it.
    static var languageCode: String {
        chosen.locale.language.languageCode?.identifier ?? "en"
    }

    /// Switches the words the app looks up now, and tells the system the
    /// language for the next launch, so what the system draws (its buttons,
    /// dates and numbers) follows too.
    func apply() {
        if self == .system {
            UserDefaults.standard.removeObject(forKey: "AppleLanguages")
        } else {
            UserDefaults.standard.set([rawValue], forKey: "AppleLanguages")
        }
        LanguageBundle.use(self == .system ? nil : rawValue)
    }
}

/// LanguageBundle is the app's bundle with its words looked up in the chosen
/// language rather than the one the app was launched in. The system has no
/// switch for it while the app runs, so the main bundle's class is replaced
/// with this one, which reads from the chosen language's folder.
final class LanguageBundle: Bundle, @unchecked Sendable {
    /// The chosen language's folder, read on any thread, written only from
    /// Settings on the main thread.
    nonisolated(unsafe) private static var chosenBundle: Bundle?
    nonisolated(unsafe) private static var isLanguageChosen = false

    override func localizedString(forKey key: String, value: String?, table tableName: String?) -> String {
        guard Self.isLanguageChosen else {
            return super.localizedString(forKey: key, value: value, table: tableName)
        }
        if let bundle = Self.chosenBundle {
            return bundle.localizedString(forKey: key, value: value, table: tableName)
        }
        // English has no folder of its own when every word is written in
        // English in the code: the key is the English.
        if let value, !value.isEmpty { return value }
        return key
    }

    static func use(_ languageCode: String?) {
        if !(Bundle.main is LanguageBundle) {
            object_setClass(Bundle.main, LanguageBundle.self)
        }
        isLanguageChosen = languageCode != nil
        chosenBundle = languageCode
            .flatMap { Bundle.main.path(forResource: $0, ofType: "lproj") }
            .flatMap { Bundle(path: $0) }
    }
}

/// DisplaySection is where the app's look and language are chosen.
struct DisplaySection: View {
    @Environment(AppModel.self) private var app
    @AppStorage(Appearance.storageKey) private var appearance = Appearance.system
    @AppStorage(DisplayLanguage.storageKey) private var language = DisplayLanguage.system

    var body: some View {
        Section {
            Picker("Appearance", selection: Binding(get: { appearance }, set: { chosen in
                chosen.apply()
                appearance = chosen
            })) {
                ForEach(Appearance.allCases) { choice in
                    Text(choice.title).tag(choice)
                }
            }
            Picker("Language", selection: Binding(get: { language }, set: { chosen in
                // The app is drawn again in the new language; Settings is
                // opened again where it was.
                chosen.apply()
                app.keptSection = .settings
                language = chosen
            })) {
                ForEach(DisplayLanguage.allCases) { choice in
                    if let nativeName = choice.nativeName {
                        Text(verbatim: nativeName).tag(choice)
                    } else {
                        Text("System").tag(choice)
                    }
                }
            }
        } header: {
            Text("Display")
        }
    }
}
