ARG IMAGE=ubuntu:24.04
FROM ${IMAGE}

COPY viaduct-integration /usr/local/bin/viaduct-integration
COPY verify.sh /usr/local/bin/viaduct-verify

# Apply the configuration twice: once to make the changes, and again to
# prove the run is idempotent. Then verify the resulting state.
RUN /usr/local/bin/viaduct-integration
RUN /usr/local/bin/viaduct-integration
RUN sh /usr/local/bin/viaduct-verify
