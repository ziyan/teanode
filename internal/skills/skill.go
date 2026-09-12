package skills

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill is what one file declares. Only the header is read; the prose
// under it is for whoever opens the file.
type Skill struct {
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Secrets     []*Secret           `yaml:"secrets"`
	Profiles    map[string]*Profile `yaml:"authenticationProfiles"`
	Tools       []*Tool             `yaml:"tools"`

	// Prose is everything after the header, kept so that a person can be
	// shown what they installed.
	Prose string `yaml:"-"`
}

// Secret is a value the skill needs and does not carry: an operator fills
// it in, and steps reach it as {{secret:KEY}}.
type Secret struct {
	Key         string `yaml:"key"`
	Description string `yaml:"description"`
}

// Profile is a way of authenticating that several steps share, so a token
// is written once in the file instead of on every step.
type Profile struct {
	Type     string `yaml:"type"`
	Token    string `yaml:"token"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Header   string `yaml:"header"`

	// The credential for an apiKey profile. The registry's own skills
	// write it as `value`; `key` is taken too, because the field used to
	// be called that and a skill may still say it.
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

// Credential is the apiKey profile's value, whichever name it was
// written under.
func (self *Profile) Credential() string {
	if strings.TrimSpace(self.Value) != "" {
		return self.Value
	}
	return self.Key
}

// Tool is one tool the skill declares, which becomes one tool of the
// agent's catalog.
type Tool struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Type        string         `yaml:"type"`
	Parameters  map[string]any `yaml:"parameters"`
	Timeout     int            `yaml:"timeout"`

	// For a shell tool: the program and its arguments, never one string,
	// because a string invites the quoting to be got wrong.
	Command []string `yaml:"command"`

	// For an http tool: the request itself, in the same fields a step
	// uses. They are written out rather than embedded because a step and
	// a tool share four names, and YAML will not have the same key twice.
	Method   string            `yaml:"method"`
	URL      string            `yaml:"url"`
	Headers  map[string]string `yaml:"headers"`
	Body     any               `yaml:"body"`
	Auth     string            `yaml:"auth"`
	Result   string            `yaml:"result"`
	Select   map[string]string `yaml:"select"`
	MaxBytes int               `yaml:"maxBytes"`

	// For a workflow: the steps in order, or, when the tool routes on a
	// parameter, one list of steps per value of it.
	Steps       []*Step            `yaml:"steps"`
	ActionField string             `yaml:"actionField"`
	Actions     map[string][]*Step `yaml:"actions"`
}

// Request is an http tool read as the one step it is, so that a tool and
// a step are carried out by the same code.
func (self *Tool) Request() *Step {
	return &Step{
		Name: self.Name, Type: KindHTTP, Method: self.Method, URL: self.URL,
		Headers: self.Headers, Body: self.Body, Auth: self.Auth,
		Result: self.Result, Select: self.Select, MaxBytes: self.MaxBytes,
		Timeout: self.Timeout,
	}
}

// Step is one request or one command inside a tool.
type Step struct {
	Name     string            `yaml:"name"`
	Type     string            `yaml:"type"`
	Method   string            `yaml:"method"`
	URL      string            `yaml:"url"`
	Headers  map[string]string `yaml:"headers"`
	Body     any               `yaml:"body"`
	Auth     string            `yaml:"auth"`
	Result   string            `yaml:"result"`
	Select   map[string]string `yaml:"select"`
	MaxBytes int               `yaml:"maxBytes"`
	If       string            `yaml:"if"`
	Command  []string          `yaml:"command"`
	Timeout  int               `yaml:"timeout"`
}

// The kinds a tool or a step may be.
const (
	KindShell    = "shell"
	KindHTTP     = "http"
	KindWorkflow = "workflow"
)

var (
	// nameShape is what a skill and a tool may be called: lower-case
	// words joined by underscores, which is how every tool in the catalog
	// is named, with a hyphen allowed in a skill's own name because the
	// registry uses them.
	nameShape  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	skillShape = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

	// reference is one {{...}} in a template.
	reference = regexp.MustCompile(`\{\{([^}]+)\}\}`)
)

// Parse reads one skill file and refuses anything it cannot carry out.
// Everything after this trusts what comes back, so the checking is done
// here and done strictly: a reference to a step that does not exist would
// otherwise become an empty string in an address, and a skill that quietly
// fetches the wrong thing is worse than one that will not install.
func Parse(content []byte) (*Skill, error) {
	header, prose, err := split(content)
	if err != nil {
		return nil, err
	}
	var skill Skill
	if err := yaml.Unmarshal(header, &skill); err != nil {
		return nil, fmt.Errorf("skills: the header is not readable: %w", err)
	}
	skill.Prose = prose
	if err := skill.validate(); err != nil {
		return nil, err
	}
	return &skill, nil
}

// split takes the YAML header out from between the first two lines that
// are exactly three dashes.
func split(content []byte) ([]byte, string, error) {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	trimmed := strings.TrimLeft(text, "\n ")
	if !strings.HasPrefix(trimmed, "---\n") {
		return nil, "", fmt.Errorf("skills: the file does not begin with a --- header")
	}
	rest := trimmed[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, "", fmt.Errorf("skills: the --- header is never closed")
	}
	header := rest[:end]
	prose := strings.TrimLeft(rest[end+len("\n---"):], "-\n")
	return []byte(header), strings.TrimSpace(prose), nil
}

func (self *Skill) validate() error {
	if !skillShape.MatchString(self.Name) {
		return fmt.Errorf("skills: %q is not a skill name: lower-case words, digits, underscores and hyphens", self.Name)
	}
	if strings.TrimSpace(self.Description) == "" {
		return fmt.Errorf("skills: %s has no description, which is what a person reads to decide about it", self.Name)
	}
	if len(self.Tools) == 0 {
		return fmt.Errorf("skills: %s declares no tools", self.Name)
	}
	known := map[string]bool{}
	for _, secret := range self.Secrets {
		if strings.TrimSpace(secret.Key) == "" {
			return fmt.Errorf("skills: %s declares a secret with no key", self.Name)
		}
		known[secret.Key] = true
	}
	for name, profile := range self.Profiles {
		switch profile.Type {
		case "bearer":
			if strings.TrimSpace(profile.Token) == "" {
				return fmt.Errorf("skills: the authentication %q of %s carries no token", name, self.Name)
			}
		case "basic":
			if strings.TrimSpace(profile.Username) == "" {
				return fmt.Errorf("skills: the authentication %q of %s carries no username", name, self.Name)
			}
		case "apiKey":
			if strings.TrimSpace(profile.Credential()) == "" {
				return fmt.Errorf("skills: the authentication %q of %s carries no value", name, self.Name)
			}
		default:
			return fmt.Errorf("skills: the authentication %q of %s is %q, which is not bearer, basic or apiKey", name, self.Name, profile.Type)
		}
		for _, value := range []string{profile.Token, profile.Username, profile.Password, profile.Key, profile.Value} {
			if err := self.checkSecrets(value, known); err != nil {
				return err
			}
		}
	}
	seen := map[string]bool{}
	for _, tool := range self.Tools {
		if !nameShape.MatchString(tool.Name) {
			return fmt.Errorf("skills: %q is not a tool name in %s: lower-case words joined by underscores", tool.Name, self.Name)
		}
		if seen[tool.Name] {
			return fmt.Errorf("skills: %s declares %s twice", self.Name, tool.Name)
		}
		seen[tool.Name] = true
		if err := self.validateTool(tool, known); err != nil {
			return err
		}
	}
	return nil
}

func (self *Skill) validateTool(tool *Tool, secrets map[string]bool) error {
	where := self.Name + "." + tool.Name
	if strings.TrimSpace(tool.Description) == "" {
		return fmt.Errorf("skills: %s has no description, which is what the model reads to decide whether to call it", where)
	}
	if tool.Parameters == nil {
		return fmt.Errorf("skills: %s declares no parameters; give it an empty object if it takes none", where)
	}
	if kind, _ := tool.Parameters["type"].(string); kind != "object" {
		return fmt.Errorf("skills: the parameters of %s are not an object", where)
	}
	// The schema is sent to a model service as JSON. YAML allows a
	// mapping key that is not a string, which JSON does not, and a schema
	// carrying one would fail to encode -- taking down every request from
	// every person on the server, not just this tool. It is refused here
	// instead, where it costs one install.
	if err := encodable(tool.Parameters); err != nil {
		return fmt.Errorf("skills: the parameters of %s cannot be sent to a model: %w", where, err)
	}
	available := parameterNames(tool.Parameters)
	switch tool.Type {
	case KindShell:
		if len(tool.Command) == 0 {
			return fmt.Errorf("skills: %s is a shell tool with no command", where)
		}
		return self.checkList(where, tool.Command, available, nil, secrets)
	case KindHTTP:
		return self.checkStep(where, tool.Request(), available, nil, secrets)
	case KindWorkflow:
		if tool.ActionField != "" {
			if !available[tool.ActionField] {
				return fmt.Errorf("skills: %s routes on %q, which is not one of its parameters", where, tool.ActionField)
			}
			if len(tool.Actions) == 0 {
				return fmt.Errorf("skills: %s routes on %q but lists no actions", where, tool.ActionField)
			}
			names := make([]string, 0, len(tool.Actions))
			for action := range tool.Actions {
				names = append(names, action)
			}
			sort.Strings(names)
			for _, action := range names {
				if err := self.checkSteps(where+" "+action, tool.Actions[action], available, secrets); err != nil {
					return err
				}
			}
			return nil
		}
		return self.checkSteps(where, tool.Steps, available, secrets)
	}
	return fmt.Errorf("skills: %s is a %q tool, which is not shell, http or workflow", where, tool.Type)
}

func (self *Skill) checkSteps(where string, steps []*Step, available map[string]bool, secrets map[string]bool) error {
	if len(steps) == 0 {
		return fmt.Errorf("skills: %s is a workflow with no steps", where)
	}
	if len(steps) > mostSteps {
		return fmt.Errorf("skills: %s has %d steps, and %d is the most", where, len(steps), mostSteps)
	}
	earlier := map[string]bool{}
	for _, step := range steps {
		if !nameShape.MatchString(step.Name) {
			return fmt.Errorf("skills: %q is not a step name in %s", step.Name, where)
		}
		if earlier[step.Name] {
			return fmt.Errorf("skills: %s has two steps called %s", where, step.Name)
		}
		if err := self.checkStep(where+"."+step.Name, step, available, earlier, secrets); err != nil {
			return err
		}
		earlier[step.Name] = true
	}
	return nil
}

func (self *Skill) checkStep(where string, step *Step, available map[string]bool, earlier map[string]bool, secrets map[string]bool) error {
	if err := self.checkCondition(where, step.If, available, earlier, secrets); err != nil {
		return err
	}
	switch step.Type {
	case KindShell:
		if len(step.Command) == 0 {
			return fmt.Errorf("skills: the shell step %s has no command", where)
		}
		return self.checkList(where, step.Command, available, earlier, secrets)
	case KindHTTP:
		if strings.TrimSpace(step.URL) == "" {
			return fmt.Errorf("skills: the http step %s has no url", where)
		}
		if step.Auth != "" && self.Profiles[step.Auth] == nil {
			return fmt.Errorf("skills: the step %s authenticates as %q, which the skill does not declare", where, step.Auth)
		}
		// Where a credential is sent has to be settled by the skill, not by
		// whoever calls the tool: with a name written into the host, a
		// caller naming a host of their own would be handed the operator's
		// secret. It counts however the credential travels -- a named
		// authentication, or a secret written into a header or a body by
		// hand, which used to go unchecked.
		if step.Auth != "" || self.carriesSecret(step) {
			if err := settledHost(step.URL); err != nil {
				return fmt.Errorf("skills: the step %s sends a credential to %w", where, err)
			}
		}
		values := []string{step.URL, step.Method}
		for _, value := range step.Headers {
			values = append(values, value)
		}
		values = append(values, bodyStrings(step.Body)...)
		if err := self.checkList(where, values, available, earlier, secrets); err != nil {
			return err
		}
		switch step.Result {
		case "", "json", "text":
		default:
			return fmt.Errorf("skills: the step %s asks for a %q result, which is not json or text", where, step.Result)
		}
		if len(step.Select) > 0 && step.Result != "json" {
			return fmt.Errorf("skills: the step %s selects from its answer without asking for json", where)
		}
		return nil
	}
	return fmt.Errorf("skills: the step %s is a %q step, which is not http or shell", where, step.Type)
}

// checkCondition refuses an if this cannot carry out, and checks the names
// in it. A name that stands for nothing at run time is empty rather than
// an error, so the names are checked but not required here.
func (self *Skill) checkCondition(where, condition string, available map[string]bool, earlier map[string]bool, secrets map[string]bool) error {
	if strings.TrimSpace(condition) == "" {
		return nil
	}
	left, operator, right, err := ReadCondition(condition)
	if err != nil {
		return fmt.Errorf("skills: the if of %s: %w", where, err)
	}
	sides := []string{left}
	if operator != "" {
		sides = append(sides, right)
	}
	for _, side := range sides {
		for _, match := range reference.FindAllStringSubmatch(side, -1) {
			if err := self.checkReference(where, strings.TrimSpace(match[1]), available, earlier, secrets); err != nil {
				return err
			}
		}
	}
	return nil
}

func (self *Skill) checkList(where string, values []string, available map[string]bool, earlier map[string]bool, secrets map[string]bool) error {
	for _, value := range values {
		for _, match := range reference.FindAllStringSubmatch(value, -1) {
			if err := self.checkReference(where, strings.TrimSpace(match[1]), available, earlier, secrets); err != nil {
				return err
			}
		}
	}
	return nil
}

// FilterJSON is the one filter a reference may carry after a bar. It
// writes the value as JSON rather than as text, so that a number or a
// true goes into a body unquoted.
const FilterJSON = "json"

// SplitReference takes a reference apart into the name and its filter.
func SplitReference(reference string) (string, string) {
	name, filter, found := strings.Cut(reference, "|")
	if !found {
		return strings.TrimSpace(reference), ""
	}
	return strings.TrimSpace(name), strings.TrimSpace(filter)
}

// checkReference is where a skill that would have quietly fetched the
// wrong thing is stopped: every {{...}} must name a parameter of this
// tool, a value an earlier step selected, or a declared secret.
func (self *Skill) checkReference(where, reference string, available map[string]bool, earlier map[string]bool, secrets map[string]bool) error {
	name, filter := SplitReference(reference)
	if filter != "" && filter != FilterJSON {
		return fmt.Errorf("skills: %s writes %q through the filter %q, and json is the only one", where, name, filter)
	}
	switch {
	case strings.HasPrefix(name, "secret:"):
		key := strings.TrimSpace(strings.TrimPrefix(name, "secret:"))
		if !secrets[key] {
			return fmt.Errorf("skills: %s uses the secret %q, which the skill does not declare", where, key)
		}
	case strings.HasPrefix(name, "steps."):
		parts := strings.SplitN(strings.TrimPrefix(name, "steps."), ".", 2)
		if len(parts) != 2 {
			return fmt.Errorf("skills: %s refers to %q, which names no value of a step", where, name)
		}
		if earlier == nil || !earlier[parts[0]] {
			return fmt.Errorf("skills: %s uses %q before the step %s has run", where, name, parts[0])
		}
	default:
		if !available[name] {
			return fmt.Errorf("skills: %s uses %q, which is not one of its parameters", where, name)
		}
	}
	return nil
}

func (self *Skill) checkSecrets(value string, secrets map[string]bool) error {
	for _, match := range reference.FindAllStringSubmatch(value, -1) {
		name := strings.TrimSpace(match[1])
		if !strings.HasPrefix(name, "secret:") {
			continue
		}
		key := strings.TrimSpace(strings.TrimPrefix(name, "secret:"))
		if !secrets[key] {
			return fmt.Errorf("skills: %s authenticates with the secret %q, which it does not declare", self.Name, key)
		}
	}
	return nil
}

// carriesSecret says whether anything this step sends holds a secret: the
// address, a header, or the body.
func (self *Skill) carriesSecret(step *Step) bool {
	values := []string{step.URL}
	for _, value := range step.Headers {
		values = append(values, value)
	}
	values = append(values, bodyStrings(step.Body)...)
	for _, value := range values {
		for _, match := range reference.FindAllStringSubmatch(value, -1) {
			name, _ := SplitReference(strings.TrimSpace(match[1]))
			if strings.HasPrefix(name, "secret:") {
				return true
			}
		}
	}
	return false
}

// settledHost refuses an address whose host is chosen by whoever calls the
// tool. Everything after the host may be templated freely; the host itself
// must be written into the skill or come from a secret, which is the
// operator's to set. A host taken from a parameter would let a caller name
// their own and be handed the operator's credential.
func settledHost(address string) error {
	address = strings.TrimSpace(address)
	scheme, rest, found := strings.Cut(address, "://")
	if !found {
		rest, scheme = address, ""
	}
	if unsettled(scheme) {
		return fmt.Errorf("an address it is given rather than one it knows")
	}
	authority := rest
	if cut := strings.IndexAny(rest, "/?#"); cut >= 0 {
		authority = rest[:cut]
	}
	if unsettled(authority) {
		return fmt.Errorf("a host chosen by whoever calls it, %q; write the host into the skill, or take it from a secret the operator sets", authority)
	}
	return nil
}

// unsettled says whether a piece of an address carries a reference that is
// not a secret.
func unsettled(piece string) bool {
	for _, match := range reference.FindAllStringSubmatch(piece, -1) {
		name, _ := SplitReference(strings.TrimSpace(match[1]))
		if !strings.HasPrefix(name, "secret:") {
			return true
		}
	}
	return false
}

// encodable says whether a value read from YAML can be written as JSON.
// A mapping with a key that is not a string is the one thing YAML takes
// and JSON does not.
func encodable(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for name, inner := range typed {
			if err := encodable(inner); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	case map[any]any:
		return fmt.Errorf("a name in it is not written as text")
	case []any:
		for index, inner := range typed {
			if err := encodable(inner); err != nil {
				return fmt.Errorf("[%d]: %w", index, err)
			}
		}
	}
	return nil
}

// parameterNames is what a tool's schema says it takes.
func parameterNames(parameters map[string]any) map[string]bool {
	names := map[string]bool{}
	properties, _ := parameters["properties"].(map[string]any)
	for name := range properties {
		names[name] = true
	}
	return names
}

// bodyStrings is every string anywhere in a step's body, which is where a
// reference can hide as well as in the address.
func bodyStrings(body any) []string {
	switch typed := body.(type) {
	case string:
		return []string{typed}
	case map[string]any:
		var values []string
		for _, value := range typed {
			values = append(values, bodyStrings(value)...)
		}
		return values
	case []any:
		var values []string
		for _, value := range typed {
			values = append(values, bodyStrings(value)...)
		}
		return values
	}
	return nil
}
