package observability

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// InitLogger configures zerolog
func InitLogger() {
	// Set time format
	zerolog.TimeFieldFormat = time.RFC3339

	// Configure console writer for pretty logging
	output := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}

	// Configure global logger
	log.Logger = zerolog.New(output).
		With().
		Timestamp().
		Caller().
		Logger()

	// Set log level (can be changed to Info in production)
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
}

// Logger returns the configured logger
func Logger() zerolog.Logger {
	return log.Logger
}
