package firewall

type BypassTarget struct {
	IP      string
	Port    int
	SetName string
	Backend string
	Timeout int
}
