package main

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/sirupsen/logrus"
)

func AddToFirewallSet(ip string, port int, setName, fwType string) {
	if ip == "" || setName == "" {
		return
	}

	go func() {
		var cmd *exec.Cmd
		if fwType == "nft" {
			// nft add element inet fw4 <setName> { <ip> . <port> }
			portStr := fmt.Sprintf("%d", port)
			cmd = exec.Command("nft", "add", "element", "inet", "fw4", setName, "{", ip, ".", portStr, "}")
		} else {
			// ipset add <setName> <ip>,<port>
			ipPort := fmt.Sprintf("%s,%d", ip, port)
			cmd = exec.Command("ipset", "add", setName, ipPort)
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
				logrus.Debugf("Added IP %s to firewall bypass set %s (%s)", ip, setName, fwType)
			}
		case <-time.After(1 * time.Second):
			logrus.Warnf("Timeout adding IP %s to firewall set %s (%s)", ip, setName, fwType)
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		}
	}()
}
