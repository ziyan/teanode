import SwiftUI
import TeaNodeKit
import UniformTypeIdentifiers

// What the dashboard and the command line do with finance that the screens
// in FinanceViews and FinanceMoreViews did not: an investment account's
// trades, renaming and deleting an account of imported statements, the
// spending rules to suggest for a category, bringing in a connection made
// elsewhere, and importing transaction rows after seeing what they would do.

// MARK: Trades

enum TradeWords {
    /// A trade's kind in words; one the app does not know is shown as sent.
    static func kind(_ tradeKind: String?) -> String {
        switch tradeKind {
        case "buy": String(localized: "Buy")
        case "sell": String(localized: "Sell")
        case "cancel": String(localized: "Canceled")
        case "transfer": String(localized: "Transfer")
        default: tradeKind ?? ""
        }
    }

    /// "2 at $100.00", or the quantity alone when the price is not known.
    static func quantityLine(_ trade: FinanceTrade) -> String? {
        guard let quantity = trade.tradedQuantity, !quantity.isEmpty else { return nil }
        guard let unitPrice = trade.unitPrice, !unitPrice.isEmpty else { return quantity }
        let price = FinanceMoney.shown(unitPrice, currencyCode: trade.currencyCode)
        return String(localized: "\(quantity) at \(price)")
    }
}

/// TradesView is an account's trades, newest first, fifty at a time: every
/// buy, sell, cancel and transfer in it, as the dashboard lists them.
struct TradesView: View {
    @Environment(FinanceModel.self) private var finance
    let account: FinanceAccount
    @State private var trades: [FinanceTrade] = []
    @State private var total = 0
    @State private var isLoaded = false

