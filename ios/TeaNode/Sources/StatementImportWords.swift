import Foundation
import TeaNodeKit

/// StatementImportWords says what a statement import did, in one whole
/// sentence per case, as the dashboard says it. The share extension
/// compiles this file too, so a statement shared in reads the same as the
/// last import on Accounts. The reason a file was not imported is the
/// server's own words.
enum StatementImportWords {
    static func summary(_ last: StatementImport.LastImport?) -> String {
        guard let last else { return String(localized: "Nothing imported yet.") }
        let reason = last.importErrorMessage.flatMap { $0.isEmpty ? nil : $0 }
        guard last.isImported else {
            guard let reason else { return String(localized: "Nothing was imported: the file held no account.") }
            return String(localized: "Nothing was imported: \(reason).")
        }
        let accountNames = (last.financeAccountNames ?? []).joined(separator: ", ")
        let added = last.addedTransactionCount ?? 0, updated = last.updatedTransactionCount ?? 0, unchanged = last.unchangedTransactionCount ?? 0
        guard let reason else {
            return String(localized: "Imported into \(accountNames): \(added) added, \(updated) updated, \(unchanged) already here.")
        }
        return String(localized: "Imported into \(accountNames): \(added) added, \(updated) updated, \(unchanged) already here. Not imported: \(reason).")
    }

    /// When the import was, for a line under its summary.
    static func dayText(_ last: StatementImport.LastImport?) -> String? {
        ServerTime.date(last?.importedAt).map { $0.formatted(date: .abbreviated, time: .shortened) }
    }
}
