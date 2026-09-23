package quic

import (
	"errors"
	"fmt"
)

var errManagedDFUnavailable = errors.New("quic: managed DF unavailable")

// Native options, including their values and admitted families, are owned by
// the platform socket API. This boundary also permits deterministic OS failures
// in lifecycle tests without granting a public descriptor capability.
type (
	managedDFOption struct{ level, name, value int }
	managedDFSocket interface {
		options() ([]managedDFOption, error)
		get(managedDFOption) (int, error)
		set(managedDFOption, int) error
	}
)
type managedDFControl func(func(managedDFSocket) error) error

type managedDFSetup struct {
	restore  func() error
	terminal bool
}

// setupManagedDF runs with the endpoint locked and no active I/O. All old values
// are read before the first write. A failed write is considered a mutation too:
// optional fallback is allowed only after every attempted option is restored.
func setupManagedDF(control managedDFControl) (managedDFSetup, error) {
	if control == nil {
		return managedDFSetup{}, nil
	}
	var saved []managedDFOption
	attempted := 0
	restore := func() error {
		return control(func(socket managedDFSocket) error {
			var errs []error
			for i := attempted - 1; i >= 0; i-- {
				option := saved[i]
				if value, err := socket.get(option); err == nil && value == option.value {
					continue
				}
				if err := socket.set(option, option.value); err != nil {
					errs = append(errs, err)
					continue
				}
				value, err := socket.get(option)
				if err != nil {
					errs = append(errs, err)
				} else if value != option.value {
					errs = append(errs, errors.New("quic: managed DF restoration did not retain saved value"))
				}
			}
			return errors.Join(errs...)
		})
	}
	err := control(func(socket managedDFSocket) error {
		options, err := socket.options()
		if err != nil {
			return err
		}
		for _, option := range options {
			value, err := socket.get(option)
			if err != nil {
				return err
			}
			saved = append(saved, managedDFOption{level: option.level, name: option.name, value: value})
		}
		for _, option := range options {
			attempted++
			if err := socket.set(option, option.value); err != nil {
				return err
			}
			value, err := socket.get(option)
			if err != nil {
				return err
			}
			if value != option.value {
				return errors.New("quic: managed DF setup did not retain requested value")
			}
		}
		return nil
	})
	if err != nil {
		if attempted != 0 {
			if rollbackErr := restore(); rollbackErr != nil {
				return managedDFSetup{terminal: true}, errors.Join(err, fmt.Errorf("quic: managed DF rollback: %w", rollbackErr))
			}
		}
		if errors.Is(err, errManagedDFUnavailable) {
			return managedDFSetup{}, nil
		}
		return managedDFSetup{}, err
	}
	if attempted == 0 {
		return managedDFSetup{}, nil
	}
	return managedDFSetup{restore: restore}, nil
}
