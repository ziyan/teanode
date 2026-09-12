package agent

import (
	"bytes"
	"embed"
	"fmt"
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

// RenderConduct renders the system message for a run.
func RenderConduct(configuration *config.Configuration, agent *models.Agent, owner *models.User, short bool) (string, error) {
	data := ConductData{
		AgentName:         agent.DisplayName(),
		PersonName:        personName(owner),
		ServerName:        configuration.Server.Name,
		Language:          languageName(Language(agent, owner)),
		Short:             short,
		HouseInstructions: strings.TrimSpace(configuration.Agent.Instructions),
		Instructions:      strings.TrimSpace(agent.Instructions),
		Voice:             describeVoice(agent.Voice),
	}
	return render("system.txt", data)
}

func render(name string, data any) (string, error) {
	var buffer bytes.Buffer
	if err := prompts.ExecuteTemplate(&buffer, name, data); err != nil {
		return "", fmt.Errorf("agent: rendering %s: %w", name, err)
	}
	return strings.TrimSpace(buffer.String()) + "\n", nil
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
