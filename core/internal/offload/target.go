package offload

import (
	"fmt"

	"UAmask/internal/firewall"
	"UAmask/internal/model"
)

type FirewallTargetOffloader struct {
	executor *firewall.Executor
	setName  string
	backend  string
}

func NewFirewallTargetOffloader(executor *firewall.Executor, setName, backend string) *FirewallTargetOffloader {
	return &FirewallTargetOffloader{
		executor: executor,
		setName:  setName,
		backend:  backend,
	}
}

func (o *FirewallTargetOffloader) Handle(signal model.OffloadSignal, completion func(error)) error {
	if o == nil || o.executor == nil {
		return fmt.Errorf("target offloader not configured")
	}

	ok := o.executor.TryEnqueueWithResult(firewall.BypassTarget{
		IP:      signal.Target.IP,
		Port:    signal.Target.Port,
		SetName: o.setName,
		Backend: o.backend,
		Timeout: signal.Timeout,
	}, completion)
	if !ok {
		return fmt.Errorf("enqueue target offload failed")
	}

	return nil
}

type NoopFlowOffloader struct{}

func (NoopFlowOffloader) Handle(model.OffloadSignal) error { return nil }
