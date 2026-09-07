// Package imap serves mailboxes to mail programs.
//
// The mapping is direct: a mailbox's folders are the IMAP mailbox tree, a
// folder's UIDs and modseq are its own, flags are the item's, and the
// message is read from storage when a client asks for it. Nothing is held in
// memory that another instance would need: a session's view of a folder is
// the list of UIDs it has been told about, and everything else is a query.
//
// Sign-in is one of the mailbox's addresses and an app password of that
// mailbox, over TLS, and nothing else.
package imap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"

	"github.com/op/go-logging"
	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/mx"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/ratelimit"
)

var log = logging.MustGetLogger("imap")

// Settings is what an IMAP listener needs.
type Settings struct {
	Database db.Database
	Storage  storage.Storage

	// TLSConfig is offered through STARTTLS on a plain listener, and is what
	// an implicit-TLS listener was wrapped with. Signing in is refused until
	// the connection is encrypted either way.
	TLSConfig *tls.Config

	// ImplicitTLS says the listener already speaks TLS, as on port 993.
	ImplicitTLS bool

	// MaxSize bounds an APPEND, in bytes; zero is unbounded.
	MaxSize int

	// AuthLimiter bounds how often one address may attempt to sign in.
	AuthLimiter *ratelimit.Registry
}

// Delimiter between a folder and its children in a name.
const delimiter = '/'

// The name IMAP reserves for the inbox, whatever the folder is called.
const inboxName = "INBOX"

// pollInterval is how often an idling session looks for itself, in case the
// notification that would have woken it was lost.
const pollInterval = 30 * time.Second

// Serve answers IMAP on the listener until the context ends.
func Serve(ctx context.Context, listener net.Listener, settings *Settings) error {
	hub := newHub()
	changes, err := settings.Database.ListenFolderChanges(ctx)
	if err != nil {
		// Without notifications every idler polls on its clock, which is
		// slower to notice mail and otherwise the same.
		log.Warningf("cannot listen for folder changes, idling sessions will poll: %s", err)
	} else {
		go hub.run(changes)
	}
	caps := goimap.CapSet{
		goimap.CapIMAP4rev1:   {},
		goimap.CapIMAP4rev2:   {},
		goimap.CapIdle:        {},
		goimap.CapMove:        {},
		goimap.CapUIDPlus:     {},
		goimap.CapNamespace:   {},
		goimap.CapUnselect:    {},
		goimap.CapChildren:    {},
		goimap.CapListStatus:  {},
		goimap.CapESearch:     {},
		goimap.CapSASLIR:      {},
		goimap.CapLiteralPlus: {},
		goimap.CapCondStore:   {},
	}
	server := imapserver.New(&imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			remote := ""
			if address, ok := conn.NetConn().RemoteAddr().(*net.TCPAddr); ok {
				remote = address.IP.String()
			}
			return &session{settings: settings, hub: hub, remote: remote}, nil, nil
		},
		Caps:         caps,
		TLSConfig:    settings.TLSConfig,
		InsecureAuth: false,
		Logger:       logger{},
	})
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	err = server.Serve(listener)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// logger routes the library's complaints to the server's log.
type logger struct{}

func (logger) Printf(format string, arguments ...interface{}) {
	log.Debugf(format, arguments...)
}

// --- waking idlers -----------------------------------------------------------

// hub fans folder changes out to the sessions idling on each folder.
type hub struct {
	mutex   sync.Mutex
	waiting map[string]map[chan struct{}]struct{}
}

func newHub() *hub {
	return &hub{waiting: map[string]map[chan struct{}]struct{}{}}
}

func (self *hub) run(changes <-chan string) {
	for folderId := range changes {
		self.mutex.Lock()
		for wake := range self.waiting[folderId] {
			select {
			case wake <- struct{}{}:
			default:
			}
		}
		self.mutex.Unlock()
	}
}

func (self *hub) subscribe(folderId string) (chan struct{}, func()) {
	wake := make(chan struct{}, 1)
	self.mutex.Lock()
	if self.waiting[folderId] == nil {
		self.waiting[folderId] = map[chan struct{}]struct{}{}
	}
	self.waiting[folderId][wake] = struct{}{}
	self.mutex.Unlock()
	return wake, func() {
		self.mutex.Lock()
		delete(self.waiting[folderId], wake)
		if len(self.waiting[folderId]) == 0 {
			delete(self.waiting, folderId)
		}
		self.mutex.Unlock()
	}
}

// --- session ------------------------------------------------------------------

type session struct {
	settings *Settings
	hub      *hub
	remote   string

	// Set by Login.
	mailbox     *models.Mailbox
	appPassword string
	checkedAt   time.Time

	// Set by Select.
	view *view
}

var _ imapserver.SessionIMAP4rev2 = (*session)(nil)

