package godocker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/builder"
	"github.com/docker/docker/builder/dockerignore"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/fileutils"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/pkg/errors"
)

const (
	defaultDockerAPIVersion = "v1.23"
	defaultDockerfile       = "Dockerfile"
	defaultDockerHost       = "tcp://127.0.0.1:2376"
)

// ImageSummary of a docker image
type ImageSummary struct {
	Containers  int64             `json:"Containers"`
	Created     int64             `json:"Created"`
	ID          string            `json:"Id"`
	Labels      map[string]string `json:"Labels"`
	ParentID    string            `json:"ParentId"`
	RepoDigests []string          `json:"RepoDigests"`
	RepoTags    []string          `json:"RepoTags"`
	SharedSize  int64             `json:"SharedSize"`
	Size        int64             `json:"Size"`
	VirtualSize int64             `json:"VirtualSize"`
}

// Docker is an interface that contains some operations which can be used to build a image from source code
type Docker interface {
	Build(ctx context.Context, contextDirectory, imagePath string, args map[string]*string) error
	Pull(ctx context.Context, imagePath string) error
	Push(ctx context.Context, imagePath string) error
	List(ctx context.Context, filters map[string]string) ([]*ImageSummary, error)
	Tag(ctx context.Context, imagePath, newImagePath string) error
	Rmi(ctx context.Context, imagePath string) error
}

type dockerCmd struct {
	cli                *client.Client
	dockerHost         string
	registry           string
	registryAuthString string
	registryAuthMap    map[string]types.AuthConfig
	noCache            bool
	forceRm            bool
	pull               bool
}

// Configs is used to create the docker client
type Configs struct {
	Host     string
	Registry string
	User     string
	Passwd   string
}

// NewClient will return a docker image builder client
func NewClient(cfg Configs) (Docker, error) {
	cli, err := client.NewClient(cfg.Host, defaultDockerAPIVersion, nil, nil)
	if err != nil {
		return nil, err
	}
	auth := types.AuthConfig{
		Username: cfg.User,
		Password: cfg.Passwd,
	}
	authBytes, _ := json.Marshal(auth)
	authBase64 := base64.URLEncoding.EncodeToString(authBytes)

	docker := &dockerCmd{
		cli:                cli,
		dockerHost:         cfg.Host,
		registry:           cfg.Registry,
		registryAuthString: authBase64,
		registryAuthMap: map[string]types.AuthConfig{
			cfg.Registry: auth,
		},
		noCache: true,
		forceRm: true,
		pull:    true,
	}

	return docker, nil
}

func (docker *dockerCmd) Build(ctx context.Context, contextDirectory, imagePath string, args map[string]*string) error {
	dockerfile := defaultDockerfile

	buildCtx, err := CreateTar(contextDirectory, dockerfile)
	if err != nil {
		return err
	}
	defer buildCtx.Close()

	response, err := docker.cli.ImageBuild(ctx, buildCtx, types.ImageBuildOptions{
		Tags:        []string{imagePath},
		NoCache:     docker.noCache,
		Remove:      true,
		ForceRemove: docker.forceRm,
		PullParent:  docker.pull,
		Dockerfile:  defaultDockerfile,
		AuthConfigs: docker.registryAuthMap,
		BuildArgs:   args,
	})
	defer response.Body.Close()
	if err != nil {
		return err
	}

	//body, err := ioutil.ReadAll(response.Body)
	if err = detectErrorMessage(response.Body); err != nil {
		return err
	}

	return nil
}

func (docker *dockerCmd) Pull(ctx context.Context, imagePath string) error {
	resp, err := docker.cli.ImagePull(ctx, imagePath, types.ImagePullOptions{
	//RegistryAuth: docker.registryAuthString,
	})
	if resp != nil {
		defer resp.Close()
	}
	if err != nil {
		return err
	}
	if err = detectErrorMessage(resp); err != nil {
		return err
	}

	return nil
}

func (docker *dockerCmd) Push(ctx context.Context, imagePath string) error {
	resp, err := docker.cli.ImagePush(ctx, imagePath, types.ImagePushOptions{
		RegistryAuth: docker.registryAuthString,
	})
	if resp != nil {
		defer resp.Close()
	}
	if err != nil {
		return err
	}
	//body, err := ioutil.ReadAll(resp)
	if err = detectErrorMessage(resp); err != nil {
		return err
	}

	return nil
}

