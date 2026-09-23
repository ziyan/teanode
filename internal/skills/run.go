package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
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

	// mostBytes is the hard cap on an answer read as text, however much a
	// step asks for; pictureBytes and fileBytes are what a step asking for
	// a picture or a file reads, and the most either may ask for. A file
	// is held whole in memory on its way to the person, which is what
	// bounds it: the same bound the agent puts on handing a file over.
	mostBytes    = 4 << 20
	pictureBytes = 2 << 20
	fileBytes    = 32 << 20
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

	// Allowance is what the operator has said this server may reach inside
	// their own network, for a skill pointed at equipment of theirs. Nil
	// keeps the guard as it is everywhere else.
	Allowance *safefetch.Allowance

	// Unverified is the equipment whose certificate is not checked, for the
	// controllers that cannot present a valid one for the address they are
	// reached at. Nil checks every certificate, which is the default.
	Unverified *safefetch.Allowance

	// Files are what the steps asking for a picture or a file fetched, in
	// the order they fetched them. They are collected here rather than put
	// in the answer because bytes are not text: the caller hands them to
	// the person as files of the conversation, and shows the model the
	// ones it can look at.
	Files []Fetched

	// The cookies the steps of this one run have been given, made when the
	// first step asks for them.
	once sync.Once
	jar  http.CookieJar
}

// Fetched is a file one step fetched rather than read as text.
type Fetched struct {
	// Step is the step that fetched it, which is what it is named after
	// when it is handed to somebody.
	Step string

	// MediaType is what the service said it is, with any parameters cut
	// off: image/jpeg, video/mp4, application/pdf.
	MediaType string

	// Data is the file itself.
	Data []byte

	// Look says a model can be shown it, which only a picture can be. A
	// clip is handed to the person and worked on by their own programs;
	// no model here watches one.
	Look bool
}

// cookies are the cookies this run's steps have been given, so that a step
// which signs in is followed by steps that are signed in. Plenty of
// equipment has no other way: a console takes a name and a password at one
// address and answers everything else only to the session it handed back.
//
// The jar belongs to the one run, so a session never outlives the call that
// opened it or reaches another person's. A jar is also where the cookie
// belongs rather than a header a skill copies about by hand: it sends each
// cookie back only to the host that set it, so a step pointed somewhere
// else cannot carry somebody's session out with it.
func (self *Running) cookies() http.CookieJar {
	if self == nil {
		return nil
	}
	self.once.Do(func() {
		jar, err := cookiejar.New(nil)
		if err != nil {
			// cookiejar.New(nil) does not fail; a run without a jar is
			// still a run, and a step that needed one says so itself.
			return
		}
		self.jar = jar
	})
	return self.jar
}

// client fetches for one step. safefetch's own client carries a ten
// second timeout, which would silently override a step that asked for
// longer, so the step's time is put on the client the guard built.
func (self *Running) client(timeout time.Duration, host string) *http.Client {
	if self != nil && self.Client != nil {
		// A client given from outside -- one that goes through the
		// person's computer -- still keeps the steps' cookies, so a skill
		// that signs in and then fetches is signed in when it fetches.
		given := *self.Client
		given.Jar = self.cookies()
		return &given
	}
	var allowance, unverified *safefetch.Allowance
	if self != nil {
		allowance, unverified = self.Allowance, self.Unverified
	}
	// Per request, with the host in hand: skipping the certificate check is
	// for the one piece of equipment the operator named, and a client shared
	// between steps could not tell them apart.
	guarded := safefetch.ClientAllowingUnverified(allowance, unverified, host)
	guarded.Timeout = timeout
	// One jar across the steps, though the clients are made one per step:
	// a workflow that signs in and then fetches is signed in when it
	// fetches.
	guarded.Jar = self.cookies()
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
	quiet := map[string]bool{}
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
		if step.Quiet {
			quiet[step.Name] = true
		}
	}
	// Everything every step selected, under the step's own name, so that
	// the model sees the working and not only the end of it -- except the
	// steps that asked to be left out of it, which is how a sign-in keeps
	// its token out of the answer while the steps after it still use it.
	shown := map[string]map[string]any{}
	for name, answer := range state.steps {
		if quiet[name] {
			continue
		}
		shown[name] = withheld(answer)
	}
	// One step's answer is that step's answer, not a map with one key in
	// it. A workflow that signs in and then does the one thing it was
	// asked reads the same way as the tool that does it in a single step.
	if len(shown) == 1 {
		for _, only := range shown {
			return only, nil
		}
	}
	// Every step was quiet. Saying so beats handing back an empty map,
	// which a model reads as the tool having done nothing.
	if len(shown) == 0 {
		return map[string]any{"done": true}, nil
	}
	whole := map[string]any{}
	for name, answer := range shown {
		whole[name] = answer
	}
	return whole, nil
}

// credentialFields are the names a skill gives a thing that signs a request.
//
// A step saying quiet is the way to keep a credential out of the answer, and
// it is the one to use. This is for the skills that do not say it yet: an
// installed skill keeps working as it was written, and until its author
// republishes it the token it selects would still be read by a model. The
// names here are the conventional ones, and a field called any of them has no
// business being shown to anybody.
var credentialFields = map[string]bool{
	"token": true, "access_token": true, "refresh_token": true, "id_token": true,
	"password": true, "secret": true, "api_key": true, "apikey": true,
	"authorization": true, "session_token": true, "session_key": true,
}

