// Package apigraph implements the GraphQL endpoint, which is the whole of the
// management API.
//
// Queries over mail, deliveries and reports read the database. Everything
// about domains, aliases, credentials, accounts and tokens reads and writes
// the configuration file through config.Store, so a change made here ends up
// in teanode.yaml and survives a restart. That is why the command line client
// goes through this endpoint too rather than editing the file: the running
// server is the only writer.
package apigraph

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/graphql-go/graphql"
	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/dns"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/mcpserve"
	"github.com/ziyan/teanode/internal/photo"
	"github.com/ziyan/teanode/internal/place"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/upgrade"
	"github.com/ziyan/teanode/internal/util/ceremony"
	"github.com/ziyan/teanode/internal/util/geoip"
	"github.com/ziyan/teanode/internal/util/graphapi"
	"github.com/ziyan/teanode/internal/util/ratelimit"
	"github.com/ziyan/teanode/internal/web"
)

var log = logging.MustGetLogger("apigraph")

type graph struct {
	// locations throttles how often an account's zone and language are
	// written; see location.go.
	locations locationTouches

	// photoAddresses remembers the street a provider named for a position, so
	// that opening a photo again does not ask it again; see photo_place.go.
	photoAddresses place.RememberedAddresses

	database db.Database
	config   config.Store
	storage  storage.Storage
	locator  geoip.Locator
	verifier dns.Verifier
	mailer   mailer.Mailer
	settings *api.Settings

	// upgrade knows what has been released and can replace this binary with
	// it. Nil when the server was started without it, in which case the
	// dashboard is told there is nothing to say rather than shown an empty
	// card.
	upgrade upgrade.Manager
	schema  graphql.Schema

	// started is when this process built the API, which is close enough to
	// when it started to be what the dashboard shows as uptime.
	started time.Time

	// authenticator is used only by the session and passkey resolvers, which
	// are the only ones a browser calls before it is anybody.
	authenticator web.Authenticator

	// ceremonies parks half-finished WebAuthn challenges, and setup profiles
	// until their one download (mailbox_profile.go). In this process by
	// default; in Redis when one is configured, which is what makes both
	// work behind a load balancer.
	ceremonies ceremony.Store

	// mcpResults holds the rest of a tool's long answer over MCP until the
	// caller reads on with result_more, in the database, so the next request
	// may reach any instance; see agent_mcp.go. Nil in a test that built the
	// graph by hand.
	mcpResults *mcpserve.ResultStore

	// photoStoreValue is the photo library's store, made by photoStore the
	// first time a photo is touched; nil when the blob store cannot stream.
	photoStoreOnce  sync.Once
	photoStoreValue *photo.Store

	// photoSearchMeanings keeps the words of a photo search read as a vector
	// for a few seconds, so that the listing and the count a page asks for
	// together call the embedding model once.
	photoSearchMeanings photoSearchMeaningCache

	// chatLimits are what one person may do in chats a minute on this
	// instance; made the first time they are wanted (chat.go).
	chatLimitsOnce  sync.Once
	chatLimitsValue *chatRateLimits

	// locationEvents is the one listener for location events, fanned out to
	// the maps open on this instance (location_events.go); mapLimits is what
	// one account may fetch of the map a minute (location_map.go).
	locationEvents locationEventHub

	// shoppingEvents is the one listener for shopping list changes, fanned
	// out to the lists open on this instance.
	shoppingEvents shoppingEventHub
	mapLimitsOnce  sync.Once
	mapLimitsValue *ratelimit.Registry

	// linkPreviews is what the previews of links need: the fetcher and its
	// limits (link_preview.go).
	linkPreviews linkPreviewState

	// locationLimits are what one account may ask of the place provider and
	// how fast it may make places (location_limits.go).
	locationLimitsOnce  sync.Once
	locationLimitsValue *locationRateLimits

	// watchedStores keeps the branches of stores found near a position, for
	// the phone to watch (shopping_watch.go).
	watchedStores watchedStoreCache

	// passwordsLimits bound the wrong proofs one person gives to unlock
	// Passwords and how fast they change it (passwords.go).
	passwordsLimits passwordsLimiters
}

// New builds the GraphQL component, generating the schema by reflection over
// the Query, Mutation and Subscription interfaces.
func New(database db.Database, configuration config.Store, messages storage.Storage, locator geoip.Locator, verifier dns.Verifier, sender mailer.Mailer, upgrader upgrade.Manager, authenticator web.Authenticator, ceremonies ceremony.Store, settings *api.Settings) (web.Component, error) {
	self := &graph{
		database:      database,
		config:        configuration,
		storage:       messages,
		locator:       locator,
		verifier:      verifier,
		mailer:        sender,
		upgrade:       upgrader,
		authenticator: authenticator,
		ceremonies:    ceremonies,
		settings:      settings,
		started:       time.Now(),
		mcpResults:    mcpserve.NewResultStore(database),
	}

	graphApi := graphapi.New()
	if worker := self.agentWorker(); worker != nil {
		worker.SetOperationsFactory(self.operationsFor)
	}
	var query Query = self
	var mutation Mutation = self
	var subscription Subscription = self
	if err := graphApi.Register(&query, &mutation, &subscription); err != nil {
		return nil, err
	}
	schema, err := graphApi.Build()
	if err != nil {
		return nil, err
	}
	self.schema = schema
	self.wrapServiceOperations()
	self.wrapQueryTransactions()
	self.wrapTransactionFreeMutations()
	return self, nil
}

