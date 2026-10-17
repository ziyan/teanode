package memory

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

// Pages other people shared with the person, and sharing the person's own.
//
// What is shared is read where it lives: `get`, `index` and `search` with an
// owner (the sharer's username) read the owner's pages as they are now, as
// far as the share goes, through tools.SharedMemory and so through
// internal/agent/shared_memory.go, the only code that reads another agent's
// graph. Nothing here writes to those pages, and a write that names an owner
// other than the person is refused before it is looked at.
//
// Whatever was read there starts with tools.SharedPageMarker, which is how the
// run that files what the conversation taught tells it from the person's own
// pages and refuses to copy it.

// The actions that read another person's shared pages.
func readsSharedPages(action string) bool {
	return action == "get" || action == "index" || action == "search"
}

// otherOwner is the person whose shared pages the call names, when it names
// somebody other than the person it is for. The person's own username is the
// person's own graph, as no owner at all is.
func otherOwner(run tools.Run, item *memoryItem) (string, bool) {
	owner := strings.TrimSpace(item.Owner)
	if owner == "" {
		return "", false
	}
	if run.Owner() != nil && strings.EqualFold(owner, run.Owner().Username) {
		return "", false
	}
	return owner, true
}

// refuseWritingToAnotherOwner is the refusal of every action that is not a
// read of shared pages when it names an owner who is not the person.
func refuseWritingToAnotherOwner(run tools.Run, item *memoryItem) error {
	switch item.Action {
	case "share", "preview", "unshare", "shares", "shared":
		// These are about the person's own shares and what is shared with
		// them, and have no use for an owner.
		return nil
	}
	if _, isOther := otherOwner(run, item); isOther && !readsSharedPages(item.Action) {
		return fmt.Errorf("pages another person shared are read only: owner is for get, index and search, and %s works on the person's own pages", item.Action)
	}
	return nil
}

// sharedMemoryOf is the run's way of reading shared pages, or the reason it
// has none.
func sharedMemoryOf(run tools.Run) (tools.SharedMemory, error) {
	shared, ok := run.(tools.SharedMemory)
	if !ok || !shared.CanReadSharedPages() {
		return nil, fmt.Errorf("pages other people shared are read only in a one-to-one conversation with the person, not in a chat with others or a background run; tell the person to ask in their own drawer")
	}
	return shared, nil
}

// sharedRootsOf are the roots shared with the person, and, for an owner, only
// his. Not found is worded the same whether the account exists or has shared
// nothing with the person: which it is is not the person's to learn.
func sharedRootsOf(ctx context.Context, shared tools.SharedMemory, owner string) ([]*models.SharedRoot, error) {
	roots, err := shared.SharedPages(ctx)
	if err != nil {
		return nil, err
	}
	if owner == "" {
		return roots, nil
	}
	var kept []*models.SharedRoot
	for _, root := range roots {
		if strings.EqualFold(root.OwnerUsername, owner) {
			kept = append(kept, root)
		}
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("nobody called %q has shared pages with the person; `shared` lists who has", owner)
	}
	return kept, nil
}

// sharedPageResult is a result carrying the shared marker and the words of
// another person's memory, which is data and never instructions.
func sharedPageResult(text string) *tools.Result {
	result := tools.TextResult("%s\n%s", tools.SharedPageMarker, text)
	result.Untrusted = true
	return result
}

// sharedRootLine is one root as a listing shows it.
func sharedRootLine(root *models.SharedRoot, isWithOwner bool) string {
	line := root.Path
	if name := strings.TrimSpace(root.Name); name != "" && !strings.EqualFold(name, models.LastSegment(root.Path)) {
		line += " - " + name
	}
	if root.Opening != "" {
		line += ": " + root.Opening
	}
	if isWithOwner {
		line += " (shared by " + root.OwnerName + ", owner:" + root.OwnerUsername + ")"
	}
	return line
}

