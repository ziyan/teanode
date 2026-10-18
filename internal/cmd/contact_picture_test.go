package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// Taking a picture off a contact asks the server to, only for a contact that
// has one, and says what it did.
func TestContactRemovePictureTakesOnlyThePictureOff(test *testing.T) {
	var removed []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var asked struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if json.NewDecoder(request.Body).Decode(&asked) != nil {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(asked.Query, "RemoveContactPicture("):
			removed = append(removed, asked.Variables["id"].(string))
			_, _ = response.Write([]byte(`{"data":{"RemoveContactPicture":{"id":"contact-1","name":"Example Aunt","emails":[],"phones":[],"hasPhoto":false}}}`))
		case strings.Contains(asked.Query, "GetContact("):
			hasPhoto := asked.Variables["id"] == "contact-1"
			_, _ = response.Write([]byte(`{"data":{"GetContact":{"id":"` + asked.Variables["id"].(string) + `","name":"Example Aunt","emails":[],"phones":[],"hasPhoto":` + map[bool]string{true: "true", false: "false"}[hasPhoto] + `}}}`))
		default:
			_, _ = response.Write([]byte(`{"data":{}}`))
		}
	}))
	test.Cleanup(server.Close)

	run := func(arguments ...string) (string, error) {
		var writer bytes.Buffer
		command := &cli.Command{
			Name: "fixture", Writer: &writer,
			Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "fixture-token"}},
			Commands: []*cli.Command{NewContactCommand()},
		}
		var runErr error
		output := captureStandardOutput(test, func() {
			runErr = command.Run(test.Context(), append([]string{"fixture", "contact"}, arguments...))
		})
		return output + writer.String(), runErr
	}

	// Without --force and without a terminal to ask on, nothing is sent.
	if _, err := run("remove-picture", "contact-1"); err == nil || len(removed) != 0 {
		test.Fatalf("%v %v", err, removed)
	}
	output, err := run("remove-picture", "contact-1", "--force")
	if err != nil || !strings.Contains(output, "took the picture off Example Aunt") || len(removed) != 1 || removed[0] != "contact-1" {
		test.Fatalf("%v: %s %v", err, output, removed)
	}
	// A contact with no picture is left alone.
	output, err = run("remove-picture", "contact-2", "--force")
	if err != nil || !strings.Contains(output, "has no picture") || len(removed) != 1 {
		test.Fatalf("%v: %s %v", err, output, removed)
	}
	// With --json both answers are the contact as JSON, whether or not there
	// was a picture to take off.
	for _, id := range []string{"contact-1", "contact-2"} {
		output, err = run("remove-picture", id, "--force", "--json")
		var answered struct {
			ID       string `json:"id"`
			HasPhoto bool   `json:"hasPhoto"`
		}
		if err != nil || json.Unmarshal([]byte(output), &answered) != nil || answered.ID == "" || answered.HasPhoto {
			test.Fatalf("%s: %v: %s", id, err, output)
		}
	}
	if len(removed) != 2 {
		test.Fatalf("a contact with no picture was asked to remove it: %v", removed)
	}
}
