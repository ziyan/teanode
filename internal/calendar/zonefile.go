package calendar

import (
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// Reading a zone the machine has never heard of.
//
// A file names its zone with a TZID and carries a VTIMEZONE describing it. The
// library resolves the name with time.LoadLocation and ignores the description
// entirely -- which works for "Europe/Berlin" and fails for everything
// Microsoft writes, because Exchange and Outlook name zones their own way:
// "W. Europe Standard Time", "Pacific Standard Time". Those are the commonest
// invitations there are.
//
// Failing there was quiet and bad: the moment came back as the zero time, the
// event was stored starting in the year one, and it then appeared in no window
// anybody ever asked about.
//
// The fix is to make the file say something the library can read, once, before
// anything else looks at it -- rather than to work around the library at every
// place a time is read. The first attempt did the latter: it expanded
// recurrences in wall clock and put the offset back one occurrence at a time,
// which cost a scan of the zone per step, got the offset wrong for the few
// days a year the rules disagree with the literal dates, and left EXDATE and
// UNTIL -- both written as real instants -- comparing against wall clock, so a
// cancelled occurrence came back and the last of a series went missing.
//
// So: a Windows name becomes the IANA name it means, everywhere in the file.
// Everything downstream is then the ordinary path, and daylight saving,
// exceptions and end dates are the library's business again.

// windowsZones maps the names Windows uses to the ones everybody else does.
//
// From the CLDR mapping, limited to the zones that actually turn up in mail.
// A name not here falls back to the description in the file.
var windowsZones = map[string]string{
	"afghanistan standard time":       "Asia/Kabul",
	"alaskan standard time":           "America/Anchorage",
	"arab standard time":              "Asia/Riyadh",
	"arabian standard time":           "Asia/Dubai",
	"arabic standard time":            "Asia/Baghdad",
	"argentina standard time":         "America/Argentina/Buenos_Aires",
	"atlantic standard time":          "America/Halifax",
	"aus central standard time":       "Australia/Darwin",
	"aus eastern standard time":       "Australia/Sydney",
	"azores standard time":            "Atlantic/Azores",
	"bangladesh standard time":        "Asia/Dhaka",
	"canada central standard time":    "America/Regina",
	"cape verde standard time":        "Atlantic/Cape_Verde",
	"caucasus standard time":          "Asia/Yerevan",
	"cen. australia standard time":    "Australia/Adelaide",
	"central america standard time":   "America/Guatemala",
	"central asia standard time":      "Asia/Almaty",
	"central brazilian standard time": "America/Cuiaba",
	"central europe standard time":    "Europe/Budapest",
	"central european standard time":  "Europe/Warsaw",
	"central pacific standard time":   "Pacific/Guadalcanal",
	"central standard time":           "America/Chicago",
	"central standard time (mexico)":  "America/Mexico_City",
	"china standard time":             "Asia/Shanghai",
	"e. africa standard time":         "Africa/Nairobi",
	"e. australia standard time":      "Australia/Brisbane",
	"e. europe standard time":         "Europe/Chisinau",
	"e. south america standard time":  "America/Sao_Paulo",
	"eastern standard time":           "America/New_York",
	"egypt standard time":             "Africa/Cairo",
	"fiji standard time":              "Pacific/Fiji",
	"fle standard time":               "Europe/Kiev",
	"georgian standard time":          "Asia/Tbilisi",
	"gmt standard time":               "Europe/London",
	"greenwich standard time":         "Atlantic/Reykjavik",
	"gtb standard time":               "Europe/Bucharest",
	"hawaiian standard time":          "Pacific/Honolulu",
	"india standard time":             "Asia/Kolkata",
	"iran standard time":              "Asia/Tehran",
	"israel standard time":            "Asia/Jerusalem",
	"jordan standard time":            "Asia/Amman",
	"korea standard time":             "Asia/Seoul",
	"middle east standard time":       "Asia/Beirut",
	"morocco standard time":           "Africa/Casablanca",
	"mountain standard time":          "America/Denver",
	"mountain standard time (mexico)": "America/Mazatlan",
	"myanmar standard time":           "Asia/Yangon",
	"n. central asia standard time":   "Asia/Novosibirsk",
	"nepal standard time":             "Asia/Kathmandu",
	"new zealand standard time":       "Pacific/Auckland",
	"newfoundland standard time":      "America/St_Johns",
	"north asia east standard time":   "Asia/Irkutsk",
	"north asia standard time":        "Asia/Krasnoyarsk",
	"pacific sa standard time":        "America/Santiago",
	"pacific standard time":           "America/Los_Angeles",
	"pacific standard time (mexico)":  "America/Tijuana",
	"pakistan standard time":          "Asia/Karachi",
	"romance standard time":           "Europe/Paris",
	"russian standard time":           "Europe/Moscow",
	"sa eastern standard time":        "America/Cayenne",
	"sa pacific standard time":        "America/Bogota",
	"sa western standard time":        "America/La_Paz",
	"se asia standard time":           "Asia/Bangkok",
	"singapore standard time":         "Asia/Singapore",
	"south africa standard time":      "Africa/Johannesburg",
	"sri lanka standard time":         "Asia/Colombo",
	"taipei standard time":            "Asia/Taipei",
	"tasmania standard time":          "Australia/Hobart",
	"tokyo standard time":             "Asia/Tokyo",
	"tonga standard time":             "Pacific/Tongatapu",
	"turkey standard time":            "Europe/Istanbul",
	"us eastern standard time":        "America/Indiana/Indianapolis",
	"us mountain standard time":       "America/Phoenix",
	"utc":                             "UTC",
	"venezuela standard time":         "America/Caracas",
	"vladivostok standard time":       "Asia/Vladivostok",
	"w. australia standard time":      "Australia/Perth",
	"w. central africa standard time": "Africa/Lagos",
	"w. europe standard time":         "Europe/Berlin",
	"west asia standard time":         "Asia/Tashkent",
	"west pacific standard time":      "Pacific/Port_Moresby",
	"yakutsk standard time":           "Asia/Yakutsk",
}

// knownAs is the name this machine knows a zone by, or empty.
//
// A file may also prefix a real name with a publisher and a version, the way
// some calendar programs do -- a leading slash, two segments of their own, and
// then the zone -- which is the same zone said at greater length.
func knownAs(tzid string) string {
	name := strings.TrimSpace(tzid)
	if name == "" {
		return ""
	}
	if _, err := time.LoadLocation(name); err == nil {
		return name
	}
	if mapped, found := windowsZones[strings.ToLower(name)]; found {
		if _, err := time.LoadLocation(mapped); err == nil {
			return mapped
		}
	}
	if at := strings.LastIndex(name, "/"); at > 0 {
		// The last two segments of a prefixed name are the zone itself.
		parts := strings.Split(name, "/")
		if len(parts) >= 2 {
			tail := parts[len(parts)-2] + "/" + parts[len(parts)-1]
			if _, err := time.LoadLocation(tail); err == nil {
				return tail
			}
		}
	}
	return ""
}

// settleZones makes a file say something the library can read.
//
// A zone whose name this machine knows by another spelling is renamed to it,
// everywhere -- the VTIMEZONE and every property that points at it. A zone
// nobody can name at all has its times rewritten as the instants they are,
// using the offsets the file itself gives, so that everything afterwards --
// the start, the end, the exceptions, the end of a series -- is talking about
// the same thing.
func settleZones(cal *ical.Calendar) {
	if cal == nil {
		return
	}
	renames := map[string]string{}
	unnameable := map[string]*ical.Component{}
	for _, child := range cal.Children {
		if child.Name != ical.CompTimezone {
			continue
		}
		property := child.Props.Get(ical.PropTimezoneID)
		if property == nil {
			continue
		}
		tzid := strings.TrimSpace(property.Value)
		if tzid == "" {
			continue
		}
		if known := knownAs(tzid); known != "" {
			if known != tzid {
				renames[tzid] = known
				property.Value = known
			}
			continue
		}
		unnameable[tzid] = child
	}
	// A property may also name a zone the file never describes.
	for _, component := range cal.Children {
		for name := range component.Props {
			for index := range component.Props[name] {
				property := &component.Props[name][index]
				tzid := strings.TrimSpace(property.Params.Get(ical.ParamTimezoneID))
				if tzid == "" {
					continue
				}
				if renamed, found := renames[tzid]; found {
					property.Params.Set(ical.ParamTimezoneID, renamed)
					continue
				}
				if _, described := unnameable[tzid]; described {
					continue
				}
				if known := knownAs(tzid); known != "" && known != tzid {
					property.Params.Set(ical.ParamTimezoneID, known)
				}
			}
		}
	}
	// What is left is a zone nobody can name. Its times become instants.
	for tzid, zone := range unnameable {
		asInstants(cal, tzid, zone)
	}
}

// asInstants rewrites every time anchored to a zone that cannot be named into
// the moment it stands for, taken from the offsets the file gives.
//
// Written as instants rather than left as wall-clock times so that the start,
// the exceptions and the end of a series are all the same kind of thing. The
// cost is that the offset is the one in force at the event's start: a series
// in an unnameable zone does not follow that zone through a change. Nobody can
// say which zone it is, so there is nothing better to follow -- and it is far
// better than the event not existing.
func asInstants(cal *ical.Calendar, tzid string, zone *ical.Component) {
	for _, component := range cal.Children {
		if component.Name == ical.CompTimezone {
			continue
		}
		for name := range component.Props {
			for index := range component.Props[name] {
				property := &component.Props[name][index]
				if !strings.EqualFold(strings.TrimSpace(property.Params.Get(ical.ParamTimezoneID)), tzid) {
					continue
				}
				value := strings.TrimSpace(property.Value)
				local, err := time.ParseInLocation("20060102T150405", value, time.UTC)
				if err != nil {
					continue
				}
				offset, found := offsetIn(zone, local)
				if !found {
					continue
				}
				property.Params.Del(ical.ParamTimezoneID)
				property.Value = local.Add(-time.Duration(offset) * time.Second).Format("20060102T150405Z")
			}
		}
	}
}

// offsetIn is the offset an observance gives at a moment, in seconds.
//
// The observance in force is the one whose own start is the latest that is not
// after the moment. The literal dates are used rather than the yearly rules
// beside them, which is right to within the few days a year they disagree --
// and this is only reached for a zone nobody can name at all, where the
// alternative is no event.
func offsetIn(zone *ical.Component, at time.Time) (int, bool) {
	best, bestAt, found := 0, time.Time{}, false
	for _, observance := range zone.Children {
		if observance.Name != ical.CompTimezoneStandard && observance.Name != ical.CompTimezoneDaylight {
			continue
		}
		offset, ok := offsetOf(observance.Props.Get(ical.PropTimezoneOffsetTo))
		if !ok {
			continue
		}
		start := observance.Props.Get(ical.PropDateTimeStart)
		if start == nil {
			continue
		}
		began, err := time.ParseInLocation("20060102T150405", strings.TrimSpace(start.Value), time.UTC)
		if err != nil {
			continue
		}
		began = time.Date(at.Year(), began.Month(), began.Day(),
			began.Hour(), began.Minute(), began.Second(), 0, time.UTC)
		if began.After(at) {
			began = began.AddDate(-1, 0, 0)
		}
		if !found || began.After(bestAt) {
			best, bestAt, found = offset, began, true
		}
	}
	return best, found
}

// offsetOf reads "+0100" or "-053000" as seconds.
func offsetOf(property *ical.Prop) (int, bool) {
	if property == nil {
		return 0, false
	}
	value := strings.TrimSpace(property.Value)
	if len(value) < 5 {
		return 0, false
	}
	sign := 1
	switch value[0] {
	case '+':
	case '-':
		sign = -1
	default:
		return 0, false
	}
	digits := value[1:]
	for _, letter := range digits {
		if letter < '0' || letter > '9' {
			return 0, false
		}
	}
	if len(digits) != 4 && len(digits) != 6 {
		return 0, false
	}
	hours := int(digits[0]-'0')*10 + int(digits[1]-'0')
	minutes := int(digits[2]-'0')*10 + int(digits[3]-'0')
	seconds := 0
	if len(digits) == 6 {
		seconds = int(digits[4]-'0')*10 + int(digits[5]-'0')
	}
	return sign * (hours*3600 + minutes*60 + seconds), true
}
