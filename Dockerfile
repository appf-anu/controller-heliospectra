FROM alpine:latest
RUN apk add --no-cache tzdata
COPY controller-heliospectra /bin
VOLUME /data
ENTRYPOINT ["controller-heliospectra"]
