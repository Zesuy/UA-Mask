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

log = main:taboption("log", TextValue, "")
log.readonly = true
log.cfgvalue = function(self, section)
    -- 从 logread 读取日志，因为 init 脚本重定向了 stdout/stderr
    return luci.sys.exec("logread -e ua3f-tproxy")
end
log.rows = 30

local apply = luci.http.formvalue("cbi.apply")
if apply then
    io.popen("/etc/init.d/ua3f-tproxy restart")
end

return ua3f_tproxy