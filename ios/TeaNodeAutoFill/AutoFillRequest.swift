import Foundation

// What the system asks of the extension, and what the extension hands back,
// as plain values. Only CredentialProviderViewController.swift imports
// AuthenticationServices; everything else works on these.

/// AutoFillPasskeyAssertion is a website's request to sign in with a passkey.
struct AutoFillPasskeyAssertion: Equatable {
    let relyingPartyId: String
    let clientDataHash: Data
    /// The credential ids the website accepts; empty when it accepts any.
    let allowedCredentialIds: [Data]
}

/// AutoFillPasskeyRegistration is a website's request to make a passkey.
struct AutoFillPasskeyRegistration: Equatable {
    let relyingPartyId: String
    let userName: String
    let userHandle: Data
    let clientDataHash: Data
}

/// AutoFillRequestedSite is a site the system names for the page or app asking.
/// A service identifier of the URL kind carries the page's full address, so its
/// port and scheme count in the match; one of the domain kind carries a host and
/// nothing else, and so does a passkey's relying party id.
struct AutoFillRequestedSite: Equatable {
    let address: String
    let isPageAddress: Bool
}

/// AutoFillListing is what a list of logins is for: the sites the page or app
/// is on, a passkey sign-in if the website asked for one, or one-time codes.
struct AutoFillListing: Equatable {
    var requestedSites: [AutoFillRequestedSite]
    var passkeyAssertion: AutoFillPasskeyAssertion?
    var isOneTimeCode: Bool
}

/// AutoFillRequest is what the system started the extension for.
enum AutoFillRequest {
    /// The person is choosing from a list: a password, a passkey or a one-time code.
    case list(AutoFillListing)
    /// The system picked a password from its suggestions; the extension only checks Face ID.
    /// `itemId` is the item id the app gave the system when it made the suggestion
    /// (the identity's record identifier); with it only that item is opened.
    case providePassword(itemId: String?, userName: String, requestedSites: [AutoFillRequestedSite])
    /// The system picked a passkey from its suggestions.
    case providePasskey(assertion: AutoFillPasskeyAssertion, itemId: String?, credentialId: Data)
    case registerPasskey(AutoFillPasskeyRegistration)
    /// The person turned TeaNode on in Settings.
    case configuration
}

/// AutoFillScreen is what the extension shows.
enum AutoFillScreen: Equatable {
    case starting
    /// Passwords is kept on this phone but Face ID has not been given yet.
    case locked
    /// The app has not unlocked Passwords on this phone (no copy, no kept key, or another person's).
    case needsApp
    case list
    case registration(AutoFillPasskeyRegistration)
    case configuration
}

/// AutoFillProblem is something to tell the person, with what to do next.
struct AutoFillProblem: Equatable {
    let title: String
    let message: String
}

/// AutoFillSignedAssertion is a passkey sign-in, ready for the system.
struct AutoFillSignedAssertion {
    let userHandle: Data
    let relyingPartyId: String
    let signature: Data
    let clientDataHash: Data
    let authenticatorData: Data
    let credentialId: Data
}

/// AutoFillCreatedPasskey is a new passkey that is saved, ready for the system.
struct AutoFillCreatedPasskey {
    let relyingPartyId: String
    let clientDataHash: Data
    let credentialId: Data
    let attestationObject: Data
}

/// AutoFillCancellation is why a request ends without a credential.
enum AutoFillCancellation {
    case userCanceled
    case failed
    case credentialIdentityNotFound
}

/// AutoFillCompletion is how the model ends a request. The view controller
/// fills it in with calls on the extension context.
struct AutoFillCompletion {
    var fillPassword: (_ userName: String, _ password: String) -> Void
    var fillPasskeyAssertion: (AutoFillSignedAssertion) -> Void
    var fillPasskeyRegistration: (AutoFillCreatedPasskey) -> Void
    var fillOneTimeCode: (_ code: String) -> Void
    var cancel: (AutoFillCancellation) -> Void
    var finishConfiguration: () -> Void

    /// Does nothing, until the view controller has set the real one.
    static let ignoring = AutoFillCompletion(
        fillPassword: { _, _ in }, fillPasskeyAssertion: { _ in }, fillPasskeyRegistration: { _ in }, fillOneTimeCode: { _ in },
        cancel: { _ in }, finishConfiguration: {})
}
