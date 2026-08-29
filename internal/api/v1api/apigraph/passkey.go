package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/ceremony"
)

// Signing in with a passkey.
//
// WebAuthn is two round trips in each direction. The server sends a challenge,
// the browser has an authenticator sign it, and the server verifies the
// signature against the public key it stored at registration. The private half
// never leaves the authenticator, which is the whole point: there is no shared
// secret to leak, phish or reuse, and a copy of this server's database is not
// a set of working credentials.
//
// The challenge is parked between the halves and cleared as it is read, so one
// challenge answers exactly one attempt. Registration is done by somebody
// already signed in; signing in is not, so that ceremony belongs to nobody
// until the assertion verifies and says who it was.

type PasskeyQuery interface {
	// List the passkeys registered to the calling account
	ListPasskeys(ctx context.Context) ([]*models.Passkey, error)

	// Report whether this server offers passkeys at all, and how many an
	// account may register, so an interface does not offer a button that
	// cannot work
	GetPasskeyPolicy(ctx context.Context) (*PasskeyPolicy, error)
}

type PasskeyMutation interface {
	// Begin registering a passkey; returns the options a browser passes to
	// navigator.credentials.create()
	BeginPasskeyRegistration(ctx context.Context) (*PasskeyCeremony, error)

	// Finish registering a passkey, verifying what the authenticator returned
	FinishPasskeyRegistration(ctx context.Context, arguments FinishPasskeyRegistrationArguments) (*models.Passkey, error)

	// Begin signing in with a passkey; returns the options for
	// navigator.credentials.get(), for somebody not yet signed in
	BeginPasskeyAssertion(ctx context.Context) (*PasskeyCeremony, error)

	// Finish signing in with a passkey, which starts a session on success
	FinishPasskeyAssertion(ctx context.Context, arguments FinishPasskeyAssertionArguments) (*SessionState, error)

	// Change what an authenticator is called
	RenamePasskey(ctx context.Context, arguments RenamePasskeyArguments) (*models.Passkey, error)

	// Remove a passkey, so that authenticator can no longer sign in
	DeletePasskey(ctx context.Context, arguments DeletePasskeyArguments) error
}

// PasskeyPolicy is what this deployment allows, so an interface can say why
// rather than offering something that fails.
type PasskeyPolicy struct {
	// Whether passkeys are configured at all
	Enabled bool `json:"enabled"`

	// How many one account may register
	MaximumPerUser int `json:"maximumPerUser"`
}

// PasskeyCeremony carries the browser's half of the exchange.
type PasskeyCeremony struct {
	// Names the parked challenge, so finishing can find it. Single-use:
	// answering it consumes it.
	CeremonyID string `json:"ceremonyId"`

	// The WebAuthn JSON, passed straight to the browser. Opaque here on
	// purpose — reshaping it would mean tracking the specification.
	Options string `json:"options"`
}

// defaultMaximumPasskeys is a phone, a laptop, a security key, and room to
// register a replacement before removing the one being replaced.
const defaultMaximumPasskeys = 5

func (self *graph) GetPasskeyPolicy(ctx context.Context) (*PasskeyPolicy, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}
	settings := self.config.Current().Passkey
	return &PasskeyPolicy{Enabled: settings.Enabled, MaximumPerUser: self.maximumPasskeys()}, nil
}

func (self *graph) maximumPasskeys() int {
	if maximum := self.config.Current().Passkey.MaximumPerUser; maximum > 0 {
		return maximum
	}
	return defaultMaximumPasskeys
}

func (self *graph) ListPasskeys(ctx context.Context) ([]*models.Passkey, error) {
	user, err := self.requireAccount(ctx)
	if err != nil {
		return nil, err
	}
	return self.database.ListPasskeysForUser(user.ID)
}

func (self *graph) BeginPasskeyRegistration(ctx context.Context) (*PasskeyCeremony, error) {
	user, err := self.requireAccount(ctx)
	if err != nil {
		return nil, err
	}
	engine, err := self.webAuthn()
	if err != nil {
		return nil, err
	}
	existing, err := self.database.ListPasskeysForUser(user.ID)
	if err != nil {
		return nil, err
	}
	// Refused before the browser is asked, so nobody is prompted to touch an
	// authenticator for a credential that will not be kept.
	if len(existing) >= self.maximumPasskeys() {
		return nil, api.ErrInvalidArguments
	}

	// Excluding what is already registered is what makes a browser say "you
	// already have one of these" rather than silently creating a second.
	//
	// A discoverable credential — one the authenticator stores and can offer
	// on its own — is required rather than preferred, because signing in
	// never names an account first. An authenticator that only keeps a
	// non-discoverable key would register happily and then never be found
	// again, which is the worst of the failure modes available here: it looks
	// like it worked.
	creation, sessionData, err := engine.BeginRegistration(
		&webAuthnUser{user: user, passkeys: existing},
		webauthn.WithExclusions(credentialDescriptors(existing)),
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
	)
	if err != nil {
		return nil, err
	}
	return self.park(ctx, user.Username, sessionData, creation)
}

