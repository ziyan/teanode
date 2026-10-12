package tools

import (
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// RequiredServices is the hosted policy of an internal tool. Remote tools do
// not inherit hosted-service requirements from their names.
func RequiredServices(tool *Tool) []models.Service {
	if tool.Family == FamilyServers || tool.Family == FamilySkills {
		return nil
	}
	if len(tool.Services) > 0 {
		return tool.Services
	}
	if tool.Name == "reminder" {
		return []models.Service{models.ServiceReminders}
	}
	services := []models.Service{}
	for _, permission := range tool.Permissions {
		var service models.Service
		switch permission {
		case models.PermissionCalendarUse:
			service = models.ServiceCalendar
		case models.PermissionContactsUse:
			service = models.ServiceContacts
		case models.PermissionMailRead, models.PermissionMailWrite, models.PermissionMailSend, models.PermissionMailboxManage:
			service = models.ServiceMailboxes
		case models.PermissionMailAudit, models.PermissionMailAuditAll, models.PermissionDomainManage, models.PermissionDomainManageAll, models.PermissionQueueManage, models.PermissionReportRead:
			service = models.ServiceMailRouting
		}
		if service != "" {
			services = append(services, service)
		}
	}
	return services
}

// RequireServices checks current operator choices again at invocation.
func RequireServices(tool *Tool, configuration *config.Configuration) error {
	for _, service := range RequiredServices(tool) {
		if err := configuration.RequireService(service); err != nil {
			return err
		}
	}
	return nil
}
