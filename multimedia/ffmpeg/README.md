# FFmpeg

FFmpeg for local media conversion, as used by Mastodon. The upstream codec
selection remains: including H.264/H.265, AV1, VP8/VP9, JPEG 2000, WebP, MP3,
Opus and other supported audio/video formats. File and pipe are the only I/O
protocols; network inputs, hardware devices, display output, ffplay and font/text
rendering are omitted. The virtual lavfi input remains for media generation and
testing. zlib, bzip2 and lzma remain for compressed media data.

This removes OpenSSL, SRT, SSH, ZeroMQ, SDL, XML and font-rendering integrations
from FFmpeg. Automatic dependency discovery is disabled. It does not remove
these libraries from unrelated packages that require them.

Names and shared-library ABI remain unchanged. Use `ffmpeg-hardened` to require
this profile. Programs needing streaming URLs, devices or text rendering need
another build. Mastodon fetches media separately and passes local files to its
media processors.

All upstream codec/conversion and library tests remain; the CLI help test omits
ffplay. Additional tests check H.264/AAC conversion, thumbnails, waveforms and
rejection of HTTPS inputs. Derivation refuses unexpected upstream flag changes.
