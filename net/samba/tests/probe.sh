# The installed Samba holds Werewolf's build policy: no PAM module, and no
# server program or Samba library that loads libpam.
set -eu

if [ -e /usr/lib/security/pam_winbind.so ]; then
	echo 'Samba installed pam_winbind.so' >&2
	exit 1
fi
test -e /usr/lib/samba/libauth-private-samba.so
for file in /usr/bin/smbd /usr/bin/nmbd /usr/lib/samba/*.so* /usr/lib/libsmbconf.so* \
	/usr/lib/libsamba-*.so*; do
	test -e "$file" || continue
	if grep -q 'libpam\.so' "$file"; then
		echo "$file names libpam" >&2
		exit 1
	fi
done
