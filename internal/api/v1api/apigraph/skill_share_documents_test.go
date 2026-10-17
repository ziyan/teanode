package apigraph

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/client"
)

// The documents the command line, the agent's skill tool, the dashboard and
// the app's mirror of them send are written by hand, so nothing checks them
// against the schema until somebody runs the screen. This reads them from where
// they are written and validates each against the real schema, so a field
// renamed here breaks a test and not a person's Approve button.
func TestEverySkillSharingDocumentIsValidAgainstTheSchema(t *testing.T) {
	world := newAvatarWorld(t)

	documents := map[string]string{
		"client ListSkillShares":      client.DocumentListSkillShares,
		"client ShareSkill":           client.DocumentShareSkill,
		"client UnshareSkill":         client.DocumentUnshareSkill,
		"client ListSkillShareCalls":  client.DocumentListSkillShareCalls,
		"client AnswerSkillShareCall": client.DocumentAnswerSkillShareCall,
	}
	for _, file := range []string{
		"../../../agent/tools/connected/skill_share.go",
		"../../../agent/tools/connected/skill.go",
		"../../../../web/src/components/skillShare.tsx",
		"../../../../web/src/pages/agentSkillSharing.tsx",
	} {
		for name, document := range backtickDocuments(t, file) {
			documents[file+" "+name] = document
		}
	}
	for name, document := range swiftDocuments(t, "../../../../ios/TeaNodeKit/Sources/TeaNodeKit/SkillSharing.swift") {
		documents["ios "+name] = document
	}
	checked := 0
	for name, document := range documents {
		if !strings.Contains(document, "SkillShare") && !strings.Contains(document, "ShareSkill") && !strings.Contains(document, "UnshareSkill") &&
			!strings.Contains(document, "isAmountDeclared") {
			continue
		}
		checked++
		t.Logf("checked %s", name)
		if _, rejected := world.graph.prepareGraphRequest(&graphRequest{Query: document}); rejected != nil {
			t.Errorf("%s is refused by the schema: %v\n%s", name, rejected.Errors[0].Message, document)
		}
	}
	if checked < 18 {
		t.Errorf("only %d documents were found; the test is not looking where it thinks", checked)
	}
}

var (
	backtickString = regexp.MustCompile("(?s)`([^`]*)`")
	constantNamed  = regexp.MustCompile("(?s)(?:const|var)?\\s*([A-Za-z_][A-Za-z0-9_]*)\\s*=\\s*`([^`]*)`")
	interpolation  = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

	// A field list a TypeScript file writes in plain quotes, possibly on the
	// line after its name.
	quotedConstant = regexp.MustCompile(`(?m)(?:const|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*'([^'\n]*)'`)

	// A Go string joined to a field list by name, "`+circleFields+`", which a
	// template literal writes as ${circleFields}.
	goJoinedConstant = regexp.MustCompile("`\\s*\\+\\s*([A-Za-z_][A-Za-z0-9_]*)\\s*\\+\\s*`")
)

// backtickDocuments are the GraphQL operations written as template literals in
// a Go or TypeScript file, with the field lists they interpolate filled in.
func backtickDocuments(t *testing.T, path string, alsoConstantsOf ...string) map[string]string {
	t.Helper()
	read := func(path string) string {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile %s: %s", path, err)
		}
		return goJoinedConstant.ReplaceAllString(string(raw), "$${$1}")
	}
	text := read(path)
	constants := map[string]string{}
	// The field lists a page imports from another file are read from it.
	for _, other := range append(alsoConstantsOf, path) {
		for _, match := range quotedConstant.FindAllStringSubmatch(read(other), -1) {
			constants[match[1]] = match[2]
		}
		for _, match := range constantNamed.FindAllStringSubmatch(read(other), -1) {
			constants[match[1]] = match[2]
		}
	}
	// Field lists written in another file the page imports.
	constants["SKILL_SHARE_FIELDS"] = `id skillName toolNames ownerUserId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex
	  granteeUserId granteeUsername granteeName granteeAvatarUrl granteeAvatarColorIndex
	  paymentLimitMinor monthlyLimitMinor currencyCode isApprovalRequired isOwnedByCaller waitingCallCount monthTotalMinor`
	constants["LIST_SKILL_SHARE_CALLS"] = ""
	resolve := func(literal string) string {
		for range 4 {
			literal = interpolation.ReplaceAllStringFunc(literal, func(reference string) string {
				return constants[interpolation.FindStringSubmatch(reference)[1]]
			})
		}
		return literal
	}
	found := map[string]string{}
	for index, match := range backtickString.FindAllStringSubmatch(text, -1) {
		literal := strings.TrimSpace(match[1])
		if !strings.HasPrefix(literal, "query") && !strings.HasPrefix(literal, "mutation") {
			continue
		}
		found[strings.Join([]string{"document", string(rune('a' + index%26))}, "-")+literal[:min(len(literal), 40)]] = resolve(literal)
	}
	return found
}

