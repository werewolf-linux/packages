#!/bin/sh
set -eu
if magick -version | grep '^Delegates' | grep -E '(^| )(x|fftw|pango|gvc)( |$)'; then
    echo 'optional GUI/FFT delegate remains' >&2
    exit 1
fi
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$work"
# Scans, web thumbnails and phone photos are common document inputs.
magick -size 120x80 gradient:white-black scan.png
for format in jpg tif webp heic; do
    magick scan.png "scan.$format"
    [ "$(magick identify -format '%wx%h' "scan.$format")" = 120x80 ]
    magick "scan.$format" -resize 60x40 "thumb-$format.png"
    [ "$(magick identify -format '%wx%h' "thumb-$format.png")" = 60x40 ]
done
magick -font DejaVu-Sans -pointsize 24 label:'Invoice 123' text.png
[ "$(magick identify -format '%k' text.png)" -gt 1 ]
# Preserve PDF rasterization through Ghostscript for Paperless document imports.
magick scan.png invoice.pdf
magick -density 72 invoice.pdf invoice.png
[ "$(magick identify -format '%wx%h' invoice.png)" = 120x80 ]
echo 'PASS: JPEG/TIFF/WebP/HEIC, resize, font rendering and PDF rasterization'
