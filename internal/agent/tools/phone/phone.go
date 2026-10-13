// Package phone is the person's phones running the TeaNode app, for the
// agent: which there are and whether notifications reach them, naming them,
// choosing what each is told, a test notification, and removing one. It
// also takes the person to where they connect a phone or set one up for
// mail, calendars, reminders and contacts, rather than doing either itself: a connect
// code and a setup profile carry what signs in as the person, and the model
// never sees either.
//
// They are actions of the account tool (internal/agent/tools/account),
// which lists them in its group.
package phone

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
	"github.com/ziyan/teanode/internal/client"
)

// Where the person does what the agent may not do for them: the page that
// shows a code to scan, and the account page that makes a setup profile.
// The dashboard lets the agent take it to both (web/src/components/dashboardPath.ts).
const (
	ConnectPath      = "/connect"
	SetupProfilePath = "/settings/sync"
)

// Tools are the phone actions, for the account tool to group.
func Tools() []*tools.Tool {
	phoneId := tools.StringProperty("the phone, by the id phones gives")
	return []*tools.Tool{
		{
			Name: "phone_list", Family: tools.FamilyAccount, Risk: tools.RiskRead,
			Description: "The person's phones running the TeaNode app: each one, whether notifications reach it and why not, the kinds it is told, and when it was last seen. Use it to answer \"why does my phone not buzz?\".",
			Parameters:  tools.Object(map[string]any{}),
			Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
				return operate(ctx, client.DocumentListAgentPhones, map[string]any{}, "ListAgentPhones", "")
			},
		},
		{
			Name: "phone_pair", Family: tools.FamilyAccount, Risk: tools.RiskRead,
			Description: "Take the dashboard to the page with a code to scan with the phone's camera, which connects the TeaNode app. Use it when they ask to connect or set up a phone. You never see the code.",
			Parameters:  tools.Object(map[string]any{}),
			Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
				return show(ctx, ConnectPath,
					"the page with the code to scan is open in the dashboard; it works for ten minutes. Tell them to open the Camera on the phone, scan it, and open TeaNode",
					"Open Settings, then Phones, then Connect a phone, in the dashboard on a computer, and scan the code it shows with the phone's camera")
			},
		},
		{
			Name: "phone_setup_profile", Family: tools.FamilyAccount, Risk: tools.RiskRead,
			Description: "Take them to account device setup, where they choose available Mail, Calendar, Reminders and Contacts for an iPhone or Mac. Mail is optional and collection setup requires no mailbox. They create the private profile themselves; you never receive its link or passwords.",
			Parameters:  tools.Object(map[string]any{}),
			Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
				return show(ctx, SetupProfilePath,
					"device setup is open; tell them to press Set up an iPhone or Mac, choose the available services, and leave Mail off if they only want Calendar, Reminders or Contacts",
					"Open Settings, then Device setup, and press Set up an iPhone or Mac. Choose available services; Calendar, Reminders and Contacts do not require a mailbox")
			},
		},
		{
			Name: "phone_rename", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
			Description: "Name a phone.",
			Parameters:  tools.Object(map[string]any{"phone_id": phoneId, "phone_name": tools.StringProperty("what the phone is called")}, "phone_id", "phone_name"),
			Preview: func(arguments json.RawMessage) string {
				asked, _ := decode(arguments)
				return fmt.Sprintf("Call a phone %q", asked.PhoneName)
			},
			Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
				asked, err := decode(call.Arguments)
				if err != nil {
					return nil, err
				}
				if asked.PhoneID == "" || asked.PhoneName == "" {
					return nil, fmt.Errorf("give phone_id, from phones, and phone_name")
				}
				return operate(ctx, client.DocumentUpdateAgentPhone, map[string]any{"phoneId": asked.PhoneID, "phoneName": asked.PhoneName}, "UpdateAgentPhone", "renamed")
			},
		},
		{
			Name: "phone_notify", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
			Description: "Set the kinds of notification a phone gets, from the kinds phones lists; every kind is all of them.",
			Parameters: tools.Object(map[string]any{
				"phone_id":           phoneId,
				"notification_kinds": tools.ArrayProperty("the kinds it should get", tools.StringProperty("a kind of notification")),
			}, "phone_id", "notification_kinds"),
			Preview: func(arguments json.RawMessage) string {
				asked, _ := decode(arguments)
				return "Notify a phone of " + strings.Join(asked.NotificationKinds, ", ")
			},
			Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
				asked, err := decode(call.Arguments)
				if err != nil {
					return nil, err
				}
				if asked.PhoneID == "" || len(asked.NotificationKinds) == 0 {
					return nil, fmt.Errorf("give phone_id, from phones, and notification_kinds")
				}
				return operate(ctx, client.DocumentUpdateAgentPhone, map[string]any{"phoneId": asked.PhoneID, "notificationKinds": asked.NotificationKinds},
					"UpdateAgentPhone", "the phone is told these kinds from now on")
			},
		},
		{
			Name: "phone_test", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
			Description: "Send a phone a test notification, to check it arrives.",
			Parameters:  tools.Object(map[string]any{"phone_id": phoneId}, "phone_id"),
			Preview:     func(json.RawMessage) string { return "Send a test notification to a phone" },
			Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
				asked, err := decode(call.Arguments)
				if err != nil {
					return nil, err
				}
				if asked.PhoneID == "" {
					return nil, fmt.Errorf("which phone? give phone_id, from phones")
				}
				return operate(ctx, client.DocumentTestAgentPhone, map[string]any{"phoneId": asked.PhoneID}, "TestAgentPhone", "a test notification is on its way; ask whether it arrived")
			},
		},
		{
			Name: "phone_remove", Family: tools.FamilyAccount, Risk: tools.RiskDestructive,
			Description: "Forget a phone and sign it out; the app on it asks to be connected again.",
			Parameters:  tools.Object(map[string]any{"phone_id": phoneId}, "phone_id"),
			Preview:     func(json.RawMessage) string { return "Remove a phone and sign it out of TeaNode" },
			Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
				asked, err := decode(call.Arguments)
				if err != nil {
					return nil, err
				}
				if asked.PhoneID == "" {
					return nil, fmt.Errorf("which phone? give phone_id, from phones")
				}
				return operate(ctx, client.DocumentRemoveAgentPhone, map[string]any{"phoneId": asked.PhoneID}, "RemoveAgentPhone", "the phone is removed and signed out")
			},
		},
	}
}

