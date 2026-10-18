import AVKit
import SwiftUI
import TeaNodeKit
import UIKit
import WebKit

extension EnvironmentValues {
    /// The conversation an answer belongs to, through which the pictures it
    /// shows are read.
    @Entry var pictureConversationId: String?
}

/// RemotePicture is a picture the server keeps, read with the app's sign-in;
/// tapping it opens it full screen, to zoom and share, unless the card around
/// it says what a tap does instead.
struct RemotePicture: View {
    let path: String
    var name = "picture.jpg"
    var maxHeight: CGFloat = 360
    /// What a tap does in place of the full-screen view: a card whose picture
    /// opens somewhere better than the lightbox (a photo in the library).
    var onOpen: (() -> Void)?
    @Environment(AppModel.self) private var app
    @State private var picture: UIImage?
    @State private var isMissing = false

    var body: some View {
        Group {
            if let picture {
                Button {
                    if let onOpen {
                        onOpen()
                    } else {
                        LightboxPresenter.present(picture, name: name)
                    }
                } label: {
                    Image(uiImage: picture)
                        .resizable()
                        .scaledToFit()
                        .frame(maxHeight: maxHeight)
                        .clipShape(.rect(cornerRadius: 12, style: .continuous))
                }
                // Borderless, not plain: in a list a plain button takes the
                // whole row's taps, and a row with other buttons beside the
                // picture opened the picture whatever was tapped.
                .buttonStyle(.borderless)
                .accessibilityLabel(name)
            } else if isMissing {
                Label(name, systemImage: "photo")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            } else {
                ProgressView()
                    .frame(width: 120, height: 90)
            }
        }
        .task(id: path) {
            picture = await app.picture(path)
            isMissing = picture == nil
        }
    }
}

/// LightboxPresenter shows a picture full screen over whatever is on
/// screen, a sheet included, as Photos does: from the top of the window
/// rather than from the view that was tapped, which inside a list or a
/// sheet had nothing to present from.
@MainActor
enum LightboxPresenter {
    static func present(_ picture: UIImage, name: String) {
        let window = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .flatMap(\.windows)
            .first(where: \.isKeyWindow)
        var top = window?.rootViewController
        while let presented = top?.presentedViewController, !presented.isBeingDismissed {
            top = presented
        }
        guard let top else { return }
        weak var shown: UIViewController?
        let controller = UIHostingController(rootView: PictureLightbox(picture: picture, name: name) {
            shown?.dismiss(animated: true)
        })
        controller.modalPresentationStyle = .overFullScreen
        controller.modalTransitionStyle = .crossDissolve
        controller.view.backgroundColor = .clear
        shown = controller
        top.present(controller, animated: true)
    }
}

/// PictureLightbox is one picture on black: pinch or double-tap to zoom,
/// drag down or the close button to go back, and share.
struct PictureLightbox: View {
    let picture: UIImage
    let name: String
    let onClose: () -> Void
    @State private var isZoomed = false
    @State private var draggedDown: CGFloat = 0

    var body: some View {
        ZStack {
            Color.black
                .opacity(1 - min(0.6, Double(abs(draggedDown)) / 400))
                .ignoresSafeArea()
            ZoomablePicture(picture: picture, isZoomed: $isZoomed)
                .ignoresSafeArea()
                .offset(y: draggedDown)
                .gesture(
                    // Down to close, only when not zoomed in: zoomed, a drag
                    // moves around the picture.
                    DragGesture(minimumDistance: 12)
                        .onChanged { value in
                            guard !isZoomed, abs(value.translation.height) > abs(value.translation.width) else { return }
                            draggedDown = value.translation.height
                        }
                        .onEnded { value in
                            guard !isZoomed else { return }
                            if abs(value.predictedEndTranslation.height) > 200 {
                                onClose()
                            } else {
                                withAnimation(.spring(duration: 0.25)) { draggedDown = 0 }
                            }
                        },
                    isEnabled: !isZoomed)
        }
        .overlay(alignment: .top) {
            HStack {
                Button(action: onClose) {
                    Image(systemName: "xmark")
                        .font(.title3.weight(.semibold))
                        .frame(width: 44, height: 44)
                }
                .buttonStyle(.glass)
                .accessibilityLabel("Close")
                Spacer()
                ShareLink(item: Image(uiImage: picture), preview: SharePreview(name, image: Image(uiImage: picture))) {
                    Image(systemName: "square.and.arrow.up")
                        .font(.title3.weight(.semibold))
                        .frame(width: 44, height: 44)
                }
                .buttonStyle(.glass)
                .accessibilityLabel("Share")
            }
            .padding(.horizontal)
            .opacity(draggedDown == 0 ? 1 : 0)
        }
        .statusBarHidden()
        .accessibilityAction(.escape, onClose)
    }
}

