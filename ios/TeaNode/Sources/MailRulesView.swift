import SwiftUI
import TeaNodeKit

/// MailRulesView is the mailbox's rules, as the dashboard's Rules tab: what
/// each does in a line, turned on and off, put in order, edited one at a
/// time, and tried on recent mail. Every change is saved as it is made.
struct MailRulesView: View {
    let mail: MailModel
    let mailboxId: String
    @State private var rules: [MailRule] = []
    @State private var editing: EditedRule?
    @State private var removing: RemovedRule?
    @State private var trials: [MailRuleTrial]?
    @State private var isTrying = false

    /// EditedRule is a rule open in the editor: its place in the list, or
    /// none for a new one.
    struct EditedRule: Identifiable {
        let id = UUID()
        let index: Int?
        let rule: MailRule
    }

    struct RemovedRule: Identifiable {
        let id = UUID()
        let index: Int
        let name: String
    }

    var body: some View {
        List {
            Section {
                if rules.isEmpty {
                    Text("No rules yet. Everything lands in the inbox.").foregroundStyle(.secondary)
                }
                ForEach(Array(rules.enumerated()), id: \.offset) { index, rule in
                    Button {
                        editing = EditedRule(index: index, rule: rule)
                    } label: {
                        MailRuleRow(rule: rule, words: words)
                    }
                    .swipeActions {
                        Button("Remove rule", systemImage: "trash", role: .destructive) {
                            removing = RemovedRule(index: index, name: words.ruleName(rule))
                        }
                        Button(rule.enabled ? "Turn off" : "Turn on", systemImage: rule.enabled ? "pause" : "play") {
                            var next = rules
                            next[index].enabled.toggle()
                            commit(next)
                        }
                        .tint(.gray)
                    }
                    .confirmationDialog(
                        "Remove rule", isPresented: Binding(get: { removing?.index == index }, set: { if !$0 { removing = nil } }),
                        titleVisibility: .visible, presenting: removing
                    ) { removed in
                        Button("Remove rule", role: .destructive) {
                            var next = rules
                            if next.indices.contains(removed.index) { next.remove(at: removed.index) }
                            commit(next)
                        }
                    } message: { removed in
                        Text("Remove the rule “\(removed.name)”? Mail it would have sorted lands in the inbox.")
                    }
                }
                .onMove { offsets, destination in
                    var next = rules
                    next.move(fromOffsets: offsets, toOffset: destination)
                    commit(next)
                }
            } footer: {
                Text("Rules run in order on every message that arrives. When a rule matches, its actions run; a rule marked “stop” ends the run.")
            }
            if !rules.isEmpty {
                Section {
                    Button {
                        Task { await tryRules() }
                    } label: {
                        HStack {
                            Text("Try on recent mail")
                            if isTrying {
                                Spacer()
                                ProgressView()
                            }
                        }
                    }
                    .disabled(isTrying)
                    if let trials {
                        ForEach(trials, id: \.item.id) { trial in
                            VStack(alignment: .leading, spacing: 2) {
                                Text(trial.item.mail?.subject.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "(no subject)"))
                                    .lineLimit(1)
                                Text(trial.item.mail?.from ?? trial.item.mail?.sender ?? "")
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                                    .lineLimit(1)
                                Text(matchedNames(trial))
                                    .font(.footnote)
                                    .foregroundStyle(trial.matched.isEmpty ? AnyShapeStyle(.secondary) : AnyShapeStyle(.tint))
                            }
                        }
                    }
                } header: {
                    if trials != nil { Text("What the rules would do") }
                } footer: {
                    if trials != nil {
                        Text("The newest messages in the Inbox, and which of the rules above would match each. Nothing is moved.")
                    }
                }
            }
        }
        .navigationTitle("Rules")
        .toolbar {
            if rules.count > 1 {
                ToolbarItem(placement: .topBarTrailing) { EditButton() }
            }
            ToolbarItem(placement: .primaryAction) {
                Button("Add a rule", systemImage: "plus") {
                    editing = EditedRule(index: nil, rule: MailRule())
                }
            }
        }
        .sheet(item: $editing) { edited in
            NavigationStack {
                MailRuleEditor(mail: mail, edited: edited, words: words) { rule in
                    var next = rules
                    if let index = edited.index, next.indices.contains(index) {
                        next[index] = rule
                    } else {
                        next.append(rule)
                    }
                    return await save(next)
                }
            }
        }
        .toast(done: Bindable(mail).doneText, problem: Bindable(mail).problemText)
        .onAppear { rules = mail.mailbox?.rules ?? [] }
        .onChange(of: mail.mailbox?.rules) { _, saved in
            rules = saved ?? []
            trials = nil
        }
    }

    private var words: MailRuleWords { MailRuleWords(folders: mail.folders) }

    private func commit(_ next: [MailRule]) {
        Task { _ = await save(next) }
    }

    /// Saves the list as it is now; the list shows it at once, and goes back
    /// to what the server has if it is refused.
    private func save(_ next: [MailRule]) async -> Bool {
        rules = next
        return await mail.act(saying: String(localized: "Saved")) {
            try await mail.connection.updateMailboxRules(mailboxId, rules: next)
        }
    }

    private func tryRules() async {
        isTrying = true
        defer { isTrying = false }
        do {
            trials = try await mail.connection.testMailboxRules(mailboxId, rules: rules)
        } catch {
            mail.problemText = String(localized: "That did not work: \(error.localizedDescription)")
        }
    }

    private func matchedNames(_ trial: MailRuleTrial) -> String {
        let names = trial.matched.compactMap { rules.indices.contains($0) ? words.ruleName(rules[$0]) : nil }
        return names.isEmpty ? String(localized: "No rule") : names.joined(separator: ", ")
    }
}

