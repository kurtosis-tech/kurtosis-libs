/*
 * Copyright (c) 2020 - present Kurtosis Technologies LLC.
 * All Rights Reserved.
 */

package networks_impl

import (
	"github.com/kurtosis-tech/example-microservice/api/api_service_client"
	"github.com/kurtosis-tech/example-microservice/datastore/datastore_service_client"
	"github.com/kurtosis-tech/kurtosis-client/golang/networks"
	"github.com/kurtosis-tech/kurtosis-client/golang/services"
	"github.com/kurtosis-tech/kurtosis-libs/golang/testsuite/services_impl/api"
	"github.com/kurtosis-tech/kurtosis-libs/golang/testsuite/services_impl/datastore"
	"github.com/palantir/stacktrace"
	"github.com/sirupsen/logrus"
	"strconv"
)

const (
	datastoreServiceId services.ServiceID = "datastore"
	apiServiceIdPrefix                    = "api-"

	waitForStartupDelayMilliseconds = 1000
	waitForStartupMaxNumPolls       = 15
)

//  A custom Network implementation is intended to make test-writing easier by wrapping low-level
//    NetworkContext calls with custom higher-level business logic
type TestNetwork struct {
	networkCtx                *networks.NetworkContext
	datastoreServiceImage     string
	apiServiceImage           string
	datastoreIPAddress        string
	datastorePort             int
	personModifyingApiClient  *api_service_client.APIClient
	personRetrievingApiClient *api_service_client.APIClient
	nextApiServiceId          int
}

func NewTestNetwork(networkCtx *networks.NetworkContext, datastoreServiceImage string, apiServiceImage string) *TestNetwork {
	return &TestNetwork{
		networkCtx:                networkCtx,
		datastoreServiceImage:     datastoreServiceImage,
		apiServiceImage:           apiServiceImage,
		datastoreIPAddress:        "",
		datastorePort:             0,
		personModifyingApiClient:  nil,
		personRetrievingApiClient: nil,
		nextApiServiceId:          0,
	}
}

//  Custom network implementations usually have a "setup" method (possibly parameterized) that is used
//   in the Test.Setup function of each test
func (network *TestNetwork) SetupDatastoreAndTwoApis() error {

	if network.personModifyingApiClient != nil || network.personRetrievingApiClient != nil {
		return stacktrace.NewError("Cannot add API services to network; one or more API services already exists")
	}

	configFactory := datastore.NewDatastoreContainerConfigFactory(network.datastoreServiceImage)
	datastoreServiceInfo, hostPortBindings, err := network.networkCtx.AddService(datastoreServiceId, configFactory)
	if err != nil {
		return stacktrace.Propagate(err, "An error occurred adding the datastore service")
	}

	datastoreClient := datastore_service_client.NewDatastoreClient(datastoreServiceInfo.GetIPAddress().String(), datastore.Port)

	err = datastoreClient.WaitForHealthy(waitForStartupMaxNumPolls, waitForStartupDelayMilliseconds)
	if err != nil {
		return stacktrace.Propagate(err, "An error occurred waiting for the datastore service to become available")
	}

	logrus.Infof("Added datastore service with host port bindings: %+v", hostPortBindings)

	network.datastoreIPAddress = datastoreServiceInfo.GetIPAddress().String()
	network.datastorePort = datastore.Port

	personModifyingApiClient, err := network.addApiService()
	if err != nil {
		return stacktrace.Propagate(err, "An error occurred adding the person-modifying API client")
	}
	network.personModifyingApiClient = personModifyingApiClient

	personRetrievingApiClient, err := network.addApiService()
	if err != nil {
		return stacktrace.Propagate(err, "An error occurred adding the person-retrieving API client")
	}
	network.personRetrievingApiClient = personRetrievingApiClient

	return nil
}

//  Custom network implementations will also usually have getters, to retrieve information about the
//   services created during setup
func (network *TestNetwork) GetPersonModifyingApiClient() (*api_service_client.APIClient, error) {
	if network.personModifyingApiClient == nil {
		return nil, stacktrace.NewError("No person-modifying API client exists")
	}
	return network.personModifyingApiClient, nil
}
func (network *TestNetwork) GetPersonRetrievingApiClient() (*api_service_client.APIClient, error) {
	if network.personRetrievingApiClient == nil {
		return nil, stacktrace.NewError("No person-retrieving API client exists")
	}
	return network.personRetrievingApiClient, nil
}

// ====================================================================================================
//                                       Private helper functions
// ====================================================================================================
func (network *TestNetwork) addApiService() (*api_service_client.APIClient, error) {

	serviceIdStr := apiServiceIdPrefix + strconv.Itoa(network.nextApiServiceId)
	network.nextApiServiceId = network.nextApiServiceId + 1
	serviceId := services.ServiceID(serviceIdStr)

	configFactory := api.NewApiContainerConfigFactory(network.apiServiceImage, network.datastoreIPAddress, network.datastorePort)
	apiServiceInfo, hostPortBindings, err := network.networkCtx.AddService(serviceId, configFactory)
	if err != nil {
		return nil, stacktrace.Propagate(err, "An error occurred adding the API service")
	}

	apiClient := api_service_client.NewAPIClient(apiServiceInfo.GetIPAddress().String(), api.Port)

	err = apiClient.WaitForHealthy(waitForStartupMaxNumPolls, waitForStartupDelayMilliseconds)
	if err != nil {
		return nil, stacktrace.Propagate(err, "An error occurred waiting for the api service to become available")
	}

	logrus.Infof("Added API service with host port bindings: %+v", hostPortBindings)
	return apiClient, nil
}