/// ZoomablePicture is a picture in UIKit's zooming scroll view: pinch and
/// double-tap zoom, and a drag pans while zoomed. SwiftUI has no zooming
/// container of its own.
struct ZoomablePicture: UIViewRepresentable {
    let picture: UIImage
    @Binding var isZoomed: Bool
    /// Whether this picture is the one on screen. A picture that stops being it
    /// (the person paged to another) goes back to its whole size, so that it is
    /// not still zoomed when it is paged back to.
    var isCurrent = true
    /// What a single tap does, such as hiding the bars around the picture. A
    /// double tap zooms instead, and does not count as one.
    var onSingleTap: (() -> Void)?

    func makeUIView(context: Context) -> UIScrollView {
        let scrollView = UIScrollView()
        scrollView.delegate = context.coordinator
        scrollView.minimumZoomScale = 1
        scrollView.maximumZoomScale = 5
        scrollView.showsHorizontalScrollIndicator = false
        scrollView.showsVerticalScrollIndicator = false
        scrollView.contentInsetAdjustmentBehavior = .never
        scrollView.backgroundColor = .clear
        let imageView = UIImageView(image: picture)
        imageView.contentMode = .scaleAspectFit
        imageView.isUserInteractionEnabled = true
        imageView.translatesAutoresizingMaskIntoConstraints = false
        scrollView.addSubview(imageView)
        NSLayoutConstraint.activate([
            imageView.widthAnchor.constraint(equalTo: scrollView.frameLayoutGuide.widthAnchor),
            imageView.heightAnchor.constraint(equalTo: scrollView.frameLayoutGuide.heightAnchor),
            imageView.leadingAnchor.constraint(equalTo: scrollView.contentLayoutGuide.leadingAnchor),
            imageView.trailingAnchor.constraint(equalTo: scrollView.contentLayoutGuide.trailingAnchor),
            imageView.topAnchor.constraint(equalTo: scrollView.contentLayoutGuide.topAnchor),
            imageView.bottomAnchor.constraint(equalTo: scrollView.contentLayoutGuide.bottomAnchor),
        ])
        context.coordinator.imageView = imageView
        let doubleTap = UITapGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.zoomOnDoubleTap(_:)))
        doubleTap.numberOfTapsRequired = 2
        scrollView.addGestureRecognizer(doubleTap)
        let singleTap = UITapGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.tapOnce(_:)))
        singleTap.numberOfTapsRequired = 1
        singleTap.require(toFail: doubleTap)
        scrollView.addGestureRecognizer(singleTap)
        return scrollView
    }

    func updateUIView(_ scrollView: UIScrollView, context: Context) {
        // The coordinator was made with the binding of the first update. A page
        // built while it was not the current one was given a binding that does
        // nothing, and would keep it: a zoomed photo then pages instead of
        // panning. Every update hands it the binding as it is now.
        context.coordinator.isZoomed = $isZoomed
        context.coordinator.onSingleTap = onSingleTap
        if !isCurrent, scrollView.zoomScale > 1 {
            scrollView.setZoomScale(1, animated: false)
        }
    }

    func makeCoordinator() -> Coordinator {
        Coordinator(isZoomed: $isZoomed, onSingleTap: onSingleTap)
    }

    @MainActor
    final class Coordinator: NSObject, UIScrollViewDelegate {
        var isZoomed: Binding<Bool>
        var onSingleTap: (() -> Void)?
        weak var imageView: UIImageView?

        init(isZoomed: Binding<Bool>, onSingleTap: (() -> Void)?) {
            self.isZoomed = isZoomed
            self.onSingleTap = onSingleTap
        }

        @objc func tapOnce(_ recognizer: UITapGestureRecognizer) {
            onSingleTap?()
        }

        func viewForZooming(in scrollView: UIScrollView) -> UIView? { imageView }

        func scrollViewDidZoom(_ scrollView: UIScrollView) {
            let isNowZoomed = scrollView.zoomScale > 1.01
            if isZoomed.wrappedValue != isNowZoomed { isZoomed.wrappedValue = isNowZoomed }
        }

        @objc func zoomOnDoubleTap(_ recognizer: UITapGestureRecognizer) {
            guard let scrollView = recognizer.view as? UIScrollView, let imageView else { return }
            if scrollView.zoomScale > 1.01 {
                scrollView.setZoomScale(1, animated: true)
            } else {
                let point = recognizer.location(in: imageView)
                let size = CGSize(width: scrollView.bounds.width / 2.5, height: scrollView.bounds.height / 2.5)
                scrollView.zoom(to: CGRect(origin: CGPoint(x: point.x - size.width / 2, y: point.y - size.height / 2), size: size), animated: true)
            }
        }
    }
}

