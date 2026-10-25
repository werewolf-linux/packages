mkdir -p /etc/runtime-hardening
: > /etc/runtime-hardening/build-root
chmod 600 /etc/runtime-hardening/build-root
mkdir -p /etc/runtime-hardening/allow/all/python
for f in native deserialize inline writable-source environment; do
  : > /etc/runtime-hardening/allow/all/python/$f
done
