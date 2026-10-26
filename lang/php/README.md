# PHP

Wolfi's newest stable PHP recipe with compiled runtime restrictions; currently `php-8.5`.

PCRE2 JIT, OPcache JIT and copying executable code into huge pages are compiled
out (`--without-pcre-jit --disable-opcache-jit --disable-huge-code-pages`). CLI
and FPM work under Memory-Deny-Write-Execute (MDWE), including regex matching
and shared-memory OPcache bytecode caching. Configuration cannot restore these
features. Werewolf enforces MDWE for PHP even when another service needs JIT.

The CLI omits readline and GD omits XPM. These app forms need neither interactive
line editing nor XPM images, so their readline/terminal and XPM/X11 runtime
dependencies are unnecessary. Other GD image formats remain enabled. Curl support
uses the HTTP(S)-only libcurl from this repository once that package is published.

## Use and allowances

Werewolf: add `php-8.5`, `php-8.5-fpm` and required `php-8.5-EXTENSION` packages to
`packages`. Wolfi, after [feed setup](../../wolfi/docs/usage.md#install):

```sh
apk add php-8.5@hardened php-8.5-fpm@hardened
```

Start CLI/FPM as a non-root user; put scripts on a read-only mount. For uploads,
add `allow: [php:upload]` to the existing Werewolf `php-fpm` service. On Wolfi, create
the empty file `/etc/runtime-hardening/allow/SERVICE/php/upload`.
Substitute any group below; see [marker files and scope](../../wolfi/docs/usage.md#grant-an-allowance).
Rebuild/deploy and restart FPM.
Use `all` for image-wide grants. Neither `php.ini`, argv nor environment grants access.

## Patches

| Patch | One-line change | Allowance / policy |
|---|---|---|
| [0001](patches/0001-deny-environment-configuration.patch) | Ignore `PHPRC` and `PHP_INI_SCAN_DIR`. | Always denied |
| [0002](patches/0002-werewolf-allowances.patch) | Read regular allowance files from the service or `all` namespace. | Always on |
| [0003](patches/0003-function-floor.patch) | Disable process-execution functions and `dl` at startup. | `exec`, `native` |
| [0004](patches/0004-unserialize-named-classes.patch) | Restrict object deserialization to explicitly named `allowed_classes`. | `deserialize` |
| [0005](patches/0005-no-url-include.patch) | Refuse `allow_url_include=1`. | Always denied |
| [0006](patches/0006-no-opcache-file-cache.patch) | Refuse nonempty `opcache.file_cache`; shared-memory OPcache remains. | Always denied |
| [0007](patches/0007-phar-wrapper-allowance.patch) | Gate the `phar://` wrapper if Phar is built. | `phar`; ineffective in shipped builds (Phar omitted) |
| [0008](patches/0008-inline.patch) | Refuse CLI `-r`, `-R`, `-B`, `-E` code. | `inline` |
| [0009](patches/0009-writable-source.patch) | Gate writable main scripts; extended to every compiled file by 0012. | `writable-source` |
| [0010](patches/0010-refuse-effective-root.patch) | Refuse effective UID 0 at module startup, including embedded PHP. | Always denied |
| [0011](patches/0011-log-denials-to-stderr.patch) | Emit JSON denial records directly to stderr. | Always on |
| [0012](patches/0012-verify-all-compiled-files.patch) | Verify opened source descriptors, including includes, prepends, preloads and OPcache hits. | `writable-source` for regular files; `inline` for generated streams/stdin |
| [0013](patches/0013-gate-evaluated-code.patch) | Gate `eval` and string compilation. | `inline` |
| [0014](patches/0014-upload-allowance.patch) | Gate multipart POST/`request_parse_body()` parsing and `move_uploaded_file()`. | `upload` |
| [0015](patches/0015-upload-progress-allowance.patch) | Gate session upload-progress callbacks before session decoding. | `upload-progress` plus `upload` |
| [0016](patches/0016-optional-capability-functions.patch) | Gate optional OS/diagnostic functions and extend the function floor. | `exec`, `native`, `namespace`, `process`, `listen`, `special-files`, `diagnostics` |
| [0017](patches/0017-mail-exec-allowance.patch) | Check the shared sendmail transport, including email-mode `error_log`. | `exec` |
| [0018](patches/0018-native-library-loaders.patch) | Gate SQLite extension loaders and optional FFI APIs/header preload. | `native` |
| [0019](patches/0019-debug-allowance.patch) | Gate fatal backtrace capture and debug output; replace `diagnostics`. | `debug` |

## Function groups

| Group | Functions restored when present |
|---|---|
| `exec` | `exec`, `system`, `passthru`, `shell_exec`, `proc_open`, `popen`, `pcntl_exec`, `mail`, `mb_send_mail`, `error_log(..., 1)` |
| `native` | `dl`, `Pdo\Sqlite::loadExtension`, `SQLite3::loadExtension`, FFI APIs and `ffi.preload` |
| `namespace` | `pcntl_unshare`, `pcntl_setns` |
| `process` | `pcntl_fork`, `pcntl_rfork`, `pcntl_forkx`, `posix_setsid`, `posix_setpgid` |
| `listen` | `stream_socket_server`, `stream_socket_accept` |
| `special-files` | `posix_mkfifo`, `posix_mknod` |
| `debug` | Fatal-error backtraces, `var_dump`, `print_r` (including return mode), `debug_zval_dump`, `debug_print_backtrace`, `phpinfo`, `phpcredits`, `ReflectionExtension::info()`, CLI `-i` and `--ri` |

Function availability and `deserialize` are fixed at startup. Mail, native loaders,
information, fatal backtrace, compiler and upload guards also check at use; progress checks once per body.

Replace existing `php:diagnostics` grants with `php:debug`; `diagnostics` no longer
grants access. Fatal traces require both `debug` and `fatal_error_backtraces=1`;
the allowance is checked before capture, even with `-d` or `ini_set()`.
Ordinary logging, error handlers, exception traces, `debug_backtrace()` and
`var_export()` remain available. Debug grants do not override `disable_functions`.

## Debug

Read [stderr denial records](../../wolfi/docs/usage.md#debug-a-refusal), then match the failure:

| Symptom | Fix |
|---|---|
| Multipart form empty; “multipart parsing is refused”; upload move returns false | Grant `upload`, including text-only multipart. Then check `file_uploads`, `post_max_size`, `upload_max_filesize`, upload temp/destination permissions and web-server limits. |
| Upload works but progress is absent | Add `upload-progress` and enable `session.upload_progress.enabled`; ordinary uploads need only `upload`. |
| “Call to undefined function” | Match the function group above; check `php -m` and `disable_functions` too. |
| Fatal error has no backtrace; debug output refused | Grant `debug`, restart PHP, and enable `fatal_error_backtraces` if needed. |
| WordPress/sendmail mail fails | Grant `exec` and permit/configure the transport, or use direct SMTP. File-mode `error_log` needs no grant. |
| `__PHP_Incomplete_Class` | Use `unserialize($data, ['allowed_classes' => ['YourClass']])`, or grant `deserialize` for broader access. |
| “unverified script source”; log group `source` | Put regular source on a read-only mount or grant `writable-source`; filters/user wrappers/stdin need `inline`. There is no `source` grant. |
| “evaluated code is refused” | Precompile templates into the image or grant `inline`; `writable-source` does not grant eval. |
| Settings ignored | Use the image's `php.ini`/scan directory or explicit CLI configuration; inspect with `php --ini`. Fixed prohibitions remain. |

Uploads grant no code execution; URL-encoded forms and saving raw request bodies
need no upload grant. Unknown source provenance is refused; symlinks use their opened
target. `@` cannot bypass checks. Allowances do not override kernel/service policy.

## Build differences

Keep Wolfi's commit, patches, toolchain, CLI/FPM, headers and configuration packages.
Retain extensions: `bcmath calendar ctype curl dom exif fileinfo gd gmp iconv intl ldap
mbstring mysqli mysqlnd openssl pcntl pdo pdo_mysql pdo_pgsql pdo_sqlite pgsql posix
simplexml soap sodium sysvsem xml xmlreader xmlwriter zip` and their config packages.
Omit Apache/mod_php, CGI, phpdbg, PEAR/PECL, docs and extensions
`bz2 ftp gettext odbc pdo_dblib pdo_odbc phar sockets sysvshm tidy xsl`.
Phar cannot be restored by an allowance; ship unpacked dependencies. Optional FFI
and SQLite3 guards do not add those extensions to the package.
Epoch is `1000 × policy revision + Wolfi epoch`; `wolfi-origin` preserves CVE matching.
Retained Wolfi tests plus [probes](tests/probe.sh) verify grants and refusals,
and run CLI and a persistent FPM worker under MDWE with regex and OPcache enabled.
[Build instructions](../../wolfi/README.md#building-locally); patches: PHP-3.01.

## Further attack-surface choices

The last decade's PHP bugs repeatedly involve native parsers, stream wrappers,
deserialization and request lifecycle state. The broadly compatible reductions
are already part of this package: omit CGI/mod_php/phpdbg, PEAR and Phar; leave
unused extensions uninstalled; gate native loading, process execution and
upload-progress callbacks; and authenticate every compiled source descriptor.
JIT and executable huge-page copying are now omitted too.

Useful application settings, with their compatibility boundaries:

| Setting | Benefit and boundary |
|---|---|
| `session.upload_progress.enabled=0` | Avoid pre-script session handling; ordinary uploads still work. Enable only with the separate progress allowance and a demonstrated need. |
| `zend.exception_ignore_args=1`, `display_errors=0`, `display_startup_errors=0`, `log_errors=1` | Keep arguments and error details out of HTTP responses; diagnostics remain in logs. These are production disclosure controls, not memory-safety fixes. |
| `session.use_strict_mode=1`, `session.use_only_cookies=1` | Reduce session fixation and URL-carried IDs; test custom session handlers and session-sharing applications. |
| `session.cookie_httponly=1`, `session.cookie_samesite=Lax` | Protect browser sessions; cross-site login/embedding flows may need another SameSite policy. Set `session.cookie_secure=1` only for HTTPS deployments. |
| Finite `max_input_vars`, `max_input_nesting_level`, `max_multipart_body_parts`, `max_file_uploads` | Bound parser complexity independently of byte size; choose per application, since large forms and imports need higher limits. |

Do not globally disable URL fopen, `php://`, filters, iconv, XML, fileinfo, EXIF,
GD or ZIP to claim compatibility with popular applications. These are used for
HTTP clients, request bodies, encoding, documents, uploads and media. Install
only the extensions each form needs. `allow_url_fopen=0` is suitable for an
application that uses cURL exclusively, but does not block cURL or all SSRF.
`open_basedir` is not a native-code security boundary; Werewolf uses Landlock.
Disabling assertions or short tags does not address the native parser bugs.

Historical rationale: PHP's [2018 Phar metadata findings and PHP 8 change](https://wiki.php.net/rfc/phar_stop_autoloading_metadata),
the [PHP 7 security changelog](https://www.php.net/ChangeLog-7.php)
(including FPM CVE-2019-11043 and image/serialization bugs), the
[PHP 8 security changelog](https://www.php.net/ChangeLog-8.php)
(including CGI CVE-2024-4577), and the
[2026 OOM/session advisories](https://www.php.net/archive/2026.php#2026-10-21-1).
See also PHP's [session security guidance](https://www.php.net/manual/en/session.security.ini.php).
These examples motivate removing unused features; they are not a claim that
one INI setting prevents every vulnerability in a subsystem.
