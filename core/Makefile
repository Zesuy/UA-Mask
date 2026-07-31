GO ?= go

.PHONY: fmt-check vet test stress cover build shell-check ci

fmt-check:
	@test -z "$$(find . -name '*.go' -print0 | xargs -0 gofmt -l)" || \
		(echo "Go files are not formatted. Run 'gofmt -w' on the files above."; \
		find . -name '*.go' -print0 | xargs -0 gofmt -l; exit 1)

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

stress:
	$(GO) test -race -count=20 \
		./internal/app \
		./internal/ingress \
		./internal/proxy \
		./internal/session \
		./internal/events \
		./internal/state \
		./internal/policy \
		./internal/offload \
		./internal/firewall \
		./integration

cover:
	$(GO) test -covermode=atomic -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

build:
	$(GO) build ./...

shell-check:
	sh -n ../openwrt/root/etc/init.d/UAmask
	@test -z "$$(grep -n 'procd_append_param command' ../openwrt/root/etc/init.d/UAmask)" || \
		(echo "OpenWrt init must launch core through a JSON config file, not business flags."; exit 1)
	@grep -q 'procd_set_param command "$$PROG" -config "$$CORE_CONFIG_PATH"' ../openwrt/root/etc/init.d/UAmask
	sh ../openwrt/tests/config-generation-test.sh
	sh ../openwrt/tests/procd-contract-test.sh

ci: fmt-check vet test build shell-check
