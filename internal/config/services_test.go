package config

import (
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"testing"
)

func TestHostedServiceDefaultsPreserveOldConfigurations(test *testing.T) {
	configuration, err := FromRows(&db.ConfigurationRows{Settings: map[string]string{}}, nil)
	if err != nil {
		test.Fatal(err)
	}
	for _, service := range []models.Service{models.ServiceMailRouting, models.ServiceMailboxes, models.ServiceCalendar, models.ServiceReminders, models.ServiceContacts} {
		if !configuration.Services.IsEnabled(service) {
			test.Fatalf("legacy service disabled: %s", service)
		}
	}
}
func TestAssistantConfigurationIgnoresInactiveMailSettings(test *testing.T) {
	configuration := Default()
	configuration.Services = AssistantServices()
	configuration.Listen.HTTPS = ""
	configuration.TLS.ACME.Enabled = false
	configuration.Listen.SMTPIncoming = "not a listener"
	configuration.Listen.SMTPOutgoing = "not a listener"
	configuration.SMTP.MaxMessageSize = 0
	configuration.DKIM.Selector = ""
	configuration.Antivirus.Enabled = true
	configuration.Antivirus.Host = ""
	configuration.Antivirus.Port = 0
	if err := configuration.Validate(); err != nil {
		test.Fatal(err)
	}
	rows, err := ToRows(configuration, 1, nil)
	if err != nil {
		test.Fatal(err)
	}
	again, err := FromRows(rows, nil)
	if err != nil {
		test.Fatal(err)
	}
	if again.Services.IsEnabled(models.ServiceMailRouting) || again.Services.IsEnabled(models.ServiceContacts) {
		test.Fatal("explicit disabled services did not round trip")
	}
	again.Services.Mailboxes.IsEnabled = new(true)
	if again.Validate() == nil {
		test.Fatal("mailboxes without routing were accepted")
	}
}

func TestHostedServiceIsChosenOnOnlyWhenSwitchedOn(test *testing.T) {
	if (HostedService{}).IsChosenOn() {
		test.Fatal("a service nobody chose counted as chosen")
	}
	if !(HostedService{}).Enabled() {
		test.Fatal("a service nobody chose stopped being on by default")
	}
	if (HostedService{IsEnabled: new(false)}).IsChosenOn() {
		test.Fatal("a service switched off counted as chosen on")
	}
	if !(HostedService{IsEnabled: new(true)}).IsChosenOn() {
		test.Fatal("a service switched on did not count as chosen")
	}
}
