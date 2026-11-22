package main

import (
	"strings"
	"testing"
	"time"

	"github.com/portmole/portmole/proto"
)

func TestRequestLineCannotDriveTheTerminal(t *testing.T) {
	line := requestLine(proto.RequestLog{
		Time: time.Now(), Method: "GET", Status: 200, Millis: 12,
		Path: "/a\x1b[2J\x1b]0;owned\x07b\r\nGET /fake 200\u009b31m",
	})
	if strings.ContainsAny(line, "\x1b\x07\r\n\u009b") {
		t.Fatalf("control characters reached the terminal: %q", line)
	}
	if !strings.Contains(line, "GET") || !strings.Contains(line, "200") || !strings.Contains(line, "12 ms") || !strings.HasSuffix(line, "31m") {
		t.Fatalf("line = %q", line)
	}
}
