"""Release gates must reject partial sets even when every downloaded file hashes."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('release', Path(__file__).with_name('verify-release.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.entries = release.matrix_entries(release.ROOT / '.github/openwrt-sdk-matrix.json')
        self.commit = 'a' * 40
        for arch in release.ARCHES:
            self.asset('UAmask-linux-' + arch, dict(platform='linux', architecture=arch))
        for entry in self.entries:
            self.asset(entry['id'] + '.' + entry['package_format'], dict(
                platform='openwrt', architecture=entry['build_arch'],
                openwrt_version=entry['openwrt'], target=entry['target'], subtarget=entry['subtarget'],
                package_architecture=entry['package_arch'], package_format=entry['package_format']))

    def asset(self, name, metadata):
        content = name.encode()
        sha = hashlib.sha256(content).hexdigest()
        (self.root / name).write_bytes(content)
        metadata.update(artifact=name, sha256=sha, dirty=False, version='1.2.3',
                        package_version='1.2.3', commit=self.commit)
        (self.root / (name + '.metadata.json')).write_text(json.dumps(metadata))
        (self.root / (name + '.sha256')).write_text(sha + '  ' + name + '\n')

    def verify(self):
        release.verify(self.root, '1.2.3', self.commit, self.entries)

    def test_complete_set_and_repeat(self):
        self.verify()
        self.verify()
        self.assertEqual(len((self.root / 'SHA256SUMS').read_text().splitlines()), 24)

    def test_missing_platform(self):
        (self.root / 'UAmask-linux-amd64.metadata.json').unlink()
        with self.assertRaisesRegex(ValueError, 'incomplete'):
            self.verify()

    def test_tampered_package(self):
        (self.root / 'UAmask-linux-amd64').write_bytes(b'tampered')
        with self.assertRaisesRegex(ValueError, 'tampering'):
            self.verify()

    def test_mixed_commit_or_dirty(self):
        path = self.root / 'UAmask-linux-amd64.metadata.json'
        original = json.loads(path.read_text())
        for field, value in (('commit', 'b' * 40), ('dirty', True)):
            with self.subTest(field=field):
                path.write_text(json.dumps(dict(original, **{field: value})))
                with self.assertRaises(ValueError):
                    self.verify()

    def test_evidence_cannot_become_release_asset(self):
        (self.root / 'guest-diagnostics.txt').write_text('test evidence')
        with self.assertRaisesRegex(ValueError, 'unexpected release assets'):
            self.verify()

    def test_both_package_managers_required(self):
        matrix = json.loads((release.ROOT / '.github/openwrt-sdk-matrix.json').read_text())
        matrix['entries'][8]['smoke'] = False
        path = self.root / 'matrix.json'
        path.write_text(json.dumps(matrix))
        with self.assertRaisesRegex(ValueError, 'both supported'):
            release.matrix_entries(path)


if __name__ == '__main__':
    unittest.main()
