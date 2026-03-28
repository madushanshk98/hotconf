package hotconf

import "time"

// Options configures the behaviour of a Watcher.
type Options[T any] struct {
	// Loader unmarshals raw file bytes into T.
	// Required. Example: json.Unmarshal, yaml.Unmarshal.
	//
	// The function signature matches encoding/json.Unmarshal so you can pass
	// it directly:
	//   hotconf.New[Config]("config.json", hotconf.Options[Config]{
	//       Loader: json.Unmarshal,
	//   })
	Loader func(data []byte, v *T) error

	// Validate is called after Loader succeeds. Return a non-nil error to
	// reject the new config; the watcher will keep the previous value and
	// call OnError instead.
	// Optional.
	Validate func(cfg T) error

	// Debounce is how long the watcher waits after the last file-system event
	// before triggering a reload. Editors often emit multiple events for a
	// single save (write + chmod + rename). Default: 100ms.
	Debounce time.Duration
}

func (o *Options[T]) applyDefaults() {
	if o.Debounce == 0 {
		o.Debounce = 100 * time.Millisecond
	}
}
