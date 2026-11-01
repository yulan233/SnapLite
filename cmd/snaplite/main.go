// Command snaplite is 简截 SnapLite: a tray screenshot tool.
package main

import (
	"log"

	"github.com/vivaui/viva/app"

	"snaplite/internal/config"
	"snaplite/internal/shell"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("config: %v (using defaults)", err)
	}
	err = app.RunApplication(func(a *app.Application) error {
		s := shell.New(a, cfg)
		// Panics Viva catches at native boundaries (painting, window
		// procedures) end up in snaplite.log instead of vanishing.
		app.SetFaultHandler(s.Fault)
		return s.Start()
	})
	if err != nil {
		log.Fatal(err)
	}
}
