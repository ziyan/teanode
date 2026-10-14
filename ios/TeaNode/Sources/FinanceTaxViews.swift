import Charts
import PhotosUI
import QuickLook
import SwiftUI
import TeaNodeKit
import UniformTypeIdentifiers

// MARK: Taxes

/// TaxesView is a tax year as the dashboard's Taxes tab shows it: per
/// person and employer the year's wages and withholding, from the W-2 or
/// the paystubs, projected to December 31; the W-2 against the paystubs;
/// what is worth knowing; and the W-2s and paystubs read, with adding more.
struct TaxesView: View {
    @Environment(FinanceModel.self) private var finance
    @State private var taxYear = Calendar.current.component(.year, from: Date())
    @State private var summary: TaxYearSummary?
    @State private var paystubs: [FinancePaystub] = []
    @State private var taxForms: [FinanceTaxFormView] = []
    @State private var isLoaded = false
    @State private var uploads: [TaxDocumentUpload] = []
    @State private var pickedItems: [PhotosPickerItem] = []
    @State private var isPickingPhotos = false
    @State private var isPickingFiles = false
    @State private var isTakingPicture = false
    @State private var mailInAddress: TaxDocumentAddress?
    @State private var isRegeneratingAddress = false

    /// This year and the four before it.
    private var taxYears: [Int] {
        let thisYear = finance.dates.component(.year, from: Date())
        return (0..<5).map { thisYear - $0 }
    }

