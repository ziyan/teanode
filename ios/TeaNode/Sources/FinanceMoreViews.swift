import Charts
import SwiftUI
import TeaNodeKit

// MARK: Budgets, categories and rules

/// BudgetsView is the month's saving, the budgets in force and to come,
/// the categories and the rules that file transactions.
struct BudgetsView: View {
    @Environment(FinanceModel.self) private var finance
    @State private var saving: SavingSummary?
    @State private var budgets: [Budget] = []
    @State private var rules: [SpendingRule] = []
    @State private var editingBudget: BudgetDraft?
    @State private var editingCategory: CategoryDraft?
    @State private var editingRule: RuleDraft?
    @State private var isSuggestingRules = false

    var body: some View {
        List {
            if let saving {
                Section("This month") {
                    MoneyRow(title: String(localized: "Income"), amount: saving.incomeAmount, of: saving.expectedIncomeAmount, currency: saving.reportingCurrencyCode)
                    MoneyRow(title: String(localized: "Spending"), amount: saving.spendingAmount, of: saving.expectedSpendingAmount, currency: saving.reportingCurrencyCode)
                    MoneyRow(title: String(localized: "Saved"), amount: saving.savingAmount, of: saving.expectedSavingAmount, currency: saving.reportingCurrencyCode)
                }
            }
            Section {
                ForEach(currentBudgets, id: \.id) { budget in
                    Button {
                        editingBudget = BudgetDraft(budget: budget, month: finance.month(Date()))
                    } label: {
                        LabeledContent {
                            VStack(alignment: .trailing) {
                                Text(FinanceMoney.shown(budget.monthlyAmount, currencyCode: budget.currencyCode)).monospacedDigit()
                                Text(budget.effectiveFrom > finance.month(Date()) ? String(localized: "from \(budget.effectiveFrom), scheduled") : String(localized: "from \(budget.effectiveFrom)"))
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        } label: {
                            Text(finance.categoryName(budget.spendingCategoryId))
                        }
                    }
                    .foregroundStyle(.primary)
                }
                Button("Set a budget", systemImage: "plus") {
                    editingBudget = BudgetDraft(month: finance.month(Date()), currency: finance.reportingCurrency)
                }
            } header: {
                Text("Budgets")
            }
            Section("Categories") {
                ForEach(finance.categories.sorted { ($0.isTransfer == true || $0.isOther == true ? 1 : 0, finance.label($0)) < ($1.isTransfer == true || $1.isOther == true ? 1 : 0, finance.label($1)) }) { category in
                    Button {
                        editingCategory = CategoryDraft(category)
                    } label: {
                        HStack {
                            Text(finance.label(category))
                            Spacer()
                            if category.isIncome == true { Chip(text: String(localized: "Income"), isAlert: false) }
                            if category.isHidden == true { Chip(text: String(localized: "Hidden"), isAlert: false) }
                        }
                    }
                    .foregroundStyle(.primary)
                }
                Button("New category", systemImage: "plus") { editingCategory = CategoryDraft() }
            }
            Section {
                ForEach(rules.sorted { ($0.rulePriority ?? 0) < ($1.rulePriority ?? 0) }) { rule in
                    Button {
                        editingRule = RuleDraft(rule)
                    } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("“\(rule.matchText)”")
                            Text(finance.categoryName(rule.spendingCategoryId)).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .foregroundStyle(.primary)
                    .swipeActions {
                        Button("Delete", systemImage: "trash", role: .destructive) {
                            Task {
                                _ = await finance.act(saying: String(localized: "Rule deleted")) { try await finance.connection.deleteSpendingRule(rule.id) }
                                await load()
                            }
                        }
                        .tint(.red)
                    }
                }
                Button("New rule", systemImage: "plus") { editingRule = RuleDraft() }
                Button("Suggest rules", systemImage: "sparkles") { isSuggestingRules = true }
            } header: {
                Text("Rules")
            } footer: {
                Text("A rule files every transaction whose merchant or description has its words, ignoring case.")
            }
        }
        .navigationTitle("Budgets")
        .task { await load() }
        .refreshable { await load() }
        .sheet(item: $editingBudget) { draft in
            BudgetEditor(draft: draft) { Task { await load() } }
        }
        .sheet(item: $editingCategory) { draft in
            CategoryEditor(draft: draft) { Task { await finance.load(); await load() } }
        }
        .sheet(item: $editingRule) { draft in
            RuleEditor(draft: draft) { Task { await load() } }
        }
        .sheet(isPresented: $isSuggestingRules) {
            SuggestRulesSheet { Task { await load() } }
        }
    }

    /// Each category's budget in force this month, and those set for later;
    /// a budget of zero has ended.
    private var currentBudgets: [Budget] {
        let thisMonth = finance.month(Date())
        let byCategory = Dictionary(grouping: budgets, by: \.spendingCategoryId)
        return byCategory.values.flatMap { rows -> [Budget] in
            let sorted = rows.sorted { $0.effectiveFrom < $1.effectiveFrom }
            let current = sorted.last { $0.effectiveFrom <= thisMonth }
            let later = sorted.filter { $0.effectiveFrom > thisMonth }
            return ([current].compactMap { $0 } + later).filter { (FinanceMoney.decimal($0.monthlyAmount) ?? 0) != 0 }
        }
        .sorted { finance.categoryName($0.spendingCategoryId) < finance.categoryName($1.spendingCategoryId) }
    }

    private func load() async {
        let connection = finance.connection
        saving = try? await connection.savingSummary(month: finance.month(Date()))
        budgets = (try? await connection.budgets()) ?? []
        rules = (try? await connection.spendingRules()) ?? []
    }
}

struct BudgetDraft: Identifiable {
    let id = UUID()
    var categoryId = ""
    var amount = ""
    var currency = ""
    var effectiveFrom = ""
    var isEditing = false

