// Package searcheverything is the agent's search tool: the person's words
// looked for across their chats, mail, contacts, photos, memory, documents,
// recordings, places and finance at once. It calls the same operation as the
// dashboard's search box and teanode search, so it finds what they find and
// is refused what they are refused. See docs/subsystems/search.md.
package searcheverything

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/search"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "search", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskRead,
				Description: "Find the person's words across everything at once: their chats, mail, contacts, photos, memory, documents, recordings, places and finance. A few results from each area, with whether there are more. Use it first when you do not know where something is; each area's own tool (named in the answer) finds more of that area and reads it.",
				Parameters: tools.Object(map[string]any{
					"words":          tools.StringProperty("what to look for: a name, a phrase, a word from the thing"),
					"areas":          tools.ArrayProperty("only these areas; every area the person may use by default", tools.EnumProperty("an area", search.Areas...)),
					"per_area_limit": tools.IntegerProperty("how many results from each area, 5 by default and 20 at most"),
				}, "words"),
				Guidance: "search finds by the words alone, not by meaning, so try the name or a distinctive word of the thing. An area with no results is left out of the answer; an area in failed_areas did not answer, so search it with its own tool. Each result's id is what that area's own tool takes (item_id for mail_read).",
				Run:      run,
			},
		}
	})
}

type arguments struct {
	Words        string   `json:"words"`
	Areas        []string `json:"areas"`
	PerAreaLimit int      `json:"per_area_limit"`
}

// areaTools is the tool that finds more of each area and reads what was found.
var areaTools = map[string]string{
	search.AreaChats:      "conversation",
	search.AreaMail:       "mail_search",
	search.AreaContacts:   "contact_book",
	search.AreaPhotos:     "photo",
	search.AreaMemory:     "memory",
	search.AreaDocuments:  "knowledge",
	search.AreaRecordings: "recording",
	search.AreaPlaces:     "location_places",
	search.AreaFinance:    "finance",
}

const document = `query ($words: String!, $perAreaLimit: Int, $areas: [String!]) {
	SearchEverything(words: $words, perAreaLimit: $perAreaLimit, areas: $areas) {
		words
		failedAreas
		areas { searchArea hasMore results { searchArea targetId titleText detailText happenedAt } }
	}
}`

type found struct {
	SearchEverything struct {
		Words       string   `json:"words"`
		FailedAreas []string `json:"failedAreas"`
		Areas       []struct {
			SearchArea string `json:"searchArea"`
			HasMore    bool   `json:"hasMore"`
			Results    []struct {
				TargetID   string     `json:"targetId"`
				TitleText  string     `json:"titleText"`
				DetailText string     `json:"detailText"`
				HappenedAt *time.Time `json:"happenedAt"`
			} `json:"results"`
		} `json:"areas"`
	} `json:"SearchEverything"`
}

type resultRow struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	Happened string `json:"happened,omitempty"`
}

type areaRow struct {
	Area    string      `json:"area"`
	Results []resultRow `json:"results"`
	HasMore bool        `json:"has_more"`
	Tool    string      `json:"tool"`
}

func run(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	current, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	asked, err := tools.DecodeArguments[arguments](call)
	if err != nil {
		return nil, err
	}
	words := strings.TrimSpace(asked.Words)
	if words == "" {
		return nil, fmt.Errorf("say what to look for")
	}
	variables := map[string]any{"words": words}
	if asked.PerAreaLimit > 0 {
		variables["perAreaLimit"] = asked.PerAreaLimit
	}
	if len(asked.Areas) > 0 {
		variables["areas"] = asked.Areas
	}
	var answer found
	if err := current.Operations().Execute(ctx, document, variables, &answer); err != nil {
		return nil, err
	}

	location := tools.Location(current.Owner())
	rows := make([]areaRow, 0, len(answer.SearchEverything.Areas))
	count := 0
	for _, area := range answer.SearchEverything.Areas {
		row := areaRow{Area: area.SearchArea, HasMore: area.HasMore, Tool: areaTools[area.SearchArea], Results: make([]resultRow, 0, len(area.Results))}
		for _, result := range area.Results {
			entry := resultRow{ID: result.TargetID, Title: result.TitleText, Detail: result.DetailText}
			if result.HappenedAt != nil {
				entry.Happened = result.HappenedAt.In(location).Format("2006-01-02 15:04")
			}
			row.Results = append(row.Results, entry)
			count++
		}
		rows = append(rows, row)
	}
	failed := answer.SearchEverything.FailedAreas
	if failed == nil {
		failed = []string{}
	}
	result, err := tools.JSONResult(map[string]any{"words": answer.SearchEverything.Words, "areas": rows, "failed_areas": failed})
	if err != nil {
		return nil, err
	}
	// What was found is the person's own mail, messages and files, which may
	// say anything.
	result.Untrusted = true
	result.Note = fmt.Sprintf("found %d result(s) in %d area(s)", count, len(rows))
	return result, nil
}
