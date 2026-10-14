import SwiftUI
import TeaNodeKit

/// DevicePasswordRow is a device's password in a list: its name, when it was
/// made, when it was last used, and a muted hint when it looks abandoned.
/// Sync and mail passwords share it so the person reads both lists the same
/// way and can tell several passwords with one name apart.
struct DevicePasswordRow<Password: DevicePassword>: View {
    let name: String
    var scopes: String = ""
    let password: Password

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(name)
            if !scopes.isEmpty {
                Text(scopes).font(.caption).foregroundStyle(.secondary)
            }
            Text(usageText).font(.caption).foregroundStyle(.secondary)
            if password.isMaybeSafeToDelete() {
                Text("May be safe to delete").font(.caption).foregroundStyle(.tertiary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var usageText: String {
        var parts: [String] = []
        if let createdAt = ServerTime.date(password.passwordCreatedAt) {
            parts.append(String(localized: "Created \(createdAt.formatted(date: .abbreviated, time: .shortened))"))
        }
        if let lastUsedAt = ServerTime.date(password.passwordLastUsedAt) {
            parts.append(String(localized: "Last used \(lastUsedAt.formatted(.relative(presentation: .named)))"))
        } else {
            parts.append(String(localized: "Never used"))
        }
        return parts.joined(separator: " · ")
    }
}
