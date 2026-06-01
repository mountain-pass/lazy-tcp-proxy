package docker

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	composecli "github.com/compose-spec/compose-go/v2/cli"
	"github.com/docker/cli/cli/command"
	cliflags "github.com/docker/cli/cli/flags"
	composeapi "github.com/docker/compose/v2/pkg/api"
	composesvc "github.com/docker/compose/v2/pkg/compose"
	"github.com/moby/moby/client"
)

const (
	composeWaitInterval = time.Second
	composeWaitTimeout  = 30 * time.Second
)

// findComposeFile returns the first compose file found for name in composeDir,
// checking <dir>/<name>.yml then <dir>/<name>.yaml. Returns "" when neither exists.
func (m *Manager) findComposeFile(name string) string {
	for _, ext := range []string{".yml", ".yaml"} {
		p := filepath.Join(m.composeDir, name+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// loadImageFromTar loads a Docker image from a tar.gz archive via the Docker API,
// equivalent to `docker load -i <tarPath>`.
func (m *Manager) loadImageFromTar(ctx context.Context, name, tarPath string) error {
	log.Printf("docker: loading image for \033[33m%s\033[0m from %s", name, tarPath)
	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("opening image archive: %w", err)
	}
	defer f.Close() //nolint:errcheck

	result, err := m.cli.ImageLoad(ctx, f)
	if err != nil {
		return fmt.Errorf("loading image: %w", err)
	}
	defer result.Close() //nolint:errcheck
	if _, err := io.Copy(io.Discard, result); err != nil {
		return fmt.Errorf("reading image load response: %w", err)
	}
	log.Printf("docker: image loaded for \033[33m%s\033[0m", name)
	return nil
}

// waitForContainerRunning polls ContainerInspect until the named container is running,
// starting it directly if it is found in a non-running state (e.g. "created").
// This handles the case where compose Up() creates the container but errors before
// starting it, leaving the container in "Created" state.
// upErr is returned if the container never appears within composeWaitTimeout.
func (m *Manager) waitForContainerRunning(ctx context.Context, name string, upErr error) error {
	deadline := time.Now().Add(composeWaitTimeout)
	for {
		result, err := m.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
		if err == nil {
			if result.Container.State.Running {
				return nil
			}
			// Container exists but is not yet running — start it directly.
			log.Printf("docker: starting container \033[33m%s\033[0m after compose up", name)
			if _, startErr := m.cli.ContainerStart(ctx, name, client.ContainerStartOptions{}); startErr != nil {
				return fmt.Errorf("starting container after compose up: %w", startErr)
			}
			return nil
		}
		if time.Now().After(deadline) {
			if upErr != nil {
				return upErr
			}
			return fmt.Errorf("container %s did not appear after compose up", name)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(composeWaitInterval):
		}
	}
}

// reprovisionWithCompose re-provisions a missing container using a Docker Compose file
// at <composeDir>/<name>.yml (or .yaml).  Before running Compose Up it optionally loads
// <composeDir>/<name>.tar.gz when that file exists.
//
// Returns (true, nil) on success, (false, nil) when no compose file is found (caller
// should surface the original not-found error), or (true, err) when re-provisioning fails.
func (m *Manager) reprovisionWithCompose(ctx context.Context, name string) (bool, error) {
	if m.composeDir == "" {
		return false, nil
	}

	composeFile := m.findComposeFile(name)
	if composeFile == "" {
		return false, nil
	}

	// Load a custom image archive alongside the compose file if one is present.
	tarPath := filepath.Join(m.composeDir, name+".tar.gz")
	if _, err := os.Stat(tarPath); err == nil {
		if err := m.loadImageFromTar(ctx, name, tarPath); err != nil {
			return true, fmt.Errorf("loading image archive for %s: %w", name, err)
		}
	}

	log.Printf("docker: container \033[33m%s\033[0m missing — re-provisioning via %s", name, composeFile)

	dockerCLI, err := command.NewDockerCli(command.WithStandardStreams())
	if err != nil {
		return true, fmt.Errorf("creating docker cli for compose: %w", err)
	}
	clientOpts := cliflags.NewClientOptions()
	if sock := os.Getenv("DOCKER_SOCK"); sock != "" {
		clientOpts.Hosts = []string{"unix://" + sock}
	}
	if err := dockerCLI.Initialize(clientOpts); err != nil {
		return true, fmt.Errorf("initializing docker cli for compose: %w", err)
	}

	opts, err := composecli.NewProjectOptions(
		[]string{composeFile},
		composecli.WithWorkingDirectory(filepath.Dir(composeFile)),
		composecli.WithName(name),
	)
	if err != nil {
		return true, fmt.Errorf("building compose options for %s: %w", name, err)
	}
	project, err := composecli.ProjectFromOptions(ctx, opts)
	if err != nil {
		return true, fmt.Errorf("loading compose project for %s: %w", name, err)
	}

	svc := composesvc.NewComposeService(dockerCLI)
	upErr := svc.Up(ctx, project, composeapi.UpOptions{
		Create: composeapi.CreateOptions{Recreate: composeapi.RecreateDiverged},
	})
	if upErr != nil {
		log.Printf("docker: compose up for \033[33m%s\033[0m returned error (%v); checking container state", name, upErr)
	}

	// compose Up() may return an error after successfully creating the container
	// (internal lookup race). Poll until the container is running before giving up.
	if waitErr := m.waitForContainerRunning(ctx, name, upErr); waitErr != nil {
		return true, waitErr
	}

	log.Printf("docker: container \033[33m%s\033[0m re-provisioned successfully", name)
	return true, nil
}
