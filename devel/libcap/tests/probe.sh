# The installed libcap holds Werewolf's build policy: no PAM module, and no
# library that loads libpam. libcap-utils, tested by Wolfi's own steps, links
# only these libraries.
set -eu

if [ -e /usr/lib/security/pam_cap.so ]; then
	echo 'libcap installed pam_cap.so' >&2
	exit 1
fi
found=
for file in /usr/lib/libcap.so.* /usr/lib/libpsx.so.*; do
	test -e "$file" || continue
	found=1
	if grep -q 'libpam\.so' "$file"; then
		echo "$file names libpam" >&2
		exit 1
	fi
done
test -n "$found"
