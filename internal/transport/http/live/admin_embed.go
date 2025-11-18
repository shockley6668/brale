package livehttp

import (
	"embed"
	"io/fs"
	"net/http"
	"path"

	"github.com/gin-gonic/gin"
)

//go:embed admin/*
var adminAssets embed.FS

func registerAdminRoutes(router *gin.Engine) {
	sub, err := fs.Sub(adminAssets, "admin")
	if err != nil {
		return
	}

	serveFile := func(name string, c *gin.Context) {
		data, err := fs.ReadFile(sub, name)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, mimeType(name), data)
	}

	router.GET("/admin", func(c *gin.Context) { serveFile("index.html", c) })
	router.GET("/admin/", func(c *gin.Context) { serveFile("index.html", c) })
	router.GET("/admin/:asset", func(c *gin.Context) {
		file := c.Param("asset")
		if file == "" {
			file = "index.html"
		}
		serveFile(file, c)
	})
}

func mimeType(name string) string {
	switch path.Ext(name) {
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	default:
		return "text/html"
	}
}
