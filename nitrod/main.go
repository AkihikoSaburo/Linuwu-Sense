package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	"nitrod/hw"
	"nitrod/server"
)

func main() {
	if err := run(); err != nil {
		slog.Error("nitrod stopped with an error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("nitrod", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	restoreAuto := flags.Bool("restore-fan-auto", false, "restore both fans to automatic control and exit")
	socketPath := flags.String("socket", "/run/nitrod.sock", "UNIX socket path")
	socketGroup := flags.String("socket-group", "nitrod", "group allowed to access the socket")
	sysRoot := flags.String("sysfs-root", "/sys", "sysfs root (primarily for tests)")
	devRoot := flags.String("dev-root", "/dev", "device root (primarily for tests)")
	fanTemperatureLimit := flags.Float64("fan-temp-limit", 85, "temperature in Celsius that forces manual fans back to auto")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if math.IsNaN(*fanTemperatureLimit) || math.IsInf(*fanTemperatureLimit, 0) || *fanTemperatureLimit <= 0 {
		return fmt.Errorf("--fan-temp-limit must be a finite positive temperature")
	}

	backend := hw.NewLinux(hw.Options{SysRoot: *sysRoot, DevRoot: *devRoot})
	defer backend.Close()
	owner := newOwner(backend, *fanTemperatureLimit, 5*time.Second)
	ownerContext, stopOwner := context.WithCancel(context.Background())
	defer stopOwner()
	go owner.Run(ownerContext)

	if *restoreAuto {
		return owner.Shutdown(context.Background())
	}

	unixServer := server.New(server.Config{Path: *socketPath, Group: *socketGroup}, owner)
	if err := unixServer.Listen(); err != nil {
		_ = owner.Shutdown(context.Background())
		stopOwner()
		return err
	}
	serveContext, stopServer := context.WithCancel(context.Background())
	defer stopServer()
	serveResult := make(chan error, 1)
	go func() { serveResult <- unixServer.Serve(serveContext) }()

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	select {
	case received := <-signals:
		slog.Info("received shutdown signal", "signal", received.String())
		stopServer()
		_ = unixServer.Close()
		<-serveResult
	case err := <-serveResult:
		if err != nil {
			stopServer()
			_ = unixServer.Close()
			_ = owner.Shutdown(context.Background())
			stopOwner()
			return err
		}
	}
	if err := owner.Shutdown(context.Background()); err != nil {
		stopOwner()
		return err
	}
	stopOwner()
	return nil
}
