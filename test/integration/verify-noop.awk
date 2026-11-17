# Reads the --json output of a run and fails if a resource reported a change
# on a run that should change nothing. Execute and Shell run their command
# every time, and Apt runs apt-get update. The manifest also installs and
# purges "hello", and holds and releases "dash", on every run, so those
# Package resources change too.
BEGIN {
	always["Execute"] = 1
	always["Shell"] = 1
	always["Apt"] = 1

	every_run["Package hello"] = 1
	every_run["Package dash"] = 1
}
/"resource_kind":/ { kind = $2; gsub(/[",]/, "", kind) }
/"description":/   { desc = $0; sub(/^ *"description": "/, "", desc); sub(/",?$/, "", desc) }
/"changed": true/  {
	if (!(kind in always) && !((kind " " desc) in every_run)) {
		print "changed on a run that should change nothing: " kind " " desc
		failed = 1
	}
}
END { exit failed }
