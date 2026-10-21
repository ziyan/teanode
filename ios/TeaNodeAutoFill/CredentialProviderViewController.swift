import AuthenticationServices
import SwiftUI
import TeaNodeKit
import UIKit

/// CredentialProviderViewController is TeaNode in Settings, General, AutoFill
/// & Passwords: Safari, Chrome and other apps ask it for the person's logins,
/// passkeys and one-time codes. The system starts it for one request at a
/// time and shows its view as a sheet over the app that asked.
///
/// This class only translates between the system's types and the plain values
/// the model works on (AutoFillRequest.swift). The model reads the phone's
/// copy of Passwords and the account key behind Face ID, and does no
/// networking except to save a passkey the person has just made.
///
/// This target builds in the Swift 5 language mode, like the share extension:
/// the AuthenticationServices overrides are not yet marked for Swift 6's
/// checking.
final class CredentialProviderViewController: ASCredentialProviderViewController {
    /// The system can call a prepare method before the view loads, so the
    /// model is made on first use.
    private lazy var model: AutoFillModel = makeModel()

    override func viewDidLoad() {
        super.viewDidLoad()
        let host = UIHostingController(rootView: AutoFillRootView(model: model))
        addChild(host)
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)
    }

    // MARK: Lists

    /// The person is filling a password.
    override func prepareCredentialList(for serviceIdentifiers: [ASCredentialServiceIdentifier]) {
        model.start(
            .list(AutoFillListing(requestedSites: Self.requestedSites(serviceIdentifiers), passkeyAssertion: nil, isOneTimeCode: false)))
    }

    /// The person is signing in on a website that accepts a passkey; the
    /// passwords of the site are in the list too.
    override func prepareCredentialList(
        for serviceIdentifiers: [ASCredentialServiceIdentifier], requestParameters: ASPasskeyCredentialRequestParameters
    ) {
        let assertion = AutoFillPasskeyAssertion(
            relyingPartyId: requestParameters.relyingPartyIdentifier, clientDataHash: requestParameters.clientDataHash,
            allowedCredentialIds: requestParameters.allowedCredentials)
        let requestedSites = Self.requestedSites(serviceIdentifiers) + [requestParameters.relyingPartyIdentifier]
        model.start(.list(AutoFillListing(requestedSites: requestedSites, passkeyAssertion: assertion, isOneTimeCode: false)))
    }

    /// The person is filling a one-time code (iOS 18 and later).
    override func prepareOneTimeCodeCredentialList(for serviceIdentifiers: [ASCredentialServiceIdentifier]) {
        model.start(
            .list(AutoFillListing(requestedSites: Self.requestedSites(serviceIdentifiers), passkeyAssertion: nil, isOneTimeCode: true)))
    }

    // MARK: A credential the system picked

    /// The account key is always behind Face ID, so nothing can be provided
    /// without showing the interface; the system then calls
    /// `prepareInterfaceToProvideCredential`.
    override func provideCredentialWithoutUserInteraction(for credentialRequest: any ASCredentialRequest) {
        extensionContext.cancelRequest(withError: Self.error(.userInteractionRequired))
    }

    override func prepareInterfaceToProvideCredential(for credentialRequest: any ASCredentialRequest) {
        if let passwordRequest = credentialRequest as? ASPasswordCredentialRequest,
            let identity = passwordRequest.credentialIdentity as? ASPasswordCredentialIdentity
        {
            model.start(
                .providePassword(
                    recordId: identity.recordIdentifier, userName: identity.user,
                    requestedSites: Self.requestedSites([identity.serviceIdentifier])))
        } else if let passkeyRequest = credentialRequest as? ASPasskeyCredentialRequest,
            let identity = passkeyRequest.credentialIdentity as? ASPasskeyCredentialIdentity
        {
            let assertion = AutoFillPasskeyAssertion(
                relyingPartyId: identity.relyingPartyIdentifier, clientDataHash: passkeyRequest.clientDataHash,
                allowedCredentialIds: [identity.credentialID])
            model.start(.providePasskey(assertion: assertion, recordId: identity.recordIdentifier, credentialId: identity.credentialID))
        } else {
            extensionContext.cancelRequest(withError: Self.error(.failed))
        }
    }

    // MARK: Making a passkey

    // `performWithoutUserInteractionIfPossible(passkeyRegistration:)` is not
    // overridden: a passkey is made only after the person has seen the site
    // and user name and has used Face ID, so the system falls back to the
    // interface below.

    override func prepareInterface(forPasskeyRegistration registrationRequest: any ASCredentialRequest) {
        guard let passkeyRequest = registrationRequest as? ASPasskeyCredentialRequest,
            let identity = passkeyRequest.credentialIdentity as? ASPasskeyCredentialIdentity
        else {
            extensionContext.cancelRequest(withError: Self.error(.failed))
            return
        }
        model.start(
            .registerPasskey(
                AutoFillPasskeyRegistration(
                    relyingPartyId: identity.relyingPartyIdentifier, userName: identity.userName, userHandle: identity.userHandle,
                    clientDataHash: passkeyRequest.clientDataHash)))
    }

    // MARK: Turning TeaNode on

    override func prepareInterfaceForExtensionConfiguration() {
        model.start(.configuration)
    }

    // MARK: Translating

    private func makeModel() -> AutoFillModel {
        let model = AutoFillModel()
        model.completion = AutoFillCompletion(
            fillPassword: { [weak self] userName, password in
                self?.extensionContext.completeRequest(
                    withSelectedCredential: ASPasswordCredential(user: userName, password: password), completionHandler: nil)
            },
            fillPasskeyAssertion: { [weak self] signed in
                self?.extensionContext.completeAssertionRequest(
                    using: ASPasskeyAssertionCredential(
                        userHandle: signed.userHandle, relyingParty: signed.relyingPartyId, signature: signed.signature,
                        clientDataHash: signed.clientDataHash, authenticatorData: signed.authenticatorData,
                        credentialID: signed.credentialId),
                    completionHandler: nil)
            },
            fillPasskeyRegistration: { [weak self] created in
                self?.extensionContext.completeRegistrationRequest(
                    using: ASPasskeyRegistrationCredential(
                        relyingParty: created.relyingPartyId, clientDataHash: created.clientDataHash,
                        credentialID: created.credentialId, attestationObject: created.attestationObject),
                    completionHandler: nil)
            },
            fillOneTimeCode: { [weak self] code in
                self?.extensionContext.completeOneTimeCodeRequest(using: ASOneTimeCodeCredential(code: code), completionHandler: nil)
            },
            cancel: { [weak self] cancellation in
                switch cancellation {
                case .userCanceled: self?.extensionContext.cancelRequest(withError: Self.error(.userCanceled))
                case .failed: self?.extensionContext.cancelRequest(withError: Self.error(.failed))
                case .credentialIdentityNotFound:
                    self?.extensionContext.cancelRequest(withError: Self.error(.credentialIdentityNotFound))
                }
            },
            finishConfiguration: { [weak self] in
                self?.extensionContext.completeExtensionConfigurationRequest()
            })
        return model
    }

    /// The error the system expects when a request ends without a credential.
    private static func error(_ code: ASExtensionError.Code) -> NSError {
        NSError(domain: ASExtensionErrorDomain, code: code.rawValue)
    }

    /// The sites the system names for the page or app asking: a domain or an
    /// address. An app is named by its bundle id, which matches no login.
    private static func requestedSites(_ serviceIdentifiers: [ASCredentialServiceIdentifier]) -> [String] {
        serviceIdentifiers.filter { $0.type == .domain || $0.type == .URL }.map(\.identifier)
    }
}