    var body: some View {
        List {
            if total > 0 {
                Text("\(total) trades")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Section {
                ForEach(trades) { trade in
                    TradeRow(trade: trade)
                        .onAppear {
                            if trade.id == trades.last?.id, trades.count < total {
                                Task { await load(isMore: true) }
                            }
                        }
                }
            } footer: {
                if !trades.isEmpty {
                    // A plain list leaves its footers in body text.
                    Text("Every buy, sell, cancel and transfer in this account, newest first. The amount is the cash the trade moved: negative when cash left the account.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .listStyle(.plain)
        .navigationTitle("Trades")
        .navigationSubtitle(account.shownName)
        .task { await load(isMore: false) }
        .refreshable { await load(isMore: false) }
        .overlay {
            if isLoaded, trades.isEmpty {
                ContentUnavailableView("No trades", systemImage: "chart.line.uptrend.xyaxis", description: Text("No trades in this account yet."))
            } else if !isLoaded {
                ProgressView()
            }
        }
    }

    private func load(isMore: Bool) async {
        do {
            let page = try await finance.connection.financeTrades(financeAccountId: account.id, offset: isMore ? trades.count : 0)
            let fresh = page.financeTrades.compactMap { $0 }
            trades = isMore ? trades + fresh.filter { row in !trades.contains { $0.id == row.id } } : fresh
            total = page.totalCount ?? trades.count
            isLoaded = true
        } catch {
            finance.problemText = error.localizedDescription
            isLoaded = true
        }
    }
}

/// TradeRow is a trade: what, when, how many at what price, and the cash it
/// moved.
struct TradeRow: View {
    let trade: FinanceTrade

    /// "2026-10-01 · Buy · 2 at $100.00": when, the kind, and the quantity.
    private var secondaryLine: String {
        [trade.tradedOn, TradeWords.kind(trade.tradeKind), TradeWords.quantityLine(trade)]
            .compactMap { $0 }
            .filter { !$0.isEmpty }
            .joined(separator: " · ")
    }

    var body: some View {
        HStack(alignment: .center) {
            VStack(alignment: .leading, spacing: 2) {
                Text(trade.shownName).lineLimit(1)
                Text(secondaryLine)
                .font(.caption)
                .foregroundStyle(.secondary)
                if (FinanceMoney.decimal(trade.feeAmount) ?? 0) != 0 {
                    let fee = FinanceMoney.shown(trade.feeAmount, currencyCode: trade.currencyCode)
                    Text("Fee \(fee)").font(.caption).foregroundStyle(.secondary)
                }
            }
            Spacer()
            Text(FinanceMoney.shown(trade.tradeAmount, currencyCode: trade.currencyCode))
                .monospacedDigit()
                .foregroundStyle((FinanceMoney.decimal(trade.tradeAmount) ?? 0) > 0 ? AnyShapeStyle(.green) : AnyShapeStyle(.primary))
        }
    }
}

/// AccountTradesLink is the row under an investment account that opens its
/// trades.
struct AccountTradesLink: View {
    let account: FinanceAccount

    var body: some View {
        NavigationLink {
            TradesView(account: account)
        } label: {
            Label("Trades", systemImage: "chart.line.uptrend.xyaxis")
                .foregroundStyle(.primary)
        }
    }
}

/// FinanceManageDemoScreens opens the screens a screenshot names
/// (`-TeaNodeScreen finance-trades`, `finance-import-preview`,
/// `finance-receipt-entry`, `finance-paystub-entry`, `finance-tax-form-entry`)
/// once the accounts are shown, so the screenshot does not depend on how far
/// the list is scrolled. It does nothing in a release build or without the name.
struct FinanceManageDemoScreens: ViewModifier {
    @Environment(FinanceModel.self) private var finance
    let accounts: [FinanceAccount]
    let onImported: () -> Void
    @State private var demoTradesAccount: FinanceAccount?
    @State private var isDemoImportOpen = false
    @State private var demoEntryScreen: DemoEntryScreen?

    func body(content: Content) -> some View {
        content
            .navigationDestination(item: $demoTradesAccount) { TradesView(account: $0) }
            .navigationDestination(isPresented: $isDemoImportOpen) { ImportTransactionRowsView(onImported: onImported) }
            .sheet(item: $demoEntryScreen) { screen in
                switch screen {
                case .receipt:
                    ReceiptEntryView(currencyCode: finance.reportingCurrency, today: finance.today) {}
                case .paystub:
                    PaystubEntryView(currencyCode: finance.reportingCurrency, today: finance.today) {}
                case .taxForm:
                    TaxFormEntryView(taxYear: finance.dates.component(.year, from: Date()) - 1) {}
                }
            }
            .task(id: accounts.count) {
                #if DEBUG
                    guard !accounts.isEmpty, let screen = DemoServer.screen else { return }
                    try? await Task.sleep(for: .milliseconds(700))
                    if screen == "finance-trades" {
                        demoTradesAccount = accounts.first { $0.accountKind == "investment" }
                    } else if screen == "finance-import-preview" {
                        isDemoImportOpen = true
                    } else {
                        demoEntryScreen = DemoEntryScreen(rawValue: screen)
                    }
                #endif
            }
    }
}

/// DemoEntryScreen is a screen for typing in a record that a screenshot opens.
private enum DemoEntryScreen: String, Identifiable {
    case receipt = "finance-receipt-entry"
    case paystub = "finance-paystub-entry"
    case taxForm = "finance-tax-form-entry"

    var id: String { rawValue }
}

extension View {
    func financeManageDemoScreens(accounts: [FinanceAccount], onImported: @escaping () -> Void) -> some View {
        modifier(FinanceManageDemoScreens(accounts: accounts, onImported: onImported))
    }
}

// MARK: Accounts of imported statements

/// StatementAccountActions gives an account that came from statement
/// imports what a provider's account does not have: a rename and a delete,
/// from a swipe or a long press. A provider's account is left as it is,
/// since its next sync would bring it back. The row only says which account
/// is asked about; the list shows the sheet and the question
/// (StatementAccountPresentations), since a sheet on a row is closed when the
/// list draws the row again, as a swipe closing or a reload of the list does.
struct StatementAccountActions: ViewModifier {
    let account: FinanceAccount
    @Binding var renaming: FinanceAccount?
    @Binding var deleting: FinanceAccount?

    func body(content: Content) -> some View {
        if account.isFromStatements {
            content
                .swipeActions {
                    Button("Delete", systemImage: "trash", role: .destructive) { deleting = account }
                        .tint(.red)
                    Button("Rename", systemImage: "pencil") { renaming = account }
                        .tint(.indigo)
                }
                .contextMenu {
                    Button("Rename", systemImage: "pencil") { renaming = account }
                    Button("Delete", systemImage: "trash", role: .destructive) { deleting = account }
                        .tint(.red)
                }
        } else {
            content
        }
    }
}

/// StatementAccountPresentations is the rename sheet and the delete question
/// of StatementAccountActions, on the list that holds the rows.
struct StatementAccountPresentations: ViewModifier {
    @Environment(FinanceModel.self) private var finance
    @Binding var renaming: FinanceAccount?
    @Binding var deleting: FinanceAccount?
    let onChange: () async -> Void

    func body(content: Content) -> some View {
        content
            .confirmationDialog(
                "Delete \(deleting?.shownName ?? "")?", isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                titleVisibility: .visible, presenting: deleting
            ) { account in
                Button("Delete account", role: .destructive) { delete(account) }
            } message: { _ in
                Text("Its transactions and its net worth history are deleted for good, and spending, budgets and net worth stop counting them. Spending rules limited to this account are deleted with it. Importing its statements or screenshots again brings the transactions back, but not values you recorded by hand.")
            }
            .sheet(item: $renaming) { account in
                RenameStatementAccountSheet(account: account, onRenamed: onChange)
            }
    }

    private func delete(_ account: FinanceAccount) {
        let name = account.shownName
        Task {
            do {
                let deleted = try await finance.connection.deleteStatementAccount(account.id)
                let count = deleted?.deletedTransactionCount ?? 0
                finance.doneText = count == 1
                    ? String(localized: "Deleted \(name) and its 1 transaction.")
                    : String(localized: "Deleted \(name) and its \(count) transactions.")
                await finance.load()
                await onChange()
            } catch {
                finance.problemText = error.localizedDescription
            }
        }
    }
}

extension View {
    func statementAccountActions(_ account: FinanceAccount, renaming: Binding<FinanceAccount?>, deleting: Binding<FinanceAccount?>) -> some View {
        modifier(StatementAccountActions(account: account, renaming: renaming, deleting: deleting))
    }

    func statementAccountPresentations(
        renaming: Binding<FinanceAccount?>, deleting: Binding<FinanceAccount?>, onChange: @escaping () async -> Void
    ) -> some View {
        modifier(StatementAccountPresentations(renaming: renaming, deleting: deleting, onChange: onChange))
    }
}

/// RenameStatementAccountSheet gives an account of imported statements the
/// person's own name and, when the statements show a word or nothing in
/// its place, the last digits of its number. Later imports keep both.
struct RenameStatementAccountSheet: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let account: FinanceAccount
    let onRenamed: () async -> Void
    @State private var accountName: String
    @State private var accountMask: String
    @State private var isSaving = false

    init(account: FinanceAccount, onRenamed: @escaping () async -> Void) {
        self.account = account
        self.onRenamed = onRenamed
        _accountName = State(initialValue: account.accountName ?? "")
        _accountMask = State(initialValue: account.accountMask ?? "")
    }

    private var isNameChanged: Bool {
        !accountName.trimmed.isEmpty && accountName.trimmed != (account.accountName ?? "")
    }

    private var isMaskChanged: Bool { accountMask.trimmed != (account.accountMask ?? "") }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Name", text: $accountName)
                } footer: {
                    Text("Later imports keep this name.")
                }
                Section {
                    TextField("Account number, last digits", text: $accountMask)
                        .keyboardType(.numberPad)
                } footer: {
                    Text("The last 4 to 8 digits, for statements that show a word or nothing in their place. Later imports keep them, and screenshots showing them are matched to this account. Leave it empty to show the number from the statements.")
                }
            }
            .navigationTitle("Rename account")
            .navigationBarTitleDisplayMode(.inline)
            .onChange(of: accountName) { accountName = String(accountName.prefix(200)) }
            .onChange(of: accountMask) { accountMask = String(accountMask.prefix(8)) }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(isSaving || accountName.trimmed.isEmpty || !(isNameChanged || isMaskChanged))
                }
            }
        }
    }

    /// What the toast says, each thing that changed, so a number that did not
    /// take is not hidden behind a rename that did.
    private func words(name: String, mask: String) -> String {
        if isNameChanged && isMaskChanged {
            return mask.isEmpty
                ? String(localized: "Renamed to \(name), and the account number is the one from the statements again.")
                : String(localized: "Renamed to \(name), and the account number now ends \(mask).")
        }
        if isNameChanged { return String(localized: "Renamed to \(name).") }
        return mask.isEmpty
            ? String(localized: "The account number is the one from the statements again.")
            : String(localized: "The account number now ends \(mask).")
    }

    private func save() {
        let name = accountName.trimmed
        let mask = accountMask.trimmed
        let saying = words(name: name, mask: mask)
        let sentName: String? = isNameChanged ? name : nil
        let sentMask: String? = isMaskChanged ? mask : nil
        Task {
            isSaving = true
            let isSaved = await finance.act(saying: saying) {
                _ = try await finance.connection.renameStatementAccount(account.id, accountName: sentName, accountMask: sentMask)
            }
            isSaving = false
            if isSaved {
                await finance.load()
                await onRenamed()
                dismiss()
            }
        }
    }
}

