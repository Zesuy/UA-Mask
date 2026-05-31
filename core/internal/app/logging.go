package app

import (
	"os"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

func setupLogging(logLevel, logFile string) {
	if logFile != "" {
		logFileRotator := &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    1,
			MaxBackups: 3,
			MaxAge:     7,
			Compress:   false,
		}
		logrus.SetOutput(logFileRotator)
	} else {
		logrus.SetOutput(os.Stdout)
	}

	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		logrus.Warnf("Invalid log level '%s', using 'info'", logLevel)
		level = logrus.InfoLevel
	}

	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
}
