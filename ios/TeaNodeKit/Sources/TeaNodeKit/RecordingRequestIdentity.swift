import Foundation

/// RecordingRequestIdentity keeps a paid reprocessing request stable through a lost response or relaunch.
@MainActor
public enum RecordingRequestIdentity {
    public static func acquire(accountKey: String, mediaId: String, in defaults: UserDefaults = .standard) -> String {
        let storageKey = key(accountKey: accountKey, mediaId: mediaId)
        if let existing = defaults.string(forKey: storageKey) { return existing }
        let requestId = UUID().uuidString
        defaults.set(requestId, forKey: storageKey)
        return requestId
    }

    public static func finish(accountKey: String, mediaId: String, requestId: String, in defaults: UserDefaults = .standard) {
        let storageKey = key(accountKey: accountKey, mediaId: mediaId)
        if defaults.string(forKey: storageKey) == requestId { defaults.removeObject(forKey: storageKey) }
    }

    private static func key(accountKey: String, mediaId: String) -> String {
        "recording.request." + Data(accountKey.utf8).base64EncodedString() + "." + Data(mediaId.utf8).base64EncodedString()
    }
}
