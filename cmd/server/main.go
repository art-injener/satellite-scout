package main

import (
	"log/slog"
	"os"
)

func main() {
	setupLogger()

	configStore, err := loadConfig()
	if err != nil {
		slog.Error("failed to load config", slogKeyError, err)
		os.Exit(1)
	}

	err = NewApp(configStore).Run()
	if err != nil {
		slog.Error("application stopped", slogKeyError, err)
		os.Exit(1)
	}
}
