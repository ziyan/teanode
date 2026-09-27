package apigraph

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/util/safefetch"
)

// A picture the agent put in an answer.
//
// The agent answers in Markdown, and a picture in Markdown is an address the
// browser fetches the moment the answer is drawn. That is the oldest way to
// take something out of a conversation with a model: a message or a page the
// agent read tells it to show a picture whose address carries what it knows,
// and the browser delivers it to whoever runs that server. So the browser
// fetches nothing itself. The dashboard asks this endpoint, which fetches a
// picture only if its address appeared, as it is, in what a tool showed the
// agent in this conversation: a page's images, a search's results. An
// address the agent put together is not one a tool showed it, and is not
// fetched.
//
// Fetched here rather than by the browser for the same reasons as the mail
// image proxy: the picture's server learns that this server looked, not who
// was reading, and the dashboard's policy can go on saying img-src 'self'.

// agentPictureMaximumSize is enough for a product photo or a chart, and
// small enough that a hostile server cannot fill a pipe with it.
const agentPictureMaximumSize = 8 << 20

// agentPictureTypes are the pictures shown: the raster formats a browser
// draws as they are. Never SVG, which can carry script.
var agentPictureTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/avif": true,
}

func (self *graph) agentPictureView(response http.ResponseWriter, request *http.Request) {
	_, found, ok := self.agentAttachmentPerson(response, request)
	if !ok {
		return
	}
	conversationId := mux.Vars(request)["conversationId"]
	written := request.URL.Query().Get("url")
	target, err := safefetch.ParseTarget(written)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "not a fetchable address"})
		return
	}
	shown := false
	if err := self.database.Transaction(func(tx db.Transaction) error {
		conversation, err := tx.GetAgentConversation(conversationId)
		if err != nil || conversation == nil || conversation.AgentID != found.ID {
			return err
		}
		shown, err = tx.HasAgentToolAnswerContaining(conversationId, pictureAddressForms(written)...)
		return err
	}); err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "cannot read the conversation"})
		return
	}
	if !shown {
		writeJSON(response, http.StatusNotFound, map[string]string{"error": "no tool showed this picture in the conversation"})
		return
	}
	contentType, body, err := safefetch.Image(request.Context(), target, agentPictureMaximumSize, func(contentType string) bool {
		return agentPictureTypes[contentType]
	})
	switch {
	case errors.Is(err, safefetch.ErrNotImage):
		writeJSON(response, http.StatusUnsupportedMediaType, map[string]string{"error": "not a picture"})
		return
	case err != nil:
		log.Debugf("cannot fetch a picture of conversation %s from %q: %s", conversationId, target.Redacted(), err)
		writeJSON(response, http.StatusBadGateway, map[string]string{"error": "could not fetch it"})
		return
	}
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	response.Header().Set("Cache-Control", "private, max-age=3600")
	response.WriteHeader(http.StatusOK)
	if _, err := response.Write(body); err != nil {
		log.Debugf("cannot write a picture of conversation %s: %s", conversationId, err)
	}
}

// pictureAddressForms are the ways an address can stand in a tool's
// answer: as written, and as JSON writes it, which turns & into &.
func pictureAddressForms(written string) []string {
	forms := []string{written}
	if encoded, err := json.Marshal(written); err == nil {
		if escaped := strings.Trim(string(encoded), `"`); escaped != written {
			forms = append(forms, escaped)
		}
	}
	return forms
}
