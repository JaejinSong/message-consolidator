package logger

import (
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
)

const logFileMode os.FileMode = 0644

// Why: lumberjack creates missing files as 0600 and reuses the existing file's mode on rotation and
// compression, so fixing the mode before handing it over keeps every generation readable by host collectors.
func ensureLogFileReadable(path string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logFileMode)
	if err != nil {
		Warnf("[LOG] cannot prepare log file %s: %v", path, err)
		return
	}
	if err := f.Close(); err != nil {
		Warnf("[LOG] close log file %s: %v", path, err)
	}
	// Why: umask can strip the create mode, and pre-existing files keep their old mode.
	if err := os.Chmod(path, logFileMode); err != nil {
		Warnf("[LOG] chmod log file %s: %v", path, err)
	}
}

// Why: a log file deleted while running is recreated 0600 by lumberjack, so re-assert the mode before each rotation.
func rotateReadable(lj *lumberjack.Logger) error {
	ensureLogFileReadable(lj.Filename)
	return lj.Rotate()
}
