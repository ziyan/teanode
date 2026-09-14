// Package tools is the agent's tool kit: what a tool is, what a call and
// a result are, the catalog with its permissions and policy, and the
// registry every tool package registers into from its own init. A tool
// reaches the run it is part of — the person, the agent, the database, the
// operations — through the context, never through the loop's own types,
// so a tool package imports this package and nothing above it.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
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
// server — a message sent, a request made on the person's behalf; granting
// hands out a way in, or changes who may do what.
//
// Granting is its own class because it is neither of the two it kept being
// filed under. Minting an API token is not destructive -- nothing is lost --
// and it is not outward -- nothing leaves. It was therefore an ordinary
// write, so an agent following instructions it read in a message could mint a
// token for the whole account, and the person was never asked. What a
// credential costs is not measured by what it changes; it is measured by what
// somebody holding it can do afterwards.
const (
	RiskRead        Risk = "read"
	RiskWrite       Risk = "write"
	RiskDestructive Risk = "destructive"
	RiskOutward     Risk = "outward"
	RiskGranting    Risk = "granting"
)

// Family is a tool's group, which is also what the operator's policy can
// name to switch the whole group off.
type Family string

// The families.
const (
	FamilyMailbox  Family = "mailbox"
	FamilyDomains  Family = "domains"
	FamilyAudit    Family = "audit"
	FamilyPeople   Family = "people"
	FamilyServer   Family = "server"
	FamilyAccount  Family = "account"
	FamilyGeneral  Family = "general"
	FamilyServers  Family = "servers"
	FamilyBrowser  Family = "browser"
	FamilyComputer Family = "computer"
	FamilySkills   Family = "skills"
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
	//
	// The card is the one moment a person decides, and it has to say what
	// will happen in the words they would use -- "Send \"Thursday?\" to
	// maria@example.net", not the call. A card that prints the arguments
	// makes the person parse JSON to decide, and the identifiers in it
	// mean nothing to them.
	Preview func(arguments json.RawMessage) string

	// PreviewIn is the same line for a tool that has to look something up
	// to say it: mail_send is handed a draft id, and what the person needs
	// to see is the subject and who it goes to. The run is in the context.
	// Set one or the other; this wins where both are set.
	PreviewIn func(ctx context.Context, arguments json.RawMessage) string

	// RiskOf, when set, says what one call would cost, for a tool whose
	// actions differ: mail_act is a write until it is delete_forever.
	RiskOf func(arguments json.RawMessage) Risk

	// Run does it.
	Run func(ctx context.Context, call *Call) (*Result, error)

	// Overlay, when set, is asked each round for what is true now; the run
	// is in the context.
	Overlay func(ctx context.Context) string
}

// Call is one use of a tool. The run it is part of — the person, the
// agent, the database, the operations — is in the context, through
// RunFrom, so a tool package needs nothing of the loop's own types.
type Call struct {
	ID        string
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

	// Images are pictures the model asked to look at, given to it as a
	// turn of its own after this round's results; not kept.
	Images []llm.ContentPart
}

// Definition is the tool as a model is given it.
func (self *Tool) Definition() llm.ToolDefinition {
	parameters := self.Parameters
	if parameters == nil {
		parameters = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return llm.ToolDefinition{Name: self.Name, Description: self.Description, Parameters: parameters}
}

// PreviewLine is what the confirmation card says.
func (self *Tool) PreviewLine(ctx context.Context, arguments json.RawMessage) string {
	if self.PreviewIn != nil {
		if line := strings.TrimSpace(self.PreviewIn(ctx, arguments)); line != "" {
			return line
		}
	}
	if self.Preview != nil {
		if line := strings.TrimSpace(self.Preview(arguments)); line != "" {
			return line
		}
	}
	return describeCall(self.Name, arguments)
}

// describeCall is the last-resort line for a tool that says nothing about
// itself: the tool's name as words, and its arguments as "name: value"
// rather than as the JSON object they arrived in.
//
// Not a good card -- a tool that can ask for a person's word should say
// what it is asking in its own words -- but a readable one, so that adding
// a tool and forgetting the sentence gives somebody a line they can act on
// instead of a blob they have to read as a programmer.
func describeCall(name string, arguments json.RawMessage) string {
	said := strings.ReplaceAll(name, "_", " ")
	said = strings.ToUpper(said[:1]) + said[1:]
	var fields map[string]any
	if err := json.Unmarshal(arguments, &fields); err != nil || len(fields) == 0 {
		return said
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(fmt.Sprintf("%v", fields[key]))
		if value == "" || value == "<nil>" || value == "false" || value == "[]" || value == "map[]" {
			continue
		}
		if len(value) > 80 {
			value = value[:80] + "…"
		}
		parts = append(parts, strings.ReplaceAll(key, "_", " ")+": "+value)
	}
	if len(parts) == 0 {
		return said
	}
	return said + " — " + strings.Join(parts, ", ")
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
		if !AllowedByPermissions(tool, permissions) {
			continue
		}
		if policy != nil && Listed(policy.Disabled, tool) {
			continue
		}
		offered = append(offered, tool)
	}
	return offered
}

