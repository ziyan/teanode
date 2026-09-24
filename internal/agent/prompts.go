package agent

import (
	"bytes"
	"embed"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// The prompts are templates shipped with the release: the conduct, and one
// per kind of run. The person's words and the operator's house
// instructions are handed to the templates as values and rendered inside
// delimited blocks; they are never interpolated into the conduct itself.

//go:embed prompts/*.txt
var promptFiles embed.FS

var prompts = template.Must(template.New("prompts").ParseFS(promptFiles, "prompts/*.txt"))

// ConductData is what the conduct is rendered with.
type ConductData struct {
	AgentName         string
	PersonName        string
	ServerName        string
	Language          string
	Short             bool
	HouseInstructions string
	Instructions      string
	Voice             string
}

// RenderConduct renders the system message for a run nobody is present
// for, in the language of the agent's own work.
func RenderConduct(configuration *config.Configuration, agent *models.Agent, owner *models.User, short bool) (string, error) {
	data := ConductData{
		AgentName:         agent.DisplayName(),
		PersonName:        personName(owner),
		ServerName:        configuration.Server.Name,
		Language:          languageName(KnowledgeLanguage(agent, owner)),
		Short:             short,
		HouseInstructions: strings.TrimSpace(configuration.Agent.Instructions),
		Instructions:      strings.TrimSpace(agent.Instructions),
		Voice:             describeVoice(agent.Voice),
	}
	return render("system.txt", data)
}

// blockTags are the delimiters the job prompts put untrusted text inside.
//
// A message body, a subject, a summarizer's own words about a message: each
// is written by somebody other than this server, and each is placed between
// one of these pairs so the model reads it as something to consider rather
// than as something to do.
//
// Read out of the prompts rather than listed here by hand. The hand list
// had four tags on it and the prompts went on to open a dozen: a document
// carrying </items>, a page carrying </facts>, a conversation carrying
// </conversation> could each close its own block and have the rest read
// as the prompt's own words. A list that has to be kept up to date by
// somebody remembering to is a list that will be out of date.
var blockTags = closingTagsInPrompts()

// closingTagsInPrompts is every closing tag the shipped prompts use.
//
// Sorted, so that what the escaping does is the same on every build. No
// tag here can be part of another -- they all begin "</" and end ">" --
// so the order they are applied in does not matter.
func closingTagsInPrompts() []string {
	entries, err := promptFiles.ReadDir("prompts")
	if err != nil {
		// The prompts are embedded in the binary, so this cannot happen
		// at run time; a build that broke them should not start.
		panic(fmt.Sprintf("agent: reading the prompts: %s", err))
	}
	found := map[string]bool{}
	for _, entry := range entries {
		content, err := promptFiles.ReadFile("prompts/" + entry.Name())
		if err != nil {
			panic(fmt.Sprintf("agent: reading the prompt %s: %s", entry.Name(), err))
		}
		for _, tag := range closingTag.FindAllString(string(content), -1) {
			found[tag] = true
		}
	}
	tags := make([]string, 0, len(found))
	for tag := range found {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

// closingTag matches the end of a block a prompt opened.
var closingTag = regexp.MustCompile(`</[a-zA-Z][a-zA-Z0-9_-]*>`)

// unclosable is text that cannot end the block it is put in.
//
// ask.go's fenced() does this for everything a tool returns, and says why:
// written as a bare join, content carrying the closing tag ended the fence
// itself and everything after it read as the loop's own words. The job
// prompts were a bare join. They are the one path a stranger reaches without
// an account and with nobody present -- triage runs on delivered mail -- so
// they are the last place that should have been left out.
//
// Said rather than dropped, so a message that genuinely discusses the
// marking still reads sensibly.
func unclosable(content string) string {
	for _, tag := range blockTags {
		said := "&lt;" + tag[1:len(tag)-1] + "&gt;"
		content = strings.ReplaceAll(content, tag, said)
	}
	return content
}

// render fills a prompt, with every string it is given made unclosable
// first.
//
// Done here rather than at each call site on purpose: there are a dozen
// places that set one of these fields and one place they all pass through,
// and a control that has to be remembered is a control that will be
// forgotten. Nothing legitimate carries one of these closing tags, so
// escaping a field that happens to be the person's own words costs nothing.
func render(name string, data any) (string, error) {
	var buffer bytes.Buffer
	if err := prompts.ExecuteTemplate(&buffer, name, guarded(data)); err != nil {
		return "", fmt.Errorf("agent: rendering %s: %w", name, err)
	}
	return strings.TrimSpace(buffer.String()) + "\n", nil
}

// guarded copies a prompt's data with every string field made unclosable.
// A value that is not a struct is returned as it came.
func guarded(data any) any {
	value := reflect.ValueOf(data)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return data
		}
		value = value.Elem()
	}
	// A map is as common here as a struct: triage and extract pass one.
	if value.Kind() == reflect.Map {
		if value.Type().Key().Kind() != reflect.String {
			return data
		}
		replaced := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			replaced.SetMapIndex(iterator.Key(), reflect.ValueOf(guardedValue(iterator.Value())))
		}
		return replaced.Interface()
	}
	if value.Kind() != reflect.Struct {
		return data
	}
	copied := reflect.New(value.Type()).Elem()
	copied.Set(value)
	for at := 0; at < copied.NumField(); at++ {
		if !copied.Field(at).CanSet() {
			continue
		}
		switch field := copied.Field(at); field.Kind() {
		case reflect.String:
			field.SetString(unclosable(field.String()))
		case reflect.Slice:
			if field.Type().Elem().Kind() != reflect.String {
				continue
			}
			replaced := reflect.MakeSlice(field.Type(), field.Len(), field.Len())
			for item := 0; item < field.Len(); item++ {
				replaced.Index(item).SetString(unclosable(field.Index(item).String()))
			}
			field.Set(replaced)
		}
	}
	return copied.Interface()
}

