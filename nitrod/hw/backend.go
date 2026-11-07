package hw

import "encoding/json"

type Value interface {
	HardwareValue()
}

type Profile struct{ Name string }
type FanSpeed struct{ CPU, GPU int }
type Boolean struct{ Enabled bool }
type USBCharging struct{ Level int }
type PerZone struct {
	Colors     [4]string
	Brightness int
}
type FourZone struct {
	Mode, Speed, Brightness, Direction int
	Red, Green, Blue                   int
}

func (Profile) HardwareValue()     {}
func (FanSpeed) HardwareValue()    {}
func (Boolean) HardwareValue()     {}
func (USBCharging) HardwareValue() {}
func (PerZone) HardwareValue()     {}
func (FourZone) HardwareValue()    {}

type Backend interface {
	Initialize() error
	Features() []string
	ProfileChoices() []string
	Read(key string) (json.RawMessage, error)
	Write(key string, value Value) error
	TemperatureC() (float64, error)
	Close() error
}