/// AttachmentView is a file the person sent with a message: a picture as a
/// thumbnail, anything else as its name and size; a tap opens it.
struct AttachmentView: View {
    let attachment: AgentAttachment
    @State private var isImportingRecording = false
    @Environment(AppModel.self) private var app

    private var path: String { "/api/v1/agent/attachments/\(attachment.id)" }

    private var openButton: some View {
        Button {
            Task { await app.preview(path, name: attachment.name) }
        } label: {
            Label("Open", systemImage: "arrow.up.forward.app")
        }
    }

    /// Offered for a sound or a video when the person has recordings.
    @ViewBuilder
    private var saveAsRecordingButton: some View {
        if app.account?.cachedRecordingUserId != nil,
            attachment.contentType.hasPrefix("audio/") || attachment.contentType.hasPrefix("video/")
        {
            Button { isImportingRecording = true } label: { Label("Save as recording", systemImage: "waveform") }
        }
    }

    var body: some View {
        if attachment.isRemoved == true {
            // The message is kept and its file went with the account.
            Label("A file that was removed with the account", systemImage: "paperclip")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(Color(.tertiarySystemFill), in: .capsule)
        } else if attachment.contentType.hasPrefix("image/") {
            RemotePicture(path: path, name: attachment.name, maxHeight: 160)
                .frame(maxWidth: 200)
        } else {
            Button {
                Task { await app.preview(path, name: attachment.name) }
            } label: {
                Label {
                    Text(verbatim: "\(attachment.name) · \(attachment.size.formatted(.byteCount(style: .file)))")
                } icon: {
                    Image(systemName: "paperclip")
                }
                .font(.footnote)
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(Color(.tertiarySystemFill), in: .capsule)
            }
            .buttonStyle(.plain)
            .contextMenu {
                openButton
                saveAsRecordingButton
            }
            .accessibilityActions {
                saveAsRecordingButton
            }
            .sheet(isPresented: $isImportingRecording) {
                if let connection = app.connection { RecordingAttachmentImportView(connection: connection, attachment: attachment) }
            }
        }
    }
}

/// FileCard is a file the agent handed over: a picture, a video or a sound to
/// play, or any other file to open. The card is the file's name and size, the
/// file and its caption, and no buttons: a tap on the picture (or on the card
/// of a file with nothing to look at) opens it, and everything else it offers
/// is in the long-press menu, which VoiceOver reaches as the header's actions.
struct FileCard: View {
    let file: AgentFile
    @Environment(AppModel.self) private var app
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @State private var shareAddress: URL?
    @State private var openedPhotoId: OpenedPhoto?
    /// The photo made of this file once the person kept it in their library.
    @State private var savedPhotoId: String?
    @State private var isSavingToLibrary = false
    @State private var savingProblemText: String?
    /// The picture, once read, which is what Copy hands on and what the menu's
    /// preview shows. The picture inside the card asks the app for the same
    /// one, and the app downloads it once for both.
    @State private var loadedPicture: UIImage?