    init(month: String, currency: String) {
        effectiveFrom = month
        self.currency = currency
    }

    init(budget: Budget, month: String) {
        categoryId = budget.spendingCategoryId
        amount = budget.monthlyAmount
        currency = budget.currencyCode ?? ""
        effectiveFrom = max(month, budget.effectiveFrom)
        isEditing = true
    }
}

/// BudgetEditor sets a category's monthly budget from a month on, or ends
/// it this month.
struct BudgetEditor: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    @State var draft: BudgetDraft
    let onSaved: () -> Void

    var body: some View {
        NavigationStack {
            Form {
                Picker("Category", selection: $draft.categoryId) {
                    Text("Choose").tag("")
                    ForEach(finance.pickable(keeping: draft.categoryId).filter { $0.isTransfer != true }) { category in
                        Text(finance.label(category)).tag(category.id)
                    }
                }
                .disabled(draft.isEditing)
                TextField("Monthly amount", text: $draft.amount).keyboardType(.decimalPad)
                TextField("Currency", text: $draft.currency).textInputAutocapitalization(.characters)
                TextField("From month (2026-10)", text: $draft.effectiveFrom).keyboardType(.numbersAndPunctuation)
                if draft.effectiveFrom < finance.month(Date()) {
                    Text("This changes months already past.").font(.footnote).foregroundStyle(.orange)
                }
                if draft.isEditing {
                    Button("End this budget", role: .destructive) {
                        save(amount: "0", from: finance.month(Date()))
                    }
                }
            }
            .navigationTitle(draft.isEditing ? String(localized: "Change budget") : String(localized: "Set a budget"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save(amount: draft.amount.trimmed, from: draft.effectiveFrom.trimmed) }
                        .disabled(draft.categoryId.isEmpty || !FinanceMoney.isDecimal(draft.amount.trimmed) || draft.amount.hasPrefix("-"))
                }
            }
        }
    }

    private func save(amount: String, from month: String) {
        let categoryId = draft.categoryId
        let currency = draft.currency.trimmed.uppercased()
        Task {
            let isSaved = await finance.act(saying: amount == "0" ? String(localized: "Budget ended") : String(localized: "Budget set")) {
                try await finance.connection.setBudget(categoryId: categoryId, monthlyAmount: amount, currencyCode: currency, effectiveFrom: month)
            }
            if isSaved {
                onSaved()
                dismiss()
            }
        }
    }
}

struct CategoryDraft: Identifiable {
    let id = UUID()
    var categoryId: String?
    var name = ""
    var parentId = ""
    var isIncome = false
    var isHidden = false
    var isBuiltIn = false

    init() {}

    init(_ category: SpendingCategory) {
        categoryId = category.id
        name = category.spendingCategoryName
        parentId = category.parentSpendingCategoryId ?? ""
        isIncome = category.isIncome ?? false
        isHidden = category.isHidden ?? false
        isBuiltIn = category.isTransfer == true || category.isOther == true
    }
}

/// CategoryEditor adds a category or changes one: its name, the category
/// above it, whether it is income, and whether it is hidden.
struct CategoryEditor: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    @State var draft: CategoryDraft
    let onSaved: () -> Void
    @State private var isConfirmingDelete = false

    var body: some View {
        NavigationStack {
            Form {
                TextField("Name", text: $draft.name)
                if !draft.isBuiltIn {
                    Picker("Under", selection: $draft.parentId) {
                        Text("Nothing").tag("")
                        ForEach(finance.categories.filter { ($0.parentSpendingCategoryId ?? "").isEmpty && $0.id != draft.categoryId && $0.isTransfer != true && $0.isOther != true }) { category in
                            Text(FinanceCategoryNames.shown(category)).tag(category.id)
                        }
                    }
                    Toggle("Income", isOn: $draft.isIncome)
                }
                Toggle("Hidden", isOn: $draft.isHidden)
                if draft.categoryId != nil, !draft.isBuiltIn {
                    Button("Delete category", role: .destructive) { isConfirmingDelete = true }
                    .confirmationDialog("Delete \(draft.name)?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                        Button("Delete category", role: .destructive) {
                            guard let categoryId = draft.categoryId else { return }
                            Task {
                                let isDeleted = await finance.act(saying: String(localized: "Category deleted")) {
                                    try await finance.connection.deleteSpendingCategory(categoryId)
                                }
                                if isDeleted {
                                    onSaved()
                                    dismiss()
                                }
                            }
                        }
                    } message: {
                        Text("Its transactions become uncategorized, and its budget and rules are deleted.")
                    }
                }
            }
            .navigationTitle(draft.categoryId == nil ? String(localized: "New category") : String(localized: "Edit category"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }.disabled(draft.name.trimmed.isEmpty)
                }
            }
        }
    }

    private func save() {
        let saving = draft
        Task {
            let isSaved = await finance.act(saying: String(localized: "Saved")) {
                try await finance.connection.saveSpendingCategory(
                    saving.categoryId, name: saving.name.trimmed, parentId: saving.parentId, isIncome: saving.isIncome, isHidden: saving.isHidden)
            }
            if isSaved {
                onSaved()
                dismiss()
            }
        }
    }
}

struct RuleDraft: Identifiable {
    let id = UUID()
    var ruleId: String?
    var matchText = ""
    var categoryId = ""
    var accountId = ""
    var minimum = ""
    var maximum = ""
    var priority = ""

