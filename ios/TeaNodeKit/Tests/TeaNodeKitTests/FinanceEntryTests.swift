import Foundation
import XCTest

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

@testable import TeaNodeKit

final class FinanceEntryTests: XCTestCase {
    private func connection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    private func variables(of request: URLRequest?) throws -> [String: Any] {
        let body = try XCTUnwrap(request?.httpBody)
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
        return try XCTUnwrap(sent["variables"] as? [String: Any])
    }

    private func receiptDraft() -> ReceiptDraft {
        var draft = ReceiptDraft(currencyCode: "usd", purchasedOn: "2026-09-10")
        draft.merchantName = " Corner Market "
        draft.totalAmount = "12.50"
        draft.subtotalAmount = "11.50"
        draft.paymentAccountMask = "1234"
        var apples = ReceiptLineDraft()
        apples.description = "Apples"
        apples.quantity = "2"
        apples.unitPriceAmount = "3.00"
        apples.lineAmount = "6.00"
        var bread = ReceiptLineDraft()
        bread.description = "Bread"
        bread.lineAmount = "5.50"
        var bag = ReceiptLineDraft()
        bag.receiptLineKind = "fee"
        bag.lineAmount = "1.00"
        draft.receiptLines = [apples, bread, ReceiptLineDraft(), bag]
        return draft
    }

    func testAReceiptIsReadyWhenItHasMerchantTotalAndLinesThatAreNumbers() {
        var draft = receiptDraft()
        XCTAssertTrue(draft.isReady)
        draft.receiptLines[0].lineAmount = "six"
        XCTAssertFalse(draft.isReady)
        draft = receiptDraft()
        draft.totalAmount = "-1"
        XCTAssertFalse(draft.isReady)
        draft = receiptDraft()
        draft.currencyCode = "US"
        XCTAssertFalse(draft.isReady)
        draft = receiptDraft()
        draft.purchasedOn = "10 September"
        XCTAssertFalse(draft.isReady)
        draft = receiptDraft()
        draft.receiptLines = [ReceiptLineDraft()]
        XCTAssertFalse(draft.isReady, "a receipt of blank lines has no lines")
    }

    func testADiscountLineMayBeBelowZeroButAnItemsUnitPriceMayNot() {
        var line = ReceiptLineDraft()
        line.receiptLineKind = "discount"
        line.lineAmount = "-1.25"
        XCTAssertTrue(line.isValid)
        line.unitPriceAmount = "-1"
        XCTAssertFalse(line.isValid)
    }

    func testAReceiptIsPreviewedWithItsFileAndOnlyRecordedAsUnbalancedWhenAccepted() async throws {
        let body = """
            {"data":{"PreviewRecordReceipt":{"receiptCheckState":"unbalanced","checkDifferenceAmount":"-1.00",
            "receiptCheckSummary":"The lines come to 1.00 less than the total.","isReplacing":false,
            "receiptMatchCandidates":[{"financeTransactionId":"charge-1","financeTransaction":{"id":"charge-1","amount":"-12.50","currencyCode":"USD","merchantName":"Corner Market"},
            "matchedAmount":"12.50","isExactAmount":true,"isSameAccount":false,"isMerchantNameShared":true,"dayDistanceCount":1,"isAutomatic":true,"matchConfidence":"high"}]}}}
            """
        let (connection, fake) = try connection(answering: body)
        var draft = receiptDraft()
        draft.isUnbalancedAccepted = true
        let previewed = try await connection.previewRecordReceipt(draft, attachmentId: "attachment-1")
        let preview = try XCTUnwrap(previewed)
        XCTAssertFalse(preview.isBalanced)
        XCTAssertEqual(preview.checkDifferenceAmount, "-1.00")
        XCTAssertEqual(preview.candidates.map(\.financeTransactionId), ["charge-1"])
        XCTAssertEqual(preview.candidates.first?.isAutomatic, true)
        XCTAssertEqual(preview.candidates.first?.financeTransaction?.shownName, "Corner Market")

        let sent = try variables(of: fake.sent.first)
        XCTAssertEqual(sent["agentAttachmentId"] as? String, "attachment-1")
        XCTAssertEqual(sent["merchantName"] as? String, "Corner Market")
        XCTAssertEqual(sent["currencyCode"] as? String, "USD")
        XCTAssertEqual(sent["purchasedOn"] as? String, "2026-09-10")
        XCTAssertEqual(sent["subtotalAmount"] as? String, "11.50")
        XCTAssertEqual(sent["paymentAccountMask"] as? String, "1234")
        XCTAssertNil(sent["merchantReceiptNumber"], "an empty field is left out")
        XCTAssertNil(sent["isUnbalancedAccepted"], "a preview writes nothing, so it accepts nothing")
        let lines = try XCTUnwrap(sent["receiptLines"] as? [[String: Any]])
        XCTAssertEqual(lines.count, 3, "the blank line is left out")
        XCTAssertEqual(lines[0]["quantity"] as? String, "2")
        XCTAssertEqual(lines[0]["unitPriceAmount"] as? String, "3.00")
        XCTAssertEqual(lines[0]["receiptLineKind"] as? String, "item")
        XCTAssertEqual(lines[2]["receiptLineKind"] as? String, "fee")
        XCTAssertNil(lines[1]["quantity"])
        let query = try XCTUnwrap(try JSONSerialization.jsonObject(with: XCTUnwrap(fake.sent.first?.httpBody)) as? [String: Any])["query"] as? String
        XCTAssertTrue(try XCTUnwrap(query).contains("PreviewRecordReceipt("))
        XCTAssertTrue(try XCTUnwrap(query).hasPrefix("query"))
    }

