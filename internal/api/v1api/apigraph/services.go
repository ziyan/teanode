package apigraph

import (
	"context"
	"reflect"

	"github.com/graphql-go/graphql"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// ServiceQuery describes hosted services without reading their collections.
type ServiceQuery interface {
	GetServiceCapabilities(context.Context) (*ServiceCapabilities, error)
}

// ServiceMutation changes operator choices through the configuration store.
type ServiceMutation interface {
	UpdateServices(context.Context, UpdateServicesArguments) (*ServiceCapabilities, error)
}

type ServiceCapabilities struct {
	IsMailRoutingEnabled     bool `json:"isMailRoutingEnabled"`
	IsMailboxEnabled         bool `json:"isMailboxEnabled"`
	IsCalendarEnabled        bool `json:"isCalendarEnabled"`
	IsRemindersEnabled       bool `json:"isRemindersEnabled"`
	IsContactsEnabled        bool `json:"isContactsEnabled"`
	IsAssistantEnabled       bool `json:"isAssistantEnabled"`
	HasPendingServiceRestart bool `json:"hasPendingServiceRestart"`
	HasCompletedSetup        bool `json:"hasCompletedSetup"`
}

type UpdateServicesArguments struct {
	MailServerName       *string `json:"mailServerName" graphapi:"nullable"`
	IsMailRoutingEnabled *bool   `json:"isMailRoutingEnabled" graphapi:"nullable"`
	IsMailboxEnabled     *bool   `json:"isMailboxEnabled" graphapi:"nullable"`
	IsCalendarEnabled    *bool   `json:"isCalendarEnabled" graphapi:"nullable"`
	IsRemindersEnabled   *bool   `json:"isRemindersEnabled" graphapi:"nullable"`
	IsContactsEnabled    *bool   `json:"isContactsEnabled" graphapi:"nullable"`
	HasCompletedSetup    *bool   `json:"hasCompletedSetup" graphapi:"nullable"`
}

func (self *graph) isServiceEnabled(service models.Service) bool {
	if self.config == nil {
		return true
	}
	if !self.config.Current().Services.IsEnabled(service) {
		return false
	}
	return self.settings == nil || self.settings.RunningServices == nil || self.settings.RunningServices.IsEnabled(service)
}

func (self *graph) requireService(service models.Service) error {
	if !self.isServiceEnabled(service) {
		return &models.ServiceUnavailableError{Service: service}
	}
	return nil
}

func (self *graph) GetServiceCapabilities(ctx context.Context) (*ServiceCapabilities, error) {
	if _, err := self.requireSignedIn(ctx); err != nil {
		return nil, err
	}
	return self.serviceCapabilities(), nil
}

func (self *graph) serviceCapabilities() *ServiceCapabilities {
	selected := self.config.Current().Services
	pending := false
	if self.settings != nil && self.settings.RunningServices != nil {
		for _, service := range []models.Service{models.ServiceMailRouting, models.ServiceMailboxes, models.ServiceCalendar, models.ServiceReminders, models.ServiceContacts} {
			pending = pending || selected.IsEnabled(service) != self.settings.RunningServices.IsEnabled(service)
		}
	}
	return &ServiceCapabilities{
		IsMailRoutingEnabled: self.isServiceEnabled(models.ServiceMailRouting), IsMailboxEnabled: self.isServiceEnabled(models.ServiceMailboxes),
		IsCalendarEnabled: self.isServiceEnabled(models.ServiceCalendar), IsRemindersEnabled: self.isServiceEnabled(models.ServiceReminders),
		IsContactsEnabled: self.isServiceEnabled(models.ServiceContacts), IsAssistantEnabled: self.config.Current().Agent.Enabled && self.settings != nil && self.agentWorker() != nil,
		HasPendingServiceRestart: pending, HasCompletedSetup: selected.HasCompletedSetup || (selected.MailRouting.IsEnabled == nil && selected.Mailboxes.IsEnabled == nil && selected.Calendar.IsEnabled == nil && selected.Reminders.IsEnabled == nil && selected.Contacts.IsEnabled == nil),
	}
}

func (self *graph) UpdateServices(ctx context.Context, arguments UpdateServicesArguments) (*ServiceCapabilities, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	if err := self.config.Update(func(configuration *config.Configuration) error {
		for _, change := range []struct {
			target   **bool
			selected *bool
		}{
			{&configuration.Services.MailRouting.IsEnabled, arguments.IsMailRoutingEnabled}, {&configuration.Services.Mailboxes.IsEnabled, arguments.IsMailboxEnabled},
			{&configuration.Services.Calendar.IsEnabled, arguments.IsCalendarEnabled}, {&configuration.Services.Reminders.IsEnabled, arguments.IsRemindersEnabled},
			{&configuration.Services.Contacts.IsEnabled, arguments.IsContactsEnabled},
		} {
			if change.selected != nil {
				*change.target = new(*change.selected)
			}
		}
		if arguments.MailServerName != nil {
			configuration.Server.Name = *arguments.MailServerName
		}
		if arguments.HasCompletedSetup != nil {
			configuration.Services.HasCompletedSetup = *arguments.HasCompletedSetup
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return self.GetServiceCapabilities(ctx)
}

// serviceOperations assigns requirements by the public operation interfaces.
// Keeping complete interfaces here makes a new operation inherit its service.
func serviceOperations() map[string]models.Service {
	operations := map[string]models.Service{}
	for _, group := range []struct {
		service    models.Service
		interfaces []reflect.Type
	}{
		{models.ServiceMailRouting, []reflect.Type{reflect.TypeOf((*DomainQuery)(nil)).Elem(), reflect.TypeOf((*DomainMutation)(nil)).Elem(), reflect.TypeOf((*AliasQuery)(nil)).Elem(), reflect.TypeOf((*AliasMutation)(nil)).Elem(), reflect.TypeOf((*CredentialQuery)(nil)).Elem(), reflect.TypeOf((*CredentialMutation)(nil)).Elem(), reflect.TypeOf((*SendQuery)(nil)).Elem(), reflect.TypeOf((*SendMutation)(nil)).Elem(), reflect.TypeOf((*MailQuery)(nil)).Elem(), reflect.TypeOf((*MailMutation)(nil)).Elem(), reflect.TypeOf((*ContentQuery)(nil)).Elem(), reflect.TypeOf((*DeliveryQuery)(nil)).Elem(), reflect.TypeOf((*DeliveryMutation)(nil)).Elem(), reflect.TypeOf((*ReportQuery)(nil)).Elem(), reflect.TypeOf((*ReportMutation)(nil)).Elem(), reflect.TypeOf((*LayoutQuery)(nil)).Elem(), reflect.TypeOf((*LayoutMutation)(nil)).Elem(), reflect.TypeOf((*TemplateQuery)(nil)).Elem(), reflect.TypeOf((*TemplateMutation)(nil)).Elem(), reflect.TypeOf((*SpamQuery)(nil)).Elem(), reflect.TypeOf((*SpamMutation)(nil)).Elem()}},
		{models.ServiceMailboxes, []reflect.Type{reflect.TypeOf((*MailboxQuery)(nil)).Elem(), reflect.TypeOf((*MailboxMutation)(nil)).Elem(), reflect.TypeOf((*MailboxComposeQuery)(nil)).Elem(), reflect.TypeOf((*MailboxComposeMutation)(nil)).Elem(), reflect.TypeOf((*MailboxRulesQuery)(nil)).Elem(), reflect.TypeOf((*MailboxAppPasswordQuery)(nil)).Elem(), reflect.TypeOf((*MailboxAppPasswordMutation)(nil)).Elem(), reflect.TypeOf((*MailboxSetupProfileMutation)(nil)).Elem(), reflect.TypeOf((*MailProgramQuery)(nil)).Elem(), reflect.TypeOf((*MailboxDirectoryQuery)(nil)).Elem(), reflect.TypeOf((*NoteQuery)(nil)).Elem(), reflect.TypeOf((*NoteMutation)(nil)).Elem(), reflect.TypeOf((*CalendarInvitationQuery)(nil)).Elem(), reflect.TypeOf((*CalendarInvitationMutation)(nil)).Elem()}},
		{models.ServiceContacts, []reflect.Type{reflect.TypeOf((*AddressBookQuery)(nil)).Elem(), reflect.TypeOf((*AddressBookMutation)(nil)).Elem()}},
		{models.ServiceCalendar, []reflect.Type{reflect.TypeOf((*CalendarQuery)(nil)).Elem(), reflect.TypeOf((*CalendarMutation)(nil)).Elem()}},
		{models.ServiceReminders, []reflect.Type{reflect.TypeOf((*ReminderQuery)(nil)).Elem(), reflect.TypeOf((*ReminderMutation)(nil)).Elem()}},
	} {
		for _, operationInterface := range group.interfaces {
			for index := 0; index < operationInterface.NumMethod(); index++ {
				operations[operationInterface.Method(index).Name] = group.service
			}
		}
	}
	// Metadata serves both event calendars and reminders; check their kind inside.
	delete(operations, "ListCalendars")
	delete(operations, "SaveCalendar")
	return operations
}

func (self *graph) wrapServiceOperations() {
	for operationName, service := range serviceOperations() {
		for _, root := range []*graphql.Object{self.schema.QueryType(), self.schema.MutationType()} {
			field := root.Fields()[operationName]
			if field == nil {
				continue
			}
			resolve := field.Resolve
			field.Resolve = func(parameters graphql.ResolveParams) (interface{}, error) {
				if _, err := self.requireSignedIn(parameters.Context); err != nil {
					return nil, err
				}
				if err := self.requireService(service); err != nil {
					return nil, err
				}
				return resolve(parameters)
			}
		}
	}
}

// ServiceSetupQuery returns desired operator choices without querying disabled services.
type ServiceSetupQuery interface {
	GetServiceSetup(context.Context) (*ServiceSetup, error)
}
type ServiceSetup struct {
	MailServerName   string               `json:"mailServerName"`
	SelectedServices *ServiceCapabilities `json:"selectedServices"`
	RunningServices  *ServiceCapabilities `json:"runningServices"`
	HasModelProvider bool                 `json:"hasModelProvider"`
	HasMailDomain    bool                 `json:"hasMailDomain"`
}

func (self *graph) GetServiceSetup(ctx context.Context) (*ServiceSetup, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	selected := configuration.Services
	choices := &ServiceCapabilities{IsMailRoutingEnabled: selected.IsEnabled(models.ServiceMailRouting), IsMailboxEnabled: selected.IsEnabled(models.ServiceMailboxes), IsCalendarEnabled: selected.IsEnabled(models.ServiceCalendar), IsRemindersEnabled: selected.IsEnabled(models.ServiceReminders), IsContactsEnabled: selected.IsEnabled(models.ServiceContacts), IsAssistantEnabled: configuration.Agent.Enabled, HasCompletedSetup: selected.HasCompletedSetup}
	setup := &ServiceSetup{MailServerName: configuration.Server.Name, SelectedServices: choices, RunningServices: self.serviceCapabilities(), HasModelProvider: len(configuration.Agent.Providers) > 0 && configuration.Agent.Models.Default != ""}
	if choices.IsMailRoutingEnabled {
		domains, err := self.transaction(ctx).ListDomains()
		if err != nil {
			return nil, err
		}
		setup.HasMailDomain = len(domains) > 0
	}
	return setup, nil
}