/// MailRuleRow is a rule in a line or two: its name, whether it is off or
/// stops, and what it does.
struct MailRuleRow: View {
    let rule: MailRule
    let words: MailRuleWords

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 6) {
                Text(words.ruleName(rule))
                    .foregroundStyle(rule.enabled ? Color(.label) : Color(.secondaryLabel))
                if !rule.enabled {
                    Text("Off").font(.caption).foregroundStyle(Color(.secondaryLabel))
                }
                if rule.stop {
                    Text("Stops here").font(.caption).foregroundStyle(Color(.secondaryLabel))
                }
            }
            Text(words.summary(rule))
                .font(.footnote)
                .foregroundStyle(Color(.secondaryLabel))
                .lineLimit(3)
        }
    }
}

/// MailRuleEditor is one rule, as the dashboard's rule dialog: its name,
/// whether it is on and stops the run, what it matches and what it does.
/// Nothing is saved until Save.
struct MailRuleEditor: View {
    let mail: MailModel
    let words: MailRuleWords
    let onSave: (MailRule) async -> Bool
    private let isNew: Bool
    /// The rule's name and switches; its conditions and actions are kept
    /// apart, each with an identity of its own while it is edited, so a
    /// field typed in keeps the keyboard and a removed row takes only
    /// itself.
    @State private var rule: MailRule
    @State private var conditions: [Edited<MailRule.Condition>]
    @State private var actions: [Edited<MailRule.Action>]
    @State private var isSaving = false
    @Environment(\.dismiss) private var dismiss

    init(mail: MailModel, edited: MailRulesView.EditedRule, words: MailRuleWords, onSave: @escaping (MailRule) async -> Bool) {
        self.mail = mail
        self.words = words
        self.onSave = onSave
        self.isNew = edited.index == nil
        self._rule = State(initialValue: edited.rule)
        self._conditions = State(initialValue: edited.rule.conditions.map { Edited(value: $0) })
        self._actions = State(initialValue: edited.rule.actions.map { Edited(value: $0) })
    }

    /// Edited is one condition or action in the editor.
    struct Edited<Value>: Identifiable {
        let id = UUID()
        var value: Value
    }

