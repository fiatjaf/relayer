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

type Settings struct {
	Host string `envconfig:"HOST" default:"0.0.0.0"`
	Port string `envconfig:"PORT" default:"7447"`
}

var (
	s   Settings
	log = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stderr})
)

var Router = mux.NewRouter()

func Start(relay Relay) {
	// read host/port (implementations can read other stuff on their own if they need)
	if err := envconfig.Process("", &s); err != nil {
		log.Panic().Err(err).Msg("couldn't process envconfig")
	}

	// expose this Log instance so implementations can use it
	Log = log.With().Str("name", relay.Name()).Logger()

	// catch the websocket call before anything else
	Router.Path("/").Headers("Upgrade", "websocket").HandlerFunc(handleWebsocket(relay))

	// nip-11, relay information
	Router.Path("/").Headers("Accept", "application/nostr+json").HandlerFunc(handleNIP11(relay))

	// allow implementations to do initialization stuff
	if err := relay.Init(); err != nil {
		Log.Fatal().Err(err).Msg("failed to start")
	}

	// wait for events to come from implementations, if this is implemented
	if inj, ok := relay.(Injector); ok {
		go func() {
			for event := range inj.InjectEvents() {
				notifyListeners(&event)
			}
		}()
	}

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
