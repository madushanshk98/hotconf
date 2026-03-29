// Example: using hotconf with a Gin HTTP server.
// This shows how to drop hotconf into an EXISTING Gin backend with minimal
// changes — just replace your global config variable with w.Get().
//
// Run:  go run .
// Then: edit config.json while the server is running.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/madushanshk98/hotconf"
)

// AppConfig mirrors whatever struct your existing Gin app already uses.
type AppConfig struct {
	Port     string `json:"port"`
	GinMode  string `json:"gin_mode"` // "debug" | "release" | "test"
	LogLevel string `json:"log_level"`
	Greeting string `json:"greeting"`
}

func main() {
	// Write a default config if missing.
	if _, err := os.Stat("config.json"); os.IsNotExist(err) {
		def := AppConfig{Port: "8080", GinMode: "debug", LogLevel: "info", Greeting: "Hello"}
		data, _ := json.MarshalIndent(def, "", "  ")
		os.WriteFile("config.json", data, 0o644)
	}

	w, err := hotconf.New[AppConfig]("config.json", hotconf.Options[AppConfig]{
		Loader: json.Unmarshal,
		Validate: func(cfg AppConfig) error {
			if cfg.Port == "" {
				return fmt.Errorf("port must not be empty")
			}
			return nil
		},
	})
	if err != nil {
		log.Fatalf("hotconf: %v", err)
	}
	defer w.Stop()

	w.OnChange(func(old, new AppConfig) {
		log.Printf("[hotconf] config reloaded: %+v", new)
	})
	w.OnError(func(err error) {
		log.Printf("[hotconf] bad config (keeping old): %v", err)
	})

	gin.SetMode(w.Get().GinMode)
	r := gin.Default()

	// Middleware: inject current config into every request context.
	// Handlers can then read it with c.MustGet("cfg").(AppConfig).
	r.Use(func(c *gin.Context) {
		c.Set("cfg", w.Get())
		c.Next()
	})

	r.GET("/", func(c *gin.Context) {
		cfg := c.MustGet("cfg").(AppConfig)
		c.String(http.StatusOK, "%s from hotconf! (log_level=%s)\n", cfg.Greeting, cfg.LogLevel)
	})

	r.GET("/config", func(c *gin.Context) {
		c.JSON(http.StatusOK, w.Get())
	})

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	addr := ":" + w.Get().Port
	log.Printf("gin server on %s — edit config.json to hot-reload", addr)
	r.Run(addr)
}