var (
	swiftString        = regexp.MustCompile(`(?s)static let (\w+) = """\n(.*?)"""`)
	swiftInterpolation = regexp.MustCompile(`\\\((\w+)\)`)
)

// swiftDocuments are the GraphQL operations the iPhone app's Kit writes as
// multi-line strings, with the field lists they interpolate filled in.
func swiftDocuments(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %s", path, err)
	}
	constants := map[string]string{}
	for _, match := range swiftString.FindAllStringSubmatch(string(raw), -1) {
		constants[match[1]] = match[2]
	}
	found := map[string]string{}
	for name, literal := range constants {
		literal = strings.TrimSpace(literal)
		if !strings.HasPrefix(literal, "query") && !strings.HasPrefix(literal, "mutation") {
			continue
		}
		for range 3 {
			literal = swiftInterpolation.ReplaceAllStringFunc(literal, func(reference string) string {
				return constants[swiftInterpolation.FindStringSubmatch(reference)[1]]
			})
		}
		found[name] = literal
	}
	return found
}

// The same check for the documents the rest of the circles work writes by
// hand: the dashboard's pages and components and the agent's tools (the
// iPhone Kit's are all checked by TestTheAppDocumentsMatchTheSchema). A
// document that is not in a file listed here is not checked, so a new screen
// or tool is added to the list.
func TestEveryCircleDocumentIsValidAgainstTheSchema(t *testing.T) {
	world := newAvatarWorld(t)

	documents := map[string]string{}
	for _, file := range []string{
		"../../../agent/tools/grantee.go",
		"../../../agent/tools/circle/circle.go",
		"../../../agent/tools/calendar/calendar.go",
		"../../../agent/tools/calendar/sharing.go",
		"../../../agent/tools/memory/shared.go",
		"../../../agent/tools/conversation/conversation.go",
		"../../../../web/src/components/calendarShare.tsx",
		"../../../../web/src/components/calendarFindTime.tsx",
		"../../../../web/src/components/circleInvitations.ts",
		"../../../../web/src/components/agentChat.tsx",
		"../../../../web/src/pages/agentConversations.tsx",
		"../../../../web/src/pages/calendar.tsx",
		"../../../../web/src/pages/circles.tsx",
		"../../../../web/src/pages/knowledgeShared.tsx",
		"../../../../web/src/pages/settings/phones.tsx",
		"../../../../web/src/pages/settings/profile.tsx",
		"../../../../web/src/pages/access/users.tsx",
		"../../../../web/src/components/agentDrawer.tsx",
		"../../../../web/src/components/accountMenu.tsx",
		"../../../../web/src/session.tsx",
	} {
		for name, document := range backtickDocuments(t, file, "../../../../web/src/components/agentChat.tsx", "../../../../web/src/pages/access/common.tsx") {
			documents[file+" "+name] = document
		}
	}
	checked := 0
	for name, document := range documents {
		checked++
		if _, rejected := world.graph.prepareGraphRequest(&graphRequest{Query: document}); rejected != nil {
			t.Errorf("%s is refused by the schema: %v\n%s", name, rejected.Errors[0].Message, document)
		}
	}
	if checked < 25 {
		t.Errorf("only %d documents were found; the test is not looking where it thinks", checked)
	}
	t.Logf("checked %d documents", checked)
}
