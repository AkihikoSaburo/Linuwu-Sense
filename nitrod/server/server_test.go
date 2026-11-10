package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type chunkReader struct {
	remaining string
}

func (r *chunkReader) Read(buffer []byte) (int, error) {
	if len(r.remaining) == 0 {
		return 0, io.EOF
	}
	count := min(len(buffer), 3, len(r.remaining))
	copy(buffer, r.remaining[:count])
	r.remaining = r.remaining[count:]
	return count, nil
}

func TestReadLinesHandlesFragmentedAndMultipleMessages(t *testing.T) {
	reader := &chunkReader{remaining: "{\"id\":1}\n{\"id\":2}\n"}
	var got []string
	err := ReadLines(reader, func(line []byte) error {
		got = append(got, string(line))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != `{"id":1}` || got[1] != `{"id":2}` {
		t.Fatalf("lines = %#v", got)
	}
}

func TestReadLinesAcceptsFinalLineWithoutTerminator(t *testing.T) {
	var got string
	err := ReadLines(strings.NewReader("{\"cmd\":\"get_state\"}"), func(line []byte) error {
		got = string(line)
		return nil
	})
	if err != nil || got != `{"cmd":"get_state"}` {
		t.Fatalf("line = %q, err=%v", got, err)
	}
}

func TestReadLinesRejectsOversizedLine(t *testing.T) {
	err := ReadLines(strings.NewReader(strings.Repeat("x", maxLineBytes+1)+"\n"), func([]byte) error { return nil })
	if err == nil {
		t.Fatal("oversized line unexpectedly accepted")
	}
}

func TestDecodeRequestRejectsMalformedJSON(t *testing.T) {
	if _, err := DecodeRequest([]byte(`{"cmd":`)); err == nil {
		t.Fatal("malformed JSON unexpectedly accepted")
	}
}

type testController struct{}

func (testController) Do(context.Context, Request) (any, error) {
	return map[string]bool{"ready": true}, nil
}

func (testController) Subscribe(context.Context) (<-chan Event, func(), error) {
	return make(chan Event), func() {}, nil
}

func TestServerFlushesResponseAfterClientHalfClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nitrod.sock")
	socketServer := New(Config{Path: path}, testController{})
	if err := socketServer.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- socketServer.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = socketServer.Close()
		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Error("server did not stop")
		}
	})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("socket mode = %04o, want 0600", got)
	}
	connection, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte("{\"id\":7,\"cmd\":\"get_state\"}\n")); err != nil {
		t.Fatal(err)
	}
	if err := connection.(*net.UnixConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(connection).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		ID   int  `json:"id"`
		OK   bool `json:"ok"`
		Data struct {
			Ready bool `json:"ready"`
		} `json:"data"`
	}
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != 7 || !response.OK || !response.Data.Ready {
		t.Fatalf("response = %#v", response)
	}
	_ = connection.Close()
}
