# Build image for Anthropic OSS Scanner (https://github.com/anthropics/oss-scanner).
# The scanner builds this file with network access and then scans the image offline,
# so every dependency for the build and the tests must be fetched here.
FROM node:24-bookworm

COPY --from=golang:1.27-bookworm /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"

RUN npm install -g corepack@latest && corepack enable

COPY . /src
WORKDIR /src

# Without THEMES_REPO_TOKEN, make build skips the private premium themes.
RUN make build

# Run the Go tests once, so the image holds every test dependency for offline runs.
# A failing test does not fail the image.
RUN go test -count=1 ./... || echo "WARNING: go test reported failures"
