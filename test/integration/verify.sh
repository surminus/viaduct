#!/bin/sh
# Asserts the state applied by the integration configuration (main.go).
set -eu

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

# Package
command -v curl >/dev/null || fail "curl not installed"

# Package purge and hold, on the platforms the config exercises them
if command -v dpkg >/dev/null; then
	if command -v hello >/dev/null; then
		fail "hello was installed but not purged"
	fi

	apt-mark showhold | grep -qx bash || fail "bash is not held"

	if apt-mark showhold | grep -qx dash; then
		fail "dash was not released"
	fi
fi

# User
id testsvc >/dev/null || fail "testsvc user missing"
getent passwd testsvc | grep -q "/bin/false" || fail "testsvc shell is not /bin/false"
[ "$(id -u appuser)" = "1500" ] || fail "appuser has wrong uid"
[ "$(id -g appuser)" = "1500" ] || fail "appuser has wrong gid"
id -nG root | grep -qw daemon || fail "root not in daemon group"

# Group
[ "$(getent group testgrp | cut -d: -f3)" = "1600" ] || fail "testgrp has wrong gid"
getent group appgrp >/dev/null || fail "appgrp group missing"
id -nG grpuser | grep -qw appgrp || fail "grpuser not in appgrp group"

# File, Directory, Link
grep -q "^hello$" /opt/viaduct-test/file || fail "file content wrong"
[ -L /opt/viaduct-test/link ] || fail "link missing"

# Chain
grep -q "^chained$" /opt/viaduct-chain/file || fail "chain file content wrong"
[ -L /opt/viaduct-chain/link ] || fail "chain link missing"

# CreateDirIfMissing
[ -d /opt/viaduct-createp/nested ] || fail "createp parent dir not created"
grep -q "^created with parents$" /opt/viaduct-createp/nested/file || fail "createp file content wrong"

# File permissions without content
[ "$(stat -c %a /opt/viaduct-test/unmanaged)" = "600" ] || fail "unmanaged file has wrong mode"
grep -q "^unmanaged$" /opt/viaduct-test/unmanaged || fail "unmanaged file content was overwritten"

# Template
grep -q "^Hello, viaduct!$" /opt/viaduct-test/greeting || fail "template not rendered"

# Archive
grep -q "^hello$" /opt/viaduct-test/extracted/file || fail "archive not extracted"

# Line
grep -q "^permanent$" /opt/viaduct-test/lines || fail "appended line missing"
grep -q "^setting=enabled$" /opt/viaduct-test/lines || fail "setting line not replaced"
[ "$(grep -c "^setting=" /opt/viaduct-test/lines)" = "1" ] || fail "setting line duplicated"

# Execute
[ -f /opt/viaduct-test/locked ] || fail "locked execute did not run"
[ ! -f /opt/viaduct-test/should-not-exist ] || fail "unless guard did not prevent execution"

# Sysctl
[ -f /etc/sysctl.d/99-viaduct-test.conf ] || fail "sysctl config missing"
grep -q "vm.swappiness" /etc/sysctl.d/99-viaduct-test.conf || fail "sysctl config content wrong"

echo "OK: all integration checks passed"
