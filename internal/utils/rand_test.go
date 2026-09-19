package utils

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRandomNumbers(t *testing.T) {
	const (
		num = 1000
		max = 12345678
	)

	var r Rand
	for range num {
		v := r.Int31n(max)
		require.GreaterOrEqual(t, v, int32(0))
		require.Less(t, v, int32(max))
	}
}

func TestRandomNumberConversions(t *testing.T) {
	const (
		childEnv  = "QUIC_GO_RAND_CONVERSION_CHILD"
		completed = "quic-go RNG conversion corpus: 14 cases completed"
	)
	if os.Getenv(childEnv) != "1" {
		executable, err := os.Executable()
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestRandomNumberConversions$", "-test.timeout=25s")
		cmd.Env = append(os.Environ(), childEnv+"=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, ctx.Err(), "%s", output)
		require.NoError(t, err, "%s", output)
		require.Contains(t, string(output), completed, "%s", output)
		return
	}

	// Only this child process replaces the shared entropy source. Fixtures run
	// sequentially and restore it before the next case. Trust crypto/rand for
	// entropy quality; these literal expectations test our conversion semantics.
	cases := []struct {
		name  string
		words []uint32
		bound int32 // zero selects Int31; positive values select Int31n
		want  int32
		calls int
	}{
		{name: "Int31 zero", words: []uint32{0}, want: 0, calls: 1},
		{name: "Int31 big endian", words: []uint32{0x01020304}, want: 0x01020304, calls: 1},
		{name: "Int31 high bit", words: []uint32{0x80000000}, want: 0, calls: 1},
		{name: "Int31 maximum", words: []uint32{0xffffffff}, want: 2147483647, calls: 1},
		{name: "bound one", words: []uint32{0xffffffff}, bound: 1, want: 0, calls: 1},
		{name: "power of two zero", words: []uint32{0}, bound: 8, want: 0, calls: 1},
		{name: "power of two mask", words: []uint32{15}, bound: 8, want: 7, calls: 1},
		{name: "non power of two zero", words: []uint32{0}, bound: 12345678, want: 0, calls: 1},
		{name: "below bound", words: []uint32{12345677}, bound: 12345678, want: 12345677, calls: 1},
		{name: "at bound", words: []uint32{12345678}, bound: 12345678, want: 0, calls: 1},
		{name: "last accepted word", words: []uint32{2135802293}, bound: 12345678, want: 12345677, calls: 1},
		{name: "two rejections", words: []uint32{2135802294, 2147483647, 42}, bound: 12345678, want: 42, calls: 1},
		{name: "maximum bound rejection", words: []uint32{2147483647, 2147483646}, bound: 2147483647, want: 2147483646, calls: 1},
		{name: "valid extreme sample", words: make([]uint32, 1000), bound: 12345678, want: 0, calls: 1000},
	}
	passed := 0
	for _, tc := range cases {
		if t.Run(tc.name, func(t *testing.T) {
			var input []byte
			for _, word := range tc.words {
				input = binary.BigEndian.AppendUint32(input, word)
			}
			reader := bytes.NewReader(input)
			original := rand.Reader
			rand.Reader = reader
			t.Cleanup(func() { rand.Reader = original })

			var r Rand
			for range tc.calls {
				var got int32
				if tc.bound == 0 {
					got = r.Int31()
				} else {
					got = r.Int31n(tc.bound)
				}
				require.Equal(t, tc.want, got)
			}
			require.Zero(t, reader.Len(), "conversion must consume exactly the supplied words")
		}) {
			passed++
		}
	}
	require.Equal(t, 14, passed, "the entire conversion corpus must pass")
	fmt.Println(completed)
}