// sharedListing is the roots shared with the person, a page of them from the
// offset, with the line that says how many more there are and the call that
// reads on. An owner narrows it to what that person shared.
func sharedListing(ctx context.Context, run tools.Run, item *memoryItem, owner string) (*tools.Result, error) {
	shared, err := sharedMemoryOf(run)
	if err != nil {
		return nil, err
	}
	roots, err := sharedRootsOf(ctx, shared, owner)
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return tools.TextResult("nobody has shared pages with the person"), nil
	}
	limit := item.Limit
	if limit <= 0 {
		limit = indexShown
	}
	offset := max(item.Offset, 0)
	if offset >= len(roots) {
		return tools.TextResult("nothing past line %d; %d pages are shared", offset, len(roots)), nil
	}
	lines := make([]string, 0, min(limit, len(roots)-offset))
	for _, root := range roots[offset:min(offset+limit, len(roots))] {
		lines = append(lines, sharedRootLine(root, owner == ""))
	}
	if next := offset + len(lines); next < len(roots) {
		call := "index again with offset: " + strconv.Itoa(next)
		if owner == "" {
			call = "shared again with offset: " + strconv.Itoa(next)
		} else {
			call += " and owner: " + owner
		}
		lines = append(lines, fmt.Sprintf("\n… %d more; %s", len(roots)-next, call))
	}
	return sharedPageResult(strings.Join(lines, "\n")), nil
}

// sharedIndexAction is the pages one person shared with the person: their
// roots, or with a path the page there and the pages under it.
func sharedIndexAction(ctx context.Context, run tools.Run, item *memoryItem, owner string) (*tools.Result, error) {
	path := models.NormalizePath(item.Path)
	if path == "" {
		return sharedListing(ctx, run, item, owner)
	}
	shared, err := sharedMemoryOf(run)
	if err != nil {
		return nil, err
	}
	roots, err := sharedRootsOf(ctx, shared, owner)
	if err != nil {
		return nil, err
	}
	page, err := shared.ReadSharedPage(ctx, roots[0].OwnerUserID, path)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, fmt.Errorf("%s shared no page at %s with the person; index with owner: %s lists what they did", owner, path, owner)
	}
	lines := []string{sharedPageLine(page)}
	for _, child := range page.Children {
		line := "  " + child.Path
		if name := strings.TrimSpace(child.Name); name != "" && !strings.EqualFold(name, models.LastSegment(child.Path)) {
			line += " - " + name
		}
		if child.Opening != "" {
			line += ": " + child.Opening
		}
		lines = append(lines, line)
	}
	return sharedPageResult(strings.Join(lines, "\n")), nil
}

func sharedPageLine(page *models.SharedPage) string {
	line := page.Path
	if name := strings.TrimSpace(page.Name); name != "" && !strings.EqualFold(name, models.LastSegment(page.Path)) {
		line += " - " + name
	}
	if opening := strings.TrimSpace(page.Opening); opening != "" {
		if index := strings.IndexAny(opening, ".\n"); index > 0 {
			opening = opening[:index]
		}
		line += ": " + strings.TrimSpace(opening)
	}
	return line + " (shared by " + page.OwnerName + ", owner:" + page.OwnerUsername + ")"
}

// sharedGetAction is one page another person shared, as the person is shown
// it: the opening, the facts numbered as the owner numbers them,
// the links to other shared pages, and the pages under it. A page that is not
// shared is not there, and the answer names what is.
func sharedGetAction(ctx context.Context, run tools.Run, item *memoryItem, owner string) (*tools.Result, error) {
	path := models.NormalizePath(item.Path)
	if path == "" {
		return nil, fmt.Errorf("which page? give a path, like people/alice-chen; index with owner: %s lists what they shared", owner)
	}
	shared, err := sharedMemoryOf(run)
	if err != nil {
		return nil, err
	}
	roots, err := sharedRootsOf(ctx, shared, owner)
	if err != nil {
		return nil, err
	}
	page, err := shared.ReadSharedPage(ctx, roots[0].OwnerUserID, path)
	if err != nil {
		return nil, err
	}
	if page == nil {
		// Only what is shared is named: a page beside it is not.
		names := make([]string, 0, len(roots))
		for _, root := range roots {
			names = append(names, root.Path)
		}
		return nil, fmt.Errorf("%s shared no page at %s with the person. What they shared: %s", owner, path, strings.Join(names, ", "))
	}
	if item.LinkOffset > 0 {
		return sharedPageResult(sharedHeading(page) + renderSharedLinks(page, item.LinkOffset)), nil
	}
	return sharedPageResult(renderSharedPage(page, item.From)), nil
}

