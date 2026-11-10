package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
)

const maxLineBytes = 64 * 1024

type Request struct {
	ID    json.RawMessage `json:"id,omitempty"`
	Cmd   string          `json:"cmd"`
	Key   string          `json:"key,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

type Event struct {
	Event string          `json:"event"`
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type Controller interface {
	Do(context.Context, Request) (any, error)
	Subscribe(context.Context) (<-chan Event, func(), error)
}

type Config struct {
	Path string
}

type Server struct {
	config     Config
	controller Controller
	listener   *net.UnixListener
	clients    sync.WaitGroup
}

func New(config Config, controller Controller) *Server {
	if config.Path == "" {
		config.Path = "/run/nitrod.sock"
	}
	return &Server{config: config, controller: controller}
}

func (s *Server) Listen() error {
	if err := os.MkdirAll(filepath.Dir(s.config.Path), 0755); err != nil {
		return err
	}
	if info, err := os.Lstat(s.config.Path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing to replace non-socket path %s", s.config.Path)
		}
		if err := os.Remove(s.config.Path); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	address, err := net.ResolveUnixAddr("unix", s.config.Path)
	if err != nil {
		return err
	}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		return err
	}
	s.listener = listener
	if err := os.Chmod(s.config.Path, 0600); err != nil {
		_ = listener.Close()
		_ = os.Remove(s.config.Path)
		return err
	}
	return nil
}

func (s *Server) Serve(ctx context.Context) error {
	if s.listener == nil {
		return errors.New("server is not listening")
	}
	go func() {
		<-ctx.Done()
		s.listener.Close()
	}()
	for {
		connection, err := s.listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				break
			}
			return err
		}
		s.clients.Add(1)
		go s.serveClient(ctx, connection)
	}
	s.clients.Wait()
	_ = os.Remove(s.config.Path)
	return nil
}

func (s *Server) Close() error {
	if s.listener == nil {
		return nil
	}
	return s.listener.Close()
}

func (s *Server) serveClient(parent context.Context, connection *net.UnixConn) {
	defer s.clients.Done()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer connection.Close()
	go func() {
		<-parent.Done()
		_ = connection.Close()
	}()
	outbound := make(chan []byte, 32)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-ctx.Done():
				for {
					select {
					case message := <-outbound:
						if err := writeLine(connection, message); err != nil {
							return
						}
					default:
						return
					}
				}
			case message := <-outbound:
				if err := writeLine(connection, message); err != nil {
					cancel()
					_ = connection.Close()
					return
				}
			}
		}
	}()

	var unsubscribe func()
	defer func() {
		if unsubscribe != nil {
			unsubscribe()
		}
	}()
	err := ReadLines(connection, func(line []byte) error {
		request, err := DecodeRequest(line)
		if err != nil {
			return queueResponse(outbound, response{OK: false, Error: "invalid JSON: " + err.Error()})
		}
		if request.Cmd == "subscribe" {
			if unsubscribe == nil {
				events, stop, err := s.controller.Subscribe(ctx)
				if err != nil {
					return queueResponse(outbound, response{ID: request.ID, OK: false, Error: err.Error()})
				}
				unsubscribe = stop
				go relayEvents(ctx, cancel, events, outbound)
			}
			return queueResponse(outbound, response{ID: request.ID, OK: true})
		}
		data, err := s.controller.Do(ctx, request)
		if err != nil {
			return queueResponse(outbound, response{ID: request.ID, OK: false, Error: err.Error()})
		}
		encoded, err := json.Marshal(data)
		if err != nil {
			return queueResponse(outbound, response{ID: request.ID, OK: false, Error: "could not encode response"})
		}
		return queueResponse(outbound, response{ID: request.ID, OK: true, Data: encoded})
	})
	if err != nil && !errors.Is(err, net.ErrClosed) && ctx.Err() == nil {
		_ = queueResponse(outbound, response{OK: false, Error: err.Error()})
	}
	cancel()
	<-writerDone
	_ = connection.Close()
}

type response struct {
	ID    json.RawMessage `json:"id,omitempty"`
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

func queueResponse(outbound chan<- []byte, value response) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	select {
	case outbound <- encoded:
		return nil
	default:
		return errors.New("client output queue is full")
	}
}

func relayEvents(ctx context.Context, cancel context.CancelFunc, events <-chan Event, outbound chan<- []byte) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				continue
			}
			select {
			case outbound <- encoded:
			default:
				cancel()
				return
			}
		}
	}
}

func ReadLines(reader io.Reader, handle func([]byte) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxLineBytes)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if err := handle(line); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func writeLine(writer io.Writer, message []byte) error {
	data := append(append([]byte(nil), message...), '\n')
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func DecodeRequest(line []byte) (Request, error) {
	var request Request
	err := json.Unmarshal(line, &request)
	return request, err
}
