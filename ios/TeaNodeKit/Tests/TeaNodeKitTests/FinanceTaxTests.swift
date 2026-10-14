import Foundation
import XCTest

@testable import TeaNodeKit

final class FinanceTaxTests: XCTestCase {
    private func connection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    func testATaxDocumentIsUploadedAsOne() async throws {
        let (connection, fake) = try connection(answering: #"{"attachments":[{"id":"attachment-1"}]}"#)
        let attachmentId = try await connection.upload(name: "stub.pdf", contentType: "application/pdf", data: Data("%PDF".utf8), isTaxDocument: true)
        XCTAssertEqual(attachmentId, "attachment-1")
        XCTAssertEqual(fake.sent.first?.url?.path, "/api/v1/agent/attachments")
        XCTAssertEqual(fake.sent.first?.url?.query, "isTaxDocument=true")
        _ = try await connection.upload(name: "picture.jpg", contentType: "image/jpeg", data: Data([0xFF]))
        XCTAssertNil(fake.sent.last?.url?.query)
    }

    func testATaxYearIsReadWithItsEmploymentsAndFindings() async throws {
        let figures = #"{"wagesAmount":"8000.00","federalIncomeTaxAmount":"900.00","socialSecurityWagesAmount":"8000.00","socialSecurityTaxAmount":"496.00","medicareWagesAmount":"8000.00","medicareTaxAmount":"116.00","dependentCareAmount":"0.00","retirementTraditionalAmount":"400.00","retirementRothAmount":"0.00","hsaAmount":"0.00","healthCoverageAmount":"0.00","localIncomeTaxAmount":"0.00","taxYearStateFigures":[{"stateCode":"ZZ","stateWagesAmount":"8000.00","stateIncomeTaxAmount":"0.00"}]}"#
        let projected = #"{"wagesAmount":"104000.00","federalIncomeTaxAmount":"11700.00","socialSecurityWagesAmount":"104000.00","socialSecurityTaxAmount":"6448.00","medicareWagesAmount":"104000.00","medicareTaxAmount":"1508.00","dependentCareAmount":"0.00","retirementTraditionalAmount":"5200.00","retirementRothAmount":"0.00","hsaAmount":"0.00","healthCoverageAmount":"0.00","localIncomeTaxAmount":"0.00","taxYearStateFigures":[{"stateCode":"ZZ","stateWagesAmount":"104000.00","stateIncomeTaxAmount":"0.00"}]}"#
        let body = """
            {"data":{"TaxYear":{"taxYear":2026,"socialSecurityWageBaseAmount":null,"isYearOver":false,"paystubCount":2,"taxFormCount":0,
            "figures":\(figures),"projectedFigures":\(projected),
            "taxYearEmployments":[{"employeeName":"Robin Example","employerName":"Example Payroll","employerTaxId":null,"taxYearFigureSource":"paystubs",
              "taxFormId":null,"latestPayDate":"2026-01-30","paystubCount":2,"payCadenceDayCount":14,"isPaystubYearComplete":false,"remainingPayCount":24,
              "figures":\(figures),"paystubFigures":\(figures),"projectedFigures":\(projected),"taxYearComparisons":[]}],
            "taxYearPeople":[],
            "taxYearFindings":[{"taxYearFindingCode":"pay_date_gap","employeeName":"Robin Example","employerName":"Example Payroll",
              "findingText":"A paystub between two pay dates is missing.","paystubIds":["paystub-1","paystub-2"]}]}}}
            """
        let (connection, _) = try connection(answering: body)
        let read = try await connection.taxYear(2026)
        let summary = try XCTUnwrap(read)
        XCTAssertEqual(summary.employments.count, 1)
        XCTAssertEqual(summary.employments.first?.isFromW2, false)
        XCTAssertEqual(summary.employments.first?.remainingPayCount, 24)
        XCTAssertEqual(summary.findings.first?.paystubIds, ["paystub-1", "paystub-2"])

        let rows = try XCTUnwrap(summary.figures).rows(projectedTo: summary.projectedFigures)
        XCTAssertEqual(rows.map(\.figureKind), [
            .wages, .federalIncomeTax, .socialSecurityWages, .socialSecurityTax, .medicareWages, .medicareTax,
            .retirementTraditional, .stateWages(stateCode: "ZZ"),
        ])
        XCTAssertEqual(rows.first?.projectedAmount, "104000.00")
    }

    func testTheEstimateIsAskedForAFilingStatusAndRingsWhatIsNotNothing() async throws {
        let body = #"{"data":{"TaxYear":{"taxYear":2026,"retirementDeferralLimitAmount":"24500.00","taxYearEstimate":{"taxFilingStatus":"head_of_household","isProjected":true,"payAmount":"110000.00","paidTaxAmount":"9000.00","upcomingWithheldTaxAmount":"12033.00","owingTaxAmount":"0.00","refundAmount":"0.00","remainingIncomeAmount":"88967.00","totalTaxAmount":"21033.00"}}}}"#
        let (connection, fake) = try connection(answering: body)
        let read = try await connection.taxYear(2026, filingStatus: .headOfHousehold)
        let estimate = try XCTUnwrap(read?.taxYearEstimate)
        XCTAssertEqual(estimate.filingStatus, .headOfHousehold)
        XCTAssertEqual(estimate.slices.map(\.sliceKind), [.paid, .upcomingWithheld, .remainingIncome])
        XCTAssertEqual(read?.retirementDeferralLimitAmount, "24500.00")
        let sent = try JSONSerialization.jsonObject(with: try XCTUnwrap(fake.sent.first?.httpBody)) as? [String: Any]
        let variables = sent?["variables"] as? [String: Any]
        XCTAssertEqual(variables?["taxFilingStatus"] as? String, "head_of_household")
        XCTAssertEqual(try XCTUnwrap(estimate.shareOfPay(estimate.totalTaxAmount)), 0.1912, accuracy: 0.0001)

        let (keeping, kept) = try self.connection(answering: #"{"data":{"SetTaxFilingStatus":"single"}}"#)
        try await keeping.setTaxFilingStatus(.single)
        let keptBody = try JSONSerialization.jsonObject(with: try XCTUnwrap(kept.sent.first?.httpBody)) as? [String: Any]
        XCTAssertEqual((keptBody?["variables"] as? [String: Any])?["taxFilingStatus"] as? String, "single")
    }

    func testTheTaxDocumentAddressIsReadAndRenewed() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"TaxDocumentAddress":{"taxDocumentAddress":"robin+taxes-invented0token01@example.com","canReadPdfs":true},"RegenerateTaxDocumentAddress":{"taxDocumentAddress":"robin+taxes-invented0token02@example.com","canReadPdfs":true}}}"#)
        let address = try await connection.taxDocumentAddress()
        XCTAssertEqual(address?.taxDocumentAddress, "robin+taxes-invented0token01@example.com")
        XCTAssertEqual(address?.canReadPdfs, true)
        let renewed = try await connection.regenerateTaxDocumentAddress()
        XCTAssertEqual(renewed?.taxDocumentAddress, "robin+taxes-invented0token02@example.com")
        let sentQuery = try JSONSerialization.jsonObject(with: try XCTUnwrap(fake.sent.last?.httpBody)) as? [String: Any]
        XCTAssertTrue((sentQuery?["query"] as? String)?.contains("RegenerateTaxDocumentAddress") == true)
    }

    func testA1099YearIsReadWithItsInvestmentIncome() async throws {
        let body = #"{"data":{"TaxYear":{"taxYear":2026,"taxYearInvestment":{"interestAmount":"1000.00","ordinaryDividendsAmount":"2000.00","qualifiedDividendsAmount":"1500.00","shortTermGainAmount":"-1900.00","longTermGainAmount":"10000.00","isCostBasisMissing":false,"taxFormIds":["form-int"]},"taxYearEstimate":{"payAmount":"110000.00","incomeAmount":"121600.00","investmentIncomeAmount":"11600.00","totalTaxAmount":"12160.00"}}}}"#
        let (connection, _) = try connection(answering: body)
        let read = try await connection.taxYear(2026)
        let summary = try XCTUnwrap(read)
        XCTAssertEqual(summary.taxYearInvestment?.shortTermGainAmount, "-1900.00")
        // Shares are of income, pay and investment income together.
        XCTAssertEqual(try XCTUnwrap(summary.taxYearEstimate?.shareOfPay("12160.00")), 0.1, accuracy: 0.0001)
        let form = try JSONDecoder().decode(FinanceTaxForm.self, from: Data(#"{"id":"form-1","taxFormKind":"1099_div"}"#.utf8))
        XCTAssertEqual(form.kindName, "1099-DIV")
        XCTAssertFalse(form.isW2)
    }

    func testOnlyTheChangedSettingsAreSent() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"SetTaxYearSetting":{"taxYear":2026,"childCount":2,"otherIncomeAmount":"4500.0000"}}}"#)
        var change = TaxYearSettingChange()
        change.childCount = 2
        change.otherIncomeAmount = "4500"
        let saved = try await connection.setTaxYearSetting(2026, change: change)
        XCTAssertEqual(saved?.childCount, 2)
        let sent = try JSONSerialization.jsonObject(with: try XCTUnwrap(fake.sent.last?.httpBody)) as? [String: Any]
        let variables = try XCTUnwrap(sent?["variables"] as? [String: Any])
        XCTAssertEqual(Set(variables.keys), ["taxYear", "childCount", "otherIncomeAmount"])
        XCTAssertEqual(variables["otherIncomeAmount"] as? String, "4500")
    }

    func testZeroRowsAreLeftOutExceptWagesAndFederalTax() throws {
        let figures = try JSONDecoder().decode(TaxYearFigures.self, from: Data(#"{"wagesAmount":"0.00","federalIncomeTaxAmount":"0.00","medicareTaxAmount":"0.00"}"#.utf8))
        let rows = figures.rows()
        XCTAssertEqual(rows.map(\.figureKind), [.wages, .federalIncomeTax])
        XCTAssertNil(rows.first?.projectedAmount)
    }

    func testAW2CountsTheChecksThatDisagree() throws {
        let body = #"{"financeTaxForm":{"id":"form-1","taxYear":2025,"payerName":"Example Payroll","recipientName":"Robin Example","taxFormEntries":[{"id":"entry-2","entryNumber":2,"boxCode":"12","boxQualifier":"D","amount":"400.00"},{"id":"entry-1","entryNumber":1,"boxCode":"1","amount":"8000.00"}]},"taxFormFindings":[{"taxFormFindingCode":"social_security_rate","isAgreeing":true},{"taxFormFindingCode":"medicare_rate","isAgreeing":false}]}"#
        let view = try JSONDecoder().decode(FinanceTaxFormView.self, from: Data(body.utf8))
        XCTAssertEqual(view.disagreeingCount, 1)
        XCTAssertEqual(view.financeTaxForm?.entries.map(\.boxName), ["1", "12 D"])
    }
}