    /// The photo this file is: the one the photo tool showed, or the copy the
    /// person just kept.
    private var photoId: String? { file.photoId ?? savedPhotoId }

    /// Whether the file is a picture or video that can be kept as a photo.
    private var canBeKept: Bool { photoId == nil && (file.isImage || file.isVideo) && !file.isArtifact && app.isPhotosOffered }

    /// Whether a tap anywhere on the card opens the file: when there is no
    /// picture to tap and no player whose controls the tap would take.
    private var isCardOpenedByTap: Bool { !file.isImage && !file.isVideo && !file.isAudio }

    /// The file's name, and its size when the server says it. The name is cut in the middle when
    /// it does not fit, so that the extension stays visible; it is never wrapped, since a name
    /// is broken at its own hyphens and dots into pieces that read as separate words. At the
    /// accessibility sizes the size goes on a line of its own, so that it does not take the
    /// name's width.
    private var header: some View {
        let layout =
            dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 2)) : AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: 6))
        let sizeText = file.size.formatted(.byteCount(style: .file))
        return layout {
            CardHeader(systemImage: "paperclip", title: file.name)
                .lineLimit(1)
                .truncationMode(.middle)
            if file.size > 0 {
                Text(verbatim: dynamicTypeSize.isAccessibilitySize ? sizeText : "· \(sizeText)")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .fixedSize()
            }
            if isCardOpenedByTap, !dynamicTypeSize.isAccessibilitySize {
                // A card that is a button as a whole says so, as a row that opens something does.
                Spacer(minLength: 0)
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityActions { accessibilityActionButtons }
    }

    /// What a tap on the picture, or on a card with nothing else to tap, does.
    private func openMainFile() {
        if let photoId {
            // A photo the agent showed opens in the app's photo viewer, where
            // it can be favorited, saved or shared.
            openedPhotoId = OpenedPhoto(id: photoId)
        } else {
            Task { await app.preview(file.path, name: file.name) }
        }
    }

    /// The main action, which is the first row of the menu.
    private var openButton: some View {
        Button(action: openMainFile) {
            if photoId != nil {
                Label("Open in Photos", systemImage: "photo.on.rectangle")
            } else {
                Label("Open", systemImage: "arrow.up.forward.app")
            }
        }
    }

    /// Keeps the picture or video as a photo in TeaNode's library, when that is offered.
    @ViewBuilder
    private var keepButton: some View {
        if canBeKept {
            Button {
                Task { await keepInLibrary() }
            } label: {
                Label("Save to my library", systemImage: "photo.badge.plus")
            }
            .disabled(isSavingToLibrary)
        }
    }

    @ViewBuilder
    private var copyButton: some View {
        if let loadedPicture {
            Button {
                UIPasteboard.general.image = loadedPicture
            } label: {
                Label("Copy", systemImage: "doc.on.doc")
            }
        }
    }

    /// The long-press menu, the main action first. Share is the system's own
    /// ShareLink here.
    @ViewBuilder
    private var menuItems: some View {
        openButton
        keepButton
        if let loadedPicture {
            ShareLink(item: Image(uiImage: loadedPicture), preview: SharePreview(file.name, image: Image(uiImage: loadedPicture))) {
                Label("Share", systemImage: "square.and.arrow.up")
            }
        }
        copyButton
    }

    /// The same actions for VoiceOver, which cannot long press a menu open.
    /// A ShareLink cannot be run from an accessibility action, so Share
    /// opens the system's share sheet itself.
    @ViewBuilder
    private var accessibilityActionButtons: some View {
        openButton
        keepButton
        if let loadedPicture {
            Button {
                ShareSheetPresenter.present(loadedPicture)
            } label: {
                Label("Share", systemImage: "square.and.arrow.up")
            }
        }
        copyButton
    }

    private var card: some View {
        // The cards' shared surface, as wide as the other cards in the conversation.
        CardSurface {
            VStack(alignment: .leading, spacing: 12) {
                header
                if file.isImage {
                    RemotePicture(path: file.path, name: file.name, onOpen: openMainFile)
                } else if file.isVideo || file.isAudio {
                    if let shareAddress {
                        VideoPlayer(player: AVPlayer(url: shareAddress))
                            .frame(height: file.isVideo ? 220 : 60)
                            .clipShape(.rect(cornerRadius: 12, style: .continuous))
                    } else {
                        ProgressView()
                    }
                }
                if !file.caption.isEmpty {
                    Text(file.caption)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .overlay(alignment: .topTrailing) {
            if isSavingToLibrary {
                ProgressView().padding(16)
            }
        }
    }

    /// The card, a button as a whole when it has nothing else to tap.
    @ViewBuilder
    private var tappableCard: some View {
        if isCardOpenedByTap {
            Button(action: openMainFile) { card }
                .buttonStyle(.plain)
        } else {
            card
        }
    }

    /// The card with its long-press menu; a picture's menu shows the picture
    /// itself as its preview.
    @ViewBuilder
    private var cardWithMenu: some View {
        if file.isImage {
            tappableCard.contextMenu {
                menuItems
            } preview: {
                if let loadedPicture {
                    Image(uiImage: loadedPicture)
                        .resizable()
                        .scaledToFit()
                } else {
                    ProgressView().padding()
                }
            }
        } else {
            tappableCard.contextMenu { menuItems }
        }
    }

    var body: some View {
        cardWithMenu
            .sensoryFeedback(.success, trigger: savedPhotoId)
            .alert("Cannot save to my library", isPresented: Binding(get: { savingProblemText != nil }, set: { if !$0 { savingProblemText = nil } }), presenting: savingProblemText) { _ in
                Button("OK", role: .cancel) {}
            } message: { text in
                Text(text)
            }
            .fullScreenCover(item: $openedPhotoId) { opened in
                if let connection = app.connection {
                    AgentPhotoViewer(photoId: opened.id, connection: connection)
                }
            }
            .task(id: file.attachmentId) {
                if file.isImage {
                    loadedPicture = await app.picture(file.path)
                }
                // A player cannot send the app's sign-in, so it is given an
                // address that opens without one, for a few hours.
                if file.isVideo || file.isAudio, let connection = app.connection {
                    shareAddress = try? await connection.shareAddress(attachmentId: file.attachmentId)
                }
            }
    }
}

/// ShareSheetPresenter opens the system's share sheet from the top of the
/// window, for the places where an accessibility action has to share and a
/// ShareLink cannot be run from code.
@MainActor
enum ShareSheetPresenter {
    static func present(_ item: Any) {
        let window = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .flatMap(\.windows)
            .first(where: \.isKeyWindow)
        var top = window?.rootViewController
        while let presented = top?.presentedViewController, !presented.isBeingDismissed {
            top = presented
        }
        guard let top else { return }
        let controller = UIActivityViewController(activityItems: [item], applicationActivities: nil)
        // An iPad shows the sheet as a popover, which needs somewhere to point.
        controller.popoverPresentationController?.sourceView = top.view
        controller.popoverPresentationController?.sourceRect = CGRect(x: top.view.bounds.midX, y: top.view.bounds.midY, width: 0, height: 0)
        top.present(controller, animated: true)
    }
}

extension FileCard {
    /// Keeps the file as a photo of the person's own library.
    fileprivate func keepInLibrary() async {
        guard let connection = app.connection else { return }
        isSavingToLibrary = true
        defer { isSavingToLibrary = false }
        do {
            savedPhotoId = try await connection.savePhotoFromAttachment(file.attachmentId).id
        } catch {
            savingProblemText = app.describe(error)
        }
    }
}

/// AgentPhotoViewer opens one photo the agent showed in a conversation in the
/// app's photo viewer. The photo is not in any list the Photos screens have
/// read, so it has a model of its own holding just that photo.
struct AgentPhotoViewer: View {
    let photoId: String
    @State private var model: PhotosModel
    @State private var readiness = Readiness.reading
    @Environment(\.dismiss) private var dismiss

    private enum Readiness { case reading, ready, failed(String) }

    init(photoId: String, connection: Connection) {
        self.photoId = photoId
        _model = State(initialValue: PhotosModel(connection: connection))
    }

    var body: some View {
        switch readiness {
        case .ready:
            PhotoViewer(model: model, listKey: .library, openedPhotoId: photoId, canShowNearby: false)
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
            let detail = try await model.connection.photoDetail(photoId)
            model.hold([detail.photo], as: .library)
            readiness = .ready
        } catch {
            readiness = .failed(error.localizedDescription)
        }
    }
}

/// ArtifactCard is something the agent made: a page, a picture drawn in SVG,
/// or a Markdown document, shown in place. Its name opens it whole on a tap;
/// that and what else it offers are in the long-press menu too, which VoiceOver
/// reaches as the name's actions. A long press on a page that is running is
/// the page's own, so the name is where the menu is sure to open.
struct ArtifactCard: View {
    let file: AgentFile
    @Environment(AppModel.self) private var app
    @Environment(\.colorScheme) private var colorScheme
    @State private var address: URL?
    @State private var markdown: String?
    @State private var isOpen = false

    private var isReady: Bool { address != nil || markdown != nil }

    private var openButton: some View {
        Button {
            isOpen = true
        } label: {
            Label("Open", systemImage: "arrow.up.forward.app")
        }
        .disabled(!isReady)
    }

    /// Copy and Share are for a document, whose words can go elsewhere; a
    /// page or a drawing has no text to hand on.
    @ViewBuilder
    private var copyButton: some View {
        if let markdown {
            Button {
                UIPasteboard.general.string = markdown
            } label: {
                Label("Copy", systemImage: "doc.on.doc")
            }
        }
    }

    var body: some View {
        AgentBubble {
            VStack(alignment: .leading, spacing: 8) {
                Button {
                    isOpen = true
                } label: {
                    HStack(spacing: 6) {
                        Label(file.name.isEmpty ? String(localized: "Untitled") : file.name, systemImage: "sparkles")
                            .font(.subheadline.bold())
                            .lineLimit(1)
                        Spacer(minLength: 8)
                        Image(systemName: "chevron.right")
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(.tertiary)
                            .accessibilityHidden(true)
                    }
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .disabled(!isReady)
                .accessibilityActions {
                    openButton
                    if let markdown {
                        Button {
                            ShareSheetPresenter.present(markdown)
                        } label: {
                            Label("Share", systemImage: "square.and.arrow.up")
                        }
                    }
                    copyButton
                }
                Group {
                    if let markdown {
                        ScrollView { MarkdownView(text: markdown) }
                    } else if let address {
                        ArtifactWebView(address: address)
                    } else {
                        ProgressView().frame(maxWidth: .infinity)
                    }
                }
                .frame(height: 320)
                .clipShape(.rect(cornerRadius: 12, style: .continuous))
            }
        }
        .contextMenu {
            openButton
            if let markdown {
                ShareLink(item: markdown) {
                    Label("Share", systemImage: "square.and.arrow.up")
                }
            }
            copyButton
        }
        .sheet(isPresented: $isOpen) {
            NavigationStack {
                Group {
                    if let markdown {
                        ScrollView { MarkdownView(text: markdown).padding() }
                    } else if let address {
                        ArtifactWebView(address: address)
                    }
                }
                .navigationTitle(file.name)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Done") { isOpen = false }
                    }
                }
            }
        }
        .task(id: "\(file.attachmentId)-\(colorScheme == .dark)") {
            guard let connection = app.connection else { return }
            if file.artifactKind == "markdown" {
                if let data = try? await connection.download(file.path) {
                    markdown = String(decoding: data, as: UTF8.self)
                }
                return
            }
            // The page runs in a web view that has no sign-in: it is given an
            // address that opens without one, and the theme to draw in.
            if let shared = try? await connection.shareAddress(attachmentId: file.attachmentId),
                var components = URLComponents(url: shared, resolvingAgainstBaseURL: false)
            {
                components.fragment = "theme=\(colorScheme == .dark ? "dark" : "light")"
                address = components.url
            }
        }
    }
}

/// ToolFiles are the files and artifacts a tool line handed over, shown
/// whether or not tool calls are.
struct ToolFiles: View {
    let line: TranscriptLine

    var body: some View {
        ForEach(AgentFile.of(toolResult: line.text, tool: line.detail), id: \.attachmentId) { file in
            if file.isArtifact {
                ArtifactCard(file: file)
            } else {
                FileCard(file: file)
            }
        }
    }
}

/// PendingFileChip is a file waiting in the composer: the picture, or the
/// file's name, with a way to take it back out.
struct PendingFileChip: View {
    let file: PendingFile
    /// On its way to the server, with the message being sent.
    var isUploading = false
    /// On the server, while the rest of the message's files are sent.
    var isUploaded = false
    let onRemove: () -> Void

    var body: some View {
        Group {
            if let preview = file.preview {
                Image(uiImage: preview)
                    .resizable()
                    .scaledToFill()
                    .frame(width: 56, height: 56)
                    .clipShape(.rect(cornerRadius: 10, style: .continuous))
            } else {
                Label(file.name, systemImage: "paperclip")
                    .font(.caption)
                    .lineLimit(1)
                    .frame(maxWidth: 160, minHeight: 56)
                    .padding(.horizontal, 8)
                    .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 10, style: .continuous))
            }
        }
        .overlay {
            if isUploading {
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(.black.opacity(isUploaded ? 0.15 : 0.35))
                Group {
                    if isUploaded {
                        Image(systemName: "checkmark.circle.fill")
                            .font(.title3)
                            .foregroundStyle(.white)
                            .accessibilityLabel("Sent")
                    } else {
                        ProgressView()
                            .tint(.white)
                            .accessibilityLabel("Sending…")
                    }
                }
            }
        }
        .overlay(alignment: .topTrailing) {
            if !isUploading {
                Button(action: onRemove) {
                    Image(systemName: "xmark.circle.fill")
                        .symbolRenderingMode(.hierarchical)
                }
                .accessibilityLabel("Remove")
            }
        }
        .animation(.default, value: isUploaded)
    }
}

