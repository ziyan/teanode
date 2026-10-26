import SwiftUI
import TeaNodeKit

// What the day's work looks like: a row for each project, and the photos of
// the day in the app's own viewer.

/// JournalWorkProjectRow is one project of the day as one row: its name, how
/// long it took and when, what the agent wrote was done, what it was about,
/// and the requests that were sent for review, each opening in the browser.
struct JournalWorkProjectRow: View {
    let project: JournalWorkProject
    @Environment(\.openURL) private var openURL

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(verbatim: project.name)
                .font(.headline)
            if !timeText.isEmpty {
                Text(verbatim: timeText)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }
            if !project.summary.isEmpty {
                Text(verbatim: project.summary)
                    .fontDesign(.serif)
            }
            topicLines
            requestLines
        }
        .padding(.vertical, 2)
    }

    private var timeText: String { JournalText.projectTimeText(project) }

    /// The first few topics, and how many more there are.
    @ViewBuilder
    private var topicLines: some View {
        let listed = project.shownTopics()
        if !listed.shown.isEmpty {
            VStack(alignment: .leading, spacing: 2) {
                ForEach(Array(listed.shown.enumerated()), id: \.offset) { _, topic in
                    Text(verbatim: "\u{2022} " + topic)
                }
                if listed.hiddenCount > 0 {
                    Text("and \(listed.hiddenCount) more")
                        .foregroundStyle(.secondary)
                }
            }
            .font(.subheadline)
        }
    }

    @ViewBuilder
    private var requestLines: some View {
        if !project.shipped.isEmpty {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(project.shipped) { request in
                    requestLine(request)
                }
            }
        }
    }

    @ViewBuilder
    private func requestLine(_ request: JournalShipped) -> some View {
        if let address = request.webAddress {
            Button {
                openURL(address)
            } label: {
                requestLabel(request, isLink: true)
            }
            .buttonStyle(.borderless)
        } else {
            requestLabel(request, isLink: false)
        }
    }

    private func requestLabel(_ request: JournalShipped, isLink: Bool) -> some View {
        HStack(spacing: 8) {
            Text(verbatim: request.title)
                .multilineTextAlignment(.leading)
            if request.isDraft {
                Text("Draft")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 1)
                    .background(Color(.tertiarySystemFill), in: Capsule())
            }
            Spacer(minLength: 0)
            if isLink {
                Image(systemName: "arrow.up.right")
                    .font(.footnote.weight(.semibold))
                    .accessibilityHidden(true)
            }
        }
        .frame(minHeight: 36)
        .contentShape(Rectangle())
    }
}

/// JournalOpenedPhoto is a photo of the day that was tapped, to open in the
/// viewer over the day.
struct JournalOpenedPhoto: Identifiable, Hashable {
    let photoId: String
    var id: String { photoId }
}

/// JournalPhotoViewer opens a photo of the day in the app's photo viewer,
/// over the day, with the day's other photos a swipe away. The photos are not
/// in any list the Photos screens have read, so it has a model of its own,
/// which holds the tapped photo at once and the others as they are read.
struct JournalPhotoViewer: View {
    let photoIds: [String]
    let openedPhotoId: String
    @State private var model: PhotosModel
    @State private var openedPhoto: Photo?
    @State private var readiness = Readiness.reading
    @Environment(\.dismiss) private var dismiss

    private enum Readiness { case reading, ready, failed(String) }

    init(photoIds: [String], openedPhotoId: String, connection: Connection) {
        self.photoIds = photoIds
        self.openedPhotoId = openedPhotoId
        _model = State(initialValue: PhotosModel(connection: connection))
    }

    var body: some View {
        switch readiness {
        case .ready:
            PhotoViewer(model: model, listKey: .library, openedPhotoId: openedPhotoId, canShowNearby: false)
                .task { await readTheRest() }
        case .reading:
            ProgressView()
                .task { await read() }
        case .failed(let problemText):
            NavigationStack {
                ContentUnavailableView {
                    Label("Cannot show this photo", systemImage: "exclamationmark.circle")
                } description: {
                    Text(problemText.asReasonSentence)
                }
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Close", systemImage: "xmark") { dismiss() }
                    }
                }
            }
        }
    }

    private func read() async {
        do {
            let detail = try await model.connection.photoDetail(openedPhotoId)
            openedPhoto = detail.photo
            model.hold([detail.photo], as: .library)
            readiness = .ready
        } catch {
            readiness = .failed(error.localizedDescription)
        }
    }

    /// Reads the day's other photos one after another and puts them in the
    /// order the day lists them, so that the viewer can page to them as they
    /// arrive. One that cannot be read is left out.
    private func readTheRest() async {
        guard let openedPhoto else { return }
        var found: [String: Photo] = [openedPhoto.id: openedPhoto]
        for photoId in photoIds where found[photoId] == nil {
            if Task.isCancelled { return }
            guard let detail = try? await model.connection.photoDetail(photoId) else { continue }
            found[photoId] = detail.photo
            model.hold(photoIds.compactMap { found[$0] }, as: .library)
        }
    }
}
