// Package mailboxsettings renames a mailbox or sets its signature.
package mailboxsettings

import (
	"context"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "mailbox_settings", Family: tools.FamilyMailbox, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionMailboxManage},
				Description: "Rename a mailbox or set its signature.",
				Parameters: tools.Object(map[string]any{
					"mailbox":        mailbox.MailboxProperty,
					"name":           tools.StringProperty("the new name"),
					"signature_text": tools.StringProperty("the plain-text signature"),
				}),
				Run: runMailboxSettings,
			},
		}
	})
}

type mailboxSettingsArguments struct {
	Mailbox       string `json:"mailbox"`
	Name          string `json:"name"`
	SignatureText string `json:"signature_text"`
}

func runMailboxSettings(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[mailboxSettingsArguments](call)
	if err != nil {
		return nil, err
	}
	operations := run.Operations()
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	view, err := mailbox.FindMailbox(views, arguments.Mailbox)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{"mailboxId": view.Mailbox.ID}
	if strings.TrimSpace(arguments.Name) != "" {
		variables["name"] = strings.TrimSpace(arguments.Name)
	}
	if arguments.SignatureText != "" {
		variables["signatureText"] = arguments.SignatureText
	}
	if len(variables) == 1 {
		return nil, fmt.Errorf("nothing to change: give name or signature_text")
	}
	var discard map[string]any
	if err := operations.Execute(ctx, `mutation ($mailboxId: String!, $name: String, $signatureText: String) { UpdateMailbox(mailboxId: $mailboxId, name: $name, signatureText: $signatureText) { mailbox { id } } }`, variables, &discard); err != nil {
		return nil, err
	}
	return tools.TextResult("changed the settings of mailbox %q", view.Mailbox.Name), nil
}
