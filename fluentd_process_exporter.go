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
	processNameRegex    = regexp.MustCompile(`.*/fluentd\s`)
	configFileNameRegex = regexp.MustCompile(`\s(-c|--config)\s+.*?([^/]+)\.conf\s*`)
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

	labelNames := []string{"conf_name", "worker_id", "pid"}
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

	log.Infof("fluentd ids = %v", ids)

	ws := 0
	for groupKey, pidList := range ids {
		for i, pid := range pidList {
			ps, err := e.procStat(groupKey, pid)
			if err != nil {
				e.scrapeFailures.Inc()
				e.scrapeFailures.Collect(ch)
				continue
			}

			labels := []string{groupKey, strconv.Itoa(i), strconv.Itoa(pid)}
			e.cpuTime.WithLabelValues(labels...).Set(ps.CPUTime())
			e.virtualMemory.WithLabelValues(labels...).Set(float64(ps.VirtualMemory()))
			e.residentMemory.WithLabelValues(labels...).Set(float64(ps.ResidentMemory()))

			ws++
		}
	}

	e.fluentdUp.Set(float64(ws))

	e.cpuTime.Collect(ch)
	e.virtualMemory.Collect(ch)
	e.residentMemory.Collect(ch)
	e.fluentdUp.Collect(ch)
}

func (e *Exporter) resolveFluentdIds() (map[string][]int, error) {
	ids := make(map[string][]int)

	procs, err := e.fs.AllProcs()
	if err != nil {
		return nil, err
	}

	for _, p := range procs {
		cla, err := p.CmdLine()
		if err != nil {
			log.Info(err)
			continue
		}
		cl := strings.Join(cla, " ")
		if !processNameRegex.MatchString(cl) {
			continue
		}

		st, err := p.NewStat()
		if err != nil {
			log.Info(err)
			continue
		}

		// PPID=1 is a supervisor.
		if st.PPID == 1 {
			log.Infof("PPID %d = %s", st.PPID, cl)
			continue
		}

		groupsKey := configFileNameRegex.FindStringSubmatch(cl)
		log.Infof("groupsKey = %v", groupsKey)

		key := "default"
		if len(groupsKey) > 0 {
			key = strings.Trim(groupsKey[2], " ")
		}

		log.Infof("Group = %s, %d", key, st.PID)
		ids[key] = append(ids[key], st.PID)
	}
	return ids, nil
}

func (e *Exporter) procStat(groupKey string, pid int) (procfs.ProcStat, error) {
	p, err := e.fs.NewProc(pid)
	if err != nil {
		log.Error(err)
		return procfs.ProcStat{}, err
	}

	ps, err := p.NewStat()
	if err != nil {
		log.Error(err)
		return procfs.ProcStat{}, err
	}
	return ps, nil
}

func main() {
	var (
		Name          = "flunetd_process_exporter"
		listenAddress = flag.String("web.listen-address", ":9224", "Address on which to expose metrics and web interface.")
		metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		showVersion   = flag.Bool("version", false, "Print version information.")
	)
	flag.Parse()

	if *showVersion {
		fmt.Print(version.Print(Name))
		os.Exit(0)
	}

	e, err := NewExporter()
	if err != nil {
		log.Fatal(err)
	}

	prometheus.MustRegister(e)
	prometheus.MustRegister(version.NewCollector(Name))

	log.Infoln("Starting ", Name, version.Info())
	log.Infoln("Build context", version.BuildContext())

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>Fluentd Process Exporter</title></head>
			<body>
			<h1>Fluentd Process Exporter: ` + version.Info() + `</h1>
			<p><a href="` + *metricsPath + `">Metrics</a></p>
			</body>
		</html>`))
	})
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
