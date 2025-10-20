module("luci.controller.ua3f-tproxy", package.seeall)

function index()
    entry({"admin", "services", "ua3f-tproxy"}, cbi("ua3f-tproxy"), "UA3F TPROXY", 1)
end