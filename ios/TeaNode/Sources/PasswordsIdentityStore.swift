import AuthenticationServices
import Foundation
import TeaNodeKit

/// PasswordsIdentityStore tells the system what Passwords can fill, so that
/// QuickType suggests a login above the keyboard and a passkey in a sign-in
/// sheet without opening TeaNode first. What it gives the system is the site,
/// the user name and the passkey's identifiers, never a password or a key; the
/// AutoFill extension is asked for those when the person picks one.
enum PasswordsIdentityStore {
    /// Replaces what the system knows with the logins and passkeys given. Does
    /// nothing when the person has not turned TeaNode on in Settings, General,
    /// AutoFill & Passwords: the store refuses writes then.
    static func replace(with records: PasswordsIdentityRecords) async {
        let store = ASCredentialIdentityStore.shared
        guard await isEnabled(store) else { return }
        var identities: [any ASCredentialIdentity] = []
        for record in records.passwordIdentities {
            let service = ASCredentialServiceIdentifier(identifier: record.serviceDomain, type: .domain)
            identities.append(ASPasswordCredentialIdentity(serviceIdentifier: service, user: record.userName, recordIdentifier: record.recordId))
        }
        for record in records.passkeyIdentities {
            identities.append(
                ASPasskeyCredentialIdentity(
                    relyingPartyIdentifier: record.relyingPartyId, userName: record.userName, credentialID: record.credentialId,
                    userHandle: record.userHandle, recordIdentifier: record.recordId))
        }
        await replaceAll(identities, in: store)
    }

    /// Forgets everything, when Passwords is erased or the phone signs out.
    static func removeAll() async {
        let store = ASCredentialIdentityStore.shared
        guard await isEnabled(store) else { return }
        await removeAll(in: store)
    }

    /// Replaces what the system knows. Written with the completion handler,
    /// which is the form the documentation gives; a failure is not told to
    /// the person, since the system asks the extension for anything missing.
    private static func replaceAll(_ identities: [any ASCredentialIdentity], in store: ASCredentialIdentityStore) async {
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            store.replaceCredentialIdentities(identities) { _, _ in
                continuation.resume()
            }
        }
    }

    private static func removeAll(in store: ASCredentialIdentityStore) async {
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            store.removeAllCredentialIdentities { _, _ in
                continuation.resume()
            }
        }
    }

    private static func isEnabled(_ store: ASCredentialIdentityStore) async -> Bool {
        await withCheckedContinuation { continuation in
            store.getState { state in
                continuation.resume(returning: state.isEnabled)
            }
        }
    }
}

/// PasswordsAccountStorage removes what Passwords keeps on this iPhone for a
/// sign-in that was removed: the encrypted copy of its items, the account key
/// behind Face ID, and the choice to use it.
enum PasswordsAccountStorage {
    static func accountWasRemoved(signInAccountKey: String, isInUse: Bool) async {
        let group = Bundle.main.object(forInfoDictionaryKey: "TeaNodeAppGroup") as? String
        if let group, let container = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group) {
            PasswordsLocation.removeCopy(groupContainer: container, signInAccountKey: signInAccountKey)
        }
        if let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first {
            PasswordsLocation.removeCopy(groupContainer: support, signInAccountKey: signInAccountKey)
        }
        UserDefaults.standard.removeObject(forKey: "passwordsFaceID.\(signInAccountKey)")
        try? await PasswordsKeychainKeyStore().delete(signInAccountKey: signInAccountKey)
        if isInUse { await PasswordsIdentityStore.removeAll() }
    }
}
