<?php
// Exercise real regex and cached PHP execution after the launcher installs MDWE.
foreach (['pcre.jit', 'opcache.jit', 'opcache.jit_buffer_size', 'opcache.huge_code_pages'] as $name) {
    if (ini_get($name) !== false || ini_set($name, '1') !== false) {
        throw new RuntimeException("Executable code feature remains: $name");
    }
}
if (PCRE_JIT_SUPPORT || preg_match('/^(?<word>[a-z]+)-([0-9]+)$/u', 'hello-42', $matches) !== 1
    || $matches['word'] !== 'hello' || preg_replace('/[0-9]+/', 'world', 'hello-42') !== 'hello-world') {
    throw new RuntimeException('Regex interpreter failed');
}
$child = __DIR__ . '/no-jit-child.php';
if (!opcache_compile_file($child) || !opcache_is_script_cached($child) || (include $child) !== 42) {
    throw new RuntimeException('OPcache bytecode execution failed');
}
$status = opcache_get_status(false);
if (!$status || !$status['opcache_enabled'] || !empty($status['jit']['enabled'])
    || $status['opcache_statistics']['hits'] < 1) {
    throw new RuntimeException('OPcache did not serve cached bytecode');
}
echo "ok\n";
