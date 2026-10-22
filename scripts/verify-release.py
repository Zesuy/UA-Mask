#!/usr/bin/env python3
"""Reject incomplete, mixed-source or tampered release candidates before upload."""
import argparse
import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
ARCHES = ('amd64', '386', 'arm64', 'armv5', 'armv6', 'armv7',
          'mips-softfloat', 'mipsle-softfloat', 'riscv64')


def matrix_entries(path):
    matrix = json.loads(Path(path).read_text())
    if matrix.get('schema_version') != 1 or not matrix.get('entries'):
        raise ValueError('invalid SDK matrix schema')
    entries = matrix['entries']
    ids, targets, smoke = set(), set(), set()
    for entry in entries:
        identity = tuple(entry[key] for key in ('openwrt', 'target', 'subtarget'))
        if entry['id'] in ids or identity in targets:
            raise ValueError('duplicate SDK identity')
        ids.add(entry['id'])
        targets.add(identity)
        prefix = 'https://downloads.openwrt.org/releases/' + '/'.join(
            (entry['openwrt'], 'targets', entry['target'], entry['subtarget'])) + '/'
        if not entry['sdk_url'].startswith(prefix + 'openwrt-sdk-'):
            raise ValueError('SDK must be pinned to official target release')
        if not re.fullmatch('[0-9a-f]{64}', entry['sdk_sha256']):
            raise ValueError('invalid SDK SHA256')
        if entry['package_format'] not in ('ipk', 'apk') or entry['build_arch'] not in ARCHES:
            raise ValueError('unsupported package format or build architecture')
        if type(entry['smoke']) is not bool:
            raise ValueError('smoke must be boolean')
        if entry['smoke']:
            if identity[1:] != ('x86', '64'):
                raise ValueError('runtime smoke currently supports x86/64 only')
            smoke.add((entry['openwrt'], entry['package_format']))
    if smoke != {('23.05.5', 'ipk'), ('25.12.5', 'apk')}:
        raise ValueError('both supported package managers require runtime smoke')
    return entries


def identity(metadata):
    if metadata['platform'] == 'linux':
        return ('linux', metadata['architecture'])
    if metadata['platform'] == 'openwrt':
        return tuple(metadata[key] for key in
                     ('platform', 'openwrt_version', 'target', 'subtarget',
                      'package_architecture', 'package_format', 'architecture'))
    raise ValueError('unsupported artifact platform')


def verify(directory, version, commit, entries):
    directory = Path(directory)
    if not re.fullmatch('[0-9a-f]{40}', commit):
        raise ValueError('expected full source commit')
    expected = {('linux', arch) for arch in ARCHES}
    expected.update(('openwrt', entry['openwrt'], entry['target'], entry['subtarget'],
                     entry['package_arch'], entry['package_format'], entry['build_arch'])
                    for entry in entries)
    seen, allowed, checksums = set(), set(), []
    for path in sorted(directory.glob('*.metadata.json')):
        metadata = json.loads(path.read_text())
        name = metadata['artifact']
        if Path(name).name != name or not name or '\n' in name or '\\' in name:
            raise ValueError('unsafe artifact basename')
        if path.name != name + '.metadata.json':
            raise ValueError('metadata basename mismatch')
        artifact = directory / name
        checksum = directory / (name + '.sha256')
        if artifact.is_symlink() or path.is_symlink() or checksum.is_symlink():
            raise ValueError('symlinks are not release assets')
        if metadata.get('commit') != commit or metadata.get('version') != version:
            raise ValueError('mixed source commit/version: ' + name)
        if metadata.get('dirty') is not False or not metadata.get('package_version'):
            raise ValueError('dirty or unversioned artifact: ' + name)
        key = identity(metadata)
        if key not in expected or key in seen:
            raise ValueError('unexpected or duplicate platform: ' + name)
        if key[0] == 'linux' and name != 'UAmask-linux-' + key[1]:
            raise ValueError('binary filename mismatch')
        if key[0] == 'openwrt' and not name.endswith('.' + metadata['package_format']):
            raise ValueError('package filename mismatch')
        sha = hashlib.sha256(artifact.read_bytes()).hexdigest()
        line = sha + '  ' + name + '\n'
        if metadata.get('sha256') != sha or checksum.read_text() != line:
            raise ValueError('artifact/checksum tampering: ' + name)
        seen.add(key)
        allowed.update((name, path.name, checksum.name))
        checksums.append(line)
    if seen != expected:
        raise ValueError('incomplete release inventory: missing ' + repr(sorted(expected - seen)))
    unexpected = {p.name for p in directory.iterdir()} - allowed - {'SHA256SUMS'}
    if unexpected:
        raise ValueError('unexpected release assets: ' + repr(sorted(unexpected)))
    (directory / 'SHA256SUMS').write_text(''.join(sorted(checksums)))
    print(f'Verified {len(seen)} artifacts from {commit}: {version}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--matrix', type=Path, default=ROOT / '.github/openwrt-sdk-matrix.json')
    parser.add_argument('--check-matrix', action='store_true')
    parser.add_argument('--directory', type=Path)
    parser.add_argument('--version')
    parser.add_argument('--commit')
    args = parser.parse_args()
    entries = matrix_entries(args.matrix)
    if args.check_matrix:
        print(f'Verified {len(entries)} pinned SDK entries and dual-format smoke coverage')
        return
    if not all((args.directory, args.version, args.commit)):
        parser.error('--directory, --version and --commit are required')
    verify(args.directory, args.version, args.commit, entries)


if __name__ == '__main__':
    main()
