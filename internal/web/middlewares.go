package web

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/handlers"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/util/bufferpool"
)

type Middleware func(http.Handler) http.Handler

func ApplyMiddlewares(handler http.Handler, middlewares ...Middleware) http.Handler {
	for _, middleware := range middlewares {
		handler = middleware(handler)
	}
	return handler
}

type accessLog struct {
	Timestamp  time.Time `json:"timestamp,omitempty"`
	IP         string    `json:"ip,omitempty"`
	Scheme     string    `json:"scheme,omitempty"`
	Host       string    `json:"host,omitempty"`
	User       string    `json:"user,omitempty"`
	Method     string    `json:"method,omitempty"`
	URI        string    `json:"uri,omitempty"`
	Protocol   string    `json:"protocol,omitempty"`
	StatusCode int       `json:"statusCode,omitempty"`
	Size       int       `json:"size"`
	Referer    string    `json:"referer,omitempty"`
	UserAgent  string    `json:"userAgent,omitempty"`
	Elapsed    float64   `json:"elapsed,omitempty"`
}

func LoggingMiddleware(handler http.Handler) http.Handler {
	timestampFormat := "2006-01-02T15:04:05.000000-07:00,"
	return handlers.CustomLoggingHandler(os.Stdout, handler, func(writer io.Writer, params handlers.LogFormatterParams) {
		scheme := "http"
		if params.Request.TLS != nil {
			scheme = "https"
		}

		user := ""
		if params.URL.User != nil {
			user = params.URL.User.Username()
		}

		// get a buffer from the pool
		buffer, releaseBuffer := bufferpool.AcquireBuffer()
		defer releaseBuffer()

		// put in the timestamp as a place holder
		if _, err := buffer.WriteString(timestampFormat); err != nil {
			log.Errorf("failed to write timestamp for access log: %s", err)
			return
		}

		// put in json message, json encoder already adds newline at the end
		if err := json.NewEncoder(buffer).Encode(&accessLog{
			Timestamp:  params.TimeStamp,
			IP:         params.Request.RemoteAddr,
			Scheme:     scheme,
			Host:       params.Request.Host,
			User:       user,
			Method:     params.Request.Method,
			URI:        params.Request.RequestURI,
			Protocol:   params.Request.Proto,
			StatusCode: params.StatusCode,
			Size:       params.Size,
			Referer:    params.Request.Referer(),
			UserAgent:  params.Request.UserAgent(),
			Elapsed:    time.Since(params.TimeStamp).Seconds(),
		}); err != nil {
			log.Errorf("failed to encode access log: %s", err)
			return
		}

		// replace timestamp
		raw := buffer.Bytes()
		copy(raw, []byte(time.Now().Format(timestampFormat)))

		// write
		if _, err := writer.Write(raw); err != nil {
			log.Errorf("failed to write access log: %s", err)
			return
		}
	})
}

func CompressionMiddleware(handler http.Handler) http.Handler {
	return handlers.CompressHandler(handler)
}

// NoStoreMiddleware stops anything under the API being cached.
//
// None of it is cacheable. The session endpoint reports who you are, and a
// reply with no Cache-Control is heuristically cacheable — so a browser is
// entitled to keep it and answer the next request itself. That is not
// theoretical: after the accounts were cleared on a development server, an
// iPhone went on showing a login form for a server that had none, because it
// never asked again. The same staleness would let a logged-out browser be told
// it is still logged in.
//
// Static assets are handled separately and correctly: the dashboard's own HTML
// is no-store, and the hashed bundles are immutable.
func NoStoreMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, api.Prefix) {
			response.Header().Set("Cache-Control", "no-store")
		}
		handler.ServeHTTP(response, request)
	})
}

func MakeServerNameMiddleware(serverName string) Middleware {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Server", serverName)
			handler.ServeHTTP(response, request)
		})
	}
}

