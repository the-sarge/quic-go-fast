package jsontext_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/quic-go/quic-go/qlogwriter/jsontext"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncoderSimpleObject(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	enc.WriteToken(jsontext.BeginObject)
	enc.WriteToken(jsontext.String("foo"))
	enc.WriteToken(jsontext.String("bar"))
	enc.WriteToken(jsontext.String("foo2"))
	enc.WriteToken(jsontext.String("bar2"))
	enc.WriteToken(jsontext.EndObject)
	output := buf.String()

	var got map[string]string
	require.NoError(t, json.Unmarshal([]byte(output), &got))
	require.Equal(t, map[string]string{"foo": "bar", "foo2": "bar2"}, got)
}

func TestEncoderArrayInts(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	enc.WriteToken(jsontext.BeginArray)
	enc.WriteToken(jsontext.Int(1))
	enc.WriteToken(jsontext.Int(2))
	enc.WriteToken(jsontext.Int(3))
	enc.WriteToken(jsontext.EndArray)
	output := buf.String()

	var got []int
	require.NoError(t, json.Unmarshal([]byte(output), &got))
	require.Equal(t, []int{1, 2, 3}, got)
}

func TestEncoderArrayStrings(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	enc.WriteToken(jsontext.BeginArray)
	enc.WriteToken(jsontext.String("one"))
	enc.WriteToken(jsontext.String("two"))
	enc.WriteToken(jsontext.EndArray)
	output := buf.String()

	var got []string
	err := json.Unmarshal([]byte(output), &got)
	require.NoError(t, err)
	require.Equal(t, []string{"one", "two"}, got)
}

func TestEncoderNestedObject(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	enc.WriteToken(jsontext.BeginObject)
	enc.WriteToken(jsontext.String("outer"))
	enc.WriteToken(jsontext.BeginObject)
	enc.WriteToken(jsontext.String("inner"))
	enc.WriteToken(jsontext.String("value"))
	enc.WriteToken(jsontext.EndObject)
	enc.WriteToken(jsontext.EndObject)
	output := buf.String()

	var got map[string]map[string]string
	require.NoError(t, json.Unmarshal([]byte(output), &got))
	require.Equal(t, map[string]map[string]string{"outer": {"inner": "value"}}, got)
}

func TestEncoderNumbersAndBool(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	enc.WriteToken(jsontext.BeginObject)
	enc.WriteToken(jsontext.String("int"))
	enc.WriteToken(jsontext.Int(42))
	enc.WriteToken(jsontext.String("uint"))
	enc.WriteToken(jsontext.Uint(100))
	enc.WriteToken(jsontext.String("float"))
	enc.WriteToken(jsontext.Float(3.14))
	enc.WriteToken(jsontext.String("true"))
	enc.WriteToken(jsontext.True)
	enc.WriteToken(jsontext.String("false"))
	enc.WriteToken(jsontext.False)
	enc.WriteToken(jsontext.String("nullv"))
	enc.WriteToken(jsontext.Null)
	enc.WriteToken(jsontext.EndObject)
	output := buf.String()

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &got))
	require.Equal(t, map[string]any{
		"int":   float64(42), // json.Unmarshal decodes numbers as float64
		"uint":  float64(100),
		"float": 3.14,
		"true":  true,
		"false": false,
		"nullv": nil,
	}, got)
}

func TestEncoderEmptyObject(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	enc.WriteToken(jsontext.BeginObject)
	enc.WriteToken(jsontext.EndObject)
	output := buf.String()

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &got))
	require.Equal(t, map[string]any{}, got)
}

func TestEncoderEmptyArray(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	enc.WriteToken(jsontext.BeginArray)
	enc.WriteToken(jsontext.EndArray)
	output := buf.String()

	var got []any
	require.NoError(t, json.Unmarshal([]byte(output), &got))
	require.Equal(t, []any{}, got)
}

func TestEncoderArrayWithNulls(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	enc.WriteToken(jsontext.BeginArray)
	enc.WriteToken(jsontext.Null)
	enc.WriteToken(jsontext.String("x"))
	enc.WriteToken(jsontext.Null)
	enc.WriteToken(jsontext.EndArray)
	output := buf.String()

	var got []any
	require.NoError(t, json.Unmarshal([]byte(output), &got))
	require.Equal(t, []any{nil, "x", nil}, got)
}

