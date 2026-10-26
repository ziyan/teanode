package dav

import (
	"bytes"
	"io"
	"net/http"
	"strings"
)

// Temporary: what an Android phone's calendar app asks and is answered, to
// find what it is missing. Removed once it syncs.

type recordedResponse struct {
	http.ResponseWriter
	httpStatus int
	body       bytes.Buffer
}

func (self *recordedResponse) WriteHeader(httpStatus int) {
	self.httpStatus = httpStatus
	self.ResponseWriter.WriteHeader(httpStatus)
}

func (self *recordedResponse) Write(content []byte) (int, error) {
	if self.body.Len() < 4000 {
		self.body.Write(content)
	}
	return self.ResponseWriter.Write(content)
}

func isDebuggedClient(request *http.Request) bool {
	return strings.Contains(request.UserAgent(), "OPPO")
}

func (self *component) serveDebugged(response http.ResponseWriter, request *http.Request, next func(http.ResponseWriter, *http.Request)) {
	var requestBody []byte
	if request.Body != nil {
		requestBody, _ = io.ReadAll(io.LimitReader(request.Body, 8000))
		request.Body = io.NopCloser(bytes.NewReader(requestBody))
	}
	recorded := &recordedResponse{ResponseWriter: response}
	next(recorded, request)
	answer := recorded.body.String()
	if len(answer) > 4000 {
		answer = answer[:4000]
	}
	log.Noticef("dav debug: %s %s depth=%q status=%d dav=%q allow=%q request=%q answer=%q",
		request.Method, request.URL.Path, request.Header.Get("Depth"), recorded.httpStatus,
		response.Header().Get("DAV"), response.Header().Get("Allow"), string(requestBody), answer)
}
