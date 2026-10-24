# Native glibc r12 comparison

Evidence: aarch64, glibc source `34106fee698c83eb0af71d4d0381e66b399be746`, Melange 0.62.1, Wolfi GCC 16 and Alpine 6.18.56. Stock 2.44-r8: 6,487 PASS / 54 FAIL. Candidate 2.44-r12: 6,236 PASS / 349 FAIL. This inventory records all 295 new failures; it accepts none as expected failures. At this historical build, the raw 2% package gate blocked releases. The current requested downstream ceiling is 5.5%; see [r13](native-r13.md).

The candidate precedes the final IFS/profiling corrections and expanded locale fixture preparation. Full statuses and outputs are retained in `build/analysis/native-baseline-r12.json`; signed result APK hashes authenticate the source data.

| Input | SHA-256 |
| --- | --- |
| Stock result APK | `a8e361c7c96a2f9dee753087b0c44a6c498f5c3216758cf6e412a08bd53697c4` |
| Candidate result APK | `fee41c1ed3ad210a50d483d4781e5c5a6e7031af9ac68f9f4f0fb369b127bc11` |

| Test | Recorded failure evidence |
| --- | --- |
| `catgets/test-gencat` | no catalog: No such file or directory |
| `catgets/tst-catgets` | cannot load catalog: No such file or directory |
| `debug/tst-sprintf-fortify-rdonly-static` | error: xdlfcn.c:29: error: dlopen: tst-sprintf-fortify-rdonly-dlopen.so: cannot open shared object file: No such file or directory |
| `dlfcn/tststatic` | modstatic.so: cannot open shared object file: No such file or directory |
| `dlfcn/tststatic2` | nonexistent: modstatic2-nonexistent.so: cannot open shared object file: No such file or directory |
| `dlfcn/tststatic3` | dlopen (modstatic3.so): modstatic3.so: cannot open shared object file: No such file or directory |
| `dlfcn/tststatic4` | dlopen [global] (modstatic3.so): modstatic3.so: cannot open shared object file: No such file or directory |
| `dlfcn/tststatic5` | dlopen (modstatic5.so): modstatic5.so: cannot open shared object file: No such file or directory |
| `elf/argv0test` | ld.so: loader overrides are disabled |
| `elf/circleload1` | dlopen: /home/build/build/elf/circlemod2.so: undefined symbol: circlemod2_undefined |
| `elf/constload1` | No stdout; inspect original exit status and companion output. |
| `elf/dblload` | cannot open dblloadmod1.so: /home/build/build/elf/dblloadmod3.so: undefined symbol: baz |
| `elf/dblunload` | cannot load dblloadmod1.so: /home/build/build/elf/dblloadmod3.so: undefined symbol: baz |
| `elf/lateglobal` | main: cannot open ltglobmod2.so: /home/build/build/elf/ltglobmod2.so: undefined symbol: bar |
| `elf/list-tunables` | tst-rtld-list-tunables.exp /home/build/build/elf//tst-rtld-list-tunables.out differ: byte 169, line 4 |
| `elf/preloadtest` | testobj6 preload |
| `elf/reldep6` | No stdout; inspect original exit status and companion output. |
| `elf/resolvfail` | cannot load testobj1.so: /home/build/build/elf/testobj1.so: undefined symbol: foo |
| `elf/tst-audit12` | Start |
| `elf/tst-audit14` | No stdout; inspect original exit status and companion output. |
| `elf/tst-audit14-cmp` | No stdout; inspect original exit status and companion output. |
| `elf/tst-audit14a` | No stdout; inspect original exit status and companion output. |
| `elf/tst-audit14a-cmp` | No stdout; inspect original exit status and companion output. |
| `elf/tst-audit15` | No stdout; inspect original exit status and companion output. |
| `elf/tst-audit15-cmp` | No stdout; inspect original exit status and companion output. |
| `elf/tst-audit16` | No stdout; inspect original exit status and companion output. |
| `elf/tst-audit16-cmp` | No stdout; inspect original exit status and companion output. |
| `elf/tst-audit18` | tst-audit18.c:120: numeric comparison failure |
| `elf/tst-audit19b` | tst-audit19b.c:84: numeric comparison failure |
| `elf/tst-audit22` | tst-audit22.c:112: numeric comparison failure |
| `elf/tst-audit23` | tst-audit23.c:266: numeric comparison failure |
| `elf/tst-audit24a` | Didn't expect signal from child: got 'Aborted' |
| `elf/tst-audit24b` | tst-audit24b.c:31: numeric comparison failure |
| `elf/tst-audit24c` | Didn't expect signal from child: got 'Aborted' |
| `elf/tst-audit24d` | Didn't expect signal from child: got 'Aborted' |
| `elf/tst-audit25a` | tst-audit25.h:42: numeric comparison failure |
| `elf/tst-audit25b` | tst-audit25.h:42: numeric comparison failure |
| `elf/tst-audit28` | error: xdlfcn.c:29: error: dlopen: mapped to libc: cannot open shared object file: No such file or directory |
| `elf/tst-bz15311` | FAIL: tst-bz15311(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-bz28937` | FAIL: tst-bz28937(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dl-debug-exclude` | FAIL: 'file=' message not found (LD_DEBUG=all failed) |
| `elf/tst-dl-llp-stack` | error: tst-dl-llp-stack.c:102: not true: w->argv[0] != NULL && w->argv[1] != NULL && w->argv[2] != NULL && strcmp (w->argv[1], "--library-path") == 0 |
| `elf/tst-dl_find_object-static` | error: xdlfcn.c:29: error: dlopen: tst-dl_find_object-mod1.so: cannot open shared object file: No such file or directory |
| `elf/tst-dlopen-auditdup` | tst-dlopen-auditdup.c:31: numeric comparison failure |
| `elf/tst-dso-ordering-all2-0` | FAIL: tst-dso-ordering-all2-0(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all3-0` | FAIL: tst-dso-ordering-all3-0(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all3-1` | FAIL: tst-dso-ordering-all3-1(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all3-2` | FAIL: tst-dso-ordering-all3-2(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all3-3` | FAIL: tst-dso-ordering-all3-3(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-0` | FAIL: tst-dso-ordering-all4-0(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-1` | FAIL: tst-dso-ordering-all4-1(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-10` | FAIL: tst-dso-ordering-all4-10(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-11` | FAIL: tst-dso-ordering-all4-11(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-12` | FAIL: tst-dso-ordering-all4-12(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-13` | FAIL: tst-dso-ordering-all4-13(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-14` | FAIL: tst-dso-ordering-all4-14(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-15` | FAIL: tst-dso-ordering-all4-15(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-16` | FAIL: tst-dso-ordering-all4-16(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-17` | FAIL: tst-dso-ordering-all4-17(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-18` | FAIL: tst-dso-ordering-all4-18(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-19` | FAIL: tst-dso-ordering-all4-19(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-2` | FAIL: tst-dso-ordering-all4-2(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-20` | FAIL: tst-dso-ordering-all4-20(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-21` | FAIL: tst-dso-ordering-all4-21(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-22` | FAIL: tst-dso-ordering-all4-22(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-23` | FAIL: tst-dso-ordering-all4-23(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-24` | FAIL: tst-dso-ordering-all4-24(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-25` | FAIL: tst-dso-ordering-all4-25(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-26` | FAIL: tst-dso-ordering-all4-26(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-27` | FAIL: tst-dso-ordering-all4-27(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-28` | FAIL: tst-dso-ordering-all4-28(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-29` | FAIL: tst-dso-ordering-all4-29(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-3` | FAIL: tst-dso-ordering-all4-3(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-30` | FAIL: tst-dso-ordering-all4-30(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-31` | FAIL: tst-dso-ordering-all4-31(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-32` | FAIL: tst-dso-ordering-all4-32(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-33` | FAIL: tst-dso-ordering-all4-33(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-34` | FAIL: tst-dso-ordering-all4-34(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-35` | FAIL: tst-dso-ordering-all4-35(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-36` | FAIL: tst-dso-ordering-all4-36(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-37` | FAIL: tst-dso-ordering-all4-37(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-38` | FAIL: tst-dso-ordering-all4-38(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-39` | FAIL: tst-dso-ordering-all4-39(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-4` | FAIL: tst-dso-ordering-all4-4(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-40` | FAIL: tst-dso-ordering-all4-40(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-41` | FAIL: tst-dso-ordering-all4-41(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-42` | FAIL: tst-dso-ordering-all4-42(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-43` | FAIL: tst-dso-ordering-all4-43(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-44` | FAIL: tst-dso-ordering-all4-44(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-45` | FAIL: tst-dso-ordering-all4-45(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-46` | FAIL: tst-dso-ordering-all4-46(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-47` | FAIL: tst-dso-ordering-all4-47(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-48` | FAIL: tst-dso-ordering-all4-48(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-49` | FAIL: tst-dso-ordering-all4-49(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-5` | FAIL: tst-dso-ordering-all4-5(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-50` | FAIL: tst-dso-ordering-all4-50(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-51` | FAIL: tst-dso-ordering-all4-51(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-52` | FAIL: tst-dso-ordering-all4-52(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-53` | FAIL: tst-dso-ordering-all4-53(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-54` | FAIL: tst-dso-ordering-all4-54(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-55` | FAIL: tst-dso-ordering-all4-55(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-56` | FAIL: tst-dso-ordering-all4-56(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-6` | FAIL: tst-dso-ordering-all4-6(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-7` | FAIL: tst-dso-ordering-all4-7(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-8` | FAIL: tst-dso-ordering-all4-8(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering-all4-9` | FAIL: tst-dso-ordering-all4-9(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering1` | FAIL: tst-dso-ordering1(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering10` | FAIL: tst-dso-ordering10(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering2` | FAIL: tst-dso-ordering2(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering3` | FAIL: tst-dso-ordering3(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering4` | FAIL: tst-dso-ordering4(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering5` | FAIL: tst-dso-ordering5_1-a_bc(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering6` | FAIL: tst-dso-ordering6(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering7` | FAIL: tst-dso-ordering7(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering8` | FAIL: tst-dso-ordering8(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-dso-ordering9` | FAIL: tst-dso-ordering9_1-abcde(GLIBC_TUNABLES=glibc.rtld.dynamic_sort=1) execution test |
| `elf/tst-execstack-needed` | No stdout; inspect original exit status and companion output. |
| `elf/tst-execstack-prog` | No stdout; inspect original exit status and companion output. |
| `elf/tst-execstack-prog-static` | No stdout; inspect original exit status and companion output. |
| `elf/tst-execstack-prog-static-tunable` | Didn't expect signal from child: got 'Segmentation fault' |
| `elf/tst-execstack-tunable` | error: ./tst-execstack.c:160: not true: h != NULL |
| `elf/tst-getauxval-static` | error: xdlfcn.c:29: error: dlopen: tst-auxvalmod.so: cannot open shared object file: No such file or directory |
| `elf/tst-glibc-hwcaps-mask` | No stdout; inspect original exit status and companion output. |
| `elf/tst-glibc-hwcaps-prepend` | No stdout; inspect original exit status and companion output. |
| `elf/tst-global1` | cannot open testobj6 |
| `elf/tst-ifunc-fault-bindnow` | No stdout; inspect original exit status and companion output. |
| `elf/tst-ifunc-fault-dep-bindnow` | No stdout; inspect original exit status and companion output. |
| `elf/tst-ifunc-fault-dep-lazy` | No stdout; inspect original exit status and companion output. |
| `elf/tst-ifunc-fault-lazy` | No stdout; inspect original exit status and companion output. |
| `elf/tst-initfinilazyfail` | error: subprocess failed: constructor |
| `elf/tst-latepthread` | error: dlopen failed: /home/build/build/elf/tst-latepthreadmod.so: undefined symbol: this_function_is_not_defined |
| `elf/tst-ld_profile` | tst-ld_profile.c:67: error: string comparison failed |
| `elf/tst-non-directory-path` | ld.so: loader overrides are disabled |
| `elf/tst-pathopt` | failed to load "renamed.so": renamed.so: cannot open shared object file: No such file or directory |
| `elf/tst-rtld-dash-dash` | ld.so: loader overrides are disabled |
| `elf/tst-rtld-does-not-exist` | ld.so: loader overrides are disabled |
| `elf/tst-rtld-load-self` | ld-linux-aarch64.so.1: loader cannot load itself |
| `elf/tst-rtld-preload` | ld.so: loader overrides are disabled |
| `elf/tst-single_threaded-static-dlopen` | error: xdlfcn.c:29: error: dlopen: tst-single_threaded-mod2.so: cannot open shared object file: No such file or directory |
| `elf/tst-sprof-basic` | Hello World |
| `elf/tst-tls-debug-recursive` | info: malloc subsystem calls: 745 |
| `elf/tst-tls20` | Didn't expect signal from child: got 'Segmentation fault' |
| `elf/tst-tls9-static` | cannot open 'tst-tlsmod5.so': tst-tlsmod5.so: cannot open shared object file: No such file or directory |
| `elf/tst-tunables-enable_secure` | error: subprocess failed: tst-tunables-enable_secure |
| `elf/tst-tunables-enable_secure-env` | error: subprocess failed: tst-tunables-enable_secure-env |
| `elf/tst-unused-dep` | No stdout; inspect original exit status and companion output. |
| `elf/vismain` | '.protected' seems to be handled correctly, good! |
| `gmon/tst-gmon-gprof` | /home/build/build/gmon/tst-gmon.data.*: No such file or directory |
| `gmon/tst-gmon-pie-gprof` | /home/build/build/gmon/tst-gmon-pie.data.*: No such file or directory |
| `gmon/tst-gmon-static-gprof` | /home/build/build/gmon/tst-gmon-static.data.*: No such file or directory |
| `gmon/tst-gmon-static-pie-gprof` | /home/build/build/gmon/tst-gmon-static-pie.data.*: No such file or directory |
| `gmon/tst-mcount-overflow-check` | FAIL: expected message not in output: monstartup: maxarcs < minarcs, setting maxarcs = minarcs |
| `iconv/tst-gconv-init-failure` | error: tst-gconv-init-failure.c:53: unexpected iconv_open error: Invalid argument |
| `iconv/tst-iconv-opt` | error: tst-iconv-opt.c:48: not true: cd != (iconv_t) -1 |
| `iconv/tst-iconv9` | error: tst-iconv9.c:60: not true: cd != (iconv_t) -1 |
| `iconv/tst-iconv_prog` | FAIL: from: "INVALID", to: "INVALID", input "\x00\x00", flags "", return code 127 |
| `iconv/tst-iconvconfig` | /home/build/iconv |
| `iconv/tst-translit-mchar` | error: xsetlocale.c:28: error: setlocale (0, "tst-translit") |
| `iconvdata/bug-iconv1` | iconv_open failed |
| `iconvdata/bug-iconv10` | iconv_open failed (IBM930) |
| `iconvdata/bug-iconv11` | iconv_open("IBM930", "UTF-8"): Invalid argument |
| `iconvdata/bug-iconv12` | line 147: cannot convert from UCS4 to UTF-16LE: Invalid argument |
| `iconvdata/bug-iconv13` | error: bug-iconv13.c:28: not true: cd != (iconv_t) -1 |
| `iconvdata/bug-iconv14` | error: bug-iconv14.c:29: not true: c != (iconv_t) -1 |
| `iconvdata/bug-iconv15` | error: bug-iconv15.c:36: not true: cd != (iconv_t) -1 |
| `iconvdata/bug-iconv2` | No stdout; inspect original exit status and companion output. |
| `iconvdata/bug-iconv4` | Can't open converter |
| `iconvdata/bug-iconv5` | cannot open iconv module |
| `iconvdata/bug-iconv6` | iconv_open failed |
| `iconvdata/bug-iconv8` | result: -1 9 2 131 |
| `iconvdata/bug-iconv9` | cannot load iconv module: Invalid argument |
| `iconvdata/iconv-test` | test data: ISO-8859-1 -> UTF8 FAILED |
| `iconvdata/tst-bug33980` | error: tst-bug33980.c:60: not true: cd != (iconv_t) -1 |
| `iconvdata/tst-e2big` | No stdout; inspect original exit status and companion output. |
| `iconvdata/tst-iconv-big5-hkscs-to-2ucs4` | error: xsetlocale.c:28: error: setlocale (6, "zh_HK.BIG5-HKSCS") |
| `iconvdata/tst-iconv-iso-2022-cn-ext` | error: tst-iconv-iso-2022-cn-ext.c:44: not true: cd != (iconv_t) -1 |
| `iconvdata/tst-iconv4` | iconv_open failed: Invalid argument |
| `iconvdata/tst-iconv6` | first iconv_open failed |
| `iconvdata/tst-iconv7` | iconv_open failed |
| `iconvdata/tst-jisx0213-progress` | error: tst-jisx0213-progress.c:45: not true: cd != (iconv_t) -1 |
| `iconvdata/tst-loading` | opening of UHC failed: Invalid argument |
| `iconvdata/tst-tables` | failed: ./tst-table.sh /home/build/build/ /home/build/build/iconvdata/ ISO646-GB BS_4730 |
| `inet/tst-idna_name_classify` | tst-idna_name_classify.c:50: numeric comparison failure |
| `intl/mtrace-tst-gettext` | No stdout; inspect original exit status and companion output. |
| `intl/tst-codeset` | tst-codeset.c:36: error: string comparison failed |
| `intl/tst-gettext` | gettext ("") failed, returned "", expected "Project-Id-Version: GNU libc 2.43.9000 |
| `intl/tst-gettext2` | String1 - First string for testing. |
| `intl/tst-gettext3` | call 1 returned: cheese |
| `intl/tst-gettext4` | beauty |
| `intl/tst-gettext5` | cheese |
| `intl/tst-plural-eval` | No stdout; inspect original exit status and companion output. |
| `intl/tst-translit` | test |
| `libio/tst-fopenloc` | error: xfopen.c:29: could not open ../iconvdata/testdata/ISO-8859-1 (mode "r,ccs=ISO-8859-1"): Invalid argument |
| `libio/tst-fopenloc-cmp` | ../iconvdata/testdata/ISO-8859-1..UTF8 /home/build/build/libio/tst-fopenloc.out differ: byte 1, line 1 |
| `locale/tst-locale-locpath` | No stdout; inspect original exit status and companion output. |
| `localedata/bug-iconv-trans` | iconv_open failed |
| `localedata/sort-test` | am_ET.UTF-8 collate-test FAIL |
| `localedata/tst-ctype` | error: tst-ctype.c:391:     islower('�' = '\xaa') not true |
| `localedata/tst-digits` | cannot set locale 'test7' |
| `localedata/tst-fmon` | FAIL: Locale tstfmon_n01y12 failed the test (error 2). |
| `localedata/tst-setlocale3` | unexpected setlocale success for "does-not-exist" locale |
| `localedata/tst-trans` | locale data files probably not loaded |
| `localedata/tst-wctype` | locale = C |
| `localedata/tst_mblen` | mblen:de_DE.ISO-8859-1:1:1:2:F:the return value is different from an expected value |
| `localedata/tst_mbrlen` | mbrlen:de_DE.ISO-8859-1:1:1:2:S:PASSED |
| `localedata/tst_mbrtowc` | mbrtowc:de_DE.ISO-8859-1:1:1:2:F:the return value is different from an expected value |
| `localedata/tst_mbsrtowcs` | mbsrtowcs:de_DE.ISO-8859-1:1:1:2:F:the return value is different from an expected value |
| `localedata/tst_mbstowcs` | mbstowcs:de_DE.ISO-8859-1:1:1:2:S:PASSED |
| `localedata/tst_mbtowc` | mbtowc:de_DE.ISO-8859-1:1:1:2:F:the return value is different from an expected value |
| `localedata/tst_swscanf` | swscanf:de_DE.ISO-8859-1:1:1:2:F:the return value is different from an expected value |
| `localedata/tst_wcrtomb` | wcrtomb:de_DE.ISO-8859-1:1:1:2:F:the return value is different from an expected value |
| `localedata/tst_wcsrtombs` | wcsrtombs:de_DE.ISO-8859-1:1:1:2:S:PASSED |
| `localedata/tst_wcstombs` | wcstombs:de_DE.ISO-8859-1:1:1:2:S:PASSED |
| `localedata/tst_wctob` | wctob:de_DE.ISO-8859-1:1:1:2:S:PASSED |
| `localedata/tst_wctomb` | wctomb:de_DE.ISO-8859-1:1:1:2:F:the return value is different from an expected value |
| `malloc/tst-compathooks-on` | error: tst-compathooks-off.c:139: not true: call_count == hook_count |
| `malloc/tst-malloc-usable` | tst-malloc-usable.c:36: numeric comparison failure (widths 64 and 32) |
| `malloc/tst-malloc-usable-mcheck` | tst-malloc-usable.c:36: numeric comparison failure (widths 64 and 32) |
| `malloc/tst-malloc-usable-threaded-main` | tst-malloc-usable.c:36: numeric comparison failure (widths 64 and 32) |
| `malloc/tst-malloc-usable-threaded-worker` | tst-malloc-usable.c:36: numeric comparison failure (widths 64 and 32) |
| `malloc/tst-malloc-usable-tunables` | ../malloc/tst-malloc-usable.c:36: numeric comparison failure (widths 64 and 32) |
| `malloc/tst-malloc-usable-tunables-malloc-check` | ../malloc/tst-malloc-usable.c:36: numeric comparison failure (widths 64 and 32) |
| `malloc/tst-malloc-usable-tunables-threaded-main` | ../malloc/tst-malloc-usable.c:36: numeric comparison failure (widths 64 and 32) |
| `malloc/tst-malloc-usable-tunables-threaded-worker` | ../malloc/tst-malloc-usable.c:36: numeric comparison failure (widths 64 and 32) |
| `malloc/tst-mtrace` | No stdout; inspect original exit status and companion output. |
| `misc/tst-bti-abort-imm` | No stdout; inspect original exit status and companion output. |
| `misc/tst-bti-abort-transitive` | called function fun |
| `misc/tst-bti-abort-unprot` | No stdout; inspect original exit status and companion output. |
| `misc/tst-bti-abort-unprot-preload` | No stdout; inspect original exit status and companion output. |
| `misc/tst-bti-dep-prot-preload` | Didn't expect signal from child: got 'Segmentation fault' |
| `misc/tst-bti-dlopen-imm` | error: ../sysdeps/unix/sysv/linux/aarch64/tst-bti-skeleton-dlopen.c:44: not true: h == NULL |
| `misc/tst-bti-dlopen-transitive` | error: ../sysdeps/unix/sysv/linux/aarch64/tst-bti-skeleton-dlopen.c:44: not true: h == NULL |
| `misc/tst-bti-ld-debug-both` | Could not find expected 'security: not compatible with AArch64 BTI: /home/build/build/misc/' in LD_DEBUG_OUTPUT file |
| `misc/tst-bti-ld-debug-dlopen` | library tst-bti-mod-unprot.so loaded normally |
| `misc/tst-bti-ld-debug-exe` | Could not find expected 'security: not compatible with AArch64 BTI: /home/build/build/misc/' in LD_DEBUG_OUTPUT file |
| `misc/tst-bti-ld-debug-shared` | called function fun |
| `misc/tst-bti-permissive-audit` | error: subprocess failed: tst-bti-audit |
| `misc/tst-bti-permissive-preload` | Didn't expect signal from child: got 'Segmentation fault' |
| `misc/tst-bti-prot-audit` | error: subprocess failed: tst-bti-audit |
| `misc/tst-bti-unprot-audit` | ../sysdeps/unix/sysv/linux/aarch64/tst-bti-skeleton-audit.c:74: error: string comparison failed |
| `misc/tst-rseq-disable` | ../sysdeps/unix/sysv/linux/tst-rseq-disable.c:73: numeric comparison failure |
| `misc/tst-sme-clone3` | svl: 64 |
| `nptl/tst-dl-debug-tid` | No stdout; inspect original exit status and companion output. |
| `nptl/tst-stack2` | error: xdlfcn.c:29: error: dlopen: tst-stack2-mod.so: cannot enable executable stack as shared object requires: Invalid argument |
| `nptl/tst-tls3` | dlopen failed |
| `nptl/tst-tls3-malloc` | dlopen failed |
| `nss/tst-nss-test1` | FAIL: passwd entry "name20000" wrong |
| `nss/tst-nss-test2` | Didn't expect signal from child: got 'Segmentation fault' |
| `nss/tst-nss-test4` | FAIL: Found 4330 errors, expected 0 |
| `nss/tst-nss-test5` | FAIL: [0] passwd entry 100.name100 missing |
| `nss/tst-nss-test_errno` | tst-nss-test_errno.c:54: error: string comparison failed |
| `posix/bug-regex33` | bug-regex33.1: false match or error: re_search() returned 9, should return -1 |
| `posix/globtest` | Tilde test failed |
| `posix/tst-execlp2` | errno = 2 (No such file or directory), expected EACCES |
| `posix/tst-execvp2` | errno = 2 (No such file or directory), expected EACCES |
| `posix/tst-execvp3` | No stdout; inspect original exit status and companion output. |
| `posix/tst-execvpe2` | errno = 2 (No such file or directory), expected EACCES |
| `posix/tst-execvpe3` | No stdout; inspect original exit status and companion output. |
| `posix/tst-execvpe7` | tst-execvpe7.c:67: error: string comparison failed |
| `posix/tst-fnmatch4` | error: tst-fnmatch4.c:27: not true: setlocale (LC_ALL, locale) != NULL |
| `posix/tst-regex` | No stdout; inspect original exit status and companion output. |
| `posix/tst-spawn-chdir` | tst-spawn-chdir.c:176: numeric comparison failure |
| `posix/tst-spawn-chdir-pidfd` | ../posix/tst-spawn-chdir.c:176: numeric comparison failure |
| `posix/tstgetopt` | --required foobar |
| `posix/wordexp-test` | info: test 9 (one "$var"): FAILED |
| `resolv/tst-resolv-ai_idn-latin1` | error: addrinfo comparison failure |
| `resolv/tst-resolv-res_init` | error: output mismatch for empty file with LOCALDOMAIN (init method res_init) |
| `resolv/tst-resolv-res_init-thread` | error: output mismatch for empty file with LOCALDOMAIN (init method res_init) |
| `stdio-common/temptest` | tempnam failed |
| `stdio-common/tst-freopen2` | error: xfopen.c:29: could not open /tmp/tst-freopen2Q1a9qP/file2 (mode "w,ccs=iso-8859-1"): Invalid argument |
| `stdio-common/tst-freopen2-mem` | Memory not freed: |
| `stdio-common/tst-freopen64-2` | error: xfopen.c:29: could not open /tmp/tst-freopen2WvnBMO/file2 (mode "w,ccs=iso-8859-1"): Invalid argument |
| `stdio-common/tst-freopen64-2-mem` | Memory not freed: |
| `stdio-common/tst-grouping_iterator` | error: tst-grouping_iterator.c:216: not true: loc != 0 |
| `stdio-common/tst-swprintf` | No stdout; inspect original exit status and companion output. |
| `stdio-common/tst-vfprintf-width-i18n` | error: xsetlocale.c:28: error: setlocale (6, "hi_IN.UTF-8") |
| `stdlib/bug-fmtmsg1` | label:part: WARNING: text |
| `stdlib/tst-fmtmsg` | GLIBC:tst-fmtmsg: HALT: halt |
| `stdlib/tst-strfmon_l` | error: newlocale (hi_IN.UTF-8): No such file or directory |
| `support/tst-support_spawn_wrap` | tst-support_spawn_wrap.c:142: error: blob comparison failed |
| `time/bug-getdate1` | Failed on getdate("1"), template is: %M |
| `time/tst-getdate` | tst-getdate.c:141: numeric comparison failure |
| `time/tst-mktime` | Wednesday |
| `time/tst-posixtz` | TZ = "AEST-10AEDST-11,M10.5.0,M3.5.0", time = 909312849 => 1998/10/25 10:54:09 dst=0 zone=UTC, FAIL |
| `time/tst-tzname` | FAIL: TZ=STD-1DST, tzname[0] = UTC |
| `time/tst-y2039` | error: tst-y2039.c:39: not true: strcmp (buf, "2039-04-30 14:00:00 PDT") == 0 |
| `timezone/test-tz` | /home/build/build/timezone/test-tz: flunked test 0 (expected 832935315, got 832910115) |
| `timezone/tst-bz28707` | warning: tzset set errno to 2 (No such file or directory)tst-bz28707.c:47: error: string comparison failed |
| `timezone/tst-bz29951` | tst-bz29951.c:31: numeric comparison failure |
| `timezone/tst-timezone` | FAILED *** |
| `wcsmbs/test-c8rtomb` | error: xsetlocale.c:28: error: setlocale (6, "zh_HK.BIG5-HKSCS") |
| `wcsmbs/test-mbrtoc8` | error: xsetlocale.c:28: error: setlocale (6, "zh_HK.BIG5-HKSCS") |
| `wcsmbs/tst-btowc` | de_DE.ISO-8859-1: btowc('�') failed, returned L'\xffffffff' instead of L'\x80' |
| `wcsmbs/tst-mbrtowc2` | FAIL |
| `wcsmbs/tst-wcsmbs-clone-overflow` | error: tst-wcsmbs-clone-overflow.c:58: __shlib_handle is NULL! |
