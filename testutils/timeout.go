package testutils

import "time"

// RunWithTimeout runs operation synchronously and interrupts it if timeout
// expires. interrupt must unblock operation (for example by expiring a native
// deadline or closing an HTTP body). Both operation and interrupt have finished
// when this function returns, so neither can retain the caller's buffers.
// Existing deadlines are left alone unless the watchdog expires.
func RunWithTimeout(timeout time.Duration, interrupt func(), operation func() (int, error)) (int, error) {
	interrupted := make(chan struct{})
	timer := time.AfterFunc(timeout, func() {
		defer close(interrupted)
		interrupt()
	})
	defer func() {
		if !timer.Stop() {
			<-interrupted
		}
	}()
	return operation()
}
