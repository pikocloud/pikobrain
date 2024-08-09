FROM --platform=$BUILDPLATFORM alpine:latest AS certs
RUN apk --update add ca-certificates

FROM scratch
EXPOSE 8080/tcp
VOLUME config
VOLUME data
ENV PATH=/ \
    CONFIG=/config/brain.yaml \
    TOOLS=/config/tools.yaml \
    DB_URL="sqlite:///data/data.sqlite?cache=shared&_fk=1&_pragma=foreign_keys(1)"

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY . /
ENTRYPOINT ["/pikobrain"]