import LocalAuthentication
import SwiftUI
import TeaNodeKit

// Skills the person lends to somebody in a circle they share, and the calls
// made through them. Each call runs on the person's own computer, as them,
// limited to the limits they set; a call above the limits, or one the server
// cannot price, waits for the person's word, which is given on a card (here, and in
// the main conversation) and never from a notification. What goes ahead and
// what waits is decided on the server; these screens draw it and send the
// answer. docs/subsystems/skills.md ("Sharing a skill") says how it works.

// MARK: - Money in words

/// SkillSharingWords is how these screens say what a share allows and what a
/// call came to.
enum SkillSharingWords {
    /// What a share allows without asking, in a line.
    static func limits(of share: SkillShare) -> String {
        if share.isApprovalRequired { return String(localized: "asks you every time") }
        var parts: [String] = []
        if let paymentLimitMinor = share.paymentLimitMinor {
            parts.append(String(localized: "up to \(SkillShareMoney.format(minor: paymentLimitMinor, currencyCode: share.currencyCode)) a payment"))
        }
        if let monthlyLimitMinor = share.monthlyLimitMinor {
            parts.append(String(localized: "up to \(SkillShareMoney.format(minor: monthlyLimitMinor, currencyCode: share.currencyCode)) a month"))
        }
        if parts.isEmpty { return String(localized: "No limits: every call waits for approval, except one that only works on something an earlier call through this share made") }
        if share.monthlyLimitMinor != nil {
            parts.append(String(localized: "\(SkillShareMoney.format(minor: share.monthTotalMinor, currencyCode: share.currencyCode)) so far this month"))
        }
        return parts.joined(separator: " · ")
    }

    static func state(_ callState: String) -> String {
        switch callState {
        case "ran": String(localized: "Ran")
        case "failed": String(localized: "Did not complete")
        case "refused": String(localized: "Refused")
        case "waiting_for_approval": String(localized: "Waiting")
        case "declined": String(localized: "Declined")
        case "expired": String(localized: "Expired")
        default: callState
        }
    }

    static func tone(_ callState: String) -> UpkeepTone {
        switch callState {
        case "ran": .good
        case "waiting_for_approval": .warning
        default: .bad
        }
    }

    /// How a call came out, as a sentence under its card.
    static func outcome(of call: SkillShareCall) -> String {
        var words: String
        switch call.callState {
        case "ran": words = String(localized: "Approved. It ran on the owner’s computer.")
        case "failed": words = String(localized: "It was tried and did not complete.")
        case "refused": words = String(localized: "It was refused before it ran.")
        case "declined": words = String(localized: "Declined.")
        case "expired": words = String(localized: "Nobody answered in time, so it expired.")
        default: words = String(localized: "Waiting.")
        }
        if !call.answerNote.isEmpty { words += " " + String(localized: "Their note: \(call.answerNote)") }
        return words
    }
}

// MARK: - The screen

/// SkillSharingView is the shared-skills screen of the Connections tab: the
/// installed skills the person can lend, what they lent with the calls made
/// through it, and what others lent them.
struct SkillSharingView: View {
    let connection: Connection
    @State private var skills: [LendableSkill] = []
    @State private var shares: [SkillShare] = []
    @State private var people: [CalendarSharePerson] = []
    @State private var isLoaded = false
    /// Why the first read did not get through; a screen that could not read
    /// what is shared says so and offers to try again, rather than saying
    /// nothing is shared.
    @State private var loadProblemText: String?
    @State private var sharing: LendableSkill?
    @State private var stopping: SkillShare?
    @State private var isBusy = false
    @State private var doneText: String?
    @State private var problemText: String?

    private var lent: [SkillShare] { shares.filter(\.isOwnedByCaller) }
    private var given: [SkillShare] { shares.filter { !$0.isOwnedByCaller } }

    var body: some View {
        List {
            lendableSection
            if !lent.isEmpty { lentSection }
            if !given.isEmpty { givenSection }
        }
        .navigationTitle("Shared skills")
        .overlay {
            if let loadProblemText, !isLoaded {
                LoadFailedView(problemText: loadProblemText) { await load() }
            } else if !isLoaded {
                ProgressView()
            }
        }
        .disabled(isBusy)
        .refreshable { await load() }
        .task { await load() }
        .sheet(item: $sharing) { skill in
            ShareSkillSheet(connection: connection, skill: skill, people: people) { said in
                doneText = said
                Task { await load() }
            }
        }
        .toast(done: $doneText, problem: $problemText)
    }

