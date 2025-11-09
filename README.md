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
    <a href="https://github.com/Zesuy/UA-Mask/discussions">Discussions</a>
  </p>
</p>

## 关于 UA-Mask

`UA-Mask` 是一个为 OpenWrt 设计的精简、高性能、一站式的 User-Agent 修改工具。我们只专注做一件事：以极致的性能实现 UA Masking。

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

## Q&A

项目与 UA3F 的关系？
- 本项目最初源于 [UA3F](https://github.com/SunBK201/UA3F) 的代码，并在此基础上进行了较大幅度的架构与实现调整。我们尊重并感谢 UA3F 的工作，项目沿用原许可证并在文档中保留致谢。
- 之所以单独维护，是因为在长期使用与压测中，我们发现并优先解决了若干影响“UA 不泄露”与“在受限设备上稳定运行”的问题，这些改动涉及架构选择与整体策略，分支独立更有利于快速演进与稳定发布。

为什么不是“稍作修改”的派生？
- 我们的目标是“以极致的稳定性避免 UA 泄露”，因此围绕 HTTP keep-alive、多协议混部场景、以及高并发与低 GC 做了系统性改造。核心差异包括：
  - 连接与请求处理路径：引入 Redirect 架构，重写热路径，避免不必要的用户态往返与冗余拷贝。
  - UA 安全性：
    - 修复 keep-alive 场景下的 UA 泄露问题（逐请求级别处理，而非仅处理首包/首个请求）。
    - 移除了基于“IP 地址”的缓存策略，改为“UA 结果”的缓存与 LRU，避免同一 IP 多协议/多 UA 场景导致误判。
  - 性能与稳定性：加入 bufio pool、worker pool、低分配实现与命中率优化，显著降低 GC 压力。
  - 流量卸载（ipset/nfset）：通过统计分析，自动将“非 HTTP/无需处理”的连接下放到内核转发，显著降低 CPU 负载。
  - 运维体验：自动生成 nftables/iptables 规则，与 OpenClash 无缝分流，无需复杂规则编排。


为什么要强调“UA 不泄露”的设计？
- 校园网、运营商侧 UA 识别通常发生在 L7/HTTP 层面。要真正避免“混合协议/长连接/多路复用”场景下的 UA 泄露，需要逐请求的解析、匹配与缓存策略，并确保在非 HTTP/无需处理的连接上尽量旁路，以释放性能。

硬路由能用吗？性能如何？
- 在 mt7920 等受限设备上，核心应用的转发吞吐（iperf3）可达到上行约 97 Mbps、下行约 78 Mbps，HTTP hey 压测接近；启用“流量卸载”后，对 P2P/Steam/加密代理等重型流量，CPU 负载可显著下降。实际体验取决于设备与流量结构，建议配合 UA 关键词白名单以最大化卸载效果。

是否会进行更底层（内核态）的实现？
- 目前阶段，我们优先保证“零泄露”与“可运维”的平衡，采用 L7 分析 + L4 绕过的策略。更底层的实现需要权衡复杂度、兼容性与维护成本，后续将根据需求与社区反馈评估。

后续计划？
- 暂无明确大版本规划，欢迎在 Discussion 中提交需求或在 Issue 中反馈问题；对事实性更正与对比测试结果亦欢迎指正。我们会持续跟踪上游项目动态，并保持良性技术交流。

致谢
- 感谢 UA3F 及相关社区对该方向的探索与贡献；本项目遵循开源许可并在文档中保留来源说明。若有归属标注不全之处，欢迎提交 PR 共同完善。


## 致谢与来源

- 本项目最初基于上游项目 UA3F 的实现开展开发，在遵循其开源许可证的前提下进行了大量架构与行为层面的改造；在此致谢 UA3F 及其贡献者。
- 上游项目
  - 仓库：<https://github.com/SunBK201/UA3F>
  - 许可证：<https://github.com/SunBK201/UA3F/blob/master/LICENSE>
- 本项目在连接/请求处理路径、缓存策略、GC 优化与“流量卸载（ipset/nfset）”等方面进行了系统性重构，形成独立项目；具体差异见上文 Q&A。
- 若本仓库中存在归属标注不全之处，欢迎通过 Issue 或 PR 指正，我们会及时修正。

<!-- MARKDOWN LINKS & IMAGES -->
[release-shield]: https://img.shields.io/github/v/release/Zesuy/UA-Mask?style=flat
[release-url]: https://github.com/Zesuy/UA-Mask/releases
[license-shield]: https://img.shields.io/github/license/Zesuy/UA-Mask.svg?style=flat
[license-url]: https://github.com/Zesuy/UA-Mask/blob/main/LICENSE
