package main

import (
	"embed"
	"log"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	service := NewApp()
	app := application.New(application.Options{
		Name:        "ESP32 NVS Key-Pair Manager",
		Description: "Manage GNSS ESP32 configuration over USB",
		Services:    []application.Service{application.NewService(service)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "ESP32 NVS Key-Pair Manager", Width: 1100, Height: 800, MinWidth: 1024, MinHeight: 700,
		EnableFileDrop: true, BackgroundColour: application.NewRGB(12, 18, 29),
		Frameless: true,
		Windows:   application.WindowsWindow{Theme: application.Dark},
	})
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		app.Event.Emit("files:dropped", e.Context().DroppedFiles())
	})
	var frontendBusy atomic.Bool
	app.Event.On("app:busy", func(e *application.CustomEvent) {
		if value, ok := e.Data.(bool); ok {
			frontendBusy.Store(value)
		}
	})
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if service.active.Load() > 0 || frontendBusy.Load() {
			e.Cancel()
			dialog := app.Dialog.Warning().SetTitle("Operation in progress").SetMessage("Wait for the current operation before closing. Writes already sent to the ESP32 cannot be canceled.").AttachToWindow(win)
			dialog.AddButton("Stay open").SetAsDefault().SetAsCancel()
			dialog.Show()
		}
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
