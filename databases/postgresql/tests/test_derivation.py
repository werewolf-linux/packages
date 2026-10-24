from copy import deepcopy
from pathlib import Path
import sys
import unittest
import yaml

sys.path.insert(0, str(Path(__file__).resolve().parents[3] / 'wolfi/scripts'))
import packages
import releases
import verify

PKG = packages.Package('postgresql')
BASELINE = yaml.safe_load((Path(__file__).parent / 'fixtures/postgresql-18.yaml').read_text())


class PostgreSQL(unittest.TestCase):
    def derive(self, version='18.6'):
        lock = dict(recipe='postgresql-18.yaml', version=version, epoch=6 if version=='18.6' else 0,
                    revision=PKG.policy['revision'], source_commit=PKG.hooks.source_commit(BASELINE),
                    release={'version':version})
        effective = releases.effective(PKG, BASELINE, lock)
        derived = PKG.hooks.derive(PKG, deepcopy(effective), lock)
        verify.check(PKG, effective, derived)
        return derived

    def test_latest_only_names_and_consumers_keep_the_upstream_recipe_tests(self):
        derived = self.derive()
        self.assertEqual('postgresql', derived['package']['name'])
        self.assertEqual('18.6', derived['package']['version'])
        self.assertEqual(1006, derived['package']['epoch'])
        names = packages.output_names(derived)
        self.assertTrue({'postgresql','postgresql-client','postgresql-contrib','postgresql-dev','libpq'} <= names)
        self.assertFalse(any('17' in name or '18' in name or 'plperl' in name or 'oci-entrypoint' in name for name in names))
        self.assertNotIn('ecpg=${{package.full-version}}',derived['package']['dependencies']['runtime'])
        for step in BASELINE['test']['pipeline']:
            if step.get('name') != PKG.hooks.KERBEROS:
                self.assertIn(step, derived['test']['pipeline'])
        self.assertEqual(PKG.hooks.PROBED, derived['test']['pipeline'][-1]['name'])
        self.assertEqual(['ssl-groups-fips-tolerant-default.patch'],PKG.hooks.upstream_patches(BASELINE))

    def test_omits_optional_authentication_but_keeps_jit_icu_xml_and_compression(self):
        derived = self.derive()
        flags=derived['environment']['environment']['CONFIGURE_FLAGS'].split()
        for flag in ['--without-gssapi','--without-ldap','--without-pam','--with-llvm','--with-icu','--with-libxml','--with-lz4','--with-zstd']:
            self.assertIn(flag,flags)
        self.assertTrue(PKG.hooks.REMOVED_BUILD.isdisjoint(derived['environment']['contents']['packages']))
        changed=deepcopy(derived)
        changed['environment']['environment']['CONFIGURE_FLAGS']+=' --with-pam'
        with self.assertRaises(AssertionError):verify.check(PKG,BASELINE,changed)

    def test_can_bump_beyond_the_wolfi_recipe_without_relabeling_its_baseline(self):
        before=deepcopy(BASELINE)
        derived=self.derive('18.7')
        self.assertEqual('18.7',derived['package']['version'])
        self.assertEqual(1000,derived['package']['epoch'])
        self.assertEqual(before,BASELINE)
        self.assertIn('upstream-origin:postgresql-18:18.7-r0',derived['package']['dependencies']['provides'])

    def test_shadow_setup_is_only_in_the_disposable_test_environment(self):
        derived=self.derive()
        tested=PKG.hooks.test_recipe(deepcopy(derived),'http://localhost')
        self.assertEqual('Initialize test account databases',tested['test']['pipeline'][0]['name'])
        self.assertNotIn('Initialize test account databases',[s.get('name') for s in derived['pipeline']])
        lifecycle=tested['test']['pipeline'][-1]
        self.assertEqual('Database lifecycle and consumer tests',lifecycle['name'])
        for step in derived['test']['pipeline']:
            if step.get('name')=='Test database creation' or step.get('name')==PKG.hooks.PROBED:
                self.assertIn(step['runs'],lifecycle['runs'])
        self.assertIn('openssl',lifecycle['needs']['packages'])


if __name__=='__main__':unittest.main()