    func testARecordedReceiptSaysWhetherItWasMatched() async throws {
        let body = """
            {"data":{"RecordReceipt":{"financeReceipt":{"id":"receipt-1","merchantName":"Corner Market","currencyCode":"USD","totalAmount":"12.50",
            "receiptLines":[],"receiptMatches":[{"receiptId":"receipt-1","financeTransactionId":"charge-1","matchedAmount":"12.50","receiptMatchSource":"receipt_matcher"}]},
            "receiptCheckSummary":"The lines add up.","receiptMatchCandidates":[],"isReplaced":true,"droppedReceiptMatchReasons":[]}}}
            """
        let (connection, fake) = try connection(answering: body)
        var draft = receiptDraft()
        draft.isUnbalancedAccepted = true
        let recordedReceipt = try await connection.recordReceipt(draft, attachmentId: "attachment-1")
        let recorded = try XCTUnwrap(recordedReceipt)
        XCTAssertEqual(recorded.financeReceipt?.id, "receipt-1")
        XCTAssertTrue(recorded.isMatched)
        XCTAssertEqual(recorded.isReplaced, true)
        XCTAssertEqual(try variables(of: fake.sent.first)["isUnbalancedAccepted"] as? Bool, true)
    }

    func testAPaystubTakesOnlyWhatItsLinesKindUses() async throws {
        var draft = PaystubDraft(currencyCode: "usd", payDate: "2026-09-30")
        draft.employerName = "Example Payroll"
        draft.employeeName = "Robin Example"
        draft.employeeTaxIdMask = "1234"
        draft.grossAmount = "4000.00"
        draft.netAmount = "3100.00"
        var earning = PaystubLineDraft()
        earning.description = "Regular"
        earning.hours = "80"
        earning.rateAmount = "50.00"
        earning.currentAmount = "4000.00"
        earning.taxJurisdictionCode = "ZZ"
        var tax = PaystubLineDraft()
        tax.paystubLineKind = "tax"
        tax.paystubTaxKind = "state_income"
        tax.taxJurisdictionCode = "ZZ"
        tax.currentAmount = "400.00"
        tax.currentWagesAmount = "4000.00"
        var deduction = PaystubLineDraft()
        deduction.paystubLineKind = "deduction"
        deduction.paystubDeductionKind = "hsa"
        deduction.currentAmount = "500.00"
        deduction.employerCurrentAmount = "100.00"
        deduction.hours = "9"
        draft.paystubLines = [earning, tax, deduction, PaystubLineDraft()]
        XCTAssertTrue(draft.isReady)

        let body = #"{"data":{"RecordPaystub":{"id":"paystub-1","employerName":"Example Payroll","payDate":"2026-09-30","grossAmount":"4000.00","netAmount":"3100.00","paystubCheckState":"balanced","paystubLines":[]}}}"#
        let (connection, fake) = try connection(answering: body)
        let recordedPaystub = try await connection.recordPaystub(draft, attachmentId: "attachment-2")
        let recorded = try XCTUnwrap(recordedPaystub)
        XCTAssertEqual(recorded.id, "paystub-1")
        XCTAssertTrue(recorded.isBalanced)

        let sent = try variables(of: fake.sent.first)
        XCTAssertEqual(sent["agentAttachmentId"] as? String, "attachment-2")
        XCTAssertEqual(sent["currencyCode"] as? String, "USD")
        XCTAssertEqual(sent["employeeTaxIdMask"] as? String, "1234")
        XCTAssertNil(sent["isUnbalancedAccepted"])
        let lines = try XCTUnwrap(sent["paystubLines"] as? [[String: Any]])
        XCTAssertEqual(lines.count, 3)
        XCTAssertEqual(lines[0]["hours"] as? String, "80")
        XCTAssertEqual(lines[0]["rateAmount"] as? String, "50.00")
        XCTAssertNil(lines[0]["taxJurisdictionCode"], "an earning has no jurisdiction")
        XCTAssertEqual(lines[1]["paystubTaxKind"] as? String, "state_income")
        XCTAssertEqual(lines[1]["currentWagesAmount"] as? String, "4000.00")
        XCTAssertNil(lines[1]["paystubDeductionKind"])
        XCTAssertEqual(lines[2]["paystubDeductionKind"] as? String, "hsa")
        XCTAssertEqual(lines[2]["employerCurrentAmount"] as? String, "100.00")
        XCTAssertNil(lines[2]["hours"], "a deduction has no hours")
    }

