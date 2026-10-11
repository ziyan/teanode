import SwiftUI
import TeaNodeKit

/// IdeasScreen is the dashboard's Ideas tab: ideas found for the person in
/// their own mail and memory, the catalog's ideas by category, and what
/// became of the rest. Trying one starts a conversation of its own and sends
/// the request it opens with; nothing starts before that.
struct IdeasScreen: View {
    let connection: Connection
    @Environment(AppModel.self) private var app
    @State private var list: IdeaList?
    @State private var isBusy = false
    @State private var doneText: String?
    @State private var problemText: String?

    private var language: String { DisplayLanguage.languageCode }

    var body: some View {
        NavigationStack {
            List {
                if let list {
                    Section {
                        if list.forYou.isEmpty {
                            Text("Nothing yet. Your agent looks for ideas in your own mail and memory each night.")
                                .foregroundStyle(.secondary)
                        }
                        ForEach(list.forYou) { idea in row(idea) }
                    } header: {
                        Text("For you")
                    } footer: {
                        Text("Found in your own mail, memory and calendar. Nothing starts until you send the request it opens with.")
                    }
                    ForEach(list.byCategory, id: \.category) { section in
                        Section(IdeaWords.category(section.category)) {
                            ForEach(section.ideas) { idea in row(idea) }
                        }
                    }
                    if !list.history.isEmpty {
                        Section("History") {
                            ForEach(list.history) { idea in historyRow(idea) }
                        }
                    }
                }
            }
            .navigationTitle("Ideas")
            .toolbar { MenuButton() }
            .withoutSwipeBack()
            .overlay { if list == nil { ProgressView() } }
            .refreshable { await load() }
            .task { await load() }
            .disabled(isBusy)
        }
        .toast(done: $doneText, problem: $problemText)
    }

    private func row(_ idea: IdeaEntry) -> some View {
        Button {
            Task { await start(idea) }
        } label: {
            // Colors named outright: in a button, primary and secondary are
            // shades of the tint, which turned every word green.
            HStack(alignment: .top, spacing: 12) {
                Text(idea.emoji ?? "💡").font(.title2)
                VStack(alignment: .leading, spacing: 3) {
                    Text(idea.headline).foregroundStyle(Color(.label))
                    if let body = idea.body, !body.isEmpty {
                        Text(body).font(.subheadline).foregroundStyle(Color(.secondaryLabel)).lineLimit(3)
                    }
                    if let reason = idea.suggestionReason, !reason.isEmpty {
                        Text(reason).font(.caption).foregroundStyle(Color(.tertiaryLabel)).lineLimit(2)
                    }
                }
                Spacer(minLength: 0)
                Image(systemName: "arrow.up.circle").foregroundStyle(.tint).accessibilityLabel("Try it")
            }
            .padding(.vertical, 2)
        }
        .swipeActions {
            Button("Dismiss", systemImage: "xmark") {
                Task { await setStatus(idea, "dismissed", saying: String(localized: "Dismissed. It is under History if you want it back.")) }
            }
            .tint(.gray)
        }
    }

    private func historyRow(_ idea: IdeaEntry) -> some View {
        HStack(spacing: 12) {
            Text(idea.emoji ?? "💡")
            VStack(alignment: .leading, spacing: 2) {
                Text(idea.headline).lineLimit(2)
                Text(IdeaWords.status(idea.ideaStatus)).font(.caption).foregroundStyle(.secondary)
            }
        }
        .swipeActions {
            if idea.ideaStatus == "started" {
                Button("Done", systemImage: "checkmark") {
                    Task { await setStatus(idea, "done", saying: String(localized: "Marked done.")) }
                }
                .tint(.green)
            }
            if idea.canRestore {
                Button("Restore", systemImage: "arrow.uturn.backward") {
                    Task { await setStatus(idea, "open", saying: String(localized: "It is back among the open ideas.")) }
                }
                .tint(.indigo)
            }
        }
    }

    private func load() async {
        do {
            list = try await connection.allIdeas(language: language)
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Starts the idea in a conversation of its own and opens it, which
    /// sends the request the idea opens with.
    private func start(_ idea: IdeaEntry) async {
        isBusy = true
        defer { isBusy = false }
        do {
            let started = try await connection.startIdea(idea.id, in: nil, language: language)
            guard let conversationId = started.conversationId else { return }
            app.openingRequest = OpeningRequest(conversationId: conversationId, text: started.openingRequest)
            app.openedConversationId = conversationId
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func setStatus(_ idea: IdeaEntry, _ status: String, saying said: String) async {
        do {
            try await connection.setIdeaStatus(idea.id, to: status)
            doneText = said
            await load()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// OpeningRequest is the first message a conversation is opened to send.
struct OpeningRequest: Equatable {
    let conversationId: String
    let text: String
}

/// IdeaWords are an idea's category and status, as the dashboard says them.
enum IdeaWords {
    static func category(_ ideaCategory: String) -> String {
        switch ideaCategory {
        case "assistant": String(localized: "Getting more from your agent")
        case "family": String(localized: "Family")
        case "fun": String(localized: "Free time")
        case "health": String(localized: "Health")
        case "home": String(localized: "Home")
        case "mail": String(localized: "Mail")
        case "money": String(localized: "Money")
        case "paperwork": String(localized: "Paperwork")
        case "shopping": String(localized: "Shopping")
        case "travel": String(localized: "Travel")
        case "work": String(localized: "Work")
        default: ideaCategory
        }
    }

    static func status(_ ideaStatus: String) -> String {
        switch ideaStatus {
        case "done": String(localized: "Done")
        case "dismissed": String(localized: "Dismissed")
        case "started": String(localized: "Started")
        case "expired": String(localized: "Out of date")
        default: ideaStatus
        }
    }
}
