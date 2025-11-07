# UA-Mask

<!-- PROJECT SHIELDS -->
[![GitHub Release][release-shield]][release-url]
[![MIT License][license-shield]][license-url]
<!-- PROJECT LOGO -->
<br />
<p align="center">
<img src="./docs/img/screemshot_general.jpg" alt="UA-Mask LuCI 界面截图" width="700"></a>

  <h3 align="center">UA-Mask</h3>

  <p align="center">
    一个为 OpenWrt 设计的高性能、低内存 User-Agent 修改工具。
    <br />
    主要用于解决校园网环境对多设备共享上网的检测问题。
    <br />
    <br />
    <a href="https://github.com/Zesuy/UA-Mask/blob/main/docs/tutorial.md"><strong>查看完整教程 »</strong></a>
    ·
    <a href="https://github.com/Zesuy/UA-Mask/issues">报告Bug</a>
    ·
  </p>
</p>

## 关于 UA-Mask

`UA-Mask` (原 `UA3F-tproxy`) 是一个为 OpenWrt 设计的精简、高性能、一站式的 User-Agent 修改工具。我们只专注做一件事：以极致的性能实现 UA Masking。

我们的优化目标是：
*   **硬路由 (受限设备)**: 在 MIPS 等设备上稳定运行，优化热路径性能，消峰填谷保证稳定体验。
*   **软路由 (高性能设备)**: 在 ARM/x86 等设备上实现高效、高吞吐。

> [!IMPORTANT]
> **`v0.4.2` 版本现已完成流量卸载！**
>
> 通过智能统计分析，UA-Mask 能将非 HTTP 流量（如 P2P、游戏、视频流等）动态卸载给内核处理，在不影响 UA 伪装效果的前提下，**极大降低 CPU 负载，实现接近“零感知”的性能体验**。

## 流量卸载

`流量卸载`是 UA-Mask 的核心性能优化功能。它通过智能分析，将纯粹的非 HTTP 流量（如 P2P、WebSocket、QUIC、加密DNS等）从处理流程中剥离，直接交由系统内核转发。

- **显著降低负载**：对于P2P、Steam、加密代理等重型流量，开启此功能可降低 80% 以上的 CPU 负载。
- **智能与安全**：我们通过统计分析确保只有纯粹的非 HTTP 连接被卸载，避免了 UA 泄露的风险。
- **白名单配合**：配合 `UA 关键词白名单` 使用效果更佳。例如，将 `Steam` 加入白名单，其 HTTP 下载流量在通过 UA 检测后即可被卸载，从而兼顾伪装效果与性能。


## 架构
`UA-Mask` 极大地优化了流量处理路径，无需依赖 OpenClash 即可独立工作，也能与之完美配合实现分流。
- 对steam，P2P等重流量，我们可以直接用流量卸载绕过，不再经过UA-Mask，只需要处理一些小流量的api请求。
- 和openclash完美分流无需冗杂配置，只需要打开openclash的`代理本机`和`绕过大陆`
![structure-all](./docs/img/structure-all.png)

## ✨ 特性

*   **一键启用**: 自动配置 `nftables` 或 `iptables` 防火墙，开箱即用。
*   **高性能 & 低GC**: 采用 Redirect 架构，开销极低；使用 bufio pool 和 worker pool，GC 极低。
*   **高效 UA 缓存**: 90% 以上请求命中 LRU 缓存，极大减少重复匹配开销。
*   **流量卸载**: 支持使用 `ipset`/`nfset` 动态绕过非 HTTP 流量及白名单目标，极大提升性能。
*   **多种匹配模式**: 支持关键词、正则表达式，全部覆盖。
*   **零泄露**: 正确处理 HTTP、非 HTTP 及混合流量中每个请求的 UA。

## 安装

### 使用预编译包

1.  前往 [Releases 页面](https://github.com/Zesuy/UA-Mask/releases)。

2.  根据路由器架构下载对应的 `.ipk` 包：

3. 安装：
    ```bash
    # 根据实际名称安装即可
    opkg update
    opkg install UAmask_*.ipk
    # 对于iptables用户，若需要使用ipset功能，请安装ipset
    opkg install ipset
    ```

###  源码编译

1.  将本项目 `clone` 到您的 OpenWrt 编译环境的 `package/luci` 目录下。
2.  推荐在编译前 `make download` 和 `make j8`，完成一次固件编译。
3.  完成后再编译本软件包：

    ```bash
    make clean
    make package/UA-Mask/compile
    ```
    编译完成后将在 `$(rootdir)/bin/packages/$(targetdir)/base/` 中生成 `UAmask_xxx.ipk` 和 `UAmask-ipt_xxx.ipk`

    如果需要打包进固件，请在 `network/Web Servers/Proxies/UAmask`选择一个`*`。

## 使用方法

安装后，你只需要：

1.  在 LuCI 界面中找到 "服务" -\> "UA-Mask"。
2.  勾选 "启用"。
3.  点击 "保存并应用"。

插件会自动为你配置好所有防火墙转发规则。你也可以在界面中自定义各项高级设置，例如运行模式、匹配规则、绕过端口等。更详细的设置请见 [完整教程](https://github.com/Zesuy/UA-Mask/blob/main/docs/tutorial.md)


<!-- MARKDOWN LINKS & IMAGES -->
[release-shield]: https://img.shields.io/github/v/release/Zesuy/UA-Mask?style=flat
[release-url]: https://github.com/Zesuy/UA-Mask/releases
[license-shield]: https://img.shields.io/github/license/Zesuy/UA-Mask.svg?style=flat
[license-url]: https://github.com/Zesuy/UA-Mask/blob/main/LICENSE