type phoneArguments struct {
	PhoneID           string   `json:"phone_id"`
	PhoneName         string   `json:"phone_name"`
	NotificationKinds []string `json:"notification_kinds"`
}

func decode(arguments json.RawMessage) (*phoneArguments, error) {
	var asked phoneArguments
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &asked); err != nil {
			return &asked, err
		}
	}
	asked.PhoneID = strings.TrimSpace(asked.PhoneID)
	asked.PhoneName = strings.TrimSpace(asked.PhoneName)
	return &asked, nil
}

// show takes the dashboard the person reads this in to a page, or says
// where to find it when there is no dashboard to take: a chat app, the
// command line, Siri, a mail, or a turn the agent started itself.
func show(ctx context.Context, path, shownNote, elsewhere string) (*tools.Result, error) {
	showing, ok := tools.MustRun(ctx).(tools.Showing)
	if !ok || !showing.CanShowPages() {
		return &tools.Result{Content: fmt.Sprintf(`{"shown": false, "where": %q}`, elsewhere)}, nil
	}
	showing.ShowPage(path)
	return &tools.Result{Content: fmt.Sprintf(`{"shown": %q, "note": %q}`, path, shownNote), Note: "Opened " + path}, nil
}

func operate(ctx context.Context, document string, variables map[string]any, operation, note string) (*tools.Result, error) {
	result, err := operator.Execute(ctx, document, variables)
	if err != nil {
		return nil, err
	}
	answer, err := tools.JSONResult(result[operation])
	if err != nil {
		return nil, err
	}
	answer.Note = note
	return answer, nil
}
