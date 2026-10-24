set -eu
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
test ! -e /usr/bin/ffplay
ffmpeg -hide_banner -protocols > "$work/protocols"
test "$(awk '/^  / {print $1}' "$work/protocols" | sort -u)" = "$(printf 'file\npipe')"
if ffmpeg -v error -i https://127.0.0.1/unavailable -f null - 2> "$work/error"; then
    echo 'network input was accepted' >&2
    exit 1
fi
grep -i 'protocol not found' "$work/error"
ffmpeg -nostdin -v error -f lavfi -i testsrc=duration=1:size=64x48:rate=10 \
    -f lavfi -i sine=frequency=1000:duration=1 -c:v libx264 -pix_fmt yuv420p \
    -c:a aac -movflags +faststart -shortest "$work/video.mp4"
ffmpeg -nostdin -v error -i "$work/video.mp4" -vf thumbnail,scale=32:24 -frames:v 1 "$work/thumb.png"
test "$(ffprobe -v error -select_streams v:0 -show_entries stream=width -of csv=p=0 "$work/thumb.png")" = 32
ffmpeg -nostdin -v error -f lavfi -i sine=frequency=1000:duration=1 \
    -filter_complex showwavespic=s=64x32 -frames:v 1 "$work/wave.png"
test -s "$work/wave.png"
ffmpeg -nostdin -v error -i "$work/video.mp4" -f null -
echo 'PASS: local H.264/AAC conversion, thumbnails, waveform and rejected network protocols'
