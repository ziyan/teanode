import SafariServices
import SwiftUI
import TeaNodeKit
import UIKit

/// WebLink is a web page the app was asked to open in its own browser.
struct WebLink: Identifiable, Equatable {
    let address: URL
    let id = UUID()
}

/// InAppBrowser shows a web page in Safari's own view, which slides up from
/// the bottom over whatever is on screen and goes back down on Done. It is
/// presented from the top of the window rather than from a view, as the
/// lightbox is: a link in a sheet (a person's card, a long message read in
/// full) has no root view free to present from while the sheet is up.
@MainActor
enum InAppBrowser {
    /// Opens a link the way LinkOpening says: a web page here, anything else
    /// with the system.
    static func open(_ address: URL) {
        switch LinkOpening.way(for: address) {
        case .inApp: present(address)
        case .system: UIApplication.shared.open(address)
        }
    }

    static func present(_ address: URL) {
        guard let top = TopViewController.find() else {
            UIApplication.shared.open(address)
            return
        }
        let configuration = SFSafariViewController.Configuration()
        configuration.entersReaderIfAvailable = false
        let browser = SFSafariViewController(url: address, configuration: configuration)
        browser.dismissButtonStyle = .done
        browser.preferredControlTintColor = UIColor(named: "AccentColor")
        browser.modalPresentationStyle = .pageSheet
        top.present(browser, animated: true)
    }
}

/// InAppBrowserPresenter opens the page the app model was asked to show. Every
/// link in the app asks through the model (the environment's openURL, set once
/// at the root), so there is one place that decides and one that presents.
struct InAppBrowserPresenter: ViewModifier {
    @Environment(AppModel.self) private var app

    func body(content: Content) -> some View {
        content
            .onChange(of: app.openedWebLink) { _, link in
                guard let link else { return }
                app.openedWebLink = nil
                InAppBrowser.present(link.address)
            }
    }
}

/// LinkPreviewBelow puts the card of the first link of a message under it,
/// when the page behind it has one. The conversation asks for the cards of its
/// messages ahead of drawing them, and one already answered is drawn from the
/// message's first frame: a card that came a moment later made the message
/// taller under the person's finger, and the list jumped. One not answered yet
/// is asked for when the message appears, and nothing is drawn until it is
/// there, so the message does not make room for a card that does not come.
struct LinkPreviewBelow: ViewModifier {
    /// The conversation the message is in, nil until it has been read: the
    /// server previews only a link written there by somebody other than the
    /// agent.
    let conversationId: String?
    let text: String
    let isMarkdown: Bool
    let alignment: HorizontalAlignment
    /// False for a message still being written or sent: its link may be half
    /// of one.
    let isReady: Bool
    /// Optional, so a message drawn where the app's environment does not reach
    /// has no card instead of stopping the app.
    @Environment(AppModel.self) private var app: AppModel?
    @State private var preview: LinkPreview?

    /// The first link as the message writes it, not cleaned.
    private var link: String? {
        conversationId != nil && isReady ? LinkText.firstLink(in: text, isMarkdown: isMarkdown) : nil
    }

    /// The card to draw: the one this view was answered, or the one already
    /// answered for its link when the view was built again.
    private var shown: LinkPreview? {
        if let preview { return preview }
        guard let link, let conversationId else { return nil }
        return app?.answeredLinkPreview(for: link, inConversation: conversationId)
    }

    func body(content: Content) -> some View {
        VStack(alignment: alignment, spacing: 6) {
            content
            if let shown, shown.hasCard {
                LinkPreviewCard(preview: shown)
            }
        }
        .task(id: link) {
            guard let link, let conversationId else {
                preview = nil
                return
            }
            preview = await app?.linkPreview(for: link, inConversation: conversationId)
        }
    }
}

extension View {
    /// The card of the first link in `text`, a message of the conversation, under this view.
    func linkPreviewBelow(
        conversationId: String?, text: String, isMarkdown: Bool, alignment: HorizontalAlignment, isReady: Bool = true
    ) -> some View {
        modifier(LinkPreviewBelow(conversationId: conversationId, text: text, isMarkdown: isMarkdown, alignment: alignment, isReady: isReady))
    }
}

/// LinkPreviewCard is the page behind a link: its picture, its site, its title
/// and a line or two about it. A card that only shows something has no
/// buttons: a tap anywhere on it opens the link, and copying it is on the long
/// press and in the accessibility actions.
struct LinkPreviewCard: View {
    let preview: LinkPreview
    @Environment(\.openURL) private var openURL

    var body: some View {
        if let address = URL(string: preview.cleanedUrl) {
            Button {
                openURL(address)
            } label: {
                CardContent(preview: preview)
            }
            .buttonStyle(.plain)
            .contextMenu {
                Button("Copy link", systemImage: "doc.on.doc") { UIPasteboard.general.string = preview.cleanedUrl }
            }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isLink)
            .accessibilityHint(Text("Opens the link"))
            .accessibilityAction(named: Text("Copy link")) { UIPasteboard.general.string = preview.cleanedUrl }
        }
    }

    /// The card itself, on the surface every card of the app shares: a
    /// secondary background, an 18 point corner and a hairline edge.
    private struct CardContent: View {
        let preview: LinkPreview

        var body: some View {
            VStack(alignment: .leading, spacing: 0) {
                if let imageUrl = preview.imageUrl {
                    LinkPreviewImage(path: imageUrl, widthPixels: preview.imageWidthPixels, heightPixels: preview.imageHeightPixels)
                }
                texts
            }
            .frame(maxWidth: 320, alignment: .leading)
            .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 18, style: .continuous))
            .clipShape(.rect(cornerRadius: 18, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(Color(.separator).opacity(0.5), lineWidth: 0.5)
            }
            .contentShape(.rect(cornerRadius: 18, style: .continuous))
        }

        private var texts: some View {
            VStack(alignment: .leading, spacing: 2) {
                if !preview.siteName.isEmpty {
                    Text(verbatim: preview.siteName)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                if !preview.title.isEmpty {
                    Text(verbatim: preview.title)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.primary)
                        .lineLimit(2)
                }
                if !preview.description.isEmpty {
                    Text(verbatim: preview.description)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
            }
            .multilineTextAlignment(.leading)
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

/// LinkPreviewImage is the page's picture as the server kept it, read with the
/// app's sign-in, so the page's own site is never asked. It keeps the picture's
/// shape while it loads, and is left out when it cannot be read.
private struct LinkPreviewImage: View {
    let path: String
    let widthPixels: Int?
    let heightPixels: Int?
    @Environment(AppModel.self) private var app: AppModel?
    @State private var picture: UIImage?
    @State private var isMissing = false

    /// The picture's shape: its own when the server said, else a wide card
    /// picture.
    private var aspectRatio: CGFloat {
        guard let widthPixels, let heightPixels, widthPixels > 0, heightPixels > 0 else { return 1.91 }
        return CGFloat(widthPixels) / CGFloat(heightPixels)
    }

    var body: some View {
        if !isMissing {
            Color(.tertiarySystemFill)
                .aspectRatio(aspectRatio, contentMode: .fit)
                .frame(maxHeight: 200)
                .overlay {
                    if let picture {
                        Image(uiImage: picture)
                            .resizable()
                            .scaledToFill()
                    }
                }
                .clipped()
                .accessibilityHidden(true)
                .task(id: path) {
                    picture = await app?.picture(path)
                    isMissing = picture == nil
                }
        }
    }
}
