package hotconf

import "errors"

// ErrFileNotFound is returned when the config file does not exist at startup.
var ErrFileNotFound = errors.New("hotconf: config file not found")

// ErrParseFailed is returned when the Loader func fails to unmarshal the file bytes.
var ErrParseFailed = errors.New("hotconf: failed to parse config file")

// ErrValidationFailed is returned when the optional Validate func rejects the parsed value.
var ErrValidationFailed = errors.New("hotconf: config validation failed")

// ErrAlreadyStopped is returned when Stop is called more than once.
var ErrAlreadyStopped = errors.New("hotconf: watcher already stopped")