    init() {}

    init(_ rule: SpendingRule) {
        ruleId = rule.id
        matchText = rule.matchText
        categoryId = rule.spendingCategoryId ?? ""
        accountId = rule.financeAccountId ?? ""
        minimum = rule.minimumAmount ?? ""
        maximum = rule.maximumAmount ?? ""
        priority = rule.rulePriority.map(String.init) ?? ""
    }
}

/// RuleEditor adds a rule or changes one: the words it looks for, the
/// category it files under, and optionally an account, amounts and order.
struct RuleEditor: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    @State var draft: RuleDraft
    let onSaved: () -> Void

    var body: some View {
        NavigationStack {
            Form {
                TextField("Words to match", text: $draft.matchText)
                Picker("Category", selection: $draft.categoryId) {
                    Text("Choose").tag("")
                    ForEach(finance.pickable(keeping: draft.categoryId)) { category in
                        Text(finance.label(category)).tag(category.id)
                    }
                }
                Picker("Account", selection: $draft.accountId) {
                    Text("All accounts").tag("")
                    ForEach(finance.accounts) { account in Text(account.shownName).tag(account.id) }
                }
                Section("Optional") {
                    TextField("At least", text: $draft.minimum).keyboardType(.decimalPad)
                    TextField("At most", text: $draft.maximum).keyboardType(.decimalPad)
                    TextField("Order (empty is last)", text: $draft.priority).keyboardType(.numberPad)
                }
            }
            .navigationTitle(draft.ruleId == nil ? String(localized: "New rule") : String(localized: "Edit rule"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(draft.matchText.trimmed.isEmpty || draft.categoryId.isEmpty
                            || (!draft.minimum.isEmpty && !FinanceMoney.isDecimal(draft.minimum)) || (!draft.maximum.isEmpty && !FinanceMoney.isDecimal(draft.maximum)))
                }
            }
        }
    }

    private func save() {
        let saving = draft
        Task {
            let isSaved = await finance.act(saying: String(localized: "Saved")) {
                try await finance.connection.saveSpendingRule(
                    saving.ruleId, matchText: saving.matchText.trimmed, categoryId: saving.categoryId, accountId: saving.accountId,
                    minimum: saving.minimum.trimmed, maximum: saving.maximum.trimmed, priority: Int(saving.priority))
            }
            if isSaved {
                onSaved()
                dismiss()
            }
        }
    }
}

// MARK: Net worth

/// NetWorthPlot is one day of the net worth line, as drawn.
private struct NetWorthPlot: Identifiable {
    let netWorthOn: String
    let date: Date
    let amountValue: Double
    let amountText: String?
    var id: String { netWorthOn }
}

/// NetWorthView is the net worth over a range, and what is owned and owed
/// by kind, each asset with its values.
struct NetWorthView: View {
    @Environment(FinanceModel.self) private var finance
    @State private var days = 90
    @State private var netWorth: NetWorth?
    @State private var assets: [FinanceAsset] = []
    @State private var isShowingClosed = false
    @State private var editingAsset: AssetDraft?
    /// Where a finger is on the net worth line.
    @State private var selectedDate: Date?