    func testAPaystubNeedsAPayDateTakeHomePayAndLinesWithAnAmount() {
        var draft = PaystubDraft(currencyCode: "USD", payDate: "2026-09-30")
        draft.employerName = "Example Payroll"
        draft.employeeName = "Robin Example"
        draft.grossAmount = "100.00"
        draft.netAmount = "-5.00"
        var line = PaystubLineDraft()
        line.currentAmount = "100.00"
        draft.paystubLines = [line]
        XCTAssertTrue(draft.isReady, "take-home pay may be below zero")
        draft.grossAmount = "-100.00"
        XCTAssertFalse(draft.isReady)
        draft.grossAmount = "100.00"
        draft.payDate = ""
        XCTAssertFalse(draft.isReady)
        draft.payDate = "2026-09-30"
        draft.employeeTaxIdMask = "123"
        XCTAssertFalse(draft.isReady, "the mask is the last four digits")
        draft.employeeTaxIdMask = ""
        draft.paystubLines[0].currentAmount = ""
        draft.paystubLines[0].description = "Regular"
        XCTAssertFalse(draft.isReady, "a line with no amount is refused")
    }

    func testATaxFormIsSentWithItsKindYearAndBoxes() async throws {
        var draft = TaxFormDraft(taxYear: 2025)
        draft.taxFormKind = "1099_int"
        draft.payerName = "Sample Bank"
        draft.recipientName = "Robin Example"
        draft.recipientTaxIdMask = "4321"
        var interest = TaxFormEntryDraft()
        interest.boxCode = " 1 "
        interest.amount = "120.50"
        var state = TaxFormEntryDraft()
        state.boxCode = "16"
        state.boxQualifier = "ZZ"
        state.textValue = "123"
        var mark = TaxFormEntryDraft()
        mark.boxCode = "13"
        mark.isChecked = true
        draft.isCorrected = true
        draft.taxFormEntries = [interest, TaxFormEntryDraft(), state, mark]
        XCTAssertTrue(draft.isReady)

        let body = #"{"data":{"RecordTaxForm":{"financeTaxForm":{"id":"form-1","taxFormKind":"1099_int","taxYear":2025,"payerName":"Sample Bank","taxFormEntries":[]},"taxFormFindings":[]}}}"#
        let (connection, fake) = try connection(answering: body)
        let recordedTaxForm = try await connection.recordTaxForm(draft, attachmentId: "attachment-3")
        let recorded = try XCTUnwrap(recordedTaxForm)
        XCTAssertEqual(recorded.id, "form-1")
        XCTAssertEqual(recorded.financeTaxForm?.kindName, "1099-INT")

        let sent = try variables(of: fake.sent.first)
        XCTAssertEqual(sent["taxFormKind"] as? String, "1099_int")
        XCTAssertEqual(sent["taxYear"] as? Int, 2025)
        XCTAssertEqual(sent["isCorrected"] as? Bool, true)
        XCTAssertEqual(sent["recipientTaxIdMask"] as? String, "4321")
        XCTAssertNil(sent["payerTaxId"])
        let entries = try XCTUnwrap(sent["taxFormEntries"] as? [[String: Any]])
        XCTAssertEqual(entries.count, 3, "the empty box is left out")
        XCTAssertEqual(entries[0]["boxCode"] as? String, "1")
        XCTAssertEqual(entries[0]["amount"] as? String, "120.50")
        XCTAssertNil(entries[0]["isChecked"])
        XCTAssertEqual(entries[1]["boxQualifier"] as? String, "ZZ")
        XCTAssertEqual(entries[1]["textValue"] as? String, "123")
        XCTAssertEqual(entries[2]["isChecked"] as? Bool, true)
    }

    func testATaxFormNeedsABoxNumberOnEveryFilledBox() {
        var draft = TaxFormDraft(taxYear: 2025)
        draft.payerName = "Sample Bank"
        draft.recipientName = "Robin Example"
        draft.taxFormEntries[0].amount = "10.00"
        XCTAssertFalse(draft.isReady, "an amount with no box")
        draft.taxFormEntries[0].boxCode = "1"
        XCTAssertTrue(draft.isReady)
        draft.taxFormEntries[0].amount = "ten"
        XCTAssertFalse(draft.isReady)
        draft.taxFormEntries[0].amount = "10.00"
        draft.taxYear = 25
        XCTAssertFalse(draft.isReady)
    }

