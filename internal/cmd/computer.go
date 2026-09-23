package cmd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/sources"
)

// NewComputerCommand builds "teanode computer": the program that attaches
// this computer to the person's agent, so that the agent's shell and
// filesystem tools reach it while they talk. It signs in with the active
// profile's token — the person's, from "teanode auth login" — never the
// server's own.
func NewComputerCommand() *cli.Command {
	options := []cli.Flag{
		&cli.StringFlag{Name: "name", Usage: "what to call this computer to the agent; the host name by default"},
		&cli.StringFlag{Name: "description", Usage: "what this computer is and what it is for, in a sentence, so the agent can tell it from your others"},
		&cli.BoolFlag{Name: "verbose", Usage: "say what the agent asks for and how long each request takes"},
	}
	return &cli.Command{
		Name:  "computer",
		Usage: "attach this computer to your agent, so it can run commands and read files here while you talk to it",
		Description: "While the program runs, your agent has two more tools: shell, which runs a command here, and\n" +
			"filesystem, which reads, writes, lists and searches your files — as you, anywhere on this\n" +
			"machine, the way a terminal of yours would. Only a conversation you are present in may use\n" +
			"them, and what changes the machine or reaches out of it asks you first. 'start' runs the\n" +
			"program in the background and 'stop' ends it; 'daemon' is the same program in the foreground,\n" +
			"for a terminal or a service manager.",
		Commands: []*cli.Command{
			{
				Name:   "daemon",
				Usage:  "run in the foreground, reconnecting when the connection drops, until interrupted",
				Flags:  options,
				Action: runComputerDaemon,
			},
			{
				Name:   "start",
				Usage:  "run in the background",
				Flags:  options,
				Action: runComputerStart,
			},
			{
				Name:      "try-source",
				Usage:     "run a source type here and print the records it reads, without the server",
				ArgsUsage: "<source.md>",
				Description: "Runs a source type the way a pass does -- lists its containers, then reads\n" +
					"them -- on this computer, and prints each record as a JSON line, with the container\n" +
					"it came from as \"container\", and a summary at\n" +
					"the end. For trying a type against the tool it calls before installing it. What\n" +
					"the type keeps between passes goes in a directory of its own, left behind to look at.",
				Flags: []cli.Flag{
					&cli.StringSliceFlag{Name: "setting", Usage: "a setting, name=value; a list setting takes a comma-separated value"},
					&cli.IntFlag{Name: "containers", Value: 2, Usage: "how many containers to read; 0 lists them and reads none"},
					&cli.IntFlag{Name: "records", Value: 5, Usage: "how many records of each container to print; -1 prints every one"},
					&cli.StringSliceFlag{Name: "only", Usage: "read only the container of this name (repeatable), however many --containers says"},
					&cli.StringFlag{Name: "state", Usage: "where the type keeps what it knows between runs; a new directory by default"},
				},
				Action: runComputerTrySource,
			},
			{
				Name:   "status",
				Usage:  "say whether the program runs here and whether the server sees it",
				Action: runComputerStatus,
			},
			{
				Name:   "stop",
				Usage:  "end the program running in the background",
				Action: runComputerStop,
			},
		},
	}
}

// computerFiles are where the background program keeps its pid and its
// log: beside the profiles.
func computerFiles() (pidPath, logPath string, err error) {
	profiles, err := ProfilesPath()
	if err != nil {
		return "", "", err
	}
	directory := filepath.Dir(profiles)
	return filepath.Join(directory, "computer.pid"), filepath.Join(directory, "computer.log"), nil
}

// computerTarget is the server and the token the program signs in with.
// The local profile is the server's own console, which is nobody: a
// computer belongs to a person.
func computerTarget(command *cli.Command) (*target, error) {
	resolved, err := resolveCommandTarget(command)
	if err != nil {
		return nil, err
	}
	if resolved.Local {
		return nil, usage("the computer signs in as you, not as the server: sign in with 'teanode auth login', or pass --url and --token")
	}
	return resolved, nil
}

