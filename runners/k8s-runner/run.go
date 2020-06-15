package runner

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/tinyci/ci-agents/clients/log"
	"github.com/tinyci/ci-agents/errors"
	fwcontext "github.com/tinyci/ci-runners/fw/context"
	v1 "github.com/tinyci/k8s-api/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Run is the encapsulation of a single run.
type Run struct {
	name   string
	runCtx *fwcontext.RunContext
	ctx    context.Context
	logger *log.SubLogger
	runner *Runner
}

func (r *Run) String() string {
	return r.Name()
}

// Name is the name of the run
func (r *Run) Name() string {
	return r.name
}

// RunContext returns the assigned *context.RunContext for this run.
func (r *Run) RunContext() *fwcontext.RunContext {
	return r.runCtx
}

// BeforeRun is executed before the next run is started.
func (r *Run) BeforeRun() *errors.Error {
	return nil
}

// AfterRun is executed before the next run is started.
func (r *Run) AfterRun() *errors.Error {
	return nil
}

func (r *Run) copyLog(job *v1.CIJob) {
	r.logger.Infof(r.ctx, "establishing log connection to assetsvc")

	cs, err := r.runner.Config.Client()
	if err != nil {
		r.logger.Errorf(r.ctx, "while getting core client: %v", err.Error())
		return
	}

	res := cs.CoreV1().Pods(r.runner.Config.Namespace).GetLogs(job.Status.PodName, &corev1.PodLogOptions{Follow: true})

	var reader io.ReadCloser

	for {
		var err error
		reader, err = res.Stream(r.ctx)
		if err != nil {
			r.logger.Errorf(r.ctx, "while configuring stream reader: %v", err.Error())
			time.Sleep(time.Second)
			continue
		}

		break
	}

	if err := r.runner.Config.C.Clients.Asset.Write(r.ctx, r.runCtx.QueueItem.Run.ID, reader); err != nil {
		r.logger.Errorf(r.ctx, "while writing log to asset service: %v", err.Error())
		return
	}
}

func (r *Run) makeResources() (corev1.ResourceList, *errors.Error) {
	resourceList := corev1.ResourceList{}
	resources := r.runCtx.QueueItem.Run.RunSettings.Resources

	m := map[corev1.ResourceName]string{
		corev1.ResourceCPU:     resources.CPU,
		corev1.ResourceMemory:  resources.Memory,
		corev1.ResourceStorage: resources.Disk,
	}

	for resName, res := range m {
		if res != "" {
			var err error

			resourceList[resName], err = resource.ParseQuantity(res)
			if err != nil {
				return resourceList, errors.New(err).Wrapf("while trying to parse %q parameters", resName)
			}
		}
	}

	return resourceList, nil
}

func (r *Run) cleanup(jobName types.NamespacedName, secret *corev1.Secret) *errors.Error {
	r.logger.Infof(context.Background(), "Cleanup of completed job %q (secretName: %q) commencing", jobName, secret.Name)

	c, err := r.runner.Config.SchemeClient()
	if err != nil {
		return errors.New(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	job := &v1.CIJob{}

	if err := c.Get(ctx, jobName, job); err == nil {
		if err := c.Delete(ctx, job); err != nil {
			return errors.New(err)
		}
	}

	if err := c.Delete(ctx, secret); err != nil {
		return errors.New(err)
	}

	return nil
}

// Run runs the CI job.
func (r *Run) Run() (bool, *errors.Error) {
	sub := r.runCtx.QueueItem.Run.Task.Submission

	jobName := fmt.Sprintf("%s-%d", r.runCtx.QueueItem.QueueName, r.runCtx.QueueItem.ID)
	secretName := jobName + "-secret"

	env := r.runCtx.QueueItem.Run.Task.TaskSettings.Env
	if env == nil {
		env = []string{}
	}

	resourceList, err := r.makeResources()
	if err != nil {
		return false, err.Wrap("could not parse resources")
	}

	jobSpec := v1.CIJobSpec{
		Image:   r.runCtx.QueueItem.Run.RunSettings.Image,
		Command: r.runCtx.QueueItem.Run.RunSettings.Command,
		Repository: v1.CIJobRepository{
			URL:        sub.HeadRef.Repository.Github.GetCloneURL(),
			SecretName: secretName,
			HeadSHA:    sub.HeadRef.SHA,
			HeadBranch: strings.TrimLeft(strings.TrimLeft(sub.HeadRef.RefName, "heads/"), "tags/"),
		},
		WorkingDir:  r.runCtx.QueueItem.Run.Task.TaskSettings.WorkDir,
		Environment: env,
		Resources:   resourceList,
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.runner.Config.Namespace,
			Name:      secretName,
		},
		StringData: map[string]string{
			"username": sub.User.Token.Username,
			"password": sub.User.Token.Token,
		},
	}

	job := &v1.CIJob{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: r.runner.Config.Namespace,
			Name:      jobName,
		},
		Spec: jobSpec,
	}

	c, err := r.runner.Config.SchemeClient()
	if err != nil {
		return false, errors.New(err)
	}

	if err := c.Create(r.ctx, secret); err != nil {
		return false, errors.New(err)
	}

	if err := c.Create(r.ctx, job); err != nil {
		return false, errors.New(err)
	}

	nsName := types.NamespacedName{Namespace: r.runner.Config.Namespace, Name: jobName}

	defer func() {
		if err := r.cleanup(nsName, secret); err != nil {
			r.logger.Errorf(context.Background(), "Error during cleanup: %v", err)
		}
	}()

	var logCopy bool

	for {
		select {
		case <-r.runCtx.Ctx.Done():
			return false, nil
		default:
			time.Sleep(time.Second)
		}

		job := &v1.CIJob{}

		if err := c.Get(context.Background(), nsName, job); err != nil {
			return false, errors.New(err)
		}

		if job.Status.PodName == "" && !job.Status.Canceled && !job.Status.Finished {
			continue
		}

		if job.Status.PodName != "" && !logCopy {
			logCopy = true
			go r.copyLog(job)
		}

		if job.Status.Finished {
			r.logger.Infof(context.Background(), "Job completed with status: %v", job.Status.Success)
			return job.Status.Success, nil
		}
	}
}
