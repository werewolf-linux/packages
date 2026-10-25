mkdir -p /etc/runtime-hardening
: > /etc/runtime-hardening/build-root
chmod 600 /etc/runtime-hardening/build-root
mkdir -p /etc/runtime-hardening/allow/all/node
for group in exec native inline writable-source environment; do
  : > /etc/runtime-hardening/allow/all/node/$group
done
