# A Debian version must start with a digit and go up: a v1.2.3 tag becomes
# 1.2.3, an untagged tree 0.0.0+<commits>.g<hash>[.dirty], where the commit
# count is what keeps one dev build newer than the last.
VERSION ?= $(shell t=$$(git describe --tags --exact-match 2>/dev/null); \
  if [ -n "$$t" ]; then echo "$$t" | sed 's/^v//'; \
  else echo "0.0.0+$$(git rev-list --count HEAD 2>/dev/null || echo 0).g$$(git rev-parse --short HEAD 2>/dev/null || echo 0)$$(git diff --quiet 2>/dev/null || echo .dirty)"; fi)
LDFLAGS := -s -w -X main.version=$(VERSION)
ARCHES  := amd64 arm64

.PHONY: all ui binary deb run clean

all: binary

ui:
	cd ui && npm ci --no-fund --no-audit && npm run build

binary: ui
	@for a in $(ARCHES); do \
	  echo "building $$a"; \
	  CGO_ENABLED=0 GOOS=linux GOARCH=$$a go build -trimpath -ldflags '$(LDFLAGS)' -o dist/homedash-linux-$$a ./cmd/homedash || exit 1; \
	done

deb: binary
	@for a in $(ARCHES); do \
	  ARCH=$$a VERSION=$(VERSION) nfpm package -f packaging/nfpm.yaml -p deb -t dist/ || exit 1; \
	done

# Serve from a scratch state dir on a local port; open http://127.0.0.1:7433
run: ui
	HOMEDASH_STATE=$${HOMEDASH_STATE:-/tmp/homedash-dev} go run ./cmd/homedash serve

clean:
	rm -rf dist ui/dist
