package modules

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/DataDog/datadog-agent/cmd/system-probe/api"
	"github.com/DataDog/datadog-agent/cmd/system-probe/utils"
	"github.com/DataDog/datadog-agent/pkg/process/config"
	"github.com/DataDog/datadog-agent/pkg/process/procutil"
	"github.com/DataDog/datadog-agent/pkg/util/log"
)

// ErrProcessUnsupported is an error type indicating that the process module is not support in the running environment
var ErrProcessUnsupported = errors.New("process module unsupported")

// Process is a module that fetches process level data
var Process = api.Factory{
	Name: "process",
	Fn: func(agentConfig *config.AgentConfig) (api.Module, error) {
		if !agentConfig.CheckIsEnabled(config.ProcessModuleCheckName) {
			log.Infof("Process module disabled")
			return nil, api.ErrNotEnabled
		}

		log.Infof("Creating process module for: %s", filepath.Base(os.Args[0]))

		// we disable returning zero values for stats to reduce parsing work on process-agent side
		p := procutil.NewProcessProbe(procutil.WithReturnZeroPermStats(false))
		if p == nil {
			return nil, ErrProcessUnsupported
		}
		return &process{probe: p}, nil
	},
}

var _ api.Module = &process{}

type process struct{ probe *procutil.Probe }

// GetStats returns stats for the module
func (t *process) GetStats() map[string]interface{} {
	return nil
}

// Register registers endpoints for the module to expose data
func (t *process) Register(httpMux *http.ServeMux) error {
	var runCounter uint64
	httpMux.HandleFunc("/proc/stats", func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		stats, err := t.probe.StatsWithPermByPID()
		if err != nil {
			log.Errorf("unable to retrieve stats using process_tracer: %s", err)
			w.WriteHeader(500)
			return
		}
		utils.WriteAsJSON(w, stats)

		count := atomic.AddUint64(&runCounter, 1)
		logProcTracerRequests(count, len(stats), start)
	})
	return nil
}

// Close cleans up the underlying probe object
func (t *process) Close() {
	if t.probe != nil {
		t.probe.Close()
	}
}

func logProcTracerRequests(count uint64, statsCount int, start time.Time) {
	args := []interface{}{count, statsCount, time.Now().Sub(start)}
	msg := "Got request on /proc/stats (count: %d): retrieved %d stats in %s"
	switch {
	case count <= 5, count%20 == 0:
		log.Infof(msg, args...)
	default:
		log.Debugf(msg, args...)
	}
}
