import SwiftUI
import TeaNodeKit

/// CodingAgentsList is the Agents part of a coding session: the session on
/// top and the agents it started for its current request under it, each
/// under the agent that started it. The question a session waits on is
/// answered in the Turns part, which the session's footer says.
struct CodingAgentsList: View {
    let session: HerdrSession
    /// Nil until the agents were read, or when they could not be.
    let read: HerdrSubagents?
    let isRead: Bool
    @State private var isSessionExpanded = true

    private var roots: [HerdrSubagentNode] { read?.tree ?? [] }

    var body: some View {
        List {
            sessionSection
            if let read, read.subagents.isEmpty, isRead {
                emptySection
            } else if read == nil, isRead {
                failedSection
            }
        }
        // The switch floats over the top right of the list.
        .contentMargins(.top, 56, for: .scrollContent)
        .overlay {
            if !isRead {
                ProgressView()
            }
        }
    }

    private var sessionSection: some View {
        Section {
            if roots.isEmpty {
                CodingSessionAgentRow(session: session, modelName: read?.sessionModelName ?? "")
            } else {
                DisclosureGroup(isExpanded: $isSessionExpanded) {
                    ForEach(roots) { node in
                        CodingAgentNodeView(node: node)
                    }
                } label: {
                    CodingSessionAgentRow(session: session, modelName: read?.sessionModelName ?? "")
                }
            }
        } header: {
            if let read, !read.subagents.isEmpty {
                Text(verbatim: Self.headingText(read: read))
            }
        } footer: {
            if session.herdrSessionState == "asking" {
                Text("The session waits for you. Its question is in Turns.")
            } else if read?.isTruncated == true {
                Text("Only the newest agents are shown.")
            }
        }
    }

    private var emptySection: some View {
        Section {
            ContentUnavailableView(
                "No agents yet", systemImage: "point.3.connected.trianglepath.dotted",
                description: Text("This session has not started any agents for this request."))
        }
        .listRowBackground(Color.clear)
        .listRowSeparator(.hidden)
    }

    private var failedSection: some View {
        Section {
            ContentUnavailableView(
                "The agents could not be read", systemImage: "exclamationmark.triangle",
                description: Text("The TeaNode program on the computer may be too old to show agents. Update it and restart it."))
        }
        .listRowBackground(Color.clear)
        .listRowSeparator(.hidden)
    }

    /// "8 agents · 2 running"; the running count is left out when none run.
    static func headingText(read: HerdrSubagents) -> String {
        let agentsText = String(localized: "\(read.subagents.count) agents")
        let runningCount = read.runningCount
        if runningCount == 0 { return agentsText }
        let runningText = String(localized: "\(runningCount) running")
        return "\(agentsText) · \(runningText)"
    }
}

/// CodingAgentNodeView is an agent and, under it, the agents it started:
/// a disclosure group open at first when it has any, a plain row when not.
struct CodingAgentNodeView: View {
    let node: HerdrSubagentNode
    @State private var isExpanded = true

    var body: some View {
        if node.children.isEmpty {
            CodingAgentRow(subagent: node.subagent)
        } else {
            DisclosureGroup(isExpanded: $isExpanded) {
                ForEach(node.children) { child in
                    CodingAgentNodeView(node: child)
                }
            } label: {
                CodingAgentRow(subagent: node.subagent)
            }
        }
    }
}

/// CodingSessionAgentRow is the session itself at the top of the tree: what
/// it is about, which coding agent and model it runs, and whether it works,
/// is idle or needs the person.
struct CodingSessionAgentRow: View {
    let session: HerdrSession
    let modelName: String

    private var detailText: String {
        [session.codingAgentName, modelName].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: session.rowTitle)
                .font(.headline)
                .lineLimit(2)
            Text(verbatim: detailText)
                .font(.subheadline)
                .foregroundStyle(.secondary)
            stateLabel
                .font(.footnote.weight(.semibold))
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var stateLabel: some View {
        if session.herdrSessionState == "asking" {
            Label("Needs you", systemImage: "questionmark.circle.fill")
                .labelStyle(.tag)
                .foregroundStyle(.orange)
        } else if session.isWorking {
            Label {
                Text("Working")
            } icon: {
                ProgressView().controlSize(.small)
            }
            .labelStyle(.tag)
            .foregroundStyle(Color.accentColor)
        } else {
            Label("Idle", systemImage: "moon.zzz")
                .labelStyle(.tag)
                .foregroundStyle(.secondary)
        }
    }
}

/// CodingAgentRow is one agent: its kind and model, its task, whether it
/// runs, is done or was stopped and for how long, and the tools it used.
struct CodingAgentRow: View {
    let subagent: HerdrSubagent

    private var detailText: String {
        [subagent.subagentName ?? "", subagent.modelName].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: subagent.subagentType)
                .font(.headline)
                .lineLimit(2)
            if !detailText.isEmpty {
                Text(verbatim: detailText)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            if !subagent.taskText.isEmpty {
                Text(verbatim: subagent.taskText)
                    .font(.subheadline)
                    .lineLimit(2)
            }
            HStack(spacing: 6) {
                stateLabel
                    .font(.footnote.weight(.semibold))
                if let durationText = Self.durationText(subagent.duration(now: Date())) {
                    Text(verbatim: durationText)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
            if !subagent.toolNames.isEmpty {
                Text("Used \(subagent.toolNames.joined(separator: ", "))")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var stateLabel: some View {
        if subagent.isRunning {
            Label {
                Text("Running")
            } icon: {
                ProgressView().controlSize(.small)
            }
            .labelStyle(.tag)
            .foregroundStyle(Color.accentColor)
        } else if subagent.isStopped {
            Label("Stopped", systemImage: "stop.circle")
                .labelStyle(.tag)
                .foregroundStyle(.secondary)
        } else {
            Label("Done", systemImage: "checkmark.circle.fill")
                .labelStyle(.tag)
                .foregroundStyle(.green)
        }
    }

    /// "2m 14s": the two largest units of the time it ran, nil when it has no
    /// start time.
    static func durationText(_ seconds: TimeInterval?) -> String? {
        guard let seconds else { return nil }
        return Duration.seconds(Int(seconds)).formatted(.units(allowed: [.hours, .minutes, .seconds], width: .narrow, maximumUnitCount: 2))
    }
}
