FROM umputun/baseimage:buildgo-latest as build

ARG GIT_BRANCH
ARG GITHUB_SHA

ADD . /build
WORKDIR /build

# one RUN: a shell variable does not survive into the next layer, so computing the version
# separately from the build leaves the linker flag empty
RUN version="${GIT_BRANCH}-$(echo "${GITHUB_SHA}" | cut -c1-7)-$(date +%Y%m%dT%H:%M:%S)" && \
    echo "version=${version}" && \
    cd app && go build -o /build/streamrecorder -ldflags "-X main.revision=${version} -s -w"

FROM umputun/baseimage:app-latest

# enables automatic changelog generation by tools like Dependabot
LABEL org.opencontainers.image.source="https://github.com/radio-t/stream-recorder"

# ffmpeg is used post-recording to add a Xing/Info VBR header to the MP3 so
# players show the correct duration; the recorder still works without it.
RUN apk add --no-cache ffmpeg

COPY --from=build /build/streamrecorder /srv/streamrecorder
RUN chown -R app:app /srv

WORKDIR /srv
ENTRYPOINT ["/srv/streamrecorder"]
