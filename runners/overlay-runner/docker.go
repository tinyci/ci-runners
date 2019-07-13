package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/fatih/color"
	"github.com/tinyci/ci-runners/fw/overlay"
)

func processLine(m map[string]interface{}, idMap map[string][]float64) bool {
	var completed bool

	if status, ok := m["status"].(string); ok && status != "" {
		if status == "Pull complete" {
			completed = true
		} else if status != "Downloading" {
			return true // continue
		}
	} else {
		return true // continue
	}

	if id, ok := m["id"].(string); ok && id != "" {
		if completed {
			if _, ok := idMap[id]; ok {
				idMap[id] = []float64{idMap[id][1], idMap[id][1]}
			} else {
				idMap[id] = []float64{1, 1}
			}
		} else if pd, ok := m["progressDetail"].(map[string]interface{}); ok && pd != nil {
			if len(pd) != 0 {
				current, _ := pd["current"].(float64)
				total, _ := pd["total"].(float64)
				idMap[id] = []float64{current, total}
			}
		}
	}

	return false
}

func outputPullRead(w io.Writer, r io.Reader) error {
	fmt.Fprintln(w)
	defer fmt.Fprint(w, color.New(color.FgGreen).Sprint("\nCompleted pull of docker image\n\n"))
	// map id -> progress report (two floats, current and total)
	idMap := map[string][]float64{}

	s := bufio.NewScanner(r)
	for s.Scan() {
		m := map[string]interface{}{}
		if err := json.Unmarshal(s.Bytes(), &m); err != nil {
			return err
		}

		if processLine(m, idMap) {
			continue
		}

		var cur, sum float64
		for _, val := range idMap {
			cur += val[0]
			sum += val[1]
		}

		if sum != 0 {
			fmt.Fprintf(
				w,
				"%s%s",
				color.New(color.FgHiMagenta, color.Bold).Sprintf("\rPulling Docker Image: "),
				color.New(color.FgHiCyan).Sprintf("%0.2f%%", (cur/sum)*100),
			)
		}
	}

	return nil
}

func (r *Run) pullImage(client *client.Client, pw *io.PipeWriter) (string, error) {
	img := r.QueueItem.Run.RunSettings.Image
	if strings.Count(img, "/") <= 1 {
		// probably a docker image, since there is no leading / for the hostname.
		// Prefix it with the docker.io hostname.
		// FIXME maybe make this configurable later.

		if strings.Count(img, "/") == 0 { // official docker image
			img = fmt.Sprintf("library/%s", img)
		}

		img = fmt.Sprintf("docker.io/%s", img)
	}

	start := time.Now()
	r.Logger.Debugf(context.Background(), "starting pull of image %v", img)

	pullRead, err := client.ImagePull(r.Context, img, types.ImagePullOptions{})
	if err != nil {
		return "", err
	}
	defer pullRead.Close()

	if err := outputPullRead(pw, pullRead); err != nil {
		r.Logger.Errorf(context.Background(), "pull of image %v failed with error: %v", img, err)
		return "", err
	}

	r.Logger.Debugf(context.Background(), "pull of image %v succeeded in %v", img, time.Since(start))

	return img, nil
}

func (r *Run) boot(client *client.Client, pw *io.PipeWriter, img string, m *overlay.Mount) error {
	config := &container.Config{
		AttachStdin:  true,
		AttachStderr: true,
		AttachStdout: true,
		Tty:          true,
		Image:        img,
		WorkingDir:   r.QueueItem.Run.Task.TaskSettings.WorkDir,
		StopSignal:   "KILL",
		Cmd:          r.QueueItem.Run.RunSettings.Command,
		Env:          r.QueueItem.Run.Task.TaskSettings.Env,
	}

	hostconfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: m.Target,
				Target: r.QueueItem.Run.Task.TaskSettings.Mountpoint,
			},
		},
		AutoRemove: true,
	}

	client.ContainerRemove(r.Context, "running", types.ContainerRemoveOptions{Force: true})

	var outErr error

	for i := 0; i < 5; i++ {
		resp, err := client.ContainerCreate(r.Context, config, hostconfig, &network.NetworkingConfig{}, "running")
		if err != nil {
			r.Logger.Errorf(context.Background(), "could not create container, retrying: %v", err)
			outErr = err
			time.Sleep(time.Second)
			continue
		}

		r.ContainerID = resp.ID
		outErr = nil
		break
	}

	if outErr != nil {
		r.Logger.Errorf(context.Background(), "could not create container, giving up: %v", outErr)
		return outErr
	}

	go func() {
		for {
			select {
			case <-r.Context.Done():
				return
			default:
			}

			attach, err := client.ContainerAttach(r.Context, r.ContainerID, types.ContainerAttachOptions{Stream: true, Stdin: true, Stdout: true, Stderr: true})
			if err != nil {
				attach.Close()
				r.Logger.Errorf(context.Background(), "error during attach, trying re-attach soon: %v", err)
				time.Sleep(time.Second)
				continue
			}

			io.Copy(pw, attach.Reader)
			r.Logger.Debug(context.Background(), "attach closed; returning gracefully")
			attach.Close()
			return
		}
	}()

	if err := client.ContainerStart(r.Context, r.ContainerID, types.ContainerStartOptions{}); err != nil {
		r.Logger.Errorf(context.Background(), "could not start container: %v", err)
		return err
	}

	if err := client.ContainerResize(r.Context, r.ContainerID, types.ResizeOptions{Height: 25, Width: 80}); err != nil {
		r.Logger.Errorf(context.Background(), "could not resize container's tty, skipping: %v", err)
	}

	return nil
}

// RunDocker runs the queue item in docker, pulling any necessary content to do so.
func (r *Run) RunDocker() error {
	defer func() {
		select {
		case <-r.Context.Done():
			return // cancel func handler will do this
		default:
			r.Cancel()
		}
	}()

	r.StartCancelFunc()

	pr, pw := io.Pipe()
	defer pw.Close()
	r.StartLogger(pr)

	gr, err := r.PullRepo(pw)
	if err != nil {
		return err
	}

	m, err := r.MountRepo(gr)
	if err != nil {
		return err
	}
	defer r.MountCleanup(m)

	img, err := r.pullImage(r.Docker, pw)
	if err != nil {
		r.Logger.Errorf(context.Background(), "could not pull image: %v", err)
		return err
	}

	if err := r.boot(r.Docker, pw, img, m); err != nil {
		r.Logger.Errorf(context.Background(), "could not boot container: %v", err)
		return err
	}

	return r.supervise(r.Docker, m)
}

func (r *Run) supervise(client *client.Client, m *overlay.Mount) error {
	defer time.Sleep(5 * time.Second) // work around a race in docker's deletion code
	errChan := make(chan error, 1)

	go func() {
		<-r.Context.Done()

		if err := client.ContainerRemove(context.Background(), r.ContainerID, types.ContainerRemoveOptions{Force: true}); err != nil {
			errChan <- err
		}
	}()

	exit, waitErr := client.ContainerWait(r.Context, r.ContainerID, container.WaitConditionNotRunning)
	select {
	case err := <-waitErr:
		r.Logger.Errorf(context.Background(), "error waiting with cleanup of cid %v: %v", r.ContainerID, err)
		return err
	case err := <-errChan: // there can be more than one error here, but we don't care
		r.Logger.Errorf(context.Background(), "error with cleanup of cid %v: %v", r.ContainerID, err)
		return err
	case ec := <-exit:
		if ec.StatusCode == 0 {
			r.Status = true
		}

		return nil
	}
}
