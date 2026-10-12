package tools

import (
	"context"
	"encoding/json"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
	"strings"
	"testing"
)

func TestHostedToolsFollowServiceChoices(test *testing.T) {
	configuration := config.Default()
	configuration.Services = config.AssistantServices()
	for _, permission := range []models.Permission{models.PermissionMailRead, models.PermissionMailSend, models.PermissionMailAuditAll, models.PermissionDomainManageAll, models.PermissionQueueManage, models.PermissionReportRead, models.PermissionCalendarUse, models.PermissionContactsUse} {
		tool := &Tool{Name: "hosted", Permissions: []models.Permission{permission}}
		if err := RequireServices(tool, configuration); err == nil {
			test.Fatalf("disabled tool still offered for %s", permission)
		}
		tool.Family = FamilyServers
		if err := RequireServices(tool, configuration); err != nil {
			test.Fatalf("connected tool inherited hosted policy: %s", err)
		}
	}
	configuration.Services.Reminders.IsEnabled = new(true)
	if err := RequireServices(&Tool{Name: "reminder", Permissions: []models.Permission{models.PermissionCalendarUse}}, configuration); err != nil {
		test.Fatal(err)
	}
}

type serviceRun struct {
	permissioned
	configuration *config.Configuration
}

func (self *serviceRun) Configuration() *config.Configuration { return self.configuration }

func TestMergedToolsKeepIndependentActionsAndRejectCachedDisabledActions(test *testing.T) {
	configuration := config.Default()
	configuration.Services = config.AssistantServices()
	mailCallCount := 0
	merged := Merge(MergedTool{Name: "account-fixture", Family: FamilyAccount, Actions: []Action{
		{Name: "read", Tool: &Tool{Name: "profile-fixture", Risk: RiskRead, Run: func(context.Context, *Call) (*Result, error) { return TextResult("profile"), nil }}},
		{Name: "mail", Tool: &Tool{Name: "mail-fixture", Risk: RiskRead, Services: []models.Service{models.ServiceMailboxes}, Run: func(context.Context, *Call) (*Result, error) { mailCallCount++; return TextResult("mail"), nil }}},
	}})
	catalog := NewCatalog()
	catalog.Register(merged)
	permissions := heldBy(models.PermissionMailRead)
	offered := catalog.Offered(permissions, nil, &configuration.Services)
	if len(offered) != 1 {
		test.Fatal("disabled mail hid the account tool")
	}
	parameters, err := json.Marshal(offered[0].Parameters)
	if err != nil {
		test.Fatal(err)
	}
	if strings.Contains(string(parameters), `"mail"`) || !strings.Contains(string(parameters), `"read"`) {
		test.Fatal("discovery still advertised disabled actions")
	}
	run := &serviceRun{permissioned: permissioned{permissions: permissions}, configuration: configuration}
	ctx := WithRun(test.Context(), run)
	if _, err := merged.Run(ctx, &Call{Arguments: json.RawMessage(`{"action":"mail"}`)}); err == nil || mailCallCount != 0 {
		test.Fatal("a cached action bypassed the service switch")
	}
	if _, err := offered[0].Run(ctx, &Call{Arguments: json.RawMessage(`{"action":"read"}`)}); err != nil {
		test.Fatal(err)
	}
	configuration.Services.MailRouting.IsEnabled = new(true)
	configuration.Services.Mailboxes.IsEnabled = new(true)
	offered = catalog.Offered(permissions, nil, &configuration.Services)
	if _, err := offered[0].Run(ctx, &Call{Arguments: json.RawMessage(`{"action":"mail"}`)}); err != nil || mailCallCount != 1 {
		test.Fatal("re-enabling did not restore the original action")
	}
}
