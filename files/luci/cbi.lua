local uci = require("luci.model.uci").cursor()

ua3f_tproxy = Map("ua3f-tproxy",
    "UA3F TPROXY",
    [[
        <a href="https://github.com/Zesuy/UA3F-tproxy" target="_blank">Version: 0.1.0</a>
        <br>
        Transparent proxy for modifying User-Agent.
    ]]
)

enable = ua3f_tproxy:section(NamedSection, "enabled", "ua3f-tproxy", "Status")
main = ua3f_tproxy:section(NamedSection, "main", "ua3f-tproxy", "Settings")

enable:option(Flag, "enabled", "Enabled")
status = enable:option(DummyValue, "status", "Status")
status.rawhtml = true
status.cfgvalue = function(self, section)
    local pid = luci.sys.exec("pidof ua3f-tproxy")
    if pid == "" then
        return "<span style='color:red'>" .. "Stopped" .. "</span>"
    else
        return "<span style='color:green'>" .. "Running" .. "</span>"
    end
end

main:tab("general", "General Settings")
main:tab("log", "Log")

port = main:taboption("general", Value, "port", "Port")
port.placeholder = "8080"
port.datatype = "port"

log_level = main:taboption("general", ListValue, "log_level", "Log Level")
log_level:value("debug")
log_level:value("info")
log_level:value("warn")
log_level:value("error")
log_level:value("fatal")
log_level:value("panic")

ua = main:taboption("general", Value, "ua", "User-Agent")
ua.placeholder = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

iface = main:taboption("general", Value, "iface", "Listen Interface(s)")
iface.placeholder = "br-lan"
iface.description = "指定一个或多个 LAN 接口，用空格分隔 (例如: 'br-lan' 或 'br-lan eth1')"

bypass_gid = main:taboption("general", Value, "bypass_gid", "Bypass GID")
bypass_gid.placeholder = "65534"
bypass_gid.datatype = "uinteger"
bypass_gid.description = "用于绕过TPROXY自身流量的GID。必须与 nft 规则中的 GID 匹配。"

bypass_ports = main:taboption("general", Value, "bypass_ports", "Bypass Destination Ports")
bypass_ports.placeholder = "22 443"
bypass_ports.description = "豁免的目标端口，用空格分隔 (例如: '22 443')。"

bypass_ips = main:taboption("general", Value, "bypass_ips", "Bypass Destination IPs")
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
        -- 修正：使用 luci.sys.call 异步执行，并重定向输出
        -- 末尾的 '&' 符号是关键，它让命令在后台运行
        luci.sys.call("/etc/init.d/ua3f-tproxy restart >/dev/null 2>&1 &")
    else
        -- 修正：使用 luci.sys.call 异步执行
        luci.sys.call("/etc/init.d/ua3f-tproxy stop >/dev/null 2>&1 &")
    end
end

return ua3f_tproxy