    // MARK: Skills to lend

    private var lendableSection: some View {
        Section {
            ForEach(skills.filter(\.isLendable)) { skill in
                Button {
                    sharing = skill
                } label: {
                    VStack(alignment: .leading, spacing: 3) {
                        Text(verbatim: skill.name).foregroundStyle(.primary)
                        Text(verbatim: [skill.description, skill.tools.map(\.name).joined(separator: ", ")].filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.footnote).foregroundStyle(.secondary)
                    }
                }
                .disabled(people.isEmpty)
            }
            if isLoaded && skills.filter(\.isLendable).isEmpty {
                Text("No skill is installed to share.").foregroundStyle(.secondary)
            }
            if isLoaded && people.isEmpty {
                OpenCirclesLink(connection: connection)
            }
        } header: {
            Text("Share a skill")
        } footer: {
            if people.isEmpty && isLoaded {
                Text("You do not share a circle with anybody yet. Start or join one under Circles to share a skill.")
            } else if isLoaded {
                Text("Let someone in a circle you share run a skill of yours. It runs on your own computer, as you. Anything above the limits you set, or that cannot be priced, waits for your word.")
            }
        }
    }

    // MARK: Lent by the person

    private var lentSection: some View {
        Section {
            ForEach(lent) { share in
                NavigationLink {
                    SkillShareCallsView(connection: connection, share: share) { Task { await load() } }
                } label: {
                    ShareRow(share: share, isOwn: true)
                }
                .confirmationDialog(
                    "Stop sharing", isPresented: Binding(get: { stopping?.id == share.id }, set: { if !$0 { stopping = nil } }),
                    titleVisibility: .visible, presenting: stopping
                ) { share in
                    Button("Stop sharing", role: .destructive) {
                        Task { await stop(share) }
                    }
                } message: { share in
                    Text("\(share.shownGrantee) loses \(share.skillName) at once, and the record of what was asked through it goes with it.")
                }
                .swipeActions {
                    Button("Stop sharing", systemImage: "trash", role: .destructive) { stopping = share }
                        .tint(.red)
                }
                .contextMenu {
                    Button("Stop sharing", systemImage: "trash", role: .destructive) { stopping = share }
                        .tint(.red)
                }
            }
        } header: {
            Text("Shared by you")
        } footer: {
            Text("Open one to see what was asked through it. What waits for you is first.")
        }
    }

    // MARK: Lent to the person

    private var givenSection: some View {
        Section {
            ForEach(given) { share in
                NavigationLink {
                    SkillShareCallsView(connection: connection, share: share) {}
                } label: {
                    ShareRow(share: share, isOwn: false)
                }
            }
        } header: {
            Text("Skills shared with you")
        } footer: {
            Text("Skills other people lend your agent. Each tool runs on their computer and is limited to the limits they set.")
        }
    }

    private func stop(_ share: SkillShare) async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await connection.unshareSkill(shareId: share.id)
            doneText = String(localized: "Stopped sharing \(share.skillName) with \(share.shownGrantee).")
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "Sharing was not stopped. Check your connection and try again."))
        }
        await load()
    }

    /// The shares and the skills are what the screen is for, so a failure to
    /// read either is told; who the person shares a circle with is read for the
    /// sheet and left as it was when it cannot be.
    private func load() async {
        let connection = connection
        async let sharesRead = connection.skillShares()
        async let skillsRead = connection.lendableSkills()
        async let peopleRead = try? connection.calendarSharePeople()
        do {
            let (readShares, readSkills) = try await (sharesRead, skillsRead)
            shares = readShares
            skills = readSkills
            people = await peopleRead ?? people
            isLoaded = true
            loadProblemText = nil
            #if DEBUG
                // `-TeaNodeScreen skill-share-sheet`: the skill's sheet open, to share it.
                if DemoServer.isRequested, DemoServer.screen == "skill-share-sheet", sharing == nil {
                    sharing = skills.first(where: \.isLendable)
                }
            #endif
        } catch {
            // With the lists already on screen a failed refresh leaves them be
            // and says so.
            if isLoaded { problemText = error.loadProblemText } else { loadProblemText = error.loadProblemText }
        }
    }
}