type FinishPasskeyRegistrationArguments struct {
	// CeremonyID is what BeginPasskeyRegistration returned
	CeremonyID string `json:"ceremonyId"`

	// Response is what navigator.credentials.create() produced, serialised
	Response string `json:"response"`

	// Name is what to call this authenticator, for example "phone"
	Name *string `json:"name"`
}

func (self *graph) FinishPasskeyRegistration(ctx context.Context, arguments FinishPasskeyRegistrationArguments) (*models.Passkey, error) {
	user, err := self.requireAccount(ctx)
	if err != nil {
		return nil, err
	}
	engine, err := self.webAuthn()
	if err != nil {
		return nil, err
	}
	parked, err := self.take(ctx, arguments.CeremonyID)
	if err != nil {
		return nil, err
	}
	// The ceremony must be the caller's own, or one person's challenge could
	// be answered into another person's account.
	if parked.Username != user.Username {
		return nil, api.ErrInvalidArguments
	}

	response, err := protocol.ParseCredentialCreationResponseBody(strings.NewReader(arguments.Response))
	if err != nil {
		return nil, api.ErrInvalidArguments
	}

	existing, err := self.database.ListPasskeysForUser(user.ID)
	if err != nil {
		return nil, err
	}
	// Checked again here rather than only where the ceremony began. Begin and
	// finish are two requests, so the first is an offer and this is the rule:
	// without it, two ceremonies started together both finish, and a limit of
	// five holds six.
	if len(existing) >= self.maximumPasskeys() {
		return nil, api.ErrInvalidArguments
	}

	credential, err := engine.CreateCredential(&webAuthnUser{user: user, passkeys: existing}, *parked.SessionData, response)
	if err != nil {
		return nil, api.ErrInvalidArguments
	}

	name := "Passkey"
	if arguments.Name != nil && strings.TrimSpace(*arguments.Name) != "" {
		name = strings.TrimSpace(*arguments.Name)
	}

	stored, err := self.database.CreatePasskey(&models.Passkey{
		ID:              config.NewID(),
		UserID:          user.ID,
		Name:            name,
		CredentialID:    credential.ID,
		PublicKey:       credential.PublicKey,
		AttestationType: credential.AttestationType,
		Transports:      transportStrings(credential.Transport),
		AAGUID:          credential.Authenticator.AAGUID,
		SignCount:       int64(credential.Authenticator.SignCount),
		BackupEligible:  credential.Flags.BackupEligible,
		BackupState:     credential.Flags.BackupState,
	})
	if err != nil {
		return nil, err
	}
	log.Noticef("%s registered the passkey %q", user.Username, name)
	return stored, nil
}

// BeginPasskeyAssertion starts a sign-in.
//
// Discoverable credentials, so the browser offers whichever passkeys it holds
// for this site and the server is never told a username first. That also means
// this request reveals nothing about who has an account here.
func (self *graph) BeginPasskeyAssertion(ctx context.Context) (*PasskeyCeremony, error) {
	engine, err := self.webAuthn()
	if err != nil {
		return nil, err
	}
	assertion, sessionData, err := engine.BeginDiscoverableLogin()
	if err != nil {
		return nil, err
	}
	// No account: who is signing in is exactly what this ceremony establishes.
	return self.park(ctx, "", sessionData, assertion)
}

type FinishPasskeyAssertionArguments struct {
	// CeremonyID is what BeginPasskeyAssertion returned
	CeremonyID string `json:"ceremonyId"`

	// Response is what navigator.credentials.get() produced, serialised
	Response string `json:"response"`
}