    var body: some View {
        List {
            Section {
                Picker("Range", selection: $days) {
                    Text("30 days").tag(30)
                    Text("90 days").tag(90)
                    Text("A year").tag(365)
                    Text("All").tag(3650)
                }
                .pickerStyle(.segmented)
                let points = (netWorth?.convertedNetWorthPoints ?? []).compactMap { $0 }
                if let last = points.last {
                    // The total, its change and the line are one row, so the
                    // callout of a touched day takes the total's place, as in
                    // Stocks.
                    VStack(alignment: .leading, spacing: 4) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(FinanceMoney.shown(last.netWorthAmount, currencyCode: netWorth?.reportingCurrencyCode))
                                .font(.title.weight(.semibold))
                                .monospacedDigit()
                            if let first = points.first, let start = FinanceMoney.decimal(first.netWorthAmount), let end = FinanceMoney.decimal(last.netWorthAmount) {
                                let change = end - start
                                Text("\(change >= 0 ? "▲" : "▼") \(FinanceMoney.shown(NSDecimalNumber(decimal: abs(change)).stringValue, currencyCode: netWorth?.reportingCurrencyCode)) since \(first.netWorthOn)")
                                    .font(.footnote)
                                    .foregroundStyle(change >= 0 ? .green : .red)
                            }
                        }
                        .opacity(selectedDate == nil ? 1 : 0)
                        netWorthChart(points)
                            .padding(.top, 8)
                    }
                } else {
                    Text("No net worth yet.").foregroundStyle(.secondary)
                }
            }
            let shown = assets.filter { isShowingClosed || !$0.isClosed }
            ForEach(FinanceAsset.ownedKinds + FinanceAsset.owedKinds, id: \.self) { kind in
                let inKind = shown.filter { $0.assetKind == kind }
                if !inKind.isEmpty {
                    Section(AssetWords.kind(kind)) {
                        ForEach(inKind) { asset in
                            NavigationLink {
                                AssetDetail(asset: asset).environment(finance)
                            } label: {
                                LabeledContent {
                                    Text(FinanceMoney.shown(asset.latestValuation?.value, currencyCode: asset.currencyCode))
                                        .monospacedDigit()
                                        .foregroundStyle(asset.isLiability == true ? AnyShapeStyle(.red) : AnyShapeStyle(.primary))
                                } label: {
                                    VStack(alignment: .leading) {
                                        Text(asset.assetName)
                                        if asset.isClosed { Text("Closed").font(.caption).foregroundStyle(.secondary) }
                                    }
                                }
                            }
                        }
                    }
                }
            }
            if assets.contains(where: \.isClosed) {
                Toggle("Show closed", isOn: $isShowingClosed)
            }
        }
        .navigationTitle("Net worth")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    editingAsset = AssetDraft(currency: finance.reportingCurrency)
                } label: {
                    Label("Add", systemImage: "plus")
                }
            }
        }
        .task(id: days) {
            await load()
            #if DEBUG
                // A screenshot of a touched day (`-TeaNodeScreen net-worth-chosen`).
                if DemoServer.screen == "net-worth-chosen", let points = netWorth?.convertedNetWorthPoints?.compactMap({ $0 }), points.count > 3 {
                    selectedDate = finance.date(ofDay: points[points.count * 3 / 5].netWorthOn)
                }
            #endif
        }
        .refreshable { await load() }
        .sheet(item: $editingAsset) { draft in
            AssetEditor(draft: draft) { Task { await load() } }
        }
    }

    /// The line over the range, its axis in the reporting currency.
    /// Touching it shows the nearest day's net worth.
    private func netWorthChart(_ points: [NetWorth.Point]) -> some View {
        let currencyCode = netWorth?.reportingCurrencyCode
        let plots = points.compactMap { point -> NetWorthPlot? in
            guard let date = finance.date(ofDay: point.netWorthOn) else { return nil }
            let amountValue = FinanceMoney.decimal(point.netWorthAmount).map { NSDecimalNumber(decimal: $0).doubleValue } ?? 0
            return NetWorthPlot(netWorthOn: point.netWorthOn, date: date, amountValue: amountValue, amountText: point.netWorthAmount)
        }
        let selectedPlot = selectedDate.flatMap { date in
            ChartShares.nearestIndex(to: date, in: plots.map(\.date)).map { plots[$0] }
        }
        let dayStyle = Date.FormatStyle(date: .abbreviated, time: .omitted, timeZone: finance.dates.timeZone)
        return Chart {
            ForEach(plots) { plot in
                LineMark(x: .value("Day", plot.date), y: .value("Net worth", plot.amountValue))
                    .foregroundStyle(ChartPalette.seriesColor(at: 0))
                    .accessibilityLabel(Text(verbatim: plot.date.formatted(dayStyle)))
                    .accessibilityValue(Text(verbatim: FinanceMoney.shown(plot.amountText, currencyCode: currencyCode)))
            }
            if let selectedPlot {
                PointMark(x: .value("Day", selectedPlot.date), y: .value("Net worth", selectedPlot.amountValue))
                    .foregroundStyle(ChartPalette.seriesColor(at: 0))
                    .symbolSize(60)
                ChartSelectionRule(value: selectedPlot.date) {
                    ChartCallout(
                        title: selectedPlot.date.formatted(dayStyle),
                        figures: [ChartFigure(valueText: FinanceMoney.shown(selectedPlot.amountText, currencyCode: currencyCode))])
                }
            }
        }
        .chartXSelection(value: $selectedDate)
        .keepsSidePanelClosed()
        // Not from zero: a net worth moves by a little of itself, and from
        // zero every line would look flat.
        .chartYScale(domain: .automatic(includesZero: false))
        .chartXAxis {
            AxisMarks(values: .automatic(desiredCount: 4)) { _ in
                AxisGridLine()
                AxisValueLabel(format: .dateTime.month(.abbreviated).day())
            }
        }
        .chartYAxis {
            AxisMarks { value in
                AxisGridLine()
                AxisValueLabel {
                    if let number = value.as(Double.self) { Text(verbatim: FinanceMoney.axisText(number, currencyCode: currencyCode)) }
                }
            }
        }
        .frame(height: 180)
    }

    private func load() async {
        let from = finance.day(finance.dates.date(byAdding: .day, value: -days, to: Date()) ?? Date())
        netWorth = try? await finance.connection.netWorth(from: from, to: finance.today)
        assets = (try? await finance.connection.assets()) ?? []
    }
}

enum AssetWords {
    static func kind(_ kind: String) -> String {
        switch kind {
        case "cash": String(localized: "Cash")
        case "investment": String(localized: "Investments")
        case "retirement": String(localized: "Retirement")
        case "property": String(localized: "Property")
        case "vehicle": String(localized: "Vehicles")
        case "other_asset": String(localized: "Other assets")
        case "credit_card": String(localized: "Credit cards")
        case "loan": String(localized: "Loans")
        case "mortgage": String(localized: "Mortgages")
        case "other_liability": String(localized: "Other debts")
        default: kind
        }
    }
}

