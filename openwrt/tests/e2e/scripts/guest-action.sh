#!/bin/sh
set -eu

ACTION=${1:?guest action is required}
case "$ACTION" in
	legacy-running|running|disabled|starting|failed|enabled-not-running|rejected|recovery|missing-stats|logs-ready|missing-log|missing-artifacts|clear-log-ready|lifecycle-count|effective-config|identity) ;;
	*) printf 'unapproved guest action: %s\n' "$ACTION" >&2; exit 64 ;;
esac

: "${E2E_SSH_PORT:?E2E_SSH_PORT is required}"
: "${E2E_SSH_KEY:?E2E_SSH_KEY is required}"
: "${E2E_RUN_DIR:?E2E_RUN_DIR is required}"

KNOWN_HOSTS=$E2E_RUN_DIR/known_hosts
printf '%s action=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$ACTION" >> "$E2E_RUN_DIR/fixture-actions.log"

ssh -i "$E2E_SSH_KEY" -p "$E2E_SSH_PORT" -o BatchMode=yes -o StrictHostKeyChecking=no \
	-o UserKnownHostsFile="$KNOWN_HOSTS" -o ConnectTimeout=3 root@127.0.0.1 sh -s -- "$ACTION" <<'REMOTE'
set -eu
action=$1

stop_fake() {
	if [ -r /tmp/uamask-e2e-fake.pid ]; then
		fake_pid=$(cat /tmp/uamask-e2e-fake.pid)
		case "$fake_pid" in ''|*[!0-9]*) ;; *) kill "$fake_pid" 2>/dev/null || true ;; esac
		rm -f /tmp/uamask-e2e-fake.pid /tmp/uamask-e2e/UAmask
	fi
}

write_valid() {
	cat > /etc/config/UAmask <<'EOF'
config 'UAmask' 'enabled'
	option enabled '1'

config 'UAmask' 'main'
	option port '12032'
	option ua 'E2E-UA'
	option operating_profile 'Medium'
	option buffer_size '8192'
	option pool_size '0'
	option cache_size '2000'
	option gogc_value '100'
	option match_mode 'keywords'
	list keywords 'Android'
	list keywords 'Legacy Phone'
	option replace_method 'full'
	option log_level 'info'
	list iface 'br-lan'
	option enable_firewall_set '1'
	option Firewall_ua_bypass '1'
	list Firewall_ua_whitelist 'Offload Legacy'
	option Firewall_drop_on_match '1'
	option firewall_advanced_settings '0'
	option firewall_nonhttp_threshold '5'
	option firewall_decision_delay '60'
	option firewall_timeout '28800'
	option bypass_gid '65533'
	list bypass_ports '22'
	list bypass_ports '80'
	list bypass_ports '443'
	list bypass_ips '127.0.0.0/8'
	list bypass_ips '192.168.0.0/16'
	option ua_regex '(iPhone|Android|Windows)'
	option proxy_host '0'
	list whitelist 'ExactUA/1.0'
	list whitelist 'ExactUA/2.0'
	option log_file '/tmp/UAmask/UAmask.log'
EOF
}

write_legacy() {
	write_valid
	uci -q delete UAmask.main.keywords
	uci set UAmask.main.keywords='Android, Legacy Phone'
	uci -q delete UAmask.main.whitelist
	uci set UAmask.main.whitelist='ExactUA/1.0,ExactUA/2.0'
	uci -q delete UAmask.main.Firewall_ua_whitelist
	uci set UAmask.main.Firewall_ua_whitelist='Offload Legacy, Offload Two'
	uci -q delete UAmask.main.iface
	uci set UAmask.main.iface='br-lan eth0'
	uci -q delete UAmask.main.bypass_ports
	uci set UAmask.main.bypass_ports='22 80 443'
	uci -q delete UAmask.main.bypass_ips
	uci set UAmask.main.bypass_ips='127.0.0.0/8 192.168.0.0/16'
	uci commit UAmask
}

