package config

import "github.com/ziyan/teanode/internal/models"

// HostedService preserves the behavior of configurations written before service switches.
type HostedService struct {
	IsEnabled *bool `yaml:"isEnabled,omitempty" json:"isEnabled"`
}

// Enabled resolves an omitted switch to the legacy enabled behavior.
func (self HostedService) Enabled() bool { return self.IsEnabled == nil || *self.IsEnabled }

// Services selects the hosted services this deployment offers.
type Services struct {
	MailRouting       HostedService `yaml:"mailRouting" json:"mailRouting"`
	Mailboxes         HostedService `yaml:"mailboxes" json:"mailboxes"`
	Calendar          HostedService `yaml:"calendar" json:"calendar"`
	Reminders         HostedService `yaml:"reminders" json:"reminders"`
	Contacts          HostedService `yaml:"contacts" json:"contacts"`
	HasCompletedSetup bool          `yaml:"hasCompletedSetup,omitempty" json:"hasCompletedSetup"`
}

// IsEnabled reports whether the requested hosted service is selected.
func (self Services) IsEnabled(service models.Service) bool {
	switch service {
	case models.ServiceMailRouting:
		return self.MailRouting.Enabled()
	case models.ServiceMailboxes:
		return self.MailRouting.Enabled() && self.Mailboxes.Enabled()
	case models.ServiceCalendar:
		return self.Calendar.Enabled()
	case models.ServiceReminders:
		return self.Reminders.Enabled()
	case models.ServiceContacts:
		return self.Contacts.Enabled()
	default:
		return false
	}
}

// AssistantServices explicitly disables hosted services for a new assistant installation.
func AssistantServices() Services {
	return Services{MailRouting: HostedService{IsEnabled: new(false)}, Mailboxes: HostedService{IsEnabled: new(false)},
		Calendar: HostedService{IsEnabled: new(false)}, Reminders: HostedService{IsEnabled: new(false)}, Contacts: HostedService{IsEnabled: new(false)}}
}

// Within limits selected services to those constructed by this process.
func (self Services) Within(running Services) Services {
	self.MailRouting.IsEnabled = new(self.IsEnabled(models.ServiceMailRouting) && running.IsEnabled(models.ServiceMailRouting))
	self.Mailboxes.IsEnabled = new(self.IsEnabled(models.ServiceMailboxes) && running.IsEnabled(models.ServiceMailboxes))
	self.Calendar.IsEnabled = new(self.IsEnabled(models.ServiceCalendar) && running.IsEnabled(models.ServiceCalendar))
	self.Reminders.IsEnabled = new(self.IsEnabled(models.ServiceReminders) && running.IsEnabled(models.ServiceReminders))
	self.Contacts.IsEnabled = new(self.IsEnabled(models.ServiceContacts) && running.IsEnabled(models.ServiceContacts))
	return self
}

// RequireService refuses an unavailable hosted service before any side effect.
func (self *Configuration) RequireService(service models.Service) error {
	if !self.Services.IsEnabled(service) {
		return &models.ServiceUnavailableError{Service: service}
	}
	return nil
}
