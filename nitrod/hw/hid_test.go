package hw

import "testing"

func TestHIDIOCSFeatureNumber(t *testing.T) {
	if got, want := ioctlHIDIOCSFeature(11), uintptr(0xC00B4806); got != want {
		t.Fatalf("ioctl = %#x, want %#x", got, want)
	}
}

func TestBuildHIDReport(t *testing.T) {
	got := buildHIDReport(0x07, 73, 4, 2, 0x12, 0x34, 0x56, 0x0F)
	want := [11]byte{0xA4, 0x21, 0x07, 73, 4, 2, 0x12, 0x34, 0x56, 0x0F, 0x00}
	if got != want {
		t.Fatalf("report = % X, want % X", got, want)
	}
}

func TestEffectAndSpeedMapping(t *testing.T) {
	tests := []struct {
		mode, speed      int
		effect, hidSpeed byte
	}{
		{0, 9, 0x02, 0}, {1, 7, 0x04, 7}, {2, 3, 0x05, 3},
		{3, 0, 0x07, 1}, {4, 9, 0x07, 10}, {5, 4, 0x09, 5},
		{6, 9, 0x0A, 10}, {7, 2, 0x0B, 3},
	}
	for _, test := range tests {
		effect, speed, ok := effectAndSpeed(test.mode, test.speed)
		if !ok || effect != test.effect || speed != test.hidSpeed {
			t.Errorf("mode=%d speed=%d got (%#x,%d,%t), want (%#x,%d,true)", test.mode, test.speed, effect, speed, ok, test.effect, test.hidSpeed)
		}
	}
	if _, _, ok := effectAndSpeed(8, 0); ok {
		t.Fatal("unsupported mode accepted")
	}
}

func TestPerZoneReportsUseOneMaskPerZone(t *testing.T) {
	fake, err := NewFake(t.TempDir(), []string{"per_zone_mode"})
	if err != nil {
		t.Fatal(err)
	}
	fake.HIDPresent = true
	value := PerZone{Colors: [4]string{"010203", "040506", "070809", "0a0b0c"}, Brightness: 60}
	if err := fake.Write("per_zone_mode", value); err != nil {
		t.Fatal(err)
	}
	if len(fake.HIDReports) != 4 {
		t.Fatalf("got %d reports, want 4", len(fake.HIDReports))
	}
	for index, report := range fake.HIDReports {
		if report[2] != 0x02 || report[4] != 0 || report[5] != 0 || report[9] != byte(1<<index) {
			t.Errorf("zone %d report = % X", index+1, report)
		}
	}
}

func TestEffectPacketsForEveryMode(t *testing.T) {
	wantEffects := []byte{0x02, 0x04, 0x05, 0x07, 0x07, 0x09, 0x0A, 0x0B}
	for mode, wantEffect := range wantEffects {
		packet, ok := effectReport(FourZone{Mode: mode, Speed: 4, Brightness: 50, Direction: 2, Red: 1, Green: 2, Blue: 3})
		if !ok {
			t.Fatalf("mode %d was rejected", mode)
		}
		if packet[2] != wantEffect || packet[3] != 50 || packet[8] != 3 || packet[9] != 0x0F {
			t.Errorf("mode %d packet = % X", mode, packet)
		}
		if mode == 0 && (packet[4] != 0 || packet[5] != 0) {
			t.Errorf("static mode packet must use speed and direction zero: % X", packet)
		}
	}
}

func TestHIDFailureFallsBackToFakeSysfs(t *testing.T) {
	fake, err := NewFake(t.TempDir(), []string{"per_zone_mode"})
	if err != nil {
		t.Fatal(err)
	}
	fake.HIDPresent = true
	fake.HIDErrorOnReport = 2
	value := PerZone{Colors: [4]string{"112233", "445566", "778899", "aabbcc"}, Brightness: 75}
	if err := fake.Write("per_zone_mode", value); err != nil {
		t.Fatal(err)
	}
	if len(fake.HIDReports) != 1 {
		t.Fatalf("failed zone should stop HID reports; got %d", len(fake.HIDReports))
	}
	if len(fake.SysfsWrites) != 1 || fake.SysfsWrites[0] != "per_zone_mode" {
		t.Fatalf("sysfs fallback writes = %#v", fake.SysfsWrites)
	}
}
