package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"nitrod/hw"
	"nitrod/safety"
	"nitrod/server"
	"nitrod/state"
)

type ownerCommand struct {
	kind    string
	request server.Request
	result  chan ownerResult
	events  chan server.Event
	subID   uint64
}

type ownerResult struct {
	data   any
	err    error
	events chan server.Event
	subID  uint64
}

type owner struct {
	backend           hw.Backend
	commands          chan ownerCommand
	done              chan struct{}
	snapshot          state.Snapshot
	subscribers       map[uint64]chan server.Event
	nextSubscriberID  uint64
	fanManual         bool
	temperatureLimit  float64
	temperaturePeriod time.Duration
	temperatureOff    bool
}

func newOwner(backend hw.Backend, temperatureLimit float64, temperaturePeriod time.Duration) *owner {
	if temperatureLimit <= 0 {
		temperatureLimit = 85
	}
	if temperaturePeriod <= 0 {
		temperaturePeriod = 5 * time.Second
	}
	return &owner{
		backend:           backend,
		commands:          make(chan ownerCommand, 64),
		done:              make(chan struct{}),
		snapshot:          state.NewSnapshot(nil, nil),
		subscribers:       make(map[uint64]chan server.Event),
		temperatureLimit:  temperatureLimit,
		temperaturePeriod: temperaturePeriod,
	}
}

func (o *owner) Run(ctx context.Context) {
	defer close(o.done)
	defer o.closeSubscribers()
	if err := o.backend.Initialize(); err != nil {
		slog.Error("hardware backend initialization failed; continuing without features", "error", err)
	}
	o.snapshot = state.NewSnapshot(o.backend.Features(), o.backend.ProfileChoices())
	for _, key := range o.snapshot.Features {
		value, err := o.backend.Read(key)
		if err != nil {
			slog.Warn("could not read initial sysfs state", "feature", key, "error", err)
			continue
		}
		o.snapshot.Set(key, value)
	}
	o.refreshFanMode()
	ticker := time.NewTicker(o.temperaturePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := o.restoreFanAuto(); err != nil {
				slog.Error("failed to restore fan auto during context shutdown", "error", err)
			}
			return
		case command := <-o.commands:
			if command.kind == "shutdown" {
				err := o.restoreFanAuto()
				command.result <- ownerResult{err: err}
				return
			}
			o.handle(command)
		case <-ticker.C:
			o.checkTemperature()
		}
	}
}

func (o *owner) Do(ctx context.Context, request server.Request) (any, error) {
	result := make(chan ownerResult, 1)
	command := ownerCommand{kind: "request", request: request, result: result}
	select {
	case o.commands <- command:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-o.done:
		return nil, errors.New("hardware owner is stopped")
	}
	select {
	case completed := <-result:
		return completed.data, completed.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-o.done:
		return nil, errors.New("hardware owner is stopped")
	}
}

func (o *owner) Subscribe(ctx context.Context) (<-chan server.Event, func(), error) {
	result := make(chan ownerResult, 1)
	select {
	case o.commands <- ownerCommand{kind: "subscribe", result: result}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-o.done:
		return nil, nil, errors.New("hardware owner is stopped")
	}
	var subscription ownerResult
	select {
	case subscription = <-result:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-o.done:
		return nil, nil, errors.New("hardware owner is stopped")
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			select {
			case o.commands <- ownerCommand{kind: "unsubscribe", subID: subscription.subID}:
			case <-o.done:
			}
		})
	}
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-o.done:
		}
	}()
	return subscription.events, stop, nil
}

func (o *owner) Shutdown(ctx context.Context) error {
	result := make(chan ownerResult, 1)
	select {
	case o.commands <- ownerCommand{kind: "shutdown", result: result}:
	case <-o.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case completed := <-result:
		<-o.done
		return completed.err
	case <-o.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (o *owner) handle(command ownerCommand) {
	switch command.kind {
	case "subscribe":
		o.nextSubscriberID++
		events := make(chan server.Event, 16)
		o.subscribers[o.nextSubscriberID] = events
		command.result <- ownerResult{events: events, subID: o.nextSubscriberID}
	case "unsubscribe":
		if events, ok := o.subscribers[command.subID]; ok {
			delete(o.subscribers, command.subID)
			close(events)
		}
	case "request":
		data, err := o.execute(command.request)
		command.result <- ownerResult{data: data, err: err}
	}
}

func (o *owner) execute(request server.Request) (any, error) {
	switch request.Cmd {
	case "get_state":
		return o.snapshot.Clone(), nil
	case "set":
		if !contains(o.snapshot.Features, request.Key) {
			return nil, fmt.Errorf("feature %q is unavailable", request.Key)
		}
		value, err := safety.Decode(request.Key, request.Value, o.snapshot.ProfileChoices)
		if err != nil {
			return nil, err
		}
		if err := o.backend.Write(request.Key, value); err != nil {
			return nil, fmt.Errorf("write %s: %w", request.Key, err)
		}
		stateValue, readErr := o.backend.Read(request.Key)
		if readErr != nil {
			stateValue = append(json.RawMessage(nil), request.Value...)
		}
		o.snapshot.Set(request.Key, stateValue)
		if request.Key == "fan_speed" {
			fan := value.(hw.FanSpeed)
			o.fanManual = fan.CPU != 0 || fan.GPU != 0
		}
		o.broadcast(server.Event{Event: "state", Key: request.Key, Value: stateValue})
		return map[string]bool{"changed": true}, nil
	default:
		return nil, fmt.Errorf("unsupported command %q", request.Cmd)
	}
}

func (o *owner) checkTemperature() {
	if !o.fanManual || o.temperatureOff || !contains(o.snapshot.Features, "fan_speed") {
		return
	}
	temperature, err := o.backend.TemperatureC()
	if err != nil {
		slog.Warn("no readable hwmon temperature sensor; manual-fan temperature safety is disabled", "error", err)
		o.temperatureOff = true
		return
	}
	if temperature < o.temperatureLimit {
		return
	}
	slog.Warn("temperature threshold exceeded; forcing fan auto", "temperature_c", temperature, "limit_c", o.temperatureLimit)
	if err := o.restoreFanAuto(); err != nil {
		slog.Error("failed to force fan auto after temperature threshold", "error", err)
	}
}

func (o *owner) restoreFanAuto() error {
	if !contains(o.snapshot.Features, "fan_speed") {
		return nil
	}
	if err := o.backend.Write("fan_speed", hw.FanSpeed{CPU: 0, GPU: 0}); err != nil {
		return err
	}
	value, err := o.backend.Read("fan_speed")
	if err != nil {
		value = json.RawMessage(`"0,0"`)
	}
	o.snapshot.Set("fan_speed", value)
	o.fanManual = false
	o.broadcast(server.Event{Event: "state", Key: "fan_speed", Value: value})
	return nil
}

func (o *owner) refreshFanMode() {
	if !contains(o.snapshot.Features, "fan_speed") {
		return
	}
	var value string
	if err := json.Unmarshal(o.snapshot.Values["fan_speed"], &value); err != nil {
		return
	}
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return
	}
	cpu, cpuErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	gpu, gpuErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	o.fanManual = cpuErr == nil && gpuErr == nil && (cpu != 0 || gpu != 0)
}

func (o *owner) broadcast(event server.Event) {
	for id, events := range o.subscribers {
		select {
		case events <- event:
		default:
			close(events)
			delete(o.subscribers, id)
			slog.Warn("removed slow socket subscriber", "subscriber", id)
		}
	}
}

func (o *owner) closeSubscribers() {
	for id, events := range o.subscribers {
		close(events)
		delete(o.subscribers, id)
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
