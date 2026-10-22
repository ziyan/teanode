package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/models"
)

// Sharing pages of the agent's memory with other people, and reading the
// pages they shared. docs/subsystems/memory.md ("Sharing") says what a reader
// is shown and what is left out, and docs/decisions/
// 20261016-shared-knowledge-is-read-where-it-lives.md why nothing is copied.
//
// A page is shared by its owner, with one person in a circle they share or
// with a circle they are in, and the share is the owner's alone to see and to
// take back. A reader sees only what internal/agent/shared_memory.go shows
// them, and a page that is not shared with the caller, or is a sibling of one
// that is, is not found.

// AgentPageShareQuery reads shares.
type AgentPageShareQuery interface {
	// The shares the caller made of their own pages, by path, each with who
	// it is for. Needs agent:use.
	ListAgentPageShares(ctx context.Context) ([]*AgentPageShareView, error)

	// Exactly what a person or a circle would be shown of a page, and of
	// what is under it, if the caller shared it with them: the same view a
	// share gives, made before there is one, so that the owner sees it
	// before they confirm. Needs agent:use, and a person the caller shares
	// a circle with or a circle the caller is in.
	PreviewAgentPageShare(ctx context.Context, arguments PreviewAgentPageShareArguments) (*AgentPageSharePreviewView, error)

	// The pages other people shared with the caller, one row for each root
	// of a shared subtree, by who shared it. Needs circle:use.
	ListSharedAgentPages(ctx context.Context) ([]*SharedAgentPageRootView, error)

	// Find, by words, pages and facts in the pages other people shared with
	// the caller: the same search the agent's memory tool makes of them, and
	// only within what is shared. Each hit says whose it is, and the answer says
	// when more matched than first allows. Needs circle:use.
	SearchSharedAgentPages(ctx context.Context, arguments SearchSharedAgentPagesArguments) (*SharedAgentPageSearchView, error)

	// One page another person shared with the caller, or a page under one:
	// its name, opening and facts, the links to other shared
	// pages, and the pages under it. Anything not shared with the caller,
	// including a page beside a shared one, is not found. Needs circle:use.
	ReadSharedAgentPage(ctx context.Context, arguments ReadSharedAgentPageArguments) (*SharedAgentPageView, error)
}

// AgentPageShareMutation shares and takes back.
type AgentPageShareMutation interface {
	// Share one of the caller's pages, and everything under it, with a
	// person they share a circle with or a circle they are in, read only. A
	// page shared again with the same person or circle changes whether its
	// quotes are shared. Needs agent:use and circle:use.
	ShareAgentPage(ctx context.Context, arguments ShareAgentPageArguments) (*AgentPageShareView, error)

	// Take a share back. The reader loses the page at once, because
	// nothing was copied. Needs agent:use.
	UnshareAgentPage(ctx context.Context, arguments UnshareAgentPageArguments) (bool, error)
}

// AgentPageShareView is one share the caller made.
type AgentPageShareView struct {
	ID string `json:"id"`

	// The root of the shared subtree, and what the page is called
	Path     string `json:"path"`
	PageName string `json:"pageName"`

	// Who it is shared with: a person or a circle, never both
	GranteeUserID           *string `json:"granteeUserId"`
	GranteeUsername         string  `json:"granteeUsername"`
	GranteeName             string  `json:"granteeName"`
	GranteeAvatarURL        *string `json:"granteeAvatarUrl"`
	GranteeAvatarColorIndex int     `json:"granteeAvatarColorIndex"`
	GranteeCircleID         *string `json:"granteeCircleId"`
	GranteeCircleName       string  `json:"granteeCircleName"`

	// Whether the words each fact was read in are shown to the reader
	IsQuoteShared bool `json:"isQuoteShared"`

	CreatedAt time.Time `json:"createdAt"`
}

