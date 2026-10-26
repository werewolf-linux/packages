<?php
// A bounded FastCGI request over the test pool's private socket.
$socket = stream_socket_client('unix://' . $argv[1], $errno, $error, 5);
if (!$socket) throw new RuntimeException($error);
stream_set_timeout($socket, 5);
function record(int $type, string $body): string {
    return pack('CCnnCC', 1, $type, 1, strlen($body), 0, 0) . $body;
}
function length(int $size): string {
    return $size < 128 ? chr($size) : pack('N', $size | 0x80000000);
}
$params = '';
foreach (['SCRIPT_FILENAME' => $argv[2], 'REQUEST_METHOD' => 'GET',
          'SERVER_PROTOCOL' => 'HTTP/1.1', 'CONTENT_LENGTH' => '0'] as $name => $value) {
    $params .= length(strlen($name)) . length(strlen($value)) . $name . $value;
}
$request = record(1, pack('nCxxxxx', 1, 0)) . record(4, $params) . record(4, '') . record(5, '');
while ($request !== '') {
    $size = fwrite($socket, $request);
    if (!$size) throw new RuntimeException('FastCGI write failed');
    $request = substr($request, $size);
}
function readBytes($socket, int $size): string {
    $data = '';
    while (strlen($data) < $size) {
        $chunk = fread($socket, $size - strlen($data));
        if ($chunk === false || $chunk === '') throw new RuntimeException('Incomplete FastCGI response');
        $data .= $chunk;
    }
    return $data;
}
$output = '';
while (true) {
    $header = unpack('Cversion/Ctype/nid/nsize/Cpadding/Creserved', readBytes($socket, 8));
    if ($header['version'] !== 1 || $header['id'] !== 1) throw new RuntimeException('Invalid FastCGI record');
    $body = readBytes($socket, $header['size']);
    readBytes($socket, $header['padding']);
    if ($header['type'] === 6) $output .= $body;
    if ($header['type'] === 7 && $body !== '') fwrite(STDERR, $body);
    if ($header['type'] === 3) {
        $end = unpack('Nstatus/Cprotocol', $body);
        if ($end['status'] !== 0 || $end['protocol'] !== 0) throw new RuntimeException('FastCGI request failed');
        break;
    }
}
fclose($socket);
$parts = explode("\r\n\r\n", $output, 2);
if (count($parts) !== 2 || $parts[1] !== "ok\n" || preg_match('/^Status: (?!200)/mi', $parts[0])) {
    throw new RuntimeException('Unexpected FPM response: ' . $output);
}
echo "ok\n";
