#!/bin/sh
# Asserts the state applied by the integration configuration (main.go).
set -eu

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

# Package
command -v curl >/dev/null || fail "curl not installed"

# User
id testsvc >/dev/null || fail "testsvc user missing"
getent passwd testsvc | grep -q "/bin/false" || fail "testsvc shell is not /bin/false"
[ "$(id -u appuser)" = "1500" ] || fail "appuser has wrong uid"
[ "$(id -g appuser)" = "1500" ] || fail "appuser has wrong gid"
id -nG root | grep -qw daemon || fail "root not in daemon group"

# File, Directory, Link
grep -q "^hello$" /opt/viaduct-test/file || fail "file content wrong"
[ -L /opt/viaduct-test/link ] || fail "link missing"

# Template
grep -q "^Hello, viaduct!$" /opt/viaduct-test/greeting || fail "template not rendered"

# Archive
grep -q "^hello$" /opt/viaduct-test/extracted/file || fail "archive not extracted"

# Execute
[ -f /opt/viaduct-test/locked ] || fail "locked execute did not run"
[ ! -f /opt/viaduct-test/should-not-exist ] || fail "unless guard did not prevent execution"

# Sysctl
[ -f /etc/sysctl.d/99-viaduct-test.conf ] || fail "sysctl config missing"
grep -q "vm.swappiness" /etc/sysctl.d/99-viaduct-test.conf || fail "sysctl config content wrong"

echo "OK: all integration checks passed"
