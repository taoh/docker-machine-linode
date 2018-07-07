package linode

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net"

	"github.com/chiefy/linodego"
	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/log"
	"github.com/docker/machine/libmachine/mcnflag"
	"github.com/docker/machine/libmachine/ssh"
	"github.com/docker/machine/libmachine/state"
)

// Driver is the implementation of BaseDriver interface
type Driver struct {
	*drivers.BaseDriver
	client *linodego.Client

	APIToken   string
	IPAddress  string
	DockerPort int

	InstanceID    int
	InstanceLabel string

	Region         string
	InstanceType   string
	RootPassword   string
	SSHPort        int
	InstanceImage  string
	InstanceKernel string
	SwapSize       int
}

// NewDriver
func NewDriver(hostName, storePath string) *Driver {
	return &Driver{
		BaseDriver: &drivers.BaseDriver{
			MachineName: hostName,
			StorePath:   storePath,
		},
	}
}

// Get Linode Client
func (d *Driver) getClient() *linodego.Client {
	if d.client == nil {
		client := linodego.NewClient(&d.APIToken, nil)
		d.client = &client
	}
	return d.client
}

func (d *Driver) DriverName() string {
	return "linode"
}

func (d *Driver) GetSSHHostname() (string, error) {
	return d.GetIP()
}

// Get IP Address for the Linode. Note that currently the IP Address
// is cached
func (d *Driver) GetIP() (string, error) {
	if d.IPAddress == "" {
		return "", fmt.Errorf("IP address is not set")
	}
	return d.IPAddress, nil
}

func (d *Driver) GetCreateFlags() []mcnflag.Flag {
	return []mcnflag.Flag{
		mcnflag.StringFlag{
			EnvVar: "LINODE_TOKEN",
			Name:   "linode-api-token",
			Usage:  "Linode API Token",
			Value:  "",
		},
		mcnflag.StringFlag{
			EnvVar: "LINODE_ROOT_PASSWORD",
			Name:   "linode-root-pass",
			Usage:  "Root Password",
		},
		mcnflag.StringFlag{
			EnvVar: "LINODE_LABEL",
			Name:   "linode-label",
			Usage:  "Linode Instance Label",
		},
		mcnflag.StringFlag{
			EnvVar: "LINODE_REGION",
			Name:   "linode-region",
			Usage:  "Linode Region",
			Value:  "us-east", // "us-central", "ap-south", "eu-central", ...
		},
		mcnflag.StringFlag{
			EnvVar: "LINODE_INSTANCE_TYPE",
			Name:   "linode-type",
			Usage:  "Linode Instance Type",
			Value:  "g6-standard-4", // "g6-nanode-1", g6-highmem-2, ...
		},
		mcnflag.IntFlag{
			EnvVar: "LINODE_SSH_PORT",
			Name:   "linode-ssh-port",
			Usage:  "Linode Instance SSH Port",
			Value:  22,
		},
		mcnflag.StringFlag{
			EnvVar: "LINODE_IMAGE",
			Name:   "linode-image",
			Usage:  "Linode Instance Image",
			Value:  "linode/debian8", // "linode/ubuntu18.04", "linode/arch", ...
		},
		mcnflag.StringFlag{
			EnvVar: "LINODE_KERNEL",
			Name:   "linode-kernel",
			Usage:  "Linode Instance Kernel",
			Value:  "linode/grub2", // linode/latest-64bit, ..
		},
		mcnflag.IntFlag{
			EnvVar: "LINODE_DOCKER_PORT",
			Name:   "linode-docker-port",
			Usage:  "Docker Port",
			Value:  2376,
		},
		mcnflag.IntFlag{
			EnvVar: "LINODE_SWAP_SIZE",
			Name:   "linode-swap-size",
			Usage:  "Linode Instance Swap Size (MB)",
			Value:  512,
		},
	}
}

func (d *Driver) GetSSHUsername() string {
	if d.SSHUser == "" {
		d.SSHUser = "root"
	}

	return d.SSHUser
}

func (d *Driver) SetConfigFromFlags(flags drivers.DriverOptions) error {
	d.APIToken = flags.String("linode-token")
	d.Region = flags.String("linode-region")
	d.InstanceType = flags.String("linode-type")
	d.RootPassword = flags.String("linode-root-pass")
	d.SSHPort = flags.Int("linode-ssh-port")
	d.InstanceImage = flags.String("linode-image")
	d.InstanceKernel = flags.String("linode-kernel")
	d.InstanceLabel = flags.String("linode-label")
	d.SwapSize = flags.Int("linode-swap-size")
	d.DockerPort = flags.Int("linode-docker-port")

	if d.APIToken == "" {
		return fmt.Errorf("linode driver requires the --linode-token option")
	}

	if d.RootPassword == "" {
		return fmt.Errorf("linode driver requires the --linode-root-pass option")
	}

	return nil
}

