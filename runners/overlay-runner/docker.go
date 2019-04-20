package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/fatih/color"
	"github.com/tinyci/ci-runners/overlay"
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

	pullRead, err := client.ImagePull(r.Context, img, types.ImagePullOptions{})
	if err != nil {
		fmt.Println(err)
		return "", err
	}

	if err := outputPullRead(pw, pullRead); err != nil {
		return "", err
	}

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

	// since this may already not exist; this can error for the right reasons
	client.ContainerRemove(context.Background(), "running", types.ContainerRemoveOptions{Force: true})

	resp, err := client.ContainerCreate(r.Context, config, hostconfig, &network.NetworkingConfig{}, "running")
	if err != nil {
		return err
	}

	r.ContainerID = resp.ID
	attach, err := client.ContainerAttach(r.Context, r.ContainerID, types.ContainerAttachOptions{Stream: true, Stdin: true, Stdout: true, Stderr: true})
	if err != nil {
		return err
	}

	go io.Copy(pw, attach.Reader)

	if err := client.ContainerStart(r.Context, r.ContainerID, types.ContainerStartOptions{}); err != nil {
		return err
	}

	if err := client.ContainerResize(r.Context, r.ContainerID, types.ResizeOptions{Height: 25, Width: 80}); err != nil {
		return err
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

	client, err := client.NewEnvClient()
	if err != nil {
		return err
	}

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

	img, err := r.pullImage(client, pw)
	if err != nil {
		return err
	}

	if err := r.boot(client, pw, img, m); err != nil {
		return err
	}

	return r.supervise(client, m)
}

func (r *Run) supervise(client *client.Client, m *overlay.Mount) error {
	errChan := make(chan error, 1)

	go func() {
		<-r.Context.Done()

		if err := client.ContainerRemove(context.Background(), r.ContainerID, types.ContainerRemoveOptions{Force: true}); err != nil {
			errChan <- err
		}
	}()

	exit, waitErr := client.ContainerWait(r.Context, r.ContainerID)
	if waitErr != nil {
		select {
		case err := <-errChan: // there can be more than one error here, but we don't care
			return err
		default:
			return waitErr
		}
	}

	if exit == 0 {
		r.Status = true
	}

	return nil
}
