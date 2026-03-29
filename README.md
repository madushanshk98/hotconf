# hotconf

Hot-reload your config file without restarting your Go process.

```
go get github.com/madushanshk98/hotconf
```

Requires **Go 1.21+** (uses generics and `atomic.Pointer[T]`).

---

## Why

Restarting a Go service to pick up a config change breaks open connections,
resets in-memory state, and adds deployment friction. `hotconf` watches your
config file with the OS file-system API, parses the new content into your
existing struct, and swaps it atomically — so `w.Get()` in your HTTP handlers
always returns the current config with zero locks and zero allocations.

---

## Quick start

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "net/http"

    "github.com/madushanshk98/hotconf"
)

type Config struct {
    Port     string `json:"port"`
    LogLevel string `json:"log_level"`
    Debug    bool   `json:"debug"`
}

func main() {
    w, err := hotconf.New[Config]("config.json", hotconf.Options[Config]{
        Loader: json.Unmarshal, // bring your own decoder
    })
    if err != nil {
        log.Fatal(err)
    }
    defer w.Stop()

    // Called every time the file reloads successfully.
    w.OnChange(func(old, new Config) {
        log.Printf("reloaded: debug %v → %v", old.Debug, new.Debug)
    })

    // Called when a reload fails — old config stays active.
    w.OnError(func(err error) {
        log.Printf("bad config, keeping old: %v", err)
    })

    http.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
        cfg := w.Get() // goroutine-safe, lock-free
        fmt.Fprintf(rw, "port=%s debug=%v\n", cfg.Port, cfg.Debug)
    })

    log.Fatal(http.ListenAndServe(":"+w.Get().Port, nil))
}
```

---

## API

### `hotconf.New[T](path string, opts Options[T]) (*Watcher[T], error)`

Creates a watcher. Performs an initial synchronous load — returns an error if
the file is missing, unparseable, or fails validation. Safe to call once at
startup.

### `w.Get() T`

Returns the current config value. Goroutine-safe and allocation-free on the
hot path. Call it at the point of use; do not cache the result across requests.

### `w.OnChange(fn func(old, new T))`

Registers a callback invoked after every successful reload. Multiple callbacks
are supported and called in registration order.

### `w.OnError(fn func(err error))`

Registers a handler called when a reload fails. The watcher keeps the previous
valid config. Use this to log, alert, or increment a metric.

### `w.Stop()`

Shuts down the file watcher and releases OS resources. Safe to call multiple
times. Typically deferred in `main`.

---

## Options

```go
hotconf.Options[T]{
    // Required. Unmarshal bytes into T.
    // json.Unmarshal, yaml.Unmarshal, toml.Unmarshal all work directly.
    Loader func(data []byte, v *T) error

    // Optional. Called after Loader. Return non-nil to reject the new config.
    Validate func(cfg T) error

    // How long to wait after the last fs event before reloading.
    // Default: 100ms. Increase if your editor emits many rapid events.
    Debounce time.Duration
}
```

---

## Format support

`hotconf` is format-agnostic — pass any decoder as `Loader`:

```go
// JSON (stdlib)
hotconf.Options[Config]{Loader: json.Unmarshal}

// YAML (gopkg.in/yaml.v3)
hotconf.Options[Config]{Loader: yaml.Unmarshal}

// TOML (github.com/BurntSushi/toml)
hotconf.Options[Config]{Loader: func(data []byte, v *Config) error {
    return toml.Unmarshal(data, v)
}}

// .env file (github.com/joho/godotenv + encoding)
hotconf.Options[Config]{Loader: myEnvLoader}
```

---

## How it works

```
config.yaml  →  fsnotify  →  debouncer  →  loader  →  atomic.Pointer[T]  →  w.Get()
                                                   ↘  OnChange callbacks
                                                   ↘  OnError handler
```

1. `fsnotify` watches the file using OS-native APIs (`inotify` on Linux,
   `FSEvents` on macOS, `ReadDirectoryChangesW` on Windows).
2. The **debouncer** waits 100 ms after the last event before triggering a
   reload, batching the rapid write+chmod+rename sequence most editors emit.
3. The **loader** reads the file, calls your `Loader` func, and optionally
   calls `Validate`.
4. On success, `atomic.Pointer[T]` is swapped — `w.Get()` is instantly
   consistent across all goroutines with no mutex.
5. On failure, the previous value is kept and `OnError` is called.

Editor compatibility: vim, Emacs, VS Code, nano, and most other editors are
handled. Vim and Emacs write via rename; `hotconf` re-adds the watch target
after rename events automatically.

---

## Dropping into an existing backend

`hotconf` makes zero assumptions about your framework. Replace your global
config variable with `w.Get()` at the call site:

```go
// Before
var cfg = loadConfigOnce()
handler := func(w http.ResponseWriter, r *http.Request) {
    fmt.Fprintln(w, cfg.Greeting) // stale after deploy
}

// After
watcher, _ := hotconf.New[Config]("config.json", opts)
handler := func(w http.ResponseWriter, r *http.Request) {
    fmt.Fprintln(w, watcher.Get().Greeting) // always current
}
```

See `_examples/stdlib` and `_examples/gin` for runnable examples.

---

## Errors

| Error | When |
|---|---|
| `hotconf.ErrFileNotFound` | File does not exist at startup |
| `hotconf.ErrParseFailed` | `Loader` returned an error |
| `hotconf.ErrValidationFailed` | `Validate` returned an error |

All errors wrap a cause so `errors.Is` and `errors.As` work normally.

---

## License

MIT
