package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nitrod/hw"
	"nitrod/server"
)

func startTestOwner(t *testing.T, backend *hw.Fake, limit float64, interval time.Duration) *owner {
	t.Helper()
	instance := newOwner(backend, limit, interval)
	go instance.Run(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := instance.Shutdown(ctx); err != nil {
			t.Errorf("owner shutdown: %v", err)
		}
	})
	return instance
}

func TestOwnerRejectsInvalidValueBeforeHardwareWrite(t *testing.T) {
	backend, err := hw.NewFake(t.TempDir(), []string{"fan_speed"})
	if err != nil {
		t.Fatal(err)
	}
	instance := startTestOwner(t, backend, 85, time.Hour)
	_, err = instance.Do(context.Background(), server.Request{Cmd: "set", Key: "fan_speed", Value: json.RawMessage(`{"cpu":101,"gpu":30}`)})
	if err == nil {
		t.Fatal("out-of-range fan value unexpectedly succeeded")
	}
	if len(backend.SysfsWrites) != 0 {
		t.Fatalf("invalid request wrote hardware: %#v", backend.SysfsWrites)
	}
}

func TestOwnerHIDFailureFallsBackAndBroadcasts(t *testing.T) {
	backend, err := hw.NewFake(t.TempDir(), []string{"per_zone_mode"})
	if err != nil {
		t.Fatal(err)
	}
	backend.HIDPresent = true
	backend.HIDErrorOnReport = 2
	instance := startTestOwner(t, backend, 85, time.Hour)
	events, unsubscribe, err := instance.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	request := server.Request{Cmd: "set", Key: "per_zone_mode", Value: json.RawMessage(`{"zone1":"112233","zone2":"445566","zone3":"778899","zone4":"aabbcc","brightness":75}`)}
	if _, err := instance.Do(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Key != "per_zone_mode" || event.Event != "state" {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for state event")
	}
	if len(backend.HIDReports) != 1 || len(backend.SysfsWrites) != 1 {
		t.Fatalf("HID reports=%d sysfs writes=%#v", len(backend.HIDReports), backend.SysfsWrites)
	}
}

func TestOwnerTemperatureTripAndShutdownRestoreAuto(t *testing.T) {
	backend, err := hw.NewFake(t.TempDir(), []string{"fan_speed"})
	if err != nil {
		t.Fatal(err)
	}
	backend.Temperature = 92
	instance := startTestOwner(t, backend, 90, 10*time.Millisecond)
	events, unsubscribe, err := instance.Subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	request := server.Request{Cmd: "set", Key: "fan_speed", Value: json.RawMessage(`{"cpu":60,"gpu":55}`)}
	if _, err := instance.Do(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for manual fan event")
	}
	select {
	case event := <-events:
		if event.Key != "fan_speed" {
			t.Fatalf("unexpected safety event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for temperature safety event")
	}
	data, err := os.ReadFile(filepath.Join(backend.Root, "fan_speed"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "0,0\n" {
		t.Fatalf("fan state after thermal trip = %q", data)
	}
	if err := instance.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