// MARK: Suggested spending rules

/// SuggestRulesSheet suggests spending rules for a category: the words the
/// transactions filed under it share, minus what the rules already cover.
/// Each is added with one tap; nothing is saved before that.
struct SuggestRulesSheet: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let onSaved: () -> Void
    @State private var categoryId = ""
    @State private var proposals: SpendingRuleProposals?
    @State private var sampledCount = 0
    @State private var isLoading = false
    @State private var addedMatchTexts: Set<String> = []

    /// The most transactions of a category one suggestion reads, two hundred
    /// at a time; the server reads up to five thousand.
    private static let maximumSampledTransactionCount = 600
    /// The most rules the server proposes at once.
    private static let maximumProposalCount = 50

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Category", selection: $categoryId) {
                        Text("Choose").tag("")
                        ForEach(finance.pickable(keeping: categoryId)) { category in
                            Text(finance.label(category)).tag(category.id)
                        }
                    }
                } footer: {
                    Text("Looks at the transactions filed under the category and suggests the words they share. Nothing is saved until you add a rule.")
                }
                if isLoading {
                    Section { ProgressView() }
                } else if !categoryId.isEmpty {
                    results
                }
            }
            .navigationTitle("Suggest rules")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
            .task(id: categoryId) { await suggest() }
        }
    }

    @ViewBuilder
    private var results: some View {
        if sampledCount == 0 {
            Section { Text("No transactions are filed under this category yet.").foregroundStyle(.secondary) }
        } else if let proposals {
            if proposals.proposals.isEmpty {
                Section {
                    Text("No rules to suggest: your spending rules already file transactions like these there.")
                        .foregroundStyle(.secondary)
                } footer: {
                    leftOutText(proposals)
                }
            } else {
                Section {
                    ForEach(proposals.proposals) { proposal in
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text("“\(proposal.matchText)”")
                                Text(detail(proposal)).font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            if addedMatchTexts.contains(proposal.matchText) {
                                Image(systemName: "checkmark").foregroundStyle(.secondary).accessibilityLabel("Added")
                            } else {
                                Button("Add") { add(proposal) }.buttonStyle(.borderless)
                            }
                        }
                    }
                } header: {
                    Text("Suggested rules")
                } footer: {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("New transactions whose merchant or description contains these words will be filed under \(finance.categoryName(categoryId)).")
                        leftOutText(proposals)
                    }
                }
            }
        }
    }

    /// How many of the transactions a rule matches, and what else it would
    /// change.
    private func detail(_ proposal: SpendingRuleProposals.Proposal) -> String {
        var parts = [String(localized: "\(proposal.financeTransactionCount ?? 0) transactions")]
        let changed = proposal.changedTransactionCount ?? 0
        if changed == 1 {
            parts.append(String(localized: "also changes 1 other transaction"))
        } else if changed > 1 {
            parts.append(String(localized: "also changes \(changed) other transactions"))
        }
        return parts.joined(separator: " · ")
    }

    /// What was left out and why, in the dashboard's words.
    @ViewBuilder
    private func leftOutText(_ proposals: SpendingRuleProposals) -> some View {
        let lines = leftOutLines(proposals)
        if !lines.isEmpty { Text(lines.joined(separator: ". ")) }
    }

    private func leftOutLines(_ proposals: SpendingRuleProposals) -> [String] {
        var lines: [String] = []
        let tooGeneric = proposals.tooGenericMatchTextCount ?? 0
        if tooGeneric == 1 {
            lines.append(String(localized: "1 left out: too short or too generic to be a rule"))
        } else if tooGeneric > 1 {
            lines.append(String(localized: "\(tooGeneric) left out: too short or too generic to be rules"))
        }
        let changingNumber = proposals.changingNumberMatchTextCount ?? 0
        if changingNumber == 1 {
            lines.append(String(localized: "1 left out: it holds a number that changes each time"))
        } else if changingNumber > 1 {
            lines.append(String(localized: "\(changingNumber) left out: they hold a number that changes each time"))
        }
        let overLimit = proposals.overLimitMatchTextCount ?? 0
        if overLimit > 0 {
            let maximum = Self.maximumProposalCount
            lines.append(String(localized: "\(overLimit) more left out: at most \(maximum) spending rules at a time"))
        }
        return lines
    }

    private func add(_ proposal: SpendingRuleProposals.Proposal) {
        let categoryId = categoryId
        Task {
            let isSaved = await finance.act(saying: String(localized: "Saved")) {
                try await finance.connection.saveSpendingRule(
                    nil, matchText: proposal.matchText, categoryId: categoryId, accountId: "", minimum: "", maximum: "", priority: nil)
            }
            if isSaved {
                addedMatchTexts.insert(proposal.matchText)
                onSaved()
            }
        }
    }

    /// Reads the transactions filed under the category, then asks the server
    /// which rules they suggest.
    private func suggest() async {
        proposals = nil
        sampledCount = 0
        addedMatchTexts = []
        guard !categoryId.isEmpty else {
            isLoading = false
            return
        }
        isLoading = true
        do {
            var filter = FinanceTransactionFilter()
            filter.spendingCategoryId = categoryId
            var transactionIds: [String] = []
            while transactionIds.count < Self.maximumSampledTransactionCount {
                let page = try await finance.connection.financeTransactions(filter, limit: 200, offset: transactionIds.count)
                let fresh = page.financeTransactions.compactMap { $0 }
                transactionIds += fresh.map(\.id)
                if fresh.isEmpty || transactionIds.count >= (page.totalCount ?? 0) { break }
            }
            sampledCount = transactionIds.count
            if !transactionIds.isEmpty {
                proposals = try await finance.connection.proposeSpendingRules(transactionIds: transactionIds, categoryId: categoryId)
            }
            isLoading = false
        } catch {
            // A category chosen over this one cancels it, and says nothing.
            if !Task.isCancelled {
                finance.problemText = error.localizedDescription
                isLoading = false
            }
        }
    }
}

