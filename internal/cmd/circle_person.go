package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// personCardNotSharedWords, personCardNeedsContactWords and
// personCardFailedWords say why a part of a card shows nothing. A part the
// signed-in account may not use is left out without a word, as the dashboard
// and the app leave it out.
const (
	personCardNotSharedWords    = "not shared with you"
	personCardNeedsContactWords = "no contact of yours is linked to them; teanode contact link <contact-id> <username> links one"
	personCardFailedWords       = "could not be read just now"
)

func runCirclePerson(ctx context.Context, command *cli.Command) error {
	name, contact := strings.TrimSpace(command.Args().First()), strings.TrimSpace(command.String("contact"))
	if command.Args().Len() > 1 || (name == "") == (contact == "") {
		return usage("whose card? usage: teanode circle person <username|user-id>, or teanode circle person --contact <address-book-id>/<contact-id>")
	}
	var addressBookId, contactId string
	if contact != "" {
		var isSplit bool
		if addressBookId, contactId, isSplit = strings.Cut(contact, "/"); !isSplit || addressBookId == "" || contactId == "" {
			return usage("--contact is the address book and the contact, <address-book-id>/<contact-id>, as teanode contact list shows them")
		}
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	userId := ""
	if name != "" {
		if userId, err = client.FindCirclePersonID(ctx, connection, name); err != nil {
			return describeNotFound(command, err, "person "+name)
		}
	}
	card, err := client.ReadPersonCard(ctx, connection, userId, addressBookId, contactId, int(command.Int("limit")))
	if err != nil {
		return describeNotFound(command, err, "person "+firstNonEmptyText(name, contact))
	}
	if command.Bool("json") {
		return PrintJSON(card)
	}
	return printPersonCard(card)
}

// firstNonEmptyText is the first of the texts that says something.
func firstNonEmptyText(texts ...string) string {
	for _, text := range texts {
		if strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

// printPersonCard writes a card as the app shows it: who they are, then each
// part with a few items, the way the other circle commands print theirs.
func printPersonCard(card *client.PersonCard) error {
	fields := [][2]string{}
	if person := card.Person; person != nil {
		heading := firstNonEmptyText(person.Name, person.Username)
		if person.Name != "" {
			heading += " (" + person.Username + ")"
		}
		if person.RelationshipWord != "" {
			heading += ", your " + person.RelationshipWord
		}
		fields = append(fields, [2]string{"person", heading}, [2]string{"user id", person.UserID})
		circles := make([]string, 0, len(card.SharedCircles))
		for _, circle := range card.SharedCircles {
			circles = append(circles, fmt.Sprintf("%s (%s)", circle.CircleName, circle.CircleKind))
		}
		fields = append(fields, [2]string{"circles", strings.Join(circles, ", ")}, [2]string{"can message", yesNo(card.CanMessage)})
	}
	if contact := card.Contact; contact != nil {
		fields = append(fields, [2]string{"contact", firstNonEmptyText(contact.Name, contact.Organization)})
		if contact.Organization != "" && contact.Name != "" {
			fields = append(fields, [2]string{"works at", contact.Organization})
		}
		if len(contact.Emails) > 0 {
			fields = append(fields, [2]string{"email", strings.Join(contact.Emails, ", ")})
		}
		if len(contact.Phones) > 0 {
			fields = append(fields, [2]string{"phone", strings.Join(contact.Phones, ", ")})
		}
		for _, address := range contact.Addresses {
			fields = append(fields, [2]string{"address", address.Written})
		}
		if contact.Note != "" {
			fields = append(fields, [2]string{"note", contact.Note})
		}
	}
	if err := printFields(fields); err != nil {
		return err
	}
	for _, print := range []func(*client.PersonCard) error{
		printPersonCardLocation, printPersonCardHealth, printPersonCardFinance, printPersonCardPhotos,
		printPersonCardMail, printPersonCardRecordings, printPersonCardMemory,
	} {
		if err := print(card); err != nil {
			return err
		}
	}
	return nil
}

// personCardSectionHeading starts a part of a card, and says why there is
// nothing under it when that is so. It reports whether there are items to
// print.
func personCardSectionHeading(heading string, sectionState client.PersonCardSectionState) bool {
	switch sectionState {
	case client.PersonCardSectionStateShown:
		fmt.Printf("\n%s\n", heading)
		return true
	case client.PersonCardSectionStateNotShared:
		fmt.Printf("\n%s: %s\n", heading, personCardNotSharedWords)
	case client.PersonCardSectionStateNeedsContact:
		fmt.Printf("\n%s: %s\n", heading, personCardNeedsContactWords)
	case client.PersonCardSectionStateFailed:
		fmt.Printf("\n%s: %s\n", heading, personCardFailedWords)
	}
	return false
}

func personCardMore(hasMore bool, noun string) {
	if hasMore {
		fmt.Printf("  and more %s\n", noun)
	}
}

func printPersonCardLocation(card *client.PersonCard) error {
	section := card.Location
	if section == nil || !personCardSectionHeading("location", section.SectionState) {
		return nil
	}
	fields := [][2]string{}
	if section.Location != nil {
		fields = append(fields, [2]string{"shared", section.Location.ShareLevel})
		if pause := section.Location.Pause; pause != nil {
			fields = append(fields, [2]string{"recording", "Location paused " + pauseEndWords(pause.EndedAt, time.Now())})
		}
		if point := section.Location.LatestPoint; point != nil {
			fields = append(fields, [2]string{"position", fmt.Sprintf("%.5f, %.5f (within %.0f m), %s ago",
				point.LatitudeDegrees, point.LongitudeDegrees, point.AccuracyMeters, timeWords(time.Since(point.MeasuredAt)))})
		}
		if visit := section.Location.OpenVisit; visit != nil {
			since := ""
			if visit.ArrivedAt != nil {
				since = fmt.Sprintf(", since %s", sinceText(*visit.ArrivedAt))
			}
			fields = append(fields, [2]string{"at", placeLine(visit.Place) + since})
		}
	}
	if err := printFields(fields); err != nil {
		return err
	}
	if len(section.TodayVisits) == 0 {
		return nil
	}
	fmt.Println("visits today")
	rows := make([][]string, 0, len(section.TodayVisits))
	for _, visit := range section.TodayVisits {
		left := timeCell(visit.DepartedAt)
		if visit.DepartedAt == nil {
			left = "still there"
		}
		rows = append(rows, []string{timeCell(visit.ArrivedAt), left, placeLine(visit.Place)})
	}
	if err := printTable([]string{"arrived", "left", "place"}, rows); err != nil {
		return err
	}
	personCardMore(section.HasMoreTodayVisits, "visits; teanode location visits --person <username> lists them")
	return nil
}

func printPersonCardHealth(card *client.PersonCard) error {
	section := card.Health
	if section == nil || !personCardSectionHeading("health", section.SectionState) {
		return nil
	}
	rows := make([][]string, 0, len(section.HealthTypes))
	for _, each := range section.HealthTypes {
		rows = append(rows, []string{each.TypeName, healthLatestWords(each)})
	}
	if err := printTable([]string{"kind", "latest day"}, rows); err != nil {
		return err
	}
	personCardMore(section.HasMore, "kinds; teanode health types --person <username> lists them")
	return nil
}

func printPersonCardFinance(card *client.PersonCard) error {
	section := card.Finance
	if section == nil || !personCardSectionHeading("finance", section.SectionState) {
		return nil
	}
	rows := make([][]string, 0, len(section.FinanceAccounts))
	for _, each := range section.FinanceAccounts {
		used := ""
		if each.CreditUsage != nil {
			used = usageShareWords(each.CreditUsage.UsageShare)
		}
		rows = append(rows, []string{financeAccountWords(each.AccountName, each.AccountMask), money(each.CurrentBalance, each.CurrencyCode), used, each.FinanceShareLevel})
	}
	if err := printTable([]string{"account", "balance", "used", "shared at"}, rows); err != nil {
		return err
	}
	personCardMore(section.HasMore, "accounts; teanode finance accounts --person <username> lists them")
	return nil
}

func printPersonCardPhotos(card *client.PersonCard) error {
	section := card.Photos
	if section == nil {
		return nil
	}
	if personCardSectionHeading("photos of them", section.PhotosState) {
		rows := make([][]string, 0, len(section.Photos))
		for _, photo := range section.Photos {
			takenAt := photo.CreatedAt
			if photo.TakenAt != nil {
				takenAt = *photo.TakenAt
			}
			rows = append(rows, []string{photo.ID, timeCell(&takenAt), photo.PlaceName})
		}
		if err := printTable([]string{"photo id", "taken", "place"}, rows); err != nil {
			return err
		}
		personCardMore(section.HasMorePhotos, "photos; teanode photo list --person <username> lists them")
	}
	if personCardSectionHeading("albums they shared with you", section.AlbumsState) {
		rows := make([][]string, 0, len(section.Albums))
		for _, album := range section.Albums {
			rows = append(rows, []string{album.ID, forTerminal(album.AlbumTitle), fmt.Sprint(album.PhotoCount)})
		}
		if err := printTable([]string{"album id", "title", "photos"}, rows); err != nil {
			return err
		}
		personCardMore(section.HasMoreAlbums, "albums; teanode photo album list lists them")
	}
	return nil
}

func printPersonCardMail(card *client.PersonCard) error {
	section := card.Mail
	if section == nil || !personCardSectionHeading("mail with them", section.SectionState) {
		return nil
	}
	rows := make([][]string, 0, len(section.Mails))
	for _, mail := range section.Mails {
		direction := "to them"
		if mail.IsFromPerson {
			direction = "from them"
		}
		subject := forTerminal(mail.Subject)
		if mail.IsUnread {
			subject += " (unread)"
		}
		rows = append(rows, []string{timeCell(&mail.ReceivedAt), direction, subject, mail.MailboxItemID})
	}
	if err := printTable([]string{"received", "", "subject", "mail id"}, rows); err != nil {
		return err
	}
	personCardMore(section.HasMore, "mail")
	return nil
}

func printPersonCardRecordings(card *client.PersonCard) error {
	section := card.Recordings
	if section == nil || !personCardSectionHeading("recordings they speak in", section.SectionState) {
		return nil
	}
	rows := make([][]string, 0, len(section.Recordings))
	for _, recording := range section.Recordings {
		rows = append(rows, []string{timeCell(recording.RecordedAt), forTerminal(recording.Title),
			timeWords(time.Duration(recording.DurationMS) * time.Millisecond), countWords(recording.SpokenPassageCount, "passage"), recording.RecordingID})
	}
	if err := printTable([]string{"recorded", "title", "length", "they speak", "recording id"}, rows); err != nil {
		return err
	}
	personCardMore(section.HasMore, "recordings; teanode recording list lists them")
	return nil
}

func printPersonCardMemory(card *client.PersonCard) error {
	section := card.Memory
	if section == nil || !personCardSectionHeading("memory", section.SectionState) {
		return nil
	}
	rows := make([][]string, 0, len(section.Pages))
	for _, page := range section.Pages {
		kind := page.NodeKind
		if page.IsMatchedByName {
			kind += " (by name)"
		}
		rows = append(rows, []string{page.NodePath, kind, forTerminal(page.Summary)})
	}
	if err := printTable([]string{"page", "kind", "summary"}, rows); err != nil {
		return err
	}
	personCardMore(section.HasMore, "pages")
	return nil
}

// sinceText is when a visit began: the time on the day it began, with the
// date before it on any other day.
func sinceText(arrivedAt time.Time) string {
	local, now := arrivedAt.In(time.Local), time.Now().In(time.Local)
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return local.Format("15:04")
	}
	return local.Format("Jan 2 15:04")
}
