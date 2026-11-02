//go:build linux || darwin || windows

package quic

import (
	"errors"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/testutils"
	"github.com/stretchr/testify/require"
)

func TestMultipathCreditNonAcceptingReceipts(t *testing.T) {
	uncertain := errors.New("socket submission progress unknown")
	for _, test := range []struct {
		name     string
		writeErr error
		wantErr  error
	}{{"fenced", errSubmissionDenied, nil}, {"rejected-size", testutils.SendMsgSizeErr, nil}, {"uncertain", uncertain, uncertain}} {
		t.Run(test.name, func(t *testing.T) {
			tc, route, _ := newCreditTestConnection(t, 2, 1)
			packedCreditFrames(t, tc)
			declineCreditPath(t, tc, monotime.Now())
			require.NoError(t, tc.conn.triggerSending(monotime.Now()).err)
			q := tc.conn.emission.queue.(*sendQueue)
			q.conn = &fakeBatchSendConn{writeErr: func(_ int, _ []byte) error { return test.writeErr }}
			q.authorized = nil
			close(q.closeCalled)
			err := q.Run()
			if test.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, test.wantErr)
			}
			require.NoError(t, tc.conn.pathCredit.expire(monotime.Now().Add(time.Hour)))
			require.Len(t, route.handlers, 4, "non-acceptance cannot erase unanswered routing obligations")
			require.Zero(t, tc.conn.pathCredit.nextExpiry())
		})
	}
}
