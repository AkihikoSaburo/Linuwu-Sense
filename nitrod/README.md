# nitrod

`nitrod` is a root-owned Acer Nitro/Predator hardware daemon for the `linuwu_sense` sysfs interface. It keeps all hardware access in one owner goroutine. Unsupported or absent driver features leave the daemon running with an empty feature list.

## Build and Test

```sh
go test ./...
go build -o nitrod .
```

The only non-standard dependency is `golang.org/x/sys/unix`, used for Linux device and ioctl calls. The fake backend writes into a temporary directory and is used by the unit tests.

## Systemd Example

Install the binary and unit, then enable the service:

```sh
sudo install -Dm755 nitrod /usr/local/bin/nitrod
sudo install -Dm644 systemd/nitrod.service /etc/systemd/system/nitrod.service
sudo systemctl daemon-reload
sudo systemctl enable --now nitrod.service
```

The service runs as root; the socket is `/run/nitrod.sock`, owned by root with mode `0600`. Only root can connect to the socket, so run client commands through `sudo`. Set `--fan-temp-limit` in `ExecStart` to configure the manual-fan temperature fallback (default 85 C). The service's `ExecStopPost` invokes `--restore-fan-auto` to cover crashes and forced termination.

## Socket Protocol

Requests are UTF-8 JSON lines, with a 64 KiB maximum per line:

```json
{"id":1,"cmd":"get_state"}
{"id":2,"cmd":"set","key":"fan_speed","value":{"cpu":50,"gpu":50}}
{"cmd":"subscribe"}
```

Boolean sysfs features take integer `0` or `1`. `platform_profile` takes a string from the detected choices. RGB values use typed fields: `per_zone_mode` takes `zone1` through `zone4` as six-digit hex strings plus integer `brightness`; `four_zone_mode` takes integer `mode`, `speed`, `brightness`, `direction`, `red`, `green`, and `blue`. Successful writes are broadcast to subscribers.

## Hardware Smoke Test

First verify feature discovery with `get_state`. On a supported laptop, test a moderate fan setting, then immediately return to automatic control:

```sh
printf '%s\n' '{"id":1,"cmd":"get_state"}' | sudo nc -U /run/nitrod.sock
printf '%s\n' '{"id":2,"cmd":"set","key":"fan_speed","value":{"cpu":35,"gpu":35}}' | sudo nc -U /run/nitrod.sock
printf '%s\n' '{"id":3,"cmd":"set","key":"fan_speed","value":{"cpu":0,"gpu":0}}' | sudo nc -U /run/nitrod.sock
```

Only test RGB after confirming `per_zone_mode` or `four_zone_mode` is advertised. This daemon does not load/unload the kernel module, persist configuration, handle suspend/resume, or poll platform profiles.