// MARK: Bringing in a connection

/// ImportCredentialView brings in a provider connection made elsewhere: a
/// Plaid access token of a link made with the server's Plaid keys, or a
/// SimpleFIN access URL already claimed from a setup token. It does not
/// use up another link the way linking again would.
struct ImportCredentialView: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let onImported: () -> Void
    @State private var providerKinds: [String] = []
    @State private var providerKind = ""
    @State private var credential = ""
    @State private var institutionName = ""
    @State private var isImporting = false

    var body: some View {
        Form {
            if providerKinds.count > 1 {
                Section {
                    Picker("Provider", selection: $providerKind) {
                        ForEach(providerKinds, id: \.self) { kind in
                            Text(verbatim: Self.providerName(kind)).tag(kind)
                        }
                    }
                }
            }
            Section {
                SecureField("Credential", text: $credential)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            } footer: {
                if providerKind == "plaid" {
                    Text("The access token of a Plaid link made with this server’s Plaid keys. Bringing it in does not use up another Plaid link the way linking again would.")
                } else {
                    Text("A SimpleFIN access URL already claimed from a setup token, beginning with https:// and holding its own user name and password.")
                }
            }
            Section {
                TextField("Institution name (optional)", text: $institutionName, prompt: Text("Asked of the provider when left empty"))
            }
            Section {
                Button("Import") { importCredential() }
                    .disabled(isImporting || credential.trimmed.isEmpty || providerKind.isEmpty)
            }
        }
        .navigationTitle("Bring an existing connection")
        .navigationBarTitleDisplayMode(.inline)
        .task {
            let kinds = (try? await finance.connection.financeProviderKinds()) ?? []
            providerKinds = kinds.isEmpty ? ["simplefin"] : kinds
            if !providerKinds.contains(providerKind) { providerKind = providerKinds[0] }
        }
    }

    private static func providerName(_ kind: String) -> String {
        switch kind {
        case "plaid": "Plaid"
        case "simplefin": "SimpleFIN"
        default: kind
        }
    }

    private func importCredential() {
        let kind = providerKind
        let token = credential.trimmed
        let name = institutionName.trimmed
        Task {
            isImporting = true
            let isDone = await finance.act(saying: String(localized: "Connection brought in. Its first sync starts within the minute.")) {
                try await finance.connection.importFinanceCredential(providerKind: kind, credential: token, institutionName: name.isEmpty ? nil : name)
            }
            isImporting = false
            if isDone {
                credential = ""
                onImported()
                dismiss()
            }
        }
    }
}

