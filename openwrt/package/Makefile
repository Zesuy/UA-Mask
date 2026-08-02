include $(TOPDIR)/rules.mk

PACKAGE_DIR:=$(abspath $(dir $(lastword $(MAKEFILE_LIST))))
OPENWRT_DIR:=$(abspath $(PACKAGE_DIR)/..)
REPO_ROOT:=$(abspath $(OPENWRT_DIR)/..)
UAMASK_SOURCE_DIR?=$(REPO_ROOT)
UAMASK_VERSION?=$(strip $(shell cat $(UAMASK_SOURCE_DIR)/VERSION))

PKG_NAME:=UAmask
PKG_VERSION:=$(UAMASK_VERSION)
PKG_RELEASE:=1

PKG_MAINTAINER:=Zesuy <hongri580@gmail.com>
PKG_LICENSE:=GPL-3.0-only
PKG_LICENSE_FILES:=LICENSE

PKG_BUILD_PARALLEL:=1
PKG_BUILD_FLAGS:=no-mips16

ifeq ($(strip $(UAMASK_PREBUILT)),)
PKG_BUILD_DEPENDS:=golang/host
endif

include $(INCLUDE_DIR)/package.mk

ifeq ($(strip $(UAMASK_PREBUILT)),)
GO_PKG:=UAmask
GO_PKG_BUILD_PKG:=$(GO_PKG)/cmd/UAmask
GO_PKG_LDFLAGS_X:=main.version=$(PKG_VERSION)
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk
UAMASK_ARCH_DEPENDS:=$(GO_ARCH_DEPENDS)
endif

# 3. nftables 包定义
define Package/UAmask
	SECTION:=net
	CATEGORY:=Network
	SUBMENU:=Web Servers/Proxies
	TITLE:=A transparent proxy for changing User-Agent (nftables)
	URL:=https://github.com/Zesuy/UA-Mask
	DEPENDS:=$(UAMASK_ARCH_DEPENDS) +luci-compat +libubox
	CONFLICTS:=ua3f-tproxy ua3f-tproxy-ipt UAmask-ipt
endef

define Package/UAmask/description
	A transparent proxy (TPROXY) for modifying HTTP User-Agent.
	This is the default version using nftables (firewall4).
endef

ifeq ($(strip $(UAMASK_PREBUILT)),)
define Build/Prepare
	$(call Build/Prepare/Default)
	$(CP) -r $(UAMASK_SOURCE_DIR)/core/cmd $(PKG_BUILD_DIR)/
	$(CP) -r $(UAMASK_SOURCE_DIR)/core/internal $(PKG_BUILD_DIR)/
	$(CP) $(UAMASK_SOURCE_DIR)/core/go.mod $(PKG_BUILD_DIR)/
	$(CP) $(UAMASK_SOURCE_DIR)/core/go.sum $(PKG_BUILD_DIR)/
	$(CP) $(UAMASK_SOURCE_DIR)/LICENSE $(PKG_BUILD_DIR)/
endef
else
define Build/Prepare
	$(call Build/Prepare/Default)
	test -x $(UAMASK_PREBUILT)
	$(CP) $(UAMASK_PREBUILT) $(PKG_BUILD_DIR)/UAmask
	$(CP) $(UAMASK_SOURCE_DIR)/LICENSE $(PKG_BUILD_DIR)/
endef

define Build/Compile
	true
endef

# The release builder has already stripped the static Go binary with
# -ldflags "-s -w". OpenWrt's sstrip corrupts its section-header string table
# when applied a second time, so prebuilt mode must preserve the ELF verbatim.
RSTRIP:=:
endif

# 4. nftables 包 conffiles
define Package/UAmask/conffiles
/etc/config/UAmask
endef

define Package/UAmask/install/assets
	$(INSTALL_DIR) $(1)/etc/init.d/
	$(INSTALL_BIN) $(UAMASK_SOURCE_DIR)/openwrt/root/etc/init.d/UAmask $(1)/etc/init.d/UAmask
	$(INSTALL_DIR) $(1)/etc/config/
	$(INSTALL_CONF) $(UAMASK_SOURCE_DIR)/openwrt/root/etc/config/UAmask $(1)/etc/config/UAmask
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/model/cbi/
	$(INSTALL_DATA) $(UAMASK_SOURCE_DIR)/openwrt/luci/model/cbi/UAmask.lua $(1)/usr/lib/lua/luci/model/cbi/UAmask.lua
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/controller/
	$(INSTALL_DATA) $(UAMASK_SOURCE_DIR)/openwrt/luci/controller/UAmask.lua $(1)/usr/lib/lua/luci/controller/UAmask.lua
endef

ifeq ($(strip $(UAMASK_PREBUILT)),)
define Package/UAmask/install
	$(call GoPackage/Package/Install/Bin,$(PKG_INSTALL_DIR))
	$(INSTALL_DIR) $(1)/usr/bin/
	$(INSTALL_BIN) $(PKG_INSTALL_DIR)/usr/bin/UAmask $(1)/usr/bin/UAmask
	$(call Package/UAmask/install/assets,$(1))
endef
$(eval $(call GoBinPackage,UAmask))
else
define Package/UAmask/install
	$(INSTALL_DIR) $(1)/usr/bin/
	$(INSTALL_BIN) $(PKG_BUILD_DIR)/UAmask $(1)/usr/bin/UAmask
	$(call Package/UAmask/install/assets,$(1))
endef
endif
$(eval $(call BuildPackage,UAmask))