/// LoadingFileChip holds a picture's place while it is read from the photo
/// library, which can take a while for one kept in iCloud.
struct LoadingFileChip: View {
    var body: some View {
        RoundedRectangle(cornerRadius: 10, style: .continuous)
            .fill(Color(.tertiarySystemFill))
            .frame(width: 56, height: 56)
            .overlay { ProgressView() }
            .accessibilityLabel("Adding a picture…")
    }
}

/// CameraPicker takes a picture with the camera when isPresented turns true,
/// asking for the camera first, and saying how to allow it when it was
/// refused. SwiftUI has no camera of its own, so UIKit's is presented from
/// the top of the window: a SwiftUI cover asked for from a menu's button,
/// while the menu was still closing, did not open.
struct CameraPicker: ViewModifier {
    @Binding var isPresented: Bool
    let onPicture: (UIImage) -> Void
    @State private var isShowingNoAccess = false
    @Environment(\.openURL) private var openURL

    func body(content: Content) -> some View {
        content
            .onChange(of: isPresented) { _, isAsked in
                guard isAsked else { return }
                isPresented = false
                Task { await open() }
            }
            .alert("TeaNode cannot use the camera", isPresented: $isShowingNoAccess) {
                Button("Open Settings") {
                    if let address = URL(string: UIApplication.openSettingsURLString) { openURL(address) }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("Allow the camera for TeaNode in Settings to take a picture.")
            }
    }

    private func open() async {
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized:
            break
        case .notDetermined:
            guard await AVCaptureDevice.requestAccess(for: .video) else { return }
        default:
            isShowingNoAccess = true
            return
        }
        // The menu that asked closes first; a presentation started while it
        // closes is dropped.
        try? await Task.sleep(for: .milliseconds(350))
        CameraPresenter.present(onPicture: onPicture)
    }
}