func (self *session) Close() error {
	self.view = nil
	return nil
}

func (self *session) Login(username, password string) error {
	if self.settings.AuthLimiter != nil && self.remote != "" && !self.settings.AuthLimiter.Allow(self.remote) {
		return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeLimit, Text: "Too many sign-in attempts; try again later"}
	}
	var mailbox *models.Mailbox
	var appPassword *models.MailboxAppPassword
	err := self.settings.Database.Transaction(func(tx db.Transaction) error {
		var err error
		mailbox, appPassword, err = access.AuthenticateAppPasswordWithID(tx, username, password)
		return err
	})
	if err != nil {
		if errors.Is(err, access.ErrInvalidAppPassword) {
			log.Noticef("refused imap sign-in as %q from %s", username, self.remote)
			return imapserver.ErrAuthFailed
		}
		log.Errorf("imap sign-in as %q failed: %s", username, err)
		return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeServerBug, Text: "Cannot sign in right now"}
	}
	self.mailbox = mailbox
	self.appPassword = appPassword.ID
	self.checkedAt = time.Now()
	log.Debugf("imap sign-in as %q into mailbox %q from %s", username, mailbox.ID, self.remote)
	return nil
}

// recheckInterval is how often an open session asks whether it may stay
// open: a revoked app password or a disabled account ends it within this.
const recheckInterval = 5 * time.Minute

// stillAllowed is the check, made inside a transaction every so often.
func (self *session) stillAllowed(tx db.Transaction) error {
	if self.mailbox == nil || time.Since(self.checkedAt) < recheckInterval {
		return nil
	}
	valid, err := access.AppPasswordStillValid(tx, self.appPassword)
	if err != nil {
		return err
	}
	if !valid {
		log.Noticef("imap session on mailbox %q ended: its app password or account is gone", self.mailbox.ID)
		return &goimap.Error{Type: goimap.StatusResponseTypeBye, Code: goimap.ResponseCodeAuthenticationFailed, Text: "This sign-in is no longer valid"}
	}
	self.checkedAt = time.Now()
	return nil
}

// transaction runs a function in a database transaction and turns its
// failure into an IMAP NO, logged once.
func (self *session) transaction(function func(tx db.Transaction) error) error {
	err := self.settings.Database.Transaction(func(tx db.Transaction) error {
		if err := self.stillAllowed(tx); err != nil {
			return err
		}
		return function(tx)
	})
	if err == nil {
		return nil
	}
	var imapError *goimap.Error
	if errors.As(err, &imapError) {
		return err
	}
	log.Errorf("imap operation failed for mailbox %q: %s", self.mailboxId(), err)
	return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeServerBug, Text: "The server could not do that"}
}

func (self *session) mailboxId() string {
	if self.mailbox == nil {
		return ""
	}
	return self.mailbox.ID
}

// --- the folder tree ------------------------------------------------------------

// named is a folder with the name a client knows it by.
type named struct {
	folder *models.MailboxFolder
	name   string
	// children says whether anything is under it.
	children bool
}

// folders is the tree as names, from the database.
func (self *session) folders(tx db.Transaction) ([]*named, error) {
	folders, err := tx.ListFolders(self.mailbox.ID)
	if err != nil {
		return nil, err
	}
	byId := map[string]*models.MailboxFolder{}
	for _, folder := range folders {
		byId[folder.ID] = folder
	}
	var nameOf func(folder *models.MailboxFolder, depth int) string
	nameOf = func(folder *models.MailboxFolder, depth int) string {
		if folder.Kind == models.MailboxFolderKindInbox {
			return inboxName
		}
		if folder.ParentID != "" && depth < 32 {
			if parent := byId[folder.ParentID]; parent != nil {
				return nameOf(parent, depth+1) + string(delimiter) + folder.Name
			}
		}
		return folder.Name
	}
	hasChildren := map[string]bool{}
	for _, folder := range folders {
		if folder.ParentID != "" {
			hasChildren[folder.ParentID] = true
		}
	}
	result := make([]*named, 0, len(folders))
	for _, folder := range folders {
		result = append(result, &named{folder: folder, name: nameOf(folder, 0), children: hasChildren[folder.ID]})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].name < result[right].name })
	return result, nil
}

// folderNamed finds one folder by the name a client used.
func (self *session) folderNamed(tx db.Transaction, name string) (*named, error) {
	folders, err := self.folders(tx)
	if err != nil {
		return nil, err
	}
	for _, candidate := range folders {
		if candidate.name == name || (strings.EqualFold(name, inboxName) && candidate.folder.Kind == models.MailboxFolderKindInbox) {
			return candidate, nil
		}
	}
	return nil, nil
}

func noSuchFolder() error {
	return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeNonExistent, Text: "No such folder"}
}

