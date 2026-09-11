// Package all imports every tool package, so that importing it is
// importing the catalog. The agent package imports it; a tool package
// left out of this list is a tool the agent does not have. The order is
// the order of the catalog.
package all

import (
	_ "github.com/ziyan/teanode/internal/agent/tools/account"
	_ "github.com/ziyan/teanode/internal/agent/tools/artifact"
	_ "github.com/ziyan/teanode/internal/agent/tools/askuser"
	_ "github.com/ziyan/teanode/internal/agent/tools/browser"
	_ "github.com/ziyan/teanode/internal/agent/tools/computer"
	_ "github.com/ziyan/teanode/internal/agent/tools/contact"
	_ "github.com/ziyan/teanode/internal/agent/tools/datetime"
	_ "github.com/ziyan/teanode/internal/agent/tools/domain"
	_ "github.com/ziyan/teanode/internal/agent/tools/folder"
	_ "github.com/ziyan/teanode/internal/agent/tools/mailact"
	_ "github.com/ziyan/teanode/internal/agent/tools/mailaudit"
	_ "github.com/ziyan/teanode/internal/agent/tools/mailboxsettings"
	_ "github.com/ziyan/teanode/internal/agent/tools/mailcomposehelp"
	_ "github.com/ziyan/teanode/internal/agent/tools/maildraft"
	_ "github.com/ziyan/teanode/internal/agent/tools/mailread"
	_ "github.com/ziyan/teanode/internal/agent/tools/mailsearch"
	_ "github.com/ziyan/teanode/internal/agent/tools/mailsend"
	_ "github.com/ziyan/teanode/internal/agent/tools/memory"
	_ "github.com/ziyan/teanode/internal/agent/tools/people"
	_ "github.com/ziyan/teanode/internal/agent/tools/replyqueue"
	_ "github.com/ziyan/teanode/internal/agent/tools/rule"
	_ "github.com/ziyan/teanode/internal/agent/tools/schedule"
	_ "github.com/ziyan/teanode/internal/agent/tools/server"
	_ "github.com/ziyan/teanode/internal/agent/tools/subscription"
	_ "github.com/ziyan/teanode/internal/agent/tools/toolsearch"
	_ "github.com/ziyan/teanode/internal/agent/tools/webfetch"
	_ "github.com/ziyan/teanode/internal/agent/tools/websearch"
)
