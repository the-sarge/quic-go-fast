package qlogwriter

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Build actual consumers so module selection, init and linker overrides are all
// exercised through the code_version emitted by the public trace constructor.
func TestTraceCodeVersion(t *testing.T) {
	const upstream = "github.com/quic-go/quic-go"
	const fork = "github.com/the-sarge/quic-go-fast"
	const moduleFile = "module " + upstream + "\n\ngo 1.26.0\n"
	localModule := t.TempDir()
	files := map[string][]byte{"go.mod": []byte(moduleFile)}
	// Only the packages needed by the trace writer are included. This keeps the
	// fixture offline and uses the current source for both synthetic versions.
	for _, dir := range []string{"qlogwriter", "qlogwriter/jsontext", "internal/protocol", "quicvarint"} {
		entries, err := os.ReadDir(filepath.Join("..", dir))
		require.NoError(t, err)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			name := dir + "/" + entry.Name()
			files[name], err = os.ReadFile(filepath.Join("..", name))
			require.NoError(t, err)
		}
	}
	for name, data := range files {
		path := filepath.Join(localModule, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, data, 0o600))
	}

	proxyFiles := make(map[string][]byte)
	for modulePath, version := range map[string]string{upstream: "v0.0.1", fork: "v0.0.2"} {
		var buf bytes.Buffer
		archive := zip.NewWriter(&buf)
		for name, data := range files {
			w, err := archive.Create(modulePath + "@" + version + "/" + name)
			require.NoError(t, err)
			_, err = w.Write(data)
			require.NoError(t, err)
		}
		require.NoError(t, archive.Close())
		prefix := "/" + modulePath + "/@v/" + version
		proxyFiles[prefix+".zip"] = buf.Bytes()
		proxyFiles[prefix+".mod"] = []byte(moduleFile)
		proxyFiles[prefix+".info"] = []byte(fmt.Sprintf(`{"Version":%q,"Time":"2026-01-01T00:00:00Z"}`, version))
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if data, ok := proxyFiles[r.URL.Path]; ok {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	defer proxy.Close()
	t.Setenv("GOPROXY", proxy.URL)
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GONOPROXY", "none")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOMODCACHE", t.TempDir())

	for _, tc := range []struct {
		name        string
		replacement string
		ldflags     string
		want        string
	}{
		{name: "ordinary module", want: "v0.0.1"},
		{name: "versioned replacement", replacement: fork + " v0.0.2", want: "v0.0.2"},
		{name: "local replacement", replacement: fmt.Sprintf("%q", filepath.ToSlash(localModule)), want: "v0.0.1 (replaced)"},
		{name: "linker revision", ldflags: "-X " + upstream + "/qlogwriter.quicGoVersion=fork-revision", want: "fork-revision"},
		{name: "linker overrides replacement", replacement: fork + " v0.0.2", ldflags: "-X " + upstream + "/qlogwriter.quicGoVersion=fork-revision", want: "fork-revision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			mod := "module example.com/qlog-consumer\n\ngo 1.26.0\n\nrequire " + upstream + " v0.0.1\n"
			if tc.replacement != "" {
				mod += "replace " + upstream + " => " + tc.replacement + "\n"
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main
import (
    "os"
    "github.com/quic-go/quic-go/qlogwriter"
)
func main() {
    trace := qlogwriter.NewFileSeq(os.Stdout)
    go trace.Run()
    _ = trace.AddProducer().Close()
}
`), 0o600))
			cmd := exec.CommandContext(t.Context(), "go", "run", "-mod=mod", "-modcacherw", "-ldflags="+tc.ldflags, ".")
			cmd.Dir = dir
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			output, err := cmd.Output()
			require.NoError(t, err, stderr.String())
			require.NotEmpty(t, output)
			require.Equal(t, RecordSeparator, output[0])
			var header struct {
				CodeVersion string `json:"code_version"`
			}
			require.NoError(t, json.Unmarshal(output[1:], &header))
			require.Equal(t, tc.want, header.CodeVersion, stderr.String())
		})
	}
}
