package runner

import (
	"context"

	"github.com/tinyci/ci-agents/errors"
	"github.com/tinyci/ci-agents/types"
	fwConfig "github.com/tinyci/ci-runners/fw/config"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Config encapsulates the full managed and specified configuration
type Config struct {
	C              fwConfig.Config `yaml:"c,inline"`
	KubeConfig     string          `yaml:"kubeconfig"`
	Namespace      string          `yaml:"namespace"`
	MaxConcurrency uint            `yaml:"max_concurrency"`
	Resources      types.Resources `yaml:"max_resources"`
	k8s            *rest.Config
}

// Config returns the underlying framework configuration, and matches the
// Configurator interface.
func (c *Config) Config() *fwConfig.Config {
	return &c.C
}

// ExtraLoad also conforms to the underlying framework configurator interface.
// It is used to import the kubeconfig. If the kubeconfig is not specified, it
// assumes it is running inside of kubernetes and attempts to get the
// associated client credential.
func (c *Config) ExtraLoad() *errors.Error {
	if c.MaxConcurrency == 0 {
		c.C.Clients.Log.Info(context.Background(), "max_concurrency not set; defaulting to 1")
		c.MaxConcurrency = 1
	}

	if c.Namespace == "" {
		return errors.New("the k8s runner requires a namespace be provided to it; provide 'default' if you really wish to run in the default namespace")
	}

	var err error
	if c.KubeConfig != "" {
		c.k8s, err = clientcmd.BuildConfigFromFlags("", c.KubeConfig)
		return errors.New(err)
	}

	c.k8s, err = rest.InClusterConfig()
	return errors.New(err)
}

// Client retrieves a pre-built kubernetes client ready for use, or error.
func (c *Config) Client() (*kubernetes.Clientset, *errors.Error) {
	cs, err := kubernetes.NewForConfig(c.k8s)
	if err != nil {
		return nil, errors.New(err).Wrap("could not create scheme client")
	}

	return cs, nil
}

// SchemeClient returns a client prepped with the tinyCI API.
func (c *Config) SchemeClient() (client.Client, *errors.Error) {
	cs, err := client.New(c.k8s, client.Options{Scheme: v1Scheme})
	if err != nil {
		return nil, errors.New(err).Wrap("could not create scheme client")
	}

	return cs, nil
}
