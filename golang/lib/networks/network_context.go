/*
 * Copyright (c) 2020 - present Kurtosis Technologies LLC.
 * All Rights Reserved.
 */

package networks

import (
	"context"
	"github.com/kurtosis-tech/kurtosis-libs/golang/lib/core_api_bindings"
	"github.com/kurtosis-tech/kurtosis-libs/golang/lib/services"
	"github.com/palantir/stacktrace"
	"github.com/sirupsen/logrus"
	"os"
	"path"
	"sync"
)

type PartitionID string

const (
	// This will alwyas resolve to the default partition ID (regardless of whether such a partition exists in the network,
	//  or it was repartitioned away)
	defaultPartitionId PartitionID = ""

	// This value - where the suite execution volume will be mounted on the testsuite container - is
	//  hardcoded inside Kurtosis Core
	suiteExVolMountpoint = "/suite-execution"
)

type serviceInfo struct {
	serviceContext *services.ServiceContext
	wrappingFunc func(serviceCtx *services.ServiceContext) services.Service
}

type NetworkContext struct {
	client core_api_bindings.TestExecutionServiceClient

	filesArtifactUrls map[services.FilesArtifactID]string

	// Mutex protecting access to the services map
	mutex *sync.Mutex

	services map[services.ServiceID]serviceInfo
}


/*
Creates a new NetworkContext object with the given parameters.

Args:
	client: The Kurtosis API client that the NetworkContext will use for modifying the state of the testnet
	filesArtifactUrls: The mapping of filesArtifactId -> URL for the artifacts that the testsuite will use
*/
func NewNetworkContext(
		client core_api_bindings.TestExecutionServiceClient,
		filesArtifactUrls map[services.FilesArtifactID]string) *NetworkContext {
	return &NetworkContext{
		mutex: &sync.Mutex{},
		client: client,
		filesArtifactUrls: filesArtifactUrls,
		services: map[services.ServiceID]serviceInfo{},
	}
}

/*
Adds a service to the network in the default partition with the given service ID

NOTE: If the network has been repartitioned and the default partition hasn't been preserved, you should use
	AddServiceToPartition instead.

Args:
	serviceId: The service ID that will be used to identify this node in the network.
	initializer: The Docker container initializer that contains the logic for starting the service

Return:
	service: The new service
*/
func (networkCtx *NetworkContext) AddService(
		serviceId services.ServiceID,
		initializer services.DockerContainerInitializer) (services.Service, services.AvailabilityChecker, error) {
	// Go mutexes aren't re-entrant, so we lock the mutex inside this call
	service, availabilityChecker, err := networkCtx.AddServiceToPartition(
		serviceId,
		defaultPartitionId,
		initializer)
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "An error occurred adding service '%v' to the network in the default partition", serviceId)
	}

	return service, availabilityChecker, nil
}

