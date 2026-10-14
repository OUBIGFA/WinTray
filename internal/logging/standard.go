package logging

import (
	"log"
	"strings"
)

// CaptureStandardLog sends library diagnostics to the same rotating file as
// application messages. A Windows GUI executable may have no usable stderr.
// Call once for the UI session, then restore before closing this logger.
func (l *Logger) CaptureStandardLog() func() {
	output, flags, prefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(l)
	log.SetFlags(0)
	log.SetPrefix("")
	return func() {
		log.SetOutput(output)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
	}
}

// Write implements io.Writer for the standard logger. Preserve diagnostics
// without a second timestamp, and report disk/closed-logger errors to callers.
func (l *Logger) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := l.write("DIAG", strings.TrimRight(string(p), "\r\n")); err != nil {
		return 0, err
	}
	return len(p), nil
}
