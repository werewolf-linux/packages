set -eu
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
vips black "$work/source.png" 32 24 --bands 3
for format in jpg png webp; do
    vips copy "$work/source.png" "$work/input.$format"
    vipsthumbnail "$work/input.$format" --size 16x16 -o "$work/thumb-$format.png"
    test "$(vipsheader -f width "$work/thumb-$format.png")" = 16
    test "$(vipsheader -f height "$work/thumb-$format.png")" = 12
done
printf '%s' R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7 | base64 -d > "$work/input.gif"
vips copy "$work/input.gif" "$work/gif.png"
test "$(vipsheader -f width "$work/gif.png")" = 1
vips icc_transform "$work/source.png" "$work/colour.png" srgb
test "$(vipsheader -f width "$work/colour.png")" = 32
vips -l > "$work/operations"
for operation in jpegload pngload webpload gifload icc_transform autorot; do
    grep -F "$operation" "$work/operations"
done
for operation in heifload svgload tiffload pdfload magickload jp2kload jxlload openslideload; do
    if grep -F "$operation" "$work/operations"; then
        echo "unexpected loader: $operation" >&2
        exit 1
    fi
done
echo 'PASS: JPEG/PNG/WebP/GIF thumbnails, colour conversion and omitted loaders'
