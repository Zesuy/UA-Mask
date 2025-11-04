local uci = require("luci.model.uci").cursor()
local nixio = require("nixio")
local luci_sys = require("luci.sys")

local stats_cache = nil
local stats_file = "/tmp/UAmask.stats"

local function get_stats()
    if stats_cache then
        return stats_cache
    end

    local f = io.open(stats_file, "r")
    if not f then
        return {}
    end

    stats_cache = {}
    for line in f:lines() do
        local key, val = line:match("([^:]+):(.*)")
        if key and val then
            stats_cache[key] = val
        end
    end
    f:close()
    return stats_cache
end

-- 辅助函数，用于从缓存中获取特定值
local function get_stat_value(key)
    local stats = get_stats()
    return stats[key] or "0"
end

UAmask = Map("UAmask",
    "UA-MASK",
    [[
    <style>
    .cbi-value-field > br:has(+ .cbi-value-description) {
        display: none !important;
    }
    </style>
        <a href="https://github.com/Zesuy/UA-Mask" target="_blank">版本：0.4.0</a>
        <br>
        用于修改 User-Agent 的透明代理，使用 REDIRECT 技术实现。
        <br>
    ]]
)

enable = UAmask:section(NamedSection, "enabled", "UAmask", "状态")
main = UAmask:section(NamedSection, "main", "UAmask", "设置")

enable:option(Flag, "enabled", "启用")
status = enable:option(DummyValue, "status", "运行状态")
status.rawhtml = true
status.cfgvalue = function(self, section)
    local pid = luci_sys.exec("pidof UAmask")
    if pid == "" then
        return "<span style='color:red'>" .. "未运行" .. "</span>"
    else
        return "<span style='color:green'>" .. "运行中" .. "</span>"
    end
end
stats_display = enable:option(DummyValue, "stats_display", "运行统计")
stats_display.rawhtml = true
stats_display.cfgvalue = function(self, section)
    local pid = luci_sys.exec("pidof UAmask")
    if pid == "" then
        return "<em>(服务未运行时不统计)</em>"
    end
    
    local stats = get_stats()
    
    -- 第一行：负载与性能
    local connections = stats["current_connections"] or "0"
    local total_reqs  = stats["total_requests"] or "0"
    local rps         = stats["rps"] or "0.00"

    -- 第二行：流量分类
    local modified    = stats["successful_modifications"] or "0"
    local passthrough = stats["direct_passthrough"] or "0"
    local rule_proc   = stats["rule_processing"] or "0"
    
    -- 第三行：缓存效率
    local cache_mod   = stats["cache_hit_modify"] or "0"
    local cache_pass  = stats["cache_hit_pass"] or "0"
    local cache_ratio = stats["total_cache_ratio"] or "0.00"

    return string.format(
        "<b>当前连接:</b> %s | <b>请求总数:</b> %s | <b>处理速率:</b> %s RPS<br>" ..
        "<b>成功修改:</b> %s | <b>直接放行:</b> %s | <b>规则处理:</b> %s<br>" ..
        "<b>缓存(修改):</b> %s | <b>缓存(放行):</b> %s | <b>总缓存率:</b> %s%%",
        connections, total_reqs, rps,
        modified, passthrough, rule_proc,
        cache_mod, cache_pass, cache_ratio
    )
end

main:tab("general", "常规设置")
main:tab("network", "网络与防火墙")
main:tab("softlog", "应用日志")

-- === Tab 1: 常规设置（UA 相关）===
-- 运行模式
operating_profile = main:taboption("general", ListValue, "operating_profile", "性能预设",
    "选择性能预设。<br>" ..
    "<b>Low：</b> 适合 128MB 路由器，支持并发 200 连接<br>"..
    "<b>Medium：</b> 适合 256MB-512MB 路由器，支持并发 500 连接<br>"..
    "<b>High：</b> 适合软路由或 1GB 以上路由器，支持并发 1000 连接<br>".. 
    "<b>注意：</b> 超过限制的连接将等待，这可以用来防止突发的连接压垮路由器<br>"..
    "我们尽可能避免内存 GC，确保高性能与降低峰值，缺点是内存占用会稍高。"
)
operating_profile:value("Low", "低(Low)")
operating_profile:value("Medium", "中(Medium)")
operating_profile:value("High", "高(High)")
operating_profile:value("custom", "自定义")
operating_profile.default = "Medium"

