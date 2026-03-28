// Package hotconf provides hot-reloading of configuration files without
// restarting the process. It watches a config file for changes, parses it
// into a user-defined struct, and swaps the value atomically so concurrent
// reads from HTTP handlers or goroutines are always safe and lock-free.
//
// Basic usage:
//
//	type AppConfig struct {
//	    Port    int    `json:"port"`
//	    Debug   bool   `json:"debug"`
//	    DSN     string `json:"dsn"`
//	}
//
//	w, err := hotconf.New[AppConfig]("config.json", hotconf.Options[AppConfig]{
//	    Loader: json.Unmarshal,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer w.Stop()
//
//	w.OnChange(func(old, new AppConfig) {
//	    log.Printf("config reloaded: port %d -> %d", old.Port, new.Port)
//	})
//
//	w.OnError(func(err error) {
//	    log.Printf("bad config, keeping previous: %v", err)
//	})
//
//	// Anywhere in your application:
//	cfg := w.Get()
//	fmt.Println(cfg.Port)
package hotconf

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches a config file and hot-reloads it into T whenever the file
// changes. All methods are safe for concurrent use.
type Watcher[T any] struct {
	path    string
	opts    Options[T]
	current atomic.Pointer[T]

	// onChange and onError are guarded by mu so callbacks can be registered
	// after New returns (e.g. inside main after the watcher is created).
	mu        sync.RWMutex
	callbacks []func(old, new T)
	errHandler func(err error)

	fsw      *fsnotify.Watcher
	debounce *debouncer
	stopOnce sync.Once
	done     chan struct{}
}

// New creates a Watcher that watches path and parses it into T using opts.
// It performs an initial load synchronously so that the first call to Get()
// never returns a zero value. Returns ErrFileNotFound if the file does not
// exist, or ErrParseFailed / ErrValidationFailed if the initial parse fails.
func New[T any](path string, opts Options[T]) (*Watcher[T], error) {
	if opts.Loader == nil {
		return nil, fmt.Errorf("hotconf: Options.Loader must not be nil")
	}
	opts.applyDefaults()

	// Initial synchronous load — fail fast if the file is bad.
	initial, err := loadFile(path, opts)
	if err != nil {
		return nil, err
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("hotconf: could not create fs watcher: %w", err)
	}
	if err := fsw.Add(path); err != nil {
		fsw.Close()
		return nil, fmt.Errorf("hotconf: could not watch %s: %w", path, err)
	}

	w := &Watcher[T]{
		path: path,
		opts: opts,
		fsw:  fsw,
		done: make(chan struct{}),
	}
	w.current.Store(&initial)

	w.debounce = newDebouncer(opts.Debounce, w.reload)

	go w.run()

	return w, nil
}

// Get returns the current config value. It is goroutine-safe and allocation-free
// after the first load. Callers should not hold a reference to the returned
// value across long-running operations if they want to observe future reloads;
// instead, call Get() each time they need the current config.
func (w *Watcher[T]) Get() T {
	return *w.current.Load()
}

// OnChange registers fn to be called after every successful reload with the
// old and new config values. Multiple callbacks can be registered; they are
// called in registration order. OnChange is safe to call at any time,
// including after the watcher is running.
func (w *Watcher[T]) OnChange(fn func(old, new T)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.callbacks = append(w.callbacks, fn)
}

// OnError registers fn to be called whenever a reload fails (parse error,
// validation error, or read error). The previous config remains active.
// Replaces any previously registered error handler.
func (w *Watcher[T]) OnError(fn func(err error)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.errHandler = fn
}

// Stop shuts down the file watcher and releases all resources. It is safe to
// call Stop more than once; subsequent calls are no-ops.
func (w *Watcher[T]) Stop() {
	w.stopOnce.Do(func() {
		w.debounce.stop()
		w.fsw.Close()
		close(w.done)
	})
}

// run is the background goroutine that reads fsnotify events.
func (w *Watcher[T]) run() {
	for {
		select {
		case <-w.done:
			return

		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			// We care about writes and renames (editors like vim use rename).
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				w.debounce.trigger()

				// Some editors (vim, Emacs) write to a temp file and rename it
				// into place. After a rename the original watch target is gone;
				// re-add it so we keep watching.
				if event.Has(fsnotify.Rename) {
					_ = w.fsw.Add(w.path)
				}
			}

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.notifyError(fmt.Errorf("hotconf: fs watcher error: %w", err))
		}
	}
}

// reload is called by the debouncer after the quiet period. It reads and
// parses the file, atomically swaps the current value, and notifies callbacks.
func (w *Watcher[T]) reload() {
	newCfg, err := loadFile(w.path, w.opts)
	if err != nil {
		w.notifyError(err)
		return
	}

	old := *w.current.Load()
	w.current.Store(&newCfg)

	w.mu.RLock()
	cbs := make([]func(old, new T), len(w.callbacks))
	copy(cbs, w.callbacks)
	w.mu.RUnlock()

	for _, cb := range cbs {
		cb(old, newCfg)
	}
}

// notifyError calls the registered error handler (if any).
func (w *Watcher[T]) notifyError(err error) {
	w.mu.RLock()
	fn := w.errHandler
	w.mu.RUnlock()

	if fn != nil {
		fn(err)
	}
}
