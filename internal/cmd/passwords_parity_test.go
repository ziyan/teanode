package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/client"
)

// Every operation on Passwords is on the API and on the command line, under the
// same names, and nothing on the command line is not an operation. The agent
// has no tool for Passwords yet; it gets its own table when it does.
func TestPasswordsParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.PasswordsQuery](), reflect.TypeFor[apigraph.PasswordsMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}

	// Each operation, then the subcommands of teanode passwords that send it.
	table := map[string][]string{
		"ReadPasswordsSetup":           {"settings"},
		"SetUpPasswords":               {"setup"},
		"UnlockPasswords":              {"unlock"},
		"ChangePasswordsPassphrase":    {"change-passphrase"},
		"ReplacePasswordsRecoveryKey":  {"new-recovery-key"},
		"UpdatePasswordsSettings":      {"settings"},
		"ErasePasswords":               {"erase"},
		"ListPasswordItems":            {"list", "show", "copy"},
		"ListPasswordRecipients":       {"share"},
		"ListWaitingPasswordShares":    {"share"},
		"ReadPasswordsAgentKey":        {"agent"},
		"SavePasswordItem":             {"add", "edit", "import"},
		"DeletePasswordItem":           {"delete"},
		"RestorePasswordItem":          {"restore"},
		"SetPasswordItemShares":        {"share", "unshare"},
		"AddPasswordShareKeys":         {"share"},
		"SetPasswordItemUsableByAgent": {"agent"},
	}
	for operation := range operations {
		if _, isListed := table[operation]; !isListed {
			test.Errorf("the table does not list the operation %s", operation)
		}
	}

	commands := map[string]*cli.Command{}
	for _, subcommand := range NewPasswordsCommand().Commands {
		commands[subcommand.Name] = subcommand
	}
	isSent := map[string]bool{}
	for operation, names := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		for _, name := range names {
			if commands[name] == nil {
				test.Errorf("teanode passwords has no %s, which %s needs", name, operation)
			}
			isSent[name] = true
		}
	}
	// What is on the command line and sends no operation: it works on this
	// computer only.
	exempt := map[string]string{
		"lock":     "forgets the keys kept on this computer",
		"generate": "makes a password on this computer",
		"export":   "reads the items the list operation returns and writes a file",
	}
	for name := range commands {
		if _, isExempt := exempt[name]; !isSent[name] && !isExempt {
			test.Errorf("teanode passwords %s is not tied to an operation of the API", name)
		}
	}
	for name, reason := range exempt {
		if strings.TrimSpace(reason) == "" || commands[name] == nil {
			test.Errorf("the exemption %s needs a reason and a command", name)
		}
	}

	// Every argument of a read operation is a flag or argument of the command
	// that asks it.
	flagNames := map[string]string{"granteeUserId": "", "granteeCircleId": "", "sinceRevision": ""}
	exemptArguments := map[string]string{
		"ListPasswordItems.sinceRevision":        "a command always lists every item, because it opens them all to find one",
		"ListPasswordRecipients.granteeUserId":   "the person is the argument of share, found by username",
		"ListPasswordRecipients.granteeCircleId": "the circle is the argument of share, found by name",
	}
	queries := reflect.TypeFor[apigraph.PasswordsQuery]()
	for operation, names := range table {
		method, isQuery := queries.MethodByName(operation)
		if !isQuery || method.Type.NumIn() < 2 {
			continue
		}
		argumentsType := method.Type.In(1)
		for index := 0; index < argumentsType.NumField(); index++ {
			argument, _, _ := strings.Cut(argumentsType.Field(index).Tag.Get("json"), ",")
			if _, isExempt := exemptArguments[operation+"."+argument]; isExempt {
				continue
			}
			if _, isKnown := flagNames[argument]; !isKnown {
				test.Errorf("%s takes %s, which no command or exemption accounts for (%v)", operation, argument, names)
			}
		}
	}
	for key, reason := range exemptArguments {
		if strings.TrimSpace(reason) == "" {
			test.Errorf("the exemption %s needs a reason", key)
		}
	}
}

// Every document names its operation, and the ones that change something are
// mutations, so a read-only client refuses them.
func TestPasswordsDocumentsNameTheirOperations(test *testing.T) {
	test.Parallel()
	for operation, document := range map[string]string{
		"ReadPasswordsSetup": client.DocumentReadPasswordsSetup, "SetUpPasswords": client.DocumentSetUpPasswords,
		"UnlockPasswords": client.DocumentUnlockPasswords, "ChangePasswordsPassphrase": client.DocumentChangePasswordsPassphrase,
		"ReplacePasswordsRecoveryKey": client.DocumentReplacePasswordsRecoveryKey, "UpdatePasswordsSettings": client.DocumentUpdatePasswordsSettings,
		"ErasePasswords": client.DocumentErasePasswords, "ListPasswordItems": client.DocumentListPasswordItems,
		"ListPasswordRecipients": client.DocumentListPasswordRecipients, "ListWaitingPasswordShares": client.DocumentListWaitingPasswordShares,
		"ReadPasswordsAgentKey": client.DocumentReadPasswordsAgentKey, "SavePasswordItem": client.DocumentSavePasswordItem,
		"DeletePasswordItem": client.DocumentDeletePasswordItem, "RestorePasswordItem": client.DocumentRestorePasswordItem,
		"SetPasswordItemShares": client.DocumentSetPasswordItemShares, "AddPasswordShareKeys": client.DocumentAddPasswordShareKeys,
		"SetPasswordItemUsableByAgent": client.DocumentSetPasswordItemUsableByAgent,
	} {
		isMutation := !strings.HasPrefix(operation, "List") && !strings.HasPrefix(operation, "Read")
		if client.IsMutationDocument(document) != isMutation {
			test.Errorf("%s: mutation is %v", operation, !isMutation)
		}
		if !strings.Contains(document, operation+"(") && !strings.Contains(document, operation+" ") {
			test.Errorf("%s is not in its document", operation)
		}
	}
}