// withheld is a step's answer with anything credential-shaped taken out of
// it. The step's own answer is untouched, so the steps after it still read
// the real value; this is only what goes back to the model.
func withheld(answer map[string]any) map[string]any {
	var kept map[string]any
	for name := range answer {
		if !credentialFields[strings.ToLower(name)] {
			continue
		}
		if kept == nil {
			kept = make(map[string]any, len(answer))
			for each, value := range answer {
				kept[each] = value
			}
		}
		kept[name] = "(kept back)"
	}
	if kept == nil {
		return answer
	}
	return kept
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
	response, err := self.running.client(allowed, target.Hostname()).Do(request)
	if err != nil {
		// The reason, without the address it was reaching. A step may
		// carry a secret in its query string -- the skill's author chooses
		// that -- and Go wraps a failed request in an error quoting the
		// whole URL. That error becomes the tool's answer, which is kept
		// in the run and sent to the model's provider. The two reports
		// below already say only the host; this one said everything.
		cause := err
		var wrapped *url.Error
		if errors.As(err, &wrapped) {
			cause = wrapped.Err
		}
		return nil, fmt.Errorf("%s did not answer: %w", target.Host, cause)
	}
	defer func() { _ = response.Body.Close() }()
	// What the step asks for, up or down, within the hard cap: asking for
	// more than the default and silently getting less cut a JSON answer
	// in half and reported it as a service that does not answer with JSON.
	// What this step reads, and the most it may ask for. A picture or a
	// file is bytes rather than letters, and both are commonly larger than
	// anything worth reading as text -- a camera's snapshot is past the
	// reading default, and a minute of video is past all of them.
	most, ceiling := int64(answerBytes), int64(mostBytes)
	switch step.Result {
	case ResultImage:
		most, ceiling = pictureBytes, pictureBytes
	case ResultFile:
		most, ceiling = fileBytes, fileBytes
	}
	if step.MaxBytes > 0 {
		most = int64(step.MaxBytes)
	}
	if most > ceiling {
		most = ceiling
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
	if step.Result == ResultImage || step.Result == ResultFile {
		return self.fetched(step, response, answer, most, cutShort)
	}
	if step.Result != ResultJSON {
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

// pictureTypes are the pictures a model can be shown and a browser can
// display. Anything else a service calls an image -- an SVG, which is a
// document that can fetch, a TIFF nothing renders -- is refused here
// rather than handed on.
var pictureTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
}

// fetched keeps what a step fetched beside the answer instead of in it.
//
// The answer a step returns is read by a model as text. A JPEG or an MP4
// read as text is a few hundred thousand characters of noise that say
// nothing about what is in them, cost what they cost, and crowd out
// everything else in the round -- so the bytes go to the caller, which
// hands them to the person as a file of the conversation and shows the
// model the ones it can look at. What goes in the answer is a line saying
// what was fetched and where the person's copy is.
func (self *run) fetched(step *Step, response *http.Response, answer []byte, most int64, cutShort bool) (map[string]any, error) {
	where := "the service"
	if response.Request != nil && response.Request.URL != nil {
		where = response.Request.URL.Host
	}
	wanted := "file"
	if step.Result == ResultImage {
		wanted = "picture"
	}
	mediaType := response.Header.Get("Content-Type")
	if cut := strings.IndexByte(mediaType, ';'); cut >= 0 {
		mediaType = mediaType[:cut]
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	// A picture has to be one a model can actually be shown; a file may be
	// anything, since nothing here reads it.
	if step.Result == ResultImage && !pictureTypes[mediaType] {
		return nil, fmt.Errorf("the step asked for a picture and %s answered with %s", where, mediaType)
	}
	// Half a file is not a file, and half a picture shown to a model is a
	// refused request rather than an answer.
	if cutShort {
		return nil, fmt.Errorf("the %s from %s is longer than the %d bytes this step reads; ask for less of it, or raise maxBytes on the step", wanted, where, most)
	}
	if len(answer) == 0 {
		return nil, fmt.Errorf("%s answered with an empty %s", where, wanted)
	}
	self.running.Files = append(self.running.Files, Fetched{
		Step: step.Name, MediaType: mediaType, Data: answer, Look: step.Result == ResultImage,
	})
	kept := map[string]any{
		"content_type": mediaType,
		"bytes":        len(answer),
	}
	if step.Result == ResultImage {
		kept["picture"] = "fetched; it is shown to you below and handed to the person"
	} else {
		kept["file"] = "fetched and handed to the person; you have not read it"
	}
	return kept, nil
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
	filled := reference.ReplaceAllStringFunc(hideDoubled(text), func(whole string) string {
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
		return scrubbed(asText(value))
	})
	return showDoubled(filled), failure
}

// scrubbed is a value with the byte the brace escape hides behind taken
// out of it.
//
// Without this a value could forge the escape. The escape turns \x00{ back
// into {{ on the way out, and a value is written in before that happens --
// so a parameter carrying \x00{ would arrive at the service as {{, which in
// a service whose payloads are templates is not a brace but a program. A
// skill that renders a fixed template with one word from the caller in it
// would run whatever that word said. The file itself cannot carry the byte,
// because Parse refuses one that does; this is the other way in.
func scrubbed(value string) string {
	if !strings.ContainsRune(value, 0) {
		return value
	}
	return strings.ReplaceAll(value, "\x00", "")
}

// fillURL writes the values into an address, escaping each one as it goes:
// a place name with a space in it would otherwise make a request no server
// answers, and a value carrying a ? or an & would add to the address
// rather than sit in it. A template that is one reference and nothing else
// is the address itself -- a step passing on a link an earlier step found
// -- and is left exactly as it came.
func (self *run) fillURL(text string) (string, error) {
	text = hideDoubled(text)
	if whole := strings.TrimSpace(text); reference.FindString(whole) == whole {
		return self.fill(showDoubled(whole))
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
		return escapeInURL(scrubbed(asText(value)))
	})
	return showDoubled(filled), failure
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
