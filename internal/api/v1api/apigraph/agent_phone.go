package apigraph

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/push"
)

// A person's phones: the TeaNode app on each, which registers itself after it
// connects (internal/api/v1api/apioauth/connect.go), and which the person
// lists, renames, chooses notifications for, tests and removes here, on the
// dashboard and with `teanode agent phone`.

// AgentPhoneQuery lists a person's phones.
type AgentPhoneQuery interface {
	// The phones the caller talks to their agent from, in the TeaNode app,
	// and whether this server can send them notifications. Needs agent:use.
	ListAgentPhones(ctx context.Context) (*AgentPhoneList, error)
}

// AgentPhoneMutation changes them.
type AgentPhoneMutation interface {
	// Register the phone the app runs on, or bring its registration up to
	// date: called by the app itself, with the token it connected with,
	// after it connects and whenever iOS gives it a new push token. Needs
	// agent:use.
	RegisterAgentPhone(ctx context.Context, arguments RegisterAgentPhoneArguments) (*AgentPhoneView, error)

	// Rename a phone or choose which notifications it gets. Needs
	// agent:use.
	UpdateAgentPhone(ctx context.Context, arguments UpdateAgentPhoneArguments) (*AgentPhoneView, error)

	// Remove a phone: it is signed out and gets nothing more. Needs
	// agent:use.
	RemoveAgentPhone(ctx context.Context, arguments AgentPhoneArguments) (bool, error)

	// Send a phone a notification that says it can be reached. Needs
	// agent:use.
	TestAgentPhone(ctx context.Context, arguments AgentPhoneArguments) (*AgentPhoneView, error)
}

// AgentPhoneList is the caller's phones and whether notifications can go
// out at all.
type AgentPhoneList struct {
	// Phones are the person's phones, the first connected first
	Phones []*AgentPhoneView `json:"phones"`

	// IsPushEnabled says whether this server sends notifications to phones
	// at all (agent.push.enabled)
	IsPushEnabled bool `json:"isPushEnabled"`

	// NotificationKinds are every kind a phone may choose: message,
	// approval, question, answer
	NotificationKinds []string `json:"notificationKinds"`
}

// AgentPhoneView is one phone.
type AgentPhoneView struct {
	ID         string `json:"id"`
	PhoneName  string `json:"phoneName"`
	PhoneModel string `json:"phoneModel"`

	// PhoneModelName is PhoneModel as people say it, such as iPhone 17 Pro
	// Max; the identifier itself when it is not one we know
	PhoneModelName string `json:"phoneModelName"`

	// NotificationKinds it receives; every kind when it chose none
	NotificationKinds []string `json:"notificationKinds"`

	// CanBeNotified says whether it has registered for notifications
	CanBeNotified bool `json:"canBeNotified"`

	CreatedAt         time.Time  `json:"createdAt"`
	LastSeenAt        *time.Time `json:"lastSeenAt" graphapi:"nullable"`
	LastDeliveredAt   *time.Time `json:"lastDeliveredAt" graphapi:"nullable"`
	LastDeliveryError string     `json:"lastDeliveryError"`
}

type RegisterAgentPhoneArguments struct {
	// PhoneName is what the phone calls itself
	PhoneName string `json:"phoneName"`

	// PhoneModel is what it is, such as iPhone17,1
	PhoneModel string `json:"phoneModel"`

	// PushRegistration is the push relay's sealed registration for the
	// phone, empty while it has not allowed notifications
	PushRegistration string `json:"pushRegistration" graphapi:"nullable"`

	// PayloadKey is the 256-bit key, base64, the phone decrypts its
	// notifications with
	PayloadKey string `json:"payloadKey" graphapi:"nullable"`
}

type UpdateAgentPhoneArguments struct {
	PhoneID string `json:"phoneId"`

	// PhoneName to give it; empty keeps the one it has
	PhoneName *string `json:"phoneName" graphapi:"nullable"`

	// NotificationKinds it should get; empty is every kind
	NotificationKinds *[]string `json:"notificationKinds" graphapi:"nullable"`
}

type AgentPhoneArguments struct {
	PhoneID string `json:"phoneId"`
}

