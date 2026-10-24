# libvips

A build for Mastodon's image processing: JPEG, PNG, WebP and GIF, with EXIF
orientation, ICC colour conversion and zlib. These are the formats Mastodon's
`config/initializers/vips.rb` permits. Automatic feature discovery and external
loader modules are disabled. HEIF/AVIF, SVG, TIFF, PDF, ImageMagick, JPEG 2000,
JPEG XL, OpenSlide and font rendering are omitted, along with the C++ API.

The package retains the `libvips` name and shared-library ABI. Use
`libvips-hardened` to require this profile. Applications needing additional
loaders or the C++ API need another build; this is not a general replacement
for every image-processing application.

The tests keep upstream's non-HEIF checks and exercise all four retained image
formats, thumbnails, ICC transforms and the absence of omitted loader operations.
`libvips-dev` retains pkg-config and linkage checks. Expat, GLib, libexif,
libjpeg-turbo, libpng, libwebp, lcms2 and zlib remain required dependencies.