// specialUse is the attribute a client uses to recognise a system folder.
func specialUse(kind models.MailboxFolderKind) (goimap.MailboxAttr, bool) {
	switch kind {
	case models.MailboxFolderKindSent:
		return goimap.MailboxAttrSent, true
	case models.MailboxFolderKindDrafts:
		return goimap.MailboxAttrDrafts, true
	case models.MailboxFolderKindArchive:
		return goimap.MailboxAttrArchive, true
	case models.MailboxFolderKindJunk:
		return goimap.MailboxAttrJunk, true
	case models.MailboxFolderKindTrash:
		return goimap.MailboxAttrTrash, true
	}
	return "", false
}

func (self *session) listData(entry *named, options *goimap.ListOptions) *goimap.ListData {
	data := &goimap.ListData{Delim: delimiter, Mailbox: entry.name}
	if attribute, ok := specialUse(entry.folder.Kind); ok {
		data.Attrs = append(data.Attrs, attribute)
	}
	if entry.children {
		data.Attrs = append(data.Attrs, goimap.MailboxAttrHasChildren)
	} else {
		data.Attrs = append(data.Attrs, goimap.MailboxAttrHasNoChildren)
	}
	if options != nil && options.ReturnSubscribed {
		// Every folder is subscribed: subscriptions are not kept, because a
		// folder somebody made is a folder they want to see.
		data.Attrs = append(data.Attrs, goimap.MailboxAttrSubscribed)
	}
	if options != nil && options.ReturnStatus != nil {
		data.Status = statusOf(entry, options.ReturnStatus)
	}
	return data
}

func statusOf(entry *named, options *goimap.StatusOptions) *goimap.StatusData {
	folder := entry.folder
	data := &goimap.StatusData{Mailbox: entry.name}
	if options.NumMessages {
		count := uint32(folder.Total)
		data.NumMessages = &count
	}
	if options.NumUnseen {
		unseen := uint32(folder.Unread)
		data.NumUnseen = &unseen
	}
	if options.UIDNext {
		data.UIDNext = goimap.UID(folder.UIDNext)
	}
	if options.UIDValidity {
		data.UIDValidity = uint32(folder.UIDValidity)
	}
	if options.NumRecent {
		var zero uint32
		data.NumRecent = &zero
	}
	data.HighestModSeq = folder.ModSeq
	return data
}

func (self *session) List(writer *imapserver.ListWriter, reference string, patterns []string, options *goimap.ListOptions) error {
	if len(patterns) == 0 {
		return writer.WriteList(&goimap.ListData{Attrs: []goimap.MailboxAttr{goimap.MailboxAttrNoSelect}, Delim: delimiter})
	}
	var entries []*named
	if err := self.transaction(func(tx db.Transaction) error {
		var err error
		entries, err = self.folders(tx)
		return err
	}); err != nil {
		return err
	}
	for _, entry := range entries {
		matched := false
		for _, pattern := range patterns {
			if imapserver.MatchList(entry.name, delimiter, reference, pattern) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if options != nil && options.SelectSpecialUse {
			if _, ok := specialUse(entry.folder.Kind); !ok {
				continue
			}
		}
		if err := writer.WriteList(self.listData(entry, options)); err != nil {
			return err
		}
	}
	return nil
}

func (self *session) Status(name string, options *goimap.StatusOptions) (*goimap.StatusData, error) {
	var entry *named
	if err := self.transaction(func(tx db.Transaction) error {
		var err error
		entry, err = self.folderNamed(tx, name)
		return err
	}); err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, noSuchFolder()
	}
	return statusOf(entry, options), nil
}

func (self *session) Namespace() (*goimap.NamespaceData, error) {
	return &goimap.NamespaceData{
		Personal: []goimap.NamespaceDescriptor{{Prefix: "", Delim: delimiter}},
	}, nil
}

func (self *session) Create(name string, options *goimap.CreateOptions) error {
	name = strings.Trim(name, string(delimiter))
	if name == "" || strings.EqualFold(name, inboxName) {
		return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeCannot, Text: "Cannot create that folder"}
	}
	return self.transaction(func(tx db.Transaction) error {
		existing, err := self.folderNamed(tx, name)
		if err != nil {
			return err
		}
		if existing != nil {
			return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAlreadyExists, Text: "That folder exists"}
		}
		// Parents are made on the way down, as a client that creates
		// "Projects/2026/March" in one command expects.
		parentId := ""
		parts := strings.Split(name, string(delimiter))
		for index, part := range parts {
			path := strings.Join(parts[:index+1], string(delimiter))
			found, err := self.folderNamed(tx, path)
			if err != nil {
				return err
			}
			if found != nil {
				parentId = found.folder.ID
				continue
			}
			created, err := tx.CreateFolder(&models.MailboxFolder{MailboxID: self.mailbox.ID, ParentID: parentId, Name: part})
			if err != nil {
				return err
			}
			parentId = created.ID
		}
		return nil
	})
}