/// ShareRow is one share in a list: the other person's picture and name, the skill
/// and its tools, what it allows, and how many calls wait.
private struct ShareRow: View {
    let share: SkillShare
    let isOwn: Bool

    var body: some View {
        HStack(spacing: 10) {
            if isOwn {
                AvatarView(name: share.shownGrantee, avatarUrl: share.granteeAvatarUrl, colorIndex: share.granteeAvatarColorIndex, size: 32)
            } else {
                AvatarView(name: share.shownOwner, avatarUrl: share.ownerAvatarUrl, colorIndex: share.ownerAvatarColorIndex, size: 32)
            }
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 8) {
                    Text(isOwn ? String(localized: "\(share.skillName) with \(share.shownGrantee)") : String(localized: "\(share.skillName) from \(share.shownOwner)"))
                    if share.waitingCallCount > 0 {
                        UpkeepTag(text: String(localized: "\(share.waitingCallCount) waiting"), tone: .warning)
                    }
                }
                Text(verbatim: share.toolNames.joined(separator: ", ")).font(.footnote).foregroundStyle(.secondary)
                Text(verbatim: SkillSharingWords.limits(of: share)).font(.footnote).foregroundStyle(.secondary)
            }
        }
    }
}

// MARK: - Sharing a skill

/// ShareSkillSheet lends tools of one skill to one person: which tools, how much
/// one payment may be, how much a month, and whether to be asked every time. A
/// tool that pays is chosen to begin with, since lending it is why anybody comes
/// here, and is not lent without a limit or the question every time.
struct ShareSkillSheet: View {
    let connection: Connection
    let skill: LendableSkill
    let people: [CalendarSharePerson]
    let onShared: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var personId = ""
    @State private var chosen: Set<String>
    @State private var paymentLimit = ""
    @State private var monthlyLimit = ""
    @State private var currency = ""
    @State private var isAskedEveryTime = false
    @State private var isSharing = false
    @State private var problemText: String?

    init(connection: Connection, skill: LendableSkill, people: [CalendarSharePerson], onShared: @escaping (String) -> Void) {
        self.connection = connection
        self.skill = skill
        self.people = people
        self.onShared = onShared
        _chosen = State(initialValue: Set(skill.tools.filter(\.isAmountDeclared).map(\.name)))
        #if DEBUG
            // `-TeaNodeScreen skill-share-sheet`: a person and limits already chosen.
            if DemoServer.isRequested, DemoServer.screen == "skill-share-sheet" {
                _personId = State(initialValue: "sam")
                _paymentLimit = State(initialValue: "100.00")
                _monthlyLimit = State(initialValue: "400.00")
                _currency = State(initialValue: "USD")
            }
        #endif
    }

