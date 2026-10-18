import PhotosUI
import SwiftUI
import TeaNodeKit
import UniformTypeIdentifiers

// Receipts, paystubs and tax forms typed in by hand, the same fields and
// words as the dashboard's forms and the CLI's record commands. Each is
// recorded from a file the server holds, so a form starts with picking one.

// MARK: Words

enum EntryWords {
    static func receiptLineKind(_ kind: String) -> String {
        switch kind {
        case "discount": String(localized: "Discount")
        case "tax": String(localized: "Tax")
        case "fee": String(localized: "Fee")
        case "tip": String(localized: "Tip")
        default: String(localized: "Item")
        }
    }

    static func paystubLineKind(_ kind: String) -> String {
        switch kind {
        case "tax": String(localized: "Tax")
        case "deduction": String(localized: "Deduction")
        case "reimbursement": String(localized: "Reimbursement")
        default: String(localized: "Earning")
        }
    }

    static func paystubTaxKind(_ kind: String) -> String {
        switch kind {
        case "social_security": String(localized: "Social Security")
        case "medicare": String(localized: "Medicare")
        case "medicare_additional": String(localized: "Additional Medicare")
        case "state_income": String(localized: "State income")
        case "local_income": String(localized: "Local income")
        case "state_disability": String(localized: "State disability")
        case "other": String(localized: "Other")
        default: String(localized: "Federal income")
        }
    }

    static func paystubDeductionKind(_ kind: String) -> String {
        switch kind {
        case "retirement_roth": String(localized: "Roth retirement")
        case "hsa": String(localized: "HSA")
        case "health_premium": String(localized: "Health premium")
        case "fsa_health": String(localized: "Health FSA")
        case "fsa_dependent_care": String(localized: "Dependent care FSA")
        case "commuter": String(localized: "Commuter")
        case "insurance": String(localized: "Insurance")
        case "other": String(localized: "Other")
        default: String(localized: "Traditional retirement")
        }
    }

    static func taxFormKind(_ kind: String) -> String {
        switch kind {
        case "1099_int": "1099-INT"
        case "1099_div": "1099-DIV"
        case "1099_b": "1099-B"
        case "1098": "1098"
        default: "W-2"
        }
    }
}

extension EntryWords {
    /// What is wrong with a field, in a sentence: "Total is not an amount".
    static func problem(_ problem: FinanceEntryProblem, isBox: Bool = false) -> String {
        let field = String(localized: String.LocalizationValue(problem.field))
        let sentence: String
        switch problem.reason {
        case .missing: sentence = String(localized: "\(field) is empty")
        case .notAmount: sentence = String(localized: "\(field) is not an amount")
        case .notNumber: sentence = String(localized: "\(field) is not a number")
        case .notDay: sentence = String(localized: "\(field) is not a day")
        case .notCurrencyCode: sentence = String(localized: "\(field) is not a currency code")
        case .notLastFourDigits: sentence = String(localized: "\(field) is not four digits")
        case .notYear: sentence = String(localized: "\(field) is not a year")
        case .noLines: return String(localized: "Add at least one line")
        case .noBoxes: return String(localized: "Add at least one box")
        }
        guard let number = problem.lineNumber else { return sentence }
        return isBox ? String(localized: "Box \(number): \(sentence)") : String(localized: "Line \(number): \(sentence)")
    }
}

// MARK: Shared parts

/// UploadedEntryFile is a picked file that is on the server, so it is not
/// sent again when a form previews and then records.
struct UploadedEntryFile {
    let fileId: UUID
    let attachmentId: String

    /// The file on the server: the one already uploaded when it is the same
    /// file, else uploaded now. Tax documents are marked so, which has the
    /// server forget any text it would keep of them.
    static func upload(_ file: PendingFile, reusing uploaded: UploadedEntryFile?, isTaxDocument: Bool, connection: Connection) async throws
        -> UploadedEntryFile
    {
        if let uploaded, uploaded.fileId == file.id { return uploaded }
        let attachmentId = try await connection.upload(name: file.name, contentType: file.contentType, data: file.data, isTaxDocument: isTaxDocument)
        return UploadedEntryFile(fileId: file.id, attachmentId: attachmentId)
    }
}

