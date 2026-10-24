import importlib.util
import os
from pathlib import Path
import unittest
from unittest.mock import patch
spec = importlib.util.spec_from_file_location('recovery', Path(__file__).resolve().parents[1] / 'scripts/recover-release.py')
recovery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(recovery)

class Recovery(unittest.TestCase):
    def setUp(self):
        self.record = {'id': 123, 'repository': {'full_name': 'test/runtime'},
            'head_repository': {'full_name': 'test/runtime'}, 'path': '.github/workflows/wolfi-refresh.yml',
            'head_sha': 'a' * 40, 'head_branch': 'main', 'event': 'push', 'run_number': 456}
        self.jobs = [{'name': f'build (node) / build ({arch}, ubuntu-26.04)', 'conclusion': 'success'}
            for arch in ('x86_64', 'aarch64')]
        env = patch.dict(os.environ, {'GITHUB_REPOSITORY': 'test/runtime'})
        env.start()
        self.addCleanup(env.stop)

    def test_recovers_only_the_original_source_identity(self):
        self.assertEqual(recovery.checked_run('123', 'node', self.record, self.jobs), ('a' * 40, 456))

    def test_missing_failed_and_duplicate_builds_fail_closed(self):
        for jobs in (self.jobs[:1], [dict(self.jobs[0], conclusion='failure'), self.jobs[1]], self.jobs + self.jobs[:1]):
            with self.subTest(jobs=jobs), self.assertRaises(ValueError):
                recovery.checked_run('123', 'node', self.record, jobs)

    def test_other_workflow_fork_run_or_source_is_rejected(self):
        for field, value in (('id', 124), ('head_repository', {'full_name': 'fork/runtime'}),
                ('path', '.github/workflows/build.yml'), ('head_sha', 'main'),
                ('head_branch', 'feature'), ('event', 'pull_request')):
            with self.subTest(field=field), self.assertRaises(ValueError):
                recovery.checked_run('123', 'node', dict(self.record, **{field: value}), self.jobs)
