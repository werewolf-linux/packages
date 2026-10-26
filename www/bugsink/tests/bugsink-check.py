"""Startup and request decoding with the optional native packages absent."""
import gzip
import importlib.util
import io
import os
from types import SimpleNamespace
import zlib

os.environ.setdefault('DJANGO_SETTINGS_MODULE', 'bugsink.settings.default')
os.environ.setdefault('SECRET_KEY', 'package-smoke-test-not-a-deployment-key')

import django
django.setup()

from django.core.exceptions import BadRequest
from django.urls import get_resolver
from bugsink.streams import content_encoding_reader, handle_request_content_encoding
from bugsink.app_settings import get_settings
from events.storage import BrotliCompression
from sentry.minidump import merge_minidump_event
from files.minidump import parse_debug_object
import ingest.tasks

for name in ('brotli', '_brotli', 'symbolic', 'milksnake'):
    assert importlib.util.find_spec(name) is None, name
assert importlib.util.find_spec('_cffi_backend') is not None
assert not get_settings().FEATURE_MINIDUMPS
assert get_resolver().url_patterns

class Unreadable:
    def read(self, *args):
        raise AssertionError('unsupported payload was read')

def rejects(call, exception):
    try:
        call()
    except exception:
        return
    raise AssertionError('unsupported feature was accepted')

request = SimpleNamespace(META={'HTTP_CONTENT_ENCODING': 'br'}, _stream=Unreadable(), _read_started=False)
rejects(lambda: content_encoding_reader(request), BadRequest)
rejects(lambda: handle_request_content_encoding(request, 1024), BadRequest)
rejects(lambda: BrotliCompression(quality=5), ValueError)
rejects(lambda: parse_debug_object(b'invalid'), BadRequest)
rejects(lambda: merge_minidump_event({}, b'invalid', None), BadRequest)
payload = b'{"message":"package smoke test"}'
compressor = zlib.compressobj(wbits=-zlib.MAX_WBITS)
deflated = compressor.compress(payload) + compressor.flush()
for encoding, body in [('gzip', gzip.compress(payload)), ('deflate', deflated)]:
    request = SimpleNamespace(META={'HTTP_CONTENT_ENCODING': encoding}, _stream=io.BytesIO(body))
    assert content_encoding_reader(request).read() == payload
print('PASS: Bugsink startup, gzip/deflate and rejection of removed native features')