extension View {
    /// Takes a picture with the camera when isPresented turns true.
    func cameraPicker(isPresented: Binding<Bool>, onPicture: @escaping (UIImage) -> Void) -> some View {
        modifier(CameraPicker(isPresented: isPresented, onPicture: onPicture))
    }
}

/// CameraPresenter shows UIKit's camera over whatever is on screen, and keeps
/// its delegate while it is open.
@MainActor
enum CameraPresenter {
    private static var delegate: Delegate?

    static func present(onPicture: @escaping (UIImage) -> Void) {
        guard UIImagePickerController.isSourceTypeAvailable(.camera), let top = topViewController() else { return }
        let picker = UIImagePickerController()
        picker.sourceType = .camera
        picker.modalPresentationStyle = .fullScreen
        let delegate = Delegate(onPicture: onPicture)
        Self.delegate = delegate
        picker.delegate = delegate
        top.present(picker, animated: true)
    }

    private static func topViewController() -> UIViewController? {
        let window = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .flatMap(\.windows)
            .first(where: \.isKeyWindow)
        var top = window?.rootViewController
        while let presented = top?.presentedViewController, !presented.isBeingDismissed {
            top = presented
        }
        return top
    }

    @MainActor
    final class Delegate: NSObject, @preconcurrency UIImagePickerControllerDelegate, @preconcurrency UINavigationControllerDelegate {
        let onPicture: (UIImage) -> Void

