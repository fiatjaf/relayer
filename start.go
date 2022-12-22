package relayer

import (
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
func Start(relay Relay) {
	var s Settings
	if err := envconfig.Process("", &s); err != nil {
		log.Panic().Err(err).Msg("couldn't process envconfig")
	}
	StartConf(s, relay)
}

// StartConf initalizes the relay and its storage using their respective Init methods,
// and starts listening for HTTP requests on host:port, as specified in the settings.
// It never returns until process termination.
func StartConf(s Settings, relay Relay) {
	// allow implementations to do initialization stuff
	if err := relay.Init(); err != nil {
		Log.Fatal().Err(err).Msg("failed to start")
	}

	// initialize storage
	if err := relay.Storage().Init(); err != nil {
		log.Fatal().Err(err).Msg("error initializing storage")
		return
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
		Addr:              s.Host + ":" + s.Port,
		WriteTimeout:      2 * time.Second,
		ReadTimeout:       2 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 2 * time.Second,
	}
	log.Debug().Str("addr", srv.Addr).Msg("listening")
	srv.ListenAndServe()
}