// MARK: Importing transaction rows

/// ImportTransactionRowsView imports a file of transaction rows, the file
/// the command line's import-transactions reads, after showing what the
/// server read from it: the account, the rows it would add, the rows it holds
/// already, and what was checked. Nothing is written before Import.
struct ImportTransactionRowsView: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let onImported: () -> Void
    @State private var arguments: [String: JSONValue]?
    @State private var fileName = ""
    @State private var preview: TransactionRowsPreview?
    @State private var isChoosingFile = false
    @State private var isWorking = false

    /// More than any account's rows.
    private static let maximumFileBytes = 8 * 1024 * 1024

    var body: some View {
        Form {
            Section {
                Button("Choose a file", systemImage: "doc") { isChoosingFile = true }
                    .actionColor()
                    .disabled(isWorking)
                if !fileName.isEmpty {
                    LabeledContent("File", value: fileName)
                }
            } footer: {
                Text("A JSON file of transaction rows, the file the command line’s import-transactions reads. You see what it would do before anything is imported.")
            }
            if isWorking, preview == nil {
                Section { ProgressView() }
            }
            if let preview {
                previewSections(preview)
            }
        }
        .navigationTitle("Import transactions")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button("Import") { importRows() }
                    .disabled(isWorking || arguments == nil || preview?.newRows.isEmpty != false)
            }
        }
        .fileImporter(isPresented: $isChoosingFile, allowedContentTypes: [.json]) { result in
            read(result)
        }
        .task {
            #if DEBUG
                // The screenshot of the preview (`-TeaNodeScreen finance-import-preview`).
                guard DemoServer.screen == "finance-import-preview", arguments == nil,
                    let variables = TransactionRowsFile.variables(from: Data(DemoServer.demoRowsFile.utf8))
                else { return }
                fileName = "rows.json"
                await choose(variables)
            #endif
        }
    }

    @ViewBuilder
    private func previewSections(_ preview: TransactionRowsPreview) -> some View {
        let currencyCode = preview.currencyCode
        Section("Account") {
            if preview.isNewAccount == true {
                Text("They would go into a new account, \(preview.accountName ?? "").")
            } else {
                LabeledContent("Account", value: preview.accountName ?? "")
                Text(accountWords(preview)).font(.callout).foregroundStyle(.secondary)
            }
        }
        Section("What would be imported") {
            LabeledContent("New rows", value: String(preview.newRows.count))
            LabeledContent("Already there", value: String(preview.presentRows.count))
            if !preview.newRows.isEmpty {
                if let first = preview.firstPostedOn, let last = preview.lastPostedOn {
                    LabeledContent("Days", value: first == last ? first : String(localized: "\(first) to \(last)"))
                }
                LabeledContent("Money in", value: FinanceMoney.shown(preview.moneyInAmount, currencyCode: currencyCode))
                LabeledContent("Money out", value: FinanceMoney.shown(preview.moneyOutAmount, currencyCode: currencyCode))
            }
            if let summary = preview.verificationSummary, !summary.isEmpty {
                // The server's summary is a phrase in lower case, as the
                // command line prints it after a colon.
                LabeledContent("Checked", value: String(summary.prefix(1)).uppercased() + String(summary.dropFirst()))
            }
        }
        if !preview.newRows.isEmpty {
            Section("New rows") {
                ForEach(preview.newRows) { row in
                    HStack(alignment: .center) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(row.description ?? "").lineLimit(2)
                            Text(row.postedOn ?? "").font(.caption).foregroundStyle(.secondary)
                            if row.hasNearbyStoredTransaction == true {
                                Text("A stored transaction of the same amount is within 3 days.")
                                    .font(.caption)
                                    .foregroundStyle(.orange)
                            }
                        }
                        Spacer()
                        Text(FinanceMoney.shown(row.amount, currencyCode: currencyCode))
                            .monospacedDigit()
                            .foregroundStyle((FinanceMoney.decimal(row.amount) ?? 0) > 0 ? AnyShapeStyle(.green) : AnyShapeStyle(.primary))
                    }
                }
            }
        }
    }

    /// How the account the rows would go into was found.
    private func accountWords(_ preview: TransactionRowsPreview) -> String {
        switch preview.accountMatch {
        case "finance_account_id": return String(localized: "An account you have, found by its id.")
        case "account_number": return String(localized: "An account you have, found by its number.")
        case "account_mask": return String(localized: "An account you have, found by the last digits of its number.")
        default: return String(localized: "An account you have.")
        }
    }

    private func read(_ result: Result<URL, Error>) {
        switch result {
        case .failure(let error):
            finance.problemText = error.localizedDescription
        case .success(let url):
            let isScoped = url.startAccessingSecurityScopedResource()
            defer { if isScoped { url.stopAccessingSecurityScopedResource() } }
            do {
                let data = try Data(contentsOf: url)
                guard data.count <= Self.maximumFileBytes else {
                    finance.problemText = String(localized: "That file is too large to be transaction rows.")
                    return
                }
                guard let variables = TransactionRowsFile.variables(from: data) else {
                    finance.problemText = String(localized: "That file is not a set of transaction rows.")
                    return
                }
                fileName = url.lastPathComponent
                Task { await choose(variables) }
            } catch {
                finance.problemText = error.localizedDescription
            }
        }
    }

    /// Takes the file's rows and asks the server what importing them would do.
    private func choose(_ variables: [String: JSONValue]) async {
        arguments = variables
        preview = nil
        isWorking = true
        do {
            preview = try await finance.connection.previewImportTransactions(variables)
        } catch {
            finance.problemText = error.localizedDescription
        }
        isWorking = false
    }

    private func importRows() {
        guard let arguments else { return }
        Task {
            isWorking = true
            do {
                let imported = try await finance.connection.importTransactions(arguments)
                finance.doneText = StatementImportWords.summary(imported)
                await finance.load()
                onImported()
                dismiss()
            } catch {
                finance.problemText = error.localizedDescription
            }
            isWorking = false
        }
    }
}

/// ImportTransactionRowsLink is the row under the statement import that opens
/// the import of transaction rows.
struct ImportTransactionRowsLink: View {
    let onImported: () -> Void

    var body: some View {
        NavigationLink {
            ImportTransactionRowsView(onImported: onImported)
        } label: {
            Label("Import transaction rows", systemImage: "square.and.arrow.down")
                .foregroundStyle(.primary)
        }
    }
}
