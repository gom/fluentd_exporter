package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
	"github.com/prometheus/procfs"
)

var (
	listenAddress = flag.String("web.listen-address", ":9224", "Address on which to expose metrics and web interface.")
	metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
	showVersion   = flag.Bool("version", false, "Print version information.")

	processNameRegex    = regexp.MustCompile(`/fluentd\s*`)
	configFileNameRegex = regexp.MustCompile(`\s(-c|--config)\s.*/(.+)\.conf\s*`)
)

const (
	namespace = "fluentd"
)

type Exporter struct {
	mutex sync.RWMutex

	fs procfs.FS

	scrapeFailures prometheus.Counter
	cpuTime        *prometheus.GaugeVec
	virtualMemory  *prometheus.GaugeVec
	residentMemory *prometheus.GaugeVec
	fluentdUp      prometheus.Gauge
}

func NewExporter() (*Exporter, error) {
	fs, err := procfs.NewFS(procfs.DefaultMountPoint)
	if err != nil {
		return nil, err
	}

	labelNames := []string{"conf_name", "worker_number", "pid"}
	return &Exporter{
		fs: fs,
		scrapeFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "exporter_scrape_failures_total",
			Help:      "Number of errors while scraping fluentd.",
		}),
		cpuTime: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cpi_time",
			Help:      "fluentd cpu time",
		},
			labelNames,
		),
		virtualMemory: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "virtual_memory_usage",
			Help:      "fluentd virtual memory usage",
		},
			labelNames,
		),
		residentMemory: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "resident_memory_usage",
			Help:      "fluentd resident memory usage",
		},
			labelNames,
		),
		fluentdUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "up",
			Help:      "the fluentd processes",
		}),
	}, nil
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	e.scrapeFailures.Describe(ch)
	e.cpuTime.Describe(ch)
	e.virtualMemory.Describe(ch)
	e.residentMemory.Describe(ch)
	e.fluentdUp.Describe(ch)
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.collect(ch)
}

func (e *Exporter) collect(ch chan<- prometheus.Metric) {
	ids, err := e.resolveFluentdIds()
	if err != nil {
		e.fluentdUp.Set(0)
		e.fluentdUp.Collect(ch)
		e.scrapeFailures.Inc()
		e.scrapeFailures.Collect(ch)
		return
	}

	log.Debugf("fluentd ids = %v", ids)

	workers := 0
	for groupKey, pidList := range ids {
		for i, pid := range pidList {
			procStat, err := e.procStat(groupKey, pid)
			if err != nil {
				e.scrapeFailures.Inc()
				e.scrapeFailures.Collect(ch)
				continue
			}

			labels := []string{groupKey, strconv.Itoa(i), strconv.Itoa(pid)}
			e.cpuTime.WithLabelValues(labels...).Set(procStat.CPUTime())
			e.virtualMemory.WithLabelValues(labels...).Set(float64(procStat.VirtualMemory()))
			e.residentMemory.WithLabelValues(labels...).Set(float64(procStat.ResidentMemory()))

			workers++
		}
	}

	e.fluentdUp.Set(float64(workers))

	e.cpuTime.Collect(ch)
	e.virtualMemory.Collect(ch)
	e.residentMemory.Collect(ch)
	e.fluentdUp.Collect(ch)
}

func (e *Exporter) resolveFluentdIds() (map[string][]int, error) {
	ids := make(map[string][]int)
	// map[config_filename] = list of pid (workers or processes)
	procs, err := e.fs.AllProcs()
	if err != nil {
		return nil, err
	}
	for _, proc := range procs {
		stat, err := proc.NewStat()
		if err != nil {
			log.Info(err)
			continue
		}
		if !processNameRegex.MatchString(stat.Comm) {
			continue
		}
		log.Debugf("Filterd %d = %s", stat.Comm, stat.PID)

		// PPID=1 is supervisor.
		if stat.PPID == 1 {
			continue
		}

		groupsKey := configFileNameRegex.FindStringSubmatch(stat.Comm)
		var key string
		if len(groupsKey) == 0 {
			key = "default"
		} else {
			key = strings.Trim(groupsKey[2], " ")
		}

		log.Debugf("Group = %s, %d", key, stat.PID)
		if _, exists := ids[key]; !exists {
			ids[key] = append(ids[key], stat.PID)
		}
	}
	return ids, nil
}

func (e *Exporter) procStat(groupKey string, pid int) (procfs.ProcStat, error) {
	proc, err := e.fs.NewProc(pid)
	if err != nil {
		log.Error(err)
		return procfs.ProcStat{}, err
	}

	procStat, err := proc.NewStat()
	if err != nil {
		log.Error(err)
		return procfs.ProcStat{}, err
	}
	return procStat, nil
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Fprintln(os.Stdout, version.Print("fluentd_exporter"))
		os.Exit(0)
	}

	e, err := NewExporter()
	if err != nil {
		log.Fatal(err)
	}

	prometheus.MustRegister(e)
	prometheus.MustRegister(version.NewCollector("fluentd_exporter"))

	log.Infoln("Starting fluentd_exporter", version.Info())
	log.Infoln("Build context", version.BuildContext())

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>fluentd Exporter</title></head>
			<body>
			<h1>fluentd Exporter v` + version.Info() + `</h1>
			<p><a href="` + *metricsPath + `">Metrics</a></p>
			</body>
		</html>`))
	})
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
