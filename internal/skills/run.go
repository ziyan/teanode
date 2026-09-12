package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/safefetch"
)

const (
	// stepSeconds is how long one step is given when it says nothing,
	// longestSeconds the most any of them may ask for, and answerBytes the
	// most that is read of one answer.
	stepSeconds    = 30
	longestSeconds = 120
	answerBytes    = 256 << 10

	// mostBytes is the hard cap, however much a step asks for.
	mostBytes = 4 << 20
)

// Shell runs a command somewhere. A skill's commands never run on this
// server: the caller hands in the person's attached computer, and a nil
// one means there is nowhere to run them and the step says so.
type Shell interface {
	// Run carries out a command line already quoted for this shell.
	Run(ctx context.Context, command string, timeout time.Duration) (string, error)

	// Windows says whether the command line will be given to cmd, which
	// gives single quotes no meaning at all and would let a value out of
	// its quoting.
	Windows() bool
}

// Secrets are the values a skill declared and the operator filled in.
type Secrets map[string]string

// Running is what carrying a tool out needs besides its arguments.
type Running struct {
	// Shell is where a command goes, which is the person's attached
	// computer. Nil means there is none and a command says so.
	Shell Shell

	// Secrets are the values the skill declared.
	Secrets Secrets

	// Client fetches. Nil means the guarded one every other part of this
	// server fetches through, which refuses private addresses; a test
	// puts its own here to reach its own server.
	Client *http.Client
}

// client fetches for one step. safefetch's own client carries a ten
// second timeout, which would silently override a step that asked for
// longer, so the step's time is put on the client the guard built.
func (self *Running) client(timeout time.Duration) *http.Client {
	if self != nil && self.Client != nil {
		return self.Client
	}
	guarded := safefetch.Client()
	guarded.Timeout = timeout
	return guarded
}

// Run carries out one of a skill's tools and answers what it produced.
func (self *Skill) Run(ctx context.Context, toolName string, arguments map[string]any, running *Running) (map[string]any, error) {
	tool := self.Tool(toolName)
	if tool == nil {
		return nil, fmt.Errorf("skills: %s has no tool called %q", self.Name, toolName)
	}
	values := map[string]any{}
	for name, value := range defaults(tool.Parameters) {
		values[name] = value
	}
	for name, value := range arguments {
		if value != nil {
			values[name] = value
		}
	}
	if err := self.required(tool, values); err != nil {
		return nil, err
	}
	if running == nil {
		running = &Running{}
	}
	state := &run{skill: self, tool: tool, values: values, running: running, steps: map[string]map[string]any{}}
	switch tool.Type {
	case KindShell:
		return state.shellStep(ctx, tool.Name, tool.Command, tool.Timeout)
	case KindHTTP:
		return state.httpStep(ctx, tool.Request())
	}
	steps := tool.Steps
	if tool.ActionField != "" {
		chosen, _ := values[tool.ActionField].(string)
		found, ok := tool.Actions[chosen]
		if !ok {
			return nil, fmt.Errorf("skills: %s does not do %q", tool.Name, chosen)
		}
		steps = found
	}
	var last map[string]any
	for _, step := range steps {
		skip, err := state.skipped(step)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		answer, err := state.one(ctx, step)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", step.Name, err)
		}
		state.steps[step.Name] = answer
		last = answer
	}
	if len(state.steps) == 1 {
		return last, nil
	}
	// Everything every step selected, under the step's own name, so that
	// the model sees the working and not only the end of it.
	whole := map[string]any{}
	for name, answer := range state.steps {
		whole[name] = answer
	}
	return whole, nil
}

// Tool is one of the skill's tools by name.
func (self *Skill) Tool(name string) *Tool {
	for _, tool := range self.Tools {
		if tool.Name == name {
			return tool
		}
	}
	return nil
}

// run is one carrying out of one tool.
type run struct {
	skill   *Skill
	tool    *Tool
	values  map[string]any
	running *Running
	steps   map[string]map[string]any
}

func (self *run) one(ctx context.Context, step *Step) (map[string]any, error) {
	if step.Type == KindShell {
		return self.shellStep(ctx, step.Name, step.Command, step.Timeout)
	}
	return self.httpStep(ctx, step)
}

