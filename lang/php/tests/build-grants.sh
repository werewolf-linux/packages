mkdir -p /etc/runtime-hardening
: > /etc/runtime-hardening/build-root
chmod 600 /etc/runtime-hardening/build-root
mkdir -p /etc/runtime-hardening/allow/all/php
for f in exec native deserialize inline writable-source upload upload-progress namespace process listen special-files debug; do
  : > /etc/runtime-hardening/allow/all/php/$f
done
