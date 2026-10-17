import LocalAuthentication
import SwiftUI
import TeaNodeKit

// Skills the person lends to somebody in a circle they share, and the requests
// made through them. Each use runs on the person's own computer, as them, held
// to the limits they set; a use above the limits, or one the server cannot
// price, waits for the person's word, which is given on a card (here, and in
// the main conversation) and never from a notification. What goes ahead and
// what waits is decided on the server; these screens draw it and send the
// answer. docs/subsystems/skills.md ("Sharing a skill") says how it works.

// MARK: - Money in words

/// SkillSharingWords is how these screens say what a share allows and what a
/// request came to.
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
        if parts.isEmpty { return String(localized: "no limit; anything it cannot price asks you") }
        if share.monthlyLimitMinor != nil {
            parts.append(String(localized: "\(SkillShareMoney.format(minor: share.monthTotalMinor, currencyCode: share.currencyCode)) so far this month"))
        }
        return parts.joined(separator: " · ")
    }

    static func state(_ callState: String) -> String {
        switch callState {
        case "ran": String(localized: "Ran")
        case "failed": String(localized: "Did not complete")
        case "refused": String(localized: "Turned back")
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

    /// How a request came out, as a sentence under its card.
    static func outcome(of call: SkillShareCall) -> String {
        var words: String
        switch call.callState {
        case "ran": words = String(localized: "Approved. It ran on the owner’s computer.")
        case "failed": words = String(localized: "It was tried and did not complete.")
        case "refused": words = String(localized: "It was turned back before it ran.")
        case "declined": words = String(localized: "Declined.")
        case "expired": words = String(localized: "Nobody answered in time, so it expired.")
        default: words = String(localized: "Waiting.")
        }
        if !call.decisionNote.isEmpty { words += " " + String(localized: "Their note: \(call.decisionNote)") }
        return words
    }
}

// MARK: - The screen

/// SkillSharingView is the shared-skills screen of the Connections tab: the
/// installed skills the person can lend, what they lent with the requests made
/// through it, and what others lent them.
struct SkillSharingView: View {
    let connection: Connection
    @State private var skills: [LendableSkill] = []
    @State private var shares: [SkillShare] = []
    @State private var people: [CalendarSharePerson] = []
    @State private var isLoaded = false
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
        .overlay { if !isLoaded { ProgressView() } }
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
        } header: {
            Text("Share a skill")
        } footer: {
            if people.isEmpty && isLoaded {
                Text("You do not share a circle with anybody yet. Start or join one under Circles to share a skill.")
            } else {
                Text("Let someone in a circle you share use a skill of yours. Each use runs on your own computer, as you. A use above the limits you set, or one that cannot be priced, waits for your word.")
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
                }
                .contextMenu {
                    Button("Stop sharing", systemImage: "trash", role: .destructive) { stopping = share }
                }
            }
        } header: {
            Text("Shared by you")
        } footer: {
            Text("Open one to see what was asked through it. Requests waiting for you are first.")
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
            Text("Skills other people lend your agent. Each use runs on their computer and is held to the limits they set.")
        }
    }

    private func stop(_ share: SkillShare) async {
        isBusy = true
        defer { isBusy = false }
        do {
            try await connection.unshareSkill(shareId: share.id)
            doneText = String(localized: "Stopped sharing \(share.skillName) with \(share.shownGrantee).")
        } catch {
            problemText = error.localizedDescription
        }
        await load()
    }

    private func load() async {
        let connection = connection
        async let sharesRead = try? connection.skillShares()
        async let skillsRead = try? connection.lendableSkills()
        async let peopleRead = try? connection.calendarSharePeople()
        shares = await sharesRead ?? shares
        skills = await skillsRead ?? skills
        people = await peopleRead ?? people
        isLoaded = true
    }
}

