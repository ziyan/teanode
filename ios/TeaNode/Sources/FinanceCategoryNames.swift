import TeaNodeKit

/// FinanceCategoryNames is how a spending category's stored name is shown,
/// as the dashboard shows it (web/src/pages/finance/spendingCategoryName.ts).
/// The categories everybody starts with are stored under the server's names,
/// in English and lowercase, because rules, budgets and the provider's
/// categories find them by those names; a person reads them in their own
/// language. A name the person wrote is shown as written, and a form that
/// edits a name edits the stored one.
enum FinanceCategoryNames {
    static func shown(_ category: SpendingCategory) -> String {
        shown(category.spendingCategoryName, isTransfer: category.isTransfer == true, isOther: category.isOther == true)
    }

    /// A stored name as shown. Transfer and other are found by their flags,
    /// never their names: somebody who already had an "other" of their own
    /// keeps it as they wrote it.
    static func shown(_ name: String, isTransfer: Bool = false, isOther: Bool = false) -> String {
        switch name {
        case "transfer": isTransfer ? String(localized: "Transfer") : name
        case "other": isOther ? String(localized: "Other") : name
        case "income": String(localized: "Income")
        case "housing": String(localized: "Housing")
        case "utilities": String(localized: "Utilities")
        case "groceries": String(localized: "Groceries")
        case "dining": String(localized: "Dining out")
        case "transport": String(localized: "Transport")
        case "travel": String(localized: "Travel")
        case "shopping": String(localized: "Shopping")
        case "health": String(localized: "Health")
        case "entertainment": String(localized: "Entertainment")
        case "subscriptions": String(localized: "Subscriptions")
        case "fees": String(localized: "Fees")
        case "gifts and donations": String(localized: "Gifts and donations")
        case "taxes": String(localized: "Taxes")
        case "loans": String(localized: "Loans")
        case "education": String(localized: "Education")
        case "children": String(localized: "Kids")
        case "business services": String(localized: "Business services")
        default: name
        }
    }

    /// Who filed a transaction under its category, as a line under it.
    static func filedBy(_ source: String) -> String {
        switch source {
        case "spending_rule": String(localized: "Filed by a spending rule")
        case "provider_category_mapping": String(localized: "Filed from the provider's category")
        case "categorize_model": String(localized: "Filed by the model")
        case "transfer_detection": String(localized: "Filed as a transfer: the same amount went the other way in another of your accounts")
        default: String(localized: "Filed automatically")
        }
    }
}
