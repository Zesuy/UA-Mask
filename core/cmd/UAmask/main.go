package main

import (
	"UAmask/internal/app"

	"github.com/sirupsen/logrus"
)

// version is replaced by release builds through -ldflags. Keeping the source
// default explicit avoids accidentally presenting an unversioned local build
// as an official release.
var version = "dev"

func main() {
	if err := app.Run(version); err != nil {
		logrus.Fatalf("UA-Mask failed to start: %v", err)
	}
}
