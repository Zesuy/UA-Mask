local uci = require("luci.model.uci").cursor()

ua3f_tproxy = Map("ua3f-tproxy",
    "UA3F-TPROXY",
    [[
        <a href="https://github.com/Zesuy/UA3F-tproxy" target="_blank">版本: 0.1.5</a>
        <br>
        用于修改 User-Agent 的透明代理,使用 TPROXY 技术实现。
        <br>
        请谨慎与其他 TPROXY 服务同时使用，可能会导致冲突和环路。
        <br>
        在默认情况下，不会修改全部的UA，而是只修改正则匹配到的(包含设备名称的)UA,其余不含设备名的UA(steam,pcdn等)不做处理
    ]]
)

enable = ua3f_tproxy:section(NamedSection, "enabled", "ua3f-tproxy", "状态")
main = ua3f_tproxy:section(NamedSection, "main", "ua3f-tproxy", "设置")

enable:option(Flag, "enabled", "启用")
status = enable:option(DummyValue, "status", "运行状态")
status.rawhtml = true
status.cfgvalue = function(self, section)
    local pid = luci.sys.exec("pidof ua3f-tproxy")
    if pid == "" then
    return "<span style='color:red'>" .. "未运行" .. "</span>"
    else
    return "<span style='color:green'>" .. "运行中" .. "</span>"
    end
end

main:tab("general", "常规设置")
main:tab("network", "网络与防火墙")
main:tab("log", "日志")


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

ua = main:taboption("general", Value, "ua", "User-Agent 标识")
ua.placeholder = "FFF"
ua.description = "用于替换设备标识的 User-Agent 字符串，当部分替换启用时，用当前值替换匹配到的部分。"

force_replace = main:taboption("general", Flag, "force_replace", "强制修改UA")
force_replace.description = "启用后将忽略正则，强制修改所有流量的 User-Agent。如果正常使用依旧掉线，请尝试启用此选项。"

partialRepalce = main:taboption("general", Flag, "partial_replace", "部分替换UA")
partialRepalce.description ="是否仅替换匹配到的正则部分，而非整个 User-Agent 字符串。"
partialRepalce.default = "0"

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


-- === Tab 3: 日志 ===

log = main:taboption("log", TextValue, "")
log.readonly = true
log.cfgvalue = function(self, section)
    -- 从 logread 读取日志，因为 init 脚本重定向了 stdout/stderr
    return luci.sys.exec("logread -e ua3f-tproxy")
end
log.rows = 30


-- === Apply/Restart 逻辑 (保持不变) ===

local apply = luci.http.formvalue("cbi.apply")
if apply then
    local enabled_form_value = luci.http.formvalue("cbid.ua3f-tproxy.enabled.enabled")

    if enabled_form_value == "1" then
        -- 使用同步操作
        luci.sys.call("/etc/init.d/ua3f-tproxy restart >/dev/null 2>&1")
    else
        luci.sys.call("/etc/init.d/ua3f-tproxy stop >/dev/null 2>&1")
    end
end

return ua3f_tproxy