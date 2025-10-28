local uci = require("luci.model.uci").cursor()
local nixio = require("nixio")
local luci_sys = require("luci.sys")

local stats_cache = nil
local stats_file = "/tmp/ua3f-tproxy.stats"

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

ua3f_tproxy = Map("ua3f-tproxy",
    "UA3F-TPROXY",
    [[
        <a href="https://github.com/Zesuy/UA3F-tproxy" target="_blank">版本: 0.2.3</a>
        <br>
        用于修改 User-Agent 的透明代理,使用 TPROXY 技术实现。
        <br>
    ]]
)

enable = ua3f_tproxy:section(NamedSection, "enabled", "ua3f-tproxy", "状态")
main = ua3f_tproxy:section(NamedSection, "main", "ua3f-tproxy", "设置")

enable:option(Flag, "enabled", "启用")
status = enable:option(DummyValue, "status", "运行状态")
status.rawhtml = true
status.cfgvalue = function(self, section)
    local pid = luci_sys.exec("pidof ua3f-tproxy")
    if pid == "" then
    return "<span style='color:red'>" .. "未运行" .. "</span>"
    else
    return "<span style='color:green'>" .. "运行中" .. "</span>"
    end
end
stats_display = enable:option(DummyValue, "stats_display", "运行统计")
stats_display.rawhtml = true
stats_display.cfgvalue = function(self, section)
    local pid = luci_sys.exec("pidof ua3f-tproxy")
    if pid == "" then
        return "<em>(服务未运行时不统计)</em>"
    end
    
    local stats = get_stats()
    
    local active = stats["active_connections"] or "0"
    local http = stats["http_requests"] or "0"
    local regex = stats["regex_hits"] or "0"
    local modified = stats["modifications_done"] or "0"

    -- 格式化为单行
    return string.format(
        "<b>当前连接:</b> %s | <b>HTTP请求:</b> %s | <b>正则匹配:</b> %s | <b>成功修改:</b> %s",
        active, http, regex, modified
    )
end

main:tab("general", "常规设置")
main:tab("network", "网络与防火墙")
main:tab("softlog", "应用日志")

-- === Tab 1: 常规设置 (UA 相关) ===
port = main:taboption("general", Value, "port", "监听端口")
port.placeholder = "12032"
port.datatype = "port"

log_level = main:taboption("general", ListValue, "log_level", "日志等级")
log_level:value("debug", "调试(debug)")
log_level:value("info", "信息(info)")
log_level:value("warn", "警告(warn)")
log_level:value("error", "错误(error)")
log_level:value("fatal", "致命(fatal)")
log_level:value("panic", "崩溃(panic)")

log_file = main:taboption("general", Value, "log_file", "应用日志路径")
log_file.placeholder = "/tmp/ua3f-tproxy/ua3f-tproxy.log"
log_file.description = "指定 Go 程序运行时日志的输出文件路径。留空将禁用文件日志。"
log_file.default = "/tmp/ua3f-tproxy/ua3f-tproxy.log"

ua = main:taboption("general", Value, "ua", "User-Agent 标识")
ua.placeholder = "FFF"
ua.description = "用于替换设备标识的 User-Agent 字符串，当部分替换启用时，用当前值替换匹配到的部分。"

ua_mode = main:taboption("general", ListValue, "ua_mode", "UA 修改模式")
ua_mode:value("smart_partial", "正则替换(部分)")
ua_mode:value("smart_full", "正则替换(全量)")
ua_mode:value("force_full", "全局替换")
ua_mode.default = "smart_full" 
ua_mode.description = "选择 User-Agent 的替换模式：<br />" ..
                      "<b>正则替换(部分):</b> 仅当UA匹配正则时，才替换UA中的匹配部分。<br />" ..
                      "<b>正则替换(全量):</b> 仅当UA匹配正则时，才将整个UA替换为新值。<br />" ..
                      "<b>全局替换:</b> 忽略正则，强制将所有流量的UA替换为新值。"

uaRegexPattern = main:taboption("general", Value, "ua_regex", "UA匹配正则")
uaRegexPattern.placeholder = "(iPhone|iPad|Android|Macintosh|Windows|Linux|Apple|Mac OS X|Mobile)"
uaRegexPattern.description = "当不使用强制替换时，用于匹配 User-Agent 的正则表达式"


-- === Tab 2: 网络与防火墙 (网络、日志等级、防火墙相关) ===



iface = main:taboption("network", Value, "iface", "监听接口")
iface.placeholder = "br-lan"
iface.description = "指定监听的lan口"

bypass_gid = main:taboption("network", Value, "bypass_gid", "绕过 GID")
bypass_gid.placeholder = "65533"
bypass_gid.datatype = "uinteger"
bypass_gid.description = "用于绕过 TPROXY 自身流量的 GID。"

proxy_host = main:taboption("network", Flag, "proxy_host", "代理主机流量")
proxy_host.description = "启用后将代理主机自身的流量。如果需要尽量避免和其他代理冲突，请禁用此选项。"

bypass_ports = main:taboption("network", Value, "bypass_ports", "绕过目标端口")
bypass_ports.placeholder = "22 443"
bypass_ports.description = "豁免的目标端口，用空格分隔 (如: '22 443')。"

bypass_ips = main:taboption("network", Value, "bypass_ips", "绕过目标 IP")
bypass_ips.placeholder = "172.16.0.0/12 192.168.0.0/16 127.0.0.0/8 169.254.0.0/16"
bypass_ips.description = "豁免的目标 IP/CIDR 列表，用空格分隔。"



softlog = main:taboption("softlog", TextValue, "")
softlog.readonly = true
softlog.rows = 30
softlog.cfgvalue = function(self, section)
    local log_file_path = self.map:get("main", "log_file")
    if not log_file_path or log_file_path == "" then
        return "(未配置应用日志文件路径)"
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

-- === Apply/Restart 逻辑 (保持不变) ===

local apply = luci.http.formvalue("cbi.apply")
if apply then
    local enabled_form_value = luci.http.formvalue("cbid.ua3f-tproxy.enabled.enabled")
    
    local pid = luci_sys.exec("pidof ua3f-tproxy")
    local is_running = (pid ~= "" and pid ~= nil)

    if enabled_form_value == "1" then
        if is_running then
            luci.sys.call("/etc/init.d/ua3f-tproxy reload >/dev/null 2>&1")
        else
            luci.sys.call("/etc/init.d/ua3f-tproxy start >/dev/null 2>&1")
        end
    else
        luci.sys.call("/etc/init.d/ua3f-tproxy stop >/dev/null 2>&1")
    end
end

return ua3f_tproxy