/*
Adds a service to the network with the given service ID, created using the given configuration ID.

NOTE: If the network hasn't been repartitioned yet, the PartitionID should be an empty string to add to the default
	partition.

Args:
	serviceId: The service ID that will be used to identify this node in the network.
	partitionId: The partition ID to add the service to
	initializer: The Docker container initializer that contains the logic for starting the service

Return:
	service.Service: The new service
*/
func (networkCtx *NetworkContext) AddServiceToPartition(
		serviceId services.ServiceID,
		partitionId PartitionID,
		initializer services.DockerContainerInitializer) (services.Service, services.AvailabilityChecker, error) {
	networkCtx.mutex.Lock()
	defer networkCtx.mutex.Unlock()

	ctx := context.Background()

	logrus.Tracef("Registering new service ID with Kurtosis API...")
	registerServiceArgs := &core_api_bindings.RegisterServiceArgs{
		ServiceId:       string(serviceId),
		PartitionId:     string(partitionId),
		FilesToGenerate: initializer.GetFilesToGenerate(),
	}
	registerServiceResp, err := networkCtx.client.RegisterService(ctx, registerServiceArgs)
	if err != nil {
		return nil, nil, stacktrace.Propagate(
			err,
			"An error occurred registering service with ID '%v' with the Kurtosis API",
			serviceId)
	}
	logrus.Tracef("New service successfully registered with Kurtosis API")

	suiteExVolMountpointOnService := initializer.GetTestVolumeMountpoint()
	generatedFilesRelativeFilepaths := registerServiceResp.GeneratedFilesRelativeFilepaths
	generatedFilesFps := map[string]*os.File{}
	generatedFilesAbsoluteFilepathsOnService := map[string]string{}
	for fileId, relativeFilepath := range generatedFilesRelativeFilepaths {
		absoluteFilepathOnTestsuite := path.Join(suiteExVolMountpoint, relativeFilepath)
		logrus.Debugf("Opening generated file at '%v' for writing...", absoluteFilepathOnTestsuite)
		fp, err := os.Create(absoluteFilepathOnTestsuite)
		if err != nil {
			return nil, nil, stacktrace.Propagate(
				err,
				"Could not open generated file '%v' for writing",
				fileId)
		}
		defer fp.Close()
		generatedFilesFps[fileId] = fp

		absoluteFilepathOnService := path.Join(suiteExVolMountpointOnService, relativeFilepath)
		generatedFilesAbsoluteFilepathsOnService[fileId] = absoluteFilepathOnService
	}

	logrus.Trace("Initializing generated files...")
	if err := initializer.InitializeGeneratedFiles(generatedFilesFps); err != nil {
		return nil, nil, stacktrace.Propagate(err, "An error occurred initializing the generated files")
	}
	logrus.Trace("Successfully initialized generated files")


	logrus.Tracef("Creating files artifact URL -> mount dirpaths map...")
	artifactUrlToMountDirpath := map[string]string{}
	for filesArtifactId, mountDirpath := range initializer.GetFilesArtifactMountpoints() {
		artifactUrl, found := networkCtx.filesArtifactUrls[filesArtifactId]
		if !found {
			return nil, nil, stacktrace.Propagate(
				err,
				"Service requested file artifact '%v', but the network" +
					"context doesn't have a URL for that file artifact; this is a bug with Kurtosis itself",
				filesArtifactId)
		}
		artifactUrlToMountDirpath[string(artifactUrl)] = mountDirpath
	}
	logrus.Tracef("Successfully created files artifact URL -> mount dirpaths map")

	logrus.Tracef("Creating start command for service...")
	serviceIpAddr := registerServiceResp.IpAddr
	entrypointArgs, cmdArgs, err := initializer.GetStartCommandOverrides(generatedFilesAbsoluteFilepathsOnService, serviceIpAddr)
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "Failed to get start command overrides")
	}
	logrus.Tracef("Successfully created start command for service")

	logrus.Tracef("Starting new service with Kurtosis API...")
	startServiceArgs := &core_api_bindings.StartServiceArgs{
		ServiceId:                   string(serviceId),
		DockerImage:                 initializer.GetDockerImage(),
		UsedPorts:                   initializer.GetUsedPorts(),
		EntrypointArgs:              entrypointArgs,
		CmdArgs:                     cmdArgs,
		DockerEnvVars:               map[string]string{}, // TODO actually support Docker env vars!
		SuiteExecutionVolMntDirpath: initializer.GetTestVolumeMountpoint(),
		FilesArtifactMountDirpaths:  artifactUrlToMountDirpath,
	}
	if _, err := networkCtx.client.StartService(ctx, startServiceArgs); err != nil {
		return nil, nil, stacktrace.Propagate(err, "An error occurred starting the service with the Kurtosis API")
	}
	logrus.Tracef("Successfully started service with Kurtosis API")

	logrus.Tracef("Creating service interface...")
	serviceContext := services.NewServiceContext(networkCtx.client, serviceId, serviceIpAddr)
	wrapWithInterface := initializer.GetServiceWrappingFunc();
	service := wrapWithInterface(serviceContext);
	logrus.Tracef("Successfully created service interface")

	networkCtx.services[serviceId] = serviceInfo{
		serviceContext: serviceContext,
		wrappingFunc:   wrapWithInterface,
	}

	availabilityChecker := services.NewDefaultAvailabilityChecker(serviceId, service)

	return service, availabilityChecker, nil
}