// SharedAgentPageRootView is a root of a subtree another person shared with
// the caller.
type SharedAgentPageRootView struct {
	// The person who shared it
	OwnerUserID           string  `json:"ownerUserId"`
	OwnerUsername         string  `json:"ownerUsername"`
	OwnerName             string  `json:"ownerName"`
	OwnerAvatarURL        *string `json:"ownerAvatarUrl"`
	OwnerAvatarColorIndex int     `json:"ownerAvatarColorIndex"`

	Path     string `json:"path"`
	Name     string `json:"name"`
	NodeKind string `json:"nodeKind"`

	// The first sentence of what the page says
	Opening       string `json:"opening"`
	IsQuoteShared bool   `json:"isQuoteShared"`
}

// SharedAgentPageHitView is a page or a fact of a shared page that a search
// found.
type SharedAgentPageHitView struct {
	// The person who shared it
	OwnerUserID   string `json:"ownerUserId"`
	OwnerUsername string `json:"ownerUsername"`
	OwnerName     string `json:"ownerName"`

	Path string `json:"path"`
	Name string `json:"name"`

	// The page's first sentence for a page, and empty for a fact
	Opening string `json:"opening"`

	// Which fact of the page it is, as its owner numbers them; zero for a
	// page
	FactNumber int `json:"factNumber"`

	// What the fact says; empty for a page
	Text string `json:"text"`
}

// SharedAgentPageView is a page another person shared, as the caller is
// shown it.
type SharedAgentPageView struct {
	OwnerUserID           string  `json:"ownerUserId"`
	OwnerUsername         string  `json:"ownerUsername"`
	OwnerName             string  `json:"ownerName"`
	OwnerAvatarURL        *string `json:"ownerAvatarUrl"`
	OwnerAvatarColorIndex int     `json:"ownerAvatarColorIndex"`

	Path     string   `json:"path"`
	Name     string   `json:"name"`
	NodeKind string   `json:"nodeKind"`
	Aliases  []string `json:"aliases"`

	// What the page says. Its overview is not shared (models.SharedOverviewNotice)
	Opening string `json:"opening"`

	// Whether the words each fact was read in are shown
	IsQuoteShared bool `json:"isQuoteShared"`

	Facts []*models.SharedFact `json:"facts"`

	// Links to pages that are shared too, and no others
	Edges    []*models.SharedEdge  `json:"edges"`
	Children []*models.SharedChild `json:"children"`
}

// AgentPageSharePreviewView is what a share would show, and which pages it
// would reach.
type AgentPageSharePreviewView struct {
	Page *SharedAgentPageView `json:"page"`

	// How many pages the share reaches: the page and everything under it. At
	// least that many where the count was cut. A page added under it later is
	// reached too.
	ReachedPageCount      int  `json:"reachedPageCount"`
	IsReachedPageCountCut bool `json:"isReachedPageCountCut"`

	// The paths of the first pages it reaches, by path and at most
	// reachedPagePathsMost of them; ReachedPageCount is the total.
	ReachedPagePaths []string `json:"reachedPagePaths"`

	// How many facts on the pages it reaches are kept from the reader whatever
	// the share, because all of their evidence is finance, recordings, the
	// location journal or a private calendar event, which have a sharing of
	// their own.
	WithheldFactCount int `json:"withheldFactCount"`
}

// reachedPageCountMost is how many pages a preview counts before it says
// "at least".
const reachedPageCountMost = 1000

// reachedPagePathsMost is how many of the pages a share reaches a preview
// lists.
const reachedPagePathsMost = 50

func describeSharedPage(page *models.SharedPage) *SharedAgentPageView {
	view := &SharedAgentPageView{
		OwnerUserID: page.OwnerUserID, OwnerUsername: page.OwnerUsername, OwnerName: page.OwnerName,
		OwnerAvatarURL: avatarUrl(page.OwnerUser), OwnerAvatarColorIndex: avatar.InitialColorIndex(page.OwnerUserID),
		Path: page.Path, Name: page.Name, NodeKind: string(page.NodeKind), Aliases: page.Aliases,
		Opening:       page.Opening,
		IsQuoteShared: page.IsQuoteShared,
		Facts:         page.Facts, Edges: page.Edges, Children: page.Children,
	}
	return view
}