/// EntryTextRow is a labeled field in a form: the label on the left and
/// what is typed on the right, with an example while it is empty.
private struct EntryTextRow: View {
    let title: LocalizedStringKey
    var placeholder: String?
    @Binding var text: String
    var keyboard: UIKeyboardType = .default
    /// Given for a name, a code or a number; plain words take sentence case
    /// and keep autocorrection.
    var capitalization: TextInputAutocapitalization? = nil

    var body: some View {
        LabeledContent(title) {
            TextField(title, text: $text, prompt: placeholder.map { Text(verbatim: $0) })
                .multilineTextAlignment(.trailing)
                .keyboardType(keyboard)
                .textInputAutocapitalization(capitalization ?? .sentences)
                .autocorrectionDisabled(keyboard != .default || capitalization != nil)
        }
    }
}

/// EntryFileSection picks the file a record is read from: a picture from the
/// library or the camera, or a PDF or picture from Files.
private struct EntryFileSection: View {
    @Binding var file: PendingFile?
    let footerText: LocalizedStringKey
    @State private var pickedItem: PhotosPickerItem?
    @State private var isPickingPhotos = false
    @State private var isPickingFiles = false
    @State private var isTakingPicture = false

    var body: some View {
        Section {
            if let file {
                HStack(spacing: 12) {
                    if let preview = file.preview {
                        Image(uiImage: preview)
                            .resizable()
                            .scaledToFill()
                            .frame(width: 44, height: 44)
                            .clipShape(.rect(cornerRadius: 8, style: .continuous))
                            .accessibilityHidden(true)
                    } else {
                        Image(systemName: "doc").foregroundStyle(.secondary).accessibilityHidden(true)
                    }
                    Text(verbatim: file.name).lineLimit(1)
                }
                Button("Remove the file", systemImage: "minus.circle") { self.file = nil }
            }
            Menu {
                Button("Photo library", systemImage: "photo.on.rectangle") { isPickingPhotos = true }
                if UIImagePickerController.isSourceTypeAvailable(.camera) {
                    Button("Camera", systemImage: "camera") { isTakingPicture = true }
                }
                Button("Files", systemImage: "folder") { isPickingFiles = true }
            } label: {
                Label(file == nil ? LocalizedStringKey("Add the file") : "Choose another file", systemImage: "paperclip")
            }
        } header: {
            Text("File")
        } footer: {
            Text(footerText)
        }
        .photosPicker(isPresented: $isPickingPhotos, selection: $pickedItem, matching: .images)
        .cameraPicker(isPresented: $isTakingPicture) { picture in
            if let picked = PendingFile(picture: picture, index: 0) { file = picked }
        }
        .fileImporter(isPresented: $isPickingFiles, allowedContentTypes: [.pdf, .image], allowsMultipleSelection: false) { result in
            guard case .success(let addresses) = result, let address = addresses.first else { return }
            if let picked = PendingFile(fileAt: address) { file = picked }
        }
        .onChange(of: pickedItem) { _, item in
            guard let item else { return }
            pickedItem = nil
            Task {
                if let data = try? await item.loadTransferable(type: Data.self), let picked = PendingFile(pictureData: data, index: 0) {
                    file = picked
                }
            }
        }
    }
}

/// EntryDayRow is a day in a form, picked as a date and kept as the text
/// the server takes. An optional day has a switch that leaves it empty.
private struct EntryDayRow: View {
    @Environment(FinanceModel.self) private var finance
    let title: String
    var isOptional = false
    @Binding var text: String

    var body: some View {
        if isOptional {
            OptionalDatePicker(
                title: title,
                date: Binding(
                    get: { finance.date(ofDay: text) },
                    set: { text = $0.map { finance.day($0) } ?? "" }))
        } else {
            DatePicker(
                title,
                selection: Binding(
                    get: { finance.date(ofDay: text) ?? Date() },
                    set: { text = finance.day($0) }),
                displayedComponents: .date)
        }
    }
}

/// EntryProblemFooter says why a form cannot be recorded yet: the first
/// field that is empty or not what the server takes, or the missing file.
private struct EntryProblemFooter: View {
    let isFileMissing: Bool
    let problem: FinanceEntryProblem?
    var isBox = false