func TestEncoderEscapedStrings(t *testing.T) {
	t.Run("no escapes", func(t *testing.T) {
		testEncoderEscapedStrings(t, "simplekey", "simplevalue")
	})

	t.Run("basic escapes", func(t *testing.T) {
		key := `key"\/`
		value := `value"\/`
		testEncoderEscapedStrings(t, key, value)
	})

	t.Run("control characters", func(t *testing.T) {
		key := "key\b\f\n\r\t"
		value := "value\b\f\n\r\t"
		testEncoderEscapedStrings(t, key, value)
	})

	t.Run("unicode low", func(t *testing.T) {
		key := "key\u0007\u001f"
		value := "value\u0007\u001f"
		testEncoderEscapedStrings(t, key, value)
	})

	t.Run("mixed all", func(t *testing.T) {
		key := `key"\\\/\b\f\n\r\t\u0007\u001f`
		value := `value"\\\/\b\f\n\r\t\u0007\u001f`
		testEncoderEscapedStrings(t, key, value)
	})
}

func testEncoderEscapedStrings(t *testing.T, key, value string) {
	buf := bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	enc.WriteToken(jsontext.BeginObject)
	enc.WriteToken(jsontext.String(key))
	enc.WriteToken(jsontext.String(value))
	enc.WriteToken(jsontext.EndObject)
	output := buf.String()

	var got map[string]string
	err := json.Unmarshal([]byte(output), &got)
	require.NoError(t, err)
	expected := map[string]string{key: value}
	require.Equal(t, expected, got)
}

func TestEncoderStringUTF8(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty"},
		{name: "ASCII", input: "closed", want: "closed"},
		{name: "Unicode", input: "\u00e9\u20ac\U0001f600\ufffd", want: "\u00e9\u20ac\U0001f600\ufffd"},
		{name: "continuation byte", input: "\x80", want: "\ufffd"},
		{name: "invalid bytes", input: "\xff\xfe", want: "\ufffd\ufffd"},
		{name: "truncated sequence", input: "\xe2\x82", want: "\ufffd\ufffd"},
		{name: "overlong sequence", input: "\xc0\xaf", want: "\ufffd\ufffd"},
		{name: "surrogate", input: "\xed\xa0\x80", want: "\ufffd\ufffd\ufffd"},
		{name: "out of range", input: "\xf4\x90\x80\x80", want: "\ufffd\ufffd\ufffd\ufffd"},
		{name: "mixed Unicode", input: "\u20ac\xff\U0001f600", want: "\u20ac\ufffd\U0001f600"},
		{name: "with escapes", input: "\"\\\n\x00\xff", want: "\"\\\n\x00\ufffd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			enc := jsontext.NewEncoder(&buf)
			for _, token := range []jsontext.Token{
				jsontext.BeginObject,
				jsontext.String(tt.input), jsontext.String(tt.input),
				jsontext.String("after"), jsontext.True,
				jsontext.EndObject,
			} {
				require.NoError(t, enc.WriteToken(token))
			}
			require.True(t, utf8.Valid(buf.Bytes()), "invalid UTF-8: %q", buf.Bytes())
			require.True(t, json.Valid(buf.Bytes()))
			var got map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
			require.Equal(t, map[string]any{tt.want: tt.want, "after": true}, got)
		})
	}
}

func FuzzEncoderString(f *testing.F) {
	for _, input := range []string{"", "closed", "\u00e9\u20ac\U0001f600\ufffd", "\xff\xfe", "\xe2\x82", "\"\\\n\x00\xff"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		var buf bytes.Buffer
		enc := jsontext.NewEncoder(&buf)
		require.NoError(t, enc.WriteToken(jsontext.BeginObject))
		require.NoError(t, enc.WriteToken(jsontext.String(input)))
		require.NoError(t, enc.WriteToken(jsontext.String(input)))
		require.NoError(t, enc.WriteToken(jsontext.EndObject))
		require.True(t, utf8.Valid(buf.Bytes()), "invalid UTF-8: %q", buf.Bytes())
		require.True(t, json.Valid(buf.Bytes()))

		// Compare decoded strings since equivalent JSON encoders can escape differently.
		expected, err := json.Marshal(map[string]string{input: input})
		require.NoError(t, err)
		var want, got map[string]string
		require.NoError(t, json.Unmarshal(expected, &want))
		require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
		require.Equal(t, want, got)
	})
}

func encodeValue(t testing.TB, enc *jsontext.Encoder, v any) (isSupported bool) {
	t.Helper()

	switch val := v.(type) {
	case map[string]any:
		require.NoError(t, enc.WriteToken(jsontext.BeginObject))
		for k, vv := range val {
			require.NoError(t, enc.WriteToken(jsontext.String(k)))
			if !encodeValue(t, enc, vv) {
				return false
			}
		}
		require.NoError(t, enc.WriteToken(jsontext.EndObject))
		return true
	case []any:
		require.NoError(t, enc.WriteToken(jsontext.BeginArray))
		for _, vv := range val {
			if !encodeValue(t, enc, vv) {
				return false // Propagate unsupported if any nested value fails
			}
		}
		require.NoError(t, enc.WriteToken(jsontext.EndArray))
		return true
	case string:
		require.NoError(t, enc.WriteToken(jsontext.String(val)))
		return true
	case int64:
		require.NoError(t, enc.WriteToken(jsontext.Int(val)))
		return true
	case uint64:
		require.NoError(t, enc.WriteToken(jsontext.Uint(val)))
		return true
	case float64:
		require.NoError(t, enc.WriteToken(jsontext.Float(val)))
		return true
	case bool:
		require.NoError(t, enc.WriteToken(jsontext.Bool(val)))
		return true
	case nil:
		require.NoError(t, enc.WriteToken(jsontext.Null))
		return true
	default:
		return false
	}
}

