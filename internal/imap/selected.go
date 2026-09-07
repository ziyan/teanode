package imap

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"sort"
	"strings"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	gomessage "github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-message/textproto"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// view is what a session knows about the folder it has selected: the UIDs it
// has been told exist, in order, which is what its sequence numbers count.
// Nothing else is cached; every command asks the database.
type view struct {
	folder   *models.MailboxFolder
	uids     []uint64
	modseq   uint64
	readOnly bool

	// searchRes is the last SEARCH's result, for the "$" marker.
	searchRes goimap.UIDSet
}

func (self *view) seqOf(uid uint64) uint32 {
	index := sort.Search(len(self.uids), func(candidate int) bool { return self.uids[candidate] >= uid })
	if index < len(self.uids) && self.uids[index] == uid {
		return uint32(index + 1)
	}
	return 0
}

func (self *view) uidAt(seq uint32) (uint64, bool) {
	if seq == 0 || int(seq) > len(self.uids) {
		return 0, false
	}
	return self.uids[seq-1], true
}

// resolve turns a number set — sequence numbers or UIDs, with "*" — into the
// UIDs it names that this session knows, in sequence order.
func (self *view) resolve(numSet goimap.NumSet) []uint64 {
	if goimap.IsSearchRes(numSet) {
		numSet = self.searchRes
	}
	var uids []uint64
	switch set := numSet.(type) {
	case goimap.SeqSet:
		max := uint32(len(self.uids))
		for _, span := range set {
			start, stop := span.Start, span.Stop
			if start == 0 {
				start = max
			}
			if stop == 0 {
				stop = max
			}
			if start > stop {
				start, stop = stop, start
			}
			for seq := start; seq <= stop && seq > 0; seq++ {
				if uid, ok := self.uidAt(seq); ok {
					uids = append(uids, uid)
				}
				if seq == ^uint32(0) {
					break
				}
			}
		}
	case goimap.UIDSet:
		var max uint64
		if len(self.uids) > 0 {
			max = self.uids[len(self.uids)-1]
		}
		for _, span := range set {
			start, stop := uint64(span.Start), uint64(span.Stop)
			if start == 0 {
				start = max
			}
			if stop == 0 {
				stop = max
			}
			if start > stop {
				start, stop = stop, start
			}
			// Walk the known UIDs rather than the range: a range of "1:*"
			// over a folder with a large UID next is not a loop of that size.
			from := sort.Search(len(self.uids), func(candidate int) bool { return self.uids[candidate] >= start })
			for index := from; index < len(self.uids) && self.uids[index] <= stop; index++ {
				uids = append(uids, self.uids[index])
			}
		}
	}
	sort.Slice(uids, func(left, right int) bool { return uids[left] < uids[right] })
	// Dedupe, as overlapping ranges are legal.
	deduped := uids[:0]
	for index, uid := range uids {
		if index == 0 || uid != uids[index-1] {
			deduped = append(deduped, uid)
		}
	}
	return deduped
}

// --- selecting ----------------------------------------------------------------

func (self *session) loadView(tx db.Transaction, folder *models.MailboxFolder) (*view, error) {
	items, err := tx.ListItems(folder.ID, &db.ItemOptions{Ascending: true})
	if err != nil {
		return nil, err
	}
	uids := make([]uint64, 0, len(items))
	for _, item := range items {
		uids = append(uids, item.UID)
	}
	fresh, err := tx.GetFolder(folder.ID)
	if err != nil {
		return nil, err
	}
	if fresh == nil {
		return nil, noSuchFolder()
	}
	return &view{folder: fresh, uids: uids, modseq: fresh.ModSeq}, nil
}

