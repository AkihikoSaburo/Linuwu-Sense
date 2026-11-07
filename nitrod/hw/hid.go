package hw

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const hidReportLength = 11

func ioctlHIDIOCSFeature(length uintptr) uintptr {
	return (uintptr(3) << 30) | (length << 16) | (uintptr('H') << 8) | 0x06
}

func buildHIDReport(effect, brightness, speed, direction, red, green, blue, zoneMask byte) [hidReportLength]byte {
	return [hidReportLength]byte{0xA4, 0x21, effect, brightness, speed, direction, red, green, blue, zoneMask, 0x00}
}

type hidDevice struct {
	deviceRoot string
	devRoot    string
	cached     string
}

func (h *hidDevice) candidates() []string {
	paths, _ := filepath.Glob(filepath.Join(h.devRoot, "hidraw*"))
	sort.Strings(paths)
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		name := filepath.Base(path)
		data, err := os.ReadFile(filepath.Join(h.deviceRoot, name, "device", "uevent"))
		if err != nil {
			continue
		}
		text := string(data)
		if strings.Contains(text, "HID_ID=0018:00000CF2:00005130") || strings.Contains(text, "HID_NAME=ENEK5130:00 0CF2:5130") {
			result = append(result, path)
		}
	}
	if h.cached != "" {
		for i, candidate := range result {
			if candidate == h.cached {
				copy(result[1:i+1], result[0:i])
				result[0] = h.cached
				break
			}
		}
	}
	return result
}

func (h *hidDevice) send(report [hidReportLength]byte) error {
	candidates := h.candidates()
	if len(candidates) == 0 {
		return errors.New("ENEK5130 HID device not found")
	}
	var failures []string
	for _, path := range candidates {
		fd, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		_, _, errno := syscall.Syscall(unix.SYS_IOCTL, uintptr(fd), ioctlHIDIOCSFeature(hidReportLength), uintptr(unsafe.Pointer(&report[0])))
		unix.Close(fd)
		runtime.KeepAlive(&report)
		if errno != 0 {
			failures = append(failures, fmt.Sprintf("%s: %v", path, errno))
			continue
		}
		h.cached = path
		return nil
	}
	return fmt.Errorf("all ENEK5130 HID candidates failed: %s", strings.Join(failures, "; "))
}
