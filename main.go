// Example: using hotconf with the standard net/http server.
// Run:  go run .
// Then: edit config.json while the server is running and watch it reload.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/yourname/hotconf"
)

// AppConfig is your application's configuration struct. Add whatever fields
// your app needs.
type AppConfig struct {
	Port     string `json:"port"`
	LogLevel string `json:"log_level"`
	Debug    bool   `json:"debug"`
	Greeting string `json:"greeting"`
}

func validate(cfg AppConfig) error {
	if cfg.Port == "" {
		return fmt.Errorf("port must not be empty")
	}
	return nil
}

func main() {
	// Write a default config.json if it does not exist.
	if _, err := os.Stat("config.json"); os.IsNotExist(err) {
		def := AppConfig{Port: "8080", LogLevel: "info", Debug: false, Greeting: "Hello"}
		data, _ := json.MarshalIndent(def, "", "  ")
		os.WriteFile("config.json", data, 0o644)
		log.Println("created default config.json")
	}

	w, err := hotconf.New[AppConfig]("config.json", hotconf.Options[AppConfig]{
		Loader:   json.Unmarshal,
		Validate: validate,
	})
	if err != nil {
		log.Fatalf("hotconf: %v", err)
	}
	defer w.Stop()

	w.OnChange(func(old, new AppConfig) {
		log.Printf("[hotconf] reloaded — port: %s → %s | debug: %v → %v | greeting: %q → %q",
			old.Port, new.Port, old.Debug, new.Debug, old.Greeting, new.Greeting)
	})

	w.OnError(func(err error) {
		log.Printf("[hotconf] reload error (keeping previous config): %v", err)
	})

	mux := http.NewServeMux()

	// Every request reads the current config — no restart needed.
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		cfg := w.Get()
		fmt.Fprintf(rw, "%s from hotconf! (debug=%v, log_level=%s)\n",
			cfg.Greeting, cfg.Debug, cfg.LogLevel)
	})

	mux.HandleFunc("/config", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(w.Get())
	})

	addr := ":" + w.Get().Port
	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		log.Printf("listening on %s — edit config.json to hot-reload", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	// Graceful shutdown on SIGINT / SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down")
}
