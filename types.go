package verexa

type Phase string

const (
	PhaseInput  Phase = "input"
	PhaseOutput Phase = "output"
)

type Action string

const (
	ActionAllow  Action = "allow"
	ActionFlag   Action = "flag"
	ActionRedact Action = "redact"
	ActionBlock  Action = "block"
)

func (a Action) known() bool {
	switch a {
	case ActionAllow, ActionFlag, ActionRedact, ActionBlock:
		return true
	}
	return false
}

// orBlock fails closed on an action added to the server after this SDK
// shipped: a security gate must not let a verdict it does not recognise through.
func (a Action) orBlock() Action {
	if a.known() {
		return a
	}
	return ActionBlock
}

type FailMode string

const (
	FailOpen   FailMode = "open"
	FailClosed FailMode = "closed"
)

type CheckRequest struct {
	TraceID      string `json:"traceId"`
	Phase        Phase  `json:"phase"`
	Text         string `json:"text"`
	SystemPrompt string `json:"systemPrompt,omitempty"`
	Profile      string `json:"profile,omitempty"`
}

type DetectorOutcome struct {
	DetectorID string  `json:"detectorId"`
	Score      float64 `json:"score"`
	Action     Action  `json:"action"`
}

type JudgeMode string

const (
	JudgeSync  JudgeMode = "sync"
	JudgeAsync JudgeMode = "async"
)

type JudgeOutcome struct {
	// Action is empty while an async escalation is still pending.
	Action    Action    `json:"action,omitempty"`
	Score     *float64  `json:"score,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Mode      JudgeMode `json:"mode,omitempty"`
	Status    string    `json:"status,omitempty"`
	Trigger   string    `json:"trigger,omitempty"`
	LatencyMs *float64  `json:"latencyMs,omitempty"`
}

type StageStatus string

const (
	StageRan     StageStatus = "ran"
	StageSkipped StageStatus = "skipped"
	StageOff     StageStatus = "off"
	StageHit     StageStatus = "hit"
	StageMiss    StageStatus = "miss"
	StagePending StageStatus = "pending"
	StageFailed  StageStatus = "failed"
)

// StageOutcome is the per-tier trace of one check: what the cascade ran,
// skipped, and spent.
type StageOutcome struct {
	Tier      int         `json:"tier"`
	ID        string      `json:"id"`
	Label     string      `json:"label"`
	Status    StageStatus `json:"status"`
	Action    Action      `json:"action,omitempty"`
	Score     float64     `json:"score"`
	LatencyMs float64     `json:"latencyMs"`
	Detail    string      `json:"detail,omitempty"`
}

type CheckResponse struct {
	Action            Action            `json:"action"`
	Score             float64           `json:"score"`
	Detectors         []DetectorOutcome `json:"detectors"`
	Text              string            `json:"text"`
	LatencyMs         float64           `json:"latencyMs"`
	Degraded          bool              `json:"degraded"`
	DegradedDetectors []string          `json:"degradedDetectors,omitempty"`
	PlanHash          string            `json:"planHash"`
	Cached            bool              `json:"cached"`
	Stages            []StageOutcome    `json:"stages"`
	Judge             *JudgeOutcome     `json:"judge,omitempty"`
}

func (r *CheckResponse) normalizeActions() {
	r.Action = r.Action.orBlock()
	for i := range r.Detectors {
		r.Detectors[i].Action = r.Detectors[i].Action.orBlock()
	}
	for i := range r.Stages {
		if r.Stages[i].Action != "" {
			r.Stages[i].Action = r.Stages[i].Action.orBlock()
		}
	}
	if r.Judge != nil && r.Judge.Action != "" {
		r.Judge.Action = r.Judge.Action.orBlock()
	}
}