/// AssetDetail is an asset's values over time, adding a value, closing,
/// reopening and deleting it.
struct AssetDetail: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    @State var asset: FinanceAsset
    @State private var history: [FinanceAsset.Valuation] = []
    @State private var newValue = ""
    @State private var note = ""
    @State private var isConfirmingDelete = false

    var body: some View {
        List {
            Section {
                LabeledContent("Kind", value: AssetWords.kind(asset.assetKind))
                LabeledContent("Currency", value: asset.currencyCode ?? "")
                if let security = asset.financeSecurity {
                    LabeledContent("Security", value: [security.tickerSymbol, security.securityName].compactMap { $0 }.joined(separator: " · "))
                }
                if let description = asset.estimateDescription, !description.isEmpty {
                    Text(description).font(.footnote).foregroundStyle(.secondary)
                }
            }
            if !asset.isClosed, asset.valuationSource != "finance_sync" {
                Section("Add a value") {
                    TextField("Value", text: $newValue).keyboardType(.decimalPad)
                    TextField("Note (optional)", text: $note)
                    Button("Add value") { Task { await addValue() } }
                        .disabled(!FinanceMoney.isDecimal(newValue.trimmed))
                }
            }
            Section("History") {
                ForEach(history) { valuation in
                    LabeledContent {
                        Text(FinanceMoney.shown(valuation.value, currencyCode: valuation.currencyCode)).monospacedDigit()
                    } label: {
                        VStack(alignment: .leading) {
                            Text(valuation.valuedOn ?? "")
                            if let note = valuation.valuationNote, !note.isEmpty {
                                Text(note).font(.caption).foregroundStyle(.secondary)
                            }
                        }
                    }
                    .swipeActions {
                        Button("Delete", systemImage: "trash", role: .destructive) {
                            Task {
                                _ = await finance.act(saying: String(localized: "Value deleted")) { try await finance.connection.deleteValuation(valuation.id) }
                                await load()
                            }
                        }
                        .tint(.red)
                    }
                }
            }
            Section {
                Button(asset.isClosed ? LocalizedStringKey("Reopen") : "Close") {
                    let assetId = asset.id
                    let isReopened = asset.isClosed
                    let today = finance.today
                    Task {
                        _ = await finance.act(saying: isReopened ? String(localized: "Reopened") : String(localized: "Closed")) {
                            try await finance.connection.closeAsset(assetId, closedOn: today, isReopened: isReopened)
                        }
                        dismiss()
                    }
                }
                Button("Delete asset", role: .destructive) { isConfirmingDelete = true }
                .confirmationDialog("Delete \(asset.assetName)?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                    Button("Delete asset", role: .destructive) {
                        let assetId = asset.id
                        Task {
                            let isDeleted = await finance.act(saying: String(localized: "Asset deleted")) { try await finance.connection.deleteAsset(assetId) }
                            if isDeleted { dismiss() }
                        }
                    }
                } message: {
                    Text("Its \(history.count) values go with it.")
                }
            }
        }
        .navigationTitle(asset.assetName)
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        history = (try? await finance.connection.assetHistory(asset.id)) ?? []
    }

    private func addValue() async {
        let value = newValue.trimmed
        let written = note.trimmed
        let assetId = asset.id
        let today = finance.today
        let isAdded = await finance.act(saying: String(localized: "Value added")) {
            try await finance.connection.recordValuation(assetId: assetId, value: value, valuedOn: today, note: written)
        }
        if isAdded {
            newValue = ""
            note = ""
            await load()
        }
    }
}

struct AssetDraft: Identifiable {
    let id = UUID()
    var name = ""
    var kind = "vehicle"
    var currency = ""
    var valuationSource = "manual"
    var estimateDescription = ""
    var value = ""

    init(currency: String) {
        self.currency = currency.isEmpty ? "USD" : currency
    }
}

/// AssetEditor adds an asset: what it is, its currency, how it is valued
/// and its first value.
struct AssetEditor: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    @State var draft: AssetDraft
    let onSaved: () -> Void

    var body: some View {
        NavigationStack {
            Form {
                TextField("Name", text: $draft.name)
                Picker("Kind", selection: $draft.kind) {
                    ForEach(FinanceAsset.ownedKinds + FinanceAsset.owedKinds, id: \.self) { kind in
                        Text(AssetWords.kind(kind)).tag(kind)
                    }
                }
                TextField("Currency", text: $draft.currency).textInputAutocapitalization(.characters)
                Picker("Valued by", selection: $draft.valuationSource) {
                    Text("You").tag("manual")
                    Text("The agent's estimate").tag("agent_estimate")
                    Text("The agent reading it").tag("agent_reading")
                }
                if draft.valuationSource == "agent_estimate" {
                    TextField("What to estimate from", text: $draft.estimateDescription, axis: .vertical)
                }
                TextField("Value now (optional)", text: $draft.value).keyboardType(.decimalPad)
            }
            .navigationTitle("New asset")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(draft.name.trimmed.isEmpty || (!draft.value.isEmpty && !FinanceMoney.isDecimal(draft.value.trimmed))
                            || (draft.valuationSource == "agent_estimate" && draft.estimateDescription.trimmed.isEmpty))
                }
            }
        }
    }

    private func save() {
        var fields: [String: JSONValue] = [
            "assetName": .string(draft.name.trimmed), "assetKind": .string(draft.kind), "currencyCode": .string(draft.currency.trimmed.uppercased()),
            "valuationSource": .string(draft.valuationSource), "estimateDescription": .string(draft.estimateDescription.trimmed),
            "isEstimateAllowed": .bool(draft.valuationSource == "agent_estimate"),
        ]
        if !draft.value.trimmed.isEmpty {
            fields["value"] = .string(draft.value.trimmed)
            fields["valuedOn"] = .string(finance.today)
        }
        let saving = fields
        Task {
            let isSaved = await finance.act(saying: String(localized: "Asset added")) {
                try await finance.connection.saveAsset(nil, fields: saving)
            }
            if isSaved {
                onSaved()
                dismiss()
            }
        }
    }
}

// MARK: Savings targets

/// SavingsTargetsView is each savings target: how much is saved, by when,
/// whether it is behind, and what a month needs.
struct SavingsTargetsView: View {
    @Environment(FinanceModel.self) private var finance
    @State private var targets: [SavingsTarget] = []
    @State private var isShowingClosed = false
    @State private var isAdding = false