    var body: some View {
        List {
            Section {
                Picker("Tax year", selection: $taxYear) {
                    ForEach(taxYears, id: \.self) { year in
                        Text(verbatim: String(year)).tag(year)
                    }
                }
            }
            mailInSection
            if !uploads.isEmpty {
                Section("Adding") {
                    ForEach(uploads) { upload in
                        TaxDocumentUploadRow(upload: upload)
                            .swipeActions {
                                if upload.isFinished {
                                    Button("Hide", systemImage: "eye.slash") {
                                        uploads.removeAll { $0.id == upload.id }
                                    }
                                }
                            }
                    }
                }
            }
            if isLoaded, paystubs.isEmpty, taxForms.isEmpty {
                Section {
                    ContentUnavailableView {
                        Label("No paystubs or W-2s", systemImage: "doc.text")
                    } description: {
                        Text("Add paystubs or W-2s for this year, as PDFs or photos, and the year is put together from them.")
                    } actions: {
                        addMenu
                    }
                }
                .listRowBackground(Color.clear)
            }
            if let summary {
                if let estimate = summary.taxYearEstimate {
                    TaxEstimateSection(estimate: estimate, taxYear: taxYear) { picked in
                        Task {
                            // The person's setting, kept on the server; the year
                            // is read again for it.
                            if await finance.act(saying: String(localized: "Saved"), { try await finance.connection.setTaxFilingStatus(picked) }) {
                                await load()
                            }
                        }
                    }
                }
                findingsSection(summary)
                ForEach(Array(summary.employments.enumerated()), id: \.offset) { _, employment in
                    employmentSection(employment)
                    comparisonSection(employment)
                }
                if summary.employments.count > 1, let figures = summary.figures {
                    Section {
                        ForEach(figures.rows(projectedTo: summary.projectedFigures)) { row in
                            TaxFigureRow(row: row)
                        }
                    } header: {
                        Text("Household")
                    } footer: {
                        if summary.projectedFigures != nil {
                            Text("Projected to December 31 where there is no W-2 yet.")
                        }
                    }
                }
            }
            if !taxForms.isEmpty {
                Section("W-2s") {
                    ForEach(taxForms) { taxFormView in
                        NavigationLink {
                            TaxFormDetailView(taxFormView: taxFormView) { Task { await load() } }
                        } label: {
                            TaxFormRow(taxFormView: taxFormView)
                        }
                        .swipeActions {
                            Button("Delete", systemImage: "trash", role: .destructive) {
                                Task {
                                    _ = await finance.act(saying: String(localized: "W-2 deleted")) { try await finance.connection.deleteTaxForm(taxFormView.id) }
                                    await load()
                                }
                            }
                        }
                    }
                }
            }
            if !paystubs.isEmpty {
                Section("Paystubs") {
                    ForEach(paystubs) { paystub in
                        NavigationLink {
                            PaystubDetailView(paystubId: paystub.id, paystub: paystub, findings: summary?.findings ?? []) { Task { await load() } }
                        } label: {
                            PaystubRow(paystub: paystub)
                        }
                        .swipeActions {
                            Button("Delete", systemImage: "trash", role: .destructive) {
                                Task {
                                    _ = await finance.act(saying: String(localized: "Paystub deleted")) { try await finance.connection.deletePaystub(paystub.id) }
                                    await load()
                                }
                            }
                        }
                    }
                }
            }
        }
        .navigationTitle("Taxes")
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) { addMenu }
        }
        .task(id: taxYear) {
            // Another year's figures are cleared rather than shown under this
            // one; coming back from a document keeps the year on screen.
            if summary?.taxYear != taxYear {
                isLoaded = false
                summary = nil
                paystubs = []
                taxForms = []
            }
            await load()
        }
        .task {
            if mailInAddress == nil {
                mailInAddress = try? await finance.connection.taxDocumentAddress()
            }
        }
        .confirmationDialog("New address?", isPresented: $isRegeneratingAddress, titleVisibility: .visible) {
            Button("Make a new address", role: .destructive) {
                Task {
                    var renewed: TaxDocumentAddress?
                    if await finance.act(saying: String(localized: "Made a new tax document address"), { renewed = try await finance.connection.regenerateTaxDocumentAddress() }) {
                        mailInAddress = renewed
                    }
                }
            }
        } message: {
            Text("The current address stops taking mail at once. Anything set to forward paystubs to it needs the new one.")
        }
        .refreshable { await load() }
        .overlay {
            if !isLoaded { ProgressView() }
        }
        .photosPicker(isPresented: $isPickingPhotos, selection: $pickedItems, maxSelectionCount: 10, matching: .images)
        .cameraPicker(isPresented: $isTakingPicture) { picture in
            if let file = PendingFile(picture: picture, index: 0) { Task { await add([file]) } }
        }
        .fileImporter(isPresented: $isPickingFiles, allowedContentTypes: [.pdf, .image], allowsMultipleSelection: true) { result in
            guard case .success(let addresses) = result else { return }
            let files = addresses.compactMap { PendingFile(fileAt: $0) }
            Task { await add(files) }
        }
        .onChange(of: pickedItems) { _, items in
            guard !items.isEmpty else { return }
            pickedItems = []
            Task {
                var files: [PendingFile] = []
                for (index, item) in items.enumerated() {
                    if let data = try? await item.loadTransferable(type: Data.self), let file = PendingFile(pictureData: data, index: index) {
                        files.append(file)
                    }
                }
                await add(files)
            }
        }
    }

    /// The address paystubs and W-2s are mailed to: each PDF or picture
    /// mailed or forwarded there is read on its own, as the dashboard says.
    @ViewBuilder
    private var mailInSection: some View {
        if let mailInAddress {
            Section {
                if let address = mailInAddress.taxDocumentAddress, !address.isEmpty {
                    Text(verbatim: address)
                        .font(.callout.monospaced())
                        .textSelection(.enabled)
                    Button("Copy address", systemImage: "doc.on.doc") { UIPasteboard.general.string = address }
                } else {
                    Text("You have no mailbox address to receive them; add the files instead.")
                        .foregroundStyle(.secondary)
                }
                Button("New address", systemImage: "arrow.clockwise") { isRegeneratingAddress = true }
            } header: {
                Text("Mail them in")
            } footer: {
                if mailInAddress.canReadPdfs == false {
                    Text("Each photo mailed or forwarded here is read as a paystub or W-2; this server cannot read PDFs.")
                } else {
                    Text("Each PDF or photo mailed or forwarded here is read as a paystub or W-2.")
                }
            }
        }
    }

    /// Adding documents: pictures from the library, a picture taken now, or
    /// PDFs and pictures from Files, several at once.
    private var addMenu: some View {
        Menu {
            Button("Photo library", systemImage: "photo.on.rectangle") { isPickingPhotos = true }
            if UIImagePickerController.isSourceTypeAvailable(.camera) {
                Button("Camera", systemImage: "camera") { isTakingPicture = true }
            }
            Button("Files", systemImage: "folder") { isPickingFiles = true }
        } label: {
            Label("Add paystubs or W-2s", systemImage: "plus")
        }
    }

    @ViewBuilder
    private func findingsSection(_ summary: TaxYearSummary) -> some View {
        let findings = summary.findings
        if !findings.isEmpty {
            Section("Findings") {
                ForEach(Array(findings.enumerated()), id: \.offset) { _, finding in
                    if let paystubId = finding.paystubIds?.first {
                        NavigationLink {
                            PaystubDetailView(paystubId: paystubId, paystub: paystubs.first(where: { $0.id == paystubId }), findings: findings) { Task { await load() } }
                        } label: {
                            TaxYearFindingRow(finding: finding)
                        }
                    } else {
                        TaxYearFindingRow(finding: finding)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func employmentSection(_ employment: TaxYearSummary.Employment) -> some View {
        Section {
            if employment.isFromW2, let taxFormView = taxForms.first(where: { $0.id == employment.taxFormId }) {
                NavigationLink {
                    TaxFormDetailView(taxFormView: taxFormView) { Task { await load() } }
                } label: {
                    Text("From the W-2").foregroundStyle(.secondary)
                }
            } else {
                Text(sourceText(employment)).foregroundStyle(.secondary)
            }
            if let figures = employment.figures {
                ForEach(figures.rows(projectedTo: employment.projectedFigures)) { row in
                    TaxFigureRow(row: row)
                }
            }
        } header: {
            Text("\(employment.employeeName ?? "") at \(employment.employerName ?? "")")
        } footer: {
            if employment.projectedFigures != nil {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Projected to December 31 with \(employment.remainingPayCount ?? 0) more paychecks like the latest.")
                    if employment.isRetirementDeferralCapped == true, let limit = summary?.retirementDeferralLimitAmount {
                        Text("Retirement contributions stop at the year's limit of \(FinanceMoney.shown(limit, currencyCode: TaxWords.currencyCode)), and the pay that would have gone in counts as wages.")
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func comparisonSection(_ employment: TaxYearSummary.Employment) -> some View {
        let comparisons = employment.comparisons
        if !comparisons.isEmpty {
            Section {
                ForEach(Array(comparisons.enumerated()), id: \.offset) { _, comparison in
                    TaxYearComparisonRow(comparison: comparison)
                }
            } header: {
                Text("W-2 against paystubs")
            } footer: {
                Text("Each box of the W-2 beside the year to date on the last paystub.")
            }
        }
    }

    /// Where an employment's figures come from: the W-2, or the paystubs
    /// through the latest one's pay date.
    private func sourceText(_ employment: TaxYearSummary.Employment) -> String {
        if employment.isFromW2 { return String(localized: "From the W-2") }
        let count = employment.paystubCount ?? 0
        let dayText = TaxWords.dayText(employment.latestPayDate, in: finance)
        // Said apart for one: a count beside another argument cannot vary by
        // plural in the catalog without substitutions.
        if count == 1 { return String(localized: "From 1 paystub through \(dayText)") }
        return String(localized: "From \(count) paystubs through \(dayText)")
    }

    private func load() async {
        let connection = finance.connection
        let chosenYear = taxYear
        async let summaryRead = connection.taxYear(chosenYear)
        async let paystubRead = connection.financePaystubs(taxYear: chosenYear)
        async let taxFormRead = connection.financeTaxForms(taxYear: chosenYear)
        do {
            let (readSummary, paystubPage, taxFormPage) = try await (summaryRead, paystubRead, taxFormRead)
            guard chosenYear == taxYear else { return }
            summary = readSummary
            paystubs = paystubPage.financePaystubs.compactMap { $0 }.sorted { ($0.payDate ?? "") > ($1.payDate ?? "") }
            taxForms = taxFormPage.taxForms.compactMap { $0 }
        } catch {
            finance.problemText = error.localizedDescription
        }
        isLoaded = true
    }

    /// Uploads each file as a tax document and has the agent read it, then
    /// waits for them: every four seconds, two minutes at most, as a
    /// receipt is waited for.
    private func add(_ files: [PendingFile]) async {
        guard !files.isEmpty else { return }
        let started = files.map { TaxDocumentUpload(fileName: $0.name) }
        uploads.append(contentsOf: started)
        var readingIds: [String] = []
        for (file, upload) in zip(files, started) {
            do {
                let attachmentId = try await finance.connection.upload(name: file.name, contentType: file.contentType, data: file.data, isTaxDocument: true)
                _ = try await finance.connection.readTaxDocument(attachmentId: attachmentId)
                update(upload.id) {
                    $0.agentAttachmentId = attachmentId
                    $0.uploadPhase = .reading
                }
                readingIds.append(attachmentId)
            } catch {
                update(upload.id) { $0.uploadPhase = .failed(error.localizedDescription) }
            }
        }
        guard !readingIds.isEmpty else { return }
        finance.doneText = String(localized: "Reading \(readingIds.count) documents")
        var waitingIds = Set(readingIds)
        for _ in 0..<30 {
            try? await Task.sleep(for: .seconds(4))
            await load()
            let readIds = Set(paystubs.compactMap(\.agentAttachmentId) + taxForms.compactMap { $0.financeTaxForm?.agentAttachmentId })
            let doneIds = waitingIds.intersection(readIds)
            uploads.removeAll { upload in upload.agentAttachmentId.map { doneIds.contains($0) } ?? false }
            waitingIds.subtract(doneIds)
            if waitingIds.isEmpty { return }
        }
        for upload in uploads where upload.agentAttachmentId.map({ waitingIds.contains($0) }) ?? false {
            update(upload.id) { $0.uploadPhase = .stillReading }
        }
    }

    private func update(_ uploadId: UUID, _ change: (inout TaxDocumentUpload) -> Void) {
        guard let index = uploads.firstIndex(where: { $0.id == uploadId }) else { return }
        change(&uploads[index])
    }
}

/// TaxDocumentUpload is a file being added as a paystub or a W-2.
struct TaxDocumentUpload: Identifiable {
    enum UploadPhase: Equatable {
        case uploading, reading, stillReading
        case failed(String)
    }

    let id = UUID()
    let fileName: String
    var agentAttachmentId: String?
    var uploadPhase = UploadPhase.uploading

    /// Whether nothing more will happen to it here.
    var isFinished: Bool {
        switch uploadPhase {
        case .uploading, .reading: false
        case .stillReading, .failed: true
        }
    }
}

/// TaxDocumentUploadRow is a file being added: its name and how far it is.
struct TaxDocumentUploadRow: View {
    let upload: TaxDocumentUpload

    var body: some View {
        HStack(spacing: 12) {
            switch upload.uploadPhase {
            case .uploading, .reading:
                ProgressView()
            case .stillReading:
                Image(systemName: "clock").foregroundStyle(.secondary).accessibilityHidden(true)
            case .failed:
                Image(systemName: "exclamationmark.triangle").foregroundStyle(.orange).accessibilityHidden(true)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: upload.fileName).lineLimit(1)
                Group {
                    switch upload.uploadPhase {
                    case .uploading: Text("Uploading…")
                    case .reading: Text("Reading…")
                    case .stillReading: Text("Still reading. Pull to refresh, or look under another year.")
                    case .failed(let problemText): Text(verbatim: problemText)
                    }
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// TaxFigureRow is one figure of a year, with where it is heading by
/// December 31 under it when there is a projection.
/// TaxEstimateSection is the year's pay as a ring, as the dashboard draws
/// it: the tax already withheld, what the rest of the year's paychecks will
/// withhold, what will be owed at filing, and what is left, the year's tax
/// in the middle. Under it each slice and each tax in figures with its
/// share of pay (the year's tax's is the effective rate), and the filing
/// status the federal tax is worked out for, the person's setting.
struct TaxEstimateSection: View {
    let estimate: TaxYearSummary.Estimate
    let taxYear: Int
    let onFilingStatus: (TaxFilingStatus) -> Void
    @State private var selectedRunningTotal: Double?
    @State private var selectedSliceKind: TaxEstimateSliceKind?

    var body: some View {
        let slices = estimate.slices
        Section {
            if !slices.isEmpty { ring(slices) }
            ForEach(slices) { slice in
                figureRow(title(slice.sliceKind), slice.amount, color: Self.color(slice.sliceKind))
            }
            if (Double(estimate.refundAmount ?? "") ?? 0) > 0 {
                figureRow(String(localized: "Refund at filing"), estimate.refundAmount, amountStyle: .green)
            }
            figureRow(String(localized: "Pay"), estimate.payAmount, isTotal: true)
            Picker("Filing status", selection: Binding(
                get: { estimate.filingStatus ?? .single },
                set: { onFilingStatus($0) }
            )) {
                ForEach(TaxFilingStatus.allCases) { status in
                    Text(Self.title(status)).tag(status)
                }
            }
            figureRow(String(localized: "Federal income tax"), estimate.federalIncomeTaxAmount)
            figureRow(String(localized: "Social Security"), estimate.socialSecurityTaxAmount)
            figureRow(String(localized: "Medicare"), estimate.medicareTaxAmount)
            if (Double(estimate.stateIncomeTaxAmount ?? "") ?? 0) > 0 {
                figureRow(String(localized: "State income tax, as withheld"), estimate.stateIncomeTaxAmount)
            }
            if (Double(estimate.localIncomeTaxAmount ?? "") ?? 0) > 0 {
                figureRow(String(localized: "Local income tax, as withheld"), estimate.localIncomeTaxAmount)
            }
            figureRow(String(localized: "Tax for the year"), estimate.totalTaxAmount, isTotal: true)
        } header: {
            if estimate.isProjected == true {
                Text("Tax for \(String(taxYear)), by December 31")
            } else {
                Text("Tax for \(String(taxYear))")
            }
        } footer: {
            VStack(alignment: .leading, spacing: 4) {
                if estimate.isTaxFilingStatusAssumed == true {
                    Text("The filing status is assumed from the number of people; pick yours above and it is kept.")
                }
                Text("An estimate from wages only: \(money(estimate.taxableIncomeAmount)) taxable after the \(money(estimate.standardDeductionAmount)) standard deduction. Other income, itemized deductions and credits are not counted.")
            }
        }
    }

    /// The slices as a donut. Touching one chooses it: it stays full, the
    /// others step back, and the middle says what it is; with none chosen
    /// the middle says the year's tax.
    private func ring(_ slices: [TaxEstimateSlice]) -> some View {
        let selectedSlice = slices.first { $0.sliceKind == selectedSliceKind }
        return Chart(slices) { slice in
            let isChosen = slice.sliceKind == selectedSlice?.sliceKind
            SectorMark(
                angle: .value("Amount", slice.amountValue),
                innerRadius: .ratio(0.62),
                outerRadius: .ratio(isChosen || selectedSlice == nil ? 1 : 0.94),
                angularInset: 1
            )
            .foregroundStyle(Self.color(slice.sliceKind))
            .opacity(ChartPalette.markOpacity(isChosen: isChosen, isAnyChosen: selectedSlice != nil))
            .accessibilityLabel(Text(title(slice.sliceKind)))
            .accessibilityValue(Text(verbatim: money(slice.amount)))
        }
        .chartLegend(.hidden)
        .chartAngleSelection(value: $selectedRunningTotal)
        .onChange(of: selectedRunningTotal) { previousTotal, runningTotal in
            guard let runningTotal, let index = ChartShares.sliceIndex(atRunningTotal: runningTotal, amounts: slices.map(\.amountValue)) else { return }
            let touchedKind = slices[index].sliceKind
            selectedSliceKind = previousTotal == nil && touchedKind == selectedSliceKind ? nil : touchedKind
        }
        .chartBackground { chartProxy in
            GeometryReader { geometry in
                if let plotFrame = chartProxy.plotFrame {
                    let frame = geometry[plotFrame]
                    let holeWidth = min(frame.width, frame.height) * 0.62
                    VStack(spacing: 2) {
                        Text(selectedSlice.map { title($0.sliceKind) } ?? String(localized: "Tax for the year"))
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(2)
                        Text(verbatim: money(selectedSlice?.amount ?? estimate.totalTaxAmount))
                            .font(.headline)
                            .monospacedDigit()
                            .lineLimit(1)
                            .contentTransition(.numericText())
                    }
                    .multilineTextAlignment(.center)
                    .minimumScaleFactor(0.4)
                    .frame(width: holeWidth * 0.82)
                    .position(x: frame.midX, y: frame.midY)
                    .accessibilityHidden(true)
                }
            }
        }
        .frame(height: 220)
        .padding(.vertical, 4)
    }

    private func money(_ amount: String?) -> String {
        FinanceMoney.shown(amount, currencyCode: TaxWords.currencyCode)
    }

    /// One figure: its name, with the slice's swatch when it is one, and
    /// its amount over its share of pay.
    private func figureRow(_ label: String, _ amount: String?, color: Color? = nil, amountStyle: Color? = nil, isTotal: Bool = false) -> some View {
        LabeledContent {
            VStack(alignment: .trailing, spacing: 0) {
                Text(verbatim: money(amount))
                    .monospacedDigit()
                    .fontWeight(isTotal ? .semibold : .regular)
                    .foregroundStyle(amountStyle ?? .primary)
                if let share = estimate.shareOfPay(amount) {
                    Text(share, format: .percent.precision(.fractionLength(1)))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .monospacedDigit()
                }
            }
        } label: {
            HStack(spacing: 8) {
                if let color { ChartSwatch(color: color) }
                Text(verbatim: label)
            }
        }
    }

    /// Tax withheld in orange, the part still to come paler, what is owed
    /// in red, and what is left in the app's green.
    static func color(_ sliceKind: TaxEstimateSliceKind) -> Color {
        switch sliceKind {
        case .paid: .orange
        case .upcomingWithheld: .orange.opacity(0.45)
        case .owing: .red
        case .remainingIncome: .accentColor
        }
    }

    /// A slice's name; what was paid is "so far" only while the year is
    /// projected.
    private func title(_ sliceKind: TaxEstimateSliceKind) -> String {
        switch sliceKind {
        case .paid: estimate.isProjected == true ? String(localized: "Paid so far") : String(localized: "Withheld")
        case .upcomingWithheld: String(localized: "Still to be withheld")
        case .owing: String(localized: "Owing at filing")
        case .remainingIncome: String(localized: "Remaining income")
        }
    }

    static func title(_ filingStatus: TaxFilingStatus) -> String {
        switch filingStatus {
        case .single: String(localized: "Single")
        case .marriedFilingJointly: String(localized: "Married filing jointly")
        case .marriedFilingSeparately: String(localized: "Married filing separately")
        case .headOfHousehold: String(localized: "Head of household")
        }
    }
}

struct TaxFigureRow: View {
    let row: TaxYearFigureRow

    var body: some View {
        LabeledContent(TaxWords.figureTitle(row.figureKind)) {
            VStack(alignment: .trailing) {
                Text(FinanceMoney.shown(row.amount, currencyCode: TaxWords.currencyCode)).monospacedDigit()
                if let projectedAmount = row.projectedAmount, !projectedAmount.isEmpty {
                    Text("\(FinanceMoney.shown(projectedAmount, currencyCode: TaxWords.currencyCode)) by December 31")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }
}

/// TaxYearComparisonRow is one W-2 box beside the paystubs' figure, and
/// whether they match.
struct TaxYearComparisonRow: View {
    let comparison: TaxYearSummary.Comparison

    var body: some View {
        LabeledContent {
            if comparison.isMatching == true {
                Label("Matches", systemImage: "checkmark.circle")
                    .foregroundStyle(.secondary)
            } else {
                Label("Differs by \(FinanceMoney.shown(comparison.differenceAmount, currencyCode: TaxWords.currencyCode))", systemImage: "exclamationmark.triangle")
                    .foregroundStyle(.orange)
            }
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: comparison.boxLabel.flatMap { $0.isEmpty ? nil : $0 } ?? comparison.boxName)
                Text("Box \(comparison.boxName): W-2 \(FinanceMoney.shown(comparison.w2Amount, currencyCode: TaxWords.currencyCode)), paystubs \(FinanceMoney.shown(comparison.paystubAmount, currencyCode: TaxWords.currencyCode))")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
            }
        }
    }
}

/// TaxYearFindingRow is one thing worth knowing about the year, in its
/// sentence, and whose it is.
struct TaxYearFindingRow: View {
    let finding: TaxYearSummary.Finding

    var body: some View {
        // Social Security withheld beyond the maximum comes back on the
        // return: worth knowing, not a problem.
        let isProblem = finding.taxYearFindingCode != "excess_social_security"
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: finding.findingText ?? "")
                let whose = [finding.employeeName, finding.employerName].compactMap { $0 }.filter { !$0.isEmpty }
                if !whose.isEmpty {
                    Text(verbatim: whose.joined(separator: ", "))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
        } icon: {
            Image(systemName: isProblem ? "exclamationmark.triangle" : "info.circle")
                .foregroundStyle(isProblem ? AnyShapeStyle(.orange) : AnyShapeStyle(.secondary))
        }
    }
}

/// TaxFormRow is a W-2 in a list: the employer, its year, and whether it
/// agrees with itself.
struct TaxFormRow: View {
    let taxFormView: FinanceTaxFormView

    var body: some View {
        let taxForm = taxFormView.financeTaxForm
        VStack(alignment: .leading, spacing: 2) {
            Text(taxForm?.payerName.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "W-2"))
                .foregroundStyle(Color(.label))
            HStack(spacing: 6) {
                Text(verbatim: taxForm?.recipientName ?? "")
                if let year = taxForm?.taxYear {
                    Text(verbatim: String(year))
                }
                if taxForm?.isCorrected == true {
                    Text("Corrected")
                }
            }
            .font(.caption)
            .foregroundStyle(Color(.secondaryLabel))
            // A passing check is not drawn in a list row (ios/DESIGN.md);
            // the detail says it agrees.
            let disagreeingCount = taxFormView.disagreeingCount
            if disagreeingCount > 0 {
                Label("\(disagreeingCount) checks disagree", systemImage: "exclamationmark.triangle")
                    .font(.caption)
                    .foregroundStyle(.orange)
            }
        }
    }
}

/// PaystubRow is a paystub in a list: the employer, the pay date, the
/// take-home pay, and how much it is off by when it does not add up.
struct PaystubRow: View {
    @Environment(FinanceModel.self) private var finance
    let paystub: FinancePaystub

    var body: some View {
        // LabeledContent centers the take-home pay on the whole row, level
        // with the disclosure chevron, as a receipt's total is.
        LabeledContent {
            Text(FinanceMoney.shown(paystub.netAmount, currencyCode: paystub.currencyCode))
                .monospacedDigit()
                .foregroundStyle(Color(.label))
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(paystub.employerName.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "Paystub"))
                    .foregroundStyle(Color(.label))
                Text(verbatim: TaxWords.dayText(paystub.payDate, in: finance))
                    .font(.caption)
                    .foregroundStyle(Color(.secondaryLabel))
                if !paystub.isBalanced {
                    Label("Off by \(FinanceMoney.shown(paystub.checkDifferenceAmount, currencyCode: paystub.currencyCode))", systemImage: "exclamationmark.triangle")
                        .font(.caption)
                        .foregroundStyle(.orange)
                }
            }
        }
    }
}

// MARK: A paystub

/// PaystubDetailView is one paystub, as the dashboard's dialog shows it:
/// who paid whom and when, its totals and whether they add up, every line,
/// what the year says about it, its file, and deleting it.
struct PaystubDetailView: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let paystubId: String
    /// The year's findings; those naming this paystub are shown.
    let findings: [TaxYearSummary.Finding]
    let onChange: () -> Void
    @State private var paystub: FinancePaystub?
    @State private var isConfirmingDelete = false
    @State private var isOpeningFile = false
    @State private var previewAddress: URL?

    init(paystubId: String, paystub: FinancePaystub? = nil, findings: [TaxYearSummary.Finding] = [], onChange: @escaping () -> Void) {
        self.paystubId = paystubId
        self.findings = findings
        self.onChange = onChange
        _paystub = State(initialValue: paystub)
    }

    var body: some View {
        List {
            if let paystub {
                summarySection(paystub)
                linesSection(paystub, lineKind: "earning", title: String(localized: "Earnings"))
                linesSection(paystub, lineKind: "tax", title: String(localized: "Taxes"))
                linesSection(paystub, lineKind: "deduction", title: String(localized: "Deductions"))
                linesSection(paystub, lineKind: "reimbursement", title: String(localized: "Reimbursements"))
                let named = findings.filter { $0.paystubIds?.contains(paystubId) == true }
                if !named.isEmpty {
                    Section("Findings") {
                        ForEach(Array(named.enumerated()), id: \.offset) { _, finding in
                            TaxYearFindingRow(finding: finding)
                        }
                    }
                }
                if let attachmentId = paystub.agentAttachmentId, !attachmentId.isEmpty {
                    Section {
                        Button {
                            Task { await openFile(attachmentId) }
                        } label: {
                            HStack {
                                Label("Open the original", systemImage: "doc.viewfinder")
                                if isOpeningFile {
                                    Spacer()
                                    ProgressView()
                                }
                            }
                        }
                        .disabled(isOpeningFile)
                    }
                }
                Section {
                    Button(role: .destructive) {
                        isConfirmingDelete = true
                    } label: {
                        Label("Delete paystub", systemImage: "trash")
                            .foregroundStyle(.red)
                    }
                }
            } else {
                ProgressView().frame(maxWidth: .infinity)
            }
        }
        .navigationTitle(paystub?.employerName.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "Paystub"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .quickLookPreview($previewAddress)
        .confirmationDialog("Delete this paystub?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
            Button("Delete paystub", role: .destructive) {
                Task {
                    let isDone = await finance.act(saying: String(localized: "Paystub deleted")) {
                        try await finance.connection.deletePaystub(paystubId)
                    }
                    if isDone {
                        onChange()
                        dismiss()
                    }
                }
            }
        } message: {
            Text("Its lines are deleted too, and the tax year no longer counts it.")
        }
    }

    @ViewBuilder
    private func summarySection(_ paystub: FinancePaystub) -> some View {
        let currencyCode = paystub.currencyCode
        Section {
            LabeledContent("Employer", value: paystub.employerName ?? "")
            LabeledContent("Employee", value: TaxWords.personText(paystub.employeeName, taxIdMask: paystub.employeeTaxIdMask))
            LabeledContent("Paid on", value: TaxWords.dayText(paystub.payDate, in: finance))
            if let start = paystub.periodStartOn, !start.isEmpty, let end = paystub.periodEndOn, !end.isEmpty {
                LabeledContent("Pay period") {
                    Text("\(TaxWords.dayText(start, in: finance)) to \(TaxWords.dayText(end, in: finance))")
                }
            }
            LabeledContent("Gross pay", value: FinanceMoney.shown(paystub.grossAmount, currencyCode: currencyCode))
            if let taxes = paystub.totalTaxAmount, !taxes.isEmpty {
                LabeledContent("Taxes", value: FinanceMoney.shown(taxes, currencyCode: currencyCode))
            }
            if let deductions = paystub.totalDeductionAmount, !deductions.isEmpty {
                LabeledContent("Deductions", value: FinanceMoney.shown(deductions, currencyCode: currencyCode))
            }
            LabeledContent("Take-home pay", value: FinanceMoney.shown(paystub.netAmount, currencyCode: currencyCode))
            if let yearToDate = paystub.yearToDateGrossAmount, !yearToDate.isEmpty {
                LabeledContent("Gross pay this year", value: FinanceMoney.shown(yearToDate, currencyCode: currencyCode))
            }
            if paystub.isBalanced {
                Label("The lines add up", systemImage: "checkmark.seal")
                    .foregroundStyle(Color(.secondaryLabel))
            } else {
                Label("The lines are off by \(FinanceMoney.shown(paystub.checkDifferenceAmount, currencyCode: currencyCode))", systemImage: "exclamationmark.triangle")
                    .foregroundStyle(.orange)
            }
        }
    }

    @ViewBuilder
    private func linesSection(_ paystub: FinancePaystub, lineKind: String, title: String) -> some View {
        let lines = paystub.lines(ofKind: lineKind)
        if !lines.isEmpty {
            Section(title) {
                ForEach(lines, id: \.id) { line in
                    PaystubLineRow(line: line, currencyCode: paystub.currencyCode)
                }
            }
        }
    }

    private func load() async {
        do {
            if let read = try await finance.connection.financePaystub(paystubId) { paystub = read }
        } catch {
            finance.problemText = error.localizedDescription
        }
    }

    private func openFile(_ attachmentId: String) async {
        isOpeningFile = true
        defer { isOpeningFile = false }
        do {
            previewAddress = try await TaxDocumentFile.save(attachmentId: attachmentId, baseName: String(localized: "Paystub"), connection: finance.connection)
        } catch {
            finance.problemText = error.localizedDescription
        }
    }
}

/// PaystubLineRow is one printed line of a paystub: what it is, this
/// period's amount, and the year's so far, with an earning's hours, a
/// tax's wages and a deduction's employer part when printed.
struct PaystubLineRow: View {
    let line: FinancePaystub.Line
    let currencyCode: String?

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: line.description.flatMap { $0.isEmpty ? nil : $0 } ?? "")
                if let detail = detailText {
                    Text(verbatim: detail)
                        .font(.caption)
                        .foregroundStyle(Color(.secondaryLabel))
                }
            }
            Spacer()
            VStack(alignment: .trailing, spacing: 2) {
                Text(FinanceMoney.shown(line.currentAmount, currencyCode: currencyCode))
                    .monospacedDigit()
                if let yearToDate = line.yearToDateAmount, !yearToDate.isEmpty {
                    Text("\(FinanceMoney.shown(yearToDate, currencyCode: currencyCode)) this year")
                        .font(.caption)
                        .foregroundStyle(Color(.secondaryLabel))
                        .monospacedDigit()
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    /// "80 hours at $50.00", "ZZ, on $4,000.00 of wages", "Employer adds $100.00".
    private var detailText: String? {
        var parts: [String] = []
        if let hours = FinanceMoney.decimal(line.hours) {
            if let rate = line.rateAmount, !rate.isEmpty {
                parts.append(String(localized: "\(hours.formatted()) hours at \(FinanceMoney.shown(rate, currencyCode: currencyCode))"))
            } else {
                parts.append(String(localized: "\(hours.formatted()) hours"))
            }
        }
        if let jurisdiction = line.taxJurisdictionCode, !jurisdiction.isEmpty {
            parts.append(jurisdiction)
        }
        if let wages = line.currentWagesAmount, !wages.isEmpty {
            parts.append(String(localized: "on \(FinanceMoney.shown(wages, currencyCode: currencyCode)) of wages"))
        }
        if let employerAmount = line.employerCurrentAmount, !employerAmount.isEmpty {
            parts.append(String(localized: "Employer adds \(FinanceMoney.shown(employerAmount, currencyCode: currencyCode))"))
        }
        return parts.isEmpty ? nil : parts.joined(separator: ", ")
    }
}

// MARK: A W-2

/// TaxFormDetailView is one W-2, as the dashboard's dialog shows it: whose
/// it is, whether it agrees with itself, every box, its file, and deleting
/// it.
struct TaxFormDetailView: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    @State var taxFormView: FinanceTaxFormView
    let onChange: () -> Void
    @State private var isConfirmingDelete = false
    @State private var isOpeningFile = false
    @State private var previewAddress: URL?

    var body: some View {
        let taxForm = taxFormView.financeTaxForm
        List {
            Section {
                LabeledContent("Employer", value: taxForm?.payerName ?? "")
                LabeledContent("Employee", value: TaxWords.personText(taxForm?.recipientName, taxIdMask: taxForm?.recipientTaxIdMask))
                if let year = taxForm?.taxYear {
                    LabeledContent("Tax year") { Text(verbatim: String(year)) }
                }
                if taxForm?.isCorrected == true {
                    Label("A corrected W-2", systemImage: "pencil.circle")
                        .foregroundStyle(Color(.secondaryLabel))
                }
            }
            let findings = taxFormView.findings
            if !findings.isEmpty {
                Section("Checks") {
                    ForEach(Array(findings.enumerated()), id: \.offset) { _, finding in
                        let isAgreeing = finding.isAgreeing == true
                        Label {
                            Text(verbatim: finding.findingText ?? "")
                        } icon: {
                            Image(systemName: isAgreeing ? "checkmark.circle" : "exclamationmark.triangle")
                                .foregroundStyle(isAgreeing ? AnyShapeStyle(.secondary) : AnyShapeStyle(.orange))
                        }
                    }
                }
            }
            let entries = taxForm?.entries ?? []
            if !entries.isEmpty {
                Section("Boxes") {
                    ForEach(entries, id: \.id) { entry in
                        TaxFormEntryRow(entry: entry)
                    }
                }
            }
            if let attachmentId = taxForm?.agentAttachmentId, !attachmentId.isEmpty {
                Section {
                    Button {
                        Task { await openFile(attachmentId) }
                    } label: {
                        HStack {
                            Label("Open the original", systemImage: "doc.viewfinder")
                            if isOpeningFile {
                                Spacer()
                                ProgressView()
                            }
                        }
                    }
                    .disabled(isOpeningFile)
                }
            }
            Section {
                Button(role: .destructive) {
                    isConfirmingDelete = true
                } label: {
                    Label("Delete W-2", systemImage: "trash")
                        .foregroundStyle(.red)
                }
            }
        }
        .navigationTitle(taxForm?.payerName.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "W-2"))
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .quickLookPreview($previewAddress)
        .confirmationDialog("Delete this W-2?", isPresented: $isConfirmingDelete, titleVisibility: .visible) {
            Button("Delete W-2", role: .destructive) {
                Task {
                    let isDone = await finance.act(saying: String(localized: "W-2 deleted")) {
                        try await finance.connection.deleteTaxForm(taxFormView.id)
                    }
                    if isDone {
                        onChange()
                        dismiss()
                    }
                }
            }
        } message: {
            Text("Its boxes are deleted too, and the tax year no longer counts it.")
        }
    }

    private func load() async {
        do {
            if let read = try await finance.connection.financeTaxForm(taxFormView.id) { taxFormView = read }
        } catch {
            finance.problemText = error.localizedDescription
        }
    }

    private func openFile(_ attachmentId: String) async {
        isOpeningFile = true
        defer { isOpeningFile = false }
        do {
            previewAddress = try await TaxDocumentFile.save(attachmentId: attachmentId, baseName: "W-2", connection: finance.connection)
        } catch {
            finance.problemText = error.localizedDescription
        }
    }
}

/// TaxFormEntryRow is one box of a W-2: its number and name, and its
/// amount, text or mark.
struct TaxFormEntryRow: View {
    let entry: FinanceTaxForm.Entry

    var body: some View {
        LabeledContent {
            if let amount = entry.amount, !amount.isEmpty {
                Text(FinanceMoney.shown(amount, currencyCode: TaxWords.currencyCode)).monospacedDigit()
            } else if let text = entry.textValue, !text.isEmpty {
                Text(verbatim: text)
            } else if entry.isChecked == true {
                Label("Marked", systemImage: "checkmark.square")
            }
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text("Box \(entry.boxName)")
                if let boxTitle = TaxWords.w2BoxTitle(entry.boxCode) {
                    Text(boxTitle)
                        .font(.caption)
                        .foregroundStyle(Color(.secondaryLabel))
                }
            }
        }
    }
}

// MARK: Words

/// TaxWords names what the tax screens show, in the reader's words.
enum TaxWords {
    /// A tax year's figures are dollars, as a W-2's are.
    static let currencyCode = "USD"

    static func figureTitle(_ figureKind: TaxYearFigureKind) -> String {
        switch figureKind {
        case .wages: String(localized: "Wages")
        case .federalIncomeTax: String(localized: "Federal income tax withheld")
        case .socialSecurityWages: String(localized: "Social Security wages")
        case .socialSecurityTax: String(localized: "Social Security tax")
        case .medicareWages: String(localized: "Medicare wages")
        case .medicareTax: String(localized: "Medicare tax")
        case .retirementTraditional: String(localized: "Traditional retirement")
        case .retirementRoth: String(localized: "Roth retirement")
        case .hsa: String(localized: "HSA")
        case .dependentCare: String(localized: "Dependent care")
        case .stateWages(let stateCode): String(localized: "\(stateCode) wages")
        case .stateIncomeTax(let stateCode): String(localized: "\(stateCode) income tax")
        case .localIncomeTax: String(localized: "Local income tax")
        }
    }

    /// What a W-2 box is, by its number.
    static func w2BoxTitle(_ boxCode: String?) -> String? {
        switch boxCode {
        case "1": String(localized: "Wages, tips, other compensation")
        case "2": String(localized: "Federal income tax withheld")
        case "3": String(localized: "Social Security wages")
        case "4": String(localized: "Social Security tax withheld")
        case "5": String(localized: "Medicare wages and tips")
        case "6": String(localized: "Medicare tax withheld")
        case "7": String(localized: "Social Security tips")
        case "8": String(localized: "Allocated tips")
        case "10": String(localized: "Dependent care benefits")
        case "11": String(localized: "Nonqualified plans")
        case "12": String(localized: "Coded amounts")
        case "13": String(localized: "Statutory employee, retirement plan, third-party sick pay")
        case "14": String(localized: "Other")
        case "15": String(localized: "State and employer's state ID")
        case "16": String(localized: "State wages")
        case "17": String(localized: "State income tax")
        case "18": String(localized: "Local wages")
        case "19": String(localized: "Local income tax")
        case "20": String(localized: "Locality name")
        default: nil
        }
    }

    /// A day the server writes as "2026-04-03", as the reader writes days.
    @MainActor
    static func dayText(_ day: String?, in finance: FinanceModel) -> String {
        guard let day, !day.isEmpty else { return "" }
        guard let date = finance.date(ofDay: day) else { return day }
        return date.formatted(Date.FormatStyle(date: .abbreviated, time: .omitted, timeZone: finance.dates.timeZone))
    }

    /// A person with the last four digits of their taxpayer number, when known.
    static func personText(_ name: String?, taxIdMask: String?) -> String {
        let name = name ?? ""
        guard let taxIdMask, !taxIdMask.isEmpty else { return name }
        return "\(name) ··\(taxIdMask)"
    }
}

/// TaxDocumentFile saves a paystub's or a W-2's file to open it, named by
/// what its bytes are, since the file's own name is not kept beside it.
enum TaxDocumentFile {
    static func save(attachmentId: String, baseName: String, connection: Connection) async throws -> URL {
        let fileData = try await connection.download("/api/v1/agent/attachments/\(attachmentId)")
        let fileExtension: String
        if fileData.starts(with: Data("%PDF".utf8)) {
            fileExtension = "pdf"
        } else if fileData.starts(with: Data([0x89, 0x50, 0x4E, 0x47])) {
            fileExtension = "png"
        } else {
            fileExtension = "jpg"
        }
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let address = folder.appendingPathComponent("\(baseName.replacingOccurrences(of: "/", with: "-")).\(fileExtension)")
        try fileData.write(to: address)
        return address
    }
}