func (self *graph) FinishPasskeyAssertion(ctx context.Context, arguments FinishPasskeyAssertionArguments) (*SessionState, error) {
	engine, err := self.webAuthn()
	if err != nil {
		return nil, err
	}
	parked, err := self.take(ctx, arguments.CeremonyID)
	if err != nil {
		return nil, err
	}
	response, err := protocol.ParseCredentialRequestResponseBody(strings.NewReader(arguments.Response))
	if err != nil {
		return nil, api.ErrNotLoggedIn
	}

	var matched *models.Passkey
	var owner *config.User

	// The library hands back the credential's identifier and asks who that
	// is; everything else — the challenge, the origin, the signature — it
	// verifies itself.
	credential, err := engine.ValidateDiscoverableLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		passkey, err := self.database.GetPasskeyByCredentialID(rawID)
		if err != nil {
			return nil, err
		}
		if passkey == nil {
			return nil, api.ErrNotLoggedIn
		}
		user := self.findAccountByID(passkey.UserID)
		if user == nil {
			return nil, api.ErrNotLoggedIn
		}
		passkeys, err := self.database.ListPasskeysForUser(user.ID)
		if err != nil {
			return nil, err
		}
		matched = passkey
		owner = user
		return &webAuthnUser{user: user, passkeys: passkeys}, nil
	}, *parked.SessionData, response)
	if err != nil || matched == nil || owner == nil {
		return nil, api.ErrNotLoggedIn
	}

	// A counter that did not advance means two authenticators are answering
	// for one credential, which means it has been cloned. The library reports
	// it; refusing is the only safe response.
	if credential.Authenticator.CloneWarning {
		log.Warningf("refused a passkey assertion for %q: the authenticator's counter went backwards", owner.Username)
		return nil, api.ErrNotLoggedIn
	}

	request := api.ContextRequest(ctx)
	ip, userAgent := "", ""
	if request != nil {
		ip, userAgent = request.RemoteAddr, request.UserAgent()
	}
	if err := self.database.RecordPasskeyUse(matched.ID, int64(credential.Authenticator.SignCount),
		credential.Flags.BackupState, time.Now(), ip, userAgent); err != nil {
		return nil, err
	}

	// The same session a password would have started, set on the same cookie:
	// how somebody proved who they are does not change what they get.
	if err := self.authenticator.StartSession(api.ContextResponse(ctx), request, owner.Username); err != nil {
		return nil, err
	}
	log.Noticef("%s signed in with the passkey %q", owner.Username, matched.Name)
	return self.signedInAs(owner.Username), nil
}

type RenamePasskeyArguments struct {
	// ID of the Passkey to rename
	PasskeyID string `json:"passkeyId"`

	// What to call this authenticator
	Name string `json:"name"`
}

func (self *graph) RenamePasskey(ctx context.Context, arguments RenamePasskeyArguments) (*models.Passkey, error) {
	passkey, err := self.requireOwnPasskey(ctx, arguments.PasskeyID)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(arguments.Name)
	if name == "" {
		return nil, api.ErrInvalidArguments
	}
	if err := self.database.RenamePasskey(passkey.ID, name); err != nil {
		return nil, err
	}
	return self.database.GetPasskey(passkey.ID)
}

type DeletePasskeyArguments struct {
	// ID of the Passkey to remove
	PasskeyID string `json:"passkeyId"`
}

func (self *graph) DeletePasskey(ctx context.Context, arguments DeletePasskeyArguments) error {
	passkey, err := self.requireOwnPasskey(ctx, arguments.PasskeyID)
	if err != nil {
		return err
	}
	// Removing the last one is allowed: a password always works here, so a
	// passkey is never the only way in and there is nothing to guard against.
	log.Noticef("%s removed the passkey %q", api.ContextAuthenticatedUsername(ctx), passkey.Name)
	return self.database.DeletePasskey(passkey.ID)
}

// requireOwnPasskey backs renaming and deleting, both of which change how
// somebody signs in.
func (self *graph) requireOwnPasskey(ctx context.Context, passkeyId string) (*models.Passkey, error) {
	user, err := self.requireAccount(ctx)
	if err != nil {
		return nil, err
	}
	passkey, err := self.database.GetPasskey(passkeyId)
	if err != nil {
		return nil, err
	}
	if passkey == nil || passkey.UserID != user.ID {
		return nil, api.ErrNotFound
	}
	return passkey, nil
}

// --- the ceremony ----------------------------------------------------------

