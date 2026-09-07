package api

import "fmt"

// Prefix is where the current API version is mounted. Everything the
// dashboard and the command line client call lives under it.
//
// The version is in the path rather than in a header so that a request can be
// read, logged and routed by a proxy without knowing anything about TeaNode,
// and so that a future version 2 can be served alongside version 1 rather
// than replacing it under everybody.
const Prefix = "/api/v1"

// The paths making up version 1.
const (
	// PathGraphQL is the whole of the management API. POST executes a query
	// or mutation; GET upgrades to a WebSocket for subscriptions.
	PathGraphQL = Prefix + "/graphql"

	// PathSend accepts a request to send a templated message.
	PathSend = Prefix + "/send/{domain}/{template}"

	// PathMailRaw serves a stored message in its original form, as a file the
	// browser can save.
	PathMailRaw = Prefix + "/mail/{mailId}/raw"

	// PathMailAttachment serves one part of a stored message: a file to save,
	// or the image an HTML part refers to with a cid: URL.
	PathMailAttachment = Prefix + "/mail/{mailId}/attachment/{index}"

	// PathMediaUpload accepts a picture to put in a template. An operator's
	// action, so it is inside the API and behind a session.
	PathMediaUpload = Prefix + "/media"

	// PathMediaFile serves an uploaded picture. Outside the API prefix, and
	// so outside the session check, because the callers are the dashboard's
	// preview and — once a message has been sent — mail programs, which have
	// no session and never will.
	PathMediaFile = "/media/{mediaId}"

	// PathMediaLink serves a picture at an address belonging to one sent
	// message. Public, and short, because it goes in the message: a mail
	// program fetching it is how an open is noticed.
	PathMediaLink = "/m/{token}"

	// PathMailRemote fetches an image a message links to somewhere else, on
	// behalf of a reader who asked for it. The address in the query is the
	// address in the message.
	PathMailRemote = Prefix + "/mail/{mailId}/remote"
)

// MailAttachmentPath is PathMailAttachment with its parameters filled in.
func MailAttachmentPath(mailId string, index int) string {
	return fmt.Sprintf("%s/mail/%s/attachment/%d", Prefix, mailId, index)
}

// MediaPath is PathMediaFile with its parameter filled in. It is where a
// picture lives while a template is being edited; a sent message names an
// address of its own instead.
func MediaPath(mediaId string) string {
	return "/media/" + mediaId
}

// MediaLinkPath is PathMediaLink with its token filled in.
func MediaLinkPath(token string) string {
	return "/m/" + token
}

// MailRemotePath is PathMailRemote with its parameter filled in. The caller
// appends the address it wants, escaped.
func MailRemotePath(mailId string) string {
	return fmt.Sprintf("%s/mail/%s/remote", Prefix, mailId)
}

const ()

// PublicPaths are reachable without the middleware turning them away.
//
// The GraphQL endpoint is on the list, which looks alarming and is not:
// logging in happens there, so a caller has to be able to reach it before
// being anybody. Authorisation is the resolvers' job — every one of them
// refuses a caller who is not an operator, and TestEveryOperationAuthorises
// fails if a new one forgets. The middleware was never the thing keeping the
// mail private; it was a second lock on the same door, and having it made the
// login endpoints have to live outside GraphQL.
func PublicPaths() []string {
	return []string{PathGraphQL}
}

// PublicPrefixes are reachable without the middleware turning them away, for
// paths with a parameter in them, which an exact match cannot name.
//
// The send endpoint is here because the caller is not an operator: it is an
// application holding an SMTP credential, and it authenticates with that
// credential inside the handler, the way it would at the submission port.
// The middleware turning it away for having no session made the endpoint
// unreachable on any server with an account, which is every server.
func PublicPrefixes() []string {
	// Sending with an API key carries its own authentication; single
	// sign-on is how somebody with no session yet gets one.
	return []string{Prefix + "/send/", Prefix + "/sso/"}
}
