package safety

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"nitrod/hw"
)

type fanRequest struct {
	CPU int `json:"cpu"`
	GPU int `json:"gpu"`
}

type zoneRequest struct {
	Zone1      string `json:"zone1"`
	Zone2      string `json:"zone2"`
	Zone3      string `json:"zone3"`
	Zone4      string `json:"zone4"`
	Brightness int    `json:"brightness"`
}

type effectRequest struct {
	Mode       int `json:"mode"`
	Speed      int `json:"speed"`
	Brightness int `json:"brightness"`
	Direction  int `json:"direction"`
	Red        int `json:"red"`
	Green      int `json:"green"`
	Blue       int `json:"blue"`
}

func Decode(key string, raw json.RawMessage, profileChoices []string) (hw.Value, error) {
	switch key {
	case "platform_profile":
		var profile string
		if err := decodeStrict(raw, &profile); err != nil {
			return nil, typeError(key, "string", err)
		}
		for _, choice := range profileChoices {
			if profile == choice {
				return hw.Profile{Name: profile}, nil
			}
		}
		return nil, fmt.Errorf("platform_profile %q is invalid; choices: %s", profile, strings.Join(profileChoices, ", "))
	case "fan_speed":
		var value fanRequest
		if err := decodeStrict(raw, &value); err != nil {
			return nil, typeError(key, `{"cpu":int,"gpu":int}`, err)
		}
		if err := rangeError("cpu fan speed", value.CPU, 0, 100); err != nil {
			return nil, err
		}
		if err := rangeError("gpu fan speed", value.GPU, 0, 100); err != nil {
			return nil, err
		}
		return hw.FanSpeed{CPU: value.CPU, GPU: value.GPU}, nil
	case "battery_limiter", "battery_calibration", "lcd_override", "backlight_timeout", "boot_animation_sound":
		var value int
		if err := decodeStrict(raw, &value); err != nil {
			return nil, typeError(key, "integer 0 or 1", err)
		}
		if value != 0 && value != 1 {
			return nil, fmt.Errorf("%s must be 0 or 1", key)
		}
		return hw.Boolean{Enabled: value == 1}, nil
	case "usb_charging":
		var value int
		if err := decodeStrict(raw, &value); err != nil {
			return nil, typeError(key, "integer 0, 10, 20, or 30", err)
		}
		if value != 0 && value != 10 && value != 20 && value != 30 {
			return nil, fmt.Errorf("usb_charging must be 0, 10, 20, or 30")
		}
		return hw.USBCharging{Level: value}, nil
	case "per_zone_mode":
		var value zoneRequest
		if err := decodeStrict(raw, &value); err != nil {
			return nil, typeError(key, `{"zone1":"rrggbb","zone2":"rrggbb","zone3":"rrggbb","zone4":"rrggbb","brightness":int}`, err)
		}
		colors := [4]string{value.Zone1, value.Zone2, value.Zone3, value.Zone4}
		for index, color := range colors {
			if len(color) != 6 || !isHex(color) {
				return nil, fmt.Errorf("zone%d must be exactly six hexadecimal characters", index+1)
			}
		}
		if err := rangeError("brightness", value.Brightness, 0, 100); err != nil {
			return nil, err
		}
		return hw.PerZone{Colors: colors, Brightness: value.Brightness}, nil
	case "four_zone_mode":
		var value effectRequest
		if err := decodeStrict(raw, &value); err != nil {
			return nil, typeError(key, `{"mode":int,"speed":int,"brightness":int,"direction":int,"red":int,"green":int,"blue":int}`, err)
		}
		if err := rangeError("mode", value.Mode, 0, 7); err != nil {
			return nil, err
		}
		if err := rangeError("speed", value.Speed, 0, 9); err != nil {
			return nil, err
		}
		if err := rangeError("brightness", value.Brightness, 0, 100); err != nil {
			return nil, err
		}
		if value.Direction != 1 && value.Direction != 2 {
			return nil, fmt.Errorf("direction must be 1 or 2")
		}
		for name, color := range map[string]int{"red": value.Red, "green": value.Green, "blue": value.Blue} {
			if err := rangeError(name, color, 0, 255); err != nil {
				return nil, err
			}
		}
		return hw.FourZone{Mode: value.Mode, Speed: value.Speed, Brightness: value.Brightness, Direction: value.Direction, Red: value.Red, Green: value.Green, Blue: value.Blue}, nil
	default:
		return nil, fmt.Errorf("unknown or unsupported feature %q", key)
	}
}

func decodeStrict(raw json.RawMessage, target any) error {
	if err := rejectNull(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func rejectNull(raw json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	return inspectJSONValue(decoder, token)
}

func inspectJSONValue(decoder *json.Decoder, token json.Token) error {
	if token == nil {
		return fmt.Errorf("null is not allowed")
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			if _, err := decoder.Token(); err != nil {
				return err
			}
			value, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := inspectJSONValue(decoder, value); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	case '[':
		for decoder.More() {
			value, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := inspectJSONValue(decoder, value); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

func typeError(key, expected string, err error) error {
	return fmt.Errorf("invalid value for %s (expected %s): %w", key, expected, err)
}

func rangeError(name string, value, minimum, maximum int) error {
	if value < minimum || value > maximum {
		return fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return nil
}

func isHex(value string) bool {
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}