        init(onPicture: @escaping (UIImage) -> Void) {
            self.onPicture = onPicture
        }

        func imagePickerController(_ picker: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]) {
            if let picture = info[.originalImage] as? UIImage {
                onPicture(picture)
            }
            picker.dismiss(animated: true)
            CameraPresenter.delegate = nil
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
            picker.dismiss(animated: true)
            CameraPresenter.delegate = nil
        }
    }
}

/// ArtifactWebView shows a page the agent made. Its scripts run, as on the
/// dashboard, but it stays on its own page: anything it tries to load in
/// its place is refused, as the dashboard's frame rules refuse it, and a
/// link the person taps opens in Safari. A page written from a mail it read
/// could otherwise send itself, and what it shows, somewhere else.
struct ArtifactWebView: UIViewRepresentable {
    let address: URL

    func makeCoordinator() -> Coordinator { Coordinator(address: address) }

    func makeUIView(context: Context) -> WKWebView {
        let view = WKWebView(frame: .zero, configuration: WKWebViewConfiguration())
        view.navigationDelegate = context.coordinator
        view.load(URLRequest(url: address))
        return view
    }

    func updateUIView(_ view: WKWebView, context: Context) {}

    @MainActor
    final class Coordinator: NSObject, WKNavigationDelegate {
        let address: URL

