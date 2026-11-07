package hw

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var allFeatures = []string{
	"platform_profile", "fan_speed", "battery_limiter", "battery_calibration", "lcd_override",
	"backlight_timeout", "boot_animation_sound", "usb_charging", "per_zone_mode", "four_zone_mode",
}

type Fake struct {
	Root             string
	Available        []string
	Profiles         []string
	HIDPresent       bool
	HIDErrorOnReport int
	HIDReports       [][hidReportLength]byte
	SysfsWrites      []string
	Temperature      float64
	TemperatureError error
	writeCount       int
}

func NewFake(root string, features []string) (*Fake, error) {
	if root == "" {
		var err error
		root, err = os.MkdirTemp("", "nitrod-fake-")
		if err != nil {
			return nil, err
		}
	}
	if features == nil {
		features = append([]string(nil), allFeatures...)
	}
	fake := &Fake{Root: root, Available: append([]string(nil), features...), Profiles: []string{"balanced", "performance", "quiet"}}
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, err
	}
	for _, key := range fake.Available {
		path := filepath.Join(root, key)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, err
		}
		initial := "0\n"
		switch key {
		case "platform_profile":
			initial = "balanced\n"
		case "fan_speed":
			initial = "0,0\n"
		case "per_zone_mode":
			initial = "000000,000000,000000,000000,0\n"
		case "four_zone_mode":
			initial = "0,0,0,1,0,0,0\n"
		}
		if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
			return nil, err
		}
	}
	return fake, nil
}

func (f *Fake) Features() []string       { return append([]string(nil), f.Available...) }
func (f *Fake) ProfileChoices() []string { return append([]string(nil), f.Profiles...) }
func (f *Fake) Initialize() error        { return nil }

func (f *Fake) Read(key string) (json.RawMessage, error) {
	if !f.hasFeature(key) {
		return nil, fmt.Errorf("feature %q is unavailable", key)
	}
	data, err := os.ReadFile(filepath.Join(f.Root, key))
	if err != nil {
		return nil, err
	}
	return json.Marshal(strings.TrimSpace(string(data)))
}

func (f *Fake) Write(key string, value Value) error {
	if !f.hasFeature(key) {
		return fmt.Errorf("feature %q is unavailable", key)
	}
	if key == "per_zone_mode" {
		zones, brightness, err := unpackPerZone(value)
		if err != nil {
			return err
		}
		if f.HIDPresent {
			for i, color := range zones {
				f.writeCount++
				if f.HIDErrorOnReport != 0 && f.writeCount == f.HIDErrorOnReport {
					break
				}
				red, green, blue := parseColor(color)
				f.HIDReports = append(f.HIDReports, buildHIDReport(0x02, byte(brightness), 0, 0, red, green, blue, byte(1<<i)))
			}
			if f.HIDErrorOnReport == 0 || f.writeCount != f.HIDErrorOnReport {
				return nil
			}
		}
		return f.writeFile(key, strings.Join(zones[:], ",")+","+strconv.Itoa(brightness)+"\n")
	}
	if key == "four_zone_mode" {
		mode, err := unpackFourZone(value)
		if err != nil {
			return err
		}
		if f.HIDPresent {
			f.writeCount++
			if f.HIDErrorOnReport == 0 || f.writeCount != f.HIDErrorOnReport {
				report, ok := effectReport(mode)
				if !ok {
					return fmt.Errorf("unsupported RGB mode %d", mode.Mode)
				}
				f.HIDReports = append(f.HIDReports, report)
				return nil
			}
		}
		return f.writeFile(key, formatFourZone(mode))
	}
	formatted, err := formatValue(key, value)
	if err != nil {
		return err
	}
	return f.writeFile(key, formatted)
}

func (f *Fake) writeFile(key, content string) error {
	if err := os.WriteFile(filepath.Join(f.Root, key), []byte(content), 0644); err != nil {
		return err
	}
	f.SysfsWrites = append(f.SysfsWrites, key)
	return nil
}

func (f *Fake) TemperatureC() (float64, error) {
	if f.TemperatureError != nil {
		return 0, f.TemperatureError
	}
	if f.Temperature <= 0 {
		return 0, fmt.Errorf("no readable hwmon temperature sensor found")
	}
	return f.Temperature, nil
}

func (f *Fake) Close() error { return nil }
func (f *Fake) hasFeature(key string) bool {
	for _, feature := range f.Available {
		if feature == key {
			return true
		}
	}
	return false
}