    private var sentence: String? {
        if isFileMissing { return String(localized: "Add the file first") }
        guard let problem else { return nil }
        return EntryWords.problem(problem, isBox: isBox)
    }

    var body: some View {
        if let sentence {
            Section {
            } footer: {
                Text(verbatim: sentence)
            }
        }
    }
}

/// ExchangeRateLookup reads today's rate from a typed currency into the
/// person's home currency, and leaves it empty when it is the same
/// currency, not a code yet, or the server has no rate.
private struct ExchangeRateLookup: ViewModifier {
    @Environment(FinanceModel.self) private var finance
    let currencyCode: String
    @Binding var rate: CurrencyRate?

    func body(content: Content) -> some View {
        content.task(id: currencyCode.trimmed.uppercased()) {
            let typedCurrencyCode = currencyCode.trimmed.uppercased()
            let homeCurrencyCode = finance.reportingCurrency.isEmpty ? "USD" : finance.reportingCurrency.uppercased()
            rate = nil
            guard FinanceEntryText.isCurrencyCode(typedCurrencyCode), typedCurrencyCode != homeCurrencyCode else { return }
            let read = try? await finance.connection.exchangeRate(from: typedCurrencyCode, to: homeCurrencyCode)
            guard !Task.isCancelled else { return }
            rate = read
        }
    }
}

extension View {
    fileprivate func exchangeRate(of currencyCode: String, into rate: Binding<CurrencyRate?>) -> some View {
        modifier(ExchangeRateLookup(currencyCode: currencyCode, rate: rate))
    }
}

/// ExchangeRateNote is the rate under a currency field: "1 EUR = 1.0834 USD".
private struct ExchangeRateNote: View {
    let rate: CurrencyRate?

    var body: some View {
        if let rate, let shown = rate.shownRate {
            Text("1 \(rate.fromCurrencyCode) = \(shown) \(rate.toCurrencyCode)")
        }
    }
}

// MARK: Receipt

