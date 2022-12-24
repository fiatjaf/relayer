package relayer

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/cors"
)

// Settings specify initial startup parameters for Start and StartConf.
type Settings struct {
	Host string `envconfig:"HOST" default:"0.0.0.0"`
	Port string `envconfig:"PORT" default:"7447"`
}

// Start calls StartConf with Settings parsed from the process environment.
func Start(relay Relay) error {
	var s Settings
	if err := envconfig.Process("", &s); err != nil {
		return fmt.Errorf("envconfig: %w", err)
	}
	return StartConf(s, relay)
}

// StartConf creates a new Server, passing it host:port for the address,
// and starts serving propagating any error returned from [Server.Start].
func StartConf(s Settings, relay Relay) error {
	addr := net.JoinHostPort(s.Host, s.Port)
	srv := NewServer(addr, relay)
	return srv.Start()
}

// Server is a base for package users to implement nostr relays.
// It can serve HTTP requests and websockets, passing control over to a relay implementation.
//
// To implement a relay, it is enough to satisfy [Relay] interface. Other interfaces are
// [Informationer], [CustomWebSocketHandler], [ShutdownAware] and AdvancedXxx types.
// See their respective doc comments.
//
// The basic usage is to call Start or StartConf, which starts serving immediately.
// For a more fine-grained control, use NewServer.
// See [basic/main.go], [whitelisted/main.go], [expensive/main.go] and [rss-bridge/main.go]
// for example implementations.
//
// The following resource is a good starting point for details on what nostr protocol is
// and how it works: https://github.com/nostr-protocol/nostr
type Server struct {
	// Default logger, as set by NewServer, is a stdlib logger prefixed with [Relay.Name],
	// outputting to stderr.
	Log Logger

	addr       string
	relay      Relay
	router     *mux.Router
	httpServer *http.Server // set at Server.Start

	// keep a connection reference to all connected clients for Server.Shutdown
	clientsMu sync.Mutex
	clients   map[*websocket.Conn]struct{}
}

// NewServer creates a relay server with sensible defaults.
// The provided address is used to listen and respond to HTTP requests.
func NewServer(addr string, relay Relay) *Server {
	srv := &Server{
		Log:     defaultLogger(relay.Name() + ": "),
		addr:    addr,
		relay:   relay,
		router:  mux.NewRouter(),
		clients: make(map[*websocket.Conn]struct{}),
	}
	srv.router.Path("/").Headers("Upgrade", "websocket").HandlerFunc(srv.handleWebsocket)
	srv.router.Path("/").Headers("Accept", "application/nostr+json").HandlerFunc(srv.handleNIP11)
	return srv
}

// Router returns an http.Handler used to handle server's in-flight HTTP requests.
// By default, the router is setup to handle websocket upgrade and NIP-11 requests.
//
// In a larger system, where the relay server is not the only HTTP handler,
// prefer using s as http.Handler instead of the returned router.
func (s *Server) Router() *mux.Router {
	return s.router
}

// Addr returns Server's HTTP listener address in host:port form.
// If the initial port value provided in NewServer is 0, the actual port
// value is picked at random and available by the time [Relay.OnInitialized]
// is called.
func (s *Server) Addr() string {
	return s.addr
}

// ServeHTTP implements http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// Start initializes the relay and its storage using their respective Init methods,
// returning any non-nil errors, and starts listening for HTTP requests on the address
// provided to NewServer.
//
// Just before starting to serve HTTP requests, Start calls Relay.OnInitialized
// allowing package users to make last adjustments, such as setting up custom HTTP
// handlers using s.Router.
//
// Start never returns until termination of the underlying http.Server, forwarding
// any but http.ErrServerClosed error from the server's ListenAndServe.
// To terminate the server, call Shutdown.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.addr = ln.Addr().String()
	return s.startListener(ln)
}

func (s *Server) startListener(ln net.Listener) error {
	// init the relay
	if err := s.relay.Init(); err != nil {
		return fmt.Errorf("relay init: %w", err)
	}
	if err := s.relay.Storage().Init(); err != nil {
		return fmt.Errorf("storage init: %w", err)
	}

	// push events from implementations, if any
	if inj, ok := s.relay.(Injector); ok {
		go func() {
			for event := range inj.InjectEvents() {
				notifyListeners(&event)
			}
		}()
	}

	s.httpServer = &http.Server{
		Handler:      cors.Default().Handler(s),
		Addr:         s.addr,
		WriteTimeout: 2 * time.Second,
		ReadTimeout:  2 * time.Second,
		IdleTimeout:  30 * time.Second,
	}
	s.httpServer.RegisterOnShutdown(s.disconnectAllClients)
	// final callback, just before serving http
	s.relay.OnInitialized(s)

	// start accepting incoming requests
	s.Log.Infof("listening on %s", s.addr)
	err := s.httpServer.Serve(ln)
	if err == http.ErrServerClosed {
		err = nil
	}
	return err
}

// Shutdown stops serving HTTP requests and send a websocket close control message
// to all connected clients.
//
// If the relay is ShutdownAware, Shutdown calls its OnShutdown, passing the context as is.
// Note that the HTTP server make some time to shutdown and so the context deadline,
// if any, may have been shortened by the time OnShutdown is called.
func (s *Server) Shutdown(ctx context.Context) error {
	err := s.httpServer.Shutdown(ctx)
	if f, ok := s.relay.(ShutdownAware); ok {
		f.OnShutdown(ctx)
	}
	return err
}

func (s *Server) disconnectAllClients() {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	for conn := range s.clients {
		conn.WriteControl(websocket.CloseMessage, nil, time.Now().Add(time.Second))
		conn.Close()
		delete(s.clients, conn)
	}
}

func defaultLogger(prefix string) Logger {
	l := log.New(os.Stderr, "", log.LstdFlags|log.Lmsgprefix)
	l.SetPrefix(prefix)
	return stdLogger{l}
}

type stdLogger struct{ log *log.Logger }

func (l stdLogger) Infof(format string, v ...any)    { l.log.Printf(format, v...) }
func (l stdLogger) Warningf(format string, v ...any) { l.log.Printf(format, v...) }
func (l stdLogger) Errorf(format string, v ...any)   { l.log.Printf(format, v...) }
