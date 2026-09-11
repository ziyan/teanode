// Package all imports every tool package, so that importing it is
// importing the catalog. The agent package imports it; a tool package
// left out of this list is a tool the agent does not have. The order is
// the order of the catalog.
package all

import (
	_ "github.com/ziyan/teanode/internal/agent/tools/artifact"
	_ "github.com/ziyan/teanode/internal/agent/tools/askuser"
	_ "github.com/ziyan/teanode/internal/agent/tools/datetime"
	_ "github.com/ziyan/teanode/internal/agent/tools/memory"
	_ "github.com/ziyan/teanode/internal/agent/tools/schedule"
	_ "github.com/ziyan/teanode/internal/agent/tools/toolsearch"
	_ "github.com/ziyan/teanode/internal/agent/tools/webfetch"
	_ "github.com/ziyan/teanode/internal/agent/tools/websearch"
)
