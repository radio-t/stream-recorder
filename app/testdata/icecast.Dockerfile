FROM ubuntu:22.04

RUN apt update && \
    apt install -y icecast2 curl ffmpeg && \
    rm -rf /var/lib/apt/lists/*

COPY icecast.xml /etc/icecast2/icecast.xml
COPY untsa.mp3 /srv
RUN mkdir -p /var/log/icecast
RUN mkdir -p /usr/share/icecast2/web

RUN useradd -ms /bin/bash icecast -g icecast

RUN chown -R icecast:icecast /var/log/icecast && \
    chown -R icecast:icecast /usr/share/icecast2 && \
    chown -R icecast:icecast /etc/icecast2

EXPOSE 8000

USER icecast

CMD icecast2 -c /etc/icecast2/icecast.xml & \
    ffmpeg -re -i /srv/untsa.mp3 -t 1 -b:a 128k -content_type audio/mpeg icecast://source:hackme@localhost:8000/stream.mp3
