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
        self.assertEqual(2006, derived['package']['epoch'])
        names = packages.output_names(derived)
        self.assertTrue({'postgresql','postgresql-client','postgresql-contrib','postgresql-dev','libpq'} <= names)
        self.assertFalse(any('17' in name or '18' in name or 'plperl' in name or 'oci-entrypoint' in name for name in names))
        self.assertNotIn('ecpg=${{package.full-version}}',derived['package']['dependencies']['runtime'])
        for step in BASELINE['test']['pipeline']:
            if step.get('name') != PKG.hooks.KERBEROS:
                self.assertIn(step, derived['test']['pipeline'])
        self.assertEqual(PKG.hooks.PROBED, derived['test']['pipeline'][-1]['name'])
        self.assertEqual(['per-version-dirs.patch', 'ssl-groups-fips-tolerant-default.patch'],
                         derived['pipeline'][1]['with']['patches'].split())

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
        self.assertEqual(2000,derived['package']['epoch'])
        self.assertEqual(before,BASELINE)
        self.assertIn('upstream-origin:postgresql-18:18.7-r0',derived['package']['dependencies']['provides'])

    def test_disposable_package_test_keeps_checks_that_fit_one_mapped_uid(self):
        derived=self.derive()
        tested=PKG.hooks.test_recipe(deepcopy(derived),'http://localhost')
        self.assertEqual([s.get('name') for s in tested['test']['pipeline']],
                         [None, 'Test if PostgreSQL binaries are present and runnable', PKG.hooks.PROBED])
        self.assertEqual(['postgresql-dev', 'postgresql-contrib'],
                         tested['test']['pipeline'][-1]['needs']['packages'])
        self.assertNotIn('shadow', tested['test']['environment']['contents']['packages'])
        self.assertNotIn('sudo-rs', tested['test']['environment']['contents']['packages'])
        changed=deepcopy(derived)
        changed['test']['pipeline'][-2]['name']='Unexpected upstream lifecycle test'
        with self.assertRaisesRegex(ValueError, 'Review changed PostgreSQL database lifecycle tests'):
            PKG.hooks.test_recipe(changed,'http://localhost')


if __name__=='__main__':unittest.main()
