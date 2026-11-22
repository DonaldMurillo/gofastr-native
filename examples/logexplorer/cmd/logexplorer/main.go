package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/DonaldMurillo/gofastr/framework"

	"github.com/DonaldMurillo/gofastr-native/examples/logexplorer"
	"github.com/DonaldMurillo/gofastr-native/render/desktop"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	replay := logexplorer.NewReplay()
	window := logexplorer.NewWindow(ctx, replay)
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{
		Name: "Log Explorer", DisableSignalHandling: true,
	}))
	return app.Run(ctx, desktop.Host{View: window.View, Updates: replay.Updates()})
}