// skipped says whether a step's if says not to run it.
//
// An if is either one value -- read as true unless it is empty, false or
// zero -- or two values compared with == or !=. Nothing else: no and, no
// or, no arithmetic. A value that is not there is empty rather than an
// error, because "only if the last step found one" is the whole point of
// the field.
func (self *run) skipped(step *Step) (bool, error) {
	condition := strings.TrimSpace(step.If)
	if condition == "" {
		return false, nil
	}
	left, operator, right, err := ReadCondition(condition)
	if err != nil {
		return false, err
	}
	if operator == "" {
		return !truthy(self.operand(left)), nil
	}
	same := strings.EqualFold(strings.TrimSpace(self.operand(left)), strings.TrimSpace(self.operand(right)))
	if operator == "!=" {
		return same, nil
	}
	return !same, nil
}

// ReadCondition takes an if apart. The operator is empty when the whole of
// it is one value.
func ReadCondition(condition string) (left, operator, right string, err error) {
	for _, candidate := range []string{"==", "!="} {
		if before, after, found := strings.Cut(condition, candidate); found {
			left, right = strings.TrimSpace(before), strings.TrimSpace(after)
			if left == "" || right == "" {
				return "", "", "", fmt.Errorf("%q compares nothing", condition)
			}
			if strings.ContainsAny(right, "=<>!&|") {
				return "", "", "", fmt.Errorf("%q is more than one comparison, which an if cannot be", condition)
			}
			return left, candidate, right, nil
		}
	}
	// One value and nothing else. Several words joined by something --
	// "enabled and true" -- would otherwise be read as a word, which is
	// not empty, which is yes: a condition that is always true and never
	// looks it.
	if strings.ContainsAny(condition, "<>&|") || strings.ContainsAny(strings.TrimSpace(condition), " \t") {
		return "", "", "", fmt.Errorf("%q is not a condition an if understands: one value, or two compared with == or !=", condition)
	}
	return condition, "", "", nil
}

// operand is one side of a condition as text: a literal written in place,
// or the value a name stands for. A name that stands for nothing is empty.
func (self *run) operand(text string) string {
	text = strings.TrimSpace(text)
	if unquoted, err := strconv.Unquote(text); err == nil {
		return unquoted
	}
	name := text
	if strings.HasPrefix(name, "{{") && strings.HasSuffix(name, "}}") {
		name = strings.Trim(name, "{}")
	}
	switch strings.ToLower(name) {
	case "true", "false", "null", "undefined", "":
		return strings.ToLower(name)
	}
	if value, ok := self.lookup(strings.TrimSpace(name)); ok {
		return asText(value)
	}
	// Not a name this tool knows: a bare word written in the condition.
	if reference.MatchString(text) || strings.HasPrefix(name, "steps.") || strings.HasPrefix(name, "secret:") {
		return ""
	}
	if _, err := strconv.ParseFloat(name, 64); err == nil {
		return name
	}
	return ""
}

// truthy is what counts as yes for a bare value.
func truthy(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "false", "0", "null", "undefined":
		return false
	}
	return true
}

func (self *run) shellStep(ctx context.Context, name string, command []string, seconds int) (map[string]any, error) {
	if self.running.Shell == nil {
		return nil, fmt.Errorf("this runs a command, which needs a computer of the person's attached; ask them to run `teanode computer start` on it")
	}
	if self.running.Shell.Windows() {
		// cmd's quoting is not this quoting, and getting it wrong lets a
		// value become another command. Until that is written and tested,
		// a skill's commands do not run there.
		return nil, fmt.Errorf("a skill's commands are not run on a Windows computer yet; the shell tool reaches it directly")
	}
	parts := make([]string, 0, len(command))
	for _, part := range command {
		filled, err := self.fill(part)
		if err != nil {
			return nil, err
		}
		parts = append(parts, Quote(filled))
	}
	printed, err := self.running.Shell.Run(ctx, strings.Join(parts, " "), secondsOf(seconds))
	if err != nil {
		return nil, err
	}
	return map[string]any{"text": cut(printed)}, nil
}

