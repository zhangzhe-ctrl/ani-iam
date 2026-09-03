package data

import "errors"

var (
	// ErrDependencyUnavailable is the stable boundary returned when a real
	// PostgreSQL or Redis dependency cannot complete an operation.
	ErrDependencyUnavailable = errors.New("legacy storage dependency unavailable")
	ErrUnsafeRuntimeRole     = errors.New("legacy postgres runtime role is not restricted")
	ErrCacheMiss             = errors.New("legacy redis key not found")
)

type dependencyError struct {
	dependency string
	operation  string
	cause      error
}

func (e *dependencyError) Error() string {
	return "legacy storage dependency unavailable: " + e.dependency + " " + e.operation
}

func (e *dependencyError) Unwrap() error { return e.cause }

func (e *dependencyError) Is(target error) bool {
	return target == ErrDependencyUnavailable
}

func unavailable(dependency, operation string, cause error) error {
	return &dependencyError{dependency: dependency, operation: operation, cause: cause}
}