/*
Gets an interface for interacting with the service with the given ID, or returns an error if no service with that ID exists.
 */
func (networkCtx *NetworkContext) GetService(serviceId services.ServiceID) (services.Service, error) {
	networkCtx.mutex.Lock()
	defer networkCtx.mutex.Unlock()

	serviceInfo, found := networkCtx.services[serviceId]
	if !found {
		return nil, stacktrace.NewError("No service info found for ID '%v'", serviceId)
	}
	serviceContext := serviceInfo.serviceContext
	wrapWithInterface := serviceInfo.wrappingFunc
	service := wrapWithInterface(serviceContext);

	// TODO Stop recreating the service every time!!
	return service, nil
}

/*
Stops the container with the given service ID, and removes it from the network.
*/
func (networkCtx *NetworkContext) RemoveService(serviceId services.ServiceID, containerStopTimeoutSeconds uint64) error {
	networkCtx.mutex.Lock()
	defer networkCtx.mutex.Unlock()

	logrus.Debugf("Removing service '%v'...", serviceId)
	args := &core_api_bindings.RemoveServiceArgs{
		ServiceId:                   string(serviceId),
		// NOTE: This is kinda weird - when we remove a service we can never get it back so having a container
		//  stop timeout doesn't make much sense. It will make more sense when we can stop/start containers
		// Independent of adding/removing them from the network
		ContainerStopTimeoutSeconds: containerStopTimeoutSeconds,
	}
	if _, err := networkCtx.client.RemoveService(context.Background(), args); err != nil {
		return stacktrace.Propagate(err, "An error occurred removing service '%v' from the network", serviceId)
	}
	delete(networkCtx.services, serviceId)
	logrus.Debugf("Successfully removed service ID %v", serviceId)
	return nil
}

/*
Repartitions the network to the given state
 */
func (networkCtx *NetworkContext) RepartitionNetwork(
		partitionServices map[PartitionID]map[services.ServiceID]bool,
		partitionConnections map[PartitionID]map[PartitionID]*core_api_bindings.PartitionConnectionInfo,
		defaultConnection *core_api_bindings.PartitionConnectionInfo) error {
	networkCtx.mutex.Lock()
	defer networkCtx.mutex.Unlock()

	reqPartitionServices := map[string]*core_api_bindings.PartitionServices{}
	for partitionId, serviceIdSet := range partitionServices {
		serviceIdStrPseudoSet := map[string]bool{}
		for serviceId := range serviceIdSet {
			serviceIdStr := string(serviceId)
			serviceIdStrPseudoSet[serviceIdStr] = true
		}
		partitionIdStr := string(partitionId)
		reqPartitionServices[partitionIdStr] = &core_api_bindings.PartitionServices{
			ServiceIdSet: serviceIdStrPseudoSet,
		}
	}

	reqPartitionConns := map[string]*core_api_bindings.PartitionConnections{}
	for partitionAId, partitionAConnsMap := range partitionConnections {
		partitionAConnsStrMap := map[string]*core_api_bindings.PartitionConnectionInfo{}
		for partitionBId, connInfo := range partitionAConnsMap {
			partitionBIdStr := string(partitionBId)
			partitionAConnsStrMap[partitionBIdStr] = connInfo
		}
		partitionAConns := &core_api_bindings.PartitionConnections{
			ConnectionInfo: partitionAConnsStrMap,
		}
		partitionAIdStr := string(partitionAId)
		reqPartitionConns[partitionAIdStr] = partitionAConns
	}

	repartitionArgs := &core_api_bindings.RepartitionArgs{
		PartitionServices:    reqPartitionServices,
		PartitionConnections: reqPartitionConns,
		DefaultConnection:    defaultConnection,
	}
	if _, err := networkCtx.client.Repartition(context.Background(), repartitionArgs); err != nil {
		return stacktrace.Propagate(err, "An error occurred repartitioning the test network")
	}
	return nil
}