// AllowedByPermissions says whether the person holds one of the
// permissions a tool asks for.
func AllowedByPermissions(tool *Tool, permissions *models.EffectivePermissions) bool {
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

// ActionsOf is the verbs a tool takes, read out of its own schema: the
// values of its "action" enumeration, in the order they are offered.
//
// From the schema rather than from a field, because a tool is action-shaped
// whether it was merged out of several tools or written that way in the
// first place. The policy page listed the verbs of the merged ones and
// nothing beside group_manage, which takes add, update and remove of its
// own -- so the page looked as though the two kinds of tool differed, and
// they do not.
//
// Empty for a tool that is one thing, and for one whose action is free text
// rather than a choice.
func ActionsOf(tool *Tool) []string {
	if tool == nil || tool.Parameters == nil {
		return nil
	}
	properties, ok := tool.Parameters["properties"].(map[string]any)
	if !ok {
		return nil
	}
	action, ok := properties["action"].(map[string]any)
	if !ok {
		return nil
	}
	switch values := action["enum"].(type) {
	case []string:
		return append([]string{}, values...)
	case []any:
		verbs := make([]string, 0, len(values))
		for _, value := range values {
			if word, ok := value.(string); ok {
				verbs = append(verbs, word)
			}
		}
		return verbs
	}
	return nil
}

// Listed says whether a policy list names the tool, by name or family, or by
// a name one of its actions used to have.
func Listed(entries []string, tool *Tool) bool {
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if strings.EqualFold(entry, tool.Name) || strings.EqualFold(entry, string(tool.Family)) {
			return true
		}
		if merged, renamed := Renamed[strings.ToLower(entry)]; renamed && strings.EqualFold(merged, tool.Name) {
			return true
		}
	}
	return false
}

// RiskFor is what a call would cost: the call's own class when the tool
// tells them apart, the tool's otherwise.
//
// Judged from the arguments as the tool will read them, which is why they are
// settled first. Read strictly here and repaired there, the two disagreed
// about the same call: a RiskOf that cannot parse what it is given falls back
// to the tool's own class, and the tool then repaired the braces and did the
// thing. Writing the arguments sloppily was enough to turn an outward call
// into an ordinary write and walk past the question -- a rule that forwards
// every message to a stranger, an invitation to a list of them, a token, a
// command. The gate and the act have to read the same bytes.
func (self *Tool) RiskFor(arguments json.RawMessage) Risk {
	settled := SettledArguments(arguments)
	if self.RiskOf != nil && len(settled) > 0 {
		if risk := self.RiskOf(settled); risk != "" {
			return risk
		}
	}
	return self.Risk
}

// SettledArguments are a call's arguments as everything that reads them must
// see them: repaired once, so that what a call is judged by and what it does
// cannot be two different things.
//
// Arguments that are already JSON are returned untouched, which is nearly
// every call. Anything else is put through the same repair the tool's own
// decoding would have done, and what will not parse even then is given back
// as it came, for the tool to refuse in its own words.
func SettledArguments(arguments json.RawMessage) json.RawMessage {
	raw := strings.TrimSpace(string(arguments))
	if raw == "" {
		return arguments
	}
	if json.Valid([]byte(raw)) {
		return arguments
	}
	repaired, err := llm.ExtractJSON(raw)
	if err != nil {
		return arguments
	}
	return json.RawMessage(repaired)
}

