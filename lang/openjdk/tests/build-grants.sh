mkdir -p /etc/runtime-hardening
: > /etc/runtime-hardening/build-root
chmod 600 /etc/runtime-hardening/build-root
# Wolfi's tests run their programs from writable directories.
mkdir -p /etc/runtime-hardening/allow/all/java
: > /etc/runtime-hardening/allow/all/java/writable-source
