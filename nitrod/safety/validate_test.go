package safety

import (
	"encoding/json"
	"strings"
	"testing"

	"nitrod/hw"
)

func TestDecodeFanSpeed(t *testing.T) {
	value, err := Decode("fan_speed", json.RawMessage(`{"cpu":50,"gpu":0}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := value.(hw.FanSpeed); !ok || got.CPU != 50 || got.GPU != 0 {
		t.Fatalf("decoded value = %#v", value)
	}
}

func TestDecodeRejectsInvalidRangesAndTypes(t *testing.T) {
	tests := []struct{ key, value string }{
		{"fan_speed", `{"cpu":101,"gpu":20}`},
		{"fan_speed", `{"cpu":"50","gpu":20}`},
		{"fan_speed", `{"cpu":null,"gpu":20}`},
		{"battery_limiter", `2`},
		{"battery_limiter", `null`},
		{"usb_charging", `15`},
		{"platform_profile", `"turbo"`},
		{"per_zone_mode", `{"zone1":"12zz00","zone2":"000000","zone3":"000000","zone4":"000000","brightness":50}`},
		{"four_zone_mode", `{"mode":1,"speed":10,"brightness":50,"direction":1,"red":0,"green":0,"blue":0}`},
		{"four_zone_mode", `{"mode":1,"speed":1,"brightness":50,"direction":1,"red":null,"green":0,"blue":0}`},
	}
	for _, test := range tests {
		_, err := Decode(test.key, json.RawMessage(test.value), []string{"balanced"})
		if err == nil {
			t.Errorf("Decode(%s, %s) unexpectedly succeeded", test.key, test.value)
		}
	}
}

func TestDecodeRGBFields(t *testing.T) {
	value, err := Decode("per_zone_mode", json.RawMessage(`{"zone1":"aabbcc","zone2":"000000","zone3":"ffffff","zone4":"123456","brightness":100}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.(hw.PerZone); got.Colors[0] != "aabbcc" || got.Brightness != 100 {
		t.Fatalf("decoded value = %#v", got)
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	_, err := Decode("fan_speed", json.RawMessage(`{"cpu":1,"gpu":1,"other":true}`), nil)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}
