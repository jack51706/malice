package maldocker

import (
	"errors"
	"os"
	"time"

	"golang.org/x/net/context"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/container"
	"github.com/docker/engine-api/types/network"
	"github.com/docker/engine-api/types/strslice"
	"github.com/docker/go-connections/nat"
	"github.com/maliceio/malice/config"
	er "github.com/maliceio/malice/malice/errors"
	"github.com/maliceio/malice/malice/maldirs"

	"regexp"

	log "github.com/Sirupsen/logrus"
)

// StartContainer starts a malice docker container
func (client *Docker) StartContainer(cmd strslice.StrSlice, name string, image string, logs bool) (types.ContainerJSONBase, error) {

	if client.Ping() {
		if _, exists, _ := client.ContainerExists(name); exists {
			log.WithFields(log.Fields{
				"exisits": exists,
				"name":    name,
				"env":     config.Conf.Environment.Run,
			}).Info("Container is already running...")
			os.Exit(0)
		}
		if _, exists, _ := client.ImageExists(image); exists {
			log.WithFields(log.Fields{
				"exisits": exists,
				"env":     config.Conf.Environment.Run,
			}).Debugf("Image `%s` already pulled.", image)
		} else {
			log.WithFields(log.Fields{
				"exisits": exists,
				"env":     config.Conf.Environment.Run}).Debugf("Pulling Image `%s`", image)
			client.PullImage(image, "latest")
		}

		createContConf := &container.Config{
			Image: image,
			Cmd:   cmd,
			Env:   []string{"MALICE_VT_API=" + os.Getenv("MALICE_VT_API")},
		}
		// fmt.Printf("%#v\n", createContConf.Cmd)
		// fmt.Printf("%#v\n", createContConf.Env)
		hostConfig := &container.HostConfig{
			Binds:      []string{maldirs.GetSampledsDir() + ":/malware:ro"},
			Privileged: false,
		}
		networkingConfig := &network.NetworkingConfig{}

		contResponse, err := client.Client.ContainerCreate(createContConf, hostConfig, networkingConfig, name)
		if err != nil {
			log.WithFields(log.Fields{"env": config.Conf.Environment.Run}).Errorf("CreateContainer error = %s\n", err)
		}

		err = client.Client.ContainerStart(contResponse.ID)
		if err != nil {
			log.WithFields(log.Fields{"env": config.Conf.Environment.Run}).Errorf("StartContainer error = %s\n", err)
		}

		if logs {
			client.LogContainer(contResponse.ID)
		}

		contJSON, err := client.ContainerInspect(contResponse.ID)
		er.CheckError(err)
		return contJSON, err
	}
	return types.ContainerJSONBase{}, errors.New("Cannot connect to the Docker daemon. Is the docker daemon running on this host?")
}

// RemoveContainer removes the `cont` container unforcedly.
// If volumes is true, the associated volumes are removed with container.
// If links is true, the associated links are removed with container.
// If force is true, the container will be destroyed with extreme prejudice.
func (client *Docker) RemoveContainer(cont types.ContainerJSONBase, volumes bool, links bool, force bool) error {
	// check if container exists
	if plugin, exists, err := client.ContainerExists(cont.Name); exists {
		er.CheckError(err)
		log.WithFields(log.Fields{"env": config.Conf.Environment.Run}).Debug("Removing Plugin container: ", cont.Name)
		err := client.Client.ContainerRemove(types.ContainerRemoveOptions{
			ContainerID:   plugin.ID,
			RemoveVolumes: volumes,
			RemoveLinks:   links,
			Force:         force,
		})
		er.CheckError(err)
		return err
	}
	// container not found
	log.WithFields(log.Fields{"env": config.Conf.Environment.Run}).Error("Plugin container does not exist. Cannot remove.")
	return nil
}

// LogContainer tails container logs to terminal
func (client *Docker) LogContainer(contID string) {

	ctx, cancel := context.WithTimeout(context.Background(), config.Conf.Docker.Timeout*time.Second)
	defer cancel()

	options := types.ContainerLogsOptions{
		ContainerID: contID,
		ShowStdout:  true,
		ShowStderr:  true,
		// Since       string
		// Timestamps  bool
		Follow: true,
		// Tail        string
	}

	logs, err := client.Client.ContainerLogs(ctx, options)
	defer logs.Close()
	er.CheckError(err)

	_, err = stdcopy.StdCopy(os.Stdout, os.Stderr, logs)
}

