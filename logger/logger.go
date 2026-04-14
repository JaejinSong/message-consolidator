package logger

import (
	"fmt"
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

var currentLevel = LevelInfo

func SetLevel(levelStr string) {
	if level, ok := levelMap[strings.ToUpper(levelStr)]; ok {
		currentLevel = level
	}
}

func Debugf(format string, v ...interface{}) {
	if currentLevel <= LevelDebug {
		log.Output(2, fmt.Sprintf("[DEBUG] "+format, v...))
	}
}

func Infof(format string, v ...interface{}) {
	if currentLevel <= LevelInfo {
		log.Output(2, fmt.Sprintf("[INFO] "+format, v...))
	}
}

func Warnf(format string, v ...interface{}) {
	if currentLevel <= LevelWarn {
		log.Output(2, fmt.Sprintf("[WARN] "+format, v...))
	}
}

func Errorf(format string, v ...interface{}) {
	if currentLevel <= LevelError {
		log.Output(2, fmt.Sprintf("[ERROR] "+format, v...))
	}
}

func InitLogging() {
	// Call AI inference logger initialization to ensure tmp/logs/ directory exists and log files are ready.
	InitAIInferenceLogger()

	lumberjackLogger := &lumberjack.Logger{
		Filename:   "/app/logs/app.log",
		MaxSize:    100, //Why: Caps individual log files at 100MB to prevent uncontrollable disk usage on the host system.
		MaxBackups: 30,
		MaxAge:     7, //Why: Retains log files for up to 7 days to balance diagnostic depth with storage efficiency.
		Compress:   true,
		LocalTime:  true,
	}

	multiWriter := io.MultiWriter(os.Stdout, lumberjackLogger)
	log.SetOutput(multiWriter)

	//Why: Implements a background goroutine to trigger log rotation at midnight, ensuring each day's logs are physically separated and easier to manage.
	go func() {
		for {
			now := time.Now()
			nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
			time.Sleep(time.Until(nextMidnight))

			Infof("[LOG] Rotating log file for new day...")
			if err := lumberjackLogger.Rotate(); err != nil {
				Errorf("[LOG] Error rotating log: %v", err)
			}
		}
	}()
}
