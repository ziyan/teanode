import MapKit
import SwiftUI
import TeaNodeKit

/// HealthRouteScreen is the path of a workout on a map, with what the
/// workout was. Routes are the person's own: a share never reaches them.
struct HealthRouteScreen: View {
    let connection: Connection
    let request: HealthRouteRequest
    @State private var points: [HealthRoutePoint] = []
    @State private var position = MapCameraPosition.automatic
    @State private var isLoading = true
    @State private var loadFailedText: String?
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    private var coordinates: [CLLocationCoordinate2D] {
        points.map { CLLocationCoordinate2D(latitude: $0.latitudeDegrees, longitude: $0.longitudeDegrees) }
    }

    var body: some View {
        GeometryReader { proxy in
            VStack(spacing: 0) {
                routeMap
                    .frame(height: proxy.size.height * (dynamicTypeSize.isAccessibilitySize ? 0.3 : 0.55))
                    .accessibilityLabel("Map of the route")
                if let loadFailedText {
                    LoadFailedView(problemText: loadFailedText) { await load() }
                } else {
                    List {
                        Section {
                            detailRows
                        }
                    }
                    .overlay {
                        if isLoading {
                            ProgressView()
                        }
                    }
                    // On under the tab bar to the bottom of the screen, as a
                    // list on its own goes, its last rows scrolling up above it.
                    .safeAreaPadding(.bottom, proxy.safeAreaInsets.bottom)
                    .ignoresSafeArea(.container, edges: .bottom)
                }
            }
        }
        .navigationTitle(request.titleText)
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private var routeMap: some View {
        Map(position: $position) {
            if coordinates.count > 1 {
                MapPolyline(coordinates: coordinates).stroke(.tint, lineWidth: 4)
            }
            if let startCoordinate = coordinates.first {
                Marker("Start", systemImage: "flag.fill", coordinate: startCoordinate)
            }
            if coordinates.count > 1, let finishCoordinate = coordinates.last {
                Marker("Finish", systemImage: "flag.checkered", coordinate: finishCoordinate)
            }
        }
    }

    /// What the workout was: when, for how long, how far and how much energy.
    @ViewBuilder
    private var detailRows: some View {
        if let startedAt = request.startedAt {
            LabeledContent("Date") {
                Text(verbatim: HealthText.momentText(startedAt))
            }
        }
        LabeledContent("Duration") {
            Text(verbatim: HealthText.durationText(request.durationSeconds)).monospacedDigit()
        }
        if let distanceMeters = request.distanceMeters {
            LabeledContent("Distance") {
                Text(verbatim: HealthText.quantityText(distanceMeters, healthType: "workout", quantityUnit: "m")).monospacedDigit()
            }
        }
        if let energyKilocalories = request.energyKilocalories {
            LabeledContent("Energy") {
                Text(verbatim: HealthText.quantityText(energyKilocalories, healthType: "workout", quantityUnit: "kcal")).monospacedDigit()
            }
        }
        if !isLoading, points.count < 2 {
            Text("No route was recorded for this workout.").foregroundStyle(.secondary)
        }
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            let route = try await connection.healthRoute(healthSampleId: request.healthSampleId)
            points = route.points
            loadFailedText = nil
            if let region = LocationMapRegion.fitting(coordinates) { position = .region(region) }
        } catch {
            if Task.isCancelled { return }
            loadFailedText = error.loadProblemText
        }
    }
}
