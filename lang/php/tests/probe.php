<?php
// What this PHP lets a program do, as one JSON line; tests/probe.sh compares it
// with the protections in patches/series and the image's allowances.
$result = [
    'exec' => function_exists('system'),                               // php:exec
    'native' => function_exists('dl'),                                 // php:native
    'process' => function_exists('pcntl_fork'),                        // php:process
    'listen' => function_exists('stream_socket_server'),               // php:listen
    'special-files' => function_exists('posix_mkfifo'),                 // php:special-files
    'debug' => function_exists('var_dump'),                           // php:debug
    'deserialize' => unserialize('O:8:"stdClass":0:{}') instanceof stdClass,  // php:deserialize
    'phar' => in_array('phar', stream_get_wrappers(), true),           // always absent
    'phar_class' => class_exists('Phar', false),
    'phar_extension' => extension_loaded('phar'),
];
ksort($result);
echo json_encode($result), "\n";
