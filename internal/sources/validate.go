package sources

import (
	"fmt"
	"regexp"
	"strings"
)

// scopeNames are the names a template may begin with, besides settings.
var scopeNames = map[string]bool{
	"container": true, "item": true, "each": true, "parent": true, "folder": true,
	"response": true, "detail": true, "output": true, "pass": true, "record": true,
	"lookup": true, "file": true,
}

var (
	settingTypes  = map[string]bool{"string": true, "path": true, "array": true, "boolean": true, "integer": true}
	parseKinds    = map[string]bool{"json": true, "xml": true, "jsonl": true, "lines": true, "markdown": true, "text": true}
	pagingKinds   = map[string]bool{"": true, "none": true, "all": true, "token": true, "limit": true, "offset": true}
	settingName   = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
	knownReaders  = map[string]bool{ReaderFiles: true, ReaderJournal: true, ReaderSent: true, ReaderWeb: true}
	knownRunsOn   = map[string]bool{RunsComputer: true, RunsServer: true}
	shellCommands = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true}
)

// validate refuses a type that could not be run as written, before
// anybody adds a source of it.
func (self *Type) validate() error {
	if !namePattern.MatchString(self.Name) || len(self.Name) > 64 {
		return fmt.Errorf("the name %q is not lowercase letters, digits and hyphens", self.Name)
	}
	if strings.TrimSpace(self.Description) == "" {
		return fmt.Errorf("a type says what it reads in its description")
	}
	for _, where := range self.Runs {
		if !knownRunsOn[where] {
			return fmt.Errorf("runs says %q, which is not computer or server", where)
		}
	}
	settings := map[string]Setting{}
	for _, setting := range self.Settings {
		if !settingName.MatchString(setting.Name) {
			return fmt.Errorf("the setting %q is not a name", setting.Name)
		}
		if _, twice := settings[setting.Name]; twice {
			return fmt.Errorf("the setting %q is declared twice", setting.Name)
		}
		if !settingTypes[setting.Type] {
			return fmt.Errorf("the setting %q has the type %q", setting.Name, setting.Type)
		}
		for _, pattern := range []string{setting.Pattern, itemPattern(setting)} {
			if pattern != "" {
				if _, err := regexp.Compile(pattern); err != nil {
					return fmt.Errorf("the setting %q: %w", setting.Name, err)
				}
			}
		}
		settings[setting.Name] = setting
	}
	secrets := map[string]bool{}
	for _, secret := range self.Secrets {
		if secret.Key == "" || secrets[secret.Key] {
			return fmt.Errorf("a secret has no key, or one declared twice")
		}
		secrets[secret.Key] = true
	}

	if self.Reader != "" {
		if !knownReaders[self.Reader] {
			return fmt.Errorf("%q is not a reader TeaNode has", self.Reader)
		}
		if len(self.Containers) > 0 || len(self.Records) > 0 {
			return fmt.Errorf("a type that names a reader says nothing of how to list or read")
		}
		return nil
	}

	if len(self.Containers) == 0 || len(self.Records) == 0 {
		return fmt.Errorf("a type says how to list its containers and how to read them")
	}
	if self.Pace != "" {
		if _, err := parseDuration(self.Pace); err != nil {
			return fmt.Errorf("pace: %w", err)
		}
	}
	if self.RunsCommands() && self.RunsOn(RunsServer) {
		return fmt.Errorf("a type that runs commands runs on a computer, never on the server")
	}

	check := func(where, template string, allowed map[string]bool) error {
		parsed, err := compileTemplate(template)
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		names, secretKeys := parsed.roots()
		for _, name := range names {
			if setting, isSetting := strings.CutPrefix(name, "settings."); isSetting {
				if _, ok := settings[setting]; !ok {
					return fmt.Errorf("%s refers to the setting %q, which is not declared", where, setting)
				}
				continue
			}
			if name == "settings" {
				return fmt.Errorf("%s refers to settings without saying which", where)
			}
			if name == "lookup" && len(self.Lookups) > 0 && allowed != nil && allowed["lookup"] {
				continue
			}
			if !scopeNames[name] || name == "lookup" || (allowed != nil && !allowed[name]) {
				return fmt.Errorf("%s refers to %q, which is not there", where, name)
			}
		}
		for _, key := range secretKeys {
			if !secrets[key] {
				return fmt.Errorf("%s refers to the secret %q, which is not declared", where, key)
			}
		}
		return nil
	}
	checkCondition := func(where, text string, allowed map[string]bool) error {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		if _, err := compileCondition(text); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		for _, template := range templateOperand.FindAllString(text, -1) {
			if err := check(where, template, allowed); err != nil {
				return err
			}
		}
		return nil
	}
	checkFiles := func(where string, files *Files, allowed map[string]bool) error {
		if strings.TrimSpace(files.In) == "" {
			return fmt.Errorf("%s: files names the directory they are in", where)
		}
		if strings.Contains(files.Match, "..") {
			return fmt.Errorf("%s: files match inside their directory", where)
		}
		for _, template := range []string{files.In, files.Match} {
			if err := check(where, template, allowed); err != nil {
				return err
			}
		}
		return nil
	}
	checkCall := func(where string, command []string, request *Request, shape Parsing, paging Paging, allowed map[string]bool) error {
		if (len(command) > 0) == (request != nil) {
			return fmt.Errorf("%s gives a command, a request or files, one of them", where)
		}
		if err := checkCommand(where, command); err != nil {
			return err
		}
		for _, word := range command {
			if err := check(where, word, allowed); err != nil {
				return err
			}
		}
		if request != nil {
			if err := self.checkRequest(where, request, secrets, check, allowed); err != nil {
				return err
			}
		}
		if !parseKinds[shape.Kind] {
			return fmt.Errorf("%s: parse is %q", where, shape.Kind)
		}
		if shape.Kind == "lines" {
			if _, err := regexp.Compile(shape.Pattern); err != nil || shape.Pattern == "" {
				return fmt.Errorf("%s: lines needs a pattern that compiles", where)
			}
		}
		if !pagingKinds[paging.Kind] {
			return fmt.Errorf("%s: paging is %q", where, paging.Kind)
		}
		if paging.Kind == "token" && (paging.Field == "" || paging.Flag == "") {
			return fmt.Errorf("%s: token paging names the field and the flag", where)
		}
		if paging.Kind == "limit" && paging.Size <= 0 {
			return fmt.Errorf("%s: limit paging says the size of a full page", where)
		}
		if paging.Kind == "offset" && (paging.Size <= 0 || paging.Flag == "") {
			return fmt.Errorf("%s: offset paging names the flag that says where to start and the size of a full page", where)
		}
		return nil
	}

	for index, refresh := range self.Refresh {
		where := fmt.Sprintf("refresh %d", index+1)
		if len(refresh.Command) == 0 {
			return fmt.Errorf("%s runs no command", where)
		}
		if err := checkCommand(where, refresh.Command); err != nil {
			return err
		}
		for _, word := range refresh.Command {
			if err := check(where, word, map[string]bool{}); err != nil {
				return err
			}
		}
		if err := checkCondition(where, refresh.When, map[string]bool{}); err != nil {
			return err
		}
	}
	for _, name := range sortedKeys(self.Lookups) {
		lookup := self.Lookups[name]
		where := "the lookup " + name
		if !settingName.MatchString(name) {
			return fmt.Errorf("%s is not a name", where)
		}
		if (lookup.File != "") == (lookup.Files != nil) {
			return fmt.Errorf("%s reads a file or the names of files, one of them", where)
		}
		if lookup.File != "" {
			if !parseKinds[lookup.Parse.Kind] {
				return fmt.Errorf("%s: parse is %q", where, lookup.Parse.Kind)
			}
			if err := check(where, lookup.File, map[string]bool{}); err != nil {
				return err
			}
		} else if err := checkFiles(where, lookup.Files, map[string]bool{}); err != nil {
			return err
		}
		if lookup.Key == "" {
			return fmt.Errorf("%s says what its key is", where)
		}
		for _, template := range []string{lookup.Key, lookup.Value} {
			if err := check(where, template, map[string]bool{"item": true}); err != nil {
				return err
			}
		}
	}

	listings := map[string]bool{}
	for index, listing := range self.Containers {
		where := fmt.Sprintf("listing %d", index+1)
		allowed := map[string]bool{"item": true, "response": true, "lookup": true}
		if err := checkCondition(where, listing.When, map[string]bool{}); err != nil {
			return err
		}
		switch {
		case listing.Each != "":
			allowed["each"] = true
			if err := check(where, "{{"+listing.Each+"}}", map[string]bool{}); err != nil {
				return err
			}
		case listing.Over != "":
			if !listings[listing.Over] {
				return fmt.Errorf("%s lists over %q, which is not an earlier listing", where, listing.Over)
			}
			allowed["parent"] = true
		}
		if listing.Walk != nil {
			allowed["folder"] = true
			for _, template := range listing.Walk.Start {
				if err := check(where, template, map[string]bool{}); err != nil {
					return err
				}
			}
			if strings.TrimSpace(listing.Walk.Child["id"]) == "" || strings.TrimSpace(listing.Walk.Start["id"]) == "" {
				return fmt.Errorf("%s walks places without saying what tells them apart: start and child each need an id", where)
			}
			for _, template := range listing.Walk.Child {
				if err := check(where, template, allowed); err != nil {
					return err
				}
			}
			if err := checkCondition(where, listing.Walk.Branch, allowed); err != nil {
				return err
			}
			for _, template := range []string{listing.Walk.Step, listing.Walk.Distinct} {
				if err := check(where, template, allowed); err != nil {
					return err
				}
			}
		}
		switch {
		case listing.Fixed != nil:
		case listing.Files != nil:
			if len(listing.Command) > 0 || listing.Request != nil {
				return fmt.Errorf("%s gives a command, a request or files, one of them", where)
			}
			if err := checkFiles(where, listing.Files, allowed); err != nil {
				return err
			}
		default:
			if err := checkCall(where, listing.Command, listing.Request, listing.Parse, listing.Paging, allowed); err != nil {
				return err
			}
		}
		if err := checkCondition(where, listing.Skip, allowed); err != nil {
			return err
		}
		if listing.Name == "" && listing.Only != "parents" {
			return fmt.Errorf("%s gives its containers no name", where)
		}
		if err := check(where, listing.Name, allowed); err != nil {
			return err
		}
		for _, template := range listing.Fields {
			if err := check(where, template, allowed); err != nil {
				return err
			}
		}
		if listing.ID != "" {
			listings[listing.ID] = true
		}
	}

	for index, reading := range self.Records {
		where := fmt.Sprintf("reading %d", index+1)
		allowed := map[string]bool{"container": true, "item": true, "response": true, "each": true, "pass": true, "lookup": true}
		switch {
		case reading.File != "" || reading.Files != nil:
			if len(reading.Command) > 0 || reading.Request != nil || (reading.File != "" && reading.Files != nil) {
				return fmt.Errorf("%s gives a command, a request, a file or files, one of them", where)
			}
			if !parseKinds[reading.Parse.Kind] {
				return fmt.Errorf("%s: parse is %q", where, reading.Parse.Kind)
			}
			if reading.Files != nil {
				if err := checkFiles(where, reading.Files, allowed); err != nil {
					return err
				}
				allowed["file"] = true
			} else if err := check(where, reading.File, allowed); err != nil {
				return err
			}
		default:
			if err := checkCall(where, reading.Command, reading.Request, reading.Parse, reading.Paging, allowed); err != nil {
				return err
			}
		}
		if reading.DropWhenMostly != nil {
			if reading.DropWhenMostly.Share <= 0 || reading.DropWhenMostly.Share >= 1 {
				return fmt.Errorf("%s: dropWhenMostly takes a share between 0 and 1", where)
			}
			if err := checkCondition(where, reading.DropWhenMostly.Items, allowed); err != nil {
				return err
			}
		}
		if err := checkCondition(where, reading.Skip, allowed); err != nil {
			return err
		}
		if reading.Record["id"] == "" {
			return fmt.Errorf("%s gives its records no id", where)
		}
		withDetail := map[string]bool{"detail": true, "record": true}
		for key, value := range allowed {
			withDetail[key] = value
		}
		for field, template := range reading.Record {
			if field == "private" {
				if err := checkCondition(where+" private", template, withDetail); err != nil {
					return err
				}
				continue
			}
			if err := check(where+" "+field, template, withDetail); err != nil {
				return err
			}
		}
		for field, template := range reading.Metadata {
			if err := check(where+" metadata "+field, template, withDetail); err != nil {
				return err
			}
		}
		if reading.Since != nil {
			if err := check(where+" since", reading.Since.First, allowed); err != nil {
				return err
			}
			if reading.Since.Window != "" {
				if _, err := parseDuration(reading.Since.Window); err != nil {
					return fmt.Errorf("%s: %w", where, err)
				}
			}
			if err := checkCondition(where, reading.Since.UnchangedWhen, allowed); err != nil {
				return err
			}
		}
		if reading.Unseen != "" && reading.Unseen != "keep" && reading.Unseen != "delete" {
			return fmt.Errorf("%s: unseen is keep or delete", where)
		}
		if reading.Detail != nil {
			// Fetched once for each version of an item: an item with no
			// version is fetched once, ever, and an edit never reaches it.
			if strings.TrimSpace(reading.Record["version"]) == "" {
				return fmt.Errorf("%s fetches a detail, so its records say their version", where)
			}
			shape := reading.Detail.Parse
			if shape.Kind == "" {
				shape.Kind = "text"
			}
			withOutput := map[string]bool{"output": true}
			for key, value := range withDetail {
				withOutput[key] = value
			}
			if err := checkCall(where+" detail", reading.Detail.Command, reading.Detail.Request, shape, Paging{}, withOutput); err != nil {
				return err
			}
			if err := checkCondition(where+" detail", reading.Detail.When, allowed); err != nil {
				return err
			}
			if err := check(where+" detail", reading.Detail.Text, withDetail); err != nil {
				return err
			}
		}
		for _, attachment := range reading.Attachments {
			withOutput := map[string]bool{"output": true, "record": true}
			for key, value := range allowed {
				withOutput[key] = value
			}
			given := 0
			for _, present := range []bool{len(attachment.Command) > 0, attachment.Path != "", attachment.Content != ""} {
				if present {
					given++
				}
			}
			if given != 1 {
				return fmt.Errorf("%s: an attachment is a command, a path or content, one of them", where)
			}
			for _, template := range []string{attachment.Path, attachment.Content, attachment.Name, attachment.Version} {
				if err := check(where+" attachment", template, withOutput); err != nil {
					return err
				}
			}
			if err := checkCommand(where+" attachment", attachment.Command); err != nil {
				return err
			}
			for _, word := range attachment.Command {
				if err := check(where+" attachment", word, withOutput); err != nil {
					return err
				}
			}
			if err := checkCondition(where+" attachment", attachment.When, allowed); err != nil {
				return err
			}
		}
	}
	return nil
}

