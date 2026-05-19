FROM alpine:3.20
RUN mkdir -p /data
COPY bin/management-server-linux /management-server
EXPOSE 8080 9090
ENTRYPOINT ["/management-server"]