    private var code: String { currency.trimmingCharacters(in: .whitespaces).lowercased() }
    private var isCurrencyValid: Bool { code.count == 3 && code.allSatisfy { $0.isASCII && $0.isLetter } }
    private var paymentMinor: Int64? { paymentLimit.trimmingCharacters(in: .whitespaces).isEmpty ? nil : SkillShareMoney.parse(paymentLimit, currencyCode: code) }
    private var monthlyMinor: Int64? { monthlyLimit.trimmingCharacters(in: .whitespaces).isEmpty ? nil : SkillShareMoney.parse(monthlyLimit, currencyCode: code) }
    private var hasLimit: Bool { !paymentLimit.trimmingCharacters(in: .whitespaces).isEmpty || !monthlyLimit.trimmingCharacters(in: .whitespaces).isEmpty }
    private var isLimitReadable: Bool {
        (paymentLimit.trimmingCharacters(in: .whitespaces).isEmpty || paymentMinor != nil)
            && (monthlyLimit.trimmingCharacters(in: .whitespaces).isEmpty || monthlyMinor != nil)
    }
    private var isPaymentUnreadable: Bool { !paymentLimit.trimmingCharacters(in: .whitespaces).isEmpty && paymentMinor == nil }
    private var isMonthlyUnreadable: Bool { !monthlyLimit.trimmingCharacters(in: .whitespaces).isEmpty && monthlyMinor == nil }
    /// Said under the amount that could not be read, in the currency's own digits.
    private var unreadableAmountText: String {
        let currencyWords = isCurrencyValid ? code.uppercased() : String(localized: "the currency below")
        return String(localized: "Type the amount in \(currencyWords), such as \(SkillShareMoney.typedExample(currencyCode: code))")
    }
    /// A currency that is typed but is not three letters, said under it.
    private var isCurrencyBad: Bool { !currency.trimmingCharacters(in: .whitespaces).isEmpty && !isCurrencyValid }
    /// One payment above what a month may come to is a limit that cannot hold; the
    /// server refuses it in the same words.
    private var isPaymentAboveMonthly: Bool {
        guard let paymentMinor, let monthlyMinor else { return false }
        return paymentMinor > monthlyMinor
    }
    private var pays: Bool { skill.tools.contains { chosen.contains($0.name) && $0.isAmountDeclared } }
    private var canShare: Bool {
        !personId.isEmpty && !chosen.isEmpty && (!pays || isAskedEveryTime || hasLimit) && (!hasLimit || isCurrencyValid || isAskedEveryTime)
            && (isLimitReadable || isAskedEveryTime) && (!isCurrencyBad || isAskedEveryTime) && (!isPaymentAboveMonthly || isAskedEveryTime)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Share with", selection: $personId) {
                        Text("Choose a person").tag("")
                        ForEach(people) { person in
                            Text(verbatim: person.pickerLabel).tag(person.userId)
                        }
                    }
                } header: {
                    Text(verbatim: skill.name).fontDesign(.monospaced)
                } footer: {
                    Text("Their agent is offered the tools you choose. Your computer must be connected for a tool to run, and you can stop sharing at any time.")
                }
                Section {
                    ForEach(skill.tools) { tool in
                        Toggle(
                            isOn: Binding(
                                get: { chosen.contains(tool.name) },
                                set: { isOn in
                                    if isOn { chosen.insert(tool.name) } else { chosen.remove(tool.name) }
                                })
                        ) {
                            VStack(alignment: .leading, spacing: 2) {
                                // In words, as the conversation names a tool; the raw name is for the
                                // server and the person who wrote the skill.
                                Text(verbatim: ToolWords.name(tool.name))
                                Group {
                                    if tool.isAmountDeclared {
                                        Text("Spends money. Calls within your limits go ahead; bigger ones wait for you.")
                                    } else if tool.isScoped {
                                        Text("Costs nothing itself. It only works on something an earlier call through this share made, such as checking or cancelling it.")
                                    } else {
                                        Text("This tool does not say what it costs, so you are asked every time")
                                    }
                                }
                                .font(.footnote).foregroundStyle(.secondary)
                            }
                        }
                    }
                } header: {
                    Text("Tools to share")
                }
                Section {
                    Toggle("Ask me every time", isOn: $isAskedEveryTime)
                    if !isAskedEveryTime {
                        // Titled, so a field that has been typed in still says
                        // what it is for.
                        LabeledContent("Per payment") {
                            TextField("100.00", text: $paymentLimit)
                                .keyboardType(.decimalPad)
                                .multilineTextAlignment(.trailing)
                        }
                        if isPaymentUnreadable {
                            Text(verbatim: unreadableAmountText).font(.footnote).foregroundStyle(.red)
                        }
                        if isPaymentAboveMonthly {
                            Text("The limit for one payment cannot be above the monthly limit.").font(.footnote).foregroundStyle(.red)
                        }
                        LabeledContent("Per month") {
                            TextField("400.00", text: $monthlyLimit)
                                .keyboardType(.decimalPad)
                                .multilineTextAlignment(.trailing)
                        }
                        if isMonthlyUnreadable {
                            Text(verbatim: unreadableAmountText).font(.footnote).foregroundStyle(.red)
                        }
                        LabeledContent("Currency") {
                            // Typed as capitals, as a currency code is written; `code` is what
                            // the server is sent.
                            TextField("Currency", text: Binding(get: { currency }, set: { currency = $0.uppercased() }), prompt: Text(verbatim: "USD"))
                                .textInputAutocapitalization(.characters)
                                .autocorrectionDisabled()
                                .multilineTextAlignment(.trailing)
                        }
                        if isCurrencyBad {
                            Text("Use the three-letter code, such as USD.").font(.footnote).foregroundStyle(.red)
                        }
                    }
                } footer: {
                    if isAskedEveryTime {
                        Text("Everything waits for your approval, whatever it costs.")
                    } else if pays && !hasLimit {
                        Text("A tool that pays is shared with a limit, or with Ask me every time.")
                    } else {
                        Text("Above these limits, or when the amount cannot be read, the call waits for you. The limits are in the currency you give. A call that fails still counts toward the month, in case the money moved.")
                    }
                }
            }
            // The skill's name is in the first section: a long one does not fit a
            // title at the largest text, and a title cannot wrap.
            .navigationTitle("Share a skill")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Share") { Task { await share() } }
                        .disabled(!canShare || isSharing)
                }
            }
            .disabled(isSharing)
            .toast(done: .constant(nil), problem: $problemText)
        }
    }

    private func share() async {
        isSharing = true
        defer { isSharing = false }
        problemText = nil
        let person = people.first { $0.userId == personId }
        do {
            try await connection.shareSkill(
                SkillShareTerms(
                    skillName: skill.name, toolNames: skill.tools.map(\.name).filter { chosen.contains($0) }, granteeUserId: personId,
                    paymentLimitMinor: isAskedEveryTime ? nil : paymentMinor, monthlyLimitMinor: isAskedEveryTime ? nil : monthlyMinor,
                    currencyCode: hasLimit && !isAskedEveryTime ? code : nil, isApprovalRequired: isAskedEveryTime))
            onShared(String(localized: "Shared \(skill.name) with \(person?.displayName ?? "")."))
            dismiss()
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "The skill was not shared. Check your connection and try again."))
        }
    }
}

