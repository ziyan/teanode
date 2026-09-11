// Package datetime is the agent's time arithmetic: now, a conversion
// between zones, a duration added, the difference between two times, a
// phrase such as "next Tuesday 3pm" read in the person's zone.
package datetime

import (
	"context"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name:   "datetime",
				Family: tools.FamilyGeneral,
				Core:   true,
				Risk:   tools.RiskRead,
				Description: "Time arithmetic in the person's zone or another: now, convert between zones, add a duration, the difference between two times, parse a phrase like \"next Tuesday 3pm\". " +
					"The current time is already in the prompt; use this for arithmetic and other zones rather than working them out in your head.",
				Parameters: tools.Object(map[string]any{
					"action":   tools.EnumProperty("what to do", "now", "convert", "add", "diff", "parse"),
					"time":     tools.StringProperty("an ISO 8601 time; the current time when absent"),
					"from":     tools.StringProperty("for diff: the earlier time; for convert: the zone the time is in"),
					"to":       tools.StringProperty("for diff: the later time; for convert: the zone to convert to"),
					"timezone": tools.StringProperty("an IANA zone such as Europe/Berlin; the person's zone when absent"),
					"duration": tools.StringProperty("for add: a duration such as 3d, 2h30m, -1w"),
					"text":     tools.StringProperty("for parse: the phrase, relative to the person's zone"),
				}, "action"),
				Run: runDatetime,
			},
		}
	})
}

type datetimeArguments struct {
	Action   string `json:"action"`
	Time     string `json:"time"`
	From     string `json:"from"`
	To       string `json:"to"`
	Timezone string `json:"timezone"`
	Duration string `json:"duration"`
	Text     string `json:"text"`
}

func runDatetime(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[datetimeArguments](call)
	if err != nil {
		return nil, err
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	location := tools.Location(run.Owner())
	if arguments.Timezone != "" {
		if loaded, err := time.LoadLocation(arguments.Timezone); err == nil {
			location = loaded
		} else {
			return nil, fmt.Errorf("%q is not a time zone", arguments.Timezone)
		}
	}
	describe := func(moment time.Time) map[string]any {
		local := moment.In(location)
		return map[string]any{
			"iso":     local.Format(time.RFC3339),
			"local":   local.Format("Monday, 2 January 2006 15:04"),
			"weekday": local.Weekday().String(),
			"zone":    location.String(),
			"unix":    local.Unix(),
		}
	}
	now := time.Now()
	switch arguments.Action {
	case "", "now":
		return tools.JSONResult(describe(now))
	case "convert":
		moment, err := tools.ParseTime(arguments.Time, location, now)
		if err != nil {
			return nil, err
		}
		if arguments.From != "" {
			from, err := time.LoadLocation(arguments.From)
			if err != nil {
				return nil, fmt.Errorf("%q is not a time zone", arguments.From)
			}
			moment = time.Date(moment.Year(), moment.Month(), moment.Day(), moment.Hour(), moment.Minute(), moment.Second(), 0, from)
		}
		to := location
		if arguments.To != "" {
			if to, err = time.LoadLocation(arguments.To); err != nil {
				return nil, fmt.Errorf("%q is not a time zone", arguments.To)
			}
		}
		location = to
		return tools.JSONResult(describe(moment))
	case "add":
		moment, err := tools.ParseTime(arguments.Time, location, now)
		if err != nil {
			return nil, err
		}
		duration, err := tools.ParseDuration(arguments.Duration)
		if err != nil {
			return nil, err
		}
		return tools.JSONResult(describe(moment.Add(duration)))
	case "diff":
		from, err := tools.ParseTime(arguments.From, location, now)
		if err != nil {
			return nil, err
		}
		to, err := tools.ParseTime(arguments.To, location, now)
		if err != nil {
			return nil, err
		}
		difference := to.Sub(from)
		return tools.JSONResult(map[string]any{"seconds": int64(difference.Seconds()), "words": tools.DurationWords(difference), "from": describe(from), "to": describe(to)})
	case "parse":
		moment, err := tools.ParsePhrase(arguments.Text, location, now)
		if err != nil {
			return nil, err
		}
		return tools.JSONResult(describe(moment))
	}
	return nil, fmt.Errorf("%q is not an action of datetime", arguments.Action)
}
