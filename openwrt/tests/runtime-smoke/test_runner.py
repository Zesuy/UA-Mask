import importlib.util
import gzip
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('smoke', Path(__file__).with_name('run.py'))
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class ContractTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.package = self.root / 'UAmask.ipk'
        self.package.write_bytes(b'controlled package')
        self.metadata = dict(platform='openwrt', architecture='amd64',
                             openwrt_version='23.05.5', target='x86', subtarget='64',
                             package_architecture='x86_64', package_format='ipk',
                             artifact=self.package.name, sha256=smoke.digest(self.package),
                             commit='a' * 40, version='1.0.0', package_version='1.0.0')

    def args(self):
        meta = self.root / 'metadata.json'
        meta.write_text(json.dumps(self.metadata))
        return smoke.parser().parse_args(['--openwrt', '23.05.5', '--package',
                                          str(self.package), '--metadata', str(meta),
                                          '--run-dir', str(self.root / 'run'),
                                          '--cache-dir', str(self.root / 'cache')])

    def test_valid_metadata(self):
        self.assertEqual(smoke.validate_metadata(self.package, self.metadata, '23.05.5'),
                         smoke.digest(self.package))

    def test_identity_mismatches_rejected(self):
        for key in ('platform', 'architecture', 'openwrt_version', 'target', 'subtarget',
                    'package_architecture', 'package_format', 'artifact', 'sha256',
                    'commit', 'version', 'package_version'):
            with self.subTest(key=key):
                bad = dict(self.metadata)
                bad[key] = 'bad value'
                with self.assertRaises(ValueError):
                    smoke.validate_metadata(self.package, bad, '23.05.5')

    def test_tampered_package_rejected(self):
        self.package.write_bytes(b'tampered')
        with self.assertRaisesRegex(ValueError, 'sha256'):
            smoke.validate_metadata(self.package, self.metadata, '23.05.5')

    def test_platform_manifest(self):
        self.assertEqual(set(smoke.PLATFORMS), {'23.05.5', '25.12.5'})
        for version, config in smoke.PLATFORMS.items():
            self.assertRegex(config['image_sha256'], r'^[a-f0-9]{64}$')
            self.assertIn('/releases/' + version + '/targets/x86/64/', config['image_url'])

    def test_signed_image_trailer_and_truncation(self):
        archive = self.root / 'signed.img.gz'
        payload = gzip.compress(b'controlled raw image')
        archive.write_bytes(payload + b'# fwtool metadata')
        raw = self.root / 'base.raw'
        smoke.extract_image(archive, raw)
        self.assertEqual(raw.read_bytes(), b'controlled raw image')
        raw.unlink()
        archive.write_bytes(payload[:-4])
        with self.assertRaisesRegex(ValueError, 'truncated'):
            smoke.extract_image(archive, raw)

    def test_run_directory_never_overwritten(self):
        args = self.args()
        smoke.Runner(args)
        with self.assertRaises(FileExistsError):
            smoke.Runner(args)

    def test_preflight_failure_retains_events_and_result(self):
        args = self.args()
        self.package.write_bytes(b'tampered')
        runner = smoke.Runner(args)
        self.assertEqual(runner.run(), 1)
        result = json.loads((runner.root / 'result.json').read_text())
        self.assertEqual(result['status'], 'FAIL')
        self.assertEqual(result['failed_stage'], 'preflight')
        self.assertTrue(all(result['cleanup'].values()))
        events = [json.loads(line) for line in (runner.root / 'events.jsonl').read_text().splitlines()]
        self.assertEqual([(e['stage'], e['event']) for e in events[:2]],
                         [('preflight', 'start'), ('preflight', 'fail')])
        self.assertTrue(all('utc' in e and e['duration_seconds'] >= 0 for e in events))

    def test_cleanup_failure_cannot_pass(self):
        runner = smoke.Runner(self.args())
        with patch.multiple(runner, **{name: lambda: None for name in
                                      ('preflight', 'image', 'boot', 'install', 'service', 'luci')}):
            with patch.object(runner, 'cleanup', side_effect=OSError('cleanup refused')):
                self.assertEqual(runner.run(), 1)
        result = json.loads((runner.root / 'result.json').read_text())
        self.assertEqual(result['status'], 'ERROR')
        self.assertEqual(result['failed_stage'], 'cleanup')

    def test_owned_private_material_removed(self):
        runner = smoke.Runner(self.args())
        runner.private.mkdir()
        (runner.private / 'key').write_text('private material')
        runner.overlay.write_bytes(b'owned')
        runner.raw.write_bytes(b'owned')
        runner.cleanup()
        self.assertTrue(all(runner.result['cleanup'].values()))


if __name__ == '__main__':
    unittest.main()
