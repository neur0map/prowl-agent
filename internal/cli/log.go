package cli

import (
	"os"

	"charm.land/log/v2"
	"github.com/charmbracelet/colorprofile"
)

// uiLog is the shared progress logger for multi-step commands (init, update,
// restart, the Ollama setup). It writes to stderr so stdout and --json output
// stay pipe-clean, and emits plain, un-styled text when stderr is not a
// terminal.
var uiLog = newUILogger()

func newUILogger() *log.Logger {
	l := log.NewWithOptions(os.Stderr, log.Options{ReportTimestamp: false})
	if !isTTY(os.Stderr) {
		l.SetColorProfile(colorprofile.NoTTY)
	}
	return l
}
