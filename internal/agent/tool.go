package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// A tool is one thing the agent can do for the person, task-shaped and
// named for what it does. Every tool has a risk class, and the class is
// the floor: a destructive or outward tool is never run without the
// person's word, whatever the model says. The catalog is what the person
// may do — filtered by their permissions, then by the operator's policy —
// so the model is never offered a tool it cannot use.

// Risk is what a tool can cost.
type Risk string

// The risk classes. Read changes nothing; write changes something the
// person can change back; destructive cannot be undone; outward leaves the
// server — a message sent, a request made on the person's behalf.
const (
	RiskRead        Risk = "read"
	RiskWrite       Risk = "write"
	RiskDestructive Risk = "destructive"
	RiskOutward     Risk = "outward"
)

// Family is a tool's group, which is also what the operator's policy can
// name to switch the whole group off.
type Family string

// The families.
const (
	FamilyMailbox Family = "mailbox"
	FamilyDomains Family = "domains"
	FamilyAudit   Family = "audit"
	FamilyPeople  Family = "people"
	FamilyServer  Family = "server"
	FamilyAccount Family = "account"
	FamilyGeneral Family = "general"
	FamilyServers Family = "servers"
	FamilyBrowser Family = "browser"
)

// Tool is one entry of the catalog.
type Tool struct {
	Name        string
	Family      Family
	Description string
	Risk        Risk

	// Parameters is the JSON schema of the arguments.
	Parameters map[string]any

	// Permissions are what the person must hold for the tool to be offered
	// at all: any one of them. Empty means anybody signed in.
	Permissions []models.Permission

	// Core tools are always in the request; the rest may be deferred
	// behind tool_search when the catalog is long.
	Core bool

	// Headless says a run with nobody present may use it: a remote tool
	// the operator marked read-only on a headless server.
	Headless bool

	// Guidance is put in the prompt when the tool is in the request, so a
	// person who cannot manage domains is not told how to.
	Guidance string

	// Preview says what a call would do, in a line, for the confirmation
	// card. Nil uses the tool's name and arguments.
	Preview func(arguments json.RawMessage) string

	// RiskOf, when set, says what one call would cost, for a tool whose
	// actions differ: mail_act is a write until it is delete_forever.
	RiskOf func(arguments json.RawMessage) Risk

	// Run does it.
	Run func(ctx context.Context, call *Call) (*Result, error)

	// Overlay, when set, is asked each round for what is true now.
	Overlay func(ctx context.Context, run *AskRun) string
}

// Call is one use of a tool.
type Call struct {
	ID        string
	Run       *AskRun
	Arguments json.RawMessage

	// Confirmed says the person approved this call.
	Confirmed bool
}

// Result is what a tool answers, for the model and for the person.
type Result struct {
	// Content is what the model reads: JSON, or plain text.
	Content string

	// Untrusted marks content that came from outside — a message, a page —
	// so the loop wraps it as data that never instructs.
	Untrusted bool

	// ShowVerbatim marks a secret the person asked to see: relayed once,
	// exactly, never kept.
	ShowVerbatim bool

	// Note is a line for the drawer, saying what was done.
	Note string
}

// Definition is the tool as a model is given it.
func (self *Tool) Definition() llm.ToolDefinition {
	parameters := self.Parameters
	if parameters == nil {
		parameters = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return llm.ToolDefinition{Name: self.Name, Description: self.Description, Parameters: parameters}
}

// preview is what the confirmation card says.
func (self *Tool) preview(arguments json.RawMessage) string {
	if self.Preview != nil {
		if line := strings.TrimSpace(self.Preview(arguments)); line != "" {
			return line
		}
	}
	return fmt.Sprintf("Run %s with %s", self.Name, strings.TrimSpace(string(arguments)))
}

// Catalog is every tool the server knows.
type Catalog struct {
	tools map[string]*Tool
	order []string
}

// NewCatalog is an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{tools: map[string]*Tool{}}
}

// Register adds a tool; a second tool of the same name is a programming
// error.
func (self *Catalog) Register(tool *Tool) {
	if tool == nil || tool.Name == "" {
		panic("agent: a tool needs a name")
	}
	if _, exists := self.tools[tool.Name]; exists {
		panic("agent: two tools named " + tool.Name)
	}
	if tool.Risk == "" {
		tool.Risk = RiskRead
	}
	self.tools[tool.Name] = tool
	self.order = append(self.order, tool.Name)
}

// Get is a tool by name, or nil.
func (self *Catalog) Get(name string) *Tool {
	return self.tools[name]
}

// All is every tool, in registration order.
func (self *Catalog) All() []*Tool {
	tools := make([]*Tool, 0, len(self.order))
	for _, name := range self.order {
		tools = append(tools, self.tools[name])
	}
	return tools
}