func (self *session) Select(name string, options *goimap.SelectOptions) (*goimap.SelectData, error) {
	var selected *view
	var firstUnseen uint32
	err := self.transaction(func(tx db.Transaction) error {
		entry, err := self.folderNamed(tx, name)
		if err != nil {
			return err
		}
		if entry == nil {
			return noSuchFolder()
		}
		selected, err = self.loadView(tx, entry.folder)
		if err != nil {
			return err
		}
		unseen := true
		first, err := tx.ListItems(entry.folder.ID, &db.ItemOptions{Unseen: &unseen, Ascending: true, Limit: 1})
		if err != nil {
			return err
		}
		if len(first) > 0 {
			firstUnseen = selected.seqOf(first[0].UID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	selected.readOnly = options != nil && options.ReadOnly
	self.view = selected
	flags := []goimap.Flag{goimap.FlagSeen, goimap.FlagAnswered, goimap.FlagFlagged, goimap.FlagDeleted, goimap.FlagDraft, goimap.FlagForwarded}
	return &goimap.SelectData{
		Flags:             flags,
		PermanentFlags:    flags,
		NumMessages:       uint32(len(selected.uids)),
		FirstUnseenSeqNum: firstUnseen,
		UIDNext:           goimap.UID(selected.folder.UIDNext),
		UIDValidity:       uint32(selected.folder.UIDValidity),
		HighestModSeq:     selected.folder.ModSeq,
	}, nil
}

func (self *session) Unselect() error {
	self.view = nil
	return nil
}

func (self *session) requireSelected() (*view, error) {
	if self.view == nil {
		return nil, &goimap.Error{Type: goimap.StatusResponseTypeBad, Text: "No folder selected"}
	}
	return self.view, nil
}

// --- messages -----------------------------------------------------------------

// itemsFor reads the items named, in UID order.
func (self *session) itemsFor(tx db.Transaction, current *view, uids []uint64) ([]*models.MailboxItem, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	return tx.ListItems(current.folder.ID, &db.ItemOptions{UIDs: uids, Ascending: true})
}

// message is one stored message as bytes, read when a fetch needs it.
func (self *session) message(mailId string) ([]byte, error) {
	headers, body, err := self.settings.Storage.Get(context.Background(), mailId)
	if err != nil {
		return nil, err
	}
	return joinMessage(headers, body), nil
}

func joinMessage(headers []string, body []byte) []byte {
	var buffer bytes.Buffer
	for _, header := range headers {
		buffer.WriteString(strings.TrimRight(header, "\r\n"))
		buffer.WriteString("\r\n")
	}
	buffer.WriteString("\r\n")
	buffer.Write(body)
	return buffer.Bytes()
}

func splitMessage(raw []byte) ([]string, []byte, error) {
	return mailparse.Split(bytes.NewReader(raw))
}

func readAll(reader goimap.LiteralReader) ([]byte, error) {
	return io.ReadAll(reader)
}

// mailFromMessage is the row for a message a client appended: what the
// headers say about it, under the mailbox's own domain.
func mailFromMessage(mailbox *models.Mailbox, headers []string, body []byte, flags models.MailboxItemFlags, at time.Time) *models.Mail {
	from, _ := mailparse.ParseAddress(mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(headers, "From")))
	if at.IsZero() {
		at = time.Now()
	}
	kind := models.MailKindOutgoing
	if flags.Draft != nil && *flags.Draft {
		kind = models.MailKindDraft
	}
	var recipients []string
	for _, name := range []string{"To", "Cc"} {
		if list, err := mail.ParseAddressList(mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(headers, name))); err == nil {
			for _, address := range list {
				recipients = append(recipients, address.Address)
			}
		}
	}
	stored := &models.Mail{
		Sender:     from,
		From:       from,
		Recipients: recipients,
		Subject:    mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(headers, "Subject")),
		MessageID:  mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(headers, "Message-ID")),
		Headers:    headers,
		Body:       body,
		Size:       uint64(len(body)),
		Status:     models.MailStatusAccepted,
		ReceivedAt: at,
		Kind:       kind,
	}
	if len(mailbox.Addresses) > 0 {
		stored.DomainID = mailbox.Addresses[0].DomainID
	}
	return stored
}

// --- flags --------------------------------------------------------------------

func flagList(item *models.MailboxItem) []goimap.Flag {
	var flags []goimap.Flag
	if item.Seen {
		flags = append(flags, goimap.FlagSeen)
	}
	if item.Answered {
		flags = append(flags, goimap.FlagAnswered)
	}
	if item.Flagged {
		flags = append(flags, goimap.FlagFlagged)
	}
	if item.Deleted {
		flags = append(flags, goimap.FlagDeleted)
	}
	if item.Draft {
		flags = append(flags, goimap.FlagDraft)
	}
	if item.Forwarded {
		flags = append(flags, goimap.FlagForwarded)
	}
	return flags
}

