import SwiftUI
import TeaNodeKit

// Finance accounts shared with other people. The person shares an account
// with a person or a circle at a level (Sharing, a part of Finance), and
// somebody who shares with the person can be chosen under Showing, when their
// accounts are shown at the level each was shared at. The agent has no part in
// any of it: it does not share an account and does not read what is shared.

/// FinanceSharer is somebody who shares finance accounts with the person.
struct FinanceSharer: Identifiable, Hashable {
    let userId: String
    let displayName: String
    var id: String { userId }
}

extension FinanceModel {
    /// The people who share an account with the person, once each, by name.
    var sharers: [FinanceSharer] {
        var found: [String: FinanceSharer] = [:]
        for account in sharedAccounts where found[account.ownerUserId] == nil {
            found[account.ownerUserId] = FinanceSharer(userId: account.ownerUserId, displayName: account.ownerShownName)
        }
        return found.values.sorted { $0.displayName.localizedCaseInsensitiveCompare($1.displayName) == .orderedAscending }
    }

    /// The person chosen under Showing, when they still share.
    var selectedSharer: FinanceSharer? {
        sharers.first { $0.userId == selectedOwnerUserId }
    }

    /// The accounts a person shares with the caller.
    func accountsShared(by sharer: FinanceSharer) -> [SharedFinanceAccount] {
        sharedAccounts.filter { $0.ownerUserId == sharer.userId }
    }
}

/// FinanceShareWords names the levels and the shares.
enum FinanceShareWords {
    static func name(_ level: FinanceShareLevel) -> String {
        switch level {
        case .balance: String(localized: "Balance")
        case .spending: String(localized: "Spending")
        case .transactions: String(localized: "Transactions")
        }
    }

    /// A level as the server wrote it, else the word itself for one added since.
    static func name(ofLevel financeShareLevel: String) -> String {
        guard let level = FinanceShareLevel(rawValue: financeShareLevel) else { return financeShareLevel }
        return name(level)
    }

    /// What a level shows.
    static func hint(_ level: FinanceShareLevel) -> String {
        switch level {
        case .balance:
            String(localized: "They see the account's name, kind and balance, and for a credit card how much of its limit is used.")
        case .spending:
            String(localized: "They also see what you spent and received each month, by spending category. No merchants.")
        case .transactions:
            String(localized: "They also see each transaction's date, amount, merchant and spending category.")
        }
    }

    /// Who a share is with: "Everyone in Family", or the person's name.
    static func name(of share: FinanceShareView) -> String {
        if share.granteeCircleId != nil {
            let circleName = share.granteeCircleName ?? ""
            return String(localized: "Everyone in \(circleName)")
        }
        if let granteeName = share.granteeName, !granteeName.isEmpty { return granteeName }
        return share.granteeUsername ?? ""
    }

    /// A month written "2026-09", as "September 2026".
    static func monthTitle(_ month: String) -> String {
        let parts = month.split(separator: "-").compactMap { Int($0) }
        guard parts.count == 2, let date = Calendar.current.date(from: DateComponents(year: parts[0], month: parts[1], day: 15)) else {
            return month
        }
        return date.formatted(.dateTime.month(.wide).year())
    }
}

// MARK: Showing

/// FinancePersonMenu chooses whose finance the screen shows: the person's
/// own, or that of somebody who shares an account.
struct FinancePersonMenu: View {
    @Environment(FinanceModel.self) private var finance

    var body: some View {
        Menu {
            Picker(
                "Showing",
                selection: Binding(
                    get: { finance.selectedOwnerUserId ?? "" },
                    set: { chosen in finance.selectedOwnerUserId = chosen.isEmpty ? nil : chosen })
            ) {
                Text("You").tag("")
                ForEach(finance.sharers) { sharer in
                    Text(verbatim: sharer.displayName).tag(sharer.userId)
                }
            }
        } label: {
            Label("Showing", systemImage: "person.2")
        }
        .accessibilityValue(Text(verbatim: finance.selectedSharer?.displayName ?? String(localized: "You")))
    }
}

