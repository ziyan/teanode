package apigraph

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Every resolver authorizes itself, because the GraphQL endpoint is reachable
// without a session: logging in happens at the same endpoint as everything
// else. TestEveryOperationAuthorizes reads the source and fails when one does
// not call one of the helpers here.
//
// The rule for a refusal is "not found", never "forbidden": "you may not touch
// this" confirms the row exists, which is itself a leak. Only the checks that
// have no row to hide — "you are not signed in" — say so.
//
// An unclaimed server refuses everything here too. Onboarding does not come
// through these helpers: GetSession and CreateFirstAccount never call them,
// which is what makes claiming the server still possible.

// requireSignedIn checks that the caller is somebody: an account, or the
// console. Enough for what is about the caller themselves — their tokens,
// their sessions, their passkeys.
func (self *graph) requireSignedIn(ctx context.Context) (*api.Principal, error) {
	principal := api.ContextPrincipal(ctx)
	if principal == nil {
		return nil, api.ErrNotLoggedIn
	}
	return principal, nil
}

// requirePermission checks that the caller holds a server or all-domains
// permission.
func (self *graph) requirePermission(ctx context.Context, permission models.Permission) (*api.Principal, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if !principal.Permissions.Has(permission) {
		return nil, api.ErrNotFound
	}
	return principal, nil
}

// requireAnyPermission checks that the caller holds a domain permission over
// at least one domain, or everywhere: what opens a page that lists things
// before any one of them is chosen.
func (self *graph) requireAnyPermission(ctx context.Context, permission models.Permission) (*api.Principal, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if !principal.Permissions.HasAnywhere(permission) {
		return nil, api.ErrNotFound
	}
	return principal, nil
}

// requireManagement checks that the caller may see the management side of
// the web UI at all: the server's own addresses, and the like.
func (self *graph) requireManagement(ctx context.Context) (*api.Principal, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if !principal.Permissions.Manages() {
		return nil, api.ErrNotFound
	}
	return principal, nil
}

// requireDomainPermission checks that the caller holds a domain permission
// over this domain, and returns the domain. A domain the caller may not touch
// reads as not found, which is also what a caller sees for a domain deleted
// since the mail referencing it arrived.
func (self *graph) requireDomainPermission(ctx context.Context, permission models.Permission, domainId string) (*models.Domain, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if domainId == "" || !principal.Permissions.HasOverDomain(permission, domainId) {
		return nil, api.ErrNotFound
	}
	domain, err := self.transaction(ctx).GetDomain(domainId)
	if err != nil {
		return nil, err
	}
	if domain == nil {
		return nil, api.ErrNotFound
	}
	return domain, nil
}

// domainsToList resolves a domain filter into the domains to query: the one
// named, when the caller holds the permission over it, or every domain they
// hold it over when none is named.
//
// An empty filter means every domain the caller may see rather than none,
// because a page that shows nothing until a domain is chosen is a page that
// answers no question on the first screen.
func (self *graph) domainsToList(ctx context.Context, permission models.Permission, domainId string) ([]string, error) {
	if domainId != "" {
		domain, err := self.requireDomainPermission(ctx, permission, domainId)
		if err != nil {
			return nil, err
		}
		return []string{domain.ID}, nil
	}
	principal, err := self.requireAnyPermission(ctx, permission)
	if err != nil {
		return nil, err
	}
	domainIds, all := principal.Permissions.DomainsWith(permission)
	if !all {
		return domainIds, nil
	}
	domains, err := self.transaction(ctx).ListDomains()
	if err != nil {
		return nil, err
	}
	domainIds = make([]string, 0, len(domains))
	for _, domain := range domains {
		domainIds = append(domainIds, domain.ID)
	}
	return domainIds, nil
}

// requireAccount is requireSignedIn plus the account it resolved to.
//
// Anything about a person rather than about the server needs the account
// itself: its identifier is what passkeys, sessions and tokens are stored
// against, and the username the request was authenticated as is only a way of
// finding it. The console is not an account.
func (self *graph) requireAccount(ctx context.Context) (*models.User, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	return principal.User, nil
}

// transaction is the one this request runs in.
func (self *graph) transaction(ctx context.Context) db.Transaction {
	return api.ContextTransaction(ctx)
}

// domainExists says whether a row's domain is still configured. Stored mail
// keeps its domain identifier after the domain is deleted, and the API says
// so rather than pretending the mail is gone.
func (self *graph) domainExists(ctx context.Context, domainId string) (bool, error) {
	if domainId == "" {
		return false, nil
	}
	domain, err := self.transaction(ctx).GetDomain(domainId)
	if err != nil {
		return false, err
	}
	return domain != nil, nil
}

