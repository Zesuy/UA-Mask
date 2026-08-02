# OpenWrt Layout

- `package/`：OpenWrt `Makefile`，负责把 `core/` 编译为可安装包。
- `root/`：打包时直接铺到目标系统根目录的文件，例如 `etc/init.d/` 和 `etc/config/`。
- `luci/`：LuCI 控制器与 CBI 页面。

运行时仍以 `/etc/config/UAmask` 为用户配置真相源；`init.d` 会将 core 所需字段转换成 `/var/run/UAmask/config.json`，校验通过后使用 `UAmask -config <path>` 启动。
