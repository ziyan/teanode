package models

import "fmt"

// Service identifies an optional hosted service, independently of account permissions.
type Service string

const (
	ServiceMailRouting Service = "mailRouting"
	ServiceMailboxes   Service = "mailboxes"
	ServiceCalendar    Service = "calendar"
	ServiceReminders   Service = "reminders"
	ServiceContacts    Service = "contacts"
	ServicePhotos      Service = "photos"
)

// ServiceUnavailableError reports a disabled or not-yet-running service.
type ServiceUnavailableError struct{ Service Service }

func (self *ServiceUnavailableError) Error() string {
	return fmt.Sprintf("the %s service is unavailable on this server", self.Service)
}

// Extensions gives API clients a stable error code.
func (self *ServiceUnavailableError) Extensions() map[string]interface{} {
	return map[string]interface{}{"code": "SERVICE_UNAVAILABLE", "service": string(self.Service)}
}
