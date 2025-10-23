# UA3F-TProxy


[![GitHub Release](https://img.shields.io/github/v/release/Zesuy/UA3F-tproxy?style=flat)](https://github.com/Zesuy/UA3F-tproxy/releases)
[![License](https://img.shields.io/github/license/Zesuy/UA3F-tproxy?style=flat)](https://github.com/Zesuy/UA3F-tproxy/blob/main/LICENSE)

一个为 OpenWrt 设计的高性能、低内存 User-Agent 修改工具 (带 LuCI 界面)。

本项目基于 [UA3F](https://github.com/SunBK201/UA3F) 重构，但使用 `TProxy (REDIRECT)` 方式重定向防火墙流量，实现了**卓越的性能**和**极低的内存占用**。

## 🎯 解决了什么问题？

本项目主要用于解决**校园网环境对多设备共享上网的检测**问题。

与此前的解决方案相比：

  * **ua2f**: 需要特定的内核功能，必须手动编译固件，使用不便。
  * **ua3f (原版)**: 依赖 Clash 等代理服务通过 SOCKS5 转发，所有流量（包括国内）都需经过代理核心，性能开销和内存占用巨大，不适合低性能的硬路由。

`ua3f-tproxy` 解决了以上痛点，它**不依赖Clash**，性能极高，配置极其简单。

## ✨ 核心特性

  * 🚀 **一键启用**: LuCI 界面勾选启用，自动配置 `nftables` 防火墙，无需任何额外配置。
  * ⚡ **高性能**: 采用 TProxy 架构，流量路径短，开销极低。
  * RAM **低内存占用**: 不依赖 Clash 核心，内存占用仅 **数MB**。
  * 🤝 **高兼容性**: 可与 `mwan3`, `openclash`, `sqm_qos` 等常见插件完美共存。
  * 🍃 **无侵入**: 配置基于 UCI，卸载后不留任何防火墙残余。

## 📊 架构对比

`ua3f-tproxy` 极大地优化了流量处理路径。

### 1\. ua3f (原版 Socks5 方案)

所有流量（包括国内）都必须经过 Clash，性能损失大。

```mermaid
graph LR
    A[LAN 流量] --> B{openclash}
    B{openclash} --socks5--> C[Ua3f]
    B{openclash} --绕过大陆--> D["UA泄露"]
    C[Ua3f] --> E[OUTPUT]
```

### 2\. ua3f-tproxy (TProxy 方案)

**场景一：单独使用（推荐）**

  * 无需依赖 OpenClash 即可修改 UA。

<!-- end list -->

```mermaid
graph LR
    A[LAN 流量] --"防火墙转发"--> B{ua3f-tproxy}
    B{ua3f-tproxy} --> C[OUTPUT]
```

**场景二：与 OpenClash 配合 (绕过大陆)**

  * **配置**：ua3f-tproxy(代理本机关: `关闭`) + openclash(代理本机关: `开启`, 绕过大陆: `开启`)
  * **效果**：国内流量仅由 ua3f-tproxy 修改 UA 后直连；国外流量由 OpenClash 代理。实现完美分流。
  * **备注**：如果关闭 OpenClash 的本机代理，OpenClash 将只代理被绕过的端口 (默认 22, 443)。

<!-- end list -->

```mermaid
graph LR
    A[LAN 流量] --> B{防火墙规则}
    B -->|端口 22, 443| C{OpenClash}
    B -->|非 22, 443 端口| D[ua3f-tproxy]
    
    C -->|国内流量| E[OUTPUT 直连]
    C -->|需要代理| F[Clash 核心]
    
    D --> E[OUTPUT 直连]
```

**场景三：与 OpenClash 配合 (全局代理)**

  * **配置**：ua3f-tproxy(代理本机关: `开启`) + openclash(代理本机: `开启`)
  * **效果**：ua3f-tproxy 发出的流量会被 OpenClash 再次捕获并处理。

<!-- end list -->

```mermaid
graph LR
    A[LAN 流量] --> B{防火墙规则}
    B -->|22 443| C{OpenClash}
    B --> D[ua3f-tproxy]
    
    C -->|国内流量| E[OUTPUT 直连]
    C -->|需要代理| F[Clash 核心]
    
    D --本机代理--> C{OpenClash}
```

## 🛠️ UA 替换模式说明

### 1\. 局部替换 (正则)

  * **工作方式**：当 UA 匹配到您设置的正则表达式时，**仅替换** UA 中被正则匹配到的那部分内容。
  * **适用场景**：只想修改 UA 中的某个关键词（如 `iPhone`），并保留其他信息。
  * **示例**：
      * 原始 UA：`Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X)`
      * 正 则：`iPhone`
      * 替换值：`FFF`
      * **结 果**：`Mozilla/5.0 (FFF; CPU iPhone OS 16_0 like Mac OS X)`

### 2\. 整体替换 (正则)

  * **工作方式**：当 UA 匹配到您设置的正则表达式时，**整个** UA 字符串都会被替换为您设置的新值。
  * **适用场景**：只针对特定 UA (如所有 `Android` 设备) 进行伪装，其他 UA 不受影响。
  * **示例**：
      * 原始 UA：`Mozilla/5.0 (Android 12; Mobile; rv:109.0)`
      * 正 则：`Android`
      * 替换值：`FFF`
      * **结 果**：`FFF`

### 3\. 全局替换 (强制)

  * **工作方式**：忽略正则表达式，**强制将所有** 流量的 UA 替换为您设置的新值。
  * **适用场景**：需要统一所有设备的 UA 标识，无论原始 UA 是什么。
  * **示例**：
      * 原始 UA：`Mozilla/5.0 (Windows NT 10.0; Win64; x64)`
      * 替换值：`FFF`
      * **结 果**：`FFF`

-----

### ⚙️ 参数说明

  * **`ua` (User-Agent 标识)**

      * 您希望使用的新 User-Agent 字符串。
      * 例如，填写 `FFF`，程序就会使用 `FFF` 来执行替换操作。

  * **`ua_mode` (UA 修改模式)**

      * 选择上述三种模式之一（**局部替换**、**整体替换**、**全局替换**），用于决定 UA 的处理逻辑。

  * **`ua_regex` (UA 匹配正则)**

      * 设置一个正则表达式，用于查找和匹配目标 UA。
      * 此参数仅在 **局部替换** 和 **整体替换** 模式下生效。
      * 例如：`(iPhone|Android|Windows)`，只有当 UA 包含这些关键字时，替换才会触发。

## 📦 安装

我们提供两种安装方式：

### 1\. 预编译包 (推荐)

1.  前往 [Releases 页面](https://github.com/Zesuy/UA3F-tproxy/releases)。
2.  下载适用于您路由器架构 (如 `x86_64`, `aarch64_cortex-a53`, `mips_24kc` 等) 的 `.ipk` 安装包。
3.  将 `.ipk` 包上传到 OpenWrt 的 `/tmp` 目录。
4.  通过 SSH 或 LuCI 终端执行安装：
    ```bash
    opkg install /tmp/luci-app-ua3f-tproxy_*.ipk
    ```

### 2\. 源码编译

1.  将本项目 `clone` 到您的 OpenWrt 编译环境的 `package/luci` 目录下。
2.  推荐在编译前 `make download` 和 `make j8`，完成一次固件编译。
3.  完成后再编译本软件包：
    ```bash
    make clean
    make package/UA3F-tproxy/compile
    ```

## 🚀 使用方法

安装后，你只需要：

1.  在 LuCI 界面中找到 "服务" -\> "UA3F TProxy"。
2.  勾选 "启用"。
3.  点击 "保存并应用"。

插件会自动为你配置好所有防火墙转发规则。你也可以在界面中自定义监听端口和需要修改的 User-Agent 字符串。

## 💡 兼容性与注意事项

  * **系统依赖**: 本项目基于 OpenWrt 23.05+ 构建，依赖 `nftables`。
  * **重要**: "ua3f-tproxy" 按照上方**架构对比**中的 "场景二" 配置可实现完美的流量处理（国内流量只改 UA，国外流量走代理）。请在 ua3f-tproxy 中 **关闭** "代理路由器本机"，并在 OpenClash 设置中 **打开** "代理路由器流量"。
  * **测试**: 已在 `X86_64` (OpenWrt 23.05) 平台测试通过，可与 `openclash`, `sqm_qos`, `mwan3`, `wireguard` 等插件正常协同工作。

## 📈 性能优化细节

`ua3f-tproxy` 在解析 HTTP 请求时也做了深度优化：

  * **流式解析**: 应用从数据流头部开始寻找 `User-Agent` 字段。
  * **立即修改**: 一旦找到该字段，立即对其进行解析和修改。
  * **直接复制**: 该字段之后的所有请求头数据，全部通过 `io.copy` 直接转发，省去了完整解析 HTTP 头的内存与性能开销。