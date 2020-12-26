/*
 * Copyright (c) 2020 - present Kurtosis Technologies LLC.
 * All Rights Reserved.
 */

package networks

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/kurtosis-tech/kurtosis-go/lib/kurtosis_service"
	"github.com/kurtosis-tech/kurtosis-go/lib/services"
	"github.com/palantir/stacktrace"
	"github.com/sirupsen/logrus"
	"os"
	"path/filepath"
)

const (
	ipPlaceholder = "KURTOSISSERVICEIP"
)

type NetworkContext struct {
	kurtosisService kurtosis_service.KurtosisService

	// The dirpath ON THE SUITE CONTAINER where the suite execution volume is mounted
	suiteExecutionVolumeDirpath string

	// Filepath to the services directory, RELATIVE to the root of the suite execution volume root
	servicesRelativeDirpath string

	// The user-defined interfaces for interacting with the node.
	// NOTE: these will need to be casted to the appropriate interface becaus Go doesn't yet have generics!
	services map[services.ServiceID]services.Service
}


/*
Creates a new NetworkContext object with the given parameters.

Args:
	kurtosisService: The Docker manager that will be used for manipulating the Docker engine during test network modification.
	suiteExecutionVolumeDirpath: The path ON THE TEST SUITE CONTAINER where the suite execution volume is mounted
	servicesRelativeDirpath: The dirpath where directories for each new service will be created to store file IO, which
		is RELATIVE to the root of the suite execution volume!
*/
func NewNetworkContext(
		kurtosisService kurtosis_service.KurtosisService,
		suiteExecutionVolumeDirpath string,
		servicesRelativeDirpath string) *NetworkContext {
	return &NetworkContext{
		kurtosisService: kurtosisService,
		suiteExecutionVolumeDirpath: suiteExecutionVolumeDirpath,
		servicesRelativeDirpath: servicesRelativeDirpath,
		services: map[services.ServiceID]services.Service{},
	}
}

// Gets the number of nodes in the network
func (networkCtx *NetworkContext) GetSize() int {
	return len(networkCtx.services)
}

/*
Adds a service to the network with the given service ID, created using the given configuration ID.

Args:
	serviceId: The service ID that will be used to identify this node in the network.
	initializer: The Docker container initializer that contains the logic for starting the service

Return:
	services.Service: The new service
	services.AvailabilityChecker: An availability checker which can be used to wait until the service is available, if desired
*/
func (networkCtx *NetworkContext) AddService(
		serviceId services.ServiceID,
		initializer services.DockerContainerInitializer) (services.Service, services.AvailabilityChecker, error) {
	if _, exists := networkCtx.services[serviceId]; exists {
		return nil, nil, stacktrace.NewError("Service ID %s already exists in the network", serviceId)
	}

	serviceDirname := fmt.Sprintf("%v-%v", serviceId, uuid.New().String())
	serviceRelativeDirpath := filepath.Join(networkCtx.servicesRelativeDirpath, serviceDirname)

	logrus.Trace("Creating directory within test volume for service...")
	testSuiteServiceDirpath := filepath.Join(networkCtx.suiteExecutionVolumeDirpath, serviceRelativeDirpath)
	err := os.Mkdir(testSuiteServiceDirpath, os.ModeDir)
	if err != nil {
		return nil, nil, stacktrace.Propagate(
			err,
			"An error occurred creating the new service's directory in the volume at filepath '%v' on the testsuite",
			testSuiteServiceDirpath)
	}
	logrus.Tracef("Successfully created directory for service: %v", testSuiteServiceDirpath)

	mountServiceDirpath := filepath.Join(initializer.GetTestVolumeMountpoint(), serviceRelativeDirpath)

	logrus.Trace("Initializing files needed for service...")
	requestedFiles := initializer.GetFilesToMount()
	osFiles := make(map[string]*os.File)
	mountFilepaths := make(map[string]string)
	for fileId, _ := range requestedFiles {
		filename := fmt.Sprintf("%v-%v", fileId, uuid.New().String())
		testSuiteFilepath := filepath.Join(testSuiteServiceDirpath, filename)
		fp, err := os.Create(testSuiteFilepath)
		if err != nil {
			return nil, nil, stacktrace.Propagate(
				err,
				"Could not create new file for requested file ID '%v'",
				fileId)
		}
		defer fp.Close()
		osFiles[fileId] = fp
		mountFilepaths[fileId] = filepath.Join(mountServiceDirpath, filename)
	}
	// NOTE: If we need the IP address when initializing mounted files, we'll need to rejigger the Kurtosis API
	//  container so that it can do a "pre-registration" - dole out an IP address before actually starting the container
	if err := initializer.InitializeMountedFiles(osFiles); err != nil {
		return nil, nil, stacktrace.Propagate(err, "An error occurred initializing the files before service start")
	}
	logrus.Tracef("Successfully initialized files needed for service")

	logrus.Tracef("Creating start command for service...")
	startCmdArgs, err := initializer.GetStartCommand(mountFilepaths, ipPlaceholder)
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "Failed to create start command")
	}
	logrus.Tracef("Successfully created start command for service")

	logrus.Tracef("Calling to Kurtosis API to create service...")
	dockerImage := initializer.GetDockerImage()
	ipAddr, err := networkCtx.kurtosisService.AddService(
		string(serviceId),
		dockerImage,
		initializer.GetUsedPorts(),
		ipPlaceholder,
		startCmdArgs,
		make(map[string]string),
		initializer.GetTestVolumeMountpoint())
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "Could not add service for Docker image %v", dockerImage)
	}
	logrus.Tracef("Kurtosis API returned IP for new service: %v", ipAddr)

	logrus.Tracef("Getting service from IP...")
	service := initializer.GetService(serviceId, ipAddr)
	logrus.Tracef("Successfully got service from IP")

	networkCtx.services[serviceId] = service

	availabilityChecker := services.NewDefaultAvailabilityChecker(serviceId, service)

	return service, availabilityChecker, nil
}

/*
Gets the node information for the service with the given service ID.
*/
func (networkCtx *NetworkContext) GetService(serviceId services.ServiceID) (services.Service, error) {
	service, found := networkCtx.services[serviceId]
	if !found {
		return nil, stacktrace.NewError("No service with ID '%v' exists in the network", serviceId)
	}

	return service, nil
}

/*
Stops the container with the given service ID, and removes it from the network.
*/
func (networkCtx *NetworkContext) RemoveService(serviceId services.ServiceID, containerStopTimeoutSeconds int) error {
	_, found := networkCtx.services[serviceId]
	if !found {
		return stacktrace.NewError("No service with ID %v found", serviceId)
	}

	logrus.Debugf("Removing service '%v'...", serviceId)
	delete(networkCtx.services, serviceId)

	// Make a best-effort attempt to stop the container
	err := networkCtx.kurtosisService.RemoveService(string(serviceId), containerStopTimeoutSeconds)
	if err != nil {
		return stacktrace.Propagate(err,
			"An error occurred removing service with ID '%v'",
			serviceId)
	}
	logrus.Debugf("Successfully removed service ID %v", serviceId)
	return nil
}