/// ShareRow is one share in a list: the other person's face and name, the skill
/// and its tools, what it allows, and how many requests wait.
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
    private var pays: Bool { skill.tools.contains { chosen.contains($0.name) && $0.isAmountDeclared } }
    private var canShare: Bool {
        !personId.isEmpty && !chosen.isEmpty && (!pays || isAskedEveryTime || hasLimit) && (!hasLimit || isCurrencyValid || isAskedEveryTime)
            && (isLimitReadable || isAskedEveryTime)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Share with", selection: $personId) {
                        Text("Choose a person").tag("")
                        ForEach(people) { person in
                            Text(verbatim: person.displayName).tag(person.userId)
                        }
                    }
                } footer: {
                    Text("Their agent is offered the tools you choose. Your computer must be connected for a use to run, and you can stop sharing at any time.")
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
                                Text(verbatim: tool.name).fontDesign(.monospaced)
                                Group {
                                    if tool.isAmountDeclared {
                                        Text("pays: needs a limit or your approval")
                                    } else if tool.isScoped {
                                        Text("only for requests made through this share")
                                    } else {
                                        Text("does not say what it costs, so every use asks you")
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
                        TextField("Most for one payment, such as 100.00", text: $paymentLimit)
                            .keyboardType(.decimalPad)
                        TextField("Most in a calendar month, such as 400.00", text: $monthlyLimit)
                            .keyboardType(.decimalPad)
                        TextField("Currency, such as usd", text: $currency)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                    }
                } footer: {
                    if isAskedEveryTime {
                        Text("Every use waits for your approval, whatever it costs.")
                    } else if pays && !hasLimit {
                        Text("A tool that pays is shared with a limit, or with Ask me every time.")
                    } else {
                        Text("Above these limits, or when the amount cannot be read, a use waits for you. The limits are in the currency you give.")
                    }
                }
                if let problemText {
                    Section { Text(verbatim: problemText).foregroundStyle(.red) }
                }
            }
            .navigationTitle("Share \(skill.name)")
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
            problemText = error.localizedDescription
        }
    }
}

// MARK: - The requests made through a share

/// SkillShareCallsView lists the requests made through one share: the ones that
/// wait first, each on a card the owner answers, then the rest in a line each.
struct SkillShareCallsView: View {
    let connection: Connection
    let share: SkillShare
    let onChanged: () -> Void
    @State private var calls: [SkillShareCall] = []
    @State private var isLoaded = false
    @State private var doneText: String?
    @State private var problemText: String?