    var body: some View {
        Form {
            Section {
                TextField("Rule name", text: $rule.name)
                Toggle("On", isOn: $rule.enabled)
                Toggle("Stop after this rule", isOn: $rule.stop)
            }
            Section("When all of these match") {
                ForEach($conditions) { $condition in
                    conditionEditor($condition.value)
                }
                .onDelete { offsets in conditions.remove(atOffsets: offsets) }
                Button("Add a condition", systemImage: "plus") {
                    conditions.append(Edited(value: MailRule.Condition()))
                }
            }
            Section("Do this") {
                ForEach($actions) { $action in
                    actionEditor($action.value)
                }
                .onDelete { offsets in actions.remove(atOffsets: offsets) }
                Button("Add an action", systemImage: "plus") {
                    actions.append(Edited(value: MailRule.Action()))
                }
            }
        }
        .navigationTitle(isNew ? "Add a rule" : "Edit rule")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .cancellationAction) {
                Button("Cancel") { dismiss() }
            }
            ToolbarItem(placement: .confirmationAction) {
                Button("Save") {
                    isSaving = true
                    var saved = rule
                    saved.conditions = conditions.map(\.value)
                    saved.actions = actions.map(\.value)
                    Task {
                        let isSaved = await onSave(saved)
                        isSaving = false
                        if isSaved { dismiss() }
                    }
                }
                .disabled(isSaving || conditions.isEmpty || actions.isEmpty)
            }
        }
        .toast(done: .constant(nil), problem: Bindable(mail).problemText)
    }

    @ViewBuilder
    private func conditionEditor(_ condition: Binding<MailRule.Condition>) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Picker("Field", selection: Binding(get: { condition.wrappedValue.field }, set: { field in
                condition.wrappedValue.field = field
                // A field keeps its operator only when it can use it.
                let operators = MailRule.Condition.operators(for: field)
                if !operators.contains(condition.wrappedValue.comparisonOperator) {
                    condition.wrappedValue.comparisonOperator = operators[0]
                }
                if field == "priority" { condition.wrappedValue.value = "" }
            })) {
                ForEach(MailRule.fields, id: \.self) { field in
                    Text(words.field(field)).tag(field)
                }
            }
            if condition.wrappedValue.field == "header" {
                TextField("Header name", text: Binding(get: { condition.wrappedValue.header ?? "" }, set: { condition.wrappedValue.header = $0 }))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            }
            if !condition.wrappedValue.isBare {
                let operators = MailRule.Condition.operators(for: condition.wrappedValue.field)
                if operators.count > 1 {
                    Picker("How it compares", selection: condition.comparisonOperator) {
                        ForEach(operators, id: \.self) { comparison in
                            Text(words.comparison(comparison)).tag(comparison)
                        }
                    }
                }
                let value = Binding(get: { condition.wrappedValue.value ?? "" }, set: { condition.wrappedValue.value = $0 })
                switch condition.wrappedValue.field {
                case "priority":
                    Picker("What it looks for", selection: value) {
                        Text("Choose…").tag("")
                        ForEach(MailRule.priorities, id: \.self) { priority in
                            Text(words.priority(priority)).tag(priority)
                        }
                    }
                case "score":
                    TextField("What it looks for", text: value).keyboardType(.decimalPad)
                case "category":
                    TextField("personal, work, newsletter… or one of your own", text: value)
                        .textInputAutocapitalization(.never)
                default:
                    TextField("What it looks for", text: value)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                }
            }
        }
        .padding(.vertical, 2)
    }

    @ViewBuilder
    private func actionEditor(_ action: Binding<MailRule.Action>) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Picker("Action", selection: action.kind) {
                ForEach(MailRule.actionKinds, id: \.self) { kind in
                    Text(words.action(kind)).tag(kind)
                }
            }
            switch action.wrappedValue.kind {
            case "move":
                Picker("Folder", selection: Binding(get: { action.wrappedValue.folderId ?? "" }, set: { action.wrappedValue.folderId = $0 })) {
                    Text("Choose a folder…").tag("")
                    ForEach(MailFolders.rows(mail.folders), id: \.folder.id) { row in
                        Text(MailNames.folder(row.folder)).tag(row.folder.id)
                    }
                }
            case "forward":
                TextField("Address to forward to", text: Binding(get: { action.wrappedValue.address ?? "" }, set: { action.wrappedValue.address = $0 }))
                    .keyboardType(.emailAddress)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            default:
                EmptyView()
            }
        }
        .padding(.vertical, 2)
    }
}