buffer_size = main:taboption("general", Value, "buffer_size", "I/O 缓冲区大小（字节）")
buffer_size:depends("operating_profile", "custom")
buffer_size.datatype = "uinteger"
buffer_size.default = "8192"
buffer_size.description = "每个连接使用的缓冲区大小，单位为字节。较大的缓冲区有助于提升吞吐性能。"

pool_size = main:taboption("general", Value, "pool_size", "工作协程池大小")
pool_size:depends("operating_profile", "custom")
pool_size.datatype = "uinteger"
pool_size.default = "0"
pool_size.description = "工作协程池的大小。设为 0 则每个连接创建协程，使用协程池会限制最大并发，但能减少 GC。<br>最大 RAM 估计：pool_size*(2*buffer_size) + cache_ram。<br>若 pool_size 设为 0，则最大 RAM 估计：连接数*(2*buffer_size) + cache_ram。"

cache_size = main:taboption("general", Value, "cache_size", "LRU 缓存大小")
cache_size:depends("operating_profile", "custom")
cache_size.datatype = "uinteger"
cache_size.default = "1000"
cache_size.description = "LRU 缓存大小。缓存更大命中率更高，预估每 1000 条占用约 300KB 内存。"

gogc_value=main:taboption("general", Value, "gogc_value", "Go 垃圾回收参数（GOGC）")
gogc_value:depends("operating_profile", "custom")
gogc_value.datatype = "uinteger"
gogc_value.default = "100"
gogc_value.description = "Go 语言的垃圾回收参数（默认 100）。<b>注意：当使用协程池，GC 压力已经很低，强烈建议保持 100 默认值。</b>"

ua = main:taboption("general", Value, "ua", "User-Agent 标识")
ua.default = "FFF"
ua.description = "用于替换的 User-Agent 字符串。"

-- 重构：匹配规则
match_mode = main:taboption("general", ListValue, "match_mode", "匹配规则",
    "定义如何确定哪些流量需要被修改。")
match_mode:value("keywords", "基于关键词（最快，推荐）")
match_mode:value("regex", "基于正则表达式（灵活）")
match_mode:value("all", "修改所有流量（强制）")
match_mode.default = "keywords"

-- 仅在 keywords 模式下显示
keywords = main:taboption("general", Value, "keywords", "关键词列表")
keywords:depends("match_mode", "keywords")
keywords.default = "Windows,Linux,Android,iPhone,Macintosh,iPad,OpenHarmony"
keywords.description = "当 UA 包含列表中的任意关键词时，替换整个 UA 为目标值。用逗号分隔。"

-- 仅在 regex 模式下显示
ua_regex = main:taboption("general", Value, "ua_regex", "正则表达式")
ua_regex:depends("match_mode", "regex")
ua_regex.default = "(iPhone|iPad|Android|Macintosh|Windows|Linux)"
ua_regex.description = "用于匹配 User-Agent 的正则表达式。"

-- 仅在 regex 模式下显示
replace_method = main:taboption("general", ListValue, "replace_method", "替换方式")
replace_method:depends("match_mode", "regex")
replace_method:value("full", "完整替换")
replace_method:value("partial", "部分替换（仅替换匹配内容）")
replace_method.default = "full"
replace_method.description = "<b>完整替换：</b> 将整个 UA 替换为新值。<br><b>部分替换：</b> 仅将 UA 中被正则匹配到的部分替换为新值。"

whitelist = main:taboption("general", Value, "whitelist", "User-Agent 白名单")
whitelist.placeholder = ""
whitelist.description = "指定不进行替换的 User-Agent，用逗号分隔（如：MicroMessenger Client,ByteDancePcdn）。"

-- === Tab 2: 网络与防火墙（网络、日志等级、防火墙相关）===

port = main:taboption("network", Value, "port", "监听端口")
port.default = "12032"
port.datatype = "port"

iface = main:taboption("network", Value, "iface", "监听接口")
iface.default = "br-lan"
iface.description = "指定监听的 LAN 口。"

enable_firewall_set = main:taboption("network", Flag, "enable_firewall_set", "启用防火墙加速（实验性）")
enable_firewall_set.default = 0
enable_firewall_set.description = "<b>[实验性功能]</b> 启用后，将创建一个 ipset（fw3）或 nfset（fw4），用于动态绕过特定目标 IP 和端口的组合，不会再进入 UAmask 处理，这将大幅提升性能，但可能造成部分 UA 未被修改。<br>如果您使用 iptables，请确保安装 ipset 软件包。"