var templateOperand = regexp.MustCompile(`\{\{[^}]*\}\}`)

func itemPattern(setting Setting) string {
	if setting.Items == nil {
		return ""
	}
	return setting.Items.Pattern
}

// checkCommand refuses a script given to a shell with a template inside
// it: a value filled into a script is code, whatever its setting's
// pattern says.
func checkCommand(where string, command []string) error {
	if len(command) == 0 {
		return nil
	}
	if strings.Contains(command[0], "{{") {
		return fmt.Errorf("%s: the program a command runs is written into the type", where)
	}
	if shellCommands[command[0]] {
		for index, word := range command {
			if word == "-c" && index+1 < len(command) && strings.Contains(command[index+1], "{{") {
				return fmt.Errorf("%s: a template inside a shell script is code; pass the value as an argument", where)
			}
		}
	}
	return nil
}

// checkRequest keeps a request's credential to a host the type settled:
// written into the type, or from a secret. A person's own secret may go
// to an address in that person's settings; an operator's may not.
func (self *Type) checkRequest(where string, request *Request, secrets map[string]bool, check func(string, string, map[string]bool) error, allowed map[string]bool) error {
	for _, template := range append([]string{request.URL, request.Body}, valuesOf(request.Headers)...) {
		if err := check(where, template, allowed); err != nil {
			return err
		}
	}
	// A secret is sent only by an authentication profile, which is what
	// ties it to the address checked below; one written into a header or
	// the body would go wherever the address pointed.
	for _, template := range append([]string{request.Body}, valuesOf(request.Headers)...) {
		if strings.Contains(template, "secret:") {
			return fmt.Errorf("%s puts a secret in a header or the body; use an authentication profile", where)
		}
	}
	if strings.Contains(request.URL, "secret:") && request.Auth == "" {
		return fmt.Errorf("%s puts a secret in the address; use an authentication profile", where)
	}
	if request.Auth == "" {
		return nil
	}
	profile, ok := self.AuthenticationProfiles[request.Auth]
	if !ok {
		return fmt.Errorf("%s uses the authentication profile %q, which is not declared", where, request.Auth)
	}
	for _, template := range []string{profile.Token, profile.Username, profile.Password, profile.Value, profile.Key} {
		if err := check(where, template, map[string]bool{}); err != nil {
			return err
		}
	}
	// Where a credential goes is its host, so the host is what is checked:
	// written into the type, taken from a secret, or -- for a person's own
	// secrets only -- from a setting that person filled in. Anything a tool
	// answered never names it.
	address := strings.TrimSpace(request.URL)
	if strings.HasPrefix(address, "http://") {
		return fmt.Errorf("%s sends a credential over plain http", where)
	}
	host := address
	if rest, isHTTPS := strings.CutPrefix(address, "https://"); isHTTPS {
		host = rest
	}
	if end := strings.IndexAny(host, "/?#"); end >= 0 {
		host = host[:end]
	}
	switch {
	case !strings.Contains(host, "{{"):
		if !strings.HasPrefix(address, "https://") {
			return fmt.Errorf("%s sends a credential to an address the type does not settle", where)
		}
		return nil
	case templateOperand.ReplaceAllString(host, "") != "" && !strings.HasPrefix(address, "https://"):
		return fmt.Errorf("%s sends a credential to an address the type does not settle", where)
	}
	for _, template := range templateOperand.FindAllString(host, -1) {
		inner := strings.TrimSpace(strings.Trim(template, "{}"))
		switch {
		case strings.HasPrefix(inner, "secret:"):
		case strings.HasPrefix(inner, "settings.") && !strings.Contains(inner, "|"):
			for _, secret := range self.Secrets {
				if secret.Scope != "person" {
					return fmt.Errorf("%s sends the operator's secret %q to an address from a setting", where, secret.Key)
				}
			}
		default:
			return fmt.Errorf("%s sends a credential to an address the type does not settle", where)
		}
	}
	return nil
}

func valuesOf(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for _, key := range sortedKeys(values) {
		result = append(result, values[key])
	}
	return result
}
