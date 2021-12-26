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

var s Settings
var log = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stderr})

var Router = mux.NewRouter()

func Start(relay Relay) {
	if err := envconfig.Process("", &s); err != nil {
		log.Panic().Err(err).Msg("couldn't process envconfig")
	}

	Log = log.With().Str("name", relay.Name()).Logger()

	if err := relay.Init(); err != nil {
		Log.Fatal().Err(err).Msg("failed to start")
	}

	Router.Path("/").Methods("GET").Headers("Upgrade", "websocket").
		HandlerFunc(handleWebsocket(relay))

	if inj, ok := relay.(Injector); ok {
		go func() {
			for event := range inj.InjectEvents() {
				notifyListeners(&event)
			}
		}()
	}

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
