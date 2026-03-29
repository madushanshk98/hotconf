package hotconf_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/madushanshk98/hotconf"
)

// ---- helpers ----------------------------------------------------------------

type testConfig struct {
	Port  int    `json:"port"`
	Debug bool   `json:"debug"`
	DSN   string `json:"dsn"`
}

func jsonLoader(data []byte, v *testConfig) error {
	return json.Unmarshal(data, v)
}

// writeConfig atomically writes JSON to path using a temp-file rename, which
// mirrors what most editors do and exercises the Rename event path.
func writeConfig(t *testing.T, path string, cfg testConfig) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("rename: %v", err)
	}
}

func tempConfigFile(t *testing.T, initial testConfig) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfig(t, path, initial)
	return path
}

// ---- tests ------------------------------------------------------------------

func TestNew_LoadsInitialConfig(t *testing.T) {
	path := tempConfigFile(t, testConfig{Port: 8080, Debug: true, DSN: "postgres://localhost/test"})

	w, err := hotconf.New[testConfig](path, hotconf.Options[testConfig]{Loader: jsonLoader})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Stop()

	got := w.Get()
	if got.Port != 8080 {
		t.Errorf("Port: got %d, want 8080", got.Port)
	}
	if !got.Debug {
		t.Errorf("Debug: got false, want true")
	}
}

func TestNew_FileNotFound(t *testing.T) {
	_, err := hotconf.New[testConfig]("/nonexistent/config.json", hotconf.Options[testConfig]{Loader: jsonLoader})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, hotconf.ErrFileNotFound) {
		t.Errorf("expected ErrFileNotFound, got: %v", err)
	}
}

func TestNew_NilLoaderReturnsError(t *testing.T) {
	_, err := hotconf.New[testConfig]("any.json", hotconf.Options[testConfig]{})
	if err == nil {
		t.Fatal("expected error for nil Loader, got nil")
	}
}

func TestNew_ParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("NOT JSON"), 0o644)

	_, err := hotconf.New[testConfig](path, hotconf.Options[testConfig]{Loader: jsonLoader})
	if !errors.Is(err, hotconf.ErrParseFailed) {
		t.Errorf("expected ErrParseFailed, got: %v", err)
	}
}

func TestNew_ValidationError(t *testing.T) {
	path := tempConfigFile(t, testConfig{Port: 0})

	_, err := hotconf.New[testConfig](path, hotconf.Options[testConfig]{
		Loader: jsonLoader,
		Validate: func(cfg testConfig) error {
			if cfg.Port == 0 {
				return fmt.Errorf("port must not be zero")
			}
			return nil
		},
	})
	if !errors.Is(err, hotconf.ErrValidationFailed) {
		t.Errorf("expected ErrValidationFailed, got: %v", err)
	}
}

func TestReload_UpdatesGet(t *testing.T) {
	path := tempConfigFile(t, testConfig{Port: 3000})

	w, err := hotconf.New[testConfig](path, hotconf.Options[testConfig]{
		Loader:   jsonLoader,
		Debounce: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Stop()

	writeConfig(t, path, testConfig{Port: 4000})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if w.Get().Port == 4000 {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Errorf("Get().Port: got %d after reload, want 4000", w.Get().Port)
}

func TestOnChange_CalledAfterReload(t *testing.T) {
	path := tempConfigFile(t, testConfig{Port: 1111})

	w, err := hotconf.New[testConfig](path, hotconf.Options[testConfig]{
		Loader:   jsonLoader,
		Debounce: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Stop()

	var mu sync.Mutex
	var got []int
	w.OnChange(func(old, new testConfig) {
		mu.Lock()
		got = append(got, new.Port)
		mu.Unlock()
	})

	writeConfig(t, path, testConfig{Port: 2222})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("OnChange callback was never called")
	}
	if got[0] != 2222 {
		t.Errorf("OnChange new.Port: got %d, want 2222", got[0])
	}
}

func TestOnChange_OldValueCorrect(t *testing.T) {
	path := tempConfigFile(t, testConfig{Port: 5000})

	w, err := hotconf.New[testConfig](path, hotconf.Options[testConfig]{
		Loader:   jsonLoader,
		Debounce: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Stop()

	done := make(chan struct{})
	var oldPort int
	w.OnChange(func(old, new testConfig) {
		oldPort = old.Port
		close(done)
	})

	writeConfig(t, path, testConfig{Port: 6000})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("OnChange was not called within timeout")
	}

	if oldPort != 5000 {
		t.Errorf("old.Port: got %d, want 5000", oldPort)
	}
}

func TestOnError_CalledOnBadReload(t *testing.T) {
	path := tempConfigFile(t, testConfig{Port: 7000})

	w, err := hotconf.New[testConfig](path, hotconf.Options[testConfig]{
		Loader:   jsonLoader,
		Debounce: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Stop()

	errCh := make(chan error, 1)
	w.OnError(func(err error) {
		select {
		case errCh <- err:
		default:
		}
	})

	// Write invalid JSON to trigger a parse error.
	os.WriteFile(path, []byte("{bad json}"), 0o644)

	select {
	case err := <-errCh:
		if !errors.Is(err, hotconf.ErrParseFailed) {
			t.Errorf("expected ErrParseFailed, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnError was not called within timeout")
	}

	// Config must not have changed.
	if w.Get().Port != 7000 {
		t.Errorf("Get().Port after bad reload: got %d, want 7000", w.Get().Port)
	}
}

func TestStop_SafeToCallTwice(t *testing.T) {
	path := tempConfigFile(t, testConfig{Port: 9090})

	w, err := hotconf.New[testConfig](path, hotconf.Options[testConfig]{Loader: jsonLoader})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w.Stop()
	w.Stop() // must not panic
}

func TestMultipleOnChangeCallbacks(t *testing.T) {
	path := tempConfigFile(t, testConfig{Port: 100})

	w, err := hotconf.New[testConfig](path, hotconf.Options[testConfig]{
		Loader:   jsonLoader,
		Debounce: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Stop()

	var wg sync.WaitGroup
	wg.Add(3)
	for i := 0; i < 3; i++ {
		w.OnChange(func(old, new testConfig) { wg.Done() })
	}

	writeConfig(t, path, testConfig{Port: 200})

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("not all OnChange callbacks were called")
	}
}

func TestConcurrentGet(t *testing.T) {
	path := tempConfigFile(t, testConfig{Port: 8888})

	w, err := hotconf.New[testConfig](path, hotconf.Options[testConfig]{
		Loader:   jsonLoader,
		Debounce: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Stop()

	var wg sync.WaitGroup
	// 20 goroutines hammering Get() while the file is being written.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = w.Get()
			}
		}()
	}

	for i := 0; i < 5; i++ {
		writeConfig(t, path, testConfig{Port: 8000 + i})
		time.Sleep(10 * time.Millisecond)
	}

	wg.Wait()
}