func MakeForwarderMiddleware(forwarderKey string) Middleware {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if ip, _, err := net.SplitHostPort(request.RemoteAddr); err == nil {
				request.RemoteAddr = ip
			}
			if forwardedFor := request.Header.Get("X-Forwarded-For"); forwardedFor != "" {
				if request.Header.Get("X-Forwarder-Key") != forwarderKey {
					log.Warningf("request from %s has X-Forwarded-For header %q, but has invalid X-Forwarder-Key", request.RemoteAddr, forwardedFor)
					http.Error(response, "Service Unavailable", http.StatusServiceUnavailable)
					return
				}
				ips := strings.Split(forwardedFor, ",")
				request.RemoteAddr = strings.TrimSpace(ips[0])
			}
			delete(request.Header, "X-Forwarder-Key")
			request.Header.Set("X-Forwarded-For", request.RemoteAddr)
			handler.ServeHTTP(response, request)
		})
	}
}

// MakeSecurityHeadersMiddleware sets the headers that decide what a browser
// will do with this page, on everything the server answers.
//
// The content security policy is the substantial one. The dashboard renders
// mail written by strangers; the sanitiser and the sandboxed frame are the
// first two layers, and this is the third — the one that holds even if the
// other two have a hole in them.
//
//	default-src 'self'      nothing loads from anywhere but this server.
//	script-src              this server, plus the hash of each inline script
//	                        the dashboard ships. Not 'unsafe-inline', which
//	                        would be the same as having no script policy.
//	style-src 'unsafe-inline'
//	                        required twice over: React writes style attributes,
//	                        and the message frame's own document styles itself
//	                        inline. A srcdoc frame inherits this policy, so
//	                        anything the frame needs has to be allowed here as
//	                        well — which is why the remote images go through
//	                        the server rather than widening img-src to https:.
//	img-src 'self' data:    every image in a message is served from here: the
//	                        attached ones from the attachment endpoint, the
//	                        remote ones through the proxy. Nothing in a message
//	                        reaches the network from the reader's browser.
//	connect-src 'self'      the API and its WebSocket, nowhere else.
//	frame-ancestors 'none'  this page is not put inside anybody else's, which
//	                        is what X-Frame-Options used to say.
//	form-action 'self'      a form in a message cannot post anywhere, and the
//	                        frame's sandbox already forbids forms outright.
//	base-uri 'none'         no injected <base> can send every relative URL on
//	                        the page somewhere else.
//	object-src 'none'       no plugins.
//
// One page is the exception to connect-src: the one the command line client
// opens to sign in, which hands the token it obtained to the client's listener
// on the reader's own machine. That page, and only that page, may reach a
// loopback address. The page is a document of its own — the client opens it
// directly — so the policy is chosen by the path the document was served at.
func MakeSecurityHeadersMiddleware(inlineScriptHashes []string) Middleware {
	policy := securityPolicy(inlineScriptHashes, "connect-src 'self'")
	commandLinePolicy := securityPolicy(inlineScriptHashes, "connect-src 'self' "+CommandLineConnectSources)

	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path == CommandLinePagePath {
				response.Header().Set("Content-Security-Policy", commandLinePolicy)
			} else {
				response.Header().Set("Content-Security-Policy", policy)
			}
			// Nothing is sniffed into a type the sender chose.
			response.Header().Set("X-Content-Type-Options", "nosniff")
			// A dashboard URL carries a message identifier, so it is not sent
			// to whatever a reader clicks through to.
			response.Header().Set("Referrer-Policy", "no-referrer")
			// The dashboard asks for none of these, so nothing it embeds gets
			// to ask either.
			response.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
			handler.ServeHTTP(response, request)
		})
	}
}

// CommandLinePagePath is the dashboard page the command line client opens to
// sign in. The client names the same path.
const CommandLinePagePath = "/cli"

// CommandLineConnectSources is what that page may connect to besides this
// server: the client's listener, which is on the reader's own machine and so
// is plain HTTP on a loopback address, on whichever port was free.
const CommandLineConnectSources = "http://127.0.0.1:* http://localhost:*"

// securityPolicy assembles the content security policy with one connect-src
// or another.
func securityPolicy(inlineScriptHashes []string, connect string) string {
	script := "script-src 'self'"
	if len(inlineScriptHashes) > 0 {
		script += " " + strings.Join(inlineScriptHashes, " ")
	}
	return strings.Join([]string{
		"default-src 'self'",
		script,
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self' data:",
		connect,
		"frame-src 'self'",
		"media-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}