// MARK: - What was asked through a share

/// SkillShareCallsView lists what was asked through one share: what waits
/// first, each on a card the owner answers, then the rest in a line each.
struct SkillShareCallsView: View {
    let connection: Connection
    let share: SkillShare
    let onChanged: () -> Void
    @State private var calls: [SkillShareCall] = []
    @State private var isLoaded = false
    @State private var loadProblemText: String?
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            if isLoaded && calls.isEmpty {
                Text("Nothing has been asked through this yet.").foregroundStyle(.secondary)
            }
            let waitingCalls = calls.filter(\.isWaiting)
            let earlierCalls = calls.filter { !$0.isWaiting }
            if !waitingCalls.isEmpty {
                Section(share.isOwnedByCaller ? LocalizedStringKey("Waiting for you") : LocalizedStringKey("Waiting")) {
                    ForEach(waitingCalls) { call in
                        SkillShareCallCard(connection: connection, call: call, isOwner: share.isOwnedByCaller) { answered in
                            replace(answered)
                            onChanged()
                        }
                        .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
                    }
                }
            }
            if !earlierCalls.isEmpty {
                Section("Earlier") {
                    ForEach(earlierCalls) { call in
                        VStack(alignment: .leading, spacing: 3) {
                            HStack(spacing: 8) {
                                Text(verbatim: call.toolName).fontDesign(.monospaced)
                                UpkeepTag(text: SkillSharingWords.state(call.callState), tone: SkillSharingWords.tone(call.callState))
                            }
                            Text(verbatim: [call.amountText, UpkeepWords.when(call.createdAt), call.requestReason].filter { !$0.isEmpty }.joined(separator: " · "))
                                .font(.footnote).foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
        .navigationTitle(Text(verbatim: share.skillName))
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if let loadProblemText, !isLoaded {
                LoadFailedView(problemText: loadProblemText) { await load() }
            } else if !isLoaded {
                ProgressView()
            }
        }
        .refreshable { await load() }
        .task { await load() }
        .toast(done: $doneText, problem: $problemText)
    }

    private func replace(_ answered: SkillShareCall) {
        calls = calls.map { $0.id == answered.id ? answered : $0 }
    }

    private func load() async {
        do {
            calls = try await connection.skillShareCalls(shareId: share.id)
            isLoaded = true
            loadProblemText = nil
        } catch {
            if isLoaded { problemText = error.loadProblemText } else { loadProblemText = error.loadProblemText }
        }
    }
}

// MARK: - The card

/// SkillShareCallCard is one call made through a share: who asked, the tool, the
/// amount, what it was given, and the reason their agent gave, quoted as their
/// words and never as an instruction. While it waits, the owner answers it
/// here: Approve runs exactly this call on their computer once, Decline stops
/// it. The person it is shared with sees only how it stands.
struct SkillShareCallCard: View {
    let connection: Connection
    let call: SkillShareCall
    let isOwner: Bool
    let onAnswered: (SkillShareCall) -> Void
    @State private var note = ""
    @State private var isAnswering = false
    @State private var problemText: String?
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// A fact's name beside its value, and above it at the largest text.
    private func factLayout(spacing: CGFloat) -> AnyLayout {
        dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: spacing / 2)) : AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: spacing))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            (dynamicTypeSize.isAccessibilitySize ? AnyLayout(VStackLayout(alignment: .leading, spacing: 6)) : AnyLayout(HStackLayout(spacing: 10))) {
                AvatarView(name: call.shownGrantee, avatarUrl: call.granteeAvatarUrl, colorIndex: call.granteeAvatarColorIndex, size: 32)
                Text(isOwner ? String(localized: "\(call.shownGrantee)’s agent asked to run \(call.toolName) with your \(call.skillName) skill") : String(localized: "Your agent asked to run \(call.toolName) with \(call.ownerName)’s \(call.skillName) skill"))
                    .font(.subheadline.weight(.semibold))
                    .fixedSize(horizontal: false, vertical: true)
            }
            VStack(alignment: .leading, spacing: 4) {
                fact(String(localized: "Tool"), call.toolName)
                if call.amountText.isEmpty {
                    fact(String(localized: "Amount"), String(localized: "No amount is stated. Only you can say whether it is fine."))
                } else {
                    fact(String(localized: "Amount"), call.amountText, isProminent: true)
                }
                ForEach(Array(call.argumentLines.enumerated()), id: \.offset) { _, line in
                    fact(line.name.capitalized, line.value)
                }
            }
            if !call.requestReason.isEmpty {
                VStack(alignment: .leading, spacing: 2) {
                    Text("\(call.shownGrantee)’s agent wrote").font(.footnote).foregroundStyle(.secondary)
                    Text(verbatim: call.requestReason)
                        .font(.callout)
                        .padding(.leading, 10)
                        .overlay(alignment: .leading) { Rectangle().fill(Color(.separator)).frame(width: 3) }
                }
            }
            if call.isWaiting && isOwner {
                TextField("A word for \(call.shownGrantee) (optional)", text: $note)
                    .textFieldStyle(.roundedBorder)
                    .disabled(isAnswering)
                // The two buttons side by side while both words fit on one
                // line each, and one above the other once they do not.
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 10) { answerButtons(isOneLine: true) }
                    VStack(spacing: 10) { answerButtons(isOneLine: false) }
                }
                .buttonBorderShape(.capsule)
                .controlSize(.large)
                .disabled(isAnswering)
                if let expiresAt = call.expiresAt, let date = ServerTime.date(expiresAt) {
                    Text("Waits until \(date.formatted(date: .abbreviated, time: .shortened)), then expires.")
                        .font(.footnote).foregroundStyle(.secondary)
                }
            } else if call.isWaiting {
                Text("Waiting for \(call.ownerName) to answer.").font(.footnote).foregroundStyle(.secondary)
            } else {
                Label(SkillSharingWords.outcome(of: call), systemImage: call.callState == "ran" ? "checkmark.circle" : "xmark.circle")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            if let problemText {
                Text(verbatim: problemText.asReasonSentence).font(.footnote).foregroundStyle(.red)
            }
        }
        .accessibilityElement(children: .contain)
    }

    @ViewBuilder
    private func answerButtons(isOneLine: Bool) -> some View {
        Button {
            Task { await answer(isApproved: true) }
        } label: {
            answerLabel("Approve and run it", isOneLine: isOneLine)
        }
        .buttonStyle(.borderedProminent)
        Button {
            Task { await answer(isApproved: false) }
        } label: {
            answerLabel("Decline", isOneLine: isOneLine).foregroundStyle(.primary)
        }
        .buttonStyle(.bordered)
    }

    /// A button's words. Side by side they keep their whole width, so that the
    /// pair no longer fits, and is stacked, before a word would be cut.
    @ViewBuilder
    private func answerLabel(_ words: LocalizedStringKey, isOneLine: Bool) -> some View {
        if isOneLine {
            Text(words).fixedSize().frame(maxWidth: .infinity)
        } else {
            Text(words).frame(maxWidth: .infinity)
        }
    }

    private func fact(_ name: String, _ value: String, isProminent: Bool = false) -> some View {
        (factLayout(spacing: 8)) {
            Text(verbatim: name).font(.footnote).foregroundStyle(.secondary)
                .frame(minWidth: dynamicTypeSize.isAccessibilitySize ? nil : 70, alignment: .leading)
            // Whole, and wrapped: the owner approves exactly this call.
            Text(verbatim: value).font(isProminent ? .headline : .callout).fixedSize(horizontal: false, vertical: true)
            if !dynamicTypeSize.isAccessibilitySize { Spacer(minLength: 0) }
        }
    }

    private func answer(isApproved: Bool) async {
        isAnswering = true
        defer { isAnswering = false }
        problemText = nil
        // Approving spends money, so it takes the phone's owner and not just
        // whoever holds it unlocked; declining spends nothing and needs no one.
        if isApproved {
            guard await DeviceOwner.confirm(approvalPrompt: String(localized: "Approve and run it")) else { return }
        }
        do {
            onAnswered(try await connection.answerSkillShareCall(callId: call.id, isApproved: isApproved, answerNote: note))
        } catch {
            problemText = error.refusalReason(
                orSay: isApproved
                    ? String(localized: "Nothing was approved. Check your connection and try again.")
                    : String(localized: "Nothing was declined. Check your connection and try again."))
        }
    }
}