func sharedHeading(page *models.SharedPage) string {
	var builder strings.Builder
	builder.WriteString(page.Path)
	if page.Name != "" {
		builder.WriteString(" - " + page.Name)
	}
	builder.WriteString(" (" + string(page.NodeKind) + "), shared by " + page.OwnerName + " (owner:" + page.OwnerUsername + "), read only")
	if len(page.Aliases) > 0 {
		builder.WriteString("\nalso called: " + strings.Join(page.Aliases, ", "))
	}
	return builder.String()
}

// renderSharedPage is a shared page as the model reads it.
func renderSharedPage(page *models.SharedPage, from int) string {
	var builder strings.Builder
	builder.WriteString(sharedHeading(page))
	if opening := strings.TrimSpace(page.Opening); opening != "" {
		builder.WriteString("\n\n" + opening)
	}
	builder.WriteString("\n\n" + models.SharedOverviewNotice)
	facts := page.Facts
	more := ""
	if from > 0 {
		var kept []*models.SharedFact
		for _, fact := range facts {
			if fact.Number >= from {
				kept = append(kept, fact)
			}
		}
		facts = kept
	}
	if len(facts) > factsShown {
		next := facts[factsShown].Number
		more = fmt.Sprintf("… %d more facts after #%d; get again with owner: %s and from: %d", len(facts)-factsShown, facts[factsShown-1].Number, page.OwnerUsername, next)
		facts = facts[:factsShown]
	}
	if len(facts) > 0 {
		builder.WriteString("\n")
		for _, fact := range facts {
			builder.WriteString("\n#" + strconv.Itoa(fact.Number) + " " + fact.Line())
			if fact.FactKind != models.FactPlain {
				builder.WriteString(" [" + string(fact.FactKind) + "]")
			}
			if fact.EvidenceSource != "" {
				builder.WriteString("  (" + fact.EvidenceSource + ")")
			}
			if fact.EvidenceQuote != "" {
				builder.WriteString("\n   quote: " + strconv.Quote(fact.EvidenceQuote))
			}
		}
	} else if from > 0 {
		builder.WriteString("\n\n… no facts numbered " + strconv.Itoa(from) + " or above")
	}
	builder.WriteString(renderSharedLinks(page, 0))
	if len(page.Children) > 0 {
		builder.WriteString("\n\nunder it: ")
		names := make([]string, 0, min(len(page.Children), childrenShown))
		for _, child := range page.Children[:min(len(page.Children), childrenShown)] {
			names = append(names, models.LastSegment(child.Path))
		}
		builder.WriteString(strings.Join(names, ", "))
		if moreCount := len(page.Children) - childrenShown; moreCount > 0 {
			fmt.Fprintf(&builder, " … and %d more: index with owner: %s, path %s lists them", moreCount, page.OwnerUsername, page.Path)
		}
	}
	if more != "" {
		builder.WriteString("\n\n" + more)
	}
	if len(page.Facts) > 0 {
		builder.WriteString("\n\ncite a fact of this page as " + sharedCitationExample(page) + "; link it as [name](memory:" + page.Path + "#N@" + page.OwnerUsername + "). Do not write it down: it is " + page.OwnerName + "'s, and stays where it is.")
	}
	return builder.String()
}

func sharedCitationExample(page *models.SharedPage) string {
	return models.SharedReference(page.Path, page.Facts[0].Number, page.OwnerName)
}