// Lengths a phone's own words are cut to.
const (
	phoneNameLongest  = 64
	phoneModelLongest = 32
	// pushRegistrationLongest bounds a sealed registration, which is a few
	// hundred characters.
	pushRegistrationLongest = 2048
)

func phoneView(phone *models.AgentPhone) *AgentPhoneView {
	kinds := phone.NotificationKinds
	if len(kinds) == 0 {
		kinds = models.NotificationKinds
	}
	return &AgentPhoneView{
		ID: phone.ID, PhoneName: phone.PhoneName, PhoneModel: phone.PhoneModel,
		PhoneModelName: models.PhoneModelName(phone.PhoneModel), NotificationKinds: kinds,
		CanBeNotified: phone.CanBeNotified(), CreatedAt: phone.CreatedAt, LastSeenAt: phone.LastSeenAt,
		LastDeliveredAt: phone.LastDeliveredAt, LastDeliveryError: phone.LastDeliveryError,
	}
}

func (self *graph) ListAgentPhones(ctx context.Context) (*AgentPhoneList, error) {
	_, found, err := self.requireConversationPerson(ctx)
	if err != nil {
		return nil, err
	}
	phones, err := self.transaction(ctx).ListAgentPhones(found.ID)
	if err != nil {
		return nil, translateError(err)
	}
	list := &AgentPhoneList{
		Phones:            make([]*AgentPhoneView, 0, len(phones)),
		IsPushEnabled:     self.config.Current().Agent.Push.IsEnabled(),
		NotificationKinds: models.NotificationKinds,
	}
	for _, phone := range phones {
		list.Phones = append(list.Phones, phoneView(phone))
	}
	return list, nil
}

func (self *graph) RegisterAgentPhone(ctx context.Context, arguments RegisterAgentPhoneArguments) (*AgentPhoneView, error) {
	_, found, err := self.requireConversationPerson(ctx)
	if err != nil {
		return nil, err
	}
	// A phone is the app that connected with its own token: registering
	// from a browser, or with a token somebody minted by hand, would make a
	// phone nobody can remove by signing it out.
	clientId, err := self.callingAppClient(ctx)
	if err != nil {
		return nil, err
	}
	pushRegistration := strings.TrimSpace(arguments.PushRegistration)
	if len(pushRegistration) > pushRegistrationLongest {
		return nil, fmt.Errorf("%w: that is not a push registration", api.ErrInvalidArguments)
	}
	sealedKey := ""
	if key := strings.TrimSpace(arguments.PayloadKey); key != "" {
		raw, err := base64.StdEncoding.DecodeString(key)
		if err != nil || len(raw) != push.PhoneKeyBytes {
			return nil, fmt.Errorf("%w: the payload key is %d random bytes, base64", api.ErrInvalidArguments, push.PhoneKeyBytes)
		}
		// Sealed with the server secret, which a server with no agent has as
		// well: a phone is told of a chat whatever the agent configuration.
		if sealedKey, err = agent.SealConnectionSecret(self.config.Current(), key); err != nil {
			return nil, err
		}
	}
	phone, err := self.transaction(ctx).PutAgentPhone(&models.AgentPhone{
		AgentID: found.ID, ClientID: clientId,
		PhoneName:        api.DisplayName(arguments.PhoneName, phoneNameLongest),
		PhoneModel:       api.DisplayName(arguments.PhoneModel, phoneModelLongest),
		PushRegistration: pushRegistration, PayloadKey: sealedKey,
	})
	if err != nil {
		return nil, translateError(err)
	}
	return phoneView(phone), nil
}

// callingAppClient is the OAuth client of the token a request was made with,
// when it is one an app was issued by connecting rather than one minted by
// hand.
func (self *graph) callingAppClient(ctx context.Context) (string, error) {
	request := api.ContextRequest(ctx)
	scheme, value, isFound := strings.Cut(request.Header.Get("Authorization"), " ")
	if !isFound || !strings.EqualFold(strings.TrimSpace(scheme), "Bearer") {
		return "", fmt.Errorf("%w: a phone registers from the TeaNode app, with the sign-in it was given", api.ErrInvalidArguments)
	}
	tokenId, ok := self.authenticator.TokenIDOf(strings.TrimSpace(value))
	if !ok {
		return "", fmt.Errorf("%w: a phone registers with the token it was given", api.ErrInvalidArguments)
	}
	token, _, err := self.database.GetToken(tokenId)
	if err != nil {
		return "", err
	}
	if token == nil || token.ClientID == "" {
		return "", fmt.Errorf("%w: a phone registers with the token it was given when it connected, not one made by hand", api.ErrInvalidArguments)
	}
	return token.ClientID, nil
}