func (self *session) Delete(name string) error {
	return self.transaction(func(tx db.Transaction) error {
		entry, err := self.folderNamed(tx, name)
		if err != nil {
			return err
		}
		if entry == nil {
			return noSuchFolder()
		}
		if entry.folder.Kind != models.MailboxFolderKindCustom {
			return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeCannot, Text: "That folder is part of the mailbox"}
		}
		if self.view != nil && self.view.folder.ID == entry.folder.ID {
			self.view = nil
		}
		return tx.DeleteFolder(entry.folder.ID)
	})
}

func (self *session) Rename(oldName, newName string, options *goimap.RenameOptions) error {
	newName = strings.Trim(newName, string(delimiter))
	if newName == "" || strings.EqualFold(newName, inboxName) {
		return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeCannot, Text: "Cannot use that name"}
	}
	return self.transaction(func(tx db.Transaction) error {
		entry, err := self.folderNamed(tx, oldName)
		if err != nil {
			return err
		}
		if entry == nil {
			return noSuchFolder()
		}
		if entry.folder.Kind != models.MailboxFolderKindCustom {
			return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeCannot, Text: "That folder is part of the mailbox"}
		}
		taken, err := self.folderNamed(tx, newName)
		if err != nil {
			return err
		}
		if taken != nil {
			return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeAlreadyExists, Text: "That folder exists"}
		}
		// The new name's parent has to exist; the last part is the name.
		parentId := ""
		if at := strings.LastIndex(newName, string(delimiter)); at >= 0 {
			parent, err := self.folderNamed(tx, newName[:at])
			if err != nil {
				return err
			}
			if parent == nil {
				return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeNonExistent, Text: "No such parent folder"}
			}
			parentId = parent.folder.ID
			newName = newName[at+1:]
		}
		_, err = tx.UpdateFolder(entry.folder.ID, func(folder *models.MailboxFolder) error {
			folder.Name = newName
			folder.ParentID = parentId
			return nil
		})
		return err
	})
}

// Subscriptions are not kept: every folder is listed. Accepting the
// commands keeps a client that insists on them happy.
func (self *session) Subscribe(name string) error   { return nil }
func (self *session) Unsubscribe(name string) error { return nil }

// --- APPEND ---------------------------------------------------------------------

// Append stores a message a client hands over — its own copy of something it
// sent, or a draft — as a row and an item, so it shows in the web UI too.
func (self *session) Append(name string, reader goimap.LiteralReader, options *goimap.AppendOptions) (*goimap.AppendData, error) {
	limit := int64(self.settings.MaxSize)
	if limit > 0 && reader.Size() > limit {
		return nil, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeLimit, Text: fmt.Sprintf("Messages may be at most %d bytes", limit)}
	}
	raw, err := readAll(reader)
	if err != nil {
		return nil, err
	}
	headers, body, err := splitMessage(raw)
	if err != nil {
		return nil, &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeParse, Text: "That is not a message"}
	}
	var data *goimap.AppendData
	err = self.transaction(func(tx db.Transaction) error {
		entry, err := self.folderNamed(tx, name)
		if err != nil {
			return err
		}
		if entry == nil {
			return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeTryCreate, Text: "No such folder"}
		}
		flags := flagsFromList(options.Flags)
		mail := mailFromMessage(self.mailbox, headers, body, flags, options.Time)
		created, err := tx.CreateMail(mail, nil)
		if err != nil {
			return err
		}
		if err := self.settings.Storage.Put(context.Background(), created.ID, headers, body); err != nil {
			return err
		}
		item, err := tx.AddItem(entry.folder.ID, created.ID, flags)
		if err != nil {
			return err
		}
		if err := tx.SetMailSearch(created.ID, mx.SearchDocument(created)); err != nil {
			return err
		}
		data = &goimap.AppendData{UID: goimap.UID(item.UID), UIDValidity: uint32(entry.folder.UIDValidity)}
		return nil
	})
	return data, err
}

// --- updates ------------------------------------------------------------------

func (self *session) Poll(writer *imapserver.UpdateWriter, allowExpunge bool) error {
	if self.view == nil {
		return nil
	}
	return self.poll(writer, allowExpunge)
}

func (self *session) Idle(writer *imapserver.UpdateWriter, stop <-chan struct{}) error {
	if self.view == nil {
		<-stop
		return nil
	}
	wake, unsubscribe := self.hub.subscribe(self.view.folder.ID)
	defer unsubscribe()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-wake:
		case <-ticker.C:
		}
		if err := self.poll(writer, true); err != nil {
			return err
		}
		if self.view == nil {
			// The folder went away under the session; there is nothing
			// left to watch.
			<-stop
			return nil
		}
	}
}
