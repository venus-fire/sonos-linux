PREFIX       ?= /usr/local
LIBDIR       := $(PREFIX)/lib/sonos-linux
BINDIR       := $(PREFIX)/bin
SYSTEMD_USER := /usr/lib/systemd/user
BINARY       := sonos-linux
VERSION      ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//' || echo "0.0.0")

.PHONY: build install uninstall enable disable deb clean user-install user-uninstall

USER_PREFIX ?= $(HOME)/.local
USER_UNIT   := $(HOME)/.config/systemd/user

build:
	go build -ldflags="-s -w" -o $(BINARY) .

install: build
	install -d $(DESTDIR)$(LIBDIR)
	install -m 755 $(BINARY) $(DESTDIR)$(LIBDIR)/$(BINARY)
	install -m 755 tray.py $(DESTDIR)$(LIBDIR)/tray.py
	install -d $(DESTDIR)$(BINDIR)
	ln -sf $(LIBDIR)/$(BINARY) $(DESTDIR)$(BINDIR)/$(BINARY)
	install -d $(DESTDIR)$(SYSTEMD_USER)
	install -m 644 sonos-linux.service $(DESTDIR)$(SYSTEMD_USER)/sonos-linux.service
	@echo ""
	@echo "Installed. Now run (without sudo):"
	@echo "  make enable"

enable:
	systemctl --user daemon-reload
	systemctl --user enable --now sonos-linux

disable:
	systemctl --user disable --now sonos-linux

uninstall:
	-systemctl --user disable --now sonos-linux 2>/dev/null
	rm -f $(DESTDIR)$(BINDIR)/$(BINARY)
	rm -rf $(DESTDIR)$(LIBDIR)
	rm -f $(DESTDIR)$(SYSTEMD_USER)/sonos-linux.service
	-systemctl --user daemon-reload 2>/dev/null

deb: build
	$(eval DEB_DIR := $(BINARY)_$(VERSION)_amd64)
	rm -rf $(DEB_DIR)
	mkdir -p $(DEB_DIR)/DEBIAN
	mkdir -p $(DEB_DIR)/usr/lib/sonos-linux
	mkdir -p $(DEB_DIR)/usr/bin
	mkdir -p $(DEB_DIR)/usr/lib/systemd/user
	cp $(BINARY) $(DEB_DIR)/usr/lib/sonos-linux/$(BINARY)
	cp tray.py $(DEB_DIR)/usr/lib/sonos-linux/tray.py
	chmod 755 $(DEB_DIR)/usr/lib/sonos-linux/$(BINARY) $(DEB_DIR)/usr/lib/sonos-linux/tray.py
	ln -sf /usr/lib/sonos-linux/$(BINARY) $(DEB_DIR)/usr/bin/$(BINARY)
	sed 's|/usr/local/lib|/usr/lib|g' sonos-linux.service > $(DEB_DIR)/usr/lib/systemd/user/sonos-linux.service
	@echo "Package: sonos-linux" > $(DEB_DIR)/DEBIAN/control
	@echo "Version: $(VERSION)" >> $(DEB_DIR)/DEBIAN/control
	@echo "Architecture: amd64" >> $(DEB_DIR)/DEBIAN/control
	@echo "Maintainer: Luis E. Gonzalez" >> $(DEB_DIR)/DEBIAN/control
	@echo "Depends: ffmpeg, pulseaudio-utils, pipewire-pulse, python3-gi, gir1.2-gtk-3.0" >> $(DEB_DIR)/DEBIAN/control
	@echo "Description: Stream Linux system audio to Sonos speakers" >> $(DEB_DIR)/DEBIAN/control
	@echo " Creates virtual audio sinks for each Sonos speaker on the network." >> $(DEB_DIR)/DEBIAN/control
	@echo " Route system audio to any sink and it plays on that speaker." >> $(DEB_DIR)/DEBIAN/control
	@echo " Includes volume sync, auto-pause, and a system tray icon." >> $(DEB_DIR)/DEBIAN/control
	dpkg-deb --build --root-owner-group $(DEB_DIR)
	rm -rf $(DEB_DIR)
	@echo ""
	@echo "Built: $(DEB_DIR).deb"

clean:
	rm -f $(BINARY)
	rm -rf $(BINARY)_*_amd64

# Rootless install into ~/.local plus a user unit in ~/.config/systemd/user.
user-install: build
	install -d $(USER_PREFIX)/lib/sonos-linux $(USER_PREFIX)/bin $(USER_UNIT)
	install -m 755 $(BINARY) $(USER_PREFIX)/lib/sonos-linux/$(BINARY)
	install -m 755 tray.py $(USER_PREFIX)/lib/sonos-linux/tray.py
	install -m 755 extras/sonos-pin-app.py $(USER_PREFIX)/bin/sonos-pin-app
	ln -sf $(USER_PREFIX)/lib/sonos-linux/$(BINARY) $(USER_PREFIX)/bin/$(BINARY)
	install -m 644 sonos-linux.user.service $(USER_UNIT)/sonos-linux.service
	install -m 644 sonos-pin-app.user.service $(USER_UNIT)/sonos-pin-app.service
	systemctl --user daemon-reload
	@echo "Installed to $(USER_PREFIX). Now run: systemctl --user enable --now sonos-linux"

user-uninstall:
	-systemctl --user disable --now sonos-linux 2>/dev/null
	rm -f $(USER_UNIT)/sonos-linux.service
	rm -rf $(USER_PREFIX)/lib/sonos-linux
	rm -f $(USER_PREFIX)/bin/$(BINARY)
	-systemctl --user daemon-reload 2>/dev/null
