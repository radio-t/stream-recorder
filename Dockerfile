FROM umputun/baseimage:buildgo-latest as build

ARG GIT_BRANCH
ARG GITHUB_SHA

ADD . /build
WORKDIR /build

RUN version=${GIT_BRANCH}-${GITHUB_SHA:0:7}-$(date +%Y%m%dT%H:%M:%S)
RUN echo "version=$version"
RUN cd app && go build -o /build/streamrecorder -ldflags "-X main.revision=${version} -s -w"

FROM umputun/baseimage:app-latest

COPY --from=build /build/streamrecorder /srv/streamrecorder
RUN chown -R app:app /srv

WORKDIR /srv
ENTRYPOINT ["/srv/streamrecorder"]