/// DeviceOwner asks the phone's owner to confirm, with Face ID, Touch ID or
/// the passcode. A phone with none of them set cannot tell its owner from
/// anyone else, so confirming is not refused there: the server's approval is
/// still the person's own, signed in.
enum DeviceOwner {
    static func confirm(approvalPrompt: String) async -> Bool {
        let context = LAContext()
        var problem: NSError?
        guard context.canEvaluatePolicy(.deviceOwnerAuthentication, error: &problem) else { return true }
        do {
            return try await context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: approvalPrompt)
        } catch {
            return false
        }
    }
}

/// SkillShareCallWaitingCard is the skill_share_call_waiting note of the main conversation as a
/// card: the note names the call, the call is read for how it stands now (it may
/// have been answered on the dashboard since), and the card is drawn from that.
/// It is read again every few seconds while the call waits, so an answer given
/// elsewhere shows here. Only the server answering that the call is not there
/// says it is gone (the share was taken back, or the two no longer share a
/// circle); a read that did not get through says so, offers to try again, and
/// keeps trying.
struct SkillShareCallWaitingCard: View {
    let connection: Connection
    let noteDetail: String
    @State private var reading: SkillShareCallReading?

    var body: some View {
        CardSurface {
            switch reading {
            case .found(let call)?:
                SkillShareCallCard(connection: connection, call: call, isOwner: true) { answered in reading = .found(answered) }
            case .gone?:
                Label("This call is gone. The share was taken back, or you no longer share a circle.", systemImage: "questionmark.circle")
                    .font(.footnote).foregroundStyle(.secondary)
            case .failed?:
                VStack(alignment: .leading, spacing: 8) {
                    Label("This call could not be loaded. It will keep trying.", systemImage: "wifi.exclamationmark")
                        .font(.footnote).foregroundStyle(.secondary)
                    Button("Try again") { Task { await read() } }
                        .buttonStyle(.bordered)
                        .buttonBorderShape(.capsule)
                }
            case nil:
                ProgressView()
            }
        }
        .task(id: noteDetail) { await keepReading() }
    }

    /// Reads the call, and again every ten seconds while it waits or could not
    /// be read.
    private func keepReading() async {
        repeat {
            await read()
            if Task.isCancelled { return }
            switch reading {
            case .found(let call)? where call.isWaiting: break
            case .failed?: break
            default: return
            }
            do { try await Task.sleep(for: .seconds(10)) } catch { return }
        } while !Task.isCancelled
    }

    private func read() async {
        guard let note = SkillShareNote.parsing(noteDetail) else {
            reading = .gone
            return
        }
        let latest = await connection.readSkillShareCall(shareId: note.skillShareId, callId: note.callId)
        if Task.isCancelled { return }
        // A read that fails after the card was drawn leaves the card as it was.
        if case .failed = latest, case .some(.found) = reading { return }
        reading = latest
    }
}
