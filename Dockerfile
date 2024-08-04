FROM --platform=$BUILDPLATFORM alpine:latest AS certs
RUN apk --update add ca-certificates

FROM scratch
EXPOSE 8080/tcp
VOLUME config
ENV PATH=/ \
    CONFIG=/config/brain.yaml \
    TOOLS=/config/tools.yaml

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY . /
ENTRYPOINT ["/pikobrain"]