package hw

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMissingModuleAdvertisesNoFeatures(t *testing.T) {
	root := t.TempDir()
	profileDirectory := filepath.Join(root, "firmware/acpi")
	if err := os.MkdirAll(profileDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDirectory, "platform_profile"), []byte("balanced\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDirectory, "platform_profile_choices"), []byte("balanced performance\n"), 0644); err != nil {
		t.Fatal(err)
	}
	backend := NewLinux(Options{SysRoot: root, DevRoot: t.TempDir()})
	if err := backend.Initialize(); err != nil {
		t.Fatal(err)
	}
	if features := backend.Features(); len(features) != 0 {
		t.Fatalf("features without driver module = %#v", features)
	}
}

func TestDriverGroupDetectionAndProfileChoices(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, moduleRelative)
	group := filepath.Join(base, "nitro_sense")
	if err := os.MkdirAll(group, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "module/linuwu_sense"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(group, "fan_speed"), []byte("0,0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	profileDirectory := filepath.Join(root, "firmware/acpi")
	if err := os.MkdirAll(profileDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(profileDirectory, "platform_profile"), []byte("balanced\n"), 0644)
	_ = os.WriteFile(filepath.Join(profileDirectory, "platform_profile_choices"), []byte("balanced performance\n"), 0644)
	backend := NewLinux(Options{SysRoot: root, DevRoot: t.TempDir()})
	if err := backend.Initialize(); err != nil {
		t.Fatal(err)
	}
	if got, want := backend.Features(), []string{"fan_speed", "platform_profile"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("features = %#v, want %#v", got, want)
	}
	if got, want := backend.ProfileChoices(), []string{"balanced", "performance"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("profile choices = %#v, want %#v", got, want)
	}
}

func TestTemperatureCUsesHottestReadableSensor(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "class/hwmon/hwmon0")
	second := filepath.Join(root, "class/hwmon/hwmon1")
	if err := os.MkdirAll(first, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(second, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(first, "temp1_input"), []byte("65000\n"), 0644)
	_ = os.WriteFile(filepath.Join(second, "temp1_input"), []byte("92000\n"), 0644)
	backend := NewLinux(Options{SysRoot: root, DevRoot: t.TempDir()})
	if err := backend.Initialize(); err != nil {
		t.Fatal(err)
	}
	if got, err := backend.TemperatureC(); err != nil || got != 92 {
		t.Fatalf("temperature = %v, err=%v; want 92", got, err)
	}
}
