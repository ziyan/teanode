import SwiftUI
import TeaNodeKit

/// MoreScreen holds what the dashboard has beyond the four tabs: the agent's
/// knowledge and the settings.
struct MoreScreen: View {
    let connection: Connection
    @Environment(AppModel.self) private var app
    @State private var path = NavigationPath()

    enum Place: Hashable {
        case knowledge, settings
    }

    var body: some View {
        NavigationStack(path: $path) {
            List {
                NavigationLink(value: Place.knowledge) {
                    Label("Knowledge", systemImage: "brain")
                }
                NavigationLink(value: Place.settings) {
                    Label("Settings", systemImage: "gearshape")
                }
            }
            .navigationTitle("More")
            .modifier(KnowledgeDestination(connection: connection, path: $path))
            .navigationDestination(for: Place.self) { place in
                switch place {
                case .knowledge:
                    KnowledgeScreen(connection: connection, path: $path)
                case .settings:
                    SettingsForm()
                }
            }
        }
        .onChange(of: app.openedKnowledgePath, initial: true) { _, knowledgePath in
            guard let knowledgePath else { return }
            app.openedKnowledgePath = nil
            path = NavigationPath()
            path.append(Place.knowledge)
            path.append(KnowledgePlace.page(knowledgePath))
        }
    }
}