// findAccountById is how a stored credential names its account.
func (self *graph) findAccountById(userId string) *models.User {
	if userId == "" {
		return nil
	}
	user, err := self.database.GetUser(userId)
	if err != nil {
		log.Errorf("failed to read the account %q: %s", userId, err)
		return nil
	}
	return user
}

// operatorName is who to name in a log line for a change.
func operatorName(ctx context.Context) string {
	if principal := api.ContextPrincipal(ctx); principal != nil {
		return principal.Username()
	}
	return api.ContextAuthenticatedUsername(ctx)
}

// domainStillExists says whether a row's domain is still configured, for
// the pages that show mail of a domain since deleted.
func (self *graph) domainStillExists(ctx context.Context, domainId string) bool {
	exists, err := self.domainExists(ctx, domainId)
	if err != nil {
		log.Errorf("failed to read the domain %q: %s", domainId, err)
		return false
	}
	return exists
}

// translateError turns what the database says into what the API says.
func translateError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, db.ErrNotFound):
		return api.ErrNotFound
	case errors.Is(err, db.ErrAlreadyExists):
		return api.ErrAlreadyExists
	case errors.Is(err, db.ErrInvalidArguments):
		return api.ErrInvalidArguments
	case errors.Is(err, db.ErrTooMuchAsked):
		// The caller's to narrow, so the caller is told rather than shown
		// a server that looks broken.
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	var validation models.ValidationErrors
	if errors.As(err, &validation) {
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, validation.Error())
	}
	return err
}

// requireReadableMail is a message the caller may see: as the operator of
// its domain, or as the owner of a mailbox holding it. Anything else, and a
// message that does not exist, is not found.
func (self *graph) requireReadableMail(ctx context.Context, mailId string) (*models.Mail, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, err
	}
	mail, err := self.transaction(ctx).GetMail(mailId, nil)
	if err != nil {
		return nil, err
	}
	if mail == nil {
		return nil, api.ErrNotFound
	}
	allowed, err := access.CanReadMail(self.transaction(ctx), principal.User, principal.Permissions, mail)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, api.ErrNotFound
	}
	return mail, nil
}

// requireCirclePerson checks that the caller is an account that may use
// circles, and returns it. The console is not an account and is in no circle.
func (self *graph) requireCirclePerson(ctx context.Context) (*models.User, error) {
	principal, err := self.requirePermission(ctx, models.PermissionCircleUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotFound
	}
	return principal.User, nil
}

// requireCircleMember checks that the caller may use circles and is in this
// one, and returns the circle with its members and the caller's own row. A
// circle the caller is not in reads as not found, which is also what a
// circle that was deleted since they last looked reads as.
func (self *graph) requireCircleMember(ctx context.Context, circleId string) (*models.Circle, *models.CircleMember, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return nil, nil, err
	}
	circle, err := self.transaction(ctx).GetCircle(circleId)
	if err != nil {
		return nil, nil, err
	}
	if circle == nil {
		return nil, nil, api.ErrNotFound
	}
	member := circle.Member(user.ID)
	if member == nil {
		return nil, nil, api.ErrNotFound
	}
	return circle, member, nil
}

// requireCircleAdmin is requireCircleMember for something only an admin of
// the circle may do. A member who is not an admin can see the circle, so they
// are told in words rather than shown a circle that does not exist.
func (self *graph) requireCircleAdmin(ctx context.Context, circleId string) (*models.Circle, *models.CircleMember, error) {
	circle, member, err := self.requireCircleMember(ctx, circleId)
	if err != nil {
		return nil, nil, err
	}
	if member.MemberRole != models.MemberRoleAdmin {
		return nil, nil, fmt.Errorf("%w: only an admin of this circle can do that", api.ErrPermissionDenied)
	}
	return circle, member, nil
}

// requireChatPerson checks that the caller is an account that may use
// circles, and returns it with its agent, made switched off when there is none:
// a person can chat with their agent off, and their conversations and phones
// hang from the agent's row. The agent being on is not asked.
func (self *graph) requireChatPerson(ctx context.Context) (*models.User, *models.Agent, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return nil, nil, err
	}
	found, err := self.transaction(ctx).EnsureAgent(user.ID)
	if err != nil {
		return nil, nil, err
	}
	return user, found, nil
}

// chatSession is a chat as one of its members reaches it: who they are, their
// own conversation for it, and the chat with their place in it.
type chatSession struct {
	User         *models.User
	Agent        *models.Agent
	Conversation *models.AgentConversation
	Chat         *models.Chat
	Member       *models.ChatMember
}