    var body: some View {
        List {
            ForEach(targets.filter { isShowingClosed || ($0.savingsTarget?.closedOn ?? "").isEmpty }) { target in
                if let goal = target.savingsTarget {
                    VStack(alignment: .leading, spacing: 6) {
                        HStack {
                            Text(goal.savingsTargetName).font(.headline)
                            Spacer()
                            if target.savingsTargetProgress?.isBehind == true {
                                Text("Behind").font(.caption.weight(.medium)).foregroundStyle(.orange)
                            }
                            if !(goal.closedOn ?? "").isEmpty {
                                Text("Closed").font(.caption).foregroundStyle(.secondary)
                            }
                        }
                        let saved = FinanceMoney.decimal(target.savingsTargetProgress?.savedAmount) ?? 0
                        let wanted = FinanceMoney.decimal(goal.targetAmount) ?? 1
                        ProgressView(value: min(1, max(0, NSDecimalNumber(decimal: saved / (wanted == 0 ? 1 : wanted)).doubleValue)))
                            .tint(target.savingsTargetProgress?.isBehind == true ? .orange : .accentColor)
                        Text("\(FinanceMoney.shown(target.savingsTargetProgress?.savedAmount, currencyCode: goal.currencyCode)) of \(FinanceMoney.shown(goal.targetAmount, currencyCode: goal.currencyCode)) by \(goal.targetOn ?? "")")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                        if let monthly = target.savingsTargetProgress?.requiredMonthlyAmount, (FinanceMoney.decimal(monthly) ?? 0) > 0 {
                            Text("Needs \(FinanceMoney.shown(monthly, currencyCode: goal.currencyCode)) a month").font(.footnote).foregroundStyle(.secondary)
                        }
                    }
                    .swipeActions {
                        let isClosed = !(goal.closedOn ?? "").isEmpty
                        Button(isClosed ? LocalizedStringKey("Reopen") : "Close") {
                            Task {
                                _ = await finance.act(saying: isClosed ? String(localized: "Reopened") : String(localized: "Closed")) {
                                    try await finance.connection.closeSavingsTarget(goal.id, isReopened: isClosed)
                                }
                                await load()
                            }
                        }
                        .tint(.indigo)
                    }
                }
            }
            if targets.contains(where: { !($0.savingsTarget?.closedOn ?? "").isEmpty }) {
                Toggle("Show closed", isOn: $isShowingClosed)
            }
        }
        .navigationTitle("Savings targets")
        .overlay {
            if targets.isEmpty {
                ContentUnavailableView("No savings targets", systemImage: "target", description: Text("Add one with +."))
            }
        }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    isAdding = true
                } label: {
                    Label("Add", systemImage: "plus")
                }
            }
        }
        .task { await load() }
        .refreshable { await load() }
        .sheet(isPresented: $isAdding) {
            SavingsTargetEditor { Task { await load() } }
        }
    }

    private func load() async {
        targets = (try? await finance.connection.savingsTargets()) ?? []
    }
}

/// SavingsTargetEditor adds a target: how much, by when, and how it is
/// measured.
struct SavingsTargetEditor: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let onSaved: () -> Void
    @State private var name = ""
    @State private var amount = ""
    @State private var targetOn = Date().addingTimeInterval(86400 * 365)
    @State private var measure = "cash_flow"

    var body: some View {
        NavigationStack {
            Form {
                TextField("Name", text: $name)
                TextField("Amount", text: $amount).keyboardType(.decimalPad)
                DatePicker("By", selection: $targetOn, in: Date()..., displayedComponents: .date)
                Picker("Measured by", selection: $measure) {
                    Text("What is left each month").tag("cash_flow")
                    Text("Net worth").tag("net_worth")
                }
            }
            .navigationTitle("New savings target")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { save() }
                        .disabled(name.trimmed.isEmpty || !FinanceMoney.isDecimal(amount.trimmed) || (FinanceMoney.decimal(amount.trimmed) ?? 0) <= 0)
                }
            }
        }
    }

    private func save() {
        let fields: [String: JSONValue] = [
            "savingsTargetName": .string(name.trimmed), "targetAmount": .string(amount.trimmed), "targetOn": .string(finance.day(targetOn)),
            "targetMeasure": .string(measure), "startedOn": .string(finance.today), "assetIds": .array([]), "financeAccountIds": .array([]),
        ]
        Task {
            let isSaved = await finance.act(saying: String(localized: "Savings target added")) {
                try await finance.connection.saveSavingsTarget(nil, fields: fields)
            }
            if isSaved {
                onSaved()
                dismiss()
            }
        }
    }
}

// MARK: Receipts

/// ReceiptsView is every receipt read, matched or not, searched.
struct ReceiptsView: View {
    @Environment(FinanceModel.self) private var finance
    @State private var receipts: [FinanceReceipt] = []
    @State private var isUnmatchedOnly = false
    @State private var searchText = ""
    @State private var isLoaded = false
    @State private var isTypingReceipt = false

    var body: some View {
        List {
            Toggle("Only unmatched", isOn: $isUnmatchedOnly)
            ForEach(receipts) { receipt in
                NavigationLink {
                    ReceiptDetailView(receipt: receipt) { Task { await load() } }
                } label: {
                    ReceiptRow(receipt: receipt)
                }
                .swipeActions {
                    Button("Delete", systemImage: "trash", role: .destructive) {
                        Task {
                            _ = await finance.act(saying: String(localized: "Receipt deleted")) { try await finance.connection.deleteReceipt(receipt.id) }
                            await load()
                        }
                    }
                    .tint(.red)
                }
            }
        }
        .navigationTitle("Receipts")
        .searchable(text: $searchText, prompt: "Merchant")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    isTypingReceipt = true
                } label: {
                    Label("Type in a receipt", systemImage: "plus")
                }
            }
        }
        .sheet(isPresented: $isTypingReceipt) {
            ReceiptEntryView(currencyCode: finance.reportingCurrency, today: finance.today) { Task { await load() } }
        }
        .task(id: "\(isUnmatchedOnly)-\(searchText)") { await load() }
        .refreshable { await load() }
        .overlay {
            if isLoaded, receipts.isEmpty {
                ContentUnavailableView("No receipts", systemImage: "receipt", description: Text("Add a receipt from a charge, or send one to your agent."))
            }
        }
    }

    private func load() async {
        receipts = (try? await finance.connection.financeReceipts(isUnmatched: isUnmatchedOnly, text: searchText.trimmed, limit: 100).financeReceipts.compactMap { $0 }) ?? []
        isLoaded = true
    }
}

