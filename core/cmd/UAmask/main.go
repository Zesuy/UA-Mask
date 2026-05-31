package main

import (
	"UAmask/internal/app"

	"github.com/sirupsen/logrus"
)

var version = "0.4.3"

func main() {
	if err := app.Run(version); err != nil {
		logrus.Fatalf("UA-Mask failed to start: %v", err)
	}
}
