package main

import (
	"os/exec"
	"time"

	"github.com/sirupsen/logrus"
)

func AddToFirewallSet(ip, setName, fwType string) {
	if ip == "" || setName == "" {
		return
	}

	go func() {
		var cmd *exec.Cmd
		if fwType == "nft" {
			cmd = exec.Command("nft", "add", "element", "inet", "fw4", setName, "{", ip, "}")
		} else {
			cmd = exec.Command("ipset", "add", setName, ip)
		}

		errChan := make(chan error, 1)
		go func() {
			errChan <- cmd.Run()
		}()

		select {
		case err := <-errChan:
			if err != nil {
				logrus.Debugf("Failed to add IP %s to firewall set %s (%s): %v", ip, setName, fwType, err)
			} else {
				logrus.Infof("Added IP %s to firewall bypass set %s (%s)", ip, setName, fwType)
			}
		case <-time.After(1 * time.Second):
			logrus.Warnf("Timeout adding IP %s to firewall set %s (%s)", ip, setName, fwType)
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		}
	}()
}
