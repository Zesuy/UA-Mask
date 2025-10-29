module("luci.controller.UAmask", package.seeall)

function index()
    entry({"admin", "services", "UAmask"}, cbi("UAmask"), "UA MASK", 1)
end