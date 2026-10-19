package qlog

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"

	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/qlogwriter"
)

// EventSchema is the qlog event schema for QUIC
const EventSchema = "urn:ietf:params:qlog:events:quic-12"

// DefaultConnectionTracer creates a qlog file in the qlog directory specified by the QLOGDIR environment variable.
// File names are <odcid>_<perspective>.sqlog.
// New directories and files are created with owner-only permissions (0700 and 0600, respectively, before umask).
// Existing directory permissions are left unchanged; QLOGDIR and its parents must be trusted.
// Qlogs can contain sensitive data, including HTTP headers and QUIC tokens.
// Returns nil if QLOGDIR is not set, the file already exists, or the log directory or file cannot be created.
func DefaultConnectionTracer(_ context.Context, isClient bool, connID ConnectionID) qlogwriter.Trace {
	return defaultConnectionTracerWithSchemas(isClient, connID, []string{EventSchema})
}

// DefaultConnectionTracerWithSchemas creates a trace like [DefaultConnectionTracer] with additional event schemas.
func DefaultConnectionTracerWithSchemas(_ context.Context, isClient bool, connID ConnectionID, eventSchemas []string) qlogwriter.Trace {
	if !slices.Contains(eventSchemas, EventSchema) {
		eventSchemas = append([]string{EventSchema}, eventSchemas...)
	}
	return defaultConnectionTracerWithSchemas(isClient, connID, eventSchemas)
}

func defaultConnectionTracerWithSchemas(isClient bool, connID ConnectionID, eventSchemas []string) qlogwriter.Trace {
	qlogDir := os.Getenv("QLOGDIR")
	if qlogDir == "" {
		return nil
	}
	if _, err := os.Stat(qlogDir); os.IsNotExist(err) {
		if err := os.MkdirAll(qlogDir, 0o700); err != nil {
			log.Printf("failed to create qlog dir %s: %v", qlogDir, err)
			return nil
		}
	}
	label := "server"
	if isClient {
		label = "client"
	}
	path := fmt.Sprintf("%s/%s_%s.sqlog", strings.TrimRight(qlogDir, "/"), connID, label)
	// Exclusive creation avoids reusing a readable file or following a symlink.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		log.Printf("Failed to create qlog file %s: %s", path, err.Error())
		return nil
	}
	fileSeq := qlogwriter.NewConnectionFileSeq(
		utils.NewBufferedWriteCloser(bufio.NewWriter(f), f),
		isClient,
		connID,
		eventSchemas,
	)
	go fileSeq.Run()
	return fileSeq
}
