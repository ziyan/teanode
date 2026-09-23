package computer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/sources"
)

// A typed source is read by running a source type: the YAML file that says
// which commands and requests list a source's containers and read their
// records. It is the records format without a script. The server sends the
// type with every request, so this program runs whatever type the server
// holds and nothing is installed here; what the runner keeps between
// passes -- when each container was last read, the records a reading that
// reads only what changed has collected, fetched text and files -- lives
// under the source's own directory in the person's cache.

// FormatTyped is a source read by running its type.
const FormatTyped = "typed"

// How long a reading may run before it stops and says it is unfinished:
// the first page of a pass is waited for longest.
const (
	typedFirstPageTime = 25 * time.Minute
	typedPageTime      = 6 * time.Minute
)

// typedCommandTime is the longest one command a type runs may take.
const typedCommandTime = 10 * time.Minute

// typedOutputBytes is the most a command may print or a request answer.
const typedOutputBytes = 512 << 20

// sourceKeyPattern is what a source's directory may be called.
var sourceKeyPattern = regexp.MustCompile(`^[0-9a-z]{1,64}$`)

// typedSource is one pass of a typed source.
type typedSource struct {
	runner       *sources.Runner
	containers   map[string]sources.Container
	isUnfinished bool
}

func openTyped(root string, arguments *ScanArguments) (*typedSource, error) {
	if !sourceKeyPattern.MatchString(arguments.SourceKey) {
		return nil, fmt.Errorf("a typed source needs a key naming its directory")
	}
	parsed, err := sources.Parse([]byte(arguments.SourceType))
	if err != nil {
		return nil, err
	}
	if parsed.Reader != "" {
		return nil, fmt.Errorf("%s is read by the %s reader, not run", parsed.Name, parsed.Reader)
	}
	if !parsed.RunsOn(sources.RunsComputer) {
		return nil, fmt.Errorf("%s runs on the server, not on a computer", parsed.Name)
	}
	for _, tool := range parsed.Requires {
		if _, err := exec.LookPath(tool); err != nil {
			return nil, fmt.Errorf("%s needs %s, which is not installed on this computer (or not on its PATH)", parsed.Name, tool)
		}
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	if err := forgetOtherSettings(root, arguments.SourceType, arguments.Settings); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(typedPageTime)
	if arguments.After == "" {
		deadline = time.Now().Add(typedFirstPageTime)
	}
	return &typedSource{runner: &sources.Runner{
		Type: parsed, Settings: arguments.Settings, Secrets: arguments.Secrets,
		Executor: &localExecutor{directory: root}, State: root, Deadline: deadline,
	}}, nil
}

// names is the pass's containers: listed on its first page and kept, so
// the pages after it read what the first page listed.
func (self *typedSource) names(ctx context.Context, first bool) ([]string, error) {
	var listed []sources.Container
	var err error
	if !first {
		listed, err = self.runner.LoadContainers()
	}
	if first || err != nil {
		if listed, err = self.runner.List(ctx); err != nil {
			return nil, err
		}
		if err := self.runner.SaveContainers(listed); err != nil {
			return nil, err
		}
	}
	self.containers = map[string]sources.Container{}
	names := make([]string, 0, len(listed))
	for _, container := range listed {
		name := recordsName(container.Name)
		if name == "" {
			continue
		}
		self.containers[name] = container
		names = append(names, name)
	}
	return names, nil
}

// read reads one container as the records reader reads a script's file.
func (self *typedSource) read(ctx context.Context, folder *recordsFolder, relative string) ([]ScanEntry, error) {
	container, ok := self.containers[relative]
	if !ok {
		return nil, fmt.Errorf("%s was not in this pass's listing", relative)
	}
	records, err := self.runner.Read(ctx, container)
	if errors.Is(err, sources.ErrUnfinished) {
		self.isUnfinished = true
	} else if err != nil {
		return nil, err
	}
	var lines bytes.Buffer
	for _, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		lines.Write(encoded)
		lines.WriteByte('\n')
	}
	return readRecords(ctx, folder, relative, &lines)
}

// localExecutor runs a type's commands on this computer, as the person,
// and makes its requests from here.
type localExecutor struct {
	directory string
}

