#!/bin/sh
# Creates a self-signed code-signing certificate, "SnapLite Local Signing", in
# the login keychain, once per Mac. package-mac.sh signs with it when it
# exists.
#
# Why: macOS ties the screen-recording permission to the app's designated
# requirement. An ad hoc signature's requirement is its cdhash, which every
# build changes, so each repackage lost the permission. With a stable
# certificate the requirement is the bundle identifier plus that certificate,
# and the permission survives rebuilds. Grant 简截 once after the first build
# signed this way.
#
# The certificate is trusted for code signing only, for this user; macOS asks
# for your password to change the trust settings.
set -eu
NAME="SnapLite Local Signing"
KEYCHAIN="$HOME/Library/Keychains/login.keychain-db"

if security find-certificate -c "$NAME" "$KEYCHAIN" >/dev/null 2>&1; then
	echo "\"$NAME\" already exists:"
	security find-identity -p codesigning "$KEYCHAIN" | grep "$NAME" || true
	exit 0
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
cat >"$TMP/cert.cfg" <<EOF
[req]
distinguished_name = dn
x509_extensions = ext
prompt = no
[dn]
CN = $NAME
[ext]
basicConstraints = critical, CA:false
keyUsage = critical, digitalSignature
extendedKeyUsage = critical, codeSigning
EOF
openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -config "$TMP/cert.cfg" \
	-keyout "$TMP/key.pem" -out "$TMP/cert.pem" 2>/dev/null
# security import reads only the legacy PKCS#12 encryption; OpenSSL 3 needs
# -legacy for it, LibreSSL (the system openssl) has no such flag.
LEGACY=""
if openssl version | grep -q '^OpenSSL 3'; then
	LEGACY="-legacy"
fi
PASS="snaplite"
# shellcheck disable=SC2086
openssl pkcs12 -export $LEGACY -inkey "$TMP/key.pem" -in "$TMP/cert.pem" \
	-name "$NAME" -out "$TMP/identity.p12" -passout "pass:$PASS"
security import "$TMP/identity.p12" -k "$KEYCHAIN" -P "$PASS" -T /usr/bin/codesign
security add-trusted-cert -r trustRoot -p codeSign -k "$KEYCHAIN" "$TMP/cert.pem"
echo "created \"$NAME\":"
security find-identity -v -p codesigning "$KEYCHAIN" | grep "$NAME"
