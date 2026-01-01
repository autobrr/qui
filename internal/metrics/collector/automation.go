package collector

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

type AutomationCollector struct {
	RuleRunTotal                            *prometheus.CounterVec
	RuleRunMatchedTrackers                  *prometheus.CounterVec
	RuleRunSpeedApplied                     *prometheus.CounterVec
	RuleRunSpeedConditionNotMet             *prometheus.CounterVec
	RuleRunShareApplied                     *prometheus.CounterVec
	RuleRunShareConditionNotMet             *prometheus.CounterVec
	RuleRunPauseApplied                     *prometheus.CounterVec
	RuleRunPauseConditionNotMet             *prometheus.CounterVec
	RuleRunTagConditionMet                  *prometheus.CounterVec
	RuleRunTagConditionNotMet               *prometheus.CounterVec
	RuleRunTagSkippedMissingUnregisteredSet *prometheus.CounterVec
	RuleRunCategoryApplied                  *prometheus.CounterVec
	RuleRunCategoryConditionNotMetOrBlocked *prometheus.CounterVec
	RuleRunDeleteApplied                    *prometheus.CounterVec
	RuleRunDeleteConditionNotMet            *prometheus.CounterVec
	RuleRunDeleteNotCompleted               *prometheus.CounterVec
}

var automationRuleRunLabels = []string{"instance_id", "rule_id", "rule_name"}

func GetAutomationRuleRunLabels(instanceID int, ruleID int, ruleName string) []string {
	return []string{strconv.Itoa(instanceID), strconv.Itoa(ruleID), ruleName}
}

func NewAutomationCollector(r *prometheus.Registry) *AutomationCollector {
	m := &AutomationCollector{
		RuleRunTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_total",
			Help: "Total number of automation rule runs",
		}, automationRuleRunLabels),
		RuleRunMatchedTrackers: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_matched_trackers",
			Help: "Number of matched trackers by automation rule",
		}, automationRuleRunLabels),
		RuleRunSpeedApplied: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_speed_applied",
			Help: "Number of speed applied by automation rule",
		}, automationRuleRunLabels),
		RuleRunSpeedConditionNotMet: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_speed_condition_not_met",
			Help: "Number of speed condition not met by automation rule",
		}, automationRuleRunLabels),
		RuleRunShareApplied: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_share_applied",
			Help: "Number of share applied by automation rule",
		}, automationRuleRunLabels),
		RuleRunShareConditionNotMet: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_share_condition_not_met",
			Help: "Number of share condition not met by automation rule",
		}, automationRuleRunLabels),
		RuleRunPauseApplied: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_pause_applied",
			Help: "Number of pause applied by automation rule",
		}, automationRuleRunLabels),
		RuleRunPauseConditionNotMet: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_pause_condition_not_met",
			Help: "Number of pause condition not met by automation rule",
		}, automationRuleRunLabels),
		RuleRunTagConditionMet: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_tag_condition_met",
			Help: "Number of tag condition met by automation rule",
		}, automationRuleRunLabels),
		RuleRunTagConditionNotMet: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_tag_condition_not_met",
			Help: "Number of tag condition not met by automation rule",
		}, automationRuleRunLabels),
		RuleRunTagSkippedMissingUnregisteredSet: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_tag_skipped_missing_unregistered_set",
			Help: "Number of tag skipped missing unregistered set by automation rule",
		}, automationRuleRunLabels),
		RuleRunCategoryApplied: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_category_applied",
			Help: "Number of category applied by automation rule",
		}, automationRuleRunLabels),
		RuleRunCategoryConditionNotMetOrBlocked: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_category_condition_not_met_or_blocked",
			Help: "Number of category condition not met or blocked by automation rule",
		}, automationRuleRunLabels),
		RuleRunDeleteApplied: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_delete_applied",
			Help: "Number of delete applied by automation rule",
		}, automationRuleRunLabels),
		RuleRunDeleteConditionNotMet: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_delete_condition_not_met",
			Help: "Number of delete condition not met by automation rule",
		}, automationRuleRunLabels),
		RuleRunDeleteNotCompleted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "qui_automation_rule_run_delete_not_completed",
			Help: "Number of delete not completed by automation rule",
		}, automationRuleRunLabels),
	}

	r.MustRegister(m.RuleRunTotal)
	r.MustRegister(m.RuleRunMatchedTrackers)
	r.MustRegister(m.RuleRunSpeedApplied)
	r.MustRegister(m.RuleRunSpeedConditionNotMet)
	r.MustRegister(m.RuleRunShareApplied)
	r.MustRegister(m.RuleRunShareConditionNotMet)
	r.MustRegister(m.RuleRunPauseApplied)
	r.MustRegister(m.RuleRunPauseConditionNotMet)
	r.MustRegister(m.RuleRunTagConditionMet)
	r.MustRegister(m.RuleRunTagConditionNotMet)
	r.MustRegister(m.RuleRunTagSkippedMissingUnregisteredSet)
	r.MustRegister(m.RuleRunCategoryApplied)
	r.MustRegister(m.RuleRunCategoryConditionNotMetOrBlocked)
	r.MustRegister(m.RuleRunDeleteApplied)
	r.MustRegister(m.RuleRunDeleteConditionNotMet)
	r.MustRegister(m.RuleRunDeleteNotCompleted)
	return m
}
