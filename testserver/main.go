package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/nicolasbonnici/gorest"
	"github.com/nicolasbonnici/gorest-benchmark/generated/resources"
	"github.com/nicolasbonnici/gorest/database"
	"github.com/nicolasbonnici/gorest/plugin"
	"github.com/nicolasbonnici/gorest/pluginloader"

	statusplugin "github.com/nicolasbonnici/gorest-status"
)

func init() {
	pluginloader.RegisterPluginFactory("status", statusplugin.NewPlugin)
}

func main() {
	cfg := gorest.Config{
		ConfigPath: ".",
		RegisterRoutes: func(router fiber.Router, db database.Database, paginationLimit int, paginationMaxLimit int, pluginRegistry *plugin.PluginRegistry) {
			app, ok := router.(*fiber.App)
			if !ok {
				panic("router is not a *fiber.App")
			}
			resources.RegisterGeneratedRoutes(app, db, paginationLimit, paginationMaxLimit, pluginRegistry)
		},
	}

	gorest.Start(cfg)
}
