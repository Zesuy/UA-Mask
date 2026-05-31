package firewall

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type CommandApplier struct {
	timeout time.Duration
}

func NewCommandApplier() *CommandApplier {
	return &CommandApplier{timeout: 10 * time.Second}
}

func (a *CommandApplier) ApplyBatch(items []BypassTarget) error {
	if len(items) == 0 {
		return nil
	}

	cmd := buildCommand(items)
	errChan := make(chan error, 1)

	go func() {
		output, err := cmd.CombinedOutput()
		if err != nil {
			err = fmt.Errorf("error: %v, output: %s", err, string(output))
		}
		errChan <- err
	}()

	select {
	case err := <-errChan:
		return err
	case <-time.After(a.timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		first := items[0]
		return fmt.Errorf("timeout executing batch for set %s (%s) with %d items", first.SetName, first.Backend, len(items))
	}
}

func buildCommand(items []BypassTarget) *exec.Cmd {
	first := items[0]
	if first.Backend == "nft" {
		args := []string{"add", "element", "inet", "fw4", first.SetName, "{"}
		elements := make([]string, 0, len(items))
		for _, item := range items {
			element := fmt.Sprintf("%s . %d", item.IP, item.Port)
			if item.Timeout > 0 {
				element += fmt.Sprintf(" timeout %ds", item.Timeout)
			}
			elements = append(elements, element)
		}
		args = append(args, strings.Join(elements, ", "))
		args = append(args, "}")
		return exec.Command("nft", args...)
	}

	cmd := exec.Command("ipset", "restore")
	var stdin strings.Builder
	for _, item := range items {
		if item.Timeout > 0 {
			fmt.Fprintf(&stdin, "add %s %s,%d timeout %d -exist\n", item.SetName, item.IP, item.Port, item.Timeout)
		} else {
			fmt.Fprintf(&stdin, "add %s %s,%d -exist\n", item.SetName, item.IP, item.Port)
		}
	}
	cmd.Stdin = strings.NewReader(stdin.String())
	return cmd
}
