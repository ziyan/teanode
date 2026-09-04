package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewDeliveryCommand builds "teanode delivery": what happened to each message
// on its way out, and the queue of what has not gone yet.
func NewDeliveryCommand() *cli.Command {
	return &cli.Command{
		Name:  "delivery",
		Usage: "the deliveries made for handled mail, and the queue",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list deliveries for a domain, or for one message",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "domain", Usage: "the domain whose deliveries to list"},
					&cli.StringFlag{Name: "mail", Usage: "the message whose deliveries to list"},
					&cli.IntFlag{Name: "first", Value: 50, Usage: "how many to show; 0 for every one"},
					JSONFlag(),
				},
				Action: runDeliveryList,
			},
			{
				Name:  "pending",
				Usage: "list what has not been delivered yet: queued, being retried, or waiting after a failure",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "domain", Usage: "only this domain; every domain unless given"},
					&cli.IntFlag{Name: "first", Value: 0, Usage: "how many to show; 0 for every one"},
					JSONFlag(),
				},
				Action: runDeliveryPending,
			},
			{
				Name:      "get",
				Aliases:   []string{"show"},
				Usage:     "show a delivery, including the last error",
				ArgsUsage: "<delivery-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runDeliveryGet,
			},
			{
				Name:      "retry",
				Usage:     "try a failed or delayed delivery again now",
				ArgsUsage: "<delivery-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runDeliveryRetry,
			},
		},
	}
}

func runDeliveryList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	var deliveries []*client.Delivery
	switch {
	case command.String("mail") != "":
		deliveries, err = client.ListDeliveriesByMail(ctx, connection, command.String("mail"))
	case command.String("domain") != "":
		domain, domainError := requireDomain(ctx, command, connection, command.String("domain"))
		if domainError != nil {
			return domainError
		}
		deliveries, err = client.ListDeliveries(ctx, connection, domain.ID, int(command.Int("first")))
	default:
		return fmt.Errorf("whose deliveries? pass --domain <domain> or --mail <mail-id>; 'teanode delivery pending' lists the queue")
	}
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(deliveries)
	}
	if len(deliveries) == 0 {
		fmt.Println("no deliveries")
		return nil
	}
	return printDeliveries(deliveries)
}

func runDeliveryPending(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domainId := ""
	if name := command.String("domain"); name != "" {
		domain, err := requireDomain(ctx, command, connection, name)
		if err != nil {
			return err
		}
		domainId = domain.ID
	}
	deliveries, err := client.ListPendingDeliveries(ctx, connection, domainId, int(command.Int("first")))
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(deliveries)
	}
	if len(deliveries) == 0 {
		fmt.Println("nothing is waiting; every delivery has been made or given up on")
		return nil
	}
	return printDeliveries(deliveries)
}

func printDeliveries(deliveries []*client.Delivery) error {
	rows := make([][]string, 0, len(deliveries))
	for _, delivery := range deliveries {
		rows = append(rows, []string{
			delivery.ID, delivery.MailID, delivery.Kind, delivery.Recipient, delivery.Status,
			fmt.Sprint(delivery.Attempts), formatTime(delivery.RetryAt), truncate(delivery.Error, 50),
		})
	}
	return printTable([]string{"ID", "MAIL", "KIND", "RECIPIENT", "STATUS", "ATTEMPTS", "NEXT TRY", "ERROR"}, rows)
}

func runDeliveryGet(ctx context.Context, command *cli.Command) error {
	deliveryId := command.Args().First()
	if deliveryId == "" {
		return fmt.Errorf("which delivery? usage: teanode delivery get <delivery-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	delivery, err := client.GetDelivery(ctx, connection, deliveryId)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(delivery)
	}
	return printFields([][2]string{
		{"id", delivery.ID},
		{"mail", delivery.MailID},
		{"domain", delivery.DomainID},
		{"alias", delivery.AliasID},
		{"kind", delivery.Kind},
		{"recipient", delivery.Recipient},
		{"status", delivery.Status},
		{"size", fmt.Sprintf("%d bytes", delivery.Size)},
		{"attempts", fmt.Sprint(delivery.Attempts)},
		{"created", formatTime(&delivery.CreatedAt)},
		{"last attempt", formatTime(delivery.AttemptedAt)},
		{"delivered", formatTime(delivery.DeliveredAt)},
		{"dropped", formatTime(delivery.DroppedAt)},
		{"next try", formatTime(delivery.RetryAt)},
		{"error", delivery.Error},
	})
}

func runDeliveryRetry(ctx context.Context, command *cli.Command) error {
	deliveryId := command.Args().First()
	if deliveryId == "" {
		return fmt.Errorf("which delivery? usage: teanode delivery retry <delivery-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	delivery, err := client.RetryDelivery(ctx, connection, deliveryId)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(delivery)
	}
	fmt.Printf("delivery %s to %s will be tried again shortly\n", delivery.ID, delivery.Recipient)
	return nil
}