type errorWriter struct {
	N int
}

func (w *errorWriter) Write(p []byte) (int, error) {
	n := min(len(p), w.N)
	w.N -= n
	if w.N <= 0 {
		return n, assert.AnError
	}
	return n, nil
}

func TestEncoderComprehensive(t *testing.T) {
	// encodes an object with all token types and nested structures
	encode := func(enc *jsontext.Encoder) error {
		if err := enc.WriteToken(jsontext.BeginObject); err != nil {
			return err
		}

		if err := enc.WriteToken(jsontext.String("simple")); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.String("value")); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.String("escaped")); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.String(`"quoted\"string"`)); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.String("invalid UTF-8")); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.String("\xff\"\n\xe2\x82")); err != nil {
			return err
		}

		if err := enc.WriteToken(jsontext.String("int")); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.Int(-42)); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.String("uint")); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.Uint(100)); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.String("float")); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.Float(3.14)); err != nil {
			return err
		}

		if err := enc.WriteToken(jsontext.String("true")); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.True); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.String("false")); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.False); err != nil {
			return err
		}

		if err := enc.WriteToken(jsontext.String("array")); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.BeginArray); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.String("item1")); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.Int(1)); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.EndArray); err != nil {
			return err
		}

		if err := enc.WriteToken(jsontext.String("nested")); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.BeginObject); err != nil {
			return err
		}
		if err := enc.WriteToken(jsontext.EndObject); err != nil {
			return err
		}

		if err := enc.WriteToken(jsontext.EndObject); err != nil {
			return err
		}
		return nil
	}

	buf := bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	require.NoError(t, encode(enc))

	for i := range buf.Len() {
		enc := jsontext.NewEncoder(&errorWriter{N: i})
		require.ErrorIs(t, encode(enc), assert.AnError)
	}
}

func FuzzEncoder(f *testing.F) {
	examples := []string{
		`{"hello": "world"}`,
		`{"foo": 123, "bar": [1, 2, 3]}`,
		`{"nested": {"a": 1, "b": [true, false, "foobar", null]}}`,
		`[{"x": 1}, {"y": "foo"}]`,
		`["foo", "bar"]`,
		`["a", {"b": [1, 2, {"c": "d"}]}, 3]`,
		`{"emptyObj": {}, "emptyArr": []}`,
		`{"mixed": [1, "two", {"three": 3}]}`,
		`[null]`,
	}
	for _, tc := range examples {
		// first test that
		// 1. it's valid JSON
		d := json.NewDecoder(bytes.NewReader([]byte(tc)))
		var expected any
		require.NoError(f, d.Decode(&expected), "corpus entry `%s` is not valid JSON", tc)
		// 2. the jsontext encoder can handle
		enc := jsontext.NewEncoder(&bytes.Buffer{})
		require.True(f, encodeValue(f, enc, expected), "expected `%s` to be supported", tc)

		f.Add([]byte(tc))
	}

	var stdlibBuf, ourBuf bytes.Buffer

	f.Fuzz(func(t *testing.T, b []byte) {
		stdlibBuf.Truncate(0)
		ourBuf.Truncate(0)
		stdlibBuf.Grow(len(b))
		ourBuf.Grow(len(b))

		d := json.NewDecoder(bytes.NewReader(b))
		var expected any
		if err := d.Decode(&expected); err != nil {
			return // invalid JSON
		}

		// only attempt to handle inputs that the standard library can handle
		stdlibEnc := json.NewEncoder(&stdlibBuf)
		require.NoError(t, stdlibEnc.Encode(expected))
		if !json.Valid(stdlibBuf.Bytes()) {
			return
		}

		// then encode using the jsontext encoder
		enc := jsontext.NewEncoder(&ourBuf)
		if isSupported := encodeValue(t, enc, expected); !isSupported {
			return
		}

		output := ourBuf.Bytes()
		require.Truef(t, json.Valid(output), "produced invalid JSON: %s", output)

		var got any
		require.NoError(t, json.Unmarshal(output, &got))
		require.JSONEq(t, ourBuf.String(), stdlibBuf.String())
	})
}