        init(address: URL) {
            self.address = address
        }

        func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction) async -> WKNavigationActionPolicy {
            guard let target = navigationAction.request.url else { return .cancel }
            // The page itself, and frames inside it from the same server.
            if isSamePage(target) { return .allow }
            if navigationAction.targetFrame?.isMainFrame == false, target.host() == address.host(), target.scheme == address.scheme {
                return .allow
            }
            if navigationAction.navigationType == .linkActivated, let scheme = target.scheme?.lowercased(), scheme == "https" || scheme == "http" {
                await UIApplication.shared.open(target)
            }
            return .cancel
        }

        private func isSamePage(_ target: URL) -> Bool {
            var asked = URLComponents(url: target, resolvingAgainstBaseURL: false)
            var shown = URLComponents(url: address, resolvingAgainstBaseURL: false)
            asked?.fragment = nil
            shown?.fragment = nil
            return asked?.url == shown?.url
        }
    }
}

/// AnswerPicture is a picture an answer shows. It is read through the
/// server, which fetches only what a tool showed the agent in this
/// conversation; one that cannot be read is shown as its link.
struct AnswerPicture: View {
    let alt: String
    let address: String
    let link: String?
    @Environment(\.pictureConversationId) private var conversationId
    @Environment(\.openURL) private var openURL

    var body: some View {
        if let conversationId, address.lowercased().hasPrefix("http") {
            VStack(alignment: .leading, spacing: 4) {
                RemotePicture(path: Connection.picturePath(conversationId: conversationId, address: address), name: alt.isEmpty ? "picture" : alt, maxHeight: 240)
                if let link, let target = URL(string: link) {
                    Button(alt.isEmpty ? link : alt) { openURL(target) }
                        .font(.footnote)
                }
            }
        } else if let target = URL(string: link ?? address) {
            Button(alt.isEmpty ? address : alt) { openURL(target) }
        }
    }
}
