package hw

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	moduleRelative  = "module/linuwu_sense/drivers/platform:acer-wmi/acer-wmi"
	profileRelative = "firmware/acpi/platform_profile"
)

type Options struct {
	SysRoot string
	DevRoot string
}

type Linux struct {
	sysRoot string
	files   map[string]string
	choices map[string]bool
	hid     hidDevice
	ready   bool
}

func NewLinux(options Options) *Linux {
	if options.SysRoot == "" {
		options.SysRoot = "/sys"
	}
	if options.DevRoot == "" {
		options.DevRoot = "/dev"
	}
	backend := &Linux{
		sysRoot: options.SysRoot,
		files:   make(map[string]string),
		choices: make(map[string]bool),
		hid: hidDevice{
			deviceRoot: filepath.Join(options.SysRoot, "class/hidraw"),
			devRoot:    options.DevRoot,
		},
	}
	return backend
}

func (l *Linux) Initialize() error {
	if !l.ready {
		l.detect()
		l.ready = true
	}
	return nil
}

func (l *Linux) detect() {
	l.files = make(map[string]string)
	l.choices = make(map[string]bool)
	base := filepath.Join(l.sysRoot, moduleRelative)
	if _, err := os.Stat(filepath.Join(l.sysRoot, "module/linuwu_sense")); err != nil {
		slog.Error("linuwu_sense kernel module is not present; hardware features are disabled", "error", err)
		return
	} else {
		foundGroup := false
		for _, group := range []string{"nitro_sense", "predator_sense"} {
			groupPath := filepath.Join(base, group)
			if info, err := os.Stat(groupPath); err == nil && info.IsDir() {
				foundGroup = true
				for _, name := range []string{"fan_speed", "battery_limiter", "battery_calibration", "lcd_override", "backlight_timeout", "boot_animation_sound", "usb_charging"} {
					path := filepath.Join(groupPath, name)
					if _, err := os.Stat(path); err == nil {
						l.files[name] = path
					}
				}
			}
		}
		if !foundGroup {
			slog.Error("linuwu_sense is loaded but nitro_sense/predator_sense sysfs groups are missing; hardware features are disabled")
			return
		}
		keyboard := filepath.Join(base, "four_zoned_kb")
		for _, name := range []string{"per_zone_mode", "four_zone_mode"} {
			path := filepath.Join(keyboard, name)
			if _, err := os.Stat(path); err == nil {
				l.files[name] = path
			}
		}
	}

	profile := filepath.Join(l.sysRoot, profileRelative)
	choices := filepath.Join(l.sysRoot, "firmware/acpi/platform_profile_choices")
	if _, err := os.Stat(profile); err == nil {
		if data, readErr := os.ReadFile(choices); readErr == nil {
			l.files["platform_profile"] = profile
			for _, choice := range strings.Fields(string(data)) {
				l.choices[choice] = true
			}
		}
	}
}

func (l *Linux) Features() []string {
	features := make([]string, 0, len(l.files))
	for key := range l.files {
		features = append(features, key)
	}
	sort.Strings(features)
	return features
}

func (l *Linux) ProfileChoices() []string {
	choices := make([]string, 0, len(l.choices))
	for choice := range l.choices {
		choices = append(choices, choice)
	}
	sort.Strings(choices)
	return choices
}

