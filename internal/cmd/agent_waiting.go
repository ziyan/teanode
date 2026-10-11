package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// newAgentWaitingCommand builds "teanode agent waiting": the questions and
// approvals the agent waits on in any conversation, and answering them.
func newAgentWaitingCommand() *cli.Command {
	return &cli.Command{
		Name:  "waiting",
		Usage: "the questions and approvals your agent waits on, in every conversation, newest first",
		Flags: []cli.Flag{
			JSONFlag(),
			&cli.IntFlag{Name: "first", Usage: "how many to list, at most 50", Value: 20},
		},
		Action: runAgentWaitingList,
		Commands: []*cli.Command{
			{
				Name:      "approve",
				Usage:     "approve what the agent asked to do",
				ArgsUsage: "<interaction-id>",
				Action: func(ctx context.Context, command *cli.Command) error {
					return runAgentWaitingResolve(ctx, command, true)
				},
			},
			{
				Name:      "deny",
				Usage:     "refuse what the agent asked to do",
				ArgsUsage: "<interaction-id>",
				Action: func(ctx context.Context, command *cli.Command) error {
					return runAgentWaitingResolve(ctx, command, false)
				},
			},
			{
				Name:      "answer",
				Usage:     "answer a question the agent asked",
				ArgsUsage: "<interaction-id> <answer>",
				Action:    runAgentWaitingAnswer,
			},
		},
	}
}

func runAgentWaitingList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	waiting, err := client.ReadAgentWaiting(ctx, connection, int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(waiting)
	}
	if waiting.WaitingCount == 0 {
		fmt.Println("nothing waits for you")
		return nil
	}
	rows := make([][]string, 0, len(waiting.Interactions))
	for _, interaction := range waiting.Interactions {
		text := interaction.InteractionText
		if len(interaction.InteractionChoices) > 0 {
			text += " [" + strings.Join(interaction.InteractionChoices, " / ") + "]"
		}
		rows = append(rows, []string{interaction.InteractionKind, forTerminal(text), formatTime(&interaction.CreatedAt), interaction.ID})
	}
	if err := printTable([]string{"KIND", "WAITING ON", "SINCE", "ID"}, rows); err != nil {
		return err
	}
	if waiting.WaitingCount > len(waiting.Interactions) {
		fmt.Printf("and %d more; --first lists more\n", waiting.WaitingCount-len(waiting.Interactions))
	}
	return nil
}

// findWaiting is the waiting card with this id, among the fifty newest.
func findWaiting(ctx context.Context, command *cli.Command, connection *client.Client, interactionId string) (*client.AgentWaitingInteraction, error) {
	waiting, err := client.ReadAgentWaiting(ctx, connection, 50)
	if err != nil {
		return nil, describeError(command, err)
	}
	for _, interaction := range waiting.Interactions {
		if interaction.ID == interactionId {
			return interaction, nil
		}
	}
	return nil, fmt.Errorf("nothing waiting has the id %s; 'teanode agent waiting' lists them", interactionId)
}

func runAgentWaitingResolve(ctx context.Context, command *cli.Command, isApproved bool) error {
	interactionId := command.Args().First()
	if interactionId == "" {
		return usage("which one? usage: teanode agent waiting approve|deny <interaction-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	interaction, err := findWaiting(ctx, command, connection, interactionId)
	if err != nil {
		return err
	}
	if interaction.InteractionKind != "approval" {
		return usage("that is a question; answer it with 'teanode agent waiting answer <interaction-id> <answer>'")
	}
	if _, err := client.ResolveAgentConfirmation(ctx, connection, interaction.RunID, interaction.CallID, isApproved); err != nil {
		return describeError(command, err)
	}
	if isApproved {
		fmt.Println("approved")
	} else {
		fmt.Println("refused")
	}
	return nil
}

func runAgentWaitingAnswer(ctx context.Context, command *cli.Command) error {
	interactionId := command.Args().Get(0)
	answer := strings.TrimSpace(strings.Join(command.Args().Slice()[min(1, command.Args().Len()):], " "))
	if interactionId == "" || answer == "" {
		return usage("usage: teanode agent waiting answer <interaction-id> <answer>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	interaction, err := findWaiting(ctx, command, connection, interactionId)
	if err != nil {
		return err
	}
	if interaction.InteractionKind != "question" {
		return usage("that asks for approval; use 'teanode agent waiting approve' or 'deny'")
	}
	if _, err := client.AnswerAgentQuestion(ctx, connection, interaction.RunID, interaction.CallID, answer); err != nil {
		return describeError(command, err)
	}
	fmt.Println("answered")
	return nil
}