func (self *run) httpStep(ctx context.Context, step *Step) (map[string]any, error) {
	address, err := self.fillURL(step.URL)
	if err != nil {
		return nil, err
	}
	target, err := safefetch.ParseTarget(strings.TrimSpace(address))
	if err != nil {
		return nil, err
	}
	method := strings.ToUpper(strings.TrimSpace(step.Method))
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if step.Body != nil {
		written, err := self.fillBody(step.Body)
		if err != nil {
			return nil, err
		}
		body = strings.NewReader(written)
	}
	allowed := secondsOf(step.Timeout)
	ctx, cancel := context.WithTimeout(ctx, allowed)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	if body != nil && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range step.Headers {
		filled, err := self.fill(value)
		if err != nil {
			return nil, err
		}
		request.Header.Set(name, filled)
	}
	if err := self.authenticate(request, step.Auth); err != nil {
		return nil, err
	}
	response, err := self.running.client(allowed).Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	// What the step asks for, up or down, within the hard cap: asking for
	// more than the default and silently getting less cut a JSON answer
	// in half and reported it as a service that does not answer with JSON.
	most := int64(answerBytes)
	if step.MaxBytes > 0 {
		most = int64(step.MaxBytes)
		if most > mostBytes {
			most = mostBytes
		}
	}
	answer, err := io.ReadAll(io.LimitReader(response.Body, most+1))
	if err != nil {
		return nil, err
	}
	cutShort := int64(len(answer)) > most
	if cutShort {
		answer = answer[:most]
	}
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("%s answered %d: %s", target.Host, response.StatusCode, cutTo(strings.TrimSpace(string(answer)), 300))
	}
	if step.Result != "json" {
		text := string(answer)
		if cutShort {
			text += "\n[cut here: the answer goes on]"
		}
		return map[string]any{"text": text}, nil
	}
	var parsed any
	if err := json.Unmarshal(answer, &parsed); err != nil {
		if cutShort {
			return nil, fmt.Errorf("%s answered with more than the %d bytes this step reads, so what came back is not whole JSON; raise maxBytes on the step", target.Host, most)
		}
		return nil, fmt.Errorf("%s did not answer with JSON: %w", target.Host, err)
	}
	if len(step.Select) == 0 {
		return map[string]any{"json": parsed}, nil
	}
	picked := map[string]any{}
	for name, path := range step.Select {
		if value, ok := pick(parsed, path); ok {
			picked[name] = value
		}
	}
	return picked, nil
}

// authenticate puts the skill's named way of authenticating on a request.
func (self *run) authenticate(request *http.Request, name string) error {
	if name == "" {
		return nil
	}
	profile := self.skill.Profiles[name]
	if profile == nil {
		return fmt.Errorf("the skill has no authentication called %q", name)
	}
	switch profile.Type {
	case "bearer":
		token, err := self.fill(profile.Token)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+token)
	case "basic":
		username, err := self.fill(profile.Username)
		if err != nil {
			return err
		}
		password, err := self.fill(profile.Password)
		if err != nil {
			return err
		}
		request.SetBasicAuth(username, password)
	case "apiKey":
		key, err := self.fill(profile.Credential())
		if err != nil {
			return err
		}
		header := profile.Header
		if header == "" {
			header = "X-API-Key"
		}
		request.Header.Set(header, key)
	}
	return nil
}

// fill writes the values into a template. Every {{...}} was checked when
// the skill was read, so what is left is finding the value; one that is
// not there is an error naming it rather than an empty string, because an
// address with a hole in it fetches the wrong thing quietly.
func (self *run) fill(text string) (string, error) {
	var failure error
	filled := reference.ReplaceAllStringFunc(text, func(whole string) string {
		name, filter := SplitReference(strings.Trim(whole, "{}"))
		value, ok := self.lookup(name)
		if !ok {
			failure = fmt.Errorf("%s has no value", name)
			return ""
		}
		if filter == FilterJSON {
			written, err := json.Marshal(value)
			if err != nil {
				failure = err
				return ""
			}
			return string(written)
		}
		return asText(value)
	})
	return filled, failure
}

// fillURL writes the values into an address, escaping each one as it goes:
// a place name with a space in it would otherwise make a request no server
// answers, and a value carrying a ? or an & would add to the address
// rather than sit in it. A template that is one reference and nothing else
// is the address itself -- a step passing on a link an earlier step found
// -- and is left exactly as it came.
func (self *run) fillURL(text string) (string, error) {
	if whole := strings.TrimSpace(text); reference.FindString(whole) == whole {
		return self.fill(whole)
	}
	var failure error
	filled := reference.ReplaceAllStringFunc(text, func(match string) string {
		name, filter := SplitReference(strings.Trim(match, "{}"))
		value, ok := self.lookup(name)
		if !ok {
			failure = fmt.Errorf("%s has no value", name)
			return ""
		}
		if filter == FilterJSON {
			written, err := json.Marshal(value)
			if err != nil {
				failure = err
				return ""
			}
			return escapeInURL(string(written))
		}
		return escapeInURL(asText(value))
	})
	return filled, failure
}

