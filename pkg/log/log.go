package log

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func init() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
}

var Verbose bool

func Debugf(f string, args ...any) {
	log.Debug().Msgf(f, args...)
}
func Infof(f string, args ...any) {
	log.Info().Msgf(f, args...)
}
func Warnf(f string, args ...any) {
	log.Warn().Msgf(f, args...)
}
func Errorf(f string, args ...any) {
	log.Error().Msgf(f, args...)
}