func (self *graph) park(ctx context.Context, username string, sessionData *webauthn.SessionData, options any) (*PasskeyCeremony, error) {
	encodedSession, err := json.Marshal(sessionData)
	if err != nil {
		return nil, err
	}
	ceremonyId, err := self.ceremonies.Park(ctx, &ceremony.Ceremony{
		Username:    username,
		SessionData: string(encodedSession),
	})
	if err != nil {
		return nil, err
	}
	encodedOptions, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	return &PasskeyCeremony{CeremonyID: ceremonyId, Options: string(encodedOptions)}, nil
}

// parked is a challenge reclaimed, with the library's state decoded.
type parked struct {
	Username    string
	SessionData *webauthn.SessionData
}

func (self *graph) take(ctx context.Context, ceremonyId string) (*parked, error) {
	found, err := self.ceremonies.Take(ctx, ceremonyId)
	if err != nil {
		if errors.Is(err, ceremony.ErrNoCeremonyInProgress) {
			return nil, api.ErrInvalidArguments
		}
		return nil, err
	}
	var sessionData webauthn.SessionData
	if err := json.Unmarshal([]byte(found.SessionData), &sessionData); err != nil {
		return nil, err
	}
	return &parked{Username: found.Username, SessionData: &sessionData}, nil
}

// webAuthn builds the engine from the configured relying party.
//
// Rebuilt per call rather than held: the configuration can change under a
// running server, and a passkey verified against a stale origin is one that
// fails for no visible reason.
func (self *graph) webAuthn() (*webauthn.WebAuthn, error) {
	settings := self.config.Current().Passkey
	if !settings.Enabled {
		// Without a relying party there is nothing to bind a credential to,
		// and a passkey registered against the wrong origin is one that will
		// never work again.
		return nil, api.ErrNotFound
	}

	relyingParty := settings.RelyingPartyID
	if relyingParty == "" {
		relyingParty = self.config.Current().Server.Name
	}
	displayName := settings.DisplayName
	if displayName == "" {
		displayName = relyingParty
	}
	origins := settings.Origins
	if len(origins) == 0 {
		origins = []string{"https://" + relyingParty}
	}

	return webauthn.New(&webauthn.Config{
		RPID:          relyingParty,
		RPDisplayName: displayName,
		RPOrigins:     origins,
	})
}

// webAuthnUser adapts an account and its credentials to what the library wants.
type webAuthnUser struct {
	user     *config.User
	passkeys []*models.Passkey
}

// WebAuthnID is the stable handle an authenticator stores beside the
// credential. The account's identifier, which never changes — a username
// would, and every passkey would stop resolving when it did.
func (self *webAuthnUser) WebAuthnID() []byte { return []byte(self.user.ID) }

func (self *webAuthnUser) WebAuthnName() string { return self.user.Username }

func (self *webAuthnUser) WebAuthnDisplayName() string {
	if self.user.Name != "" {
		return self.user.Name
	}
	return self.user.Username
}

func (self *webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	credentials := make([]webauthn.Credential, 0, len(self.passkeys))
	for _, passkey := range self.passkeys {
		credentials = append(credentials, webauthn.Credential{
			ID:              passkey.CredentialID,
			PublicKey:       passkey.PublicKey,
			AttestationType: passkey.AttestationType,
			Transport:       protocolTransports(passkey.Transports),
			Flags: webauthn.CredentialFlags{
				BackupEligible: passkey.BackupEligible,
				BackupState:    passkey.BackupState,
			},
			Authenticator: webauthn.Authenticator{
				AAGUID: passkey.AAGUID,
				//nolint:gosec // the counter is whatever the authenticator reported
				SignCount:    uint32(passkey.SignCount),
				CloneWarning: false,
			},
		})
	}
	return credentials
}

func credentialDescriptors(passkeys []*models.Passkey) []protocol.CredentialDescriptor {
	descriptors := make([]protocol.CredentialDescriptor, 0, len(passkeys))
	for _, passkey := range passkeys {
		descriptors = append(descriptors, protocol.CredentialDescriptor{
			Type:         protocol.PublicKeyCredentialType,
			CredentialID: passkey.CredentialID,
			Transport:    protocolTransports(passkey.Transports),
		})
	}
	return descriptors
}

func transportStrings(transports []protocol.AuthenticatorTransport) []string {
	converted := make([]string, 0, len(transports))
	for _, transport := range transports {
		converted = append(converted, string(transport))
	}
	return converted
}

func protocolTransports(transports []string) []protocol.AuthenticatorTransport {
	converted := make([]protocol.AuthenticatorTransport, 0, len(transports))
	for _, transport := range transports {
		converted = append(converted, protocol.AuthenticatorTransport(transport))
	}
	return converted
}
