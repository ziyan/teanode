package agent

import (
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// whenHappened reads the date the run gave, in the person's zone, and how
// precisely it was given.
func whenHappened(run *Run, said string) (*time.Time, string) {
	said = strings.TrimSpace(said)
	if said == "" || strings.EqualFold(said, "null") {
		return nil, ""
	}
	location := time.Local
	if run.Owner != nil && run.Owner.Timezone != "" {
		if loaded, err := time.LoadLocation(run.Owner.Timezone); err == nil {
			location = loaded
		}
	}
	for _, layout := range []struct{ format, precision string }{
		{"2006-01-02", models.HappenedDay}, {"2006-01", models.HappenedMonth},
		{"2006", models.HappenedYear}, {time.RFC3339, models.HappenedDay},
	} {
		if when, err := time.ParseInLocation(layout.format, said, location); err == nil {
			return &when, layout.precision
		}
	}
	return nil, ""
}

// kindOfPath guesses what a page is about from where it was filed.
func kindOfPath(path string) models.AgentNodeKind {
	switch strings.Split(path, "/")[0] {
	case models.PathPeople:
		return models.NodePerson
	case "organizations":
		return models.NodeOrganization
	case models.PathProjects:
		return models.NodeProject
	case models.PathPlaces:
		return models.NodePlace
	case models.PathThings:
		return models.NodeThing
	case models.PathTime:
		return models.NodePeriod
	case models.PathSelf:
		return models.NodeSelf
	}
	return models.NodeTopic
}

// nameFromSlug is a path segment as a name.
func nameFromSlug(slug string) string {
	words := strings.Split(strings.ReplaceAll(slug, "-", " "), " ")
	for index, word := range words {
		if word == "" {
			continue
		}
		runes := []rune(word)
		words[index] = strings.ToUpper(string(runes[0])) + string(runes[1:])
	}
	return strings.Join(words, " ")
}

// firstWordsOf is the opening of a sentence, for naming a page nobody
// named.
func firstWordsOf(text string, count int) string {
	words := strings.Fields(text)
	if len(words) > count {
		words = words[:count]
	}
	return strings.Join(words, " ")
}