/// ReceiptRow is a receipt in a list: where and when, what it came to,
/// whether its lines add up, and how far it is matched to charges.
struct ReceiptRow: View {
    let receipt: FinanceReceipt

    var body: some View {
        // LabeledContent centers the total on the whole row, level with the
        // disclosure chevron, rather than on the merchant's line.
        LabeledContent {
            Text(FinanceMoney.shown(receipt.totalAmount, currencyCode: receipt.currencyCode))
                .monospacedDigit()
                .foregroundStyle(Color(.label))
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(receipt.merchantName.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "Receipt"))
                    .foregroundStyle(Color(.label))
                HStack(spacing: 6) {
                    Text(receipt.purchasedOn.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "No day"))
                    let lineCount = (receipt.receiptLines ?? []).compactMap { $0 }.count
                    if lineCount > 0 {
                        Text("\(lineCount) lines")
                    }
                    let matches = (receipt.receiptMatches ?? []).compactMap { $0 }
                    Text(matches.isEmpty ? String(localized: "Not matched") : String(localized: "Matched to \(matches.count) charges"))
                    if receipt.agentAttachmentId?.isEmpty == false {
                        Image(systemName: "photo").accessibilityLabel("Has its photo")
                    }
                }
                .font(.caption)
                .foregroundStyle(Color(.secondaryLabel))
            }
        }
    }
}

/// ReceiptDetailView is one receipt, as the dashboard shows it: the photo it
/// was read from, opened full size with a tap; what it says about the
/// purchase; every line; and what it is matched to. A receipt opened from a
/// charge can be taken off it.
struct ReceiptDetailView: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let receipt: FinanceReceipt
    /// The charge it was opened from, if any.
    var transactionId: String?
    let onChange: () -> Void
    @State private var isConfirmingDelete = false
    @State private var memory = InMemoryModel()

    var body: some View {
        List {
            if let attachmentId = receipt.agentAttachmentId, !attachmentId.isEmpty {
                Section {
                    RemotePicture(path: "/api/v1/agent/attachments/\(attachmentId)", name: "receipt.jpg", maxHeight: 360)
                        .frame(maxWidth: .infinity)
                } footer: {
                    Text("Tap the photo to see it full size.")
                }
            }
            Section {
                LabeledContent("Merchant", value: receipt.merchantName ?? "")
                if let purchasedOn = receipt.purchasedOn, !purchasedOn.isEmpty {
                    LabeledContent("Bought on", value: purchasedOn)
                }
                if let number = receipt.merchantReceiptNumber, !number.isEmpty {
                    LabeledContent("Receipt number", value: number)
                }
                if let subtotal = receipt.subtotalAmount, !subtotal.isEmpty {
                    LabeledContent("Subtotal", value: FinanceMoney.shown(subtotal, currencyCode: receipt.currencyCode))
                }
                LabeledContent("Total", value: FinanceMoney.shown(receipt.totalAmount, currencyCode: receipt.currencyCode))
                if receipt.receiptCheckState == "balanced" {
                    Label("The lines add up", systemImage: "checkmark.seal")
                        .foregroundStyle(Color(.secondaryLabel))
                } else if let difference = receipt.checkDifferenceAmount, !difference.isEmpty {
                    Label("The lines are off by \(FinanceMoney.shown(difference, currencyCode: receipt.currencyCode))", systemImage: "exclamationmark.triangle")
                        .foregroundStyle(.orange)
                }
            }
            let lines = (receipt.receiptLines ?? []).compactMap { $0 }
            if !lines.isEmpty {
                Section("Lines") {
                    ForEach(lines, id: \.id) { line in
                        HStack(alignment: .firstTextBaseline) {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(line.description.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "No description"))
                                // The server keeps quantities to eight places:
                                // "1.00000000" is one, said by not saying it.
                                if let quantity = line.quantity.flatMap({ Decimal(string: $0, locale: Locale(identifier: "en_US_POSIX")) }), quantity != 1 {
                                    let unitPrice = FinanceMoney.shown(line.unitPriceAmount, currencyCode: receipt.currencyCode)
                                    let unitText = line.quantityUnit.map { " \($0)" } ?? ""
                                    Text(verbatim: "\(quantity.formatted())\(unitText) × \(unitPrice)")
                                        .font(.caption)
                                        .foregroundStyle(Color(.secondaryLabel))
                                }
                            }
                            Spacer()
                            Text(FinanceMoney.shown(line.lineAmount, currencyCode: receipt.currencyCode))
                                .monospacedDigit()
                        }
                    }
                }
            }
            Section {
                let matches = (receipt.receiptMatches ?? []).compactMap { $0 }
                Text(matches.isEmpty ? String(localized: "Not matched to a charge yet.") : String(localized: "Matched to \(matches.count) charges"))
                    .foregroundStyle(Color(.secondaryLabel))
                if let transactionId {
                    Button("Take off this charge", systemImage: "link.badge.minus") {
                        Task {
                            let isDone = await finance.act(saying: String(localized: "Taken off")) {
                                try await finance.connection.matchReceipt(receipt.id, to: transactionId, amount: nil, isUndone: true)
                            }
                            if isDone {
                                onChange()
                                dismiss()
                            }
                        }
                    }
                }
            }
            InMemorySection(model: memory)
            Section {
                Button(role: .destructive) {
                    isConfirmingDelete = true
                } label: {
                    Label("Delete receipt", systemImage: "trash")
                }
                .tint(.red)
                .confirmationDialog("Delete this receipt?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
                    Button("Delete receipt", role: .destructive) {
                        Task {
                            let isDone = await finance.act(saying: String(localized: "Receipt deleted")) {
                                try await finance.connection.deleteReceipt(receipt.id)
                            }
                            if isDone {
                                onChange()
                                dismiss()
                            }
                        }
                    }
                } message: {
                    Text("Its picture and lines are deleted too.")
                }
            }
        }
        .navigationTitle(receipt.merchantName.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "Receipt"))
        .navigationBarTitleDisplayMode(.inline)
        .inMemory(memory, recordKind: "finance_receipt", recordId: receipt.id)
    }
}

