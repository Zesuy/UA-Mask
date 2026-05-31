# OpenWrt Layout

- `package/`：OpenWrt `Makefile`，负责把 `core/` 编译为可安装包。
- `root/`：打包时直接铺到目标系统根目录的文件，例如 `etc/init.d/` 和 `etc/config/`。
- `luci/`：LuCI 控制器与 CBI 页面。
