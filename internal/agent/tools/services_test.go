package tools

import (
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
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
