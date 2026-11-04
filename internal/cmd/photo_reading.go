package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// Reading new photos: each new picture described as it arrives, and the
// receipts, statements and tax documents in them recorded.

func newPhotoReadingCommand() *cli.Command {
	return &cli.Command{
		Name:  "reading",
		Usage: "whether your new photos are described as they arrive, and the receipts, statements and tax documents in them recorded",
		Commands: []*cli.Command{
			{Name: "on", Usage: "describe each new picture and video taken in the last 30 days as it arrives, and record the receipts, statements and tax documents in them", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoReading},
			{Name: "off", Usage: "stop reading new photos; what was read is kept", Flags: []cli.Flag{JSONFlag()}, Action: runPhotoReading},
		},
	}
}

// runPhotoReading is reading on and reading off.
func runPhotoReading(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 0 {
		return usage(fmt.Sprintf("usage: teanode photo reading %s", command.Name))
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	isNowEnabled, err := client.SetPhotoReading(ctx, connection, command.Name == "on")
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]bool{"isPhotoReadingEnabled": isNowEnabled})
	}
	if isNowEnabled {
		_, _ = fmt.Fprintln(command.Writer, "reading your new photos: each is described as it arrives, and a receipt, a statement or a tax document in one is recorded")
		return nil
	}
	_, _ = fmt.Fprintln(command.Writer, "no longer reading your new photos; what was read is kept")
	return nil
}

// photoReadingText is whether new photos are read, as teanode photo library
// says it.
func photoReadingText(library *client.PhotoLibrary) string {
	switch {
	case !library.IsDescriptionAvailable:
		return "not available: " + library.DescriptionUnavailableMessage
	case !library.IsPhotoReadingEnabled:
		return "off (teanode photo reading on)"
	}
	return "on"
}

// photoDocumentText is what a described photo was read as, empty for a
// picture that is no document.
func photoDocumentText(documentKind models.PhotoDocumentKind) string {
	switch documentKind {
	case models.PhotoDocumentReceipt:
		return "a receipt (teanode finance receipts)"
	case models.PhotoDocumentStatement:
		return "a statement (teanode finance transactions)"
	case models.PhotoDocumentTaxDocument:
		return "a tax document (teanode finance paystubs, teanode finance tax-forms)"
	}
	return ""
}
