#!/usr/bin/env python3
"""Exact-package smoke in an owned disposable OpenWrt guest (stdlib only)."""
import argparse
from contextlib import contextmanager
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import signal
import socket
import subprocess
import sys
import time
import urllib.request
import zlib


PLATFORMS = json.loads(Path(__file__).with_name('platforms.json').read_text())


def digest(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as source:
        for block in iter(lambda: source.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def extract_image(archive, destination):
    # Signed OpenWrt images may append fwtool metadata after the gzip member.
    # The caller verifies the hash of the entire archive before extraction.
    decoder = zlib.decompressobj(16 + zlib.MAX_WBITS)
    with Path(archive).open('rb') as source, Path(destination).open('xb') as out:
        for block in iter(lambda: source.read(64 * 1024), b''):
            out.write(decoder.decompress(block))
            if decoder.eof:
                break
        if not decoder.eof:
            raise ValueError('truncated gzip image')


def validate_metadata(package, metadata, version):
    p = PLATFORMS[version]
    expected = {'platform': 'openwrt', 'architecture': 'amd64',
                'openwrt_version': version, 'target': 'x86', 'subtarget': '64',
                'package_architecture': 'x86_64', 'package_format': p['package_format'],
                'artifact': package.name, 'sha256': digest(package)}
    for key, value in expected.items():
        if metadata.get(key) != value:
            raise ValueError('metadata mismatch: ' + key)
    if package.suffix != '.' + p['package_format']:
        raise ValueError('package extension mismatch')
    if not re.fullmatch(r'[0-9a-fA-F]{40}', str(metadata.get('commit', ''))):
        raise ValueError('metadata commit must be a 40-character SHA')
    for field in ('version', 'package_version'):
        if not re.fullmatch(r'[0-9A-Za-z._+~-]+', str(metadata.get(field, ''))):
            raise ValueError('invalid metadata ' + field)
    return expected['sha256']


def parser():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--openwrt', required=True, choices=sorted(PLATFORMS))
    for name in ('package', 'metadata', 'run-dir', 'cache-dir'):
        p.add_argument('--' + name, required=True, type=Path)
    p.add_argument('--image-file', type=Path)
    p.add_argument('--fail-after', choices=['boot'])
    return p


class Runner:
    def __init__(self, args):
        self.args = args
        self.root = args.run_dir.absolute()
        self.root.mkdir(parents=True, exist_ok=False)
        self.private = self.root / 'private'
        self.overlay = self.root / 'router.qcow2'
        self.raw = self.root / 'base.raw'
        self.process = None
        self.connected = False
        self.stage_name = 'preflight'
        self.result = dict(schema_version=1, status='ERROR', source_commit=None,
                           package_sha256=None, openwrt=args.openwrt,
                           failed_stage=None, error=None, cleanup={})
        for name in ('events.jsonl', 'qemu-serial.log', 'qemu-stderr.log',
                     'guest-identity.txt', 'package-install.txt', 'package-status.txt',
                     'service-checks.txt', 'guest-diagnostics.txt'):
            (self.root / name).touch()

    def event(self, stage, action, duration=0, **details):
        record = dict(stage=stage, event=action,
                      utc=datetime.now(timezone.utc).isoformat(),
                      duration_seconds=duration, **details)
        with (self.root / 'events.jsonl').open('a') as out:
            out.write(json.dumps(record) + '\n')

    @contextmanager
    def stage(self, name):
        self.stage_name = name
        start = time.monotonic()
        self.event(name, 'start')
        try:
            yield
        except BaseException as exc:
            self.event(name, 'fail', time.monotonic() - start, error=str(exc))
            raise
        else:
            self.event(name, 'end', time.monotonic() - start, result='PASS')

    def command(self, argv, output, timeout=60, data=None, check=True):
        with (self.root / output).open('ab') as out:
            out.write(('\n$ ' + shlex.join(map(str, argv)) + '\n').encode())
            out.flush()
            proc = subprocess.run(list(map(str, argv)), input=data, stdout=out,
                                  stderr=subprocess.STDOUT, timeout=timeout)
        if check and proc.returncode:
            raise RuntimeError(f'command failed ({proc.returncode}); see {output}')
        return proc.returncode

    def ssh(self, script, output='service-checks.txt', timeout=90, check=True):
        return self.command(['ssh', '-i', self.private / 'key', '-p', self.port,
                             '-o', 'BatchMode=yes', '-o', 'IdentitiesOnly=yes',
                             '-o', 'StrictHostKeyChecking=no', '-o', 'ConnectTimeout=2',
                             '-o', 'UserKnownHostsFile=' + str(self.private / 'known_hosts'),
                             'root@127.0.0.1', 'sh -s'], output, timeout,
                            script.encode(), check)

    def preflight(self):
        a = self.args
        self.metadata = json.loads(a.metadata.read_text())
        self.result['source_commit'] = self.metadata.get('commit')
        self.result['package_sha256'] = validate_metadata(a.package, self.metadata, a.openwrt)
        self.result['package'] = self.metadata
        for tool in ('qemu-img', 'qemu-system-x86_64', 'ssh', 'ssh-keygen'):
            if not shutil.which(tool):
                raise RuntimeError('missing host prerequisite: ' + tool)
        self.private.mkdir(mode=0o700)
        self.command(['ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f',
                      self.private / 'key'], 'key-generation.txt')

    def image(self):
        p = PLATFORMS[self.args.openwrt]
        cache = self.args.cache_dir.absolute()
        cache.mkdir(parents=True, exist_ok=True)
        archive = self.args.image_file
        if archive is None:
            archive = cache / Path(p['image_url']).name
            if not archive.exists():
                # Unique partial file: never overwrite another run's download.
                import tempfile
                with tempfile.NamedTemporaryFile(dir=cache, delete=False) as out:
                    partial = Path(out.name)
                    try:
                        with urllib.request.urlopen(p['image_url'], timeout=60) as response:
                            shutil.copyfileobj(response, out)
                        out.flush()
                        if digest(partial) != p['image_sha256']:
                            raise ValueError('downloaded image SHA256 mismatch')
                        os.link(partial, archive)
                    except FileExistsError:
                        pass
                    finally:
                        partial.unlink(missing_ok=True)
        if digest(archive) != p['image_sha256']:
            raise ValueError('image SHA256 mismatch')
        self.result['image_sha256'] = p['image_sha256']
        extract_image(archive, self.raw)
        self.raw.chmod(0o444)
        self.command(['qemu-img', 'create', '-f', 'qcow2', '-F', 'raw', '-b',
                      self.raw, self.overlay], 'image-prepare.txt')

    def boot(self):
        with socket.socket() as port:
            port.bind(('127.0.0.1', 0))
            self.port = str(port.getsockname()[1])
        accelerator = 'kvm' if os.access('/dev/kvm', os.R_OK | os.W_OK) else 'tcg'
        self.result['accelerator'] = accelerator
        serial = self.root / 'serial.sock'
        argv = ['qemu-system-x86_64', '-m', '512', '-smp', '2', '-accel', accelerator,
                '-drive', f'file={self.overlay},format=qcow2,if=virtio',
                '-netdev', f'user,id=lan,hostfwd=tcp:127.0.0.1:{self.port}-:22',
                '-device', 'virtio-net-pci,netdev=lan', '-display', 'none',
                '-monitor', 'none', '-no-reboot', '-chardev',
                f'socket,id=console,path={serial},server=on,wait=off,logfile={self.root / "qemu-serial.log"}',
                '-serial', 'chardev:console']
        with (self.root / 'qemu-stderr.log').open('ab') as stderr:
            self.process = subprocess.Popen(argv, stdout=stderr, stderr=stderr)
        public = (self.private / 'key.pub').read_text().strip()
        deadline = time.monotonic() + 180
        sent = False
        with socket.socket(socket.AF_UNIX) as console:
            while not serial.exists():
                self.wait(deadline)
            console.connect(str(serial))
            console.settimeout(1)
            console.sendall(b'\n')
            while True:
                text = (self.root / 'qemu-serial.log').read_text(errors='replace')
                if 'Please press Enter' in text and not sent:
                    console.sendall(b'\n')
                if re.search(r'root@[^:]+:[^\n]*#', text):
                    break
                self.wait(deadline)
            bootstrap = ("umask 077; mkdir -p /etc/dropbear; printf '%s\\n' " +
                         shlex.quote(public) + ' > /etc/dropbear/authorized_keys; '
                         "uci set network.lan.proto='dhcp'; uci -q delete network.lan.ipaddr; "
                         'uci -q delete network.lan.netmask; uci commit network; '
                         '/etc/init.d/network restart\n')
            console.sendall(bootstrap.encode())
            while self.ssh('true\n', 'ssh-readiness.txt', timeout=6, check=False):
                self.wait(deadline)
        self.connected = True
        self.ssh('set -eu\ncat /etc/openwrt_release\nuname -a\ncat /proc/sys/kernel/random/boot_id\n'
                 '. /etc/openwrt_release\n[ "$DISTRIB_RELEASE" = ' +
                 shlex.quote(self.args.openwrt) + ' ]\n', 'guest-identity.txt')
        if self.args.fail_after == 'boot':
            raise RuntimeError('intentional --fail-after boot failure')

    def wait(self, deadline):
        if self.process and self.process.poll() is not None:
            raise RuntimeError('owned QEMU exited; see qemu-stderr.log')
        if time.monotonic() >= deadline:
            raise TimeoutError('guest readiness deadline exceeded')
        time.sleep(0.5)

    def install(self):
        fmt = PLATFORMS[self.args.openwrt]['package_format']
        # SSH stdin avoids scp/sftp server differences between releases.
        with self.args.package.open('rb') as source:
            data = source.read()
        argv = ['ssh', '-i', str(self.private / 'key'), '-p', self.port,
                '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=no',
                '-o', 'UserKnownHostsFile=' + str(self.private / 'known_hosts'),
                'root@127.0.0.1', 'cat > /tmp/exact.' + fmt]
        self.command(argv, 'package-install.txt', 60, data)
        remote = '/tmp/exact.' + fmt
        verify = 'echo ' + shlex.quote(self.result['package_sha256'] + '  ' + remote) + ' | sha256sum -c -\n'
        if fmt == 'ipk':
            install = 'opkg update\nopkg install ' + remote + '\nopkg install uhttpd luci-mod-admin-full\n'
            status = 'opkg list-installed\nopkg status UAmask\n'
            version = "opkg status UAmask | sed -n 's/^Version: //p'"
        else:
            install = 'apk update\napk add --allow-untrusted ' + remote + '\napk add uhttpd luci-mod-admin-full\n'
            status = 'apk info -v\ncat /lib/apk/db/installed\n'
            version = "awk 'BEGIN {RS=\"\"; FS=\"\\n\"} /(^|\\n)P:UAmask(\\n|$)/ {for(i=1;i<=NF;i++) if($i ~ /^V:/) print substr($i,3)}' /lib/apk/db/installed"
        self.ssh('set -eu\n' + verify + install, 'package-install.txt', timeout=600)
        self.ssh('set -eu\n' + status, 'package-status.txt')
        expected = self.metadata['package_version'] + PLATFORMS[self.args.openwrt]['package_release']
        self.result['expected_installed_version'] = expected
        self.ssh('set -eu\nv=$(' + version + ')\nprintf "installed_version=%s\\n" "$v"\n'
                 '[ "$v" = ' + shlex.quote(expected) + ' ]\n', 'package-status.txt')
        self.ssh('set -eu\nv=$(/usr/bin/UAmask -version)\nprintf "%s\\n" "$v"\n'
                 '[ "$v" = ' + shlex.quote('UA-Mask version: ' + self.metadata['version']) +
                 ' ]\n', 'package-status.txt')

    def service(self):
        self.ssh('''set -eu
[ "$(uci get UAmask.enabled.enabled)" = 0 ]
/etc/init.d/UAmask start || true
! pidof UAmask
uci set UAmask.enabled.enabled=1
uci commit UAmask
/etc/init.d/UAmask start
''')
        self.running()
        self.ssh('''set -eu
pidof UAmask > /tmp/smoke-old-pid
/etc/init.d/UAmask stop
''')
        deadline = time.monotonic() + 30
        while self.ssh('! pidof UAmask\n', check=False):
            self.wait(deadline)
        self.ssh('set -eu\n/etc/init.d/UAmask restart\n')
        self.running()
        self.ssh('''set -eu
[ "$(cat /tmp/smoke-old-pid)" != "$(pidof UAmask)" ]
before=$(pidof UAmask)
printf '{invalid' > /tmp/invalid.json
if /usr/bin/UAmask -check-config -config /tmp/invalid.json; then exit 1; fi
[ "$before" = "$(pidof UAmask)" ]
kill -0 "$before"
''')

    def running(self):
        deadline = time.monotonic() + 45
        probe = '''set -eu
pid=$(pidof UAmask)
[ -n "$pid" ]
kill -0 "$pid"
ubus call service list '{"name":"UAmask"}' > /tmp/smoke-procd.json
cat /tmp/smoke-procd.json
grep -q '"running": true' /tmp/smoke-procd.json
netstat -lnt | grep -E ':12032[[:space:]]'
/usr/bin/UAmask -check-config -config /var/run/UAmask/config.json
'''
        while self.ssh(probe, check=False):
            self.wait(deadline)

    def luci(self):
        self.ssh('''set -eu
if [ -f /usr/lib/lua/luci/controller/UAmask.lua ]; then
    test -f /usr/lib/lua/luci/model/cbi/UAmask.lua
    # Check dispatcher registration separately from the unauthenticated login HTTP.
    lua -e 'local found=false; entry=function(p,t) if table.concat(p,"/")=="admin/services/UAmask" and t=="UAmask" then found=true end end; cbi=function(v) return v end; require("luci.controller.UAmask").index(); assert(found)'
    echo lua-assets-present
else
    test -d /www/luci-static/resources/view/uamask
    echo js-assets-present
fi
touch /etc/config/wireless
/etc/init.d/rpcd restart
/etc/init.d/uhttpd restart
''', 'luci-checks.txt')
        deadline = time.monotonic() + 30
        while self.ssh("set -eu\nwget -S -O /tmp/luci-response http://127.0.0.1/cgi-bin/luci/admin/services/UAmask\ngrep -q luci_username /tmp/luci-response\ncat /tmp/luci-response\n",
                      'luci-checks.txt', check=False):
            self.wait(deadline)

    def diagnostics(self):
        if self.connected:
            self.ssh('''logread
ubus call service list '{"name":"UAmask"}'
ps w
netstat -lntp
cat /var/run/UAmask/config.json
if command -v nft; then nft list ruleset; else iptables-save; fi
''', 'guest-diagnostics.txt', timeout=30, check=False)

    def cleanup(self):
        state = dict(qemu_stopped=False, overlay_removed=False, private_material_removed=False)
        errors = []
        try:
            if self.process and self.process.poll() is None:
                # Popen owns this exact child; no global PID lookup or process matching.
                self.process.terminate()
                try:
                    self.process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    self.process.kill()
                    self.process.wait(timeout=10)
            state['qemu_stopped'] = self.process is None or self.process.poll() is not None
        except Exception as exc:
            errors.append(str(exc))
        if state['qemu_stopped']:
            try:
                self.overlay.unlink(missing_ok=True)
                self.raw.unlink(missing_ok=True)
                state['overlay_removed'] = not self.overlay.exists() and not self.raw.exists()
            except Exception as exc:
                errors.append(str(exc))
        try:
            if self.private.exists():
                shutil.rmtree(self.private)
            state['private_material_removed'] = not self.private.exists()
        except Exception as exc:
            errors.append(str(exc))
        self.result['cleanup'] = state
        if not all(state.values()):
            raise RuntimeError('cleanup failed: ' + '; '.join(errors))

    def run(self):
        try:
            for name in ('preflight', 'image', 'boot', 'install', 'service', 'luci'):
                with self.stage(name):
                    getattr(self, name)()
            self.result['status'] = 'PASS'
        except BaseException as exc:
            self.result.update(status='FAIL', failed_stage=self.stage_name, error=str(exc))
        finally:
            try:
                with self.stage('diagnostics'):
                    self.diagnostics()
            except BaseException as exc:
                self.event('diagnostics', 'warning', error=str(exc))
            try:
                with self.stage('cleanup'):
                    self.cleanup()
            except BaseException as exc:
                self.result.update(status='ERROR', failed_stage='cleanup', error=str(exc))
            (self.root / 'result.json').write_text(json.dumps(self.result, indent=2) + '\n')
        return 0 if self.result['status'] == 'PASS' else 1


def main(argv=None):
    args = parser().parse_args(argv)
    try:
        runner = Runner(args)
    except OSError as exc:
        print(str(exc), file=sys.stderr)
        return 2
    def interrupted(signum, frame):
        raise RuntimeError('interrupted by signal ' + str(signum))
    for sig in (signal.SIGTERM, signal.SIGINT):
        signal.signal(sig, interrupted)
    return runner.run()


if __name__ == '__main__':
    sys.exit(main())