// guardedValue is one value out of a map, which may hold anything.
func guardedValue(value reflect.Value) any {
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return value.Interface()
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.String:
		return unclosable(value.String())
	case reflect.Slice:
		if value.Type().Elem().Kind() != reflect.String {
			return value.Interface()
		}
		replaced := make([]string, value.Len())
		for at := 0; at < value.Len(); at++ {
			replaced[at] = unclosable(value.Index(at).String())
		}
		return replaced
	}
	return value.Interface()
}

func personName(owner *models.User) string {
	if owner == nil {
		return "the person"
	}
	if owner.Name != "" {
		return owner.Name
	}
	return owner.Username
}

// languageName turns a locale tag into the word a prompt uses; the model
// understands "en" but reads "English" better.
func languageName(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	base, _, _ := strings.Cut(tag, "-")
	switch base {
	case "", "en":
		return "English"
	case "ja":
		return "Japanese"
	case "zh":
		return "Chinese"
	case "de":
		return "German"
	case "fr":
		return "French"
	case "es":
		return "Spanish"
	case "it":
		return "Italian"
	case "pt":
		return "Portuguese"
	case "nl":
		return "Dutch"
	case "ko":
		return "Korean"
	case "ru":
		return "Russian"
	}
	return "the language with the code " + tag
}

func describeVoice(voice *models.AgentVoice) string {
	if voice == nil {
		return ""
	}
	var lines []string
	if voice.Tone != "" && voice.Tone != "neutral" {
		lines = append(lines, "Tone: "+voice.Tone+".")
	}
	if voice.Length != "" && voice.Length != "medium" {
		lines = append(lines, "Length: keep it "+voice.Length+".")
	}
	if voice.Greeting != "" {
		lines = append(lines, "Open a message with: "+voice.Greeting)
	}
	if voice.Signoff != "" {
		lines = append(lines, "Sign off with: "+voice.Signoff)
	}
	return strings.Join(lines, "\n")
}