    var body: some View {
        List {
            if isLoaded && calls.isEmpty {
                Text("Nothing has been asked through this yet.").foregroundStyle(.secondary)
            }
            ForEach(calls) { call in
                if call.isWaiting {
                    PaymentCard(connection: connection, call: call, isOwner: share.isOwnedByCaller) { answered in
                        replace(answered)
                        onChanged()
                    }
                    .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
                } else {
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
        .navigationTitle(Text(verbatim: share.skillName))
        .navigationBarTitleDisplayMode(.inline)
        .overlay { if !isLoaded { ProgressView() } }
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
        } catch {
            if !isLoaded { problemText = error.localizedDescription }
        }
        isLoaded = true
    }
}

// MARK: - The card

/// PaymentCard is one request made through a share: who asked, the tool, the
/// amount, what it was given, and the reason their agent gave, quoted as their
/// words and never as an instruction. While it waits, the owner answers it
/// here: Approve runs exactly this call on their computer once, Decline stops
/// it. The person it is shared with sees only how it stands.
struct PaymentCard: View {
    let connection: Connection
    let call: SkillShareCall
    let isOwner: Bool
    let onAnswered: (SkillShareCall) -> Void
    @State private var note = ""
    @State private var isAnswering = false
    @State private var problemText: String?
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    /// The two buttons side by side, or one above the other once the text is
    /// too large for both words to fit on a line.
    private var buttonLayout: AnyLayout {
        dynamicTypeSize.isAccessibilitySize ? AnyLayout(VStackLayout(spacing: 10)) : AnyLayout(HStackLayout(spacing: 10))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 10) {
                AvatarView(name: call.shownGrantee, avatarUrl: call.granteeAvatarUrl, colorIndex: call.granteeAvatarColorIndex, size: 32)
                Text(isOwner ? String(localized: "\(call.shownGrantee) asked to use your \(call.skillName) skill") : String(localized: "You asked to use \(call.ownerName)’s \(call.skillName) skill"))
                    .font(.subheadline.weight(.semibold))
                    .fixedSize(horizontal: false, vertical: true)
            }
            VStack(alignment: .leading, spacing: 4) {
                fact(String(localized: "Tool"), call.toolName)
                if call.amountText.isEmpty {
                    fact(String(localized: "Amount"), String(localized: "Not stated in the request. Only you can say whether it is fine."))
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
                buttonLayout {
                    Button {
                        Task { await answer(isApproved: true) }
                    } label: {
                        Text("Approve and run it").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    Button {
                        Task { await answer(isApproved: false) }
                    } label: {
                        Text("Decline").frame(maxWidth: .infinity).foregroundStyle(.primary)
                    }
                    .buttonStyle(.bordered)
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
                Text(verbatim: problemText).font(.footnote).foregroundStyle(.red)
            }
        }
        .accessibilityElement(children: .contain)
    }

    private func fact(_ name: String, _ value: String, isProminent: Bool = false) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(verbatim: name).font(.footnote).foregroundStyle(.secondary).frame(minWidth: 70, alignment: .leading)
            Text(verbatim: value).font(isProminent ? .headline : .callout)
            Spacer(minLength: 0)
        }
    }

    private func answer(isApproved: Bool) async {
        isAnswering = true
        defer { isAnswering = false }
        problemText = nil
        // Approving spends money, so it takes the phone's owner and not just
        // whoever holds it unlocked; declining spends nothing and needs no one.
        if isApproved {
            guard await DeviceOwner.confirm(reason: String(localized: "Approve this payment")) else { return }
        }
        do {
            onAnswered(try await connection.answerSkillShareCall(callId: call.id, isApproved: isApproved, decisionNote: note))
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// DeviceOwner asks the phone's owner to confirm, with Face ID, Touch ID or
/// the passcode. A phone with none of them set cannot tell its owner from
/// anyone else, so confirming is not refused there: the server's approval is
/// still the person's own, signed in.
enum DeviceOwner {
    static func confirm(reason: String) async -> Bool {
        let context = LAContext()
        var problem: NSError?
        guard context.canEvaluatePolicy(.deviceOwnerAuthentication, error: &problem) else { return true }
        do {
            return try await context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason)
        } catch {
            return false
        }
    }
}

/// PaymentWaitingCard is the payment_waiting note of the main conversation as a
/// card: the note names the call, the call is read for how it stands now (it may
/// have been answered on the dashboard since), and the card is drawn from that.
/// A call that is gone, because the share was taken back, says so.
struct PaymentWaitingCard: View {
    let connection: Connection
    let detail: String
    @State private var call: SkillShareCall?
    @State private var isGone = false
    @State private var isLoaded = false

    var body: some View {
        CardSurface {
            if let call {
                PaymentCard(connection: connection, call: call, isOwner: true) { answered in self.call = answered }
            } else if isLoaded || isGone {
                Label("This request is no longer there.", systemImage: "questionmark.circle")
                    .font(.footnote).foregroundStyle(.secondary)
            } else {
                ProgressView()
            }
        }
        .task(id: detail) { await load() }
    }

    private func load() async {
        defer { isLoaded = true }
        guard let note = SkillShareNote.parsing(detail) else {
            isGone = true
            return
        }
        do {
            call = try await connection.skillShareCalls(shareId: note.skillShareId).first { $0.id == note.callId }
            isGone = call == nil
        } catch {
            isGone = true
        }
    }
}
