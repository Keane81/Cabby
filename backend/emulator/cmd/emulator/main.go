// Command emulator runs a park of emulated cabbers against a local Cabby stack: each cabber
// registers, logs in and keeps sending the positions of a walk through the public REST contract of
// the gateway, so that the system can be loaded the way real traffic would load it.
package main

import (
	"os"
	"os/signal"
	"syscall"
)

func main() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, signals))
}