/// MailRuleWords are the words a rule is shown in, the dashboard's.
struct MailRuleWords {
    let folders: [MailFolder]

    func ruleName(_ rule: MailRule) -> String {
        rule.name.trimmingCharacters(in: .whitespaces).isEmpty ? String(localized: "Unnamed rule") : rule.name
    }

    /// What a rule does in a line: the list is read far more often than it
    /// is edited, by someone finding out where their mail goes.
    func summary(_ rule: MailRule) -> String {
        let conditions = rule.conditions.map(describe).joined(separator: ", ")
        let actions = rule.actions.map(describe).joined(separator: ", ")
        return String(localized: "When \(conditions)") + " · " + String(localized: "then \(actions)")
    }

    func field(_ field: String) -> String {
        switch field {
        case "from": String(localized: "From")
        case "to": String(localized: "To")
        case "subject": String(localized: "Subject")
        case "header": String(localized: "Header")
        case "score": String(localized: "Spam score")
        case "sender-known": String(localized: "Sender is a contact")
        case "category": String(localized: "Category, as the agent sorted it")
        case "priority": String(localized: "Priority, as the agent sorted it")
        case "needs-reply": String(localized: "The agent says it needs a reply")
        default: String(localized: "Any message")
        }
    }

    /// A field as a rule's line names it: the editor's words say who
    /// decided a category or a priority, which the line has no room for.
    func shortField(_ field: String) -> String {
        switch field {
        case "category": String(localized: "Category")
        case "priority": String(localized: "Priority")
        case "needs-reply": String(localized: "Needs a reply")
        default: self.field(field)
        }
    }

    func comparison(_ comparison: String) -> String {
        switch comparison {
        case "equals": String(localized: "is")
        case "matches": String(localized: "matches pattern")
        case "above": String(localized: "is above")
        case "below": String(localized: "is below")
        default: String(localized: "contains")
        }
    }

    func action(_ kind: String) -> String {
        switch kind {
        case "markRead": String(localized: "Mark as read")
        case "flag": String(localized: "Star")
        case "forward": String(localized: "Forward to")
        case "delete": String(localized: "Delete")
        default: String(localized: "Move to folder")
        }
    }

    func priority(_ priority: String) -> String {
        switch priority {
        case "high": String(localized: "High")
        case "low": String(localized: "Low")
        default: String(localized: "Normal")
        }
    }

    private func describe(_ condition: MailRule.Condition) -> String {
        let name = shortField(condition.field)
        if condition.isBare { return name }
        let header = condition.field == "header" ? " " + (condition.header ?? "") : ""
        let value = condition.field == "priority" && !(condition.value ?? "").isEmpty ? priority(condition.value ?? "") : condition.value ?? ""
        return "\(name)\(header) \(comparison(condition.comparisonOperator)) \(value)".trimmingCharacters(in: .whitespaces)
    }

    private func describe(_ action: MailRule.Action) -> String {
        let kind = self.action(action.kind)
        switch action.kind {
        case "move":
            let folder = folders.first { $0.id == action.folderId }.map(MailNames.folder) ?? String(localized: "Choose a folder…")
            return "\(kind) \(folder)"
        case "forward":
            return "\(kind) \(action.address ?? "")".trimmingCharacters(in: .whitespaces)
        default:
            return kind
        }
    }
}