/// ReceiptEntryView types in a receipt: the file it is from, who it is
/// from, its lines and totals. Preview asks the server what recording it
/// would do (whether the lines add up, which charge it matches), and then
/// Record does it.
struct ReceiptEntryView: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let onRecorded: () -> Void
    @State private var draft: ReceiptDraft
    @State private var file: PendingFile?
    @State private var uploaded: UploadedEntryFile?
    @State private var preview: ReceiptEntryPreview?
    @State private var previewedDraft: ReceiptDraft?
    @State private var previewedFileId: UUID?
    @State private var isUnbalancedAccepted = false
    @State private var rate: CurrencyRate?
    @State private var isBusy = false
    @State private var problemText: String?

    init(currencyCode: String, today: String, onRecorded: @escaping () -> Void) {
        _draft = State(initialValue: ReceiptDraft(currencyCode: currencyCode.isEmpty ? "USD" : currencyCode, purchasedOn: today))
        self.onRecorded = onRecorded
    }

    /// Whether the preview on screen is of what is typed now.
    private var isPreviewCurrent: Bool {
        preview != nil && previewedDraft == draft && previewedFileId == file?.id
    }

    private var isRecordable: Bool {
        isPreviewCurrent && (preview?.isBalanced == true || isUnbalancedAccepted)
    }

    var body: some View {
        NavigationStack {
            Form {
                EntryFileSection(file: $file, footerText: "The picture or file the receipt is from. It is kept with the receipt.")
                Section {
                    EntryTextRow(title: "Merchant", text: $draft.merchantName, capitalization: .words)
                    EntryTextRow(title: "Receipt number", placeholder: String(localized: "Optional"), text: $draft.merchantReceiptNumber, capitalization: .never)
                    EntryDayRow(title: String(localized: "Bought on"), isOptional: true, text: $draft.purchasedOn)
                    EntryTextRow(title: "Currency", placeholder: "USD", text: $draft.currencyCode, capitalization: .characters)
                    EntryTextRow(title: "Card last digits", placeholder: String(localized: "Optional"), text: $draft.paymentAccountMask, keyboard: .numberPad)
                } footer: {
                    ExchangeRateNote(rate: rate)
                }
                ForEach($draft.receiptLines) { $line in
                    Section {
                        Picker("Kind", selection: $line.receiptLineKind) {
                            ForEach(FinanceEntryKinds.receiptLineKinds, id: \.self) { kind in
                                Text(EntryWords.receiptLineKind(kind)).tag(kind)
                            }
                        }
                        EntryTextRow(title: "Description", placeholder: String(localized: "Optional"), text: $line.description)
                        EntryTextRow(title: "Quantity", placeholder: String(localized: "Optional"), text: $line.quantity, keyboard: .decimalPad)
                        EntryTextRow(title: "Unit price", placeholder: String(localized: "Optional"), text: $line.unitPriceAmount, keyboard: .decimalPad)
                        EntryTextRow(title: "Amount", placeholder: "0.00", text: $line.lineAmount, keyboard: .numbersAndPunctuation)
                        if draft.receiptLines.count > 1 {
                            Button("Remove this line", systemImage: "trash", role: .destructive) {
                                draft.receiptLines.removeAll { $0.id == line.id }
                            }
                            .tint(.red)
                            .actionColor(isDestructive: true)
                        }
                    } header: {
                        Text("Line \(lineNumber(of: line))")
                    }
                }
                Section {
                    Button("Add a line", systemImage: "plus") { draft.receiptLines.append(ReceiptLineDraft()) }
                } footer: {
                    Text("A discount is typed below zero, as it is printed.")
                }
                Section {
                    EntryTextRow(title: "Subtotal", placeholder: String(localized: "Optional"), text: $draft.subtotalAmount, keyboard: .decimalPad)
                    EntryTextRow(title: "Total", placeholder: "0.00", text: $draft.totalAmount, keyboard: .decimalPad)
                }
                if let preview, isPreviewCurrent {
                    ReceiptPreviewSections(preview: preview, currencyCode: draft.currencyCode.trimmed.uppercased(), isUnbalancedAccepted: $isUnbalancedAccepted)
                }
                EntryProblemFooter(isFileMissing: file == nil, problem: draft.firstProblem)
                if isBusy {
                    Section { ProgressView().frame(maxWidth: .infinity) }
                }
            }
            .navigationTitle("Type in a receipt")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(isPreviewCurrent ? LocalizedStringKey("Record") : "Preview") {
                        Task {
                            if isPreviewCurrent {
                                await record()
                            } else {
                                await runPreview()
                            }
                        }
                    }
                    .disabled(file == nil || !draft.isReady || isBusy || (isPreviewCurrent && !isRecordable))
                }
            }
            .disabled(isBusy)
            .exchangeRate(of: draft.currencyCode, into: $rate)
            .toast(done: .constant(nil), problem: $problemText)
        }
    }

    private func lineNumber(of line: ReceiptLineDraft) -> Int {
        (draft.receiptLines.firstIndex { $0.id == line.id } ?? 0) + 1
    }

    private func runPreview() async {
        guard let file else { return }
        isBusy = true
        defer { isBusy = false }
        do {
            let source = try await UploadedEntryFile.upload(file, reusing: uploaded, isTaxDocument: false, connection: finance.connection)
            uploaded = source
            let read = try await finance.connection.previewRecordReceipt(draft, attachmentId: source.attachmentId)
            preview = read
            previewedDraft = draft
            previewedFileId = file.id
            isUnbalancedAccepted = false
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func record() async {
        guard let uploaded else { return }
        isBusy = true
        defer { isBusy = false }
        var recording = draft
        recording.isUnbalancedAccepted = isUnbalancedAccepted
        do {
            let recorded = try await finance.connection.recordReceipt(recording, attachmentId: uploaded.attachmentId)
            finance.doneText = recorded?.isMatched == true ? String(localized: "Receipt recorded and matched") : String(localized: "Receipt recorded")
            onRecorded()
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// ReceiptPreviewSections is what the server says recording the receipt
/// would do: whether its lines add up, whether it replaces one read from the
/// same file, and the charges it could explain.
private struct ReceiptPreviewSections: View {
    @Environment(FinanceModel.self) private var finance
    let preview: ReceiptEntryPreview
    let currencyCode: String
    @Binding var isUnbalancedAccepted: Bool

    var body: some View {
        Section {
            if preview.isBalanced {
                Label("The lines add up", systemImage: "checkmark.seal")
                    .foregroundStyle(Color(.secondaryLabel))
            } else {
                Label("The lines are off by \(FinanceMoney.shown(preview.checkDifferenceAmount, currencyCode: currencyCode))", systemImage: "exclamationmark.triangle")
                    .foregroundStyle(.orange)
                Toggle("Record it as it is", isOn: $isUnbalancedAccepted)
            }
            if preview.isReplacing == true {
                Label("It replaces the receipt already read from this file.", systemImage: "arrow.triangle.2.circlepath")
                    .foregroundStyle(Color(.secondaryLabel))
            }
        } header: {
            Text("What recording it does")
        } footer: {
            if !preview.isBalanced, let summary = preview.receiptCheckSummary, !summary.isEmpty {
                Text(verbatim: summary.asReasonSentence)
            }
        }
        Section {
            ForEach(preview.candidates) { candidate in
                LabeledContent {
                    Text(FinanceMoney.shown(candidate.matchedAmount, currencyCode: candidate.financeTransaction?.currencyCode ?? currencyCode))
                        .monospacedDigit()
                        .foregroundStyle(Color(.label))
                } label: {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: candidate.financeTransaction?.shownName ?? "")
                            .foregroundStyle(Color(.label))
                        HStack(spacing: 6) {
                            Text(verbatim: TaxWords.dayText(candidate.financeTransaction?.postedOn, in: finance))
                            if candidate.isAutomatic == true {
                                Text("Matched when recorded")
                            }
                        }
                        .font(.caption)
                        .foregroundStyle(Color(.secondaryLabel))
                    }
                }
            }
        } header: {
            Text("Charges it could explain")
        } footer: {
            if preview.candidates.isEmpty {
                Text("No charge matches it yet. It is recorded without one.")
            }
        }
    }
}

// MARK: Paystub

/// PaystubEntryView types in a paystub: the file it is from, who paid whom,
/// its totals and its lines. A paystub whose lines do not add up is refused
/// unless the person says to record it as it is.
struct PaystubEntryView: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let onRecorded: () -> Void
    @State private var draft: PaystubDraft
    @State private var file: PendingFile?
    @State private var uploaded: UploadedEntryFile?
    @State private var rate: CurrencyRate?
    @State private var isBusy = false
    @State private var problemText: String?

    init(currencyCode: String, today: String, onRecorded: @escaping () -> Void) {
        _draft = State(initialValue: PaystubDraft(currencyCode: currencyCode.isEmpty ? "USD" : currencyCode, payDate: today))
        self.onRecorded = onRecorded
    }

    var body: some View {
        NavigationStack {
            Form {
                EntryFileSection(file: $file, footerText: "The paystub's PDF or picture. The server does not keep its text, which holds a taxpayer number.")
                Section {
                    EntryTextRow(title: "Employer", text: $draft.employerName, capitalization: .words)
                    EntryTextRow(title: "Employer ID", placeholder: String(localized: "Optional"), text: $draft.employerTaxId, keyboard: .numbersAndPunctuation, capitalization: .characters)
                    EntryTextRow(title: "Employee", text: $draft.employeeName, capitalization: .words)
                    EntryTextRow(title: "Taxpayer number, last 4", placeholder: String(localized: "Optional"), text: $draft.employeeTaxIdMask, keyboard: .numberPad)
                } footer: {
                    Text("Only the last four digits of a taxpayer number are kept.")
                }
                Section {
                    EntryDayRow(title: String(localized: "Pay date"), text: $draft.payDate)
                    EntryDayRow(title: String(localized: "Period starts"), isOptional: true, text: $draft.periodStartOn)
                    EntryDayRow(title: String(localized: "Period ends"), isOptional: true, text: $draft.periodEndOn)
                    EntryTextRow(title: "Currency", placeholder: "USD", text: $draft.currencyCode, capitalization: .characters)
                } footer: {
                    ExchangeRateNote(rate: rate)
                }
                Section {
                    EntryTextRow(title: "Gross pay", placeholder: "0.00", text: $draft.grossAmount, keyboard: .decimalPad)
                    EntryTextRow(title: "Gross pay this year", placeholder: String(localized: "Optional"), text: $draft.yearToDateGrossAmount, keyboard: .decimalPad)
                    EntryTextRow(title: "Total taxes", placeholder: String(localized: "Optional"), text: $draft.totalTaxAmount, keyboard: .decimalPad)
                    EntryTextRow(title: "Total deductions", placeholder: String(localized: "Optional"), text: $draft.totalDeductionAmount, keyboard: .decimalPad)
                    EntryTextRow(title: "Take-home pay", placeholder: "0.00", text: $draft.netAmount, keyboard: .numbersAndPunctuation)
                } footer: {
                    Text("As printed, without a sign.")
                }
                ForEach($draft.paystubLines) { $line in
                    Section {
                        PaystubLineFields(line: $line)
                        if draft.paystubLines.count > 1 {
                            Button("Remove this line", systemImage: "trash", role: .destructive) {
                                draft.paystubLines.removeAll { $0.id == line.id }
                            }
                            .tint(.red)
                            .actionColor(isDestructive: true)
                        }
                    } header: {
                        Text("Line \(lineNumber(of: line))")
                    }
                }
                Section {
                    Button("Add a line", systemImage: "plus") { draft.paystubLines.append(PaystubLineDraft()) }
                }
                Section {
                    Toggle("Record it as it is", isOn: $draft.isUnbalancedAccepted)
                } footer: {
                    Text("A paystub whose lines do not add up to its totals and take-home pay is refused unless you record it as it is.")
                }
                EntryProblemFooter(isFileMissing: file == nil, problem: draft.firstProblem)
            }
            .navigationTitle("Type in a paystub")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Record") { Task { await record() } }
                        .disabled(file == nil || !draft.isReady || isBusy)
                }
            }
            .disabled(isBusy)
            .exchangeRate(of: draft.currencyCode, into: $rate)
            .toast(done: .constant(nil), problem: $problemText)
        }
    }

    private func lineNumber(of line: PaystubLineDraft) -> Int {
        (draft.paystubLines.firstIndex { $0.id == line.id } ?? 0) + 1
    }

    private func record() async {
        guard let file else { return }
        isBusy = true
        defer { isBusy = false }
        do {
            let source = try await UploadedEntryFile.upload(file, reusing: uploaded, isTaxDocument: true, connection: finance.connection)
            uploaded = source
            _ = try await finance.connection.recordPaystub(draft, attachmentId: source.attachmentId)
            finance.doneText = String(localized: "Paystub recorded")
            onRecorded()
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

/// PaystubLineFields are the fields of one paystub line: what every line
/// has, then what its kind takes.
private struct PaystubLineFields: View {
    @Binding var line: PaystubLineDraft

    var body: some View {
        Picker("Kind", selection: $line.paystubLineKind) {
            ForEach(FinanceEntryKinds.paystubLineKinds, id: \.self) { kind in
                Text(EntryWords.paystubLineKind(kind)).tag(kind)
            }
        }
        EntryTextRow(title: "Description", placeholder: String(localized: "Optional"), text: $line.description)
        switch line.paystubLineKind {
        case "earning":
            EntryTextRow(title: "Hours", placeholder: String(localized: "Optional"), text: $line.hours, keyboard: .decimalPad)
            EntryTextRow(title: "Rate", placeholder: String(localized: "Optional"), text: $line.rateAmount, keyboard: .decimalPad)
        case "tax":
            Picker("Which tax", selection: $line.paystubTaxKind) {
                ForEach(FinanceEntryKinds.paystubTaxKinds, id: \.self) { kind in
                    Text(EntryWords.paystubTaxKind(kind)).tag(kind)
                }
            }
            EntryTextRow(title: "State or place", placeholder: String(localized: "Optional"), text: $line.taxJurisdictionCode, capitalization: .characters)
            EntryTextRow(title: "Wages this period", placeholder: String(localized: "Optional"), text: $line.currentWagesAmount, keyboard: .decimalPad)
            EntryTextRow(title: "Wages this year", placeholder: String(localized: "Optional"), text: $line.yearToDateWagesAmount, keyboard: .decimalPad)
        case "deduction":
            Picker("Pays for", selection: $line.paystubDeductionKind) {
                ForEach(FinanceEntryKinds.paystubDeductionKinds, id: \.self) { kind in
                    Text(EntryWords.paystubDeductionKind(kind)).tag(kind)
                }
            }
            EntryTextRow(title: "Employer adds this period", placeholder: String(localized: "Optional"), text: $line.employerCurrentAmount, keyboard: .decimalPad)
            EntryTextRow(title: "Employer adds this year", placeholder: String(localized: "Optional"), text: $line.employerYearToDateAmount, keyboard: .decimalPad)
        default:
            EmptyView()
        }
        EntryTextRow(title: "Amount this period", placeholder: "0.00", text: $line.currentAmount, keyboard: .numbersAndPunctuation)
        EntryTextRow(title: "Amount this year", placeholder: String(localized: "Optional"), text: $line.yearToDateAmount, keyboard: .numbersAndPunctuation)
    }
}

// MARK: Tax form

/// TaxFormEntryView types in a W-2, 1099 or 1098: the file it is from, its
/// kind and year, who paid whom, and the boxes that print something.
struct TaxFormEntryView: View {
    @Environment(FinanceModel.self) private var finance
    @Environment(\.dismiss) private var dismiss
    let onRecorded: () -> Void
    @State private var draft: TaxFormDraft
    @State private var file: PendingFile?
    @State private var uploaded: UploadedEntryFile?
    @State private var isBusy = false
    @State private var problemText: String?

    init(taxYear: Int, onRecorded: @escaping () -> Void) {
        _draft = State(initialValue: TaxFormDraft(taxYear: taxYear))
        self.onRecorded = onRecorded
    }

    /// This year and the five before it, and the year chosen when it is older.
    private var taxYears: [Int] {
        let thisYear = finance.dates.component(.year, from: Date())
        let years = (0..<6).map { thisYear - $0 }
        return years.contains(draft.taxYear) ? years : years + [draft.taxYear]
    }

    var body: some View {
        NavigationStack {
            Form {
                EntryFileSection(file: $file, footerText: "The form's PDF or picture. The server does not keep its text, which holds a taxpayer number.")
                Section {
                    Picker("Form", selection: $draft.taxFormKind) {
                        ForEach(FinanceEntryKinds.taxFormKinds, id: \.self) { kind in
                            Text(verbatim: EntryWords.taxFormKind(kind)).tag(kind)
                        }
                    }
                    Picker("Tax year", selection: $draft.taxYear) {
                        ForEach(taxYears, id: \.self) { year in
                            Text(verbatim: String(year)).tag(year)
                        }
                    }
                    Toggle("Corrected form", isOn: $draft.isCorrected)
                }
                Section {
                    EntryTextRow(title: "Payer", text: $draft.payerName, capitalization: .words)
                    EntryTextRow(title: "Payer ID", placeholder: String(localized: "Optional"), text: $draft.payerTaxId, keyboard: .numbersAndPunctuation, capitalization: .characters)
                    EntryTextRow(title: "Recipient", text: $draft.recipientName, capitalization: .words)
                    EntryTextRow(title: "Taxpayer number, last 4", placeholder: String(localized: "Optional"), text: $draft.recipientTaxIdMask, keyboard: .numberPad)
                } footer: {
                    Text("Only the last four digits of a taxpayer number are kept.")
                }
                ForEach($draft.taxFormEntries) { $entry in
                    Section {
                        EntryTextRow(title: "Box", placeholder: "1", text: $entry.boxCode, keyboard: .numbersAndPunctuation, capitalization: .never)
                        EntryTextRow(title: "Letter, state or label", placeholder: String(localized: "Optional"), text: $entry.boxQualifier)
                        EntryTextRow(title: "Amount", placeholder: String(localized: "Optional"), text: $entry.amount, keyboard: .decimalPad)
                        EntryTextRow(title: "Text", placeholder: String(localized: "Optional"), text: $entry.textValue)
                        Toggle("Checked", isOn: $entry.isChecked)
                        if draft.taxFormEntries.count > 1 {
                            Button("Remove this box", systemImage: "trash", role: .destructive) {
                                draft.taxFormEntries.removeAll { $0.id == entry.id }
                            }
                            .tint(.red)
                            .actionColor(isDestructive: true)
                        }
                    } header: {
                        Text("Box \(entryNumber(of: entry))")
                    } footer: {
                        if let title = TaxWords.boxTitle(draft.taxFormKind, entry.boxCode.trimmed.lowercased()) {
                            Text(verbatim: title)
                        }
                    }
                }
                Section {
                    Button("Add a box", systemImage: "plus") { draft.taxFormEntries.append(TaxFormEntryDraft()) }
                } footer: {
                    Text("Only the boxes that print something.")
                }
                EntryProblemFooter(isFileMissing: file == nil, problem: draft.firstProblem, isBox: true)
            }
            .navigationTitle("Type in a tax form")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Record") { Task { await record() } }
                        .disabled(file == nil || !draft.isReady || isBusy)
                }
            }
            .disabled(isBusy)
            .toast(done: .constant(nil), problem: $problemText)
        }
    }

    private func entryNumber(of entry: TaxFormEntryDraft) -> Int {
        (draft.taxFormEntries.firstIndex { $0.id == entry.id } ?? 0) + 1
    }

    private func record() async {
        guard let file else { return }
        isBusy = true
        defer { isBusy = false }
        do {
            let source = try await UploadedEntryFile.upload(file, reusing: uploaded, isTaxDocument: true, connection: finance.connection)
            uploaded = source
            _ = try await finance.connection.recordTaxForm(draft, attachmentId: source.attachmentId)
            finance.doneText = String(localized: "Tax form recorded")
            onRecorded()
            dismiss()
        } catch {
            problemText = error.localizedDescription
        }
    }
}