/// SharedFinanceView is what one person shares: their accounts at the level
/// each was shared at. An account shared for its spending or more opens.
struct SharedFinanceView: View {
    @Environment(FinanceModel.self) private var finance
    let sharer: FinanceSharer

    var body: some View {
        let accounts = finance.accountsShared(by: sharer)
        List {
            Section {
                ForEach(accounts) { account in
                    if account.isShared(atLeast: .spending) {
                        NavigationLink(value: account) {
                            SharedFinanceAccountRow(account: account)
                        }
                    } else {
                        SharedFinanceAccountRow(account: account)
                    }
                }
            } header: {
                Text("Accounts shared by \(sharer.displayName)")
            } footer: {
                Text("You see each account as much as its owner chose to share. Your agent does not read any of it.")
            }
        }
        .navigationTitle(sharer.displayName)
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(for: SharedFinanceAccount.self) { account in
            SharedFinanceAccountScreen(account: account)
        }
        .refreshable { await finance.loadShared() }
    }
}

/// SharedFinanceAccountRow is one shared account: its name and what it is
/// shared for, its balance, and for a card how much of its limit is used.
struct SharedFinanceAccountRow: View {
    let account: SharedFinanceAccount

    var body: some View {
        LabeledContent {
            VStack(alignment: .trailing, spacing: 2) {
                Text(FinanceMoney.shown(account.currentBalance, currencyCode: account.currencyCode)).monospacedDigit()
                if let usageShare = account.creditUsage?.usageShare {
                    Text("\(FinanceMoney.percent(usageShare)) used")
                        .font(.caption)
                        .foregroundStyle(CreditWords.color(usageShare))
                }
            }
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: account.shownName)
                Text(verbatim: FinanceShareWords.name(ofLevel: account.financeShareLevel))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// SharedFinanceAccountScreen is one shared account: its balance, and what
/// its level adds: the months by spending category, and the transactions.
struct SharedFinanceAccountScreen: View {
    @Environment(FinanceModel.self) private var finance
    let account: SharedFinanceAccount
    @State private var spending: [SharedFinanceSpendingRow] = []
    @State private var hasLoaded = false
    @State private var loadFailedText: String?

    var body: some View {
        List {
            Section {
                LabeledContent("Balance") {
                    Text(FinanceMoney.shown(account.currentBalance, currencyCode: account.currencyCode)).monospacedDigit()
                }
                if let usage = account.creditUsage {
                    LabeledContent("Owed") {
                        Text(FinanceMoney.shown(usage.owedAmount, currencyCode: account.currencyCode)).monospacedDigit()
                    }
                    LabeledContent("Credit limit") {
                        Text(FinanceMoney.shown(usage.creditLimitAmount, currencyCode: account.currencyCode)).monospacedDigit()
                    }
                }
            } header: {
                Text(verbatim: account.shownName)
            } footer: {
                Text(verbatim: FinanceShareWords.name(ofLevel: account.financeShareLevel))
            }
            if account.isShared(atLeast: .transactions) {
                Section {
                    NavigationLink {
                        SharedFinanceTransactionsScreen(account: account)
                    } label: {
                        Label("Transactions", systemImage: "list.bullet.rectangle").foregroundStyle(.primary)
                    }
                }
            }
            if let loadFailedText, !hasLoaded {
                Section {
                    Text(verbatim: loadFailedText.asReasonSentence).foregroundStyle(.secondary)
                }
            } else if hasLoaded, spending.isEmpty, account.isShared(atLeast: .spending) {
                Section {
                    Text("Nothing spent or received yet").foregroundStyle(.secondary)
                }
            }
            ForEach(months, id: \.self) { month in
                Section {
                    ForEach(spending.filter { $0.spendingMonth == month }) { row in
                        SharedSpendingRow(row: row)
                    }
                } header: {
                    Text(verbatim: FinanceShareWords.monthTitle(month))
                }
            }
        }
        .navigationTitle(account.accountName)
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
    }

    /// The months with spending, newest first.
    private var months: [String] {
        Array(Set(spending.map { $0.spendingMonth })).sorted(by: >)
    }

    private func load() async {
        do {
            spending = try await finance.connection.sharedFinanceSpending(financeAccountId: account.financeAccountId)
            hasLoaded = true
            loadFailedText = nil
        } catch {
            loadFailedText = error.loadProblemText
        }
    }
}

/// SharedSpendingRow is one spending category of a month: what went out, and
/// what came in where something did.
struct SharedSpendingRow: View {
    let row: SharedFinanceSpendingRow

    var body: some View {
        LabeledContent {
            VStack(alignment: .trailing, spacing: 2) {
                Text(FinanceMoney.shown(row.moneyOut, currencyCode: row.currencyCode)).monospacedDigit()
                if (FinanceMoney.decimal(row.moneyIn) ?? 0) > 0 {
                    Text("\(FinanceMoney.shown(row.moneyIn, currencyCode: row.currencyCode)) in")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        } label: {
            Text(verbatim: row.spendingCategoryName.isEmpty ? String(localized: "No category yet") : row.spendingCategoryName)
        }
    }
}

/// SharedFinanceTransactionsScreen is a shared account's transactions,
/// newest first, read a page at a time as the list is scrolled.
struct SharedFinanceTransactionsScreen: View {
    @Environment(FinanceModel.self) private var finance
    let account: SharedFinanceAccount
    @State private var transactions: [SharedFinanceTransaction] = []
    @State private var totalCount = 0
    @State private var hasLoaded = false
    @State private var isLoadingMore = false
    @State private var loadFailedText: String?
    private let pageSize = 50

    var body: some View {
        Group {
            if let loadFailedText, !hasLoaded {
                LoadFailedView(problemText: loadFailedText) { await loadFirst() }
            } else if !hasLoaded {
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if transactions.isEmpty {
                ContentUnavailableView("No transactions yet", systemImage: "list.bullet.rectangle")
            } else {
                List {
                    ForEach(transactions) { transaction in
                        SharedTransactionRow(transaction: transaction)
                            .onAppear {
                                if transaction.id == transactions.last?.id { Task { await loadMore() } }
                            }
                    }
                    if isLoadingMore {
                        ProgressView().frame(maxWidth: .infinity)
                    }
                }
                .refreshable { await loadFirst() }
            }
        }
        .navigationTitle(account.accountName)
        .navigationBarTitleDisplayMode(.inline)
        .task { await loadFirst() }
    }

    private func loadFirst() async {
        do {
            let page = try await finance.connection.sharedFinanceTransactions(financeAccountId: account.financeAccountId, limit: pageSize, offset: 0)
            transactions = page.financeTransactions
            totalCount = page.totalCount
            hasLoaded = true
            loadFailedText = nil
        } catch {
            loadFailedText = error.loadProblemText
        }
    }

    private func loadMore() async {
        guard !isLoadingMore, transactions.count < totalCount else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page = try await finance.connection.sharedFinanceTransactions(
                financeAccountId: account.financeAccountId, limit: pageSize, offset: transactions.count)
            transactions.append(contentsOf: page.financeTransactions)
            totalCount = page.totalCount
        } catch {
            finance.problemText = error.loadProblemText
        }
    }
}

/// SharedTransactionRow is one shared transaction: who it was with and its
/// category, the day, and the amount.
struct SharedTransactionRow: View {
    let transaction: SharedFinanceTransaction

    var body: some View {
        HStack(alignment: .center) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: merchantText).lineLimit(1)
                HStack(spacing: 4) {
                    Text(verbatim: transaction.postedOn)
                    if let categoryName = transaction.spendingCategoryName, !categoryName.isEmpty {
                        Text(verbatim: categoryName)
                    }
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
            Spacer()
            Text(FinanceMoney.shown(transaction.amount, currencyCode: transaction.currencyCode))
                .monospacedDigit()
                .foregroundStyle((FinanceMoney.decimal(transaction.amount) ?? 0) > 0 ? AnyShapeStyle(.green) : AnyShapeStyle(.primary))
        }
        .accessibilityElement(children: .combine)
    }

    private var merchantText: String {
        if let merchantName = transaction.merchantName, !merchantName.isEmpty { return merchantName }
        return String(localized: "No merchant name")
    }
}

// MARK: Sharing

/// FinanceSharingView is the Sharing part of Finance: which of the person's
/// accounts are shared with whom, and at what level, each changeable and
/// removable, and the way to share another.
struct FinanceSharingView: View {
    @Environment(FinanceModel.self) private var finance
    @State private var shares: [FinanceShareView] = []
    @State private var hasLoaded = false
    @State private var loadFailedText: String?
    @State private var isSharing = false

    var body: some View {
        List {
            Section {
                if hasLoaded, shares.isEmpty {
                    Text("None of your accounts is shared with anybody.").foregroundStyle(.secondary)
                }
                ForEach(shares) { share in
                    NavigationLink {
                        FinanceShareScreen(share: share) { await load() }
                    } label: {
                        FinanceShareRow(share: share)
                    }
                }
                Button("Share an account", systemImage: "person.badge.plus") { isSharing = true }
                    .actionColor()
                    .disabled(finance.accounts.isEmpty)
            } header: {
                Text("Sharing")
            } footer: {
                Text("People you share an account with see only what the level you choose shows. Your agent never reads what others share with you.")
            }
        }
        .navigationTitle("Sharing")
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if let loadFailedText, !hasLoaded {
                LoadFailedView(problemText: loadFailedText) { await load() }
            }
        }
        .sheet(isPresented: $isSharing) {
            FinanceShareSheet { await load() }
        }
        .task { await load() }
        .refreshable { await load() }
    }

    private func load() async {
        do {
            shares = try await finance.connection.financeShares()
            hasLoaded = true
            loadFailedText = nil
        } catch {
            loadFailedText = error.loadProblemText
        }
    }
}

/// FinanceShareRow is one share: which account, who it is with and the level.
struct FinanceShareRow: View {
    let share: FinanceShareView

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: share.shownAccountName)
            Text("\(FinanceShareWords.name(of: share)), \(FinanceShareWords.name(ofLevel: share.financeShareLevel))")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }
}

