include $(TOPDIR)/rules.mk

# 1. 修改包名和版本
PKG_NAME:=ua3f-tproxy
PKG_VERSION:=0.2.2
PKG_RELEASE:=1

PKG_MAINTAINER:=Zesuy <hongri580@gmail.com>
PKG_LICENSE:=GPL-3.0-only
PKG_LICENSE_FILES:=LICENSE

PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_FLAGS:=no-mips16

# 2. 修改 Go 包路径和版本变量
GO_PKG:=ua3f-tproxy
GO_PKG_LDFLAGS_X:= main.version=$(PKG_VERSION)

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

# 3. nftables 包定义
define Package/ua3f-tproxy
	SECTION:=net
	CATEGORY:=Network
	SUBMENU:=Web Servers/Proxies
	TITLE:=A transparent proxy for changing User-Agent (nftables)
	URL:=https://github.com/Zesuy/UA3F-tproxy
	DEPENDS:=$(GO_ARCH_DEPENDS) +luci-compat +firewall4 +kmod-nft-tproxy
endef

define Package/ua3f-tproxy/description
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
define Package/ua3f-tproxy/conffiles
/etc/config/ua3f-tproxy
endef

# 5.nftables 包 install 步骤
define Package/ua3f-tproxy/install
	$(call GoPackage/Package/Install/Bin,$(PKG_INSTALL_DIR))

	$(INSTALL_DIR) $(1)/usr/bin/
	$(INSTALL_BIN) $(PKG_INSTALL_DIR)/usr/bin/ua3f-tproxy $(1)/usr/bin/ua3f-tproxy
	$(INSTALL_DIR) $(1)/etc/init.d/
	$(INSTALL_BIN) ./files/ua3f-tproxy.init $(1)/etc/init.d/ua3f-tproxy
	$(INSTALL_DIR) $(1)/etc/config/
	$(INSTALL_CONF) ./files/ua3f-tproxy.uci $(1)/etc/config/ua3f-tproxy
	
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/model/cbi/
	$(INSTALL_CONF) ./files/luci/cbi.lua $(1)/usr/lib/lua/luci/model/cbi/ua3f-tproxy.lua
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/controller/
	$(INSTALL_CONF) ./files/luci/controller.lua $(1)/usr/lib/lua/luci/controller/ua3f-tproxy.lua
	
endef

# 7. IPTABLES 包定义
define Package/ua3f-tproxy-ipt
	SECTION:=net
	CATEGORY:=Network
	SUBMENU:=Web Servers/Proxies
	TITLE:=A transparent proxy for changing User-Agent (iptables)
	URL:=https://github.com/Zesuy/UA3F-tproxy
	DEPENDS:=$(GO_ARCH_DEPENDS) +luci-compat +firewall +kmod-ipt-tproxy
	CONFLICTS:=ua3f-tproxy firewall4
endef

define Package/ua3f-tproxy-ipt/description
	A transparent proxy (TPROXY) for modifying HTTP User-Agent.
	This is the legacy version using iptables (firewall3).
endef

# 8. 新增：IPTABLES 包 conffiles
define Package/ua3f-tproxy-ipt/conffiles
/etc/config/ua3f-tproxy
endef

# 9. 新增：IPTABLES 包 install 步骤
define Package/ua3f-tproxy-ipt/install
	$(INSTALL_DIR) $(1)/usr/bin/
	$(INSTALL_BIN) $(PKG_INSTALL_DIR)/usr/bin/ua3f-tproxy $(1)/usr/bin/ua3f-tproxy
	$(INSTALL_DIR) $(1)/etc/init.d/
	$(INSTALL_BIN) ./files/ua3f-tproxy_ipt.init $(1)/etc/init.d/ua3f-tproxy
	$(INSTALL_DIR) $(1)/etc/config/
	$(INSTALL_CONF) ./files/ua3f-tproxy.uci $(1)/etc/config/ua3f-tproxy
	
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/model/cbi/
	$(INSTALL_CONF) ./files/luci/cbi.lua $(1)/usr/lib/lua/luci/model/cbi/ua3f-tproxy.lua
	$(INSTALL_DIR) $(1)/usr/lib/lua/luci/controller/
	$(INSTALL_CONF) ./files/luci/controller.lua $(1)/usr/lib/lua/luci/controller/ua3f-tproxy.lua
endef


$(eval $(call GoBinPackage,ua3f-tproxy))
$(eval $(call BuildPackage,ua3f-tproxy))
$(eval $(call BuildPackage,ua3f-tproxy-ipt))