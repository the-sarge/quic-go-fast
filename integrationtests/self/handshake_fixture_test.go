package self_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/quic-go/quic-go"
)

type configForClientResult struct {
	conn *quic.Conn
	err  error
}

// A configForClientWorker publishes once, then owns its connection until cleanup.
// Publication cannot block if the owning test stops consuming results.
type configForClientWorker struct {
	result chan configForClientResult
	done   chan struct{}
}

func startConfigForClientWorker(ctx context.Context, operation func(context.Context) (*quic.Conn, error)) *configForClientWorker {
	worker := &configForClientWorker{result: make(chan configForClientResult, 1), done: make(chan struct{})}
	go func() {
		defer close(worker.done)
		conn, err := operation(ctx)
		worker.result <- configForClientResult{conn: conn, err: err}
		if conn != nil {
			<-ctx.Done()
			conn.CloseWithError(0, "config-for-client fixture shutdown")
		}
	}()
	return worker
}

func (w *configForClientWorker) join() error {
	select {
	case <-w.done:
		return nil
	case <-time.After(time.Second):
		return errors.New("timeout joining config-for-client worker")
	}
}

func waitConfigForClient(accept, dial *configForClientWorker) (*quic.Conn, error) {
	acceptResult, dialResult := accept.result, dial.result
	var conn *quic.Conn
	var timeout <-chan time.Time
	for acceptResult != nil || dialResult != nil {
		select {
		case result := <-acceptResult:
			if result.err != nil {
				return nil, fmt.Errorf("config-for-client accepting connection: %w", result.err)
			}
			acceptResult = nil
		case result := <-dialResult:
			if result.err != nil {
				// Prefer an already-published accept failure over its client symptom.
				if acceptResult != nil {
					select {
					case accepted := <-acceptResult:
						if accepted.err != nil {
							return nil, fmt.Errorf("config-for-client accepting connection: %w", accepted.err)
						}
					default:
					}
				}
				return nil, fmt.Errorf("config-for-client dialing: %w", result.err)
			}
			conn = result.conn
			dialResult = nil
			timeout = time.After(time.Second)
		case <-timeout:
			return nil, errors.New("timeout waiting for config-for-client accept result")
		}
	}
	return conn, nil
}