func describeSharedRoot(root *models.SharedRoot) *SharedAgentPageRootView {
	return &SharedAgentPageRootView{
		OwnerUserID: root.OwnerUserID, OwnerUsername: root.OwnerUsername, OwnerName: root.OwnerName,
		OwnerAvatarURL: avatarUrl(root.OwnerUser), OwnerAvatarColorIndex: avatar.InitialColorIndex(root.OwnerUserID),
		Path: root.Path, Name: root.Name, NodeKind: string(root.NodeKind),
		Opening: root.Opening, IsQuoteShared: root.IsQuoteShared,
	}
}

func describeAgentPageShare(share *models.AgentPageShare) *AgentPageShareView {
	view := &AgentPageShareView{
		ID: share.ID, Path: share.Path, IsQuoteShared: share.IsQuoteShared, CreatedAt: share.CreatedAt,
		GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName, GranteeCircleName: share.GranteeCircleName,
	}
	if share.GranteeUserID != "" {
		view.GranteeUserID = &share.GranteeUserID
		view.GranteeAvatarURL = avatarUrl(share.GranteeUser)
		view.GranteeAvatarColorIndex = avatar.InitialColorIndex(share.GranteeUserID)
	}
	if share.GranteeCircleID != "" {
		view.GranteeCircleID = &share.GranteeCircleID
	}
	return view
}

func (self *graph) ListAgentPageShares(ctx context.Context) ([]*AgentPageShareView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	tx := self.transaction(ctx)
	shares, err := tx.ListPageSharesBy(principal.User.ID)
	if err != nil {
		return nil, err
	}
	views := make([]*AgentPageShareView, 0, len(shares))
	for _, share := range shares {
		view := describeAgentPageShare(share)
		if node, err := tx.GetAgentNode(share.AgentID, share.Path); err != nil {
			return nil, err
		} else if node != nil {
			view.PageName = node.Name
		}
		views = append(views, view)
	}
	return views, nil
}

type PreviewAgentPageShareArguments struct {
	// The page to share
	Path string `json:"path"`

	// The person to share with, who must share a circle with the caller;
	// leave out when sharing with a circle
	GranteeUserID string `json:"granteeUserId" graphapi:"nullable"`

	// The circle to share with, which the caller must be in; leave out when
	// sharing with a person
	GranteeCircleID string `json:"granteeCircleId" graphapi:"nullable"`

	// Whether the quotes would be shared too; not by default
	IsQuoteShared bool `json:"isQuoteShared" graphapi:"nullable"`
}

func (self *graph) PreviewAgentPageShare(ctx context.Context, arguments PreviewAgentPageShareArguments) (*AgentPageSharePreviewView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	path := models.NormalizePath(arguments.Path)
	granteeUserId, granteeCircleId := strings.TrimSpace(arguments.GranteeUserID), strings.TrimSpace(arguments.GranteeCircleID)
	if path == "" || (granteeUserId == "") == (granteeCircleId == "") {
		return nil, fmt.Errorf("%w: preview one page for one person or one circle", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	// A preview answers for the people the caller could share with and no
	// others, so that asking is not a way to learn who is on this server.
	if granteeUserId != "" {
		isMutual, err := tx.SharesCircle(principal.User.ID, granteeUserId)
		if err != nil {
			return nil, err
		}
		if !isMutual {
			return nil, api.ErrNotFound
		}
	} else {
		circle, err := tx.GetCircle(granteeCircleId)
		if err != nil {
			return nil, err
		}
		if circle == nil || circle.Member(principal.User.ID) == nil {
			return nil, api.ErrNotFound
		}
	}
	candidate := &models.AgentPageShare{
		Path: path, GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId, IsQuoteShared: arguments.IsQuoteShared,
	}
	page, err := agent.PreviewSharedPage(tx, principal.User.ID, candidate, path)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, api.ErrNotFound
	}
	owner, err := tx.GetAgentByUser(principal.User.ID)
	if err != nil {
		return nil, err
	}
	under, err := tx.ListAgentNodesUnder(owner.ID, path, reachedPageCountMost+1)
	if err != nil {
		return nil, err
	}
	reached := 0
	reachedPaths := []string{}
	for _, node := range under {
		if models.PathIsWithin(node.Path, path) {
			reached++
			if len(reachedPaths) < reachedPagePathsMost {
				reachedPaths = append(reachedPaths, node.Path)
			}
		}
	}
	withheldFactCount, err := agent.CountWithheldFacts(tx, principal.User.ID, path)
	if err != nil {
		return nil, err
	}
	preview := &AgentPageSharePreviewView{
		Page: describeSharedPage(page), ReachedPageCount: min(reached, reachedPageCountMost),
		ReachedPagePaths: reachedPaths, WithheldFactCount: withheldFactCount,
	}
	preview.IsReachedPageCountCut = reached > reachedPageCountMost
	return preview, nil
}

func (self *graph) ListSharedAgentPages(ctx context.Context) ([]*SharedAgentPageRootView, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return nil, err
	}
	roots, err := agent.SharedPagesFor(self.transaction(ctx), user.ID)
	if err != nil {
		return nil, err
	}
	views := make([]*SharedAgentPageRootView, 0, len(roots))
	for _, root := range roots {
		views = append(views, describeSharedRoot(root))
	}
	return views, nil
}