// flagsFromList is every flag the server keeps, set as the list says: the
// shape of a STORE FLAGS or an APPEND.
func flagsFromList(list []goimap.Flag) models.MailboxItemFlags {
	seen, answered, flagged, deleted, draft, forwarded := false, false, false, false, false, false
	for _, flag := range list {
		switch canonical(flag) {
		case canonical(goimap.FlagSeen):
			seen = true
		case canonical(goimap.FlagAnswered):
			answered = true
		case canonical(goimap.FlagFlagged):
			flagged = true
		case canonical(goimap.FlagDeleted):
			deleted = true
		case canonical(goimap.FlagDraft):
			draft = true
		case canonical(goimap.FlagForwarded):
			forwarded = true
		}
	}
	return models.MailboxItemFlags{Seen: &seen, Answered: &answered, Flagged: &flagged, Deleted: &deleted, Draft: &draft, Forwarded: &forwarded}
}

// flagsChange is only the flags named, set to value: the shape of +FLAGS and
// -FLAGS.
func flagsChange(list []goimap.Flag, value bool) models.MailboxItemFlags {
	var flags models.MailboxItemFlags
	for _, flag := range list {
		on := value
		switch canonical(flag) {
		case canonical(goimap.FlagSeen):
			flags.Seen = &on
		case canonical(goimap.FlagAnswered):
			flags.Answered = &on
		case canonical(goimap.FlagFlagged):
			flags.Flagged = &on
		case canonical(goimap.FlagDeleted):
			flags.Deleted = &on
		case canonical(goimap.FlagDraft):
			flags.Draft = &on
		case canonical(goimap.FlagForwarded):
			flags.Forwarded = &on
		}
	}
	return flags
}

func canonical(flag goimap.Flag) goimap.Flag {
	return goimap.Flag(strings.ToLower(string(flag)))
}

// --- FETCH --------------------------------------------------------------------

