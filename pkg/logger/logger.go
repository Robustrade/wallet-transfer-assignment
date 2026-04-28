package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

var log zerolog.Logger

// Init initialises the global logger. Call once at startup.
func Init(level int) {
	zerolog.TimeFieldFormat = time.RFC3339

	consoleWriter := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	log = zerolog.New(consoleWriter).With().Timestamp().Logger()

	zerolog.SetGlobalLevel(zerolog.Level(level))
}

func Info(fn, msg string, fields map[string]interface{}) {
	ev := log.Info().Str("fn", fn)
	for k, v := range fields {
		ev = ev.Interface(k, v)
	}
	ev.Msg(msg)
}

func Error(fn, msg string, err error, fields map[string]interface{}) {
	ev := log.Error().Str("fn", fn).Err(err)
	for k, v := range fields {
		ev = ev.Interface(k, v)
	}
	ev.Msg(msg)
}

func Warn(fn, msg string, fields map[string]interface{}) {
	ev := log.Warn().Str("fn", fn)
	for k, v := range fields {
		ev = ev.Interface(k, v)
	}
	ev.Msg(msg)
}

func Debug(fn, msg string, fields map[string]interface{}) {
	ev := log.Debug().Str("fn", fn)
	for k, v := range fields {
		ev = ev.Interface(k, v)
	}
	ev.Msg(msg)
}
