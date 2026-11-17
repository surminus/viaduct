ARG IMAGE=ubuntu:24.04
FROM ${IMAGE}

COPY viaduct-integration /usr/local/bin/viaduct-integration
COPY verify.sh /usr/local/bin/viaduct-verify
COPY verify-noop.awk /usr/local/bin/viaduct-verify-noop.awk

# A dry run on the fresh image has to succeed and change nothing
RUN /usr/local/bin/viaduct-integration --dry-run
RUN test ! -e /opt/viaduct-test

# Apply the configuration twice: once to make the changes, and again to
# prove the run is idempotent, so the second run may only report commands as
# changes. Then verify the resulting state.
RUN /usr/local/bin/viaduct-integration
RUN /usr/local/bin/viaduct-integration --json > /tmp/second-run.json
RUN awk -f /usr/local/bin/viaduct-verify-noop.awk /tmp/second-run.json
RUN sh /usr/local/bin/viaduct-verify