/// FinanceShareScreen changes the level of one share, or stops it. A lower
/// level is saved at once; the level is not changed on the screen until the
/// server has taken it.
struct FinanceShareScreen: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let share: FinanceShareView
    let onChanged: () async -> Void
    @State private var chosenLevel: FinanceShareLevel
    @State private var isConfirmingStop = false

    init(share: FinanceShareView, onChanged: @escaping () async -> Void) {
        self.share = share
        self.onChanged = onChanged
        _chosenLevel = State(initialValue: share.level ?? .balance)
    }

    var body: some View {
        let name = FinanceShareWords.name(of: share)
        Form {
            Section {
                Picker(
                    "How much",
                    selection: Binding(get: { chosenLevel }, set: { updated in save(updated) })
                ) {
                    ForEach(FinanceShareLevel.allCases, id: \.self) { level in
                        Text(verbatim: FinanceShareWords.name(level)).tag(level)
                    }
                }
                .pickerStyle(.inline)
                .labelsHidden()
            } header: {
                Text("\(share.shownAccountName), shared with \(name)")
            } footer: {
                Text(verbatim: FinanceShareWords.hint(chosenLevel))
            }
            Section {
                Button(role: .destructive) {
                    isConfirmingStop = true
                } label: {
                    Label("Stop sharing", systemImage: "person.badge.minus").foregroundStyle(.red)
                }
                .tint(.red)
                .actionColor(isDestructive: true)
                .confirmationDialog("Stop sharing", isPresented: $isConfirmingStop, titleVisibility: .visible) {
                    Button("Stop sharing", role: .destructive) { stop(name: name) }
                } message: {
                    Text("\(name) will no longer see \(share.shownAccountName).")
                }
            }
        }
        .navigationTitle(share.accountName)
        .navigationBarTitleDisplayMode(.inline)
    }

    /// Saves the level; a refusal leaves the choice where it was.
    private func save(_ updated: FinanceShareLevel) {
        Task {
            let name = FinanceShareWords.name(of: share)
            let isSaved = await finance.act(saying: String(localized: "\(share.shownAccountName) is shared with \(name).")) {
                try await finance.connection.shareFinanceAccount(financeAccountId: share.financeAccountId, with: share.target, level: updated)
            }
            if isSaved {
                chosenLevel = updated
                await onChanged()
            }
        }
    }

    private func stop(name: String) {
        Task {
            let isStopped = await finance.act(saying: String(localized: "\(share.shownAccountName) is no longer shared with \(name).")) {
                try await finance.connection.unshareFinanceAccount(shareId: share.id)
            }
            if isStopped {
                await onChanged()
                dismiss()
            }
        }
    }
}