// renderSharedLinks is linksShown of a shared page's links from offset, each
// on a line of its own, and the line that says how many more and the get that
// reads them.
func renderSharedLinks(page *models.SharedPage, offset int) string {
	edges := page.Edges
	if len(edges) == 0 {
		return ""
	}
	if offset >= len(edges) {
		return fmt.Sprintf("\n\n… no links past the first %d; the page has %d, and a get without link_offset starts at the first", offset, len(edges))
	}
	var builder strings.Builder
	builder.WriteString("\n")
	if offset > 0 {
		fmt.Fprintf(&builder, "\nlinks %d to %d of %d:", offset+1, min(offset+linksShown, len(edges)), len(edges))
	}
	for _, edge := range edges[offset:min(offset+linksShown, len(edges))] {
		relation, guess := string(edge.Relation), ""
		if edge.IsProposed {
			relation, guess = "perhaps "+relation, " (the agent's guess)"
		}
		if edge.FromPath == page.Path {
			builder.WriteString("\n→ " + relation + " " + edge.ToPath + guess)
		} else {
			builder.WriteString("\n← " + edge.FromPath + " " + relation + " this" + guess)
		}
	}
	if next := offset + linksShown; next < len(edges) {
		fmt.Fprintf(&builder, "\n… %d more links; get again with owner: %s and link_offset: %d", len(edges)-next, page.OwnerUsername, next)
	}
	return builder.String()
}

// sharedHitsText is what a search of shared pages found, each hit marked with
// whose it is.
func sharedHitsText(hits []*models.SharedHit) string {
	lines := make([]string, 0, len(hits))
	for _, hit := range hits {
		if hit.FactNumber > 0 {
			lines = append(lines, models.SharedReference(hit.Path, hit.FactNumber, hit.OwnerName)+" "+hit.Text)
			continue
		}
		line := hit.Path
		if name := strings.TrimSpace(hit.Name); name != "" && !strings.EqualFold(name, models.LastSegment(hit.Path)) {
			line += " - " + name
		}
		if hit.Opening != "" {
			line += ": " + hit.Opening
		}
		lines = append(lines, line+" (shared by "+hit.OwnerName+", owner:"+hit.OwnerUsername+")")
	}
	return strings.Join(lines, "\n")
}

// sharedSearch is the hits of a search of shared pages, those of one owner
// when the call names one, and the line that says when there may be more.
func sharedSearch(ctx context.Context, shared tools.SharedMemory, query, owner string, limit int) (string, error) {
	hits, err := shared.SearchSharedPages(ctx, query, limit)
	if err != nil {
		return "", err
	}
	kept := hits[:0:0]
	pageCount, factCount := 0, 0
	for _, hit := range hits {
		if owner != "" && !strings.EqualFold(hit.OwnerUsername, owner) {
			continue
		}
		kept = append(kept, hit)
		if hit.FactNumber > 0 {
			factCount++
		} else {
			pageCount++
		}
	}
	if len(kept) == 0 {
		return "", nil
	}
	text := sharedHitsText(kept)
	// Each owner's search is cut at the limit, with nothing to page it on,
	// so a list that reached it may not be all there is.
	if pageCount >= limit || factCount >= limit {
		text += fmt.Sprintf("\n\n… there may be more shared matches than these; search again with owner: <username> and limit: %d to look further", limit*2)
	}
	return text, nil
}

// searchSharedAction is a search of only what one person shared.
func searchSharedAction(ctx context.Context, run tools.Run, item *memoryItem, owner string) (*tools.Result, error) {
	query := strings.TrimSpace(item.Query)
	if query == "" {
		query = strings.TrimSpace(item.Text)
	}
	if query == "" {
		return nil, fmt.Errorf("search what? give some words")
	}
	shared, err := sharedMemoryOf(run)
	if err != nil {
		return nil, err
	}
	if _, err := sharedRootsOf(ctx, shared, owner); err != nil {
		return nil, err
	}
	limit := item.Limit
	if limit <= 0 {
		limit = searchLimit
	}
	text, err := sharedSearch(ctx, shared, query, owner, limit)
	if err != nil {
		return nil, err
	}
	if text == "" {
		return tools.TextResult("nothing about that among what %s shared", owner), nil
	}
	return sharedPageResult(text), nil
}