func (self *session) Fetch(writer *imapserver.FetchWriter, numSet goimap.NumSet, options *goimap.FetchOptions) error {
	current, err := self.requireSelected()
	if err != nil {
		return err
	}
	uids := current.resolve(numSet)
	var items []*models.MailboxItem
	if err := self.transaction(func(tx db.Transaction) error {
		var err error
		items, err = self.itemsFor(tx, current, uids)
		if err != nil {
			return err
		}
		if options.ChangedSince > 0 {
			kept := items[:0]
			for _, item := range items {
				if item.ModSeq > options.ChangedSince {
					kept = append(kept, item)
				}
			}
			items = kept
		}
		return nil
	}); err != nil {
		return err
	}

	// Reading the body marks the message seen, unless the client peeked.
	markSeen := false
	for _, section := range options.BodySection {
		if !section.Peek {
			markSeen = true
		}
	}
	needsBytes := options.Envelope || options.BodyStructure != nil || len(options.BodySection) > 0 ||
		len(options.BinarySection) > 0 || len(options.BinarySectionSize) > 0
	var toMark []string
	for _, item := range items {
		if markSeen && !item.Seen && !current.readOnly {
			item.Seen = true
			toMark = append(toMark, item.ID)
		}
		var raw []byte
		if needsBytes {
			raw, err = self.message(item.MailID)
			if err != nil {
				log.Warningf("cannot read message %q for fetch: %s", item.MailID, err)
				raw = joinMessage(nil, nil)
			}
		}
		response := writer.CreateMessage(current.seqOf(item.UID))
		if err := writeMessage(response, item, raw, options); err != nil {
			return err
		}
	}
	if len(toMark) > 0 {
		yes := true
		if err := self.transaction(func(tx db.Transaction) error {
			_, err := tx.SetItemFlags(toMark, models.MailboxItemFlags{Seen: &yes})
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func writeMessage(writer *imapserver.FetchResponseWriter, item *models.MailboxItem, raw []byte, options *goimap.FetchOptions) error {
	writer.WriteUID(goimap.UID(item.UID))
	if options.Flags {
		writer.WriteFlags(flagList(item))
	}
	if options.InternalDate {
		writer.WriteInternalDate(item.AddedAt)
	}
	if options.RFC822Size {
		size := int64(len(raw))
		if raw == nil && item.Mail != nil {
			size = int64(item.Mail.Size)
		}
		writer.WriteRFC822Size(size)
	}
	if options.Envelope {
		header, err := textproto.ReadHeader(bufio.NewReader(bytes.NewReader(raw)))
		if err == nil {
			writer.WriteEnvelope(imapserver.ExtractEnvelope(header))
		} else {
			writer.WriteEnvelope(&goimap.Envelope{})
		}
	}
	if options.BodyStructure != nil {
		writer.WriteBodyStructure(imapserver.ExtractBodyStructure(bytes.NewReader(raw)))
	}
	for _, section := range options.BodySection {
		content := imapserver.ExtractBodySection(bytes.NewReader(raw), section)
		part := writer.WriteBodySection(section, int64(len(content)))
		_, writeErr := part.Write(content)
		closeErr := part.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	for _, section := range options.BinarySection {
		content := imapserver.ExtractBinarySection(bytes.NewReader(raw), section)
		part := writer.WriteBinarySection(section, int64(len(content)))
		_, writeErr := part.Write(content)
		closeErr := part.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	for _, section := range options.BinarySectionSize {
		writer.WriteBinarySectionSize(section, imapserver.ExtractBinarySectionSize(bytes.NewReader(raw), section))
	}
	return writer.Close()
}

// --- STORE --------------------------------------------------------------------

func (self *session) Store(writer *imapserver.FetchWriter, numSet goimap.NumSet, flags *goimap.StoreFlags, options *goimap.StoreOptions) error {
	current, err := self.requireSelected()
	if err != nil {
		return err
	}
	if current.readOnly {
		return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeNoPerm, Text: "The folder was opened read-only"}
	}
	uids := current.resolve(numSet)
	var changed []*models.MailboxItem
	if err := self.transaction(func(tx db.Transaction) error {
		items, err := self.itemsFor(tx, current, uids)
		if err != nil {
			return err
		}
		var ids []string
		for _, item := range items {
			if options != nil && options.UnchangedSince > 0 && item.ModSeq > options.UnchangedSince {
				continue
			}
			ids = append(ids, item.ID)
		}
		if len(ids) == 0 {
			return nil
		}
		var change models.MailboxItemFlags
		switch flags.Op {
		case goimap.StoreFlagsSet:
			change = flagsFromList(flags.Flags)
		case goimap.StoreFlagsAdd:
			change = flagsChange(flags.Flags, true)
		case goimap.StoreFlagsDel:
			change = flagsChange(flags.Flags, false)
		}
		if _, err := tx.SetItemFlags(ids, change); err != nil {
			return err
		}
		changed, err = self.itemsFor(tx, current, uids)
		return err
	}); err != nil {
		return err
	}
	if flags.Silent {
		return nil
	}
	for _, item := range changed {
		response := writer.CreateMessage(current.seqOf(item.UID))
		response.WriteUID(goimap.UID(item.UID))
		response.WriteFlags(flagList(item))
		if err := response.Close(); err != nil {
			return err
		}
	}
	return nil
}

// --- EXPUNGE ------------------------------------------------------------------

func (self *session) Expunge(writer *imapserver.ExpungeWriter, uids *goimap.UIDSet) error {
	current, err := self.requireSelected()
	if err != nil {
		return err
	}
	if current.readOnly {
		return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeNoPerm, Text: "The folder was opened read-only"}
	}
	var removed []uint64
	if err := self.transaction(func(tx db.Transaction) error {
		deleted := true
		items, err := tx.ListItems(current.folder.ID, &db.ItemOptions{Deleted: &deleted, Ascending: true})
		if err != nil {
			return err
		}
		var ids []string
		for _, item := range items {
			if uids != nil && !uids.Contains(goimap.UID(item.UID)) {
				continue
			}
			ids = append(ids, item.ID)
			removed = append(removed, item.UID)
		}
		if len(ids) == 0 {
			return nil
		}
		_, err = tx.DeleteItems(ids)
		return err
	}); err != nil {
		return err
	}
	return self.dropFromView(removed, writer.WriteExpunge)
}

// dropFromView forgets UIDs the folder no longer holds, telling the client
// their sequence numbers from the highest down, which is the order in which
// the numbers that remain stay right.
func (self *session) dropFromView(removed []uint64, write func(seqNum uint32) error) error {
	current := self.view
	if current == nil || len(removed) == 0 {
		return nil
	}
	sort.Slice(removed, func(left, right int) bool { return removed[left] > removed[right] })
	for _, uid := range removed {
		seq := current.seqOf(uid)
		if seq == 0 {
			continue
		}
		current.uids = append(current.uids[:seq-1], current.uids[seq:]...)
		if write != nil {
			if err := write(seq); err != nil {
				return err
			}
		}
	}
	return nil
}

// --- COPY and MOVE --------------------------------------------------------------

func (self *session) Copy(numSet goimap.NumSet, destination string) (*goimap.CopyData, error) {
	current, err := self.requireSelected()
	if err != nil {
		return nil, err
	}
	uids := current.resolve(numSet)
	data := &goimap.CopyData{}
	err = self.transaction(func(tx db.Transaction) error {
		target, err := self.folderNamed(tx, destination)
		if err != nil {
			return err
		}
		if target == nil {
			return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeTryCreate, Text: "No such folder"}
		}
		items, err := self.itemsFor(tx, current, uids)
		if err != nil {
			return err
		}
		data.UIDValidity = uint32(target.folder.UIDValidity)
		for _, item := range items {
			created, err := tx.AddItem(target.folder.ID, item.MailID, flagsOf(item))
			if err != nil {
				return err
			}
			data.SourceUIDs.AddNum(goimap.UID(item.UID))
			data.DestUIDs.AddNum(goimap.UID(created.UID))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (self *session) Move(writer *imapserver.MoveWriter, numSet goimap.NumSet, destination string) error {
	current, err := self.requireSelected()
	if err != nil {
		return err
	}
	if current.readOnly {
		return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeNoPerm, Text: "The folder was opened read-only"}
	}
	uids := current.resolve(numSet)
	data := &goimap.CopyData{}
	var removed []uint64
	err = self.transaction(func(tx db.Transaction) error {
		target, err := self.folderNamed(tx, destination)
		if err != nil {
			return err
		}
		if target == nil {
			return &goimap.Error{Type: goimap.StatusResponseTypeNo, Code: goimap.ResponseCodeTryCreate, Text: "No such folder"}
		}
		if target.folder.ID == current.folder.ID {
			return &goimap.Error{Type: goimap.StatusResponseTypeNo, Text: "Source and destination are the same folder"}
		}
		items, err := self.itemsFor(tx, current, uids)
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		moved, err := tx.MoveItems(ids, target.folder.ID)
		if err != nil {
			return err
		}
		data.UIDValidity = uint32(target.folder.UIDValidity)
		for index, item := range items {
			data.SourceUIDs.AddNum(goimap.UID(item.UID))
			if index < len(moved) {
				data.DestUIDs.AddNum(goimap.UID(moved[index].UID))
			}
			removed = append(removed, item.UID)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := writer.WriteCopyData(data); err != nil {
		return err
	}
	return self.dropFromView(removed, writer.WriteExpunge)
}

func flagsOf(item *models.MailboxItem) models.MailboxItemFlags {
	seen, answered, flagged, deleted, draft, forwarded := item.Seen, item.Answered, item.Flagged, item.Deleted, item.Draft, item.Forwarded
	return models.MailboxItemFlags{Seen: &seen, Answered: &answered, Flagged: &flagged, Deleted: &deleted, Draft: &draft, Forwarded: &forwarded}
}

// --- SEARCH ---------------------------------------------------------------------

func (self *session) Search(kind imapserver.NumKind, criteria *goimap.SearchCriteria, options *goimap.SearchOptions) (*goimap.SearchData, error) {
	current, err := self.requireSelected()
	if err != nil {
		return nil, err
	}
	var items []*models.MailboxItem
	if err := self.transaction(func(tx db.Transaction) error {
		var err error
		items, err = tx.ListItems(current.folder.ID, &db.ItemOptions{Ascending: true})
		return err
	}); err != nil {
		return nil, err
	}
	needsBytes := criteriaNeedBytes(criteria)
	data := &goimap.SearchData{}
	var seqSet goimap.SeqSet
	var uidSet goimap.UIDSet
	for _, item := range items {
		seq := current.seqOf(item.UID)
		if seq == 0 {
			continue
		}
		var raw []byte
		if needsBytes {
			raw, err = self.message(item.MailID)
			if err != nil {
				continue
			}
		}
		if !matches(current, item, seq, raw, criteria) {
			continue
		}
		uidSet.AddNum(goimap.UID(item.UID))
		var number uint32
		switch kind {
		case imapserver.NumKindSeq:
			seqSet.AddNum(seq)
			number = seq
		case imapserver.NumKindUID:
			number = uint32(item.UID)
		}
		if data.Min == 0 || number < data.Min {
			data.Min = number
		}
		if number > data.Max {
			data.Max = number
		}
		data.Count++
		if item.ModSeq > data.ModSeq {
			data.ModSeq = item.ModSeq
		}
	}
	current.searchRes = uidSet
	switch kind {
	case imapserver.NumKindSeq:
		data.All = seqSet
	case imapserver.NumKindUID:
		data.All = uidSet
	}
	return data, nil
}

func criteriaNeedBytes(criteria *goimap.SearchCriteria) bool {
	if criteria == nil {
		return false
	}
	if len(criteria.Header) > 0 || len(criteria.Body) > 0 || len(criteria.Text) > 0 || !criteria.SentSince.IsZero() || !criteria.SentBefore.IsZero() {
		return true
	}
	for _, not := range criteria.Not {
		if criteriaNeedBytes(&not) {
			return true
		}
	}
	for _, or := range criteria.Or {
		if criteriaNeedBytes(&or[0]) || criteriaNeedBytes(&or[1]) {
			return true
		}
	}
	return false
}

func matches(current *view, item *models.MailboxItem, seq uint32, raw []byte, criteria *goimap.SearchCriteria) bool {
	for _, set := range criteria.SeqNum {
		if !set.Contains(seq) {
			return false
		}
	}
	for _, set := range criteria.UID {
		if !set.Contains(goimap.UID(item.UID)) {
			return false
		}
	}
	if !matchDate(item.AddedAt, criteria.Since, criteria.Before) {
		return false
	}
	have := map[goimap.Flag]bool{}
	for _, flag := range flagList(item) {
		have[canonical(flag)] = true
	}
	for _, flag := range criteria.Flag {
		if !have[canonical(flag)] {
			return false
		}
	}
	for _, flag := range criteria.NotFlag {
		if have[canonical(flag)] {
			return false
		}
	}
	size := int64(len(raw))
	if raw == nil && item.Mail != nil {
		size = int64(item.Mail.Size)
	}
	if criteria.Larger != 0 && size <= criteria.Larger {
		return false
	}
	if criteria.Smaller != 0 && size >= criteria.Smaller {
		return false
	}
	if raw != nil {
		entity, _ := gomessage.Read(bytes.NewReader(raw))
		if entity == nil {
			entity, _ = gomessage.New(gomessage.Header{}, bytes.NewReader(nil))
		}
		header := mail.Header{Header: entity.Header}
		for _, field := range criteria.Header {
			if !matchHeaderFields(header.FieldsByKey(field.Key), field.Value) {
				return false
			}
		}
		if !criteria.SentSince.IsZero() || !criteria.SentBefore.IsZero() {
			sent, err := header.Date()
			if err != nil || !matchDate(sent, criteria.SentSince, criteria.SentBefore) {
				return false
			}
		}
		for _, text := range criteria.Text {
			entity, _ := gomessage.Read(bytes.NewReader(raw))
			if entity == nil || !matchEntity(entity, text, true) {
				return false
			}
		}
		for _, body := range criteria.Body {
			entity, _ := gomessage.Read(bytes.NewReader(raw))
			if entity == nil || !matchEntity(entity, body, false) {
				return false
			}
		}
	}
	for _, not := range criteria.Not {
		if matches(current, item, seq, raw, &not) {
			return false
		}
	}
	for _, or := range criteria.Or {
		if !matches(current, item, seq, raw, &or[0]) && !matches(current, item, seq, raw, &or[1]) {
			return false
		}
	}
	return true
}

func matchDate(when, since, before time.Time) bool {
	// RFC 3501 compares dates without their zones.
	when = time.Date(when.Year(), when.Month(), when.Day(), 0, 0, 0, 0, time.UTC)
	if !since.IsZero() && when.Before(since) {
		return false
	}
	if !before.IsZero() && !when.Before(before) {
		return false
	}
	return true
}

func matchHeaderFields(fields gomessage.HeaderFields, pattern string) bool {
	if pattern == "" {
		return fields.Len() > 0
	}
	pattern = strings.ToLower(pattern)
	for fields.Next() {
		value, _ := fields.Text()
		if strings.Contains(strings.ToLower(value), pattern) {
			return true
		}
	}
	return false
}

func matchEntity(entity *gomessage.Entity, pattern string, includeHeader bool) bool {
	if pattern == "" {
		return true
	}
	if includeHeader && matchHeaderFields(entity.Header.Fields(), pattern) {
		return true
	}
	if multipart := entity.MultipartReader(); multipart != nil {
		for {
			part, err := multipart.NextPart()
			if err == io.EOF {
				break
			} else if err != nil {
				return false
			}
			if matchEntity(part, pattern, includeHeader) {
				return true
			}
		}
		return false
	}
	contentType, _, err := entity.Header.ContentType()
	if err != nil {
		return false
	}
	if !strings.HasPrefix(contentType, "text/") && !strings.HasPrefix(contentType, "message/") {
		return false
	}
	content, err := io.ReadAll(entity.Body)
	if err != nil {
		return false
	}
	return bytes.Contains(bytes.ToLower(content), bytes.ToLower([]byte(pattern)))
}

// --- updates ---------------------------------------------------------------------

// poll tells the client what changed in the selected folder since it last
// asked: messages that arrived, messages that left, flags that moved.
func (self *session) poll(writer *imapserver.UpdateWriter, allowExpunge bool) error {
	current := self.view
	var fresh *models.MailboxFolder
	var items []*models.MailboxItem
	var changedFlags []*models.MailboxItem
	if err := self.transaction(func(tx db.Transaction) error {
		var err error
		fresh, err = tx.GetFolder(current.folder.ID)
		if err != nil {
			return err
		}
		if fresh == nil {
			return nil
		}
		if fresh.ModSeq == current.modseq {
			return nil
		}
		items, err = tx.ListItems(current.folder.ID, &db.ItemOptions{Ascending: true})
		if err != nil {
			return err
		}
		changedFlags, err = tx.ListItems(current.folder.ID, &db.ItemOptions{SinceModSeq: current.modseq, Ascending: true})
		return err
	}); err != nil {
		return err
	}
	if fresh == nil {
		// The folder is gone from under the session.
		self.view = nil
		return nil
	}
	if fresh.ModSeq == current.modseq {
		return nil
	}
	known := map[uint64]bool{}
	for _, uid := range current.uids {
		known[uid] = true
	}
	present := map[uint64]bool{}
	var added []uint64
	for _, item := range items {
		present[item.UID] = true
		if !known[item.UID] {
			added = append(added, item.UID)
		}
	}
	var removed []uint64
	for _, uid := range current.uids {
		if !present[uid] {
			removed = append(removed, uid)
		}
	}
	if allowExpunge {
		if err := self.dropFromView(removed, writer.WriteExpunge); err != nil {
			return err
		}
	}
	if len(added) > 0 {
		current.uids = append(current.uids, added...)
		sort.Slice(current.uids, func(left, right int) bool { return current.uids[left] < current.uids[right] })
		if err := writer.WriteNumMessages(uint32(len(current.uids))); err != nil {
			return err
		}
	}
	for _, item := range changedFlags {
		if !known[item.UID] {
			continue
		}
		seq := current.seqOf(item.UID)
		if seq == 0 {
			continue
		}
		if err := writer.WriteMessageFlags(seq, goimap.UID(item.UID), flagList(item)); err != nil {
			return err
		}
	}
	// Removals not yet told stay in the view, and so does the modseq they
	// happened at: the next poll that may expunge will find them again.
	if allowExpunge || len(removed) == 0 {
		current.modseq = fresh.ModSeq
	}
	current.folder = fresh
	return nil
}
