# fluentd_process_exporter

Prometheus exporter for process metrics about fluentd.

## Build and Run

```
$ make
$ ./fluentd_process_exporter
```

## Metrics

Name | Type
------|------
fluentd_cpu_time | gauge 
fluentd_resident_memory_usage | gauge
fluentd_virtual_memory_usage | gauge
fluentd_up | gauge

## Options
Name |Description | Default
---------|-------------|----|
web.listen-address | Address on which to expose metrics and web interface | :9224 
web.telemetry-path | Path under which to expose metrics | /metrics 

## Example
fluentd processes are grouped by the config file name.

If we have 2 processes and 3 / 1 workers each, following metrics will be exposed.
* Processes
```
   PID   PPID CMD
110627      1 /ruby/2.5.0/bin/fluentd -c other_fluent.conf -d
110630 110627  \_ /ruby/2.5.0/bin/fluentd -c other_fluent.conf -d
 18013      1 /ruby/2.5.0/bin/fluentd -c fluent.conf -d
 18018  18013  \_ /ruby/2.5.0/bin/fluentd -c fluent.conf -d
 18019  18013  \_ /ruby/2.5.0/bin/fluentd -c fluent.conf -d
 18021  18013  \_ /ruby/2.5.0/bin/fluentd -c fluent.conf -d
```

* Metrics
```
# HELP fluentd_cpi_time fluentd cpu time
# TYPE fluentd_cpi_time gauge
fluentd_cpu_time{group="fluent",id="fluent_0"} 12.48
fluentd_cpu_time{group="fluent",id="fluent_1"} 12.51
fluentd_cpu_time{group="fluent",id="fluent_2"} 13.1
fluentd_cpu_time{group="other_fluent",id="other_fluent_0"} 2777.72
# HELP fluentd_resident_memory_usage fluentd resident memory usage
# TYPE fluentd_resident_memory_usage gauge
fluentd_resident_memory_usage{group="fluent",id="fluent_0"} 2.8639232e+07
fluentd_resident_memory_usage{group="fluent",id="fluent_1"} 3.106816e+07
fluentd_resident_memory_usage{group="fluent",id="fluent_2"} 2.8975104e+07
fluentd_resident_memory_usage{group="other_fluent",id="other_fluent_0"} 3.0334976e+07
# HELP fluentd_up the fluentd processes
# TYPE fluentd_up gauge
fluentd_up 4
# HELP fluentd_virtual_memory_usage fluentd virtual memory usage
# TYPE fluentd_virtual_memory_usage gauge
fluentd_virtual_memory_usage{group="fluent",id="fluent_0"} 4.453376e+08
fluentd_virtual_memory_usage{group="fluent",id="fluent_1"} 4.4550144e+08
fluentd_virtual_memory_usage{group="fluent",id="fluent_2"} 4.45526016e+08
fluentd_virtual_memory_usage{group="other_fluent",id="other_fluent_0"} 5.89860864e+08
```

## Docker

```
$ make docker
$ docker run --net=host --pid=host fluentd_process_exporter:master
```