Firewall_ua_whitelist= main:taboption("network", Value, "Firewall_ua_whitelist", "UA 关键词白名单")
Firewall_ua_whitelist:depends("enable_firewall_set", "1")
Firewall_ua_whitelist.placeholder = ""
Firewall_ua_whitelist.description = "指定不通过 UAmask 代理的 UA 关键词（防火墙级别绕过），用逗号分隔（如：Valve/Steam,360pcdn）。"

Firewall_ua_bypass=main:taboption("network", Flag, "Firewall_ua_bypass", "绕过非http流量")
Firewall_ua_bypass:depends("enable_firewall_set", "1")
Firewall_ua_bypass.description = "启用后，将绕过使用非 HTTP 流量的 IP 和端口，10 分钟内不再通过 UAmask 代理。"

Firewall_drop_on_match=main:taboption("network", Flag, "Firewall_drop_on_match", "匹配时断开连接")
Firewall_drop_on_match:depends("enable_firewall_set", "1")
Firewall_drop_on_match.description = "启用后，当流量匹配非http和 UA 白名单规则时，将直接断开连接，强制其重新建立连接绕过 UAmask。"

proxy_host = main:taboption("network", Flag, "proxy_host", "代理主机流量")
proxy_host.description = "启用后将代理主机自身的流量。如果需要尽量避免和其他代理冲突，请禁用此选项。"

bypass_gid = main:taboption("network", Value, "bypass_gid", "绕过 GID")
bypass_gid:depends("proxy_host", "1")
bypass_gid.default = "65533"
bypass_gid.datatype = "uinteger"
bypass_gid.description = "用于绕过 TPROXY 自身流量的 GID。"

bypass_ports = main:taboption("network", Value, "bypass_ports", "绕过目标端口")
bypass_ports.placeholder = "22 443"
bypass_ports.description = "豁免的目标端口，用空格分隔（如：22 443）。"

bypass_ips = main:taboption("network", Value, "bypass_ips", "绕过目标 IP")
bypass_ips.default = "172.16.0.0/12 192.168.0.0/16 127.0.0.0/8 169.254.0.0/16"
bypass_ips.description = "豁免的目标 IP/CIDR 列表，用空格分隔。"





log_level = main:taboption("softlog", ListValue, "log_level", "日志等级")
log_level.default = "info"
log_level:value("debug", "调试（debug）")
log_level:value("info", "信息（info）")
log_level:value("warn", "警告（warn）")
log_level:value("error", "错误（error）")
log_level:value("fatal", "致命（fatal）")
log_level:value("panic", "崩溃（panic）")

log_file = main:taboption("softlog", Value, "log_file", "应用日志路径")
log_file.placeholder = "/tmp/UAmask/UAmask.log"
log_file.description = "指定 Go 程序运行时日志的输出文件路径。留空将禁用文件日志。"

softlog = main:taboption("softlog", TextValue, "log_display","")
softlog.readonly = true
softlog.rows = 30
softlog.cfgvalue = function(self, section)
    local log_file_path = self.map:get("main", "log_file")
    if not log_file_path or log_file_path == "" then
        return "（未配置应用日志文件路径）"
    end
    return luci.sys.exec("tail -n 200 \"" .. log_file_path .. "\" 2>/dev/null")
end

local clear_btn = main:taboption("softlog", Button, "clear_log", "清空应用日志")
clear_btn.inputstyle = "reset"
clear_btn.write = function(self, section)
    local log_file_path = self.map:get(section, "log_file")
    if log_file_path and log_file_path ~= "" and nixio.fs.access(log_file_path) then
        luci.sys.exec("> \"" .. log_file_path .. "\"")
    end
end

-- === Apply/Restart 逻辑（保持不变）===

local apply = luci.http.formvalue("cbi.apply")
if apply then
    local enabled_form_value = luci.http.formvalue("cbid.UAmask.enabled.enabled")
    
    local pid = luci_sys.exec("pidof UAmask")
    local is_running = (pid ~= "" and pid ~= nil)

    if enabled_form_value == "1" then
        if is_running then
            luci.sys.call("/etc/init.d/UAmask reload >/dev/null 2>&1")
        else
            luci.sys.call("/etc/init.d/UAmask start >/dev/null 2>&1")
        end
    else
        luci.sys.call("/etc/init.d/UAmask stop >/dev/null 2>&1")
    end
end

return UAmask