func runComputerDaemon(ctx context.Context, command *cli.Command) error {
	resolved, err := computerTarget(command)
	if err != nil {
		return err
	}
	options := &computer.Options{Token: resolved.Token, Name: command.String("name"), Description: command.String("description")}
	// One for the life of the program rather than one per connection: a
	// build left running in the background outlives a network that drops,
	// and ends when the program does.
	options.Background = computer.NewBackgroundCommands()
	defer options.Background.Close()
	// What it was asked and how long that took, beside where it connected.
	// A daemon that logs only its connections leaves the server's "did
	// not answer within ten minutes" with nothing to check it against.
	if command.Bool("verbose") {
		options.Notice = func(text string) {
			_, _ = fmt.Fprintf(command.ErrWriter, "%s %s\n", time.Now().Format(time.RFC3339), text)
		}
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	address := strings.Replace(strings.Replace(client.NormalizeURL(resolved.URL), "https://", "wss://", 1), "http://", "ws://", 1) + "/api/v1/agent/computer"
	dialer := *websocket.DefaultDialer
	if resolved.Insecure {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // the person asked for it, for a development server
	}
	wait := time.Second
	for {
		connection, response, err := dialer.DialContext(ctx, address, http.Header{"User-Agent": {"teanode computer"}})
		if err == nil {
			_, _ = fmt.Fprintf(command.ErrWriter, "%s connected to %s\n", time.Now().Format(time.RFC3339), resolved.URL)
			err = computer.Serve(ctx, connection, options)
			_ = connection.Close()
			wait = time.Second
		} else if response != nil {
			// A server that says no before the connection is one that
			// will say no again: a feature the operator switched off, a
			// sign-in it does not take.
			if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("%s answered %s; attaching a computer is off on this server, or the sign-in is not taken", resolved.URL, response.Status)
			}
			err = fmt.Errorf("%s answered %s", resolved.URL, response.Status)
		}
		var refused *computer.RefusedError
		switch {
		case ctx.Err() != nil:
			_, _ = fmt.Fprintln(command.ErrWriter, "stopped")
			return nil
		case errors.As(err, &refused):
			// Not worth trying again: the answer will be the same.
			return refused
		case err != nil:
			_, _ = fmt.Fprintf(command.ErrWriter, "%s %s; trying again in %s\n", time.Now().Format(time.RFC3339), forTerminal(err.Error()), wait)
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			_, _ = fmt.Fprintln(command.ErrWriter, "stopped")
			return nil
		}
		wait = min(wait*2, time.Minute)
	}
}

func runComputerStart(ctx context.Context, command *cli.Command) error {
	if _, err := computerTarget(command); err != nil {
		return err
	}
	pidPath, logPath, err := computerFiles()
	if err != nil {
		return err
	}
	if pid, running := runningComputer(pidPath); running {
		_, _ = fmt.Fprintf(command.Writer, "already running, pid %d\n", pid)
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()
	arguments := []string{"computer", "daemon"}
	root := command.Root()
	for _, flag := range []string{"url", "profile"} {
		if value := root.String(flag); value != "" {
			arguments = append(arguments, "--"+flag, value)
		}
	}
	// A token never goes on a command line, where every local user can
	// read it: it reaches the program through its environment.
	environment := os.Environ()
	if token := root.String("token"); token != "" {
		environment = append(environment, "TEANODE_TOKEN="+token)
	}
	if root.Bool("insecure") {
		arguments = append(arguments, "--insecure")
	}
	for _, flag := range []string{"name", "description"} {
		if value := command.String(flag); value != "" {
			arguments = append(arguments, "--"+flag, value)
		}
	}
	child := exec.Command(executable, arguments...)
	child.Stdout, child.Stderr = logFile, logFile
	child.Env = environment
	detach(child)
	if err := child.Start(); err != nil {
		return err
	}
	pid := child.Process.Pid
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		return err
	}
	// Let go of it: the program outlives this command.
	_ = child.Process.Release()
	_, _ = fmt.Fprintf(command.Writer, "started, pid %d; its log is %s\n", pid, logPath)
	return nil
}

