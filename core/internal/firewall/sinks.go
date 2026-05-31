package firewall

type ExecutorSink struct {
	executor *Executor
}

func NewExecutorSink(executor *Executor) *ExecutorSink {
	return &ExecutorSink{executor: executor}
}

func (s *ExecutorSink) Enqueue(target BypassTarget) {
	if s == nil || s.executor == nil {
		return
	}
	s.executor.Enqueue(target)
}

type WhitelistBypassSink struct {
	executor *Executor
	setName  string
	backend  string
}

func NewWhitelistBypassSink(executor *Executor, setName, backend string) *WhitelistBypassSink {
	return &WhitelistBypassSink{
		executor: executor,
		setName:  setName,
		backend:  backend,
	}
}

func (s *WhitelistBypassSink) EnqueueBypass(ip string, port int, timeout int) {
	if s == nil || s.executor == nil || s.setName == "" || s.backend == "" {
		return
	}

	s.executor.Enqueue(BypassTarget{
		IP:      ip,
		Port:    port,
		SetName: s.setName,
		Backend: s.backend,
		Timeout: timeout,
	})
}

type NoopReporter struct{}

func (NoopReporter) ReportHTTP(string, int)    {}
func (NoopReporter) ReportNonHTTP(string, int) {}
