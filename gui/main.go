package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"memcached-gui/service"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	connSvc, err := service.NewConnectionService()
	if err != nil {
		println("Error initializing connection service:", err.Error())
		return
	}

	opSvc := service.NewOperationService(connSvc)

	app := NewApp()

	err = wails.Run(&options.App{
		Title:  "Memcached GUI",
		Width:  1200,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
			connSvc,
			opSvc,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
