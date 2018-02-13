FROM quay.io/prometheus/golang-builder as builder

ADD . /go/src/github.com/gom/fluentd_process_exporter
WORKDIR /go/src/github.com/gom/fluentd_process_exporter
RUN make

FROM quay.io/prometheus/busybox:latest

COPY --from=builder /go/src/github.com/gom/fluentd_process_exporter/fluentd_process_exporter /bin/fluentd_process_exporter

EXPOSE 9224
ENTRYPOINT [ "/bin/fluentd_process_exporter" ]