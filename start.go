package relayer

import (
	"net"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/cors"
	"github.com/rs/zerolog"
)

// Settings specify initial startup parameters for a relay server.
// See StartConf for details.
type Settings struct {
	Host string `envconfig:"HOST" default:"0.0.0.0"`
	Port string `envconfig:"PORT" default:"7447"`
}

var log = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stderr})

var Router = mux.NewRouter()

// Start calls StartConf with Settings parsed from the process environment.
func Start(relay Relay) error {
	var s Settings
	if err := envconfig.Process("", &s); err != nil {
		return fmt.Errorf("envconfig: %w", err)
	}
	return StartConf(s, relay)
}

// StartConf initalizes the relay and its storage using their respective Init methods,
// returning any non-nil errors, and starts listening for HTTP requests on host:port otherwise,
// as specified in the settings.
//
// StartConf never returns until termination of the underlying http.Server, forwarding
// any but http.ErrServerClosed error from the server's ListenAndServe.
func StartConf(s Settings, relay Relay) error {
	// allow implementations to do initialization stuff
	if err := relay.Init(); err != nil {
		return fmt.Errorf("relay init: %w", err)
	}

	// initialize storage
	if err := relay.Storage().Init(); err != nil {
		return fmt.Errorf("storage init: %w", err)
	}

	// expose this Log instance so implementations can use it
	Log = log.With().Str("name", relay.Name()).Logger()

	// catch the websocket call before anything else
	Router.Path("/").Headers("Upgrade", "websocket").HandlerFunc(handleWebsocket(relay))

	// nip-11, relay information
	Router.Path("/").Headers("Accept", "application/nostr+json").HandlerFunc(handleNIP11(relay))

	// wait for events to come from implementations, if this is implemented
	if inj, ok := relay.(Injector); ok {
		go func() {
			for event := range inj.InjectEvents() {
				notifyListeners(&event)
			}
		}()
	}

	relay.OnInitialized()

	// start http server
	srv := &http.Server{
		Handler:           cors.Default().Handler(Router),
		Addr:              net.JoinHostPort(s.Host, s.Port),
		WriteTimeout:      2 * time.Second,
		ReadTimeout:       2 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
	}
	log.Debug().Str("addr", srv.Addr).Msg("listening")
	srvErr := srv.ListenAndServe()
	if srvErr == http.ErrServerClosed {
		srvErr = nil
	}
	return srvErr
}
