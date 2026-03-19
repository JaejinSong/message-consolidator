package main

import (
	"io"
	"log"
	"os"
	"strings"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	LevelDebug = iota
	LevelInfo
	LevelWarn
	LevelError
)

var levelMap = map[string]int{
	"DEBUG": LevelDebug,
	"INFO":  LevelInfo,
	"WARN":  LevelWarn,
	"ERROR": LevelError,
}

func getLogLevel() int {
	if cfg == nil {
		return LevelInfo
	}
	if level, ok := levelMap[strings.ToUpper(cfg.LogLevel)]; ok {
		return level
	}
	return LevelInfo
}

func debugf(format string, v ...interface{}) {
	if getLogLevel() <= LevelDebug {
		log.Printf("[DEBUG] "+format, v...)
	}
}

func infof(format string, v ...interface{}) {
	if getLogLevel() <= LevelInfo {
		log.Printf("[INFO] "+format, v...)
	}
}

func warnf(format string, v ...interface{}) {
	if getLogLevel() <= LevelWarn {
		log.Printf("[WARN] "+format, v...)
	}
}

func errorf(format string, v ...interface{}) {
	if getLogLevel() <= LevelError {
		log.Printf("[ERROR] "+format, v...)
	}
}

func initLogging() {
	lumberjackLogger := &lumberjack.Logger{
		Filename:   "logs/app.log",
		MaxSize:    100, // megabytes
		MaxBackups: 30,
		MaxAge:     7, // days
		Compress:   true,
		LocalTime:  true,
	}

	multiWriter := io.MultiWriter(os.Stdout, lumberjackLogger)
	log.SetOutput(multiWriter)

	// Daily rotation logic
	go func() {
		for {
			now := time.Now()
			nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
			time.Sleep(time.Until(nextMidnight))

			infof("[LOG] Rotating log file for new day...")
			if err := lumberjackLogger.Rotate(); err != nil {
				errorf("[LOG] Error rotating log: %v", err)
			}
		}
	}()
}
