import Observation
import StoreKit
import SwiftUI
import TeaNodeKit

/// AppUpdateModel knows whether a newer app can be installed: it works out
/// where this copy came from, asks the server what is newest there, and keeps
/// the person's "not now" for that update, so that the next one is offered
/// again. A copy Xcode installed has no channel and is offered nothing.
@MainActor
@Observable
final class AppUpdateModel {
    /// The newer app, whether or not the person put it off: Settings keeps
    /// offering it after the card has gone.
    private(set) var update: AppUpdate?
    private var dismissalKey = UserDefaults.standard.string(forKey: AppUpdateModel.dismissalDefaultsKey)
    private var askedAt: Date?

    private static let dismissalDefaultsKey = "dismissedAppUpdate"

    /// How often it asks. The app comes to the front many times a day, and
    /// builds reach testers a few times a day at most.
    private static let askEvery: TimeInterval = 60 * 60

    /// The update to bring up, when the person has not put it off.
    var updateToOffer: AppUpdate? {
        guard let update, update.dismissalKey != dismissalKey else { return nil }
        return update
    }

    func refresh(connection: Connection) async {
        if let askedAt, Date().timeIntervalSince(askedAt) < Self.askEvery { return }
        guard let channel = await Self.installedChannel() else { return }
        askedAt = Date()
        // Nothing to say about a question that failed: the next one may not.
        guard let found = try? await connection.appUpdate(channel: channel) else {
            update = nil
            return
        }
        let info = Bundle.main.infoDictionary
        let version = info?["CFBundleShortVersionString"] as? String ?? ""
        let build = info?["CFBundleVersion"] as? String ?? ""
        update = found.isNewer(thanVersion: version, build: build) ? found : nil
    }

    func dismiss() {
        guard let update else { return }
        dismissalKey = update.dismissalKey
        UserDefaults.standard.set(update.dismissalKey, forKey: Self.dismissalDefaultsKey)
    }

    /// Where this copy was installed from, as the App Store signed it.
    private static func installedChannel() async -> AppUpdateChannel? {
        #if DEBUG
            if DemoServer.isRequested { return .testFlight }
        #endif
        guard let transaction = try? await AppTransaction.shared else { return nil }
        let environment = transaction.unsafePayloadValue.environment
        if environment == .production { return .appStore }
        if environment == .sandbox { return .testFlight }
        return nil
    }
}

extension AppUpdate {
    /// What the button that installs it says.
    var installWords: LocalizedStringKey {
        updateChannel == AppUpdateChannel.appStore.rawValue ? "Open the App Store" : "Open TestFlight"
    }
}

/// AppUpdateCard offers an update at the top of the chats, on the cards'
/// surface, until the person installs it or says not now.
struct AppUpdateCard: View {
    let update: AppUpdate
    @Environment(AppModel.self) private var app
    @Environment(\.openURL) private var openURL

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // Not a Label: inside a list it takes the rows' wide icon column.
            HStack(spacing: 6) {
                Image(systemName: "arrow.down.circle")
                    .accessibilityHidden(true)
                Text("Update available")
            }
            .font(.footnote)
            .foregroundStyle(.secondary)
            Text("TeaNode \(update.versionText) is ready to install.")
                .font(.body)
                .fixedSize(horizontal: false, vertical: true)
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 10) { buttons }
                VStack(spacing: 10) { buttons }
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 18, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(Color(.separator), lineWidth: 0.5)
        }
    }

    @ViewBuilder
    private var buttons: some View {
        Button {
            if let address = URL(string: update.installUrl) { openURL(address) }
        } label: {
            Text(update.installWords).frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        Button {
            withAnimation { app.appUpdates.dismiss() }
        } label: {
            Text("Not now").frame(maxWidth: .infinity)
        }
        .buttonStyle(.bordered)
    }
}

/// FeedAppUpdateCard offers an update at the top of the feed, above every
/// card, until the person installs it or says not now. It is drawn apart from
/// the agent's cards: the app's own logo, a wash of the accent and a border in
/// it, and one large button, because it is the app speaking, not the agent.
struct FeedAppUpdateCard: View {
    let update: AppUpdate
    @Environment(AppModel.self) private var app
    @Environment(\.openURL) private var openURL
    @ScaledMetric(relativeTo: .title3) private var logoSide: CGFloat = 56

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .center, spacing: 14) {
                Image("Logo")
                    .resizable()
                    .scaledToFit()
                    .frame(width: logoSide, height: logoSide)
                    .clipShape(.rect(cornerRadius: logoSide * 0.225, style: .continuous))
                    .shadow(color: Color.accentColor.opacity(0.35), radius: 8, y: 3)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text("New version")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(Color.accentColor)
                    Text("TeaNode \(update.versionText)")
                        .font(.title3.weight(.bold))
                        .foregroundStyle(Color.primary)
                    Text(update.whereWords)
                        .font(.subheadline)
                        .foregroundStyle(Color.secondary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            // Room for the close button in the corner; the install button
            // below runs the card's width.
            .padding(.trailing, 24)
            Button {
                if let address = URL(string: update.installUrl) { openURL(address) }
            } label: {
                Label(update.installWords, systemImage: "arrow.down.circle.fill")
                    .font(.body.weight(.semibold))
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 4)
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.capsule)
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            ZStack {
                FeedCard.surface
                LinearGradient(
                    colors: [Color.accentColor.opacity(0.22), Color.accentColor.opacity(0.04)],
                    startPoint: .topLeading, endPoint: .bottomTrailing)
            }
        }
        .clipShape(.rect(cornerRadius: FeedCard.cornerRadius, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: FeedCard.cornerRadius, style: .continuous)
                .strokeBorder(Color.accentColor.opacity(0.5), lineWidth: 1)
        }
        .overlay(alignment: .topTrailing) {
            Button {
                withAnimation { app.appUpdates.dismiss() }
            } label: {
                Label("Not now", systemImage: "xmark")
                    .labelStyle(.iconOnly)
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.secondary)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .padding(4)
        }
    }
}

extension AppUpdate {
    /// Where the update waits, under its version on the feed's card.
    var whereWords: LocalizedStringKey {
        updateChannel == AppUpdateChannel.appStore.rawValue ? "Ready on the App Store" : "Ready to install from TestFlight"
    }
}
