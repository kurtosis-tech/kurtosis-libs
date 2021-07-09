package nginx_static

import (
	"github.com/kurtosis-tech/kurtosis-client/golang/services"
	"strconv"
)

const (
	dockerImage = "flashspys/nginx-static"

	ListenPort = 80

	testVolumeMountpoint = "/test-volume"

	nginxStaticFilesDirpath = "/static"
)

/*
A config factory implementation to launch an NginxStaticService pre-initialized with the contents of
	the given files artifact
*/
type NginxStaticContainerConfigFactory struct {
	filesArtifactId services.FilesArtifactID
}

// NOTE: The files artifact ID is optional; if it's emptystring then no files artifact will be extracted
func NewNginxStaticContainerConfigFactory(filesArtifactId services.FilesArtifactID) *NginxStaticContainerConfigFactory {
	return &NginxStaticContainerConfigFactory{filesArtifactId: filesArtifactId}
}

func (factory NginxStaticContainerConfigFactory) GetCreationConfig(containerIpAddr string) (*services.ContainerCreationConfig, error) {
	result := services.NewContainerCreationConfigBuilder(
		dockerImage,
		testVolumeMountpoint,
	).WithUsedPorts(map[string]bool{
		strconv.Itoa(ListenPort): true,
	}).WithFilesArtifacts(map[services.FilesArtifactID]string{
		factory.filesArtifactId: nginxStaticFilesDirpath,
	}).Build()
	return result, nil
}

func (factory NginxStaticContainerConfigFactory) GetRunConfig(containerIpAddr string, generatedFileFilepaths map[string]string, staticFileFilepaths map[services.StaticFileID]string) (*services.ContainerRunConfig, error) {
	return services.NewContainerRunConfigBuilder().Build(), nil
}