// escapeInURL percent-encodes what cannot sit in an address literally, and
// what would change its shape, and leaves the rest: a comma and a slash go
// through, because skills put coordinates and paths in addresses.
func escapeInURL(value string) string {
	var written strings.Builder
	for _, letter := range []byte(value) {
		if letter <= 0x20 || letter >= 0x7f || strings.IndexByte("\"<>{}|\\^`#?&", letter) >= 0 {
			_, _ = fmt.Fprintf(&written, "%%%02X", letter)
			continue
		}
		written.WriteByte(letter)
	}
	return written.String()
}

// fillBody writes the values through a body of any shape and hands back
// the JSON to send.
func (self *run) fillBody(body any) (string, error) {
	if text, ok := body.(string); ok {
		return self.fill(text)
	}
	walked, err := self.walk(body)
	if err != nil {
		return "", err
	}
	written, err := json.Marshal(walked)
	if err != nil {
		return "", err
	}
	return string(written), nil
}

func (self *run) walk(value any) (any, error) {
	switch typed := value.(type) {
	case string:
		return self.fill(typed)
	case map[string]any:
		walked := map[string]any{}
		for name, inner := range typed {
			written, err := self.walk(inner)
			if err != nil {
				return nil, err
			}
			walked[name] = written
		}
		return walked, nil
	case []any:
		walked := make([]any, 0, len(typed))
		for _, inner := range typed {
			written, err := self.walk(inner)
			if err != nil {
				return nil, err
			}
			walked = append(walked, written)
		}
		return walked, nil
	}
	return value, nil
}

// lookup is what one reference names: a secret, something an earlier step
// selected, or one of the tool's own parameters.
func (self *run) lookup(name string) (any, bool) {
	if key, found := strings.CutPrefix(name, "secret:"); found {
		value, ok := self.running.Secrets[strings.TrimSpace(key)]
		return value, ok
	}
	if rest, found := strings.CutPrefix(name, "steps."); found {
		step, field, _ := strings.Cut(rest, ".")
		answer, ok := self.steps[step]
		if !ok {
			return nil, false
		}
		value, ok := answer[field]
		return value, ok
	}
	value, ok := self.values[name]
	return value, ok
}

// required refuses a call that left out something the schema asks for,
// before a request is made with a hole in it.
func (self *Skill) required(tool *Tool, values map[string]any) error {
	listed, _ := tool.Parameters["required"].([]any)
	for _, entry := range listed {
		name, _ := entry.(string)
		if name == "" {
			continue
		}
		if value, ok := values[name]; !ok || asText(value) == "" {
			return fmt.Errorf("%s needs %s", tool.Name, name)
		}
	}
	return nil
}

// defaults are the values a schema fills in for itself.
func defaults(parameters map[string]any) map[string]any {
	filled := map[string]any{}
	properties, _ := parameters["properties"].(map[string]any)
	for name, described := range properties {
		if shape, ok := described.(map[string]any); ok {
			if value, ok := shape["default"]; ok {
				filled[name] = value
			}
		}
	}
	return filled
}

// pick reads one value out of a parsed answer by a path: names separated
// by dots, with a number or a [number] for a place in a list.
func pick(value any, path string) (any, bool) {
	for _, part := range strings.Split(strings.ReplaceAll(strings.ReplaceAll(path, "[", "."), "]", ""), ".") {
		if part == "" {
			continue
		}
		if index, err := strconv.Atoi(part); err == nil {
			list, ok := value.([]any)
			if !ok || index < 0 || index >= len(list) {
				return nil, false
			}
			value = list[index]
			continue
		}
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

// Quote wraps a word for /bin/sh, which is what a computer runs a command
// through. Single quotes take everything literally, and the one character
// they cannot hold is closed, escaped and opened again. This is not cmd's
// quoting, which is why a Windows computer is refused above.
func Quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func asText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	}
	written, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(written)
}

func secondsOf(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = stepSeconds
	}
	if seconds > longestSeconds {
		seconds = longestSeconds
	}
	return time.Duration(seconds) * time.Second
}

func cut(text string) string { return cutTo(text, answerBytes) }

func cutTo(text string, most int) string {
	if len(text) <= most {
		return text
	}
	return text[:most] + "\n[cut here: there is more]"
}