func (self *graph) AddRoutes(router *mux.Router) error {
	router.Path(api.PathGraphQL).Methods(http.MethodGet).HandlerFunc(self.webSocketView)
	router.Path(api.PathGraphQL).Methods(http.MethodPost).HandlerFunc(self.withLocation(self.graphView))
	// The Model Context Protocol, for a harness using the person's tools.
	router.Path(api.PathAgentMCP).Methods(http.MethodPost).HandlerFunc(self.mcpView)
	router.Path(api.PathAgentTab).Methods(http.MethodGet).HandlerFunc(self.tabView)
	router.Path(api.PathAgentTabView).Methods(http.MethodGet).HandlerFunc(self.tabViewerView)
	router.Path(api.PathAgentVoice).Methods(http.MethodGet).HandlerFunc(self.voiceView)
	router.Path(api.PathAgentVoiceSample).Methods(http.MethodGet).HandlerFunc(self.voiceSampleView)
	router.Path(api.PathAgentComputer).Methods(http.MethodGet).HandlerFunc(self.computerView)
	// Files for a draft go up as multipart bodies, not inside a query: a
	// browser can stream them and show how far along each is.
	router.Path(api.PathDraftAttachments).Methods(http.MethodPut).HandlerFunc(self.draftAttachmentsView)
	router.Path(api.PathNewDraftAttachments).Methods(http.MethodPost).HandlerFunc(self.draftAttachmentsView)
	// And files for a conversation with the agent, the same way.
	router.Path(api.PathRecordingPart).Methods(http.MethodPut).HandlerFunc(self.recordingPartView)
	router.Path(api.PathRecordingComplete).Methods(http.MethodPost).HandlerFunc(self.recordingCompleteView)
	router.Path(api.PathRecordingMedia).Methods(http.MethodGet, http.MethodHead).HandlerFunc(self.recordingMediaView)
	router.Path(api.PathRecordingPlayback).Methods(http.MethodGet, http.MethodHead).HandlerFunc(self.recordingPlaybackView)
	router.Path(api.PathRecordingLive).Methods(http.MethodGet).HandlerFunc(self.recordingLiveView)
	// Photos and videos for the library go up over the resumable upload
	// protocol, and come back as previews and files for their owner.
	router.Path(api.PathPhotoUploads).Methods(http.MethodOptions, http.MethodPost).HandlerFunc(self.photoUploadCollectionView)
	router.Path(api.PathPhotoUpload).Methods(http.MethodOptions, http.MethodHead, http.MethodPatch, http.MethodDelete).HandlerFunc(self.photoUploadView)
	router.Path(api.PathPhotoPreview).Methods(http.MethodGet, http.MethodHead).HandlerFunc(self.photoPreviewView)
	router.Path(api.PathPhotoFile).Methods(http.MethodGet, http.MethodHead).HandlerFunc(self.photoFileView)
	router.Path(api.PathPhotoFacePicture).Methods(http.MethodGet, http.MethodHead).HandlerFunc(self.photoFacePictureView)
	router.Path(api.PathPhotoPlaybackPreview).Methods(http.MethodGet, http.MethodHead).HandlerFunc(self.photoPlaybackPreviewView)
	router.Path(api.PathPhotoPlaybackFile).Methods(http.MethodGet, http.MethodHead).HandlerFunc(self.photoPlaybackFileView)
	router.Path(api.PathAgentAttachments).Methods(http.MethodPost).HandlerFunc(self.agentAttachmentsView)
	router.Path(api.PathAgentAttachment).Methods(http.MethodGet, http.MethodHead).HandlerFunc(self.agentAttachmentView)
	// And the file behind a document a knowledge source indexed, so that
	// a page citing a screenshot can show the screenshot.
	router.Path(api.PathAgentDocumentFile).Methods(http.MethodGet).HandlerFunc(self.agentDocumentFileView)
	// And a picture the agent put in an answer, fetched here rather than by
	// the browser.
	router.Path(api.PathAgentPicture).Methods(http.MethodGet).HandlerFunc(self.agentPictureView)
	// A contact's picture, which is inside its card and far too large to
	// carry through a listing.
	router.Path(api.PathContactPhoto).Methods(http.MethodGet, http.MethodHead).HandlerFunc(self.contactPhotoView)
	// A person's picture, which is a file in storage and far too large to
	// carry through a listing.
	router.Path(api.PathAvatarUser).Methods(http.MethodGet, http.MethodHead).HandlerFunc(self.avatarView)
	// The picture of a link preview, fetched from the linked page once and
	// served from here (link_preview.go).
	router.Path(api.PathLinkPreviewImage).Methods(http.MethodGet, http.MethodHead).HandlerFunc(self.linkPreviewImageView)
	router.Path(api.PathShoppingPicture).Methods(http.MethodGet, http.MethodHead).HandlerFunc(self.shoppingPictureView)
	// The map style host's files, fetched here for the signed-in browser
	// (location_map.go).
	router.PathPrefix(api.PathMap + "/").Methods(http.MethodGet).HandlerFunc(self.mapView)
	// A setup profile, downloaded once by the device that installs it.
	router.Path(api.PathMailboxSetupProfile).Methods(http.MethodGet).HandlerFunc(self.setupProfileView)
	return nil
}