func (self *localExecutor) Command(ctx context.Context, words []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, typedCommandTime)
	defer cancel()
	command := exec.CommandContext(ctx, words[0], words[1:]...)
	command.Dir = self.directory
	// The person's own environment: the tools a type calls are signed in
	// as them and read their configuration.
	command.Env = os.Environ()
	command.WaitDelay = 2 * time.Second
	prepare(command)
	var output bytes.Buffer
	command.Stdout = &limitedWriter{writer: &output, remaining: typedOutputBytes}
	tail := &refreshTail{limit: refreshTailBytes}
	command.Stderr = tail
	err := command.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, &sources.CommandError{Words: words, ExitCode: -1, Said: fmt.Sprintf("did not finish within %s", typedCommandTime)}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil, &sources.CommandError{Words: words, ExitCode: exit.ExitCode(), Said: strings.TrimSpace(string(tail.held))}
	}
	if err != nil {
		return nil, fmt.Errorf("%s could not be run: %w", words[0], err)
	}
	return output.Bytes(), nil
}

func (self *localExecutor) Request(ctx context.Context, request *sources.PreparedRequest) (int, []byte, error) {
	return DoRequest(ctx, request)
}

// DoRequest makes a type's web request, from wherever it runs.
func DoRequest(ctx context.Context, request *sources.PreparedRequest) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var body io.Reader
	if request.Body != "" {
		body = strings.NewReader(request.Body)
	}
	prepared, err := http.NewRequestWithContext(ctx, request.Method, request.URL, body)
	if err != nil {
		return 0, nil, fmt.Errorf("the request could not be made")
	}
	for key, value := range request.Headers {
		prepared.Header.Set(key, value)
	}
	response, err := typedRequestClient.Do(prepared)
	if err != nil {
		// Said without the address, which may carry what a type put in
		// it, since this text is shown on the source and to the agent.
		var failed *url.Error
		if errors.As(err, &failed) {
			return 0, nil, fmt.Errorf("the request to %s failed: %w", prepared.URL.Host, failed.Err)
		}
		return 0, nil, fmt.Errorf("the request to %s failed", prepared.URL.Host)
	}
	defer func() { _ = response.Body.Close() }()
	content, err := io.ReadAll(io.LimitReader(response.Body, typedOutputBytes))
	return response.StatusCode, content, err
}

// typedRequestClient follows a redirect only on the same scheme and host:
// a type's credential goes where the type said, and a service that answers
// with an address elsewhere does not decide otherwise.
var typedRequestClient = &http.Client{
	CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		first := via[0].URL
		if request.URL.Scheme != first.Scheme || request.URL.Host != first.Host {
			return fmt.Errorf("a redirect to %s is not followed", request.URL.Host)
		}
		return nil
	},
}

// limitedWriter keeps at most so many bytes, and says so when there was
// more, since a listing cut short must never pass as a whole one.
type limitedWriter struct {
	writer    io.Writer
	remaining int64
}

func (self *limitedWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > self.remaining {
		return 0, fmt.Errorf("the command printed more than %d bytes", typedOutputBytes)
	}
	self.remaining -= int64(len(data))
	return self.writer.Write(data)
}

// forgetOtherSettings clears what a source's directory knows when it was
// learned under another type, another version of it, or other settings: the listing, when each
// container was last read, and the records a reading kept. A mailbox read
// as one account and then pointed at another must not keep reporting the
// first account's threads, and a reading that starts from where the last
// one stopped must start again. Fetched files and text stay: their names
// already say which item and version they are. A directory from before
// this was written down is taken to be the current settings'.
func forgetOtherSettings(root, sourceType string, settings map[string]any) error {
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(append([]byte(sourceType+"\x00"), encoded...))
	fingerprint := hex.EncodeToString(sum[:])
	path := filepath.Join(root, "settings.sha256")
	previous, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(previous)) != fingerprint {
		for _, name := range []string{"containers.json", "since.json", "store"} {
			if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
				return err
			}
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte(fingerprint+"\n"), 0o600)
}

// typedRootFor resolves a typed source's directory under the person's
// home.
func typedRootFor(options *Options, sourceKey string) string {
	return filepath.Join(options.Home, ".cache", "teanode", "sources", sourceKey)
}

// NewLocalExecutor runs a type's commands on this computer from a
// directory, for trying a type out by hand.
func NewLocalExecutor(directory string) sources.Executor {
	return &localExecutor{directory: directory}
}
