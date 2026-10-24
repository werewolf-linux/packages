import contextlib
import io
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import source

MIRRORS = ('https://example.invalid/one.git', 'https://example.invalid/two.git')


class SourceFetch(unittest.TestCase):
    def repository(self, root):
        repository = root / 'upstream'
        repository.mkdir()
        def git(*args):
            return subprocess.run(['git', '-C', str(repository), *args], check=True,
                                  capture_output=True, text=True).stdout.strip()
        git('init')
        (repository / 'file').write_text('locked\n')
        git('add', 'file')
        git('-c', 'user.name=fixture', '-c', 'user.email=fixture@example.invalid',
            'commit', '-m', 'locked')
        commit = git('rev-parse', 'HEAD')
        (repository / 'file').write_text('newer branch tip\n')
        git('add', 'file')
        git('-c', 'user.name=fixture', '-c', 'user.email=fixture@example.invalid',
            'commit', '-m', 'newer')
        return repository, commit

    def test_fallback_fetches_exact_commit_without_branch_history(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            upstream, commit = self.repository(root)
            destination = root / 'checkout'
            with contextlib.redirect_stdout(io.StringIO()):
                source.checkout(destination, commit, sources=(str(root / 'unavailable'), str(upstream)))
            self.assertEqual('locked\n', (destination / 'file').read_text())
            self.assertTrue((destination / '.git/shallow').is_file())
            actual = subprocess.check_output(['git', '-C', str(destination), 'rev-parse', 'HEAD'], text=True)
            self.assertEqual(commit, actual.strip())

    def test_invalid_pin_and_existing_checkout_are_rejected_before_mutation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            destination = root / 'checkout'
            for commit in ('master', '--upload-pack=attacker', '0' * 39):
                with self.assertRaises(ValueError):
                    source.checkout(destination, commit, MIRRORS)
                self.assertFalse(destination.exists())
            destination.mkdir()
            (destination / '.git').mkdir()
            (destination / 'file').write_text('keep\n')
            with self.assertRaises(ValueError):
                source.checkout(destination, '0' * 40, MIRRORS)
            self.assertEqual('keep\n', (destination / 'file').read_text())

    def test_wrong_commit_fails_without_checkout_or_retry(self):
        with tempfile.TemporaryDirectory() as directory:
            def run(command, **kwargs):
                output = 'f' * 40 if 'rev-parse' in command else ''
                return subprocess.CompletedProcess(command, 0, output, '')
            with patch.object(source.subprocess, 'run', side_effect=run) as commands, \
                 patch.object(source.time, 'sleep') as sleep, \
                 contextlib.redirect_stdout(io.StringIO()):
                with self.assertRaisesRegex(ValueError, 'differs'):
                    source.checkout(Path(directory), '0' * 40, MIRRORS)
            self.assertFalse(any('checkout' in call.args[0] for call in commands.call_args_list))
            sleep.assert_not_called()

    def test_timeouts_exhaust_bounded_retries_without_checkout(self):
        with tempfile.TemporaryDirectory() as directory:
            def run(command, **kwargs):
                if 'fetch' in command:
                    self.assertEqual(120, kwargs['timeout'])
                    self.assertIn('--depth=1', command)
                    self.assertIn('fetch.fsckObjects=true', command)
                    raise subprocess.TimeoutExpired(command, kwargs['timeout'])
                return subprocess.CompletedProcess(command, 0, '', '')
            with patch.object(source.subprocess, 'run', side_effect=run) as commands, \
                 patch.object(source.time, 'sleep') as sleep, \
                 contextlib.redirect_stdout(io.StringIO()):
                with self.assertRaisesRegex(RuntimeError, 'bounded retries'):
                    source.checkout(Path(directory), '0' * 40, MIRRORS)
            fetches = [call for call in commands.call_args_list if 'fetch' in call.args[0]]
            self.assertEqual(len(MIRRORS) * len(source.BACKOFF), len(fetches))
            self.assertEqual([15, 45], [call.args[0] for call in sleep.call_args_list])
            self.assertFalse(any('checkout' in call.args[0] for call in commands.call_args_list))


if __name__ == '__main__':
    unittest.main()
