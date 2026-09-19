package self_test

import (
	"errors"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

func TestMultiplexServerLifecycle(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "writer failure"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newMultiplexTest()
			defer func() { require.NoError(t, fixture.shutdown()) }()
			ln, err := quic.Listen(newUDPConnLocalhost(t), getTLSConfig(), getQuicConfig(nil))
			require.NoError(t, err)
			original := errors.New("controlled multiplex write failure")
			writerDone := make(chan struct{})
			fixture.serve(ln, func(str *quic.SendStream) error {
				defer close(writerDone)
				if fail {
					return original
				}
				_, err := str.Write(PRData)
				return err
			})
			tr := &quic.Transport{Conn: newUDPConnLocalhost(t)}
			fixture.transports = append(fixture.transports, tr)
			result := fixture.receive(tr, ln.Addr())
			if fail {
				// Leave the client result unconsumed, as happens after a parent fatal assertion.
				require.ErrorIs(t, fixture.wait(nil, time.Second), original)
			} else {
				require.NoError(t, fixture.wait(result, 5*time.Second))
			}
			require.NoError(t, fixture.shutdown())
			select {
			case <-writerDone:
			default:
				t.Fatal("fixture completed before nested writer")
			}
			if fail {
				require.ErrorIs(t, fixture.failure(), original)
			}
		})
	}
}
