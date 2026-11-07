package load_test

// Clock reads enclose the existing time.Now boundaries. They never redefine
// the workload timeline or extrapolate a wrapper-wide average clock frequency.
type attackLinuxClock struct {
	StartBeforeNS      int64  `json:"monotonic_start_before_ns"`
	DrainAfterNS       int64  `json:"monotonic_drain_after_ns"`
	BootID             string `json:"boot_id"`
	DrainBootID        string `json:"drain_boot_id"`
	TimeNamespace      string `json:"time_namespace"`
	DrainTimeNamespace string `json:"drain_time_namespace"`
	Error              string `json:"error,omitempty"`
}

type attackClockAnchor struct {
	monotonicNS   int64
	bootID        string
	timeNamespace string
}
