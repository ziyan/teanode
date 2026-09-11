package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// The connected servers, from a terminal: which there are, and connecting
// one with your own credential. Authorizing one is done in the browser —
// the address is printed and the dashboard finishes it.

func newAgentMCPCommand() *cli.Command {
	return &cli.Command{
		Name:  "mcp",
		Usage: "the connected servers the operator declared, and your connections to them",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "the servers, and where you stand with each",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentMCPList,
			},
			{
				Name:      "connect",
				Usage:     "connect a server: --credential for one that takes yours (- reads it from stdin); an authorizing server prints the address to open",
				ArgsUsage: "<server>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "credential", Usage: "your credential for the server; - reads it from stdin"},
				},
				Action: runAgentMCPConnect,
			},
			{
				Name:      "disconnect",
				Usage:     "forget your credential or authorization for a server",
				ArgsUsage: "<server>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentMCPDisconnect,
			},
		},
	}
}

func runAgentMCPList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	servers, err := client.ListAgentServers(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(servers)
	}
	rows := make([][]string, 0, len(servers))
	for _, server := range servers {
		state := server.Status
		if state == "" {
			state = "no connection needed"
		}
		if !server.Enabled {
			state = "off"
		}
		rows = append(rows, []string{server.Name, server.Transport, server.Auth, state, server.LastError})
	}
	return printTable([]string{"server", "transport", "auth", "status", "error"}, rows)
}

func runAgentMCPConnect(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return fmt.Errorf("which server? teanode agent mcp list shows them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	servers, err := client.ListAgentServers(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	var server *client.AgentServer
	for _, candidate := range servers {
		if candidate.Name == name {
			server = candidate
		}
	}
	if server == nil {
		return fmt.Errorf("there is no server %q", name)
	}
	if server.Auth == "oauth" {
		address, err := client.BeginAgentServerOAuth(ctx, connection, name, strings.TrimSuffix(connection.URL(), "/")+"/agent?connect="+name)
		if err != nil {
			return describeError(command, err)
		}
		_, _ = fmt.Fprintf(command.Writer, "Open this address in your browser to authorize %s; the dashboard finishes the connection:\n\n%s\n", name, address)
		return nil
	}
	credential, err := readValue(command, command.String("credential"))
	if err != nil {
		return err
	}
	view, err := client.ConnectAgentServer(ctx, connection, name, credential)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	if view.Status == "error" {
		return fmt.Errorf("%s did not answer: %s", name, view.LastError)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s, %d tool(s)\n", name, view.Status, view.Tools)
	return nil
}

func runAgentMCPDisconnect(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return fmt.Errorf("which server? teanode agent mcp list shows them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.DisconnectAgentServer(ctx, connection, name)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s\n", name, view.Status)
	return nil
}