// MARK: Setup

/// FinanceSetupView is the banks linked, syncing, pausing and removing
/// them, linking one through SimpleFIN, and the reporting currency.
struct FinanceSetupView: View {
    @Environment(FinanceModel.self) private var finance
    @State private var sources: [FinanceSource] = []
    @State private var setupToken = ""
    @State private var currency = ""
    @State private var removing: FinanceSource?

    var body: some View {
        List {
            Section("Linked") {
                ForEach(sources) { source in
                    VStack(alignment: .leading, spacing: 4) {
                        HStack {
                            Text(source.institutionName.flatMap { $0.isEmpty ? nil : $0 } ?? source.name ?? String(localized: "Unnamed"))
                            Spacer()
                            if source.isSignInRequired == true { Text("Sign-in required").font(.caption).foregroundStyle(.orange) }
                            if source.isEnabled == false { Text("Off").font(.caption).foregroundStyle(.orange) }
                        }
                        if let lastRun = ServerTime.date(source.lastRunAt) {
                            Text("Synced \(lastRun.formatted(.relative(presentation: .named)))").font(.caption).foregroundStyle(.secondary)
                        }
                        if let error = source.lastError, !error.isEmpty {
                            Text(error.asReasonSentence).font(.caption).foregroundStyle(.red)
                        }
                    }
                    .swipeActions {
                        Button("Remove", systemImage: "trash", role: .destructive) { removing = source }
                            .tint(.red)
                        Button(source.isEnabled == false ? LocalizedStringKey("Turn on") : "Turn off") {
                            Task {
                                _ = await finance.act(saying: String(localized: "Saved")) {
                                    try await finance.connection.switchFinanceSource(source.id, isEnabled: source.isEnabled == false)
                                }
                                await load()
                            }
                        }
                        .tint(.indigo)
                    }
                    .contextMenu {
                        if source.providerKind != "statement" {
                            Button("Sync", systemImage: "arrow.clockwise") {
                                Task {
                                    _ = await finance.act(saying: String(localized: "Syncing")) { try await finance.connection.syncFinanceSource(source.id) }
                                }
                            }
                            .tint(.blue)
                        }
                    }
                    .confirmationDialog("Remove this bank?", isPresented: Binding(get: { removing?.id == source.id }, set: { if !$0 { removing = nil } }), titleVisibility: .visible) {
                        Button("Remove", role: .destructive) {
                            guard let source = removing else { return }
                            Task {
                                _ = await finance.act(saying: String(localized: "Removed")) { try await finance.connection.deleteFinanceSource(source.id) }
                                await load()
                            }
                        }
                    } message: {
                        Text("Its accounts and transactions are deleted from TeaNode. For SimpleFIN, also revoke the access on its site.")
                    }
                }
                if sources.isEmpty {
                    Text("No bank linked yet.").foregroundStyle(.secondary)
                }
            }
            Section {
                SecureField("SimpleFIN setup token", text: $setupToken)
                Button("Link") {
                    let token = setupToken.trimmed
                    setupToken = ""
                    Task {
                        _ = await finance.act(saying: String(localized: "Linked")) { try await finance.connection.linkSimpleFIN(setupToken: token) }
                        await load()
                    }
                }
                .disabled(setupToken.trimmed.isEmpty)
            } header: {
                Text("Link through SimpleFIN")
            } footer: {
                Text("A setup token works once. To link through Plaid, use the dashboard.")
            }
            Section {
                NavigationLink("Bring an existing connection") {
                    ImportCredentialView { Task { await load() } }
                }
            }
            Section {
                TextField("Currency (USD)", text: $currency).textInputAutocapitalization(.characters)
                Button("Save") {
                    let code = currency.trimmed.uppercased()
                    Task {
                        _ = await finance.act(saying: String(localized: "Saved")) { try await finance.connection.setReportingCurrency(code) }
                        await finance.load()
                    }
                }
                .disabled(currency.trimmed.uppercased().range(of: "^[A-Z]{3}$", options: .regularExpression) == nil)
            } header: {
                Text("Reporting currency")
            } footer: {
                Text("Totals are converted to it.")
            }
        }
        .navigationTitle("Banks and currency")
        .task {
            currency = finance.reportingCurrency
            await load()
        }
        .refreshable { await load() }
    }

    private func load() async {
        sources = (try? await finance.connection.financeSources()) ?? []
    }
}
