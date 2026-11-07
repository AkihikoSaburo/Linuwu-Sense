obj-m := src/linuwu_sense.o

KVER  ?= $(if $(KERNELRELEASE),$(KERNELRELEASE),$(shell uname -r))
KDIR  := /lib/modules/$(KVER)/build
PWD   := $(shell pwd)

MODNAME  := linuwu_sense
PKGVER   := $(shell sed -n 's/^PACKAGE_VERSION="\(.*\)"/\1/p' dkms.conf)

ifeq ($(strip $(PKGVER)),)
$(error PACKAGE_VERSION is empty or dkms.conf not found)
endif

DKMS_SRC := /usr/src/$(MODNAME)-$(PKGVER)

.PHONY: all clean install uninstall

all:
	@if [ "$$(id -u)" -eq 0 ]; then \
		echo "Error: Jangan jalankan target 'all' sebagai root (jalankan sebagai user biasa)." >&2; \
		exit 1; \
	fi
	$(MAKE) --no-print-directory -f "$(KDIR)/Makefile" M="$(PWD)" modules
	@# Module signing di bawah ini hanya untuk alur build manual.
	@# Pada instalasi DKMS, penandatanganan modul ditangani oleh DKMS (misal via framework.conf).
	@if [ -f "$(HOME)/module-signing/MOK.priv" ] && [ -f "$(HOME)/module-signing/MOK.der" ]; then \
		if [ -x "/lib/modules/$(KVER)/build/scripts/sign-file" ]; then \
			SIGN_TOOL="/lib/modules/$(KVER)/build/scripts/sign-file"; \
		elif [ -x "/usr/src/linux-headers-$(KVER)/scripts/sign-file" ]; then \
			SIGN_TOOL="/usr/src/linux-headers-$(KVER)/scripts/sign-file"; \
		else \
			echo "ERROR: sign-file tool not found, but MOK keys exist." >&2; \
			exit 1; \
		fi; \
		echo "Signing module linuwu_sense.ko using $$SIGN_TOOL"; \
		if [ -w "$(PWD)/src/linuwu_sense.ko" ]; then \
			"$$SIGN_TOOL" sha256 \
				"$(HOME)/module-signing/MOK.priv" \
				"$(HOME)/module-signing/MOK.der" \
				"$(PWD)/src/linuwu_sense.ko"; \
		else \
			sudo "$$SIGN_TOOL" sha256 \
				"$(HOME)/module-signing/MOK.priv" \
				"$(HOME)/module-signing/MOK.der" \
				"$(PWD)/src/linuwu_sense.ko"; \
		fi; \
	else \
		echo "MOK keys not found in ~/module-signing/. Skipping module signing (Common for non-Secure Boot)."; \
	fi

clean:
	$(MAKE) --no-print-directory -f "$(KDIR)/Makefile" M="$(PWD)" clean

install:
	@if [ "$$(id -u)" -eq 0 ]; then \
		echo "Error: Jangan jalankan target 'install' sebagai root (jalankan sebagai user biasa, sudo dipanggil otomatis di dalam)." >&2; \
		exit 1; \
	fi
	@command -v dkms >/dev/null 2>&1 || { echo "Error: dkms tidak ditemukan. Silakan pasang dkms terlebih dahulu melalui package manager sistem Anda." >&2; exit 1; }
	-sudo rmmod acer_wmi 2>/dev/null
	@echo "blacklist acer_wmi" | sudo tee /etc/modprobe.d/blacklist-acer_wmi.conf > /dev/null
	-sudo dkms remove -m $(MODNAME) -v $(PKGVER) --all
	-sudo rm -rf $(DKMS_SRC)
	sudo mkdir -p $(DKMS_SRC)
	sudo cp -a dkms.conf Makefile src $(DKMS_SRC)/
	sudo find $(DKMS_SRC)/src \( -name '*.o' -o -name '*.ko' -o -name '*.mod*' -o -name '*.cmd' -o -name 'Module.symvers' -o -name 'modules.order' \) -delete
	sudo dkms add -m $(MODNAME) -v $(PKGVER)
	sudo dkms build -m $(MODNAME) -v $(PKGVER)
	sudo dkms install -m $(MODNAME) -v $(PKGVER) --force
	@echo "$(MODNAME)" | sudo tee /etc/modules-load.d/$(MODNAME).conf > /dev/null
	-sudo rmmod $(MODNAME) 2>/dev/null
	sudo modprobe $(MODNAME)
	@echo "Module $(MODNAME) $(PKGVER) berhasil dipasang via DKMS dan siap dimuat saat boot."

uninstall:
	-sudo dkms remove -m $(MODNAME) -v $(PKGVER) --all
	-sudo rm -rf $(DKMS_SRC)
	-sudo rm -f /etc/modules-load.d/$(MODNAME).conf
	-sudo rm -f /etc/modprobe.d/blacklist-acer_wmi.conf
	-sudo rmmod $(MODNAME) 2>/dev/null
	-sudo modprobe acer_wmi 2>/dev/null
	-sudo depmod -a
	@echo "Modul $(MODNAME) dan konfigurasi terkait berhasil dihapus."