start_valid() {
	stop_fake
	write_valid
	uci commit UAmask
	/etc/init.d/UAmask reload >/dev/null 2>&1 || /etc/init.d/UAmask start >/dev/null 2>&1
	mkdir -p /tmp/UAmask
}

case "$action" in
	legacy-running)
		stop_fake
		write_legacy
		/etc/init.d/UAmask reload >/dev/null 2>&1 || /etc/init.d/UAmask start >/dev/null 2>&1
		;;
	running)
		start_valid
		;;
	disabled)
		stop_fake
		write_valid
		uci set UAmask.enabled.enabled='0'; uci commit UAmask
		/etc/init.d/UAmask reload >/dev/null 2>&1 || true
		;;
	starting)
		stop_fake
		/etc/init.d/UAmask stop >/dev/null 2>&1 || true
		write_valid; uci commit UAmask
		for _attempt in $(seq 1 30); do
			pidof UAmask >/dev/null 2>&1 || break
			sleep 1
		done
		ubus call service delete '{"name":"UAmask"}' >/dev/null 2>&1 || true
		ubus call service add '{"name":"UAmask","instances":{"UAmask":{}}}' >/dev/null
		for _attempt in $(seq 1 20); do
			state=$(/usr/libexec/uamask-luci status | jsonfilter -e '@.state')
			[ "$state" = starting ] && break
			sleep 1
		done
		[ "${state:-}" = starting ]
		;;
	failed)
		stop_fake
		/etc/init.d/UAmask stop >/dev/null 2>&1 || true
		write_valid
		uci set UAmask.main.match_mode='regex'; uci set UAmask.main.ua_regex='('; uci commit UAmask
		/etc/init.d/UAmask start >/dev/null 2>&1 || true
		;;
	enabled-not-running)
		stop_fake
		/etc/init.d/UAmask stop >/dev/null 2>&1 || true
		write_valid; uci commit UAmask
		rm -f /var/run/UAmask/last_error
		;;
	rejected)
		start_valid
		uci set UAmask.main.match_mode='regex'; uci set UAmask.main.ua_regex='('; uci commit UAmask
		/etc/init.d/UAmask reload >/dev/null 2>&1 || true
		;;
	recovery)
		start_valid
		;;
	missing-stats)
		start_valid
		/etc/init.d/UAmask stop >/dev/null 2>&1 || true
		mkdir -p /tmp/uamask-e2e
		cp /bin/sleep /tmp/uamask-e2e/UAmask
		/tmp/uamask-e2e/UAmask 300 >/dev/null 2>&1 &
		printf '%s\n' "$!" > /tmp/uamask-e2e-fake.pid
		rm -f /tmp/UAmask.stats
		;;
	logs-ready)
		start_valid
		: > /tmp/UAmask/UAmask.log
		index=1
		while [ "$index" -le 250 ]; do printf 'fixture-line-%03d\n' "$index" >> /tmp/UAmask/UAmask.log; index=$((index + 1)); done
		;;
	missing-log)
		start_valid
		rm -f /tmp/UAmask/UAmask.log
		;;
	missing-artifacts)
		stop_fake
		/etc/init.d/UAmask stop >/dev/null 2>&1 || true
		write_valid; uci commit UAmask
		rm -f /var/run/UAmask/config.json /var/run/UAmask/last_error /tmp/UAmask.stats /tmp/UAmask/UAmask.log
		;;
	clear-log-ready)
		start_valid
		printf 'clear-me\n' > /tmp/UAmask/UAmask.log
		;;
	lifecycle-count)
		logread | grep -Ec '(Starting|Reloading|Stopping) UAmask' || true
		;;
	effective-config)
		/usr/bin/UAmask -dump-effective-config -config /var/run/UAmask/config.json
		;;
	identity)
		pid=$(pidof UAmask | awk '{print $1}')
		config_sha=$(sha256sum /var/run/UAmask/config.json | awk '{print $1}')
		printf '{"pid":"%s","config_sha256":"%s"}\n' "$pid" "$config_sha"
		;;
esac
REMOTE