// requirePhone is one of the caller's phones. The caller is a person who may
// use the agent, or one who may chat, whose agent may be switched off or not
// exist yet: a phone hangs from the person's agent row and is told of what
// happens in their chats as well as of what their agent says.
func (self *graph) requirePhone(ctx context.Context, phoneId string) (*models.Agent, *models.AgentPhone, error) {
	_, found, err := self.requireConversationPerson(ctx)
	if err != nil {
		return nil, nil, err
	}
	phone, err := self.transaction(ctx).GetAgentPhone(found.ID, strings.TrimSpace(phoneId))
	if err != nil {
		return nil, nil, translateError(err)
	}
	if phone == nil {
		return nil, nil, fmt.Errorf("%w: no phone of yours has that id", api.ErrNotFound)
	}
	return found, phone, nil
}

func (self *graph) UpdateAgentPhone(ctx context.Context, arguments UpdateAgentPhoneArguments) (*AgentPhoneView, error) {
	found, phone, err := self.requirePhone(ctx, arguments.PhoneID)
	if err != nil {
		return nil, err
	}
	name := phone.PhoneName
	if arguments.PhoneName != nil {
		if name = api.DisplayName(*arguments.PhoneName, phoneNameLongest); name == "" {
			return nil, fmt.Errorf("%w: a phone needs a name", api.ErrInvalidArguments)
		}
	}
	kinds := phone.NotificationKinds
	if arguments.NotificationKinds != nil {
		kinds = nil
		for _, kind := range *arguments.NotificationKinds {
			kind = strings.TrimSpace(kind)
			if !slices.Contains(models.NotificationKinds, kind) {
				return nil, fmt.Errorf("%w: %q is not a kind of notification; the kinds are %s", api.ErrInvalidArguments, kind, strings.Join(models.NotificationKinds, ", "))
			}
			if !slices.Contains(kinds, kind) {
				kinds = append(kinds, kind)
			}
		}
	}
	tx := self.transaction(ctx)
	if err := tx.UpdateAgentPhoneSettings(found.ID, phone.ID, name, kinds); err != nil {
		return nil, translateError(err)
	}
	phone.PhoneName, phone.NotificationKinds = name, kinds
	return phoneView(phone), nil
}

func (self *graph) RemoveAgentPhone(ctx context.Context, arguments AgentPhoneArguments) (bool, error) {
	found, phone, err := self.requirePhone(ctx, arguments.PhoneID)
	if err != nil {
		return false, err
	}
	if err := self.transaction(ctx).DeleteAgentPhone(found.ID, phone.ID); err != nil {
		return false, translateError(err)
	}
	// Signed out as well: a phone removed here must not go on acting as
	// the person. Every token its client holds, since a refresh replaces
	// the one it connected with.
	if _, err := self.database.RevokeClientTokens(found.UserID, phone.ClientID, time.Now()); err != nil {
		return false, err
	}
	return true, nil
}

func (self *graph) TestAgentPhone(ctx context.Context, arguments AgentPhoneArguments) (*AgentPhoneView, error) {
	found, phone, err := self.requirePhone(ctx, arguments.PhoneID)
	if err != nil {
		return nil, err
	}
	notifier := self.phoneNotifier()
	if notifier == nil {
		return nil, agent.ErrUnavailable
	}
	name := found.Name
	if strings.TrimSpace(name) == "" {
		name = "TeaNode"
	}
	if err := notifier.NotifyPhone(ctx, phone, &push.Content{
		NotificationKind: "test", Title: name, Body: "This phone can be reached. Notifications from " + name + " will arrive here.",
	}); err != nil {
		return nil, fmt.Errorf("%w: %w", api.ErrInvalidArguments, err)
	}
	updated, err := self.transaction(ctx).GetAgentPhone(found.ID, phone.ID)
	if err != nil || updated == nil {
		return phoneView(phone), nil
	}
	return phoneView(updated), nil
}