func (l *Linux) Read(key string) (json.RawMessage, error) {
	path, ok := l.files[key]
	if !ok {
		return nil, fmt.Errorf("feature %q is unavailable", key)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(strings.TrimSpace(string(data)))
	return encoded, err
}

func (l *Linux) Write(key string, value Value) error {
	if _, ok := l.files[key]; !ok {
		return fmt.Errorf("feature %q is unavailable", key)
	}
	if key == "per_zone_mode" {
		colors, brightness, err := unpackPerZone(value)
		if err != nil {
			return err
		}
		if l.hidRGBZones(colors, brightness) {
			slog.Info("RGB update used ENEK5130 HID")
			return nil
		}
		slog.Info("RGB HID unavailable or failed; falling back to sysfs", "feature", key)
		return l.writeSysfs(key, strings.Join(colors[:], ",")+","+strconv.Itoa(brightness)+"\n")
	}
	if key == "four_zone_mode" {
		mode, err := unpackFourZone(value)
		if err != nil {
			return err
		}
		if l.hidRGBEffect(mode) {
			slog.Info("RGB update used ENEK5130 HID")
			return nil
		}
		slog.Info("RGB HID unavailable or failed; falling back to sysfs", "feature", key)
		return l.writeSysfs(key, formatFourZone(mode))
	}
	if key == "platform_profile" {
		profile, ok := value.(Profile)
		if !ok || !l.choices[profile.Name] {
			return fmt.Errorf("platform_profile must be one of %v", l.ProfileChoices())
		}
	}
	formatted, err := formatValue(key, value)
	if err != nil {
		return err
	}
	return l.writeSysfs(key, formatted)
}

func (l *Linux) writeSysfs(key, value string) error {
	file, err := os.OpenFile(l.files[key], os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(file, value)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func (l *Linux) TemperatureC() (float64, error) {
	paths, err := filepath.Glob(filepath.Join(l.sysRoot, "class/hwmon/hwmon*", "temp*_input"))
	if err != nil {
		return 0, err
	}
	var hottest float64
	found := false
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		milliC, parseErr := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if parseErr == nil {
			celsius := milliC / 1000
			if !found || celsius > hottest {
				hottest = celsius
				found = true
			}
		}
	}
	if found {
		return hottest, nil
	}
	return 0, errors.New("no readable hwmon temperature sensor found")
}

func (l *Linux) Close() error { return nil }

func (l *Linux) hidRGBZones(colors [4]string, brightness int) bool {
	for i, color := range colors {
		red, green, blue := parseColor(color)
		report := buildHIDReport(0x02, byte(brightness), 0, 0, red, green, blue, byte(1<<i))
		if err := l.hid.send(report); err != nil {
			slog.Warn("ENEK5130 zone report failed", "zone", i+1, "error", err)
			return false
		}
	}
	return true
}

func (l *Linux) hidRGBEffect(mode FourZone) bool {
	report, ok := effectReport(mode)
	if !ok {
		return false
	}
	if err := l.hid.send(report); err != nil {
		slog.Warn("ENEK5130 effect report failed", "error", err)
		return false
	}
	return true
}

func parseColor(color string) (byte, byte, byte) {
	red, _ := strconv.ParseUint(color[0:2], 16, 8)
	green, _ := strconv.ParseUint(color[2:4], 16, 8)
	blue, _ := strconv.ParseUint(color[4:6], 16, 8)
	return byte(red), byte(green), byte(blue)
}

func unpackPerZone(value Value) ([4]string, int, error) {
	zone, ok := value.(PerZone)
	if !ok {
		return [4]string{}, 0, fmt.Errorf("per_zone_mode requires hw.PerZone")
	}
	return zone.Colors, zone.Brightness, nil
}

func unpackFourZone(value Value) (FourZone, error) {
	mode, ok := value.(FourZone)
	if !ok {
		return FourZone{}, fmt.Errorf("four_zone_mode requires hw.FourZone")
	}
	return mode, nil
}

func formatValue(key string, value Value) (string, error) {
	switch key {
	case "platform_profile":
		profile, ok := value.(Profile)
		if !ok {
			return "", fmt.Errorf("invalid platform profile value")
		}
		return profile.Name + "\n", nil
	case "fan_speed":
		fan, ok := value.(FanSpeed)
		if !ok {
			return "", fmt.Errorf("fan_speed requires hw.FanSpeed")
		}
		return fmt.Sprintf("%d,%d\n", fan.CPU, fan.GPU), nil
	case "battery_limiter", "battery_calibration", "lcd_override", "backlight_timeout", "boot_animation_sound":
		flag, ok := value.(Boolean)
		if !ok {
			return "", fmt.Errorf("%s requires hw.Boolean", key)
		}
		if flag.Enabled {
			return "1\n", nil
		}
		return "0\n", nil
	case "usb_charging":
		charging, ok := value.(USBCharging)
		if !ok {
			return "", fmt.Errorf("usb_charging requires hw.USBCharging")
		}
		return strconv.Itoa(charging.Level) + "\n", nil
	case "per_zone_mode":
		colors, brightness, err := unpackPerZone(value)
		if err != nil {
			return "", err
		}
		return strings.Join(colors[:], ",") + "," + strconv.Itoa(brightness) + "\n", nil
	case "four_zone_mode":
		mode, err := unpackFourZone(value)
		if err != nil {
			return "", err
		}
		return formatFourZone(mode), nil
	default:
		return "", fmt.Errorf("unknown feature %q", key)
	}
}

func formatFourZone(mode FourZone) string {
	return fmt.Sprintf("%d,%d,%d,%d,%d,%d,%d\n", mode.Mode, mode.Speed, mode.Brightness, mode.Direction, mode.Red, mode.Green, mode.Blue)
}

func effectAndSpeed(mode, speed int) (byte, byte, bool) {
	if mode == 0 {
		return 0x02, 0, true
	}
	effects := map[int]byte{1: 0x04, 2: 0x05, 3: 0x07, 4: 0x07, 5: 0x09, 6: 0x0A, 7: 0x0B}
	effect, ok := effects[mode]
	if !ok {
		return 0, 0, false
	}
	if effect == 0x04 || effect == 0x05 {
		return effect, byte(speed), true
	}
	return effect, byte(min(10, max(1, speed+1))), true
}

func effectReport(mode FourZone) ([hidReportLength]byte, bool) {
	effect, speed, ok := effectAndSpeed(mode.Mode, mode.Speed)
	if !ok {
		return [hidReportLength]byte{}, false
	}
	direction := byte(mode.Direction)
	if mode.Mode == 0 {
		direction = 0
	}
	return buildHIDReport(effect, byte(mode.Brightness), speed, direction, byte(mode.Red), byte(mode.Green), byte(mode.Blue), 0x0F), true
}