func (d *Driver) PreCreateCheck() error {
	return nil
}

func (d *Driver) Create() error {
	log.Debug("Creating Linode machine instance...")

	publicKey, err := d.createSSHKey()
	if err != nil {
		return err
	}

	client := d.getClient()

	// Create a linode
	log.Debug("Creating linode instance")
	createOpts := linodego.InstanceCreateOptions{
		Region:         d.Region,
		Type:           d.InstanceType,
		Label:          d.InstanceLabel,
		RootPass:       d.RootPassword,
		AuthorizedKeys: []string{publicKey},
		Image:          d.InstanceImage,
		SwapSize:       &d.SwapSize,
	}

	linode, err := client.CreateInstance(&createOpts)
	if err != nil {
		return err
	}

	for _, address := range linode.IPv4 {
		if private := privateIP(*address); !private {
			d.IPAddress = address.String()
			break
		}
	}

	if d.IPAddress == "" {
		return errors.New("Linode IP Address is not found")
	}

	log.Debugf("Created Linode Instance ID %d, IP address %s",
		d.InstanceID,
		d.IPAddress)

	if err != nil {
		return err
	}

	log.Debug("Waiting for Machine Running...")
	if err := linodego.WaitForInstanceStatus(client, d.InstanceID, linodego.InstanceRunning, 120); err != nil {
		return fmt.Errorf("wait for machine running failed: %s", err)
	}

	return nil
}

func (d *Driver) GetURL() (string, error) {
	ip, err := d.GetIP()
	if err != nil {
		return "", err
	}
	if ip == "" {
		return "", nil
	}

	return fmt.Sprintf("tcp://%s:%d", ip, d.DockerPort), nil
}

func (d *Driver) GetState() (state.State, error) {
	linode, err := d.getClient().GetInstance(d.InstanceID)
	if err != nil {
		return state.Error, err
	}

	switch linode.Status {
	case linodego.InstanceRunning:
		return state.Running, nil
	case linodego.InstanceOffline,
		linodego.InstanceRebuilding,
		linodego.InstanceMigrating:
		return state.Stopped, nil
	case linodego.InstanceShuttingDown, linodego.InstanceDeleting:
		return state.Stopping, nil
	case linodego.InstanceProvisioning,
		linodego.InstanceRebooting,
		linodego.InstanceBooting,
		linodego.InstanceCloning,
		linodego.InstanceRestoring:
		return state.Starting, nil

	}

	// deleting, migrating, rebuilding, cloning, restoring ...
	return state.None, nil
}

func (d *Driver) Start() error {
	log.Debug("Start...")
	_, err := d.getClient().BootInstance(d.InstanceID, 0)
	return err
}

func (d *Driver) Stop() error {
	log.Debug("Stop...")
	_, err := d.getClient().ShutdownInstance(d.InstanceID)
	return err
}

func (d *Driver) Remove() error {
	client := d.getClient()
	log.Debugf("Removing linode: %d", d.InstanceID)
	if err := client.DeleteInstance(d.InstanceID); err != nil {
		return err
	}
	return nil
}

func (d *Driver) Restart() error {
	log.Debug("Restarting...")
	_, err := d.getClient().RebootInstance(d.InstanceID)
	return err
}

func (d *Driver) Kill() error {
	log.Debug("Killing...")
	_, err := d.getClient().ShutdownInstance(d.InstanceID)
	return err
}

func (d *Driver) createSSHKey() (string, error) {
	if err := ssh.GenerateSSHKey(d.GetSSHKeyPath()); err != nil {
		return "", err
	}

	publicKey, err := ioutil.ReadFile(d.publicSSHKeyPath())
	if err != nil {
		return "", err
	}

	return string(publicKey), nil
}

// publicSSHKeyPath is always SSH Key Path appended with ".pub"
func (d *Driver) publicSSHKeyPath() string {
	return d.GetSSHKeyPath() + ".pub"
}

// privateIP determines if an IP is for private use (RFC1918)
// https://stackoverflow.com/a/41273687
func privateIP(ip net.IP) bool {
	private := false
	_, private24BitBlock, _ := net.ParseCIDR("10.0.0.0/8")
	_, private20BitBlock, _ := net.ParseCIDR("172.16.0.0/12")
	_, private16BitBlock, _ := net.ParseCIDR("192.168.0.0/16")
	private = private24BitBlock.Contains(ip) || private20BitBlock.Contains(ip) || private16BitBlock.Contains(ip)
	return private
}
