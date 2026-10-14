import Foundation
import TeaNodeKit

/// StatementImportWords says what a statement import did, in a sentence,
/// as the dashboard says it. The share extension compiles this file too,
/// so a statement shared in reads the same as the last import on Accounts.
enum StatementImportWords {
    static func summary(_ last: StatementImport.LastImport?, isDated: Bool = true) -> String {
        guard let last else { return String(localized: "Nothing imported yet.") }
        guard last.isImported else {
            let reason = last.importErrorMessage.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "the file held no account")
            return String(localized: "Nothing was imported: \(reason).")
        }
        let accountNames = (last.financeAccountNames ?? []).joined(separator: ", ")
        var said = String(localized: "Imported into \(accountNames): \(last.addedTransactionCount ?? 0) added, \(last.updatedTransactionCount ?? 0) updated, \(last.unchangedTransactionCount ?? 0) already here.")
        if let reason = last.importErrorMessage, !reason.isEmpty {
            said += " " + String(localized: "Not imported: \(reason).")
        }
        if isDated, let day = ServerTime.date(last.importedAt) {
            said = "\(day.formatted(date: .abbreviated, time: .shortened)) · " + said
        }
        return said
    }
}