// withSharedSearch adds, after the person's own results, what a search finds
// in the pages other people shared with them, each marked with its owner. A
// run that may not read shared pages adds nothing, and a failure to search
// them is said rather than hidden.
func withSharedSearch(ctx context.Context, run tools.Run, item *memoryItem, own *tools.Result) *tools.Result {
	shared, ok := run.(tools.SharedMemory)
	if !ok || !shared.CanReadSharedPages() || item.Offset > 0 {
		return own
	}
	query := strings.TrimSpace(item.Query)
	if query == "" {
		query = strings.TrimSpace(item.Text)
	}
	limit := item.Limit
	if limit <= 0 {
		limit = searchLimit
	}
	text, err := sharedSearch(ctx, shared, query, "", limit)
	if err != nil {
		own.Content += "\n\n(the pages other people shared could not be searched: " + err.Error() + ")"
		return own
	}
	if text == "" {
		return own
	}
	section := "Shared with the person by others (read only):\n" + text
	if own.Content == "nothing about that" {
		own.Content = tools.SharedPageMarker + "\n" + section
	} else {
		own.Content += "\n\n" + tools.SharedPageMarker + "\n" + section
	}
	own.Untrusted = true
	return own
}

// --- sharing the person's own pages -----------------------------------

// shareAction shares one of the person's pages, and everything under it, with
// a person they share a circle with or a circle they are in, read only. It
// goes through the same operation the dashboard and the command line do.
func shareAction(ctx context.Context, run tools.Run, item *memoryItem) (*tools.Result, error) {
	path := models.NormalizePath(item.Path)
	if path == "" {
		return nil, fmt.Errorf("which page? give a path, like people/alice-chen")
	}
	grantee, err := tools.ResolveGrantee(ctx, run.Operations(), item.With)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{"path": path, "isQuoteShared": item.IsQuoteShared, "granteeUserId": nil, "granteeCircleId": nil}
	if grantee.UserID != "" {
		variables["granteeUserId"] = grantee.UserID
	} else {
		variables["granteeCircleId"] = grantee.CircleID
	}
	var result struct {
		ShareAgentPage struct {
			ID            string `json:"id"`
			IsQuoteShared bool   `json:"isQuoteShared"`
		} `json:"ShareAgentPage"`
	}
	if err := run.Operations().Execute(ctx,
		`mutation ($path: String!, $granteeUserId: String, $granteeCircleId: String, $isQuoteShared: Boolean) {
		   ShareAgentPage(path: $path, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, isQuoteShared: $isQuoteShared) { id isQuoteShared }
		 }`, variables, &result); err != nil {
		return nil, err
	}
	answer, err := tools.JSONResult(map[string]any{
		"path": path, "with": grantee.Description, "is_quote_shared": result.ShareAgentPage.IsQuoteShared, "share_id": result.ShareAgentPage.ID,
	})
	if err != nil {
		return nil, err
	}
	answer.Note = "shared " + path + " with " + grantee.Description
	return answer, nil
}

// shareCard is the card the person answers before a page is shared. It says
// the page goes with everything under it, and whether the words each fact was
// read in go too, since those are quoted from their mail, chat and documents.
func shareCard(page, with string, isQuoteShared bool) string {
	quotes := "without the quotes each fact was read from"
	if isQuoteShared {
		quotes = "with the quotes each fact was read from"
	}
	return "Share " + page + " and everything under it with " + with + ", read only, " + quotes
}

// previewFactsShown is the most facts a preview lists.
const previewFactsShown = 20

