# The installed packages hold Werewolf's OpenSSH build policy.
set -eu

loader=
for path in /usr/lib/ld-linux-*.so.* /lib/ld-linux-*.so.* /lib64/ld-linux-*.so.*; do
	if [ -x "$path" ]; then
		loader=$path
		break
	fi
done
test -n "$loader"

# No program loads PAM, SELinux (with its PCRE2) or zlib. Each keeps libcrypto,
# whose ECDSA verifies P-256 security keys.
for program in /usr/bin/sshd /usr/lib/ssh/sshd-session /usr/lib/ssh/sshd-auth \
	/usr/bin/ssh /usr/bin/ssh-agent /usr/bin/ssh-keygen; do
	libraries=$("$loader" --list "$program")
	for library in libpam.so libselinux.so libpcre2 libz.so; do
		if printf '%s\n' "$libraries" | grep -q "$library"; then
			echo "$program loads $library" >&2
			exit 1
		fi
	done
	printf '%s\n' "$libraries" | grep -q libcrypto.so
done

work=$(mktemp -d)
ssh-keygen -q -t ed25519 -N '' -f "$work/host"
sshd -T -f /dev/null -h "$work/host" > "$work/defaults"
grep -qx 'Compression no' "$work/defaults"
if grep -qi '^UsePAM' "$work/defaults"; then
	echo 'sshd has PAM support' >&2
	exit 1
fi
if sshd -T -f /dev/null -h "$work/host" -o Compression=yes > /dev/null 2>&1; then
	echo 'sshd accepts Compression yes' >&2
	exit 1
fi
test "$(ssh -Q compression)" = none

# Both security key types, ed25519 and P-256 (Apple's Secure Enclave), remain.
keys=sk-ssh-ed25519@openssh.com,sk-ecdsa-sha2-nistp256@openssh.com
sshd -T -f /dev/null -h "$work/host" -o PubkeyAcceptedAlgorithms=$keys |
	grep -qx "PubkeyAcceptedAlgorithms $keys"
ssh -Q key | grep -qx sk-ecdsa-sha2-nistp256@openssh.com

# The client takes no PKCS#11 provider.
ssh -G -I /nonexistent localhost 2>&1 > /dev/null | grep -qx 'no support for PKCS#11.'
rm -rf "$work"
