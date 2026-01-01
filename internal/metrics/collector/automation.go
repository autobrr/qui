package collector

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

type AutomationCollector struct {
	RuleRunTotal                *prometheus.CounterVec
	RuleRunTorrentsMatchedTotal *prometheus.CounterVec
	RuleRunActionTotal          *prometheus.CounterVec
}

func NewAutomationCollector(r *prometheus.Registry) *AutomationCollector {
	m := &AutomationCollector{
		RuleRunTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "qui",
			Subsystem: "automation",
			Name:      "rule_run_total",
			Help:      "Total number of automation rule runs",
		}, []string{"instance_id", "instance_name", "rule_id", "rule_name"}),
		RuleRunTorrentsMatchedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "qui",
			Subsystem: "automation",
			Name:      "rule_run_torrents_matched_total",
			Help:      "Total number of torrents that matched the trackers in the rule",
		}, []string{"instance_id", "instance_name", "rule_id", "rule_name"}),
		RuleRunActionTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "qui",
			Subsystem: "automation",
			Name:      "rule_run_action_total",
			Help:      "Total number of automation rule actions",
		}, []string{"instance_id", "instance_name", "rule_id", "rule_name", "action"}),
	}

	r.MustRegister(m.RuleRunTotal)
	r.MustRegister(m.RuleRunTorrentsMatchedTotal)
	r.MustRegister(m.RuleRunActionTotal)
	return m
}

func (m *AutomationCollector) GetAutomationRuleRunTotal(instanceID int, instanceName string, ruleID int, ruleName string) prometheus.Counter {
	return m.RuleRunTotal.With(prometheus.Labels{
		"instance_id":   strconv.Itoa(instanceID),
		"instance_name": instanceName,
		"rule_id":       strconv.Itoa(ruleID),
		"rule_name":     ruleName,
	})
}

func (m *AutomationCollector) GetAutomationRuleRunTorrentsMatchedTotal(instanceID int, instanceName string, ruleID int, ruleName string) prometheus.Counter {
	return m.RuleRunTorrentsMatchedTotal.With(prometheus.Labels{
		"instance_id":   strconv.Itoa(instanceID),
		"instance_name": instanceName,
		"rule_id":       strconv.Itoa(ruleID),
		"rule_name":     ruleName,
	})
}

func (m *AutomationCollector) GetAutomationRuleRunActionTotal(instanceID int, instanceName string, ruleID int, ruleName string) *prometheus.CounterVec {
	return m.RuleRunActionTotal.MustCurryWith(prometheus.Labels{
		"instance_id":   strconv.Itoa(instanceID),
		"instance_name": instanceName,
		"rule_id":       strconv.Itoa(ruleID),
		"rule_name":     ruleName,
	})
}