type SearchSharedAgentPagesArguments struct {
	// The words to look for, in the names, openings and facts of the shared
	// pages
	Query string `json:"query"`

	// The most hits to give back; 20 by default and at most 50
	First int `json:"first" graphapi:"nullable"`
}

// SharedAgentPageSearchView is what a search of the shared pages found.
type SharedAgentPageSearchView struct {
	Hits []*SharedAgentPageHitView `json:"hits"`

	// More matched than first allows; a search with more words narrows it.
	HasMoreHits bool `json:"hasMoreHits"`
}

// The hits one search of shared pages gives back: by default, and at most.
const (
	defaultSharedSearchHits = 20
	maximumSharedSearchHits = 50
)

func (self *graph) SearchSharedAgentPages(ctx context.Context, arguments SearchSharedAgentPagesArguments) (*SharedAgentPageSearchView, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(arguments.Query)
	if query == "" {
		return nil, fmt.Errorf("%w: say what to look for", api.ErrInvalidArguments)
	}
	first := defaultSharedSearchHits
	if arguments.First > 0 {
		first = min(arguments.First, maximumSharedSearchHits)
	}
	// One more than is given back, so that the search says whether anything
	// was left out. The owners' hits are interleaved before the cut, or an
	// owner with many matches would fill the answer.
	hits, err := agent.SearchSharedPages(self.transaction(ctx), user.ID, query, first+1)
	if err != nil {
		return nil, err
	}
	hits = agent.InterleaveSharedHits(hits)
	view := &SharedAgentPageSearchView{Hits: []*SharedAgentPageHitView{}, HasMoreHits: len(hits) > first}
	if view.HasMoreHits {
		hits = hits[:first]
	}
	for _, hit := range hits {
		view.Hits = append(view.Hits, &SharedAgentPageHitView{
			OwnerUserID: hit.OwnerUserID, OwnerUsername: hit.OwnerUsername, OwnerName: hit.OwnerName,
			Path: hit.Path, Name: hit.Name, Opening: hit.Opening, FactNumber: hit.FactNumber, Text: hit.Text,
		})
	}
	return view, nil
}

type ReadSharedAgentPageArguments struct {
	// The person who shared it
	OwnerUserID string `json:"ownerUserId"`

	// The page: the root they shared or a page under it
	Path string `json:"path"`
}

func (self *graph) ReadSharedAgentPage(ctx context.Context, arguments ReadSharedAgentPageArguments) (*SharedAgentPageView, error) {
	user, err := self.requireCirclePerson(ctx)
	if err != nil {
		return nil, err
	}
	page, err := agent.ReadSharedPage(self.transaction(ctx), user.ID, strings.TrimSpace(arguments.OwnerUserID), arguments.Path)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, api.ErrNotFound
	}
	return describeSharedPage(page), nil
}