// requireChatMember checks that the caller may chat and that the conversation
// is their own conversation for a chat they are, or were, in, and returns
// the chat. Anything else, including another person's conversation and an
// operator asking for one, is not found: a chat is between its people.
func (self *graph) requireChatMember(ctx context.Context, conversationId string) (*chatSession, error) {
	user, found, err := self.requireChatPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	conversation, err := tx.GetAgentConversation(strings.TrimSpace(conversationId))
	if err != nil {
		return nil, err
	}
	if conversation == nil || !conversation.IsChat() || conversation.AgentID != found.ID {
		return nil, api.ErrNotFound
	}
	return chatSessionOf(tx, user, found, conversation)
}

// chatSessionOf is the caller's place in the chat their conversation is for,
// or not found when they have none.
func chatSessionOf(tx db.Transaction, user *models.User, found *models.Agent, conversation *models.AgentConversation) (*chatSession, error) {
	chat, err := tx.GetChat(conversation.ChatID)
	if err != nil {
		return nil, err
	}
	if chat == nil {
		return nil, api.ErrNotFound
	}
	member := chat.Member(user.ID)
	if member == nil {
		return nil, api.ErrNotFound
	}
	return &chatSession{User: user, Agent: found, Conversation: conversation, Chat: chat, Member: member}, nil
}

// requireChatMessage is requireChatMember for a message: it must be one the
// caller can read, which is one written while they were in the chat.
func (self *graph) requireChatMessage(ctx context.Context, messageId string) (*chatSession, *models.ChatMessage, error) {
	user, found, err := self.requireChatPerson(ctx)
	if err != nil {
		return nil, nil, err
	}
	tx := self.transaction(ctx)
	message, err := tx.GetChatMessage(strings.TrimSpace(messageId))
	if err != nil {
		return nil, nil, err
	}
	if message == nil {
		return nil, nil, api.ErrNotFound
	}
	chat, err := tx.GetChat(message.ChatID)
	if err != nil {
		return nil, nil, err
	}
	if chat == nil {
		return nil, nil, api.ErrNotFound
	}
	member := chat.Member(user.ID)
	if member == nil || !isReadableByMember(message, member) {
		return nil, nil, api.ErrNotFound
	}
	conversation, err := tx.EnsureChatConversation(found.ID, chat.ID)
	if err != nil {
		return nil, nil, err
	}
	return &chatSession{User: user, Agent: found, Conversation: conversation, Chat: chat, Member: member}, message, nil
}

// isReadableByMember says whether a message was written while the member
// was in the chat: after they joined, and before they left if they did.
func isReadableByMember(message *models.ChatMessage, member *models.ChatMember) bool {
	if message.CreatedAt.Before(member.JoinedAt) {
		return false
	}
	return member.RemovedAt == nil || !message.CreatedAt.After(*member.RemovedAt)
}

// requireConversationPerson is the caller for the operations on conversations,
// which a chat shares with a conversation with the agent. It lets in a person
// who may use circles, whose agent need not be on, and one who may use the
// agent, whose agent must be: which of them it takes is settled once the
// conversation is known, by requireConversationUse. The agent is the caller's,
// made switched off for a person who can chat and has none.
func (self *graph) requireConversationPerson(ctx context.Context) (*api.Principal, *models.Agent, error) {
	principal, err := self.requireSignedIn(ctx)
	if err != nil {
		return nil, nil, err
	}
	if principal.User == nil {
		return nil, nil, api.ErrNotLoggedIn
	}
	canChat := principal.Permissions.Has(models.PermissionCircleUse)
	if !canChat && !principal.Permissions.Has(models.PermissionAgentUse) {
		return nil, nil, api.ErrNotFound
	}
	tx := self.transaction(ctx)
	found, err := tx.GetAgentByUser(principal.User.ID)
	if err != nil {
		return nil, nil, err
	}
	// A person who can chat and has no agent row gets one switched off, but
	// only once they are in a circle to chat with: opening the drawer is not
	// a reason to make a row for everybody on the server.
	if found == nil && canChat {
		circles, err := tx.ListCircles(principal.User.ID)
		if err != nil {
			return nil, nil, err
		}
		if len(circles) > 0 {
			if found, err = tx.EnsureAgent(principal.User.ID); err != nil {
				return nil, nil, err
			}
		}
	}
	if found == nil {
		return nil, nil, agent.ErrUnavailable
	}
	return principal, found, nil
}

// requireConversationUse says whether the caller may use the conversation:
// a chat needs circle:use, and any other conversation the agent permission
// and an agent that is on.
func requireConversationUse(principal *api.Principal, found *models.Agent, conversation *models.AgentConversation) error {
	if conversation.IsChat() {
		if !principal.Permissions.Has(models.PermissionCircleUse) {
			return api.ErrNotFound
		}
		return nil
	}
	if !principal.Permissions.Has(models.PermissionAgentUse) {
		return api.ErrNotFound
	}
	if !found.Active() {
		return agent.ErrUnavailable
	}
	return nil
}