/// FinanceShareSheet chooses an account, a person or a circle and the level to
/// share it at.
struct FinanceShareSheet: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let onShared: () async -> Void
    @State private var people: [CalendarSharePerson] = []
    @State private var circles: [CalendarShareCircle] = []
    @State private var isLoaded = false
    @State private var loadProblemText: String?
    @State private var financeAccountId = ""
    @State private var chosen: ShareTarget?
    @State private var chosenLevel = FinanceShareLevel.balance
    @State private var isWorking = false
    /// Said inside the sheet: the toast of the screen behind it is hidden.
    @State private var problemText: String?
    @State private var doneText: String?

    var body: some View {
        NavigationStack {
            Form {
                if isLoaded && people.isEmpty && circles.isEmpty {
                    Section {
                        Text("You are not in a circle with anyone yet.")
                            .foregroundStyle(.secondary)
                        OpenCirclesLink(connection: finance.connection)
                    }
                } else if isLoaded {
                    Section {
                        Picker("Account", selection: $financeAccountId) {
                            Text("Choose an account").tag("")
                            ForEach(finance.accounts) { account in
                                Text(verbatim: account.shownName).tag(account.id)
                            }
                        }
                    }
                    Section {
                        ShareTargetPicker(people: people, circles: circles, chosen: $chosen)
                    } footer: {
                        Text("Choose a person, or a whole circle.")
                    }
                    Section {
                        Picker("How much", selection: $chosenLevel) {
                            ForEach(FinanceShareLevel.allCases, id: \.self) { level in
                                Text(verbatim: FinanceShareWords.name(level)).tag(level)
                            }
                        }
                        .pickerStyle(.inline)
                        .labelsHidden()
                    } header: {
                        Text("What they see")
                    } footer: {
                        Text(verbatim: FinanceShareWords.hint(chosenLevel))
                    }
                }
            }
            .navigationTitle("Share an account")
            .navigationBarTitleDisplayMode(.inline)
            .overlay {
                if let loadProblemText, !isLoaded {
                    LoadFailedView(problemText: loadProblemText) { await load() }
                } else if !isLoaded {
                    ProgressView()
                }
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }.disabled(isWorking)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isWorking {
                        ProgressView()
                    } else {
                        Button("Share") { Task { await share() } }
                            .disabled(chosen == nil || financeAccountId.isEmpty)
                    }
                }
            }
            .toast(done: $doneText, problem: $problemText)
            .task { await load() }
        }
        .presentationDetents([.large])
    }

    private func load() async {
        do {
            async let readPeople = finance.connection.calendarSharePeople()
            async let readCircles = finance.connection.calendarShareCircles()
            (people, circles) = try await (readPeople, readCircles)
            isLoaded = true
            loadProblemText = nil
        } catch {
            loadProblemText = error.loadProblemText
        }
    }

    private func share() async {
        guard let target = chosen, !financeAccountId.isEmpty else { return }
        isWorking = true
        defer { isWorking = false }
        let name = ShareTargetPicker.name(of: target, people: people, circles: circles)
        do {
            try await finance.connection.shareFinanceAccount(financeAccountId: financeAccountId, with: target, level: chosenLevel)
            finance.doneText = String(localized: "Shared with \(name).")
            await onShared()
            dismiss()
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "Could not save the sharing.")).asReasonSentence
        }
    }
}