// NeedsConfirmation says whether a call must wait for the person: the risk
// class first, then the operator's list, then the person's own. Without
// arguments it answers for the tool as a whole.
func NeedsConfirmation(tool *Tool, arguments json.RawMessage, policy *config.AgentTools, agent *models.Agent) bool {
	risk := tool.RiskFor(arguments)
	if risk == RiskDestructive || risk == RiskOutward || risk == RiskGranting {
		return true
	}
	if policy != nil && Listed(policy.Confirm, tool) {
		return true
	}
	if agent != nil && Listed(agent.Confirm, tool) {
		return true
	}
	return false
}

// ResultCharacters bounds a tool's answer as the history keeps it and as
// a tab's answer is cut.
const ResultCharacters = 24000

// DeferralThreshold is how many definitions go in a request before the
// rest wait behind tool_search: forty is what a model keeps straight.
const DeferralThreshold = 40

// Split divides the offered tools into the ones sent this round and the
// ones only listed: everything when the catalog is short; the core set
// plus whatever was loaded when it is long.
func Split(offered []*Tool, loaded map[string]bool, compact bool) (sent, deferred []*Tool) {
	if len(offered) <= DeferralThreshold && !compact {
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
// Exported: every tool package builds its parameters with them.

func Object(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func StringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func EnumProperty(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}

func IntegerProperty(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func BooleanProperty(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func ArrayProperty(description string, items map[string]any) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": items}
}

// DecodeArguments reads a call's arguments, repairing what a model
// mangled.
func DecodeArguments[T any](call *Call) (T, error) {
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

// JSONResult encodes a value as a tool's content.
func JSONResult(value any) (*Result, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &Result{Content: string(encoded)}, nil
}

// TextResult is a plain-text tool answer.
func TextResult(format string, arguments ...any) *Result {
	return &Result{Content: fmt.Sprintf(format, arguments...)}
}

// MustJSON encodes a value the code built itself.
func MustJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

// ActionOf is the "action" of a call's arguments, lowercased, for a tool
// whose risk depends on it. Read from the JSON, never by looking for the
// word inside it: a name or a title containing "delete" is not a delete.
func ActionOf(arguments json.RawMessage) string {
	var call struct {
		Action string `json:"action"`
	}
	if len(arguments) == 0 || json.Unmarshal(arguments, &call) != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(call.Action))
}

// imageTypes are the pictures a model can look at and a browser shows.
var imageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// IsImage says whether a file is a picture a model can look at.
func IsImage(contentType string) bool {
	return imageTypes[strings.ToLower(strings.TrimSpace(contentType))]
}

// --- confirmation cards --------------------------------------------------

// PreviewOf builds a tool's Preview from a function of its arguments, so
// that a card is written as the sentence it is rather than as JSON
// handling. Arguments that will not decode give the tool's own words back
// through the fallback, because a card is shown before anything runs and
// must say something either way.
func PreviewOf[T any](say func(T) string) func(json.RawMessage) string {
	return func(arguments json.RawMessage) string {
		var call T
		if err := json.Unmarshal(arguments, &call); err != nil {
			return ""
		}
		return say(call)
	}
}

// Named is how a card points at a thing: what the person calls it, in
// quotes, or a stand-in when the call gives no name.
func Named(value, stand string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return stand
	}
	return strconv.Quote(value)
}

// In is " in <where>", or nothing: where a call names a mailbox or a domain
// the card says which, and where it does not there is only one to mean.
func In(where string) string {
	if where = strings.TrimSpace(where); where == "" {
		return ""
	}
	return " in " + where
}

// Some is a few things named in a line, with the rest counted.
func Some(values []string, limit int) string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			kept = append(kept, value)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	if len(kept) <= limit {
		return strings.Join(kept, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(kept[:limit], ", "), len(kept)-limit)
}