func runComputerStatus(ctx context.Context, command *cli.Command) error {
	pidPath, _, err := computerFiles()
	if err != nil {
		return err
	}
	if pid, running := runningComputer(pidPath); running {
		_, _ = fmt.Fprintf(command.Writer, "running here, pid %d\n", pid)
	} else {
		_, _ = fmt.Fprintln(command.Writer, "not running here")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.ReadAgentComputers(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	switch {
	case !view.Allowed:
		_, _ = fmt.Fprintln(command.Writer, "the server does not let people attach a computer")
	case len(view.Computers) == 0:
		_, _ = fmt.Fprintln(command.Writer, "the server sees no computer of yours")
	default:
		for _, computer := range view.Computers {
			_, _ = fmt.Fprintf(command.Writer, "the server sees %s (%s) since %s\n", computer.Name, computer.System, computer.Since.Local().Format("2006-01-02 15:04"))
			if computer.Description != "" {
				_, _ = fmt.Fprintf(command.Writer, "  %s\n", computer.Description)
			}
		}
	}
	return nil
}

func runComputerStop(ctx context.Context, command *cli.Command) error {
	pidPath, _, err := computerFiles()
	if err != nil {
		return err
	}
	pid, running := runningComputer(pidPath)
	if !running {
		_, _ = fmt.Fprintln(command.Writer, "not running")
		return nil
	}
	if err := stopProcess(pid); err != nil {
		return err
	}
	for waited := 0; waited < 50 && processAlive(pid); waited++ {
		time.Sleep(100 * time.Millisecond)
	}
	_ = os.Remove(pidPath)
	_, _ = fmt.Fprintln(command.Writer, "stopped")
	return nil
}

// runningComputer reads the pid file and says whether that process is
// alive; a stale file is removed.
func runningComputer(pidPath string) (int, bool) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		_ = os.Remove(pidPath)
		return 0, false
	}
	if !processAlive(pid) {
		_ = os.Remove(pidPath)
		return pid, false
	}
	return pid, true
}

// runComputerTrySource runs a source type here, as a pass would, and
// prints what it reads.
func runComputerTrySource(ctx context.Context, command *cli.Command) error {
	path := command.Args().First()
	if path == "" {
		return usage("which type? usage: teanode computer try-source <source.md>")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	kind, err := sources.Parse(content)
	if err != nil {
		return err
	}
	if kind.Reader != "" {
		return fmt.Errorf("%s is read by the %s reader built into TeaNode; there is nothing to try here", kind.Name, kind.Reader)
	}
	settings := map[string]any{}
	for _, pair := range command.StringSlice("setting") {
		name, value, found := strings.Cut(pair, "=")
		if !found {
			return usage(fmt.Sprintf("%q is not name=value", pair))
		}
		settings[name] = value
		for _, setting := range kind.Settings {
			if setting.Name != name {
				continue
			}
			switch setting.Type {
			case "array":
				var list []any
				for _, each := range strings.Split(value, ",") {
					if each = strings.TrimSpace(each); each != "" {
						list = append(list, each)
					}
				}
				settings[name] = list
			case "boolean":
				settings[name] = value == "true" || value == "yes" || value == "1"
			case "integer":
				number, err := strconv.Atoi(value)
				if err != nil {
					return usage(fmt.Sprintf("%s is a whole number", name))
				}
				settings[name] = number
			}
		}
	}
	state := command.String("state")
	if state == "" {
		if state, err = os.MkdirTemp("", "teanode-source-"+kind.Name+"-"); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	runner := &sources.Runner{Type: kind, Settings: settings, Executor: computer.NewLocalExecutor(state), State: state}
	containers, err := runner.List(ctx)
	if err != nil {
		return fmt.Errorf("listing: %w", err)
	}
	if err := runner.SaveContainers(containers); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d containers\n", len(containers))
	for index, container := range containers {
		fmt.Fprintf(os.Stderr, "  %s\n", container.Name)
		if index >= 20 {
			fmt.Fprintf(os.Stderr, "  and %d more\n", len(containers)-index-1)
			break
		}
	}
	only := map[string]bool{}
	for _, name := range command.StringSlice("only") {
		only[name] = true
	}
	read := 0
	for _, container := range containers {
		if len(only) > 0 {
			if !only[container.Name] {
				continue
			}
		} else if read >= int(command.Int("containers")) {
			break
		}
		read++
		records, err := runner.Read(ctx, container)
		if err != nil && !errors.Is(err, sources.ErrUnfinished) {
			return fmt.Errorf("reading %s: %w", container.Name, err)
		}
		fmt.Fprintf(os.Stderr, "%s: %d records\n", container.Name, len(records))
		for index, record := range records {
			if limit := int(command.Int("records")); limit >= 0 && index >= limit {
				break
			}
			record["container"] = container.Name
			encoded, _ := json.Marshal(record)
			fmt.Println(string(encoded))
		}
	}
	fmt.Fprintf(os.Stderr, "state kept in %s\n", state)
	return nil
}
