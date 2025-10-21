local uci = require("luci.model.uci").cursor()

ua3f_tproxy = Map("ua3f-tproxy",
    "UA3F 透明代理",
    [[
        <a href="https://github.com/Zesuy/UA3F-tproxy" target="_blank">版本: 0.1.1</a>
        <br>
        用于修改 User-Agent 的透明代理。
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
main:tab("log", "日志")

port = main:taboption("general", Value, "port", "端口")
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
ua.placeholder = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

iface = main:taboption("general", Value, "iface", "监听接口")
iface.placeholder = "br-lan"
iface.description = "指定一个或多个 LAN 接口，用空格分隔 (如: 'br-lan' 或 'br-lan eth1'),如果出问题会导致环路卡死！"

bypass_gid = main:taboption("general", Value, "bypass_gid", "绕过 GID")
bypass_gid.placeholder = "65534"
bypass_gid.datatype = "uinteger"
bypass_gid.description = "用于绕过 TPROXY 自身流量的 GID。如果不知道是什么请保持默认"

bypass_ports = main:taboption("general", Value, "bypass_ports", "绕过目标端口")
bypass_ports.placeholder = "22 443"
bypass_ports.description = "豁免的目标端口，用空格分隔 (如: '22 443')。"

bypass_ips = main:taboption("general", Value, "bypass_ips", "绕过目标 IP")
bypass_ips.placeholder = "10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 127.0.0.0/8 169.254.0.0/16"
bypass_ips.description = "豁免的目标 IP/CIDR 列表，用空格分隔。"

log = main:taboption("log", TextValue, "")
log.readonly = true
log.cfgvalue = function(self, section)
    -- 从 logread 读取日志，因为 init 脚本重定向了 stdout/stderr
    return luci.sys.exec("logread -e ua3f-tproxy")
end
log.rows = 30

local apply = luci.http.formvalue("cbi.apply")
if apply then
    local enabled_form_value = luci.http.formvalue("cbid.ua3f-tproxy.enabled.enabled")

    if enabled_form_value == "1" then
        -- 使用 luci.sys.call 异步执行，并重定向输出
        luci.sys.call("/etc/init.d/ua3f-tproxy restart >/dev/null 2>&1 &")
    else
        luci.sys.call("/etc/init.d/ua3f-tproxy stop >/dev/null 2>&1 &")
    end
end

return ua3f_tproxy