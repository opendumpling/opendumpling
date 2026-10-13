// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package metric provides functions to initialize a prometheus metric
// detailing build info
package metric

import (
	"runtime"

	"github.com/opendumpling/opendumpling/globals"
	"github.com/opendumpling/opendumpling/version"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	labelGoVersion       = "goversion"
	labelGitRevision     = "revision"
	labelDumplingVersion = "version"
)

// buildInfoVec is a gauge metric whose value is always equal to 1 and whose
// labels contain the current go version, git revision, and dumpling version.
var buildInfoVec = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Namespace: globals.MetricNamespace,
		Name:      "build_info",
		Help:      "Gauge with labels describing go version, git revision hash, and Dumpling release version.",
	},
	[]string{labelGoVersion, labelGitRevision, labelDumplingVersion},
)

func getBuildInfoLabels() map[string]string {
	verInfo := version.Get()

	return map[string]string{
		labelGoVersion:       runtime.Version(),
		labelGitRevision:     verInfo.Revision,
		labelDumplingVersion: verInfo.Version,
	}
}

// InitializeBuildInfo registers the dumpling_build_info metric with its
// correct labels and sets its value to 1.
func InitializeBuildInfo(r prometheus.Registerer) {
	if r == nil {
		return
	}

	r.MustRegister(buildInfoVec)
	l := prometheus.Labels(getBuildInfoLabels())
	buildInfoVec.With(l).Set(float64(1))
}
