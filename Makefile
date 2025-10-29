include $(TOPDIR)/rules.mk

# 1. 修改包名和版本
PKG_NAME:=UAmask
PKG_VERSION:=0.3.1
PKG_RELEASE:=1

PKG_MAINTAINER:=Zesuy <hongri580@gmail.com>
PKG_LICENSE:=GPL-3.0-only
PKG_LICENSE_FILES:=LICENSE

PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_FLAGS:=no-mips16

# 2. 修改 Go 包路径和版本变量
GO_PKG:=UAmask
GO_PKG_LDFLAGS_X:= main.version=$(PKG_VERSION)

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

# 3. nftables 包定义
define Package/UAmask
	SECTION:=net
	CATEGORY:=Network
	SUBMENU:=Web Servers/Proxies
	TITLE:=A transparent proxy for changing User-Agent (nftables)
	URL:=https://github.com/Zesuy/UA-Mask
	DEPENDS:=$(GO_ARCH_DEPENDS) +luci-compat +firewall4 +kmod-nft-tproxy
	CONFLICTS:=ua3f-tproxy ua3f-tproxy-ipt
endef

define Package/UAmask/description
	A transparent proxy (TPROXY) for modifying HTTP User-Agent.
	This is the default version using nftables (firewall4).
endef

define Build/Prepare
	$(CP) $(CURDIR)/src/* $(PKG_BUILD_DIR)/
	$(CP) $(CURDIR)/go.mod $(PKG_BUILD_DIR)/
	$(CP) $(CURDIR)/go.sum $(PKG_BUILD_DIR)/
	$(CP) $(CURDIR)/LICENSE $(PKG_BUILD_DIR)/
endef

# 4. nftables 包 conffiles
define Package/UAmask/conffiles
/etc/config/UAmask
endef

# 5.nftables 包 install 步骤
define Package/UAmask/install
	$(call GoPackage/Package/Install/Bin,$(PKG_INSTALL_DIR))

	$(INSTALL_DIR) $(1)/usr/bin/
	$(INSTALL_BIN) $(PKG_INSTALL_DIR)/usr/bin/UAmask $(1)/usr/bin/UAmask
	$(INSTALL_DIR) $(1)/etc/init.d/
	$(INSTALL_BIN) ./files/UAmask.init $(1)/etc/init.d/UAmask
	$(INSTALL_DIR) $(1)/etc/config/
	$(INSTALL_CONF) ./files/UAmask.uci $(1)/etc/config/UAmask
	
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/model/cbi/
	$(INSTALL_CONF) ./files/luci/cbi.lua $(1)/usr/lib/lua/luci/model/cbi/UAmask.lua
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/controller/
	$(INSTALL_CONF) ./files/luci/controller.lua $(1)/usr/lib/lua/luci/controller/UAmask.lua
	
endef

# 7. IPTABLES 包定义
define Package/UAmask-ipt
	SECTION:=net
	CATEGORY:=Network
	SUBMENU:=Web Servers/Proxies
	TITLE:=A transparent proxy for changing User-Agent (iptables)
	URL:=https://github.com/Zesuy/UA-Mask
	DEPENDS:=$(GO_ARCH_DEPENDS) +luci-compat +firewall +kmod-ipt-tproxy
	CONFLICTS:=UAmask firewall4 ua3f-tproxy ua3f-tproxy-ipt
endef

define Package/UAmask-ipt/description
	A transparent proxy (TPROXY) for modifying HTTP User-Agent.
	This is the legacy version using iptables (firewall3).
endef

# 8. 新增：IPTABLES 包 conffiles
define Package/UAmask-ipt/conffiles
/etc/config/UAmask
endef

# 9. 新增：IPTABLES 包 install 步骤
define Package/UAmask-ipt/install
	$(INSTALL_DIR) $(1)/usr/bin/
	$(INSTALL_BIN) $(PKG_INSTALL_DIR)/usr/bin/UAmask $(1)/usr/bin/UAmask
	$(INSTALL_DIR) $(1)/etc/init.d/
	$(INSTALL_BIN) ./files/UAmask_ipt.init $(1)/etc/init.d/UAmask
	$(INSTALL_DIR) $(1)/etc/config/
	$(INSTALL_CONF) ./files/UAmask.uci $(1)/etc/config/UAmask
	
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/model/cbi/
	$(INSTALL_CONF) ./files/luci/cbi.lua $(1)/usr/lib/lua/luci/model/cbi/UAmask.lua
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/controller/
	$(INSTALL_CONF) ./files/luci/controller.lua $(1)/usr/lib/lua/luci/controller/UAmask.lua
endef


$(eval $(call GoBinPackage,UAmask))
$(eval $(call BuildPackage,UAmask))
$(eval $(call BuildPackage,UAmask-ipt))