// MARK: Demo

#if DEBUG
    extension DemoServer {
        /// The demo answers to the typed-in record operations: a receipt that
        /// adds up and matches one charge, a paystub, a tax form and a rate.
        /// Longer names first, since PreviewRecordReceipt holds RecordReceipt.
        static func financeEntryAnswer(query: String) -> String? {
            if query.contains("PreviewRecordReceipt(") {
                return """
                    {"data":{"PreviewRecordReceipt":{"receiptCheckState":"balanced","checkDifferenceAmount":"0","receiptCheckSummary":"The lines add up.","isReplacing":false,
                    "receiptMatchCandidates":[{"financeTransactionId":"charge-corner-market","financeTransaction":{"id":"charge-corner-market","postedOn":"2026-09-10","amount":"-42.17","currencyCode":"USD","merchantName":"Corner Market","description":"CORNER MARKET 0042"},
                    "matchedAmount":"42.17","isExactAmount":true,"isSameAccount":true,"isMerchantNameShared":true,"dayDistanceCount":0,"isAutomatic":true,"matchConfidence":"high"}]}}}
                    """
            }
            if query.contains("RecordReceipt(") {
                return """
                    {"data":{"RecordReceipt":{"financeReceipt":{"id":"receipt-typed","merchantName":"Corner Market","currencyCode":"USD","totalAmount":"42.17","receiptCheckState":"balanced","receiptLines":[],
                    "receiptMatches":[{"receiptId":"receipt-typed","financeTransactionId":"charge-corner-market","matchedAmount":"42.17","receiptMatchSource":"receipt_matcher"}]},
                    "receiptCheckSummary":"The lines add up.","receiptMatchCandidates":[],"isReplaced":false,"droppedReceiptMatchReasons":[]}}}
                    """
            }
            if query.contains("RecordPaystub(") {
                return #"{"data":{"RecordPaystub":{"id":"paystub-typed","employerName":"Example Payroll","payDate":"2026-09-30","currencyCode":"USD","grossAmount":"4000.00","netAmount":"3100.00","paystubCheckState":"balanced","paystubLines":[]}}}"#
            }
            if query.contains("RecordTaxForm(") {
                return #"{"data":{"RecordTaxForm":{"financeTaxForm":{"id":"form-typed","taxFormKind":"1099_int","taxYear":2025,"payerName":"Sample Bank","taxFormEntries":[]},"taxFormFindings":[]}}}"#
            }
            if query.contains("ExchangeRate(") {
                return #"{"data":{"ExchangeRate":{"fromCurrencyCode":"EUR","toCurrencyCode":"USD","rate":"1.08340000","rateOn":"2026-10-16","rateSource":"central_bank"}}}"#
            }
            return nil
        }
    }
#endif
