PREFIX ?= /usr/local
DESTDIR ?=
BINDIR = $(PREFIX)/bin
UNITDIR ?= /etc/systemd/system
SYSUSERSDIR ?= /usr/lib/sysusers.d
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf 'dev')
ENABLE ?= 1

BUILD_DIR = build
BINARY = $(BUILD_DIR)/nitrod
UNIT_SOURCE = systemd/nitrod.service
SYSUSERS_SOURCE = packaging/nitrod.sysusers

.DEFAULT_GOAL := help
.PHONY: build test install uninstall purge clean help

build:
	@if [ "$$(id -u)" -eq 0 ]; then \
		printf '%s\n' 'error: make build must run as a regular user, not root' >&2; \
		exit 1; \
	fi
	@mkdir -p "$(BUILD_DIR)"
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o "$(BINARY)" .

test:
	go vet ./...
	go test -race ./...

install:
	@if [ ! -f "$(BINARY)" ]; then \
		printf '%s\n' 'error: build/nitrod not found; run make build first as a regular user' >&2; \
		exit 1; \
	fi
	install -D -m 0755 "$(BINARY)" "$(DESTDIR)$(BINDIR)/nitrod"
	install -D -m 0644 "$(SYSUSERS_SOURCE)" "$(DESTDIR)$(SYSUSERSDIR)/nitrod.sysusers"
	sed 's|/usr/local/bin/nitrod|$(BINDIR)/nitrod|g' "$(UNIT_SOURCE)" | install -D -m 0644 /dev/stdin "$(DESTDIR)$(UNITDIR)/nitrod.service"
	@if [ -z "$(DESTDIR)" ]; then \
		systemd-sysusers; \
		systemctl daemon-reload; \
		if [ "$(ENABLE)" = "1" ]; then \
			systemctl enable nitrod.service; \
			systemctl restart nitrod.service; \
		else \
			if systemctl is-active --quiet nitrod.service; then systemctl restart nitrod.service; fi; \
		fi; \
	fi

uninstall:
	@if [ -z "$(DESTDIR)" ]; then \
		systemctl stop nitrod.service >/dev/null 2>&1 || true; \
		systemctl disable nitrod.service >/dev/null 2>&1 || true; \
		if ! systemctl cat nitrod.service >/dev/null 2>&1 && [ -x "$(DESTDIR)$(BINDIR)/nitrod" ]; then \
			"$(DESTDIR)$(BINDIR)/nitrod" --restore-fan-auto >/dev/null 2>&1 || true; \
		fi; \
	fi
	@rm -f -- "$(DESTDIR)$(BINDIR)/nitrod" "$(DESTDIR)$(UNITDIR)/nitrod.service" "$(DESTDIR)$(SYSUSERSDIR)/nitrod.sysusers" || true
	@if [ -z "$(DESTDIR)" ]; then systemctl daemon-reload >/dev/null 2>&1 || true; fi

purge: uninstall
	@for relative_path in /etc/nitrod /var/lib/nitrod; do \
		case "$$relative_path" in \
			/etc/nitrod|/var/lib/nitrod) \
				if [ -n "$(DESTDIR)" ]; then path="$(DESTDIR)$$relative_path"; else path="$$relative_path"; fi; \
				if [ -n "$$path" ]; then rm -rf -- "$$path" || true; fi ;; \
			*) printf 'refusing unsafe purge path: %s\n' "$$relative_path" >&2; exit 1 ;; \
		esac; \
	done; \
	if [ -z "$(DESTDIR)" ] && getent group nitrod >/dev/null 2>&1; then groupdel nitrod >/dev/null 2>&1 || true; fi

clean:
	@if [ -z "$(BUILD_DIR)" ] || [ "$(BUILD_DIR)" = "/" ]; then \
		printf '%s\n' 'error: refusing to clean an empty or root build directory' >&2; \
		exit 1; \
	fi
	rm -rf -- "$(BUILD_DIR)"

help:
	@printf '%s\n' \
		'nitrod targets:' \
		'  make build      Build build/nitrod as a non-root user' \
		'  make test       Run go vet and race-enabled tests' \
		'  make install    Install an existing build/nitrod' \
		'  make uninstall  Remove daemon files, keeping config/state/group' \
		'  make purge      Uninstall and remove config/state/group' \
		'  make clean      Remove build artifacts' \
		'  make help       Show this help (default target)'