package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// StdioTransport runs a server as a subprocess and speaks to it over its
// standard input and output, one JSON message per line. Only the operator
// declares one, in the configuration: a subprocess runs as this server, on
// its host.
type StdioTransport struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Scanner

	writeMutex sync.Mutex
	pending    map[int64]chan *Response
	mutex      sync.Mutex
	closed     chan struct{}
	failure    error
}

// StdioSettings are what a subprocess needs.
type StdioSettings struct {
	Command    string
	Args       []string
	Env        map[string]string
	WorkingDir string
}

// NewStdioTransport starts the subprocess.
func NewStdioTransport(settings *StdioSettings) (*StdioTransport, error) {
	if strings.TrimSpace(settings.Command) == "" {
		return nil, fmt.Errorf("mcp: no command")
	}
	command := exec.Command(settings.Command, settings.Args...)
	command.Dir = settings.WorkingDir
	command.Env = os.Environ()
	for key, value := range settings.Env {
		command.Env = append(command.Env, key+"="+value)
	}
	command.Stderr = io.Discard
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("mcp: cannot start %s: %w", settings.Command, err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), 16<<20)
	self := &StdioTransport{command: command, stdin: stdin, stdout: scanner, pending: map[int64]chan *Response{}, closed: make(chan struct{})}
	go self.read()
	return self, nil
}

// read dispatches responses to the calls waiting for them.
func (self *StdioTransport) read() {
	defer close(self.closed)
	for self.stdout.Scan() {
		line := strings.TrimSpace(self.stdout.Text())
		if line == "" {
			continue
		}
		var response Response
		if err := json.Unmarshal([]byte(line), &response); err != nil || response.ID == nil {
			continue // a notification, or noise on stdout
		}
		self.mutex.Lock()
		channel, ok := self.pending[*response.ID]
		if ok {
			delete(self.pending, *response.ID)
		}
		self.mutex.Unlock()
		if ok {
			channel <- &response
		}
	}
	self.mutex.Lock()
	self.failure = fmt.Errorf("mcp: the server closed its output")
	for id, channel := range self.pending {
		delete(self.pending, id)
		close(channel)
	}
	self.mutex.Unlock()
}

func (self *StdioTransport) write(message any) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	_, err = self.stdin.Write(append(encoded, '\n'))
	return err
}

// Call writes a request and waits for its response.
func (self *StdioTransport) Call(ctx context.Context, request *Request) (*Response, error) {
	if request.ID == nil {
		return nil, fmt.Errorf("mcp: a request needs an id")
	}
	channel := make(chan *Response, 1)
	self.mutex.Lock()
	if self.failure != nil {
		self.mutex.Unlock()
		return nil, self.failure
	}
	self.pending[*request.ID] = channel
	self.mutex.Unlock()
	if err := self.write(request); err != nil {
		self.mutex.Lock()
		delete(self.pending, *request.ID)
		self.mutex.Unlock()
		return nil, err
	}
	select {
	case response, ok := <-channel:
		if !ok {
			return nil, fmt.Errorf("mcp: the server went away")
		}
		return response, nil
	case <-ctx.Done():
		self.mutex.Lock()
		delete(self.pending, *request.ID)
		self.mutex.Unlock()
		return nil, ctx.Err()
	}
}

// Notify writes a notification.
func (self *StdioTransport) Notify(ctx context.Context, notification *Request) error {
	return self.write(notification)
}

// Close ends the subprocess: its input is closed, and it is killed if it
// does not leave within a moment.
func (self *StdioTransport) Close() error {
	_ = self.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- self.command.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = self.command.Process.Kill()
		<-done
	}
	return nil
}