    func testAnExchangeRateIsAskedForAndShown() async throws {
        let body = #"{"data":{"ExchangeRate":{"fromCurrencyCode":"EUR","toCurrencyCode":"USD","rate":"1.08340000","rateOn":"2026-10-16","rateSource":"central_bank"}}}"#
        let (connection, fake) = try connection(answering: body)
        let read = try await connection.exchangeRate(from: "eur", to: "usd")
        let rate = try XCTUnwrap(read)
        XCTAssertEqual(rate.toCurrencyCode, "USD")
        XCTAssertEqual(rate.shownRate, "1.0834")
        let sent = try variables(of: fake.sent.first)
        XCTAssertEqual(sent["fromCurrencyCode"] as? String, "EUR")
        XCTAssertEqual(sent["toCurrencyCode"] as? String, "USD")
        XCTAssertNil(sent["rateOn"], "today's rate")
    }

    func testARateThatIsNotAPositiveNumberShowsNothing() throws {
        let rate = try JSONDecoder().decode(CurrencyRate.self, from: Data(#"{"fromCurrencyCode":"EUR","toCurrencyCode":"USD","rate":"0"}"#.utf8))
        XCTAssertNil(rate.shownRate)
    }

    func testARateIsShownToFiveSignificantDigitsWithoutTrailingZeros() throws {
        func shown(_ rate: String) throws -> String? {
            let json = #"{"fromCurrencyCode":"EUR","toCurrencyCode":"USD","rate":"\#(rate)"}"#
            return try JSONDecoder().decode(CurrencyRate.self, from: Data(json.utf8)).shownRate
        }
        XCTAssertEqual(try shown("1.08340000"), "1.0834")
        XCTAssertEqual(try shown("1.08337"), "1.0834")
        XCTAssertEqual(try shown("1.50000000"), "1.5")
        XCTAssertEqual(try shown("0.0092"), "0.0092")
        XCTAssertEqual(try shown("150.123456"), "150.12")
        XCTAssertEqual(try shown("2"), "2")
    }

    func testNumbersTypedWithACommaAreReadAsDecimalPoints() {
        XCTAssertEqual(FinanceEntryText.normalized(" 12,50 "), "12.50")
        XCTAssertEqual(FinanceEntryText.normalized("1,250.00"), "1,250.00", "a comma beside a point is a thousands mark, which the server refuses")
        XCTAssertTrue(FinanceEntryText.isAmount("12,5", isNegativeAllowed: false))
        XCTAssertFalse(FinanceEntryText.isAmount("1,250.00", isNegativeAllowed: false))
        XCTAssertTrue(FinanceEntryText.isQuantity("0,5"))
        var draft = receiptDraft()
        draft.totalAmount = "12,50"
        XCTAssertTrue(draft.isReady)
        XCTAssertEqual(draft.variables(attachmentId: "attachment-1", isRecording: false)["totalAmount"], .string("12.50"))
    }

    func testAFormNamesTheFirstFieldThatIsNotReady() {
        var draft = receiptDraft()
        XCTAssertNil(draft.firstProblem)
        draft.totalAmount = "twelve"
        XCTAssertEqual(draft.firstProblem, FinanceEntryProblem(field: "Total", reason: .notAmount))
        draft.merchantName = " "
        XCTAssertEqual(draft.firstProblem, FinanceEntryProblem(field: "Merchant", reason: .missing), "the first in the order of the form")
        draft = receiptDraft()
        draft.receiptLines[1].lineAmount = ""
        XCTAssertEqual(draft.firstProblem, FinanceEntryProblem(field: "Amount", reason: .missing, lineNumber: 2))
        draft.receiptLines = [ReceiptLineDraft()]
        XCTAssertEqual(draft.firstProblem?.reason, .noLines)

        var paystub = PaystubDraft(currencyCode: "USD", payDate: "")
        paystub.employerName = "Example Payroll"
        paystub.employeeName = "Robin Example"
        XCTAssertEqual(paystub.firstProblem, FinanceEntryProblem(field: "Pay date", reason: .missing))

        var form = TaxFormDraft(taxYear: 2025)
        form.payerName = "Sample Bank"
        form.recipientName = "Robin Example"
        XCTAssertEqual(form.firstProblem?.reason, .noBoxes)
        form.taxFormEntries[0].amount = "10"
        XCTAssertEqual(form.firstProblem, FinanceEntryProblem(field: "Box", reason: .missing, lineNumber: 1))
    }
}