func (docker *dockerCmd) List(ctx context.Context, filter map[string]string) ([]*ImageSummary, error) {
	args := filters.NewArgs()
	for k, v := range filter {
		args.Add(k, v)
	}
	imageSummaryList, err := docker.cli.ImageList(ctx, types.ImageListOptions{
		Filters: args,
	})
	if err != nil {
		return nil, err
	}
	var imageSummaryPointerList []*ImageSummary
	for _, summary := range imageSummaryList {
		imageSummaryPointerList = append(imageSummaryPointerList, &ImageSummary{
			Containers:  summary.Containers,
			Created:     summary.Created,
			ID:          summary.ID,
			Labels:      summary.Labels,
			ParentID:    summary.ParentID,
			RepoDigests: summary.RepoDigests,
			RepoTags:    summary.RepoTags,
			SharedSize:  summary.SharedSize,
			Size:        summary.Size,
			VirtualSize: summary.VirtualSize,
		})
	}
	return imageSummaryPointerList, nil
}
func (docker *dockerCmd) Tag(ctx context.Context, imagePath, newImagePath string) error {
	err := docker.cli.ImageTag(ctx, imagePath, newImagePath)
	if err != nil {
		return err
	}
	return nil
}
func (docker *dockerCmd) Rmi(ctx context.Context, imagePath string) error {
	_, err := docker.cli.ImageRemove(ctx, imagePath, types.ImageRemoveOptions{})
	if err != nil {
		return err
	}
	return nil
}

// CreateTar create a build context tar for the specified project and service name.
func CreateTar(contextDirectory, dockerfile string) (io.ReadCloser, error) {
	dockerfileName := filepath.Join(contextDirectory, dockerfile)

	absContextDirectory, err := filepath.Abs(contextDirectory)
	if err != nil {
		return nil, err
	}

	filename := dockerfileName

	if dockerfile == "" {
		dockerfileName = defaultDockerfile
		filename = filepath.Join(absContextDirectory, dockerfileName)

		// Just to be nice ;-) look for 'dockerfile' too but only
		// use it if we found it, otherwise ignore this check
		if _, err = os.Lstat(filename); os.IsNotExist(err) {
			tmpFN := path.Join(absContextDirectory, strings.ToLower(dockerfileName))
			if _, err = os.Lstat(tmpFN); err == nil {
				dockerfileName = strings.ToLower(dockerfileName)
				filename = tmpFN
			}
		}
	}

	origDockerfile := dockerfileName // used for error msg
	if filename, err = filepath.Abs(filename); err != nil {
		return nil, err
	}

	// Now reset the dockerfileName to be relative to the build context
	dockerfileName, err = filepath.Rel(absContextDirectory, filename)
	if err != nil {
		return nil, err
	}

	// And canonicalize dockerfile name to a platform-independent one
	dockerfileName, err = archive.CanonicalTarNameForPath(dockerfileName)
	if err != nil {
		return nil, fmt.Errorf("Cannot canonicalize dockerfile path %s: %v", dockerfileName, err)
	}

	if _, err = os.Lstat(filename); os.IsNotExist(err) {
		return nil, fmt.Errorf("Cannot locate Dockerfile: %s", origDockerfile)
	}
	var includes = []string{"."}
	var excludes []string

	dockerIgnorePath := path.Join(contextDirectory, ".dockerignore")
	dockerIgnore, err := os.Open(dockerIgnorePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		excludes = make([]string, 0)
	} else {
		excludes, err = dockerignore.ReadAll(dockerIgnore)
		if err != nil {
			return nil, err
		}
	}

	// If .dockerignore mentions .dockerignore or the Dockerfile
	// then make sure we send both files over to the daemon
	// because Dockerfile is, obviously, needed no matter what, and
	// .dockerignore is needed to know if either one needs to be
	// removed.  The deamon will remove them for us, if needed, after it
	// parses the Dockerfile.
	keepThem1, _ := fileutils.Matches(".dockerignore", excludes)
	keepThem2, _ := fileutils.Matches(dockerfileName, excludes)
	if keepThem1 || keepThem2 {
		includes = append(includes, ".dockerignore", dockerfileName)
	}

	if err := builder.ValidateContextDirectory(contextDirectory, excludes); err != nil {
		return nil, fmt.Errorf("Error checking context is accessible: '%s'. Please check permissions and try again.", err)
	}

	options := &archive.TarOptions{
		Compression:     archive.Uncompressed,
		ExcludePatterns: excludes,
		IncludeFiles:    includes,
	}

	return archive.TarWithOptions(contextDirectory, options)
}

func detectErrorMessage(in io.Reader) error {
	dec := json.NewDecoder(in)

	for {
		var jm jsonmessage.JSONMessage
		if err := dec.Decode(&jm); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		//log.Printf("docker resp: %+v", jm)
		// skip progress message
		//if jm.Progress == nil {
		//glog.Infof("%v", jm)
		//}
		if jm.Error != nil {
			return jm.Error
		}

		if len(jm.ErrorMessage) > 0 {
			return errors.New(jm.ErrorMessage)
		}
	}
	return nil
}
