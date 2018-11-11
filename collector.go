package main

import (
	"fmt"
	"time"

	"github.com/kennygrant/sanitize"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"

	"encoding/json"
	"os/exec"
	"strings"

	"github.com/spf13/viper"
)

type collector struct {
	target string
	module *Module
}

type Module struct {
	sshUser         string
	sshIdentityFile string
	sshPort         int
	needSudo        bool
	path            string
	prefix          string
}

type InspecOutput struct {
	Controls []struct {
		ID            string `json:"id"`
		ProfileID     string `json:"profile_id"`
		ProfileSha256 string `json:"profile_sha256"`
		Status        string `json:"status"`
		CodeDesc      string `json:"code_desc"`
		Message       string `json:"message,omitempty"`
		SkipMessage   string `json:"skip_message,omitempty"`
		Resource      string `json:"resource,omitempty"`
	} `json:"controls"`
	Statistics struct {
		Duration float64 `json:"duration"`
	} `json:"statistics"`
	Version string `json:"version"`
}

func ScrapeTarget(target string, config *Module) (InspecOutput, error) {
	inspecArgs := []string{
		"exec",
		config.path,
		"--reporter",
		"json-min",
	}
	if target != "" {
		inspecArgs = append(inspecArgs,
			"-t",
			fmt.Sprintf("ssh://%v@%v:%v", config.sshUser, target, config.sshPort),
			"-i",
			config.sshIdentityFile)

		if config.needSudo {
			inspecArgs = append(inspecArgs, "--sudo")
		}
	}

	var inspecData InspecOutput
	inspecCommand := exec.Command(viper.GetString("inspec_path"), inspecArgs...)
	fmt.Printf("%v", inspecCommand.Args)
	inspecOutput, err := inspecCommand.CombinedOutput()

	if err != nil && err.Error() != "exit status 100" {
		return inspecData, err
		//log.Fatalf("inspecCommand.Run() failed with %s", err)
	}

	err = json.Unmarshal(inspecOutput, &inspecData)
	if err != nil {
		return inspecData, err
		//log.Fatalf("inspec Output convertion failed with %s", err)
	}
	return inspecData, nil
}

// Describe implements Prometheus.Collector.
func (c collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- prometheus.NewDesc("dummy", "dummy", nil, nil)
}

// Collect implements Prometheus.Collector.
func (c collector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	inspecData, err := ScrapeTarget(c.target, c.module)
	if err != nil {
		log.Infof("Error scraping target %s: %s", c.target, err)
		ch <- prometheus.NewInvalidMetric(prometheus.NewDesc("inspec_error", "Error scraping target", nil, nil), err)
		return
	}
	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc(c.module.prefix+"total_returned", "Tests returned from scrape process.", nil, nil),
		prometheus.GaugeValue,
		float64(len(inspecData.Controls)))

	passed := 0
	for _, check := range inspecData.Controls {
		ch <- prometheus.MustNewConstMetric(
			prometheus.NewDesc(c.module.prefix+normalize(check.CodeDesc), check.CodeDesc, nil, nil),
			prometheus.GaugeValue,
			isPassed(check.Status))
		if isPassed(check.Status) > 0 {
			passed++
		}
	}

	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc(c.module.prefix+"total_passed", "Tests returned from scrape process.", nil, nil),
		prometheus.GaugeValue,
		float64(passed))

	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc("inspec_scrape_duration_seconds", "Time inspec took.", nil, nil),
		prometheus.GaugeValue,
		float64(time.Since(start).Seconds()))
}

func normalize(desc string) string {
	return strings.Replace(
		sanitize.Name(strings.Replace(desc, "/", "_", -1)), "-", "_", -1)
}

func isPassed(passed string) float64 {
	if passed == "passed" {
		return float64(1)
	} else {
		return float64(0)
	}
}
