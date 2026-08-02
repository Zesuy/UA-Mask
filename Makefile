ifneq ($(wildcard $(TOPDIR)/rules.mk),)
include $(CURDIR)/openwrt/package/Makefile
endif

LUCI_E2E_DIR := $(CURDIR)/openwrt/tests/e2e

.PHONY: luci-e2e-install luci-e2e-lint luci-e2e-prepare luci-e2e-smoke luci-e2e luci-e2e-package luci-e2e-failure-probe

luci-e2e-install:
	cd $(LUCI_E2E_DIR) && PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 npm ci --no-audit --no-fund

luci-e2e-lint:
	cd $(LUCI_E2E_DIR) && npm run lint
	sh -n $(LUCI_E2E_DIR)/scripts/prepare-base.sh
	sh -n $(LUCI_E2E_DIR)/scripts/run.sh
	sh -n $(LUCI_E2E_DIR)/scripts/guest-action.sh
	sh -n $(LUCI_E2E_DIR)/scripts/verify-ipk.sh

luci-e2e-prepare:
	$(LUCI_E2E_DIR)/scripts/prepare-base.sh --print-path

luci-e2e-smoke:
	$(LUCI_E2E_DIR)/scripts/run.sh fast smoke

luci-e2e:
	$(LUCI_E2E_DIR)/scripts/run.sh fast full

luci-e2e-package:
	@test -n "$(IPK)" || (echo "IPK=/absolute/path/to/UAmask.ipk is required"; exit 2)
	IPK="$(IPK)" $(LUCI_E2E_DIR)/scripts/run.sh package full

luci-e2e-failure-probe:
	$(LUCI_E2E_DIR)/scripts/run.sh fast failure-probe