// ContainerInspect returns types.ContainerJSON from Container ID
// if the container name exists, otherwise false.
func (client *Docker) ContainerInspect(id string) (types.ContainerJSONBase, error) {
	contJSON, err := client.Client.ContainerInspect(id)
	return *contJSON.ContainerJSONBase, err
}

// ContainerExists returns APIContainers containers list and true
// if the container name exists, otherwise false.
func (client *Docker) ContainerExists(name string) (types.Container, bool, error) {
	return client.ParseContainers(name, true)
}

// ContainerRunning returns APIContainers containers list and true
// if the container name exists and is running, otherwise false.
func (client *Docker) ContainerRunning(name string) (types.Container, bool, error) {
	return client.ParseContainers(name, false)
}

// ParseContainers parses the containers
func (client *Docker) ParseContainers(name string, all bool) (types.Container, bool, error) {
	// list containers
	log.WithFields(log.Fields{"env": config.Conf.Environment.Run}).Debug("Searching for container: ", name)
	containers, err := client.listContainers(all)
	if err != nil {
		return types.Container{}, false, err
	}
	// locate docker container that matches name
	r := regexp.MustCompile(name)
	if len(containers) != 0 {
		for _, container := range containers {
			for _, n := range container.Names {
				if r.MatchString(n) {
					log.WithFields(log.Fields{"env": config.Conf.Environment.Run}).Debug("Container FOUND: ", name)
					return container, true, nil
				}
			}
		}
	}
	log.WithFields(log.Fields{"env": config.Conf.Environment.Run}).Debug("Container NOT Found: ", name)
	return types.Container{}, false, nil
}

// listContainers returns array of types.Containers and error
func (client *Docker) listContainers(all bool) ([]types.Container, error) {
	options := types.ContainerListOptions{All: all}
	containers, err := client.Client.ContainerList(options)
	if err != nil {
		return nil, err
	}
	return containers, nil
}

// StartELK creates an ELK container from the image blacktop/elk
func (client *Docker) StartELK(logs bool) (types.ContainerJSONBase, error) {
	name := "elk"
	image := "blacktop/elk"

	if client.Ping() {
		if _, exists, _ := client.ContainerExists(name); exists {
			log.WithFields(log.Fields{
				"exisits": exists,
				"name":    name,
				"env":     config.Conf.Environment.Run,
				"url":     "http://" + client.ip,
			}).Info("Container is already running...")
			os.Exit(0)
		}
		if _, exists, _ := client.ImageExists(image); exists {
			log.WithFields(log.Fields{
				"exisits": exists,
				"env":     config.Conf.Environment.Run,
			}).Debugf("Image `%s` already pulled.", image)
		} else {
			log.WithFields(log.Fields{
				"exisits": exists,
				"env":     config.Conf.Environment.Run}).Debugf("Pulling Image `%s`", image)
			client.PullImage(image, "latest")
		}

		createContConf := &container.Config{
			Image: image,
		}
		portBindings := nat.PortMap{
			"80/tcp":   {{HostIP: "0.0.0.0", HostPort: "80"}},
			"9200/tcp": {{HostIP: "0.0.0.0", HostPort: "9200"}},
		}
		hostConfig := &container.HostConfig{
			PortBindings: portBindings,
			Privileged:   false,
		}
		networkingConfig := &network.NetworkingConfig{}

		contResponse, err := client.Client.ContainerCreate(createContConf, hostConfig, networkingConfig, name)
		if err != nil {
			log.WithFields(log.Fields{"env": config.Conf.Environment.Run}).Errorf("CreateContainer error = %s\n", err)
		}

		err = client.Client.ContainerStart(contResponse.ID)
		if err != nil {
			log.WithFields(log.Fields{"env": config.Conf.Environment.Run}).Errorf("StartContainer error = %s\n", err)
		}

		if logs {
			client.LogContainer(contResponse.ID)
		}

		contJSON, err := client.ContainerInspect(contResponse.ID)
		er.CheckError(err)
		return contJSON, err
	}
	return types.ContainerJSONBase{}, errors.New("Cannot connect to the Docker daemon. Is the docker daemon running on this host?")
}