// previewShareAction shows the person what sharing a page would show, through
// the operation the dashboard and the command line use for it, so that the
// person decides knowing how far the share reaches. Nothing is shared.
func previewShareAction(ctx context.Context, run tools.Run, item *memoryItem) (*tools.Result, error) {
	path := models.NormalizePath(item.Path)
	if path == "" {
		return nil, fmt.Errorf("which page? give a path, like people/alice-chen")
	}
	grantee, err := tools.ResolveGrantee(ctx, run.Operations(), item.With)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{"path": path, "isQuoteShared": item.IsQuoteShared, "granteeUserId": nil, "granteeCircleId": nil}
	if grantee.UserID != "" {
		variables["granteeUserId"] = grantee.UserID
	} else {
		variables["granteeCircleId"] = grantee.CircleID
	}
	var result struct {
		PreviewAgentPageShare struct {
			Page struct {
				Path     string `json:"path"`
				Name     string `json:"name"`
				Opening  string `json:"opening"`
				NodeKind string `json:"nodeKind"`
				Facts    []struct {
					Number        int    `json:"number"`
					Text          string `json:"text"`
					EvidenceQuote string `json:"evidenceQuote"`
				} `json:"facts"`
				Edges    []struct{} `json:"edges"`
				Children []struct {
					Path string `json:"path"`
				} `json:"children"`
			} `json:"page"`
			ReachedPageCount      int  `json:"reachedPageCount"`
			IsReachedPageCountCut bool `json:"isReachedPageCountCut"`
		} `json:"PreviewAgentPageShare"`
	}
	if err := run.Operations().Execute(ctx,
		`query ($path: String!, $granteeUserId: String, $granteeCircleId: String, $isQuoteShared: Boolean) {
		   PreviewAgentPageShare(path: $path, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, isQuoteShared: $isQuoteShared) {
		     page { path name nodeKind opening facts { number text evidenceQuote } edges { relation } children { path } }
		     reachedPageCount isReachedPageCountCut
		   }
		 }`, variables, &result); err != nil {
		return nil, err
	}
	preview := result.PreviewAgentPageShare
	facts := make([]map[string]any, 0, previewFactsShown)
	for _, fact := range preview.Page.Facts {
		if len(facts) == previewFactsShown {
			break
		}
		row := map[string]any{"number": fact.Number, "text": fact.Text}
		if fact.EvidenceQuote != "" {
			row["quote"] = fact.EvidenceQuote
		}
		facts = append(facts, row)
	}
	answer := map[string]any{
		"path": path, "with": grantee.Description, "is_quote_shared": item.IsQuoteShared,
		"reached_page_count": preview.ReachedPageCount, "is_reached_page_count_cut": preview.IsReachedPageCountCut,
		"page": map[string]any{
			"name": preview.Page.Name, "opening": preview.Page.Opening, "fact_count": len(preview.Page.Facts), "facts": facts,
			"link_count": len(preview.Page.Edges), "pages_directly_under_it": len(preview.Page.Children),
		},
	}
	if len(preview.Page.Facts) > previewFactsShown {
		answer["more_facts"] = fmt.Sprintf("%d more facts on the page are shown too", len(preview.Page.Facts)-previewFactsShown)
	}
	return tools.JSONResult(answer)
}

// sharesAction lists the shares the person made, each with who it is for.
func sharesAction(ctx context.Context, run tools.Run) (*tools.Result, error) {
	var result struct {
		ListAgentPageShares []struct {
			ID                string `json:"id"`
			Path              string `json:"path"`
			PageName          string `json:"pageName"`
			GranteeUsername   string `json:"granteeUsername"`
			GranteeCircleName string `json:"granteeCircleName"`
			IsQuoteShared     bool   `json:"isQuoteShared"`
		} `json:"ListAgentPageShares"`
	}
	if err := run.Operations().Execute(ctx, `query { ListAgentPageShares { id path pageName granteeUsername granteeCircleName isQuoteShared } }`, nil, &result); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(result.ListAgentPageShares))
	for _, share := range result.ListAgentPageShares {
		with := share.GranteeUsername
		if share.GranteeCircleName != "" {
			with = models.DescribeCircleByName(share.GranteeCircleName)
		}
		rows = append(rows, map[string]any{"share_id": share.ID, "path": share.Path, "page": share.PageName, "with": with, "is_quote_shared": share.IsQuoteShared})
	}
	return tools.JSONResult(map[string]any{"shares": rows})
}

// unshareAction takes a share back.
func unshareAction(ctx context.Context, run tools.Run, item *memoryItem) (*tools.Result, error) {
	shareId := strings.TrimSpace(item.ShareID)
	if shareId == "" {
		return nil, fmt.Errorf("which share? give share_id, from `shares`")
	}
	if err := run.Operations().Execute(ctx, `mutation ($shareId: String!) { UnshareAgentPage(shareId: $shareId) }`,
		map[string]any{"shareId": shareId}, &struct{}{}); err != nil {
		return nil, err
	}
	answer, err := tools.JSONResult(map[string]any{"unshared": shareId})
	if err != nil {
		return nil, err
	}
	answer.Note = "took a share back"
	return answer, nil
}
