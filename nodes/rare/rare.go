// Copyright 2020 Nokia
// Licensed under the BSD 3-Clause License.
// SPDX-License-Identifier: BSD-3-Clause

package rare

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
	log "github.com/sirupsen/logrus"
	"github.com/srl-labs/containerlab/nodes"
	"github.com/srl-labs/containerlab/types"
	"github.com/srl-labs/containerlab/utils"
)

var kindnames = []string{"rare"}

// Register registers the node in the NodeRegistry.
func Register(r *nodes.NodeRegistry) {
	r.Register(kindnames, func() nodes.Node {
		return new(rare)
	}, nil)
}

type rare struct {
	nodes.DefaultNode
}

func (n *rare) Init(cfg *types.NodeConfig, opts ...nodes.NodeOption) error {
	// Init DefaultNode
	n.DefaultNode = *nodes.NewDefaultNode(n)

	n.Cfg = cfg
	for _, o := range opts {
		o(n)
	}
	
	n.Cfg.Binds = append(n.Cfg.Binds, fmt.Sprint(filepath.Join(n.Cfg.LabDir, "run"), ":/rtr/run"))

	return nil
}

func (n *rare) genInterfacesEnv() {
	// Add eth0 manually
	n.Cfg.Env["CLAB_INTF_0"] = "eth0"

	for i, e := range n.Endpoints {
		ifaceName := e.GetIfaceName()
		envKey := fmt.Sprintf("CLAB_INTF_%d", i+1)
		n.Cfg.Env[envKey] = ifaceName
	}
}


func (n *rare) PreDeploy(ctx context.Context, params *nodes.PreDeployParams) error {
	// Generate the interface environment variables
	n.genInterfacesEnv()

	utils.CreateDirectory(n.Cfg.LabDir, 0777)

	_, err := n.LoadOrGenerateCertificate(params.Cert, params.TopologyName)
	if err != nil {
		return err
	}

	return n.createRAREFiles()
}

func (n *rare) PostDeploy(ctx context.Context, params *nodes.PostDeployParams) error {
	// disable IPv6 at runtime for every interface and globally
	// Retry loop to wait until the container is fully running
	for {
		// Check if the container is running
		cmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", n.Cfg.LongName)
		output, err := cmd.CombinedOutput()
		if err != nil || string(output) != "true\n" {
			log.Infof("Container %s not yet running, waiting...\n", n.Cfg.LongName)
			time.Sleep(2 * time.Second) // Wait a bit and retry
		} else {
			break // Container is running, exit the loop
		}
	}

	// Proceed with sysctl commands for individual interfaces if container exists
	for i := 0; ; i++ {
		envKey := fmt.Sprintf("CLAB_INTF_%d", i)
		if iface, ok := n.Cfg.Env[envKey]; ok {
			// Use os/exec to set the sysctl values after container start
			cmd := exec.Command("docker", "exec", n.Cfg.LongName, "sysctl", "-w", fmt.Sprintf("net.ipv6.conf.%s.disable_ipv6=1", iface))
			_, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("Failed to disable IPv6 on interface %s of container %s", iface, n.Cfg.LongName)
			}
		} else {
			break // No more interfaces in the environment variables
		}
	}

	// Disable IPv6 globally for all and default
	sysctlCommands := []string{
		"net.ipv6.conf.all.disable_ipv6=1",
		"net.ipv6.conf.default.disable_ipv6=1",
	}

	for _, sysctlCmd := range sysctlCommands {
		cmd := exec.Command("docker", "exec", n.Cfg.LongName, "sysctl", "-w", sysctlCmd)
		_, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("Failed to execute sysctl command %s on container %s", sysctlCmd, n.Cfg.LongName)
		}
	}

	return nil
}

func (n *rare) createRAREFiles() error {
	// Create the "run" directory that will be bind mounted to the rare node
	utils.CreateDirectory(filepath.Join(n.Cfg.LabDir, "run"), 0777)

	return nil
}