type ShareAgentPageArguments struct {
	// The page to share, with everything under it
	Path string `json:"path"`

	// The person to share with, who must share a circle with the caller;
	// leave out when sharing with a circle
	GranteeUserID string `json:"granteeUserId" graphapi:"nullable"`

	// The circle to share with, which the caller must be in; leave out when
	// sharing with a person
	GranteeCircleID string `json:"granteeCircleId" graphapi:"nullable"`

	// Whether the words each fact was read in are shown to the reader; not
	// by default
	IsQuoteShared bool `json:"isQuoteShared" graphapi:"nullable"`
}

func (self *graph) ShareAgentPage(ctx context.Context, arguments ShareAgentPageArguments) (*AgentPageShareView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotLoggedIn
	}
	if _, err := self.requirePermission(ctx, models.PermissionCircleUse); err != nil {
		return nil, err
	}
	granteeUserId, granteeCircleId := strings.TrimSpace(arguments.GranteeUserID), strings.TrimSpace(arguments.GranteeCircleID)
	if (granteeUserId == "") == (granteeCircleId == "") {
		return nil, fmt.Errorf("%w: share with one person or one circle", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	// What the grantee was given before, to tell whether this is more.
	earlier, err := tx.ListPageSharesBy(principal.User.ID)
	if err != nil {
		return nil, err
	}
	saved, err := tx.SharePage(principal.User.ID, &models.AgentPageShare{
		Path: arguments.Path, GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId, IsQuoteShared: arguments.IsQuoteShared,
	})
	if err != nil {
		// A page the caller does not have, a person who shares no circle,
		// one who does not exist, a circle the caller is not in: all of them
		// answer as nothing found, because which it was is not the caller's
		// to learn.
		return nil, translateError(err)
	}
	log.Noticef("%s shared a page of their memory", principal.User.Username)
	view := describeAgentPageShare(saved)
	if node, err := tx.GetAgentNode(saved.AgentID, saved.Path); err != nil {
		return nil, err
	} else if node != nil {
		view.PageName = node.Name
	}
	if isPageShareWider(earlier, saved) {
		subject := view.PageName
		if subject == "" {
			subject = saved.Path
		}
		if err := self.tellAboutShare(ctx, principal.User, models.ShareReceivedNote{
			SharerName: principal.User.DisplayName(), ShareKind: models.ShareKindMemoryPage, ShareSubject: subject,
			ShareScopes: models.PageShareScopes(saved.IsQuoteShared),
		}, granteeUserId, granteeCircleId); err != nil {
			return nil, err
		}
	}
	return view, nil
}

// isPageShareWider says whether the share gives its grantee more than the
// person's shares of the page did before: there was none for them, or the quotes
// were not shared.
func isPageShareWider(earlier []*models.AgentPageShare, saved *models.AgentPageShare) bool {
	for _, share := range earlier {
		if share.Path == saved.Path && share.GranteeUserID == saved.GranteeUserID && share.GranteeCircleID == saved.GranteeCircleID {
			return saved.IsQuoteShared && !share.IsQuoteShared
		}
	}
	return true
}

type UnshareAgentPageArguments struct {
	ShareID string `json:"shareId"`
}

func (self *graph) UnshareAgentPage(ctx context.Context, arguments UnshareAgentPageArguments) (bool, error) {
	principal, err := self.requirePermission(ctx, models.PermissionAgentUse)
	if err != nil {
		return false, err
	}
	if principal.User == nil {
		return false, api.ErrNotLoggedIn
	}
	tx := self.transaction(ctx)
	share, err := tx.GetPageShare(strings.TrimSpace(arguments.ShareID))
	if err != nil {
		return false, err
	}
	// The share is the owner's to take back; anyone else is told there is no
	// such share.
	if share == nil || share.OwnerUserID != principal.User.ID {
		return false, api.ErrNotFound
	}
	if err := tx.DeletePageShare(share.ID); err != nil {
		return false, translateError(err)
	}
	return true, nil
}
