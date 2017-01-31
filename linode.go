package linode

import (
	"errors"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/log"
	"github.com/docker/machine/libmachine/mcnflag"
	"github.com/docker/machine/libmachine/mcnutils"
	"github.com/docker/machine/libmachine/ssh"
	"github.com/docker/machine/libmachine/state"
	"github.com/taoh/linodego"
)

// Driver is the implementation of BaseDriver interface
type Driver struct {
	*drivers.BaseDriver
	client *linodego.Client

	APIKey     string
	IPAddress  string
	DockerPort int

	LinodeId    int
	LinodeLabel string

	DataCenterId   int
	PlanId         int
	PaymentTerm    int
	RootPassword   string
	SSHPort        int
	DistributionId int
	KernelId       int
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
		d.client = linodego.NewClient(d.APIKey, nil)
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
			Name:   "linode-api-key",
			Usage:  "Linode API Key",
			Value:  "",
			EnvVar: "LINODE_API_KEY",
		},
		mcnflag.StringFlag{
			EnvVar: "LINODE_ROOT_PASSWORD",
			Name:   "linode-root-pass",
			Usage:  "Root password",
		},
		mcnflag.StringFlag{
			EnvVar: "LINODE_LABEL",
			Name:   "linode-label",
			Usage:  "Linode label",
		},
		mcnflag.IntFlag{
			EnvVar: "LINODE_DATACENTER_ID",
			Name:   "linode-datacenter-id",
			Usage:  "Linode Data Center Id",
			Value:  2,
		},
		mcnflag.IntFlag{
			EnvVar: "LINODE_PLAN_ID",
			Name:   "linode-plan-id",
			Usage:  "Linode plan id",
			Value:  1,
		},
		mcnflag.IntFlag{
			EnvVar: "LINODE_PAYMENT_TERM",
			Name:   "linode-payment-term",
			Usage:  "Linode Payment term",
			Value:  1, // valid values: 1, 12, 24
		},
		mcnflag.IntFlag{
			EnvVar: "LINODE_SSH_PORT",
			Name:   "linode-ssh-port",
			Usage:  "Linode SSH Port",
			Value:  22,
		},
		mcnflag.IntFlag{
			EnvVar: "LINODE_DISTRIBUTION_ID",
			Name:   "linode-distribution-id",
			Usage:  "Linode Distribution Id",
			Value:  140, // Debian 8 (Ubuntu 16.04 LTD = 146)
		},
		mcnflag.IntFlag{
			EnvVar: "LINODE_KERNEL_ID",
			Name:   "linode-kernel-id",
			Usage:  "Linode Kernel Id",
			Value:  210, // default kernel, GRUB 2,
		},
		mcnflag.IntFlag{
			EnvVar: "LINODE_DOCKER_PORT",
			Name:   "linode-docker-port",
			Usage:  "Docker Port",
			Value:  2376,
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
	d.APIKey = flags.String("linode-api-key")
	d.DataCenterId = flags.Int("linode-datacenter-id")
	d.PlanId = flags.Int("linode-plan-id")
	d.PaymentTerm = flags.Int("linode-payment-term")
	d.RootPassword = flags.String("linode-root-pass")
	d.SSHPort = flags.Int("linode-ssh-port")
	d.DistributionId = flags.Int("linode-distribution-id")
	d.KernelId = flags.Int("linode-kernel-id")
	d.LinodeLabel = flags.String("linode-label")
	d.DockerPort = flags.Int("linode-docker-port")

	if d.APIKey == "" {
		return fmt.Errorf("linode driver requires the --linode-api-key option")
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
	linodeResponse, err := client.Linode.Create(
		d.DataCenterId,
		d.PlanId,
		d.PaymentTerm,
	)
	if err != nil {
		return err
	}

	d.LinodeId = linodeResponse.LinodeId.LinodeId
	log.Debugf("Linode created: %d", d.LinodeId)

	if d.LinodeLabel != "" {
		log.Debugf("Updating linode label to %s", d.LinodeLabel)
		_, err := client.Linode.Update(d.LinodeId, map[string]interface{}{"Label": d.LinodeLabel})
		if err != nil {
			return err
		}
	}

	linodeIPListResponse, err := client.Ip.List(d.LinodeId, -1)
	if err != nil {
		return err
	}
	for _, fullIpAddress := range linodeIPListResponse.FullIPAddresses {
		if fullIpAddress.IsPublic == 1 {
			d.IPAddress = fullIpAddress.IPAddress
		}
	}

	if d.IPAddress == "" {
		return errors.New("Linode IP Address is not found.")
	}

	log.Debugf("Created linode ID %d, IP address %s",
		d.LinodeId,
		d.IPAddress)

	// Deploy distribution
	args := make(map[string]string)
	args["rootPass"] = d.RootPassword
	args["rootSSHKey"] = publicKey
	distributionId := d.DistributionId

	log.Debug("Create disk")
	createDiskJobResponse, err := d.client.Disk.CreateFromDistribution(distributionId, d.LinodeId, "Primary Disk", 24576-256, args)

	if err != nil {
		return err
	}

	jobId := createDiskJobResponse.DiskJob.JobId
	diskId := createDiskJobResponse.DiskJob.DiskId
	log.Debugf("Linode create disk task :%d.", jobId)

	// wait until the creation is finished
	err = d.waitForJob(jobId, "Create Disk Task", 60)
	if err != nil {
		return err
	}

	// create swap
	log.Debug("Create swap disk")
	createDiskJobResponse, err = d.client.Disk.Create(d.LinodeId, "swap", "Swap Disk", 256, nil)
	if err != nil {
		return err
	}

	jobId = createDiskJobResponse.DiskJob.JobId
	swapDiskId := createDiskJobResponse.DiskJob.DiskId
	log.Debugf("Linode create swap disk task :%d.", jobId)

	// wait until the creation is finished
	err = d.waitForJob(jobId, "Create Swap Disk Task", 60)
	if err != nil {
		return err
	}

	// create config
	log.Debug("Create configuration")
	args2 := make(map[string]string)
	args2["DiskList"] = fmt.Sprintf("%d,%d", diskId, swapDiskId)
	args2["RootDeviceNum"] = "1"
	args2["RootDeviceRO"] = "true"
	args2["helper_distro"] = "true"
	kernelId := d.KernelId
	_, err = d.client.Config.Create(d.LinodeId, kernelId, "My Docker Machine Configuration", args2)

	if err != nil {
		return err
	}

	log.Debugf("Linode configuration created.")

	// Boot
	log.Debug("Booting")
	jobResponse, err := d.client.Linode.Boot(d.LinodeId, -1)
	if err != nil {
		return err
	}
	jobId = jobResponse.JobId.JobId
	log.Debugf("Booting linode, job id: %v", jobId)
	// wait for boot
	err = d.waitForJob(jobId, "Booting linode", 60)
	if err != nil {
		return err
	}

	log.Debug("Waiting for Machine Running...")
	if err := mcnutils.WaitForSpecific(drivers.MachineInState(d, state.Running), 120, 3*time.Second); err != nil {
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
	linodes, err := d.getClient().Linode.List(d.LinodeId)
	if err != nil {
		return state.Error, err
	}

	// Status flag values:
	// -2: Boot Failed
	// -1: Being Created
	//  0: Brand New
	//  1: Running
	//  2: Powered Off
	//  3: Shutting Down
	//  4: Saved to Disk
	//
	switch linodes.Linodes[0].Status {
	case -1, 0:
		return state.Starting, nil
	case 1:
		return state.Running, nil
	case -2, 2, 4:
		return state.Stopped, nil
	case 3:
		return state.Stopping, nil
	}
	return state.None, nil
}

func (d *Driver) Start() error {
	log.Debug("Start...")
	_, err := d.getClient().Linode.Boot(d.LinodeId, -1)
	return err
}

func (d *Driver) Stop() error {
	log.Debug("Stop...")
	_, err := d.getClient().Linode.Shutdown(d.LinodeId)
	return err
}

func (d *Driver) Remove() error {
	client := d.getClient()
	log.Debugf("Removing linode: %d", d.LinodeId)
	if _, err := client.Linode.Delete(d.LinodeId, true); err != nil {
		return err
	}
	return nil
}

func (d *Driver) Restart() error {
	log.Debug("Restarting...")
	_, err := d.getClient().Linode.Reboot(d.LinodeId, -1)
	return err
}

func (d *Driver) Kill() error {
	log.Debug("Killing...")
	_, err := d.getClient().Linode.Shutdown(d.LinodeId)
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

// waitForJob checks job status every 1 second until timeout
func (d *Driver) waitForJob(jobId int, jobName string, timeOutSeconds int) error {
	log.Debugf("Wait for job %s completion...", jobName)
	timeout := time.After(time.Duration(timeOutSeconds) * time.Second)
	tick := time.Tick(1000 * time.Millisecond)
	for {
		select {
		case <-timeout:
			return fmt.Errorf("Job %s timed out after %d seconds.", jobName, timeOutSeconds)
		case <-tick:
			{
				clientJobResponse, err := d.getClient().Job.List(d.LinodeId, jobId, false)
				if err != nil {
					return err
				}

				if len(clientJobResponse.Jobs) < 0 || clientJobResponse.Jobs[0].JobId != jobId {
					return fmt.Errorf("Job %s is not found.", jobName)
				}

				if clientJobResponse.Jobs[0].HostSuccess.String() == "1" {
					log.Debugf("Linode job %s completed.", jobName)
					return nil
				}
				// if not success, wait for next check
			}
		}
	}
}

// publicSSHKeyPath is always SSH Key Path appended with ".pub"
func (d *Driver) publicSSHKeyPath() string {
	return d.GetSSHKeyPath() + ".pub"
}
