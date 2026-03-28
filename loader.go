package hotconf

import (
	"fmt"
	"os"
)

// loadFile reads the file at path, calls opts.Loader to unmarshal the bytes
// into T, then (optionally) calls opts.Validate. Returns the parsed value or
// a wrapped sentinel error.
func loadFile[T any](path string, opts Options[T]) (T, error) {
	var zero T

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, fmt.Errorf("%w: %s", ErrFileNotFound, path)
		}
		return zero, fmt.Errorf("%w: %s: %v", ErrParseFailed, path, err)
	}

	var cfg T
	if err := opts.Loader(data, &cfg); err != nil {
		return zero, fmt.Errorf("%w: %v", ErrParseFailed, err)
	}

	if opts.Validate != nil {
		if err := opts.Validate(cfg); err != nil {
			return zero, fmt.Errorf("%w: %v", ErrValidationFailed, err)
		}
	}

	return cfg, nil
}