// Offered is the catalog as one person sees it: what their permissions
// admit, minus what the operator switched off.
func (self *Catalog) Offered(permissions *models.EffectivePermissions, policy *config.AgentTools) []*Tool {
	offered := make([]*Tool, 0, len(self.order))
	for _, tool := range self.All() {
		if !allowedByPermissions(tool, permissions) {
			continue
		}
		if policy != nil && listed(policy.Disabled, tool) {
			continue
		}
		offered = append(offered, tool)
	}
	return offered
}

func allowedByPermissions(tool *Tool, permissions *models.EffectivePermissions) bool {
	if len(tool.Permissions) == 0 {
		return true
	}
	if permissions == nil {
		return false
	}
	for _, permission := range tool.Permissions {
		if permissions.Has(permission) {
			return true
		}
	}
	return false
}

// listed says whether a policy list names the tool, by name or family.
func listed(entries []string, tool *Tool) bool {
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if strings.EqualFold(entry, tool.Name) || strings.EqualFold(entry, string(tool.Family)) {
			return true
		}
	}
	return false
}

// riskOf is what a call would cost: the call's own class when the tool
// tells them apart, the tool's otherwise.
func (self *Tool) riskOf(arguments json.RawMessage) Risk {
	if self.RiskOf != nil && len(arguments) > 0 {
		if risk := self.RiskOf(arguments); risk != "" {
			return risk
		}
	}
	return self.Risk
}

// NeedsConfirmation says whether a call must wait for the person: the risk
// class first, then the operator's list, then the person's own. Without
// arguments it answers for the tool as a whole.
func NeedsConfirmation(tool *Tool, arguments json.RawMessage, policy *config.AgentTools, agent *models.Agent) bool {
	risk := tool.riskOf(arguments)
	if risk == RiskDestructive || risk == RiskOutward {
		return true
	}
	if policy != nil && listed(policy.Confirm, tool) {
		return true
	}
	if agent != nil && listed(agent.Confirm, tool) {
		return true
	}
	return false
}

// deferralThreshold is how many definitions go in a request before the
// rest wait behind tool_search: forty is what a model keeps straight.
const deferralThreshold = 40

// Split divides the offered tools into the ones sent this round and the
// ones only listed: everything when the catalog is short; the core set
// plus whatever was loaded when it is long.
func Split(offered []*Tool, loaded map[string]bool, compact bool) (sent, deferred []*Tool) {
	if len(offered) <= deferralThreshold && !compact {
		return offered, nil
	}
	for _, tool := range offered {
		if tool.Core || loaded[tool.Name] {
			sent = append(sent, tool)
		} else {
			deferred = append(deferred, tool)
		}
	}
	return sent, deferred
}

// Search is the deferred tools matching the words, for tool_search.
func Search(deferred []*Tool, query string, limit int) []*Tool {
	words := strings.Fields(strings.ToLower(query))
	type scored struct {
		tool  *Tool
		score int
	}
	var matches []scored
	for _, tool := range deferred {
		haystack := strings.ToLower(tool.Name + " " + string(tool.Family) + " " + tool.Description)
		score := 0
		for _, word := range words {
			if strings.Contains(haystack, word) {
				score++
				if strings.Contains(strings.ToLower(tool.Name), word) {
					score++
				}
			}
		}
		if score > 0 || len(words) == 0 {
			matches = append(matches, scored{tool, score})
		}
	}
	sort.SliceStable(matches, func(left, right int) bool { return matches[left].score > matches[right].score })
	if limit <= 0 {
		limit = 10
	}
	tools := make([]*Tool, 0, limit)
	for _, match := range matches {
		if len(tools) >= limit {
			break
		}
		tools = append(tools, match.tool)
	}
	return tools
}

// Schema helpers, so a tool's parameters read as what they are.

func object(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func enumProperty(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}

func integerProperty(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func booleanProperty(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func arrayProperty(description string, items map[string]any) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": items}
}

// decodeArguments reads a call's arguments, repairing what a model
// mangled.
func decodeArguments[T any](call *Call) (T, error) {
	var arguments T
	raw := strings.TrimSpace(string(call.Arguments))
	if raw == "" || raw == "null" {
		return arguments, nil
	}
	if err := json.Unmarshal([]byte(raw), &arguments); err != nil {
		repaired, repairErr := llm.ExtractJSON(raw)
		if repairErr != nil {
			return arguments, fmt.Errorf("the arguments are not JSON: %w", err)
		}
		if err := json.Unmarshal([]byte(repaired), &arguments); err != nil {
			return arguments, fmt.Errorf("the arguments are not what the tool takes: %w", err)
		}
	}
	return arguments, nil
}

// jsonResult encodes a value as a tool's content.
func jsonResult(value any) (*Result, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &Result{Content: string(encoded)}, nil
}

// textResult is a plain-text tool answer.
func textResult(format string, arguments ...any) *Result {
	return &Result{Content: fmt.Sprintf(format, arguments...)}
}

// mustJSON encodes a value the code built itself.
func mustJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
